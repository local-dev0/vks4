// Пакет layout — layout engine MCU.
// Принимает список peer-ов и режим раскладки, возвращает координаты ячеек.
package layout

import (
	"math"
	"sort"
)

type Mode string

const (
	ModeGrid          Mode = "grid"
	ModeActiveSpeaker Mode = "active_speaker"
	ModeLecture       Mode = "lecture"
	ModePiP           Mode = "pip"
	ModeCustom        Mode = "custom"
)

type Cell struct {
	PeerID string  `json:"peerId"`
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	W      float64 `json:"w"`
	H      float64 `json:"h"`
	Z      int     `json:"z"`
	// SlotIndex — индекс slot-N если ячейка привязана к slot, иначе -1.
	// Используется чтобы расположить placeholder-pad соответствующего slot'а в координатах
	// cell когда peer не назначен. -1 = peer pinning по UUID, placeholder не нужен.
	SlotIndex int `json:"slotIndex,omitempty"`
}

type Spec struct {
	Mode    Mode
	Width   int
	Height  int
	Cells   []Cell // только для Custom
	Speaker string
	Presenter string
}

// Compute раскладывает участников и возвращает координаты cells.
// peers — стабильно упорядоченный список (по joinedAt).
func Compute(spec Spec, peers []string) []Cell {
	if len(peers) == 0 {
		return nil
	}
	switch spec.Mode {
	case ModeCustom:
		return spec.Cells
	case ModeActiveSpeaker:
		return activeSpeaker(spec, peers)
	case ModeLecture:
		return lecture(spec, peers)
	case ModePiP:
		return pip(spec, peers)
	default:
		return grid(spec, peers)
	}
}

func grid(spec Spec, peers []string) []Cell {
	n := len(peers)
	cols := int(math.Ceil(math.Sqrt(float64(n))))
	rows := int(math.Ceil(float64(n) / float64(cols)))
	W := float64(spec.Width) / float64(cols)
	H := float64(spec.Height) / float64(rows)
	out := make([]Cell, 0, n)
	for i, p := range peers {
		c := i % cols
		r := i / cols
		out = append(out, Cell{
			PeerID: p, SlotIndex: -1,
			X:      float64(c) * W, Y: float64(r) * H, W: W, H: H,
		})
	}
	return out
}

// activeSpeaker — большой кадр спикера + ряд thumbnail-ов снизу.
func activeSpeaker(spec Spec, peers []string) []Cell {
	speaker := spec.Speaker
	if speaker == "" {
		speaker = peers[0]
	}
	thumbs := make([]string, 0, len(peers))
	for _, p := range peers {
		if p != speaker {
			thumbs = append(thumbs, p)
		}
	}
	sort.Strings(thumbs)
	out := []Cell{}
	mainH := float64(spec.Height) * 0.8
	out = append(out, Cell{PeerID: speaker, SlotIndex: -1, X: 0, Y: 0, W: float64(spec.Width), H: mainH, Z: 1})
	if len(thumbs) == 0 {
		return out
	}
	thW := float64(spec.Width) / float64(len(thumbs))
	thH := float64(spec.Height) - mainH
	for i, p := range thumbs {
		out = append(out, Cell{PeerID: p, SlotIndex: -1, X: float64(i) * thW, Y: mainH, W: thW, H: thH})
	}
	return out
}

// lecture — слайды/presenter — слева 70%, аудитория — справа.
func lecture(spec Spec, peers []string) []Cell {
	presenter := spec.Presenter
	if presenter == "" {
		presenter = peers[0]
	}
	others := make([]string, 0, len(peers))
	for _, p := range peers {
		if p != presenter {
			others = append(others, p)
		}
	}
	pW := float64(spec.Width) * 0.7
	out := []Cell{{PeerID: presenter, SlotIndex: -1, X: 0, Y: 0, W: pW, H: float64(spec.Height), Z: 1}}
	if len(others) == 0 {
		return out
	}
	rightW := float64(spec.Width) - pW
	rowH := float64(spec.Height) / float64(len(others))
	for i, p := range others {
		out = append(out, Cell{PeerID: p, SlotIndex: -1, X: pW, Y: float64(i) * rowH, W: rightW, H: rowH})
	}
	return out
}

// pip — большой spectator (или active speaker) + маленький presenter в углу.
func pip(spec Spec, peers []string) []Cell {
	main := peers[0]
	if spec.Speaker != "" {
		main = spec.Speaker
	}
	out := []Cell{{PeerID: main, SlotIndex: -1, X: 0, Y: 0, W: float64(spec.Width), H: float64(spec.Height), Z: 1}}
	if len(peers) > 1 {
		small := peers[1]
		if spec.Presenter != "" {
			small = spec.Presenter
		}
		sw := float64(spec.Width) * 0.25
		sh := float64(spec.Height) * 0.25
		out = append(out, Cell{
			PeerID: small, SlotIndex: -1,
			X:      float64(spec.Width) - sw - 16,
			Y:      float64(spec.Height) - sh - 16,
			W:      sw, H: sh, Z: 5,
		})
	}
	return out
}
