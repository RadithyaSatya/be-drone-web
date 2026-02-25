CREATE TABLE users (
    id SERIAL PRIMARY KEY,
    email VARCHAR(100) NOT NULL UNIQUE,
    dob DATE,                         
    phone VARCHAR(20),                
    username VARCHAR(100) NOT NULL UNIQUE,
    pilot_cert VARCHAR(100),
    password_hash VARCHAR(255) NOT NULL, 
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE missions (
    id SERIAL PRIMARY KEY,
    user_id INT NOT NULL REFERENCES users(id),
    uav_id INT,
    mission_name VARCHAR(255) NOT NULL,
    schedule VARCHAR(255) NOT NULL,
    is_recurring BOOLEAN NOT NULL DEFAULT FALSE,
    status VARCHAR(50) NOT NULL,
    timestamp TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMPTZ
);


CREATE TABLE waypoints (
    id SERIAL PRIMARY KEY,
    mission_id INT NOT NULL REFERENCES missions(id),
    sequence_order INT NOT NULL,
    latitude NUMERIC(10, 8) NOT NULL,
    longitude NUMERIC(11, 8) NOT NULL,
    altitude NUMERIC(6, 2) NOT NULL,
    action VARCHAR(50),
    action_duration INT,
    UNIQUE (mission_id, sequence_order) 
);

CREATE TABLE locations (
    id SERIAL PRIMARY KEY,
    entity_id INT,
    type INT,
    latitude NUMERIC(10, 8) NOT NULL,
    longitude NUMERIC(11, 8) NOT null,
    timestamp TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

INSERT INTO locations  (
    entity_id, 
    type, 
    latitude, 
    longitude
    
) VALUES (
    1, 
    1, 
    -6.261001324327185, 
    106.95949672320901
    
);

CREATE TABLE footages (
    id SERIAL PRIMARY KEY,
    uav_id INTEGER NOT NULL,
    mission_id INTEGER REFERENCES missions(id) ON DELETE SET NULL,
    filename VARCHAR(255) NOT NULL,
    file_path VARCHAR(512) NOT NULL,
    uploaded_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE mission_history (
    id SERIAL PRIMARY KEY,
    mission_id INT NOT NULL REFERENCES missions(id),
    user_id INT,
    uav_id INT,
    status VARCHAR(50) NOT NULL,
    mission_snapshot JSONB NOT NULL,
    failure_reason TEXT,
    started_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE mission_history_media (
    id SERIAL PRIMARY KEY,
    history_id INT NOT NULL REFERENCES mission_history(id),
    mission_id INT NOT NULL REFERENCES missions(id),
    media_type VARCHAR(20) NOT NULL,
    file_path VARCHAR(512) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX mission_history_media_mission_id_idx ON mission_history_media (mission_id);
CREATE INDEX mission_history_media_history_id_idx ON mission_history_media (history_id);
CREATE INDEX missions_deleted_at_idx ON missions (deleted_at);
CREATE INDEX missions_user_status_active_idx ON missions (user_id, status, schedule) WHERE deleted_at IS NULL;
CREATE INDEX mission_history_mission_id_created_at_idx ON mission_history (mission_id, created_at DESC);
CREATE INDEX mission_history_user_id_created_at_idx ON mission_history (user_id, created_at DESC);


CREATE TABLE mission_log (
    id SERIAL PRIMARY KEY,
    mission_id INTEGER NOT NULL,
    uav_id INTEGER NOT NULL,
    waypoints INTEGER NOT NULL,
    altitude INTEGER NOT NULL,
    coordinate VARCHAR(512) NOT NULL,
    batt INTEGER NOT NULL,
    times TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

ALTER TABLE mission_log
    ALTER COLUMN altitude TYPE DOUBLE PRECISION,
    ALTER COLUMN batt TYPE DOUBLE PRECISION,
    ALTER COLUMN coordinate TYPE TEXT;



INSERT INTO users (
    email, 
    dob, 
    phone, 
    username, 
    pilot_cert, 
    password_hash
) VALUES (
    'rishaldy7@gmail.com', 
    '1990-01-01', 
    '+628561947593', 
    'aldy', 
    '998271219289', 
    '$2a$10$wT.fQ8fH3S5p.hT.Q1u0uO/t2S1x.M.Q1u0uO/t2S1x.M.');

TRUNCATE TABLE mission_log;
ALTER SEQUENCE mission_log_id_seq RESTART WITH 1;
