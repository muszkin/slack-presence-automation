-- name: GetAppliedState :one
SELECT status_emoji, status_text, presence, source, applied_at
FROM applied_state
WHERE id = 1;

-- name: SetAppliedState :exec
UPDATE applied_state
SET status_emoji = ?,
    status_text  = ?,
    presence     = ?,
    source       = ?,
    applied_at   = unixepoch()
WHERE id = 1;
