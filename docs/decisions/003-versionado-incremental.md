# ADR 003: Versionado incremental (v1 / v2 / v3) sobre interfaces estables

## Estado
Aceptado

## Contexto
El proyecto tiene alcance suficiente para ser una plataforma de deploy "real" (multi-tenant,
zero-downtime, multi-servidor), pero implementarlo todo de una sin un v1 funcionando es un
riesgo alto de sobre-ingeniería y de nunca terminar un entregable demostrable.

## Decisión
Dividir el proyecto en tres versiones incrementales, cada una un hito funcional completo y
demostrable por sí mismo:

- **v1**: single server, deploy síncrono, solo Dockerfile, sin zero-downtime.
- **v2**: zero-downtime deploys, builds asíncronos con logs en streaming, rollback.
- **v3**: multi-tenancy con límites de recursos, webhooks de GitHub, logs persistentes y
  buscables.

Para que esta progresión no requiera reescrituras, las siguientes abstracciones se diseñan
desde v1 como interfaces estables, aunque v1 solo tenga una implementación de cada una:

- **`Builder`**: abstrae "cómo se construye una imagen a partir de un repo". v1 implementa
  `DockerfileBuilder`. v2/v3 podrían agregar detección de buildpacks sin tocar el resto del
  sistema.
- **`ProxyManager`**: abstrae "cómo se le informa al proxy las reglas de routing actuales". v1
  implementa `TraefikFileProxyManager` (ver ADR 002). Encapsula el detalle de que es un archivo
  YAML — el resto del sistema solo conoce la interfaz.
- **Modelo de datos `Deployment`** con histórico desde v1 (ver `../specs/SPEC-v1.md`), aunque la
  funcionalidad de rollback (que *usa* ese histórico) recién se implemente en v2.

## Razones

- Permite tener un v1 entregable, demostrable y útil en semanas, no meses.
- Evita el riesgo de diseñar interfaces especulativas para casos de uso que todavía no están
  bien entendidos — las interfaces de v1 nacen con una sola implementación real detrás, lo cual
  las mantiene honestas (no abstraen de más).
- Cada versión es, en sí misma, una demostración incremental de profundidad técnica creciente
  para el portfolio: v1 muestra fundamentos sólidos, v2 muestra manejo de concurrencia y
  consistencia, v3 muestra pensamiento de sistemas distribuidos.

## Alternativas consideradas

- **Diseñar todas las interfaces pensando en v3 desde el día 1** (por ejemplo, un `ProxyManager`
  con soporte multi-servidor ya en su firma): descartado por riesgo de sobre-ingeniería y de
  diseñar interfaces equivocadas antes de tener experiencia real con el problema en v1.
- **Un solo SPEC.md para las tres versiones**: descartado — ver razones de organización
  documental en el README de `../specs/`.

## Consecuencias

- `../specs/SPEC-v2.md` y `../specs/SPEC-v3.md` no se escriben exhaustivamente desde el inicio. Existen como
  documentos "semilla" (alcance y decisiones de alto nivel) y se completan a nivel de tarea
  recién al terminar la versión anterior, revisando el código real (no solo el spec) de esa
  versión.
- Cualquier desviación de las interfaces estables definidas acá, descubierta durante la
  implementación de v1, debe documentarse como una actualización a este ADR o a
  `../ARCHITECTURE.md`, no simplemente parcheada en el código sin registro.
