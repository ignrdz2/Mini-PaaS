# Desarrollo

## Requisitos

- Go 1.25+
- Docker Desktop (daemon corriendo)
- Git

## Estructura del proyecto

```
cmd/
  orchestrator/   — binario del servidor (API REST)
  deployctl/      — binario del CLI
internal/
  api/            — handlers HTTP y setup del router
  builder/        — interfaz Builder + implementación DockerfileBuilder
  deploy/         — Orchestrator, WaitHealthy
  docker/         — wrapper sobre el SDK de Docker
  proxy/          — interfaz ProxyManager + implementación Traefik file provider
  store/          — interfaz Store + implementación PostgreSQL (sqlc + pgx/v5)
  integration/    — tests e2e (requieren docker compose up)
testdata/
  sample-app/     — app HTTP mínima de prueba para tests e2e
migrations/       — DDL SQL de referencia (las migraciones se aplican desde store/migrate.go)
docs/
  ARCHITECTURE.md — diseño del sistema y contratos entre versiones
  specs/          — especificación de cada versión (SPEC-v1.md, …)
  decisions/      — ADRs con el razonamiento detrás de cada decisión clave
  development/    — registro de implementación fase a fase
```

## Comandos habituales

**Compilar todo:**
```bash
go build ./...
```

**Compilar el CLI:**
```bash
go build -o bin/deployctl ./cmd/deployctl
```

**Levantar Postgres para tests:**
```bash
docker compose up -d postgres
```

**Correr todos los tests unitarios e integración con Postgres:**
```bash
docker compose up -d postgres
go test ./...
```

Los tests de `internal/store/` y `internal/docker/` y `internal/builder/` requieren
servicios reales (Postgres y Docker daemon). Los de `internal/deploy/` y `cmd/deployctl/`
son totalmente unitarios y no requieren infraestructura.

**Correr solo los tests que no necesitan infraestructura:**
```bash
go test ./internal/deploy/... ./cmd/deployctl/...
```

**Correr el test e2e completo** (requiere `docker compose up --build -d` y un repo público
con Dockerfile que lea `PORT` del entorno):
```bash
E2E_TEST_REPO_URL=https://github.com/usuario/repo \
  go test -tags=integration -timeout=5m ./internal/integration/...
```

## Regenerar código de sqlc

Si se modifica algún archivo en `internal/store/queries/`:
```bash
sqlc generate
```

Requiere `sqlc` v1.31.1 instalado (`go install github.com/sqlc-dev/sqlc/cmd/sqlc@v1.31.1`).
Los archivos generados (`db.go`, `models.go`, `querier.go`, `*.sql.go`) no deben editarse a mano.

## Stack de dependencias clave

| Paquete | Versión | Uso |
|---|---|---|
| `github.com/jackc/pgx/v5` | v5.10.0 | Driver Postgres |
| `github.com/docker/docker` | v28.5.2 | SDK de Docker |
| `github.com/go-chi/chi/v5` | v5.3.0 | Router HTTP |
| `github.com/spf13/cobra` | v1.10.2 | CLI |
| `github.com/fatih/color` | v1.19.0 | Output con color |
| `github.com/olekukonko/tablewriter` | v0.0.5 | Tablas en terminal |
| `gopkg.in/yaml.v3` | v3.0.1 | Config YAML de Traefik |
