-- +goose Up
-- +goose StatementBegin
CREATE TABLE meeting_patterns_new (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    title_pattern TEXT    NOT NULL DEFAULT '',
    status_emoji  TEXT    NOT NULL DEFAULT '',
    status_text   TEXT    NOT NULL DEFAULT '',
    presence      TEXT    NOT NULL DEFAULT 'dnd' CHECK (presence IN ('auto', 'away', 'dnd')),
    priority      INTEGER NOT NULL DEFAULT 0,
    created_at    INTEGER NOT NULL DEFAULT (unixepoch())
);
-- +goose StatementEnd

-- +goose StatementBegin
INSERT INTO meeting_patterns_new (id, title_pattern, status_emoji, status_text, presence, priority, created_at)
SELECT id, title_pattern, status_emoji, status_text, presence, priority, created_at
FROM meeting_patterns;
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

-- +goose StatementBegin
INSERT INTO meeting_patterns (title_pattern, status_emoji, status_text, presence, priority)
SELECT '', ':calendar:', 'In a meeting', 'dnd', -1000
WHERE NOT EXISTS (
    SELECT 1 FROM meeting_patterns WHERE title_pattern = '' AND priority = -1000
);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
CREATE TABLE meeting_patterns_rollback (
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
INSERT INTO meeting_patterns_rollback (id, title_pattern, status_emoji, status_text, presence, priority, created_at)
SELECT id, title_pattern, status_emoji, status_text, presence, priority, created_at
FROM meeting_patterns
WHERE length(title_pattern) > 0;
-- +goose StatementEnd

-- +goose StatementBegin
DROP INDEX IF EXISTS idx_meeting_patterns_priority;
-- +goose StatementEnd

-- +goose StatementBegin
DROP TABLE meeting_patterns;
-- +goose StatementEnd

-- +goose StatementBegin
ALTER TABLE meeting_patterns_rollback RENAME TO meeting_patterns;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE INDEX idx_meeting_patterns_priority ON meeting_patterns(priority DESC);
-- +goose StatementEnd
