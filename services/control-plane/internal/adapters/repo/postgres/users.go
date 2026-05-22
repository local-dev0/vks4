package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vks4/vks4/services/control-plane/internal/domain"
)

type UserRow struct {
	ID           uuid.UUID
	Email        string
	Name         string
	PasswordHash string
	Role         string
	Disabled     bool
	CreatedAt    time.Time
}

func (r UserRow) Domain() domain.User {
	return domain.User{
		ID:        r.ID,
		Email:     r.Email,
		Name:      r.Name,
		Role:      domain.Role(r.Role),
		Disabled:  r.Disabled,
		CreatedAt: r.CreatedAt,
	}
}

type Users struct{ db *pgxpool.Pool }

func NewUsers(db *pgxpool.Pool) *Users { return &Users{db: db} }

func (s *Users) GetByEmail(ctx context.Context, email string) (*UserRow, error) {
	row := s.db.QueryRow(ctx, `
		SELECT id, email, name, password_hash, role, disabled, created_at
		FROM users WHERE email = $1`, email)
	var u UserRow
	err := row.Scan(&u.ID, &u.Email, &u.Name, &u.PasswordHash, &u.Role, &u.Disabled, &u.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &u, nil
}

func (s *Users) GetByID(ctx context.Context, id uuid.UUID) (*UserRow, error) {
	row := s.db.QueryRow(ctx, `
		SELECT id, email, name, password_hash, role, disabled, created_at
		FROM users WHERE id = $1`, id)
	var u UserRow
	err := row.Scan(&u.ID, &u.Email, &u.Name, &u.PasswordHash, &u.Role, &u.Disabled, &u.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &u, nil
}

func (s *Users) List(ctx context.Context) ([]UserRow, error) {
	rows, err := s.db.Query(ctx, `
		SELECT id, email, name, password_hash, role, disabled, created_at
		FROM users ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []UserRow
	for rows.Next() {
		var u UserRow
		if err := rows.Scan(&u.ID, &u.Email, &u.Name, &u.PasswordHash, &u.Role, &u.Disabled, &u.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

type CreateUserInput struct {
	Email, Name, PasswordHash, Role string
}

func (s *Users) Create(ctx context.Context, in CreateUserInput) (*UserRow, error) {
	row := s.db.QueryRow(ctx, `
		INSERT INTO users (email, name, password_hash, role)
		VALUES ($1,$2,$3,$4)
		RETURNING id, email, name, password_hash, role, disabled, created_at`,
		in.Email, in.Name, in.PasswordHash, in.Role)
	var u UserRow
	if err := row.Scan(&u.ID, &u.Email, &u.Name, &u.PasswordHash, &u.Role, &u.Disabled, &u.CreatedAt); err != nil {
		return nil, err
	}
	return &u, nil
}

func (s *Users) UpdateRole(ctx context.Context, id uuid.UUID, role string) error {
	_, err := s.db.Exec(ctx, `UPDATE users SET role=$2, updated_at=now() WHERE id=$1`, id, role)
	return err
}

func (s *Users) UpdatePassword(ctx context.Context, id uuid.UUID, hash string) error {
	_, err := s.db.Exec(ctx, `UPDATE users SET password_hash=$2, updated_at=now() WHERE id=$1`, id, hash)
	return err
}

func (s *Users) SetDisabled(ctx context.Context, id uuid.UUID, disabled bool) error {
	_, err := s.db.Exec(ctx, `UPDATE users SET disabled=$2, updated_at=now() WHERE id=$1`, id, disabled)
	return err
}

func (s *Users) Delete(ctx context.Context, id uuid.UUID) error {
	_, err := s.db.Exec(ctx, `DELETE FROM users WHERE id=$1`, id)
	return err
}
