-- +goose Up
-- +goose StatementBegin
CREATE TABLE schedule_rules_new (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    days_of_week  INTEGER NOT NULL CHECK (days_of_week BETWEEN 1 AND 127),
    start_minute  INTEGER NOT NULL CHECK (start_minute BETWEEN 0 AND 1439),
    end_minute    INTEGER NOT NULL CHECK (end_minute BETWEEN 1 AND 1440 AND end_minute > start_minute),
    status_emoji  TEXT    NOT NULL DEFAULT '',
    status_text   TEXT    NOT NULL DEFAULT '',
    presence      TEXT    NOT NULL DEFAULT 'auto' CHECK (presence IN ('auto', 'away', 'dnd', 'available')),
    priority      INTEGER NOT NULL DEFAULT 0,
    created_at    INTEGER NOT NULL DEFAULT (unixepoch()),
    updated_at    INTEGER NOT NULL DEFAULT (unixepoch())
);
-- +goose StatementEnd

-- +goose StatementBegin
INSERT INTO schedule_rules_new
SELECT * FROM schedule_rules;
-- +goose StatementEnd

-- +goose StatementBegin
DROP INDEX IF EXISTS idx_schedule_rules_priority;
-- +goose StatementEnd

-- +goose StatementBegin
DROP TABLE schedule_rules;
-- +goose StatementEnd

-- +goose StatementBegin
ALTER TABLE schedule_rules_new RENAME TO schedule_rules;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE INDEX idx_schedule_rules_priority ON schedule_rules(priority DESC);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TABLE overrides_new (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    expires_at   INTEGER NOT NULL,
    status_emoji TEXT    NOT NULL DEFAULT '',
    status_text  TEXT    NOT NULL DEFAULT '',
    presence     TEXT    NOT NULL DEFAULT 'auto' CHECK (presence IN ('auto', 'away', 'dnd', 'available')),
    created_at   INTEGER NOT NULL DEFAULT (unixepoch())
);
-- +goose StatementEnd

-- +goose StatementBegin
INSERT INTO overrides_new
SELECT * FROM overrides;
-- +goose StatementEnd

-- +goose StatementBegin
DROP INDEX IF EXISTS idx_overrides_expires_at;
-- +goose StatementEnd

-- +goose StatementBegin
DROP TABLE overrides;
-- +goose StatementEnd

-- +goose StatementBegin
ALTER TABLE overrides_new RENAME TO overrides;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE INDEX idx_overrides_expires_at ON overrides(expires_at);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TABLE applied_state_new (
    id           INTEGER PRIMARY KEY CHECK (id = 1),
    status_emoji TEXT    NOT NULL DEFAULT '',
    status_text  TEXT    NOT NULL DEFAULT '',
    presence     TEXT    NOT NULL DEFAULT 'auto'    CHECK (presence IN ('auto', 'away', 'dnd', 'available')),
    source       TEXT    NOT NULL DEFAULT 'default' CHECK (source   IN ('override', 'calendar', 'schedule', 'default')),
    applied_at   INTEGER NOT NULL DEFAULT (unixepoch())
);
-- +goose StatementEnd

-- +goose StatementBegin
INSERT INTO applied_state_new
SELECT * FROM applied_state;
-- +goose StatementEnd

-- +goose StatementBegin
DROP TABLE applied_state;
-- +goose StatementEnd

-- +goose StatementBegin
ALTER TABLE applied_state_new RENAME TO applied_state;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TABLE meeting_patterns_new (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    title_pattern TEXT    NOT NULL DEFAULT '',
    status_emoji  TEXT    NOT NULL DEFAULT '',
    status_text   TEXT    NOT NULL DEFAULT '',
    presence      TEXT    NOT NULL DEFAULT 'dnd' CHECK (presence IN ('auto', 'away', 'dnd', 'available')),
    priority      INTEGER NOT NULL DEFAULT 0,
    created_at    INTEGER NOT NULL DEFAULT (unixepoch())
);
-- +goose StatementEnd

-- +goose StatementBegin
INSERT INTO meeting_patterns_new
SELECT * FROM meeting_patterns;
-- +goose StatementEnd

-- +goose StatementBegin
DROP INDEX IF EXISTS idx_meeting_patterns_priority;
-- +goose StatementEnd

-- +goose StatementBegin
DROP TABLE meeting_patterns;
-- +goose StatementEnd

-- +goose StatementBegin
ALTER TABLE meeting_patterns_new RENAME TO meeting_patterns;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE INDEX idx_meeting_patterns_priority ON meeting_patterns(priority DESC);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DELETE FROM schedule_rules WHERE presence = 'available';
-- +goose StatementEnd

-- +goose StatementBegin
DELETE FROM overrides WHERE presence = 'available';
-- +goose StatementEnd

-- +goose StatementBegin
DELETE FROM meeting_patterns WHERE presence = 'available';
-- +goose StatementEnd

-- +goose StatementBegin
UPDATE applied_state SET presence = 'auto' WHERE presence = 'available';
-- +goose StatementEnd
