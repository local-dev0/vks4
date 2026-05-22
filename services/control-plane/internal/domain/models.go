package domain

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

type Role string

const (
	RoleAdmin     Role = "admin"
	RoleOperator  Role = "operator"
	RoleModerator Role = "moderator"
	RoleViewer    Role = "viewer"
)

func (r Role) Valid() bool {
	switch r {
	case RoleAdmin, RoleOperator, RoleModerator, RoleViewer:
		return true
	}
	return false
}

type User struct {
	ID        uuid.UUID `json:"id"`
	Email     string    `json:"email"`
	Name      string    `json:"name"`
	Role      Role      `json:"role"`
	Disabled  bool      `json:"disabled"`
	CreatedAt time.Time `json:"createdAt"`
}

type RoomMode string

const (
	RoomMeeting RoomMode = "meeting"
	RoomLecture RoomMode = "lecture"
	RoomWebinar RoomMode = "webinar"
)

type Room struct {
	ID              uuid.UUID  `json:"id"`
	Name            string     `json:"name"`
	Description     string     `json:"description"`
	Mode            RoomMode   `json:"mode"`
	MaxParticipants int        `json:"maxParticipants"`
	WaitingRoom     bool       `json:"waitingRoom"`
	Locked          bool       `json:"locked"`
	Recording       bool       `json:"recording"`
	DefaultLayout   Layout     `json:"defaultLayout"`
	JoinURL         string     `json:"joinUrl,omitempty"`
	CreatedBy       *uuid.UUID `json:"createdBy,omitempty"`
	CreatedAt       time.Time  `json:"createdAt"`
}

type LayoutMode string

const (
	LayoutGrid          LayoutMode = "grid"
	LayoutActiveSpeaker LayoutMode = "active_speaker"
	LayoutLecture       LayoutMode = "lecture"
	LayoutPiP           LayoutMode = "pip"
	LayoutCustom        LayoutMode = "custom"
)

type LayoutCell struct {
	ID     string  `json:"id"`
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	W      float64 `json:"w"`
	H      float64 `json:"h"`
	ZIndex int     `json:"zIndex,omitempty"`
	// VAD=true: ячейка становится "горячей" — текущий активный спикер автоматически
	// замещает peer'а назначенного в этот slot. Когда говорящего нет, отображается
	// исходно назначенный peer (или placeholder если slot пуст).
	VAD bool `json:"vad,omitempty"`
}

type LayoutBackground struct {
	Color string `json:"color,omitempty"`
	URL   string `json:"url,omitempty"`
}

type LayoutLogo struct {
	URL string  `json:"url,omitempty"`
	X   float64 `json:"x,omitempty"`
	Y   float64 `json:"y,omitempty"`
	W   float64 `json:"w,omitempty"`
	H   float64 `json:"h,omitempty"`
}

type Layout struct {
	Mode       LayoutMode       `json:"mode"`
	Width      int              `json:"width,omitempty"`
	Height     int              `json:"height,omitempty"`
	Background LayoutBackground `json:"background,omitempty"`
	Logo       LayoutLogo       `json:"logo,omitempty"`
	Cells      []LayoutCell     `json:"cells,omitempty"`
	// ShowNames=true рисует подпись (displayName) внизу каждой ячейки в MCU output.
	// По умолчанию true для backward-compat. Toggle в Room Control.
	ShowNames *bool `json:"showNames,omitempty"`
	// Стиль подписи. Применяется ко всем peer'ам сразу через textoverlay.
	NameBgAlpha   *float64 `json:"nameBgAlpha,omitempty"`   // 0..1
	NameFontSize  *int     `json:"nameFontSize,omitempty"`  // pt
	NameFontColor *string  `json:"nameFontColor,omitempty"` // "#RRGGBB"
}

func (l Layout) MarshalJSONBytes() ([]byte, error) { return json.Marshal(l) }

// LayoutTemplate — пользовательский шаблон раскладки, переиспользуется между комнатами.
// Хранит только геометрию ячеек со slot-N плейсхолдерами; конкретные peer-ы привязываются
// уже в Room Control при применении шаблона к активной комнате.
type LayoutTemplate struct {
	ID         uuid.UUID        `json:"id"`
	Name       string           `json:"name"`
	Width      int              `json:"width"`
	Height     int              `json:"height"`
	Cells      []LayoutCell     `json:"cells"`
	Background LayoutBackground `json:"background,omitempty"`
	CreatedBy  *uuid.UUID       `json:"createdBy,omitempty"`
	CreatedAt  time.Time        `json:"createdAt"`
}

type RecordingStatus string

const (
	RecordingRunning  RecordingStatus = "running"
	RecordingFinished RecordingStatus = "finished"
	RecordingFailed   RecordingStatus = "failed"
)

type Recording struct {
	ID        uuid.UUID       `json:"id"`
	RoomID    uuid.UUID       `json:"roomId"`
	Status    RecordingStatus `json:"status"`
	StartedAt time.Time       `json:"startedAt"`
	EndedAt   *time.Time      `json:"endedAt,omitempty"`
	SizeBytes *int64          `json:"sizeBytes,omitempty"`
	URL       string          `json:"url,omitempty"`
}

type ParticipantRole string

const (
	ParticipantHost      ParticipantRole = "host"
	ParticipantPresenter ParticipantRole = "presenter"
	ParticipantAttendee  ParticipantRole = "attendee"
)

type AuditEntry struct {
	ID        uuid.UUID      `json:"id"`
	ActorID   *uuid.UUID     `json:"actorId,omitempty"`
	Action    string         `json:"action"`
	Target    string         `json:"target,omitempty"`
	Payload   map[string]any `json:"payload,omitempty"`
	CreatedAt time.Time      `json:"createdAt"`
}
