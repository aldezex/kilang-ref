---
chapter: 10
title: El intérprete CEK
part: I
hours: 9
milestone: **M1**: `ki run fib.ki` imprime `6765`
---

## Objetivo

Al terminar, Ki ejecuta programas. Es el hito M1, y con él acaba la Parte I:

```console
$ cat fib.ki
fn fib(n: Int) -> Int {
  if n < 2 { n } else { fib(n - 1) + fib(n - 2) }
}

fn main() {
  let x = fib(20)
  print(x)
}
$ ki run fib.ki
6765
```

## Teoría

### La máquina CEK

Recorrer el árbol con recursión de Go heredaría su pila, y desbordarla mata el proceso. Una **máquina abstracta** guarda en datos lo que la recursión esconde: ejecutar es un bucle de transiciones.

Este intérprete es el **oráculo**: la referencia con la que se comparan los otros backends. Tiene tres partes:

- **Control:** la expresión que toca evaluar, o el valor que acaba de salir.
- **Entorno:** las variables de la función en curso.
- **Continuación** (*Kontinuation*, la K): qué hacer con ese valor, una pila de marcos.

Una expresión compuesta empuja un marco y pasa a su primera parte; un valor va al marco de arriba. Así corre `fn f() -> Int { 1 + 2 }`:

| Control | Qué hace | Pila |
|---|---|---|
| `add_int(1, 2)` | empuja un `primFrame`, pasa al primer argumento | `[prim]` |
| `1` | pasa a valor | `[prim]` |
| valor 1 | saca el marco, empuja otro con `first: 1`, pasa al segundo | `[prim 1]` |
| `2` | pasa a valor | `[prim 1]` |
| valor 2 | saca el marco y calcula: valor 3 | `[]` |

Los cinco marcos:

| Marco | Espera | Cuando llega |
|---|---|---|
| `primFrame` | un argumento de una primitiva | pasa al segundo o calcula |
| `argFrame` | la función o un argumento | con todos, llama |
| `letFrame` | la inicialización de un `let` | lo escribe y sigue |
| `matchFrame` | lo que compara un `match` | elige el brazo |
| `retFrame` | el cuerpo de una función | recorta los entornos y baja el valor |

Sacar el `retFrame` es volver. Como la continuación son datos, capturarla, en el capítulo 21, será copiar un trozo de esa pila.

### Interfaces selladas y uniones a mano

Ki usa las dos formas de un tipo suma, según cuántos valores hay y quién los guarda:

- **Un struct con `Kind`** (capítulo 6) para los nodos: muchos, en un array sin punteros, vivos toda la compilación.
- **Una interfaz sellada** (capítulo 3) para `Value` y `frame`: formas con campos distintos, cortas de vida. Cada `case` del type switch ve solo los campos de su forma.

El precio de la interfaz: guardar en ella un `IntV` reserva memoria, salvo de 0 a 255.

:::from Rust
Un `enum Value` con `match` daría la exhaustividad en compilación; aquí la dan un `default` y un test.
:::

### `int64`: desborda sin avisar

En Go, el desbordamiento con signo está definido: sin panic, `MaxInt64 + 1` da `MinInt64`. Solo una expresión constante da error de compilación.

Ki exige un panic: cada operación calcula `r` y comprueba, con aritmética explícita, sin `math/bits`:

- **Suma:** `(a^r)&(b^r) < 0`. `x^r` tiene el bit de signo a 1 si `x` y `r` difieren en signo, y una suma desborda si los dos operandos difieren del resultado.
- **Resta:** `(a^b)&(a^r) < 0`. Operandos de signo distinto, y un resultado sin el signo de `a`.
- **Producto:** `r/b != a`: si dio la vuelta, dividir no devuelve `a`. Un operando 0 va antes, porque dividir por 0 sí es un panic de Go.
- **El punto ciego:** en Go, `MinInt64 / -1` no falla y da `MinInt64`, así que `MinInt64 * -1` pasaría la prueba del producto. El par va aparte.
- **Negar `MinInt64`** también desborda.

`/` trunca hacia cero y `%` toma el signo del dividendo: `-7 / 2` es -3 y `-7 % 3` es -1. En Ki, `MinInt64 / -1` es un panic y `MinInt64 % -1` vale 0.

:::more Desplazamientos en Go
Ki aún no tiene `<<` ni `>>`, y Core no tiene primitivas de desplazamiento. Para cuando los tenga:

- `<<` también desborda sin avisar: con `var x int64 = 1`, `x << 63` es `MinInt64`.
- Un contador igual o mayor que el ancho está definido: con `var c uint = 64`, `x << c` es 0, `8 >> c` es 0 y `-1 >> c` es -1.
- Con signo, `>>` es aritmético: `-8 >> 1` es -4.
- Un contador negativo es un panic en ejecución.
:::

### Pilas como slices

Empujar es `append` y sacar es `s[:n-1]`. La máquina tiene dos pilas: `stack []frame` y `envs [][]Value`.

El «marco» del capítulo 9, el de `FrameSize` y `Slot`, es aquí un entorno, y sus slots, casillas.

El código indexa `envs` y nunca guarda `&envs[i]`, porque un `append` puede copiar las cabeceras a otro array:

```go
envs := make([][]int, 1, 1)
envs[0] = []int{1, 2}
p, slot := &envs[0], &envs[0][0]
envs = append(envs, []int{3}) // no room: the headers are copied to a new array
fmt.Println(p == &envs[0], slot == &envs[0][0]) // false true
```

`p` se queda en el array viejo. Las casillas no se mueven: cada cabecera apunta a su propio array.

:::from Rust
`let p = &v[0]; v.push(2); use(p)` no compila. Go lo acepta, y la regla es tuya.
:::

### Panics de Ki y de Go

Un panic de Ki es un estado: la máquina escribe su mensaje, guarda `Halt{Code: 101}` y el bucle termina.

Un `panic` de Go es un bug del compilador, y el `recover` de cada pase (capítulo 9) lo convierte en un ICE: sin recuperar saldría con 2, el código del uso incorrecto.

| Código | Significado |
|---|---|
| 0 | terminó bien |
| 1 | errores de compilación, fichero ilegible o stdout perdido |
| 2 | uso incorrecto de `ki` |
| 70 | ICE |
| 101 | panic de Ki |
| 103 | desbordamiento de la pila de Ki |

`go run` enmascara el código: con `os.Exit(101)` dentro, sale con 1. Por eso el runner ejecuta binarios construidos.

### Llamadas de cola

Una llamada normal empuja un `retFrame`, que recuerda el entorno de quien llama, y añade un entorno.

Una llamada está en cola si es lo último que hace su función. Si el marco de arriba ya es un `retFrame`, nada espera el valor salvo el retorno: la llamada reutiliza ese marco y **sustituye** el entorno.

`Let` y `Match` ya no están en la pila cuando corre su cuerpo o su brazo: `count(n - 1)` en un brazo está en cola. En `n + sumTo(n - 1)`, no, porque un `primFrame` espera.

Ki aún no tiene `while` (capítulo 23): su bucle es la llamada de cola, de profundidad constante.

### El límite de la pila

La recursión de Ki vive en un slice, en el heap de Go, así que el límite es nuestro: la variable de entorno `KI_STACK_MB`, en MiB, 256 por defecto. Cada MiB son 1<<14 marcos, a 64 bytes nominales, sin contar los entornos, que crecen con las variables de la función.

Empujar un `retFrame` con la pila en el límite termina en `panic: stack overflow at <posición>`, con 103. Una llamada de cola no empuja, así que pasa.

La pila de Go es lo contrario. Su límite es 1 GB y, como se duplica, muere con 512 MiB: `fatal error: stack overflow`. Ningún `recover` lo detiene y sale con 2.

### `kirt/out`: un solo escritor

`kirt` es el runtime, y nace con este paquete. Los tres backends escriben por él: con un escritor cada uno, tendrían su orden de salida y su vaciado.

- **`bufio.Writer`** junta las escrituras en 64 KiB: una llamada al sistema por línea saldría cara. Recuerda su primer error y lo repite.
- **`sync.Mutex`** hace de cada `Write` o `Flush` un todo, para cuando escriban varias goroutines.
- **Una terminal** vacía en cada línea: `f.Stat()` da el modo, y `os.ModeCharDevice` marca un dispositivo de caracteres (también `/dev/null`). `Stderr` vacía siempre por líneas.
- **`out.Exit`** vacía y llama a `os.Exit`, que se salta los `defer`. Si stdout falló y el código iba a ser 0, lo dice por stderr y sale con 1.

Un panic vacía stdout antes de escribir su mensaje: así sale detrás de lo impreso.

### El test de imports

Un test hace cumplir tres reglas; de `kirt`, aún solo existe `out`:

- **`kirt` solo importa la biblioteca estándar y a sí mismo.** `ki build` (capítulo 48) copiará `kirt` junto al programa generado, y esa copia tiene que compilar sola.
- **De `kirt`, el oráculo solo importa `out`, `splitmix` y `fmtnum`,** hojas que no importan nada más de `kirt`. Si usara el planificador o los canales de `kirt`, no podría encontrar sus fallos.
- **`unsafe` solo bajo `kirt/heap`,** el único sitio donde un entero se convierte en puntero.

`parser.ParseFile` (capítulo 7), con `parser.ImportsOnly`, lee solo los `import`, así que un comentario no cuenta.

Cada regla corre sobre el repositorio y sobre árboles pequeños con el error que debe cazar: una regla que nunca se ha visto encontrar algo no demuestra nada.

### Goroutines, lo justo

El test del escritor las necesita; la Parte IV las enseña. `go f()`, los canales y `select` vienen del capítulo 8. Para esperar a varias, un `sync.WaitGroup`, en la forma larga de `wg.Go` (capítulo 1): `wg.Add(1)` avisa antes de lanzar, `defer wg.Done()` descuenta y `wg.Wait()` espera.

Sin el `Mutex`, dos goroutines tocarían el mismo `bufio.Writer` a la vez, y `go test -race` lo detecta.

### El proceso ayudante

Un test no puede ver morir a su propio proceso, así que relanza su binario con otro cometido, la idea de `runKi` (capítulo 1):

```go
// -test.run is go test's -run, as the test binary spells it
cmd := exec.Command(os.Args[0], "-test.run=^TestHelperProcess$")
cmd.Env = append(os.Environ(), "KI_TEST_HELPER=panic")
```

`TestHelperProcess` hace `return` si la variable no está (un `go test` normal); si está, `panic("boom")` o `os.Exit(101)`. El padre lee su código de salida.

### Delve

`dlv` depura el binario real. `dlv exec` arranca `ki`; tras `--` van sus argumentos. `break` para en una función, `continue` sigue hasta otra parada o el final, `print` evalúa una expresión y `clearall` borra las paradas.

### M1: `fib.ki`, etapa por etapa

Cada capítulo ha construido una fila, sobre `tests/golden/ch10/fib.ki` (`fn` está en la línea 4):

| Pase | Qué hace | Cómo verlo |
|---|---|---|
| `lex` | `4:1 KwFn`, `4:4 LowerIdent(fib)`, … | `ki emit tokens` |
| `parse` | `(fn fib (params (n Int)) …` | `ki emit ast` |
| `resolve` | `fib` es una definición; `print`, un extern | `ki check`: silencio |
| `check` | `n < 2` es `Bool`; las ramas, `Int` | `ki check`: silencio |
| `elaborate` | `if` a `match`, `+` a `add_int` | `ki emit core` |
| `lint` | valida ese Core | ICE si no vale |
| `run` | `6765` | `ki run` |

## Tests primero

{{cards}}

Por cada tarjeta: escribe el test, míralo fallar, implementa lo justo para el verde, y solo entonces despliega su referencia.

Los ayudantes de test nuevos van al principio de su fichero de `ref/ch10-sK`; `runCLI` y `writeKi` vienen de los capítulos 1 y 9.

## Implementación

### Sesión 1: valores y la máquina (≈ 2 h)

#### Los tests, en rojo

1. **Escribe T10.01–T10.11.** La interfaz, completa:

```go
package interp
type Value interface{ value() }
type IntV struct{ V int64 }
type BoolV struct{ V bool }
type UnitV struct{}
type FunV struct{ Fun core.FunID }
func Show(v Value) string
type Halt struct{ Code int }
type Stats struct{ Steps, MaxDepth, MaxEnvs int }
func New(p *core.Program, sm *base.SourceManager, out, err io.Writer) *Machine
func (m *Machine) RunMain() Halt
func (m *Machine) Call(f core.FunID, args []Value) (Value, bool) // false: the program panicked
func (m *Machine) Stats() Stats

package testsupport
func FunNamed(t *testing.T, c *Compiled, name string) core.FunID
```

2. **Míralos fallar.** Sobre `ch09-s3`, `internal/interp` no compila:

```console
$ go test ./internal/interp
# github.com/aldezex/kilang/internal/interp [github.com/aldezex/kilang/internal/interp.test]
internal/interp/value_internal_test.go:20:9: undefined: Value
…
FAIL	github.com/aldezex/kilang/internal/interp [build failed]
```

#### El código

{{file ch10-s1 internal/interp/value.go}}

{{file ch10-s1 internal/interp/machine.go}}

{{diff ch09-s3..ch10-s1 internal/testsupport/compile.go}}

- **`run`** es el bucle: con una expresión llama a `step`; con un valor, a `applyFrame`.
- **El `default: panic("unreachable")` de `Show`** se prueba con `fakeV`, un `Value` que no conoce, en un test interno: solo el paquete puede escribir `value()`.

{{reftest T10.06}}

Si te atascas o tu versión se desvía demasiado, copia el fichero de `ref/ch10-s1` y sigue.

Al terminar: tus tests en verde, commit y etiqueta `ch10-s1`; compárate con `ref/ch10-s1`.

### Sesión 2: primitivas comprobadas y panics (≈ 2,5 h)

#### Los tests, en rojo

1. **Escribe T10.12–T10.34.** Interfaz nueva, sin exportar, para los tests internos:

```go
func addInt(a, b int64) (int64, bool) // true: it overflowed
func subInt(a, b int64) (int64, bool)
func mulInt(a, b int64) (int64, bool)
func negInt(a int64) (int64, bool)
type divErr uint8                     // divOK, divByZero, divOverflow
func divInt(a, b int64) (int64, divErr)
func remInt(a, b int64) (int64, divErr)
func applyPrim(p core.Prim, a, b Value) (Value, string) // "": it has a value; otherwise the panic's message
func divResult(r int64, e divErr) (Value, string)
```

2. **Míralos fallar.** No compila: `prims_internal_test.go:44:27: undefined: addInt`.

#### El código

{{file ch10-s2 internal/interp/prims.go}}

{{diff ch10-s1..ch10-s2 internal/interp/machine.go}}

- **`applyPrim`:** el comprobador de tipos garantiza la forma de los operandos; si falla, `v.(IntV)` hace un panic de Go: un ICE.
- **`intResult(addInt(…))`** usa una regla de Go: los resultados de una llamada pueden ser todos los argumentos de otra.
- **`Call` vacía la pila y el `halt`.** Un panic deja marcos a medias, y el valor de la siguiente llamada iría a ellos.

Quita de `mulInt` el caso del punto ciego y su test lo ve: `mulInt(-9223372036854775808, -1) overflow = false, want true`.

{{reftest T10.14}}

{{reftest T10.34}}

Si te atascas o tu versión se desvía demasiado, copia el fichero de `ref/ch10-s2` y sigue.

Al terminar: tus tests en verde, commit y etiqueta `ch10-s2`; compárate con `ref/ch10-s2`.

### Sesión 3: llamadas, `match` y llamadas de cola (≈ 2,5 h)

#### Los tests, en rojo

1. **Escribe T10.35–T10.53.** T10.52 y T10.53 van en `machine_internal_test.go`, y usan esto:

```go
type frame interface{ frame() }
func (m *Machine) applyFrame(f frame, v Value)
func framesFor(setting string) int // frames for that value of KI_STACK_MB; bad or <= 0: the default, 256
```

2. **Míralos fallar.** No compila: `machine_internal_test.go:65:14: undefined: framesFor`.

#### El código

{{diff ch10-s2..ch10-s3 internal/interp/machine.go}}

{{diff ch10-s2..ch10-s3 internal/interp/prims.go}}

- **`argFrame`** acumula la función llamada (el *callee*) y los argumentos. El callee es una expresión más: una función en una variable se llama igual.
- **`enter`** aplica la regla de cola. La función que arranca `Call` no tiene `retFrame`, así que su primera llamada de cola empuja uno.

{{reftest T10.43}}

{{reftest T10.48}}

:::more Por qué `MaxDepth` llega a 16 387
El límite decide cuándo se rechaza una llamada; no es una cota de `MaxDepth`, que cuenta además los marcos que evaluar los argumentos ya empujó.

Con `KI_STACK_MB=1`, el límite es 16 384. En `fn f(n: Int) -> Int { 1 + f(n + 1) }`, con `fn main() { let x = f(0) }`, cada nivel deja un `primFrame` y un `retFrame`; con el `letFrame` de `main`, son 1 + 2k marcos.

La primera llamada que encuentra el límite ve 16 385 (k = 8 192). Antes, su `argFrame` y el `primFrame` de `n + 1` ya habían subido el máximo a 16 387.

Ese exceso depende de lo que anidan las expresiones alrededor de la llamada y dentro de ella, acotado por `MaxNesting` y `MaxChain` juntos.
:::

Si te atascas o tu versión se desvía demasiado, copia el fichero de `ref/ch10-s3` y sigue.

Al terminar: tus tests en verde, commit y etiqueta `ch10-s3`; compárate con `ref/ch10-s3`.

### Sesión 4: `kirt/out`, `print`, `ki run` y el hito M1 (≈ 2 h)

#### Los tests, en rojo

1. **Escribe T10.54–T10.92,** con los golden de `tests/golden/ch10/`. Cambia la lista `commands:` de `cli_test.go`: acaba en `emit, run`. La interfaz nueva:

```go
package out // kirt/out
type Writer struct { mu sync.Mutex; w *bufio.Writer; lineFlush bool }
func New(w io.Writer, size int, lineFlush bool) *Writer // size <= 0: 64 KiB
func ForFile(f *os.File) *Writer
func (w *Writer) Write(p []byte) (int, error)
func (w *Writer) WriteString(s string) (int, error)
func (w *Writer) Flush() error
var Stdout, Stderr *Writer
func FlushAll() error // the error of Stdout's flush
func Exit(code int)

package coretest // internal/core/coretest
func ExternProgram(name string) *core.Program

package driver
// usage: ki run [--backend=interp] [--diagnostics=human|short|json] FILE
func (s *Session) execute(p *core.Program) int
```

2. **Escribe ya `coretest`:** sin él no arrancan dos paquetes de test. No va en `testsupport`, que importa `driver`: un test de `driver` formaría un ciclo.

{{file ch10-s4 internal/core/coretest/extern.go}}

3. **Míralos fallar.** `kirt/out`, `internal/driver` y `cmd/ki` no compilan, y el golden de `fib` falla. Un panic de Go corta el binario de `internal/interp` (`print` aún no corre). `kirt` e `internal/toolchain` ya pasan: las reglas se cumplen, y los hechos de Go no dependen de ti.

Tres de los golden:

{{file ch10-s4 tests/golden/ch10/fib.ki}}

{{file ch10-s4 tests/golden/ch10/print_then_panic.ki}}

{{file ch10-s4 tests/golden/ch10/division.ki}}

#### El escritor

{{file ch10-s4 kirt/out/writer.go}}

{{reftest T10.57}}

Aquí, ocho goroutines escriben a la vez y otra llama a `Flush` hasta el `close(stop)`. Un canal cerrado siempre está listo, y `select` con `default` lo consulta sin bloquearse. `runtime.Gosched()` no sincroniza: solo cede el procesador.

`string(rune('a'+g))` convierte un code point en texto.

#### `print` y los externs

`print` es un `Fun` sin cuerpo: `enter` lo ejecuta por la tabla, sin marco ni entorno.

{{file ch10-s4 internal/interp/externs.go}}

{{diff ch10-s3..ch10-s4 internal/interp/machine.go}}

{{diff ch10-s3..ch10-s4 internal/interp/prims.go}}

`haltAt` pregunta si `out` sabe vaciarse (capítulo 3): `interp` no conoce a `Writer`.

{{reftest T10.92}}

#### `ki run`

Compila, exige `main` y ejecuta la máquina como un pase más.

{{file ch10-s4 internal/driver/run.go}}

{{diff ch10-s3..ch10-s4 internal/driver/commands.go}}

`pipeline.go` solo cambia un comentario:

{{diff ch10-s3..ch10-s4 internal/driver/pipeline.go}}

{{file ch10-s4 cmd/ki/main.go}}

{{file ch10-s4 docs/errors/E0303.md}}

`cmd/ki` termina en `out.Exit`: con `os.Exit`, `ki run fib.ki | cat` no imprime nada y sale con 0. `go test ./cmd/ki` lo ve; `go test ./tests` no, porque no pasa por `main`.

`tests/main_test.go` y `ci/ci-local.sh` quitan `KI_STACK_MB` del entorno:

{{diff ch10-s3..ch10-s4 tests/main_test.go}}

{{diff ch10-s3..ch10-s4 ci/ci-local.sh}}

#### Las reglas y los hechos

{{file ch10-s4 kirt/imports_test.go}}

:::more Cómo recorre y lee `imports_test.go`
- **`filepath.SkipDir`,** devuelto desde la función de `WalkDir`, salta ese directorio.
- **`file.Imports` y `spec.Path.Value`** dan cada import y su ruta tal como está escrita, con comillas; `strconv.Unquote` (capítulo 7) se las quita.
- **Un primer elemento sin punto** es la biblioteca estándar: una ruta de módulo empieza por un dominio.
- **La barra de `modulePath+"/kirt/"`** deja fuera a `kirtx`.
:::

Estos dos ficheros montan el proceso ayudante; `// bookx: not-in-book` dispensa de tarjeta al segundo.

{{file ch10-s4 internal/toolchain/exitcodes_test.go}}

{{file ch10-s4 internal/toolchain/helper_process_test.go}}

{{reftest T10.77}}

{{reftest T10.79}}

#### Delve

Construye `ki` (`go build -o build/bin/ki ./cmd/ki`) e instala Delve en una versión fijada, nunca `@latest`. No entra en `go.mod`. `dlv` queda por defecto en `~/go/bin`: tiene que estar en tu `PATH`.

```console
$ GOTOOLCHAIN=local go install github.com/go-delve/delve/cmd/dlv@v1.27.2
$ dlv exec build/bin/ki -- run tests/golden/ch10/fib.ki
(dlv) break interp.(*Machine).enter
Breakpoint 1 set at 0x583f96 for github.com/aldezex/kilang/internal/interp.(*Machine).enter() ./internal/interp/machine.go:457
(dlv) continue
> [Breakpoint 1] github.com/aldezex/kilang/internal/interp.(*Machine).enter() ./internal/interp/machine.go:457 (hits goroutine(1):1 total:1) (PC: 0x583f96)
Warning: debugging optimized function
…
(dlv) print vals
[]github.com/aldezex/kilang/internal/interp.Value len: 2, cap: 2, [
	github.com/aldezex/kilang/internal/interp.FunV {Fun: 1},
	github.com/aldezex/kilang/internal/interp.IntV {V: 20},
]
(dlv) print len(m.stack)
1
(dlv) clearall
Breakpoint 1 cleared at 0x583f96 for github.com/aldezex/kilang/internal/interp.(*Machine).enter() ./internal/interp/machine.go:457
(dlv) continue
6765
Process 1668899 has exited with status 0
(dlv) exit
```

La parada es `fib(20)`, porque `enter` no ve la llamada a `main`: `fib` es la función 1, y en la pila está el `letFrame` de `main`.

:::more Variables en un binario optimizado
Es lo que avisa el `Warning`: algunas variables no se ven. `next` avanza una línea sin entrar en las llamadas, y tras dos, `print callee` da `could not find symbol value for callee`. Con `go build -gcflags=all='-N -l'`, sin optimizar y sin inlining, da `FunV {Fun: 1}`.
:::

Si te atascas o tu versión se desvía demasiado, copia el fichero de `ref/ch10-s4` y sigue.

Al terminar: tus tests en verde, commit y etiqueta `ch10-s4`; compárate con `ref/ch10-s4`.

## Ejecútalo

Desde la raíz de tu repo, con `ki` construido; crea `so.ki` y `nomain.ki`:

```console
$ build/bin/ki run tests/golden/ch10/fib.ki
6765
$ echo $?
0
$ build/bin/ki run tests/golden/ch10/print_then_panic.ki
1
panic: integer overflow at tests/golden/ch10/print_then_panic.ki:8:29
$ echo $?
101
$ build/bin/ki run tests/golden/ch10/division.ki
3
-3
1
-1
1
$ cat so.ki
fn f(n: Int) -> Int { 1 + f(n + 1) }
fn main() { print(f(0)) }
$ KI_STACK_MB=1 build/bin/ki run so.ki
panic: stack overflow at so.ki:1:27
$ echo $?
103
$ cat nomain.ki
fn helper() -> Int { 1 }
$ build/bin/ki run nomain.ki
error[E0303]: no `main` function
 --> nomain.ki:1:1
  |
1 | fn helper() -> Int { 1 }
  | ^ this file has no `fn main()`
  = note: `ki run` starts the program at `fn main()`; `ki check` does not need one
  = help: add `fn main() { … }`
$ echo $?
1
$ # /dev/full fails every write
$ build/bin/ki run tests/golden/ch10/fib.ki > /dev/full
ki: cannot write to stdout: write /dev/stdout: no space left on device
$ echo $?
1
```

## Ejercicios

:::exercise
**1. `ki run --stats`.** Imprime por stderr `steps: N, max depth: D` al terminar, con su test.
:::

:::solution
En `run.go`, un flag más en `runRun`, que pasa a `execute`:

```go
	var stats bool
	// in the function given to fileArgsWith:
	fs.BoolVar(&stats, "stats", false, "print the steps and the deepest stack on stderr at the end")
	// and at the end of runRun:
	return s.execute(p, stats)

func (s *Session) execute(p *core.Program, stats bool) int {
	var h interp.Halt
	s.runPass("run", func() {
		m := interp.New(p, &s.Sources, s.Stdout, s.Stderr)
		h = m.RunMain()
		if stats {
			st := m.Stats()
			fmt.Fprintf(s.Stderr, "steps: %d, max depth: %d\n", st.Steps, st.MaxDepth)
		}
	})
	return h.Code
}
```

La línea de uso pasa a ser `ki run [--backend=interp] [--stats] [--diagnostics=human|short|json] FILE`, en `commands.go` y en la constante `runUsage` de `run_test.go`. Las dos llamadas a `execute` del test del ICE en la máquina reciben un `false`. El test, en `TestRun`:

```go
t.Run("stats_go_to_stderr", func(t *testing.T) {
	rows := []struct {
		name, src, stderr string
		code              int
	}{
		{"returns", "fn main() { print(7) }\n", "steps: 5, max depth: 1\n", 0},
		{"panics", "fn main() { print(1 / 0) }\n", "panic: division by zero at PATH:1:21\nsteps: 8, max depth: 2\n", 101},
	}
	for _, row := range rows {
		path := writeKi(t, row.src)
		want := strings.ReplaceAll(row.stderr, "PATH", path)
		code, stdout, stderr := runCLI(t, "run", "--stats", path)
		if code != row.code || stderr != want {
			t.Errorf("%s: got %d, stdout %q, stderr %q, want %d and stderr %q", row.name, code, stdout, stderr, row.code, want)
		}
	}
	// Without the flag, no line is added.
	code, stdout, stderr := runCLI(t, "run", writeKi(t, "fn main() { print(7) }\n"))
	if code != 0 || stdout != "7\n" || stderr != "" {
		t.Errorf("without --stats: got %d, stdout %q, stderr %q, want 0, \"7\\n\" and nothing on stderr", code, stdout, stderr)
	}
})
```

`print(7)` da cinco pasos: la llamada, la función llamada, su marco, el argumento y otra vez el marco. Con `fib.ki`: `steps: 415931, max depth: 42`. La línea va por stderr, que no espera, así que redirigida puede salir antes que el `6765`.
:::

:::exercise
**2. `ki_print_bool`.** Añade el extern, su declaración en el preludio, su test y su golden.
:::

:::solution
En `stdlib/prelude.ki`:

```text
#[extern("ki_print_bool")]
pub fn printBool(x: Bool)
```

En la tabla de `externs.go`:

```go
	// printBool(x: Bool) writes true or false and a line feed, like print.
	"ki_print_bool": func(m *Machine, args []Value) Value {
		_, _ = io.WriteString(m.out, strconv.FormatBool(boolOf(args[0]))+"\n")
		return UnitV{}
	},
```

En `TestExterns`:

```go
t.Run("print_bool_writes_true_or_false", func(t *testing.T) {
	rows := []struct{ name, src, want string }{
		{"true", "fn main() { printBool(true) }", "true\n"},
		{"false", "fn main() { printBool(false) }", "false\n"},
		{"computed", "fn main() { printBool(1 < 2) }", "true\n"},
	}
	for _, row := range rows {
		h, stdout, stderr := runMain(t, row.src)
		if h != (interp.Halt{}) || stdout != row.want || stderr != "" {
			t.Errorf("%s: got %+v, stdout %q, stderr %q, want Halt{0}, stdout %q and no stderr", row.name, h, stdout, stderr, row.want)
		}
	}
})
```

El golden, `tests/golden/ch10/print_bool.ki` (`T10.93` es la tarjeta que añades):

```text
// test: T10.93
// run: run
// expect-stdout:
// | true
// | false
fn main() {
  printBool(1 < 2)
  printBool(2 < 1)
}
```

El preludio ha crecido, y siete subtests fijaban su tamaño a propósito. Se ponen en rojo y `go test ./...` los nombra. Cada uno pide uno más: funciones, definiciones, items o atributos, y las funciones y definiciones del fichero suben un índice. El menos evidente es el test de Lint que rechaza una variable sin ligar: `f` es ahora `Funs.Ptr(2)`.

Con `printBool(1)`, el comprobador de tipos ya da E0401: la firma del preludio basta.
:::

:::exercise
**3. `fib(30)` frente a Python.** Mídelo con `time` frente al mismo programa en Python.
:::

:::solution
```text
fn fib(n: Int) -> Int {
  if n < 2 { n } else { fib(n - 1) + fib(n - 2) }
}

fn main() {
  print(fib(30))
}
```

```python
def fib(n):
    return n if n < 2 else fib(n - 1) + fib(n - 2)

print(fib(30))
```

Como orden de magnitud, `ki run` tarda unos 0,85 s y `python3`, unos 0,074 s: unas once veces menos. `ki` da 51 158 202 pasos, cada uno con un `switch`, y reserva memoria para entornos y valores. Ser rápido no es su trabajo: es el oráculo.
:::

:::exercise
**4. Un bucle sin memoria.** Explica por qué este programa, `count.ki`, no crece en memoria:

```text
fn count(n: Int, acc: Int) -> Int {
  if n == 0 { acc } else { count(n - 1, acc + 1) }
}

fn main() {
  print(count(10_000_000, 0))
}
```

Compruébalo con `GODEBUG=gctrace=1 build/bin/ki run count.ki`, que escribe en stderr una línea por recolección; en `A->B->C MB`, `C` es lo que sigue vivo.
:::

:::solution
La llamada está en cola: el `matchFrame` ya salió de la pila, así que lo que hay arriba es un `retFrame`. `enter` lo reutiliza y sustituye el entorno; el viejo queda sin referencias y el recolector lo libera. Con `--stats`, del ejercicio 1: `steps: 220000020, max depth: 4`.

Son unas 1000 recolecciones en unos 4 s, casi todas iguales:

```text
gc 1008 @4.045s 2%: 0.061+0.13+0.19 ms clock, 0.98+0.011/0.098/0+3.0 ms cpu, 3->3->0 MB, 4 MB goal, 0 MB stacks, 0 MB globals, 16 P
```

`3->3->0 MB` es el heap al empezar, al acabar y vivo: cada vuelta reserva, pero nada se queda. Como contraste, este programa, que no es de cola, acaba con unos 400 MB de heap y más de 200 MB vivos:

```text
fn sumTo(n: Int) -> Int {
  if n == 0 { 0 } else { n + sumTo(n - 1) }
}

fn main() {
  print(sumTo(2_000_000))
}
```
:::

:::exercise
**5. Vaciar a la mitad.** Haz que `kirt/out` vacíe también cuando el buffer pasa de la mitad (`Buffered()` da los bytes que esperan). Mide si cambia algo con `print` en un bucle de 10⁶ iteraciones, contra un fichero: `/dev/null` vacía en cada línea.
:::

:::solution
En `writer.go`, un campo `half int` (`half: size / 2` en `New`) y, en `Write`:

```go
	if !w.lineFlush {
		n, err := w.w.Write(p)
		if err == nil && w.w.Buffered() > w.half {
			err = w.w.Flush()
		}
		return n, err
	}
```

El test, en `TestWriter`, con el borde exacto:

```go
t.Run("flushes_past_half", func(t *testing.T) {
	var dest bytes.Buffer
	w := out.New(&dest, 8, false)
	for _, step := range []struct {
		write string
		want  int // the bytes in the destination after it
	}{
		{"abcd", 0},   // exactly half stays in the buffer
		{"e", 5},      // one past half goes out, with what was waiting
		{"fgh", 5},    // the buffer starts again
		{"ijklm", 13}, // 8 bytes waiting
	} {
		if _, err := w.WriteString(step.write); err != nil {
			t.Fatalf("WriteString(%q) = %v, want nil", step.write, err)
		}
		if got := dest.Len(); got != step.want {
			t.Errorf("after WriteString(%q), the destination holds %d bytes, want %d", step.write, got, step.want)
		}
	}
})
```

Con `>=` en lugar de `>`, la primera fila falla. También falla el subtest del tamaño por defecto, que fijaba que el buffer guarda su tamaño entero. Ahora guarda la mitad: ahí, `chunk` mide `min(1024, row.bytes/2)`, el bucle escribe hasta `row.bytes/2`, y tras un byte más el destino tiene `row.bytes/2+1`.

El programa:

```text
fn loop(n: Int) {
  if n == 0 {
  } else {
    print(n)
    loop(n - 1)
  }
}

fn main() {
  loop(1_000_000)
}
```

Con un `Writer` que cuenta las escrituras de su destino, pasan de 106 a 211. A un fichero, los dos tardan unos 0,48 s y la salida es idéntica: nada medible, porque la máquina domina y una escritura cada 32 KiB sigue siendo rara. Lo que sí se nota es vaciar por líneas: con `> /dev/null`, un dispositivo de caracteres, tarda 0,87 s.
:::

## Preguntas de repaso

:::review
¿Por qué `Value` es una interfaz sellada y `core.Expr` un struct con `Kind`?
:::

:::answer
Los nodos son muchos y viven en un array sin punteros durante toda la compilación: una interfaz haría de cada uno una reserva y un puntero. Los valores tienen formas con campos distintos y vidas cortas, y el type switch solo deja leer los campos de cada forma. Se paga en reservas, y este intérprete no compite en velocidad.
:::

:::review
¿Por qué un panic de Ki no se implementa con `panic` de Go y un `recover`?
:::

:::answer
Porque un `panic` de Go en la máquina significa un bug del compilador, y ya tiene su camino: un ICE con 70. Si los dos usaran el mismo mecanismo, habría que distinguirlos por el valor, y un bug podría parecer un panic del programa. Como estado, `Halt{101}` es un resultado más, que los otros backends deben reproducir.
:::

:::review
¿Por qué `mulInt` necesita un caso aparte para `MinInt64` y `-1`, si ya divide de vuelta?
:::

:::answer
Porque `MinInt64 * -1` da `MinInt64` al dar la vuelta, y `MinInt64 / -1`, en Go, también da `MinInt64`, sin fallar: `r/b` es `a` y la prueba diría que no desbordó. Es el único par con ese punto ciego.
:::

:::review
¿Por qué `count(n - 1)` dentro de un `if` está en cola y `1 + count(n - 1)` no?
:::

:::answer
Un `if` es un `match` en Core, y su `matchFrame` sale de la pila antes de que el brazo corra: lo único que espera el valor es el `retFrame`. En `1 + …`, un `primFrame` espera el resultado para sumar, así que la función no ha terminado y su entorno tiene que seguir vivo.
:::

:::review
¿Por qué `retFrame` guarda un índice de `envs` y no `&envs[i]`?
:::

:::answer
Porque un `append` a `envs` puede copiar las cabeceras a otro array, y `&envs[i]` se quedaría en el viejo, sin que nada avise. El índice sigue valiendo tras cualquier `append`, y volver es solo cortar: `envs[:f.env+1]`.
:::

:::review
¿Por qué `out.Exit` vacía de forma explícita y no con `defer out.FlushAll()` en `main`?
:::

:::answer
Porque `os.Exit` no ejecuta los `defer`: el vaciado no ocurriría y lo que siguiera en el buffer se perdería, con código 0. Además, `Exit` necesita el resultado del vaciado para decidir el código: si la respuesta no se pudo escribir, 0 sería mentira.
:::

:::review
¿Por qué `haltAt` vacía stdout antes de escribir el mensaje del panic?
:::

:::answer
Porque stdout retiene y stderr no. Si los dos van al mismo sitio, como en `2>&1 | cat`, el mensaje saldría antes que las líneas previas al panic. El orden engañaría a quien lo lee.
:::

:::review
¿Por qué el test de imports corre también sobre árboles escritos a propósito?
:::

:::answer
Porque una regla que no encuentra nada puede estar rota: un prefijo mal escrito, un directorio que no recorre. Los árboles con un error, como `kirtx`, que solo empieza igual, demuestran que caza lo que dice.
:::

## Resumen

- Una máquina CEK guarda control, entorno y continuación en datos: ejecutar es un bucle.
- `Value` es una interfaz sellada; los nodos, un struct con `Kind`.
- Go da la vuelta sin avisar: Ki comprueba con aritmética explícita.
- Un panic de Ki es `Halt{101}`; un `panic` de Go, un ICE con 70.
- Una llamada de cola reutiliza el `retFrame`: los bucles no crecen.
- `KI_STACK_MB` da un límite propio, con salida 103.
- `kirt/out` es el único escritor, y `out.Exit` vacía antes de `os.Exit`.
- El proceso ayudante prueba lo que termina el proceso; `go/parser` hace cumplir las reglas de imports.

## Go en el trabajo

**Uniones etiquetadas.** Lua representa cada valor con un `TValue`: una unión y un byte de etiqueta. CPython usa las dos formas: `struct _expr`, con `kind` y una unión, para el AST, y un puntero a su tipo en los objetos, más cerca de una interfaz.

**Desbordamiento y CVE.** En CVE-2015-1538, en Android, una multiplicación sin comprobar en datos MP4 permitía ejecutar código a distancia.

La especificación de Go define el desbordamiento y prohíbe al compilador suponer que no ocurre. Donde es indefinido, como en C con signo, el compilador puede borrar la comprobación.

Definirlo no lo evita: CVE-2022-23772 es un desbordamiento en `Rat.SetString`, de `math/big`, con consumo de memoria sin control.

**El flush olvidado.** La documentación de `bufio.Writer` pide llamar a `Flush` al terminar. Sin él se pierde lo que quedaba, y un disco lleno puede no verse hasta ese vaciado.
