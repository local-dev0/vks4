CREATE TABLE IF NOT EXISTS users (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    email         citext UNIQUE NOT NULL,
    name          text NOT NULL DEFAULT '',
    password_hash text NOT NULL,
    role          text NOT NULL CHECK (role IN ('admin','operator','moderator','viewer')),
    disabled      boolean NOT NULL DEFAULT false,
    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS rooms (
    id               uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name             text NOT NULL,
    description      text NOT NULL DEFAULT '',
    mode             text NOT NULL DEFAULT 'meeting' CHECK (mode IN ('meeting','lecture','webinar')),
    max_participants integer NOT NULL DEFAULT 50,
    waiting_room     boolean NOT NULL DEFAULT false,
    locked           boolean NOT NULL DEFAULT false,
    recording        boolean NOT NULL DEFAULT false,
    default_layout   jsonb NOT NULL DEFAULT '{"mode":"grid"}',
    created_by       uuid REFERENCES users(id) ON DELETE SET NULL,
    created_at       timestamptz NOT NULL DEFAULT now(),
    updated_at       timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_rooms_created_at ON rooms (created_at DESC);

CREATE TABLE IF NOT EXISTS recordings (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    room_id    uuid NOT NULL REFERENCES rooms(id) ON DELETE CASCADE,
    status     text NOT NULL CHECK (status IN ('running','finished','failed')),
    started_at timestamptz NOT NULL DEFAULT now(),
    ended_at   timestamptz,
    size_bytes bigint,
    url        text,
    metadata   jsonb NOT NULL DEFAULT '{}'
);

CREATE INDEX IF NOT EXISTS idx_recordings_room_id ON recordings (room_id);

CREATE TABLE IF NOT EXISTS audit_log (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    actor_id   uuid REFERENCES users(id) ON DELETE SET NULL,
    action     text NOT NULL,
    target     text NOT NULL DEFAULT '',
    payload    jsonb NOT NULL DEFAULT '{}',
    ip         inet,
    user_agent text,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_audit_created_at ON audit_log (created_at DESC);

CREATE TABLE IF NOT EXISTS refresh_tokens (
    jti        uuid PRIMARY KEY,
    user_id    uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    expires_at timestamptz NOT NULL,
    revoked    boolean NOT NULL DEFAULT false,
    created_at timestamptz NOT NULL DEFAULT now()
);
