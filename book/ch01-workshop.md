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

Cada etapa cuelga de `ki`: por eso empiezas por el driver, que recibe la línea de órdenes.

### Qué hace `go build`

Go se instala como un único árbol, el *toolchain*: compilador, enlazador, biblioteca estándar y el comando `go`.

Un paquete es un directorio. `go build` lee los `import`, compila lo que no esté en la caché y enlaza un ejecutable con su runtime dentro.

Un import o una variable local sin usar son errores, no avisos.

### `go.mod`: módulo, versión y toolchain

Un *módulo* es un árbol de paquetes con un `go.mod` en la raíz:

- `module` da el prefijo de todo import propio. La referencia, `kilang-ref`, el repositorio con el que te comparas, usa el mismo, y sus ficheros encajan en tu árbol.
- `go 1.27` es la versión **mínima** del lenguaje: un toolchain más viejo no carga el módulo.
- `toolchain go1.27.3` es el **preferido**.
- `ignore ./build` saca `build/` entero de `./...`, todos los paquetes.

El repositorio y la CI usan `GOTOOLCHAIN=local`: nada cambia el compilador en silencio.

:::more Qué hace `GOTOOLCHAIN`
Con `auto`, por defecto, si el `go.mod` pide un Go más nuevo que el instalado, `go` lo busca o **lo descarga** y lo ejecuta. Con `local` usa siempre el instalado.

A cambio, `local` deja de imponer la línea `toolchain`: la vigilan `ci-local.sh` y un test.
:::

### Paquetes y visibilidad

Un paquete `main` con una función `main` es un ejecutable; cualquier otro, una biblioteca. `cmd/ki/` es el de `ki`, y el compilador vive bajo `internal/`.

La primera letra decide la visibilidad: otro paquete ve `driver.Version`, *exportado* (`pub`), pero no `goVersion`.

`internal/` hace de `pub(crate)`, por directorios: lo que cuelga de él solo lo importa el árbol que lo contiene. Así `internal/driver` no es una API pública que luego no puedas cambiar.

### Lo básico del lenguaje

| Concepto | Go | Rust | TypeScript | Python |
|---|---|---|---|---|
| Tipos básicos | `int` (64 bits en amd64), `int64`, `string`, `bool`, `byte` | `i64`, `String`, `bool`, `u8` | `number`, `string`, `boolean` | `int`, `str`, `bool` |
| Función | `func div(a, b int) int` | `fn div(a: i64, b: i64) -> i64` | `function div(a: number, b: number): number` | `def div(a, b):` |
| Retornos múltiples | `func divmod(a, b int) (int, int)`; `q, r := divmod(17, 5)` | una tupla | un array `[q, r]` | una tupla |
| Bucle clásico | `for i := 0; i < 3; i++ { … }` | `for i in 0..3` | `for (let i = 0; i < 3; i++)` | `for i in range(3)` |
| `range` | `for i := range 3` (0, 1, 2); `for i, x := range xs` (`_` descarta) | `for (i, x) in xs.iter().enumerate()` | `xs.forEach((x, i) => …)` | `for i, x in enumerate(xs)` |
| `switch` | sin *fall-through*; `fallthrough` lo pide | `match`, exhaustivo | `switch` con `break` | `match` |

- `x := 3` declara e infiere el tipo.
- `for` es el único bucle: también `for n < 3 { … }` y `for { … }`.
- Toda variable empieza con su *valor cero*: `0`, `""`, `false` o `nil`, el de punteros, slices, mapas, funciones e interfaces.
- Un `switch` sin expresión lleva una condición en cada `case`.

### Lo justo para el driver

- **Un puntero a struct es el mismo valor, no una copia.** `&x` lo obtiene, y cambiar un campo por él cambia el original. El recolector libera la memoria cuando ningún puntero la alcanza.
- **Un método es una función con receptor.** `func (p Point) String() string` es de `Point`; con receptor puntero, `func (b *Buffer) Write(…)` es de `*Buffer`.
- **Un tipo satisface una interfaz por sus métodos,** sin declararlo. `%v` imprime cualquier valor en su forma por defecto; con `String() string`, lo que devuelve.
- **`io.Writer` e `io.Reader` son «cualquier cosa que sepa escribir o leer».** `driver.Main` escribe en `os.Stdout` o, en los tests, en un `strings.Builder`.
- **`error` es un valor:** el último resultado de una función que puede fallar, `nil` si no falló. Se comprueba con `if err != nil { … }`. No hay excepciones.

:::from Rust
`io.Writer` es `std::io::Write` sin `impl`. `(T, error)` hace de `Result<T, E>`, sin obligarte a mirarlo.
:::

### `gofmt` y `go vet`

`gofmt` reescribe el código en el único estilo de Go, y `ci/ci-local.sh` exige que `gofmt -l` no liste nada.

`go vet` busca errores que compilan, como un `%d` con un `string`.

### `testing`: tests, subtests y tablas

Un test es una `func TestXxx(t *testing.T)` en un fichero `_test.go`. No hay `assert_eq!`: comparas con un `if` y escribes el mensaje. `t.Errorf` anota el fallo y sigue; `t.Fatalf` lo anota y termina.

- **Subtests.** `t.Run("algun_caso", func(t *testing.T) { … })` crea un test hijo, que se informa y selecciona por separado. Su nombre es un literal: `bookx`, que verifica el libro, lo enlaza con su tarjeta, la ficha del test.
- **Closures.** Esa `func` sin nombre ve las variables de la función que la contiene.
- **Ayudas.** `t.Helper()` marca un ayudante: el fallo señala la línea que lo llamó. `t.Cleanup(f)` corre `f` al terminar el test. `t.TempDir()` da un directorio que se borra solo.
- **Tests de tabla.** La tabla es un *slice* (una vista de un array) de structs anónimos. Sus filas van *dentro de un subtest*, sin `t.Run`: así una tarjeta cubre el subtest con su tabla, y cada fallo nombra su fila. `%q` imprime una cadena entre comillas, con escapes:

```go
tests := []struct{ name, want string }{
	{"go_line", "go 1.27"},
}
for _, tc := range tests {
	if !hasLine(lines, tc.want) {
		t.Errorf("%s: go.mod has no line %q", tc.name, tc.want)
	}
}
```

### `flag` y el patrón de subcomandos

Ki solo usa la biblioteca estándar, y su `flag` no trae subcomandos. El patrón es una tabla de comandos y un `flag.FlagSet` por comando: `Main` busca la primera palabra, y el comando parsea el resto.

Cada `FlagSet` usa `flag.ContinueOnError` y `SetOutput(io.Discard)`: `flag` no termina el proceso ni escribe en su stderr, y `ki` informa del error.

### Por qué `Version` es una constante

`debug.ReadBuildInfo()` devuelve lo que `go build` graba en cada binario, y de ahí sale la versión de Go.

La de `ki` no puede salir de ahí: en Go 1.27 es `"(devel)"` fuera de git y, dentro, sin etiqueta semver (`ch01-s1` no lo es), una pseudo-versión que cambia con cada commit.

### TDD rojo-verde y tests golden

- **Rojo.** Escribes el test y lo ves fallar, por la razón esperada: uno que nunca has visto en rojo puede pasar por accidente.
- **Verde.** El código mínimo que lo hace pasar.
- **Refactor.** Limpias con los tests como red.

Un test fuerte compara valores exactos y mira los bordes. Pregúntate qué implementación equivocada, un *mutante*, deja pasar; si es «cualquiera», no sirve.

Un *test golden* compara la salida de un programa con la esperada, guardada en un fichero; en Ki, en la cabecera de cada programa (capítulo 3).

### GitHub Actions y compilación cruzada

GitHub Actions ejecuta *workflows*, ficheros YAML en `.github/workflows/`, ante un evento. Cada *job* corre en un *runner*, una máquina virtual nueva, y puede ir dentro de un **contenedor** Docker.

La **compilación cruzada** detecta la *deriva*, el código que no compila en otro destino: `syscall.Kill` falla en Windows. Sin cgo (llamadas a C), bastan dos variables: `GOOS=windows GOARCH=amd64 go build ./cmd/ki` produce un `.exe` desde Linux.

## Tests primero

{{cards}}

Úsalas así: escribe el test de la tarjeta, míralo fallar, implementa lo justo para el verde, y solo entonces despliega su referencia.

## Implementación

### Sesión 1: el módulo, `ki --version` y `ci-local.sh` (≈ 3 h)

#### Puesta en marcha

1. **Comprueba el toolchain.**

```console
$ go version
go version go1.27.3 linux/amd64
```

2. **Instala lo que necesita `-race`,** el detector de carreras de datos: compilador de C y cabeceras.

```sh
sudo apt install gcc libc6-dev
```

3. **Crea el repositorio en `~/kilang`, nunca bajo `/mnt/c`:** WSL recomienda el sistema de ficheros de Linux por velocidad.

```sh
mkdir ~/kilang && cd ~/kilang
git init -b main
go mod init github.com/aldezex/kilang
```

4. **Edita el `go.mod`.** `go mod init` escribe `go 1.27.3`, que exige el parche 3; Ki solo necesita la 1.27:

{{file ch01-s1 go.mod}}

5. **Añade este `.gitignore`.** Sin `/build/`, un `git add -A` subiría el binario.

{{file ch01-s1 .gitignore}}

6. **Añade una licencia MIT** con la CLI de GitHub: `gh repo license view mit > LICENSE`, y rellena `[year]` y `[fullname]`.
7. **Publica.**

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

Para compararte o recuperar un fichero:

```sh
git diff ref/ch01-s1 -- internal/driver/
git restore --source=ref/ch01-s1 -- <fichero>
```

#### Los tests, en rojo

Tu primer test en Go, T01.01–T01.04, va completo, en `internal/driver/cli_test.go`. Es `package driver`, el paquete que prueba, así que ve sus nombres sin exportar.

{{file ch01-s1 internal/driver/cli_test.go}}

- `args ...string` es un parámetro variádico; dentro es un `[]string`.
- `(code int, stdout, stderr string)` son resultados con nombre, que empiezan con su valor cero.
- `var out strings.Builder` empieza en su valor cero, que ya sirve. Su `Write` tiene receptor puntero: de ahí `&out`.

El primer rojo suele ser un error de compilación:

```console
$ go test ./internal/driver/
# github.com/aldezex/kilang/internal/driver [github.com/aldezex/kilang/internal/driver.test]
internal/driver/cli_test.go:19:9: undefined: Main
internal/driver/cli_test.go:45:19: undefined: Version
FAIL	github.com/aldezex/kilang/internal/driver [build failed]
FAIL
```

#### El código

`main` solo conecta `driver.Main` con el proceso:

{{file ch01-s1 cmd/ki/main.go}}

`os.Args[1:]` son los argumentos sin el nombre del programa.

{{file ch01-s1 internal/driver/version.go}}

`if bi, ok := …; ok { … }` es un `if` con sentencia inicial: `bi` y `ok` solo existen dentro.

{{file ch01-s1 internal/driver/cli.go}}

`fmt.Fprintf` escribe en el `io.Writer` que le pases.

#### `ci/ci-local.sh`

El script que decide el verde de una sesión.

{{file ch01-s1 ci/ci-local.sh}}

Hazlo ejecutable (`chmod +x ci/ci-local.sh`) y ejecútalo. Lo ya pasado sin cambios sale `(cached)`, salvo con `-count=1`.

```console
$ ci/ci-local.sh
?   	github.com/aldezex/kilang/cmd/ki	[no test files]
ok  	github.com/aldezex/kilang/internal/driver	0.002s
?   	github.com/aldezex/kilang/cmd/ki	[no test files]
ok  	github.com/aldezex/kilang/internal/driver	1.014s
verify: OK
```

Si te atascas o tu versión se desvía demasiado, copia el fichero de `ref/ch01-s1` y sigue.

Al terminar: tus tests en verde, commit y etiqueta anotada `ch01-s1`; compárate con `ref/ch01-s1`.

```sh
git tag -a ch01-s1 -m "ch01-s1: module, ki --version and ci-local"
```

### Sesión 2: comandos con `flag`, tests de tabla y los hechos del toolchain (≈ 2,5 h)

#### Los tests de la CLI

Añades `version` y sus errores de uso. El error de un flag no debe escaparse al stderr *real*. `os.Stderr` es una variable reasignable: `processStderr(t, f)` la apunta a un fichero, ejecuta `f` y devuelve lo escrito.

`defer` aplaza una llamada hasta que termina la función, no el bloque: un `Drop` a mano. Aquí restaura `os.Stderr` aunque `f` falle con `t.Fatalf`:

```go
defer func() { os.Stderr = saved }()
```

1. **Copia `processStderr`** al final de tu `cli_test.go`, y añade `"os"` a su `import`:

```sh
{ echo; git show ref/ch01-s2:internal/driver/cli_test.go | sed -n '/^\/\/ processStderr/,/^}/p'; } >> internal/driver/cli_test.go
```

2. **Escribe T01.05–T01.07.** T01.04 cambia a `--version, version`; su tarjeta da la lista final.
3. **Míralos fallar.** `go test ./internal/driver/` falla en T01.04–T01.07.

#### La tabla de comandos

{{file ch01-s2 internal/driver/session.go}}

Go no tiene constructores: por convención, `NewX` construye un `X`; aquí, un puntero, para no copiar la sesión.

{{file ch01-s2 internal/driver/commands.go}}

`command` guarda una función en `Run`, que informa de errores con su comando. No puede buscarlo en `commands`, que se inicializa a partir de `Run`: Go rechaza ese ciclo. Por eso `Run` es un *closure* que captura el puntero `c`; `return *c` devuelve una copia.

`runVersion` tiene tres salidas más:

| Salida | Flujo | Código |
|---|---|---|
| Error de uso | stderr | 2 |
| Ayuda pedida (`-h`) | stdout | 0 |
| Fallo al escribir en stdout | stderr | 1 |

`errors.Is(err, flag.ErrHelp)` pregunta si el error es ese valor (o lo envuelve).

{{diff ch01-s1..ch01-s2 internal/driver/cli.go}}

`usage()` saca la línea `commands:` de la tabla, así que nunca se desfasa. `make([]string, n)` crea un slice de `n` cadenas vacías.

{{reftest T01.05}}

{{reftest T01.06}}

{{reftest T01.07}}

#### Los hechos del toolchain

Cada hecho «en Go 1.27» tiene un test en `internal/toolchain/`, que falla si Go lo cambia.

1. **Copia T01.08–T01.10 en `internal/toolchain/toolchain_test.go`.** `toolchain_test` es un paquete de test externo, lo habitual si no necesitas nada sin exportar: como `tests/` en Rust.

{{file ch01-s2 internal/toolchain/toolchain_test.go}}

2. **Copia T01.11–T01.16 en `facts_test.go`.** Ejecutan `go` sobre módulos desechables. Las comillas invertidas son cadenas *raw*, sin escapes y de varias líneas.

{{file ch01-s2 internal/toolchain/facts_test.go}}

:::more Ejecutar un programa desde un test
- `exec.Command("go", args...)`, de `os/exec`, prepara el proceso, y `args...` expande el slice.
- `cmd.Dir` es su directorio; `cmd.Env`, el entorno del test más cuatro variables que lo aíslan de la configuración de la máquina.
- Un `bytes.Buffer` guarda lo que le escriben, y `cmd.Stdout = &stdout` le conecta la salida del proceso.
- `cmd.Run()` devuelve `nil` si el proceso sale con 0 y, si no, un error.
- `errors.As(r.err, &exit)` busca en el error uno de tipo `*exec.ExitError`, con el código de salida (como `downcast_ref` en Rust).
:::

3. **El rojo va al revés:** ya pasan. Pide `"go 1.27.3"` en la tabla de `toolchain_test.go`, mira el fallo y deshazlo.

Si te atascas o tu versión se desvía demasiado, copia el fichero de `ref/ch01-s2` y sigue.

Al terminar: tus tests en verde, commit y etiqueta `ch01-s2`; compárate con `ref/ch01-s2`.

```sh
git tag -a ch01-s2 -m "ch01-s2: commands with flag, table tests and toolchain facts"
```

### Sesión 3: la CI en GitHub Actions (≈ 2,5 h)

#### Los tests del workflow

Un workflow no se prueba en local: estos tests leen `ci.yml` como texto, porque la biblioteca estándar no trae parser de YAML.

1. **Generaliza el ayudante de la sesión 2:**

{{diff ch01-s2..ch01-s3 internal/toolchain/toolchain_test.go}}

El `return ""` tras `t.Fatalf` es para el compilador: una función con resultado acaba en sentencia terminal, y él no sabe que `Fatalf` nunca vuelve.

2. **Copia los dos ayudantes y escribe T01.17–T01.20.** En YAML, una línea solo significa algo bajo su padre: por eso guardan su sangría. Clona cada ruta de `under` antes de alargarla: comparten base.

```sh
git show ref/ch01-s3:internal/toolchain/ci_test.go | sed '/^func TestCIWorkflow/,$d' > internal/toolchain/ci_test.go
```

:::more Cómo usar los ayudantes de `ci_test.go`
`readCIWorkflow(t) []ciLine` guarda de cada línea su sangría y su texto. `under(t, lines, "jobs:", "check:", "env:")` devuelve lo que cuelga de esa ruta.

`under` busca cada cabecera entre los hijos directos de la anterior, y para el test si falta alguna: `GOTOOLCHAIN: auto` es correcto en «Set up Go» y un error en el job.

`slices.Equal` exige exactamente esas líneas, y `slices.Contains`, que una esté entre ellas.
:::

:::more Dos `append` sobre una misma base
Un slice tiene longitud y capacidad, y mientras quede sitio, `append` escribe en el mismo array. `make([]string, 0, 4)` da longitud 0 y sitio para 4. Con `base := append(make([]string, 0, 4), "jobs:", "check:")`, `append(base, "env:")` y luego `append(base, "steps:")` dan los dos `[jobs: check: steps:]`.

Por eso las rutas se copian antes: `append(slices.Clone(job), "env:")`. `slices.Clone` hace de `to_vec()`, y el *borrow checker* de Rust rechazaría esas dos vistas.
:::

3. **Míralos fallar.** Sin el workflow (numeración de la referencia):

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

1. **Añade el workflow.**

{{file ch01-s3 .github/workflows/ci.yml}}

:::note
**Fe de erratas.** El comentario de `apt` se equivoca: ya reintenta tres veces por defecto, y `Acquire::Retries=3` no cambia nada.
:::

:::more Las decisiones de `ci.yml`
**El contenedor.** `ubuntu:26.04`, la Ubuntu de tu WSL, no se mueve como `ubuntu-latest`.

**«Set up Go»** es el único con `GOTOOLCHAIN: auto`: `setup-go` lee la línea `toolchain` solo si `GOTOOLCHAIN` no vale `local`; con `local`, instalaría la 1.27.x más alta.

**«Trust the checkout»** hace falta porque el checkout es del usuario del runner, no del `root` del contenedor, y `git`, al que `go build` pregunta el commit, rechaza un repositorio ajeno.
:::

2. **Ve el rojo al revés.** Cambia el `go1.27.3` de «Check the Go version» por `go1.27.4` y borra `pull_request: {}`: fallan dos tests. Deshaz los cambios.
3. **Haz commit y push a `main`.** En *Actions*, «Set up Go» registra `Setup go version spec 1.27.3` y «Run ci-local» termina con `verify: OK`.

{{reftest T01.17}}

{{reftest T01.18}}

{{reftest T01.19}}

{{reftest T01.20}}

#### El binario, probado como proceso

Ningún test de `driver` ejecuta `main`. Copia T01.21 en `cmd/ki/main_test.go`:

{{file ch01-s3 cmd/ki/main_test.go}}

`package main` no se importa, y `main` termina con `os.Exit`: `runKi` relanza el binario de test con `KI_TEST_RUN_MAIN=1`, y entonces `TestMain` llama a `main`.

Si un paquete de test define `TestMain`, `go test` la llama en lugar de ejecutar los tests; `m.Run()` los ejecuta y devuelve el código.

Cambia un momento `os.Args[1:]` por `os.Args` en `main.go`.

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

#### Ayuda y errores de escritura

Escribe T01.22 y T01.23 en `cli_test.go`. El segundo necesita un `io.Writer` que falle siempre, con `errors.New("disk full")` como error:

```go
type failingWriter struct{ err error }

func (w failingWriter) Write([]byte) (int, error) { return 0, w.err }
```

El rojo va al revés; deshaz cada cambio:

- Quita de `runVersion` el `if errors.Is(err, flag.ErrHelp) { … }` y su import `"errors"`: el primero falla con `got 2`.
- Cambia el `return 1` de `printOut` por `return 0`: el segundo, con `got 0`.

{{reftest T01.22}}

{{reftest T01.23}}

Si te atascas o tu versión se desvía demasiado, copia el fichero de `ref/ch01-s3` y sigue.

Al terminar: tus tests en verde, commit y etiqueta `ch01-s3`; compárate con `ref/ch01-s3`.

```sh
git tag -a ch01-s3 -m "ch01-s3: CI on GitHub Actions with cross-compile checks"
git push origin main ch01-s1 ch01-s2 ch01-s3
```

## Ejecútalo

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
**1. `ki help`.** Añade un comando `help` que imprima la lista de comandos por stdout y salga con 0, con su test. Como `version`, debe rechazar un argumento de más y atender `-h`. Pista: `help` lee `commands`, y su literal haría un ciclo; `func init()` corre después de dar valor a las variables del paquete.
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

Si `help` escribiera por stderr, `err` no estaría vacío; si devolviera 2, falla el código. Añade la fila `{"help", "help", "ki help: unexpected argument \"extra\"\nusage: ki help\n"}` a la tabla de argumentos de más, y cambia la lista de comandos del test que la fija a `--version, version, help`.

En el literal de `commands`, `helpCommand()` da `initialization cycle for commands`, porque `help` lee `commands`; en una función `init`, `commands` ya tiene su valor y `append` añade la fila. Para no copiar el parseo de `runVersion`, sácalo a un ayudante que responda él mismo si la orden pide ayuda o está mal:

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

`fs.NArg()` cuenta los argumentos que no son flags. `runVersion` queda en el mismo `if code, ok := checkNoArgs(s, c, args); !ok { return code }` más su `return printOut(…)`, y sus tests siguen en verde. La referencia no conserva `help`: si lo dejas en tu repo, añádelo cada vez que el libro cambie esa lista.
:::

:::exercise
**2. Leer un fallo.** Quita el `\n` del formato de `runVersion` y ejecuta `go test -v -run 'TestCLI/version_prints_version_and_go' ./internal/driver/`, y después con `-json` en lugar de `-v`. ¿Para quién es cada salida?
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

`=== RUN` abre cada test y `--- FAIL` lo cierra; la sangría anida el subtest en su padre, que falla porque falló un hijo. Los `%q` muestran las cadenas con escapes: solo falta `\n`. Es una salida para personas.

Con `-json`, cada evento es un objeto por línea, con su `Action` y el nombre completo en `Test`; el evento del fallo es:

```text
{"Time":"…","Action":"fail","Package":"github.com/aldezex/kilang/internal/driver","Test":"TestCLI/version_prints_version_and_go","Elapsed":0}
```

Es para programas: `bookx` cruza con las tarjetas la lista de tests que pasan según `go test -json`, y por eso las filas de tabla no usan `t.Run`, que haría de cada fila un test.

`-run` selecciona por nombre, y cada trozo entre barras es una expresión regular sin anclar: `-run 'TestCLI/usage'` ejecuta todo subtest cuyo nombre contenga `usage`.
:::

:::exercise
**3. Una carrera de datos.** Crea un módulo bajo `build/` (`mkdir -p build/race && cd build/race && go mod init example.com/race`). Una goroutine es una función que corre a la vez que las demás; con un `sync.WaitGroup`, `wg.Go(f)` lanza `f` en una y `wg.Wait()` espera a todas. Guarda esta función en `count.go`:

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
**4. `-shuffle`.** Ejecuta `go test -shuffle=on ./...` tres veces, y después `go test -v -shuffle=on ./internal/toolchain/`. ¿Qué cambia? ¿Qué garantiza que pasen las tres?
:::

:::solution
Sin `-v` ves las mismas líneas `ok`, ninguna `(cached)`. Con `-v`, la primera línea es la semilla (`-test.shuffle 1793059846188786862`) y el orden de las funciones `TestXxx` cambia de una ejecución a otra. Los subtests no se barajan: salen siempre en el orden del fichero.

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

El libro no lo necesita: los destinos de Ki son linux/amd64, windows/amd64 y linux/arm64, y los dos últimos ya se compilan en cada push; macOS no es un destino. Para vigilar la deriva hacia macOS basta otra compilación cruzada, `GOOS=darwin GOARCH=arm64 go build -o /dev/null ./cmd/ki`, que hoy compila.
:::

## Preguntas de repaso

:::review
¿Por qué `Main` recibe los flujos y devuelve un `int`, en lugar de usar `os.Stdout` y llamar a `os.Exit`?
:::

:::answer
Para que los tests la ejecuten en proceso: le pasan dos `strings.Builder`, comparan lo escrito, y el binario de test sobrevive. `os.Exit` queda en `cmd/ki/main.go`, que se prueba relanzando el binario.
:::

:::review
¿Por qué el `go.mod` dice `go 1.27` y `toolchain go1.27.3`, y no el `go 1.27.3` de `go mod init`?
:::

:::answer
`go` es la versión mínima del lenguaje, y Ki necesita la 1.27, no el parche 3. `toolchain` dice con qué toolchain se verificó, y un test comprueba que es el que ejecuta los tests. Con `go 1.27.3`, un Go 1.27.2 se negaría a cargar el módulo sin que el lenguaje lo justifique.
:::

:::review
¿Por qué el repositorio y la CI usan `GOTOOLCHAIN=local`, si `auto` descargaría el toolchain correcto?
:::

:::answer
Porque una descarga silenciosa cambiaría el compilador sin que nadie lo decida. Con `local`, si el Go instalado no es el esperado, `ci-local.sh` y «Check the Go version» paran. Actualizar Go es una decisión con su commit.
:::

:::review
¿Por qué los comandos usan `ContinueOnError` y `SetOutput(io.Discard)`?
:::

:::answer
Con `ExitOnError`, `Parse` llamaría a `os.Exit(2)` dentro de `Main` y mataría el test; con `ContinueOnError` el error vuelve a `ki`. Sin `io.Discard`, `flag` escribiría su ayuda en el stderr real del proceso, lo que un test comprueba con `processStderr`.
:::

:::review
¿Por qué `ci-local.sh` compila `ki` para Windows y ARM64 si nadie ejecuta esos binarios hasta los capítulos finales?
:::

:::answer
Para atrapar la deriva cuando es barata: `syscall.Kill` compila en tu máquina y falla en Windows, y sin la comprobación el fallo aparecería entonces, bajo sesenta capítulos de código.
:::

:::review
¿Por qué se pasa `&buf` donde se espera un `io.Writer` cuando `buf` es un `bytes.Buffer`, y por qué un `strings.Builder` recién declarado ya sirve?
:::

:::answer
Porque `Write` de `bytes.Buffer` tiene receptor puntero: el método es de `*bytes.Buffer`, así que solo `&buf` es un `io.Writer`. Y el valor cero de `strings.Builder` es un `Builder` vacío y utilizable: no hay constructor que olvidar.
:::

## Resumen

- `go.mod` declara módulo, versión mínima y toolchain; `GOTOOLCHAIN=local` no descarga otro.
- Un directorio es un paquete; la mayúscula exporta; `internal/` limita quién importa.
- `io.Writer` es cualquier cosa con `Write`; `error` es un valor.
- `go build` compila y enlaza desde los `import`.
- `main` se prueba relanzando el binario de test.
- La CLI es una tabla de comandos: error de uso, stderr y 2; ayuda, stdout y 0.
- La versión de Go sale de `debug.ReadBuildInfo`, y la de `ki`, de una constante.
- `ci/ci-local.sh` es la puerta de cada sesión; la CI lo repite.

## Go en el trabajo

**`gofmt` como contrato social.** Rob Pike: «Gofmt's style is no one's favorite, yet gofmt is everyone's favorite».

**Nombres sin tartamudeo.** `driver.Main`, no `driver.DriverMain`: `bufio` llama `Reader` a su lector, «not `BufReader`, because users see it as `bufio.Reader`» (Effective Go).

**`go vet` y `-race` en toda CI.** Según su documentación, `-race` suele multiplicar memoria y tiempo, y «only finds races that happen at runtime».

**La compilación cruzada como control de deriva.** Un runner de Linux compila para Windows, macOS y ARM64; no sustituye a ejecutar los tests allí.
