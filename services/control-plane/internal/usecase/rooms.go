package usecase

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"

	"github.com/vks4/vks4/services/control-plane/internal/adapters/mediarpc"
	"github.com/vks4/vks4/services/control-plane/internal/adapters/repo/postgres"
	redisrepo "github.com/vks4/vks4/services/control-plane/internal/adapters/repo/redisrepo"
	"github.com/vks4/vks4/services/control-plane/internal/adapters/signalingrpc"
	"github.com/vks4/vks4/services/control-plane/internal/domain"
	apperr "github.com/vks4/vks4/services/control-plane/internal/pkg/errors"
)

type Rooms struct {
	repo      *postgres.Rooms
	presence  *redisrepo.Presence
	audit     *postgres.Audit
	media     mediarpc.Client
	signaling *signalingrpc.Client
	pubURL    string
}

func NewRooms(r *postgres.Rooms, p *redisrepo.Presence, a *postgres.Audit, m mediarpc.Client, sig *signalingrpc.Client, pubURL string) *Rooms {
	return &Rooms{repo: r, presence: p, audit: a, media: m, signaling: sig, pubURL: pubURL}
}

func (r *Rooms) hydrate(room domain.Room) domain.Room {
	room.JoinURL = fmt.Sprintf("%s/r/%s", r.pubURL, room.ID)
	return room
}

func (r *Rooms) List(ctx context.Context, q string, limit, offset int) ([]domain.Room, int, error) {
	rows, total, err := r.repo.List(ctx, q, limit, offset)
	if err != nil {
		return nil, 0, apperr.Wrap(apperr.CodeInternal, "db", err)
	}
	out := make([]domain.Room, 0, len(rows))
	for _, row := range rows {
		out = append(out, r.hydrate(row.Domain()))
	}
	return out, total, nil
}

func (r *Rooms) Get(ctx context.Context, id uuid.UUID) (*domain.Room, error) {
	row, err := r.repo.Get(ctx, id)
	if err != nil {
		return nil, apperr.Wrap(apperr.CodeInternal, "db", err)
	}
	if row == nil {
		return nil, apperr.New(apperr.CodeNotFound, "room not found")
	}
	d := r.hydrate(row.Domain())
	return &d, nil
}

type CreateRoomInput struct {
	Name            string
	Description     string
	Mode            domain.RoomMode
	MaxParticipants int
	WaitingRoom     bool
	DefaultLayout   *domain.Layout
}

func (r *Rooms) Create(ctx context.Context, actor uuid.UUID, in CreateRoomInput) (*domain.Room, error) {
	if in.Name == "" {
		return nil, apperr.New(apperr.CodeInvalidArgument, "name required")
	}
	if in.Mode == "" {
		in.Mode = domain.RoomMeeting
	}
	if in.MaxParticipants <= 0 {
		in.MaxParticipants = 50
	}
	layout := domain.Layout{Mode: domain.LayoutGrid, Width: 1280, Height: 720}
	if in.DefaultLayout != nil {
		layout = *in.DefaultLayout
	}
	lb, _ := json.Marshal(layout)
	row, err := r.repo.Create(ctx, postgres.CreateRoomInput{
		Name: in.Name, Description: in.Description, Mode: string(in.Mode),
		MaxParticipants: in.MaxParticipants, WaitingRoom: in.WaitingRoom,
		DefaultLayout: lb, CreatedBy: actor,
	})
	if err != nil {
		return nil, apperr.Wrap(apperr.CodeInternal, "db", err)
	}
	d := r.hydrate(row.Domain())
	if err := r.media.CreateRoom(ctx, d); err != nil {
		// не fail-нем создание комнаты, но логируем; media создаст её лениво при первом peer
		_ = r.audit.Write(ctx, postgres.AuditWrite{ActorID: &actor, Action: "rooms.create.mediafail",
			Target: row.ID.String(), Payload: map[string]any{"err": err.Error()}})
	}
	_ = r.audit.Write(ctx, postgres.AuditWrite{ActorID: &actor, Action: "rooms.create", Target: row.ID.String()})
	return &d, nil
}

func (r *Rooms) Update(ctx context.Context, actor, id uuid.UUID, set map[string]any) (*domain.Room, error) {
	if v, ok := set["default_layout"]; ok {
		b, _ := json.Marshal(v)
		set["default_layout"] = b
	}
	if err := r.repo.Update(ctx, id, set); err != nil {
		return nil, apperr.Wrap(apperr.CodeInternal, "db", err)
	}
	_ = r.audit.Write(ctx, postgres.AuditWrite{ActorID: &actor, Action: "rooms.update", Target: id.String(), Payload: set})
	return r.Get(ctx, id)
}

func (r *Rooms) Delete(ctx context.Context, actor, id uuid.UUID) error {
	if err := r.repo.Delete(ctx, id); err != nil {
		return apperr.Wrap(apperr.CodeInternal, "db", err)
	}
	_ = r.media.DestroyRoom(ctx, id)
	_ = r.presence.Clear(ctx, id)
	_ = r.audit.Write(ctx, postgres.AuditWrite{ActorID: &actor, Action: "rooms.delete", Target: id.String()})
	return nil
}

func (r *Rooms) SetLocked(ctx context.Context, actor, id uuid.UUID, locked bool) error {
	if err := r.repo.Update(ctx, id, map[string]any{"locked": locked}); err != nil {
		return apperr.Wrap(apperr.CodeInternal, "db", err)
	}
	_ = r.audit.Write(ctx, postgres.AuditWrite{ActorID: &actor, Action: "rooms.lock", Target: id.String(),
		Payload: map[string]any{"locked": locked}})
	return nil
}

func (r *Rooms) UpdateLayout(ctx context.Context, actor, id uuid.UUID, layout domain.Layout) error {
	lb, _ := json.Marshal(layout)
	if err := r.repo.Update(ctx, id, map[string]any{"default_layout": lb}); err != nil {
		return apperr.Wrap(apperr.CodeInternal, "db", err)
	}
	if err := r.media.UpdateLayout(ctx, id, layout); err != nil {
		_ = r.audit.Write(ctx, postgres.AuditWrite{ActorID: &actor, Action: "rooms.layout.mediafail",
			Target: id.String(), Payload: map[string]any{"err": err.Error()}})
	}
	// Уведомляем всех клиентов в комнате через signaling — они применят layout сразу.
	if r.signaling != nil {
		if err := r.signaling.Broadcast(ctx, id.String(), "layout", layout); err != nil {
			_ = r.audit.Write(ctx, postgres.AuditWrite{ActorID: &actor, Action: "rooms.layout.broadcastfail",
				Target: id.String(), Payload: map[string]any{"err": err.Error()}})
		}
	}
	_ = r.audit.Write(ctx, postgres.AuditWrite{ActorID: &actor, Action: "rooms.layout", Target: id.String(),
		Payload: map[string]any{"mode": layout.Mode}})
	return nil
}

func (r *Rooms) Participants(ctx context.Context, id uuid.UUID) ([]redisrepo.Participant, error) {
	// Side-effect: дополнительно пушим текущий layout из БД в media-worker.
	// Это идемпотентно (повторное применение тех же cells не делает ничего вредного)
	// и решает проблему «после рестарта media-worker раскладка пропадает» — при первом
	// же poll'е Room Control от фронтенда media-worker получает свежую геометрию cells.
	go func() {
		bg, cancel := context.WithTimeout(context.Background(), 5*1e9)
		defer cancel()
		if row, err := r.repo.Get(bg, id); err == nil && row != nil {
			d := row.Domain()
			if len(d.DefaultLayout.Cells) > 0 {
				_ = r.media.UpdateLayout(bg, id, d.DefaultLayout)
			}
		}
	}()
	return r.presence.List(ctx, id)
}

// Slots — текущий slot→peerID mapping комнаты, читается из media-worker
// (он хранит in-memory). Используется Room Control'ом для инициализации state'а
// после перезагрузки страницы.
func (r *Rooms) Slots(ctx context.Context, id uuid.UUID) (map[string]string, error) {
	return r.media.GetRoster(ctx, id)
}

// AssignSlots — оператор задаёт slot→peerID mapping для комнаты.
// Сервер media-worker применит и сразу обновит compositor pad'ы для активных peer'ов.
func (r *Rooms) AssignSlots(ctx context.Context, actor, id uuid.UUID, slots map[int]string) error {
	if err := r.media.AssignSlots(ctx, id, slots); err != nil {
		return apperr.Wrap(apperr.CodeUnavailable, "media slots", err)
	}
	_ = r.audit.Write(ctx, postgres.AuditWrite{ActorID: &actor, Action: "rooms.slots", Target: id.String(),
		Payload: map[string]any{"slots": slots}})
	// Также broadcast через signaling чтобы клиенты сразу обновили UI roster.
	if r.signaling != nil {
		_ = r.signaling.Broadcast(ctx, id.String(), "slots", slots)
	}
	return nil
}

func (r *Rooms) Kick(ctx context.Context, actor, id uuid.UUID, peerID string) error {
	if err := r.media.KickPeer(ctx, id, peerID); err != nil {
		return apperr.Wrap(apperr.CodeUnavailable, "media kick", err)
	}
	_ = r.presence.Remove(ctx, id, peerID)
	_ = r.audit.Write(ctx, postgres.AuditWrite{ActorID: &actor, Action: "rooms.kick", Target: id.String(),
		Payload: map[string]any{"peerId": peerID}})
	return nil
}
