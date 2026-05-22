// Пакет protocol описывает протокол WebSocket-сигналинга между admin/клиентом и сервером.
// Формат — JSON-сообщения с обязательным полем "type". Контракт повторяет docs/SIGNALING.md.
package protocol

import "encoding/json"

type Envelope struct {
	Type    string          `json:"type"`
	ID      string          `json:"id,omitempty"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

// ---- client → server ----

const (
	TypeJoin      = "join"
	TypeLeave     = "leave"
	TypeOffer     = "offer"
	TypeAnswer    = "answer"
	TypeCandidate = "candidate"
	TypeChat      = "chat"
	TypeControl   = "control"
	TypePing      = "ping"
)

type JoinPayload struct {
	RoomID      string `json:"roomId"`
	Token       string `json:"token"`
	DisplayName string `json:"displayName"`
}

type SDPPayload struct {
	SDP string `json:"sdp"`
}

type CandidatePayload struct {
	Candidate string `json:"candidate"`
	Mid       string `json:"sdpMid,omitempty"`
	MLine     int    `json:"sdpMLineIndex,omitempty"`
}

type ChatPayload struct {
	Text string `json:"text"`
}

type ControlPayload struct {
	Action string          `json:"action"`
	Data   json.RawMessage `json:"data,omitempty"`
}

// ---- server → client ----

const (
	TypeJoined        = "joined"
	TypePeerJoined    = "peer-joined"
	TypePeerLeft      = "peer-left"
	TypeActiveSpeaker = "active-speaker"
	TypeLayout        = "layout"
	TypeStats         = "stats"
	TypeError         = "error"
	TypePong          = "pong"
)

type JoinedPayload struct {
	SelfID       string         `json:"selfId"`
	Participants []Participant  `json:"participants"`
}

type Participant struct {
	PeerID      string `json:"peerId"`
	DisplayName string `json:"displayName"`
	Role        string `json:"role"`
}

type ActiveSpeakerPayload struct {
	PeerID string `json:"peerId"`
}

type ErrorPayload struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}
