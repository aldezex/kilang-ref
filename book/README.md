# El libro: su Markdown y sus comprobaciones

Este directorio guarda la prosa de *Ki: de cero a experto. Construye tu propio lenguaje funcional en Go*, las tarjetas de test de cada capítulo y el registro de las etiquetas verificadas. Quien escribe un capítulo sigue `docs/superpowers/process/prose.md`; este fichero describe el dialecto de Markdown que entienden `bookx` y la página del libro, y lo que `bookx verify` exige de él.

## Qué hay aquí

| Fichero | Contenido |
|---|---|
| `chNN-slug.md` | La prosa del capítulo `NN` (dos cifras), con directivas en lugar de código copiado. |
| `tests/chNN.tests` | Las tarjetas de test del capítulo (spec 11.3). |
| `tests/chNN.list` | Los tests que pasan en el capítulo, una línea `<import path> <test>` por test, ordenadas. No se escribe a mano: lo genera `ci/check-tags.sh` con `bookx testlist`. |
| `verified-tags.txt` | Una línea `<etiqueta> <sha40> <YYYY-MM-DD> <goversion>` por etiqueta verificada. Tampoco se escribe a mano: lo escribe `ci/check-tags.sh`. |

La prosa y las tarjetas viven en `main`, dentro del rango de su capítulo y antes de la etiqueta `chNN`.

## Cabecera del capítulo

Cada `chNN-slug.md` empieza con un bloque de metadatos entre líneas `---`:

```text
---
chapter: 3
title: El título del capítulo
part: I
hours: 8
milestone: el hito literal del apartado 12.5 del spec
---
```

`chapter` es el número sin ceros a la izquierda, `part` el número romano de la parte, `hours` las horas del capítulo y `milestone` el hito. La página del libro lee estos cinco campos y quita el bloque antes de mostrar el texto.

## Secciones fijas

En este orden y con estos títulos de nivel 2:

1. `## Objetivo`
2. `## Teoría`
3. `## Tests primero`
4. `## Implementación`, con un `### Sesión K: <título> (≈ H h)` por cada sesión del capítulo
5. `## Ejecútalo`
6. `## Ejercicios`
7. `## Preguntas de repaso`
8. `## Resumen`
9. `## Go en el trabajo`

## Directivas

Una directiva se escribe con dobles llaves, **sola en su línea** (con sangría o sin ella): `bookx` la sustituye por un bloque de código o una tabla, y no puede hacerlo en medio de una frase. Dentro de una valla de código o de un fragmento en línea con comillas invertidas, las llaves son solo texto, y así se pueden poner ejemplos.

| Directiva | Qué muestra |
|---|---|
| `{{file TAG RUTA}}` | Un fichero entero, tal como está en la etiqueta. |
| `{{listing TAG REGION}}` | La región marcada con ese id en el código de la etiqueta. |
| `{{diff TAG..TAG RUTA}}` | Los cambios de un fichero entre dos etiquetas. |
| `{{reftest TNN.MM}}` | El test de referencia de una tarjeta `unit`, `prop`, `example` o `bench` del capítulo, plegado en la página. |
| `{{cards}}` | La tabla de tarjetas del capítulo: Id, Caso, Por qué, Esperado y Nivel. Cada capítulo la lleva. |

**La prosa cita solo etiquetas de sesión** (`chNN-sK`), nunca la etiqueta del capítulo (`chNN`): mientras escribes la prosa, esa etiqueta todavía no existe. `{{reftest}}` lee el test en la etiqueta `chNN` si existe y, si no, en la última sesión del capítulo (la de mayor `K`).

### Regiones en el código

Una región es un trozo de un fichero que el libro quiere citar. Se marca con comentarios en una línea propia (`//` en Go, `#` en shell y YAML):

```text
// @book lexer.line_rule hint "escribe la regla de línea"
…código…
// @elide
…más código…
// @end
```

El modo que sigue al id es `full` (el código tal cual), `hint "texto"` (un texto que sustituye al código hasta que lo intentas), `solution` (el código, plegado) o `test` (un test que se muestra antes del código que lo hace pasar). Una línea que solo lleva `@elide` se muestra como un comentario de puntos suspensivos. El id sigue `[a-z0-9_]+(\.[a-z0-9_]+)+` y no cambia entre capítulos. Solo se leen marcas en ficheros `.go`, `.sh`, `.yml`, `.yaml`, `.ki` y `.s`, y no en los que cuelgan de un directorio `testdata`.

## Contenedores

Los contenedores empiezan con `:::tipo` en una línea y terminan con `:::` en otra:

`:::exercise`, `:::solution`, `:::review`, `:::answer`, `:::from <Rust|TS|Python>`, `:::note` y `:::bug`.

Van **siempre en secuencia y nunca anidados**, porque la página solo abre un contenedor cuando no hay otro abierto. Un `:::solution` empieza **después** del `:::` que cierra su `:::exercise`, y lo mismo vale para `:::answer` y `:::review`. Las líneas dentro de una valla de código no cuentan. `bookx verify` señala `nested container :::<tipo>` y `unclosed container`.

## Atributos de las vallas

`bookx render` convierte cada directiva de código en una valla con atributos que lee la página:

````text
```go title="internal/syntax/lexer.go" tag="ch03-s2" mode="file"
```
````

- `title` es la ruta del fichero (o del test), `tag` la etiqueta y `mode` uno de `file`, `listing`, `diff` o `test`.
- Una región `hint` añade `hint="…"`, y un `{{reftest}}` pone en `hint` el id y el nombre de su tarjeta.
- En el valor de cualquier atributo, `"` se escribe `&quot;` y `` ` `` se escribe `&#96;`; la página los restituye.
- El lenguaje sale de la extensión: `.go` es `go`, `.ki` es `ki`, `.sh` es `sh`, `.yml` es `yaml`, `.s` es `goasm`, `.md` es `markdown` y cualquier otra (también `.mod`) es `text`.
- Si el texto citado tiene una línea con tres o más comillas invertidas, la valla usa una más que la racha más larga, para que el texto no la cierre.
- Los bytes que no son UTF-8 válido se muestran como U+FFFD.

## Cómo se escriben los tests del libro

`bookx` lee los nombres de los tests del código y de `go test -json`, así que su forma es un contrato:

- Un test del libro es una función `TestXxx`, un subtest `t.Run("nombre_en_snake_case", …)` con el nombre como **literal de cadena** (nunca una variable ni un `fmt.Sprintf`), un fuzz target `FuzzXxx`, un `ExampleXxx` o un `BenchmarkXxx`.
- Los tests de tabla van **dentro de un subtest**, y la tarjeta cubre el subtest con toda su tabla. Las filas **no** usan `t.Run`: se recorren con un bucle y se informan con `t.Errorf("%s: …", fila.nombre, …)`. Así `go test -json` lista el subtest y no sus filas, y `bookx` encuentra el literal en el fichero.
- Una función `TestXxx` se declara en un solo fichero, y todos sus subtests viven en él.
- Un test que necesita `internal/testsupport` vive en el paquete de test externo (`package sema_test`); uno que toca identificadores no exportados, en un `*_internal_test.go` con otra función, que no importa `testsupport`.
- Un fichero de test que contiene el comentario `// bookx: not-in-book` (en la práctica, en su primera línea) queda fuera del libro: sus tests no necesitan tarjeta.

## Qué comprueba `bookx verify`

`go build -o build/bin/bookx ./cmd/bookx && build/bin/bookx verify` imprime `bookx verify: OK` o una lista de problemas `fichero:línea: mensaje`, ordenada, y sale con 1. Comprueba siete reglas:

1. Cada `file`, `listing` y `diff` se resuelve en su etiqueta, y un `listing` encuentra su región una sola vez. Un `diff` cuyo fichero es igual en las dos etiquetas también es un problema, porque mostraría un bloque vacío.
2. En cada etiqueta citada, las marcas `@book` y `@end` están equilibradas y los ids son únicos.
3. Las tarjetas tienen ids únicos y consecutivos, con el `NN` de su fichero, y cada prosa lleva `{{cards}}`.
4. El `file` de cada tarjeta existe en el código del capítulo y su `ref` aparece en él.
5. Cada test nuevo del capítulo (el que está en `chNN.list` y no en el `ch(NN-1).list`) tiene tarjeta, y cada tarjeta que no es un `bench` tiene su test en `chNN.list`.
6. Cada etiqueta citada está en `verified-tags.txt` con el SHA de su commit, y cada etiqueta `chNN` lleva la prosa y las tarjetas de su capítulo.
7. Los contenedores no se anidan.

El código de un capítulo se lee en la etiqueta `chNN` si existe y, si no, en su última sesión. Un capítulo con prosa y `chNN.list` pero sin etiqueta `chNN` está *pendiente*: se comprueba con la prosa y las tarjetas del árbol de trabajo.

Otros comandos: `bookx render <dir>` escribe el Markdown con las directivas resueltas (`<dir>/chNN.md`), `bookx web <dir> [--repo DIR] [--all]` escribe la página del libro (`index.html`, `book/outline.json`, `book/manifest.json` y `book/chNN.md` de cada capítulo publicado, o de todos con `--all`; solo añade y reemplaza ficheros, así que conviene darle un directorio propio), `bookx regions <etiqueta>` lista las regiones de una etiqueta y `bookx testlist <fichero>` filtra la salida de `go test -json`. `verify`, `render`, `regions` y `web` aceptan `--repo DIR` (por defecto, el directorio actual).
