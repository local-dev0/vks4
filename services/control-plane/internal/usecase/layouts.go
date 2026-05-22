package usecase

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"

	"github.com/vks4/vks4/services/control-plane/internal/adapters/repo/postgres"
	"github.com/vks4/vks4/services/control-plane/internal/domain"
	apperr "github.com/vks4/vks4/services/control-plane/internal/pkg/errors"
)

type LayoutTemplates struct {
	repo  *postgres.LayoutTemplates
	audit *postgres.Audit
}

func NewLayoutTemplates(r *postgres.LayoutTemplates, a *postgres.Audit) *LayoutTemplates {
	return &LayoutTemplates{repo: r, audit: a}
}

func (s *LayoutTemplates) List(ctx context.Context) ([]domain.LayoutTemplate, error) {
	rows, err := s.repo.List(ctx)
	if err != nil {
		return nil, apperr.Wrap(apperr.CodeInternal, "db", err)
	}
	out := make([]domain.LayoutTemplate, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.Domain())
	}
	return out, nil
}

func (s *LayoutTemplates) Get(ctx context.Context, id uuid.UUID) (*domain.LayoutTemplate, error) {
	row, err := s.repo.Get(ctx, id)
	if err != nil {
		return nil, apperr.Wrap(apperr.CodeInternal, "db", err)
	}
	if row == nil {
		return nil, apperr.New(apperr.CodeNotFound, "layout template not found")
	}
	d := row.Domain()
	return &d, nil
}

type CreateLayoutTemplateInput struct {
	Name       string
	Width      int
	Height     int
	Cells      []domain.LayoutCell
	Background domain.LayoutBackground
}

func (s *LayoutTemplates) Create(ctx context.Context, actor uuid.UUID, in CreateLayoutTemplateInput) (*domain.LayoutTemplate, error) {
	if in.Name == "" {
		return nil, apperr.New(apperr.CodeInvalidArgument, "name required")
	}
	if in.Width <= 0 {
		in.Width = 1280
	}
	if in.Height <= 0 {
		in.Height = 720
	}
	cellsB, _ := json.Marshal(in.Cells)
	bgB, _ := json.Marshal(in.Background)
	row, err := s.repo.Create(ctx, postgres.CreateLayoutTemplateInput{
		Name: in.Name, Width: in.Width, Height: in.Height,
		Cells: cellsB, Background: bgB, CreatedBy: &actor,
	})
	if err != nil {
		return nil, apperr.Wrap(apperr.CodeInternal, "db", err)
	}
	_ = s.audit.Write(ctx, postgres.AuditWrite{ActorID: &actor, Action: "layouts.create", Target: row.ID.String(),
		Payload: map[string]any{"name": in.Name}})
	d := row.Domain()
	return &d, nil
}

func (s *LayoutTemplates) Update(ctx context.Context, actor, id uuid.UUID, set map[string]any) (*domain.LayoutTemplate, error) {
	// сериализуем cells/background если переданы
	if v, ok := set["cells"]; ok {
		b, _ := json.Marshal(v)
		set["cells"] = b
	}
	if v, ok := set["background"]; ok {
		b, _ := json.Marshal(v)
		set["background"] = b
	}
	if err := s.repo.Update(ctx, id, set); err != nil {
		return nil, apperr.Wrap(apperr.CodeInternal, "db", err)
	}
	_ = s.audit.Write(ctx, postgres.AuditWrite{ActorID: &actor, Action: "layouts.update", Target: id.String()})
	return s.Get(ctx, id)
}

func (s *LayoutTemplates) Delete(ctx context.Context, actor, id uuid.UUID) error {
	if err := s.repo.Delete(ctx, id); err != nil {
		return apperr.Wrap(apperr.CodeInternal, "db", err)
	}
	_ = s.audit.Write(ctx, postgres.AuditWrite{ActorID: &actor, Action: "layouts.delete", Target: id.String()})
	return nil
}
