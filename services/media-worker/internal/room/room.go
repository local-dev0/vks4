// Room — агрегат комнаты в media-worker.
package room

import (
	"context"
	"errors"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/pion/webrtc/v4"
	"go.uber.org/zap"

	"github.com/vks4/vks4/services/media-worker/internal/asd"
	"github.com/vks4/vks4/services/media-worker/internal/layout"
	"github.com/vks4/vks4/services/media-worker/internal/peer"
	"github.com/vks4/vks4/services/media-worker/internal/pipeline"
)

type LayoutMode = layout.Mode

// CustomCell — ячейка кастомной раскладки. ID — это либо "slot-N", либо peerID (UUID).
// Сервер резолвит ID в текущего peer'а слота при applyLayout.
// VAD=true: ячейка автоматически переключается на активного спикера, когда кто-то говорит.
type CustomCell struct {
	ID         string
	X, Y, W, H float64
	Z          int
	VAD        bool
}

type Room struct {
	ID         string
	mu         sync.RWMutex
	peers      map[string]*peer.Peer
	joinOrder  []string
	pipeline   pipeline.Pipeline
	det        *asd.Detector
	mode       LayoutMode
	recording  bool
	recordPath string
	log        *zap.Logger
	ice        []webrtc.ICEServer
	videoCodec string
	width      int
	height     int
	sfu        bool
	publicIP   string
	udpMin     uint16
	udpMax     uint16
	createdAt  time.Time

	// SFU slot mapping: slots[i] = peerID который занимает slot i (или "" свободно).
	// Размер = peer.AudioSlotCount (20). Slot peer'а определяет в какой audio-track других
	// peers форвардятся его аудио (mix-minus); видео всех peer'ов всегда идёт в slot-0 video
	// у каждого получателя (MCU output).
	slots [20]string

	// customCells — пользовательская раскладка из LayoutEditor. Применяется только когда
	// mode == ModeCustom. ID каждой ячейки резолвится в peerID через r.slots ("slot-N") либо
	// принимается как peerID напрямую.
	customCells []CustomCell

	// displayNames — мапа peerID → имя для подписи в MCU output (textoverlay).
	displayNames map[string]string

	bvSent  atomic.Uint64
	bvEmpty atomic.Uint64
	bvErr   atomic.Uint64
	baSent  atomic.Uint64
	baErr   atomic.Uint64

	// Маршрутизация RTP-пакетов peer → pipeline.
	videoIn map[string]chan<- []byte
	audioIn map[string]chan<- []byte

	// Egress: pipeline → все peer-ы.
	stopEgress chan struct{}
}

type Options struct {
	ID         string
	Log        *zap.Logger
	Pipeline   pipeline.Pipeline
	ICE        []webrtc.ICEServer
	VideoCodec string
	Width      int
	Height     int
	// SFU=true — RTP-форвардер: ingress пакеты от peer A раздаются всем остальным
	// напрямую через TrackLocalStaticRTP, без GStreamer compositing.
	// SFU=false — MCU режим через pipeline (требует функционирующий GStreamer pipeline).
	SFU      bool
	PublicIP string
	UDPMin   uint16
	UDPMax   uint16
}

func New(opts Options) *Room {
	r := &Room{
		ID:         opts.ID,
		peers:      map[string]*peer.Peer{},
		pipeline:   opts.Pipeline,
		det:        asd.New(-45, 800*time.Millisecond),
		mode:       layout.ModeGrid,
		log:        opts.Log,
		ice:        opts.ICE,
		videoCodec: opts.VideoCodec,
		width:      opts.Width,
		height:     opts.Height,
		sfu:        opts.SFU,
		publicIP:   opts.PublicIP,
		udpMin:     opts.UDPMin,
		udpMax:     opts.UDPMax,
		videoIn:    map[string]chan<- []byte{},
		audioIn:    map[string]chan<- []byte{},
		stopEgress: make(chan struct{}),
		createdAt:  time.Now(),
	}
	if !opts.SFU {
		go r.egressLoop()
		go r.asdLoop()
	}
	return r
}

func (r *Room) AddPeer(ctx context.Context, peerID, displayName, sdpOffer string) (string, error) {
	r.mu.Lock()
	if _, ok := r.peers[peerID]; ok {
		r.mu.Unlock()
		return "", errors.New("peer exists")
	}
	r.mu.Unlock()

	codec := webrtc.MimeTypeVP8
	if r.videoCodec == "h264" {
		codec = webrtc.MimeTypeH264
	}
	p, err := peer.New(r.ID, peerID, peer.Options{
		ICE: r.ice, Log: r.log, Sink: r, VideoCodec: codec,
		PublicIP: r.publicIP, UDPMin: r.udpMin, UDPMax: r.udpMax,
	})
	if err != nil {
		return "", err
	}
	// В SFU режиме GStreamer pipeline НЕ нужен — мы форвардим RTP напрямую.
	// Пропускаем создание bin'ов чтобы не тратить CPU на encoder/decoder впустую.
	var videoIn, audioIn chan<- []byte
	if !r.sfu {
		videoIn, audioIn, err = r.pipeline.AddPeer(peerID)
		if err != nil {
			_ = p.Close()
			return "", err
		}
	}
	answer, err := p.Negotiate(ctx, sdpOffer)
	if err != nil {
		_ = p.Close()
		if !r.sfu {
			_ = r.pipeline.RemovePeer(peerID)
		}
		return "", err
	}

	r.mu.Lock()
	r.peers[peerID] = p
	if !r.sfu {
		r.videoIn[peerID] = videoIn
		r.audioIn[peerID] = audioIn
	}
	r.joinOrder = append(r.joinOrder, peerID)
	if r.displayNames == nil {
		r.displayNames = map[string]string{}
	}
	r.displayNames[peerID] = displayName
	// Assign первый свободный slot для нового peer'а.
	for i := range r.slots {
		if r.slots[i] == "" {
			r.slots[i] = peerID
			break
		}
	}
	r.mu.Unlock()
	if !r.sfu {
		if displayName != "" {
			_ = r.pipeline.SetPeerName(peerID, displayName)
		}
		r.applyLayout()
	}
	return answer, nil
}

// SlotOf возвращает slot index для peer'а (-1 если не назначен).
func (r *Room) SlotOf(peerID string) int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for i, id := range r.slots {
		if id == peerID {
			return i
		}
	}
	return -1
}

// AssignSlots — оператор явно назначает peerID на конкретные slot'ы.
// Принимает частичный mapping (можно задать только slot 0, остальные сохраняются).
// Если peerID уже занимает другой slot, он автоматически освобождается оттуда.
// "" в значении освобождает slot.
// После переназначения вызывает applyLayout — compositor pad'ы получают новые координаты,
// а forwardAudio/forwardVideo SFU начинают слать пакеты в новые слоты у получателей.
func (r *Room) AssignSlots(assign map[int]string) {
	r.log.Info("AssignSlots", zap.String("room", r.ID), zap.Any("assign", assign))
	r.mu.Lock()
	for slot, peerID := range assign {
		if slot < 0 || slot >= len(r.slots) {
			r.log.Warn("AssignSlots: skip out-of-range", zap.Int("slot", slot), zap.Int("max", len(r.slots)))
			continue
		}
		// если этот peerID уже в другом слоте — освобождаем тот
		if peerID != "" {
			for i, id := range r.slots {
				if id == peerID && i != slot {
					r.slots[i] = ""
				}
			}
		}
		r.slots[slot] = peerID
	}
	r.log.Info("AssignSlots done", zap.String("room", r.ID), zap.Strings("slots", r.slots[:]))
	r.mu.Unlock()
	if !r.sfu {
		r.applyLayout()
	}
}

// Roster — slot→peerID для broadcast клиентам.
func (r *Room) Roster() map[int]string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := map[int]string{}
	for i, id := range r.slots {
		if id != "" {
			out[i] = id
		}
	}
	return out
}

func (r *Room) RemovePeer(peerID string) error {
	r.mu.Lock()
	p := r.peers[peerID]
	delete(r.peers, peerID)
	delete(r.videoIn, peerID)
	delete(r.audioIn, peerID)
	r.joinOrder = removeID(r.joinOrder, peerID)
	for i, id := range r.slots {
		if id == peerID {
			r.slots[i] = ""
		}
	}
	r.mu.Unlock()
	r.det.Forget(peerID)
	if p != nil {
		_ = p.Close()
	}
	if !r.sfu {
		_ = r.pipeline.RemovePeer(peerID)
		r.applyLayout()
	}
	return nil
}

func (r *Room) SetLayout(m LayoutMode) {
	r.mu.Lock()
	r.mode = m
	// Сбрасываем custom cells — переключаемся на алгоритмический режим.
	if m != layout.ModeCustom {
		r.customCells = nil
	}
	r.mu.Unlock()
	r.applyLayout()
}

// SetCustomLayout сохраняет пользовательскую раскладку и применяет её к compositor'у.
// Поддерживаемые ID ячеек: "slot-0".."slot-3" (резолвятся в текущего peer'а слота) или
// UUID peer'а (применяется напрямую). Ячейки без peer'а в слоте пропускаются.
func (r *Room) SetCustomLayout(cells []CustomCell) {
	r.mu.Lock()
	r.mode = layout.ModeCustom
	r.customCells = append([]CustomCell(nil), cells...)
	r.mu.Unlock()
	r.applyLayout()
}

func (r *Room) Layout() LayoutMode {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.mode
}

func (r *Room) applyLayout() {
	r.mu.RLock()
	mode := r.mode
	speaker := r.det.Current()
	peers := append([]string(nil), r.joinOrder...)
	width, height := r.width, r.height
	cellsCopy := append([]CustomCell(nil), r.customCells...)
	slotsCopy := r.slots
	r.mu.RUnlock()

	var cells []layout.Cell
	if mode == layout.ModeCustom && len(cellsCopy) > 0 {
		cells = make([]layout.Cell, 0, len(cellsCopy))
		for _, c := range cellsCopy {
			// SlotIndex для placeholder-управления: "slot-N" → N, "speaker"/peer-pin (UUID) → -1.
			slotIdx := -1
			if strings.HasPrefix(c.ID, "slot-") {
				if n, err := strconv.Atoi(c.ID[5:]); err == nil && n >= 0 && n < len(slotsCopy) {
					slotIdx = n
				}
			}
			// PeerID может быть пустым (slot не занят) — pipeline покажет placeholder.
			peerID := resolveCellID(c.ID, slotsCopy[:], speaker)
			// VAD-флаг: если ячейка помечена как VAD и кто-то сейчас говорит — этот peer
			// замещает изначально назначенного. Когда тишина — fallback на slot-резолв.
			if c.VAD && speaker != "" {
				peerID = speaker
			}
			cells = append(cells, layout.Cell{
				PeerID: peerID,
				X: c.X, Y: c.Y, W: c.W, H: c.H, Z: c.Z,
				SlotIndex: slotIdx,
			})
		}
	} else {
		cells = layout.Compute(layout.Spec{
			Mode: mode, Width: width, Height: height, Speaker: speaker,
		}, peers)
	}
	if err := r.pipeline.UpdateLayout(cells); err != nil {
		r.log.Warn("update layout", zap.String("room", r.ID), zap.Error(err))
	}
}

// resolveCellID — резолвит cell.id в текущий peerID комнаты:
//   - "slot-N"  → r.slots[N] (peer занявший slot N)
//   - "speaker" → текущий активный спикер из ASD (или "" если никто не говорит)
//   - UUID     → как есть (peer-pinning по конкретному peerID)
func resolveCellID(id string, slots []string, speaker string) string {
	if id == "speaker" {
		return speaker
	}
	if strings.HasPrefix(id, "slot-") {
		n, err := strconv.Atoi(id[5:])
		if err != nil || n < 0 || n >= len(slots) {
			return ""
		}
		return slots[n]
	}
	return id
}

// OnVideoRTP реализует sink: либо форвардим в pipeline (MCU), либо broadcast
// всем кроме отправителя (SFU).
func (r *Room) OnVideoRTP(peerID string, pkt []byte) {
	if r.sfu {
		r.forwardVideo(peerID, pkt)
		return
	}
	r.mu.RLock()
	ch := r.videoIn[peerID]
	r.mu.RUnlock()
	if ch == nil {
		return
	}
	select {
	case ch <- pkt:
	default:
	}
}

func (r *Room) OnAudioRTP(peerID string, pkt []byte) {
	// Audio ВСЕГДА форвардится SFU-style: peer A → slot[A].audio у каждого ДРУГОГО peer'а.
	// Это бесплатный mix-minus (отправитель не получает своё аудио обратно).
	// Video остаётся в MCU compositor.
	r.forwardAudio(peerID, pkt)
	// В MCU режиме параллельно пушим audio в pipeline — он микширует и пишет в recording.
	// Egress output из audiomixer'а игнорируется в broadcast (он бы вызывал self-echo).
	if !r.sfu {
		r.mu.RLock()
		ch := r.audioIn[peerID]
		r.mu.RUnlock()
		if ch != nil {
			select {
			case ch <- pkt:
			default:
			}
		}
	}
}

// SFU forward: пакет от sender идёт в slot N (где N — позиция sender в r.slots)
// у всех остальных peer-ов. Тогда клиент видит каждого источника в отдельном
// remote track (с stream.id = "slot-N") и может класть в нужный <video>.
func (r *Room) forwardVideo(sender string, pkt []byte) {
	r.mu.RLock()
	slot := -1
	for i, id := range r.slots {
		if id == sender {
			slot = i
			break
		}
	}
	if slot < 0 {
		r.mu.RUnlock()
		return
	}
	targets := make([]*peer.Peer, 0, len(r.peers))
	for id, p := range r.peers {
		if id == sender {
			continue
		}
		targets = append(targets, p)
	}
	r.mu.RUnlock()
	for _, p := range targets {
		_ = p.WriteVideoSlot(slot, pkt)
	}
}

func (r *Room) forwardAudio(sender string, pkt []byte) {
	r.mu.RLock()
	slot := -1
	for i, id := range r.slots {
		if id == sender {
			slot = i
			break
		}
	}
	if slot < 0 {
		r.mu.RUnlock()
		return
	}
	targets := make([]*peer.Peer, 0, len(r.peers))
	for id, p := range r.peers {
		if id == sender {
			continue
		}
		targets = append(targets, p)
	}
	r.mu.RUnlock()
	for _, p := range targets {
		_ = p.WriteAudioSlot(slot, pkt)
	}
}

func (r *Room) egressLoop() {
	for {
		select {
		case <-r.stopEgress:
			return
		case s, ok := <-r.pipeline.VideoOut():
			if !ok {
				return
			}
			r.broadcastVideo(s.Data)
		case s, ok := <-r.pipeline.AudioOut():
			if !ok {
				return
			}
			// Audio из pipeline.AudioOut() игнорируем в broadcast — это микс ВСЕХ peer'ов,
			// включая отправителя, что вызывает self-echo. Mix-minus делается через
			// SFU-forward в OnAudioRTP. Pipeline всё равно дренируем чтобы appsink не
			// заблокировался. Recording mix идёт по отдельной ветке pipeline (mp4mux),
			// она работает независимо от этого канала.
			_ = s
		}
	}
}

func (r *Room) asdLoop() {
	for {
		select {
		case <-r.stopEgress:
			return
		case lvl, ok := <-r.pipeline.ASDLevel():
			if !ok {
				return
			}
			if newSpk, changed := r.det.Push(asd.Sample{PeerID: lvl.PeerID, DBFS: lvl.DBFS, When: time.Now()}); changed {
				r.log.Info("ASD speaker changed", zap.String("room", r.ID), zap.String("speaker", newSpk), zap.Float64("dbfs", lvl.DBFS))
				r.applyLayout()
				// TODO: пушим active-speaker событие в signaling через шину/Redis.
			}
		}
	}
}

// broadcastVideo используется только из MCU egressLoop (микшированный поток в slot-0).
func (r *Room) broadcastVideo(pkt []byte) {
	if len(pkt) == 0 {
		r.bvEmpty.Add(1)
		return
	}
	r.mu.RLock()
	targets := make([]*peer.Peer, 0, len(r.peers))
	for _, p := range r.peers {
		targets = append(targets, p)
	}
	r.mu.RUnlock()
	n := r.bvSent.Add(1)
	if n == 1 || n%500 == 0 {
		r.log.Info("broadcastVideo", zap.Uint64("n", n), zap.Int("targets", len(targets)),
			zap.Int("size", len(pkt)), zap.Uint64("empty", r.bvEmpty.Load()),
			zap.Uint64("write_err", r.bvErr.Load()))
	}
	for _, p := range targets {
		if err := p.WriteVideoSlot(0, pkt); err != nil {
			r.bvErr.Add(1)
		}
	}
}

func (r *Room) broadcastAudio(pkt []byte) {
	if len(pkt) == 0 {
		return
	}
	r.mu.RLock()
	targets := make([]*peer.Peer, 0, len(r.peers))
	for _, p := range r.peers {
		targets = append(targets, p)
	}
	r.mu.RUnlock()
	for _, p := range targets {
		_ = p.WriteAudioSlot(0, pkt)
	}
}

func (r *Room) StartRecording(dir string) (string, error) {
	r.mu.Lock()
	if r.recording {
		r.mu.Unlock()
		return r.recordPath, nil
	}
	path := filepath.Join(dir, r.ID+".mp4")
	r.mu.Unlock()
	if err := r.pipeline.StartRecording(path); err != nil {
		return "", err
	}
	r.mu.Lock()
	r.recording = true
	r.recordPath = path
	r.mu.Unlock()
	return path, nil
}

func (r *Room) StopRecording() (string, int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.recording {
		return "", 0, nil
	}
	p, size, err := r.pipeline.StopRecording()
	r.recording = false
	return p, size, err
}

func (r *Room) Close(ctx context.Context) error {
	close(r.stopEgress)
	r.mu.Lock()
	peers := make([]*peer.Peer, 0, len(r.peers))
	for _, p := range r.peers {
		peers = append(peers, p)
	}
	r.peers = nil
	r.mu.Unlock()
	for _, p := range peers {
		_ = p.Close()
	}
	return r.pipeline.Stop(ctx)
}

func (r *Room) Snapshot() Snapshot {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return Snapshot{
		ID:           r.ID,
		Participants: len(r.peers),
		Layout:       string(r.mode),
		Recording:    r.recording,
		CreatedAt:    r.createdAt,
	}
}

type Snapshot struct {
	ID           string    `json:"id"`
	Participants int       `json:"participants"`
	Layout       string    `json:"layout"`
	Recording    bool      `json:"recording"`
	CreatedAt    time.Time `json:"createdAt"`
}

func removeID(s []string, id string) []string {
	for i, v := range s {
		if v == id {
			return append(s[:i], s[i+1:]...)
		}
	}
	return s
}
