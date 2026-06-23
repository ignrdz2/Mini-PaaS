-- name: CreateApp :one
INSERT INTO apps (name, repo_url, health_path)
VALUES ($1, $2, $3)
RETURNING *;

-- name: GetAppByName :one
SELECT * FROM apps
WHERE name = $1;

-- name: ListApps :many
SELECT * FROM apps
ORDER BY created_at DESC;

-- name: DeleteApp :exec
DELETE FROM apps
WHERE name = $1;
