---
chapter: 7
title: Nombres, ámbitos y errores al nivel de rustc
part: I
hours: 9
milestone: `unknown name 'fibb'; did you mean 'fib'?`, con el fix-it que lo sustituye; los fix-its se muestran como sugerencias de rustc; todos los códigos E01xx–E03xx al nivel del apartado 7.3 y el catálogo `docs/errors` con sus ejemplos probados
---

## Objetivo

Al terminar, `ki check` sabe qué significa cada nombre de un programa: si es un parámetro, un `let` o una función, y cuál. Cuando no significa nada, lo dice como lo diría rustc, con el arreglo escrito:

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

Las dos últimas líneas son un *fix-it*: la línea tal cual y la línea arreglada. La sesión 4 pone al mismo nivel los diecinueve códigos que existen y crea `docs/errors`, un catálogo cuyos ejemplos se ejecutan en cada CI. Es el hito del capítulo, camino de M1 (capítulo 10).

## Teoría

### Maps a fondo

El capítulo 4 enseñó lo básico de `map[K]V`: literal, `make`, búsqueda con dos resultados, un `nil` que se lee y no se escribe, y un `range` cuyo orden cambia. Falta lo que importa a un resolvedor de nombres:

- **Semántica de referencia.** Un valor de tipo map es un descriptor de una tabla que vive aparte. Pasarlo a una función o asignarlo a otra variable copia el descriptor, no la tabla: con `m := map[string]int{"a": 1}` y una `add(m)` que hace `m["b"] = 2`, `len(m)` vale 2 al volver.
- **Claves comparables.** La clave tiene que admitir `==`: números, strings, punteros, structs de campos comparables. `map[[]byte]int` no compila: `invalid map key type []byte`.
- **`clear(m)`** vacía el map (`len` 0, sigue sin ser `nil`).
- **El orden.** Ninguna salida del compilador depende del orden de un `range` sobre un map (spec 7.2): lo que se enseña se ordena antes, por id o por nombre. Un test que lo olvida falla a veces: en esta máquina, uno que compara `"fib main print"` con las claves de un map de tres, sin ordenar, falló 2 436 de 10 000 veces.
- **Tablas suizas.** Desde Go 1.24, el map es «a new builtin `map` implementation based on Swiss Tables»: grupos de 8 casillas con una palabra de control. El orden aleatorio no cambió.

:::from Rust
`HashMap` pide `Eq + Hash`, y `Vec<u8>` los cumple: `HashMap<Vec<u8>, i32>` compila, mientras que en Go un slice nunca es clave. Y en Go nadie es dueño del map: copiar la variable comparte la tabla, algo que en Rust pediría un `Rc<RefCell<HashMap<…>>>`.
:::

### `maps`, `slices` e `iter.Seq`

`maps.Keys(m)` devuelve un `iter.Seq[K]` (el tipo de iterador del capítulo 4) cuyo orden, dice su documentación, «is not specified»; `slices.Sorted` lo recoge en un slice nuevo y lo ordena. `slices.Sorted(maps.Keys(set))` es, desde este capítulo, la forma de sacar de un map una lista que no dependa del azar.

Los ámbitos escriben su propio iterador. `visible` devuelve `func(yield func(base.Symbol) bool)`: recorre la cadena de ámbitos de dentro afuera, ordena los nombres de cada uno por `Symbol` con `slices.Sorted(maps.Keys(…))` y para en cuanto `yield` devuelve `false`. Quien lo usa escribe `for sym := range sc.visible()`, sin saber que hay maps debajo.

:::from Python
`sorted(d)` es la misma idea; pero un `dict` ya conserva el orden de inserción, y por eso en Python el olvido no se nota hasta que alguien construye el diccionario en otro orden.
:::

### `//go:embed` y el preludio

`print` no lo define tu fichero: lo declara `stdlib/prelude.ki`, que viaja dentro del binario de `ki`. La directiva `//go:embed stdlib`, sobre una variable `embed.FS`, hace que `go build` copie el directorio en el ejecutable, y `fs.ReadFile(Stdlib, "stdlib/prelude.ki")` lo lee en tiempo de ejecución.

Por qué `embed.go` vive en la raíz del módulo, en el paquete `kilang`: la documentación de `embed` dice que los ficheros se leen «from the package directory or subdirectories». Desde `internal/driver`, `//go:embed ../../stdlib` da `pattern ../../stdlib: invalid pattern syntax`, y `//go:embed stdlib` busca `internal/driver/stdlib`: `pattern stdlib: no matching files found`. El único paquete que tiene `stdlib/` debajo es el de la raíz. La ruta dentro del `FS` conserva el directorio: `fs.ReadFile(Stdlib, "prelude.ki")` da `file does not exist`.

:::from Rust
`include_str!("prelude.ki")` hace lo mismo con un fichero, en tiempo de compilación, y admite rutas con `..`. Go pide que el fichero cuelgue del paquete, y a cambio da un sistema de ficheros entero (`embed.FS`) que se recorre como cualquier `fs.FS`, la interfaz de `io/fs`, con un solo método, `Open`.
:::

### La distancia de edición con dos filas

`fibb` está a una edición de `fib`: borrar una `b`. La **distancia de Levenshtein** es el menor número de inserciones, borrados y sustituciones que convierten una cadena en otra. Es programación dinámica: la casilla `[i][j]` de una tabla es la distancia entre los `i` primeros bytes de `a` y los `j` primeros de `b`, y sale de tres vecinas:

```text
d[i][j] = min(d[i-1][j] + 1,                       // delete a[i-1]
              d[i][j-1] + 1,                       // insert b[j-1]
              d[i-1][j-1] + (a[i-1] != b[j-1]))    // keep or replace
```

La fila 0 vale `j` (construir `j` bytes desde nada) y la columna 0, `i`. Como cada fila solo mira la anterior, bastan dos slices de `len(b)+1` enteros que se intercambian con `prev, cur = cur, prev`. Se cuentan bytes: `é` y `e` están a distancia 2.

### Tablas de símbolos, ámbitos y sombreado

Resolver nombres es decidir, para cada `ExprName` del árbol, a qué se refiere. Las piezas:

- **Definiciones** (`Def`, numeradas por `DefID`): cada función de cada módulo. El preludio es el módulo 0 y el fichero, el 1. Una búsqueda mira primero las del módulo y luego las `pub` del preludio, así que tu `print` tapa el de la biblioteca sin ser una función repetida (E0302).
- **Variables locales** (`Local`, por `LocalID`): cada parámetro y cada `let`, también dos `let x` seguidos, que son dos variables.
- **Ámbitos**: un map de nombres a `LocalID` con un puntero al ámbito de fuera. Los parámetros son uno; cada bloque, otro dentro. Buscar es subir por la cadena; el primer ámbito que tiene el nombre gana, y eso es el **sombreado**. Un `let` liga su nombre *después* de resolver su inicializador: en `let x = x`, la `x` de la derecha es la de fuera.
- **Tablas laterales**: lo que se aprende no se escribe en el árbol (los pases no mutan su entrada), sino en slices indexados por id (`ExprRes[ExprID]`, `LetLocal[StmtID]`, `ParamLocals[ItemID]`), con una fila por nodo.
- **Dos pasadas**: la primera recoge todas las definiciones; la segunda recorre los cuerpos. Así `main` puede llamar a una función que se define más abajo.
- **Sugerencias deterministas**: los candidatos de un «did you mean» se juntan en un map (para no repetir un nombre), se ordenan, y los empates a la misma distancia los gana el primero en el alfabeto.

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

Y uno de Elm 0.15.1, del artículo «Compiler Errors for Humans» de Evan Czaplicki, transcrito de su captura sin la cabecera:

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

Lo que tienen en común es la anatomía de un `Diagnostic`: un **mensaje** que nombra la cosa; una **etiqueta primaria** que dice qué está mal *en ese sitio*; **secundarias** en el otro sitio que explica el error (la primera definición, el paréntesis que quedó abierto); una **nota** con la regla cuando el mensaje no la hace evidente; una **ayuda** con una acción concreta; y, cuando la corrección es mecánica, un **fix-it**. Desde la sesión 4, todo código de Ki cumple ese listón (spec 7.3): fix-it siempre con su ayuda, una entrada en el catálogo y cada código nuevo completo desde que nace.

**Una ayuda genérica es peor que ninguna.** «check your syntax» no dice qué hacer y enseña a ignorar la línea de ayuda, también cuando sí sirve. Donde no hay nada concreto, Ki calla: el E0201 de una expresión que falta no tiene ayuda.

**Un fix-it es una edición `{span, replacement}`**: span vacío, inserción; reemplazo vacío, borrado. El renderizador humano lo enseña en la forma «diff» de rustc bajo la ayuda: una línea con la barra y, por cada línea tocada, `N - original` y `N + parcheada`, con las reglas de visualización del código (tabuladores, U+FFFD, ventana). rustc la usa cuando la edición borra algo y marca con `+` una inserción, como arriba; Ki usa siempre el par. Si una línea tiene dos ediciones, las dos se colocan con las posiciones de la línea **original**, que es lo mismo que aplicarlas de derecha a izquierda: en `x = fibb + gammaa`, cambiar `fibb` por `f` antes que `gammaa` correría tres bytes la segunda edición si se usara el texto ya cambiado. Un fix-it sin ayuda, uno en otro fichero, uno que cruza un salto de línea o dos que se solapan son errores del pase que los construyó: `RenderHuman` hace panic con el motivo.

**El catálogo es documentación ejecutable.** `docs/errors/E0301.md` explica el código y trae un ejemplo erróneo y uno corregido, en vallas `ki erroneous` y `ki corrected`. `tests/errors_docs_test.go` ejecuta cada ejemplo con `driver.Main` y `--diagnostics=json`: el erróneo tiene que dar su código y el corregido, salir limpio. Y lee el fuente como Go: `parser.ParseFile` (`go/parser`) da el árbol de un fichero (`token.NewFileSet()` guarda sus posiciones; `SkipObjectResolution` omite ligar nombres), y `ast.Inspect` lo recorre llamando a una función con cada nodo (si devuelve `false`, no baja a sus hijos). `codesIn` exige en cada llamada a `diag.E` o `diag.W` un literal entero (otro argumento es un error del test), que `strconv.ParseUint(lit.Value, 0, 16)` lee: `0x12D` vale, `70000` no cabe. Un comentario o una cadena no son llamadas; una expresión regular sobre el texto los contaría, y el fichero de un código quitado seguiría vivo. La expresión regular queda para los nombres: `docFileName` compila con `regexp.MustCompile` el patrón `^([EW][0-9]{4})\.md$` (roto, haría panic al arrancar el test: `missing closing )`), y `FindStringSubmatch` da `["E0301.md" "E0301"]` o `nil`. Cada código del fuente necesita su fichero y cada fichero, su código: un código nuevo sin entrada rompe la CI en la sesión que lo introduce.

Dos palabras más en la valla. `escaped` dice que el bloque es un literal de cadena de Go, que el test decodifica con `strconv.Unquote`: así `E0101.md` escribe `"…// caf\xE9\n"`, con un byte que un fichero UTF-8 no puede llevar, y `E0102.md`, un `\x01`. Y `also=E0201` lista los otros códigos que el ejemplo da: hoy una cadena, un carácter o un flotante no pueden ir en una expresión, así que los ejemplos de E0106, E0108 y E0109, y el corregido de E0110, dan además un E0201. Cada código listado tiene que aparecer.

:::from Python
`ast.walk(ast.parse(src))` da cada nodo, como `ast.Inspect` pero sin poda; y no ve comentarios. Y `ast.literal_eval(r'"caf\xE9"')` da `'café'`: en Python `\xE9` es un carácter, en Go un byte.
:::

**Código a código.** Los del resolvedor son E0301, nombre desconocido; E0302, función definida dos veces; E0304, `#[extern]` fuera de la biblioteca estándar; y E0305, atributo desconocido. E0303, sin `main`, llegará con `ki run` en el capítulo 10. La sesión 4 lleva al listón lo que ya existía:

| Qué cambia | Códigos |
|---|---|
| La primaria, que no tenía texto, lo gana | E0106 «unknown escape», E0201 «expected `)` here», E0203, E0204 «nesting level 257 starts here», E0205 |
| La primaria hacía de ayuda o hablaba del libro, y la ayuda pasa a su sitio | E0102, E0304, E0305 |
| Secundaria nueva | E0201 de un cierre («unclosed `(` opened here»), E0203, E0301 de `let x = x` |
| Nota nueva | E0101, E0102, E0104 (solo letras), E0109, E0301 de `let x = x`, E0304, E0305 |
| Ayuda sin edición | E0101, E0105, E0106 en un carácter, E0107 sin dígitos, E0108 vacío o con `"` o `${` sueltos, E0109, E0201 de `let`, E0202 cuyo cierre caería en un literal sin cerrar, E0204, E0205, E0302, E0304 sin cuerpo |
| Ayuda con edición | E0102, E0103, E0104, E0106 en una cadena, E0107, E0108 de varios caracteres sin `"` ni `${` sueltos, o sin cerrar tras uno, E0110 fuera de un `${` y sin barra impar al final, E0201 de un cierre que falta, E0202, E0203, E0301, E0304, E0305 |

Donde no hay edición es porque no sería mecánica: qué dígitos querías, qué nombre lleva tu variable, cuál de las dos funciones sobra. Y a veces la edición solo vale en un caso: `E0203` solo reescribe `a < b < c` como `a < b && b < c` si el operando central es un token; y el E0201 de un cierre solo lo escribe si el token hallado es `}`, `]`, `;`, `let` o el comienzo de un item, porque en `print(1 2)` falta una coma, no un `)`.

Los capítulos 3, 5 y 6 enseñaron la salida de antes (primarias sin texto, tarjetas «sin ayuda»). Siguen exactos para las etiquetas que citan, y por eso no se reescriben: llevan una fe de erratas que remite aquí, y sus tarjetas dicen lo que el código da ahora.

## Tests primero

{{cards}}

Úsalas como siempre: escribe el test de la tarjeta, míralo fallar, implementa lo justo para el verde, y solo entonces despliega la referencia con su `{{reftest}}`.

Los tests de `internal/sema` viven en dos paquetes. `resolve_test.go`, `scope_test.go` y `suggest_test.go` son `package sema_test`, porque usan `internal/testsupport`, que pasa un texto por el compilador entero y por eso importa `driver`, que importa `sema`: desde `package sema`, Go diría `import cycle not allowed in test`. `scope_internal_test.go` y `suggest_internal_test.go` son `package sema` y tocan `scope`, `suggest` y `resolver`, que no se exportan.

Las tarjetas describen el final del capítulo. T07.02, T07.05–T07.07, T07.20, T07.32, T07.33 y T07.36 cambian en la sesión 4, cuando sus diagnósticos ganan etiquetas, ayudas y ediciones; hasta entonces se escriben como en `git show ref/ch07-sK:book/tests/ch07.tests` y en el test de `ref/ch07-sK`, con la `K` de su sesión.

## Implementación

### Sesión 1: el preludio, `embed.go` y las definiciones (≈ 2 h)

#### Los tests, en rojo

T07.01–T07.17. La interfaz:

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

Copia `testsupport` entero, que es código de test:

{{file ch07-s1 internal/testsupport/compile.go}}

{{file ch07-s1 tests/errors/ch07/duplicate_definition.ki}}

Una función sin cuerpo pasa a necesitar `#[extern]`, así que los tests del capítulo 6 que la escribían sin él cambian:

{{diff ch06-s4..ch07-s1 internal/syntax/parser_test.go}}

{{diff ch06-s4..ch07-s1 internal/syntax/parser_stmt_internal_test.go}}

Sobre `ch06-s4` no compila: `embed_test.go:13:28: undefined: Stdlib`, `p.Session.ParseSource undefined`. Escribe ya, enteros, `embed.go`, `stdlib/prelude.ki` y `pipeline.go` (más abajo): son fontanería. Con un `Stdlib` vacío, `parseFiles` no encuentra el preludio y hace panic, y un `ParseSource` de mentira deja módulos vacíos que los tests indexan: los dos cortan el binario. Añade los tipos de `sema` y un `Resolve` que solo crea las tablas (`Modules` con `ExprRes`, `LetLocal` y `ParamLocals` de una fila por nodo). Tres subtests de la referencia, `duplicate_definition_E0302`, `extern_only_in_std_E0304` y `unknown_attribute_E0305`, leen `Sink.All()[0]` tras un `t.Errorf` y en rojo hacen panic: en tu versión, pon un `t.Fatalf` delante; con la referencia, `go test ./... -skip 'TestResolve/(duplicate_definition_E0302|extern_only_in_std_E0304|unknown_attribute_E0305)$'`. Fallan cinco subtests de `TestResolve` (el primero, `the program has 0 definitions, want 3 (the prelude's print and the two of the file)`), `TestPipeline/analyze_does_not_write`, `TestParser/bodyless_fn_needs_extern_E0201`, `TestParseStmt/e0201_sites` y el golden `duplicate_definition.ki`. `tables_are_indexed_by_id` ya pasa (las tablas son el stub), y `missing_main_is_not_a_resolve_error` también: un stub nunca encuentra `main`, y ese test solo dice algo junto a `main_found`.

Los ayudantes, al principio de `resolve_test.go` en `ref/ch07-s1`: `nameExprs` da los `ExprName` de un nombre; `onlyRes` y `userRes`, la resolución del único que hay; `defsOf`, las `Def` de un módulo; `wantCodes` compara los códigos de error con `testsupport.ErrorCodes`; `resolveSources` resuelve con un preludio escrito por el test.

#### El código

{{file ch07-s1 embed.go}}

{{file ch07-s1 stdlib/prelude.ki}}

{{file ch07-s1 internal/driver/pipeline.go}}

{{file ch07-s1 internal/sema/resolve.go}}

{{diff ch06-s4..ch07-s1 internal/syntax/parser.go}}

{{diff ch06-s4..ch07-s1 internal/driver/check.go}}

{{diff ch06-s4..ch07-s1 internal/driver/commands.go}}

{{diff ch06-s4..ch07-s1 internal/driver/emit.go}}

- **`parseFiles`** lexa el preludio y el fichero con un solo interner: el mismo nombre es el mismo `Symbol` en los dos, y eso es lo que deja a `print` encontrar su definición.
- **`collect`** es la primera pasada: una `Def` por función, también por la segunda de un nombre repetido, que da E0302 y deja que el nombre siga siendo la primera. `tops` son maps que solo se consultan, nunca se recorren.
- **`attributes`** decide la clase: `extern` fuera del preludio es E0304 y la función queda como `DefFn`; cualquier otro atributo es E0305.
- **`fileArgs`** ya no lee el fichero: devuelve la ruta, y cada comando llama a `LoadAndParse` (o a `load`, en `emit tokens`, que no lexa el preludio). `emit ast` no llama a `Analyze`: su volcado es de la sintaxis (T07.16).

{{reftest T07.03}}

{{reftest T07.05}}

El mensaje de la etiqueta: `ch07-s1: the prelude, embed.go and top-level definitions`.

Si te atascas o tu versión se desvía demasiado, copia el fichero de `ref/ch07-s1` y sigue.

Al terminar: tus tests en verde, commit y etiqueta `ch07-s1`; compárate con `ref/ch07-s1`.

### Sesión 2: ámbitos, variables locales y sombreado (≈ 2 h)

#### Los tests, en rojo

T07.18–T07.25. La interfaz exportada no cambia: `Locals`, `LetLocal` y `ParamLocals` empiezan a tener contenido. La interna:

```go
type scope struct { names map[base.Symbol]LocalID; parent *scope }
func (s *scope) bind(name base.Symbol, id LocalID)
func (s *scope) lookup(name base.Symbol) (LocalID, bool)
func (s *scope) visible() iter.Seq[base.Symbol] // for the suggestions of session 3
```

Sobre `ch07-s1` no compila: `scope_internal_test.go:30:13: undefined: scope`. Con los métodos devolviendo el valor cero y un `visible` que devuelve `func(yield func(base.Symbol) bool) {}` (con `nil`, el `range` haría panic), fallan `TestScope/visible_lists_innermost_first_once` (`pass 0: visible names [], want [1 4 5 7 2 6 8 3 9]`) y los siete subtests de `TestScopes`; ninguno hace panic, y `TestResolve` sigue en verde.

Los ayudantes de `scope_test.go`, en `ref/ch07-s2`, encuentran cosas en el texto: el fichero y un span (`userFile`, `spanIn`), el enésimo uso de un nombre (`useAt`), una función, sus parámetros o la variable de un `let` (`fnNamed`, `paramsOf`, `paramLocal`, `letNamed`), las variables del fichero (`userLocals`). Y comprueban: `wantLocal`, un uso `ResLocal` con su nombre, span y dueña; `userDiags`, exactamente n diagnósticos por posición; `checkUnknown` y `wantUnknown`, un E0301. `resolve_test.go` gana `resAt`, la resolución del enésimo nombre de un módulo. `firstN`, en `scope_internal_test.go`, corta un iterador tras n nombres.

#### El código

{{file ch07-s2 internal/sema/scope.go}}

{{diff ch07-s1..ch07-s2 internal/sema/resolve.go}}

- **`bind`** crea el map la primera vez: un ámbito sin variables no paga ninguno.
- **`function`** abre el ámbito de los parámetros y liga cada uno, también los de una función sin cuerpo; el cuerpo, un bloque, abre otro dentro. `bodies` recorre ahora las `Def` y no los items, para que cada variable tenga por dueña la definición que se recorre, también la segunda de un E0302.
- **`ExprBlock`** guarda el ámbito de fuera, abre uno y lo restaura al salir: lo que liga un bloque acaba con él, cola incluida.
- **`StmtLet`** resuelve primero el inicializador y liga después. Si inviertes las dos líneas, T07.20 ya no da el E0301 de `let x = x` (`error codes [], want [E0301]`), y en T07.21 la `y` de `let y = y + 1` es la nueva.
- **`name`** busca primero en los ámbitos y luego entre las definiciones, y si no hay nada informa E0301 y deja `ResError`, que dice a los pases de después que el nombre ya está explicado.

{{reftest T07.21}}

{{reftest T07.25}}

El mensaje de la etiqueta: `ch07-s2: scopes, locals and shadowing`.

Si te atascas o tu versión se desvía demasiado, copia el fichero de `ref/ch07-s2` y sigue.

Al terminar: tus tests en verde, commit y etiqueta `ch07-s2`; compárate con `ref/ch07-s2`.

### Sesión 3: «did you mean» y la comprobación de nombres (≈ 2 h)

#### Los tests, en rojo

T07.26–T07.38. La interfaz:

```go
func EditDistance(a, b string) int                          // package base
func suggest(name string, candidates []string) (string, bool) // package sema, unexported
func (r *resolver) candidates() []string
```

Sobre `ch07-s2` no compila: `undefined: EditDistance`, `undefined: suggest`. Con stubs que devuelven 0, `("", false)` y `nil`, fallan `TestEditDistance/basic`, los siete subtests de `TestSuggest`, dos de `TestSuggestions` y el golden `unknown_name_render.ki`, sin panic; `no_suggestion_when_far` ya pasa, porque «sin ayuda» es lo que pide. `TestSuggest` (en `suggest_internal_test.go`) usa un closure `want` que compara `suggest` con una fila, y en sus dos últimos subtests monta un `resolver` a mano. En `suggest_test.go`, `wantSuggestion` exige un solo E0301 con su ayuda, `suggestionIn` y `suggestionWith` analizan con el preludio de verdad o con uno propio, y `didYouMean` escribe la ayuda. El golden de T07.36, en su forma de esta sesión:

{{file ch07-s3 tests/errors/ch07/unknown_name_render.ki}}

#### El código

{{file ch07-s3 internal/base/edit_distance.go}}

{{file ch07-s3 internal/sema/suggest.go}}

{{diff ch07-s2..ch07-s3 internal/sema/resolve.go}}

- **El umbral** es `max(1, len(name)/3)`: una edición para un nombre corto, una más por cada tres bytes. `fibb` admite una; `zzzzzz`, dos, y ninguno de los candidatos del preludio está tan cerca.
- **El descarte por largo**: cada edición cambia el largo en uno como mucho, así que un candidato que difiere en más que el umbral no se mide. T07.37 lo comprueba contando asignaciones con `testing.AllocsPerRun`.
- **El desempate** `c < best` es lo que hace determinista la ayuda aunque los candidatos llegaran desordenados; `candidates` los ordena de todas formas, para que la lista misma sea estable (T07.31).
- **El freno**: una vez que el sink ha tirado un error (pasados los 100), no se buscan candidatos; la ayuda iría a un diagnóstico que nadie ve (T07.38).

{{reftest T07.26}}

{{reftest T07.30}}

El mensaje de la etiqueta: `ch07-s3: did you mean and name checking`.

Si te atascas o tu versión se desvía demasiado, copia el fichero de `ref/ch07-s3` y sigue.

Al terminar: tus tests en verde, commit y etiqueta `ch07-s3`; compárate con `ref/ch07-s3`.

### Sesión 4: diagnósticos al nivel de rustc y el catálogo (≈ 2,5 h)

#### Los tests, en rojo

T07.39–T07.96. La interfaz nueva:

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

El formato del catálogo es el del comentario al principio de `tests/errors_docs_test.go`: `# E0301: unknown name`, un texto, `## Erroneous example` con una valla `ki erroneous`, `## Corrected` con `ki corrected`, y las palabras `escaped`, `run` (para `ki run`, que llega en el capítulo 10) y `also=`.

Antes, lleva a su forma final los tests que ya tienes: las etiquetas ganan texto, y donde un test exigía «sin ayuda» o «sin nota», ahora exige la que hay. Escribe esos cambios desde las tarjetas T07.02, T07.05–T07.07, T07.20, T07.32, T07.33 y las nuevas. Las líneas que tocan tests de los capítulos 3, 5 y 6 (etiquetas con texto, `wantBadEscape` esperando «unknown escape», los ayudantes del E0203) tienen sus tarjetas en esos capítulos, reescritas por su fe de erratas, salvo T06.12 y T06.38, que reescribe esta sesión; en `internal/syntax` pesan tanto que se toman de estos diffs:

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

Los de `diag` y `sema`, casi todos nuevos, se escriben desde las tarjetas y se comparan al final.

Los ayudantes nuevos: `wantPanic` (en `render_test.go`) exige un panic con un texto exacto; `applyFixits` (en `lexer_test.go`) y `applyFixes` (en `parser_expr_internal_test.go`), el mismo código, aplican las ediciones para volver a lexar o parsear. En `parser_stmt_internal_test.go`, `expectedHere` escribe la primaria «expected <what> here»; `e0201Close` construye el E0201 de un cierre con secundaria, ayuda e inserción, y `e0201Opened`, el que solo lleva la secundaria; `wantCloser` compara con uno u otro el primer diagnóstico, que da `firstDiag`; `e0201Position` da el span del token hallado. En `recover_test.go` hacen lo mismo `primaryOf`, `recE0201Close`, `recE0201Opened`, `recE0201Let` (el `let` sin nombre) y `recE0205`. `chainedDiag` (en `parser_expr_internal_test.go`, y no el método del parser de ese nombre) y `recE0203` ganan la secundaria, la ayuda y la inserción del E0203 esperado. `checkUnknownSelf` y `checkUnknownWith`, en `scope_test.go`, comprueban un E0301 con secundaria y nota.

Sobre `ch07-s3` no compila: `internal/diag/diagnostic_test.go:107:8: undefined: Replace`, `p.prev undefined`, `undefined: closerIsMissing`, `undefined: diag.Delete`. Con stubs (las tres constructoras devuelven `FixIt{}`, `WithFixIt` devuelve `d`, `prev` devuelve `Token{}`, `closerIsMissing` devuelve `false` y `Attr` gana los dos campos), fallan 110 subtests de 17 funciones (sin el golden de T07.90), ninguno con panic; los ocho golden nuevos de `internal/diag/testdata` faltan (`reading testdata/fixit_replacement_diff.golden (run go test ./internal/diag -update to create it)`), y `no_trailing_spaces` cuenta 14 de 22. Escríbelos con `go test ./internal/diag -update` cuando el renderizador ya exista, no antes (congelaría la salida del stub), y léelos contra las reglas; `wantGoldens` pasa a 22. `TestErrorDocs` falla mientras `docs/errors` no existe (`cannot read the catalog: open ../docs/errors: no such file or directory`); con el catálogo escrito, pasa ya sobre el código de la sesión 3, porque los códigos existen. Escribe un fichero por código, los diecinueve, como `E0301.md` (más abajo), con `escaped` y `also=` donde la teoría dice; si te atascas, copia `docs/errors` de `ref/ch07-s4`. Los dos golden que cambian de salida, `tests/errors/ch03/invalid_utf8_render.ki` y `tests/errors/ch07/unknown_name_render.ki`, se reescriben con `build/bin/ki test --update tests/errors` dos veces: la primera reescribe el bloque `// expect-stderr:`, que gana líneas y empuja el programa hacia abajo; la segunda graba la línea nueva del error.

#### El código

{{file ch07-s4 internal/diag/diagnostic.go}}

{{diff ch07-s3..ch07-s4 internal/diag/render.go}}

- **`WithFixIt`** recorta con `slices.Clip` antes del `append`, como los demás `With`: dos copias del mismo diagnóstico no comparten hueco (T07.39).
- **`groupFixits`** comprueba las cuatro reglas y agrupa por línea; **`applyFixits`** (la de `render.go`) copia el texto hasta cada edición y salta su span, sobre las posiciones originales. El ancho del margen sale de la mayor línea mostrada, de una etiqueta o de una edición (T07.45, en las dos direcciones).
- **`showWindow`** y **`writeNumbered`** salen de lo que ya hacía el código de las etiquetas: el diff y el código usan la misma función y no pueden pintar distinto (T07.47). La ventana de una línea larga se centra en la primaria y se ensancha a otra etiqueta si juntas caben en `roomForLabels`, 120 bytes (T07.96).

{{diff ch07-s3..ch07-s4 internal/syntax/source_check.go}}

{{diff ch07-s3..ch07-s4 internal/syntax/token.go}}

{{diff ch07-s3..ch07-s4 internal/syntax/lexer.go}}

{{diff ch07-s3..ch07-s4 internal/syntax/numbers.go}}

{{diff ch07-s3..ch07-s4 internal/syntax/strings.go}}

- **E0103** inserta un `*/` por nivel abierto: con comentarios anidados, uno solo dejaría el mismo error. Si el fichero acaba en una `/` suelta (`/* see a/`), la edición empieza con un espacio, porque `/*/` abriría otro comentario.
- **E0107**: `digitRun` devuelve el span de los `_` que cierran una tirada, y hay un `Delete` por tirada (`1_.5_e3_` tiene tres).
- **E0108** sin cerrar ofrece la comilla solo tras un carácter exacto (o tras una barra impar, sin edición): tras `'ab` o un `'` suelto daría otro E0108. Con varios caracteres propone la cadena con el texto tal como está escrito, salvo que lleve una `"` o un `${` sin escapar (acabaría la cadena o abriría una interpolación): entonces la ayuda dice qué escapar y no hay edición; `'ab\"'` sí la tiene. La ayuda solo cita textos de hasta 80 bytes, con la `visible` de `lexer.go`; la edición lleva el texto entero. **`danglingBackslash`** quita la edición cuando la línea acaba en una barra impar, que escaparía la comilla. **E0110** pone la comilla al final de la línea, y su ayuda lo dice, salvo dentro de un `${` abierto. **E0109** pide un exponente menor si lo hay y lo de delante cabe; si no, «use a smaller value».
- **`endsInComment`**: si un `//` o un `/*` sin cerrar acaba el fichero, el cierre de un E0202 quedaría comentado, y va tras el último token. Si el cierre cae en un literal sin cerrar o en su final (**`inUnterminatedLiteral`**), solo hay ayuda: dónde va depende de cómo acabe el literal.

{{diff ch07-s3..ch07-s4 internal/syntax/ast.go}}

{{diff ch07-s3..ch07-s4 internal/syntax/parser.go}}

{{diff ch07-s3..ch07-s4 internal/syntax/parser_expr.go}}

{{diff ch07-s3..ch07-s4 internal/syntax/parser_stmt.go}}

{{diff ch07-s3..ch07-s4 internal/syntax/recover.go}}

- **`expectedWith`** es `expected` con un closure que decora el diagnóstico antes de informarlo, y solo si la supresión lo deja pasar.
- **`prev`** sustituye al campo `end` por el último token entero. El cierre que falta se escribe tras él y no ante el token hallado: con `(1⏎}`, tras el `1`. No cuenta los `Newline`, cuyo final es el principio de la línea siguiente (T07.78).
- **`OneLine`**: dentro de corchetes un salto de línea es un espacio, así que un atributo puede ocupar tres líneas; borrarlo con un fix-it cruzaría saltos, y entonces solo hay ayuda.
- **`namedErrorItem`**: si la firma falla tras el nombre, el `ItemError` lo conserva (T06.12).
- **`closerIsMissing`** es la lista de tokens de la teoría; la secundaria sobre el delimitador abierto sale siempre (T07.91).
- **`chainedDiag`** mira los tokens alrededor del cursor sin leer el resto de la cadena, para que el E0203 salga antes que lo que la cadena informe después.

{{diff ch07-s3..ch07-s4 internal/sema/resolve.go}}

`initializing` es una pila de los `let` cuyo inicializador se recorre: un nombre desconocido que es el de uno de ellos apunta al más interior (T07.80). E0305 usa `suggest` con `extern` como único candidato solo en la biblioteca estándar; en tu fichero, donde `extern` es E0304, la ayuda es borrar el atributo. En una función sin cuerpo, E0304 no trae la edición que lo borra: sin él faltaría el cuerpo, y la ayuda pide las dos cosas. `broken` guarda los nombres que conservan los `ItemError`, que no son `Def`: `lookupDef`, que ahora devuelve un `Res`, los da como `ResError` sin informar, tras las definiciones del módulo y antes del preludio. Así llamar a una función de firma rota no suma un E0301 (T07.92).

{{diff ch07-s3..ch07-s4 internal/sema/suggest.go}}

La distancia cuesta el producto de los dos largos: un nombre de 60 000 bytes frente a una función de otros tantos tardaba, en esta máquina, unos cuatro segundos en `ch07-s3`; ahora, pasados 128 bytes, no hay ayuda (T07.93).

{{file ch07-s4 docs/errors/E0301.md}}

{{file ch07-s4 tests/errors_docs_test.go}}

`runExample` escribe cada ejemplo en `t.TempDir()` y lo pasa por `driver.Main`, como los golden; `exampleArgs` elige `check` o `run`; `outcomeOf` lee el stderr, y `quiet`, si calló; `checks_catch_bad_catalogs` le da a cada comprobación un catálogo malo escrito a mano y exige que lo rechace, porque un test que solo ve catálogos buenos pasaría aunque no comprobara nada. Por eso las comprobaciones devuelven sus problemas como texto en vez de llamar a `t.Errorf`.

- **`parseDoc`** es un bucle con tres estados, constantes de un `const` local con `iota`: texto, una valla `ki`, que es un ejemplo, y otra valla, que no se lee. `shapeProblems` mira títulos y secciones; `exampleProblems`, la salida de cada ejemplo frente a su código y su `also=`.
- **`sourceCodes`** lee `internal/` con `codesUnder`: `filepath.WalkDir` y `codesIn` en cada `.go` que no es de test; `readCatalog` lee `docs/errors`; `catalogProblems` compara los dos conjuntos en los dos sentidos y ordena lo que encuentra, y `sortedCodes` da los ficheros por código: ningún fallo sale en el orden de un map.

{{file ch07-s4 tests/errors/ch07/unclosed_paren_render.ki}}

{{reftest T07.39}}

{{reftest T07.43}}

Compárate ahora con los tests de `diag` y `sema` de la referencia:

{{diff ch07-s3..ch07-s4 internal/diag/render_test.go}}

{{diff ch07-s3..ch07-s4 internal/sema/resolve_test.go}}

{{diff ch07-s3..ch07-s4 internal/sema/scope_test.go}}

{{diff ch07-s3..ch07-s4 internal/sema/suggest_test.go}}

{{diff ch07-s3..ch07-s4 internal/sema/suggest_internal_test.go}}

{{file ch07-s4 internal/sema/analyze_fuzz_test.go}}

`FuzzAnalyze` es el `FuzzParse` de los pases que siguen al parser (T07.94).

El mensaje de la etiqueta: `ch07-s4: rustc-style diagnostics: fix-it suggestions, the bar for E01xx–E03xx and the docs/errors catalog`.

Si te atascas o tu versión se desvía demasiado, copia el fichero de `ref/ch07-s4` y sigue.

Al terminar: tus tests en verde, commit y etiqueta `ch07-s4`; compárate con `ref/ch07-s4`.

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

El E0301 de `total` no tiene ayuda: ningún candidato está a una edición. Y en JSON, el del hito:

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
- Nada visible depende del orden de un map: `slices.Sorted(maps.Keys(m))`, desempates explícitos, y un `iter.Seq` como `visible` que no enseña sus maps.
- `//go:embed stdlib` mete el preludio en el binario; por eso `embed.go` está en la raíz.
- La distancia de edición es programación dinámica que solo necesita dos filas.
- Resolver son dos pasadas: definiciones primero, cuerpos después; los ámbitos son maps encadenados, el más interior gana, y lo resuelto va en tablas laterales por id.
- Una sugerencia está al alcance con `max(1, len/3)` ediciones; los empates, por orden alfabético.
- Un diagnóstico tiene primaria con texto, secundarias, nota, ayuda concreta y fix-it cuando es mecánico; una ayuda genérica es peor que ninguna.
- Los fix-its se aplican sobre las posiciones originales, de derecha a izquierda, y se muestran como el diff de rustc.
- `docs/errors` tiene una entrada por código, con ejemplos que `TestErrorDocs` ejecuta; leer el fuente con `go/parser` lo sincroniza con él.

## Go en el trabajo

**Un map o un slice ordenado.** En esta máquina, buscando en orden salteado, un `map[string]int` costó unos 8 ns con 8 claves, 11 con 1 000 y 24 con 100 000; con claves `uint32`, 4,3, 6,8 y 13,5 ns. Una clave string hay que hashearla y compararla byte a byte, y por eso el compilador busca por `Symbol` y no por texto. `slices.BinarySearch` sobre un slice ordenado de strings perdió en los tres tamaños; de `uint32`, solo ganó con 8 claves.

**La iteración aleatoria en CI.** El runtime baraja el orden a propósito; el precio es el test de la teoría, que pasa en tu portátil y falla en la CI una vez de cada cuatro. `go test -count=100` lo destapa en segundos.

**Los mensajes como producto.** rustc mantiene una guía de estilo en la *rustc-dev-guide* («Diagnostic output style guide»): los mensajes «start with a lowercase letter and do not end with punctuation». Su guía de sugerencias va más allá que Ki: pide evitar «did you mean» («Suggestions should not be a question») y prefiere «there is a struct with a similar name: `Foo`». Y cada sugerencia lleva su `Applicability`, de `MachineApplicable` a `MaybeIncorrect`: lo que Ki no distingue todavía.

**`go vet` señala.** `go vet` sobre `fmt.Printf("%d\n", name)` con un `string` dice `main.go:7:14: fmt.Printf format %d has arg name of wrong type string`: qué y dónde, sin ayuda. En Go 1.27, `go vet -fix` aplica el arreglo que un analizador ofrece, y con `-diff` lo enseña como parche sin tocar el fichero; para `fmt.Printf(s)` es `fmt.Printf("%s", s)`, y para el `%d` no hay nada, porque no hay edición que adivine si sobra el verbo o el argumento.
