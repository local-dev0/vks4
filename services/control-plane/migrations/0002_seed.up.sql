-- Bootstrap admin. Пароль хешируется отдельно при первом старте,
-- здесь сохраняем плейсхолдер; control-plane при запуске установит реальный bcrypt-хеш.
INSERT INTO users (id, email, name, password_hash, role)
VALUES (
    '00000000-0000-0000-0000-000000000001',
    'admin@local',
    'Administrator',
    '$bootstrap$',
    'admin'
)
ON CONFLICT (email) DO NOTHING;
