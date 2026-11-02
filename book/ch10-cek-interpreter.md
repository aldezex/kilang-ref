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

Un intérprete podría recorrer el árbol con funciones recursivas de Go, pero heredaría la pila de Go, y desbordarla mata el proceso. Una **máquina abstracta** guarda en datos lo que la recursión esconde: ejecutar es un bucle de transiciones. Este intérprete es el **oráculo**: la referencia con la que se comparan los otros backends.

La máquina CEK tiene tres partes:

- **Control:** la expresión que toca evaluar, o el valor que acaba de salir.
- **Entorno:** las variables de la función en curso.
- **Continuación** (*Kontinuation*, de ahí la K): qué hacer con ese valor, una pila de marcos.

Una expresión compuesta empuja un marco y pasa a su primera parte; un valor va al marco de arriba. Así corre `fn f() -> Int { 1 + 2 }`:

| Paso | Control | Qué hace | Pila después |
|---|---|---|---|
| 1 | `add_int(1, 2)` | empuja un `primFrame` y pasa al primer argumento | `[prim]` |
| 2 | `1` | lo convierte en valor | `[prim]` |
| 3 | valor 1 | saca el marco, empuja otro con `first: 1` y pasa al segundo | `[prim 1]` |
| 4 | `2` | lo convierte en valor | `[prim 1]` |
| 5 | valor 2 | saca el marco y calcula: el control es el valor 3 | `[]` |

Los cinco marcos:

| Marco | Espera el valor de | Cuando llega |
|---|---|---|
| `primFrame` | un argumento de una primitiva | pasa al segundo o calcula |
| `argFrame` | la función llamada o un argumento de la llamada | con todos, hace la llamada |
| `letFrame` | la inicialización de un `let` | lo escribe en su casilla y sigue con el cuerpo |
| `matchFrame` | lo que compara un `match` | elige el brazo |
| `retFrame` | el cuerpo de una función | recorta los entornos y pasa el valor al marco de abajo: es volver |

Que la continuación sean datos se cobra en el capítulo 21: capturarla será copiar un trozo de esa pila.

### Interfaces selladas y uniones a mano

Ki usa las dos formas de un tipo suma, según cuántos valores hay y quién los guarda:

- **Un struct con `Kind`** (capítulo 6) para los nodos: muchos, en un array sin punteros, vivos toda la compilación.
- **Una interfaz sellada** (capítulo 3) para `Value` y `frame`: formas con campos distintos, cortas de vida. Cada `case` del type switch ve solo los campos de su forma. El precio: guardar un `IntV` en la interfaz reserva memoria, salvo de 0 a 255 (5 no reserva; 1000 y -5, sí).

El `default: panic("unreachable")` también se prueba. Solo el paquete puede escribir `value()`, así que `fakeV`, el valor que `Show` no conoce, vive en un test interno.

:::from Rust
Un `enum Value` con `match` daría esa exhaustividad en compilación. Aquí la dan el `default` y su test.
:::

:::from TS
`const _: never = v` en el `default` de una unión lo comprueba en compilación.
:::

### `int64`: desborda sin avisar

En Go, el desbordamiento con signo está definido: no hay panic, y `MaxInt64 + 1` da `MinInt64`. Solo una expresión constante da error de compilación. Ki exige un panic, así que cada operación calcula y después comprueba con aritmética explícita, sin `math/bits` ni intrínsecos:

- **Suma:** `(a^r)&(b^r) < 0`. `x^r` tiene el bit de signo a 1 si `x` y `r` difieren en signo, y una suma desborda cuando los dos operandos difieren del resultado.
- **Resta:** `(a^b)&(a^r) < 0`. Operandos de signo distinto, y un resultado que no tiene el signo de `a`.
- **Producto:** `r/b != a`: si dio la vuelta, dividir no devuelve `a`. Un operando 0 se responde antes, porque dividir por 0 sí es un panic de Go.
- **El punto ciego:** `MinInt64 / -1` no falla en Go, da `MinInt64`. Por eso `MinInt64 * -1` pasaría la prueba del producto, y el par va aparte. Negar `MinInt64` también desborda.

:::more Comprobar las fórmulas con `int8`
Con `int8` se puede probar todo: en sus 65 536 pares, las tres comprobaciones, con el cero y el par `MinInt8` y `-1` aparte, coinciden con el resultado exacto.
:::

`/` trunca hacia cero y `%` toma el signo del dividendo: `-7 / 2` es -3, `-7 % 3` es -1 y `7 % -3` es 1. En Ki, `MinInt64 / -1` es un panic y `MinInt64 % -1` vale 0.

:::from Rust
`a + b` hace panic en debug y da la vuelta con `-O`; `checked_add` devuelve `None`. Ki se comporta siempre como `checked_add` seguido de un panic.
:::

:::more Desplazamientos en Go, para cuando Ki los tenga
Ki aún no tiene `<<` ni `>>`, y Core no tiene primitivas de desplazamiento. En Go 1.27:

- `<<` también desborda sin avisar: con `var x int64 = 1`, `x << 63` es `MinInt64`. Como expresión constante, `int64(1) << 63` no compila.
- Un contador igual o mayor que el ancho está definido: con `var c uint = 64`, `x << c` es 0; `8 >> 64` es 0 y `-1 >> 64` es -1. Con `x << 64`, compila, pero `go vet` protesta.
- Con signo, `>>` es aritmético: `-8 >> 1` es -4.
- Un contador negativo es un panic en ejecución: `runtime error: negative shift amount`.
:::

### Pilas como slices

Empujar es `append` y sacar es `s[:n-1]`. La máquina tiene dos pilas: `stack []frame` y `envs [][]Value`. El «marco» del capítulo 9, el de `FrameSize` y `Slot`, es aquí un entorno, y sus slots, casillas; los marcos de `stack` son otra cosa.

El código indexa `envs` y nunca guarda `&envs[i]`, porque un `append` puede copiar las cabeceras a otro array:

```go
envs := make([][]int, 1, 1)
envs[0] = []int{1, 2}
p, slot := &envs[0], &envs[0][0]
envs = append(envs, []int{3}) // no room: the headers are copied to a new array
fmt.Println(p == &envs[0], slot == &envs[0][0]) // false true
```

`p` se queda en el array viejo. Las casillas no se mueven, porque cada cabecera apunta a su propio array.

:::from Rust
`let p = &v[0]; v.push(2); use(p)` no compila (E0502). Go lo acepta, y la regla es tuya.
:::

### Un panic de Ki no es un panic de Go

Un panic de Ki es un estado: la máquina escribe su mensaje, guarda `Halt{Code: 101}` y el bucle termina. Un `panic` de Go significa un bug del compilador, y el `recover` de cada pase (capítulo 9) lo convierte en un ICE.

| Código | Significado |
|---|---|
| 0 | el programa terminó bien |
| 1 | errores de compilación, fichero ilegible o stdout perdido |
| 2 | uso incorrecto de `ki` |
| 70 | ICE |
| 101 | panic de Ki |
| 103 | desbordamiento de la pila de Ki |

Tres hechos de Go:

- Un `panic` sin recuperar sale con 2.
- `os.Exit` se salta los `defer`, así que hay que vaciar los buffers antes.
- `go run` enmascara el código: con `os.Exit(101)` dentro, imprime `exit status 101` y sale con 1. Por eso el runner ejecuta binarios construidos.

:::from Rust
`std::process::exit` tampoco ejecuta destructores.
:::

### Llamadas de cola

Una llamada normal empuja un `retFrame`, que recuerda el entorno de quien llama, y añade un entorno nuevo; sacar ese marco es volver.

Una llamada está en cola si es lo último que hace su función. La regla de la máquina: si el marco de arriba ya es un `retFrame`, nada espera el valor salvo el retorno, así que la llamada reutiliza ese marco y **sustituye** el entorno.

`Let` y `Match` ya no están en la pila cuando corre su cuerpo o su brazo: `count(n - 1)` en un brazo está en cola; en `n + sumTo(n - 1)`, no, porque un `primFrame` espera. Ki aún no tiene `while` (llega como azúcar en el capítulo 23): su bucle es la llamada de cola, de profundidad constante.

### El límite de la pila

La recursión de Ki vive en un slice, en el heap de Go, así que el límite es nuestro: `KI_STACK_MB` mebibytes, 256 por defecto. Cada MiB son 1<<14 marcos, a 64 bytes nominales: por defecto, 4 194 304.

Una llamada que va a empujar su `retFrame` con la pila en el límite termina en `panic: stack overflow at <posición>`, con 103.

Es lo contrario de la pila de Go: al pasar de 1 GB da `fatal error: stack overflow`, ningún `recover` lo detiene y sale con 2.

:::from Python
Python limita la recursión con un contador y lanza `RecursionError`, una excepción. Ki termina el proceso.
:::

### `kirt/out`: un solo escritor

`kirt` es el runtime, y nace con este paquete. Los tres backends escriben por él: si cada uno tuviera su escritor, tendría su orden de salida y su propio vaciado.

| Pieza | Para qué |
|---|---|
| `bufio.Writer` | junta las escrituras en un buffer de 64 KiB: una llamada al sistema por línea saldría cara. Recuerda su primer error y lo repite |
| `sync.Mutex` | hace de cada `Write` o `Flush` un todo, para cuando escriban varias goroutines |
| `os.ModeCharDevice` | detecta la terminal: `f.Stat()` da el modo, y un dispositivo de caracteres vacía en cada línea. `/dev/null` también lo es. `Stderr` vacía siempre por líneas |
| `out.Exit` | vacía y después llama a `os.Exit`. Si stdout no se pudo escribir y el código iba a ser 0, lo dice por stderr y sale con 1; otro código no cambia |

Un panic vacía stdout antes de escribir su mensaje, para que salga detrás de lo que el programa imprimió.

:::from Rust
Al soltarse, un `BufWriter` intenta vaciar, pero ignora los errores: hay que llamar a `flush`.
:::

### El test de imports: una regla con dientes

Tres reglas de arquitectura; de los subpaquetes, aún solo existe `out`:

| Regla | Por qué |
|---|---|
| `kirt` solo importa la biblioteca estándar y a sí mismo | cuando llegue `ki build` (capítulo 48), copiará `kirt` junto al programa generado, y esa copia tiene que compilar sola |
| el oráculo solo importa de `kirt` los paquetes `out`, `splitmix` y `fmtnum` | son hojas: no importan otros paquetes de `kirt`, y no tienen planificador ni heap, que llegarán más adelante. Si usara el planificador o los canales de `kirt`, no podría encontrar sus fallos |
| `unsafe` solo se permite bajo `kirt/heap` | es el único sitio donde un entero se convierte en puntero |

`parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)` (capítulo 7) lee solo los `import`, así que un comentario no cuenta. Cada regla corre sobre el repositorio y sobre árboles pequeños con el error que debe cazar: una regla que nunca se ha visto encontrar algo no demuestra nada.

### Goroutines, lo justo

El test del escritor las necesita; la Parte IV las enseña. Es la versión larga del `wg.Go` del capítulo 1: `go f()` lanza `f`, `wg.Add(1)` avisa antes, `defer wg.Done()` descuenta y `wg.Wait()` espera.

En T10.57, ocho goroutines escriben a la vez y otra llama a `Flush` sin parar hasta el `close(stop)` del test, con `stop := make(chan struct{})`. Un canal cerrado siempre está listo, y `select` con `default` lo consulta sin bloquearse. `runtime.Gosched()` no sincroniza nada: cede el procesador, para que esa goroutine no lo acapare.

Sin el `Mutex`, `go test -race` imprime `WARNING: DATA RACE` en `bufio.(*Writer).Write`.

:::from Rust
`Mutex<T>` guarda el dato dentro, y `Send` y `Sync` rechazan la carrera en compilación. En Go, el `Mutex` protege por convención y `-race` lo vigila en ejecución.
:::

### El proceso ayudante

Un test no puede ver morir a su propio proceso, así que relanza su binario con otro cometido, la idea de `runKi` (capítulo 1):

```go
// -test.run is go test's -run, as the test binary spells it
cmd := exec.Command(os.Args[0], "-test.run=^TestHelperProcess$")
cmd.Env = append(os.Environ(), "KI_TEST_HELPER=panic")
```

`TestHelperProcess` hace `return` si la variable no está (un `go test` normal); si está, `panic("boom")` o `os.Exit(101)`. El padre lee el código con `errors.As` y `ExitCode()`.

:::from Python
`subprocess.run([sys.executable, …])` relanza el intérprete; aquí se relanza el binario de test.
:::

### Delve

Lo probarás al final de la sesión 4, con `ki run`. `dlv` depura el binario real. Se instala en una versión fijada, nunca `@latest`, y no entra en `go.mod`:

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

| Orden | Qué hace |
|---|---|
| `go install …@v1.27.2` | compila esa versión y deja `dlv` en `$(go env GOBIN)` o, si sale vacío, en `$(go env GOPATH)/bin` (por defecto `~/go/bin`), como `cargo install`; ese directorio tiene que estar en tu `PATH` |
| `dlv exec build/bin/ki -- run …` | arranca el binario; lo que va tras `--` son sus argumentos |
| `break interp.(*Machine).enter` | pone un punto de parada en `enter`, por donde pasa cada llamada de Ki salvo la de `main`; la forma es `paquete.(*Tipo).Método` |
| `continue` | corre hasta el siguiente punto de parada o hasta el final |
| `print vals` | evalúa una expresión en el punto donde está parado |
| `next` | avanza una línea sin entrar en las llamadas |
| `clearall` | borra los puntos de parada |
| `exit` | sale de `dlv` |

Es la llamada `fib(20)`: `print` es la función 0, `fib` la 1, y en la pila está el `letFrame` de `main`. Direcciones y pid varían.

En un binario optimizado, algunas variables no se ven: `enter` declara `callee` en la línea 459, pero tras dos `next`, en la 460, `print callee` da `could not find symbol value for callee`. Con `go build -gcflags=all='-N -l'`, sin optimizar y sin inlining, da `FunV {Fun: 1}`.

### M1: `fib.ki`, etapa por etapa

Cada capítulo ha construido una fila:

| Pase | Qué hace con `fib.ki` | Cómo verlo |
|---|---|---|
| `lex` | `4:1 KwFn`, `4:4 LowerIdent(fib)`, … (línea 4, tras los tres comentarios de su cabecera), con los `Newline` de la regla de línea | `ki emit tokens tests/golden/ch10/fib.ki` |
| `parse` | `(fn fib (params (n Int)) (ret Int) (block (if (< n 2) …` | `ki emit ast` |
| `resolve` | `fib` es una definición; `print`, el extern del preludio | `ki check`: si el programa está bien, no dice nada y sale con 0 |
| `check` | `n < 2` es `Bool`, y las dos ramas son `Int` | `ki check`: la prueba es que no informa de ningún error |
| `elaborate` | `if` pasa a `match lt_int(n, 2)`, y `+`, a `add_int` | `ki emit core` |
| `lint` | comprueba que ese Core es válido | un ICE si no lo es |
| `run` | 415 931 pasos, 42 marcos como mucho (`--stats`, ejercicio 1), y `6765` | `ki run` |

## Tests primero

{{cards}}

Úsalas: escribe el test de la tarjeta, míralo fallar, implementa lo justo para el verde, y solo entonces despliega la referencia con su `{{reftest}}`. Describen el final del capítulo; cada sesión dice cuáles tocan.

:::more Los ayudantes de test, sesión por sesión
Están al principio de cada fichero, en `ref/ch10-sK`. Los tests que usan `testsupport` van en `package interp_test`; los que tocan lo no exportado, en `*_internal_test.go`.

- **Sesión 1, `value_internal_test.go`.** `fakeV` es un `Value` que `Show` no conoce.
- **Sesión 1, `machine_test.go`.** `machineOf` da una máquina nueva para un programa compilado (`testsupport.Compile`, capítulo 9) y los buffers de su stdout y su stderr; `call` compila y llama a una función por su nombre (`FunNamed`), y falla si hay panic, también uno de Go dentro de la máquina; `panicText` devuelve el valor de un panic de Go.
- **Sesión 2, `prims_internal_test.go`.** Una fila `arithRow` lleva dos operandos, el resultado y si desborda; `checkArith` recorre las filas con una operación. Una fila que desborda no mira el valor.
- **Sesión 2, `machine_test.go`.** `runMain` ejecuta el `main` de una fuente y da el `Halt`, stdout y stderr; `wantPanic` exige `Halt{101}`, ese stderr entero y stdout vacío.
- **Sesión 3.** En `machine_test.go`, `runMainOn` es `runMain` que además devuelve la máquina, para leer sus `Stats`, y `wantHalt` es `wantPanic` con otro código. En `machine_internal_test.go`, `fakeFrame` es un marco que `applyFrame` no conoce.
- **Sesión 4, `kirt/out`.** `failingWriter`, en `writer_test.go`, falla en cada `Write`. `TestWriterModes`, en `writer_internal_test.go`, lee el campo `lineFlush`. En T10.57, `string(rune('a'+g))` convierte un code point en texto.
- **Sesión 4, `externs_test.go`.** `heldOut` retiene lo escrito hasta `Flush`; con stderr sobre el mismo registro, muestra el orden.
- **Sesión 4, `internal/driver`.** `runCLI` (capítulo 1) y `writeKi` (capítulo 9, en `ice_test.go`).
- **Sesión 4, `kirt/imports_test.go`.** `importsUnder` recoge los imports de un árbol; `kirtProblems`, `interpProblems` y `unsafeProblems` son las tres reglas; `writeTree` y `goFile` escriben árboles a propósito; `checkRule` compara los problemas con la lista esperada.
- **Sesión 4, `internal/toolchain`.** `helperRun`, en `exitcodes_test.go`, lanza el ayudante y da el código y las salidas. `helper_process_test.go` lleva `// bookx: not-in-book`, que dispensa a sus tests de tener tarjeta: `TestHelperProcess` no comprueba nada, es el programa que se lanza.
- **Sesión 4, `cmd/ki/main_test.go`.** `runKi` y `runKiWith` (con más entorno) ejecutan `main` como proceso hijo. Con `stdoutEnv`, `TestMain` pone en el hijo un `out.Stdout` que siempre falla (otro `failingWriter`).
:::

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

- **`run`** es el bucle: con un control que es expresión llama a `step`, y con un valor, a `applyFrame`. Sin marcos y con un valor, ha terminado.
- **`letFrame`** escribe la casilla y pasa al cuerpo sin empujar nada: una cadena de `let` no hace crecer la pila.
- **`Call`** escribe cada argumento en la casilla de su `Var.Slot`.
- **El `default` de `step`** dice qué clase de expresión falta. Las sesiones 2 y 3 la completan, y queda en `panic("unreachable")`.

{{reftest T10.06}}

Si te atascas o tu versión se desvía demasiado, copia el fichero de `ref/ch10-s1` y sigue.

Al terminar: tus tests en verde, commit y etiqueta `ch10-s1` (mensaje: `ch10-s1: values and the machine`); compárate con `ref/ch10-s1`.

### Sesión 2: primitivas comprobadas y panics (≈ 2,5 h)

#### Los tests, en rojo

1. **Escribe T10.12–T10.34.** Interfaz nueva, sin exportar y usada por los tests internos:

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

- **`applyPrim`** es un `switch` con las 15 primitivas. Los operandos tienen la forma correcta porque el comprobador de tipos aceptó el programa; si no, `v.(IntV)` hace el panic de Go de una aserción, y es un ICE. `intResult(addInt(…))` usa una regla de Go: los resultados de una llamada pueden ser todos los argumentos de otra.
- **`first == nil`** distingue los dos marcos de una binaria, y `nil` no es ningún `Value`.
- **`m.halt = &h`** guarda la dirección de una variable local, y es válido: `h` escapa al heap (capítulo 2).
- **`Call` vacía la pila y el `halt`.** Un panic deja marcos a medias; sin vaciarlos, `good` de T10.34 daría 105 en lugar de 5.

Quita de `mulInt` el caso del punto ciego y T10.14 lo ve: `mulInt(-9223372036854775808, -1) overflow = false, want true`.

{{reftest T10.14}}

{{reftest T10.34}}

Si te atascas o tu versión se desvía demasiado, copia el fichero de `ref/ch10-s2` y sigue.

Al terminar: tus tests en verde, commit y etiqueta `ch10-s2` (mensaje: `ch10-s2: checked primitives and panics`); compárate con `ref/ch10-s2`.

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

- **`argFrame`** acumula la función llamada (el *callee*) y los argumentos en `vals`. El callee es una expresión más: una función en una variable se llama igual.
- **`enter`** aplica la regla de cola. La primera función no tiene `retFrame`, así que su primera llamada de cola empuja uno.
- **`retFrame{env}`** guarda el índice del entorno de quien llama; al aplicarlo, `envs` se corta ahí.
- **`selectArm`** prefiere el brazo cuyo patrón es el valor, esté donde esté, y si no hay, el primer comodín.
- **`haltAt`** es `panicAt` con el código como parámetro, para compartirlo con el desbordamiento.
- **`framesFor`** ignora el error de `Atoi`, porque el número ya lo dice: 0, o el extremo de su signo. `min` evita que el producto dé la vuelta.

Pon `false &&` en la condición de cola de `enter` y T10.43 lo ve: `Stats().MaxDepth = 1000002, want 3`.

:::more El límite al detalle: los 16 387 marcos de T10.48
El límite decide cuándo se rechaza una llamada; no es una cota de `MaxDepth`, que cuenta además los marcos que evaluar los argumentos ya empujó.

Con `KI_STACK_MB=1`, el límite es 16 384. En `fn f(n: Int) -> Int { 1 + f(n + 1) }`, cada nivel deja un `primFrame` y un `retFrame`; con el `letFrame` de `main`, son 1 + 2k marcos. La primera llamada que encuentra el límite ve 16 385 marcos en la pila (k = 8 192). Antes de esa comprobación, su `argFrame` y el `primFrame` de `n + 1` ya habían subido el máximo a 16 387.

La cifra es de este programa: el exceso depende de lo que anidan las expresiones alrededor de la llamada y dentro de ella, acotado por `MaxNesting` y `MaxChain` juntos. T10.49 y T10.50 fijan el borde exacto de la regla: con 1 MiB, caben 5 461 llamadas del programa de T10.49, y una de cola pasa con la pila justo en el límite.

La pila cuenta marcos y no entornos: cada llamada viva guarda además el suyo (una casilla de 16 bytes por variable), así que la memoria al llegar al límite es la de los marcos más esos entornos, y crece con las variables de la función que recursa.
:::

{{reftest T10.43}}

{{reftest T10.48}}

Si te atascas o tu versión se desvía demasiado, copia el fichero de `ref/ch10-s3` y sigue.

Al terminar: tus tests en verde, commit y etiqueta `ch10-s3` (mensaje: `ch10-s3: calls, match, tail calls and the stack limit`); compárate con `ref/ch10-s3`.

### Sesión 4: `kirt/out`, `print`, `ki run` y el hito M1 (≈ 2 h)

#### Los tests, en rojo

1. **Escribe T10.54–T10.92,** con los golden de `tests/golden/ch10/`. Cambia la línea `commands:` de `usage_lists_commands`, en `cli_test.go`, para que acabe en `emit, run`. La interfaz nueva:

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

2. **Escribe ya `coretest`,** porque dos paquetes de test lo importan y sin él ni arrancan (`no required module provides package …/coretest`). Monta a mano un programa con un extern que el intérprete no tiene. No puede estar en `testsupport`: importa `driver`, y un test de `driver` formaría un ciclo.

{{file ch10-s4 internal/core/coretest/extern.go}}

3. **Míralos fallar.** `kirt/out`, `internal/driver` y `cmd/ki` no compilan (`undefined: ForFile`, `s.execute undefined`), y el golden de `fib` falla. Un panic de Go corta el binario de `internal/interp`: `interp: the program calls function 0, which has no body`. `kirt` e `internal/toolchain` ya pasan: las reglas se cumplen, y los hechos de Go no dependen de ti.

Dos de los golden:

{{file ch10-s4 tests/golden/ch10/fib.ki}}

{{file ch10-s4 tests/golden/ch10/print_then_panic.ki}}

#### El escritor

{{file ch10-s4 kirt/out/writer.go}}

- **`Write`, con `lineFlush`,** envía hasta el último `\n` de `p` (`bytes.LastIndexByte`) y deja el resto en el buffer.
- **`FlushAll`** devuelve solo el error de `Stdout`: no queda dónde contar que falló stderr.

{{reftest T10.57}}

#### `print` y los externs

{{file ch10-s4 internal/interp/externs.go}}

{{diff ch10-s3..ch10-s4 internal/interp/machine.go}}

{{diff ch10-s3..ch10-s4 internal/interp/prims.go}}

- **Un `Fun` sin cuerpo** se ejecuta en `enter`, por la tabla: no empuja marco ni entorno.
- **Un nombre que no está** es un `panic` de Go y por tanto un ICE: solo la biblioteca estándar declara externs.
- **`m.out.(interface{ Flush() error })`,** en `haltAt`, pregunta si `out` sabe vaciarse, con una interfaz anónima (capítulo 3). `interp` no necesita saber qué es un `Writer`.

{{reftest T10.92}}

#### `ki run`

{{file ch10-s4 internal/driver/run.go}}

{{diff ch10-s3..ch10-s4 internal/driver/commands.go}}

{{file ch10-s4 cmd/ki/main.go}}

{{file ch10-s4 docs/errors/E0303.md}}

- **`fileArgsWith`** recibe una función que define flags y devuelve otra que comprueba sus valores tras parsear: así un `--backend` malo se informa con fichero o sin él.
- **E0303** sale solo si no hay otros errores, en el span vacío del principio del fichero. `ki check` no pide `main`.
- **La máquina es un pase más,** `run`: un `panic` de Go dentro sale como ICE con 70.
- **`main` termina en `out.Exit`.** Cámbialo por `os.Exit`, reconstruye `ki`, y `ki run fib.ki | cat` no imprime nada y sale con 0. `build/bin/ki test tests/golden/ch10/fib.ki` lo ve (`- 6765`, `+ <eof>`) y `go test ./cmd/ki` también, en T10.91; el `TestKi` de `go test ./tests` no, porque no pasa por `main`.

`tests/main_test.go` y `ci/ci-local.sh` quitan `KI_STACK_MB` del entorno: exportada por error, desbordaría `deep_recursion.ki`.

{{diff ch10-s3..ch10-s4 tests/main_test.go}}

{{diff ch10-s3..ch10-s4 ci/ci-local.sh}}

#### Las reglas y los hechos

{{file ch10-s4 kirt/imports_test.go}}

| En `imports_test.go` | Qué hace |
|---|---|
| `filepath.SkipDir` | devuelto desde la función de `WalkDir`, salta ese directorio |
| `file.Imports` y `spec.Path.Value` | cada import y su ruta tal como está escrita, con comillas; `strconv.Unquote` (capítulo 7) se las quita |
| el primer elemento de la ruta, sin punto | así se reconoce la biblioteca estándar: una ruta de módulo empieza por un dominio |
| la barra de `modulePath+"/kirt/"` | deja fuera a `kirtx` |

Los otros dos ficheros son el proceso ayudante de la teoría.

{{file ch10-s4 internal/toolchain/exitcodes_test.go}}

{{file ch10-s4 internal/toolchain/helper_process_test.go}}

{{reftest T10.77}}

{{reftest T10.79}}

Construye `ki` (`go build -o build/bin/ki ./cmd/ki`), instala Delve y repite la sesión de la teoría.

Si te atascas o tu versión se desvía demasiado, copia el fichero de `ref/ch10-s4` y sigue.

Al terminar: tus tests en verde, commit y etiqueta `ch10-s4` (mensaje: `ch10-s4: kirt/out, print, ki run and milestone 1`); compárate con `ref/ch10-s4`.

## Ejecútalo

Desde la raíz de tu repo, con `ki` construido:

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

La línea de uso pasa a ser `ki run [--backend=interp] [--stats] [--diagnostics=human|short|json] FILE`, en `commands.go` y en la constante `runUsage` de `run_test.go`. Las dos llamadas a `execute` de `go_panic_in_the_machine_is_an_ice` reciben un `false`. El test, en `TestRun`:

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

El preludio ha crecido, y siete subtests fijaban su tamaño a propósito. Se ponen en rojo y hay que actualizarlos:

- `TestElaborate/defs_become_funs`: 6 funciones, no 5.
- `TestElaborate/lower_runs_under_the_elaborate_pass`: 3, no 2.
- `TestLint/accepts_elaborated_fib`: 3 funciones, y `fib` es la 2.
- `TestLint/rejects_unbound_var`: `Funs.Ptr(2)`, porque `f` es ahora la función 2.
- `TestPipeline/parse_source_puts_prelude_first`: el preludio tiene 2 items y 2 atributos.
- `TestResolve/collects_top_level_fns`: 4 definiciones, y las del fichero son `Defs.Get(2)` y `Defs.Get(3)`.
- `TestResolve/user_print_shadows_prelude`: 2 definiciones en el módulo 0.

Con `printBool(1)`, el comprobador de tipos ya da E0401: la firma del preludio basta.
:::

:::exercise
**3. `fib(30)` frente a Python.** Mídelo con `time` y compáralo con el mismo programa en Python.
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

En esta máquina, tres ejecuciones de cada uno: `ki run` tarda unos 0,85 s y `python3` (3.14), unos 0,074 s, unas once veces menos. `ki` da 51 158 202 pasos, cada uno con un `switch`, y reserva memoria para entornos y valores. Ser rápido no es su trabajo: es el oráculo, la referencia con la que se comparan los otros backends.
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
La llamada está en cola: el `matchFrame` ya salió de la pila, así que lo que hay arriba es un `retFrame`. `enter` lo reutiliza y sustituye el entorno; el viejo queda sin referencias y el GC lo recoge. Con `--stats`, del ejercicio 1: `steps: 220000020, max depth: 4`.

Son unas 1000 recolecciones en unos 4 s, casi todas iguales:

```text
gc 1008 @4.045s 2%: 0.061+0.13+0.19 ms clock, 0.98+0.011/0.098/0+3.0 ms cpu, 3->3->0 MB, 4 MB goal, 0 MB stacks, 0 MB globals, 16 P
```

`3->3->0 MB` es el heap al empezar, al acabar y vivo: cada vuelta reserva, pero nada se queda (en unas pocas decenas de líneas, `3->4->0`). Como contraste, este programa, que no es de cola, acaba con unos 400 MB de heap y más de 200 MB vivos:

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
**5. Vaciar a la mitad.** Haz que `kirt/out` vacíe también cuando el buffer pasa de la mitad (`Buffered()` da los bytes que esperan), y mide si cambia algo con `print` en un bucle de 10⁶ iteraciones. Mide contra un fichero, no contra `/dev/null`, que vacía en cada línea.
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

Con `>=` en lugar de `>`, la primera fila falla. T10.56 también falla, porque fijaba que el buffer guarda su tamaño entero. Ahora guarda la mitad: en ese subtest, `chunk` mide `min(1024, row.bytes/2)`, el bucle escribe hasta `row.bytes/2`, y tras un byte más el destino tiene `row.bytes/2+1`.

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

Son 6 888 896 bytes. Con un `Writer` que cuenta las escrituras de su destino, pasan de 106 a 211. A un fichero, los dos tardan unos 0,48 s y la salida es idéntica: nada medible, porque la máquina domina y una escritura cada 32 KiB sigue siendo rara. Lo que sí se nota es vaciar por líneas: con `> /dev/null`, un dispositivo de caracteres, tarda 0,87 s.
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
Porque `MinInt64 * -1` da `MinInt64` al dar la vuelta, y `MinInt64 / -1`, en Go, también da `MinInt64`, sin fallar: `r/b` es `a` y la prueba diría que no desbordó. Es el único par con ese punto ciego. Sin el caso, T10.14 falla con `mulInt(-9223372036854775808, -1) overflow = false`.
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
Porque stdout retiene y stderr no. Si los dos van al mismo sitio, como en `2>&1 | cat`, el mensaje saldría antes que las líneas que el programa imprimió antes del panic, y el orden engañaría a quien lo lee.
:::

:::review
¿Por qué el test de imports corre también sobre árboles escritos a propósito, si el repositorio ya cumple las reglas?
:::

:::answer
Porque una regla que no encuentra nada puede estar rota: un prefijo mal escrito, un directorio que no recorre. Los árboles con un error, como `kirtx`, que solo empieza igual, demuestran que caza lo que dice.
:::

## Resumen

- Una máquina CEK guarda control, entorno y continuación en datos; ejecutar es un bucle, no una recursión de Go.
- `Value` es una interfaz sellada por sus formas y su vida corta; los nodos, un struct con `Kind`.
- Go da la vuelta sin avisar: las comprobaciones son aritmética explícita, con `MinInt64` y `-1` aparte.
- Un panic de Ki es `Halt{101}`; un `panic` de Go es un ICE con 70.
- Una llamada de cola reutiliza el `retFrame` y sustituye el entorno: los bucles no crecen.
- `KI_STACK_MB` da un límite propio, con salida 103, frente al `fatal error` de Go.
- `kirt/out` es el único escritor: buffer, `Mutex`, vaciado por líneas en una terminal.
- `out.Exit` vacía antes de `os.Exit`, y un stdout perdido convierte el 0 en 1.
- El proceso ayudante prueba lo que termina el proceso; `go/parser` da dientes a las reglas de imports.

## Go en el trabajo

**Uniones etiquetadas.** Lua 5.4 representa cada valor con un `TValue`: una unión y un byte de etiqueta. CPython usa las dos formas del capítulo: sus nodos de AST son `struct _expr`, con `kind` y una unión; sus objetos llevan un puntero a su tipo, `ob_type`, más cerca de una interfaz.

**Desbordamiento y CVE.** En CVE-2015-1538, en Android, una multiplicación sin comprobar en datos MP4 permitía ejecutar código a distancia.

La especificación de Go define el resultado del desbordamiento y prohíbe al compilador suponer que no ocurre. Donde es indefinido, como en C con signo, el compilador puede borrar la comprobación que lo caza: `gcc -O2` reduce `a + 1 < a` a 0.

Definirlo no lo evita: CVE-2022-23772 es un desbordamiento en `Rat.SetString`, de `math/big`, con consumo de memoria sin control.

**El flush olvidado.** La documentación de `bufio.Writer` pide llamar a `Flush` al terminar. Sin él se pierde lo que quedaba en el buffer, y un disco lleno puede no verse hasta ese vaciado.

:::more Las fuentes
**Lua.** `lobject.h`, 5.4.7: `#define TValuefields Value value_; lu_byte tt_`, con el comentario «an actual value plus a tag with its type».

**CPython.** En las cabeceras de 3.14, `internal/pycore_ast.h`: `struct _expr { enum _expr_kind kind; union { … } v; … }`. En `object.h`, `struct _object` lleva `PyTypeObject *ob_type`.

**Go.** La especificación, «Integer overflow»: «Overflow does not cause a run-time panic. A compiler may not optimize code under the assumption that overflow does not occur.» Sobre `/`: «the quotient `q = x / -1` is equal to `x` (and `r = 0`) due to two's-complement integer overflow».

**C.** C11 (N1570), 6.5p5: «If an exceptional condition occurs during the evaluation of an expression (that is, if the result is not mathematically defined or not in the range of representable values for its type), the behavior is undefined.» `man gcc` (15.2), `-Wstrict-overflow`: «the compiler optimizes based on the assumption that signed overflow does not occur». Con `int overflows(int a) { return a + 1 < a; }`, `gcc -O2 -S` da `xorl %eax, %eax`, y `overflows(INT_MAX)` vale 0; con `-fwrapv`, 1. La misma función en Go, con `MaxInt64`, da `true`.

**CVE.** CVE-2015-1538: «Integer overflow in the SampleTable::setSampleToChunkParams function … allows remote attackers to execute arbitrary code via crafted atoms in MP4 data that trigger an unchecked multiplication». CVE-2022-23772: «Rat.SetString in math/big in Go before 1.16.14 and 1.17.x before 1.17.7 has an overflow that can lead to Uncontrolled Memory Consumption».

**`bufio`.** `src/bufio/bufio.go`: «After all data has been written, the client should call the Writer.Flush method to guarantee all data has been forwarded to the underlying io.Writer», y «all subsequent writes, and Writer.Flush, will return the error».
:::
