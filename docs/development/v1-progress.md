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

---

## Fase 3 — Runtime + Healthcheck ✅

**Planificado:** wrapper sobre el SDK de Docker para correr/detener containers y asignar
puerto libre; lógica de healthcheck con polling HTTP y timeout.

**Implementado:**

### `internal/docker/client.go` — `DockerClient`

Constructor `NewDockerClient()` usa `client.FromEnv` + `WithAPIVersionNegotiation` para
respetar `DOCKER_HOST` y negociar versión de API automáticamente.

**`RunContainer(ctx, imageTag, appName)`**
- Llama a `FindFreePort()` para obtener un puerto libre del OS.
- Crea el container con `ContainerCreate` usando `NetworkMode: "host"` e inyectando
  `PORT=<n>` como variable de entorno.
- Arranca con `ContainerStart`. Retorna `(containerID, port, error)`.
- Etiqueta el container con `mini-paas.app=<appName>` para identificación futura.

> Convención documentada en código: el orquestador elige el puerto y lo pasa al container
> como `PORT`. La app deployada es responsable de leer `PORT` y escuchar en ese puerto.
> No se hace port binding explícito en Docker — el proxy y el healthcheck acceden al
> container directamente vía red host.

**`StopAndRemoveContainer(ctx, containerID)`**
- `ContainerStop` + `ContainerRemove(Force: true)`. Tolera `IsErrNotFound` en ambas
  operaciones — no es error si el container ya fue removido.

**`FindFreePort()`**
- `net.Listen("tcp", "localhost:0")` → lee `ln.Addr().(*net.TCPAddr).Port` → cierra.

### `internal/deploy/healthcheck.go` — `WaitHealthy`

```go
func WaitHealthy(ctx context.Context, port int, healthPath string, timeout time.Duration) error
```

- Crea un `context.WithTimeout` interno sobre el contexto recibido.
- Ticker a 500ms. En cada tick: `http.NewRequestWithContext` + `httpClient.Do` (timeout
  de 2s por request).
- Considera healthy cualquier `resp.StatusCode < 500`. Errores de red → reintento silencioso.
- Si el contexto se cancela o el timeout se agota: retorna error descriptivo con la URL
  y la duración configurada.

### Tests

**`internal/docker/client_test.go`** (3 tests, requieren Docker daemon):

| Test | Qué verifica |
|---|---|
| TestFindFreePort | retorna puerto > 0 sin error |
| TestRunContainer_y_StopAndRemove | container arranca, se detiene y remueve; segunda llamada tolerante |
| TestRunContainer_ImagenInexistente | error con imagen que no existe en el daemon |

**`internal/deploy/healthcheck_test.go`** (4 tests, sin Docker):

| Test | Qué verifica |
|---|---|
| TestWaitHealthy_ServidorSano | httptest.Server con 200 → WaitHealthy retorna nil |
| TestWaitHealthy_HealthPath | path `/health` con 404 (< 500) → también healthy |
| TestWaitHealthy_Timeout | puerto cerrado + timeout de 1s → error en ≤2s |
| TestWaitHealthy_ContextCancelado | contexto cancelado previo → error inmediato |

**Resultado final:** 7/7 PASS (4 deploy + 3 docker), `go build ./...` limpio.

### Ajustes respecto al plan
- `alpine:3.20` en `TestRunContainer_y_StopAndRemove`: alpine no tiene proceso por
  defecto que ocupe el puerto, pero basta para verificar que el container arranca y se
  puede remover. El puerto es irrelevante para este test (el healthcheck se testea aparte).
- Se agregó `TestRunContainer_ImagenInexistente` (no estaba en el plan) para cubrir el
  caso de fallo de `ContainerCreate` con imagen ausente.

---

---

## Fase 4 — ProxyManager ✅

**Planificado:** interfaz `ProxyManager`, `TraefikFileProxyManager` con serialización YAML
y escritura atómica, tests sin necesitar Traefik corriendo.

**Implementado:**

### Dependencia agregada
- `gopkg.in/yaml.v3` para serialización del YAML de Traefik.

### `internal/proxy/proxy.go` — interfaz estable
```go
type ProxyManager interface {
    Sync(ctx context.Context, routes []Route) error
}
type Route struct {
    AppName    string
    TargetPort int
}
```
Firma idéntica a la definida en ARCHITECTURE.md.

### `internal/proxy/traefik_file.go` — `TraefikFileProxyManager`

Constructor `NewTraefikFileProxyManager(configPath string)` recibe el path completo al
archivo YAML dinámico (valor de `TRAEFIK_CONFIG_PATH`).

**`Sync(ctx, routes)`:**
- Routes vacías → escribe YAML vacío válido (`{}` serializado).
- Por cada route genera las tres entradas requeridas por Traefik file provider:
  - Router: `rule: "PathPrefix(\`/<appName>\`)"`, service, middlewares
  - Middleware: `stripPrefix` con `prefixes: ["/<appName>"]`
  - Service: `loadBalancer.servers[0].url = "http://localhost:<targetPort>"`
- Serializa el config completo de una vez (no incremental) con `yaml.Marshal`.
- Escritura atómica en `escribirAtomico`: `os.CreateTemp` en el mismo directorio → write
  → close → `os.Rename`. Si falla en cualquier punto limpia el temporal.

### Tests (`internal/proxy/proxy_test.go`) — 6 tests, sin Traefik real

| Test | Qué verifica |
|---|---|
| TestSync_RoutesVacias | archivo creado, YAML parseble sin error |
| TestSync_UnaRoute | router, middleware, service y puerto correctos (string matching) |
| TestSync_UnaRoute_EstructuraCompleta | estructura YAML validada via `yaml.Unmarshal` a map |
| TestSync_VariasRoutes | todas las apps y puertos presentes en el mismo YAML |
| TestSync_DirectorioInexistente | error cuando el directorio padre no existe |
| TestSync_SobreescribeContenidoPrevio | segunda Sync elimina apps que ya no están en routes |

Todos los tests usan `t.TempDir()` como directorio base del configPath.

**Resultado final:** 6/6 PASS, `go build ./...` limpio.

### Ajustes respecto al plan
- Ninguno. La implementación sigue el plan exactamente.

---

---

## Fase 5 — Orquestación ✅

**Planificado:** `internal/deploy/orchestration.go` coordinando Store → clone → Builder →
RunContainer → WaitHealthy → ProxyManager, con transiciones de estado correctas y manejo
de errores en cada paso.

**Implementado:**

### `internal/deploy/orchestration.go` — `Orchestrator`

**Interfaces y constructores:**
- `dockerRunner`: interfaz interna mínima (`RunContainer` + `StopAndRemoveContainer`)
  que permite sustituir `*docker.DockerClient` por un stub en tests unitarios.
- `NewOrchestrator(s, b, d, p, ...Option)`: constructor público, acepta `*docker.DockerClient`.
- `NewOrchestratorWithRunner(s, b, d, p, ...Option)`: constructor de test, acepta `dockerRunner`.
- `WithHealthTimeout(d)`: `Option` funcional para sobreescribir el timeout de 30s.
- `ErrAppNotFound`: error centinela exportado para que los handlers de la API puedan
  mapear el caso "app no existe" a un 404.

**`Deploy(ctx, appName)` — secuencia completa:**

| Paso | Estado | Acción |
|---|---|---|
| 1 | — | `GetApp` → `ErrAppNotFound` si no existe |
| 2 | `pending` | `CreateDeployment` |
| 3 | `pending` | `git clone --depth 1` a `os.MkdirTemp`; `defer os.RemoveAll` |
| 4 | `building` | `Builder.Build` |
| 5 | `building` | `RunContainer` → containerID, port |
| 6 | `healthcheck` | `UpdateDeploymentStatus` con containerID + port; luego `WaitHealthy` |
| 7 | `running` | `UpdateDeploymentStatus` con `finished_at=now()` |
| 8 | — | Buscar deployment `running` anterior → `StopAndRemoveContainer` + marcar `stopped` |
| 9 | — | `ListApps` + `GetActiveDeployment` por cada app → `ProxyManager.Sync` (fallo no fatal) |
| 10 | — | Retornar deployment con status `running` |

En cada paso de fallo: se llama `fallarDeployment` que marca el deployment como `failed`
con `error_message` + `finished_at` antes de retornar el error.

### Tests unitarios (`internal/deploy/orchestration_test.go`) — 5 tests

Todos usan stubs en memoria (sin Docker real, sin Postgres, sin Traefik):

| Test | Qué verifica |
|---|---|
| TestDeploy_HappyPath_SecuenciaDeEstados | status `running`, containerID y port correctos, proxy recibe routes |
| TestDeploy_AppNoExiste | retorna `ErrAppNotFound` |
| TestDeploy_FalloBuild_EstadoFailed | status `failed`, `ErrorMessage` poblado |
| TestDeploy_FalloHealthcheck_ContainerDetenido | status `failed`, container detenido vía stub |
| TestDeploy_FalloProxy_NoAfectaResultado | status `running` aunque `proxy.Sync` falle |

Los tests de happy path y proxy caído levantan un `httptest.Server` real para que `WaitHealthy`
pueda completarse sin timeout; los demás usan port 1 (nadie escucha) con timeout de 500ms.

**Resultado final:** 9/9 PASS en `./internal/deploy/...` (4 healthcheck + 5 orchestration),
`go build ./...` limpio.

### Ajustes respecto al plan
- Se introdujo la interfaz interna `dockerRunner` para desacoplar el Orchestrator del tipo
  concreto `*docker.DockerClient` en tests — sin esto los tests unitarios habrían requerido
  Docker daemon real, contradiciendo el objetivo de ser unitarios.
- `NewOrchestratorWithRunner` y `WithHealthTimeout` se agregaron como superficie de test
  explícita; `NewOrchestrator` mantiene la firma pública original.

---

---

## Fase 6 — API REST ✅

**Planificado:** servidor HTTP con 7 endpoints REST, router chi/v5, `cmd/orchestrator/main.go`
que reemplaza el stub con el arranque real del sistema.

**Implementado:**

### Dependencia agregada
- `github.com/go-chi/chi/v5 v5.3.0` — router HTTP con soporte de parámetros de URL y middleware.

### `internal/api/server.go` — `Server`

```go
type Server struct {
    store        store.Store
    orchestrator *deploy.Orchestrator
    docker       *docker.DockerClient
    proxy        proxy.ProxyManager
    router       *chi.Mux
}

func NewServer(s store.Store, o *deploy.Orchestrator, d *docker.DockerClient, p proxy.ProxyManager) *Server
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request)
```

`NewServer` inicializa el router y registra los 7 endpoints con `middleware.Logger` y
`middleware.Recoverer`. `ServeHTTP` delega directamente al chi router.

### `internal/api/handlers.go` — los 7 handlers

Todos responden en JSON (`Content-Type: application/json`). Los errores siguen el formato
`{"error": "mensaje"}` con el status HTTP apropiado.

| Handler | Método + Path | Descripción |
|---|---|---|
| `crearApp` | `POST /apps` | Valida `name` y `repo_url` no vacíos; `health_path` default `/`. 409 en nombre duplicado (detecta código Postgres `23505`). Retorna 201. |
| `listarApps` | `GET /apps` | Lista todas las apps. Retorna array vacío `[]` si no hay ninguna. |
| `obtenerApp` | `GET /apps/{name}` | App + campo `active_deployment` (objeto o `null`). |
| `eliminarApp` | `DELETE /apps/{name}` | Detiene el container activo, marca el deployment como `stopped`, elimina la app, sincroniza el proxy. Retorna 204. |
| `crearDeployment` | `POST /apps/{name}/deployments` | Llama `Orchestrator.Deploy` con timeout de 10 minutos. Si el deploy termina en `failed`, retorna 200 con el deployment (el cliente lee `error_message`). Solo 500 si no se pudo iniciar el proceso. |
| `listarDeployments` | `GET /apps/{name}/deployments` | Histórico completo de deployments de la app. |
| `obtenerDeployment` | `GET /apps/{name}/deployments/{id}` | Deployment puntual por UUID. Valida el formato del ID con `pgtype.UUID.Scan`. |

**Tipos de respuesta:**
- `appResponse` — campos JSON limpios: UUID como string `xxxxxxxx-xxxx-...`, timestamps en RFC3339.
- `deploymentResponse` — campos nullable (`container_id`, `internal_port`, `finished_at`,
  `error_message`) serializados como punteros Go: `null` en JSON si no están presentes.
- `appConDeployment` — embebe `appResponse` + campo `active_deployment *deploymentResponse`.

**Helper `sincronizarProxy`** en el `Server`: reutiliza la misma lógica que el Orchestrator
(listar apps, obtener deployment activo por cada una, construir `[]proxy.Route`) para
sincronizar Traefik al eliminar una app.

### `cmd/orchestrator/main.go` — arranque real

Lee configuración de variables de entorno con defaults para desarrollo local:

| Variable | Default |
|---|---|
| `DATABASE_URL` | `postgres://postgres:postgres@localhost:5432/mini_paas?sslmode=disable` |
| `TRAEFIK_CONFIG_PATH` | `/tmp/traefik-dynamic.yml` |
| `LISTEN_ADDR` | `:8080` |

Secuencia de arranque: `NewPostgresStore` → `NewDockerClient` → `NewTraefikFileProxyManager`
→ `NewDockerfileBuilder` → `NewOrchestrator` → `NewServer` → `http.ListenAndServe`.

### Ajuste respecto al plan
- Se agregó `Client() *client.Client` a `DockerClient` para exponer el cliente subyacente
  del SDK. Es necesario porque `NewDockerfileBuilder` recibe un `*client.Client` directamente
  (no un `*DockerClient`), y el main necesita pasar el mismo cliente a ambos componentes.

### Verificación

```
go build ./cmd/orchestrator  ✅
go build ./...               ✅
```

Smoke test con Postgres real (`docker compose up -d postgres`):

```
GET  /apps        → 200  []
POST /apps        → 201  {"id":"...","name":"test","repo_url":"https://github.com/x/y","health_path":"/","created_at":"..."}
```

---

---

## Fase 7 — CLI deployctl ✅

**Planificado:** CLI `deployctl` con subcomandos cobra, output tabular con color, flag `--json`
para scripting, tests invocando el binario compilado contra un `httptest.Server`.

**Implementado:**

### Dependencias agregadas
- `github.com/spf13/cobra v1.10.2` — árbol de subcomandos
- `github.com/fatih/color v1.19.0` — output con color ANSI
- `github.com/olekukonko/tablewriter v0.0.5` — tablas formateadas

> Se fijó `tablewriter` en v0.0.5 en lugar de la última (v1.1.4): la v1.x tiene una API
> completamente incompatible (builder pattern) que rompería el código. La v0.0.5 tiene la
> API clásica y estable.

### Estructura de archivos (`cmd/deployctl/`)

| Archivo | Responsabilidad |
|---|---|
| `main.go` | Punto de entrada; construye el árbol cobra y llama `Execute()` |
| `client.go` | `apiClient`: HTTP client con `hacer()`, detección de `{"error":"..."}`, dos constructores (con/sin timeout) |
| `output.go` | Tablas, detalle, colores por status, `imprimirError` (stderr + exit 1), `--json` |
| `cmd_apps.go` | Subcomandos `apps create/list/get/delete/deploy` |
| `cmd_deployments.go` | Subcomandos `deployments list/get` |
| `deployctl_test.go` | 11 tests de integración vía `os/exec` |

### Comandos implementados

| Comando | Descripción |
|---|---|
| `apps create <name> --repo <url> [--health-path /]` | 201 → confirma con ID corto |
| `apps list [--json]` | Tabla: Name, Repo URL, Created At |
| `apps get <name> [--json]` | Detalle + `active_deployment` o mensaje vacío |
| `apps delete <name>` | 204 → confirma eliminación |
| `apps deploy <name>` | Sin timeout de cliente; spinner de espera; exit 1 si `status=failed` |
| `deployments list <app-name> [--json]` | Tabla: ID (8 chars), Status, Image Tag, Created At |
| `deployments get <app-name> <id> [--json]` | Detalle completo del deployment |

**Comportamiento de `apps deploy`:**
- Muestra mensaje de progreso mientras espera la respuesta HTTP síncrona (hasta 10 min).
- Status `running` → mensaje de éxito, exit 0.
- Status `failed` → `error_message` en stderr, exit 1.
- App inexistente (404) → error en stderr, exit 1.

**`--json` global en comandos de lectura:** serializa la respuesta del servidor como JSON
indentado directamente en stdout. El output tabular usa colores ANSI desactivables con
`NO_COLOR=1` (respetado automáticamente por `fatih/color`).

**Base URL:** `DEPLOYCTL_API_URL` (default: `http://localhost:8080`).

### Tests (`deployctl_test.go`) — 11 tests

`TestMain` compila el binario una sola vez en un directorio temporal antes de ejecutar
los tests. Cada test levanta su propio `httptest.Server` que simula una respuesta concreta.

| Test | Qué verifica |
|---|---|
| `TestAppsCreate` | stdout contiene el nombre, exit 0 |
| `TestAppsList` | ambas apps aparecen en la tabla |
| `TestAppsListJSON` | output es JSON válido con `--json` |
| `TestAppsGet` | stdout contiene el nombre de la app |
| `TestAppsDelete` | stdout menciona el nombre, exit 0 |
| `TestAppsDeployExitoso` | stdout contiene `running`, exit 0 |
| `TestAppsDeployFallido` | stderr contiene `error_message`, exit 1 |
| `TestAppsDeployAppNoExiste` | stderr contiene mensaje de error, exit 1 |
| `TestDeploymentsList` | ID corto aparece en la tabla |
| `TestDeploymentsGet` | UUID completo aparece en el detalle |
| `TestErrorServidor` | error del servidor llega a stderr, exit 1 |

**Resultado final:** `go build -o bin/deployctl ./cmd/deployctl` ✅ · `go build ./...` ✅ · **11/11 PASS**

---

## Próximas fases

| Fase | Descripción | Estado |
|---|---|---|
| Fase 8 | Integración end-to-end (docker compose up, test e2e) | Pendiente |
| Fase 9 | Documentación pública (README, limpieza de docs/) | Pendiente |
