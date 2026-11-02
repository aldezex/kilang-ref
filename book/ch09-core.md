---
chapter: 9
title: Core: la referencia semántica
part: I
hours: 6
milestone: `ki emit core`; Core Lint en verde
---

## Objetivo

Al terminar, `ki` traduce un programa sin errores a Core, su representación intermedia (IR), comprueba el resultado y lo enseña:

```console
$ cat fib.ki
fn fib(n: Int) -> Int {
  if n < 2 { n } else { fib(n - 1) + fib(n - 2) }
}

fn main() {
  let x = fib(20)
  print(x)
}
$ build/bin/ki emit core fib.ki
fn fib(n) =
  match lt_int(n, 2) {
    true => n
    false => add_int(fib(sub_int(n, 1)), fib(sub_int(n, 2)))
  }
fn main() =
  let x = fib(20);
  print(x)
```

Ya no hay `if` ni `+` ni tipos, y Core Lint ha aceptado el resultado. El capítulo 10 ejecutará este Core para cerrar el hito M1.

Y un fallo de un pase deja de ser un panic suelto: es un informe con código de salida 70, un ICE (*internal compiler error*), que no es un diagnóstico ni lleva código `E…`.

## Teoría

### Por qué una IR central

Ki tendrá tres backends (intérprete, VM y nativo). Si cada uno leyera el árbol de sintaxis, cada uno decidiría qué significa un `&&`, y acabarían en desacuerdo. Por eso hay un lenguaje pequeño en medio: **Core es la única referencia semántica**, y lo que Core dice que hace un programa es lo que hace.

Core no tiene azúcar sintáctico ni tipos, y es **estricto**: evalúa los argumentos antes de la llamada. A la Parte I le bastan nueve clases de nodo: literales (`LitInt`, `LitBool`, `LitUnit`), referencias (`VarRef`, `FunRef`), llamadas (`Call`, `PrimCall`, a una primitiva como `add_int`), `Let` (con su `Init` y su `Body`) y `Match`.

**La etapa S0.** Core se transforma por etapas, de S0 a S5, todas subconjuntos o extensiones de la misma IR, y cada pase baja de una a la siguiente. Este capítulo solo tiene S0.

**La elaboración** traduce el árbol tipado a S0:

| Ki | Core S0 |
|---|---|
| `if c { a } else { b }` | `match c { true => a; false => b }` |
| `if c { a }` | `match c { true => a; false => () }` |
| `a && b` | `match a { true => b; false => false }` |
| `a \|\| b` | `match a { true => true; false => b }` |
| `{ let x = e; s; t }` | `let x = e; let _ = s; t` |
| `a + b`, `-a` | `add_int(a, b)`, `neg_int(a)` |
| `a == b` | `eq_int(a, b)` o `eq_bool(a, b)` |

En la salida de `ki emit core`, cada brazo va en su línea, sin `;`.

- **`&&` y `||` son `match`** porque Core es estricto: un `and_bool(a, b)` evaluaría siempre `b`.
- **`==` se decide aquí** por el tipo del operando izquierdo: es el último pase que conoce los tipos.
- **Solo corre sobre programas sin errores.** Ante un nodo de error hace panic: si llega uno, el fallo es del compilador.

**Binders únicos y slots.** Un *binder* es lo que introduce una variable: un parámetro o un `let`. Cada uno es una `Var` propia, aunque se llame igual que otra, para que un id baste para saber de qué variable se habla.

Su `Slot` es su índice en el marco, la memoria de los parámetros y los `let` de una llamada, que usará la máquina del capítulo 10. `FrameSize` cuenta los slots, y nada los reutiliza.

**Las anotaciones `rep`.** Cada `Var` lleva un `Rep`: cómo se representa su valor en ejecución, hoy siempre `RepVal`. El capítulo 56 añade `RepI64` y `RepF64` para no empaquetar números en código de un solo tipo; la elaboración es quien puede rellenarlo, porque aún ve los tipos. Por eso el campo existe ya.

### Los invariantes y Lint

Como Core no tiene tipos, nada impide escribir un Core sin sentido, y un pase con un bug lo escribirá. **Core Lint** comprueba lo que los pases prometen y los siguientes dan por hecho:

- cada `VarRef` está en ámbito: un parámetro, o el `Let` cuyo cuerpo la contiene (el `Init` queda fuera: un `Init` que lee la `Var` de su propio `Let` es una violación; en Ki, `let x = x` son dos `Var`);
- una llamada a una `FunRef` lleva tantos argumentos como parámetros, y una primitiva, los de su `Arity`;
- un `Match` tiene los brazos `true` y `false`, o un `_`;
- una función tiene cuerpo o nombre de extern, no las dos cosas ni ninguna;
- cada slot cabe en su marco.

Una violación no es un error del programa, sino del compilador: se convierte en un ICE. El diseño también pide binders únicos; este `Lint` no lo comprueba aún, y es el ejercicio 1.

### Builder e interfaces pequeñas

`Expr` es un struct plano: `Kind` dice qué campos significan algo, y los demás valen cero. Es el mismo diseño de nodos en un `IdVec`, sin punteros, que el árbol de sintaxis (capítulos 4 y 6). Rellenarlo a mano invita a olvidar un campo, así que hay una **API builder**:

- **Un método por clase de nodo.** Crea el nodo, lo guarda y devuelve su id.
- **De abajo arriba.** Los hijos existen, con su id, antes que el padre.
- **Copia las listas al pool.** Quien llama puede reutilizar su slice.
- **No valida.** Construye lo que le piden: sin esto, los tests no podrían fabricar Core inválido para `Lint`.

:::from Rust
Sería un `enum` con datos, y leer `Init` de un `LitInt` no compilaría. En Go es una convención, que protegen el builder y `default: panic("unreachable")` en cada `switch`.
:::

**Interfaces pequeñas.** `Builder` es un struct y no una interfaz: tiene una sola implementación, y una interfaz la declara quien la consume. Cuantos menos métodos tiene, más tipos la cumplen sin saberlo. Las que este capítulo implementa tienen uno: `*ICE` es un `error` por su `Error()`, y `Prim` es un `fmt.Stringer` por su `String()`, que es lo que usa el `%s` de `Lint`.

### Recorridos: `iter.Seq` y una pila propia

El capítulo 4 escribió `All`, un `iter.Seq2`; `Program.Subexprs` da un `iter.Seq[ExprID]` con la raíz y todo lo que hay debajo, en preorden (cada expresión antes que sus hijos). Lo que en otros lenguajes sería un generador aquí es una función que recibe `yield`.

**No puede ser recursivo.** Un bloque de 100 000 sentencias es una cadena de 100 000 `Let`, y el parser solo acota el anidamiento de las expresiones (capítulo 6). Con un marco de Go por `Let`, ese bloque cabría en el 1 GB de pila por defecto; uno bastante mayor daría un `fatal error`, que `recover` no ve.

Por eso los tests bajan el límite a 1 MiB: con 1 GB, esa recursión no se notaría.

**La pila es un slice.** Se saca un id, se entrega y se apilan sus hijos en orden inverso, para que el primero salga antes. Si `yield` devuelve `false`, se acaba.

`Lint` necesita más: el ámbito cambia entre los hijos de un `Let`. Su pila guarda pasos (visitar, ligar, desligar) y un `Let` apila, de arriba abajo, `Init`, ligar, `Body` y desligar. `Print`, el impresor de Core, usa la misma idea con tareas: una expresión, un texto o un salto de línea.

:::from Python
Un generador recursivo con `yield from` no se libra: con 100 000 niveles da `RecursionError`.
:::

### `defer`, `panic` y `recover`: la frontera del pase

Un panic de Go sin recuperar sale con 2 (capítulo 2), el mismo código que un uso incorrecto de la CLI, y el pase solo se deduce leyendo la pila. La **frontera del pase** lo convierte en un informe con código propio.

**`defer`** tiene dos reglas nuevas: las llamadas diferidas corren en orden inverso (LIFO), y sus argumentos se evalúan en la línea del `defer`. Un closure, en cambio, lee la variable al correr:

```go
x := 1
defer fmt.Println("arg", x)                  // prints: arg 1
defer func() { fmt.Println("closure", x) }() // prints first: closure 2
x = 2
```

Así limitan la pila los tests: `SetMaxStack` devuelve el límite anterior, y el `defer` lo restaura al salir.

```go
old := debug.SetMaxStack(1 << 20)
defer debug.SetMaxStack(old)
```

:::from Rust
`Drop` también corre en orden inverso, pero al salir del bloque y sobre valores; `defer` corre al salir de la función, con una llamada que escribes tú.
:::

**El patrón `guard`** junta `defer` con `recover`, que solo funciona llamado directamente por la función diferida (capítulo 2), y con un resultado con nombre que esa función puede escribir (capítulo 3):

```go
func guard(pass string, f func()) (ice *ICE) {
	defer func() {
		if r := recover(); r != nil {
			ice = &ICE{Pass: pass, Value: r, Stack: debug.Stack()}
		}
	}()
	f()
	return nil
}
```

`runtime/debug.Stack()` devuelve la pila de la goroutine como `[]byte`. Va dentro de la diferida porque esa función corre antes de que el panic deshaga nada, y la pila aún muestra quién lo hizo; al volver de `guard`, ya no.

:::from Rust
`catch_unwind` ya devuelve el panic como un `Err`; en Go, `guard` lo escribes tú.
:::

**Tres piezas:**

1. `guard` convierte el panic en un valor con el nombre del pase.
2. `runPass` ejecuta el pase bajo `guard` y, si hay ICE, hace panic con el `*ICE`: ningún pase debe correr sobre lo que otro dejó a medias.
3. `catchICE`, diferida en `Main`, reconoce el `*ICE` (`r.(*ICE)`, capítulo 3), escribe el informe y pone el código a 70. Cualquier otro panic lo relanza: no viene de un pase.

**¿Por qué 70?** Es `EX_SOFTWARE` en `sysexits.h`, «internal software error». Así, un script distingue un programa mal escrito (1), un comando mal escrito (2) y un pase roto (70). El panic de un programa Ki es otra cosa, y sale con 101 en el capítulo 10.

**El gancho.** Sin un bug a mano no se puede probar la frontera, así que con `KI_DEBUG_PANIC_IN=<pase>`, ese pase hace panic al empezar. Lo lee `Main` y no `NewSession`: una `Session` de test no depende del entorno.

:::more Lo que la frontera no cubre: `fatal error`, los diagnósticos y los impresores
- Un `fatal error` no es un panic. Con `debug.SetMaxStack(1 << 20)` y una recursión infinita, un `defer` con `recover` no llega a correr: `runtime: goroutine stack exceeds 1048576-byte limit`, `fatal error: stack overflow`, código 2.
- Cuando salta un ICE, los diagnósticos que ya estaban en el sink no se escriben. T09.09 lo fija: con el gancho en `resolve`, un fichero con un error de sintaxis da el informe y ningún `error[`.
- `core.Print`, `syntax.DumpTokens`, `syntax.PrintModule` y `ReportAll` no son pases: corren fuera de `runPass`, y un panic ahí saldría con 2.
- `recover` da el valor tal cual, también uno del runtime: `guard` guarda `runtime error: index out of range [3] with length 0` como cualquier otro (T09.08).
:::

## Tests primero

{{cards}}

Úsalas como siempre: escribe el test de la tarjeta, míralo fallar, implementa lo justo para el verde, y solo entonces despliega la referencia con su `{{reftest}}`. Describen el final del capítulo: T09.09 y T09.10 ganan filas en la sesión 3, y hasta entonces se escriben como en `git show ref/ch09-s1:book/tests/ch09.tests`.

:::more Los ayudantes de test, sesión por sesión
- **Sesión 1, `internal/core/core_test.go`** (`package core_test`). `span` crea un span en el fichero 1, para que un span olvidado, que sería del fichero 0, se note; `newBuilder` da un `Builder` sobre un `Program` vacío.
- **Sesión 1, `internal/driver/ice_test.go`: el entorno.** Es `package driver`, porque prueba `guard` y `runPass`. Su `TestMain` borra `KI_DEBUG_PANIC_IN`, para que un shell que la exporte no rompa los tests que pasan por `Main`; `tests/main_test.go` hace lo mismo, y `ci/ci-local.sh` la borra para los goldens que corre con el binario. `t.Setenv` pone una variable de entorno y la restaura al acabar el test.
- **Sesión 1, `ice_test.go`: los ayudantes.** `explode` hace panic; es una función con nombre, y su nombre sale en la pila aunque el compilador sustituya la llamada por su cuerpo. `writeKi` escribe un fichero temporal. `checkICE` exige código 70, stdout vacío y el mensaje seguido de la pila. `raised` y `newSession`, closures de T09.11, devuelven el valor de un panic y crean una `Session` con el gancho pedido. `runCLI` ya existía.
- **Sesión 2, `internal/testsupport/compile.go`.** `Compile` pasa una fuente por el front end (lexer, parser, resolución y tipos), falla el test si hay errores, y elabora.
- **Sesión 2, `internal/core/elaborate_test.go`.** `elabFun` busca una función por nombre; `elabText` da el texto de un span; `elabVar`, el nombre y el slot de una `Var`; `elabShape` escribe una expresión en una línea, sin spans, y `elabWant` compara esa línea; `elabCompileCapped` compila con la pila limitada a 1 MiB; `elabRaised` devuelve el valor de un panic. El `why` de T09.29 cita ya `ki emit core`, que llega en la sesión 3, porque la tabla sale de la última etiqueta; su test no cambia.
- **Sesión 3, `internal/core/lint_test.go`.** `lintProg` (con `newLintProg`, `variable`, `push`, `fn` y `extern`) monta programas a mano, con variables `v<id>` y funciones `f<id>`; `lintFun` y `lintVar` escriben esos nombres en una violación; `lintCheck` compara la lista entera de violaciones; `lintMatch` crea un `Match` con los patrones pedidos; `lintFirstDiff` da el primer byte distinto de dos textos. `slices.Collect`, en T09.41, convierte un `iter.Seq` en un slice.
- **Sesión 3, otros.** `withStackCap`, en `elaborate_test.go`, ejecuta una función con la pila limitada; `printSource`, en `print_test.go`, compila e imprime; `raised` y `newSession` suben a nivel de fichero en `ice_test.go`.
:::

## Implementación

### Sesión 1: la IR Core, el builder y la frontera de los ICE (≈ 2 h)

#### Los tests, en rojo

1. **Escribe T09.01–T09.13.** T09.13 va en `internal/base/list_range_test.go`, porque el pool de listas pasa a `base`. La interfaz, completa:

```go
package core
type ExprID uint32; type VarID uint32; type FunID uint32
type Rep uint8        // RepVal; chapter 56 adds RepI64 and RepF64
type Var struct { Name base.Symbol; Rep Rep; Slot uint32; Span base.Span }
type Prim uint8       // PrimAddInt, PrimSubInt, PrimMulInt, PrimDivInt, PrimRemInt, PrimNegInt, PrimEqInt, PrimNeInt, PrimLtInt, PrimLeInt, PrimGtInt, PrimGeInt, PrimEqBool, PrimNeBool, PrimNotBool
func (p Prim) String() string  // "add_int", …, "not_bool"; otherwise "Prim(n)"
func (p Prim) Arity() int      // 1 for PrimNegInt and PrimNotBool, 2 for the rest
type ExprKind uint8   // LitInt, LitBool, LitUnit, VarRef, FunRef, Call, PrimCall, Let, Match
type PatKind uint8    // PatTrue, PatFalse, PatWildcard
type Arm struct { Pat PatKind; Body ExprID }
type Expr struct {
	Kind ExprKind; Span base.Span
	Int int64; Bool bool; Var VarID; Fun FunID; Prim Prim
	Callee ExprID; Args base.ListRange    // over ExprPool (Call and PrimCall)
	Init, Body ExprID                     // Let
	Scrutinee ExprID; Arms base.ListRange // over ArmPool
}
type Fun struct { Name base.Symbol; Params []VarID; Body ExprID; HasBody bool; ExternName string; FrameSize uint32; Span base.Span; UserModule bool }
type Program struct { Funs base.IdVec[FunID, Fun]; Exprs base.IdVec[ExprID, Expr]; Vars base.IdVec[VarID, Var]; ExprPool []ExprID; ArmPool []Arm; Main FunID; HasMain bool }
func (p *Program) Args(r base.ListRange) []ExprID
func (p *Program) ArmsOf(r base.ListRange) []Arm
type Builder struct { P *Program }
func (b *Builder) LitInt(sp base.Span, v int64) ExprID
func (b *Builder) LitBool(sp base.Span, v bool) ExprID
func (b *Builder) LitUnit(sp base.Span) ExprID
func (b *Builder) VarRef(sp base.Span, v VarID) ExprID
func (b *Builder) FunRef(sp base.Span, f FunID) ExprID
func (b *Builder) Call(sp base.Span, callee ExprID, args []ExprID) ExprID
func (b *Builder) Prim(sp base.Span, p Prim, args []ExprID) ExprID
func (b *Builder) Let(sp base.Span, v VarID, init, body ExprID) ExprID
func (b *Builder) Match(sp base.Span, scrutinee ExprID, arms []Arm) ExprID

package base
func Slice[T any](pool []T, r ListRange) []T
func AppendRange[T any](pool *[]T, items []T) ListRange

package driver
const ExitICE = 70
const debugPanicEnv = "KI_DEBUG_PANIC_IN"
type ICE struct { Pass string; Value any; Stack []byte } // Error(): "internal compiler error: <pass>: <value>"
func guard(pass string, f func()) (ice *ICE)
func (s *Session) runPass(name string, f func())
func (s *Session) catchICE(code *int)
// Session gains DebugPanicIn string; Main reads it from KI_DEBUG_PANIC_IN
func Main(args []string, stdin io.Reader, stdout, stderr io.Writer) (code int)
```

2. **Míralos fallar.** Sobre `ch08-s4`, los tres paquetes no compilan:

```console
$ go test ./internal/core ./internal/driver ./internal/base
github.com/aldezex/kilang/internal/core: no non-test Go files in …/internal/core
FAIL	github.com/aldezex/kilang/internal/core [build failed]
# github.com/aldezex/kilang/internal/base [github.com/aldezex/kilang/internal/base.test]
internal/base/list_range_test.go:32:10: undefined: Slice
…
# github.com/aldezex/kilang/internal/driver [github.com/aldezex/kilang/internal/driver.test]
internal/driver/ice_test.go:18:24: undefined: debugPanicEnv
internal/driver/ice_test.go:43:13: undefined: guard
…
FAIL	github.com/aldezex/kilang/internal/driver [build failed]
FAIL	github.com/aldezex/kilang/internal/base [build failed]
FAIL
```

No hay stubs que valgan aquí: un `Builder` vacío haría que `p.Exprs.Get` cortara el binario de test con un panic.

3. **Escribe `core.go`, `builder.go`, `base.Slice`, `base.AppendRange`, `ice.go` y `tests/main_test.go`, y cambia `Main`, `Session` y `ci/ci-local.sh`.** Deja sin tocar `pipeline.go` y `emit.go`. Queda un solo rojo, `debug_hook_exits_70`: `ki check with the hook in check: exit code 0, want 70`, porque ningún pase pasa aún por `runPass`. `debug_hook_names_one_pass` ya está en verde, porque un gancho sin conectar tampoco hace nada.

#### El código

{{file ch09-s1 internal/core/core.go}}

{{file ch09-s1 internal/core/builder.go}}

`Args` y `ArmsOf` leen un `ListRange` como el árbol de sintaxis. Core es el segundo árbol con `ListRange`, así que `slice` y `appendRange` salen de `syntax` y pasan a `base`, para no copiarlos. No cambia lo que hacen:

{{diff ch08-s4..ch09-s1 internal/base/idvec.go}}

{{diff ch08-s4..ch09-s1 internal/syntax/ast.go}}

En `parser.go`, `parser_expr.go` y `parser_stmt.go`, cada `appendRange(` pasa a ser `base.AppendRange(`.

{{file ch09-s1 internal/driver/ice.go}}

{{diff ch08-s4..ch09-s1 internal/driver/cli.go}}

{{diff ch08-s4..ch09-s1 internal/driver/session.go}}

- **`Main`** gana un resultado con nombre para que `defer s.catchICE(&code)` lo escriba. `&code`, un puntero evaluado en esa línea, apunta al resultado que `Main` devolverá.
- **`catchICE`** es ella misma la función diferida, y por eso su `recover` funciona.

Para que un shell que exporte `KI_DEBUG_PANIC_IN` no rompa los goldens:

{{file ch09-s1 tests/main_test.go}}

{{diff ch08-s4..ch09-s1 ci/ci-local.sh}}

Ahora conecta los pases. `runPass` recibe un `func()` sin resultado, así que cada pase escribe el suyo en una variable capturada. `s.lex` existe porque `parseFiles` y `ki emit tokens` comparten ese paso:

{{diff ch08-s4..ch09-s1 internal/driver/pipeline.go}}

{{diff ch08-s4..ch09-s1 internal/driver/emit.go}}

{{reftest T09.08}}

{{reftest T09.11}}

Si te atascas o tu versión se desvía demasiado, copia el fichero de `ref/ch09-s1` y sigue.

Al terminar: tus tests en verde, commit y etiqueta `ch09-s1` (mensaje: `ch09-s1: the Core IR, the builder and the ICE boundary`); compárate con `ref/ch09-s1`.

### Sesión 2: elaboración a Core S0 (≈ 2 h)

#### Los tests, en rojo

1. **Escribe T09.14–T09.29** en `internal/core/elaborate_test.go`. La interfaz:

```go
package core
func Elaborate(mods []*syntax.Module, res *sema.ResolvedProgram, typed *sema.TypedProgram, in *base.Interner) *Program

package driver
func (s *Session) Lower(f *Frontend) *core.Program

package sema
const PreludeModule ModuleIdx = 0 // it was preludeModule

package testsupport
type Compiled struct { *Program; Core *core.Program }
func Compile(t *testing.T, source string) *Compiled
```

2. **Míralos fallar.** No compilan: `undefined: testsupport.Compiled`.
3. **Añade `Compile` y `Lower`, y un stub:** `Elaborate` devuelve `&Program{}`. Fallan los 16 subtests, ninguno con panic; el primero, con `the Program has no function called "pick"`.

{{diff ch09-s1..ch09-s2 internal/testsupport/compile.go}}

{{diff ch09-s1..ch09-s2 internal/driver/pipeline.go}}

#### El código

{{file ch09-s2 internal/core/elaborate.go}}

`Elaborate` tiene que distinguir las funciones del preludio de las del fichero para rellenar `UserModule`, y por eso `preludeModule` se exporta:

{{diff ch09-s1..ch09-s2 internal/sema/resolve.go}}

{{diff ch09-s1..ch09-s2 internal/sema/suggest.go}}

- **`FunID(d)` es la `Fun` de la `DefID` `d`.** Se elabora en orden de `Def`, así que una llamada a una función definida más abajo ya tiene su id.
- **Slots.** El `let` toma su slot antes que los bloques de su `Init`: los slots siguen el orden en que el programa nombra las variables.
- **`block` usa bucles.** El primero, hacia delante, reparte slots y elabora los `Init`; el segundo, hacia atrás, construye la cadena, porque el `Body` de cada `Let` debe existir antes. Con `block` recursivo, T09.24 acaba en `fatal error: stack overflow`, por la pila de 1 MiB.

:::more Más de `elaborate.go`: `expr`, los spans y `equalityPrim`
- **`expr` sí es recursivo.** El parser acota la profundidad de una expresión (`MaxNesting`, `MaxChain`), así que un árbol del parser es seguro; uno hecho a mano, no.
- **`shortCircuit`** es el `match` de la tabla; su literal lleva el span de toda la expresión.
- **El `PrimCall` lleva el span del operador,** que es donde el capítulo 10 informará de un desbordamiento. Con `x.Span`, T09.20 da `add_int: the span is "a  +   b" at 2:11, want "+" at 2:14`.
- **`equalityPrim`.** Una variable de tipo sin ligar da la primitiva de `Int`: en ejecución no existe un valor de tipo desconocido, y el capítulo 13 quita la regla.
:::

{{reftest T09.15}}

{{reftest T09.21}}

Si te atascas o tu versión se desvía demasiado, copia el fichero de `ref/ch09-s2` y sigue.

Al terminar: tus tests en verde, commit y etiqueta `ch09-s2` (mensaje: `ch09-s2: elaboration to Core S0`); compárate con `ref/ch09-s2`.

### Sesión 3: Core Lint, el impresor y `ki emit core` (≈ 2 h)

#### Los tests, en rojo

1. **Escribe T09.30–T09.51 y amplía T09.09 y T09.10.** Los dos goldens van en `tests/golden/ch09/`, y `TestCLI` cambia su línea de uso. `withStackCap` sale de `elabCompileCapped`, porque ahora limita también a `Lint`, `Print` y `Subexprs`:

{{diff ch09-s2..ch09-s3 internal/core/elaborate_test.go}}

La interfaz:

```go
package core
func (p *Program) Subexprs(root ExprID) iter.Seq[ExprID]
func Lint(p *Program, in *base.Interner) []string // empty means valid; each one reads "fun NAME: WHAT"
func Print(p *Program, in *base.Interner) string

package driver
func lintOrPanic(p *core.Program, in *base.Interner)
func (s *Session) lint(p *core.Program)
// usage: ki emit tokens|ast|core [--diagnostics=human|short|json] FILE
```

2. **Míralos fallar.** `internal/core` e `internal/driver` no compilan (`undefined: core.Lint`, `undefined: lintOrPanic`), y los goldens salen con 2: ``ki emit: unknown target `core` ``.
3. **Añade cinco stubs:** `Subexprs` que no entrega nada, `Lint` que devuelve `nil`, `Print` que devuelve `""`, y `lintOrPanic` y `lint` vacías. Nada hace panic. Fallan 8 subtests de `TestLint`, 2 de `TestSubexprs`, los 5 de `TestPrint`, 3 de `TestCLI`, 2 de `TestICE` y los dos goldens.
4. **Los tres `accepts_*` pasan ya,** porque un `Lint` vacío lo acepta todo: por eso existen los `rejects_*`.

#### El código

{{file ch09-s3 internal/core/lint.go}}

- **`appendChildren`** junta en un sitio los hijos de cada nodo y su orden. Lo usan `Subexprs` y `walk`, salvo el `Let`, que `walk` apila a mano. `kids[:0]` reutiliza el array: un solo buffer para todo el recorrido.
- **`Subexprs`** devuelve un closure de tipo `func(yield func(ExprID) bool)`. Sin el `if !yield(id)`, el `break` de T09.42 acaba en el panic del capítulo 4.
- **`walk`** apila los pasos de un `Let` al revés de como salen. Si `lintBind` saliera antes que el `Init`, T09.33 fallaría.
- **`scope` cuenta, por `Var`, los binders que abarcan la expresión:** un conjunto sacaría de ámbito una `Var` con dos al acabar el `Let` interior (T09.32). `delete(m, k)` quita una entrada de un mapa, aquí al llegar a 0.
- **`Lint` recibe el `Interner`** porque su texto es el mensaje del informe, y quien lo lee tiene el fichero, no los ids.

{{file ch09-s3 internal/core/print.go}}

:::more El impresor: tareas, sangría y nombres
- **Una pila de tareas.** `node` escribe el principio de un nodo y apila el resto; `push` pone `pr.seq` al revés para que la primera salga antes.
- **Sangría.** Lo que empieza en mitad de una línea sigue en ella, y sus otras líneas llevan dos espacios más, salvo el `}`. Un `Let` de una cadena (`chain`) mantiene la sangría.
- **Nombres.** `x`, `x_1`, `x_2`…, y `_`, `_1`…, saltando un número que ya sea el nombre de otra variable: dos variables de una función nunca se imprimen igual.
- **Literales.** `strconv.FormatInt` es el inverso de `ParseInt` (capítulo 5): de `int64` a texto, en la base que le pidas. `strconv.FormatBool` da `true` o `false`, y `strconv.FormatUint`, en los tests, es `FormatInt` para un `uint64`.
- **Es para leer, no para parsear.** Un `Let` como callee no tiene delimitador: `print({ let g = f; g }(1))` se imprime `print(let g = f;` y, debajo, `g(1))`, que parece un `Let` cuyo cuerpo es la llamada.
:::

{{diff ch09-s2..ch09-s3 internal/driver/pipeline.go}}

{{diff ch09-s2..ch09-s3 internal/driver/emit.go}}

{{diff ch09-s2..ch09-s3 internal/driver/commands.go}}

- **`lintOrPanic`** une las violaciones con `\n` y hace panic: el pase `lint` lo convierte en un ICE.
- **`emitCore` para antes de `Lower` si hay errores.** Un error del programa da su diagnóstico y código 1, no un ICE. Tampoco necesita `main`.
- **`Lower` no llama a `lint`.** Cada comando que baja un programa debe llamarlo justo después, y lo vigila una fila de `debug_hook_exits_70`; `ki run` necesitará la suya.

Los dos goldens:

{{file ch09-s3 tests/golden/ch09/fib_core.ki}}

{{file ch09-s3 tests/golden/ch09/blocks_core.ki}}

{{reftest T09.33}}

{{reftest T09.39}}

{{reftest T09.42}}

Si te atascas o tu versión se desvía demasiado, copia el fichero de `ref/ch09-s3` y sigue.

Al terminar: tus tests en verde, commit y etiqueta `ch09-s3` (mensaje: `ch09-s3: Core Lint, printer and ki emit core`); compárate con `ref/ch09-s3`.

## Ejecútalo

Con `ki` construido (`go build -o build/bin/ki ./cmd/ki`), el `fib.ki` del objetivo da lo de arriba. Un programa con todo el azúcar:

```console
$ cat blocks.ki
fn ok(a: Int, b: Int) -> Bool {
  a < b && !(a == 0)
}

fn main() {
  let x = 1
  if ok(x, 2) { print(x) }
  print(-x)
}
$ build/bin/ki emit core blocks.ki
fn ok(a, b) =
  match lt_int(a, b) {
    true => not_bool(eq_int(a, 0))
    false => false
  }
fn main() =
  let x = 1;
  let _ = match ok(x, 2) {
    true => print(x)
    false => ()
  };
  print(neg_int(x))
```

Un error del programa no es un ICE, y con él no hay Core:

```console
$ cat bad.ki
fn main() { print(nope) }
$ build/bin/ki emit core bad.ki
error[E0301]: unknown name `nope`
 --> bad.ki:1:19
  |
1 | fn main() { print(nope) }
  |                   ^^^^ not found in this scope
$ echo $?
1
```

Un fallo del compilador, con el gancho:

```console
$ KI_DEBUG_PANIC_IN=lint build/bin/ki emit core fib.ki 2>&1 | head -3
internal compiler error: lint: debug panic requested
goroutine 1 [running]:
runtime/debug.Stack()
$ KI_DEBUG_PANIC_IN=lint build/bin/ki emit core fib.ki 2>/dev/null
$ echo $?
70
```

Debajo va la pila entera, con tus rutas. Stdout queda vacío.

## Ejercicios

:::exercise
**1. Binders únicos.** Un `Init` que lee la `Var` de su `Let` ya es una violación (T09.33). `Lint` tolera una `Var` ligada por dos binders. Detéctalo con la violación ``variable `x` is bound more than once``, y pruébalo con un test que construya Core inválido a mano.
:::

:::solution
En `lint.go`, un mapa para todo el programa (`bound: make(map[VarID]bool)` en `Lint`, y el campo `bound map[VarID]bool` en `linter`) y un método:

```go
// binder reports the variable v if another binder of the Program has bound it.
func (l *linter) binder(v VarID) {
	if l.bound[v] {
		l.report("variable `%s` is bound more than once", l.in.String(l.p.Vars.Get(v).Name))
	}
	l.bound[v] = true
}
```

Se llama en el bucle de `Params` de `fun`, antes de `l.slot(v, f.FrameSize)`, y en el `case Let` de `check`, antes de `l.slot(e.Var, frame)`. El test, en `TestLint`:

```go
t.Run("rejects_a_variable_bound_twice", func(t *testing.T) {
	// let v = 1; add_int(let v = 2; v, v): two nested Lets of one variable,
	// read in the outer body after the inner Let. The scope count accepts it.
	l := newLintProg()
	v := l.variable(0)
	inner := l.Let(span(0, 1), v, l.LitInt(span(0, 1), 2), l.VarRef(span(0, 1), v))
	sum := l.Prim(span(0, 1), core.PrimAddInt, []core.ExprID{inner, l.VarRef(span(0, 1), v)})
	f := l.fn(1, nil, l.Let(span(0, 1), v, l.LitInt(span(0, 1), 1), sum))
	lintCheck(t, "nested", l.P, l.in, lintFun(f)+": variable "+lintVar(v)+" is bound more than once")

	// A parameter and a Let of the same function.
	l = newLintProg()
	p := l.variable(0)
	f = l.fn(1, []core.VarID{p}, l.Let(span(0, 1), p, l.LitInt(span(0, 1), 1), l.VarRef(span(0, 1), p)))
	lintCheck(t, "parameter and Let", l.P, l.in, lintFun(f)+": variable "+lintVar(p)+" is bound more than once")

	// Two functions: a variable belongs to one of them, and the second
	// one is the one reported.
	l = newLintProg()
	w := l.variable(0)
	l.fn(1, []core.VarID{w}, l.VarRef(span(0, 1), w))
	g := l.fn(1, []core.VarID{w}, l.VarRef(span(0, 1), w))
	lintCheck(t, "two functions", l.P, l.in, lintFun(g)+": variable "+lintVar(w)+" is bound more than once")

	// Two variables with the same name are not one variable: what counts
	// is the VarID. It is what the elaborator makes of a shadowing `let`.
	c := testsupport.Compile(t, "fn s(x: Int) -> Int {\n  let y = x\n  let y = y + 1\n  y\n}\n")
	lintCheck(t, "shadowing", c.Core, c.Session.Interner)
})
```

Sin la comprobación, el primer caso da `nested: Lint returned [], want […]`. Si la llamada falta solo en `Params`, fallan `parameter and Let` y `two functions`. `nested` lee `v` tras el `Let` interior porque ahí se separa el contador de un conjunto: con un `scope` booleano, `Lint` añade un ``unbound variable `v0` `` falso.

Con la comprobación, T09.32 se pone en rojo a propósito: su fila `a variable bound by two nested Lets` es el programa de `nested`, y ahora es una violación (``Lint returned ["fun f1: variable `v1` is bound more than once"], want []``). Quítala, porque `nested` ya la cubre, y T09.32 se queda en nueve casos. Cambia también los comentarios de `Lint` y de `Var` (`core.go`), que dicen que esto no se comprueba. Los demás tests del capítulo siguen en verde: ningún otro de los que pasan por `Lint` liga dos veces una variable.
:::

:::exercise
**2. `ki emit core --ids`.** Añade un flag que escriba, tras cada variable, su `VarID` y su slot, como el `--stats` del ejercicio 1 del capítulo 5.
:::

:::solution
Si no conservaste el parámetro de aquel ejercicio, `fileArgs` recibe una función que añade flags, y `check`, `emit tokens` y `emit ast` pasan `nil`:

```go
func fileArgs(s *Session, c *command, args []string, more func(fs *flag.FlagSet)) (path string, code int, done bool) {
	fs := newFlagSet(c)
	diagnostics := fs.String("diagnostics", "human", "how to write diagnostics: human, short or json")
	if more != nil {
		more(fs)
	}
	// … as before
```

En `emitCore` (con `flag` importado):

```go
	var ids bool
	path, code, done := fileArgs(s, c, args, func(fs *flag.FlagSet) {
		fs.BoolVar(&ids, "ids", false, "write the VarID and the slot after each variable")
	})
	// … after s.lint(p):
	text := core.Print(p, s.Interner)
	if ids {
		text = core.PrintIDs(p, s.Interner)
	}
	return finishEmit(s, c, text)
```

En `print.go`, un campo `ids bool` en `printer`; el cuerpo de `Print` pasa a `printAll`:

```go
func Print(p *Program, in *base.Interner) string { return printAll(p, in, false) }

// PrintIDs is Print with the VarID and the slot after each variable: `x#3@0` is
// the variable 3, in the slot 0 of its frame.
func PrintIDs(p *Program, in *base.Interner) string { return printAll(p, in, true) }

func printAll(p *Program, in *base.Interner, ids bool) string {
	pr := &printer{p: p, in: in, ids: ids}
	// … as Print did
```

Y al final de `name`, tras `pr.used[s] = true`, para que los sufijos no se mezclen con los nombres en uso:

```go
	if pr.ids {
		s += "#" + strconv.Itoa(int(v)) + "@" + strconv.Itoa(int(pr.p.Vars.Get(v).Slot))
	}
```

La línea de uso de `commands.go`, común a los tres destinos aunque `--ids` solo valga en `core`, pasa a `ki emit tokens|ast|core [--ids] [--diagnostics=human|short|json] FILE`, también en sus cuatro apariciones en `cli_test.go` (las dos constantes y los dos literales de `emit_ast`). El test, en `TestCLI`:

```go
t.Run("emit_core_ids", func(t *testing.T) {
	// Var 0 is the parameter of the prelude's print, so x is 1 and y is 2;
	// the slots start again in each function.
	path := writeKi(t, "fn id(x: Int) -> Int {\n  let y = x\n  y\n}\n")
	code, out, errOut := runCLI(t, "emit", "core", "--ids", path)
	want := "fn id(x#1@0) =\n  let y#2@1 = x#1@0;\n  y#2@1\n"
	if code != 0 || out != want || errOut != "" {
		t.Errorf("--ids: got %d %q %q, want 0 %q \"\"", code, out, errOut, want)
	}
	// Without the flag, the same text as ever; and the flag is only of core.
	code, out, _ = runCLI(t, "emit", "core", path)
	if want := "fn id(x) =\n  let y = x;\n  y\n"; code != 0 || out != want {
		t.Errorf("no --ids: got %d %q, want 0 %q", code, out, want)
	}
	if code, _, _ := runCLI(t, "emit", "tokens", "--ids", path); code != 2 {
		t.Errorf("emit tokens --ids: got %d, want 2", code)
	}
})
```

Sobre el `fib.ki` del objetivo:

```console
$ build/bin/ki emit core --ids fib.ki
fn fib(n#1@0) =
  match lt_int(n#1@0, 2) {
    true => n#1@0
    false => add_int(fib(sub_int(n#1@0, 1)), fib(sub_int(n#1@0, 2)))
  }
fn main() =
  let x#2@0 = fib(20);
  print(x#2@0)
```
:::

:::exercise
**3. `Subexprs` en postorden.** Escribe `SubexprsPost`, que entrega cada expresión después de sus hijos, sin recursión, con su test.
:::

:::solution
Una expresión entra dos veces en la pila: la primera apila su vuelta y sus hijos, y la segunda se entrega.

```go
// SubexprsPost returns an iterator over root and every expression under it, in
// post-order: each expression comes after its children. Like Subexprs, it keeps
// a stack of its own, so depth adds no frames of Go, and a loop that stops early
// stops the walk.
func (p *Program) SubexprsPost(root ExprID) iter.Seq[ExprID] {
	type step struct {
		id   ExprID
		done bool // its children have been visited: it is its turn
	}
	return func(yield func(ExprID) bool) {
		stack := []step{{id: root}}
		var kids []ExprID
		for len(stack) > 0 {
			st := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			if st.done {
				if !yield(st.id) {
					return
				}
				continue
			}
			stack = append(stack, step{id: st.id, done: true})
			e := p.Exprs.Get(st.id)
			kids = p.appendChildren(kids[:0], &e)
			for i := len(kids) - 1; i >= 0; i-- {
				stack = append(stack, step{id: kids[i]})
			}
		}
	}
}
```

En `TestSubexprs`:

```go
t.Run("postorder_visits_children_first", func(t *testing.T) {
	// let x = add_int(1, 2); x
	b, p := newBuilder()
	x := p.Vars.Push(core.Var{})
	one := b.LitInt(span(0, 1), 1)
	two := b.LitInt(span(2, 3), 2)
	sum := b.Prim(span(0, 3), core.PrimAddInt, []core.ExprID{one, two})
	ref := b.VarRef(span(5, 6), x)
	let := b.Let(span(0, 6), x, sum, ref)
	if got, want := slices.Collect(p.SubexprsPost(let)), []core.ExprID{one, two, sum, ref, let}; !slices.Equal(got, want) {
		t.Errorf("post-order of the Let visited %v, want %v", got, want)
	}
	// A leaf is its own walk, and a break stops it.
	if got, want := slices.Collect(p.SubexprsPost(one)), []core.ExprID{one}; !slices.Equal(got, want) {
		t.Errorf("post-order of a leaf visited %v, want %v", got, want)
	}
	var first []core.ExprID
	for id := range p.SubexprsPost(let) {
		first = append(first, id)
		if len(first) == 2 {
			break
		}
	}
	if want := []core.ExprID{one, two}; !slices.Equal(first, want) {
		t.Errorf("a loop that breaks after two ids got %v, want %v", first, want)
	}
	// 100 000 nested Lets, under the 1 MiB stack cap: no recursion.
	root := b.LitUnit(span(0, 1))
	for range 100_000 {
		root = b.Let(span(0, 1), x, b.LitInt(span(0, 1), 0), root)
	}
	n := 0
	withStackCap(func() {
		for range p.SubexprsPost(root) {
			n++
		}
	})
	if n != 200_001 {
		t.Errorf("the long chain gave %d ids, want 200001", n)
	}
})
```

Si entregas al sacar la expresión por primera vez, sale el preorden: `post-order of the Let visited [4 2 0 1 3], want [0 1 2 3 4]`.
:::

:::exercise
**4. `guard` y los errores del runtime.** `guard` ya recoge cualquier panic, también los del runtime. Prueba con un test que una división por cero en Go dentro de un pase acaba en un ICE con código 70. Añade después a `ICE` un método `Runtime() bool` que diga si el panic lo lanzó el runtime (`runtime.Error`, una interfaz que cumplen los valores de esos panics) o el código del compilador.
:::

:::solution
En `ice.go`, con `runtime` importado:

```go
// Runtime reports whether the runtime raised the panic (an index out of range, a
// division by zero, a nil pointer), as opposed to a panic of the compiler's code.
func (e *ICE) Runtime() bool {
	_, ok := e.Value.(runtime.Error)
	return ok
}
```

En `TestGuard`:

```go
t.Run("captures_runtime_error", func(t *testing.T) {
	zero := 0 // a variable: `1 / 0` does not compile
	ice := guard("check", func() { _ = 1 / zero })
	if ice == nil {
		t.Fatal("guard returned nil for a division by zero")
	}
	if want := "internal compiler error: check: runtime error: integer divide by zero"; ice.Error() != want {
		t.Errorf("Error() = %q, want %q", ice.Error(), want)
	}
	if !ice.Runtime() {
		t.Errorf("Runtime() = false for %#v, want true", ice.Value)
	}
	if ice := guard("check", func() { panic("boom") }); ice == nil || ice.Runtime() {
		t.Errorf("panic(\"boom\") gave %v, want an ICE with Runtime() false", ice)
	}
	// All the way to the exit code, as a pass that divides by zero.
	code := func() (code int) {
		s := newSession("")
		defer s.catchICE(&code)
		s.runPass("check", func() { _ = 1 / zero })
		return 0
	}()
	if code != ExitICE {
		t.Errorf("a pass that divides by zero ends with %d, want %d", code, ExitICE)
	}
})
```

`1 / 0` con constantes no llega a ejecutarse: `invalid operation: division by zero`. Si `guard` guardara `Value: fmt.Sprint(r)`, se perdería el tipo, y el test daría `Runtime() = false for "runtime error: integer divide by zero", want true`: por eso `ICE` guarda el valor tal cual.
:::

## Preguntas de repaso

:::review
¿Por qué `a && b` se elabora a un `match` y no a una primitiva `and_bool(a, b)`?
:::

:::answer
Porque Core es estricto: evalúa los argumentos antes de la llamada, y `and_bool(a, b)` evaluaría `b` siempre. `f() && g()` llamaría a `g` aunque `f` diera `false`. En un `match`, `b` solo se evalúa en el brazo `true`.
:::

:::review
¿Por qué `Subexprs` y `Lint` usan una pila propia, si la pila de Go llega a 1 GB?
:::

:::answer
Porque la longitud de una cadena de `Let` la decide el programa: nada la acota, a diferencia del anidamiento de una expresión. Pasado el límite, Go no hace panic, sino un `fatal error` que `recover` no ve, así que ni siquiera habría informe de ICE. Por eso los tests limitan la pila a 1 MiB: con el límite por defecto, una recursión sobre 100 000 sentencias cabría y no se vería.
:::

:::review
¿Por qué `guard` llama a `debug.Stack()` dentro de la función diferida y no después de `f()`?
:::

:::answer
Porque si `f` hace panic, lo que hay después de `f()` no se ejecuta; y fuera de `guard`, la pila ya no contiene la función que falló. La función diferida corre antes de que el panic deshaga nada, así que la pila aún muestra su origen, que es lo que necesita quien lee el informe.
:::

:::review
¿Por qué `runPass` vuelve a hacer panic con el `*ICE`, en lugar de devolverlo?
:::

:::answer
Porque el pase siguiente no debe correr sobre lo que el anterior dejó a medias, y cada llamador tendría que acordarse de mirar el resultado. El panic deshace el comando entero hasta `catchICE`, en un solo sitio: `Parse` no recibe un `TokenBuffer` vacío, y ningún `ReportAll` se ejecuta.
:::

:::review
¿Por qué un ICE sale con 70, si Go ya sale con 2 tras un panic?
:::

:::answer
Porque 2 significa aquí «uso incorrecto de la CLI», y en un panic de Go sin informe el pase solo se deduce leyendo la pila. Con 70 (`EX_SOFTWARE`), un script o el runner de golden distinguen un pase roto de un comando mal escrito y de un programa con errores (1).
:::

:::review
¿Por qué una violación de `Lint` es un ICE y no un diagnóstico?
:::

:::answer
Porque un diagnóstico le dice a quien programa en Ki que corrija su fichero, y aquí el fichero no tiene la culpa: pasó el front end sin errores, y el Core lo hizo el compilador. Por eso `emitCore` comprueba el sink antes de `Lower`: un error del programa nunca llega a `Lint`.
:::

:::review
¿Por qué un `Let` apila en `walk` cuatro pasos, y no sus dos hijos como cualquier otro nodo?
:::

:::answer
Porque la variable solo está en ámbito en el `Body`: tiene que entrar tras el `Init` y salir tras el `Body`. Con los dos hijos sin más, un `Init` que lee la `Var` de su `Let` pasaría, o la variable seguiría visible para un argumento hermano del `Let`. Esos dos pasos intermedios son el trabajo que una recursión haría antes y después de cada llamada.
:::

:::review
¿Por qué `defer s.catchICE(&code)` pasa un puntero, si los argumentos de `defer` se evalúan en esa línea?
:::

:::answer
Justo por eso: en esa línea `code` aún vale 0, y un `catchICE(code int)` recibiría ese 0 y escribiría en su copia. Lo que se evalúa es la dirección del resultado con nombre, y a través de ella la diferida cambia lo que `Main` devuelve. Sin el puntero, un ICE acabaría con código 0.
:::

## Resumen

- Core es la única referencia semántica: pequeño, estricto, sin tipos y sin azúcar.
- La elaboración da Core S0: `if`, `&&` y `||` a `match`, bloques a cadenas de `Let`, operadores a primitivas.
- Cada binder es una `Var` con su slot y su `Rep`.
- El builder crea los nodos de abajo arriba y no valida; `Lint` comprueba los invariantes.
- `iter.Seq` produce con `yield`; una cadena de `Let` de cualquier longitud pide una pila explícita.
- `defer` es LIFO, y sus argumentos se evalúan en su línea; un closure lee la variable al correr.
- `debug.Stack()` dentro de la diferida conserva el origen del panic.
- `guard`, `runPass` y `catchICE` convierten un panic de un pase en un informe con código 70.
- Un `fatal error` no pasa por `recover`.
- Una violación de `Lint` es un ICE; un error del programa nunca llega a `Lower`.

## Go en el trabajo

**`defer` para liberar recursos.** `defer f.Close()` o `defer mu.Unlock()`, junto a lo que abren o bloquean, cubren todos los caminos de salida.

En un bucle, no: las llamadas corren al final de la función, no de la vuelta, así que los ficheros se quedan abiertos. Y el compilador solo usa su forma barata, los *open-coded defers*, en funciones sin `defer` en bucles: en un bucle, cada `defer` cuesta más de diez veces una llamada. La salida es llevar el cuerpo a una función.

**`recover` en servidores.** `net/http` recupera el panic de un handler, registra la pila y corta esa petición: una rota no tumba las demás. Ki tiene un programa en un proceso, así que recupera para informar y termina. `recover` solo ve los panics de su goroutine, y uno sin recuperar en cualquier otra tumba el proceso.

**Las IR en compiladores reales.** GHC baja Haskell a Core y lo comprueba con `-dcore-lint`; Swift tiene SIL, sobre el que emite diagnósticos propios del lenguaje; MLIR es una infraestructura de IR extensible con dialectos. El compilador de Go tiene `checkFunc`, que comprueba su IR si lo activas.

:::more Las fuentes y la medida
**`defer`.** `src/runtime/panic.go` en Go 1.27: los *open-coded defers* se usan «for functions in which no defer statements occur in loops». En esta máquina, `Lock`, `n++` y `Unlock` en un `func() { … }()` costaron unos 3,5 ns sin `defer` y 4 con él; mil `defer` en un bucle, unos 13 µs, frente a 1 µs por las mismas mil llamadas sin él. `//go:noinline` impide que el compilador sustituya la llamada por su cuerpo, que aquí está vacío, y no quede nada que medir. El bloque va en un `_test.go` que importe `sync` y `testing`, y se ejecuta con `go test -bench . -run '^$'`:

```go
//go:noinline
func work() {}

func BenchmarkCall(b *testing.B) {
	for b.Loop() {
		for range 1000 {
			work()
		}
	}
}

func BenchmarkDefer(b *testing.B) {
	for b.Loop() {
		func() {
			for range 1000 {
				defer work()
			}
		}()
	}
}

var (
	mu sync.Mutex
	n  int
)

func BenchmarkLock(b *testing.B) {
	for b.Loop() {
		func() {
			mu.Lock()
			n++
			mu.Unlock()
		}()
	}
}

func BenchmarkLockDefer(b *testing.B) {
	for b.Loop() {
		func() {
			mu.Lock()
			defer mu.Unlock()
			n++
		}()
	}
}
```

**`net/http`.** La documentación de `Handler`: «If ServeHTTP panics, the server (the caller of ServeHTTP) assumes that the effect of the panic was isolated to the active request. It recovers the panic, logs a stack trace to the server error log, and either closes the network connection or sends an HTTP/2 RST_STREAM». El código es el `defer` de `(*conn).serve`, en `server.go`, que escribe `http: panic serving …`; el valor `http.ErrAbortHandler` corta sin registrar.

**GHC.** Su guía de usuario, sobre `-dcore-lint`: «Turn on heavyweight intra-pass sanity-checking within GHC, at Core level. (It checks GHC's sanity, not yours.)». Es la distinción entre un ICE y un diagnóstico.

**Swift.** `docs/SIL/SIL.md`: «SIL is an SSA-form IR with high-level semantic information designed to implement the Swift programming language». En SSA (*static single assignment*), cada variable se asigna una sola vez.

**MLIR.** Su *LangRef*: «MLIR (Multi-Level IR) is a compiler intermediate representation»; «Dialects are the mechanism by which to engage with and extend the MLIR ecosystem».

**Go.** `src/cmd/compile/internal/ssa/check.go`: «checkFunc checks invariants of f», y `compile.go` lo llama tras cada pase cuando `checkEnabled` está activo.
:::
