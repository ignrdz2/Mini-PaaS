-- Tabla para logs de build en tiempo real y acceso histórico.
-- Cada fila es una línea del output del build (docker build + git clone).
-- ON DELETE CASCADE: si se elimina el deployment, sus logs también desaparecen.
CREATE TABLE IF NOT EXISTS deployment_logs (
    id              bigserial PRIMARY KEY,
    deployment_id   uuid NOT NULL REFERENCES deployments(id) ON DELETE CASCADE,
    created_at      timestamptz NOT NULL DEFAULT now(),
    message         text NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_deployment_logs_deployment_id
    ON deployment_logs(deployment_id);

-- Referencia al deployment origen de un rollback.
-- NULL en deployments normales; UUID del deployment origen en rollbacks.
ALTER TABLE deployments
    ADD COLUMN IF NOT EXISTS rolled_back_from uuid REFERENCES deployments(id);
