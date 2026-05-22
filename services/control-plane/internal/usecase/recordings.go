package usecase

import (
	"context"

	"github.com/google/uuid"

	"github.com/vks4/vks4/services/control-plane/internal/adapters/mediarpc"
	"github.com/vks4/vks4/services/control-plane/internal/adapters/repo/postgres"
	"github.com/vks4/vks4/services/control-plane/internal/domain"
	apperr "github.com/vks4/vks4/services/control-plane/internal/pkg/errors"
)

type Recordings struct {
	repo  *postgres.Recordings
	audit *postgres.Audit
	media mediarpc.Client
}

func NewRecordings(r *postgres.Recordings, a *postgres.Audit, m mediarpc.Client) *Recordings {
	return &Recordings{repo: r, audit: a, media: m}
}

func (r *Recordings) Start(ctx context.Context, actor, roomID uuid.UUID) (*domain.Recording, error) {
	if cur, _ := r.repo.FindRunning(ctx, roomID); cur != nil {
		d := cur.Domain()
		return &d, nil
	}
	row, err := r.repo.Create(ctx, roomID)
	if err != nil {
		return nil, apperr.Wrap(apperr.CodeInternal, "db", err)
	}
	if _, err := r.media.StartRecording(ctx, roomID); err != nil {
		_ = r.repo.Fail(ctx, row.ID)
		return nil, apperr.Wrap(apperr.CodeUnavailable, "media start", err)
	}
	_ = r.audit.Write(ctx, postgres.AuditWrite{ActorID: &actor, Action: "rec.start", Target: roomID.String()})
	d := row.Domain()
	return &d, nil
}

func (r *Recordings) Stop(ctx context.Context, actor, roomID uuid.UUID) (*domain.Recording, error) {
	cur, err := r.repo.FindRunning(ctx, roomID)
	if err != nil {
		return nil, apperr.Wrap(apperr.CodeInternal, "db", err)
	}
	if cur == nil {
		return nil, apperr.New(apperr.CodeNotFound, "no running recording")
	}
	url, size, err := r.media.StopRecording(ctx, roomID)
	if err != nil {
		_ = r.repo.Fail(ctx, cur.ID)
		return nil, apperr.Wrap(apperr.CodeUnavailable, "media stop", err)
	}
	if err := r.repo.Finish(ctx, cur.ID, size, url); err != nil {
		return nil, apperr.Wrap(apperr.CodeInternal, "db", err)
	}
	_ = r.audit.Write(ctx, postgres.AuditWrite{ActorID: &actor, Action: "rec.stop", Target: roomID.String()})
	return r.Get(ctx, cur.ID)
}

func (r *Recordings) Get(ctx context.Context, id uuid.UUID) (*domain.Recording, error) {
	row, err := r.repo.Get(ctx, id)
	if err != nil {
		return nil, apperr.Wrap(apperr.CodeInternal, "db", err)
	}
	if row == nil {
		return nil, apperr.New(apperr.CodeNotFound, "not found")
	}
	d := row.Domain()
	return &d, nil
}

func (r *Recordings) List(ctx context.Context, roomID *uuid.UUID) ([]domain.Recording, error) {
	rows, err := r.repo.List(ctx, roomID)
	if err != nil {
		return nil, apperr.Wrap(apperr.CodeInternal, "db", err)
	}
	out := make([]domain.Recording, 0, len(rows))
	for _, row := range rows {
		out = append(out, row.Domain())
	}
	return out, nil
}

func (r *Recordings) Delete(ctx context.Context, actor, id uuid.UUID) error {
	if err := r.repo.Delete(ctx, id); err != nil {
		return apperr.Wrap(apperr.CodeInternal, "db", err)
	}
	_ = r.audit.Write(ctx, postgres.AuditWrite{ActorID: &actor, Action: "rec.delete", Target: id.String()})
	return nil
}
