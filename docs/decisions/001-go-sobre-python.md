# ADR 001: Go como lenguaje de implementación

## Estado
Aceptado

## Contexto
El resto del portfolio (Text-to-SQL Dashboard, RAG Pipeline, Precios-UY) está construido en
Python con FastAPI. Para este proyecto —una plataforma de deploy minimalista— se evaluó
continuar con ese stack o adoptar Go.

El dominio del proyecto (orquestación de containers, reverse proxies, herramientas de
infraestructura) tiene un ecosistema de referencia mayormente escrito en Go: Docker, Kubernetes,
Traefik, Terraform. Esto no es casualidad — Go fue diseñado con prioridades que calzan bien con
este tipo de software: concurrencia de primera clase (goroutines/channels), binarios estáticos
sin runtime que distribuir, y tipado fuerte con bajo ceremonial.

## Decisión
Implementar tanto el orquestador (backend) como el CLI en Go.

## Razones

- **Coherencia con el dominio**: trabajar en el mismo lenguaje que las herramientas con las que
  el sistema integra (Docker SDK, cliente de Traefik) reduce friction conceptual — la
  documentación, ejemplos y issues de esas librerías están pensados para Go.
- **Concurrencia nativa**: streaming de logs de containers, builds, y futuros healthchecks
  concurrentes (v2/v3) son casos de uso naturales para goroutines, sin la complejidad de
  asyncio/threading que tendría la alternativa en Python.
- **Distribución simple**: un CLI compilado a un único binario estático, sin necesitar un
  intérprete ni un virtualenv en la máquina del usuario, es una ventaja real para una
  herramienta de línea de comandos.
- **Valor narrativo para portfolio**: el resto de los proyectos ya demuestra dominio de
  Python/FastAPI. Sumar Go demuestra capacidad de moverse fuera de la zona de confort y elegir
  herramientas en base al dominio del problema, no por inercia.

## Alternativas consideradas

- **Python + FastAPI**: hubiera sido más rápido de implementar dado la experiencia previa, y
  reusable directamente con patrones ya establecidos (logging, estructura de proyecto). Se
  descartó porque no aporta ningún diferencial sobre los proyectos existentes del portfolio, y
  porque las librerías de Docker/Traefik en Python son wrappers más finitos sobre las mismas
  APIs que en Go son ciudadanos de primera clase.

## Consecuencias

- Curva de aprendizaje en Go durante el desarrollo; se espera que el código de v1 tenga menos
  pulido idiomático que el Python ya dominado. Esto es aceptable y esperado.
- No hay reuso directo de patrones (logging, testing) entre este proyecto y el resto del
  portfolio. Se documentan los patrones de Go elegidos (ver `../specs/SPEC-v1.md`) para mantener
  consistencia interna del propio proyecto.
- El testing de CLI vía subprocess (igual que en el proyecto de CLI tool planeado por separado)
  es directamente aplicable en Go con el paquete estándar `os/exec` y `testing`.
