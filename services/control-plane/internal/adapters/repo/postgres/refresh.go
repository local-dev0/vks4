package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type RefreshTokens struct{ db *pgxpool.Pool }

func NewRefreshTokens(db *pgxpool.Pool) *RefreshTokens { return &RefreshTokens{db: db} }

func (s *RefreshTokens) Store(ctx context.Context, jti, userID uuid.UUID, exp time.Time) error {
	_, err := s.db.Exec(ctx, `
		INSERT INTO refresh_tokens (jti, user_id, expires_at) VALUES ($1,$2,$3)
		ON CONFLICT (jti) DO NOTHING`, jti, userID, exp)
	return err
}

func (s *RefreshTokens) Revoke(ctx context.Context, jti uuid.UUID) error {
	_, err := s.db.Exec(ctx, `UPDATE refresh_tokens SET revoked=true WHERE jti=$1`, jti)
	return err
}

func (s *RefreshTokens) IsValid(ctx context.Context, jti uuid.UUID) (bool, error) {
	row := s.db.QueryRow(ctx,
		`SELECT revoked, expires_at FROM refresh_tokens WHERE jti=$1`, jti)
	var revoked bool
	var exp time.Time
	if err := row.Scan(&revoked, &exp); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil
		}
		return false, err
	}
	return !revoked && time.Now().Before(exp), nil
}
