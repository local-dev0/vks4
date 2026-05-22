-- Стиль подписи участников хранится в шаблоне (а не в комнате) — оператор настраивает
-- его на уровне дизайна layout'а, не для каждой комнаты отдельно.
ALTER TABLE layout_templates
    ADD COLUMN IF NOT EXISTS name_bg_alpha   double precision,
    ADD COLUMN IF NOT EXISTS name_bg_color   text,
    ADD COLUMN IF NOT EXISTS name_font_size  integer,
    ADD COLUMN IF NOT EXISTS name_font_color text;
