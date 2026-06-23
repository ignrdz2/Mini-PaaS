# Mini-PaaS v1 — Progreso de implementación

Registro de lo implementado en cada fase de v1, contrastado con lo planificado en
[SPEC-v1.md](../specs/SPEC-v1.md) y [ARCHITECTURE.md](../ARCHITECTURE.md).

---

## Fase 0 — Setup ✅

**Planificado:** inicializar módulo Go, estructura de carpetas, docker-compose.yml con Postgres
y Traefik, migración inicial, y decidir la librería de acceso a Postgres.

**Implementado:**

- Módulo Go inicializado (`github.com/ignrdz2/mini-paas`, go 1.25.0). Todos los paquetes en
  `internal/` creados como stubs vacíos.
- `docker-compose.yml` con Postgres 16 y Traefik v3.0. El servicio `orchestrator` está
  preparado como bloque comentado para activarse en Fase 8.
- Migración `migrations/0001_init.up.sql` con las tablas `apps` y `deployments`.
  El schema incluye `health_path` (con default `/`) en `apps`, que no figura en el DDL
  de SPEC-v1.md pero sí estaba contemplado en la sección Healthcheck — la migración es
  la fuente de verdad.
- ADR 005 documentado: se elige **sqlc + pgx/v5** sobre pgx directo o un ORM.

**Desviaciones del plan:** ninguna. El único ajuste fue agregar el índice
`idx_deployments_app_id` que SPEC-v1.md menciona en el DDL pero omitió en el checklist.

---

## Fase 1 — Store ✅

**Planificado:** interfaz `Store`, implementación Postgres con sqlc + pgx/v5, tests de
integración contra Postgres real.

**Implementado:**

### Dependencias agregadas
- `github.com/jackc/pgx/v5 v5.10.0` (driver runtime)
- `github.com/jackc/pgx/v5/pgxpool` (pool de conexiones)
- `github.com/jackc/puddle/v2` (transitiva de pgxpool)

### Queries SQL (`internal/store/queries/`)
- `apps.sql`: `CreateApp`, `GetAppByName`, `ListApps`, `DeleteApp`
- `deployments.sql`: `CreateDeployment`, `GetDeployment`, `ListDeploymentsByApp`,
  `GetActiveDeploymentByApp`, `UpdateDeploymentStatus`
- `UpdateDeploymentStatus` usa `COALESCE($n, columna)` para actualizar campos opcionales
  sin necesidad de múltiples queries.

### Código generado por sqlc v1.31.1 (`internal/store/`, no editar)
- `db.go` — interfaz DBTX, struct Queries, constructor New
- `models.go` — structs App y Deployment con tipos pgtype para campos nullable
- `querier.go` — interfaz Querier (generada automáticamente con `emit_interface: true`)
- `apps.sql.go`, `deployments.sql.go` — implementaciones de cada query

### Código de negocio escrito a mano
- `store.go` — interfaz `Store` con firmas limpias (sin structs de params para casos simples)
  y `UpdateDeploymentParams` (tipo propio del dominio, no del código generado)
- `postgres.go` — `PostgresStore` que embebe `*Queries` y wrappea las operaciones:
  - `CreateApp`, `GetApp`, `CreateDeployment`, `ListDeployments`, `GetActiveDeployment`,
    `UpdateDeploymentStatus`: wrappers explícitos que adaptan firmas
  - `ListApps`, `DeleteApp`, `GetDeployment`: promovidos directamente desde `*Queries`
    (la firma del método generado coincide exactamente con la interfaz Store)

### Tests (`internal/store/store_test.go`)
11 tests de integración en `package store` (acceso interno para poder usar `pool.Exec`
y ejecutar `TRUNCATE apps CASCADE` entre tests).

| Test | Qué verifica |
|---|---|
| TestCreateApp_HappyPath | inserción correcta, ID válido |
| TestCreateApp_DuplicateName | error en nombre duplicado (constraint UNIQUE) |
| TestGetApp_Existente | recuperar app por nombre |
| TestGetApp_NoExiste | retorna `pgx.ErrNoRows` |
| TestListApps | cuenta correcta con múltiples apps |
| TestDeleteApp | app desaparece tras delete |
| TestCreateDeployment | status inicial `pending`, AppID correcto |
| TestGetActiveDeployment_SinRunning | retorna `pgx.ErrNoRows` si no hay `running` |
| TestUpdateDeploymentStatus | actualiza status, container_id, internal_port |
| TestGetActiveDeployment_ConRunning | retorna el deployment correcto tras update a `running` |
| TestListDeployments | cuenta correcta para una app dada |

**Resultado final:** 11/11 PASS, `go build ./...` limpio.

### Ajustes respecto al plan
- La opción `emit_pointers_for_null_fields: true` del prompt de Fase 1 **no existe** en
  sqlc v1.28 ni v1.31.1 — se eliminó del `sqlc.yaml`. Los campos nullable se generan con
  tipos `pgtype.Text`, `pgtype.Int4`, `pgtype.Timestamptz` (no punteros Go).
- sqlc genera `RepoUrl` (minúscula l), no `RepoURL`. Se usa `RepoUrl` en todo el código.
- La conversión directa `UpdateDeploymentStatusParams(params)` es válida porque ambos
  structs tienen campos idénticos sin struct tags — comportamiento garantizado por la spec
  de Go.

---

## Fase 2 — Builder ✅

**Planificado:** interfaz `Builder`, `DockerfileBuilder` usando el SDK de Docker (no
`exec("docker build")`), captura de logs, tests con Docker daemon real.

**Implementado:**

### Dependencias agregadas
- `github.com/docker/docker v28.5.2+incompatible` (módulo monorepo del SDK oficial)
  y sus transitivas: `github.com/containerd/errdefs`, `github.com/distribution/reference`,
  `github.com/docker/go-connections`, `github.com/docker/go-units`.

> El plan original indicaba `go get github.com/docker/docker/client` y
> `github.com/docker/docker/api/types` como módulos separados, pero a partir de Docker v27+
> el SDK volvió al módulo monorepo `github.com/docker/docker` (con `+incompatible` porque
> aún no usa Go modules v2). Se usa ese módulo directamente.

### Interfaz (`internal/builder/builder.go`)
```go
type Builder interface {
    Build(ctx context.Context, sourcePath string, imageTag string) (BuildResult, error)
}
type BuildResult struct {
    ImageTag string
    Logs     string  // últimos 4000 chars del output del build
}
```
Firma idéntica a la definida en ARCHITECTURE.md — interfaz estable.

### Implementación (`internal/builder/dockerfile.go`)
`DockerfileBuilder` recibe un `*client.Client` en el constructor (`NewDockerfileBuilder`).

Flujo de `Build`:
1. Verifica que existe `sourcePath/Dockerfile` — error explícito si no.
2. Empaqueta el directorio en un tar en memoria (`crearTarDesdeDirectorio`), usando
   separadores Unix para compatibilidad con el daemon de Docker.
3. Llama `client.ImageBuild()` con `ImageBuildOptions{Tags, Dockerfile, Remove: true}`.
4. Parsea el stream de respuesta (JSON line-by-line): acumula `stream` en un buffer,
   detecta mensajes con campo `error` y retorna error inmediatamente.
5. Trunca los logs a los últimos 4000 caracteres antes de retornar (`truncarUltimos4000`).

### Tests (`internal/builder/builder_test.go`)
3 tests de integración en `package builder_test`, requieren Docker daemon corriendo.

| Test | Dockerfile de prueba | Resultado esperado |
|---|---|---|
| TestBuild_HappyPath | `FROM alpine:3.20\nCMD ["echo","ok"]` | BuildResult con ImageTag y Logs no vacío |
| TestBuild_SinDockerfile | directorio vacío | error que menciona "Dockerfile" |
| TestBuild_DockerfileInvalido | instrucción inexistente `INSTRUCCION_INVALIDA` | error que menciona "build" o "Dockerfile" |

`TestBuild_HappyPath` limpia la imagen generada con `ImageRemove` en `t.Cleanup`.

**Resultado final:** 3/3 PASS, `go build ./...` limpio.

### Ajustes respecto al plan
- En Docker v28, los Dockerfiles con instrucciones desconocidas son rechazados por el
  daemon en la fase de parseo HTTP (antes de que arranque el stream de build), por lo que
  el error llega desde `ImageBuild()` directamente, no desde el stream. El test
  `TestBuild_DockerfileInvalido` valida el error real recibido.
- `ImageRemove` en v28 recibe `image.RemoveOptions{}` (struct) en lugar de un `bool`
  como en versiones anteriores del SDK.

---

## Próximas fases

| Fase | Descripción | Estado |
|---|---|---|
| Fase 3 | Runtime + Healthcheck (DockerClient wrapper, WaitHealthy) | Pendiente |
| Fase 4 | ProxyManager (TraefikFileProxyManager, YAML atómico) | Pendiente |
| Fase 5 | Orquestación (coordina Builder → runtime → healthcheck → proxy) | Pendiente |
| Fase 6 | API REST (7 endpoints, chi router, main.go) | Pendiente |
| Fase 7 | CLI deployctl (cobra, output tabular, --json) | Pendiente |
| Fase 8 | Integración end-to-end (docker compose up, test e2e) | Pendiente |
| Fase 9 | Documentación pública (README, limpieza de docs/) | Pendiente |
