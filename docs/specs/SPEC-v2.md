# SPEC v2 (semilla) — Zero-downtime y mejor DX

> **Este documento es una semilla, no un spec accionable todavía.** Contiene el alcance y las
> decisiones de alto nivel discutidas al diseñar v1, pensando hacia adelante. **No** se baja a
> nivel de tarea-por-tarea ni de endpoint-por-endpoint.
>
> Cuando llegue el momento de implementar v2: completar este documento revisando el código
> real de v1 (no solo `SPEC-v1.md`), `../ARCHITECTURE.md`, y los ADRs relevantes — siguiendo el
> mismo proceso de discusión y preguntas usado para llegar a `SPEC-v1.md`, no completándolo de
> una sola pasada sin chequear decisiones con el autor del proyecto.

## Alcance previsto

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
  que un deploy nuevo." Esto sugiere que rollback podría no ser un camino de código
  completamente distinto, sino una variante del flujo de deploy que se salta el paso de Build.
  A confirmar/diseñar en detalle cuando se complete este spec.
- **`Builder`** ya es una interfaz, no una función concreta. Agregar `BuildpackBuilder` como
  segunda implementación es el caso de uso para el que la interfaz fue diseñada — el punto de
  decisión pendiente es cómo el sistema elige cuál `Builder` usar por app (¿autodetección?
  ¿flag explícito al crear la app?).

## Preguntas abiertas a resolver antes de detallar el spec completo

Estas son preguntas que **no** tienen sentido responder ahora (dependen de cómo termine viéndose
v1 en la práctica), pero que hay que abordar explícitamente al completar este documento:

- El streaming de logs: ¿WebSocket, Server-Sent Events, o polling simple a un endpoint que
  devuelve logs incrementales? Cada uno tiene tradeoffs distintos de complejidad en el CLI.
- El estado intermedio durante zero-downtime (dos containers de la misma app corriendo a la
  vez) — ¿se modela como dos filas `running` simultáneas en `deployments`, o se necesita un
  estado nuevo tipo `switching`?
- Rollback: ¿se permite rollback a cualquier deployment del histórico, o solo a los últimos N?
  ¿Qué pasa si la imagen de ese deployment viejo ya no existe localmente (fue limpiada por
  `docker system prune`, por ejemplo)?
- Buildpacks: ¿vale la pena una detección propia simple, o directamente integrar algo como
  Cloud Native Buildpacks (`pack` CLI) para no reinventar un detector? Esto es una decisión de
  alcance vs. autenticidad de aprendizaje a discutir en su momento.

## Limitaciones que v2 todavía no resuelve (quedan para v3)

- Sin límites de recursos por container.
- Sin auto-deploy en push (sigue siendo manual vía CLI).
- Logs solo en vivo, no persistidos/buscables después de que el container termina.
