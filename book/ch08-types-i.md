---
chapter: 8
title: Tipos I: comprobar contra firmas, con errores legibles
part: I
hours: 9
milestone: errores de tipo con dos etiquetas y su origen
---

## Objetivo

Al terminar, `ki check` sabe qué tipo tiene cada expresión y, cuando dos no encajan, lo dice con dos etiquetas: el sitio que está mal y el que explica por qué se esperaba otra cosa.

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

La secundaria es el origen de la expectativa: ese es el hito, camino de M1 (capítulo 10). Nacen E0401 a E0406, con su entrada en `docs/errors`, y una regla para todo el compilador: una equivocación, un error.

## Teoría

### Los tipos como datos: una tabla que solo crece

Un tipo es un dato más: `Int`, `Bool`, `()`, una función `(Int, Bool) -> Int` o una variable por resolver. `internal/sema` los guarda en un `base.IdVec[TypeID, Type]` (capítulo 4), y un tipo nombra a otros por id, nunca por puntero.

**Hash-consing.** La tabla construye cada tipo una vez, así que comparar dos tipos es comparar dos ids, mientras ninguna variable ligada esté por medio (`Unify`, más abajo, resuelve antes).

**La clave de `Fun`.** Para eso, `Fun` busca en un map antes de crear un nodo. La clave son los parámetros, el resultado y los efectos (en la Parte I, ninguno).

Una clave tiene que admitir `==`: un slice no sirve, ni un struct con un slice. Así que `funKey` lleva los parámetros en un `string`, cuatro bytes por id (`encodeIDs`). El ancho fijo importa: en decimal y pegados, `[1, 12]` y `[11, 2]` darían los dos `"112"`.

**Inmutabilidad por convención.** Nadie debe cambiar lo compartido, pero Go no tiene valores `const` compuestos ni un `&` de solo lectura. Así que los tipos son ids, la tabla solo crece (no hay `Delete`) y `Fun` guarda su propia copia de los parámetros (`slices.Clone`), por si quien llama reutiliza su slice.

:::from Rust
Una clave con `Vec<TypeId>` compila; Go obliga a codificar. Lo que garantiza el préstamo compartido `&[TypeId]`, aquí lo garantiza la disciplina: devolver ids y copiar al entrar.
:::

:::more Los mensajes del compilador, `encoding/binary` y lo que la convención no impide
Un campo en minúscula de `TypeTable` (`t.types`), desde otro paquete, da `t.types undefined (cannot refer to unexported field types)`. En Go 1.27, `map[[]TypeID]TypeID` da `invalid map key type []TypeID`, y un `funKey` con un campo `[]TypeID`, `invalid map key type funKey`. `const pair = [2]int{1, 2}` da `[2]int{…} (value of type [2]int) is not constant`, y lo mismo un slice o un struct. En Rust, con `#[derive(Hash, PartialEq, Eq)]`, la clave compila, y también `const PAIR: [i32; 2] = [1, 2];` y `const SIZES: &[i32] = &[1, 2, 3];` (rustc 1.97.1).

`encodeIDs` usa `binary.LittleEndian.PutUint32`, de `encoding/binary`, que pone un `uint32` en cuatro bytes; `word[:]` es un slice sobre el array `[4]byte` entero.

La convención no impide que alguien escriba en el slice que `Node` devuelve: `tbl.Node(f).Params[0] = boolT` convierte `(Int) -> Int` en `(Bool) -> Int`, y `Fun([Int], Int)` sigue devolviendo ese id.
:::

### Union-find sobre slices

Una variable de tipo es un nodo `TypeVariable` cuyo `Var` indexa otro `IdVec` de registros `TypeVar`: si está ligada (`IsBound`) y a qué (`Bound`). Es un bosque *union-find* sin punteros: el enlace al padre es un id.

- **`Bind(v, to)`** une: escribe el enlace y no comprueba nada.
- **`Resolve(id)`** busca: sigue los enlaces hasta un tipo que no es una variable ligada. Un segundo bucle, la **compresión de caminos**, liga cada variable del camino a ese final, y la siguiente consulta da un solo paso. `vars.Ptr` (capítulo 4) deja escribir el registro en su sitio.

### Lo que ve otro paquete

Los campos de `TypeTable` empiezan por minúscula, así que fuera de `sema` no existen.

Un anticipo del capítulo 14, el *embedding*: un struct puede llevar otro sin nombre de campo, `type Mark struct { Range; Text string }`. Si `Range` tiene `Lo` y un método `Len`, `m.Lo` y `m.Len()` son de `Range`, promovidos, y `m.Range` nombra el campo entero.

:::from Python
Un `_types` se puede saltar; la minúscula la impone el compilador. El embedding no es herencia: quien pide un `Range` necesita `m.Range`.
:::

### Unificación de Robinson y el occurs check

Comprobar es plantear ecuaciones: en `fn double(x) -> Int { x * 2 }`, el `*` quiere `Int` a cada lado, así que el tipo de `x`, una variable, tiene que ser `Int`. **Unificar** dos tipos (`x ~ y`) es hacerlos iguales ligando variables. El algoritmo de Robinson mira sus cabezas (el nodo de fuera), ya resueltas:

1. Si son el mismo id, ya está.
2. Si una es el tipo de error, también (más abajo).
3. Si una es una variable sin ligar, se liga a la otra.
4. Si las dos son funciones con el mismo número de parámetros, se unifican sus partes.
5. Si no, no hay unificación posible: E0401.

**El occurs check.** `a ~ (a) -> Int` no tiene solución: `a` tendría que contenerse a sí misma, y ningún tipo finito es solución. Antes de ligar, `Occurs` mira si la variable aparece dentro del tipo; si aparece, es E0405. El paso 1 va primero por lo mismo: `Occurs(a, a)` es verdadero, y `a ~ a` daría un E0405 falso.

**Compartir también explota.** Como cada tipo existe una vez, `(t, t) -> Int`, con `t` el nivel de abajo, lleva el mismo nodo dos veces. Cuarenta niveles son cuarenta nodos y 2⁴⁰ caminos, y unas cuarenta líneas lo construyen. Recorrer cada camino tardaría horas.

Por eso `Occurs` y `Unify` llevan un conjunto de lo ya visitado, un map con valores `struct{}`, que no ocupan nada (el valor se escribe `struct{}{}`). Y usan pila propia, sin recursión, porque la profundidad de un tipo la decide el programa que se compila (capítulo 6).

:::more El programa de cuarenta líneas, y qué guarda cada conjunto
`p0`…`p40` son los parámetros sin anotación de una sola función. Llamar a uno, `p1(p0, p0)`, lo liga a una función de los tipos de sus argumentos (sesión 3); `p2(p1, p1)` sube otro nivel.

`Occurs` guarda un `map[TypeID]struct{}` de las funciones ya miradas; `Unify`, uno de pares (`map[pair]struct{}`), porque la misma función puede encontrarse con dos distintas (T08.12). El map es `nil` hasta que hace falta: leerlo da el valor cero (capítulo 4).
:::

### Comprobación bidireccional

`infer` deduce el tipo de una expresión de lo que lleva dentro: `n < 2` es `Bool`.

`check` recibe el tipo que su sitio espera y, cuando puede, lo pasa hacia dentro. Un bloque se lo da a su cola. Un `if` con `else` compara su rama `else` con la `then`, y el resultado con lo esperado, sobre el `if` entero. Las dos se encuentran en `Unify`, cuando ya no hay hacia dónde empujar.

En `let x: Bool = { 1 }`, inferir y comparar pondría el error sobre las llaves; empujar la expectativa, sobre el `1`, la expresión que está mal.

Las funciones son aquí un solo grupo **monomórfico**: primero todas las firmas (un parámetro sin anotación recibe una variable nueva), después los cuerpos.

Lo que un cuerpo o una llamada aprende de un parámetro lo ve cada uso de la función, y una llamada a una función definida más abajo encuentra su firma. La generalización (una función con tipos distintos) llega en el capítulo 13.

### Procedencia y mensajes al estilo de Elm y de rustc

El listón es el del capítulo 7: una secundaria en el sitio que explica el error, aquí el **origen** de la expectativa: la anotación, el tipo de retorno, el parámetro, la otra rama. Cada `Unify` lleva un `Reason`: una clase (`ReasonKind`), ese sitio (`Because`) y, para un operador, su texto y su aridad.

Si falla, la razón elige la secundaria: «expected because of this annotation». Una condición de `if` no tiene ningún sitio que la explique, y lleva una nota con la regla. Una cola de función con valor donde se espera `()` lleva además una ayuda: ligarlo con `let` o, si devolverlo lo arregla (`CanReturn`), devolverlo.

Un mensaje enseña dos tipos: si cada uno empezara en `a`, `expected (a) -> Int, found (a, a) -> Int` parecería hablar de la misma variable. `showAll` los escribe con una sola numeración, por orden de aparición.

### El tipo de error, que unifica en silencio

Si el comprobador inventara un tipo para un nombre desconocido, que ya dio su E0301, `nope + 1` añadiría un E0401 por el mismo descuido, y el error de verdad quedaría enterrado.

`TypeErr` (`Show`, que escribe un tipo, lo escribe `{error}`) es el tipo de lo que una fase anterior no entendió: un `ExprError`, un `ResError`, un nombre de tipo desconocido. `Unify` lo da por igual a cualquier otro, en silencio y sin ligar variables: una variable ligada al error taparía los errores de después.

Una operación con un operando de tipo error tiene tipo error (salvo un `==` rechazado por su operando izquierdo, sesión 3: `Bool`).

### Una goroutine que vigila: `within`

Un recorrido equivocado de 40 niveles no falla, se cuelga: `go test` lo corta a los diez minutos sin decir la fila ni el motivo, y el binario entero muere. `within.Run` ejecuta el trabajo en otra goroutine y espera con un `select`. En pequeño:

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

- `go f()` lanza `f` en una goroutine nueva (como `wg.Go`, capítulo 1).
- `make(chan struct{})` crea un canal; `close(done)` lo cierra, y recibir de un canal cerrado (`<-done`) no espera.
- `time.Duration` cuenta nanosegundos; `time.After(d)` devuelve un canal que recibe un valor pasado `d`.
- `select` espera al primero de sus casos que pueda avanzar.
- `t.Fatalf` va en la goroutine del test: dentro del `go func()`, `go vet` lo rechaza.

:::from Rust
Es `thread::spawn` con un `mpsc::channel` y `rx.recv_timeout(d)`. Como en Go, el hilo colgado no se puede matar desde fuera: solo se deja de esperarlo.
:::

## Tests primero

{{cards}}

Escribe el test de la tarjeta, míralo fallar, implementa lo justo para el verde, y solo entonces despliega la referencia con su `{{reftest}}`.

Casi todos van en `types_internal_test.go` (`TestTypeTable`) y `unify_internal_test.go` (`TestUnify`), en `package sema` porque leen `tbl.vars`, y en `check_test.go` (`TestCheck`), en `package sema_test`.

Las tarjetas describen el final del capítulo: T08.17 y T08.20 cambian en la sesión 3, y T08.19 y T08.23 en la 4; en su sesión se escriben como en `git show ref/ch08-sK:book/tests/ch08.tests`.

:::more `within` y los ayudantes de test, sesión por sesión
Al cortar un test colgado, `go test` da un panic que nombra el test y vuelca las pilas, pero no la fila ni el motivo.

**`within`.** Un test que llama a `within(t, 100*time.Millisecond, func() { select {} })` (un `select` sin casos no avanza nunca) falla a la décima de segundo con `not finished after 100ms`; `within.Run`, de `internal/testsupport/within`, toma su plazo, 10 segundos, de `within.Deadline`. Su paquete solo importa `testing` y `time`, así que lo usan también los tests de `package sema`, que no pueden importar `testsupport`. El mensaje de `go vet` es `call to (*testing.T).Fatalf from a non-test goroutine`.

- **Sesión 1, `types_internal_test.go`.** `varID` da la variable de un tipo y falla si no lo es; `varOf`, su registro; `bind` liga; `funMaker` abrevia `Fun` con la fila vacía; `distinctIDs` falla por cada par de tipos con el mismo id; y `checkFunKeysTellIdsApart` crea funciones sobre ids que confundirían una clave decimal o de uno o dos bytes por id, y exige un id por función y ningún nodo de más.
- **Sesión 2, `unify_internal_test.go`.** `unifyEnv`, con `newUnifyEnv`, es una tabla con los tres tipos predefinidos a mano; `fn(ret, params...)`, con el resultado primero, y `fresh` crean funciones y variables; `isUnbound` mira el registro; `unify` llama a `Unify` con un sink propio; `check` exige la respuesta y los diagnósticos campo por campo con `reflect.DeepEqual`; `annotated` es la razón de un `let` anotado, y `badAnnotated`, `check` con ella; `annotatedLabel`, `unusedHelp` y `unusedBindHelp` son los textos esperados; `wantMismatch`, `withBecause` y `wantInfinite` construyen el E0401 y el E0405 esperados; `panics`, con `defer` y `recover`, dice si una función hace panic.
- **Sesión 3, `check_test.go`.** Pasa cada fuente por `testsupport.Analyze` y reutiliza los ayudantes de `scope_test.go` y `resolve_test.go`. `wantDiags` exige exactamente esos diagnósticos, ordenados por posición y comparados campo por campo; `mismatch`, `secondary`, `arityError` y `notCallable` construyen los esperados; `exprType` da el tipo de la expresión de un span; `bindEdit` aplica la ligadura que propone la ayuda, para volver a comprobar el programa; `unusedHelp` y `unusedBindHelp` repiten los textos de `unify_internal_test.go`, que este paquete no ve.
- **Sesión 3, `export_internal_test.go`.** `VarOrigin` deja a `check_test.go` leer el `Origin` de una variable: lo exportado en un `_test.go` de `package sema` lo ve `sema_test`, y la API no cambia.
- **Sesión 4, `check_test.go`.** `mainError` y `unknownType` construyen el E0404 y el E0406 esperados, y `onlyDiag` exige que haya uno solo. Los tests de E0404 aplican la edición de cada fila con `ApplyFixits` y vuelven a comprobar el resultado.
:::

## Implementación

### Sesión 1: la tabla de tipos (≈ 2 h)

#### Los tests, en rojo

1. **Escribe T08.01–T08.09.** La interfaz, completa:

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

2. **Copia `internal/testsupport/within`** de `ref/ch08-s1` (es código de test, como `testsupport`).
3. **Míralo fallar.** Sobre `ch07-s4`, el test no compila: `types_internal_test.go:23:31: undefined: TypeTable`.
4. **Escribe las declaraciones, `MaxTypeText` y métodos que devuelvan el valor cero** (`Resolve` devuelve su `id`, `Show` devuelve `""`). `NewTypeTable` crea la fila de efectos vacía (`t.rows.Push(Row{})`; en la Parte I, ninguna función tiene otra): sin ella, `fun_types_hash_cons` corta el binario. Fallan los nueve subtests, ninguno con panic.

#### El código

{{file ch08-s1 internal/sema/types.go}}

**`Show`** escribe con una pila de piezas: un `id` por escribir o un texto tal cual. Una función apila sus partes de la última a la primera, para que salgan en orden.

:::more Sesión 1, los porqués: el nombre `TypeVariable`, la fila vacía, `Fun`, `Resolve`, `Show`, `Rank` y `Level`
- La clase de una variable se llama `TypeVariable` y no `TypeVar`: con los dos nombres, Go diría `TypeVar redeclared in this block`.
- Sin la fila vacía, el panic es `index out of range [0] with length 0`. Con ella, el primer subtest falla con `Int and Bool have the same id, 0`.
- **`Fun`** busca la clave antes de copiar nada: pedir otra vez una función que ya existe no añade nodo. `Fun(nil, …)` y `Fun([]TypeID{}, …)` dan la misma clave, `""`, así que son el mismo tipo.
- En `Resolve`, si el segundo bucle recolocara solo el primer eslabón, la cadena de cinco de T08.08 lo vería en las variables 1 a 3.
- En `Show`, una función apila su resultado, ` -> `, `)` y los parámetros; y si con la pieza siguiente el texto pasara de `MaxTypeText` bytes, escribe `…` y para (T08.03).
- `varName` da `a`…`z`, `a1`… (T08.05), y una variable solo se numera cuando su nombre se escribe (T08.13, con `showAll`).
- **`Rank` y `Level`** se guardan y nadie los lee: la interfaz ya es la final, y esperan al capítulo 13.
:::

{{reftest T08.08}}

Si te atascas o tu versión se desvía demasiado, copia el fichero de `ref/ch08-s1` y sigue.

Al terminar: tus tests en verde, commit y etiqueta `ch08-s1` (mensaje: `ch08-s1: the type table`); compárate con `ref/ch08-s1`.

### Sesión 2: unificación con procedencia (≈ 2,5 h)

#### Los tests, en rojo

1. **Escribe T08.10–T08.19** (T08.17, como en `ref/ch08-s2`). La interfaz:

```go
type ReasonKind uint8 // ReasonAnnotation, ReasonLetAnnotation, ReasonReturnType, ReasonBlockTail,
                      // ReasonArgument, ReasonIfBranches, ReasonIfCondition, ReasonOperand
type Reason struct { Kind ReasonKind; Because base.Span; Index int; Detail string; CanReturn bool }
type Unifier struct { Types *TypeTable; Sink *diag.Sink }
func (u *Unifier) Unify(expected, found TypeID, foundAt base.Span, r Reason) bool
func (t *TypeTable) showAll(ids ...TypeID) []string // one set of names for all of them
```

2. **Míralos fallar.** Sobre `ch08-s1` no compila: `unify_internal_test.go:65:52: undefined: Reason`.
3. **Copia `reason.go` entero y escribe un `Unify` que devuelva `true`.** Fallan nueve de los diez subtests de `TestUnify`, sin panic.

#### El código

{{file ch08-s2 internal/sema/reason.go}}

**El catálogo antes de que nadie llame a `Unify`.** E0401 y E0405 nacen con sus entradas en `docs/errors`, pero `ki check` todavía no comprueba tipos, y sus ejemplos erróneos no pueden dar su código.

Con la palabra de valla `pending=<valor>`, el ejemplo se ejecuta y solo se le pide no dar su código, y el test **falla** el día que lo da: `` now reports E0401: remove `pending` ``.

El valor dice quién hará alcanzable el código: en la referencia, `T36`, el nombre que llevaba la sesión 3; en tu repo, vale `pending=ch08-s3`.

El orden que funciona:

1. **Escribe `TestErrorDocs/pending_word`** (T08.20).
2. **Cambia `parseDoc` y `exampleProblems`** (T07.86, T07.87: `git show ref/ch08-s2:book/tests/ch07.tests`).
3. **Escribe las dos entradas:** `E0401.md` (siete pares, uno por sitio) y `E0405.md`.
4. **Por último, `unify.go`:** el test exige una entrada por cada código de la fuente, y al revés.

{{diff ch08-s1..ch08-s2 tests/errors_docs_test.go}}

{{file ch08-s2 docs/errors/E0405.md}}

{{file ch08-s2 internal/sema/unify.go}}

{{diff ch08-s1..ch08-s2 internal/sema/types.go}}

- **Una equivocación, un diagnóstico.** La primera parte que no cuadra termina `Unify`, y la etiqueta escribe los dos tipos que recibió `Unify`, no la parte que falló.
- **`bind`,** en minúscula, hace el occurs check que `Bind` no hace; si falla, la variable queda sin ligar.

:::more Sesión 2, los porqués: el orden de `Unify`, la ayuda, `pending=` y los siete sitios de `E0401.md`
- `same_type_ok` ya pasa con el `Unify` que devuelve `true`, porque decir que sí sin ligar nada es lo correcto para dos tipos iguales.
- **El orden.** Se apilan el resultado y los parámetros del último al primero. Apilados en orden, en `(p, p) -> Int` contra `(Int, Bool) -> Int` el error saldría en el primer parámetro, y T08.12 diría `p resolves to Bool, want Int`.
- **La ayuda** solo sale con `ReasonBlockTail` y `()` esperado, y sin fix-it: «this value is unused: bind it with `let name = …`», más «, or return it from the function» si la razón lleva `CanReturn`. Cómo llamar al valor, y si devolverlo, lo decides tú.
- **`indefinite`** elige «a» o «an» por la primera letra del tipo: `!1` dice «`!` expects a `Bool` operand» (T08.14).
- **El catálogo.** Sin `pending=`, `erroneous_examples_report_their_code` fallaría con `reports [], want at least one E0401`. T08.20 en rojo falla cinco veces, la primera porque la valla tiene `` the unknown word "pending=T36" ``.
- **`E0401.md`** tiene siete pares; sus sitios son la anotación de un `let`, el tipo de retorno, la cola `()`, el argumento, la otra rama, la condición y el operando, todos con la misma marca y el mismo párrafo.
- **T07.86, T07.87 y T07.94.** T07.86 y T07.87, que enumeran las palabras de la valla, aprenden `pending=`: las tarjetas de esta sesión están en `git show ref/ch08-s2:book/tests/ch07.tests`. T07.87 cambia otra vez en la sesión 3, cuando la marca desaparece, y T07.94 en la 4, cuando `FuzzAnalyze` gana tres semillas del E0404 y el E0406. La tabla del capítulo 7 las muestra como eran en `ch07-s4`.
:::

{{reftest T08.11}}

Si te atascas o tu versión se desvía demasiado, copia el fichero de `ref/ch08-s2` y sigue.

Al terminar: tus tests en verde, commit y etiqueta `ch08-s2` (mensaje: `ch08-s2: unification with provenance`); compárate con `ref/ch08-s2`.

### Sesión 3: el comprobador bidireccional (≈ 2,5 h)

#### Los tests, en rojo

1. **Escribe T08.21–T08.37** (T08.23, como en `ref/ch08-s3`) **y pon T08.17 como en su tarjeta.** La interfaz:

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

2. **Copia `testsupport` entero:**

{{diff ch08-s2..ch08-s3 internal/testsupport/compile.go}}

`Analyze` comprobará tipos, y algunos tests del capítulo 7 analizaban fuentes que no tipan. **Pasa a `testsupport.Resolve`,** que solo resuelve nombres, cuatro llamadas de `scope_test.go` y una de `resolve_test.go`.

3. **Míralos fallar.** Sobre `ch08-s2` no compila: `compile.go:83:47: p.Frontend.Typed undefined`.
4. **Añade el campo, haz que `Session.Analyze` llame a `Check` y escribe un `Check`** que cree las tablas con el tipo de error en cada entrada (expresiones, `Def`, `Local`). Fallan los dieciséis subtests de `TestCheck`, `TestUnify/mismatch_has_reason_label` y `TestParseExpr/call_arg_list_span`, sin panic.

#### El código

{{file ch08-s3 internal/sema/check.go}}

- **`Check`** corre `signatures`, `bodies` y `judgeEqualities`; `check` e `infer` son la teoría.
- **`checkIf`** sin `else` compara primero `()` con lo esperado, sobre el `if` entero; si falla, la rama solo se infiere.
- **`elseBranch`** devuelve el tipo de error si las ramas no cuadran, para que el `if` no dé otro error donde se usa (T08.35).
- **`call`**: otra aridad es E0402 sobre la lista de argumentos, que guarda el parser; un valor que no es función, E0403. Un parámetro sin anotación se liga a una función de los tipos de los argumentos.
- **`equality`**: `==` sobre una función o `()` es un solo E0401 con nota en el operando izquierdo. Si es aún una variable sin ligar, `judgeEqualities` lo juzga tras todos los cuerpos (T08.30, T08.31).

{{diff ch08-s2..ch08-s3 internal/sema/unify.go}}

`Unify` omite una secundaria en otro fichero: el parámetro de `print` está en `std/prelude.ki`, y `RenderHuman` hace panic con una etiqueta así.

{{diff ch08-s2..ch08-s3 internal/syntax/parser_expr.go}}

{{diff ch08-s2..ch08-s3 internal/driver/pipeline.go}}

**El catálogo se pone al día solo.** Con `Check` en `Analyze`, `TestErrorDocs` falla con ocho `` now reports …: remove `pending` ``, pide `E0402.md` y `E0403.md`, y los ejemplos corregidos de E0201 y E0203 ya no están limpios.

1. **Quita las ocho marcas** y los dos párrafos que decían que `ki check` no comprobaba tipos.
2. **Escribe las dos entradas nuevas.** `E0403.md` sigue el molde de `E0402.md`.
3. **Cambia esos ejemplos** a `let sum = 1 + 2` y `let ok = 1 < 2 && 2 < 3`, y sus gemelos erróneos igual.
4. **Añade a `E0401.md` dos pares,** los de `==` entre funciones.

{{diff ch08-s2..ch08-s3 docs/errors/E0405.md}}

{{file ch08-s3 docs/errors/E0402.md}}

:::more Sesión 3, los porqués: `signatures`, `checkAt`, `checkIf`, `call`, `equality` y el catálogo
- **El rojo.** Esas cinco llamadas analizan fuentes que no tipan: `fn f(a: Int) { a }`, por ejemplo, devuelve `Int` donde se espera `()`; otra pasa un `()` donde se pide un `Int`. Las tablas del `Check` provisional tienen una entrada por expresión de cada módulo, por `Def` y por `Local`.
- **`signatures`** rellena primero todas las tablas con el tipo de error y después da a cada `Def` su firma. Una expresión que el parser descartó conserva ese tipo, y no el id 0, que es `Int` (T08.21).
- **`checkAt`** es inferir y unificar, con el error en la expresión entera. `stmts` comprueba un `let` anotado con `check`: con `checkAt`, `let x: Int = if true { 1 }` daría dos errores en lugar de uno, y T08.28 lo dice (`let annotation: 2 diagnostics, want 1`).
- **`checkIf`**, si `()` cuadra, comprueba la rama contra `()` sin `CanReturn`: la rama no es lo que devuelve la función.
- **`Unify`** también omite la secundaria con el span cero, «ningún sitio».
- **`call`**: la nota del E0402 escribe los 16 primeros parámetros (`maxNoteParams`) y `…` si hay más, porque cada llamada mala la repite. Ligar el parámetro es lo que da el E0405 de `fn g(x) { x(x) }`.
- **El span de la lista.** El comprobador no tiene el texto, y «el final de la función llamada (el `Callee`) más uno» apuntaría a un blanco en `f (x)` o al `)` de `(f)(x)`: por eso lo guarda el parser (T08.37).
- **`equality`** no pasa por `Unify` para su regla propia, y en el `==` rechazado el operando derecho solo se infiere. Una variable sin ligar se apunta para `judgeEqualities`: así, con `fn same(x, y) -> Bool { x == y }`, `same(f, f)` da el mismo error esté `same` delante o detrás de `main`, y `x == y` entre dos parámetros que nada liga se acepta.
- **El catálogo.** Las ocho marcas son siete de E0401 y la de E0405, que tampoco queda pendiente: T08.36 ya lo da. `fn main() { 1 + 2 }` devuelve un `Int` donde se espera `()`, y `print(1 < 2 && 2 < 3)` pasa un `Bool` a `print`, que pide un `Int`.
:::

{{reftest T08.28}}

Si te atascas o tu versión se desvía demasiado, copia el fichero de `ref/ch08-s3` y sigue.

Al terminar: tus tests en verde, commit y etiqueta `ch08-s3` (mensaje: `ch08-s3: the bidirectional checker`); compárate con `ref/ch08-s3`.

### Sesión 4: errores legibles, sin cascadas, y `main` (≈ 2 h)

#### Los tests, en rojo

1. **Escribe T08.38–T08.48 y pon T08.23 como en su tarjeta.** La interfaz:

```go
// package syntax
type Item struct { …; ParamList, RetClause base.Span; ParamListOneLine, RetClauseOneLine bool }
func (p *parser) oneLine(from int) bool
func (p *parser) blanksBefore(hi uint32) uint32
// package testsupport (test-only code)
func ApplyFixits(src string, d diag.Diagnostic) string
```

Copia `fixits.go`:

{{file ch08-s4 internal/testsupport/fixits.go}}

2. **Añade a `analyze_fuzz_test.go` las tres semillas de T07.94** (`git show ref/ch08-s4:book/tests/ch07.tests`).
3. **Míralos fallar.** Sobre `ch08-s3` no compila: `parser_test.go:217:75: it.ParamList undefined`.
4. **Añade los cuatro campos, sin rellenarlos.** Fallan los tests de `main`, E0406, cascadas tras un error de sintaxis, spans de la firma y T08.48; otros ya pasan.

:::more Qué falla sobre `ch08-s3`, test por test
- `TestCheck/main_params_E0404`, `TestCheck/main_return_E0404` y las dos filas de `main` de `TestCheck/fn_without_arrow_returns_unit`.
- Diecisiete filas de `TestCheck/syntax_error_has_no_type_error`: un E0401 o un E0402 de más, como `reported [E0201 E0401], want [E0201]` o `[E0201 E0402]`; cuatro también `has the if with type "()", want {error}`.
- `TestParser/signature_spans`.
- `TestCheck/unknown_type_E0406`, en cada fila: `Bol: 0 diagnostics, want 1`.
- T08.48: `mismatch: … want at most a quarter`.

`no_cascade_after_unknown_name` y los cuatro golden de `tests/errors/ch08` ya pasan: el comprobador de la sesión 3 cumple la regla del tipo de error para los nombres, y estas tarjetas la fijan.
:::

Un golden de `tests/errors/ch08` (T08.42–T08.45):

{{file ch08-s4 tests/errors/ch08/if_branches_render.ki}}

La cabecera lleva el error a la línea 12, y el número de dos cifras ensancha el margen: es lo que el comparador de anotaciones no vería.

#### El código

{{diff ch08-s3..ch08-s4 internal/syntax/ast.go}}

{{diff ch08-s3..ch08-s4 internal/syntax/parser.go}}

{{diff ch08-s3..ch08-s4 internal/sema/check.go}}

{{diff ch08-s3..ch08-s4 internal/sema/unify.go}}

- **E0404 y el parser.** `main` tiene que ser `fn main()`, y quitar lo que sobra necesita dos spans. El comprobador no tiene el texto, y `fn main (x)` o `fn main()->Int` son válidos: no puede buscar el `(` ni el `->`. El parser guarda ambos spans, y `oneLine` dice si hay un salto de línea dentro.
- **E0406.** `typeOf` informa un nombre de tipo desconocido una vez, y desde ahí es el tipo de error. `docs/errors/E0406.md` tiene dos pares; `E0401.md` gana el de un valor sin usar en `main`.
- **Cascadas tras un error de sintaxis.** Un `if` sin `else` cuya rama el parser no pudo leer tiene el tipo de error (`unreadThen`). Mientras falte un argumento, `call` no juzga la cuenta (`missingArg`).
- **El sink lleno.** Si ya perdió un error (`Truncated`, capítulo 3), los tipos de otro que se va a perder no se escriben; `Unify` responde igual (T08.48).

{{file ch08-s4 docs/errors/E0404.md}}

{{diff ch08-s3..ch08-s4 docs/errors/E0402.md}}

:::more Sesión 4, los porqués: `RetClause`, `mainSignature`, E0406, `unreadThen`, `missingArg` y el sink
- **`oneLine`** lo comparten ahora `attr` e `item`.
- **`if_branch_mismatch.ki`** tiene el mismo programa que el golden de arriba, con `// ERROR(E0401) @32`.
- **`RetClause`** empieza en los blancos justo antes del `->` (`blanksBefore`), no en el `)`: así un comentario entre los dos sobrevive (T08.41).
- **`mainSignature`** corre entre las firmas y los cuerpos, y solo juzga el primer `main` del fichero, el mismo al que se refieren los nombres. Una firma en varias líneas lleva la ayuda sin ediciones: una edición es de una línea. `bodies` no da `CanReturn` a `main`, que E0404 ata a `() -> ()`.
- **E0406.** `fn f(x: Bol) {}` da ``unknown type `Bol` `` con «did you mean `Bool`?» y su edición; `Itn`, a dos ediciones de `Int`, no la lleva.
- **`unreadThen`.** El `()` de un `if` sin `else` cuya rama el parser no pudo leer no lo ha escrito nadie: ese `if` tiene el tipo de error. La condición y, con `else`, la otra rama se comprueban igual (T08.39).
- **`missingArg`.** `g(1,,2)` deja un nodo de error que alarga la lista: mientras falte un argumento, `call` no juzga la cuenta, porque ese E0402 se iría al borrar la coma. `c.missingArg` es un valor de método. Tampoco liga un parámetro sin anotación (`x(1,,2)` sería una función de tres parámetros que nadie escribió); los demás se comprueban igual. Un operador suelto también falta: `g(1, +, 2)` es una suma sin operandos. `2 +` y `-2` cuentan, porque solo se mira el principio: mirando la derecha, T08.39 diría `reported [E0201], want [E0201 E0402]`.
- **El sink lleno.** `show` apila todos los parámetros de una función antes de cortar, y un error que se va a perder no debe pagarlo: no escriben tipos el `mismatch` y el `bind` de `unify.go`, `rejectedEqualityError` y `arityError`. T08.48 mide los bytes reservados, más estables que el reloj: `runtime.ReadMemStats(&m)` rellena un `runtime.MemStats`, y `TotalAlloc`, que solo crece, se resta entre dos lecturas.
:::

{{reftest T08.38}}

Si te atascas o tu versión se desvía demasiado, copia el fichero de `ref/ch08-s4` y sigue.

Al terminar: tus tests en verde, commit y etiqueta `ch08-s4` (mensaje: `ch08-s4: readable errors, no cascades, main`); compárate con `ref/ch08-s4`.

## Ejecútalo

Con `ki` construido (`go build -o build/bin/ki ./cmd/ki`), el `fib.ki` del objetivo da lo de arriba y sale con 1. Un fichero con cuatro equivocaciones:

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

La línea 8 parece tener dos problemas y da un solo error: la suma tiene tipo error y la anotación no se queja. Un tipo infinito, y el formato corto:

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

- Un tipo es un id en una tabla que solo crece; con hash-consing, la igualdad de tipos, resueltas las variables, es comparar ids.
- Un slice no puede ser clave de un map: `funKey` usa un `string`.
- Go no tiene `const` compuestos: la inmutabilidad es una convención.
- Las variables son un union-find sobre `IdVec`, con compresión de caminos.
- Unificar es resolver, comparar, ligar con occurs check y bajar por las partes.
- La comprobación bidireccional empuja la expectativa hacia la expresión que está mal.
- Cada `Unify` lleva un `Reason`, que elige la secundaria, la nota o la ayuda: el origen del error.
- El tipo de error unifica en silencio y se propaga: una equivocación, un error.
- `pending=` deja nacer una entrada del catálogo antes de que su código sea alcanzable, y caduca.

## Go en el trabajo

**Los mensajes, revisados como producto.** Los UI tests de rustc guardan la salida esperada junto al test y exigen anotar los errores en la fuente. En Ki, `if_branches_render.ki` guarda el texto entero, y `if_branch_mismatch.ki` fija el código y la posición de todos los errores.

**Claves compuestas.** `go/types` también hace hash-consing de instancias genéricas, con claves `string`, porque una lista de argumentos de tipo no puede ser clave.

:::more Las fuentes y la medida: rustc-dev-guide, `go/types` y `mapfast`
**rustc.** La *rustc-dev-guide*: «UI tests store the expected output from the compiler in `.stderr` and `.stdout` snapshots next to the test». Además, «UI tests require that errors are also annotated within the source», porque los `.stderr` se generan solos (con `--bless`, y después se leen a mano) y las anotaciones «ensure that no additional unexpected errors are generated». En Ki, el texto de `if_branches_render.ki` lo reescribe `ki test --update`.

**Go.** `src/go/types/check_test.go` lee comentarios `/* ERROR "pattern" */` tras el token del error y exige que el patrón sea una subcadena del mensaje en esa posición. El `Context` de `go/types` reutiliza instancias genéricas idénticas, sin garantizarlo siempre, con un `map[string][]ctxtEntry` cuya clave escribe `instanceHash`.

**La forma de la clave** decide qué código corre: hay caminos rápidos para claves de 4 u 8 bytes que se comparan como memoria y para strings. `mapfast`, en `src/cmd/compile/internal/walk/walk.go`, los elige; cualquier otra clave va por el camino general, `mapaccess2`, que llama a las funciones de hash e igualdad del tipo. En esta máquina, con 1 000 claves ya construidas, buscar con la forma de `funKey` (dos `uint32` y un `string`) costó unos 14,3 ns, y con todo en un `string`, unos 8,7. `funKey` se queda como struct por legibilidad, aunque un `string` busque algo más rápido: su coste está en construir la clave (ejercicio 4).
:::
