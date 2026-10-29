---
chapter: 8
title: Tipos I: comprobar contra firmas, con errores legibles
part: I
hours: 9
milestone: errores de tipo con dos etiquetas y su origen
---

## Objetivo

Al terminar, `ki check` sabe qué tipo tiene cada expresión de un programa y, cuando dos no encajan, lo dice con dos etiquetas: la del sitio que está mal y la del sitio que explica por qué se esperaba otra cosa.

```console
$ cat fib.ki
fn fib(n: Int) -> Int {
  if n < 2 { n } else { fib(n - 1) + fib(n - 2) }
}

fn main() {
  print(fib(true))
}
$ build/bin/ki check fib.ki
error[E0401]: mismatched types
 --> fib.ki:6:13
  |
1 | fn fib(n: Int) -> Int {
  |        ------ the parameter is declared here
  ...
6 |   print(fib(true))
  |             ^^^^ expected `Int`, found `Bool`
```

La primaria está en el `true`; la secundaria, cinco líneas más arriba, es el origen de la expectativa. Ese es el hito del capítulo, camino de M1 (capítulo 10). Por el camino nacen E0401 a E0406, cada uno con su entrada en `docs/errors`, y una regla para todo el compilador: una equivocación, un error.

## Teoría

### Los tipos como datos: una tabla que solo crece

Para el comprobador, un tipo es un dato más: `Int`, `Bool`, `()`, una función `(Int, Bool) -> Int` o una variable que todavía no se sabe qué es. `internal/sema` los guarda como el resto del compilador guarda sus nodos: en un `base.IdVec[TypeID, Type]` (capítulo 4), y un tipo nombra a otros por id, nunca por puntero.

La tabla construye cada tipo **una vez** (*hash-consing*): pedir dos veces `(Int) -> Int` da el mismo id, así que saber si dos tipos son el mismo es comparar dos números (mientras ninguna variable ligada esté por medio; `Unify`, más abajo, resuelve antes de comparar). Para eso, `Fun` busca en un map antes de crear un nodo. La clave tendría que ser «estos parámetros, este resultado, estos efectos (en la Parte I, ninguno)», y los parámetros son un slice. En Go 1.27, `map[[]TypeID]TypeID` no compila: `invalid map key type []TypeID`, y un `funKey` con un campo `[]TypeID` tampoco (`invalid map key type funKey`): una clave tiene que admitir `==`. Así que `funKey` lleva los parámetros en un `string` con cuatro bytes por id. `encodeIDs` los escribe con `binary.LittleEndian.PutUint32`, de `encoding/binary`, que pone un `uint32` en cuatro bytes (`word[:]` es un slice sobre el array `[4]byte` entero). El ancho fijo importa: en decimal y pegados, `[1, 12]` y `[11, 2]` darían los dos `"112"`.

El otro lado de compartir es que nadie puede cambiar lo compartido. Go no tiene valores `const` compuestos: `const pair = [2]int{1, 2}` da `[2]int{…} (value of type [2]int) is not constant`, y lo mismo un slice o un struct. Tampoco hay un `&` de solo lectura. Así que la inmutabilidad es una convención que el código hace barata: los tipos son ids, la tabla solo crece (no hay `Delete`) y `Fun` guarda su propia copia de los parámetros (`slices.Clone`), por si quien llama reutiliza su slice. Lo que no puede impedir es que alguien escriba en el slice que `Node` devuelve: `tbl.Node(f).Params[0] = boolT` convierte `(Int) -> Int` en `(Bool) -> Int`, y `Fun([Int], Int)` sigue devolviendo ese id.

:::from Rust
`HashMap<FunKey, TypeId>` con `params: Vec<TypeId>` y `#[derive(Hash, PartialEq, Eq)]` compila; Go obliga a codificar. Y en Rust `const PAIR: [i32; 2] = [1, 2];` y `const SIZES: &[i32] = &[1, 2, 3];` compilan (rustc 1.97.1). Lo que en Rust garantiza el préstamo compartido `&[TypeId]`, aquí lo garantiza la disciplina: devolver ids y copiar al entrar.
:::

### Union-find sobre slices

Una variable de tipo es un nodo `TypeVariable` de la tabla con un `Var TypeVarID`, que indexa otro `IdVec`, el de los registros `TypeVar`. Un registro dice si la variable está ligada (`IsBound`) y a qué tipo (`Bound`). Es un bosque *union-find* (`Bind` une, `Resolve` busca) sin punteros: el enlace al padre es un id.

- **`Bind(v, to)`** liga: escribe el enlace. No comprueba nada; eso es trabajo de quien llama.
- **`Resolve(id)`** sigue los enlaces hasta un tipo que no es una variable ligada, en dos bucles: el primero encuentra el final; el segundo, la **compresión de caminos**, liga cada variable del camino directamente a ese final, de modo que la siguiente consulta da un solo paso. `vars.Ptr` (capítulo 4) es lo que deja escribir el registro en su sitio.
- **`Rank` y `Level`** se guardan y nadie los lee: la interfaz ya es la final, y esperan al capítulo 13.

### Lo que ve otro paquete

Los campos de `TypeTable` empiezan por minúscula, así que fuera de `sema` no existen. Desde otro paquete, `t.types` da `t.types undefined (cannot refer to unexported field types)`. Los tests que necesitan mirar `tbl.vars` (si una variable quedó ligada, a qué) viven por eso en `types_internal_test.go` y `unify_internal_test.go`, en `package sema`.

Un anticipo del capítulo 14, el *embedding*, con tipos de juguete: un struct puede llevar otro sin nombre de campo, `type Mark struct { Range; Text string }`, y si `Range` tiene `Lo`, `Hi` y un método `Len`, `m.Lo` y `m.Len()` son los de su `Range`, promovidos, mientras `m.Range` sigue nombrando el campo entero.

:::from Python
Un `_types` es una convención que cualquiera puede saltarse; en Go, la minúscula la impone el compilador. Y el embedding no es herencia: `Mark` no es un `Range`, y una función que pide un `Range` necesita `m.Range`.
:::

### Unificación de Robinson y el occurs check

Comprobar `fn double(x) -> Int { x * 2 }` es plantear ecuaciones: el `*` quiere `Int` a cada lado, así que el tipo de `x`, una variable `a`, tiene que ser `Int`. **Unificar** dos tipos (`x ~ y`) es hacerlos iguales ligando variables, y el algoritmo de Robinson lo hace mirando sus cabezas (el nodo de fuera), ya resueltas:

1. Si son el mismo id, ya está.
2. Si una es el tipo de error, también (más abajo, por qué).
3. Si una es una variable sin ligar, se liga a la otra.
4. Si las dos son funciones con el mismo número de parámetros, se unifican sus partes.
5. Si no, no hay unificación posible: E0401.

El paso 3 tiene una trampa: `a ~ (a) -> Int` no tiene solución, porque `a` tendría que contenerse a sí misma: su texto no tendría fin (`Show`, que escribe un tipo como texto, lo cortaría en `…`), y ningún tipo finito es solución. Antes de ligar, el **occurs check** (`Occurs`) mira si la variable aparece dentro del tipo; si aparece, es E0405, «cannot construct an infinite type». Y el paso 1 va antes que todo por la misma razón: `Occurs(a, a)` es verdadero, así que `a ~ a` daría un E0405 falso si llegara al paso 3.

La tabla construye cada tipo una vez, así que un tipo puede contener el mismo tipo dos veces en cada nivel: `(t, t) -> Int`, con `t` el nivel de abajo. Cuarenta niveles son cuarenta nodos y 2⁴⁰ caminos. Un recorrido que baje por cada camino tardaría horas, y un programa de unas cuarenta líneas lo construye: `p0`…`p40` son los parámetros sin anotación de una sola función, y llamar a uno, `p1(p0, p0)`, lo liga a una función de los tipos de sus argumentos (sesión 3); `p2(p1, p1)` sube otro nivel. Por eso `Occurs` y `Unify` llevan un conjunto de lo ya visitado, con valores de tipo `struct{}`, que no ocupan nada (el valor se escribe `struct{}{}`): el primero, un `map[TypeID]struct{}` de las funciones ya miradas; el segundo, uno de pares (`map[pair]struct{}`), porque la misma función puede encontrarse con dos distintas (T08.12). El map es `nil` hasta que hace falta: leerlo da el valor cero (capítulo 4). Y los dos usan su propia pila en vez de llamarse a sí mismos, porque la profundidad de un tipo la decide el programa que se compila (capítulo 6).

### Comprobación bidireccional

`infer` deduce el tipo de una expresión de lo que lleva dentro: `1` es `Int`, `n < 2` es `Bool`. `check` recibe el tipo que su sitio espera y, cuando puede, lo pasa hacia dentro: un bloque se lo da a su cola; un `if` con `else` compara su rama `else` con la `then`, y el resultado con lo esperado, sobre el `if` entero. Las dos se encuentran en `Unify`, cuando ya no hay hacia dónde empujar. La diferencia se ve en dónde cae el error. En `let x: Bool = { 1 }`, inferir el bloque y comparar después lo pondría sobre las llaves; empujar la expectativa lo pone sobre el `1`, la expresión que está mal.

Las funciones son, en este capítulo, un solo grupo **monomórfico**: primero se crean todas las firmas (un parámetro sin anotación recibe una variable nueva) y después se comprueban los cuerpos. Lo que un cuerpo o una llamada aprende de un parámetro lo ve cada uso de la función, y una llamada a una función definida más abajo encuentra su firma. La generalización, que permite usar una misma función con tipos distintos, llega en el capítulo 13.

### Procedencia y mensajes al estilo de Elm y de rustc

El listón es el del capítulo 7: una secundaria en el sitio que explica el error, que aquí es el **origen** de la expectativa: la anotación, el tipo de retorno, el parámetro, la otra rama. Cada llamada a `Unify` lleva un `Reason`: una clase (`ReasonKind`), el sitio que lo explica (`Because`) y, para un operador, su texto y su aridad. Si la unificación falla, la razón elige la secundaria del capítulo 7: «expected because of this annotation», «the parameter is declared here», «the other branch has type `Int`». Una condición de `if` no tiene ningún sitio que la explique, y lleva una nota con la regla en su lugar.

Un mensaje enseña dos tipos con los mismos nombres de variable: si cada uno empezara en `a`, `expected (a) -> Int, found (a, a) -> Int` parecería hablar de la misma variable. `showAll` escribe los dos con una sola numeración, por orden de aparición en el texto.

### El tipo de error, que unifica en silencio

Un nombre desconocido ya dio su E0301. Si el comprobador le inventara un tipo, `nope + 1` añadiría un E0401 por el mismo descuido, y en un programa grande el error de verdad quedaría enterrado. `TypeErr` (que `Show` escribe `{error}`) es el tipo de todo lo que una fase anterior no pudo entender: un `ExprError` del parser, un `ResError` del resolvedor, un nombre de tipo desconocido (que la sesión 4 informa como E0406). `Unify` lo da por igual a cualquier otro sin decir nada y sin ligar variables (una variable ligada al error taparía los errores de después), y una operación con un operando de tipo error tiene tipo error (salvo un `==` rechazado por su operando izquierdo, sesión 3, que es `Bool`).

### Una goroutine que vigila: `within`

Los tests de 40 niveles tienen un problema propio: un recorrido equivocado no falla, se cuelga, y `go test` lo corta a los diez minutos con un panic que nombra el test y vuelca las pilas, pero no la fila ni el motivo, y el binario entero muere. `within.Run`, de `internal/testsupport/within`, ejecuta el trabajo en otra goroutine y espera con un `select`. Su paquete solo importa `testing` y `time`, así que lo usan también los tests de `package sema`, que no pueden importar `testsupport`. En pequeño, con el plazo como argumento:

```go
func within(t *testing.T, d time.Duration, work func()) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		defer close(done)
		work()
	}()
	select {
	case <-done:
	case <-time.After(d):
		t.Fatalf("not finished after %v", d)
	}
}
```

`go f()` lanza `f` en una goroutine nueva (lo que `wg.Go` hacía en el capítulo 1). `make(chan struct{})` crea un canal; `close(done)` lo cierra, y recibir de un canal cerrado (`<-done`) no espera. `time.Duration` cuenta nanosegundos; `time.After(d)` devuelve un canal que recibe un valor pasado `d`. `select` espera al primero de sus casos que pueda avanzar. Un test que llama a `within(t, 100*time.Millisecond, func() { select {} })` (un `select` sin casos no avanza nunca) falla a la décima de segundo con `not finished after 100ms`; `within.Run` toma su plazo, 10 segundos, de `within.Deadline`. El `t.Fatalf` va en la goroutine del test y no dentro del `go func()`: `go vet` rechaza ese con `call to (*testing.T).Fatalf from a non-test goroutine`.

:::from Rust
Es `thread::spawn` con un `mpsc::channel` y `rx.recv_timeout(d)`. Como en Go, el hilo colgado no se puede matar desde fuera: solo se deja de esperarlo.
:::

## Tests primero

{{cards}}

Úsalas como siempre: escribe el test de la tarjeta, míralo fallar, implementa lo justo para el verde, y solo entonces despliega la referencia con su `{{reftest}}`.

Casi todos los tests viven en tres sitios. `types_internal_test.go` (`TestTypeTable`) y `unify_internal_test.go` (`TestUnify`) son `package sema`, porque leen `tbl.vars`. `check_test.go` (`TestCheck`) es `package sema_test`: pasa cada fuente por `testsupport.Analyze`, que desde la sesión 3 también comprueba tipos, y reutiliza los ayudantes de `scope_test.go` y `resolve_test.go`. Las tarjetas describen el final del capítulo: T08.17 y T08.20 cambian en la sesión 3, y T08.19 y T08.23 en la 4; en su sesión se escriben como en `git show ref/ch08-sK:book/tests/ch08.tests`.

## Implementación

### Sesión 1: la tabla de tipos (≈ 2 h)

#### Los tests, en rojo

T08.01–T08.09. La interfaz, completa:

```go
type TypeID uint32; type RowID uint32; type TypeVarID uint32
type Builtin uint8   // BuiltinInt, BuiltinBool, BuiltinUnit
type TypeKind uint8  // TypeBuiltin, TypeFun, TypeVariable, TypeErr
type Type struct { Kind TypeKind; Builtin Builtin; Params []TypeID; Ret TypeID; Effects RowID; Var TypeVarID }
type Row struct { Labels []TypeID; Tail TypeVarID; HasTail bool } // Part I: only the empty row
type VarKind uint8   // VarKindType, VarKindRow
type Origin struct { Span base.Span; Note string }
type TypeVar struct { Bound TypeID; IsBound bool; Rank uint8; Level uint32; Kind VarKind; Origin Origin }
type funKey struct { ret TypeID; effects RowID; params string }
type TypeTable struct { types base.IdVec[TypeID, Type]; vars base.IdVec[TypeVarID, TypeVar]; rows base.IdVec[RowID, Row]; funs map[funKey]TypeID; … }
func NewTypeTable() *TypeTable
func (t *TypeTable) Builtin(b Builtin) TypeID
func (t *TypeTable) Error() TypeID
func (t *TypeTable) EmptyRow() RowID
func (t *TypeTable) Fun(params []TypeID, ret TypeID, effects RowID) TypeID
func (t *TypeTable) FreshVar(origin Origin) TypeID
func (t *TypeTable) Node(id TypeID) Type
func (t *TypeTable) Resolve(id TypeID) TypeID
func (t *TypeTable) Bind(v TypeVarID, to TypeID)
func (t *TypeTable) Occurs(v TypeVarID, in TypeID) bool
func (t *TypeTable) Show(id TypeID) string
const MaxTypeText = 256 // Show cuts a longer text and ends it in `…`
// package within (internal/testsupport/within), copied from ref/ch08-s1
const Deadline = 10 * time.Second
func Run(t *testing.T, what string, work func())
```

La clase de una variable se llama `TypeVariable` y no `TypeVar`: con los dos nombres, Go diría `TypeVar redeclared in this block`.

Copia `internal/testsupport/within` de `ref/ch08-s1` (es código de test, como `testsupport`). Sobre `ch07-s4`, el test no compila: `types_internal_test.go:23:31: undefined: TypeTable`. Escribe las declaraciones, `MaxTypeText` y métodos que devuelvan el valor cero (`Resolve` devuelve su `id`, `Show` devuelve `""`), con un `NewTypeTable` que ya cree la fila de efectos vacía (`t.rows.Push(Row{})`): sin ella, `fun_types_hash_cons` lee `tbl.rows.Get(empty)` y corta el binario con `index out of range [0] with length 0`. Así fallan los nueve subtests, ninguno con panic; el primero, con `Int and Bool have the same id, 0`.

Los ayudantes de `types_internal_test.go`: `varID` da la variable de un tipo y falla si no lo es; `varOf`, su registro; `bind` liga; `funMaker` abrevia `Fun` con la fila vacía; `distinctIDs` falla por cada par de tipos con el mismo id; y `checkFunKeysTellIdsApart` crea funciones sobre ids que confundirían una clave decimal o de uno o dos bytes por id, y exige un id por función y ningún nodo de más.

#### El código

{{file ch08-s1 internal/sema/types.go}}

- **`Fun`** busca la clave antes de copiar nada: pedir otra vez una función que ya existe no añade nodo. `Fun(nil, …)` y `Fun([]TypeID{}, …)` dan la misma clave, `""`, así que son el mismo tipo. Si `encodeIDs` escribiera los ids en decimal, T08.02 fallaría con `Fun([1 0], 0) and Fun([10], 0) have the same id, 401`.
- **`Resolve`**: si el segundo bucle recolocara solo el primer eslabón, la cadena de cinco de T08.08 lo vería en las variables 1 a 3.
- **`Occurs`** mira cada función una vez (`seen`). Sin esa guarda, la fila de 40 niveles de T08.09 falla a los diez segundos con `Occurs on the types of 40 levels: not finished after 10s, when it should take well under a second`.
- **`Show`** escribe con una pila de piezas: un `id` por escribir o un texto tal cual. Una función apila su resultado, ` -> `, `)` y los parámetros del último al primero, para que salgan en el orden del texto. `varName` da `a`…`z`, `a1`… (T08.05). Y corta: si con la pieza siguiente el texto pasara de `MaxTypeText` bytes, escribe `…` y para (T08.03); y una variable solo se numera cuando su nombre se escribe, cosa que fija T08.13 en la sesión 2, con `showAll`.

{{reftest T08.08}}

El mensaje de la etiqueta: `ch08-s1: the type table`.

Si te atascas o tu versión se desvía demasiado, copia el fichero de `ref/ch08-s1` y sigue.

Al terminar: tus tests en verde, commit y etiqueta `ch08-s1`; compárate con `ref/ch08-s1`.

### Sesión 2: unificación con procedencia (≈ 2,5 h)

#### Los tests, en rojo

T08.10–T08.20. La interfaz:

```go
type ReasonKind uint8 // ReasonAnnotation, ReasonLetAnnotation, ReasonReturnType, ReasonBlockTail,
                      // ReasonArgument, ReasonIfBranches, ReasonIfCondition, ReasonOperand
type Reason struct { Kind ReasonKind; Because base.Span; Index int; Detail string; CanReturn bool }
type Unifier struct { Types *TypeTable; Sink *diag.Sink }
func (u *Unifier) Unify(expected, found TypeID, foundAt base.Span, r Reason) bool
func (t *TypeTable) showAll(ids ...TypeID) []string // one set of names for all of them
```

Sobre `ch08-s1` no compila: `unify_internal_test.go:65:52: undefined: Reason`. Copia `reason.go` entero y escribe un `Unify` que devuelva `true`: fallan nueve de los diez subtests de `TestUnify`, sin panic. `same_type_ok` ya pasa, porque decir que sí sin ligar nada es lo correcto para dos tipos iguales.

Los ayudantes de `unify_internal_test.go`: `unifyEnv`, con `newUnifyEnv`, es una tabla con los tres tipos predefinidos a mano; `fn(ret, params...)`, con el resultado primero, y `fresh` crean funciones y variables; `isUnbound` mira el registro; `unify` llama a `Unify` con un sink propio; `check` exige la respuesta y los diagnósticos campo por campo con `reflect.DeepEqual`; `annotated` es la razón de un `let` anotado, y `badAnnotated`, `check` con ella; `annotatedLabel`, `unusedHelp` y `unusedBindHelp` son los textos esperados; `wantMismatch`, `withBecause` y `wantInfinite` construyen el E0401 y el E0405 esperados; `panics`, con `defer` y `recover`, dice si una función hace panic.

#### El código

{{file ch08-s2 internal/sema/reason.go}}

{{file ch08-s2 internal/sema/unify.go}}

{{diff ch08-s1..ch08-s2 internal/sema/types.go}}

- **Resolver antes de comparar.** Sin el `if e == f`, `Int ~ Int` cae en el `default` y da «expected `Int`, found `Int`» (T08.10).
- **El orden.** Se apila el resultado y luego los parámetros del último al primero. Si los apilaras en orden, en `(p, p) -> Int` contra `(Int, Bool) -> Int` el error saldría en el primer parámetro y T08.12 diría `p resolves to Bool, want Int`.
- **Una equivocación, un diagnóstico.** La primera parte que no cuadra termina `Unify`, y la etiqueta escribe con `showAll` los dos tipos que recibió `Unify`, no la parte que falló, cortados como los corta `Show`.
- **`bind`** hace el occurs check; si falla, la variable queda sin ligar y la nota usa `showAll(vType, to)`, así que la variable que se contiene se llama `a` aunque en el tipo aparezca después: `y ~ (x, y) -> Int` dice `` `a` would have to be `(b, a) -> Int` ``.
- **La ayuda** solo sale con `ReasonBlockTail` y `()` esperado, y sin fix-it: «this value is unused: bind it with `let name = …`», más «, or return it from the function» si la razón lleva `CanReturn`. Cómo llamar al valor, y si devolverlo, lo decides tú.
- **`indefinite`** elige «a» o «an» por la primera letra del tipo: `!1` dice «`!` expects a `Bool` operand» (T08.14).

**El catálogo antes de que nadie llame a `Unify`.** E0401 y E0405 nacen en esta sesión, y con ellos sus entradas en `docs/errors`; pero `ki check` todavía no comprueba tipos, así que sus ejemplos erróneos no pueden dar su código, y `erroneous_examples_report_their_code` fallaría con `reports [], want at least one E0401`. La palabra de valla `pending=<valor>` lo resuelve: el ejemplo se ejecuta, no se le pide nada salvo no dar su código, y el test **falla** el día que lo da, con `` now reports E0401: remove `pending` ``. Es una marca que caduca. El valor dice quién hará alcanzable el código; en la referencia es `T36`, el nombre que llevaba la sesión 3 de este capítulo, y en tu repo puedes escribir `pending=ch08-s3`.

El orden que funciona: primero `TestErrorDocs/pending_word` (T08.20), que en rojo falla cinco veces, la primera porque la valla tiene `` the unknown word "pending=T36" ``; después `parseDoc` y `exampleProblems`; después los dos ficheros del catálogo; y por último `unify.go`, que sin ellos da `the source builds E0401, and docs/errors/E0401.md does not exist`.

{{diff ch08-s1..ch08-s2 tests/errors_docs_test.go}}

{{file ch08-s2 docs/errors/E0405.md}}

`E0401.md` tiene siete pares, uno por sitio que explica la expectativa (la anotación de un `let`, el tipo de retorno, la cola `()`, el argumento, la otra rama, la condición y el operando), con la misma marca y el mismo párrafo. Este cambio toca tarjetas del capítulo 7: T07.86 y T07.87 enumeran las palabras de la valla y en `ch08-s2` aprenden `pending=`; T07.87 cambia otra vez en la sesión 3, cuando la marca desaparece, y T07.94 en la 4, cuando `FuzzAnalyze` gana tres semillas del E0404 y el E0406. La tabla del capítulo 7 las muestra como eran en `ch07-s4`; las de ahora están en `git show ref/ch08-s4:book/tests/ch07.tests`.

{{reftest T08.11}}

El mensaje de la etiqueta: `ch08-s2: unification with provenance`.

Si te atascas o tu versión se desvía demasiado, copia el fichero de `ref/ch08-s2` y sigue.

Al terminar: tus tests en verde, commit y etiqueta `ch08-s2`; compárate con `ref/ch08-s2`.

### Sesión 3: el comprobador bidireccional (≈ 2,5 h)

#### Los tests, en rojo

T08.21–T08.37. La interfaz:

```go
// package sema
type TypedModule struct { ExprTypes []TypeID } // indexed by ExprID
type TypedProgram struct { Types *TypeTable; Modules []TypedModule; DefTypes base.IdVec[DefID, TypeID]; LocalTypes base.IdVec[LocalID, TypeID] }
func Check(mods []*syntax.Module, res *ResolvedProgram, in *base.Interner, sink *diag.Sink) *TypedProgram
// package driver
type Frontend struct { …; Resolved *sema.ResolvedProgram; Typed *sema.TypedProgram }
// package syntax: Expr.OpSpan of an ExprCall is the argument list, from `(` to `)`
// package testsupport (test-only code)
func Resolve(t *testing.T, source string) *Program // names only, no types
func DefType(t *testing.T, p *Program, name string) string
// package sema, export_internal_test.go (compiled only for tests)
func VarOrigin(t *TypeTable, v TypeVarID) Origin
```

Copia `testsupport` entero:

{{diff ch08-s2..ch08-s3 internal/testsupport/compile.go}}

`Analyze` comprueba tipos desde ahora, y algunos tests del capítulo 7 analizaban fuentes que no tipan (`fn f(a: Int) { a }` devuelve `Int` donde se espera `()`). Cuatro llamadas de `scope_test.go` y una de `resolve_test.go` pasan a `testsupport.Resolve`, que solo resuelve nombres.

Sobre `ch08-s2` no compila: `compile.go:83:47: p.Frontend.Typed undefined`. Añade el campo, haz que `Session.Analyze` llame a `Check` y escribe un `Check` que cree las tablas con el tipo de error en cada entrada (una por expresión de cada módulo, por `Def` y por `Local`). Fallan los dieciséis subtests de `TestCheck`, `TestUnify/mismatch_has_reason_label` y `TestParseExpr/call_arg_list_span`, sin panic.

Los ayudantes de `check_test.go`: `wantDiags` exige exactamente esos diagnósticos, ordenados por posición y comparados campo por campo; `mismatch`, `secondary`, `arityError` y `notCallable` construyen los esperados; `exprType` da el tipo de la expresión de un span; `bindEdit` aplica la ligadura que propone la ayuda, para volver a comprobar el programa; `unusedHelp` y `unusedBindHelp` repiten los textos de `unify_internal_test.go`, que este paquete no ve. `VarOrigin` le deja leer el `Origin` de una variable: lo exportado en un `_test.go` de `package sema` lo ve `sema_test`, y la API no cambia.

#### El código

{{file ch08-s3 internal/sema/check.go}}

- **`signatures`** rellena primero todas las tablas con el tipo de error y después da a cada `Def` su firma. Una expresión que el parser descartó conserva ese tipo, y no el id 0, que es `Int` (T08.21).
- **`check` e `infer`** son la teoría, y `checkAt` es inferir y unificar, con el error en la expresión entera. `stmts` comprueba un `let` anotado con `check`: con `checkAt`, `let x: Int = if true { 1 }` daría dos errores en lugar de uno, y T08.28 lo dice (`let annotation: 2 diagnostics, want 1`).
- **`checkIf`** sin `else`: primero `()` contra lo esperado, sobre el `if` entero; si falla, la rama solo se infiere; si no, se comprueba contra `()` sin `CanReturn`: la rama no es lo que devuelve la función. **`elseBranch`** devuelve el tipo de error si las ramas no cuadran, para que el `if` no dé otro error donde se usa: si devolviera el de `then`, T08.35 contaría dos.
- **`call`**: una función con otra aridad es E0402 sobre la lista de argumentos, y su nota escribe los 16 primeros parámetros (`maxNoteParams`) y `…` si hay más, porque cada llamada mala la repite; un valor que no lo es, E0403; un parámetro sin anotación, una variable, se liga a una función de los tipos de los argumentos, y por ahí llega E0405 en `fn g(x) { x(x) }`.
- **`equality`** no pasa por `Unify` para su regla propia: `==` sobre una función o `()` es un solo E0401 con nota en el operando izquierdo, y el derecho solo se infiere. Si es aún una variable sin ligar, se apunta, y `judgeEqualities` lo juzga tras todos los cuerpos: así, con `fn same(x, y) -> Bool { x == y }`, `same(f, f)` da el mismo error esté `same` delante o detrás de `main`, y `x == y` entre dos parámetros que nada liga se acepta (T08.30, T08.31).

{{diff ch08-s2..ch08-s3 internal/sema/unify.go}}

Una secundaria en otro fichero no se puede dibujar bajo la primaria: el parámetro de `print` está en `std/prelude.ki`, y `RenderHuman` hace panic con una etiqueta así. `Unify` la omite, igual que con el span cero, que es «ningún sitio».

{{diff ch08-s2..ch08-s3 internal/syntax/parser_expr.go}}

{{diff ch08-s2..ch08-s3 internal/driver/pipeline.go}}

El comprobador no tiene el texto, y «el final de la función llamada (el `Callee`) más uno» apuntaría a un blanco en `f (x)` o al `)` de `(f)(x)`: por eso el parser guarda el span de la lista (T08.37).

**El catálogo se pone al día solo.** En cuanto `Check` entra en `Analyze`, `TestErrorDocs` falla con ocho `` now reports …: remove `pending` `` (siete de E0401 y el de E0405), pide `E0402.md` y `E0403.md`, y encuentra que los ejemplos corregidos de E0201 y E0203 ya no están limpios: `fn main() { 1 + 2 }` devuelve un `Int` donde se espera `()`, y `print(1 < 2 && 2 < 3)` pasa un `Bool` a `print`, que pide un `Int`. Quita las ocho marcas y los dos párrafos que decían que `ki check` no comprobaba tipos, escribe las dos entradas nuevas y cambia esos ejemplos a `let sum = 1 + 2` y `let ok = 1 < 2 && 2 < 3`, y sus gemelos erróneos del mismo modo. `E0401.md` gana además dos pares, los de `==` entre funciones. E0405 tampoco queda pendiente: T08.36 ya lo da.

{{diff ch08-s2..ch08-s3 docs/errors/E0405.md}}

{{file ch08-s3 docs/errors/E0402.md}}

`E0403.md` sigue el mismo molde.

{{reftest T08.28}}

El mensaje de la etiqueta: `ch08-s3: the bidirectional checker`.

Si te atascas o tu versión se desvía demasiado, copia el fichero de `ref/ch08-s3` y sigue.

Al terminar: tus tests en verde, commit y etiqueta `ch08-s3`; compárate con `ref/ch08-s3`.

### Sesión 4: errores legibles, sin cascadas, y `main` (≈ 2 h)

#### Los tests, en rojo

T08.38–T08.48. La interfaz:

```go
// package syntax
type Item struct { …; ParamList, RetClause base.Span; ParamListOneLine, RetClauseOneLine bool }
func (p *parser) oneLine(from int) bool
func (p *parser) blanksBefore(hi uint32) uint32
// package testsupport (test-only code)
func ApplyFixits(src string, d diag.Diagnostic) string
```

{{file ch08-s4 internal/testsupport/fixits.go}}

Los tests de E0404 aplican la edición de cada fila y vuelven a comprobar el resultado. En `check_test.go`, `mainError` y `unknownType` construyen el E0404 y el E0406 esperados, y `onlyDiag` exige que haya uno solo. Añade también a `analyze_fuzz_test.go` las tres semillas de T07.94.

Sobre `ch08-s3` no compila: `parser_test.go:217:75: it.ParamList undefined`. Con los cuatro campos y el valor cero, fallan `TestCheck/main_params_E0404`, `TestCheck/main_return_E0404`, las dos filas de `main` de `TestCheck/fn_without_arrow_returns_unit`, diecisiete filas de `TestCheck/syntax_error_has_no_type_error` (un E0401 o un E0402 de más, como `reported [E0201 E0401], want [E0201]` o `[E0201 E0402]`; cuatro también `has the if with type "()", want {error}`) y `TestParser/signature_spans`, y `TestCheck/unknown_type_E0406` falla en cada fila (`Bol: 0 diagnostics, want 1`). T08.48 falla con `mismatch: … want at most a quarter`. `no_cascade_after_unknown_name` y los cuatro golden de `tests/errors/ch08` ya pasan: el comprobador de la sesión 3 cumple la regla del tipo de error para los nombres, y estas tarjetas la fijan. Si quitas de `binary` la propagación del tipo de error, la primera dice `reported [E0301 E0401], want [E0301]`.

{{file ch08-s4 tests/errors/ch08/if_branches_render.ki}}

Diez líneas de cabecera llevan el error a la línea 12, y el número de dos cifras ensancha el margen: es lo que el comparador de anotaciones no vería. `if_branch_mismatch.ki` tiene el mismo programa con `// ERROR(E0401) @32`.

#### El código

{{diff ch08-s3..ch08-s4 internal/syntax/ast.go}}

{{diff ch08-s3..ch08-s4 internal/syntax/parser.go}}

{{diff ch08-s3..ch08-s4 internal/sema/check.go}}

{{diff ch08-s3..ch08-s4 internal/sema/unify.go}}

- **Por qué el parser.** El comprobador no tiene el texto, y `fn main (x)` o `fn main()->Int` son válidos: no puede buscar el `(` ni el `->`. El parser guarda los dos spans, y `oneLine`, que ahora comparten `attr` e `item`, dice si un salto de línea cae dentro.
- **`RetClause`** empieza en los blancos justo antes del `->` (`blanksBefore`), no en el `)`: así un comentario entre los dos sobrevive. Si empezara en el `)`, T08.41 diría `the fix-it gives "fn main() { 1 }", want "fn main() /* c */ { 1 }"`.
- **`mainSignature`** corre entre las firmas y los cuerpos, y solo juzga el primer `main` del fichero, el mismo al que se refieren los nombres. Una firma en varias líneas lleva la ayuda sin ediciones: una edición es de una línea.
- **E0406.** `typeOf` informa un nombre de tipo desconocido una vez, donde se escribe, y desde ahí es el tipo de error: `fn f(x: Bol) {}` da ``unknown type `Bol` `` con «did you mean `Bool`?» y su edición; `Itn`, a dos ediciones de `Int`, no la lleva. Su entrada, `docs/errors/E0406.md`, tiene dos pares. Y `bodies` no da `CanReturn` a `main`, que E0404 ata a `() -> ()`, y `E0401.md` gana el par de un valor sin usar en `main`.
- **`unreadThen`.** El `()` de un `if` sin `else` cuya rama el parser no pudo leer no lo ha escrito nadie: ese `if` tiene el tipo de error. La condición y, con `else`, la otra rama se comprueban igual (T08.39).
- **`missingArg`.** `g(1,,2)` deja un nodo de error que alarga la lista: su E0402 se iría al borrar la coma. Mientras falte un argumento, `call` no juzga la cuenta (`c.missingArg` es un valor de método) ni liga un parámetro sin anotación (`x(1,,2)` sería una función de tres parámetros que nadie escribió); los demás se comprueban igual. Un operador suelto también falta: `g(1, +, 2)` es una suma sin operandos. `2 +` y `-2` cuentan, porque solo se mira el principio: mirando la derecha, T08.39 diría `reported [E0201], want [E0201 E0402]`.
- **El sink lleno.** `show` apila todos los parámetros de una función antes de cortar, y un error que se va a perder no debe pagarlo: si el sink ya perdió uno (`Truncated`, capítulo 3), el `mismatch` y el `bind` de `unify.go`, `rejectedEqualityError` y `arityError` no escriben tipos; `Unify` responde igual. T08.48 mide los bytes reservados, más estables que el reloj: `runtime.ReadMemStats(&m)` rellena un `runtime.MemStats`, y `TotalAlloc`, que solo crece, se resta entre dos lecturas.

{{file ch08-s4 docs/errors/E0404.md}}

{{diff ch08-s3..ch08-s4 docs/errors/E0402.md}}

{{reftest T08.38}}

El mensaje de la etiqueta: `ch08-s4: readable errors, no cascades, main`.

Si te atascas o tu versión se desvía demasiado, copia el fichero de `ref/ch08-s4` y sigue.

Al terminar: tus tests en verde, commit y etiqueta `ch08-s4`; compárate con `ref/ch08-s4`.

## Ejecútalo

Con `ki` construido (`go build -o build/bin/ki ./cmd/ki`), el `fib.ki` del objetivo da exactamente lo de arriba y sale con 1. Un fichero con cuatro equivocaciones:

```console
$ cat bad.ki
fn fib(n: Int) -> Int {
  if n < 2 { n } else { fib(n - 1) + fib(n - 2) }
}

fn main(args: Int) {
  let big = fib(20) > 100
  print(if big { 1 } else { false })
  let total: Bool = fib(10) + nope
  let pair = fib(1, 2)
  print(pair)
}
$ build/bin/ki check bad.ki
error[E0404]: `main` must be `fn main()`
 --> bad.ki:5:4
  |
5 | fn main(args: Int) {
  |    ^^^^ declared here with 1 parameter(s)
  = note: it takes no parameters and returns `()`
  = help: remove the parameters
  |
5 - fn main(args: Int) {
5 + fn main() {

error[E0401]: mismatched types
 --> bad.ki:7:29
  |
7 |   print(if big { 1 } else { false })
  |                             ^^^^^ expected `Int`, found `Bool`
  |                  - the other branch has type `Int`

error[E0301]: unknown name `nope`
 --> bad.ki:8:31
  |
8 |   let total: Bool = fib(10) + nope
  |                               ^^^^ not found in this scope

error[E0402]: this function takes 1 argument(s) but 2 were supplied
 --> bad.ki:9:17
  |
1 | fn fib(n: Int) -> Int {
  |    --- `fib` is declared here
  ...
9 |   let pair = fib(1, 2)
  |                 ^^^^^^ supplied here
  = note: `fib` takes `(Int)`
$ echo $?
1
```

La línea 8 tiene dos problemas en apariencia (`nope` y un `Int` donde se espera `Bool`) y un solo error: la suma tiene tipo error y la anotación no se queja. Un tipo infinito, y el formato corto:

```console
$ cat inf.ki
fn twice(f) -> Int { f(f) }

fn main() {
  print(twice(1))
}
$ build/bin/ki check inf.ki
error[E0405]: cannot construct an infinite type
 --> inf.ki:1:22
  |
1 | fn twice(f) -> Int { f(f) }
  |                      ^ this type would contain itself
  = note: `a` would have to be `(a) -> b`, which has no finite size
$ build/bin/ki check --diagnostics=short bad.ki
bad.ki:5:4: error[E0404]: `main` must be `fn main()`
bad.ki:7:29: error[E0401]: mismatched types
bad.ki:8:31: error[E0301]: unknown name `nope`
bad.ki:9:17: error[E0402]: this function takes 1 argument(s) but 2 were supplied
```

## Ejercicios

:::exercise
**1. Un `if` sin `else` usado como valor.** Haz que `let x: Int = if true { 1 }` explique por qué el `if` es `()`, con una nota «this `if` has no `else`, so it is `()`», con test y golden.
:::

:::solution
`Unify` informa directamente al sink, así que `checkIf` no puede añadir nada después. Dale un sink propio y reenvía lo que reciba, con la nota:

```go
	if !e.HasElse {
		// The E0401 goes to a sink of its own first, so that it can say why
		// the `if` is `()` before it is reported.
		var own diag.Sink
		u := Unifier{Types: c.tp.Types, Sink: &own}
		if !u.Unify(expected, c.unitT, e.Span, r) {
			for _, d := range own.All() {
				c.sink.Report(d.WithNote("this `if` has no `else`, so it is `()`"))
			}
			c.infer(e.Then)
			return c.unitT, false
		}
```

Un `diag.Sink` vacío ya sirve. Declara en `check_test.go` una constante `noElse` con el texto de la nota y añade `.WithNote(noElse)` al E0401 esperado de cuatro filas: en `if_without_else_is_unit`, `let annotation`, `tail of an Int body` y el primero de `mistake inside the branch`; en `if_branches_must_agree`, `else if without else, not the type of the other branch`. El golden, `tests/errors/ch08/if_without_else_value.ki`, lleva la tarjeta nueva T08.49 de tu `ch08.tests`:

```text
// test: T08.49
// run: check
// expect-exit: 1
// expect-stderr:
// | error[E0401]: mismatched types
// |   --> tests/errors/ch08/if_without_else_value.ki:13:16
// |    |
// | 13 |   let x: Int = if true { 1 }
// |    |                ^^^^^^^^^^^^^ expected `Int`, found `()`
// |    |          --- expected because of this annotation
// |    = note: this `if` has no `else`, so it is `()`
fn main() {
  let x: Int = if true { 1 }
  print(x)
}
```

Escríbelo con las tres primeras líneas y `build/bin/ki test --update tests/errors/ch08/if_without_else_value.ki` dos veces, como en el capítulo 7: la primera graba el bloque y empuja el programa hacia abajo, la segunda la línea 13. Con la solución, `go test ./...` y `build/bin/ki test --root . tests` quedan en verde.
:::

:::exercise
**2. `a`, `b`, `c`, estables dentro de un mismo mensaje.** Escribe un test sobre un programa entero, y no sobre `Unify`, de que un mensaje con dos tipos llama a sus variables `a`, `b`, `c` por orden de aparición, sea cual sea el orden en que se crearon, y comprueba qué implementaciones mata.
:::

:::solution
En `TestCheck`:

```go
	t.Run("names_stable_in_one_message", func(t *testing.T) {
		// The same message whichever function was checked first: the names
		// follow the text of the label, not the order of the variables.
		for _, src := range []string{
			"fn one(x) -> Int { 1 }\nfn two(x, y) -> Int { 1 }\nfn main() { let f = if true { one } else { two } }",
			"fn two(x, y) -> Int { 1 }\nfn one(x) -> Int { 1 }\nfn main() { let f = if true { one } else { two } }",
		} {
			p := testsupport.Analyze(t, src)
			wantDiags(t, src, p, secondary(
				mismatch(spanIn(t, p, src, "else { two", "two"), "expected `(a) -> Int`, found `(b, c) -> Int`"),
				spanIn(t, p, src, "{ one }", "one"), "the other branch has type `(a) -> Int`"))
		}
	})
```

Pasa sobre `ch08-s4`. Si el `mismatch` de `unify.go` llamara a `Show` dos veces en lugar de a `showAll`, el primer programa daría `found (a, b) -> Int`; si `show`, que usan `Show` y `showAll`, nombrara las variables por su id, el segundo daría `expected (c) -> Int, found (a, b) -> Int`, porque las variables de `two` se crean antes.
:::

:::exercise
**3. `Unify` es simétrico cuando tiene éxito.** Escribe un test de que `Unify(x, y)` y `Unify(y, x)` aciertan o fallan juntos y, si aciertan, dejan los mismos tipos.
:::

:::solution
En `TestUnify`, con una tabla nueva para cada orden, para que los dos partan de tipos sin tocar:

```go
	t.Run("symmetric_on_success", func(t *testing.T) {
		type shape struct {
			name  string
			build func(e *unifyEnv, a, b TypeID) TypeID
		}
		shapes := []shape{
			{"Int", func(e *unifyEnv, a, b TypeID) TypeID { return e.intT }},
			{"Bool", func(e *unifyEnv, a, b TypeID) TypeID { return e.boolT }},
			{"()", func(e *unifyEnv, a, b TypeID) TypeID { return e.unitT }},
			{"{error}", func(e *unifyEnv, a, b TypeID) TypeID { return e.tbl.Error() }},
			{"a", func(e *unifyEnv, a, b TypeID) TypeID { return a }},
			{"(a) -> Int", func(e *unifyEnv, a, b TypeID) TypeID { return e.fn(e.intT, a) }},
			{"(Int) -> b", func(e *unifyEnv, a, b TypeID) TypeID { return e.fn(b, e.intT) }},
			{"(a, a) -> b", func(e *unifyEnv, a, b TypeID) TypeID { return e.fn(b, a, a) }},
			{"(Bool, b) -> Bool", func(e *unifyEnv, a, b TypeID) TypeID { return e.fn(e.boolT, e.boolT, b) }},
			{"() -> a", func(e *unifyEnv, a, b TypeID) TypeID { return e.fn(a) }},
			{"((a) -> Int) -> b", func(e *unifyEnv, a, b TypeID) TypeID { return e.fn(b, e.fn(e.intT, a)) }},
		}
		for _, x := range shapes {
			for _, y := range shapes {
				var ok [2]bool
				var shown [2][]string
				for i := range 2 {
					e := newUnifyEnv()
					tx := x.build(e, e.fresh(), e.fresh()) // x has two variables
					ty := y.build(e, e.fresh(), e.fresh()) // and y two others
					if i == 0 {
						ok[i], _ = e.unify(tx, ty, annotated)
					} else {
						ok[i], _ = e.unify(ty, tx, annotated)
					}
					shown[i] = e.tbl.showAll(tx, ty)
				}
				row := x.name + " ~ " + y.name
				switch {
				case ok[0] != ok[1]:
					t.Errorf("%s: Unify gave %v one way and %v the other", row, ok[0], ok[1])
				case ok[0] && !slices.Equal(shown[0], shown[1]):
					t.Errorf("%s: the types are %q one way and %q the other", row, shown[0], shown[1])
				case ok[0] && shown[0][0] != shown[0][1] && x.name != "{error}" && y.name != "{error}":
					t.Errorf("%s: after Unify the two sides are %q, want one type", row, shown[0])
				}
			}
		}
	})
```

`showAll` da a los dos lados una sola numeración, así que la comparación no depende de qué variable se ligó a cuál. El tipo de error unifica sin hacer iguales los dos lados, y por eso la última condición lo exceptúa. Pasa sobre `ch08-s4`. Si quitas el caso `fn.Kind == TypeVariable` de `Unify`, falla con `Int ~ a: Unify gave false one way and true the other`; si el tipo de error solo se mira a la derecha, con `Int ~ {error}: Unify gave true one way and false the other`.
:::

:::exercise
**4. Lo que vale el hash-consing.** Mide `Fun` con y sin hash-consing sobre 10⁵ firmas.
:::

:::solution
En `types_internal_test.go`, «sin hash-consing» es un `Fun` que siempre añade un nodo:

```go
// newBenchTable returns a table with the three builtin types and seven
// variables, and those ten types. Every call gives the same ids.
func newBenchTable() (*TypeTable, []TypeID) {
	tbl := NewTypeTable()
	ts := []TypeID{tbl.Builtin(BuiltinInt), tbl.Builtin(BuiltinBool), tbl.Builtin(BuiltinUnit)}
	for range 7 {
		ts = append(ts, tbl.FreshVar(Origin{}))
	}
	return tbl, ts
}

// funNoCons is Fun without the map: every call pushes a node.
func (t *TypeTable) funNoCons(params []TypeID, ret TypeID, effects RowID) TypeID {
	return t.types.Push(Type{Kind: TypeFun, Params: slices.Clone(params), Ret: ret, Effects: effects})
}

func BenchmarkFun(b *testing.B) {
	// 10^5 signatures (two parameters and a result) of which 1000 are
	// different, as a program asks for the same few types again and again.
	const n, distinct = 100_000, 1000
	_, ts := newBenchTable()
	sigs := make([][3]TypeID, n)
	for i := range sigs {
		k := i % distinct
		sigs[i] = [3]TypeID{ts[k%10], ts[k/10%10], ts[k/100%10]}
	}
	for _, consed := range []bool{true, false} {
		name := "no_hash_cons"
		if consed {
			name = "hash_consed"
		}
		b.Run(name, func(b *testing.B) {
			var nodes int
			for b.Loop() {
				tbl, _ := newBenchTable()
				for _, s := range sigs {
					if consed {
						tbl.Fun(s[:2], s[2], tbl.EmptyRow())
					} else {
						tbl.funNoCons(s[:2], s[2], tbl.EmptyRow())
					}
				}
				nodes = tbl.types.Len()
			}
			b.ReportMetric(float64(nodes), "nodes")
		})
	}
}
```

`b.ReportMetric` añade una columna a la salida. Con `go test ./internal/sema -run '^$' -bench BenchmarkFun -benchmem`, en esta máquina: con hash-consing, unos 4,3 ms, 1,09 MB y 1 011 nodos por cada 10⁵ llamadas; sin él, unos 8 ms, 28,6 MB y 100 011 nodos. Las asignaciones casi empatan (101 041 frente a 100 038): con hash-consing, casi todas son la clave, el `string` que `encodeIDs` construye en cada llamada, también cuando la función ya existe.
:::

## Preguntas de repaso

:::review
¿Valdría como `params` de `funKey` escribir los ids en decimal separados por comas (`"1,12"`)? ¿Por qué cuatro bytes por id?
:::

:::answer
Valdría: con la coma, `[1, 12]` y `[11, 2]` dan `"1,12"` y `"11,2"`, y la lista se recupera partiendo por las comas. Pero hay que convertir con `strconv`, y desde los ids de cuatro cifras la clave es más larga. Cuatro bytes fijos no necesitan separador: el ancho dice dónde acaba cada id.
:::

:::review
¿Por qué `elseBranch` devuelve el tipo de error cuando las ramas no cuadran, en vez del tipo de la rama `then`?
:::

:::answer
Porque el `if` no tiene ningún tipo válido: elegir el de `then` sería apostar por una de las dos ramas, y si la buena era la otra, cada uso del `if` daría un segundo error por la misma equivocación.
:::

:::review
`let x: Int = if true { 1 }` da un solo error, sobre el `if` entero, y nada sobre el `1`. ¿Por qué no sobre el `1`, si es el valor que sobra?
:::

:::answer
Porque la equivocación es el `else` que falta: con él, el `1` estaría bien. Por eso `checkIf` compara primero `()` con lo esperado y, si falla, solo infiere la rama, para no perder los errores que tenga por dentro. Si lo esperado es `()`, la comparación cuadra y entonces sí sobra el `1`.
:::

:::review
¿Por qué `Occurs` necesita un conjunto de visitados, si un tipo de la Parte I no puede ser cíclico?
:::

:::answer
Porque no hace falta un ciclo para que un recorrido en árbol explote: basta compartir. Con hash-consing, `(t, t) -> Int` lleva el mismo nodo dos veces, y cuarenta niveles así son cuarenta nodos y 2⁴⁰ caminos. Un programa válido de unas cuarenta líneas lo construye, y el occurs check corre en cada ligadura.
:::

:::review
¿Por qué la secundaria de un argumento de `print(true)` desaparece, si el parámetro existe y tiene un sitio?
:::

:::answer
Porque ese sitio está en `std/prelude.ki`, otro fichero, y el renderizador dibuja las secundarias bajo el fragmento de la primaria: `RenderHuman` haría panic.
:::

:::review
¿Por qué la marca `pending=` hace fallar el test cuando el ejemplo ya da su código, en vez de limitarse a no ejecutarlo?
:::

:::answer
Porque una marca que no molesta se queda para siempre: el ejemplo seguiría sin comprobarse aunque el código ya existiera. Al fallar en cuanto el comprobador funciona, la sesión que lo hace alcanzable tiene que quitarla, y el ejemplo pasa a la regla normal.
:::

## Resumen

- Un tipo es un id en una tabla que solo crece; el hash-consing hace que la igualdad de tipos, resueltas las variables, sea comparar números.
- Un slice no puede ser clave de un map; `funKey` lleva los parámetros en un `string` de cuatro bytes por id.
- Go no tiene `const` compuestos: la inmutabilidad es una convención (ids, copia al entrar, nadie escribe en `Node(id).Params`).
- Las variables son un union-find sobre `IdVec`, con compresión de caminos en `Resolve`; `Rank` y `Level` esperan al capítulo 13.
- Unificar es Robinson: resolver, comparar, ligar con occurs check y bajar por las partes, con pila propia y un conjunto de visitados.
- La comprobación bidireccional empuja la expectativa hasta la cola de un bloque, y el error cae en la expresión que está mal.
- Cada `Unify` lleva un `Reason`, y la razón elige la secundaria, la nota o la ayuda: el origen del error.
- El tipo de error unifica en silencio y se propaga: una equivocación, un error.
- `pending=` deja nacer una entrada del catálogo antes de que su código sea alcanzable, y obliga a quitarla en cuanto sobra.

## Go en el trabajo

**Los mensajes, revisados como producto.** La *rustc-dev-guide* describe los UI tests: «UI tests store the expected output from the compiler in `.stderr` and `.stdout` snapshots next to the test». Además, «UI tests require that errors are also annotated within the source», porque los `.stderr` se generan solos (con `--bless`, y después se leen a mano) y las anotaciones «ensure that no additional unexpected errors are generated». Es la pareja de Ki: `if_branches_render.ki` guarda el texto entero, que `ki test --update` reescribe, y su gemelo `if_branch_mismatch.ki` fija el código y la posición de todos los errores del fichero. Go hace algo parecido con menos texto: `src/go/types/check_test.go` lee comentarios `/* ERROR "pattern" */` tras el token del error y exige que el patrón sea una subcadena del mensaje en esa posición.

**Claves compuestas.** `go/types` también hace hash-consing: su `Context` reutiliza instancias genéricas idénticas, sin garantizarlo siempre, con un `map[string][]ctxtEntry` cuya clave escribe `instanceHash`, porque una lista de argumentos de tipo no puede ser clave.

Y la forma de la clave decide qué código corre en cada búsqueda. `mapfast`, en `src/cmd/compile/internal/walk/walk.go`, elige caminos rápidos del runtime para claves de 4 u 8 bytes que se comparan como memoria y para strings; cualquier otra clave va por el camino general, `mapaccess2`, que llama a las funciones de hash e igualdad del tipo. En esta máquina, con 1 000 claves ya construidas, buscar un struct de dos `uint32` y un `string`, la forma de `funKey`, costó unos 14,3 ns, y el mismo contenido en un solo `string`, unos 8,7. `funKey` se queda como struct por legibilidad: su coste está en construir la clave (ejercicio 4).
