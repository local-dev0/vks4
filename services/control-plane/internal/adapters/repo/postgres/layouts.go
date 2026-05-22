package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vks4/vks4/services/control-plane/internal/domain"
)

type LayoutTemplates struct{ db *pgxpool.Pool }

func NewLayoutTemplates(db *pgxpool.Pool) *LayoutTemplates { return &LayoutTemplates{db: db} }

type LayoutTemplateRow struct {
	ID         uuid.UUID
	Name       string
	Width      int
	Height     int
	Cells      []byte
	Background []byte
	CreatedBy  *uuid.UUID
	CreatedAt  time.Time
}

func (r LayoutTemplateRow) Domain() domain.LayoutTemplate {
	var cells []domain.LayoutCell
	_ = json.Unmarshal(r.Cells, &cells)
	var bg domain.LayoutBackground
	_ = json.Unmarshal(r.Background, &bg)
	return domain.LayoutTemplate{
		ID:        r.ID,
		Name:      r.Name,
		Width:     r.Width,
		Height:    r.Height,
		Cells:     cells,
		Background: bg,
		CreatedBy: r.CreatedBy,
		CreatedAt: r.CreatedAt,
	}
}

func (s *LayoutTemplates) List(ctx context.Context) ([]LayoutTemplateRow, error) {
	rows, err := s.db.Query(ctx, `
		SELECT id,name,width,height,cells,background,created_by,created_at
		FROM layout_templates ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []LayoutTemplateRow
	for rows.Next() {
		r := LayoutTemplateRow{}
		if err := rows.Scan(&r.ID, &r.Name, &r.Width, &r.Height, &r.Cells, &r.Background, &r.CreatedBy, &r.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, nil
}

func (s *LayoutTemplates) Get(ctx context.Context, id uuid.UUID) (*LayoutTemplateRow, error) {
	r := LayoutTemplateRow{}
	err := s.db.QueryRow(ctx, `
		SELECT id,name,width,height,cells,background,created_by,created_at
		FROM layout_templates WHERE id=$1`, id).Scan(
		&r.ID, &r.Name, &r.Width, &r.Height, &r.Cells, &r.Background, &r.CreatedBy, &r.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &r, nil
}

type CreateLayoutTemplateInput struct {
	Name       string
	Width      int
	Height     int
	Cells      []byte
	Background []byte
	CreatedBy  *uuid.UUID
}

func (s *LayoutTemplates) Create(ctx context.Context, in CreateLayoutTemplateInput) (*LayoutTemplateRow, error) {
	r := LayoutTemplateRow{}
	err := s.db.QueryRow(ctx, `
		INSERT INTO layout_templates (name, width, height, cells, background, created_by)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id, name, width, height, cells, background, created_by, created_at`,
		in.Name, in.Width, in.Height, in.Cells, in.Background, in.CreatedBy).Scan(
		&r.ID, &r.Name, &r.Width, &r.Height, &r.Cells, &r.Background, &r.CreatedBy, &r.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &r, nil
}

func (s *LayoutTemplates) Update(ctx context.Context, id uuid.UUID, set map[string]any) error {
	if len(set) == 0 {
		return nil
	}
	// собираем динамический UPDATE
	cols := []string{}
	args := []any{}
	idx := 1
	for k, v := range set {
		// whitelist допустимых колонок
		switch k {
		case "name", "width", "height", "cells", "background":
			cols = append(cols, k+"=$"+itoaN(idx))
			args = append(args, v)
			idx++
		}
	}
	if len(cols) == 0 {
		return nil
	}
	cols = append(cols, "updated_at=now()")
	args = append(args, id)
	q := "UPDATE layout_templates SET " + joinComma(cols) + " WHERE id=$" + itoaN(idx)
	_, err := s.db.Exec(ctx, q, args...)
	return err
}

func (s *LayoutTemplates) Delete(ctx context.Context, id uuid.UUID) error {
	_, err := s.db.Exec(ctx, `DELETE FROM layout_templates WHERE id=$1`, id)
	return err
}

func itoaN(n int) string {
	if n < 10 {
		return string('0' + rune(n))
	}
	// support for n>=10 (we won't have many cols но safety)
	s := ""
	for n > 0 {
		s = string('0'+rune(n%10)) + s
		n /= 10
	}
	return s
}

func joinComma(s []string) string {
	if len(s) == 0 {
		return ""
	}
	out := s[0]
	for _, v := range s[1:] {
		out += ", " + v
	}
	return out
}
