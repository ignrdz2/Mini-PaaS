# SPEC v1 — Mini-PaaS: Fundación

> Este documento es la fuente de verdad para la implementación de v1. Ver `../ARCHITECTURE.md`
> para el contexto global del sistema y las interfaces que deben mantenerse estables hacia
> v2/v3, y `../decisions/` para el razonamiento detrás de las decisiones clave.

## Alcance de v1 (y qué NO incluye)

**Incluye:**
- Deploy de una app a partir de un repo Git público con `Dockerfile` en la raíz.
- Build síncrono de la imagen Docker.
- Healthcheck real post-arranque antes de marcar el deployment como exitoso.
- Routing HTTP por path (`localhost/<app-name>`) vía Traefik, configurado dinámicamente por el
  orquestador.
- Histórico de deployments persistido en PostgreSQL.
- CLI (`deployctl`) que habla con el orquestador vía API REST.
- Todo el entorno (orquestador, Postgres, Traefik) levantado con `docker compose up`.

**Explícitamente fuera de alcance (ver versiones futuras):**
- Zero-downtime deploys (v2).
- Rollback (v2) — aunque el histórico que lo habilita ya se persiste desde v1.
- Builds asíncronos / streaming de logs en vivo (v2).
- Detección de buildpacks sin Dockerfile (v2).
- Límites de recursos por container, multi-tenancy real, webhooks de GitHub (v3).

## Stack técnico

| Componente | Tecnología |
|---|---|
| Orquestador | Go 1.22+, librería HTTP estándar o `chi` para routing |
| CLI | Go, `cobra` para subcomandos |
| Acceso a Docker | `github.com/docker/docker/client` (SDK oficial) contra socket Unix local |
| Persistencia | PostgreSQL 16, `sqlc` o `database/sql` + `pgx` (a decidir en Fase 0) |
| Migraciones | `golang-migrate` |
| Proxy | Traefik v3, file provider (ver ADR 002) |
| Orquestación local | Docker Compose |

## Estructura de carpetas

```
mini-paas/
├── docker-compose.yml
├── go.mod
├── cmd/
│   ├── orchestrator/
│   │   └── main.go
│   └── deployctl/
│       └── main.go
├── internal/
│   ├── builder/
│   │   ├── builder.go          # interfaz Builder
│   │   └── dockerfile.go       # implementación DockerfileBuilder
│   ├── proxy/
│   │   ├── proxy.go            # interfaz ProxyManager
│   │   └── traefik_file.go     # implementación TraefikFileProxyManager
│   ├── store/
│   │   ├── store.go            # interfaz Store
│   │   ├── postgres.go         # implementación sobre Postgres
│   │   └── models.go           # App, Deployment
│   ├── docker/
│   │   └── client.go           # wrapper sobre el SDK de Docker
│   ├── api/
│   │   ├── server.go
│   │   └── handlers.go
│   └── deploy/
│       └── orchestration.go    # coordina Builder + docker run + healthcheck + ProxyManager
├── migrations/
│   ├── 0001_init.up.sql
│   └── 0001_init.down.sql
└── docs/
    ├── ARCHITECTURE.md
    ├── specs/
    └── decisions/
```

## Modelo de datos (DDL)

```sql
CREATE TABLE apps (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name        text NOT NULL UNIQUE,
    repo_url    text NOT NULL,
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
```

Nota: `status` se modela como `text` con validación a nivel aplicación, no como `enum` de
Postgres, para no pagar el costo de una migración cada vez que v2/v3 agreguen estados nuevos.

## Flujo end-to-end de un deploy

```
1. deployctl apps create mi-app --repo https://github.com/user/repo
   → POST /apps  { name, repo_url }
   → orquestador inserta fila en `apps`, devuelve 201

2. deployctl apps deploy mi-app
   → POST /apps/mi-app/deployments
   → orquestador:
     a. inserta deployment con status=pending
     b. clona el repo a un directorio temporal (git clone --depth 1)
     c. status=building; Builder.Build() → docker build, tag = <app-name>:<short-sha-o-timestamp>
        - si falla: status=failed, error_message=<stderr del build>, responde 4xx/5xx al cliente
     d. corre el container vía SDK de Docker, mapeando el puerto interno de la app a un puerto
        host elegido dinámicamente (libre en ese momento)
     e. status=healthcheck; hace polling HTTP al container (ver sección Healthcheck) con timeout
        - si nunca pasa: status=failed, se detiene y elimina el container, error_message=<detalle>
     f. status=running; container_id e internal_port quedan guardados
     g. ProxyManager.Sync() reescribe la config de Traefik con la ruta nueva
        (incluye las rutas de TODAS las apps con deployment running, no solo la nueva)
     h. si había un deployment running anterior de esta misma app: se detiene y se marca
        status=stopped (este es el único punto de "downtime" de v1 — documentado como
        limitación conocida, resuelto en v2)
   → responde 200 con el deployment final (incluye status, url de acceso)

3. Usuario accede a http://localhost/mi-app
   → Traefik matchea PathPrefix(/mi-app), aplica StripPrefix, reenvía al container
```

## Healthcheck (v1)

- Configurable por app vía un campo opcional al crear la app (`health_path`, default `/`).
- El orquestador hace polling HTTP GET a `http://localhost:<internal_port><health_path>`
  cada 500ms, con un timeout total configurable (default 30s).
- Considera "sano" cualquier respuesta con status code `< 500` (no se exige `200` exacto, para
  no ser demasiado estricto con apps de prueba que puedan redirigir).
- Si el timeout se agota sin una respuesta sana: el deployment falla, el container se detiene y
  elimina, y el error se persiste en `error_message`.

## API REST (contrato v1)

| Método | Path | Descripción |
|---|---|---|
| `POST` | `/apps` | Crea una app. Body: `{name, repo_url, health_path?}` |
| `GET` | `/apps` | Lista todas las apps |
| `GET` | `/apps/{name}` | Detalle de una app + su deployment activo (si existe) |
| `DELETE` | `/apps/{name}` | Elimina la app y detiene su deployment activo |
| `POST` | `/apps/{name}/deployments` | Dispara un nuevo deploy (síncrono — la respuesta llega cuando termina) |
| `GET` | `/apps/{name}/deployments` | Lista histórico de deployments de la app |
| `GET` | `/apps/{name}/deployments/{id}` | Detalle de un deployment puntual |

Todas las respuestas en JSON. Errores con un body consistente: `{"error": "mensaje"}` y el
status code HTTP apropiado (`400` validación, `404` no encontrado, `409` conflicto —ej. nombre
duplicado—, `500` error interno/de build/de runtime).

## CLI (`deployctl`) — comandos v1

```
deployctl apps create <name> --repo <url> [--health-path /healthz]
deployctl apps list
deployctl apps get <name>
deployctl apps delete <name>
deployctl apps deploy <name>
deployctl deployments list <app-name>
deployctl deployments get <app-name> <deployment-id>
```

- Output humano por default (tabla formateada, colores vía `fatih/color` o similar).
- Flag global `--json` en todos los comandos de lectura, para output parseable.
- El comando `deploy` muestra un spinner/progreso mientras espera la respuesta síncrona del
  orquestador (la llamada HTTP puede tardar decenas de segundos mientras se buildea la imagen).
- Configuración de a qué orquestador conectarse vía variable de entorno
  (`DEPLOYCTL_API_URL`, default `http://localhost:8080`) — sin archivo de config todavía (eso
  es más propio del proyecto de CLI tool separado; acá se mantiene simple).

## Traefik — configuración dinámica generada

Por cada deployment con `status=running`, el `TraefikFileProxyManager` genera una entrada en el
archivo de configuración dinámica (formato YAML, providers.file):

```yaml
http:
  routers:
    mi-app:
      rule: "PathPrefix(`/mi-app`)"
      service: mi-app
      middlewares:
        - mi-app-stripprefix
  middlewares:
    mi-app-stripprefix:
      stripPrefix:
        prefixes:
          - "/mi-app"
  services:
    mi-app:
      loadBalancer:
        servers:
          - url: "http://localhost:<internal_port>"
```

El archivo se regenera **completo** (no se hace edición incremental) a partir del estado actual
en Postgres, cada vez que cambia el conjunto de deployments activos. Se escribe a un archivo
temporal en el mismo directorio y se hace `os.Rename` para que la escritura sea atómica desde
la perspectiva de Traefik (que está watcheando el archivo final).

## docker-compose.yml (servicios del propio sistema)

```yaml
services:
  postgres:
    image: postgres:16
    environment:
      POSTGRES_DB: minipaas
      POSTGRES_USER: minipaas
      POSTGRES_PASSWORD: minipaas
    ports: ["5432:5432"]
    volumes: ["pgdata:/var/lib/postgresql/data"]

  traefik:
    image: traefik:v3.0
    command:
      - "--providers.file.directory=/etc/traefik/dynamic"
      - "--providers.file.watch=true"
      - "--entrypoints.web.address=:80"
    ports: ["80:80"]
    volumes:
      - "./traefik/dynamic:/etc/traefik/dynamic"

  orchestrator:
    build: .
    environment:
      DATABASE_URL: postgres://minipaas:minipaas@postgres:5432/minipaas
      TRAEFIK_CONFIG_PATH: /etc/traefik/dynamic/dynamic.yml
      DOCKER_HOST: unix:///var/run/docker.sock
    volumes:
      - "/var/run/docker.sock:/var/run/docker.sock"
      - "./traefik/dynamic:/etc/traefik/dynamic"
    ports: ["8080:8080"]
    depends_on: ["postgres", "traefik"]

volumes:
  pgdata:
```

Nota: el orquestador corriendo dentro de un container necesita el socket de Docker montado como
volumen para hablar con el daemon del host — sigue siendo "local" en el sentido de ADR 001 (no
es un daemon remoto), solo que accedido desde dentro de un container.

## Manejo de errores — casos a cubrir explícitamente

- Repo no clonable (URL inválida, repo privado sin credenciales, repo no existe).
- Repo sin `Dockerfile` en la raíz.
- `docker build` falla (Dockerfile inválido, dependencia no resuelve, etc.) — capturar y
  persistir stderr relevante en `error_message`, truncado a un tamaño razonable (ej. últimas
  4000 caracteres).
- Puerto interno: si por alguna razón no se encuentra un puerto libre, fallar explícitamente
  (no debería pasar en desarrollo local, pero el caso debe manejarse, no asumirse).
- Healthcheck timeout (ver sección dedicada arriba).
- Nombre de app duplicado → `409 Conflict`.
- Deploy a una app que no existe → `404 Not Found`.

## Testing

- **Unitarios**: lógica de `Builder` (parsing de resultado de build), `ProxyManager`
  (generación correcta del YAML a partir de un slice de rutas — esto no requiere Traefik real
  corriendo, solo verificar el YAML generado), validaciones de la capa API.
- **Integración**: contra un Docker daemon real (no mockeado) y una Postgres real (vía
  Docker Compose en el entorno de test o testcontainers-go) — al menos un test end-to-end que
  cree una app de prueba mínima (un repo con un `Dockerfile` trivial tipo `python:3.12-slim`
  sirviendo un healthcheck simple), la deploye, verifique que responde por Traefik, y la borre.
- **CLI**: tests que invocan el binario compilado vía `os/exec`, verificando stdout/stderr/exit
  codes — igual filosofía que el proyecto de CLI tool separado del portfolio.

## Fases de implementación

> Cada fase es una unidad de trabajo razonable para un prompt a Claude Code. Un commit por
> tarea, sin saltar fases, siguiendo el flujo de trabajo habitual.

### Fase 0 — Setup
- [ ] Inicializar módulo Go, estructura de carpetas.
- [ ] `docker-compose.yml` con Postgres y Traefik (sin el orquestador todavía).
- [ ] Migración inicial (`0001_init.sql`) con el esquema de `apps` y `deployments`.
- [ ] Decisión final de librería de acceso a Postgres (`pgx` directo vs `sqlc`) — documentar
      como ADR si difiere de lo sugerido.

### Fase 1 — Store
- [ ] Interfaz `Store` + implementación Postgres: CRUD de `apps`, creación y actualización de
      `deployments`.
- [ ] Tests unitarios del Store contra una Postgres real (Docker Compose o testcontainers).

### Fase 2 — Builder
- [ ] Interfaz `Builder`.
- [ ] `DockerfileBuilder`: clona repo (`git clone --depth 1`), valida presencia de `Dockerfile`,
      ejecuta build vía SDK de Docker, captura logs/errores.
- [ ] Tests con un repo de prueba real (puede ser un repo público mínimo creado para este fin).

### Fase 3 — Runtime + Healthcheck
- [ ] Wrapper sobre el SDK de Docker para correr/detener containers, asignar puerto libre.
- [ ] Lógica de healthcheck (polling HTTP con timeout).

### Fase 4 — ProxyManager
- [ ] Interfaz `ProxyManager`.
- [ ] `TraefikFileProxyManager`: generación del YAML completo a partir de rutas activas,
      escritura atómica.
- [ ] Tests unitarios de generación de YAML (sin necesitar Traefik corriendo).

### Fase 5 — Orquestación
- [ ] `internal/deploy/orchestration.go`: coordina Builder → runtime → healthcheck →
      ProxyManager → actualización de estado en Store, siguiendo el flujo end-to-end descrito
      arriba, incluyendo el manejo de errores en cada paso.

### Fase 6 — API REST
- [ ] Handlers para los 7 endpoints listados en la sección API REST.
- [ ] Validación de inputs, manejo de errores con el formato de respuesta consistente.

### Fase 7 — CLI
- [ ] `deployctl` con los comandos listados, cliente HTTP del orquestador, output humano +
      `--json`.

### Fase 8 — Integración end-to-end
- [ ] Levantar todo con `docker compose up`, deployar una app de prueba real, verificar acceso
      vía Traefik, documentar el resultado (capturas o asciinema, para usar en el README del
      proyecto).
- [ ] Test de integración automatizado que cubra este mismo flujo.

### Fase 9 — Documentación pública del repo
- [ ] README con instrucciones de uso, demo, limitaciones conocidas.
- [ ] Limpieza de `docs/` para repo público: este `SPEC-v1.md` y los ADRs se mantienen (son
      contenido de diseño legítimo para mostrar), se elimina cualquier referencia a Claude Code
      o checkboxes de progreso, consistente con la práctica ya usada en otros proyectos del
      portfolio.
