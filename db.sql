BEGIN;

-- =========================================================
-- 1) USERS
-- =========================================================
CREATE TABLE users (
    id SERIAL PRIMARY KEY,
    email VARCHAR(100) NOT NULL UNIQUE,
    username VARCHAR(100) NOT NULL UNIQUE,
    password_hash VARCHAR(255) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- =========================================================
-- 2) FAILURE_CODE
-- =========================================================
CREATE TABLE failure_code (
    code VARCHAR PRIMARY KEY,
    description VARCHAR NOT NULL,
    is_retryable BOOLEAN NOT NULL DEFAULT false,
    is_critical BOOLEAN NOT NULL DEFAULT false,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- =========================================================
-- 3) UAV
-- =========================================================
CREATE TABLE uav (
    id SERIAL PRIMARY KEY,
    serial_number VARCHAR(255),
    name VARCHAR(255),
    model VARCHAR(255),
    firmware_version VARCHAR(255),
    camera_spec VARCHAR(255),
    image_url TEXT,
    max_range_meter INT,
    max_flight_time_min INT,
    owner_id INT REFERENCES users(id) ON DELETE RESTRICT,
    is_active BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMPTZ,
    deleted_by INT
);

CREATE UNIQUE INDEX uav_serial_number_key
    ON uav (serial_number)
    WHERE serial_number IS NOT NULL;

CREATE INDEX uav_owner_id_idx ON uav (owner_id);

-- =========================================================
-- 4) UAV_STATUS (1:1)
-- =========================================================
CREATE TABLE uav_status (
    uav_id INT PRIMARY KEY REFERENCES uav(id) ON DELETE CASCADE,
    battery_percent INT,
    is_connected BOOLEAN,
    is_in_flight BOOLEAN,
    is_docked BOOLEAN,
    latitude DOUBLE PRECISION,
    longitude DOUBLE PRECISION,
    last_heartbeat TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- =========================================================
-- 5) DOCKING
-- =========================================================
CREATE TABLE docking (
    id SERIAL PRIMARY KEY,
    uav_id INT NOT NULL REFERENCES uav(id) ON DELETE CASCADE,
    name VARCHAR(255),
    location_name VARCHAR(255),
    latitude DOUBLE PRECISION,
    longitude DOUBLE PRECISION,
    is_primary BOOLEAN NOT NULL DEFAULT false,
    is_active BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX docking_uav_id_idx ON docking (uav_id);

-- =========================================================
-- 6) DOCKING_STATUS (1:1)
-- =========================================================
CREATE TABLE docking_status (
    docking_id INT PRIMARY KEY REFERENCES docking(id) ON DELETE CASCADE,
    door_open BOOLEAN,
    drone_present BOOLEAN,
    charging BOOLEAN,
    temperature DOUBLE PRECISION,
    is_online BOOLEAN,
    last_heartbeat TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- =========================================================
-- 7) MISSIONS
-- =========================================================
CREATE TABLE missions (
    id SERIAL PRIMARY KEY,
    user_id INT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    uav_id INT REFERENCES uav(id) ON DELETE RESTRICT,
    mission_name VARCHAR(255) NOT NULL,
    schedule VARCHAR(255),
    is_recurring BOOLEAN NOT NULL DEFAULT false,
    status VARCHAR(50) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMPTZ,
    deleted_by INT
);

-- =========================================================
-- 8) WAYPOINTS
-- =========================================================
CREATE TABLE waypoints (
    id SERIAL PRIMARY KEY,
    mission_id INT NOT NULL REFERENCES missions(id) ON DELETE CASCADE,
    sequence_order INT NOT NULL,
    latitude DOUBLE PRECISION NOT NULL,
    longitude DOUBLE PRECISION NOT NULL,
    altitude DOUBLE PRECISION,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (mission_id, sequence_order)
);

-- =========================================================
-- 9) MISSION_HISTORY
-- =========================================================
CREATE TABLE mission_history (
    id SERIAL PRIMARY KEY,
    mission_id INT NOT NULL REFERENCES missions(id) ON DELETE RESTRICT,
    user_id INT REFERENCES users(id) ON DELETE RESTRICT,
    uav_id INT REFERENCES uav(id) ON DELETE RESTRICT,
    docking_id INT REFERENCES docking(id) ON DELETE RESTRICT,
    current_state VARCHAR(100),
    final_status VARCHAR(100),
    failure_code VARCHAR REFERENCES failure_code(code) ON DELETE RESTRICT,
    retry_count INT,
    total_duration_ms INT,
    mission_snapshot JSONB,
    failure_reason TEXT,
    started_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX mission_history_uav_id_idx ON mission_history (uav_id);
CREATE INDEX mission_history_docking_id_idx ON mission_history (docking_id);
CREATE INDEX mission_history_created_at_idx ON mission_history (created_at);

-- =========================================================
-- 10) MISSION_EVENT
-- =========================================================
CREATE TABLE mission_event (
    id BIGSERIAL PRIMARY KEY,
    history_id INT NOT NULL REFERENCES mission_history(id) ON DELETE CASCADE,
    from_state VARCHAR(100),
    to_state VARCHAR(100),
    result VARCHAR(100),
    failure_code VARCHAR REFERENCES failure_code(code) ON DELETE RESTRICT,
    message TEXT,
    is_terminal BOOLEAN,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX mission_event_history_created_at_idx
    ON mission_event (history_id, created_at);

-- =========================================================
-- 11) MISSION_MEDIA
-- =========================================================
CREATE TABLE mission_media (
    id BIGSERIAL PRIMARY KEY,
    history_id INT NOT NULL REFERENCES mission_history(id) ON DELETE CASCADE,
    event_id BIGINT,
    media_type VARCHAR(20) NOT NULL,
    file_path VARCHAR(512) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX mission_media_history_id_idx
    ON mission_media (history_id);

-- =========================================================
-- 12) DEVICE_TOKENS
-- =========================================================
CREATE TABLE device_tokens (
    id BIGSERIAL PRIMARY KEY,
    token_hash VARCHAR(64) NOT NULL UNIQUE,
    scope_type VARCHAR(20) NOT NULL,
    uav_id INT REFERENCES uav(id) ON DELETE CASCADE,
    docking_id INT REFERENCES docking(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_used_at TIMESTAMPTZ,
    revoked_at TIMESTAMPTZ,
    CONSTRAINT device_tokens_scope_check CHECK (
        (scope_type = 'uav' AND uav_id IS NOT NULL AND docking_id IS NULL) OR
        (scope_type = 'docking' AND docking_id IS NOT NULL AND uav_id IS NULL)
    )
);

CREATE INDEX device_tokens_uav_id_idx ON device_tokens (uav_id);
CREATE INDEX device_tokens_docking_id_idx ON device_tokens (docking_id);

COMMIT;