-- +goose Up
-- +goose StatementBegin
CREATE TABLE schedule_rules (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    days_of_week  INTEGER NOT NULL CHECK (days_of_week BETWEEN 1 AND 127),
    start_minute  INTEGER NOT NULL CHECK (start_minute BETWEEN 0 AND 1439),
    end_minute    INTEGER NOT NULL CHECK (end_minute BETWEEN 1 AND 1440 AND end_minute > start_minute),
    status_emoji  TEXT    NOT NULL DEFAULT '',
    status_text   TEXT    NOT NULL DEFAULT '',
    presence      TEXT    NOT NULL DEFAULT 'auto' CHECK (presence IN ('auto', 'away', 'dnd')),
    priority      INTEGER NOT NULL DEFAULT 0,
    created_at    INTEGER NOT NULL DEFAULT (unixepoch()),
    updated_at    INTEGER NOT NULL DEFAULT (unixepoch())
);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE INDEX idx_schedule_rules_priority ON schedule_rules(priority DESC);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TABLE overrides (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    expires_at   INTEGER NOT NULL,
    status_emoji TEXT    NOT NULL DEFAULT '',
    status_text  TEXT    NOT NULL DEFAULT '',
    presence     TEXT    NOT NULL DEFAULT 'auto' CHECK (presence IN ('auto', 'away', 'dnd')),
    created_at   INTEGER NOT NULL DEFAULT (unixepoch())
);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE INDEX idx_overrides_expires_at ON overrides(expires_at);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TABLE applied_state (
    id           INTEGER PRIMARY KEY CHECK (id = 1),
    status_emoji TEXT    NOT NULL DEFAULT '',
    status_text  TEXT    NOT NULL DEFAULT '',
    presence     TEXT    NOT NULL DEFAULT 'auto'    CHECK (presence IN ('auto', 'away', 'dnd')),
    source       TEXT    NOT NULL DEFAULT 'default' CHECK (source   IN ('override', 'calendar', 'schedule', 'default')),
    applied_at   INTEGER NOT NULL DEFAULT (unixepoch())
);
-- +goose StatementEnd

-- +goose StatementBegin
INSERT INTO applied_state (id) VALUES (1);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS applied_state;
-- +goose StatementEnd

-- +goose StatementBegin
DROP INDEX IF EXISTS idx_overrides_expires_at;
-- +goose StatementEnd

-- +goose StatementBegin
DROP TABLE IF EXISTS overrides;
-- +goose StatementEnd

-- +goose StatementBegin
DROP INDEX IF EXISTS idx_schedule_rules_priority;
-- +goose StatementEnd

-- +goose StatementBegin
DROP TABLE IF EXISTS schedule_rules;
-- +goose StatementEnd
