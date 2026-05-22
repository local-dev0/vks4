package postgres

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vks4/vks4/services/control-plane/internal/domain"
)

type Audit struct{ db *pgxpool.Pool }

func NewAudit(db *pgxpool.Pool) *Audit { return &Audit{db: db} }

type AuditWrite struct {
	ActorID   *uuid.UUID
	Action    string
	Target    string
	Payload   map[string]any
	IP        string
	UserAgent string
}

func (a *Audit) Write(ctx context.Context, w AuditWrite) error {
	pl, _ := json.Marshal(w.Payload)
	_, err := a.db.Exec(ctx, `
		INSERT INTO audit_log (actor_id, action, target, payload, ip, user_agent)
		VALUES ($1,$2,$3,$4, NULLIF($5,'')::inet, $6)`,
		w.ActorID, w.Action, w.Target, pl, w.IP, w.UserAgent)
	return err
}

type AuditRow struct {
	ID        uuid.UUID
	ActorID   *uuid.UUID
	Action    string
	Target    string
	Payload   []byte
	CreatedAt time.Time
}

func (a *Audit) List(ctx context.Context, limit, offset int) ([]domain.AuditEntry, error) {
	rows, err := a.db.Query(ctx, `
		SELECT id, actor_id, action, target, payload, created_at
		FROM audit_log ORDER BY created_at DESC LIMIT $1 OFFSET $2`, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.AuditEntry
	for rows.Next() {
		var r AuditRow
		if err := rows.Scan(&r.ID, &r.ActorID, &r.Action, &r.Target, &r.Payload, &r.CreatedAt); err != nil {
			return nil, err
		}
		var pl map[string]any
		_ = json.Unmarshal(r.Payload, &pl)
		out = append(out, domain.AuditEntry{
			ID:        r.ID,
			ActorID:   r.ActorID,
			Action:    r.Action,
			Target:    r.Target,
			Payload:   pl,
			CreatedAt: r.CreatedAt,
		})
	}
	return out, rows.Err()
}
