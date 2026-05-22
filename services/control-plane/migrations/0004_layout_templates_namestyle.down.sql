ALTER TABLE layout_templates
    DROP COLUMN IF EXISTS name_bg_alpha,
    DROP COLUMN IF EXISTS name_bg_color,
    DROP COLUMN IF EXISTS name_font_size,
    DROP COLUMN IF EXISTS name_font_color;
