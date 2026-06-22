CREATE TABLE apps (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name        text NOT NULL UNIQUE,
    repo_url    text NOT NULL,
    health_path text NOT NULL DEFAULT '/',
    created_at  timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE deployments (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    app_id          uuid NOT NULL REFERENCES apps(id),
    image_tag       text NOT NULL,
    status          text NOT NULL DEFAULT 'pending',
        -- valores válidos: pending | building | healthcheck | running | failed | stopped
    container_id    text,
    internal_port   integer,
    created_at      timestamptz NOT NULL DEFAULT now(),
    finished_at     timestamptz,
    error_message   text
);

CREATE INDEX idx_deployments_app_id ON deployments(app_id);
