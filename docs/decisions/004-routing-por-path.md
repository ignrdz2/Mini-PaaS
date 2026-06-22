# ADR 004: Routing por path (con strip-prefix) en lugar de subdominios

## Estado
Aceptado

## Contexto
Cada app deployada necesita ser alcanzable desde el navegador/curl del usuario. Las dos
estrategias estándar son:

- **Subdominios**: `mi-app.localhost` → requiere que el sistema operativo resuelva wildcards
  `*.localhost` a `127.0.0.1` (funciona out-of-the-box en la mayoría de sistemas modernos, pero
  no es 100% garantizado en todos los entornos) o herramientas externas como `nip.io`.
- **Paths**: `localhost/mi-app` → no requiere ninguna configuración de DNS/hosts, funciona
  igual en cualquier entorno (incluyendo CI, contenedores anidados, WSL, etc.).

## Decisión
Usar routing por path: `localhost/<nombre-app>`. Traefik hace **strip del prefijo** antes de
reenviar la request al container — la app deployada nunca ve `/mi-app` en la URL que recibe,
recibe la request como si estuviera en la raíz (`/`).

## Razones

- **Cero configuración adicional**: no depende de la resolución de DNS local del sistema
  operativo del usuario, lo cual hace la demo reproducible en cualquier máquina sin pasos
  extra.
- **Simplicidad para v1**: el alcance de v1 es validar el flujo completo de build → deploy →
  routing, no la fidelidad de producción del modelo de URLs.

## Consecuencia técnica importante (a tener en cuenta en apps deployadas)

Como Traefik hace strip-prefix, una app que genera URLs absolutas hacia sus propios assets
(ej. `<script src="/app.js">` en lugar de relativas) va a fallar, porque esa request
`/app.js` no tiene el prefijo `/mi-app` y Traefik no sabe a qué app rutearla.

Esto es una limitación conocida de v1, no un bug a resolver ahí. Se documenta explícitamente en
`../specs/SPEC-v1.md` como una limitación, junto con la mitigación recomendada (que las apps de prueba
usadas para la demo usen rutas relativas, o se configure su base path si el framework lo
soporta).

## Alternativas consideradas

- **Subdominios vía `*.localhost`**: más fiel a cómo funcionan plataformas reales (Heroku,
  Railway), y evita el problema de assets con rutas absolutas. Se descartó para v1 por la
  dependencia de resolución DNS local, que agrega una variable fuera del control del código
  mismo del proyecto. Queda como candidato natural para v2/v3 si se decide perseguir mayor
  fidelidad con plataformas reales.

## Consecuencias

- `TraefikFileProxyManager` (ver ADR 002) genera reglas de tipo `PathPrefix` + middleware de
  `StripPrefix` para cada app.
- Las apps de prueba elegidas para la demo de v1 deben validarse contra esta limitación antes
  de documentarlas como ejemplo funcionando.
