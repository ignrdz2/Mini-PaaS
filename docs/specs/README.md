# Specs por versión

Este proyecto se desarrolla en tres versiones incrementales (ver `../ARCHITECTURE.md` para el
resumen de alcance de cada una, y `../decisions/003-versionado-incremental.md` para el
razonamiento completo detrás de esta estrategia).

- **`SPEC-v1.md`**: spec exhaustivo y accionable. Es la fuente de verdad usada durante la
  implementación de v1, con tareas concretas por fase.
- **`SPEC-v2.md`** y **`SPEC-v3.md`**: documentos "semilla". Contienen el alcance previsto y
  las decisiones de diseño de alto nivel pensadas desde el inicio del proyecto, pero
  deliberadamente **no** bajan a nivel de tarea — ese detalle se completa recién al terminar la
  versión anterior, revisando el código real (no solo el spec) de esa versión, y discutiendo
  explícitamente las preguntas abiertas que cada semilla deja planteadas.

Esta separación evita dos problemas: especificar v2/v3 con tanto detalle desde el día 1 que
quede obsoleto para cuando se llega a implementarlos, y por otro lado perder de vista —mientras
se implementa v1— hacia dónde tiene que evolucionar el sistema.
