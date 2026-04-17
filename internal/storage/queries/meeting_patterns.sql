-- name: ListMeetingPatterns :many
SELECT id, title_pattern, status_emoji, status_text, presence, priority, created_at
FROM meeting_patterns
ORDER BY priority DESC, id ASC;

-- name: InsertMeetingPattern :one
INSERT INTO meeting_patterns (title_pattern, status_emoji, status_text, presence, priority)
VALUES (?, ?, ?, ?, ?)
RETURNING id, title_pattern, status_emoji, status_text, presence, priority, created_at;

-- name: DeleteMeetingPattern :execrows
DELETE FROM meeting_patterns WHERE id = ?;
