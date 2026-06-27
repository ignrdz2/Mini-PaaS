# SPEC v2 — Zero-downtime y mejor DX

## Alcance

- **Builds asíncronos con streaming de logs**: el deploy ya no bloquea al CLI hasta terminar.
  El CLI puede hacer `deployctl apps deploy mi-app --follow` y ver logs en tiempo real del
  build y del arranque del container.
- **Zero-downtime deploys**: el deploy nuevo se buildea y arranca como container adicional
  (la app vieja sigue sirviendo tráfico), pasa su healthcheck, y solo ahí se switchea el
  tráfico atómicamente — recién después se detiene el container viejo.
- **Rollback**: poder volver a cualquier deployment anterior del histórico (que ya se persiste
  desde v1) con un comando explícito.
- **Detección de buildpacks**: para repos sin `Dockerfile`, detectar el tipo de proyecto
  (`package.json` → Node, `requirements.txt`/`pyproject.toml` → Python, etc.) y generar un
  Dockerfile razonable on-the-fly.

## Cómo se apoya en las interfaces de v1

- **`ProxyManager.Sync`** (ver `../ARCHITECTURE.md`) ya está diseñada para recibir el conjunto
  completo de rutas activas y regenerar la config. El switch de zero-downtime es, en esencia,
  llamar `Sync` con la ruta del container nuevo en lugar de la del viejo — no debería requerir
  cambiar la firma de la interfaz, solo el momento y el orden en que se la invoca dentro de la
  orquestación del deploy.
- **Tabla `deployments`** ya tiene histórico completo con `status`. Rollback es,
  conceptualmente, "tomar un deployment anterior con `status` apto, volver a correr su
  `image_tag` ya existente (sin rebuildear), pasar healthcheck, y hacer el mismo switch atómico
  que un deploy nuevo."
- **`Builder`** ya es una interfaz, no una función concreta. Agregar `BuildpackBuilder` como
  segunda implementación es el caso de uso para el que la interfaz fue diseñada.

## Decisiones tomadas

### Streaming de logs: Server-Sent Events (SSE)

El endpoint `GET /apps/{name}/deployments/{id}/logs` emite eventos SSE.

SSE es unidireccional (servidor → cliente), funciona sobre HTTP plano sin upgrade de protocolo,
y es más simple de implementar en Go y de consumir en el CLI que WebSocket. El polling simple
fue descartado por la latencia innecesaria que introduce.

**Formato de eventos:**
- Tipo `log` con `{"message":"...","timestamp":"..."}` mientras el build está en curso.
- Tipo `done` con `{"status":"running|failed","deployment_id":"..."}` al terminar.

**Acceso histórico:** si el deployment ya terminó cuando el cliente se conecta, el endpoint
devuelve todos los logs almacenados en `deployment_logs` y cierra la conexión inmediatamente.

### Estado durante zero-downtime: reordenamiento, no estado nuevo

No se agrega ningún estado nuevo a la tabla `deployments`.

La clave es reordenar dos pasos en `orchestration.go`. En v1 el orden era:

1. Marcar nuevo deployment como `running`
2. Detener container viejo
3. Sincronizar proxy

En v2 el orden pasa a ser:

1. Marcar nuevo deployment como `running`
2. **Sincronizar proxy** (apunta al nuevo)
3. Detener container viejo

`GetActiveDeployment` usa `ORDER BY created_at DESC LIMIT 1`, por lo que ya retorna el
deployment más reciente. Al momento de sincronizar el proxy, el nuevo es el `running` más
reciente → Traefik apunta al nuevo → el viejo se detiene sin haber servido tráfico desde el
switch.

Hay un período brevísimo donde hay dos filas `running` para la misma app (entre marcar el
nuevo y detener el viejo). Es inofensivo: el proxy ya apunta al nuevo.

### Rollback: a cualquier deployment anterior con status=stopped

Se permite rollback a cualquier deployment del histórico cuyo `status` sea `stopped`.

**Imagen no encontrada localmente:** si la imagen Docker ya no existe (fue limpiada por
`docker system prune` u otro motivo), el rollback falla con error explícito — no se
re-buildea automáticamente.

**Flujo:** el rollback salta el paso de build y sigue la transición
`pending → healthcheck → running`.

**Trazabilidad:** el rollback crea un nuevo deployment con el `image_tag` del deployment
origen, marcando la procedencia con el campo `rolled_back_from` (UUID del deployment origen).
Los deployments normales tienen `rolled_back_from = NULL`.

### Buildpacks: detección automática propia, sin herramientas externas

Se agrega `BuildpackBuilder` implementando la interfaz `Builder`. No se integra Cloud Native
Buildpacks (`pack` CLI) ni ninguna herramienta externa; la detección propia es suficiente para
el alcance de aprendizaje del proyecto.

**Criterio de selección** (evaluado en orden de prioridad):

1. `Dockerfile` en la raíz → `DockerfileBuilder` (mismo que v1, siempre tiene prioridad).
2. `go.mod` → Dockerfile Go generado on-the-fly.
3. `package.json` → Dockerfile Node.js generado on-the-fly.
4. `requirements.txt` o `pyproject.toml` → Dockerfile Python generado on-the-fly.
5. Ninguno detectado → error explícito con la lista de archivos buscados.

No se agrega campo al schema de `apps`: la detección ocurre en cada build. Si el repo cambia
de tipo de proyecto entre deploys, el sistema se adapta automáticamente.

## Limitaciones que v2 todavía no resuelve (quedan para v3)

- Sin límites de recursos por container.
- Sin auto-deploy en push (sigue siendo manual vía CLI).
- Logs solo en vivo, no persistidos/buscables después de que el container termina.
