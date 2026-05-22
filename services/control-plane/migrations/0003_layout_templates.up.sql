-- Шаблоны раскладок: переиспользуемые layouts с slot-N плейсхолдерами.
-- Создаются оператором в Layout Editor, применяются к комнатам через UI.
CREATE TABLE IF NOT EXISTS layout_templates (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name       text NOT NULL,
    width      integer NOT NULL DEFAULT 1280,
    height     integer NOT NULL DEFAULT 720,
    cells      jsonb NOT NULL DEFAULT '[]'::jsonb,
    background jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_by uuid REFERENCES users(id) ON DELETE SET NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_layout_templates_created_at ON layout_templates (created_at DESC);
