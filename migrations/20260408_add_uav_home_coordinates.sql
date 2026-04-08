ALTER TABLE uav
    ADD COLUMN IF NOT EXISTS home_latitude DOUBLE PRECISION,
    ADD COLUMN IF NOT EXISTS home_longitude DOUBLE PRECISION;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'uav_home_latitude_check'
    ) THEN
        ALTER TABLE uav
            ADD CONSTRAINT uav_home_latitude_check
            CHECK (home_latitude BETWEEN -90 AND 90 OR home_latitude IS NULL);
    END IF;
END $$;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'uav_home_longitude_check'
    ) THEN
        ALTER TABLE uav
            ADD CONSTRAINT uav_home_longitude_check
            CHECK (home_longitude BETWEEN -180 AND 180 OR home_longitude IS NULL);
    END IF;
END $$;

UPDATE uav u
SET
    home_latitude = COALESCE(u.home_latitude, d.latitude),
    home_longitude = COALESCE(u.home_longitude, d.longitude)
FROM (
    SELECT DISTINCT ON (uav_id)
        uav_id,
        latitude,
        longitude
    FROM docking
    WHERE is_active = true
    ORDER BY uav_id, is_primary DESC, created_at DESC
) d
WHERE d.uav_id = u.id
  AND (u.home_latitude IS NULL OR u.home_longitude IS NULL);
