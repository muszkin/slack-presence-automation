-- +goose Up
-- +goose StatementBegin
CREATE TABLE meeting_patterns (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    title_pattern TEXT    NOT NULL CHECK (length(title_pattern) > 0),
    status_emoji  TEXT    NOT NULL DEFAULT '',
    status_text   TEXT    NOT NULL DEFAULT '',
    presence      TEXT    NOT NULL DEFAULT 'dnd' CHECK (presence IN ('auto', 'away', 'dnd')),
    priority      INTEGER NOT NULL DEFAULT 0,
    created_at    INTEGER NOT NULL DEFAULT (unixepoch())
);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE INDEX idx_meeting_patterns_priority ON meeting_patterns(priority DESC);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_meeting_patterns_priority;
-- +goose StatementEnd

-- +goose StatementBegin
DROP TABLE IF EXISTS meeting_patterns;
-- +goose StatementEnd
