# ADR 005: Acceso a PostgreSQL vía sqlc

## Estado
Aceptado

## Contexto
SPEC-v1.md deja abierta la elección entre dos enfoques para acceder a Postgres desde Go:

- **`pgx` directo** (`github.com/jackc/pgx/v5`): driver de Postgres de alta performance para Go.
  Se usa directamente con `database/sql` o con la API nativa de pgx: el desarrollador escribe
  queries SQL como strings, ejecuta con `QueryRow`/`Exec`, y scannea manualmente las columnas
  al struct destino.
- **`sqlc`** (`github.com/sqlc-dev/sqlc`): herramienta de generación de código. El desarrollador
  escribe queries SQL en archivos `.sql` y sqlc genera código Go tipado (structs y funciones)
  a partir de ellas. En runtime usa pgx como driver.

## Decisión
Usar **sqlc** para la capa `internal/store`.

## Razones

- **Seguridad en tiempo de compilación**: los tipos de los parámetros y columnas de cada query
  quedan en el código Go generado. Si el schema de la base de datos cambia y los queries quedan
  inconsistentes, la compilación falla — no hay que esperar a un error en runtime.
- **Queries como artefactos de primera clase**: los archivos `.sql` son legibles, versionables,
  y auditables directamente. No hay strings de SQL dispersos en el código Go ni un DSL de ORM
  que aprender.
- **Sin magia de ORM**: sqlc no infiere el schema desde structs de Go ni aplica convenciones
  implícitas. El SQL que se escribe es exactamente el que se ejecuta — coherente con la
  filosofía del proyecto de entender qué pasa un nivel por debajo.
- **Performance**: el código generado usa pgx/v5 directamente, sin abstracción adicional en
  el camino crítico de cada query.
- **Bajo overhead de adopción**: sqlc es una herramienta de desarrollo (no una dependencia
  de runtime). La única dependencia runtime adicional es pgx/v5, que se necesitaría de todas
  formas con el approach de pgx directo.

## Alternativas consideradas

- **pgx directo con `database/sql`**: más simple de arrancar, sin tooling extra. El problema
  es el scaneo manual de columnas a structs: propenso a errores de orden de columnas que no
  detecta el compilador, y repetitivo para cada query. A medida que el Store crece (CRUD de
  apps y deployments con varios estados posibles), el boilerplate acumula.
- **pgx directo con la API nativa** (sin `database/sql`): evita la indirección de
  `database/sql` y tiene mejor performance, pero no resuelve el problema del scaneo manual.
  Tampoco aporta el beneficio de tener los queries como SQL legible separado del código Go.
- **GORM u otro ORM**: descartado. Agrega una abstracción significativa, implica aprender un
  DSL propio para queries complejos, y va en contra del objetivo del proyecto de entender qué
  pasa exactamente en cada interacción con la base de datos.

## Consecuencias

- Las queries SQL viven en `internal/store/queries/` como archivos `.sql`.
- El código Go generado por sqlc vive en `internal/store/` (junto a los archivos de interfaz
  y modelos escritos a mano).
- sqlc requiere un archivo de configuración `sqlc.yaml` en la raíz del proyecto.
- El workflow de desarrollo para el Store es: editar `.sql` → correr `sqlc generate` → el
  código generado actualiza `internal/store/`.
- `github.com/jackc/pgx/v5` es la única dependencia runtime nueva. sqlc en sí es una
  herramienta de desarrollo (se instala con `go install` o se descarga como binario).
- `github.com/golang-migrate/migrate/v4` se usa para correr las migraciones, con el driver
  de pgx5 (`migrate/database/pgx/v5`).
