---
chapter: 1
title: El taller: el toolchain de Go, módulos, `go test` y la CI
part: I
hours: 8
milestone: tu repo `aldezex/kilang` creado; `ki --version` (desde `debug.ReadBuildInfo`); `go test ./...`, `go vet ./...` y `gofmt -l` en verde, también con `-race`, en local y en la CI de GitHub; comprobación de compilación cruzada para windows/amd64 y linux/arm64
---

## Objetivo

Al terminar tendrás tu repositorio, `aldezex/kilang`, publicado en GitHub, y un binario que ya responde:

```console
$ build/bin/ki --version
ki 0.0.1 (go1.27.3)
```

Aún no hay compilador: construyes el taller, una CLI con sus tests, `ci/ci-local.sh` y la CI que lo repite en cada push. Es el primer paso hacia el hito M1 (capítulo 10): `ki run fib.ki` imprimirá `6765`.

## Teoría

### El mapa: qué vas a construir

Ki es un lenguaje funcional puro con efectos algebraicos, y su compilador está escrito en Go. La Parte I construye la primera versión del pipeline:

```text
fichero .ki
  → SourceMap y spans            (cap. 2)
  → diagnósticos con caret       (cap. 3)  ki check
  → lexer y regla de línea       (cap. 5)  ki emit tokens
  → parser, AST                  (cap. 6)  ki emit ast
  → nombres y ámbitos            (cap. 7)
  → tipos                        (cap. 8)
  → Core, la IR de referencia    (cap. 9)  ki emit core
  → intérprete CEK               (cap. 10) ki run   → M1
```

Cada etapa cuelga de `ki`; por eso el capítulo empieza por el driver, el paquete que recibe la línea de órdenes y decide qué ejecutar.

### Go desde cero: qué hace `go build`

Go se instala como un único árbol, el *toolchain*: compilador, enlazador, biblioteca estándar y el comando `go`. `go build` lee los `import`, deduce el grafo de paquetes, compila lo que no esté en la caché y enlaza un ejecutable. No hay cabeceras ni makefiles: un paquete es un directorio.

:::from Rust
Como `cargo build`, sin `Cargo.lock` con cien crates (Ki solo usa la biblioteca estándar) ni perfiles `debug` y `release`.
:::

### `go.mod`: módulo, versión y toolchain

Un *módulo* es un árbol de paquetes con un `go.mod` en la raíz. `module github.com/aldezex/kilang` da el prefijo de todo import propio; la referencia, `kilang-ref`, usa el mismo, así que sus ficheros encajan en tu árbol.

`go 1.27` es la versión **mínima** del lenguaje: un toolchain más viejo no carga el módulo. `toolchain go1.27.3` es el **preferido**. `ignore ./build` saca ese directorio y sus subdirectorios de `./...`.

Con `GOTOOLCHAIN=auto`, por defecto, si el `go.mod` pide un Go más nuevo que el instalado, `go` lo busca o **lo descarga** y lo ejecuta. Con `local`, el del repositorio y la CI, usa siempre el instalado: nada cambia el compilador en silencio.

A cambio, `local` deja de imponer la línea `toolchain`: la vigila `TestRuntimeVersionMatchesToolchainLine`.

:::more Los mensajes exactos de `go`, los tests que los fijan, las fuentes y para qué sirve `ignore`
- Con `GOTOOLCHAIN=local` y un `go.mod` que pide `go 1.28`, go1.27.3 responde `go: go.mod requires go >= 1.28 (running go 1.27.3; GOTOOLCHAIN=local)` y sale con 1.
- `toolchain` es «a suggested toolchain», según go.dev/doc/toolchain: con `local`, un módulo con `toolchain go1.27.9` compila sin queja con go1.27.3. Entonces solo la hacen cumplir `ci-local.sh`, con `go version`, y `TestRuntimeVersionMatchesToolchainLine`, con `runtime.Version()`.
- En Go 1.27, `go mod init` escribe la versión completa del toolchain instalado y ninguna línea `toolchain`. Lo fija `TestToolchainFacts/go_mod_init_writes_full_version`; el detalle está en `docs/toolchain.md` de la referencia: `git show ref/ch01-s3:docs/toolchain.md`.
- `TestToolchainFacts/plain_build_has_devel_version` fija el `"(devel)"` de un `go build` normal fuera de un repositorio git.
- Con `ignore ./build`, un fichero roto en `build/` no rompe `go vet ./...` ni `go test ./...`. Según la referencia de módulos (go.dev/ref/mod), el comando `go` ignora el directorio de `ignore` y lo que cuelga de él «when matching package patterns». Lo fija `TestToolchainFacts/ignore_directive_hides_build`.
:::

:::from Rust
La línea `go` se parece a `rust-version` (la MSRV), y `toolchain` a fijar un canal.
:::

### Paquetes, nombres exportados, `cmd/` e `internal/`

Un paquete `main` con una función `main` es un ejecutable; cualquier otro, una biblioteca. `cmd/ki/` es el de `ki`; el compilador vive bajo `internal/`.

La primera letra decide la visibilidad: otro paquete ve `driver.Version`, *exportado*, y `goVersion` solo existe dentro de `driver`.

`internal/` añade una frontera que impone el comando `go`: un paquete bajo `.../internal/` solo lo importa código del árbol que contiene ese `internal`. Así `internal/driver` no es una API pública que luego no puedas cambiar.

:::from Rust
La mayúscula es `pub`, sin más grados; `internal/` hace de `pub(crate)`, por directorios.
:::

### Lo básico del lenguaje, en una tabla

| Concepto | Go | Rust | TypeScript | Python |
|---|---|---|---|---|
| Tipos básicos | `int` (64 bits en amd64), `int64`, `string`, `bool`, `byte` | `i64`, `String`, `bool`, `u8` | `number`, `string`, `boolean` | `int`, `str`, `bool` |
| Función | `func div(a, b int) int` | `fn div(a: i64, b: i64) -> i64` | `function div(a: number, b: number): number` | `def div(a, b):` |
| Retornos múltiples | `func divmod(a, b int) (int, int)`; `q, r := divmod(17, 5)` | una tupla | un array `[q, r]` | una tupla |
| Bucle clásico | `for i := 0; i < 3; i++ { … }` | `for i in 0..3` | `for (let i = 0; i < 3; i++)` | `for i in range(3)` |
| `range` | `for i := range 3` (0, 1, 2); `for i, x := range xs` (`_` descarta) | `for (i, x) in xs.iter().enumerate()` | `xs.forEach((x, i) => …)` | `for i, x in enumerate(xs)` |
| `switch` | sin *fall-through*; `fallthrough` lo pide | `match`, exhaustivo | `switch` con `break` | `match` |

| En Go | Qué significa |
|---|---|
| `for` | El único bucle: con tres cláusulas, solo con la condición (`for n < 3 { … }`, un `while`) o sin nada (`for { … }`, un `loop`). |
| `x := 3` | Declara e infiere el tipo. |
| *Valor cero* | Con el que empieza toda variable: `0`, `""`, `false` o `nil`, el de punteros, slices, mapas, funciones e interfaces (un `error` sin fallo es `nil`). |

:::from Rust
Un `switch` sin `default` que no casa con ningún `case` no hace nada, sin aviso.
:::

### Lo justo para el driver

- **Un puntero a struct es el mismo valor, no una copia.** Cambiar un campo de un struct recibido cambia una copia; de un puntero a él, el original.
- **Un método es una función con receptor.** `func (p Point) String() string` es de `Point`. Con receptor puntero, `func (b *Buffer) Write(…)` es de `*Buffer`, no de `Buffer`.
- **Un tipo satisface una interfaz por sus métodos.** Con `String() string` (`fmt.Stringer`), `%v` imprime lo que devuelve.
- **`io.Writer` e `io.Reader` son «cualquier cosa que sepa escribir o leer»**: interfaces de un método, `Write` y `Read`. `driver.Main` escribe en `os.Stdout` o, en los tests, en un `strings.Builder`, que guarda lo escrito.
- **`error` es un valor**: el último resultado de una función que puede fallar, que se comprueba con `if err != nil { … }`. No hay excepciones.

:::from Rust
`io.Writer` es `std::io::Write` sin `impl`, y `String()` hace de `impl Display`. Go devuelve `(T, error)` en lugar de `Result<T, E>`, sin que el compilador te obligue a mirarlo.
:::

:::from Rust
El puntero no es una referencia: sin `&mut` ni tiempos de vida, el recolector libera la memoria cuando ningún puntero la alcanza.
:::

### `gofmt` y `go vet`

`gofmt` reescribe el código en el único estilo de Go. `gofmt -l` lista los ficheros fuera de ese formato, y `ci/ci-local.sh` exige que no liste ninguno.

`go vet` busca errores que compilan, como un `%d` con un `string`.

:::from Rust
`gofmt` es `rustfmt` sin `rustfmt.toml`; `go vet`, un `clippy` que casi siempre busca errores, no estilo.
:::

:::more Mensajes del compilador y de `vet`, retornos múltiples y `switch` sin expresión, `bytes.Buffer` y `&buf`, `gofmt -l`, y comparaciones con Rust, TS y Python
- Un import o una variable local sin usar son un error de compilación, no un aviso: `"os" imported and not used`; `declared and not used: x`.
- Un `internal/` importado desde otro módulo: `use of internal package example.com/b/internal/secret not allowed`.
- Desde fuera, un nombre sin exportar es como si no existiera; desde un paquete de test externo: `undefined: ext.hidden`.
- Los retornos múltiples se asignan todos o ninguno; lo que sobre, a `_`. A medias, `x := two()`: `assignment mismatch: 1 variable but two returns 2 values`.
- Un `bytes.Buffer` se pasa como `&buf`, porque su `Write` tiene receptor puntero. Pasar `buf`: `bytes.Buffer does not implement io.Writer (method Write has pointer receiver)`. Los tests de `internal/toolchain` usan `bytes.Buffer`.
- `gofmt -l` sale con 0 aunque liste algún fichero: por eso `ci-local.sh` mira su salida.
- `go vet`, sobre `fmt.Sprintf("%d items", name)` con un `name` `string`: `fmt.Sprintf format %d has arg name of wrong type string`.
- `go test` pasa sobre cada paquete, antes de sus tests, un subconjunto «de alta confianza» de vet; si encuentra algo, el paquete sale `[build failed]`.
- Un `switch` sin expresión lleva una condición en cada `case`: sustituye a una cadena de `if`/`else if`.
- **`switch`, Rust.** Lo que más se echa de menos es la exhaustividad; el capítulo 3 lo compensa.
- **`go build`, TS.** Frente a `tsc` más `node`, hace los dos trabajos, y el binario lleva su runtime.
- **`go build`, Python.** Comprueba tipos y genera código máquina antes de ejecutar nada; un import o una variable local sin usar son errores, no avisos de linter.
- **`go.mod`, Rust.** Hace de `Cargo.toml` y `rust-toolchain.toml`.
- **Paquetes, Rust.** No hay `mod` ni `use`: el directorio es el paquete y el import es su ruta.
- **Visibilidad, Python.** La minúscula hace de `_nombre`, pero la impone el compilador.
- **`io.Writer` y `error`, Rust.** Para ser un `io.Writer` basta tener `Write([]byte) (int, error)`, sin `impl Write for MiTipo`. El `?` se escribe `if err != nil { return …, err }`.
:::

### `testing`: tests, subtests y tablas

Un test es una `func TestXxx(t *testing.T)` en un fichero `_test.go`.

- **Subtests.** `t.Run("version_command", func(t *testing.T) { … })` crea un test hijo, `TestCLI/version_command`, que se informa y selecciona por separado. Su nombre es un literal en `snake_case`: `bookx`, la herramienta que verifica el libro, lo enlaza con su tarjeta, la ficha de cada test.
- **Tests de tabla.** Las filas se recorren *dentro de un subtest*, sin `t.Run` por fila, y cada fallo la nombra: `t.Errorf("%s: …", tc.name, …)`. Así una tarjeta cubre el subtest con su tabla, y `go test -json` no lista filas.
- **Slice de structs.** `[]struct{ name, want string }{ {"go_line", "go 1.27"}, … }` es un struct anónimo y un *slice*, una vista de un array, con sus filas, en la misma expresión.
- **Literal de función.** `func(t *testing.T) { … }` es una función sin nombre, que `t.Run` recibe como argumento. Ve las variables de la función que la contiene: es un *closure*.

`t.Errorf` anota el fallo y sigue; `t.Fatalf` lo anota y termina.

:::from Rust
No hay `assert_eq!`: comparas con un `if` y escribes el mensaje con lo obtenido y lo esperado.
:::

### `flag` y el patrón de subcomandos

Para subcomandos con `flag`, de la biblioteca estándar, el patrón es una tabla de comandos y un `flag.FlagSet` por comando: `Main` busca la primera palabra, y el comando parsea el resto.

Cada `FlagSet` usa `flag.ContinueOnError`: con `flag.ExitOnError`, un flag desconocido terminaría el proceso con 2 y mataría el binario de test, que ejecuta `Main` en proceso. `Parse` devuelve el error, `ki` lo informa en su formato, y `SetOutput(io.Discard)` evita que `flag` escriba además su ayuda en el stderr del proceso.

:::from Rust
`flag` está lejos de `clap`, pero no es una dependencia.
:::

### `debug.ReadBuildInfo` y por qué `Version` es una constante

`runtime/debug.ReadBuildInfo()` devuelve lo que `go build` graba en cada binario: la versión de Go (`GoVersion`) y la del módulo (`Main.Version`). `goVersion()` usa la primera.

La de `ki` no puede salir de ahí: en Go 1.27, `Main.Version` es `"(devel)"` fuera de un repositorio git y, dentro, una pseudo-versión que cambia con cada commit. Por eso `Version` es una constante, `"0.0.1"`.

:::from Rust
No hay `env!("CARGO_PKG_VERSION")`: la versión de un módulo es la etiqueta de git que lo publica.
:::

### TDD rojo-verde y tests golden

- **Rojo.** Escribes el test de algo que no existe y lo ves fallar, por la razón esperada: uno que nunca has visto en rojo puede pasar por accidente.
- **Verde.** El código mínimo que lo hace pasar.
- **Refactor.** Limpias con los tests como red.

Un test fuerte compara valores exactos y mira los bordes: si `ki` listara sus comandos con `version` antes que `--version`, solo lo vería `usage_lists_commands`. Pregúntate qué implementación equivocada dejaría pasar cada test; si es «cualquiera», no sirve.

Un *test golden* compara la salida de un programa con la esperada, guardada en un fichero. En Ki va en la cabecera de cada programa, con su código de salida, y la compara `ki test` (capítulo 3).

### GitHub Actions: runners, contenedores y compilación cruzada

GitHub Actions ejecuta *workflows*, ficheros YAML en `.github/workflows/`, ante un evento, como un push. Cada *job* corre en un *runner*, una máquina virtual nueva (`runs-on: ubuntu-latest`), y puede correr dentro de un **contenedor** con imagen de Docker: `ubuntu:26.04`, la Ubuntu de tu WSL, no se mueve como `ubuntu-latest`.

En la **compilación cruzada**, `GOOS=windows GOARCH=amd64 go build ./cmd/ki` produce un `.exe` desde Linux. Compilar para Windows y ARM64 en cada push detecta la *deriva*, el código que solo compila en Linux: `syscall.Kill` falla para windows/amd64 con `undefined: syscall.Kill`.

:::from Rust
No hay `rustup target add`: sin cgo (llamadas a C), bastan las dos variables.
:::

:::more `t.Helper`, `t.Cleanup` y `t.TempDir`, `-run`, `-count=1`, `-shuffle=on`, `-json` y `-race`, cgo en cruzado, `goVersion()` y más comparaciones
- `t.Helper()` marca un ayudante: el fallo señala la línea que lo llamó, no su `t.Errorf`.
- `t.Cleanup(f)` registra una función que corre al terminar el test, también si falló.
- `t.TempDir()` da un directorio que se borra cuando terminan el test y sus subtests.
- `-run` selecciona por nombre. En `-run 'TestCLI/usage_lists'`, cada trozo entre barras es una expresión regular sin anclar: `-run 'TestCLI/usage'` ejecuta todo subtest cuyo nombre contenga `usage`, como `no_args_is_usage_error` y, desde la sesión 3, `version_help_prints_usage`.
- `-count=1` ejecuta aunque haya caché: sin él, un segundo `go test` sin cambios responde `ok … (cached)`.
- `-shuffle=on` baraja el orden (ejercicio 4), y `-json` emite un objeto por evento.
- `-race` compila con el detector de carreras de datos. Necesita cgo, el mecanismo de Go para llamar a C, y cgo un compilador de C: lo instala «Puesta en marcha». En Go 1.27, sin `gcc` en el `PATH`, `go env CGO_ENABLED` da `0`, y entonces `go test -race` sale con 2 y `-race requires cgo` (lo fija `TestToolchainFacts/race_requires_cgo`).
- Para windows/amd64 y linux/arm64, `go env CGO_ENABLED` también da `0`. `syscall.Kill` compila en linux/amd64 y linux/arm64.
- Sin información de build, `goVersion()` usa `runtime.Version()`, la misma cadena. `Version` se cambia a mano.
- **Literal de función, TS.** Una *arrow function* sin forma abreviada y con los tipos obligatorios: `func(x int) int { return x * 2 }` frente a `(x: number) => x * 2`.
- **`go test`, Python.** `t.Run` se parece a `parametrize` con ids, y `t.Cleanup` y `t.TempDir`, a las fixtures con `yield` y a `tmp_path`.
- **`flag`, Rust.** Ni derive, ni subcomandos, ni ayuda con formato.
- **Compilación cruzada, Rust.** A menudo hace falta un enlazador del destino. Go compila la biblioteca estándar desde sus fuentes y enlaza con su `link`.
:::

## Tests primero

{{cards}}

Úsalas así: escribe el test de la tarjeta, míralo fallar, implementa lo justo para el verde, y solo entonces despliega su referencia. La sesión 1 da el fichero de test; desde la 2 los escribes tú.

## Implementación

### Sesión 1: el módulo, `ki --version` y `ci-local.sh` (≈ 3 h)

#### Puesta en marcha

1. **Comprueba el toolchain.**

```console
$ go version
go version go1.27.3 linux/amd64
```

2. **Instala lo que `-race`, el detector de carreras de datos, necesita,** los mismos paquetes que la CI:

```sh
sudo apt install gcc libc6-dev
```

3. **Crea el repositorio en `~/kilang`, nunca bajo `/mnt/c`:**

```sh
mkdir ~/kilang && cd ~/kilang
git init -b main
go mod init github.com/aldezex/kilang
```

4. **Edita el `go.mod`.** `go 1.27.3`, lo que escribe `go mod init` en Go 1.27, exige el parche 3, y Ki solo necesita el lenguaje de la 1.27:

{{file ch01-s1 go.mod}}

5. **Añade este `.gitignore`.**

{{file ch01-s1 .gitignore}}

6. **Añade una licencia MIT:** `gh repo license view mit > LICENSE`, y rellena `[year]` y `[fullname]`.
7. **Haz el primer commit y publica:**

```sh
git add go.mod .gitignore LICENSE
git commit -m "init: module and MIT license"
gh repo create aldezex/kilang --public --source . --push
```

8. **Trae las etiquetas de la referencia,** con un prefijo que no choque con las tuyas:

```sh
git remote add --no-tags ref https://github.com/aldezex/kilang-ref.git
git fetch ref 'refs/tags/*:refs/tags/ref/*'
```

Más adelante, con ellas te comparas o recuperas un fichero:

```sh
git diff ref/ch01-s1 -- internal/driver/
git restore --source=ref/ch01-s1 -- <fichero>
```

#### Los tests, en rojo

T01.01–T01.04 fijan la CLI mínima: `--version`, por stdout con 0; sin comando válido, dos líneas de uso por stderr con 2. El 2 es el código de salida habitual para «la línea de órdenes está mal».

Tu primer test en Go va completo, en `internal/driver/cli_test.go`:

{{file ch01-s1 internal/driver/cli_test.go}}

- `args ...string`: parámetro variádico (`...args: string[]` en TS, `*args` en Python); dentro es un `[]string`, vacío en `runCLI(t)`.
- `(code int, stdout, stderr string)`: resultados con nombre, que empiezan con su valor cero y se asignan como variables.
- `&out`: la dirección de un `strings.Builder`, cuyo valor cero ya es utilizable; su `Write` tiene receptor puntero.

```console
$ go test ./internal/driver/
# github.com/aldezex/kilang/internal/driver [github.com/aldezex/kilang/internal/driver.test]
internal/driver/cli_test.go:19:9: undefined: Main
internal/driver/cli_test.go:45:19: undefined: Version
FAIL	github.com/aldezex/kilang/internal/driver [build failed]
FAIL
```

En un lenguaje compilado, el primer rojo suele ser un error de compilación.

#### El código

`cmd/ki/main.go` solo conecta `driver.Main` con el proceso:

{{file ch01-s1 cmd/ki/main.go}}

Es el único `os.Exit` de `ki`: `Main` recibe los flujos y devuelve el código, así que los tests la ejecutan en proceso.

{{file ch01-s1 internal/driver/version.go}}

{{file ch01-s1 internal/driver/cli.go}}

#### `ci/ci-local.sh`

El script que decide el verde de una sesión: `gofmt -l`, `go vet`, el build, los tests con y sin `-race` y la compilación cruzada. `set -euo pipefail` lo corta en el primer fallo.

{{file ch01-s1 ci/ci-local.sh}}

Hazlo ejecutable (`chmod +x ci/ci-local.sh`) y ejecútalo. La segunda tanda es la de `-race`; los tiempos varían, y lo ya pasado sin cambios sale `(cached)`.

```console
$ ci/ci-local.sh
?   	github.com/aldezex/kilang/cmd/ki	[no test files]
ok  	github.com/aldezex/kilang/internal/driver	0.002s
?   	github.com/aldezex/kilang/cmd/ki	[no test files]
ok  	github.com/aldezex/kilang/internal/driver	1.014s
verify: OK
```

:::more Por qué no `/mnt/c`, qué hace `gh`, el `.gitignore`, Delve, el código de salida 1, y la sintaxis menor de esta sesión
- En WSL, `/mnt/c` es el disco de Windows, montado por `9p` (lo muestra `findmnt`), y la documentación de WSL recomienda trabajar en el sistema de ficheros de Linux por velocidad.
- `gh repo create` crea el repositorio en GitHub, lo añade como remoto (`--source .`) y sube los commits (`--push`).
- `/build/` es donde `ci/ci-local.sh` escribe `build/bin/ki`: sin esa línea, un `git add -A` subiría el binario. `/.claude/`, `/.superpowers/` y `.cache/` son carpetas de trabajo de la referencia (`ci-local.sh` salta las dos primeras en su `find`); en tu repo no estorban.
- Delve, el depurador, se instala en el capítulo 10.
- El código de salida 1 dirá que `ki` no pudo escribir la respuesta (sesión 3) o que tu programa Ki tiene errores.
- `package driver`: el paquete que prueba, así que el test ve sus nombres sin exportar.
- `TestCLI`, no `TestMain`: `func TestMain(m *testing.M)` tiene un significado propio (sesión 3).
- `%q` escribe una cadena entre comillas y con escapes (`"a\nb"`); `%v`, desde la sesión 2, cualquier valor en su forma por defecto.
- `checkUsageError` mira solo el prefijo y que haya dos líneas, porque la lista de comandos crece; solo `usage_lists_commands` la fija: aquí, `--version`; en su tarjeta, T01.04, la lista final.
- `os.Args[1:]` son los argumentos sin el nombre del programa.
- `if bi, ok := debug.ReadBuildInfo(); ok { … }` es un `if` con sentencia inicial: `bi` y `ok` solo existen dentro de él.
- El comentario antes de `package driver` es la documentación que muestra `go doc`.
- `fmt.Fprintf` escribe en el `io.Writer` que le pases.
:::

Si te atascas o tu versión se desvía demasiado, copia el fichero de `ref/ch01-s1` y sigue.

Al terminar: tus tests en verde, commit y etiqueta anotada `ch01-s1`; compárate con `ref/ch01-s1`.

```sh
git tag -a ch01-s1 -m "ch01-s1: module, ki --version and ci-local"
```

### Sesión 2: comandos con `flag`, tests de tabla y los hechos del toolchain (≈ 2,5 h)

#### Los tests de la CLI

T01.05–T01.07 añaden `version` y dos errores de uso: flag desconocido y argumento de más.

T01.06 pide que nada se escape al stderr *real*. `os.Stderr` es una variable reasignable: `processStderr(t, f)` la apunta a un fichero de `t.TempDir()`, ejecuta `f` y devuelve lo escrito. `defer` aplaza una llamada hasta que termina la función: `defer func() { os.Stderr = saved }()` restaura el original aunque `f` falle con `t.Fatalf`.

:::from Rust
`defer` hace de `Drop`, a mano y al salir de la función, no del bloque.
:::

1. **Copia `processStderr`** al final de tu `cli_test.go`, y añade `"os"` a su bloque `import`:

```sh
{ echo; git show ref/ch01-s2:internal/driver/cli_test.go | sed -n '/^\/\/ processStderr/,/^}/p'; } >> internal/driver/cli_test.go
```

2. **Escribe T01.05–T01.07 en `TestCLI`.** T01.07 es una tabla de dos filas en un subtest, y T01.04 cambia a `--version, version`.
3. **Míralos fallar.** `go test ./internal/driver/` falla en `usage_lists_commands`, `version_command`, `version_rejects_unknown_flag` y `version_flag_rejects_extra_args`.

#### La tabla de comandos

{{file ch01-s2 internal/driver/session.go}}

Go no tiene constructores: por convención, `NewX` construye un `X`, como `NewSession`.

{{file ch01-s2 internal/driver/commands.go}}

`command` es un struct cuyo campo `Run` es una función. `Run` es un *closure* que captura su comando: lo necesita para informar errores, y no puede buscarlo en `commands`, que se inicializa a partir de `Run`.

`runVersion` tiene tres salidas más: un error de uso, a stderr con 2; la ayuda pedida (`-h`), a stdout con 0; y un stdout que no se puede escribir, a stderr con 1 (`printOut`).

{{diff ch01-s1..ch01-s2 internal/driver/cli.go}}

Ya pasa; compárate:

{{reftest T01.05}}

{{reftest T01.06}}

{{reftest T01.07}}

#### Los hechos del toolchain, fijados a tests

Cada hecho «en Go 1.27» tiene un test en `internal/toolchain/`, que falla si Go lo cambia.

Copia T01.08–T01.10, que miran el `go.mod`, en `internal/toolchain/toolchain_test.go`. `toolchain_test` es un paquete de test externo (el sufijo `_test` en `package`), lo habitual si el test no necesita nada sin exportar.

:::from Rust
Como los tests de integración de `tests/`: compila aparte y solo ve lo exportado.
:::

{{file ch01-s2 internal/toolchain/toolchain_test.go}}

Copia en `facts_test.go` T01.11–T01.16, que ejecutan `go` con `exec.Command`, de `os/exec`, sobre módulos desechables en `t.TempDir()`:

{{file ch01-s2 internal/toolchain/facts_test.go}}

Estos tests pasan desde el principio: el rojo va al revés. Pide `"go 1.27.3"` en la tabla de `TestGoMod/lines`, mira el fallo y deshazlo.

:::more La sintaxis menor de la sesión 2, `NewSession`, el ciclo de inicialización, `os/exec`, y `defer` y `os.Stderr` frente a Rust y Python
- Crea la sesión con `NewSession`, no con el literal `&Session{…}`, aunque compila: ahí se fijarán los campos con un valor inicial distinto de cero. Devuelve un puntero para que la sesión no se copie en cada llamada.
- `Session` es el único valor que recibe cada comando.
- `-h`, `-help` y `--help` no son un error: `Parse` devuelve `flag.ErrHelp`, y `errors.Is(err, flag.ErrHelp)` pregunta si el error es ese valor (o lo envuelve).
- Con el 1 de `printOut`, un script no toma una respuesta perdida por buena. Sus tests y los de la ayuda llegan en la sesión 3.
- En `versionCommand`, `Run` captura un puntero `c` a un `command` nuevo (`&command{…}`), y `return *c` devuelve una copia. Buscar en `commands` desde `Run`: `initialization cycle for commands`, seguido de la cadena que cierra el ciclo, `… runVersion refers to commands`.
- `fs.NArg()` cuenta los argumentos que no son flags.
- `_, _ = fmt.Fprintf(…)` descarta a propósito lo que devuelve escribir en stderr: si falla, no queda dónde decirlo.
- `usage()` saca la línea `commands:` de la tabla: `make([]string, len(commands))` crea un slice de cadenas vacías que el bucle rellena, y `strings.Join` las une, así que la línea nunca se desfasa de la tabla.
- `hasLine` compara líneas enteras: con `strings.Contains`, un `go 1.27.3` olvidado pasaría por `go 1.27`. `no_dependencies` es un subtest aparte: una negación, no una fila más.
- `exec.Command("go", args...)` prepara el proceso, y `args...` expande el slice. `cmd.Dir` es su directorio; `cmd.Env`, el entorno del test más cuatro variables que lo aíslan de la configuración de la máquina. `cmd.Run()` devuelve `nil` si el proceso sale con 0 y, si no, un error.
- `errors.As(r.err, &exit)` busca en el error uno de tipo `*exec.ExitError` (como `downcast_ref` en Rust), con el código de salida: `race_requires_cgo` exige un 2.
- El `String()` de `goRun` hace que `%v` imprima la ejecución entera.
- Las comillas invertidas son cadenas *raw*, sin escapes y de varias líneas.
- **`defer`, Rust y Python.** También hace de *scope guard*; es un `finally` (o un `with`) escrito junto a lo que limpia.
- **`os.Stderr`, Rust.** Una variable global mutable, sin `static mut` ni `unsafe`: cualquier paquete puede reasignarla.
:::

Si te atascas o tu versión se desvía demasiado, copia el fichero de `ref/ch01-s2` y sigue.

Al terminar: tus tests en verde, commit y etiqueta `ch01-s2`; compárate con `ref/ch01-s2`.

```sh
git tag -a ch01-s2 -m "ch01-s2: commands with flag, table tests and toolchain facts"
```

### Sesión 3: la CI en GitHub Actions (≈ 2,5 h)

#### Los tests del workflow

Un workflow no se prueba en local, pero sí lo que dice: T01.17–T01.20 leen `ci.yml` como texto, porque la biblioteca estándar no tiene parser de YAML.

1. **Generaliza el ayudante de la sesión 2,** con los tests en verde como red:

{{diff ch01-s2..ch01-s3 internal/toolchain/toolchain_test.go}}

2. **Escribe `internal/toolchain/ci_test.go`: dos ayudantes y los cuatro subtests de `TestCIWorkflow`.** En YAML, una línea solo significa algo bajo su padre. `readCIWorkflow(t) []ciLine` guarda de cada línea su sangría y su texto; `under(t, lines, path...)` devuelve lo que cuelga de una ruta como `"jobs:", "check:", "env:"`.

Las rutas comparten base: `append(slices.Clone(job), "env:")` copia antes de añadir, o dos podrían pisarse.

:::more Los ayudantes de `ci_test.go`, dos `append` sobre una misma base y el `return ""` tras `t.Fatalf`
- Un slice tiene longitud y capacidad, y mientras quede sitio, `append` escribe en el mismo array. `make([]string, 0, 4)` da longitud 0 y sitio para 4. Con `base := append(make([]string, 0, 4), "jobs:", "check:")`, `append(base, "env:")` y luego `append(base, "steps:")` dan los dos `[jobs: check: steps:]`.
- **`append`, Rust.** El *borrow checker* rechazaría dos vistas de un slice que escriben en el mismo sitio. `slices.Clone` hace de `to_vec()`.
- `readRepoLines` lee cualquier fichero desde la raíz del repositorio (`go test` corre en el directorio del paquete, así que el `go.mod` está en `../../go.mod`), y T01.19 reutiliza `toolchainVersion`, la búsqueda de la línea `toolchain`.
- `readCIWorkflow` salta líneas en blanco y comentarios (un `# GOTOOLCHAIN: local` no cuenta), y quita al texto de cada línea los espacios iniciales.
- `GOTOOLCHAIN: auto` es correcto en «Set up Go» y un error en el job. Por eso `under` busca cada cabecera entre los hijos directos de la anterior, y para el test si falta alguna.
- `slices.Equal` exige exactamente esas líneas, y `slices.Contains`, que una esté entre ellas (T01.18).
- El `return ""` tras `t.Fatalf` es para el compilador: una función con resultado debe acabar en una sentencia terminal, como `return` o `panic` (si no, `missing return`), y Go no sabe que `Fatalf` nunca vuelve, lo que Rust diría con `-> !`.
:::

3. **Míralos fallar.** Sin el workflow (los números de línea son los de la referencia):

```console
$ go test ./internal/toolchain/
--- FAIL: TestCIWorkflow (0.00s)
    --- FAIL: TestCIWorkflow/runs_ci_local (0.00s)
        ci_test.go:73: cannot read .github/workflows/ci.yml: open ../../.github/workflows/ci.yml: no such file or directory
    --- FAIL: TestCIWorkflow/pins_toolchain (0.00s)
        ci_test.go:83: cannot read .github/workflows/ci.yml: open ../../.github/workflows/ci.yml: no such file or directory
    --- FAIL: TestCIWorkflow/go_version_matches_toolchain (0.00s)
        ci_test.go:107: cannot read .github/workflows/ci.yml: open ../../.github/workflows/ci.yml: no such file or directory
    --- FAIL: TestCIWorkflow/triggers (0.00s)
        ci_test.go:120: cannot read .github/workflows/ci.yml: open ../../.github/workflows/ci.yml: no such file or directory
FAIL
FAIL	github.com/aldezex/kilang/internal/toolchain	0.495s
FAIL
```

#### El workflow

{{file ch01-s3 .github/workflows/ci.yml}}

Solo «Set up Go» lleva `GOTOOLCHAIN: auto`: `setup-go` lee la línea `toolchain` solo si `GOTOOLCHAIN` no vale `local`; con `local`, instalaría la 1.27.x más alta.

:::more Cómo ver fallar T01.19 y T01.20, los seis pasos de `ci.yml`, «Trust the checkout», `on`, `permissions` y `timeout-minutes`
1. **Install system packages:** la imagen base no trae `git`, certificados, `gcc` ni `libc6-dev` (cgo, para `-race`). `tar` sí viene, pero se nombra porque `setup-go` desempaqueta Go con él. `-o Acquire::Retries=3` reintenta una descarga que falla.
2. **Check out the repository,** con `fetch-depth: 0`: historia y etiquetas.
3. **Set up Go:** `actions/setup-go` instala Go. Con `local`, leería la línea `go`, y la 1.27.x más alta algún día no será la 1.27.3. La regla sale de su código, y la documenta `docs/toolchain.md` de la referencia; `setup-go` vuelve a exportar `local` a los pasos siguientes.
4. **Check the Go version** detiene el job si el Go no es exactamente `go1.27.3`; el literal cambia con el `go.mod`, y T01.19 lo vigila.
5. **Trust the checkout:** el checkout es del usuario del runner, no del `root` del contenedor, y `git`, al que `go build` pregunta el commit, rechaza un repositorio ajeno.
6. **Run ci-local:** el mismo script que ejecutas tú.

Lo dispara cada push a `main`, cada etiqueta `ch*` o `m*` y cada pull request. El job `check` corre dentro de `ubuntu:26.04`, con `GOTOOLCHAIN: local` y `GOFLAGS` vacío. `permissions: contents: read` deja al token del job solo leer el repositorio. `timeout-minutes: 15` corta un job colgado a los 15 minutos y no a las seis horas que permite GitHub.

T01.19 y T01.20 fallan al revés: cambia un momento el `go1.27.3` de «Check the Go version» por `go1.27.4` y borra `pull_request: {}`; fallan `go_version_matches_toolchain` y `triggers`, con las líneas encontradas y las esperadas. Deshaz los cambios.
:::

Haz commit y push a `main`. En *Actions*, «Set up Go» registra `Setup go version spec 1.27.3` (leyó la línea `toolchain`) y «Run ci-local» termina con `verify: OK`. Compárate:

{{reftest T01.17}}

{{reftest T01.18}}

{{reftest T01.19}}

{{reftest T01.20}}

#### El binario, probado como proceso

Ningún test de `driver` ejecuta `main`: con `os.Args` en lugar de `os.Args[1:]`, T01.01–T01.03 seguirían en verde. Lo cierra T01.21:

{{file ch01-s3 cmd/ki/main_test.go}}

`package main` no se importa, y `main` termina con `os.Exit`: el test relanza su binario con `KI_TEST_RUN_MAIN=1`, y entonces `TestMain` llama a `main`. Si un paquete de test define `TestMain`, `go test` la llama en lugar de ejecutar los tests; `m.Run()` los ejecuta y devuelve el código.

El rojo, al revés: cambia un momento `os.Args[1:]` por `os.Args` en `main.go`.

```console
$ go test ./cmd/ki/
--- FAIL: TestBinary (0.00s)
    --- FAIL: TestBinary/streams_and_exit_codes (0.00s)
        main_test.go:78: version: exit code 2, want 0
        main_test.go:81: version: stdout "", want "ki 0.0.1 (go1.27.3)\n"
        main_test.go:84: version: stderr "usage: ki <command> [args]\ncommands: --version, version\n", want empty
FAIL
FAIL	github.com/aldezex/kilang/cmd/ki	0.007s
FAIL
```

{{reftest T01.21}}

:::more `os.Args`, `os.Environ` y el binario de test como `ki`
Con `os.Args`, `ki` toma la ruta del binario por un comando desconocido; de las tres filas, solo `version` lo detecta. `runKi` lanza `os.Executable()` con `KI_TEST_RUN_MAIN=1` y pasa el entorno del padre (`os.Environ()`) más la variable; en el hijo la ve `os.Getenv`. El código de salida se obtiene con `errors.As`, como en `race_requires_cgo`.

Frente a Rust: Cargo da a los tests de integración la ruta del binario en `env!("CARGO_BIN_EXE_<nombre>")`. `go test` no construye `ki` aparte, así que el binario de test hace de `ki`.
:::

#### Ayuda y errores de escritura

Escribe en `TestCLI` T01.22 y T01.23: la ayuda y el fallo al escribir en stdout. T01.23 necesita un `io.Writer` que falle siempre, y basta este `Write` (`errors.New("disk full")` pide `"errors"` en el `import`):

```go
type failingWriter struct{ err error }

func (w failingWriter) Write([]byte) (int, error) { return 0, w.err }
```

El código ya existe: el rojo va al revés; deshaz cada cambio:

- Quita el `if errors.Is(err, flag.ErrHelp) { … }` de `runVersion` y su import `"errors"`: `version_help_prints_usage` falla con `got 2` y `ki --version: flag: help requested` en stderr.
- Cambia el `return 1` de `printOut` por `return 0`: `version_reports_write_error` falla con `got 0 …, want 1 …`.

{{reftest T01.22}}

{{reftest T01.23}}

Si te atascas o tu versión se desvía demasiado, copia el fichero de `ref/ch01-s3` y sigue.

Al terminar: tus tests en verde, commit y etiqueta `ch01-s3`; compárate con `ref/ch01-s3`.

```sh
git tag -a ch01-s3 -m "ch01-s3: CI on GitHub Actions with cross-compile checks"
git push origin main ch01-s1 ch01-s2 ch01-s3
```

## Ejecútalo

Con la sesión 3 terminada (tiempos y `(cached)`, como en la sesión 1):

```console
$ ci/ci-local.sh
ok  	github.com/aldezex/kilang/cmd/ki	0.006s
ok  	github.com/aldezex/kilang/internal/driver	0.002s
ok  	github.com/aldezex/kilang/internal/toolchain	0.490s
ok  	github.com/aldezex/kilang/cmd/ki	2.036s
ok  	github.com/aldezex/kilang/internal/driver	1.011s
ok  	github.com/aldezex/kilang/internal/toolchain	1.487s
verify: OK
$ build/bin/ki --version; echo $?
ki 0.0.1 (go1.27.3)
0
$ build/bin/ki; echo $?
usage: ki <command> [args]
commands: --version, version
2
```

## Ejercicios

:::exercise
**1. `ki help`.** Añade un comando `help` que imprima la lista de comandos por stdout y salga con 0, con su test. Como `version`, debe rechazar un argumento de más y atender `-h`. Una pista: registrarlo en el literal de `commands` crea un ciclo de inicialización, porque `help` lee `commands`; Go ejecuta las funciones `func init()` de un paquete después de dar valor a sus variables.
:::

:::solution
`help` imprime el uso por stdout y con 0: pedida, la ayuda es la salida, no un error. El test:

```go
t.Run("help_lists_commands_on_stdout", func(t *testing.T) {
	code, out, err := runCLI(t, "help")
	want := "usage: ki <command> [args]\ncommands: --version, version, help\n"
	if code != 0 || out != want || err != "" {
		t.Fatalf("got %d %q %q, want 0 %q \"\"", code, out, err, want)
	}
})
```

Si `help` escribiera por stderr, `err` no estaría vacío; si devolviera 2, falla el código. Añade la fila `{"help", "help", "ki help: unexpected argument \"extra\"\nusage: ki help\n"}` a `version_flag_rejects_extra_args`, y cambia la lista de `usage_lists_commands` a `--version, version, help`.

En el literal de `commands`, `helpCommand()` da `initialization cycle for commands` (`… usage refers to commands`); en una función `init`, `commands` ya tiene su valor y `append` añade la fila. Para no copiar el parseo de `runVersion`, con su `flag.ErrHelp`, sácalo a un ayudante que responda él mismo si la orden pide ayuda o está mal:

```go
func checkNoArgs(s *Session, c *command, args []string) (int, bool) {
	fs := flag.NewFlagSet(c.Name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return printOut(s, c, "usage: "+c.Usage+"\n"), false
		}
		return usageError(s, c, err), false
	}
	if fs.NArg() > 0 {
		return usageError(s, c, fmt.Errorf("unexpected argument %q", fs.Arg(0))), false
	}
	return 0, true
}

func init() {
	commands = append(commands, helpCommand())
}

func helpCommand() command {
	c := &command{Name: "help", Usage: "ki help"}
	c.Run = func(s *Session, args []string) int {
		if code, ok := checkNoArgs(s, c, args); !ok {
			return code
		}
		return printOut(s, c, usage())
	}
	return *c
}
```

`runVersion` queda en el mismo `if code, ok := checkNoArgs(s, c, args); !ok { return code }` más su `return printOut(…)`; T01.22 y T01.23 siguen en verde. La referencia no conserva `help`: si lo dejas en tu repo, añádelo cada vez que el libro cambie la lista de `usage_lists_commands`.
:::

:::exercise
**2. Leer un fallo.** Quita el `\n` del formato de `runVersion` y ejecuta `go test -v -run 'TestCLI/version_prints_version_and_go' ./internal/driver/`, y después lo mismo con `-json` en lugar de `-v`. ¿Para quién está pensada cada salida?
:::

:::solution
Con `-v` (con tus ayudantes en otro sitio del fichero, el número de línea será otro):

```console
=== RUN   TestCLI
=== RUN   TestCLI/version_prints_version_and_go
    cli_test.go:77: got 0 "ki 0.0.1 (go1.27.3)" "", want 0 "ki 0.0.1 (go1.27.3)\n" ""
--- FAIL: TestCLI (0.00s)
    --- FAIL: TestCLI/version_prints_version_and_go (0.00s)
FAIL
FAIL	github.com/aldezex/kilang/internal/driver	0.002s
FAIL
```

`=== RUN` abre cada test y `--- FAIL` lo cierra; la sangría anida el subtest en `TestCLI`, que falla porque falló un hijo. Los `%q` muestran las cadenas con escapes: solo falta `\n`. Es una salida para personas.

Con `-json`, cada evento es un objeto por línea, con su `Action` y el nombre completo en `Test`; el evento del fallo es:

```text
{"Time":"…","Action":"fail","Package":"github.com/aldezex/kilang/internal/driver","Test":"TestCLI/version_prints_version_and_go","Elapsed":0}
```

Es para programas: `bookx` cruza con las tarjetas la lista de tests que pasan según `go test -json`, y por eso las filas de tabla no usan `t.Run`, que haría de cada fila un test.
:::

:::exercise
**3. Una carrera de datos.** Crea un módulo de prueba bajo `build/`, que ni git ni `./...` ven (`mkdir -p build/race && cd build/race && go mod init example.com/race`). Una goroutine es una función que corre a la vez que las demás; con un `sync.WaitGroup`, `wg.Go(f)` lanza `f` en una y `wg.Wait()` espera a todas. Esta función incrementa un contador desde dos goroutines sin sincronizar; guárdala en `count.go`:

```go
package race

import "sync"

// CountTwice adds 1 to a counter from two goroutines at once.
func CountTwice() int {
	n := 0
	var wg sync.WaitGroup
	wg.Go(func() { n++ })
	wg.Go(func() { n++ })
	wg.Wait()
	return n
}
```

Escribe un test que espere 2 y ejecuta `go test` y `go test -race`. ¿Por qué pasa sin `-race`?
:::

:::solution
El test, en `count_test.go`:

```go
func TestCountTwice(t *testing.T) {
	if got := CountTwice(); got != 2 {
		t.Fatalf("CountTwice() = %d, want 2", got)
	}
}
```

`go test` da `ok`. `go test -race` sale con 1 (rutas abreviadas; direcciones, números de goroutine y cuál de las dos líneas sale primero varían):

```text
WARNING: DATA RACE
Read at 0x00c000018298 by goroutine 10:
      …/race/count.go:10 +0x2e
Previous write at 0x00c000018298 by goroutine 9:
      …/race/count.go:9 +0x44
…
    testing.go:1867: race detected during execution of test
```

Sin `-race` pasa porque una carrera no garantiza un resultado malo: dos incrementos diminutos rara vez se pisan, pero con otra carga u otra máquina sí. Protegiendo `n` con un `sync.Mutex` (`mu.Lock(); n++; mu.Unlock()`), `go test -race` da `ok`.
:::

:::exercise
**4. `-shuffle`.** Ejecuta `go test -shuffle=on ./...` tres veces, y después `go test -v -shuffle=on ./internal/toolchain/`, que tiene cuatro funciones `TestXxx`. ¿Qué cambia? ¿Qué garantiza que pasen las tres?
:::

:::solution
Sin `-v` ves las mismas líneas `ok`, ninguna `(cached)`. Con `-v`, la primera línea es la semilla (`-test.shuffle 1793059846188786862`) y el orden de las cuatro funciones cambia de una ejecución a otra. Los subtests no se barajan: los seis de `TestToolchainFacts` salen siempre en el orden del fichero.

Que pase con varios órdenes indica, para esos órdenes, que ningún test depende del estado que deja otro. Si uno falla, `-shuffle=<semilla>` lo repite.
:::

:::exercise
**5. Una matriz de sistemas.** Añade a `ci.yml` una matriz con `ubuntu-latest` y `macos-latest`, y razona por qué el libro no la necesita.
:::

:::solution
El job `check` no admite la matriz: para `jobs.<job_id>.container`, la sintaxis de workflows dice «If you are using GitHub-hosted runners, you must use an Ubuntu runner». Hace falta otro job, sin contenedor, `apt-get` ni «Trust the checkout»:

```yaml
  check-os:
    strategy:
      matrix:
        os: [ubuntu-latest, macos-latest]
    runs-on: ${{ matrix.os }}
    env:
      GOTOOLCHAIN: local
      GOFLAGS: ""
    steps:
      - uses: actions/checkout@v7
        with:
          fetch-depth: 0
      - uses: actions/setup-go@v7
        env:
          GOTOOLCHAIN: auto
        with:
          go-version-file: go.mod
          cache: false
      - run: test "$(go version | awk '{print $3}')" = go1.27.3
      - run: ci/ci-local.sh
```

Por qué el libro no lo necesita: los destinos de Ki son linux/amd64, windows/amd64 y linux/arm64, y los dos últimos ya se compilan en cada push; macOS no es un destino. Para vigilar la deriva hacia macOS basta otra compilación cruzada, `GOOS=darwin GOARCH=arm64 go build -o /dev/null ./cmd/ki`, que hoy compila.
:::

## Preguntas de repaso

:::review
¿Por qué `Main` recibe `stdin`, `stdout` y `stderr` y devuelve un `int`, en lugar de usar `os.Stdout` y llamar a `os.Exit`?
:::

:::answer
Para que los tests la ejecuten en proceso: le pasan dos `strings.Builder`, comparan lo escrito, y el binario de test sobrevive. `os.Exit` queda en `cmd/ki/main.go`, que prueba T01.21.
:::

:::review
¿Por qué el `go.mod` dice `go 1.27` y `toolchain go1.27.3`, y no el `go 1.27.3` de `go mod init`?
:::

:::answer
`go` es la versión mínima del lenguaje, y Ki necesita la 1.27, no el parche 3; `toolchain` dice con qué toolchain se verificó, y `TestRuntimeVersionMatchesToolchainLine` comprueba que ejecuta los tests. Con `go 1.27.3`, un Go 1.27.2 se negaría a cargar el módulo sin que el lenguaje lo justifique.
:::

:::review
¿Por qué el repositorio y la CI usan `GOTOOLCHAIN=local`, si `auto` descargaría el toolchain correcto?
:::

:::answer
Porque una descarga silenciosa cambiaría el compilador sin que nadie lo decida. Con `local`, si el Go instalado no es el esperado, `ci-local.sh` y «Check the Go version» paran. Actualizar Go es una decisión con su commit.
:::

:::review
¿Por qué los comandos usan `ContinueOnError` y `SetOutput(io.Discard)` en su `FlagSet`?
:::

:::answer
Con `ExitOnError`, `Parse` llamaría a `os.Exit(2)` dentro de `Main` y mataría el test; con `ContinueOnError` el error vuelve a `ki`. Sin `io.Discard`, `flag` escribiría su ayuda en el stderr real del proceso, lo que `version_rejects_unknown_flag` comprueba con `processStderr`.
:::

:::review
¿Por qué `ci-local.sh` compila `ki` para Windows y ARM64 si nadie ejecuta esos binarios hasta los capítulos finales?
:::

:::answer
Para atrapar la deriva cuando es barata: `syscall.Kill` compila en tu máquina y falla en Windows, y sin la comprobación el fallo aparecería en el capítulo 66, bajo sesenta capítulos de código.
:::

:::review
¿Por qué los tests pasan `&buf` a donde se espera un `io.Writer` cuando `buf` es un `bytes.Buffer`, y por qué un `strings.Builder` recién declarado con `var` ya sirve?
:::

:::answer
Porque `Write` de `bytes.Buffer` tiene receptor puntero: el método es de `*bytes.Buffer`, así que solo `&buf` es un `io.Writer`. Y el valor cero de `strings.Builder` es un `Builder` vacío y utilizable: no hay constructor que olvidar.
:::

## Resumen

- `go.mod` declara módulo, versión mínima y toolchain preferido; `GOTOOLCHAIN=local` no descarga otro.
- Un directorio es un paquete; la mayúscula exporta; `internal/` limita quién importa.
- `io.Writer` es cualquier cosa con `Write`; `error` es un valor.
- `go build` compila y enlaza a partir de los `import`.
- `main` se prueba relanzando el binario de test.
- La CLI es una tabla de comandos; los errores de uso, a stderr con 2; la ayuda pedida, a stdout con 0.
- `ki --version` saca la versión de Go de `debug.ReadBuildInfo`, y la suya, de una constante.
- `ci/ci-local.sh` es la puerta de cada sesión, y la CI lo ejecuta igual.

## Go en el trabajo

**`gofmt` como contrato social.** Rob Pike, en Gopherfest SV (2015): «Gofmt's style is no one's favorite, yet gofmt is everyone's favorite».

**Nombres sin tartamudeo.** `driver.Main`, no `driver.DriverMain`: `bufio` llama `Reader` a su lector, «not `BufReader`, because users see it as `bufio.Reader`» (Effective Go).

**`go vet` y `-race` en toda CI.** Según su documentación, en un programa típico `-race` cuesta 5–10 veces más memoria y 2–20 más tiempo, y «only finds races that happen at runtime»: vale lo que tu cobertura.

**La compilación cruzada como control de deriva.** Un runner de Linux compila para Windows, macOS y ARM64 en segundos; no sustituye a ejecutar los tests allí.
