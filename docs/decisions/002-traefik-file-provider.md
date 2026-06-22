# ADR 002: Traefik con File Provider en lugar de Docker Provider

## Estado
Aceptado

## Contexto
Traefik necesita una fuente de verdad para sus reglas de routing (qué dominio/path mapea a qué
container). Soporta varios "providers", entre ellos:

- **Docker provider**: Traefik se conecta al socket de Docker y lee labels en los containers
  (`traefik.http.routers.miapp.rule=...`) para autoconfigurarse.
- **File provider**: Traefik observa un archivo (YAML/TOML) en disco y recarga su configuración
  cada vez que cambia (`--providers.file.watch=true`).

## Decisión
Usar el **file provider**. El orquestador es responsable de escribir el archivo de
configuración dinámica de Traefik cada vez que cambia el estado de los deployments.

## Razones

- **Separación de responsabilidades clara**: el orquestador decide el routing (es dueño de esa
  lógica), Traefik solo la ejecuta. Con el Docker provider, parte de la lógica de routing queda
  implícita en labels distribuidos a través de containers, lo cual es más difícil de razonar
  como un todo.
- **Debuggability**: un archivo YAML es algo que se puede abrir, leer, versionar y diffear
  directamente. Las labels de Docker requieren `docker inspect` por container para reconstruir
  el estado completo del routing.
- **Preparación para v2/v3**: el flujo de zero-downtime deploys (v2) implica tener brevemente
  dos containers de la misma app corriendo (el viejo activo, el nuevo en healthcheck) y luego
  swichear el tráfico atómicamente. Esto se modela de forma más directa reescribiendo un archivo
  de config (cambiás a qué container apunta una regla) que coordinando labels en containers que
  se crean/destruyen.
- **No acopla el routing al ciclo de vida del container**: con el Docker provider, si el
  orquestador necesita rutear tráfico a algo que no es un container Docker corriendo ahí mismo
  (por ejemplo, en un v3 multi-servidor), el approach se cae. El file provider es agnóstico a
  *dónde* corre el destino.

## Alternativas consideradas

- **Docker provider con labels**: más "mágico" y requiere menos código (Traefik se
  autoconfigura), pero acopla el routing al ciclo de vida de containers individuales y es más
  opaco para debuggear. Descartado por las razones de separación de responsabilidades arriba.
- **API dinámica de Traefik (provider HTTP)**: Traefik también soporta un provider HTTP, donde
  expone un endpoint que Traefik consulta. Es conceptualmente similar al file provider pero sin
  el beneficio de "archivo legible en disco como artefacto". Se descartó por no aportar ventajas
  claras sobre el file provider para el alcance de v1.

## Consecuencias

- El orquestador necesita escribir el archivo de forma atómica (escribir a un archivo temporal
  y hacer `rename`) para evitar que Traefik lea un YAML a medio escribir.
- El archivo de configuración dinámica se convierte en una fuente de verdad derivada (se
  regenera completamente a partir del estado en PostgreSQL en cada cambio), nunca editada a
  mano ni parcialmente.
- Traefik debe correr con `--providers.file.watch=true` apuntando al path de este archivo.
