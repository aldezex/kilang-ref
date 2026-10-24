# El toolchain de Go

Este fichero registra la versión de Go con la que empieza el libro, los hechos de versión que usan las Partes I y II reverificados sobre ella, y cada cambio de versión posterior (spec, apartado 11.4). La investigación del diseño verificó esos hechos sobre go1.26.5 (`docs/research/r4-go-curriculum.md`, con ids `V…`, y `docs/research/tech-check.md`, con ids `V-t…`); esa versión es la procedencia, no una versión a usar.

Los comandos se ejecutaron el 2026-10-24 en módulos desechables bajo `build/toolchain/`, que se borraron al terminar. Corrieron con `GOTOOLCHAIN=local`, salvo el `go env` de la primera fila, que muestra el valor por defecto, y el primero de los dos `go mod init`. Salvo en la fila de `go mod init`, cada módulo declaraba `go 1.27` y `toolchain go1.27.3`.

## Versión de partida

- Versión: `go version go1.27.3 linux/amd64`.
- Fecha: 2026-10-24.
- `GOROOT`: `~/.local/opt/go1.27.3`, instalado a mano; `~/.local/opt/go` apunta a él y `go` está en el `PATH`.
- Entorno por defecto: `GOTOOLCHAIN=auto`, `GOAMD64=v1`, `CGO_ENABLED=1`. El repositorio y la CI ejecutan `go` con `GOTOOLCHAIN=local`, para que nunca se descargue otro toolchain en silencio.
- `go.mod`: `go 1.27`, `toolchain go1.27.3` e `ignore ./build`.

```console
$ go version && go env GOROOT GOTOOLCHAIN GOAMD64 CGO_ENABLED
go version go1.27.3 linux/amd64
/home/scami/.local/opt/go1.27.3
auto
v1
1
```

## Hechos reverificados

Los hechos de R4 y de la revisión técnica que usan las Partes I y II (las filas siguientes) siguen valiendo en go1.27.3; entre ellos, solo cambia el texto que escribe `go mod init`, porque es la versión instalada: ahora `go 1.27.3`. Los demás ids de esos dos documentos no se reverificaron aquí. La columna «Test que lo fija» se rellena cuando cada hecho queda fijado a su test en `internal/toolchain/` (spec 11.4: «cada hecho de versión que aparece en el libro está fijado a un test»). Hasta entonces lleva `—`.

| Hecho | Comando | Resultado en go1.27.3 | Capítulo | Test que lo fija |
|---|---|---|---|---|
| El toolchain instalado es go1.27.3, con `GOAMD64=v1` y `CGO_ENABLED=1` por defecto (en 1.26.5, tech-check V-t0) | `go version && go env GOROOT GOTOOLCHAIN GOAMD64 CGO_ENABLED` | `go version go1.27.3 linux/amd64`, `/home/scami/.local/opt/go1.27.3`, `auto`, `v1`, `1` | 1 | `TestRuntimeVersionMatchesToolchainLine` (solo la versión de Go) |
| `go mod init` escribe la versión completa en la línea `go` (R4 V15: en 1.26.5 escribía `go 1.26.5`, aunque las notas de 1.26 anunciaban `go 1.(N-1).0`) | `go mod init example.com/m` | `go 1.27.3`, no `go 1.27.0`, y ninguna línea `toolchain`; igual con `GOTOOLCHAIN=auto` y con `local`. El capítulo 1 la reescribe como `go 1.27` + `toolchain go1.27.3`. Un `go.mod` con esas dos líneas e `ignore ./build` no cambia tras `go list`, `go build`, `go vet` ni `go mod tidy` | 1 | `TestToolchainFacts/go_mod_init_writes_full_version` (solo la línea `go 1.27.3`, con `GOTOOLCHAIN=local`); las tres líneas del `go.mod` del repositorio, `TestGoMod/lines` |
| La directiva `ignore` saca `build/` de `./...` (en 1.26.5, tech-check V-t10) | `ignore ./build` en el `go.mod`, un `build/x/main.go` que no compila (`package main`, una línea en blanco y, en la línea 3, `func main() { this does not compile }`) y `GOTOOLCHAIN=local go list ./...` | lista solo `example.com/m` y `example.com/m/internal/a`, y sale con 0; `go build ./...` y `go vet ./...` también salen con 0. Sin la línea `ignore`, `go list` añade `example.com/m/build/x`, y `go build ./...` falla con `build/x/main.go:3:20: syntax error: unexpected name does at end of statement` | 1 | `TestToolchainFacts/ignore_directive_hides_build` (solo `go list`) |
| Un `panic` de Go sin recuperar sale con 2 (R4 V13) | binario hecho con `go build` cuyo `main` hace `panic("boom")` | sale con 2; stderr empieza por `panic: boom`, seguido de la traza (`goroutine 1 [running]:`) | 10 | — |
| `os.Exit` se salta los `defer` (R4 V13) | binario hecho con `go build` cuyo `main` hace `defer fmt.Println("deferred ran")` y después `os.Exit(101)` | sale con 101; stdout y stderr vacíos, así que el `defer` no corre | 10 | — |
| `go run` enmascara el código de salida (R4 V13) | `go run` del programa de la fila anterior | stderr `exit status 101`, y `go run` sale con 1. Con el programa del `panic`: la traza, `exit status 2`, y también sale con 1 | 10 | — |
| `debug.ReadBuildInfo` en un `go build` sin VCS | `go build -buildvcs=false` de un programa que imprime `bi.Main.Version`, `bi.GoVersion` y `runtime.Version()` | `Main.Version` es `"(devel)"` y `GoVersion` es `"go1.27.3"`, igual que `runtime.Version()`. Por eso `ki --version` lleva una constante propia más `bi.GoVersion`. Con VCS, `Main.Version` cambia con el estado del repositorio: si el módulo está en la raíz de un repositorio git sin etiquetas semver (una etiqueta anotada `ch01-s1` no cuenta), da la pseudo-versión `v0.0.0-20261024213547-ad33e4c9f4d2`; con cambios sin commit, la misma con `+dirty`; con la etiqueta `v0.1.0`, `v0.1.0`; si el módulo está en un subdirectorio del repositorio y el `go.mod` de la raíz no existe o declara un módulo cuya ruta no prolonga la suya, `(devel)` más los ajustes `vcs`, `vcs.revision`, `vcs.time` y `vcs.modified`; un módulo anidado cuya ruta prolonga la del módulo de la raíz (`example.com/root/sub` bajo `example.com/root`) recibe la pseudo-versión | 1 | `TestToolchainFacts/plain_build_has_devel_version` (solo `Main.Version`, sin VCS) |
| `go help testflag` lista los flags de `go test` que usa el libro (en 1.26.5, R4 V19 para `-artifacts`) | `go help testflag` | lista `-run`, `-count`, `-shuffle`, `-json`, `-cover`, `-fuzz`, `-fuzztime`, `-bench`, `-benchmem` y `-artifacts` (este, con «Save test artifacts in the directory specified by -outputdir.») | 1 (`-run`, `-count`, `-shuffle`, `-json`); 2 y 4 (`-bench`, `-benchmem`); 5 (`-fuzz`, `-fuzztime`); 34 (`-artifacts`); `-cover`: R4 lo sitúa en el 1, pero el plan de ese capítulo no lo usa | `TestToolchainFacts/testflag_lists_shuffle_fuzz_artifacts` (`-shuffle`, `-fuzz` y `-artifacts`) |
| `go test -race` detecta una carrera (R4 V9) | `go test -race` sobre un test en el que dos goroutines lanzadas con `wg.Go` hacen `n++` sin sincronizar | stdout contiene `WARNING: DATA RACE` y `race detected during execution of test`; el test falla y `go test` sale con 1. Sin `-race`, el mismo test pasa | 1 | — |
| `-race` necesita cgo (R4 V9) | `CGO_ENABLED=0 go test -race` sobre el mismo módulo | `go: -race requires cgo; enable cgo by setting CGO_ENABLED=1`, y sale con 2 | 1 | `TestToolchainFacts/race_requires_cgo` (solo el código 2 y el texto `-race requires cgo`) |
| `errors.AsType` (1.26) funciona con instanciación explícita (R4 V14) | un programa con `errors.AsType[*fs.PathError](err)` sobre el error de `os.Open` de un fichero que no existe | compila y corre: `ok` es `true`, con `Op` `open` y la ruta del fichero | 4, 19 | — |
| Las restricciones autorreferentes (1.26) compilan (R4 V14) | el mismo programa, con `type Adder[A Adder[A]] interface{ Add(A) A }` y una `Sum[A Adder[A]]` sobre un `type Num int` | compila y corre: `Sum(Num(0), []Num{1, 2, 3})` da 6 | 17 | — |
| `new(expr)` (1.26) acepta una expresión (R4 V14) | el mismo programa, con `new(int64(300))` | compila y corre: un `*int64` que apunta a 300 | 11 | — |
| Go 1.27 admite métodos genéricos sobre tipos concretos (no existían en 1.26) | el mismo programa, con `func (s *Set) Map[U any](f func(int) U) []U` sobre `type Set struct{ items []int }` | compila y corre, con el argumento de tipo inferido (`s.Map(func(x int) string {…})`) y explícito (`s.Map[float64](…)`). Un método con parámetros de tipo en una interfaz sigue rechazado, con un error de sintaxis que también da `gofmt`: `interface method must have no type parameters` | 4, 17 | `TestToolchainFacts/generic_method_on_concrete_type` (el rechazo en interfaces no se fija) |
| La compilación cruzada no necesita nada más que `GOOS` y `GOARCH` | `GOOS=windows GOARCH=amd64 go build -o /dev/null .` y `GOOS=linux GOARCH=arm64 go build -o /dev/null .` sobre el programa de `debug.ReadBuildInfo` | las dos salen con 0, sin errores; para los dos destinos, `go env CGO_ENABLED` da `0` | 1 | — |
| `go vet` y `gofmt` quedan limpios sobre los programas anteriores | `go vet ./...` en cada módulo y `gofmt -l` sobre sus ficheros | `go vet ./...` sale con 0 en todos, y `gofmt -l` no lista ningún fichero (salvo el `build/x/main.go` roto a propósito y la interfaz con el método genérico, que se dejaron fuera) | 1 | — |

## CI

Aquí van las versiones de `actions/checkout` y `actions/setup-go` que usa `.github/workflows/ci.yml`. Vacío hasta que exista ese workflow.

## Delve

Aquí va la versión fijada de `dlv`, con su fecha y el comando `go install github.com/go-delve/delve/cmd/dlv@vX.Y.Z` (nunca `@latest`). Vacío hasta el capítulo 10.

## Historial de cambios de versión

Una línea por cada cambio de versión de Go: la fecha, la versión de origen y la de destino, y los canarios que corrieron. Vacío: el libro sigue en go1.27.3.

## Etiquetas verificadas con otro parche

`ci/check-tags.sh` salta (`skip <tag>: needs <goversion>`) las etiquetas cuyo `go.mod` pide un parche de Go distinto del instalado; esas etiquetas siguen verificadas con su versión y se listan aquí. Vacío por ahora.
