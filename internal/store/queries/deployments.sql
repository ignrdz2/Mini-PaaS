-- name: CreateDeployment :one
INSERT INTO deployments (app_id, image_tag)
VALUES ($1, $2)
RETURNING *;

-- name: GetDeployment :one
SELECT * FROM deployments
WHERE id = $1;

-- name: ListDeploymentsByApp :many
SELECT * FROM deployments
WHERE app_id = $1
ORDER BY created_at DESC;

-- name: GetActiveDeploymentByApp :one
SELECT * FROM deployments
WHERE app_id = $1
  AND status = 'running'
ORDER BY created_at DESC
LIMIT 1;

-- name: UpdateDeploymentStatus :one
UPDATE deployments
SET
    status        = $2,
    container_id  = COALESCE($3, container_id),
    internal_port = COALESCE($4, internal_port),
    finished_at   = COALESCE($5, finished_at),
    error_message = COALESCE($6, error_message)
WHERE id = $1
RETURNING *;
