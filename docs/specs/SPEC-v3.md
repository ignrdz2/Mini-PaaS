# SPEC v3 (semilla) — Multi-tenancy y producción real

> **Este documento es una semilla, no un spec accionable todavía.** Ver la nota equivalente en
> `SPEC-v2.md` — el mismo criterio aplica acá, con la salvedad de que v3 depende de cómo
> termine viéndose v2 en la práctica, así que el nivel de incertidumbre acá es todavía mayor.
> Completar recién con el código real de v2 delante, no antes.

## Alcance previsto

- **Límites de recursos por container**: CPU y memoria acotados por app (el SDK de Docker
  expone esto directamente vía `HostConfig.Resources` al crear el container — no debería requerir
  cambios estructurales grandes, principalmente exponer la configuración y aplicarla).
- **Logs persistentes y buscables**: los logs de containers dejan de vivir solo en el daemon de
  Docker (efímeros, se pierden si el container se elimina) y se vuelcan a algún almacenamiento
  consultable. Candidatos a evaluar en su momento: Loki (si se quiere experiencia con el
  ecosistema de observability), o simplemente Postgres con un índice de texto si el volumen es
  bajo (coherente con "no traer una pieza de infra nueva sin necesidad real").
- **Webhooks de GitHub**: recibir un evento de push y disparar un deploy automáticamente, sin
  intervención manual del CLI — el verdadero "git push to deploy".
- **Exploración de soporte multi-servidor**: no necesariamente una implementación completa
  (eso empieza a acercarse a reinventar Kubernetes, que no es el objetivo del proyecto), sino
  al menos un diseño explícito de qué cambiaría y un prototipo acotado si el tiempo lo permite.
  Este ítem es candidato a quedar como sección de diseño/ADR en lugar de código funcional,
  decisión a tomar explícitamente al llegar acá.

## Cómo se apoya en las interfaces de v1/v2

- **`ProxyManager`** es nuevamente el punto de extensión más probable para la exploración de
  multi-servidor: una implementación que hable con un proxy distribuido en lugar de escribir un
  archivo local, sin que el resto del sistema (Builder, Store, orquestación) necesite saber la
  diferencia.
- Los **webhooks** se integran como un disparador adicional del mismo flujo de deploy ya
  existente desde v1/v2 (build → run → healthcheck → switch) — la pieza nueva es el endpoint
  que recibe el evento de GitHub y lo traduce a la misma llamada interna que ya dispara
  `deployctl apps deploy`, no un flujo de deploy paralelo y distinto.

## Preguntas abiertas a resolver antes de detallar el spec completo

- Límites de recursos: ¿valor fijo global, configurable por app, o con un default razonable y
  override opcional? ¿Qué pasa si una app excede su límite — se la mata, se la throttlea?
- Logs: ¿cuánto tiempo se retienen? ¿Hay un volumen real de datos a considerar dado que esto es
  un proyecto de portfolio (no producción real con tráfico real), o alcanza con una solución
  simple sin preocuparse de escala?
- Webhooks: ¿cómo se valida que el webhook viene realmente de GitHub (verificación de firma
  HMAC con un secret compartido)? Es un detalle de seguridad no trivial a no pasar por alto.
- Multi-servidor: ¿el objetivo es realmente correr esto en más de un servidor, o el valor para
  portfolio está más en el documento de diseño/ADR que en código funcionando? Vale la pena
  discutir esto explícitamente antes de invertir tiempo de implementación acá.

## Nota sobre alcance

De las tres versiones, v3 es la que con más probabilidad requiere recortar alcance respecto a
lo listado arriba. Es preferible terminar v3 con 2 de los 4 ítems implementados sólidamente (y
el resto documentado como diseño/ADR) que con los 4 a medio terminar. Esta priorización se
decide explícitamente al completar este spec, no se asume de antemano.
