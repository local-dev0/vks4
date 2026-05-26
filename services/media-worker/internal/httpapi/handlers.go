// HTTP API media-worker — компактный аналог mcu.proto.
// Используется signaling-сервисом и control-plane для управления комнатой.
package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"

	"github.com/vks4/vks4/services/media-worker/internal/layout"
	"github.com/vks4/vks4/services/media-worker/internal/room"
)

// sipParticipant — формат для Redis-hash room:{id}:participants
// (тот же что использует signaling/presence — control-plane читает оттуда же).
type sipParticipant struct {
	PeerID      string    `json:"peerId"`
	DisplayName string    `json:"displayName"`
	Role        string    `json:"role"`
	Transport   string    `json:"transport"`
	JoinedAt    time.Time `json:"joinedAt"`
}

func (a *API) presenceAddSIP(ctx context.Context, roomID, peerID, displayName string) {
	if a.Rdb == nil {
		return
	}
	p := sipParticipant{
		PeerID: peerID, DisplayName: displayName, Role: "guest",
		Transport: "sip", JoinedAt: time.Now(),
	}
	b, _ := json.Marshal(p)
	if err := a.Rdb.HSet(ctx, "room:"+roomID+":participants", peerID, b).Err(); err != nil {
		a.Log.Warn("presence add sip", zap.Error(err))
	}
}

func (a *API) presenceRemoveSIP(ctx context.Context, roomID, peerID string) {
	if a.Rdb == nil {
		return
	}
	_ = a.Rdb.HDel(ctx, "room:"+roomID+":participants", peerID).Err()
}

type API struct {
	Rooms        *room.Registry
	RecordingDir string
	Log          *zap.Logger
	Rdb          *redis.Client // для резолва room-alias (name→UUID) в SIP-сценарии
}

// resolveRoomID — если в Redis есть alias "vks4:room:alias:{name}" → UUID, вернёт UUID.
// Иначе возвращает входной id как есть (UUID-ы напрямую передаются от signaling без алиаса).
func (a *API) resolveRoomID(ctx context.Context, id string) string {
	if a.Rdb == nil || id == "" {
		return id
	}
	key := "vks4:room:alias:" + id
	if v, err := a.Rdb.Get(ctx, key).Result(); err == nil && v != "" {
		return v
	}
	return id
}

func (a *API) Router() http.Handler {
	r := chi.NewRouter()
	r.Get("/health", a.health)
	r.Get("/rooms", a.listRooms)
	r.Post("/rooms/{room}/peers", a.addPeer)
	r.Delete("/rooms/{room}/peers/{peer}", a.removePeer)
	r.Post("/rooms/{room}/sip-peers", a.addSIPPeer)
	r.Delete("/rooms/{room}/sip-peers/{peer}", a.removeSIPPeer)
	r.Get("/rooms/{room}/roster", a.roster)
	r.Put("/rooms/{room}/layout", a.updateLayout)
	r.Put("/rooms/{room}/slots", a.assignSlots)
	r.Post("/rooms/{room}/recording", a.startRecording)
	r.Delete("/rooms/{room}/recording", a.stopRecording)
	r.Delete("/rooms/{room}", a.destroyRoom)
	return r
}

func (a *API) roster(w http.ResponseWriter, r *http.Request) {
	rm := a.Rooms.Get(chi.URLParam(r, "room"))
	if rm == nil {
		writeJSON(w, http.StatusOK, map[string]any{"slots": map[string]string{}})
		return
	}
	slots := rm.Roster()
	// преобразуем int -> string-keys для JSON
	out := map[string]string{}
	for k, v := range slots {
		out[fmt.Sprintf("%d", k)] = v
	}
	writeJSON(w, http.StatusOK, map[string]any{"slots": out})
}

func (a *API) health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "rooms": a.Rooms.Count()})
}

func (a *API) listRooms(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, a.Rooms.Snapshot())
}

type addPeerReq struct {
	PeerID      string `json:"peerId"`
	DisplayName string `json:"displayName"`
	Role        string `json:"role"`
	SDPOffer    string `json:"sdpOffer"`
}

type addPeerResp struct {
	SDPAnswer string `json:"sdpAnswer"`
}

func (a *API) addPeer(w http.ResponseWriter, r *http.Request) {
	roomID := chi.URLParam(r, "room")
	var in addPeerReq
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	rm, err := a.Rooms.GetOrCreate(r.Context(), roomID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	answer, err := rm.AddPeer(r.Context(), in.PeerID, in.DisplayName, in.SDPOffer)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, addPeerResp{SDPAnswer: answer})
}

type addSIPPeerReq struct {
	PeerID      string `json:"peerId"`
	DisplayName string `json:"displayName"`
}

type addSIPPeerResp struct {
	AudioPort int `json:"audioPort"`
	VideoPort int `json:"videoPort"`
}

// addSIPPeer создаёт SIP-peer (plain-RTP) в комнате — для интеграции FreeSWITCH B2BUA.
// Возвращает UDP port куда sip-gateway должен слать Opus RTP.
// roomID может быть либо UUID (от WebRTC потока), либо человекочитаемое имя из SIP
// (например, "room1" — то что набрал терминал). Алиас name→UUID хранится в Redis
// и пишется control-plane'ом при создании комнаты.
func (a *API) addSIPPeer(w http.ResponseWriter, r *http.Request) {
	rawID := chi.URLParam(r, "room")
	roomID := a.resolveRoomID(r.Context(), rawID)
	if roomID != rawID {
		a.Log.Info("sip alias resolved", zap.String("alias", rawID), zap.String("room", roomID))
	}
	var in addSIPPeerReq
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	rm, err := a.Rooms.GetOrCreate(r.Context(), roomID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	sp, err := rm.AddSIPPeer(in.PeerID, in.DisplayName)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	// Запись в presence: admin UI читает Redis hash room:{id}:participants через control-plane.
	a.presenceAddSIP(r.Context(), roomID, in.PeerID, in.DisplayName)
	writeJSON(w, http.StatusOK, addSIPPeerResp{AudioPort: sp.AudioPort, VideoPort: sp.VideoPort})
}

func (a *API) removeSIPPeer(w http.ResponseWriter, r *http.Request) {
	roomID := a.resolveRoomID(r.Context(), chi.URLParam(r, "room"))
	peerID := chi.URLParam(r, "peer")
	rm := a.Rooms.Get(roomID)
	if rm == nil {
		a.presenceRemoveSIP(r.Context(), roomID, peerID)
		w.WriteHeader(http.StatusNoContent)
		return
	}
	rm.RemoveSIPPeer(peerID)
	a.presenceRemoveSIP(r.Context(), roomID, peerID)
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) removePeer(w http.ResponseWriter, r *http.Request) {
	rm := a.Rooms.Get(chi.URLParam(r, "room"))
	if rm == nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	_ = rm.RemovePeer(chi.URLParam(r, "peer"))
	w.WriteHeader(http.StatusNoContent)
}

type layoutCell struct {
	ID     string  `json:"id"`     // "slot-N" | peerID (UUID)
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	W      float64 `json:"w"`
	H      float64 `json:"h"`
	ZIndex int     `json:"zIndex,omitempty"`
	VAD    bool    `json:"vad,omitempty"`
}

type layoutReq struct {
	Mode          string       `json:"mode"`
	Width         int          `json:"width,omitempty"`
	Height        int          `json:"height,omitempty"`
	Cells         []layoutCell `json:"cells,omitempty"`
	ShowNames     *bool        `json:"showNames,omitempty"`
	NameBgAlpha   *float64     `json:"nameBgAlpha,omitempty"`
	NameBgColor   *string      `json:"nameBgColor,omitempty"`
	NameFontSize  *int         `json:"nameFontSize,omitempty"`
	NameFontColor *string      `json:"nameFontColor,omitempty"`
}

func (a *API) updateLayout(w http.ResponseWriter, r *http.Request) {
	rm := a.Rooms.Get(chi.URLParam(r, "room"))
	if rm == nil {
		http.Error(w, "no room", http.StatusNotFound)
		return
	}
	var in layoutReq
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	mode := layout.Mode(in.Mode)
	if mode == layout.ModeCustom && len(in.Cells) > 0 {
		cells := make([]room.CustomCell, 0, len(in.Cells))
		for _, c := range in.Cells {
			cells = append(cells, room.CustomCell{
				ID: c.ID, X: c.X, Y: c.Y, W: c.W, H: c.H, Z: c.ZIndex, VAD: c.VAD,
			})
		}
		rm.SetCustomLayout(cells)
	} else {
		rm.SetLayout(mode)
	}
	a.Log.Info("updateLayout received",
		zap.String("room", chi.URLParam(r, "room")),
		zap.String("mode", in.Mode),
		zap.Int("cells", len(in.Cells)),
		zap.Bool("showNames_present", in.ShowNames != nil),
		zap.Any("showNames", in.ShowNames),
	)
	if in.ShowNames != nil {
		rm.SetShowNames(*in.ShowNames)
	}
	if in.NameBgAlpha != nil || in.NameBgColor != nil || in.NameFontSize != nil || in.NameFontColor != nil {
		rm.SetNameStyle(in.NameBgAlpha, in.NameBgColor, in.NameFontSize, in.NameFontColor)
	}
	w.WriteHeader(http.StatusNoContent)
}

// assignSlots — body: {"0": "peerId", "1": "peerId", "2": "", ...}
// Slot номера — строки (так удобнее в JSON), значения — peerID или "" для очистки.
func (a *API) assignSlots(w http.ResponseWriter, r *http.Request) {
	rm := a.Rooms.Get(chi.URLParam(r, "room"))
	if rm == nil {
		http.Error(w, "no room", http.StatusNotFound)
		return
	}
	var raw map[string]string
	if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	assign := make(map[int]string, len(raw))
	for k, v := range raw {
		var i int
		if _, err := fmt.Sscanf(k, "%d", &i); err == nil {
			assign[i] = v
		}
	}
	rm.AssignSlots(assign)
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) startRecording(w http.ResponseWriter, r *http.Request) {
	rm, err := a.Rooms.GetOrCreate(r.Context(), chi.URLParam(r, "room"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	dir := a.RecordingDir
	if dir == "" {
		dir = "/data/recordings"
	}
	path, err := rm.StartRecording(filepath.Clean(dir))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"path": path})
}

func (a *API) stopRecording(w http.ResponseWriter, r *http.Request) {
	rm := a.Rooms.Get(chi.URLParam(r, "room"))
	if rm == nil {
		http.Error(w, "no room", http.StatusNotFound)
		return
	}
	path, size, err := rm.StopRecording()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"path": path, "size": size})
}

func (a *API) destroyRoom(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*1e9) // 5s
	defer cancel()
	_ = a.Rooms.Destroy(ctx, chi.URLParam(r, "room"))
	w.WriteHeader(http.StatusNoContent)
}

func writeJSON(w http.ResponseWriter, code int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	if body != nil {
		_ = json.NewEncoder(w).Encode(body)
	}
}
