package usecase

import (
	"context"
	"strings"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"

	"github.com/vks4/vks4/services/control-plane/internal/adapters/repo/postgres"
	"github.com/vks4/vks4/services/control-plane/internal/domain"
	apperr "github.com/vks4/vks4/services/control-plane/internal/pkg/errors"
)

type Users struct {
	repo  *postgres.Users
	audit *postgres.Audit
}

func NewUsers(r *postgres.Users, a *postgres.Audit) *Users { return &Users{repo: r, audit: a} }

func (u *Users) List(ctx context.Context) ([]domain.User, error) {
	rows, err := u.repo.List(ctx)
	if err != nil {
		return nil, apperr.Wrap(apperr.CodeInternal, "db", err)
	}
	out := make([]domain.User, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.Domain())
	}
	return out, nil
}

type CreateUserInput struct {
	Email, Password, Name string
	Role                  domain.Role
}

func (u *Users) Create(ctx context.Context, actor uuid.UUID, in CreateUserInput) (*domain.User, error) {
	in.Email = strings.ToLower(strings.TrimSpace(in.Email))
	if in.Email == "" || len(in.Password) < 8 || !in.Role.Valid() {
		return nil, apperr.New(apperr.CodeInvalidArgument, "invalid user")
	}
	h, err := bcrypt.GenerateFromPassword([]byte(in.Password), bcrypt.DefaultCost)
	if err != nil {
		return nil, apperr.Wrap(apperr.CodeInternal, "hash", err)
	}
	row, err := u.repo.Create(ctx, postgres.CreateUserInput{
		Email: in.Email, Name: in.Name, PasswordHash: string(h), Role: string(in.Role),
	})
	if err != nil {
		return nil, apperr.Wrap(apperr.CodeConflict, "create", err)
	}
	d := row.Domain()
	_ = u.audit.Write(ctx, postgres.AuditWrite{ActorID: &actor, Action: "users.create", Target: row.ID.String()})
	return &d, nil
}

func (u *Users) SetRole(ctx context.Context, actor, id uuid.UUID, role domain.Role) error {
	if !role.Valid() {
		return apperr.New(apperr.CodeInvalidArgument, "invalid role")
	}
	if err := u.repo.UpdateRole(ctx, id, string(role)); err != nil {
		return apperr.Wrap(apperr.CodeInternal, "db", err)
	}
	_ = u.audit.Write(ctx, postgres.AuditWrite{ActorID: &actor, Action: "users.role", Target: id.String(),
		Payload: map[string]any{"role": role}})
	return nil
}

func (u *Users) SetPassword(ctx context.Context, actor, id uuid.UUID, password string) error {
	if len(password) < 8 {
		return apperr.New(apperr.CodeInvalidArgument, "weak password")
	}
	h, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return apperr.Wrap(apperr.CodeInternal, "hash", err)
	}
	if err := u.repo.UpdatePassword(ctx, id, string(h)); err != nil {
		return apperr.Wrap(apperr.CodeInternal, "db", err)
	}
	_ = u.audit.Write(ctx, postgres.AuditWrite{ActorID: &actor, Action: "users.password", Target: id.String()})
	return nil
}

func (u *Users) SetDisabled(ctx context.Context, actor, id uuid.UUID, disabled bool) error {
	if err := u.repo.SetDisabled(ctx, id, disabled); err != nil {
		return apperr.Wrap(apperr.CodeInternal, "db", err)
	}
	_ = u.audit.Write(ctx, postgres.AuditWrite{ActorID: &actor, Action: "users.disabled", Target: id.String(),
		Payload: map[string]any{"disabled": disabled}})
	return nil
}

func (u *Users) Delete(ctx context.Context, actor, id uuid.UUID) error {
	if err := u.repo.Delete(ctx, id); err != nil {
		return apperr.Wrap(apperr.CodeInternal, "db", err)
	}
	_ = u.audit.Write(ctx, postgres.AuditWrite{ActorID: &actor, Action: "users.delete", Target: id.String()})
	return nil
}

func (u *Users) Me(ctx context.Context, id uuid.UUID) (*domain.User, error) {
	row, err := u.repo.GetByID(ctx, id)
	if err != nil {
		return nil, apperr.Wrap(apperr.CodeInternal, "db", err)
	}
	if row == nil {
		return nil, apperr.New(apperr.CodeNotFound, "user not found")
	}
	d := row.Domain()
	return &d, nil
}
