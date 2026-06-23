# Mini-PaaS

Una plataforma de deploy minimalista inspirada en Heroku/Railway, construida desde cero en Go.
Toma un repositorio Git con un `Dockerfile`, construye la imagen, corre el container, y rutea
tráfico hacia él mediante Traefik — todo gestionado con una CLI y una API REST propias.

---

## Demo

```bash
# crear una app
deployctl apps create mi-app --repo https://github.com/usuario/repo

# deployar (síncrono — espera a que el healthcheck pase)
deployctl apps deploy mi-app

# la app ya está disponible vía Traefik
curl http://localhost/mi-app/

# ver el histórico de deployments
deployctl deployments list mi-app

# eliminar la app y detener el container
deployctl apps delete mi-app
```

---

## Quick start

**Requisitos:**
- Go 1.25+
- Docker Desktop (con el daemon corriendo)
- Git

**1. Clonar el repo:**

```bash
git clone https://github.com/ignrdz2/mini-paas
cd mini-paas
```

**2. Levantar el stack (orquestador + Postgres + Traefik):**

```bash
docker compose up --build -d
```

El orquestador arranca en `:8080` y aplica las migraciones automáticamente.
Traefik escucha en `:80`.

**3. Compilar el CLI:**

```bash
go build -o bin/deployctl ./cmd/deployctl
```

Opcionalmente, mover `bin/deployctl` a algún directorio en el `PATH`.

**4. Hacer el primer deploy:**

El repositorio a deployar debe cumplir dos condiciones:
- Tener un `Dockerfile` en la raíz.
- La app debe leer la variable de entorno `PORT` y escuchar en ese puerto.

```bash
./bin/deployctl apps create mi-app --repo https://github.com/usuario/mi-repo
./bin/deployctl apps deploy mi-app
curl http://localhost/mi-app/
```

**Variable de entorno opcional:**

```bash
# si el orquestador no corre en localhost:8080
export DEPLOYCTL_API_URL=http://otro-host:8080
```

---

## Arquitectura

El sistema tiene cuatro componentes principales que se comunican en cadena:

```
usuario → deployctl (CLI) → orquestador (API REST) → Docker daemon
                                                    → Traefik (file provider)
                                                    → PostgreSQL
```

- **`deployctl`**: CLI standalone que habla con el orquestador vía HTTP. Sin lógica de negocio propia.
- **Orquestador**: servicio Go que coordina el build, el runtime y el proxy. Expone 7 endpoints REST.
- **Builder**: abstracción sobre `docker build`. v1 requiere `Dockerfile` explícito.
- **ProxyManager**: regenera la config dinámica de Traefik en cada deploy/delete.
- **Store**: persistencia en PostgreSQL con histórico completo de deployments desde v1.

Para el detalle de decisiones de diseño y las interfaces que se mantienen estables hacia v2/v3,
ver [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md).

---

## Limitaciones conocidas de v1

Documentadas a propósito — mostrar qué le falta para ser "producción real" es parte del proyecto:

- **Sin alta disponibilidad**: un solo servidor; si el orquestador cae no hay failover.
- **Sin TLS**: HTTP plano. Traefik soporta Let's Encrypt nativamente (candidato para v2/v3).
- **Downtime en cada deploy**: el container viejo se detiene antes de levantar el nuevo. Se resuelve en v2.
- **Solo apps con `Dockerfile`**: sin detección de buildpacks. Llega en v2.
- **Sin aislamiento de recursos**: cualquier app puede consumir todo el CPU/RAM disponible. Llega en v3.
- **Routing por path con limitación de assets**: las apps deben usar rutas relativas para assets estáticos (ver [ADR 004](docs/decisions/004-routing-por-path.md)).

---

## Roadmap

### v2 — Zero-downtime y mejor DX
- Builds asíncronos con streaming de logs (`deployctl apps deploy --follow`).
- Deploy zero-downtime: nuevo container up + healthcheck → switch atómico de tráfico → kill del viejo.
- Rollback a un deployment anterior usando el histórico ya persistido desde v1.

### v3 — Multi-tenancy y producción real
- Límites de CPU/memoria por container.
- Logs persistentes y buscables.
- Webhooks de GitHub para auto-deploy en push.

---

## Desarrollo

Ver [CONTRIBUTING.md](CONTRIBUTING.md) para instrucciones de desarrollo y tests.
