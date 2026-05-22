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

type Recordings struct{ db *pgxpool.Pool }

func NewRecordings(db *pgxpool.Pool) *Recordings { return &Recordings{db: db} }

type RecordingRow struct {
	ID        uuid.UUID
	RoomID    uuid.UUID
	Status    string
	StartedAt time.Time
	EndedAt   *time.Time
	SizeBytes *int64
	URL       *string
}

func (r RecordingRow) Domain() domain.Recording {
	url := ""
	if r.URL != nil {
		url = *r.URL
	}
	return domain.Recording{
		ID:        r.ID,
		RoomID:    r.RoomID,
		Status:    domain.RecordingStatus(r.Status),
		StartedAt: r.StartedAt,
		EndedAt:   r.EndedAt,
		SizeBytes: r.SizeBytes,
		URL:       url,
	}
}

func (s *Recordings) Create(ctx context.Context, roomID uuid.UUID) (*RecordingRow, error) {
	row := s.db.QueryRow(ctx, `
		INSERT INTO recordings (room_id, status) VALUES ($1,'running')
		RETURNING id, room_id, status, started_at, ended_at, size_bytes, url`, roomID)
	var r RecordingRow
	if err := row.Scan(&r.ID, &r.RoomID, &r.Status, &r.StartedAt, &r.EndedAt, &r.SizeBytes, &r.URL); err != nil {
		return nil, err
	}
	return &r, nil
}

func (s *Recordings) Finish(ctx context.Context, id uuid.UUID, size int64, url string) error {
	_, err := s.db.Exec(ctx, `
		UPDATE recordings SET status='finished', ended_at=now(), size_bytes=$2, url=$3 WHERE id=$1`,
		id, size, url)
	return err
}

func (s *Recordings) Fail(ctx context.Context, id uuid.UUID) error {
	_, err := s.db.Exec(ctx, `UPDATE recordings SET status='failed', ended_at=now() WHERE id=$1`, id)
	return err
}

func (s *Recordings) Get(ctx context.Context, id uuid.UUID) (*RecordingRow, error) {
	row := s.db.QueryRow(ctx, `
		SELECT id, room_id, status, started_at, ended_at, size_bytes, url
		FROM recordings WHERE id=$1`, id)
	var r RecordingRow
	if err := row.Scan(&r.ID, &r.RoomID, &r.Status, &r.StartedAt, &r.EndedAt, &r.SizeBytes, &r.URL); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &r, nil
}

func (s *Recordings) List(ctx context.Context, roomID *uuid.UUID) ([]RecordingRow, error) {
	var rows pgx.Rows
	var err error
	if roomID == nil {
		rows, err = s.db.Query(ctx, `
			SELECT id, room_id, status, started_at, ended_at, size_bytes, url
			FROM recordings ORDER BY started_at DESC LIMIT 500`)
	} else {
		rows, err = s.db.Query(ctx, `
			SELECT id, room_id, status, started_at, ended_at, size_bytes, url
			FROM recordings WHERE room_id=$1 ORDER BY started_at DESC LIMIT 500`, *roomID)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RecordingRow
	for rows.Next() {
		var r RecordingRow
		if err := rows.Scan(&r.ID, &r.RoomID, &r.Status, &r.StartedAt, &r.EndedAt, &r.SizeBytes, &r.URL); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Recordings) Delete(ctx context.Context, id uuid.UUID) error {
	_, err := s.db.Exec(ctx, `DELETE FROM recordings WHERE id=$1`, id)
	return err
}

func (s *Recordings) FindRunning(ctx context.Context, roomID uuid.UUID) (*RecordingRow, error) {
	row := s.db.QueryRow(ctx, `
		SELECT id, room_id, status, started_at, ended_at, size_bytes, url
		FROM recordings WHERE room_id=$1 AND status='running' ORDER BY started_at DESC LIMIT 1`, roomID)
	var r RecordingRow
	err := row.Scan(&r.ID, &r.RoomID, &r.Status, &r.StartedAt, &r.EndedAt, &r.SizeBytes, &r.URL)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &r, nil
}
