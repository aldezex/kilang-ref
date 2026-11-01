---
chapter: 7
title: Nombres, ámbitos y errores al nivel de rustc
part: I
hours: 9
milestone: `unknown name 'fibb'; did you mean 'fib'?`, con el fix-it que lo sustituye; los fix-its se muestran como sugerencias de rustc; todos los códigos E01xx–E03xx al nivel del apartado 7.3 y el catálogo `docs/errors` con sus ejemplos probados
---

## Objetivo

Al terminar, `ki check` sabe qué significa cada nombre: un parámetro, un `let` o una función, y cuál. Si no significa nada, lo dice como rustc, con el arreglo escrito:

```console
$ cat fib.ki
fn fib(n: Int) -> Int {
  if n < 2 { n } else { fibb(n - 1) + fib(n - 2) }
}

fn main() {
  print(fib(20))
}
$ build/bin/ki check fib.ki
error[E0301]: unknown name `fibb`
 --> fib.ki:2:25
  |
2 |   if n < 2 { n } else { fibb(n - 1) + fib(n - 2) }
  |                         ^^^^ not found in this scope
  = help: did you mean `fib`?
  |
2 -   if n < 2 { n } else { fibb(n - 1) + fib(n - 2) }
2 +   if n < 2 { n } else { fib(n - 1) + fib(n - 2) }
```

Las dos últimas líneas son un *fix-it*: la línea tal cual y la arreglada. La sesión 4 lleva a ese nivel los diecinueve códigos y crea `docs/errors`, un catálogo cuyos ejemplos se ejecutan en cada CI. Es el hito, camino de M1 (capítulo 10).

## Teoría

### Maps a fondo

El capítulo 4 enseñó lo básico de `map[K]V`, con su `nil` que se lee y no se escribe. Falta lo que importa a un resolvedor:

- **Semántica de referencia.** Un map es un descriptor de una tabla que vive aparte, y pasarlo copia el descriptor: una `add(m)` que hace `m["b"] = 2` cambia el map de quien la llama.
- **Claves comparables.** La clave admite `==`: números, strings, punteros, structs de campos comparables. `map[[]byte]int` no compila: `invalid map key type []byte`.
- **`clear(m)`** vacía el map (`len` 0, sigue sin ser `nil`).
- **El orden.** Cambia a propósito, y ninguna salida del compilador depende de él (spec 7.2): lo que se enseña se ordena antes, por id o por nombre.
- **Tablas suizas.** Desde Go 1.24, el map usa grupos de 8 casillas con una palabra de control. El orden aleatorio no cambió.

:::from Rust
`HashMap<Vec<u8>, i32>` compila; en Go un slice nunca es clave. Nadie es dueño del map: copiar la variable comparte la tabla, lo que en Rust pediría un `Rc<RefCell<HashMap<…>>>`.
:::

### `maps`, `slices` e `iter.Seq`

`slices.Sorted(maps.Keys(m))` saca de un map una lista que no depende del azar: `maps.Keys` devuelve un `iter.Seq[K]` (capítulo 4) de orden no especificado, y `slices.Sorted` lo recoge en un slice nuevo y lo ordena.

Los ámbitos (más abajo) escriben su propio iterador. `visible` devuelve `func(yield func(base.Symbol) bool)`: recorre los ámbitos de dentro afuera, ordena cada ámbito por `Symbol` y para cuando `yield` devuelve `false`. Quien lo usa escribe `for sym := range sc.visible()`, sin ver los maps.

:::from Python
`sorted(d)` es la misma idea; pero un `dict` conserva el orden de inserción, y el olvido no se nota hasta construirlo en otro orden.
:::

### `//go:embed` y el preludio

`print` no lo define tu fichero: lo declara `stdlib/prelude.ki`, que viaja dentro del binario. `//go:embed stdlib`, sobre una variable `embed.FS`, hace que `go build` copie el directorio en el ejecutable, y `fs.ReadFile(Stdlib, "stdlib/prelude.ki")` lo lee en tiempo de ejecución.

`embed.go` vive en la raíz del módulo, en el paquete `kilang`, porque `embed` solo lee «from the package directory or subdirectories», y solo la raíz tiene `stdlib/` debajo.

:::from Rust
`include_str!("prelude.ki")` hace lo mismo con un fichero y admite `..`. Go pide que cuelgue del paquete, y a cambio `embed.FS` se recorre como cualquier `fs.FS`, la interfaz de `io/fs`.
:::

:::more Qué dice Go si `//go:embed` no está en la raíz
Desde `internal/driver`, `//go:embed ../../stdlib` da `pattern ../../stdlib: invalid pattern syntax`, y `//go:embed stdlib` busca `internal/driver/stdlib`: `pattern stdlib: no matching files found`.

La ruta dentro del `FS` conserva el directorio: `fs.ReadFile(Stdlib, "prelude.ki")` da `file does not exist`. La interfaz `fs.FS` tiene un solo método, `Open`.
:::

### La distancia de edición con dos filas

La **distancia de Levenshtein** es el menor número de inserciones, borrados y sustituciones que convierten una cadena en otra: `fibb` está a una de `fib`. Es programación dinámica: la casilla `[i][j]` es la distancia entre los `i` primeros bytes de `a` y los `j` primeros de `b`, y sale de tres vecinas:

```text
d[i][j] = min(d[i-1][j] + 1,                       // delete a[i-1]
              d[i][j-1] + 1,                       // insert b[j-1]
              d[i-1][j-1] + (a[i-1] != b[j-1]))    // keep or replace
```

La fila 0 vale `j` (construir `j` bytes desde nada) y la columna 0, `i`. Cada fila solo mira la anterior: bastan dos slices de `len(b)+1` enteros que se intercambian con `prev, cur = cur, prev`. Se cuentan bytes: `é` y `e` están a distancia 2.

### Tablas de símbolos, ámbitos y sombreado

Resolver nombres es decidir a qué se refiere cada `ExprName`:

- **Definiciones** (`Def`, por `DefID`): cada función de cada módulo (el preludio es el 0 y el fichero, el 1). Se buscan primero las del módulo y luego las `pub` del preludio: tu `print` tapa el de la biblioteca sin ser una función repetida (E0302).
- **Variables locales** (`Local`, por `LocalID`): cada parámetro y cada `let`; dos `let x` seguidos son dos variables.
- **Ámbitos**: un map de nombres a `LocalID` con un puntero al de fuera. Los parámetros son uno; cada bloque, otro dentro.
- **Sombreado**: subiendo por la cadena, gana el primer ámbito con el nombre. Un `let` liga *después* de resolver su inicializador: en `let x = x`, la `x` de la derecha es la de fuera.
- **Tablas laterales**: los pases no mutan el árbol; lo aprendido va en slices por id (`ExprRes[ExprID]`, `LetLocal[StmtID]`, `ParamLocals[ItemID]`), una fila por nodo.
- **Dos pasadas**: primero las definiciones, luego los cuerpos. Así `main` puede llamar a una función definida más abajo.
- **Sugerencias deterministas**: los candidatos de un «did you mean» se ordenan, y a igual distancia gana el primero en el alfabeto.

### El listón: errores al nivel de rustc

Tres errores de rustc 1.97.1, sobre programas como los de Ki:

```text
error[E0425]: cannot find function `helpr` in this scope
 --> s2.rs:3:13
  |
1 | fn helper() -> i32 { 1 }
  | ------------------ similarly named function `helper` defined here
2 | fn main() {
3 |     let x = helpr();
  |             ^^^^^
  |
help: a function with a similar name exists
  |
3 |     let x = helper();
  |                 +

error: mismatched closing delimiter: `}`
 --> p.rs:1:17
  |
1 | fn c() -> i64 { (1 }
  |               - ^  ^ mismatched closing delimiter
  |               | |
  |               | unclosed delimiter
  |               closing delimiter possibly meant for this

error[E0428]: the name `f` is defined multiple times
 --> d.rs:2:1
  |
1 | fn f() {}
  | ------ previous definition of the value `f` here
2 | fn f() {}
  | ^^^^^^ `f` redefined here
  |
  = note: `f` must be defined only once in the value namespace of this module
```

Uno de Elm 0.15.1, sin cabecera, de «Compiler Errors for Humans» (Evan Czaplicki):

```text
Cannot find variable `List.nap`.

6|   div [] (List.nap viewUser users)
             ^^^^^^^^
`List` does not expose `nap`. Maybe you want one of the following?

    List.map
    List.any
    List.map2
    List.map3
```

Comparten la anatomía de un `Diagnostic`:

- **mensaje**: nombra la cosa;
- **etiqueta primaria**: qué está mal *en ese sitio*;
- **secundarias**: el otro sitio que explica el error (la primera definición, el paréntesis abierto);
- **nota**: la regla, si el mensaje no la hace evidente;
- **ayuda**: una acción concreta;
- **fix-it**: la corrección, si es mecánica.

Desde la sesión 4, todo código de Ki cumple ese listón (spec 7.3): fix-it siempre con su ayuda, entrada en el catálogo y cada código nuevo completo desde que nace.

**Una ayuda genérica es peor que ninguna.** «check your syntax» no dice qué hacer y enseña a ignorar la línea de ayuda, también cuando sirve. Donde no hay nada concreto, Ki calla: el E0201 de una expresión que falta no tiene ayuda.

**Un fix-it es una edición `{span, replacement}`**: span vacío, inserción; reemplazo vacío, borrado. Se enseña en la forma «diff» de rustc, bajo la ayuda: `N - original` y `N + parcheada` por línea tocada. rustc marca las inserciones con `+`, como arriba; Ki usa siempre el par.

**Dos ediciones en una línea** usan las posiciones de la línea **original**, que equivale a aplicarlas de derecha a izquierda: en `x = fibb + gammaa`, cambiar `fibb` por `f` primero correría tres bytes la otra.

Un fix-it sin ayuda, en otro fichero o que cruza un salto de línea, y dos que se solapan, son errores del pase que los construyó: `RenderHuman` hace panic con el motivo.

No hay edición donde no sería mecánica: qué dígitos querías, qué nombre lleva tu variable, cuál de las dos funciones sobra. A veces solo vale en un caso: `a < b < c` solo se reescribe si `b` es un token, y en `print(1 2)` falta una coma, no un `)`.

:::more Los códigos del resolvedor, qué gana cada uno en la sesión 4 y cuándo hay edición
Los del resolvedor son E0301, nombre desconocido; E0302, función definida dos veces; E0304, `#[extern]` fuera de la biblioteca estándar; y E0305, atributo desconocido. E0303, sin `main`, llegará con `ki run` en el capítulo 10.

| Qué cambia | Códigos |
|---|---|
| La primaria, que no tenía texto, lo gana | E0106 «unknown escape», E0201 «expected `)` here», E0203, E0204 «nesting level 257 starts here», E0205 |
| La primaria hacía de ayuda o hablaba del libro, y la ayuda pasa a su sitio | E0102, E0304, E0305 |
| Secundaria nueva | E0201 de un cierre («unclosed `(` opened here»), E0203, E0301 de `let x = x` |
| Nota nueva | E0101, E0102, E0104 (solo letras), E0109, E0301 de `let x = x`, E0304, E0305 |
| Ayuda sin edición | E0101, E0105, E0106 en un carácter, E0107 sin dígitos, E0108 vacío o con `"` o `${` sueltos, E0109, E0201 de `let`, E0202 cuyo cierre caería en un literal sin cerrar, E0204, E0205, E0302, E0304 sin cuerpo |
| Ayuda con edición | E0102, E0103, E0104, E0106 en una cadena, E0107, E0108 de varios caracteres sin `"` ni `${` sueltos, o sin cerrar tras uno, E0110 fuera de un `${` y sin barra impar al final, E0201 de un cierre que falta, E0202, E0203, E0301, E0304, E0305 |

A veces la edición solo vale en un caso:

- `E0203` solo reescribe `a < b < c` como `a < b && b < c` si el operando central es un token.
- El E0201 de un cierre solo lo escribe si el token hallado es `}`, `]`, `;`, `let` o el comienzo de un item, porque en `print(1 2)` falta una coma, no un `)`.
:::

Los capítulos 3, 5 y 6 muestran la salida anterior (primarias sin texto), exacta para sus tags, y por eso no se reescriben: una fe de erratas remite aquí.

**El catálogo es documentación ejecutable.** `docs/errors/E0301.md` explica el código y trae un ejemplo erróneo y uno corregido, en vallas `ki erroneous` y `ki corrected`. `tests/errors_docs_test.go` ejecuta cada uno con `driver.Main`: el erróneo tiene que dar su código y el corregido, salir limpio.

Cada código necesita su fichero y cada fichero, su código: uno nuevo sin entrada rompe la CI.

**El test lee el fuente como Go.** `parser.ParseFile` (`go/parser`) da el árbol de un fichero, y `ast.Inspect` llama a una función con cada nodo (si devuelve `false`, no baja a sus hijos). Busca las llamadas a `diag.E` y `diag.W`; una expresión regular contaría comentarios y cadenas, y el fichero de un código quitado seguiría vivo.

:::from Python
`ast.walk(ast.parse(src))` da cada nodo, como `ast.Inspect` pero sin poda.
:::

**Dos palabras de la valla.** `escaped` hace que el test decodifique el bloque con `strconv.Unquote`, para bytes que un fichero de texto no puede llevar. `also=E0201` lista otros códigos que el ejemplo da.

## Tests primero

{{cards}}

Úsalas así: escribe el test de la tarjeta, míralo fallar, implementa lo justo para el verde, y solo entonces despliega la referencia con su `{{reftest}}`.

Describen el final del capítulo. T07.02, T07.05–T07.07, T07.20, T07.32, T07.33 y T07.36 cambian en la sesión 4; hasta entonces se escriben como en `git show ref/ch07-sK:book/tests/ch07.tests` y en el test de `ref/ch07-sK`, con la `K` de su sesión.

:::more Paquetes de test, ayudantes y el rojo, sesiones 1 a 3
`resolve_test.go`, `scope_test.go` y `suggest_test.go` son `package sema_test`, porque usan `internal/testsupport`, que pasa un texto por el compilador entero y por eso importa `driver`, que importa `sema`: desde `package sema`, Go diría `import cycle not allowed in test`. `scope_internal_test.go` y `suggest_internal_test.go` son `package sema` y tocan `scope`, `suggest` y `resolver`, que no se exportan.

Los ayudantes, en `ref/ch07-sK`:

- **Sesión 1, `resolve_test.go`.** `nameExprs` da los `ExprName` de un nombre; `onlyRes` y `userRes`, la resolución del único que hay; `defsOf`, las `Def` de un módulo; `wantCodes` compara los códigos de error con `testsupport.ErrorCodes`; `resolveSources` resuelve con un preludio escrito por el test.
- **Sesión 2, `scope_test.go`.** Encuentran cosas en el texto: el fichero y un span (`userFile`, `spanIn`), el enésimo uso de un nombre (`useAt`), una función, sus parámetros o la variable de un `let` (`fnNamed`, `paramsOf`, `paramLocal`, `letNamed`), las variables del fichero (`userLocals`). Y comprueban: `wantLocal`, un uso `ResLocal` con su nombre, span y dueña; `userDiags`, exactamente n diagnósticos por posición; `checkUnknown` y `wantUnknown`, un E0301.
- **Sesión 2, otros ficheros.** `resolve_test.go` gana `resAt`, la resolución del enésimo nombre de un módulo. `firstN`, en `scope_internal_test.go`, corta un iterador tras n nombres. En rojo, el primer fallo es `pass 0: visible names [], want [1 4 5 7 2 6 8 3 9]`; con el `StmtLet` al revés, `error codes [], want [E0301]`.
- **Sesión 3, `suggest_internal_test.go`.** `TestSuggest` usa un closure `want` que compara `suggest` con una fila, y en sus dos últimos subtests monta un `resolver` a mano.
- **Sesión 3, `suggest_test.go`.** `wantSuggestion` exige un solo E0301 con su ayuda, `suggestionIn` y `suggestionWith` analizan con el preludio de verdad o con uno propio, y `didYouMean` escribe la ayuda. `no_suggestion_when_far` ya pasa con los stubs, porque «sin ayuda» es lo que pide.
:::

## Implementación

### Sesión 1: el preludio, `embed.go` y las definiciones (≈ 2 h)

#### Los tests, en rojo

1. **Escribe T07.01–T07.17.** `resolve_test.go` es `package sema_test`: `testsupport` importa `driver`, que importa `sema`, y desde dentro sería un ciclo. La interfaz:

```go
// package kilang (embed.go): //go:embed stdlib
var Stdlib embed.FS
// package syntax
const ExternAttr = "extern" // a function without a body needs #[extern]

// package sema
type DefID uint32; type LocalID uint32; type ModuleIdx uint32
type DefKind uint8 // DefFn, DefExternFn
type Def struct { Name base.Symbol; Kind DefKind; Module ModuleIdx; Item syntax.ItemID; Span base.Span; Pub bool; ExternName string }
type Local struct { Name base.Symbol; Span base.Span; Owner DefID }
type ResKind uint8 // ResNone, ResDef, ResLocal, ResError
type Res struct { Kind ResKind; Def DefID; Local LocalID }
type ModuleRes struct { ExprRes []Res; LetLocal []LocalID; ParamLocals [][]LocalID }
type ResolvedProgram struct { Defs base.IdVec[DefID, Def]; Locals base.IdVec[LocalID, Local]; Modules []ModuleRes; Main DefID; HasMain bool }
func Resolve(mods []*syntax.Module, user ModuleIdx, in *base.Interner, sink *diag.Sink) *ResolvedProgram

// package driver
type Frontend struct { Modules []*syntax.Module; User sema.ModuleIdx; Resolved *sema.ResolvedProgram }
func (s *Session) ParseSource(displayPath string, contents []byte) *Frontend
func (s *Session) LoadAndParse(path string) (*Frontend, bool)
func (s *Session) Analyze(f *Frontend)

// package testsupport (test-only code)
type Program struct { Session *driver.Session; Frontend *driver.Frontend; Out, Err bytes.Buffer }
func Analyze(t *testing.T, source string) *Program
func ErrorCodes(p *Program) []string
```

2. **Copia `testsupport`, que pasa un texto por el compilador entero;** luego, el golden de T07.17:

{{file ch07-s1 internal/testsupport/compile.go}}

{{file ch07-s1 tests/errors/ch07/duplicate_definition.ki}}

3. **Cambia los tests del capítulo 6 con funciones sin cuerpo,** que necesitan `#[extern]`, solo válido en la biblioteca estándar (E0304):

{{diff ch06-s4..ch07-s1 internal/syntax/parser_test.go}}

{{diff ch06-s4..ch07-s1 internal/syntax/parser_stmt_internal_test.go}}

4. **Míralos fallar.** Sobre `ch06-s4` no compila: `embed_test.go:13:28: undefined: Stdlib`, `p.Session.ParseSource undefined`.
5. **Escribe ya, enteros, `embed.go`, `stdlib/prelude.ki` y `pipeline.go`** (más abajo): con stubs, los tests hacen panic.
6. **Añade los tipos de `sema` y un `Resolve` que solo crea las tablas.**
7. **Evita tres panics:** tres subtests leen `Sink.All()[0]` tras un `t.Errorf`. En tu versión, pon un `t.Fatalf` delante; con la referencia, `go test ./... -skip 'TestResolve/(duplicate_definition_E0302|extern_only_in_std_E0304|unknown_attribute_E0305)$'`.
8. **Mira qué falla ahora:** cinco subtests de `TestResolve`, `TestPipeline/analyze_does_not_write`, `TestParser/bodyless_fn_needs_extern_E0201`, `TestParseStmt/e0201_sites` y el golden `duplicate_definition.ki`.

#### El código

{{file ch07-s1 embed.go}}

{{file ch07-s1 stdlib/prelude.ki}}

{{file ch07-s1 internal/driver/pipeline.go}}

{{file ch07-s1 internal/sema/resolve.go}}

{{diff ch06-s4..ch07-s1 internal/syntax/parser.go}}

**Los comandos** pasan por `LoadAndParse`:

{{diff ch06-s4..ch07-s1 internal/driver/check.go}}

{{diff ch06-s4..ch07-s1 internal/driver/commands.go}}

{{diff ch06-s4..ch07-s1 internal/driver/emit.go}}

- **`parseFiles`** lexa el preludio y el fichero con un solo interner: el mismo nombre es el mismo `Symbol` en los dos, y así `print` encuentra su definición.
- **`collect`**, la primera pasada, crea una `Def` por función, también por la segunda de un nombre repetido, que da E0302 y deja que el nombre siga siendo la primera.

:::more Detalle de la sesión 1: por qué paran los stubs, el rojo, `attributes`, `tops` y `fileArgs`
Con un `Stdlib` vacío, `parseFiles` no encuentra el preludio y hace panic, y un `ParseSource` de mentira deja módulos vacíos que los tests indexan. El `Resolve` del stub crea `Modules` con `ExprRes`, `LetLocal` y `ParamLocals` de una fila por nodo.

El primer fallo de `TestResolve`: `the program has 0 definitions, want 3 (the prelude's print and the two of the file)`.

Con tus `t.Fatalf` en lugar del `-skip`, los tres subtests del paso 7 también fallan: ocho en `TestResolve`, y no cinco.

`tables_are_indexed_by_id` ya pasa (las tablas son el stub), y `missing_main_is_not_a_resolve_error` también: un stub nunca encuentra `main`, y ese test solo dice algo junto a `main_found`.

`tops` son maps que solo se consultan, nunca se recorren.

`attributes` decide la clase: `extern` fuera del preludio es E0304 y la función queda como `DefFn`; cualquier otro atributo es E0305.

`fileArgs` devuelve la ruta, y cada comando llama a `LoadAndParse` (o a `load`, en `emit tokens`, que no lexa el preludio). `emit ast` no llama a `Analyze`: su volcado es de la sintaxis (T07.16).
:::

{{reftest T07.03}}

{{reftest T07.05}}

Si te atascas o tu versión se desvía demasiado, copia el fichero de `ref/ch07-s1` y sigue.

Al terminar: tus tests en verde, commit y etiqueta `ch07-s1` (mensaje: `ch07-s1: the prelude, embed.go and top-level definitions`); compárate con `ref/ch07-s1`.

### Sesión 2: ámbitos, variables locales y sombreado (≈ 2 h)

#### Los tests, en rojo

1. **Escribe T07.18–T07.25.** `scope_internal_test.go` es `package sema`: `scope` no se exporta. La interfaz exportada no cambia: `Locals`, `LetLocal` y `ParamLocals` empiezan a tener contenido. La interna:

```go
type scope struct { names map[base.Symbol]LocalID; parent *scope }
func (s *scope) bind(name base.Symbol, id LocalID)
func (s *scope) lookup(name base.Symbol) (LocalID, bool)
func (s *scope) visible() iter.Seq[base.Symbol] // for the suggestions of session 3
```

2. **Míralos fallar.** Sobre `ch07-s1` no compila: `scope_internal_test.go:30:13: undefined: scope`.
3. **Añade stubs:** los métodos devuelven el valor cero, y `visible`, `func(yield func(base.Symbol) bool) {}` (con `nil`, el `range` haría panic). Fallan `TestScope/visible_lists_innermost_first_once` y los siete subtests de `TestScopes`, sin panic; `TestResolve` sigue en verde.

#### El código

{{file ch07-s2 internal/sema/scope.go}}

{{diff ch07-s1..ch07-s2 internal/sema/resolve.go}}

- **`bind`** crea el map la primera vez: un ámbito sin variables no paga ninguno.
- **`bodies`** recorre las `Def`, no los items: cada variable tiene por dueña la definición que se recorre, también la segunda de un E0302.
- **`ExprBlock`** abre un ámbito y restaura el de fuera al salir: lo que liga un bloque acaba con él, cola incluida.
- **`StmtLet`** resuelve el inicializador y liga después; al revés, T07.20 ya no da el E0301 de `let x = x`.
- **`name`** busca en los ámbitos y luego en las definiciones. Si no hay nada, informa E0301 y deja `ResError`: para los pases siguientes, el nombre ya está explicado.

{{reftest T07.21}}

{{reftest T07.25}}

Si te atascas o tu versión se desvía demasiado, copia el fichero de `ref/ch07-s2` y sigue.

Al terminar: tus tests en verde, commit y etiqueta `ch07-s2` (mensaje: `ch07-s2: scopes, locals and shadowing`); compárate con `ref/ch07-s2`.

### Sesión 3: «did you mean» y la comprobación de nombres (≈ 2 h)

#### Los tests, en rojo

1. **Escribe T07.26–T07.38.** La interfaz:

```go
func EditDistance(a, b string) int                          // package base
func suggest(name string, candidates []string) (string, bool) // package sema, unexported
func (r *resolver) candidates() []string
```

2. **Escribe el golden de T07.36,** en su forma de esta sesión:

{{file ch07-s3 tests/errors/ch07/unknown_name_render.ki}}

3. **Míralos fallar.** Sobre `ch07-s2` no compila: `undefined: EditDistance`, `undefined: suggest`.
4. **Añade stubs** que devuelven 0, `("", false)` y `nil`. Fallan `TestEditDistance/basic`, los siete subtests de `TestSuggest`, dos de `TestSuggestions` y el golden `unknown_name_render.ki`, sin panic.

#### El código

{{file ch07-s3 internal/base/edit_distance.go}}

{{file ch07-s3 internal/sema/suggest.go}}

{{diff ch07-s2..ch07-s3 internal/sema/resolve.go}}

- **El umbral** es `max(1, len(name)/3)`: una edición para un nombre corto, una más por cada tres bytes (`fibb` admite una; `zzzzzz`, dos).
- **El descarte por largo**: cada edición cambia el largo en uno como mucho, así que no se mide un candidato que difiere en más que el umbral (T07.37).
- **El desempate** `c < best` hace determinista la ayuda aunque los candidatos lleguen desordenados; `candidates` los junta en un map (sin repetidos) y los ordena, para que la lista sea estable (T07.31).
- **El freno**: cuando el sink ya ha descartado un error (pasados los 100), no se buscan candidatos; la ayuda iría a un diagnóstico que nadie ve (T07.38).

{{reftest T07.26}}

{{reftest T07.30}}

Si te atascas o tu versión se desvía demasiado, copia el fichero de `ref/ch07-s3` y sigue.

Al terminar: tus tests en verde, commit y etiqueta `ch07-s3` (mensaje: `ch07-s3: did you mean and name checking`); compárate con `ref/ch07-s3`.

### Sesión 4: diagnósticos al nivel de rustc y el catálogo (≈ 2,5 h)

#### Los tests, en rojo

1. **La interfaz nueva, para T07.39–T07.96:**

```go
// package diag
func Insert(at base.Span, text string) FixIt // empty span at at.Lo
func Delete(span base.Span) FixIt
func Replace(span base.Span, text string) FixIt
func (d Diagnostic) WithFixIt(f FixIt) Diagnostic // a copy; the help line says what it does
// RenderJSON: "fixits":[{"line","col","end_line","end_col","replacement"}] after "help"

// package syntax
type Attr struct { …; NameSpan base.Span; OneLine bool } // the name; no line break from # to ]
func (p *parser) prev() Token // the last token consumed, Newlines aside
func closerIsMissing(k TokenKind) bool // found k: is the closer what is missing?
type TokenBuffer struct { …; endsInComment bool; unterminatedLiteral base.Span }
func (tb *TokenBuffer) inUnterminatedLiteral(at uint32) bool

// package sema
const maxSuggestLen = 128 // a longer unknown name gets no suggestion
```

2. **Lleva a su forma final tus tests,** desde las tarjetas T07.02, T07.05–T07.07, T07.20, T07.32 y T07.33: donde exigían «sin ayuda» o «sin nota», ahora exigen la que hay.
3. **Escribe los nuevos desde las tarjetas:** `internal/syntax`, `diag`, `sema` y `tests/errors_docs_test.go`; los cambios del capítulo 3, en el diff de `render_test.go` (más abajo).
4. **Toma de estos diffs los tests de los capítulos 3, 5 y 6,** que en `internal/syntax` cambian en muchas líneas:

{{diff ch07-s3..ch07-s4 internal/syntax/source_check_test.go}}

{{diff ch07-s3..ch07-s4 internal/syntax/lexer_test.go}}

{{diff ch07-s3..ch07-s4 internal/syntax/numbers_test.go}}

{{diff ch07-s3..ch07-s4 internal/syntax/strings_test.go}}

{{diff ch07-s3..ch07-s4 internal/syntax/parser_test.go}}

{{diff ch07-s3..ch07-s4 internal/syntax/parser_internal_test.go}}

{{diff ch07-s3..ch07-s4 internal/syntax/parser_expr_internal_test.go}}

{{diff ch07-s3..ch07-s4 internal/syntax/parser_stmt_internal_test.go}}

{{diff ch07-s3..ch07-s4 internal/syntax/recover_test.go}}

{{diff ch07-s3..ch07-s4 internal/syntax/parser_fuzz_test.go}}

5. **Míralos fallar.** Sobre `ch07-s3` no compila: `internal/diag/diagnostic_test.go:107:8: undefined: Replace`, `p.prev undefined`, `undefined: closerIsMissing`, `undefined: diag.Delete`.
6. **Añade stubs:** las tres constructoras devuelven `FixIt{}`, `WithFixIt` devuelve `d`, `prev` devuelve `Token{}`, `closerIsMissing` devuelve `false` y `Attr` gana los dos campos. Fallan 110 subtests de 17 funciones (sin el golden de T07.90), ninguno con panic; `-update` aún congelaría el stub.
7. **Escribe el catálogo:** un fichero por código, los diecinueve, como `E0301.md` (más abajo). El formato, `escaped` y `also=`, en el pliegue del catálogo. Si te atascas, copia `docs/errors` de `ref/ch07-s4`.

:::more Qué falla sobre `ch07-s3` y los ayudantes de la sesión 4
**Dentro de los 110 subtests.** No cuentan el golden de T07.90; con él son 111 de 18 funciones. Entre los 110 están los de `TestRenderHuman`, donde faltan los ocho golden nuevos de `internal/diag/testdata` (`reading testdata/fixit_replacement_diff.golden (run go test ./internal/diag -update to create it)`) y `no_trailing_spaces` cuenta 14 de 22, y los de `TestErrorDocs`, que falla mientras `docs/errors` no existe (`cannot read the catalog: open ../docs/errors: no such file or directory`). Con el catálogo escrito, pasa ya sobre el código de la sesión 3, porque los códigos existen.

**Los tests del capítulo 3.** Lo que cambia (la ventana de una línea larga, `fixits` en JSON) está en el diff de `render_test.go`.

Los ayudantes, en `ref/ch07-s4`:

- **Las ediciones.** `wantPanic` (en `render_test.go`) exige un panic con un texto exacto; `applyFixits` (en `lexer_test.go`) y `applyFixes` (en `parser_expr_internal_test.go`), el mismo código, aplican las ediciones para volver a lexar o parsear.
- **`parser_stmt_internal_test.go`.** `expectedHere` escribe la primaria «expected <what> here»; `e0201Close` construye el E0201 de un cierre con secundaria, ayuda e inserción, y `e0201Opened`, el que solo lleva la secundaria; `wantCloser` compara con uno u otro el primer diagnóstico, que da `firstDiag`; `e0201Position` da el span del token hallado.
- **`recover_test.go`.** Hacen lo mismo `primaryOf`, `recE0201Close`, `recE0201Opened`, `recE0201Let` (el `let` sin nombre) y `recE0205`. `chainedDiag` (en `parser_expr_internal_test.go`, y no el método del parser de ese nombre) y `recE0203` ganan la secundaria, la ayuda y la inserción del E0203 esperado.
- **`scope_test.go`.** `checkUnknownSelf` y `checkUnknownWith` comprueban un E0301 con secundaria y nota.
:::

#### El código

**`diag`:** el fix-it y su diff.

{{file ch07-s4 internal/diag/diagnostic.go}}

{{diff ch07-s3..ch07-s4 internal/diag/render.go}}

`applyFixits` (la de `render.go`) copia el texto hasta cada edición y salta su span, sobre las posiciones originales.

**El lexer:** los E01xx llegan al listón.

{{diff ch07-s3..ch07-s4 internal/syntax/source_check.go}}

{{diff ch07-s3..ch07-s4 internal/syntax/token.go}}

{{diff ch07-s3..ch07-s4 internal/syntax/lexer.go}}

{{diff ch07-s3..ch07-s4 internal/syntax/numbers.go}}

{{diff ch07-s3..ch07-s4 internal/syntax/strings.go}}

:::more El diff, la ventana y los casos raros de cada código
- **`WithFixIt`** recorta con `slices.Clip` antes del `append`, como los demás `With`: dos copias del mismo diagnóstico no comparten hueco (T07.39).
- **`groupFixits`** comprueba las cuatro reglas y agrupa por línea.
- **La forma «diff».** rustc la usa cuando la edición borra algo. El par de Ki lleva una línea con la barra delante y las reglas de visualización del código (tabuladores, U+FFFD, ventana): **`showWindow`** y **`writeNumbered`** salen del código de las etiquetas, así que el diff y el código usan la misma función y no pueden pintar distinto (T07.47).
- **Margen y ventana.** El ancho del margen sale de la mayor línea mostrada, de una etiqueta o de una edición (T07.45, en las dos direcciones). La ventana de una línea larga se centra en la primaria y se ensancha a otra etiqueta si juntas caben en `roomForLabels`, 120 bytes (T07.96).
- **E0107.** `digitRun` devuelve el span de los `_` que cierran una tirada, y hay un `Delete` por tirada (`1_.5_e3_` tiene tres).
- **E0103** inserta un `*/` por nivel abierto: con comentarios anidados, uno solo dejaría el mismo error. Si el fichero acaba en una `/` suelta (`/* see a/`), la edición empieza con un espacio, porque `/*/` abriría otro comentario.
- **E0108 sin cerrar** ofrece la comilla solo tras un carácter exacto (o tras una barra impar, sin edición): tras `'ab` o un `'` suelto daría otro E0108.
- **E0108 con varios caracteres** propone la cadena, salvo que lleve una `"` o un `${` sin escapar (acabaría la cadena o abriría una interpolación): entonces la ayuda dice qué escapar y no hay edición. La cadena lleva el texto tal como está escrito, y `'ab\"'` sí tiene edición. La ayuda solo cita textos de hasta 80 bytes, con la `visible` de `lexer.go`; la edición lleva el texto entero.
- **`danglingBackslash`** quita la edición cuando la línea acaba en una barra impar, que escaparía la comilla.
- **E0110** pone la comilla al final de la línea, y su ayuda lo dice, salvo dentro de un `${` abierto.
- **E0109** pide un exponente menor si lo hay y lo de delante cabe; si no, «use a smaller value».
- **`endsInComment`.** Si un `//` o un `/*` sin cerrar acaba el fichero, el cierre de un E0202 quedaría comentado, y va tras el último token. Si el cierre cae en un literal sin cerrar o en su final (**`inUnterminatedLiteral`**), solo hay ayuda: dónde va depende de cómo acabe el literal.
:::

**El parser:** los E02xx; el cierre que falta va tras el último token (`prev`).

{{diff ch07-s3..ch07-s4 internal/syntax/ast.go}}

{{diff ch07-s3..ch07-s4 internal/syntax/parser.go}}

{{diff ch07-s3..ch07-s4 internal/syntax/parser_expr.go}}

{{diff ch07-s3..ch07-s4 internal/syntax/parser_stmt.go}}

{{diff ch07-s3..ch07-s4 internal/syntax/recover.go}}

**`sema`:** los E03xx.

{{diff ch07-s3..ch07-s4 internal/sema/resolve.go}}

{{diff ch07-s3..ch07-s4 internal/sema/suggest.go}}

:::more El parser y `sema`, función a función
- **`prev`** sustituye al campo `end` por el último token entero: el cierre que falta va tras él, no ante el token hallado (con `(1⏎}`, tras el `1`). No cuenta los `Newline`, cuyo final es el principio de la línea siguiente (T07.78).
- **`expectedWith`** es `expected` con un closure que decora el diagnóstico antes de informarlo, y solo si la supresión lo deja pasar.
- **`closerIsMissing`** es la lista de tokens del pliegue de los códigos, en la teoría; la secundaria sobre el delimitador abierto sale siempre (T07.91).
- **`OneLine`.** Dentro de corchetes un salto de línea es un espacio, así que un atributo puede ocupar tres líneas; borrarlo con un fix-it cruzaría saltos, y entonces solo hay ayuda.
- **`namedErrorItem`.** Si la firma falla tras el nombre, el `ItemError` lo conserva (T06.12, en el diff de `parser_test.go`).
- **`chainedDiag`** mira los tokens alrededor del cursor sin leer el resto de la cadena, para que el E0203 salga antes que lo que la cadena informe después (T06.48). T06.38 comprueba la ayuda y la edición; las dos, en el diff de `parser_expr_internal_test.go`.
- **`initializing`** es una pila de los `let` cuyo inicializador se recorre: un nombre desconocido que es el de uno de ellos apunta al más interior (T07.80).
- **`broken`** guarda los nombres que conservan los `ItemError`, que no son `Def`: `lookupDef`, que ahora devuelve un `Res`, los da como `ResError` sin informar, tras las definiciones del módulo y antes del preludio. Así llamar a una función de firma rota no suma un E0301 (T07.92).
- **E0305** usa `suggest` con `extern` como único candidato solo en la biblioteca estándar; en tu fichero, donde `extern` es E0304, la ayuda es borrar el atributo.
- **E0304** en una función sin cuerpo no trae la edición que lo borra: sin él faltaría el cuerpo, y la ayuda pide las dos cosas.
- **`maxSuggestLen`.** La distancia cuesta el producto de los dos largos: pasados 128 bytes, no hay ayuda (T07.93). Un nombre de 60 000 bytes frente a una función de otros tantos tardaba unos cuatro segundos en `ch07-s3`.
- **`FuzzAnalyze`** es el `FuzzParse` de los pases que siguen al parser (T07.94).
:::

**El catálogo y su test:**

{{file ch07-s4 docs/errors/E0301.md}}

{{file ch07-s4 tests/errors_docs_test.go}}

**`checks_catch_bad_catalogs`** da a cada comprobación un catálogo malo y exige que lo rechace: un test que solo ve catálogos buenos pasaría aunque no comprobara nada.

:::more El formato del catálogo y su test, función a función
**El formato** es el del comentario al principio del test: `# E0301: unknown name`, un texto, `## Erroneous example` con una valla `ki erroneous`, `## Corrected` con `ki corrected`, y las palabras `escaped`, `run` (para `ki run`, que llega en el capítulo 10) y `also=`. Los ejemplos pasan por `driver.Main` con `--diagnostics=json`.

**`escaped`** dice que el bloque es un literal de cadena de Go: así `E0101.md` escribe `"…// caf\xE9\n"`, con un byte que un fichero UTF-8 no puede llevar, y `E0102.md`, un `\x01`. En Python, `ast.literal_eval(r'"caf\xE9"')` da `'café'`: allí `\xE9` es un carácter, en Go un byte.

**`also=E0201`.** Cada código listado tiene que aparecer. Hoy una cadena, un carácter o un flotante no pueden ir en una expresión, así que los ejemplos de E0106, E0108 y E0109, y el corregido de E0110, dan además un E0201.

**`codesIn`.** `token.NewFileSet()` guarda las posiciones de `parser.ParseFile`, y `SkipObjectResolution` omite ligar nombres. Cada llamada a `diag.E` o `diag.W` tiene que llevar un literal entero (otro argumento es un error del test), que `strconv.ParseUint(lit.Value, 0, 16)` lee: `0x12D` vale, `70000` no cabe.

**`docFileName`.** La expresión regular queda para los nombres de fichero: `docFileName` compila con `regexp.MustCompile` el patrón `^([EW][0-9]{4})\.md$` (roto, haría panic al arrancar el test: `missing closing )`), y `FindStringSubmatch` da `["E0301.md" "E0301"]` o `nil`.

**El resto:**

- `parseDoc` es un bucle con tres estados, constantes de un `const` local con `iota`: texto, una valla `ki`, que es un ejemplo, y otra valla, que no se lee.
- `runExample` escribe cada ejemplo en `t.TempDir()` y lo pasa por `driver.Main`, como los golden; `exampleArgs` elige `check` o `run`; `outcomeOf` lee el stderr, y `quiet`, si calló.
- Las comprobaciones devuelven sus problemas como texto, sin `t.Errorf`, para que `checks_catch_bad_catalogs` pueda probarlas. `shapeProblems` mira títulos y secciones; `exampleProblems`, la salida de cada ejemplo frente a su código y su `also=`.
- `sourceCodes` lee `internal/` con `codesUnder`: `filepath.WalkDir` y `codesIn` en cada `.go` que no es de test; `readCatalog` lee `docs/errors`; `catalogProblems` compara los dos conjuntos en los dos sentidos y ordena lo que encuentra, y `sortedCodes` da los ficheros por código: ningún fallo sale en el orden de un map.
:::

**Escribe el golden de T07.90:**

{{file ch07-s4 tests/errors/ch07/unclosed_paren_render.ki}}

{{reftest T07.39}}

{{reftest T07.43}}

Compárate con los tests de `diag` y `sema`:

{{diff ch07-s3..ch07-s4 internal/diag/render_test.go}}

{{diff ch07-s3..ch07-s4 internal/sema/resolve_test.go}}

{{diff ch07-s3..ch07-s4 internal/sema/scope_test.go}}

{{diff ch07-s3..ch07-s4 internal/sema/suggest_test.go}}

{{diff ch07-s3..ch07-s4 internal/sema/suggest_internal_test.go}}

{{file ch07-s4 internal/sema/analyze_fuzz_test.go}}

Con el código escrito y `ki` recompilado:

1. **Escribe los ocho golden nuevos de `diag`** con `go test ./internal/diag -update`, y léelos contra las reglas; `wantGoldens` pasa a 22.
2. **Reescribe los dos golden que cambian de salida,** `tests/errors/ch03/invalid_utf8_render.ki` y `tests/errors/ch07/unknown_name_render.ki` (T07.36), con `build/bin/ki test --update tests/errors` dos veces: la primera reescribe `// expect-stderr:`, que gana líneas y empuja el programa hacia abajo; la segunda graba la línea nueva del error.

Si te atascas o tu versión se desvía demasiado, copia el fichero de `ref/ch07-s4` y sigue.

Al terminar: tus tests en verde, commit y etiqueta `ch07-s4` (mensaje: `ch07-s4: rustc-style diagnostics: fix-it suggestions, the bar for E01xx–E03xx and the docs/errors catalog`); compárate con `ref/ch07-s4`.

## Ejecútalo

Con `ki` construido (`go build -o build/bin/ki ./cmd/ki`), el `fib.ki` del objetivo da exactamente lo de arriba y sale con 1. Un fichero con cuatro errores:

```console
$ cat bad.ki
#[extrn("x")]
fn twice(n: Int) -> Int { (n + n }

fn main() {
  let total = total + 1
  prnt(twice(2))
}
$ build/bin/ki check bad.ki
error[E0305]: unknown attribute `extrn`
 --> bad.ki:1:1
  |
1 | #[extrn("x")]
  | ^^^^^^^^^^^^^ not an attribute Ki knows
  = note: a program cannot use attributes yet
  = help: remove it
  |
1 - #[extrn("x")]
1 +

error[E0201]: expected `)`, found `}`
 --> bad.ki:2:34
  |
2 | fn twice(n: Int) -> Int { (n + n }
  |                                  ^ expected `)` here
  |                           - unclosed `(` opened here
  = help: close it with `)`
  |
2 - fn twice(n: Int) -> Int { (n + n }
2 + fn twice(n: Int) -> Int { (n + n) }

error[E0301]: unknown name `total`
 --> bad.ki:5:15
  |
5 |   let total = total + 1
  |               ^^^^^ not found in this scope
  |       ----- `total` is bound by this `let`, which cannot see itself
  = note: a `let` is visible only after its own statement

error[E0301]: unknown name `prnt`
 --> bad.ki:6:3
  |
6 |   prnt(twice(2))
  |   ^^^^ not found in this scope
  = help: did you mean `print`?
  |
6 -   prnt(twice(2))
6 +   print(twice(2))
$ echo $?
1
```

El E0301 de `total` no tiene ayuda: ningún candidato está a una edición. En JSON, el del hito:

```console
$ build/bin/ki check --diagnostics=json fib.ki
{"severity":"error","code":"E0301","message":"unknown name `fibb`","path":"fib.ki","line":2,"col":25,"labels":[{"primary":true,"line":2,"col":25,"message":"not found in this scope"}],"notes":[],"help":"did you mean `fib`?","fixits":[{"line":2,"col":25,"end_line":2,"end_col":29,"replacement":"fib"}]}
```

## Ejercicios

:::exercise
**1. `W0301`, una variable sin usar.** Añade el warning `` unused variable `x` `` para un `let` que nadie lee, al nivel del listón: primaria «never used», ayuda «prefix it with `_` to keep it, or delete the `let`», el fix-it del prefijo, tests y su `docs/errors/W0301.md`.
:::

:::solution
`diag` gana `NewWarning`, igual que `NewError` con `Severity: Warning`. El resolvedor lleva `used []bool` (crece en `bind`, se marca en `name` cuando un nombre es `ResLocal`), `lets []LocalID` (las variables que `stmt` añade tras el `bind` de cada `let`; `function` lo vacía al empezar y lo recorre al acabar con `unused`) y `unusedLets []diag.Diagnostic`, los warnings pendientes; `resolve.go` importa `strings`:

```go
func (r *resolver) unused(l LocalID) {
	loc := r.rp.Locals.Get(l)
	name := r.in.String(loc.Name)
	if r.used[l] || strings.HasPrefix(name, "_") {
		return
	}
	r.unusedLets = append(r.unusedLets, diag.NewWarning(diag.W(301), "unused variable `"+name+"`",
		diag.Label{Span: loc.Span, Message: "never used"}).
		WithHelp("prefix it with `_` to keep it, or delete the `let`").
		WithFixIt(diag.Insert(loc.Span, "_")))
}
```

`unused` no informa: guarda el warning en `unusedLets`, y al final de `Resolve` se informan solo `if !sink.HasErrors()`, porque junto a un error una variable puede parecer sin usar solo porque la línea que la leía está rota. Nada más compilar, `TestErrorDocs` exige el fichero: `the source builds W0301, and docs/errors/W0301.md does not exist`. Escríbelo con `let total = 1` y `print(2)` como erróneo y `let _total = 1` como corregido, y cambia los corregidos de E0201 y E0301, `fn main() { let x = 1 }`, por `fn main() { let x = 1; print(x) }`: ahora darían el warning. El test, en `TestScopes`, con seis filas (sin usar; dos `let x` con la primera sin leer; leída; `_total`; un parámetro; y un programa con un error), compara el warning entero con `reflect.DeepEqual` (importa `reflect`). Sin la guarda de `HasErrors`, falla la última fila; sin la regla del `_`, la cuarta. `ki check` sale con 0: un warning no es un error.
:::

:::exercise
**2. Un local antes que una definición.** Haz que, a igual distancia, la sugerencia prefiera una variable a una función, con test.
:::

:::solution
En `suggest.go`, y `name` llama a `r.suggestion(name)` en lugar de `suggest(name, r.candidates())`:

```go
func (r *resolver) suggestion(name string) (string, bool) {
	best, ok := suggest(name, r.candidates())
	if !ok {
		return "", false
	}
	var locals []string
	for sym := range r.scope.visible() {
		locals = append(locals, r.in.String(sym))
	}
	if local, ok := suggest(name, locals); ok && base.EditDistance(name, local) == base.EditDistance(name, best) {
		return local, true
	}
	return best, true
}
```

`best` ya es el más cercano de todos, así que un local solo gana si empata. Cambia la fila `function first in the alphabet` de T07.32, que ahora espera `gammb`, y su comentario. El subtest nuevo, `local_wins_a_tie`, pide `alphb` (parámetro) frente a `alpha` (función), `prinx` (un `let`) frente a `print`, y `counter` (función a una edición) frente a `countxx` (parámetro a dos): la distancia sigue mandando. Sobre el código de `ch07-s4` falla con `help "did you mean `alpha`?", want "did you mean `alphb`?"`.
:::

:::exercise
**3. Determinismo con un map debajo.** Escribe un test de que las sugerencias son deterministas aunque los candidatos pasen por un map.
:::

:::solution
En `TestSuggestions` (añade `fmt` a los imports):

```go
	t.Run("deterministic_with_ties", func(t *testing.T) {
		var src strings.Builder
		for c := 'a'; c <= 'p'; c++ {
			fmt.Fprintf(&src, "fn k%c() {}\n", c)
		}
		src.WriteString("fn main() {\n  kz()\n}\n")
		for range 50 {
			suggestionIn(t, "sixteen ties", src.String(), "  kz()", "kz", didYouMean("ka"))
			if t.Failed() {
				return
			}
		}
	})
```

Dieciséis funciones a una edición de `kz`, y cada análisis crea su map con su semilla. Pasa sin cambios. Lo instructivo es qué lo rompe: quitar solo el `slices.Sorted` de `candidates` no, ni quitar solo el desempate `c < best` de `suggest`; cada protección basta. Sin las dos, falla en cada ejecución con otra letra.
:::

:::exercise
**4. `ki emit names FILE`.** Un destino de `emit` que liste cada `Def` y cada `Local` con su span.
:::

:::solution
`runEmit` gana `case "names": return emitNames(s, c, args[1:])`, y la línea de uso pasa a `ki emit tokens|ast|names [--diagnostics=human|short|json] FILE` en `commands.go` y en los tres sitios de `cli_test.go` que la escriben; `emit.go` importa `strings` y `base`:

```go
	s.Analyze(f)
	rp := f.Resolved
	where := func(sp base.Span) string {
		lo, hi := s.Sources.LineCol(sp.File, sp.Lo), s.Sources.LineCol(sp.File, sp.Hi)
		return fmt.Sprintf("%s:%d:%d-%d", s.Sources.DisplayPath(sp.File), lo.Line, lo.Col, hi.Col)
	}
	var b strings.Builder
	for id, d := range rp.Defs.All() {
		fmt.Fprintf(&b, "def %d %s %s\n", id, s.Interner.String(d.Name), where(d.Span))
	}
	for id, l := range rp.Locals.All() {
		owner := rp.Defs.Get(l.Owner).Name
		fmt.Fprintf(&b, "local %d %s %s (%s)\n", id, s.Interner.String(l.Name), where(l.Span), s.Interner.String(owner))
	}
	return finishEmit(s, c, b.String())
```

Antes, `fileArgs` y `LoadAndParse` como en `emitAST`. Para `fib.ki` da cinco líneas, de `def 0 print std/prelude.ki:3:8-13` a `local 1 n fib.ki:1:8-9 (fib)`, y luego el E0301. El test, `TestCLI/emit_names`, llama a `t.Chdir(t.TempDir())` (Go 1.24: cambia de directorio y lo restaura con `Cleanup`) para que la ruta mostrada sea `a.ki`, y compara la salida entera.
:::

:::exercise
**5. `ki check --fix FILE`.** Aplica a una copia del fichero todos los fix-its de la primera comprobación, de derecha a izquierda, y vuelve a comprobarla; con test de que `fibb` se convierte en `fib` y de que dos fix-its en una línea no se pisan.
:::

:::solution
`fileArgs` gana el parámetro `more func(*flag.FlagSet)` (`emit` pasa `nil`) y `check` registra `fs.BoolVar(&fix, "fix", false, …)`; la línea de uso pasa a `ki check [--fix] [--diagnostics=human|short|json] FILE`, también en `cli_test.go`. Tras `Analyze`, `checkFixed` junta los fix-its del fichero que compilas, aplica, imprime la copia por stdout y la comprueba en una `Session` nueva, cuyo diagnóstico y código de salida son los que cuentan. El fichero del disco no cambia. `check.go` importa `cmp`, `flag`, `slices` y `diag`, e `io` si imprimes con `io.WriteString`.

```go
func applyFixits(src []byte, fixits []diag.FixIt) []byte {
	fixits = slices.Clone(fixits)
	slices.SortStableFunc(fixits, func(a, b diag.FixIt) int { return cmp.Compare(b.Span.Lo, a.Span.Lo) })
	out := slices.Clone(src)
	limit := uint32(len(src))
	for _, f := range fixits {
		if f.Span.Hi > limit {
			continue // overlaps one already applied
		}
		out = slices.Replace(out, int(f.Span.Lo), int(f.Span.Hi), []byte(f.Replacement)...)
		limit = f.Span.Lo
	}
	return out
}
```

`slices.Replace` sustituye `out[i:j]` por los bytes dados. De derecha a izquierda, cada edición trabaja sobre posiciones que ninguna otra ha movido todavía. El test, `TestCLI/check_fix`, usa el `fib` del objetivo con un `main` que dice `prnt(fibb(20))`: espera la copia con `fib` y `print(fib(20))`, código 0, stderr vacío y el fichero intacto. Ordenadas de izquierda a derecha y sin la guarda, el `fibb` → `fib` de la línea 2 acorta el texto un byte, así que `prnt` → `print` cae un byte a la derecha: `pprintfib(20))`.
:::

## Preguntas de repaso

:::review
¿Por qué un `let` liga su nombre después de resolver el inicializador y no antes?
:::

:::answer
Porque así `let x = x + 1` lee la `x` de fuera, que es lo que quien lo escribe quiere, y `let x = x` sin otra `x` es un error en el origen. Si ligara antes, la `x` de la derecha sería la propia variable, todavía sin valor.
:::

:::review
¿Por qué las ayudas de «did you mean» no pueden depender del orden de un map, si el map ya es aleatorio y el resultado «casi siempre» sale igual?
:::

:::answer
Porque «casi siempre» es un test que falla en la CI una de cada pocas veces y una ayuda que cambia entre dos ejecuciones del mismo fichero. Con empates, el primero que llega depende de la semilla; el desempate alfabético y la lista ordenada lo fijan.
:::

:::review
¿Por qué `RenderHuman` hace panic ante un fix-it sin ayuda, en vez de pintarlo sin más?
:::

:::answer
Porque es un fallo del pase que construyó el diagnóstico, no del programa que se compila: un parche sin la frase que dice qué hace no es una sugerencia. Un panic con el motivo lo hace visible en el primer test que lo pinta, también en `FuzzParse` y `FuzzAnalyze`, que renderizan cada diagnóstico.
:::

:::review
¿Por qué E0203 solo ofrece la edición cuando el operando central es un token?
:::

:::answer
Porque la edición copia ese operando a la derecha del `&&`. Un nombre o un número se pueden copiar sin cambiar nada; `f(x)` o un bloque se evaluarían dos veces, con sus efectos.
:::

:::review
`let a = g(1` seguido de una línea `h(2)` da el E0201 con su secundaria, sin ayuda ni edición, aunque faltaba un `)`. ¿Es un fallo del listón?
:::

:::answer
No: el listón pide la edición solo cuando es mecánica. El token hallado es `h`, un nombre, que también seguiría a una coma olvidada, `g(1, h(2))`; un `)` arreglaría un caso y estropearía el otro, como en `print(1 2)`. rustc lo resuelve con niveles de confianza (`Applicability`, en «Go en el trabajo»); Ki, callando.
:::

:::review
¿Qué gana el catálogo con ejecutar sus ejemplos, si un texto bien revisado ya los explica?
:::

:::answer
Que no envejecen en silencio: si un código cambia de mensaje o desaparece, o si el parser empieza a admitir cadenas en una expresión, la CI falla en esa sesión. La palabra `also=` está hecha para eso: hay que quitarla en cuanto el E0201 deje de salir.
:::

## Resumen

- Un map es un descriptor de una tabla: copiarlo es compartirla.
- Nada visible depende del orden de un map: `slices.Sorted(maps.Keys(m))` y desempates explícitos.
- `//go:embed stdlib` mete el preludio en el binario; por eso `embed.go` está en la raíz.
- La distancia de edición es programación dinámica con dos filas.
- Resolver son dos pasadas: definiciones primero, cuerpos después. Los ámbitos son maps encadenados y lo resuelto va en tablas laterales por id.
- Una sugerencia está al alcance con `max(1, len/3)` ediciones; los empates, por el alfabeto.
- Un diagnóstico tiene primaria con texto, secundarias, nota, ayuda concreta y fix-it si es mecánico; una ayuda genérica es peor que ninguna.
- Los fix-its usan las posiciones originales y se muestran como el diff de rustc.
- `docs/errors` tiene una entrada por código, con ejemplos que `TestErrorDocs` ejecuta; `go/parser` lo sincroniza con el fuente.

## Go en el trabajo

**Un map o un slice ordenado.** Una clave string hay que hashearla y compararla byte a byte: por eso el compilador busca por `Symbol`, no por texto. Una búsqueda en orden salteado, en esta máquina:

| Búsqueda | Clave `string` | Clave `uint32` |
|---|---|---|
| `map`, 8 claves | unos 8 ns | 4,3 ns |
| `map`, 1 000 claves | 11 ns | 6,8 ns |
| `map`, 100 000 claves | 24 ns | 13,5 ns |
| `slices.BinarySearch` sobre un slice ordenado | perdió en los tres tamaños | solo ganó con 8 claves |

**La iteración aleatoria en CI.** Un test que compara `"fib main print"` con las claves de un map, sin ordenar, pasa en tu portátil y falla en la CI una vez de cada cuatro. `go test -count=100` lo destapa en segundos.

**Los mensajes como producto.** La guía de sugerencias de la *rustc-dev-guide* va más allá que Ki: pide evitar «did you mean» («Suggestions should not be a question»). Cada sugerencia lleva su `Applicability`, de `MachineApplicable` a `MaybeIncorrect`, que Ki no distingue todavía.

**`go vet` señala.** Sobre `fmt.Printf("%d\n", name)` con un `string`, dice `main.go:7:14: fmt.Printf format %d has arg name of wrong type string`: qué y dónde, sin ayuda. En Go 1.27, `go vet -fix` aplica el arreglo que un analizador ofrece; para el `%d` no hay ninguno: ninguna edición adivina si sobra el verbo o el argumento.

:::more Las guías de rustc y el arreglo de `go vet`
La guía de estilo de la *rustc-dev-guide* («Diagnostic output style guide») dice que los mensajes «start with a lowercase letter and do not end with punctuation». La de sugerencias prefiere «there is a struct with a similar name: `Foo`».

`go vet -fix -diff` enseña el arreglo como parche sin tocar el fichero. Para `fmt.Printf(s)` es `fmt.Printf("%s", s)`.
:::
