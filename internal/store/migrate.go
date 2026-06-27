package store

import (
	"context"
	"fmt"
)

// schemaMigration contiene el DDL completo del schema (v1 + v2).
// Usa IF NOT EXISTS / ADD COLUMN IF NOT EXISTS en cada objeto para que sea
// idempotente — se puede ejecutar en cada arranque sin importar si las tablas
// ya existen o si la columna fue agregada en una ejecución anterior.
const schemaMigration = `
CREATE TABLE IF NOT EXISTS apps (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name        text NOT NULL UNIQUE,
    repo_url    text NOT NULL,
    health_path text NOT NULL DEFAULT '/',
    created_at  timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS deployments (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    app_id          uuid NOT NULL REFERENCES apps(id),
    image_tag       text NOT NULL,
    status          text NOT NULL DEFAULT 'pending',
    container_id    text,
    internal_port   integer,
    created_at      timestamptz NOT NULL DEFAULT now(),
    finished_at     timestamptz,
    error_message   text
);

CREATE INDEX IF NOT EXISTS idx_deployments_app_id ON deployments(app_id);

-- v2: referencia al deployment origen de un rollback (NULL en deploys normales).
ALTER TABLE deployments
    ADD COLUMN IF NOT EXISTS rolled_back_from uuid REFERENCES deployments(id);

-- v2: logs de build línea a línea para SSE y acceso histórico.
CREATE TABLE IF NOT EXISTS deployment_logs (
    id              bigserial PRIMARY KEY,
    deployment_id   uuid NOT NULL REFERENCES deployments(id) ON DELETE CASCADE,
    created_at      timestamptz NOT NULL DEFAULT now(),
    message         text NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_deployment_logs_deployment_id
    ON deployment_logs(deployment_id);
`

// RunMigrations aplica el schema completo contra la base de datos.
// Es idempotente: se puede llamar en cada arranque sin efectos secundarios.
func (s *PostgresStore) RunMigrations(ctx context.Context) error {
	if _, err := s.pool.Exec(ctx, schemaMigration); err != nil {
		return fmt.Errorf("error al aplicar migraciones: %w", err)
	}
	return nil
}
