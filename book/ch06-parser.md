---
chapter: 6
title: El parser: descenso recursivo, Pratt y recuperación
part: I
hours: 11
milestone: `ki emit ast` (S-expresiones)
---

## Objetivo

Al terminar, `ki` lee la estructura de un programa: el parser convierte los tokens del capítulo 5 en un árbol de sintaxis y no se para en el primer error. Lo enseña un destino nuevo de `ki emit`:

```console
$ cat fib.ki
fn fib(n: Int) -> Int {
  if n < 2 { n } else { fib(n - 1) + fib(n - 2) }
}

fn main() {
  let n = fib(20)
  print(n)
}
$ ki emit ast fib.ki
(fn fib (params (n Int)) (ret Int) (block (if (< n 2) (block n) (block (+ (call fib (- n 1)) (call fib (- n 2)))))))
(fn main (params) (ret _) (block (let n _ (call fib 20)) (call print n)))
```

Una línea por función, en S-expresiones: cada nodo entre paréntesis, con su clase delante. `_` es un tipo que el programa no escribió. Que `print` no exista es asunto del capítulo 7. Es el hito, camino de M1 (capítulo 10).

:::note
**Fe de erratas (2026-10-28).** Los mensajes de los errores de este capítulo mejoran en el capítulo 7 («El listón: errores al nivel de rustc»), que los explica.
:::

## Teoría

### La pila de una goroutine y la recursión

Un parser de **descenso recursivo** tiene una función por regla de la gramática, y las llamadas se anidan como el texto: en `((1))`, `group` acaba llamando a `group`. Cada nivel del texto es un nivel de la pila de Go, así que la profundidad la decide el fichero.

La pila de una goroutine empieza pequeña y crece por copia, duplicándose, hasta 512 MiB. Duplicarla otra vez pasa del límite de 1 GB, y entonces no hay panic, sino un error fatal:

```text
runtime: goroutine stack exceeds 1000000000-byte limit
runtime: sp=… stack=[…]
fatal error: stack overflow
```

Un `fatal error` no se puede capturar: `recover()` no lo ve, y el proceso termina con código 2. Sin límite, 10⁶ paréntesis anidados lo provocan.

Por eso los límites son del parser, el único pase que ve el texto sin forma:

- **`MaxNesting`** (256 niveles, E0204) acota lo que anida.
- **`MaxChain`** (1 024, E0205) acota lo que no anida en el texto: `1 + 1 + … + 1` tiene en su árbol un nivel por operador. Cuenta operadores binarios y llamadas por expresión de sentencia, la expresión entera con sus paréntesis y argumentos.

Así, el árbol queda acotado, y un pase posterior no se desborda si gasta poca pila por nivel.

:::from Rust
`catch_unwind` tampoco atrapa un desbordamiento de pila: el programa aborta.
:::

:::more Las cifras de la pila, y Rust, Python y Node al desbordarse
`newstack`, en `runtime/stack.go`, reserva la pila del doble de tamaño y copia en ella la vieja. El límite, según la documentación de `debug.SetMaxStack` en Go 1.27, es «1 GB on 64-bit systems» (en `runtime/proc.go`, `maxstacksize = 1000000000`). Como los tamaños son potencias de dos, la mayor pila que llega a existir es de 512 MiB.

Sin límite, el parser lee 10⁵ paréntesis anidados: cada uno son cinco llamadas y unos 1 900 bytes de pila. El árbol tiene como mucho unos 256 × 1 024 niveles, porque cada bloque o condición anidados tiene su propia cadena, y el impresor de este capítulo lo recorre con menos de 256 MiB de pila.

- **Rust** aborta con `thread 'main' (…) has overflowed its stack` y `fatal runtime error: stack overflow, aborting`. Cambia la distancia: un hilo nuevo recibe 2 MiB de pila (en las plataformas de primer nivel), y una goroutine crece hasta 512 MiB.
- **Python 3.14** corta antes y deja capturarlo: pasado `sys.getrecursionlimit()`, 1 000, la recursión lanza `RecursionError: maximum recursion depth exceeded`, que `except` recoge.
- **Node 24** tampoco da un fatal error: la recursión sin fondo lanza `RangeError: Maximum call stack size exceeded`, y `catch` la recoge.
:::

### Descenso recursivo y Pratt

Las reglas van en los comentarios del código (la notación, en la sesión 1): `item = { attr } [ 'pub' ] 'fn' lower '(' …`. Para firmas y sentencias basta mirar el token actual. Ki no hace *backtracking* (apartado 5.3 de su especificación, «el spec»): cada decisión se toma con el token que hay delante y no se deshace.

Las expresiones necesitan más. Con una función por nivel de precedencia, `1` pasaría por seis llamadas hasta `primary`. **Pratt** pone los niveles en una tabla de **potencias de enlace**: cuanto mayor, más sujeta el operador a sus operandos.

| Potencia | Operadores |
|---|---|
| 0 | libre, para `\|>` |
| 1 | `\|\|` |
| 2 | `&&` |
| 3 | `==` `!=` `<` `<=` `>` `>=`, que no se encadenan |
| 4 | libre, para `..` |
| 5 | `+` `-` |
| 6 | `*` `/` `%` |
| 7 | prefijos `-` y `!` |
| Por encima de todos | la llamada, postfija |

El algoritmo:

```text
pratt(minBP):
  lhs = prefix()                       // a literal, a name, a group, -x, !x
  loop:
    if the token is `(`: lhs = call(lhs, arguments); continue
    if the token is an operator with power bp >= minBP:
      consume it; rhs = pratt(bp + 1); lhs = (op lhs rhs)
    else: return lhs
```

Con `1 - 2 * 3`, `pratt(0)` lee `1` y ve `-` (5): pide el operando derecho con `pratt(6)`, que lee `2`, ve `*` (6 ≥ 6), lo consume y devuelve `(* 2 3)`. El resultado: `(- 1 (* 2 3))`.

El `bp + 1` decide la **asociatividad**. En `1 - 2 - 3`, el `pratt(6)` del operando derecho ve el segundo `-` (5 < 6) y vuelve, y el bucle de fuera lo toma: `(- (- 1 2) 3)`. Con `pratt(bp)`, saldría `(- 1 (- 2 3))`.

**Las comparaciones no se encadenan.** En Python, `a < b < c` es `a < b and b < c`; en Go compara un `bool` con un `int` y no compila.

Ki, como Rust, la rechaza: E0203 sobre el segundo operador, una sola vez, con la ayuda «use `&&`: `a < b && b < c`». El parser lee el resto de la cadena y la convierte en un nodo de error.

### Recuperación, sincronización y supresión

Detenerse en el primer error obliga a compilar una vez por error. El parser de Ki sigue, con tres piezas.

**Nodos de error.** Donde el texto deja de tener sentido queda un `ItemError`, `StmtError`, `TypeError` o `ExprError`: en el volcado, `(error)`. El árbol queda completo, y los pases posteriores no deben informar sobre ellos: ya lo hizo el parser.

**Sincronización.** Tras un error, `block` salta al final de la sentencia. Para en un `Newline`, `;` o `}` que no pertenezcan a lo saltado, o en un `let` al empezar una línea. El `let` hace falta en `let =⏎let y`: tras un `=` la regla de línea no pone `Newline`, y el salto se comería `let y`.

Tras un error en un item (una definición de primer nivel), el parser salta al comienzo de otro.

**Supresión.** El spec (7.3) pide no emitir errores «a menos de 3 tokens del anterior»: suelen ser el mismo visto desde otro sitio.

La distancia se cuenta **en tokens que no son `Newline`**, una marca de la regla de línea: contarla daría errores distintos al mismo código según cómo se reparta en líneas.

Sin estas piezas, `fn f() { let =⏎let = }` da seis diagnósticos; con ellas, uno.

Un delimitador abierto al final del fichero es E0202. Un token `Error` del lexer ya tiene su diagnóstico: el parser lo cuenta como error de la sentencia y no dice nada más.

:::more Dónde para el salto exactamente, y dónde salen errores de más
**La sincronización de una sentencia** para en el siguiente `Newline` o `;` que no esté dentro de un delimitador abierto por lo que salta, en un `}` que no cierre una llave abierta por lo saltado (así cierra el bloque y, de paso, un `(` que la sentencia dejó abierto), o en un `let` que empieza una línea, fuera de esos delimitadores. Uno a media línea se salta: `foo(let x = 1)` da un solo error (T06.86).

**La de un item** salta hasta el siguiente `fn`, `pub`, `#`, `type`, `trait`, `impl`, `effect` o `import` fuera de delimitadores.

**Los seis diagnósticos** salen al seguir en el token siguiente, como en la sesión 3: «expected identifier», «expected newline» y «expected expression» sobre cada `=`.

**La promesa es no dar cascadas:** un segundo error de la sentencia sale si el parser llega a él a 3 tokens o más del primero. `g(,) + h(,)` da dos. `f(1 2) + g(,)` da uno: un hueco no corta la expresión, pero tras el `)` que falta el salto se come `g(,)`.

**Un error suprimido** no mueve el punto desde el que se mide.

**La `{` sin cerrar** es la única entrada que cuesta más sin límite: se traga todo lo de detrás. Un `fn` dentro de un bloque será una sentencia (funciones locales), así que cada línea que empieza un item da un E0201. Con 100 líneas o más, el E0202, que se informa el último, pasa del límite de `diag.MaxErrors` (T06.101).

**Una `{`** que, en la línea siguiente a la condición de un `if`, ya no es su bloque, con un `else` detrás (T06.102), o la de `if b == == {`, cuyo segundo `==` toma el bloque por operando, cuesta uno o dos errores más.

**El E0202** es `unclosed delimiter`, con la etiqueta «this `{` is never closed», y solo del más interno: `fn f() { (1` informa del `(`. Un error a menos de 3 tokens del final lo suprime: `fn f() {⏎  g(,)⏎` solo da el E0201.

**`go/parser`,** sin el modo `AllErrors`, descarta todo error en la misma línea que el anterior. En Go 1.27, `ParseFile` con `f(1 2); f(1 2); f(1 2)` en una línea da un error; repartido en tres líneas, tres. `ki check` da tres en los dos casos.
:::

### La búsqueda acotada

Ki tendrá lambdas, `(x, y) => x + y`, y al ver `(` el parser no sabe si es una lambda o un grupo, `(x + y) * 2`. El spec (5.3) lo resuelve con una **búsqueda acotada**: buscar el `)` emparejado contando niveles y mirar si lo sigue `=>`.

Es barata: no consume nada. Tampoco es *backtracking*: se decide antes de leer. Llega en el capítulo 15.

### El AST en tablas: `IdVec`, pools y `ListRange`

Un `Module` guarda cuatro `IdVec` (items, sentencias, expresiones y tipos) y cuatro pools: un nodo nombra a otro por su id, y una lista de hijos es un `ListRange` (capítulo 4) sobre un pool. Lo nuevo:

- **Contigüidad.** En `f(g(1), 2)`, añadidos al pool según se leen, los argumentos de `g` se intercalarían con los de `f`. Por eso cada lista se junta en un slice local y `appendRange` la añade de una vez. Recibe `*[]T`, un puntero al slice, porque `append` devuelve una cabecera nueva y el `Module` debe quedarse con ella (en Rust, `&mut Vec<T>`).
- **El id 0 es un id.** Un hijo opcional lleva una marca al lado: `Ret` y `HasRet`, `Else` y `HasElse`.
- **Vistas recortadas.** `ExprList` devuelve `pool[First:end:end]` (capítulo 2) sobre el pool recortado a su longitud: un `append` de quien la recibe realoja, y no pisa la lista siguiente.

¿Por qué ids y no punteros en un lenguaje con GC?

| Razón | Qué gana |
|---|---|
| Compacidad | Un `Expr` ocupa 104 bytes (`unsafe.Sizeof`); con punteros y slices en lugar de ids y rangos, 176 |
| Nodos sin punteros | Ningún campo de `Expr` es un puntero: el recolector no recorre el array de `Exprs` (el capítulo 4 midió qué gana) |
| Tablas laterales | Un pase posterior guarda lo que sabe de cada nodo en sus propias tablas por id, sin tocar el árbol |
| Volcados estables | Un id es el orden de creación, no una dirección de memoria: dos ejecuciones dan el mismo volcado |

:::from Rust
En Rust, los ids cambian `Box<Expr>` y el *borrow checker* por índices que el compilador no comprueba. En Go el recolector ya libera un árbol de punteros: los ids no se eligen por corrección, sino por esas razones.
:::

### `switch` sobre `Kind` y `panic("unreachable")`

`Expr` no es una interfaz sellada (capítulo 3) sino un struct con un campo `Kind`. Un valor de interfaz lleva un puntero, así que el array dejaría de estar libre de ellos y cada nodo sería una reserva. La clase dice qué campos significan algo.

El `switch` de Go no es exhaustivo: un `ExprMatch` nuevo sin su `case` compila. Por eso cada `switch` sobre `Kind` termina en `default: panic("unreachable")`: un panic con traza, no un volcado al que le falta un nodo.

:::from Rust
Un `match` sobre un `enum` sí es exhaustivo: olvidar una variante no compila (`error[E0004]`). En Go, esa red es el `default` y los tests que recorren cada clase.
:::

### Closures en los ayudantes de los tests

Un closure (capítulo 1) ve las variables de la función que lo contiene **como variables**, no como copias. Aquí, `get()` devuelve 2:

```go
n := 1
get := func() int { return n }
n = 2
```

Los tests lo usan en ayudantes como `spanAt`, y cada fila de `TestPrinter` lleva un campo `build func(b *tree) syntax.ExprID`, que construye su árbol.

:::from Rust
El ejemplo no compila en Rust (`error[E0506]`): el closure toma prestada `n`. En Go, los dos la comparten, y el recolector la mantiene viva.
:::

### El impresor y las S-expresiones

`PrintModule` acumula el texto en un `strings.Builder` (capítulo 3) y devuelve un `string`; quien lo escribe en un `io.Writer` es `ki emit`. Las S-expresiones no son ambiguas, y un golden las compara con `diff`.

### Fuzzing del parser y nombres

`FuzzParse` hace con el parser lo que `FuzzLex` con el lexer: pasa cualquier entrada por `Lex` y `Parse`. Comprueba que no hay panic, tampoco al recorrer el árbol con `PrintModule`, y que todo span cae dentro del fichero.

La guía de estilo de Google pide no repetir lo que dice el paquete (`syntax.Parse`, no `syntax.SyntaxParse`) y accesores sin `Get` (`Params`, `Attrs`).

## Tests primero

{{cards}}

Úsalas: escribe el test de la tarjeta, míralo fallar, implementa lo justo para el verde, y solo entonces despliega la referencia con su `{{reftest}}`. Describen el final del capítulo. Unas pocas dependen de sesiones posteriores y antes se escriben de otra forma, en `git show ref/ch06-sK:book/tests/ch06.tests`: cada sesión dice cuáles.

:::more Los dos paquetes de test y los ayudantes, sesión por sesión
**Dos paquetes.** `parser_test.go`, `ast_test.go`, `ast_printer_test.go`, `recover_test.go` y `parser_fuzz_test.go` son `package syntax_test` y solo usan lo exportado. `parser_internal_test.go`, `parser_expr_internal_test.go` y `parser_stmt_internal_test.go` son `package syntax`, porque llaman a métodos sin exportar como `p.expr(0)` y `p.letStmt()`. Los ayudantes no se exportan, así que `syntax_test` no ve los de `package syntax`, y `recover_test.go` tiene copias de algunos.

Los ayudantes de las sesiones 2 a 4 están al principio de su fichero en `ref/ch06-sK`. Otros viven en un solo subtest, como los closures `spanAt` y `span` en `TestParser/spans`.

- **Sesión 1, `parser_test.go`.** `parseFile` lexa con el `lexFile` del capítulo 5 y parsea; `printModule` exige cero diagnósticos y devuelve el volcado; `wantModule` lo compara.
- **Sesión 1, `ast_printer_test.go`.** `tree` construye un `Module` a mano (`intLit`, `call`, `binary`…), para probar el impresor sin el parser, y `printBody` y `wantBody` imprimen una función `f` con ese cuerpo.
- **Sesión 2, `parser_expr_internal_test.go`.** `newExprRun` pone el parser en el primer token de un texto de id 1 (`newSource`); `parseExprSexpr` lee una expresión con `p.expr(0)`, exige cero diagnósticos y todo el texto usado, y la imprime; `r.sexpr` imprime una expresión; `r.wantAt`, dónde quedó el cursor; `lastIndex`, dónde aparece un texto por última vez; `wantExpr` y `wantText` comparan; `wantOnlyDiag` exige un único diagnóstico, con `reflect.DeepEqual`; `chainedDiag` y `tooLongDiag` escriben el E0203 y el E0205 esperados; `plusChain(n)` da `1` y `n` veces ` + 1`, y `wrapPlus`, su volcado.
- **Sesión 3, `parser_stmt_internal_test.go`.** Reutiliza los de la sesión 2. `parseLetSexpr` lee un `let` con `p.letStmt()`; `parseModule` parsea un texto sin errores léxicos y devuelve un `moduleRun` con `text`, `messages` y `body` (el cuerpo de la primera función); `wantModuleText` compara el volcado y exige cero diagnósticos; `e0201` escribe un E0201; `nth` y `after` dan posiciones en el texto.
- **Sesión 4, `recover_test.go`.** `recParse` lexa, parsea y devuelve un `recRun`; `at` da el span de la enésima aparición de un texto; `wantDiags` compara la lista entera de diagnósticos; `wantText`, el volcado; `recE0201`–`recE0204` escriben los esperados; `wrapLet` pone una expresión como inicializador de un `let` en `main`.

**La forma de antes** de una tarjeta está también en el test de `ref/ch06-sK`.
:::

## Implementación

### Sesión 1: el AST y las firmas de función (≈ 3 h)

#### Los tests, en rojo

1. **Escribe T06.01–T06.19.** Describen el AST, estructura de datos que se copia tal cual.

{{file ch06-s1 internal/syntax/ast.go}}

Y estas funciones:

```go
func Parse(tb *TokenBuffer, file base.FileID, in *base.Interner, sink *diag.Sink) *Module
func PrintModule(m *Module, in *base.Interner) string

// unexported, for T06.14
type parser struct { tb *TokenBuffer; pos int; end uint32; m *Module; … }
func newParser(tb *TokenBuffer, file base.FileID, in *base.Interner, sink *diag.Sink) *parser
func (p *parser) advance() Token
func (p *parser) at(k TokenKind) bool
func (p *parser) eat(k TokenKind) (Token, bool)
```

2. **Cambia tu `lexFile`:** un fichero vacío delante da al de prueba el id 1, que `lexFile` comprueba (T06.12).

{{diff ch05-s4..ch06-s1 internal/syntax/lexer_test.go}}

3. **Escribe T06.13 como en `ref/ch06-s1`:** hoy un cuerpo solo puede ser `{}`, y el parser no informa de nada.
4. **Míralos fallar.** No compilan: `parser_internal_test.go:19:8: undefined: newParser`.
5. **Copia `ast.go` y añade estos stubs:**

```go
type parser struct { tb *TokenBuffer; pos int; end uint32 }

func Parse(…) *Module                           { return &Module{File: file} }
func PrintModule(…) string                      { return "" }
func newParser(…) *parser                       { return &parser{} }
func (p *parser) advance() Token                { return Token{} }
func (p *parser) at(k TokenKind) bool           { return false }
func (p *parser) eat(k TokenKind) (Token, bool) { return Token{}, false }
```

Fallan, sin panic, `TestParserCursor`, `TestPrinter` y doce de `TestParser`; `TestAST` y `empty_module` ya pasan.

#### El código

{{file ch06-s1 internal/syntax/parser.go}}

- **`advance`** nunca consume el `Eof`: un bucle que lo olvide no sale del buffer.
- **`item`** consume al menos un token y, donde la firma deja de tener sentido, devuelve un `ItemError` con lo leído. Hay cuerpo si un `{` sigue a la firma en la misma línea: `pub fn print(x: Int)` sin cuerpo es una declaración externa.

{{file ch06-s1 internal/syntax/ast_printer.go}}

`item`, `optType`, `stmt` y `expr` son un `switch` por clase: cada `case` escribe su forma y baja a cada hijo.

:::more Cómo leer las reglas, y detalles de `attrs`, `typeExpr` y `end`
Las reglas usan la notación del spec (`docs/superpowers/specs/2026-10-24-ki-design-go.md` en la referencia, apartado 5.3). `item` decide por `#`, `pub` o `fn`; `block`, por `let` o por cualquier otra cosa. Entre comillas va un token; `[ x ]` es x opcional; `{ x }`, x cero o más veces; `|` separa alternativas y `( )` agrupa. `lower` (`LowerIdent`), `string` y `Newline` son clases de token, no reglas.

- **`attrs`** salta los `Newline` tras cada atributo, porque `]` cierra una expresión y la regla de línea pone uno. Quita ese `skipNewlines` y T06.09 lo ve: el atributo de `#[extern("ki_print_int")]⏎pub fn print(x: Int)` sale como un `(error)` aparte.
- **`typeExpr`** ante un error no consume nada salvo un `(`: el `)` que cierra los parámetros sigue ahí para quien llama.
- **`end`** es el final del último token consumido sin contar los `Newline`: `spanFrom` lo usa para que el span de un nodo acabe en su último token y no en el salto de línea.
- **El recorte.** En `ast.go`, si quitas de `slice` la línea que recorta el pool, T06.16 falla: `ExprList of a range inside the capacity but past the length did not panic`.
- **`attrs` y `params`** juntan la lista en un slice local y solo la añaden al pool si sale bien: una lista que falla no deja nada (T06.13). Ese test tiene hoy 46 fuentes, `fn f() {` entre ellas, y no mira diagnósticos.
- **El cuerpo.** En la línea siguiente, la regla de línea pone antes del `{` un `Newline`, y por eso la función no tiene cuerpo.
:::

{{reftest T06.14}}

{{reftest T06.16}}

Si te atascas o tu versión se desvía demasiado, copia el fichero de `ref/ch06-s1` y sigue.

Al terminar: tus tests en verde, commit y etiqueta `ch06-s1` (mensaje: `ch06-s1: the AST and function signatures`); compárate con `ref/ch06-s1`.

### Sesión 2: expresiones con Pratt y el límite de cadena (≈ 3 h)

#### Los tests, en rojo

1. **Escribe T06.20–T06.50,** en `parser_expr_internal_test.go`. La interfaz nueva es interna:

```go
const MaxChain = 1024

// unexported; the parser gains a field `chain chainState`
type chainState struct {
	from    int  // index of the first token of the statement-level expression
	count   int  // binary operators and calls consumed so far
	tooLong bool // E0205 was reported
}
func (p *parser) expr(minBP int) ExprID // a statement-level expression
func (p *parser) skipStatement()        // after E0205, to the end of the statement
```

2. **Escribe como en `ref/ch06-s2`, porque dependen de las sesiones 3 y 4:** T06.45, sin su fila `call with a semicolon inside`; T06.49 y T06.50 (hoy `skip_statement_counts_consumed_braces`), con el estado de `chain` puesto a mano.
3. **Míralos fallar.** No compilan (`r.p.expr undefined`). Con la interfaz y estos stubs:

```go
func (p *parser) expr(minBP int) ExprID { return p.m.Exprs.Push(Expr{Kind: ExprError}) }
func (p *parser) skipStatement()        {}
```

Fallan sin panic los 31 subtests de `TestParseExpr`:

```text
"1 + 2 * 3": the expression stops at token 0 (Int), want it to use the whole source
```

#### El código

{{file ch06-s2 internal/syntax/parser_expr.go}}

{{diff ch06-s1..ch06-s2 internal/syntax/parser.go}}

- **`pratt`** es el algoritmo de la teoría, y **`binaryOp`**, la tabla: un `switch` que devuelve tres valores.
- **La cadena.** `countChain` cuenta cada operador binario y llamada. Pasado el límite informa E0205 una vez: los bucles paran, y `expr` salta el resto de la sentencia y devuelve un nodo de error. La expresión queda **abandonada**.

`Parse` aún no llama a `expr`: un cuerpo sigue siendo `{}`.

:::more Tres mutantes de `pratt`; `expr`, `group` y `callArgs` por dentro; el E0203 en Go y en Rust
- **`p.pratt(bp)`** en lugar de `bp + 1`: T06.21 da `(- 1 (- 2 3))`, que vale 2 en lugar de -4.
- **`prefix` con `pratt(0)`:** T06.22 daría `(neg (* a b))`.
- **Sin la comprobación tras una comparación,** la que llama a `chainedComparison`: T06.24 recibe `(< (< a b) c)` sin diagnóstico.

`countChain` marca `tooLong` al informar el E0205. Los prefijos no cuentan en la cadena, porque su profundidad es asunto del límite de anidamiento. `expr` guarda la cuenta de fuera y la restaura: una sentencia dentro de un bloque dentro de una expresión tendrá la suya. `group` no crea nodo: devuelve la expresión de dentro. `callArgs` junta los argumentos y los añade al pool al final, por la contigüidad.

En Go, `a < b < c` da `invalid operation: a < b < c (mismatched types untyped bool and int)`. Rust la rechaza con el mismo mensaje que Ki: `comparison operators cannot be chained`.
:::

{{reftest T06.34}}

{{reftest T06.30}}

Si te atascas o tu versión se desvía demasiado, copia el fichero de `ref/ch06-s2` y sigue.

Al terminar: tus tests en verde, commit y etiqueta `ch06-s2` (mensaje: `ch06-s2: expressions with Pratt parsing and the chain limit`); compárate con `ref/ch06-s2`.

### Sesión 3: bloques, `let` e `if` (≈ 2,5 h)

#### Los tests, en rojo

1. **Escribe T06.51–T06.72,** en `parser_stmt_internal_test.go`. La interfaz exportada no cambia; la interna:

```go
func (p *parser) letStmt() StmtID                            // T06.54 reads a let with it
func (p *parser) block() ExprID                              // moves to parser_stmt.go
func (p *parser) ifExpr() ExprID
func (p *parser) exprOrSkip(minBP int) (e ExprID, abandoned bool)
func (p *parser) expected(what string)                       // E0201 at the cursor
func (p *parser) expect(k TokenKind, what string) (Token, bool)
func (tb *TokenBuffer) text(t Token) string                  // Lex keeps the source in tb
```

El parser empieza a informar E0201, `expected <what>, found <token>`.

2. **Lleva T06.13 a su forma final:** sin `fn f() {`, cada fuente debe dar un E0201, un E0202 o el E0104.
3. **Escribe T06.65, T06.70 y T06.71 como en `ref/ch06-s3`:** dependen de la sesión 4. De las tablas largas (T06.69, 41 filas), escribe las primeras filas de cada mensaje y copia el resto de `ref/ch06-s4`.
4. **Míralos fallar.** No compila (`r.p.letStmt undefined`). Añade un `letStmt` que devuelva `p.m.Stmts.Push(Stmt{Kind: StmtError})`.
5. **Dos subtests de la referencia hacen panic en rojo:** indexan sin comprobar la longitud. En tu versión, pon delante un `t.Fatalf` que la compruebe; con la referencia, usa `-skip`, el contrario de `-run`:

```console
$ go test ./internal/syntax -skip 'TestParseStmt/(let_missing_equals_keeps_the_name|found_text_is_the_source_text)$'
```

Fallan los 22 subtests de `TestParseStmt` (con `-skip`, 20) y `TestParser/malformed_input_still_parses`.

#### El código

{{file ch06-s3 internal/syntax/parser_stmt.go}}

{{diff ch06-s2..ch06-s3 internal/syntax/parser.go}}

{{diff ch06-s2..ch06-s3 internal/syntax/parser_expr.go}}

{{diff ch06-s2..ch06-s3 internal/syntax/token.go}}

{{diff ch06-s2..ch06-s3 internal/syntax/lexer.go}}

- **`block`** decide si una expresión es sentencia o cola por lo que la sigue. Un `;` la hace sentencia; un `Newline`, también, salvo que detrás venga el `}`: entonces es la cola, el valor del bloque.
- **`letStmt`** sin `=` sigue ligando el nombre, con un nodo de error por inicializador: lo de después puede usar `x` sin un error más.
- **`found`** cita el token con `tb.text`: por eso `Lex` guarda ahora el fichero en el buffer.
- **`ifExpr`** lee la condición con `exprOrSkip`, que además dice si se abandonó: entonces el resto, ramas incluidas, ya se saltó, y el `if` no debe buscar su `{`.

:::more Los textos exactos del E0201, los panics del rojo, `found` y un mutante de `block`
`<what>` es `expression`, `identifier`, `type`, `item`, `` `=` ``, `` `)` ``, `` `{` ``, `` `}` `` o `newline`, y en la firma y los atributos también `` `(` ``, `` `[` ``, `` `]` `` y `string`. `` `{` `` solo lo pide la rama de un `if`: un `fn` sin `{` en su línea es una función sin cuerpo, no un error. El token hallado se escribe con su texto del fichero entre comillas invertidas, o `newline`, o `end of file`.

T06.13 pierde la fila `fn f() {` porque un bloque sin cerrar es ya otro asunto; el E0104 es el del lexer.

T06.65, T06.70 y T06.71 tienen otra forma hoy porque aquí el `$` da dos errores (el del lexer y un E0201), una llave sin cerrar es E0201 `` expected `}`, found end of file ``, un `if` abandonado no arrastra lo que lo contiene y tras un error el parser sigue en el token siguiente.

Los dos panics del rojo, que cortan el binario, son `let_missing_equals_keeps_the_name` (`index out of range [0] with length 0`) y `found_text_is_the_source_text` (`index out of range [1] with length 1`).

**`found`** corta el token de `src`: `0xFF` sale como `` `0xFF` ``, no como 255.

En `block`, si en el `Newline` quitas `&& p.at(RBrace)`, T06.60 da `(block (call g))` para `{ f()⏎ g() }`: `f()` se toma como cola y `g()` la pisa.
:::

{{reftest T06.60}}

{{reftest T06.64}}

Si te atascas o tu versión se desvía demasiado, copia el fichero de `ref/ch06-s3` y sigue.

Al terminar: tus tests en verde, commit y etiqueta `ch06-s3` (mensaje: `ch06-s3: blocks, let and if`); compárate con `ref/ch06-s3`.

### Sesión 4: recuperación, límite de anidamiento y `ki emit ast` (≈ 2,5 h)

#### Los tests, en rojo

1. **Escribe T06.73–T06.105:** `TestRecover`, `FuzzParse` y los tres golden. La interfaz:

```go
const MaxNesting = 256
// usage: ki emit tokens|ast [--diagnostics=human|short|json] FILE

// unexported: chainState loses from and tooLong, and gains abandoned (E0205,
// E0204 or an abandoned condition); skipStatement becomes syncStatement.
```

2. **Lleva a su forma final** T06.45, T06.49, T06.50, T06.65, T06.70 y T06.71, como en estos diffs, y cambia la línea de uso en `TestCLI`. Escribe `emit_ast` y `emit_dump_before_diagnostics` (T06.100, T06.103) antes de leer el último diff.

{{diff ch06-s3..ch06-s4 internal/syntax/parser_expr_internal_test.go}}

{{diff ch06-s3..ch06-s4 internal/syntax/parser_stmt_internal_test.go}}

{{diff ch06-s3..ch06-s4 internal/driver/cli_test.go}}

3. **Míralos fallar.** No compila (`undefined: syntax.MaxNesting`). Con la constante no hay panics.

:::more Qué falla sobre `ch06-s3`, y las demás líneas de los diffs
**El rojo.** `suppression_at_distance_two` enseña el problema entero: seis diagnósticos donde quiere uno. Con la constante fallan 22 subtests de `TestRecover`, `chain_skips_to_statement_end` de `TestParseExpr`, tres de `TestParseStmt` (`if_condition_chain_limit`, `found_text_is_the_source_text` y `recovery_in_blocks`), tres de `TestCLI` (`emit_unknown_target`, `emit_ast` y `emit_dump_before_diagnostics`) y los tres golden. `FuzzParse` y cinco de `TestRecover` ya pasan.

**Los diffs.** Sus demás líneas (T06.48, T06.59, T06.69, T06.72 y comentarios), y T06.13 en `parser_test.go`, son de la referencia, que escribió esas tarjetas antes en otra forma; las tuyas ya están así. Una llave sin cerrar deja de ser E0201 y pasa a E0202.
:::

#### Recuperación y anidamiento

{{file ch06-s4 internal/syntax/recover.go}}

{{diff ch06-s3..ch06-s4 internal/syntax/parser.go}}

{{diff ch06-s3..ch06-s4 internal/syntax/parser_expr.go}}

{{diff ch06-s3..ch06-s4 internal/syntax/parser_stmt.go}}

- **Supresión.** `advance` cuenta en `index` los tokens que no son `Newline`. `admit` decide si un error se informa.
- **Sincronización.** Si `p.errs` cambió tras una sentencia, `block` llama a `syncStatement`.
- **Anidamiento.** `enter` y `leave` rodean cada paréntesis, argumento, prefijo, condición, rama y bloque.
- **Abandono.** E0204 marca la expresión como abandonada, igual que E0205; `condition` lo propaga al `if` y a lo que lo contiene: lo que esperaban ya se saltó.

:::more Qué test caza cada pieza, y `trackBracket`, `admit` y `depth` por dentro
- **`tokIndex`** da a un error hallado en un `Newline` el índice del token al que sigue. Si `advance` contara también los `Newline`, T06.78 lo ve: en `fn f() { let x⏎⏎⏎let = }` la distancia pasaría de 2 a 3 y saldría un segundo error.
- **`trackBracket`** lleva los delimitadores con la `bracketStack` de la regla de línea.
- **`admit`** cuenta el error con `detect`. `report` y `expected` pasan por ella, así que vale para todos los códigos: T06.85 la prueba con E0205 y E0204.
- **`depth`** no es de ninguna expresión: una sentencia en un bloque anidado empieza a la profundidad del bloque. Lo fija T06.96.
- **La parada en `let`.** Quítala y T06.77 recibe un solo diagnóstico: el salto se come `let y`.
- **`!p.tok().NewlineBefore`.** Tras una sentencia, `block` pide separador solo en la misma línea: tras un `(` sin cerrar no hay `Newline` hasta el `}`, y vale el salto del fichero. Sin esa condición, la primera fuente de T06.104 da cuatro errores.
- **El límite.** Con `p.depth > MaxNesting` en lugar de `==`, T06.79 acepta 257 paréntesis.
- **`expected`** calla ante un token `Error`, pero lo cuenta. Sin su `if`, T06.91 ve ``expected expression, found `$` `` tras el E0104 del lexer.
- **Las tres guardas «Cannot happen today»** no tienen test: puedes omitirlas.
:::

{{reftest T06.86}}

{{reftest T06.83}}

#### `ki emit ast` y `ki check`

{{diff ch06-s3..ch06-s4 internal/driver/emit.go}}

{{diff ch06-s3..ch06-s4 internal/driver/check.go}}

`finishEmit` da el volcado por stdout, los diagnósticos por stderr y al final el código de salida: el árbol de un fichero con errores se ve entero. `ki check` pasa ahora por el parser:

{{file ch06-s4 tests/errors/ch06/three_errors.ki}}

{{file ch06-s4 tests/golden/ch06/fib_ast.ki}}

#### El fuzz

{{reftest T06.99}}

Las semillas leen el golden con `../../tests/…`: `go test` corre en el directorio del paquete (capítulo 4). `sp == (base.Span{})` compara con el valor cero; sin los paréntesis, Go leería la `{` como el cuerpo del `if`.

1. **Ejecútalo.** ≈ 10⁶ entradas, `PASS` y nada en `testdata/`:

```console
$ go test ./internal/syntax -run='^$' -fuzz=FuzzParse -fuzztime=10s
```

2. **Provoca un bucle que no consume tokens:** en `block`, cambia por `continue` el `break` que sigue a `p.unclosed(open)`.
3. **Repite con `-parallel=1`,** un solo proceso. El contador se para en `gathering baseline coverage: k/n completed` (con la caché vacía, en `execs: N (0/sec)`), y termina en `PASS`: una entrada que no vuelve no es un fallo.
4. **Lo caza el tiempo máximo,** que corta T06.74 con `panic: test timed out after 20s`:

```console
$ go test -run TestRecover -timeout 20s ./internal/syntax
```

Deshaz el cambio.

Si te atascas o tu versión se desvía demasiado, copia el fichero de `ref/ch06-s4` y sigue.

Al terminar: tus tests en verde, commit y etiqueta `ch06-s4` (mensaje: `ch06-s4: error recovery, nesting limit and ki emit ast`); compárate con `ref/ch06-s4`.

## Ejecútalo

Desde la raíz de tu repo, con `ki` construido (`go build -o build/bin/ki ./cmd/ki`), el `fib.ki` del objetivo da las dos líneas de arriba. Un fichero con cuatro errores:

```console
$ cat bad.ki
fn a() { let = 1 }
fn b() -> Int { 1 + }
fn c() -> Int { (1 }
fn main() { if 1 < 2 < 3 { 0 } }
$ build/bin/ki emit ast bad.ki
(fn a (params) (ret _) (block (error)))
(fn b (params) (ret Int) (block (+ 1 (error))))
(fn c (params) (ret Int) (block (error)))
(fn main (params) (ret _) (block (if (error) (block 0))))
error[E0201]: expected identifier, found `=`
 --> bad.ki:1:14
  |
1 | fn a() { let = 1 }
  |              ^

error[E0201]: expected expression, found `}`
 --> bad.ki:2:21
  |
2 | fn b() -> Int { 1 + }
  |                     ^

error[E0201]: expected `)`, found `}`
 --> bad.ki:3:20
  |
3 | fn c() -> Int { (1 }
  |                    ^

error[E0203]: comparison operators cannot be chained
 --> bad.ki:4:22
  |
4 | fn main() { if 1 < 2 < 3 { 0 } }
  |                      ^
  = help: use `&&`: `a < b && b < c`
$ echo $?
1
```

Un error por función, y el árbol con un `(error)` en su sitio. El `if` de `main` conserva sus ramas: solo su condición es un error. Y diez mil paréntesis:

```console
$ printf 'fn main() {\n  let x = %s\n}\n' "$(printf '(%.0s' $(seq 10000))" > deep.ki
$ build/bin/ki check --diagnostics=short deep.ki
deep.ki:2:267: error[E0204]: expression is nested too deeply
```

La columna 267 es el paréntesis 257: el primero está en la 11.

## Ejercicios

:::exercise
**1. Un fix-it para E0203.** Añade al E0203 un fix-it (el campo `Fixits` del capítulo 3) que proponga la cadena reescrita con `&&` y los operandos reales: para `x + 1 < y < z`, `x + 1 < y && y < z`.
:::

:::solution
El fix-it necesita la cadena entera, que se lee después de informar. Pero informar al final cambiaría el orden: en T06.48, el E0205 del tercer operando saldría antes que el E0203. La solución decide en el operador, como antes, y guarda lo que la cadena informe en un sink aparte para echarlo después (añade `strings` a los imports):

```go
		if bp == bpCompare && isComparison(p.tok().Kind) {
			lhs = p.chainedComparison(start, t.Span)
		}

func (p *parser) chainedComparison(start, first base.Span) ExprID {
	d := diag.NewError(diag.E(203), "comparison operators cannot be chained",
		diag.Label{Span: p.tok().Span}).
		WithHelp("use `&&`: `a < b && b < c`")
	shown := p.admit() // decided here, at the second operator, as before
	errs, sink := p.errs, p.sink
	var later diag.Sink // what the rest of the chain reports goes after E0203
	p.sink = &later
	ops := []base.Span{first}
	for isComparison(p.tok().Kind) {
		if !p.countChain(p.tok().Span) {
			break
		}
		ops = append(ops, p.advance().Span)
		p.pratt(bpCompare + 1)
	}
	p.sink = sink
	if shown {
		if p.errs == errs && !p.chain.abandoned {
			d.Fixits = []diag.FixIt{{Span: p.spanFrom(start), Replacement: p.andChain(start, ops)}}
		}
		p.sink.Report(d)
	}
	for _, l := range later.All() {
		p.sink.Report(l)
	}
	return p.errorExpr(start)
}

// andChain writes the chain that starts at start, has its operators at ops and
// ends at the last token read, as comparisons joined by `&&`, with the text of
// the operands as the file has it.
func (p *parser) andChain(start base.Span, ops []base.Span) string {
	text := func(lo, hi uint32) string { return strings.TrimSpace(string(p.tb.src[lo:hi])) }
	operands := []string{text(start.Lo, ops[0].Lo)}
	for i := 1; i < len(ops); i++ {
		operands = append(operands, text(ops[i-1].Hi, ops[i].Lo))
	}
	operands = append(operands, text(ops[len(ops)-1].Hi, p.end))
	parts := make([]string, len(ops))
	for i, op := range ops {
		parts[i] = operands[i] + " " + text(op.Lo, op.Hi) + " " + operands[i+1]
	}
	return strings.Join(parts, " && ")
}
```

Los operandos son el texto entre operadores, no los spans de los nodos, que dejan fuera los paréntesis de un grupo. Una cadena con otro error dentro no lleva fix-it. Ningún renderizador muestra aún los fix-its, pero los tests comparan el diagnóstico entero: un ayudante `withFix(d, sp, text)` pone el esperado, T06.24 pide `a < b && b < c` sobre `[0, 9)`, y T06.38 gana los campos `chain` (el texto que se reemplaza) y `fix` y una fila, `(a) < (b + 1)  <  c`, que conserva paréntesis y quita espacios. Sin el sink aparte, T06.46 y T06.48 fallan por el orden.
:::

:::exercise
**2. `ki emit ast --pretty`.** Añade `PrintModulePretty`, que pone el cuerpo de cada función, cada rama de un `if` y cada sentencia de un bloque y su cola en su propia línea, dos espacios más adentro, y el flag `--pretty`. Con su test.
:::

:::solution
El `printer` gana `pretty bool` y `depth int`, y un método que recibe un closure:

```go
// child writes what f writes, one level deeper, after a space or, with
// pretty, after a line break and the indentation of its level.
func (pr *printer) child(f func()) {
	pr.depth++
	if pr.pretty {
		pr.b.WriteByte('\n')
		pr.str(strings.Repeat("  ", pr.depth))
	} else {
		pr.str(" ")
	}
	f()
	pr.depth--
}
```

El cuerpo de la función pasa a ser `pr.child(func() { … })`, las ramas `pr.child(func() { pr.expr(e.Then) })`, y cada sentencia y la cola de un bloque, igual. `PrintModule` y `PrintModulePretty` llaman al mismo bucle con `pretty` a `false` o a `true`, así que el formato compacto no cambia y los tests de siempre lo vigilan. El flag es el patrón de `--stats` del capítulo 5: `fileArgs` recibe una función que añade flags (`check` pasa `nil`; si no hiciste el ejercicio 1 del capítulo 5, añade antes a `fileArgs` su parámetro `more func(*flag.FlagSet)`, y `emit tokens`, `nil`), `emitAST` registra `fs.BoolVar(&pretty, "pretty", false, …)` y elige la función de volcado. La línea de uso pasa a ser `ki emit tokens|ast [--pretty] [--diagnostics=human|short|json] FILE`, también en `TestCLI`. El test, en `TestParser`, parsea `fib`, un `extern` y un `main`, y exige:

```text
(fn fib (params (n Int)) (ret Int)
  (block
    (if (< n 2)
      (block
        n)
      (block
        (+ (call fib (- n 1)) (call fib (- n 2)))))))
(pub fn print (attrs (extern "ki_print_int")) (params (x Int)) (ret _)
  none)
(fn main (params) (ret _)
  (block
    (let n _ (call fib 20))
    (call print n)))
```

Si `child` ignora `pretty`, el test recibe el volcado de una línea. `ki emit tokens --pretty` sigue siendo un error de uso.
:::

:::exercise
**3. Sin `MaxNesting`.** Demuestra, sin dejarlo en el repo, qué pasaría sin el límite: sube `MaxNesting` a `1 << 30`, limita la pila con `debug.SetMaxStack(1 << 20)` (de `runtime/debug`) y parsea 10⁵ paréntesis. Lee el `fatal error`.
:::

:::solution
Un test temporal en `package syntax_test` que llama a `debug.SetMaxStack(1 << 20)`, difiere una función con `recover()` y parsea `fn main() { let x = ` con 10⁵ paréntesis. `go test -run TestDeepParens ./internal/syntax/` da:

```text
runtime: goroutine stack exceeds 1048576-byte limit
runtime: sp=… stack=[…]
fatal error: stack overflow

runtime stack:
runtime.throw(…)
…
goroutine … [running]:
github.com/aldezex/kilang/internal/syntax.(*parser).primary(…)
…
github.com/aldezex/kilang/internal/syntax.(*parser).prefix(…)
…
github.com/aldezex/kilang/internal/syntax.(*parser).pratt(…)
…
github.com/aldezex/kilang/internal/syntax.(*parser).groupBody(…)
…
```

y `FAIL`. Se lee de arriba abajo: el límite (1 MiB), dónde estaba la pila, la causa, la pila del runtime que lo detectó y la de la goroutine, con `primary`, `group`, `groupBody`, `pratt` y `prefix` repetidos hasta que la traza omite unos 2 600 (`...N frames elided...`). El `recover` no ejecutó nada: el binario de test sale con 2.
:::

:::exercise
**4. Cien mil funciones.** Mide `Parse` sobre un fichero de 10⁵ funciones (`b.Loop`, `b.SetBytes`, `-benchmem`) y explica las reservas con el perfil de memoria.
:::

:::solution
En `parser_bench_test.go` (`package syntax_test`), `manyFunctions(n)` escribe funciones como `fn f7(n: Int) -> Int {⏎  let m = n * 2 + 7⏎  if m < 10 { m } else { f7(m - 1) + g(m, 3) }⏎}`. El benchmark lexa con `Lex` una vez fuera del bucle, llama a `b.SetBytes(int64(len(src)))` y mide solo `Parse`, dentro de `for b.Loop() { … }`, con una comprobación de que salen 100 000 items. En esta máquina, con 10,4 MB de fuente, unos 120–210 ms por `Parse` (49–84 MB/s), 1,39 GB y unas 400 000 reservas por operación. Con `-memprofile` y `go tool pprof -sample_index=alloc_space -top`, el 82–86 % de los bytes es `IdVec.Push` de la tabla de expresiones: un `append` que, con millones de elementos, crece un 25 % cada vez y copia todo el array de `Expr` de 104 bytes: reserva en total unas cinco veces el tamaño final, y copia unas cuatro. Las reservas (`alloc_objects`) son los slices locales de `callArgs`, `params` y `block`, uno por lista, unas cuatro por función. Una capacidad inicial para las tablas evitaría esas copias.
:::

:::exercise
**5. `|>` en el nivel 0.** Añade el operador `|>` con potencia 0, solo al parser, con tests de precedencia.
:::

:::solution
En `ast.go`, `BinPipe` tras `BinOr` y su texto, `BinPipe: "|>"`. En `parser_expr.go`, `bpPipe = 0` y `case Pipe: return BinPipe, bpPipe, true` en `binaryOp`. Nada más: `expr(0)` acepta potencias desde 0, y el operando derecho se lee con `pratt(1)`. El test, en `TestParseExpr`:

```go
	t.Run("pipe", func(t *testing.T) {
		for _, row := range []struct{ src, want string }{
			{"a |> f", "(|> a f)"},
			{"a |> f |> g", "(|> (|> a f) g)"}, // to the left
			{"a || b |> f", "(|> (|| a b) f)"}, // looser than ||
			{"a |> f || g", "(|> a (|| f g))"}, // also on its right
			{"a < b |> f", "(|> (< a b) f)"},   // and than a comparison
			{"x + 1 |> f(2)", "(|> (+ x 1) (call f 2))"},
			{"(a |> f) * 2", "(* (|> a f) 2)"}, // parentheses still win
		} {
			wantExpr(t, "pipe", row.src, row.want)
		}
		// A line that starts with |> continues the statement (line rule).
		wantModuleText(t, "pipe", "fn main() {\n  xs\n    |> sum\n}", "(fn main (params) (ret _) (block (|> xs sum)))\n")
	})
```

Con `bpPipe = 1`, la cuarta fila da `(|| (|> a f) g)`. Y T06.15 falla hasta que su fila `BinOp(13)` pase a `BinOp(14)`: `BinOp(13).String() = "|>", want "BinOp(13)"`. Convertir `e |> f(…)` en `f(e, …)` es trabajo de la elaboración, el pase que traduce el AST a Core (capítulo 9), no del parser.
:::

## Preguntas de repaso

:::review
¿Por qué los límites de anidamiento y de cadena están en el parser y no en cada pase que recorre el árbol?
:::

:::answer
Porque un desbordamiento de pila en Go es un `fatal error` que nada captura: ningún pase podría convertirlo en un diagnóstico. El parser es el único que ve el texto antes de que exista el árbol; si el árbol que entrega está acotado, el impresor, el comprobador y el intérprete pueden recorrerlo con una pila acotada.
:::

:::review
¿Por qué `pratt` lee el operando derecho con `bp + 1` y no con `bp`?
:::

:::answer
Porque así un operador de la misma potencia no entra en el operando derecho: lo deja para el bucle de fuera, que lo aplica a lo ya construido, y la asociatividad es por la izquierda.
:::

:::review
¿Por qué la supresión mide la distancia en tokens que no son `Newline`, y no en líneas?
:::

:::answer
Porque un `Newline` lo pone la regla de línea, no quien programa: contarlo, o contar líneas como `go/parser`, haría que el mismo código diera errores distintos según cómo se reparta.
:::

:::review
¿Por qué `syncStatement` para en un `let` que empieza una línea, si un `let` no es un separador?
:::

:::answer
Porque tras un `=` la línea no ha acabado y la regla de línea no pone `Newline`: en `let =⏎let y }`, el salto no encontraría separador antes del `}` y se comería la sentencia siguiente. Y solo una sentencia empieza por `let`: es un final seguro.
:::

:::review
¿Por qué `callArgs` junta los argumentos en un slice local en lugar de añadirlos al pool según los lee?
:::

:::answer
Porque un argumento puede ser otra llamada, que añade sus propios argumentos al pool mientras se lee: los de dentro quedarían intercalados con los de fuera, y un `ListRange` tiene que ser contiguo.
:::

:::review
¿Qué gana `ki check` con que el parser siga tras un error, y por qué hace falta la supresión para que eso ayude?
:::

:::answer
Gana informar en una pasada de varios errores, no solo del primero. Pero seguir en el token siguiente produce cascadas: un `let =` daba tres mensajes sobre el mismo `=`. La sincronización salta lo que ya no tiene sentido y la supresión calla lo que es el mismo error visto otra vez.
:::

## Resumen

- Desbordar la pila es un `fatal error` que `recover` no captura: por eso el parser acota el anidamiento (256) y las cadenas (1 024).
- Descenso recursivo: una función por regla, sin *backtracking*.
- Pratt: una tabla de potencias y un bucle; `pratt(bp + 1)` asocia por la izquierda.
- Un error deja un nodo de error; el parser salta lo roto y calla los errores a menos de 3 tokens del anterior, sin contar los `Newline`.
- La búsqueda acotada mira hacia delante sin consumir nada.
- El AST en tablas: nodos sin punteros, tablas laterales por id y volcados estables.
- Un `switch` no es exhaustivo: `default: panic("unreachable")` hace ruidoso el olvido.
- Un closure captura variables, no valores.
- `ki emit ast` da S-expresiones, y `ki check` pasa ya por el parser.
- `FuzzParse` vigila panics y spans; un bucle sin fin lo caza `-timeout`.

## Go en el trabajo

**Parsers a mano.** Los dos parsers de Go están escritos a mano. El del compilador lee las expresiones binarias con un bucle, el esquema de `pratt`.

**Errores en el editor.** Un editor ve casi siempre un programa a medias, y un parser que se rinde no le sirve. `go/ast` tiene nodos de error: `BadExpr`, `BadStmt` y `BadDecl`.

:::more Los parsers de Go por dentro
El del compilador es `cmd/compile/internal/syntax/parser.go`: su bucle, `for … p.prec > prec`, llama a `p.binaryExpr(nil, tprec)` con la precedencia del operador.

`go/parser` acota su recursión con `maxNestLev = 1e5`: da `exceeded max nesting depth`, y un comentario dice para qué es el límite: «to prevent stack exhaustion». Su recuperación es la de Ki con otras piezas: `advance(stmtStart)` salta hasta una palabra clave que empieza sentencia, y un contador, `syncCnt`, garantiza que el parser avanza, contra un bucle como el de «El fuzz».

`go/ast` es el AST que comparten `go/printer`, `go/format` y `cmd/gofmt`. `BadExpr` es «a placeholder for an expression containing syntax errors for which a correct expression node cannot be created».
:::

**Variables de bucle.** Un clásico de entrevista: ¿qué da llamar a cada función de `fs`?

```go
for i := 0; i < 3; i++ {
	fs = append(fs, func() int { return i })
}
```

Con `go 1.21` en el `go.mod`, da `3 3 3`: los closures capturan la única variable. Con `go 1.22`, `0 1 2`. Es el `var` frente al `let` de un `for` en JavaScript.
