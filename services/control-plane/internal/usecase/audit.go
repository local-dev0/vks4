package usecase

import (
	"context"

	"github.com/vks4/vks4/services/control-plane/internal/adapters/repo/postgres"
	"github.com/vks4/vks4/services/control-plane/internal/domain"
	apperr "github.com/vks4/vks4/services/control-plane/internal/pkg/errors"
)

type Auditor struct{ repo *postgres.Audit }

func NewAuditor(r *postgres.Audit) *Auditor { return &Auditor{repo: r} }

func (a *Auditor) List(ctx context.Context, limit, offset int) ([]domain.AuditEntry, error) {
	if limit <= 0 || limit > 1000 {
		limit = 100
	}
	if offset < 0 {
		offset = 0
	}
	out, err := a.repo.List(ctx, limit, offset)
	if err != nil {
		return nil, apperr.Wrap(apperr.CodeInternal, "db", err)
	}
	return out, nil
}
