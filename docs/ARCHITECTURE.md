# Arquitectura — Mini-PaaS

## Qué es esto

Una plataforma de deploy minimalista, inspirada en Heroku/Railway/Render, implementada desde
cero para entender (no solo usar) los mecanismos que hacen posible `git push` → aplicación
corriendo en producción.

El sistema toma un repositorio Git con un `Dockerfile`, construye una imagen, la corre como
container, y rutea tráfico hacia ella mediante un reverse proxy configurado dinámicamente.

Este documento describe la arquitectura que **persiste a través de las tres versiones**
planeadas del proyecto. El detalle de tareas e implementación de cada versión vive en
`specs/SPEC-v{N}.md`. Las decisiones puntuales con sus alternativas consideradas viven en
`decisions/`.

## Por qué existe este proyecto

El resto del portfolio (Text-to-SQL Dashboard, RAG Pipeline, Precios-UY) demuestra
construcción de aplicaciones que *corren sobre* infraestructura ya existente (Docker Compose
local, servicios cloud gestionados). Este proyecto invierte el ángulo: construye una pieza de
la infraestructura misma. El objetivo es demostrar comprensión de qué pasa "un nivel por
debajo" de `docker compose up` — cómo se construye una plataforma, no solo cómo se la consume.

## Componentes del sistema

```
                    ┌─────────────┐
   usuario  ──────▶ │  CLI        │
                    │ (deployctl) │
                    └──────┬──────┘
                           │ HTTP (API REST)
                           ▼
                    ┌─────────────────────┐
                    │   Orquestador        │
                    │   (Go, API REST)     │
                    │                      │
                    │  - Builder           │◀── interfaz estable
                    │  - ProxyManager      │◀── interfaz estable
                    │  - Store (Postgres)  │
                    └──────┬──────┬────────┘
                           │      │
              docker.sock  │      │ escribe config
                           ▼      ▼
                    ┌──────────┐ ┌──────────┐
                    │  Docker  │ │ Traefik  │◀── tráfico externo
                    │  daemon  │ │ (proxy)  │
                    └────┬─────┘ └────┬─────┘
                         │            │
                         ▼            ▼
                  containers de apps deployadas
```

- **CLI (`deployctl`)**: binario standalone en Go. Cliente puro de la API REST del orquestador;
  no contiene lógica de negocio. Mismo lenguaje y librerías (`cobra`/`viper` o equivalente) que
  el proyecto de CLI tool "serio" planeado por separado — son proyectos distintos del
  portfolio, pero con afinidad de stack intencional.
- **Orquestador**: servicio Go que expone una API REST. Es el cerebro del sistema: recibe
  comandos del CLI, coordina al Builder, al ProxyManager y al Store, y mantiene el estado de
  verdad de qué apps y deployments existen.
- **Builder**: interfaz que abstrae "cómo convertir un repo en una imagen corriendo". v1 tiene
  una sola implementación (`DockerfileBuilder`, ver ADR 003).
- **ProxyManager**: interfaz que abstrae "cómo informarle al proxy las reglas de routing
  vigentes". v1 implementa `TraefikFileProxyManager` (ver ADR 002).
- **Store**: capa de persistencia sobre PostgreSQL. Guarda apps, deployments (con histórico
  desde v1) y su estado.
- **Docker daemon**: el daemon de Docker de la propia máquina, accedido vía socket Unix local
  (`/var/run/docker.sock`) usando el SDK oficial de Go (no se invoca el CLI de `docker` como
  subprocess).
- **Traefik**: reverse proxy real, gratuito y open source, corriendo como container. Recibe
  tráfico externo y lo rutea según el archivo de configuración dinámica que el orquestador
  mantiene (ver ADR 002).

## Interfaces estables (contrato entre versiones)

Estas dos interfaces son el mecanismo principal por el cual v2 y v3 extienden el sistema sin
reescribir v1. Se definen en v1 con una sola implementación cada una, pero su forma está
pensada para no romperse al agregar implementaciones nuevas.

### `Builder`

Responsabilidad: dado el código fuente de un repo ya clonado, producir una imagen Docker
identificada por un tag.

```go
type Builder interface {
    Build(ctx context.Context, sourcePath string, imageTag string) (BuildResult, error)
}
```

- v1: `DockerfileBuilder` — requiere un `Dockerfile` en la raíz del repo, falla si no existe.
- v2/v3 (especulativo, sujeto a revisión cuando se llegue): podría agregarse un
  `BuildpackBuilder` que detecte el tipo de proyecto (`package.json`, `requirements.txt`, etc.)
  y genere un Dockerfile on-the-fly. El resto del sistema no necesita saber cuál `Builder` se
  usó — solo le importa el resultado.

### `ProxyManager`

Responsabilidad: dado el estado actual de qué apps están activas y a qué container/puerto
apuntan, asegurar que el proxy rutee tráfico correctamente.

```go
type ProxyManager interface {
    Sync(ctx context.Context, routes []Route) error
}
```

- v1: `TraefikFileProxyManager` — regenera el archivo de configuración dinámica de Traefik
  completo a partir del slice de `Route` recibido, y lo escribe atómicamente (ver ADR 002).
- v2: el mismo `Sync` se reutiliza para el switch atómico de zero-downtime — el cambio de
  tráfico del container viejo al nuevo es, en esencia, llamar `Sync` con una `Route` actualizada.
- v3 (especulativo): si se explora soporte multi-servidor, esta interfaz es el punto de
  extensión natural — una implementación distinta podría hablar con un proxy distribuido en vez
  de escribir un archivo local.

## Modelo de datos (núcleo, estable desde v1)

```
apps
  id            uuid PK
  name          text unique        -- usado en el path de routing: /<name>
  repo_url      text
  created_at    timestamptz

deployments
  id            uuid PK
  app_id        uuid FK -> apps.id
  image_tag     text               -- tag de la imagen Docker construida
  status        text               -- pending | building | healthcheck | running | failed | stopped
  container_id  text nullable      -- id del container Docker en runtime
  internal_port int nullable       -- puerto interno asignado al container
  created_at    timestamptz
  finished_at   timestamptz nullable
  error_message text nullable
```

`deployments` guarda histórico completo desde v1 (no solo el deployment activo), aunque la
funcionalidad de rollback que *usa* ese histórico recién llega en v2 (ver ADR 003). En v1, la
"app corriendo" se determina como el `deployment` más reciente de esa app con `status = running`.

## Qué entra en cada versión (alto nivel)

El detalle accionable de cada versión vive en su propio spec. Esto es solo el resumen de
alcance para tener el mapa completo a la vista.

### v1 — Fundación (single server, síncrono)
- Deploy de apps con `Dockerfile`, build síncrono, un solo servidor.
- Healthcheck real antes de marcar el deployment como `running`.
- Routing por path vía Traefik file provider.
- Histórico de deployments persistido, sin rollback todavía.

### v2 — Zero-downtime y mejor DX
- Builds asíncronos con streaming de logs (CLI con `--follow`).
- Deploy zero-downtime: nuevo container arriba, healthcheck pasa, switch atómico de tráfico,
  recién ahí se mata el container viejo.
- Rollback a un deployment anterior usando el histórico ya existente desde v1.
- Detección básica de buildpacks como alternativa a requerir `Dockerfile`.

### v3 — Multi-tenancy y producción real
- Límites de recursos (CPU/memoria) por container.
- Logs persistentes y buscables (no solo streaming en vivo).
- Webhooks de GitHub: auto-deploy en push, sin intervención manual del CLI.
- Exploración (no necesariamente implementación completa) de soporte multi-servidor.

## Decisiones técnicas transversales

Resumen — el detalle y las alternativas consideradas de cada una están en su ADR correspondiente:

| Decisión | Elegido | ADR |
|---|---|---|
| Lenguaje de implementación | Go | [001](decisions/001-go-sobre-python.md) |
| Cómo configurar Traefik | File provider | [002](decisions/002-traefik-file-provider.md) |
| Estrategia de versionado | v1/v2/v3 sobre interfaces estables | [003](decisions/003-versionado-incremental.md) |
| Estrategia de routing | Paths con strip-prefix | [004](decisions/004-routing-por-path.md) |

Otras decisiones ya tomadas, sin ADR dedicado por ser de menor impacto/reversibilidad:

- **Persistencia**: PostgreSQL (consistencia con el resto del portfolio, paridad
  desarrollo/producción).
- **Acceso a Docker**: SDK oficial de Go (`docker/docker/client`) contra el socket Unix local,
  nunca invocando el binario `docker` vía subprocess.
- **Topología de despliegue del propio sistema**: el orquestador, Postgres y Traefik corren
  como servicios de un mismo `docker-compose.yml`, consistente con el resto del portfolio.
- **Cliente/servidor real**: CLI y orquestador son binarios separados que se comunican por HTTP,
  no un único binario monolítico — esto deja la puerta abierta a usar el CLI desde una máquina
  distinta a donde corre el orquestador, sin cambios de arquitectura.

## Costo: completamente gratis

Todo el desarrollo y la demo de v1 corren localmente sin ningún costo: Go, Docker, Traefik y
PostgreSQL son gratuitos y open source. Los "repos a deployar" de prueba pueden ser los propios
proyectos del portfolio (Precios-UY, RAG Pipeline). Si en v2/v3 se decide exponer una demo
pública persistente, existen tiers gratuitos viables (Oracle Cloud free tier para una VM ARM
permanente) — pero esto es una decisión a tomar en su momento, no un requisito de v1.

## Limitaciones conocidas de v1 (documentadas a propósito)

Mostrar explícitamente qué le falta al sistema para ser "producción real" es tan parte del
proyecto como lo que sí hace:

- Sin alta disponibilidad: un solo servidor, si el orquestador cae, no hay failover.
- Sin TLS: HTTP plano en v1 (Traefik soporta Let's Encrypt nativo, candidato natural para
  v2/v3 si se expone públicamente).
- Sin aislamiento de recursos entre containers (cualquier app puede consumir todo el CPU/RAM
  disponible) — llega en v3.
- Downtime breve en cada deploy (se detiene el container viejo antes de levantar el nuevo) —
  resuelto en v2.
- Solo soporta apps con `Dockerfile` explícito, sin detección de buildpacks — v2.
- Routing por path tiene la limitación de assets con rutas absolutas (ver ADR 004).
