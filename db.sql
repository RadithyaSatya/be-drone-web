BEGIN;

-- =========================================================
-- USERS
-- =========================================================
CREATE TABLE users (
    id SERIAL PRIMARY KEY,
    email VARCHAR(100) NOT NULL UNIQUE,
    username VARCHAR(100) NOT NULL UNIQUE,
    password_hash VARCHAR(255) NOT NULL,
    dob DATE,
    phone VARCHAR(20),
    pilot_cert VARCHAR(100),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ
);

-- =========================================================
-- FAILURE_CODE
-- =========================================================
CREATE TABLE failure_code (
    code VARCHAR PRIMARY KEY,
    description VARCHAR NOT NULL,
    is_retryable BOOLEAN NOT NULL DEFAULT false,
    is_critical BOOLEAN NOT NULL DEFAULT false,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- =========================================================
-- UAV
-- =========================================================
CREATE TABLE uav (
    id SERIAL PRIMARY KEY,
    serial_number VARCHAR(255) UNIQUE,
    name VARCHAR(255),
    model VARCHAR(255),
    firmware_version VARCHAR(255),
    camera_spec VARCHAR(255),
    image_url TEXT,
    max_range_meter INT,
    max_flight_time_min INT,
    owner_id INT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    is_active BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ,
    deleted_at TIMESTAMPTZ,
    deleted_by INT REFERENCES users(id) ON DELETE SET NULL
);

CREATE INDEX uav_owner_id_idx ON uav (owner_id);

-- =========================================================
-- UAV_STATUS (1:1)
-- =========================================================
CREATE TABLE uav_status (
    uav_id INT PRIMARY KEY REFERENCES uav(id) ON DELETE CASCADE,
    battery_percent INT CHECK (battery_percent BETWEEN 0 AND 100),
    is_connected BOOLEAN,
    is_in_flight BOOLEAN,
    is_docked BOOLEAN,
    latitude DOUBLE PRECISION CHECK (latitude BETWEEN -90 AND 90),
    longitude DOUBLE PRECISION CHECK (longitude BETWEEN -180 AND 180),
    last_heartbeat TIMESTAMPTZ,
    updated_at TIMESTAMPTZ
);

-- =========================================================
-- DOCKING
-- =========================================================
CREATE TABLE docking (
    id SERIAL PRIMARY KEY,
    uav_id INT NOT NULL REFERENCES uav(id) ON DELETE CASCADE,
    name VARCHAR(255),
    location_name VARCHAR(255),
    latitude DOUBLE PRECISION CHECK (latitude BETWEEN -90 AND 90),
    longitude DOUBLE PRECISION CHECK (longitude BETWEEN -180 AND 180),
    is_primary BOOLEAN NOT NULL DEFAULT false,
    is_active BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ
);

CREATE INDEX docking_uav_id_idx ON docking (uav_id);

-- =========================================================
-- DOCKING_STATUS (1:1)
-- =========================================================
CREATE TABLE docking_status (
    docking_id INT PRIMARY KEY REFERENCES docking(id) ON DELETE CASCADE,
    door_open BOOLEAN,
    drone_present BOOLEAN,
    charging BOOLEAN,
    temperature DOUBLE PRECISION,
    is_online BOOLEAN,
    last_heartbeat TIMESTAMPTZ,
    updated_at TIMESTAMPTZ
);

-- =========================================================
-- MISSIONS
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
    updated_at TIMESTAMPTZ,
    deleted_at TIMESTAMPTZ,
    deleted_by INT REFERENCES users(id) ON DELETE SET NULL
);

CREATE INDEX missions_user_id_idx ON missions (user_id);
CREATE INDEX missions_uav_id_idx ON missions (uav_id);

-- =========================================================
-- WAYPOINTS
-- =========================================================
CREATE TABLE waypoints (
    id SERIAL PRIMARY KEY,
    mission_id INT NOT NULL REFERENCES missions(id) ON DELETE CASCADE,
    sequence_order INT NOT NULL,
    latitude DOUBLE PRECISION NOT NULL CHECK (latitude BETWEEN -90 AND 90),
    longitude DOUBLE PRECISION NOT NULL CHECK (longitude BETWEEN -180 AND 180),
    altitude DOUBLE PRECISION,
    action VARCHAR(100),
    action_duration BIGINT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (mission_id, sequence_order)
);

-- =========================================================
-- MISSION_HISTORY
-- =========================================================
CREATE TABLE mission_history (
    id SERIAL PRIMARY KEY,
    mission_id INT NOT NULL REFERENCES missions(id) ON DELETE RESTRICT,
    user_id INT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    uav_id INT REFERENCES uav(id) ON DELETE RESTRICT,
    docking_id INT REFERENCES docking(id) ON DELETE RESTRICT,
    current_state VARCHAR(100),
    final_status VARCHAR(100),
    failure_code VARCHAR REFERENCES failure_code(code) ON DELETE RESTRICT,
    retry_count INT DEFAULT 0,
    total_duration_ms BIGINT,
    mission_snapshot JSONB,
    failure_reason TEXT,
    started_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX mission_history_mission_id_idx ON mission_history (mission_id);
CREATE INDEX mission_history_user_id_idx ON mission_history (user_id);
CREATE INDEX mission_history_uav_id_idx ON mission_history (uav_id);
CREATE INDEX mission_history_created_at_idx ON mission_history (created_at);

-- =========================================================
-- MISSION_EVENT
-- =========================================================
CREATE TABLE mission_event (
    id BIGSERIAL PRIMARY KEY,
    history_id INT NOT NULL REFERENCES mission_history(id) ON DELETE CASCADE,
    from_state VARCHAR(100),
    to_state VARCHAR(100),
    result VARCHAR(100),
    failure_code VARCHAR REFERENCES failure_code(code) ON DELETE RESTRICT,
    message TEXT,
    is_terminal BOOLEAN DEFAULT false,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX mission_event_history_idx
    ON mission_event (history_id, created_at);

-- =========================================================
-- MISSION_MEDIA
-- =========================================================
CREATE TABLE mission_media (
    id BIGSERIAL PRIMARY KEY,
    history_id INT NOT NULL REFERENCES mission_history(id) ON DELETE CASCADE,
    event_id BIGINT REFERENCES mission_event(id) ON DELETE SET NULL,
    media_type VARCHAR(20) NOT NULL,
    file_path VARCHAR(512) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX mission_media_history_id_idx
    ON mission_media (history_id);

COMMIT;