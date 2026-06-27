-- name: CreateDeploymentLog :one
INSERT INTO deployment_logs (deployment_id, message)
VALUES (@deployment_id, @message)
RETURNING *;

-- name: ListDeploymentLogs :many
SELECT * FROM deployment_logs
WHERE deployment_id = @deployment_id
ORDER BY id ASC;

-- name: ListDeploymentLogsAfter :many
SELECT * FROM deployment_logs
WHERE deployment_id = @deployment_id
  AND id > @after_id
ORDER BY id ASC;
