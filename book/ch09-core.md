---
chapter: 9
title: Core: la referencia semántica
part: I
hours: 6
milestone: `ki emit core`; Core Lint en verde
---

## Objetivo

Al terminar, `build/bin/ki emit core fib.ki` traduce un programa sin errores a Core, su representación intermedia (IR), comprueba el resultado y lo enseña. La salida está en «Ejecútalo».

En Core ya no hay `if` ni `+` ni tipos. El capítulo 10 ejecutará este Core para cerrar el hito M1.

Un fallo de un pase deja de ser un panic suelto: es un ICE (*internal compiler error*), un informe con código 70.

## Teoría

### Por qué una IR central

Ki tendrá tres backends: intérprete, VM y nativo. Si cada uno leyera el árbol de sintaxis, cada uno decidiría qué significa un `&&`, y acabarían en desacuerdo.

Por eso hay un lenguaje pequeño en medio. **Core es la única referencia semántica**: lo que Core dice que hace un programa es lo que hace.

No tiene azúcar ni tipos, y es **estricto**: evalúa los argumentos antes de la llamada.

A la Parte I le bastan nueve clases de nodo:

```math
e ::= n | true | false | ()              LitInt, LitBool, LitUnit
    | x                                  VarRef: una variable
    | f                                  FunRef: una función
    | e₀(e₁, …, eₙ)                      Call
    | p(e₁, …, eₖ)                       PrimCall: p es una primitiva, como add_int,
                                         y k = aridad(p) ∈ {1, 2}
    | let x = e₁; e₂                     Let, con Init e₁ y Body e₂
    | match e { π₁ => e₁ … πₘ => eₘ }    Match
π ::= true | false | _
```

**La etapa S0.** Core se transforma por etapas, de S0 a S5, todas subconjuntos o extensiones de la misma IR, y cada pase baja de una a la siguiente. Este capítulo solo tiene S0.

### La elaboración

**La elaboración** traduce el árbol tipado a S0. `⟦e⟧` es el Core de la expresión Ki `e`:

```math
⟦if c { a } else { b }⟧  =  match ⟦c⟧ { true => ⟦a⟧; false => ⟦b⟧ }
⟦if c { a }⟧             =  match ⟦c⟧ { true => ⟦a⟧; false => () }
⟦a && b⟧                 =  match ⟦a⟧ { true => ⟦b⟧; false => false }
⟦a || b⟧                 =  match ⟦a⟧ { true => true; false => ⟦b⟧ }

⟦{ let x = e; r }⟧       =  let x = ⟦e⟧; ⟦{ r }⟧
⟦{ s; r }⟧               =  let _ = ⟦s⟧; ⟦{ r }⟧       s, una sentencia de expresión;
                                                       cada _ es una Var nueva
⟦{ t }⟧                  =  ⟦t⟧                         t, la cola
⟦{ }⟧                    =  ()

⟦a + b⟧ = add_int(⟦a⟧, ⟦b⟧)     ⟦-a⟧ = neg_int(⟦a⟧)     ⟦!a⟧ = not_bool(⟦a⟧)
⟦a ⊙ b⟧ = p⊙(⟦a⟧, ⟦b⟧)          con - * / % < <= > >= ↦ sub_int mul_int div_int rem_int
                                                        lt_int le_int gt_int ge_int
⟦a == b⟧ = eq_bool(⟦a⟧, ⟦b⟧)    si τ(a) = Bool
⟦a == b⟧ = eq_int(⟦a⟧, ⟦b⟧)     si τ(a) = Int, o τ(a) es una variable de tipo sin ligar
⟦a != b⟧                        igual, con ne_bool y ne_int
```

- **`&&` y `||` son `match`** porque Core es estricto: un `and_bool(a, b)` evaluaría siempre `b`.
- **`==` se decide aquí:** es el último pase que conoce los tipos. Una variable de tipo sin ligar da `eq_int`: en ejecución no existe un valor de tipo desconocido.
- **Solo corre sobre programas sin errores.** Ante un nodo de error hace panic: el fallo es del compilador.

**Binders y slots.** Un *binder* introduce una variable: un parámetro o un `let`. Cada uno es una `Var` propia, aunque se llame igual que otra: un id basta para saber de cuál se habla.

Su `Slot` es su índice en el marco, la memoria de los parámetros y los `let` de una llamada, que usará la máquina del capítulo 10:

```math
binders(f) = v₀, v₁, …, vₙ₋₁     parámetros primero, luego en el orden en que el programa los nombra
slot(vᵢ) = i                    FrameSize(f) = n
```

Los bloques interiores comparten el marco de su función, y nada reutiliza un slot.

**Las anotaciones `rep`.** `Rep` dice cómo se representa en ejecución el valor de una `Var`; hoy, siempre `RepVal`. Existe ya porque el capítulo 56 añade `RepI64` y `RepF64`, para no empaquetar números en código de un solo tipo. Solo la elaboración puede rellenarlo: aún ve los tipos.

### Los invariantes y Lint

Core no tiene tipos, así que un pase con un bug escribirá un Core sin sentido. **Core Lint** comprueba lo que los pases prometen y los siguientes dan por hecho.

El primero es el ámbito. `Δ ⊢ e` dice que, con las variables `Δ` ligadas, `e` solo lee variables ligadas:

```math
  x ∈ Δ                 Δ ⊢ e₁     Δ ∪ {x} ⊢ e₂             params(f) ⊢ cuerpo(f)
───────── (Var)        ──────────────────────── (Let)       ───────────────────── (Fun)
  Δ ⊢ x                  Δ ⊢ let x = e₁; e₂                          ⊢ f

  Δ ⊢ e₁   …   Δ ⊢ eₙ
───────────────────── (Nodo)      e, otro nodo; e₁ … eₙ, sus hijos
         Δ ⊢ e
```

(Let) comprueba el `Init` con el `Δ` de fuera: un `Init` que lee la `Var` de su `Let` es una violación (en Ki, `let x = x` son dos `Var`).

Los demás invariantes:

```math
f(e₁, …, eₙ), f una FunRef   ⇒   n = |params(f)|       (a través de una variable no se
                                                        comprueba: Lint no sabe qué guarda)
p(e₁, …, eₖ)                 ⇒   k = aridad(p)
match e { π₁ … πₘ }          ⇒   {true, false} ⊆ {π₁ … πₘ}  ∨  _ ∈ {π₁ … πₘ}
toda función f               ⇒   tiene cuerpo o nombre de extern, no ambos ni ninguno
v, binder de f               ⇒   slot(v) < FrameSize(f)
```

Una violación es un error del compilador, no del programa: se convierte en un ICE. Los binders únicos aún no se comprueban: son el ejercicio 1.

### Builder e interfaces pequeñas

`Expr` es un struct plano, en un `IdVec` como el árbol de sintaxis. `Kind` dice qué campos significan algo, y los demás valen cero.

Rellenarlo a mano invita a olvidar un campo, así que hay una **API builder**:

- **Un método por clase de nodo:** lo crea, lo guarda y devuelve su id.
- **De abajo arriba:** los hijos existen antes que el padre.
- **Copia las listas al pool:** quien llama puede reutilizar su slice.
- **No valida:** así los tests fabrican Core inválido.

:::from Rust
Sería un `enum` con datos, y leer `Init` de un `LitInt` no compilaría. En Go es una convención, que protege el builder.
:::

**Interfaces pequeñas.** `Builder` es un struct y no una interfaz: tiene una sola implementación, y una interfaz la declara quien la consume.

Cuantos menos métodos, más tipos la cumplen sin saberlo. `*ICE` es un `error` por su `Error()`, y `Prim`, un `fmt.Stringer` por su `String()`.

### Recorridos: `iter.Seq` y una pila propia

El recorrido más simple de Core es `Program.Subexprs`. Da un `iter.Seq[ExprID]`, como el `iter.Seq2` del capítulo 4: la raíz y todo lo que hay debajo, cada expresión antes que sus hijos (preorden). En Python sería un generador; aquí es una función que recibe `yield`.

**No puede ser recursivo.** Un bloque de 100 000 sentencias es una cadena de 100 000 `Let`, y el parser solo acota el anidamiento de las expresiones (capítulo 6). Con un marco de Go por `Let`, ese bloque cabe en el 1 GB de pila por defecto; uno bastante mayor da un `fatal error`, que `recover` no ve.

Los tests bajan el límite a 1 MiB: con 1 GB, esa recursión no se notaría.

**La pila es un slice:**

```pseudo
función Subexprs(raíz):
  pila ← [raíz]
  mientras pila no esté vacía:
    id ← saca(pila)
    si yield(id) devuelve falso entonces devuelve
    para cada hijo h de id, del último al primero:
      apila(pila, h)
```

Los hijos entran al revés para que el primero salga antes. Si `yield` devuelve `false`, se acaba: seguir entregando da el panic del capítulo 4.

`Lint` necesita más, porque la regla (Let) liga `x` en `e₂` y no en `e₁`.

En `recorre`, `ámbito` es un contador y no un conjunto: una `Var` ligada dos veces no sale de ámbito al cerrar el `Let` interior (ejercicio 1).

```pseudo
función recorre(f):                        ámbito[v]: los binders abiertos de v
  si f no tiene cuerpo entonces devuelve                      un extern
  para cada p ∈ params(f): ámbito[p] ← ámbito[p] + 1          la regla (Fun)
  pila ← [visita(cuerpo(f))]
  mientras pila no esté vacía:
    paso ← saca(pila)
    si paso es liga(v) entonces ámbito[v] ← ámbito[v] + 1
    si paso es desliga(v) entonces ámbito[v] ← ámbito[v] − 1
    si paso es visita(e) entonces
      comprueba(e)                         solo el nodo: una VarRef pide ámbito[v] > 0
      si e es let v = e₁; e₂ entonces
        apila desliga(v), visita(e₂), liga(v), visita(e₁)      salen al revés
      si no
        apila los hijos de e, del último al primero
  para cada p ∈ params(f): ámbito[p] ← ámbito[p] − 1          deja ámbito limpio para la siguiente
```

### `defer`, `panic` y `recover`: la frontera del pase

Un panic de Go sin recuperar sale con 2 (capítulo 2), y el pase solo se deduce leyendo la pila. La **frontera del pase** lo convierte en un informe con código propio.

**`defer`** tiene dos reglas nuevas: las llamadas diferidas corren en orden inverso (LIFO), y sus argumentos se evalúan en la línea del `defer`. Un closure, en cambio, lee la variable al correr.

:::from Rust
`Drop` también corre en orden inverso, pero al salir del bloque; `defer`, al salir de la función.
:::

**El patrón `guard`** junta `defer` con `recover`, que solo funciona llamado directamente por la función diferida (capítulo 2). También usa un resultado con nombre que esa función puede escribir (capítulo 3). Con `runPass` y `catchICE`, forma la frontera:

```pseudo
función guard(pase, f) → ice:
  al salir, con una función diferida:
    si r ← recover() no es nulo entonces
      ice ← ICE{pase, r, la pila de ahora}
  f()
  devuelve nulo

función runPass(nombre, f):
  ice ← guard(nombre, «si DebugPanicIn = nombre entonces panic; f()»)
  si ice no es nulo entonces panic(ice)

función catchICE(&código):                 diferida por Main
  r ← recover()
  si r es nulo entonces devuelve
  si r no es un *ICE entonces panic(r)
  escribe el informe por stderr
  código ← 70
```

- **`guard`** toma la pila de la goroutine, como `[]byte`, con `runtime/debug.Stack()` dentro de la diferida: corre antes de que el panic deshaga nada, y al volver de `guard` la pila ya no muestra quién lo hizo.
- **`runPass`** relanza el `*ICE`: ningún pase debe correr sobre lo que otro dejó a medias.
- **`catchICE`** reconoce el `*ICE` con `r.(*ICE)` (capítulo 3) y relanza otro panic, que no viene de un pase.

**¿Por qué 70?** Es `EX_SOFTWARE`, el error interno de `sysexits.h`: un script distingue un programa mal escrito (1), un comando mal escrito (2) y un pase roto (70).

**El gancho.** Para probar la frontera sin un bug a mano, con `KI_DEBUG_PANIC_IN=<pase>` ese pase hace panic al empezar. Lo lee `Main` y no `NewSession`: una `Session` de test no depende del entorno.

:::more Lo que la frontera no cubre
- **Un `fatal error`** no es un panic: el `defer` con `recover` no corre, y el código es 2.
- **Los diagnósticos pendientes.** Cuando salta un ICE, los que ya estaban en el sink no se escriben: con el gancho en `resolve`, un error de sintaxis da el informe y ningún `error[`.
- **Lo que no es un pase.** `core.Print`, `syntax.DumpTokens`, `syntax.PrintModule` y `ReportAll` corren fuera de `runPass`, y un panic ahí saldría con 2.

**Fe de erratas.** En la rama `main`, los comandos ya escriben los diagnósticos como pase `render`, de modo que un panic ahí sale con 70, y el informe de un ICE acaba diciendo cuántos diagnósticos no muestra.
:::

## Preguntas de repaso

:::review
¿Por qué `a && b` se elabora a un `match` y no a `and_bool(a, b)`?
:::

:::answer
Porque Core es estricto: evalúa los argumentos antes de la llamada, y `and_bool(a, b)` evaluaría `b` siempre. `f() && g()` llamaría a `g` aunque `f` diera `false`. En un `match`, `b` solo se evalúa en el brazo `true`.
:::

:::review
¿Por qué elige la elaboración entre `eq_int` y `eq_bool`, y no la máquina?
:::

:::answer
Porque Core no tiene tipos: después de la elaboración, ningún pase sabe si `a` es `Int` o `Bool`. La elaboración es el último pase que conoce los tipos, así que la decisión queda escrita en la primitiva.
:::

:::review
¿Por qué `Subexprs` y `Lint` usan una pila propia, si el límite de la de Go es 1 GB?
:::

:::answer
Porque la longitud de una cadena de `Let` la decide el programa, y nada la acota. Pasado el límite, Go no hace panic, sino un `fatal error` que `recover` no ve: ni siquiera habría informe de ICE. Por eso los tests limitan la pila a 1 MiB: con el límite por defecto, una recursión sobre 100 000 sentencias cabría y no se vería.
:::

:::review
¿Por qué `guard` toma la pila dentro de la función diferida y no después de `f()`?
:::

:::answer
Porque si `f` hace panic, lo que hay después de `f()` no se ejecuta, y fuera de `guard` la pila ya no contiene la función que falló. La diferida corre antes de que el panic deshaga nada, así que la pila aún muestra su origen, que es lo que necesita quien lee el informe.
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
Porque un diagnóstico le dice a quien programa en Ki que corrija su fichero, y aquí el fichero no tiene la culpa: pasó el front end sin errores, y el Core lo hizo el compilador. Por eso el comando comprueba el sink antes de bajar a Core: un error del programa nunca llega a `Lint`.
:::

:::review
¿Por qué, en el recorrido de `Lint`, un `Let` apila cuatro pasos y no sus dos hijos?
:::

:::answer
Porque la variable solo está en ámbito en el `Body`: tiene que entrar tras el `Init` y salir tras el `Body`. Con los dos hijos sin más, un `Init` que lee la `Var` de su `Let` pasaría, o la variable seguiría visible para un argumento hermano del `Let`. Esos dos pasos intermedios son el trabajo que una recursión haría antes y después de cada llamada.
:::

## Resumen

- Core es la única referencia semántica: estricto, sin tipos, sin azúcar y con nueve clases de nodo.
- La elaboración baja `if`, `&&` y `||` a `match`, los bloques a cadenas de `Let` y los operadores a primitivas, y decide `==` por el tipo.
- Cada binder es una `Var` con su slot y su `Rep`.
- El builder crea de abajo arriba y no valida; `Lint` comprueba los invariantes.
- Un `Let` liga su variable en el `Body` y no en el `Init`: la regla (Let).
- Una cadena de `Let` pide una pila explícita; un `fatal error` no pasa por `recover`.
- `defer` es LIFO y evalúa sus argumentos en su línea.
- `guard`, `runPass` y `catchICE` convierten el panic de un pase en un informe con código 70.
- Una violación de `Lint` es un ICE; un error del programa nunca llega a `Lower`.

## Go en el trabajo

**`defer` para liberar recursos.** `defer f.Close()` o `defer mu.Unlock()` cubren todos los caminos de salida.

En un bucle, no: corren al final de la función, no de la vuelta, y los ficheros se quedan abiertos. Ahí la función entera pierde su forma barata, los *open-coded defers*: cada `defer` cuesta más de diez veces una llamada. La salida es llevar el cuerpo a una función.

**`recover` en servidores.** `net/http` recupera el panic de un handler, registra la pila y corta esa petición: una rota no tumba las demás. Ki tiene un programa en un proceso, así que recupera para informar y termina.

`recover` solo ve los panics de su goroutine, y uno sin recuperar en cualquier otra tumba el proceso.

**Las IR en compiladores reales.** GHC baja Haskell a Core y lo comprueba con `-dcore-lint`: «It checks GHC's sanity, not yours», la distinción entre un ICE y un diagnóstico. Swift tiene SIL; MLIR es una infraestructura de IR extensible con dialectos.

## Tests primero

{{cards}}

Úsalas como siempre: escribe el test de la tarjeta, míralo fallar, implementa lo justo para el verde, y solo entonces despliega su referencia.

:::more Los ayudantes de los tests
Los tests de referencia y las soluciones los usan sin definirlos; están en `ref/ch09-s3`.

- **`core_test.go`.** `span` crea un span en el fichero 1. `newBuilder` da un `Builder` sobre un `Program` vacío.
- **`ice_test.go`,** en `package driver` porque prueba `guard` y `runPass`. Al arrancar, borra `KI_DEBUG_PANIC_IN`. `explode` hace panic; tiene nombre, y sale en la pila aunque el compilador sustituya la llamada por su cuerpo. `writeKi` escribe un fichero temporal, `raised` devuelve el valor de un panic y `newSession` crea una `Session` con el gancho pedido.
- **`elaborate_test.go`.** `elabFun` busca una función por nombre, `elabText` da el texto de un span y `elabWant` compara una expresión escrita en una línea, sin spans.
- **`lint_test.go`.** `lintProg` monta programas a mano, con variables `v<id>` y funciones `f<id>`. `lintCheck` compara la lista entera de violaciones.
:::

## Implementación

### Sesión 1: la IR Core, el builder y la frontera de los ICE (≈ 2 h)

#### Los tests, en rojo

1. **Escribe T09.01–T09.13;** los de `internal/core`, en `package core_test`. T09.09 y T09.10 ganan filas en la sesión 3; hasta entonces se escriben como en `git show ref/ch09-s1:book/tests/ch09.tests`. La interfaz, completa:

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

```console doctest="skip: go test escribe los paquetes en un orden que varía"
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

Aquí no valen stubs: con un `Builder` vacío, `p.Exprs.Get` corta el binario de test con un panic.

3. **Escribe `core.go`, `builder.go`, `base.Slice`, `base.AppendRange`, `ice.go` y `tests/main_test.go`, y cambia `Main`, `Session` y `ci/ci-local.sh`.** Deja sin tocar `pipeline.go` y `emit.go`. Queda un solo rojo, el test que exige 70 con el gancho: ningún pase pasa aún por `runPass`.

#### Go: `defer`

Las reglas de `defer` de la Teoría:

```go
x := 1
defer fmt.Println("arg", x)                  // prints: arg 1
defer func() { fmt.Println("closure", x) }() // prints first: closure 2
x = 2
```

#### El código

{{file ch09-s1 internal/core/core.go}}

{{file ch09-s1 internal/core/builder.go}}

Core es el segundo árbol con `ListRange`, así que `slice` y `appendRange` pasan de `syntax` a `base`:

{{diff ch08-s4..ch09-s1 internal/base/idvec.go}}

{{diff ch08-s4..ch09-s1 internal/syntax/ast.go}}

En `parser.go`, `parser_expr.go` y `parser_stmt.go`, cada `appendRange(` pasa a ser `base.AppendRange(`.

{{file ch09-s1 internal/driver/ice.go}}

`guard`, `runPass` y `catchICE` son el pseudocódigo de la Teoría.

{{diff ch08-s4..ch09-s1 internal/driver/cli.go}}

{{diff ch08-s4..ch09-s1 internal/driver/session.go}}

- **`Main`** gana un resultado con nombre para que `defer s.catchICE(&code)` lo escriba a través del puntero.
- **`catchICE`** es ella misma la función diferida, y por eso su `recover` funciona.

Los tests del gancho ponen `KI_DEBUG_PANIC_IN` con `t.Setenv`, que la restaura al acabar. Un shell que la exporte rompería los golden, así que sus runners la borran:

{{file ch09-s1 tests/main_test.go}}

{{diff ch08-s4..ch09-s1 ci/ci-local.sh}}

Ahora conecta los pases. `runPass` recibe un `func()` sin resultado, así que cada pase escribe el suyo en una variable capturada:

{{diff ch08-s4..ch09-s1 internal/driver/pipeline.go}}

{{diff ch08-s4..ch09-s1 internal/driver/emit.go}}

{{reftest T09.08}}

{{reftest T09.11}}

Si te atascas o tu versión se desvía demasiado, copia el fichero de `ref/ch09-s1` y sigue.

Al terminar: tus tests en verde, commit y etiqueta `ch09-s1`; compárate con `ref/ch09-s1`.

### Sesión 2: elaboración a Core S0 (≈ 2 h)

#### Los tests, en rojo

1. **Escribe T09.14–T09.29.** La interfaz:

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
3. **Añade `Compile`, `Lower` y un stub.** Con `Elaborate` devolviendo `&Program{}`, fallan los 16 subtests, ninguno con panic.

{{diff ch09-s1..ch09-s2 internal/testsupport/compile.go}}

{{diff ch09-s1..ch09-s2 internal/driver/pipeline.go}}

#### Go: `debug.SetMaxStack`

`SetMaxStack` devuelve el límite anterior, y el `defer` lo restaura al acabar el test:

```go
old := debug.SetMaxStack(1 << 20)
defer debug.SetMaxStack(old)
```

#### El código

{{file ch09-s2 internal/core/elaborate.go}}

`Elaborate` necesita `preludeModule` para rellenar `UserModule`, y por eso se exporta:

{{diff ch09-s1..ch09-s2 internal/sema/resolve.go}}

{{diff ch09-s1..ch09-s2 internal/sema/suggest.go}}

- **`block` usa bucles.** Uno, hacia delante, reparte slots y elabora los `Init`. Otro, hacia atrás, construye la cadena: el `Body` de cada `Let` debe existir antes. Recursivo, desbordaría la pila de 1 MiB.
- **`expr` sí es recursivo,** porque el parser acota la profundidad de una expresión. Un árbol del parser es seguro; uno hecho a mano, no.
- **El `PrimCall` lleva el span del operador:** ahí informará el capítulo 10 de un desbordamiento.
- **`equalityPrim`** es la regla de `==` de la Teoría. El capítulo 13 quita su caso de la variable sin ligar.

{{reftest T09.15}}

{{reftest T09.21}}

Si te atascas o tu versión se desvía demasiado, copia el fichero de `ref/ch09-s2` y sigue.

Al terminar: tus tests en verde, commit y etiqueta `ch09-s2`; compárate con `ref/ch09-s2`.

### Sesión 3: Core Lint, el impresor y `ki emit core` (≈ 2 h)

#### Los tests, en rojo

1. **Escribe T09.30–T09.51 y amplía T09.09, T09.10 y la línea de uso de `cli_test.go`.** El límite de 1 MiB de la sesión 2 pasa a `withStackCap`, porque ahora limita también a `Lint`, `Print` y `Subexprs`:

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

2. **Míralos fallar.** `internal/core` e `internal/driver` no compilan, y los golden salen con 2.
3. **Añade stubs vacíos** de `Subexprs` (un iterador vacío, no `nil`), `Lint`, `Print`, `lintOrPanic` y `lint`. Fallan 20 subtests y los dos golden, sin panic. Los tres casos de aceptación ya pasan: un `Lint` vacío lo acepta todo, y por eso existen los de rechazo.

#### El código

{{file ch09-s3 internal/core/lint.go}}

- **`walk`** es el `recorre` de la Teoría; `check`, su `comprueba`, y `scope`, su `ámbito`.
- **`kids[:0]`** reutiliza el array de `appendChildren`: un solo buffer por recorrido.
- **`delete(m, k)`** quita una entrada de un mapa.
- **`Lint` recibe el `Interner`** porque su texto es el mensaje del informe, y quien lo lee no tiene los ids.
- **En los tests,** `slices.Collect` convierte un `iter.Seq` en un slice.
- **Amplía el comentario de `Var`** (`core.go`): en la Parte I, `Lint` tampoco comprueba su binder único (ejercicio 1).

{{file ch09-s3 internal/core/print.go}}

`Print` también recorre con una pila propia. Escribe los literales con `strconv.FormatInt`, el inverso de `ParseInt` (capítulo 5), y `strconv.FormatBool`.

:::more `Print` es solo para leer
Un `Let` como *callee* (la función llamada) no tiene delimitador: `print({ let g = f; g }(1))` se imprime `print(let g = f;` y, debajo, `g(1))`, que parece un `Let` cuyo cuerpo es la llamada.
:::

Ahora conecta `ki emit core`:

{{diff ch09-s2..ch09-s3 internal/driver/pipeline.go}}

{{diff ch09-s2..ch09-s3 internal/driver/emit.go}}

{{diff ch09-s2..ch09-s3 internal/driver/commands.go}}

- **`emitCore` para antes de `Lower` si hay errores:** un error del programa da su diagnóstico y código 1.
- **`Lower` no llama a `lint`.** Debe llamarlo cada comando que baja un programa.

Los dos golden:

{{file ch09-s3 tests/golden/ch09/fib_core.ki}}

{{file ch09-s3 tests/golden/ch09/blocks_core.ki}}

{{reftest T09.33}}

{{reftest T09.39}}

{{reftest T09.42}}

Si te atascas o tu versión se desvía demasiado, copia el fichero de `ref/ch09-s3` y sigue.

Al terminar: tus tests en verde, commit y etiqueta `ch09-s3`; compárate con `ref/ch09-s3`.

## Ejecútalo

Con `ki` construido (`go build -o build/bin/ki ./cmd/ki`), crea `fib.ki`:

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

Un programa con más azúcar:

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

Un error del programa no es un ICE:

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
**1. Binders únicos.** `Lint` tolera una `Var` ligada por dos binders. Detéctalo con la violación ``variable `x` is bound more than once``, y pruébalo con un test que construya Core inválido a mano.
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

Sin la comprobación, el primer caso da `nested: Lint returned [], want […]`. `nested` lee `v` tras el `Let` interior porque ahí se separa el contador de un conjunto: con un `scope` booleano, `Lint` añade un ``unbound variable `v0` `` falso.

La comprobación pone en rojo un test a propósito: su fila `a variable bound by two nested Lets` es el programa de `nested`, y ahora es una violación. Quítala, porque `nested` ya la cubre; ese test se queda en nueve casos. Cambia también los comentarios de `Lint` y de `Var` (`core.go`), que dicen que esto no se comprueba. Los demás tests del capítulo siguen en verde.
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

Al final de `name`, tras `pr.used[s] = true`, para que los sufijos no se mezclen con los nombres en uso:

```go
	if pr.ids {
		s += "#" + strconv.Itoa(int(v)) + "@" + strconv.Itoa(int(pr.p.Vars.Get(v).Slot))
	}
```

La línea de uso de `commands.go`, común a los tres destinos aunque `--ids` solo valga en `core`, pasa a `ki emit tokens|ast|core [--ids] [--diagnostics=human|short|json] FILE`. Cámbiala también en `cli_test.go`, donde aparece cuatro veces. El test, en `TestCLI`:

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

Sobre el `fib.ki` de «Ejecútalo»:

```console doctest="skip: necesita la solución del ejercicio 2"
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
**4. `guard` y los errores del runtime.** `guard` recoge también los panics del runtime. Prueba con un test que una división por cero en Go dentro de un pase acaba en un ICE con código 70. Añade a `ICE` un método `Runtime() bool` que diga si el panic lo lanzó el runtime (`runtime.Error`, una interfaz que cumplen esos valores) o el compilador.
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

Si `guard` guardara `Value: fmt.Sprint(r)`, se perdería el tipo, y el test daría `Runtime() = false for "runtime error: integer divide by zero", want true`: por eso `ICE` guarda el valor tal cual.
:::


:::exercise
**5. El puntero de `catchICE`.** Haz que `catchICE` reciba `code int` en lugar de `*int`. ¿Qué código da un ICE, y por qué?
:::

:::solution
Con `func (s *Session) catchICE(code int)` y `defer s.catchICE(code)`, todo compila, y `go vet` no dice nada. Pero el test del gancho falla:

```text
ki check with the hook in check: exit code 0, want 70
```

Es la regla de `defer` de la Teoría: sus argumentos se evalúan en la línea del `defer`. En esa línea `code` aún vale 0, y `catchICE` recibe ese 0 y escribe en su copia. Con el puntero, lo que se evalúa es la dirección del resultado con nombre, y a través de ella la diferida cambia lo que `Main` devuelve. Sin él, un ICE acaba con código 0.
:::
