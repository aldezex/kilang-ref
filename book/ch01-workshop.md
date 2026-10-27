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

El compilador todavía no existe: construyes el taller. Una CLI con subcomandos y sus tests, tests que fijan los hechos del toolchain que cita el libro, `ci/ci-local.sh`, que ejecuta en tu máquina lo mismo que la CI, y un workflow de GitHub Actions que lo repite en cada push. Es el primer paso hacia el hito M1 (capítulo 10): `ki run fib.ki` imprimirá `6765`.

## Teoría

### El mapa: qué vas a construir

Ki es un lenguaje funcional puro con efectos algebraicos, y su compilador está escrito en Go, solo con la biblioteca estándar. La Parte I construye la primera versión del pipeline:

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

Go se instala como un único árbol, el *toolchain*: compilador, enlazador, biblioteca estándar y el comando `go`. `go build` lee los `import`, deduce el grafo de paquetes, compila cada paquete que no esté ya en la caché y enlaza un ejecutable. No hay cabeceras ni makefiles: un paquete es un directorio.

:::from Rust
`go build` es lo más parecido a `cargo build`, pero sin `Cargo.lock` con cien crates (Ki solo usa la biblioteca estándar) ni perfiles `debug` y `release`.
:::

:::from TS
Frente a `tsc` más `node`, `go build` hace los dos trabajos, y el binario lleva su runtime.
:::

:::from Python
Go comprueba tipos y genera código máquina antes de ejecutar nada, y rechaza lo que en Python sería un aviso de linter: un import sin usar (`"os" imported and not used`) o una variable local sin usar (`declared and not used: x`).
:::

### `go.mod`: módulo, versión y toolchain

Un *módulo* es un árbol de paquetes con un `go.mod` en la raíz. `module github.com/aldezex/kilang` da la ruta, y todo import propio empieza por ella. La referencia, `kilang-ref`, usa la misma ruta, así que sus ficheros encajan en tu árbol sin tocar un import.

Dos líneas deciden con qué Go se compila:

- `go 1.27` es la versión **mínima** del lenguaje. Un toolchain más viejo no carga el módulo: con `GOTOOLCHAIN=local` y un `go.mod` que pide `go 1.28`, go1.27.3 responde `go: go.mod requires go >= 1.28 (running go 1.27.3; GOTOOLCHAIN=local)` y sale con 1.
- `toolchain go1.27.3` es el toolchain **preferido** («a suggested toolchain», según go.dev/doc/toolchain).

Quién obedece esas líneas lo decide `GOTOOLCHAIN`. Por defecto vale `auto`: si el `go.mod` pide un Go más nuevo que el instalado, `go` lo busca o **lo descarga** y lo ejecuta en su lugar. Con `local`, que usan el repositorio y la CI, `go` usa siempre el instalado, y nada cambia el compilador en silencio. A cambio, la línea `toolchain` deja de imponerse (un módulo con `toolchain go1.27.9` compila sin queja con go1.27.3), y por eso `TestRuntimeVersionMatchesToolchainLine` compara `runtime.Version()` del binario de test con esa línea.

¿Por qué no dejar lo que escribe `go mod init`? En Go 1.27, `go mod init` escribe la versión completa del toolchain instalado, `go 1.27.3`, y ninguna línea `toolchain` (lo fija `TestToolchainFacts/go_mod_init_writes_full_version`; el detalle está en `docs/toolchain.md` de la referencia: `git show ref/ch01-s3:docs/toolchain.md`). Esa línea exige el parche 3, cuando Ki solo necesita el lenguaje de la 1.27.

La última línea, `ignore ./build`, es la directiva `ignore`: según la referencia de módulos (go.dev/ref/mod), el comando `go` ignora ese directorio y lo que cuelga de él «when matching package patterns», como `./...`. Sin ella, un fichero roto que dejaras en `build/` rompería `go vet ./...` y `go test ./...` (lo fija `TestToolchainFacts/ignore_directive_hides_build`).

:::from Rust
El `go.mod` hace de `Cargo.toml` y de `rust-toolchain.toml`: `go` se parece a `rust-version` (la MSRV) y `toolchain` a fijar un canal, que con `GOTOOLCHAIN=local` solo hacen cumplir `ci-local.sh` y `TestRuntimeVersionMatchesToolchainLine`.
:::

### Paquetes, nombres exportados, `cmd/` e `internal/`

Cada directorio es un paquete. Un paquete `main` con una función `main` es un ejecutable; cualquier otro, una biblioteca. `cmd/ki/` es el `main` de `ki`; el compilador vive bajo `internal/`.

La visibilidad la decide la primera letra: `Version`, `Main` y `Session` son *exportados*, y otro paquete los ve como `driver.Version`. `goVersion` y `usageError` solo existen dentro de `driver`; desde fuera, el compilador trata un nombre sin exportar como si no existiera. `internal/` añade una frontera que impone el comando `go`: un paquete bajo `.../internal/` solo lo importa código del árbol que contiene ese `internal`. Desde otro módulo falla con `use of internal package example.com/b/internal/secret not allowed`. Así `internal/driver` no es una API pública que luego no puedas cambiar.

:::from Rust
La mayúscula es `pub`, sin más grados: ni `pub(crate)` ni `pub(super)`. El papel de `pub(crate)` lo cumple `internal/`, a nivel de directorios. Y no hay `mod` ni `use`: el directorio es el paquete y el import es su ruta.
:::

:::from Python
La minúscula hace de `_nombre`, pero la impone el compilador.
:::

### Lo básico del lenguaje, en una tabla

| Concepto | Go | Rust | TypeScript | Python |
|---|---|---|---|---|
| Tipos básicos | `int` (64 bits en amd64), `int64`, `string`, `bool`, `byte` | `i64`, `String`, `bool`, `u8` | `number`, `string`, `boolean` | `int`, `str`, `bool` |
| Función | `func div(a, b int) int` | `fn div(a: i64, b: i64) -> i64` | `function div(a: number, b: number): number` | `def div(a, b):` |
| Retornos múltiples | `func divmod(a, b int) (int, int)`; `q, r := divmod(17, 5)` | una tupla | un array `[q, r]` | una tupla |
| Bucle clásico | `for i := 0; i < 3; i++ { … }` | `for i in 0..3` | `for (let i = 0; i < 3; i++)` | `for i in range(3)` |
| `range` | `for i := range 3` (0, 1, 2); `for i, x := range xs` | `for (i, x) in xs.iter().enumerate()` | `xs.forEach((x, i) => …)` | `for i, x in enumerate(xs)` |
| `switch` | sin *fall-through*; `fallthrough` lo pide | `match`, exhaustivo | `switch` con `break` | `match` |

`for` es el único bucle: con tres cláusulas, solo con la condición (`for n < 3 { … }`, un `while`) o sin nada (`for { … }`, un `loop`). Un `switch` sin expresión, con una condición en cada `case`, sustituye a una cadena de `if`/`else if`. `x := 3` declara e infiere el tipo. Los retornos múltiples se asignan todos o ninguno (`x := two()` da `assignment mismatch: 1 variable but two returns 2 values`); lo que sobre, a `_`. Y toda variable empieza con su *valor cero*: `0`, `""`, `false` o `nil`, el de punteros, slices, mapas, funciones e interfaces (un `error` sin fallo es `nil`).

:::from Rust
Lo que más se echa de menos es la exhaustividad: un `switch` sin `default` que no casa con ningún `case` no hace nada, sin aviso (el capítulo 3 lo compensa). A cambio, no existe la variable sin inicializar.
:::

### Lo justo para testear

Los tests de tabla necesitan tres piezas (los capítulos 2 y 6 las amplían):

- **Un literal de slice de structs.** `[]struct{ name, want string }{ {"go_line", "go 1.27"}, … }` declara un struct anónimo y un slice con sus filas en la misma expresión.
- **`range` sobre él.** `for _, tc := range tests { … }` recorre las filas (`_` descarta el índice).
- **Un literal de función.** `func(t *testing.T) { … }` es una función sin nombre escrita donde se usa; `t.Run` la recibe como argumento, y ve las variables de la función que la contiene: es un *closure*.

:::from TS
Un literal de función es una *arrow function* con sintaxis larga: `func(x int) int { return x * 2 }` frente a `(x: number) => x * 2`, sin forma abreviada y con los tipos obligatorios.
:::

### Lo justo para el driver

El código del capítulo usa cuatro ideas que los capítulos 2 y 3 desarrollan:

- **Un puntero a struct es el mismo valor, no una copia.** `NewSession` devuelve un `*Session`. Una función que recibe un `Session` y cambia un campo cambia su copia; con un `*Session`, el original.
- **Un método es una función con receptor.** `func (r goRun) String() string` es el método `String` de `goRun`. Con receptor puntero, `func (b *Buffer) Write(…)`, es de `*Buffer`, no de `Buffer`. Un tipo satisface una interfaz por sus métodos: si tiene `String() string` (`fmt.Stringer`), `%v` imprime lo que devuelve.
- **`io.Reader` e `io.Writer` son «cualquier cosa que sepa leer o escribir»**: interfaces de un método, `Read` y `Write`. `Main` escribe en un `io.Writer`: en el programa, `os.Stdout`; en los tests, un `strings.Builder` que guarda lo escrito (los de `internal/toolchain` usan `bytes.Buffer`). Ojo: como `Write` de `bytes.Buffer` tiene receptor puntero, el `io.Writer` es `&buf`; pasar `buf` da `bytes.Buffer does not implement io.Writer (method Write has pointer receiver)`.
- **`error` es un valor**: el último resultado de una función que puede fallar, que se comprueba con `if err != nil { … }`. No hay excepciones.

:::from Rust
`io.Writer` es `std::io::Write` con conformidad implícita: sin `impl Write for MiTipo`, basta tener `Write([]byte) (int, error)`, y un método `String()` hace de `impl Display`. En lugar de `Result<T, E>`, Go devuelve `(T, error)`, y en lugar de `?` escribes `if err != nil { return …, err }`, sin que el compilador te obligue a mirarlo. Y el puntero no es una referencia: ni `&mut`, ni tiempos de vida; el recolector libera la memoria cuando ningún puntero la alcanza.
:::

### `gofmt` y `go vet`

`gofmt` reescribe el código en el único estilo de Go, sin opciones. `gofmt -l` lista los ficheros que no están en ese formato, pero sale con 0 aunque liste alguno: por eso `ci/ci-local.sh` exige que su salida esté vacía.

`go vet` busca errores que compilan: `fmt.Sprintf("%d items", name)` con un `name` `string` compila, y `go vet` dice `fmt.Sprintf format %d has arg name of wrong type string`. `go test` ejecuta además un subconjunto «de alta confianza» de vet sobre cada paquete antes de ejecutar sus tests; si encuentra algo, el paquete sale como `[build failed]`.

:::from Rust
`gofmt` es `rustfmt` sin `rustfmt.toml`, y `go vet` hace de `clippy` con una filosofía más estrecha: casi siempre errores, no estilo.
:::

### `testing`: tests, subtests y tablas

Un test es una `func TestXxx(t *testing.T)` en un fichero `_test.go`; falla si llama a `t.Errorf` (anota y sigue) o `t.Fatalf` (anota y termina).

- **Subtests.** `t.Run("version_command", func(t *testing.T) { … })` crea un test hijo, `TestCLI/version_command`, que se informa y selecciona por separado. El libro nombra cada subtest con un literal en `snake_case`, que `bookx`, la herramienta que verifica el libro, enlaza con su tarjeta (la ficha de cada test, en «Tests primero»).
- **Tests de tabla.** Las filas se recorren *dentro de un subtest*, sin `t.Run` por fila, y cada fallo lleva el nombre de la fila: `t.Errorf("%s: …", tc.name, …)`. Así una tarjeta cubre el subtest con su tabla, y `go test -json` lista el subtest, no las filas.
- **`t.Helper()`** marca un ayudante: cuando `checkUsageError` falla, el mensaje señala su llamada dentro del subtest, no su `t.Errorf`.
- **`t.Cleanup(f)`** registra una función que corre al terminar el test, también si falló. `t.TempDir()` te da un directorio que se borra cuando terminan el test y sus subtests.

Los flags que usa el capítulo:

- `-run 'TestCLI/usage_lists'` selecciona por nombre; cada trozo entre barras es una expresión regular sin anclar, así que `-run 'TestCLI/usage'` ejecuta todo subtest cuyo nombre contenga `usage`, como `no_args_is_usage_error` y, desde la sesión 3, `version_help_prints_usage`.
- `-count=1` ejecuta aunque haya caché; sin él, un segundo `go test` sin cambios responde `ok … (cached)`.
- `-shuffle=on` baraja el orden (ejercicio 4); `-json` emite un objeto por evento.
- `-race` compila con el detector de carreras de datos. En Go 1.27 necesita cgo, el mecanismo de Go para llamar a C, y cgo un compilador de C: sin `gcc` en el `PATH`, `go env CGO_ENABLED` da `0`, y entonces `go test -race` sale con 2 y `-race requires cgo` (lo fija `TestToolchainFacts/race_requires_cgo`). «Puesta en marcha» instala `gcc`.

:::from Rust
Frente a `#[test]` y `cargo test`, Go no tiene `assert_eq!`: comparas con un `if` y escribes el mensaje con lo obtenido y lo esperado.
:::

:::from Python
Frente a pytest, `t.Run` se parece a `parametrize` con ids, y `t.Cleanup` y `t.TempDir`, a las fixtures con `yield` y a `tmp_path`.
:::

### `flag` y el patrón de subcomandos

Para una CLI con subcomandos, como `go` o `git`, el patrón es una tabla de comandos y un `flag.FlagSet` por comando: `Main` busca la primera palabra en la tabla, y el comando parsea el resto.

Cada `FlagSet` de `ki` se crea con `flag.ContinueOnError`: con `flag.ExitOnError`, un flag desconocido terminaría el proceso con el código 2, y dentro de `Main`, que los tests ejecutan en proceso, mataría el binario de test. Con `ContinueOnError`, `Parse` devuelve el error y `ki` lo informa en su formato; `SetOutput(io.Discard)` evita que `flag` escriba además su ayuda en el stderr del proceso.

:::from Rust
`flag` está lejos de `clap`: ni derive, ni subcomandos, ni ayuda con formato. A cambio no es una dependencia.
:::

### `debug.ReadBuildInfo` y por qué `Version` es una constante

`runtime/debug.ReadBuildInfo()` devuelve lo que `go build` graba en cada binario: la versión de Go (`GoVersion`), la versión del módulo principal (`Main.Version`). `ki --version` toma de ahí la versión de Go: `goVersion()` devuelve `bi.GoVersion`, la misma cadena que `runtime.Version()`, y solo usa `runtime.Version()` si el binario no lleva información de build.

La versión de `ki` no puede salir de ahí. En Go 1.27, un `go build` normal fuera de un repositorio git da `Main.Version == "(devel)"` (lo fija `TestToolchainFacts/plain_build_has_devel_version`). Dentro de uno da una pseudo-versión que cambia con cada commit. Ninguna es un número estable, así que `Version` es una constante, `"0.0.1"`, que se cambia a mano.

:::from Rust
En Rust escribirías `env!("CARGO_PKG_VERSION")`. Go no tiene campo de versión en `go.mod`: la versión de un módulo es la etiqueta de git que lo publica.
:::

### TDD rojo-verde y tests golden

El libro trabaja con TDD estricto. **Rojo**: escribes el test de un comportamiento que no existe y lo ves fallar, por la razón esperada; un test que nunca has visto en rojo puede pasar por accidente. **Verde**: el código mínimo que lo hace pasar. **Refactor**: limpias con los tests como red.

Un test fuerte compara valores exactos y mira los bordes: `usage_lists_commands` compara el stderr byte a byte, y si `ki` listara `version` antes que `--version`, solo ese test lo vería. Pregúntate qué implementación equivocada dejaría pasar cada test; si la respuesta es «cualquiera», no sirve.

Los *tests golden* comparan la salida real de un programa con la esperada, guardada en un fichero; en Ki, cada programa de prueba la lleva en su cabecera, con su código de salida, y el ejecutor `ki test` compara (capítulo 3).

### GitHub Actions: runners, contenedores y compilación cruzada

GitHub Actions ejecuta *workflows*, ficheros YAML en `.github/workflows/`, ante un evento, como un push. Cada *job* corre en un *runner*, una máquina virtual nueva (`runs-on: ubuntu-latest`), y puede correr dentro de un **contenedor**: con `container: { image: ubuntu:26.04 }`, los pasos se ejecutan en esa imagen de Docker, la Ubuntu de tu WSL, que no se mueve como `ubuntu-latest`. `actions/setup-go` instala Go, con la versión que diga el `go.mod`.

La **compilación cruzada** es la otra mitad: `GOOS=windows GOARCH=amd64 go build ./cmd/ki` produce un `.exe` desde Linux, sin instalar nada (para esos destinos, `go env CGO_ENABLED` da `0`). Compilar para Windows y ARM64 en cada push detecta la *deriva*, el código que sin que lo notes solo compila en Linux: `syscall.Kill` compila en linux/amd64 y linux/arm64, y para windows/amd64 falla con `undefined: syscall.Kill`.

:::from Rust
En Rust hacen falta `rustup target add` y, a menudo, un enlazador del destino. Go compila la biblioteca estándar del destino desde sus fuentes y enlaza con su propio `link`: sin cgo, bastan las dos variables.
:::

## Tests primero

Las tarjetas del capítulo, con valores exactos:

{{cards}}

Úsalas así: escribe el test de la tarjeta, míralo fallar (rojo), implementa lo justo para que pase (verde), y solo entonces despliega el test de referencia con su `{{reftest}}`. La sesión 1 te da el fichero de test; desde la sesión 2 los escribes a partir de las tarjetas.

## Implementación

### Sesión 1: el módulo, `ki --version` y `ci-local.sh` (≈ 3 h)

#### Puesta en marcha

Comprueba el toolchain:

```console
$ go version
go version go1.27.3 linux/amd64
```

`-race` necesita un compilador de C; instala los mismos paquetes que la CI:

```sh
sudo apt install gcc libc6-dev
```

Crea el repositorio en `~/kilang`, nunca bajo `/mnt/c`: en WSL, `/mnt/c` es el disco de Windows montado por `9p` (lo muestra `findmnt`), y la documentación de WSL recomienda trabajar en el sistema de ficheros de Linux por velocidad. Delve, el depurador, se instala en el capítulo 10.

```sh
mkdir ~/kilang && cd ~/kilang
git init -b main
go mod init github.com/aldezex/kilang
```

Edita el `go.mod`, que dice `go 1.27.3`, para que quede así:

{{file ch01-s1 go.mod}}

Y un `.gitignore`, igual que el de la referencia:

{{file ch01-s1 .gitignore}}

La línea que importa es `/build/`: ahí escribe `ci/ci-local.sh` el binario, y sin ella un `git add -A` subiría `build/bin/ki`. `/.claude/`, `/.superpowers/` y `.cache/` son carpetas de trabajo de la referencia (`ci-local.sh` salta las dos primeras en su `find`); en tu repo no estorban.

Añade una licencia MIT: `gh repo license view mit > LICENSE` escribe el texto con dos huecos, `[year]` y `[fullname]`, que rellenas. Haz el primer commit y publica con `gh`, que crea el repositorio en GitHub, lo añade como remoto (`--source .`) y sube los commits (`--push`):

```sh
git add go.mod .gitignore LICENSE
git commit -m "init: module and MIT license"
gh repo create aldezex/kilang --public --source . --push
```

Por último, trae las etiquetas de la referencia con un prefijo que no choque con las tuyas:

```sh
git remote add --no-tags ref https://github.com/aldezex/kilang-ref.git
git fetch ref 'refs/tags/*:refs/tags/ref/*'
```

Con ellas te comparas o recuperas un fichero cuando lo necesites (no ahora): `git diff ref/ch01-s1 -- internal/driver/`, `git restore --source=ref/ch01-s1 -- <fichero>`.

#### Los tests, en rojo

T01.01–T01.04 describen la CLI mínima: `ki --version` imprime una línea por stdout y sale con 0; sin argumentos o con un comando desconocido, dos líneas de uso por stderr y 2. El 2 es el código habitual para «la línea de órdenes está mal» (el propio `flag` sale con 2 en modo `ExitOnError`), distinto del 1, que `ki` da cuando no puede escribir la respuesta (sesión 3) y dará cuando tu programa Ki tenga errores. La tarjeta T01.04 da la lista del final del capítulo, `--version, version`; en la sesión 1 es solo `--version`.

Como es tu primer test en Go, aquí va completo. Escríbelo en `internal/driver/cli_test.go`:

{{file ch01-s1 internal/driver/cli_test.go}}

El fichero está en `package driver`, el paquete que prueba, y ve sus nombres sin exportar. `runCLI` ejecuta `Main` en el mismo proceso con dos `strings.Builder` como stdout y stderr, y devuelve lo escrito: por eso `Main` recibe los flujos como parámetros. Se llama `TestCLI`, no `TestMain`, porque `func TestMain(m *testing.M)` tiene un significado propio, que verás en la sesión 3.

Su sintaxis es nueva. `args ...string` es un parámetro variádico (`...args: string[]` en TS, `*args` en Python): dentro es un `[]string`, vacío en `runCLI(t)`. `(code int, stdout, stderr string)` son resultados con nombre: empiezan con su valor cero y se asignan como variables. `var out, errOut strings.Builder` declara dos `Builder` con su valor cero, vacío y utilizable; `&out` pasa su dirección (su `Write` tiene receptor puntero). En los mensajes, `%q` imprime una cadena entre comillas y con escapes (`"a\nb"`), y `%v`, cualquier valor en su forma por defecto.

`checkUsageError` mira solo el prefijo y que haya dos líneas, porque la lista de comandos crece; solo `usage_lists_commands` la fija.

```console
$ go test ./internal/driver/
# github.com/aldezex/kilang/internal/driver [github.com/aldezex/kilang/internal/driver.test]
internal/driver/cli_test.go:19:9: undefined: Main
internal/driver/cli_test.go:45:19: undefined: Version
FAIL	github.com/aldezex/kilang/internal/driver [build failed]
FAIL
```

Ese es el rojo: en un lenguaje compilado, el primero suele ser un error de compilación.

#### El código

`cmd/ki/main.go` solo conecta `driver.Main` con el proceso:

{{file ch01-s1 cmd/ki/main.go}}

`os.Args[1:]` son los argumentos sin el nombre del programa, y este es el único `os.Exit` de `ki`: `Main` devuelve el código, y los tests la llaman cuantas veces quieran.

La versión, en `internal/driver/version.go`:

{{file ch01-s1 internal/driver/version.go}}

`if bi, ok := debug.ReadBuildInfo(); ok { … }` es un `if` con sentencia inicial: `bi` y `ok` solo existen dentro de él.

Y el driver, en `internal/driver/cli.go`:

{{file ch01-s1 internal/driver/cli.go}}

El comentario antes de `package driver` es la documentación que muestra `go doc`. `Main` es un `switch` sin expresión: o el único argumento es `--version`, o es un error de uso. `fmt.Fprintf` escribe en el `io.Writer` que le pases.

#### `ci/ci-local.sh`

Falta el script que decide si una sesión está en verde, el mismo de la CI:

{{file ch01-s1 ci/ci-local.sh}}

Exporta `GOTOOLCHAIN=local`, deja fuera `GOFLAGS`, `GOENV` y `GOWORK` de tu máquina, y comprueba que el Go instalado es el de la línea `toolchain` del `go.mod` del árbol en que corre, para que una etiqueta antigua se verifique con su propio parche. Pasa `gofmt -l` sobre la lista de `find`, no de `git ls-files`, porque también corre en copias de etiquetas sin repositorio; exige salida vacía, y se para si `find` no encuentra ningún fichero Go. Después, `go vet`, el binario en `build/bin/ki`, los tests con y sin `-race` y las dos compilaciones cruzadas. `set -euo pipefail` lo corta en el primer fallo.

Hazlo ejecutable (`chmod +x ci/ci-local.sh`) y ejecútalo. Los tiempos varían, la segunda tanda es la de `-race`, y un paquete que ya pasó sus tests sin cambios sale `(cached)`.

```console
$ ci/ci-local.sh
?   	github.com/aldezex/kilang/cmd/ki	[no test files]
ok  	github.com/aldezex/kilang/internal/driver	0.002s
?   	github.com/aldezex/kilang/cmd/ki	[no test files]
ok  	github.com/aldezex/kilang/internal/driver	1.014s
verify: OK
```

Las etiquetas de sesión son anotadas: `git tag -a ch01-s1 -m "ch01-s1: module, ki --version and ci-local"`.

Si te atascas o tu versión se desvía demasiado, copia el fichero de `ref/ch01-s1` y sigue.

Al terminar: tus tests en verde, commit y etiqueta `ch01-s1`; compárate con `ref/ch01-s1`.

### Sesión 2: comandos con `flag`, tests de tabla y los hechos del toolchain (≈ 2,5 h)

#### Los tests de la CLI

T01.05–T01.07 añaden el comando `version`, igual que `--version`, y dos errores de uso: un flag desconocido y un argumento de más. T01.04 cambia a `--version, version`. Escribe los subtests en `TestCLI`; T01.07 es una tabla de dos filas dentro de un único subtest.

T01.06 tiene la trampa que explica su tarjeta: nada debe escaparse al stderr *real* del proceso. Lo comprueba un ayudante, `processStderr(t *testing.T, f func()) string`, con dos piezas aún sin enseñar: cópialo de la referencia al final de tu `cli_test.go` y añade `"os"` a su bloque `import`:

```sh
{ echo; git show ref/ch01-s2:internal/driver/cli_test.go | sed -n '/^\/\/ processStderr/,/^}/p'; } >> internal/driver/cli_test.go
```

`os.Stderr` es una variable del paquete `os` y se puede reasignar: el ayudante la apunta a un fichero de `t.TempDir()`, ejecuta `f` y devuelve lo escrito. La otra pieza es `defer`, que aplaza una llamada hasta que termina la función: `defer func() { os.Stderr = saved }()` restaura el stderr original aunque `f` falle con `t.Fatalf`.

:::from Rust
`defer` hace el trabajo de `Drop` o de un *scope guard*, pero a mano y al salir de la función, no del bloque. Y `os.Stderr` es una variable global mutable, sin `static mut` ni `unsafe`: cualquier paquete puede reasignarla.
:::

:::from Python
`defer` es un `finally` (o un `with`) escrito junto a lo que limpia.
:::

Con el código de la sesión 1, `go test ./internal/driver/` falla en `usage_lists_commands`, `version_command`, `version_rejects_unknown_flag` y `version_flag_rejects_extra_args`: el rojo.

#### La tabla de comandos

Un tipo nuevo, en `internal/driver/session.go`:

{{file ch01-s2 internal/driver/session.go}}

`Session` es el único valor que recibe cada comando, y crecerá con el libro. `NewSession` es el constructor que usa `Main` (por convención, `NewX` construye un `X`). Go no tiene constructores, y un literal `&Session{…}` siempre compila, pero los campos que necesiten un valor inicial distinto de cero se fijarán en `NewSession`: no uses el literal. Devuelve un puntero para que la sesión no se copie en cada llamada.

Los comandos, en `internal/driver/commands.go`:

{{file ch01-s2 internal/driver/commands.go}}

`command` es un struct cuyo campo `Run` es una función: «qué hacer», guardado en una tabla. `commands` es un slice en orden de registro, el de la línea `commands:`.

`versionCommand` resuelve el problema que explica su comentario: `Run` necesita su propio comando para informar errores con el nombre correcto, y no puede buscarlo en `commands`, que se inicializa a partir de `Run` (daría `initialization cycle for commands`, seguido de la cadena que cierra el ciclo, `… runVersion refers to commands`). La salida es un *closure*: `&command{…}` da la dirección de un `command` nuevo, `versionCommand` guarda ese puntero `c` y `Run` lo captura; `return *c` devuelve una copia del valor apuntado, con el mismo `Run`.

`runVersion` crea un `FlagSet` aunque `version` no tenga flags, para que un `-x` reciba el mismo mensaje que en cualquier comando. `fs.Parse(args)` devuelve un `error` que se comprueba con `!= nil`, y `fs.NArg()` cuenta los argumentos que no son flags. `usageError` escribe `ki <nombre>: <error>` y la línea `usage:`, y devuelve 2.

Dos casos más. `-h`, `-help` o `--help` no son un error, sino una petición de ayuda: `Parse` devuelve `flag.ErrHelp`, y `errors.Is(err, flag.ErrHelp)` pregunta si el error es ese valor (o lo envuelve); `errors.As`, en `facts_test.go`, busca en cambio un error de un tipo. `ki` responde la línea `usage:` por stdout, con 0. Las respuestas pasan por `printOut`: si escribir en stdout falla (un disco lleno), lo dice por stderr y devuelve 1, y un script no toma una respuesta perdida por buena. `_, _ = fmt.Fprintf(…)` descarta a propósito lo que devuelve escribir en stderr: si falla, no queda dónde decirlo. Sus tests llegan en la sesión 3.

`Main` pasa a buscar en la tabla:

{{diff ch01-s1..ch01-s2 internal/driver/cli.go}}

`usage()` construye la línea `commands:` desde la tabla: `make([]string, len(commands))` crea un slice de cadenas vacías que el bucle rellena, y `strings.Join` las une, así que la línea nunca se desfasa. Con esto, el paquete pasa. Compara tus tests con los de la referencia:

{{reftest T01.05}}

{{reftest T01.06}}

{{reftest T01.07}}

#### Los hechos del toolchain, fijados a tests

Cada hecho que el libro cita «en Go 1.27» está fijado a un test en `internal/toolchain/`, que salta antes de que el texto mienta si Go lo cambia.

T01.08–T01.10 miran el `go.mod` del repositorio. Van en `internal/toolchain/toolchain_test.go`, en el paquete `toolchain_test`: un paquete de test externo (el sufijo `_test` en la cláusula `package`), lo habitual cuando el test no necesita nada sin exportar; va completo. `go test` ejecuta los tests en el directorio de su paquete, así que el `go.mod` está en `../../go.mod`.

:::from Rust
Un paquete de test externo es como los tests de integración de `tests/`: compila aparte y solo ve lo exportado (un nombre sin exportar da `undefined: ext.hidden`).
:::

{{file ch01-s2 internal/toolchain/toolchain_test.go}}

`hasLine` compara líneas enteras: con `strings.Contains`, un `go 1.27.3` olvidado pasaría por `go 1.27`. `no_dependencies` es su propio subtest porque es una negación, no una fila más.

T01.11–T01.16, en `facts_test.go`, ejecutan `go` sobre módulos desechables en `t.TempDir()`. Es la primera vez que aparece `os/exec`, así que va completo:

{{file ch01-s2 internal/toolchain/facts_test.go}}

En `runGo`, `exec.Command("go", args...)` prepara el proceso (`args...` expande el slice), `cmd.Dir` fija su directorio y `cmd.Env` le da el entorno del test más cuatro variables que lo aíslan de la configuración de la máquina. `cmd.Run()` devuelve `nil` si el proceso sale con 0 y, si no, un error del que `race_requires_cgo` saca el código exacto, 2, con `errors.As(r.err, &exit)`, que busca en el error uno de tipo `*exec.ExitError` (como `downcast_ref` en Rust). Con el método `String()` de `goRun`, `%v` imprime la ejecución entera. Los programas de prueba van entre comillas invertidas: cadenas *raw*, sin escapes y de varias líneas.

Estos tests fijan hechos ya ciertos y pasan desde el principio, así que el rojo se comprueba al revés: cambia un momento la tabla de `TestGoMod/lines` para pedir `"go 1.27.3"`, mira el fallo y deshaz el cambio.

El mensaje de la etiqueta: `ch01-s2: commands with flag, table tests and toolchain facts`.

Si te atascas o tu versión se desvía demasiado, copia el fichero de `ref/ch01-s2` y sigue.

Al terminar: tus tests en verde, commit y etiqueta `ch01-s2`; compárate con `ref/ch01-s2`.

### Sesión 3: la CI en GitHub Actions (≈ 2,5 h)

#### Los tests del workflow

Un workflow no se prueba en local, pero sí lo que dice: T01.17–T01.20 leen `ci.yml` como texto (no hay parser de YAML en la biblioteca estándar) y fijan lo que dicen sus tarjetas. Escribes los cuatro subtests en `TestCIWorkflow`.

Primero generaliza el ayudante de la sesión 2: `readGoMod(t)` pasa a ser `readRepoLines(t, rel)`, que lee cualquier fichero desde la raíz del repositorio, y la búsqueda de la línea `toolchain` sale a `toolchainVersion(t)`, que T01.19 reutiliza. Es un refactor con los tests en verde como red:

{{diff ch01-s2..ch01-s3 internal/toolchain/toolchain_test.go}}

El `return ""` tras `t.Fatalf` es para el compilador: una función con resultado debe acabar en una sentencia terminal, como `return` o `panic` (si no, `missing return`), y Go no sabe que `Fatalf` nunca vuelve, lo que Rust diría con `-> !`.

Después escribe `internal/toolchain/ci_test.go`. YAML anida por sangría, y una línea solo significa algo bajo su padre: `GOTOOLCHAIN: auto` es correcto en «Set up Go» y un error en el job. `readCIWorkflow(t) []ciLine` salta líneas en blanco y comentarios (un `# GOTOOLCHAIN: local` no cuenta) y guarda de cada línea su sangría y su texto sin los espacios iniciales. `under(t, lines, path...)` sigue una ruta de cabeceras, como `"jobs:", "check:", "env:"`, cada una entre los hijos directos de la anterior, y devuelve el texto de lo que cuelga de la última (o para el test si falta alguna). Para comparar, el paquete `slices`: `slices.Equal` exige exactamente esas líneas, y `slices.Contains`, que una esté entre ellas (T01.18).

`append(slices.Clone(job), "env:")` copia antes de añadir. Un slice tiene longitud y capacidad: `make([]string, 0, 4)` crea uno de longitud 0 con sitio para 4, y mientras quede sitio, `append` escribe en el mismo array sin copiar. Con `base := append(make([]string, 0, 4), "jobs:", "check:")`, `append(base, "env:")` y luego `append(base, "steps:")` dan los dos `[jobs: check: steps:]`: dos rutas hechas desde la misma base podrían pisarse.

:::from Rust
Un slice es una vista (puntero, longitud, capacidad) de un array compartido: nada impide dos vistas que escriben en el mismo sitio, lo que el *borrow checker* rechazaría. `slices.Clone` hace de `to_vec()`.
:::

Sin el workflow, el rojo es claro (los números de línea son los de la referencia):

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

`on` lo dispara con cada push a `main`, cada etiqueta `ch*` o `m*` y cada pull request; `permissions: contents: read` deja al token del job solo leer el repositorio. El job `check` corre dentro de `ubuntu:26.04`, con `GOTOOLCHAIN: local`, `GOFLAGS` vacío y `timeout-minutes: 15`, que corta un job colgado a los 15 minutos y no a las seis horas que permite GitHub. Los pasos:

1. **Install system packages**: la imagen base no trae `git`, certificados, `gcc` ni `libc6-dev` (cgo, para `-race`). `tar` sí viene, pero se nombra porque `setup-go` desempaqueta Go con él. `-o Acquire::Retries=3` reintenta una descarga que falla.
2. **Check out the repository**, con `fetch-depth: 0`: historia y etiquetas.
3. **Set up Go.** Según el código de `setup-go` (lo documenta `docs/toolchain.md` de la referencia), `go-version-file: go.mod` lee la línea `toolchain` solo si `GOTOOLCHAIN` no vale `local` en su entorno; si vale `local`, lee la línea `go` e instalaría la 1.27.x más alta disponible, que algún día no será la 1.27.3. Por eso este paso, y solo él, lleva `GOTOOLCHAIN: auto`; `setup-go` vuelve a exportar `local` a los pasos siguientes.
4. **Check the Go version** detiene el job si el Go no es exactamente `go1.27.3`; el literal cambia con el `go.mod`, y T01.19 lo vigila.
5. **Trust the checkout**: el checkout es del usuario del runner, no del `root` del contenedor, y `git`, al que `go build` pregunta el commit, rechaza un repositorio ajeno.
6. **Run ci-local**: el mismo script que ejecutas tú.

T01.19 y T01.20 se ven fallar al revés: cambia un momento ese `go1.27.3` por `go1.27.4` y borra `pull_request: {}`; fallan `go_version_matches_toolchain` y `triggers`, con las líneas encontradas y las esperadas. Deshaz los cambios.

Haz commit y push a `main`, y mira el job en la pestaña *Actions*: «Set up Go» registra `Setup go version spec 1.27.3` (leyó la línea `toolchain`) y «Run ci-local» termina con `verify: OK`. Compara tus subtests con la referencia:

{{reftest T01.17}}

{{reftest T01.18}}

{{reftest T01.19}}

{{reftest T01.20}}

#### El binario, probado como proceso

Los tests de `driver` llaman a `Main` dentro del proceso, y ninguno ejecuta `main`: si `main.go` pasara `os.Args` en lugar de `os.Args[1:]`, T01.01–T01.03 seguirían en verde. T01.21, en `cmd/ki/main_test.go`, lo cierra. Va completo, porque es nuevo:

{{file ch01-s3 cmd/ki/main_test.go}}

`package main` no se importa, y `main` termina con `os.Exit`, así que el test relanza su propio binario. Ahí entra `TestMain`: si un paquete de test la define, `go test` la llama en lugar de ejecutar los tests, y `m.Run()` los ejecuta y devuelve el código para `os.Exit`. `runKi` lanza `os.Executable()`, el binario de test, con el entorno del padre (`os.Environ()`) más `KI_TEST_RUN_MAIN=1`; en el hijo, `os.Getenv` ve la variable y `TestMain` llama a `main`. El código de salida se obtiene con `errors.As`, como en `race_requires_cgo`.

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

Con `os.Args`, `ki` recibe como primer argumento la ruta del binario y la toma por un comando desconocido; de las tres filas, solo `version` detecta ese cambio.

:::from Rust
Cargo da a los tests de integración la ruta del binario en `env!("CARGO_BIN_EXE_<nombre>")`. `go test` no construye `ki` aparte, así que el binario de test hace de `ki`.
:::

{{reftest T01.21}}

#### Ayuda y errores de escritura

T01.22 y T01.23 fijan los dos casos de `runVersion` que la sesión 2 dejó sin test: `-h` o `--help` dan la línea `usage:` por stdout y 0, y un stdout que no se puede escribir, 1 y `ki version: cannot write to stdout: disk full`. Escríbelos en `TestCLI`, como tablas de dos filas. T01.23 necesita un `io.Writer` que falle siempre: `type failingWriter struct{ err error }` con `func (w failingWriter) Write([]byte) (int, error) { return 0, w.err }`. Con ese método ya es un `io.Writer`; `errors.New("disk full")` pide `"errors"` en el `import`.

El código ya existe, así que el rojo va al revés. Sin el `if errors.Is(err, flag.ErrHelp) { … }` de `runVersion` (y sin su import `"errors"`), `version_help_prints_usage` falla con `got 2` y `ki --version: flag: help requested` en stderr; con `return 0` en lugar del `return 1` de `printOut`, `version_reports_write_error` falla con `got 0 …, want 1 …`. Deshaz los cambios.

{{reftest T01.22}}

{{reftest T01.23}}

El mensaje de la etiqueta: `ch01-s3: CI on GitHub Actions with cross-compile checks`.

Si te atascas o tu versión se desvía demasiado, copia el fichero de `ref/ch01-s3` y sigue.

Al terminar: tus tests en verde, commit y etiqueta `ch01-s3`; compárate con `ref/ch01-s3`.

## Ejecútalo

Con la sesión 3 terminada (los tiempos varían, y un paquete que ya pasó sus tests sin cambios sale `(cached)` en cualquiera de las dos tandas):

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

- `go build` compila y enlaza a partir de los `import`, con caché; `ignore ./build` saca `build/` de `./...`.
- `go.mod` declara el módulo, la versión mínima del lenguaje (`go 1.27`) y el toolchain preferido (`toolchain go1.27.3`); `GOTOOLCHAIN=local` impide descargar otro.
- Un directorio es un paquete; la mayúscula exporta; `internal/` limita quién importa.
- `for` es el único bucle; `switch` no cae al caso siguiente; los retornos múltiples hacen de tuplas y de `Result`.
- `io.Writer` es cualquier cosa con `Write`, sin declararlo; `error` es un valor; las tablas de test viven dentro de un subtest.
- La CLI es una tabla de comandos con un `FlagSet` por comando en `ContinueOnError`; los errores de uso van a stderr con 2, la ayuda pedida a stdout con 0.
- `ki --version` toma la versión de Go de `debug.ReadBuildInfo` y la suya de una constante: `Main.Version` es `(devel)` o una pseudo-versión.
- `ci/ci-local.sh` (`gofmt`, `go vet`, tests con y sin `-race`, compilación cruzada) es la puerta de cada sesión, y la CI de GitHub ejecuta el mismo script en un contenedor fijado.
- `main` se prueba como proceso: el test relanza su propio binario, y en el hijo `TestMain` llama a `main`.

## Go en el trabajo

**`gofmt` como contrato social.** Un proverbio que Rob Pike presentó en Gopherfest SV en 2015: «Gofmt's style is no one's favorite, yet gofmt is everyone's favorite». Por eso `gofmt -l` vacío es una puerta de la CI.

**Nombres de paquete sin tartamudeo.** El nombre del paquete aparece en cada uso. Effective Go pone el ejemplo de `bufio`: su lector se llama `Reader`, «not `BufReader`, because users see it as `bufio.Reader`». Por eso `driver.Main` y no `driver.DriverMain`.

**`go vet` y `-race` en toda CI.** Los dos atrapan fallos que el compilador deja pasar. Según su documentación, el detector cuesta 5–10 veces más memoria y 2–20 veces más tiempo en un programa típico, y «only finds races that happen at runtime»: un `-race` en verde vale lo que la cobertura de tus tests.

**La compilación cruzada como control de deriva.** Un runner de Linux compila una CLI para Windows, macOS y ARM64 en segundos; no sustituye a ejecutar los tests allí, pero impide que la deriva se acumule.
