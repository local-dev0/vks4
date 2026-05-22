package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vks4/vks4/services/control-plane/internal/domain"
)

type Rooms struct{ db *pgxpool.Pool }

func NewRooms(db *pgxpool.Pool) *Rooms { return &Rooms{db: db} }

type RoomRow struct {
	ID              uuid.UUID
	Name            string
	Description     string
	Mode            string
	MaxParticipants int
	WaitingRoom     bool
	Locked          bool
	Recording       bool
	DefaultLayout   []byte
	CreatedBy       *uuid.UUID
	CreatedAt       time.Time
}

func (r RoomRow) Domain() domain.Room {
	var l domain.Layout
	_ = json.Unmarshal(r.DefaultLayout, &l)
	return domain.Room{
		ID:              r.ID,
		Name:            r.Name,
		Description:     r.Description,
		Mode:            domain.RoomMode(r.Mode),
		MaxParticipants: r.MaxParticipants,
		WaitingRoom:     r.WaitingRoom,
		Locked:          r.Locked,
		Recording:       r.Recording,
		DefaultLayout:   l,
		CreatedBy:       r.CreatedBy,
		CreatedAt:       r.CreatedAt,
	}
}

func (s *Rooms) List(ctx context.Context, q string, limit, offset int) ([]RoomRow, int, error) {
	var rows pgx.Rows
	var err error
	if q == "" {
		rows, err = s.db.Query(ctx, `
			SELECT id,name,description,mode,max_participants,waiting_room,locked,recording,default_layout,created_by,created_at
			FROM rooms ORDER BY created_at DESC LIMIT $1 OFFSET $2`, limit, offset)
	} else {
		rows, err = s.db.Query(ctx, `
			SELECT id,name,description,mode,max_participants,waiting_room,locked,recording,default_layout,created_by,created_at
			FROM rooms WHERE name ILIKE '%'||$1||'%' OR description ILIKE '%'||$1||'%'
			ORDER BY created_at DESC LIMIT $2 OFFSET $3`, q, limit, offset)
	}
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []RoomRow
	for rows.Next() {
		r := RoomRow{}
		if err := rows.Scan(&r.ID, &r.Name, &r.Description, &r.Mode, &r.MaxParticipants,
			&r.WaitingRoom, &r.Locked, &r.Recording, &r.DefaultLayout, &r.CreatedBy, &r.CreatedAt); err != nil {
			return nil, 0, err
		}
		out = append(out, r)
	}
	var total int
	if err := s.db.QueryRow(ctx, `SELECT COUNT(*) FROM rooms`).Scan(&total); err != nil {
		return nil, 0, err
	}
	return out, total, rows.Err()
}

func (s *Rooms) Get(ctx context.Context, id uuid.UUID) (*RoomRow, error) {
	row := s.db.QueryRow(ctx, `
		SELECT id,name,description,mode,max_participants,waiting_room,locked,recording,default_layout,created_by,created_at
		FROM rooms WHERE id=$1`, id)
	r := RoomRow{}
	err := row.Scan(&r.ID, &r.Name, &r.Description, &r.Mode, &r.MaxParticipants,
		&r.WaitingRoom, &r.Locked, &r.Recording, &r.DefaultLayout, &r.CreatedBy, &r.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &r, nil
}

type CreateRoomInput struct {
	Name            string
	Description     string
	Mode            string
	MaxParticipants int
	WaitingRoom     bool
	DefaultLayout   []byte
	CreatedBy       uuid.UUID
}

func (s *Rooms) Create(ctx context.Context, in CreateRoomInput) (*RoomRow, error) {
	row := s.db.QueryRow(ctx, `
		INSERT INTO rooms (name,description,mode,max_participants,waiting_room,default_layout,created_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7)
		RETURNING id,name,description,mode,max_participants,waiting_room,locked,recording,default_layout,created_by,created_at`,
		in.Name, in.Description, in.Mode, in.MaxParticipants, in.WaitingRoom, in.DefaultLayout, in.CreatedBy)
	r := RoomRow{}
	if err := row.Scan(&r.ID, &r.Name, &r.Description, &r.Mode, &r.MaxParticipants,
		&r.WaitingRoom, &r.Locked, &r.Recording, &r.DefaultLayout, &r.CreatedBy, &r.CreatedAt); err != nil {
		return nil, err
	}
	return &r, nil
}

func (s *Rooms) Update(ctx context.Context, id uuid.UUID, set map[string]any) error {
	if len(set) == 0 {
		return nil
	}
	// Простая динамическая сборка (whitelisted keys).
	allowed := map[string]bool{
		"name": true, "description": true, "mode": true,
		"max_participants": true, "waiting_room": true,
		"default_layout": true, "locked": true, "recording": true,
	}
	sql := "UPDATE rooms SET updated_at=now()"
	args := []any{}
	i := 1
	for k, v := range set {
		if !allowed[k] {
			continue
		}
		sql += ", " + k + "=$" + itoa(i)
		args = append(args, v)
		i++
	}
	sql += " WHERE id=$" + itoa(i)
	args = append(args, id)
	_, err := s.db.Exec(ctx, sql, args...)
	return err
}

func (s *Rooms) Delete(ctx context.Context, id uuid.UUID) error {
	_, err := s.db.Exec(ctx, `DELETE FROM rooms WHERE id=$1`, id)
	return err
}

func itoa(i int) string {
	// Маленькая утилита, чтобы не тянуть strconv в горячем пути.
	if i < 10 {
		return string(rune('0' + i))
	}
	return string(rune('0'+i/10)) + string(rune('0'+i%10))
}
