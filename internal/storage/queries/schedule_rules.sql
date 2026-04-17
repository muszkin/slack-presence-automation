-- name: ListScheduleRules :many
SELECT id, days_of_week, start_minute, end_minute, status_emoji, status_text, presence, priority, created_at, updated_at
FROM schedule_rules
ORDER BY priority DESC, id ASC;

-- name: GetScheduleRule :one
SELECT id, days_of_week, start_minute, end_minute, status_emoji, status_text, presence, priority, created_at, updated_at
FROM schedule_rules
WHERE id = ?;

-- name: InsertScheduleRule :one
INSERT INTO schedule_rules (days_of_week, start_minute, end_minute, status_emoji, status_text, presence, priority)
VALUES (?, ?, ?, ?, ?, ?, ?)
RETURNING id, days_of_week, start_minute, end_minute, status_emoji, status_text, presence, priority, created_at, updated_at;

-- name: UpdateScheduleRule :one
UPDATE schedule_rules
SET days_of_week = ?,
    start_minute = ?,
    end_minute   = ?,
    status_emoji = ?,
    status_text  = ?,
    presence     = ?,
    priority     = ?,
    updated_at   = unixepoch()
WHERE id = ?
RETURNING id, days_of_week, start_minute, end_minute, status_emoji, status_text, presence, priority, created_at, updated_at;

-- name: DeleteScheduleRule :execrows
DELETE FROM schedule_rules WHERE id = ?;
