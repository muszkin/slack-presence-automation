-- name: ListActiveOverrides :many
SELECT id, expires_at, status_emoji, status_text, presence, created_at
FROM overrides
WHERE expires_at > ?
ORDER BY created_at DESC;

-- name: InsertOverride :one
INSERT INTO overrides (expires_at, status_emoji, status_text, presence)
VALUES (?, ?, ?, ?)
RETURNING id, expires_at, status_emoji, status_text, presence, created_at;

-- name: DeleteOverride :execrows
DELETE FROM overrides WHERE id = ?;

-- name: DeleteExpiredOverrides :execrows
DELETE FROM overrides WHERE expires_at <= ?;

-- name: ClearOverrides :execrows
DELETE FROM overrides;
