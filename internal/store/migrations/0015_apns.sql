-- The Apple apps' devices for notifications (APNs device tokens), per
-- account. environment is production (TestFlight, App Store) or sandbox
-- (development builds).
CREATE TABLE apns_devices (
    token       TEXT PRIMARY KEY,
    user_id     INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    environment TEXT NOT NULL CHECK (environment IN ('production', 'sandbox')),
    created_at  INTEGER NOT NULL
);
CREATE INDEX apns_devices_user ON apns_devices (user_id);
