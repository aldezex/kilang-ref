---
chapter: 6
title: El parser: descenso recursivo, Pratt y recuperación
part: I
hours: 11
milestone: `ki emit ast` (S-expresiones)
---

## Objetivo

Al terminar, `ki` lee la estructura de un programa: el parser convierte tokens en un árbol de sintaxis (AST) y no se para en el primer error. Lo enseña `ki emit ast`:

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

Una línea por función, en S-expresiones: cada nodo entre paréntesis, su clase delante. `_` es un tipo que el programa no escribió. `print` aún no existe: capítulo 7.

Es el hito, camino de M1 (capítulo 10).

:::note
**Fe de erratas (2026-10-28).** Los mensajes de error de este capítulo mejoran en el capítulo 7, que los explica.
:::

## Teoría

### La pila de una goroutine y la recursión

Un parser de **descenso recursivo** tiene una función por regla de la gramática, y las llamadas se anidan como el texto. Así, el fichero decide la profundidad de la pila.

La pila de una goroutine crece por copia, duplicándose, hasta 512 MiB. Duplicarla otra vez pasa del límite de 1 GB, y entonces no hay panic, sino un error fatal:

```text
runtime: goroutine stack exceeds 1000000000-byte limit
runtime: sp=… stack=[…]
fatal error: stack overflow
```

Un `fatal error` no se puede capturar: `recover()` no lo ve, y el proceso termina con código 2. Sin límite, 10⁶ paréntesis anidados lo provocan.

Por eso los límites son del parser, el pase que crea el árbol:

- **`MaxNesting`** (256 niveles, E0204) acota lo que anida.
- **`MaxChain`** (1 024, E0205) acota lo que no anida en el texto: `1 + 1 + … + 1` tiene en su árbol un nivel por operador. Cuenta operadores binarios y llamadas de la expresión entera de una sentencia, con sus paréntesis y argumentos.

Juntos acotan el árbol: un pase posterior no se desborda si gasta poca pila por nivel.

:::more Cuánta pila gasta el parser
Sin límite, cada paréntesis son cinco llamadas y unos 1 900 bytes de pila: el parser lee 10⁵ y cae con 10⁶.

Con los límites, el árbol tiene como mucho unos 256 × 1 024 niveles, porque cada bloque o condición anidados tiene su propia cadena. El impresor de este capítulo lo recorre con menos de 256 MiB de pila.
:::

### Descenso recursivo y Pratt

Para firmas y sentencias basta mirar el token actual. Ki no hace *backtracking*: ninguna decisión se deshace. Lo pide el 5.3 de su especificación, «el spec» (`docs/superpowers/specs/2026-10-24-ki-design-go.md` en la referencia).

Las expresiones necesitan más. Con una función por nivel de precedencia, `1` pasaría por seis llamadas hasta el literal. **Pratt** pone los niveles en una tabla de **potencias de enlace**: cuanto mayor, más sujeta el operador a sus operandos.

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

Con `1 - 2 * 3`, `pratt(0)` lee `1` y ve `-` (5): pide el operando derecho con `pratt(6)`, que lee `2`, ve `*` (6 ≥ 6) y devuelve `(* 2 3)`. El resultado: `(- 1 (* 2 3))`.

El `bp + 1` decide la **asociatividad**. En `1 - 2 - 3`, el `pratt(6)` del operando derecho ve el segundo `-` (5 < 6) y vuelve, y el bucle de fuera lo toma: `(- (- 1 2) 3)`. Con `pratt(bp)`, saldría `(- 1 (- 2 3))`.

**Las comparaciones no se encadenan.** En Python, `a < b < c` es `a < b and b < c`; en Go compara un `bool` con un `int` y no compila.

Ki, como Rust, la rechaza: un E0203 sobre el segundo operador, y un nodo de error por toda la cadena.

### Recuperación, sincronización y supresión

Detenerse en el primer error obliga a compilar una vez por error. Ki sigue, con tres piezas.

**Nodos de error.** Donde el texto deja de tener sentido queda un nodo de error (`ExprError`, `StmtError`…): en el volcado, `(error)`. El árbol queda completo, y los pases posteriores no deben informar sobre ellos.

**Sincronización.** Tras un error, `syncStatement` salta al final de la sentencia: un `Newline`, `;` o `}` que no pertenezcan a lo saltado, o un `let` al empezar una línea. Tras un error en un item (una definición de primer nivel), el parser salta al comienzo de otro.

El `let` hace falta en `let =⏎let y`: tras un `=` la regla de línea no pone `Newline`, y el salto se comería `let y`.

Un delimitador abierto al final del fichero es E0202.

**Supresión.** El spec (7.3) pide no emitir errores «a menos de 3 tokens del anterior»: suelen ser el mismo visto desde otro sitio.

Se cuentan **tokens que no son `Newline`**: contar esa marca de la regla de línea daría errores distintos al mismo código según cómo se reparta en líneas.

Un token `Error` del lexer ya tiene su diagnóstico: el parser no añade otro.

Sin estas piezas, `fn f() { let =⏎let = }` da seis diagnósticos; con ellas, uno.

:::more Los errores de más que quedan
La promesa es no dar cascadas. Un segundo error de la sentencia sale si el parser llega a él a 3 tokens o más del primero: `g(,) + h(,)` da dos. `f(1 2) + g(,)` da uno: tras el `)` que falta, el salto se come `g(,)`.

La `{` sin cerrar es la única entrada que da errores de más sin límite: se traga lo de detrás. Un `fn` dentro de un bloque será una sentencia (funciones locales), así que cada línea que empieza un item da un error. Con 100 líneas o más, el E0202, que se informa el último, pasa del límite de `diag.MaxErrors`.
:::

### La búsqueda acotada

Ki tendrá lambdas, `(x, y) => x + y`, y al ver `(` el parser no sabe si es una lambda o un grupo. El spec (5.3) lo resuelve con una **búsqueda acotada**: buscar el `)` emparejado contando niveles y mirar si lo sigue `=>`.

Es barata, no consume nada, y no es *backtracking*: se decide antes de leer. Llega en el capítulo 15.

### El AST en tablas: `IdVec`, pools y `ListRange`

Un `Module`, el árbol de un fichero, guarda cuatro `IdVec` (items, sentencias, expresiones y tipos) y cuatro pools. Un nodo nombra a otro por su id, y una lista de hijos es un `ListRange` (capítulo 4). Lo nuevo:

- **Contigüidad.** Añadidos según se leen, los argumentos de `g` en `f(g(1), 2)` se intercalarían con los de `f`. Por eso cada lista se junta en un slice local y `appendRange` la añade de una vez.
- **`*[]T`.** `appendRange` recibe un puntero al slice porque `append` devuelve una cabecera nueva y el `Module` debe quedarse con ella.
- **El id 0 es un id,** no «nada»: un hijo opcional lleva una marca al lado, `Ret` y `HasRet`.
- **Vistas recortadas.** `ExprList` devuelve `pool[First:end:end]` (capítulo 2): un `append` de quien la recibe realoja, y no pisa la lista siguiente.

¿Por qué ids y no punteros en un lenguaje con GC?

| Razón | Qué gana |
|---|---|
| Compacidad | Un `Expr` ocupa 104 bytes; con punteros y slices en lugar de ids y rangos, 176 |
| Nodos sin punteros | El recolector no recorre el array de `Exprs` (el capítulo 4 midió qué gana) |
| Tablas laterales | Un pase posterior guarda lo que sabe de cada nodo en sus tablas por id, sin tocar el árbol |
| Volcados estables | Un id es el orden de creación, no una dirección: dos ejecuciones dan el mismo volcado |

:::from Rust
Los ids cambian `Box<Expr>` y el *borrow checker* por índices que el compilador no comprueba. En Go, el recolector ya libera un árbol de punteros: se eligen por esas razones.
:::

### `switch` sobre `Kind` y `panic("unreachable")`

`Expr` no es una interfaz sellada (capítulo 3) sino un struct con un campo `Kind`. Un valor de interfaz lleva un puntero: el array dejaría de estar libre de ellos y cada nodo sería una reserva.

El `switch` de Go no es exhaustivo: un `ExprMatch` nuevo sin su `case` compila. Por eso cada `switch` sobre `Kind` termina en `default: panic("unreachable")`: un panic con traza, no un volcado al que le falta un nodo.

:::from Rust
Un `match` sobre un `enum` sí es exhaustivo: olvidar una variante no compila.
:::

### Closures en los tests

Un closure (capítulo 1) ve las variables de la función que lo contiene **como variables**, no como copias. Aquí, `get()` devuelve 2:

```go
n := 1
get := func() int { return n }
n = 2
```

:::from Rust
No compila en Rust: el closure toma prestada `n`. En Go, el recolector la mantiene viva.
:::

Los tests lo usan: sus ayudantes capturan `t` y `src` sin recibirlos.

### El impresor y las S-expresiones

`PrintModule` acumula el texto en un `strings.Builder` (capítulo 3) y devuelve un `string`; `ki emit` lo escribe en un `io.Writer`. Las S-expresiones no son ambiguas, y un golden las compara con `diff`.

### Fuzzing del parser

`FuzzParse` comprueba, como `FuzzLex` en el lexer, que ninguna entrada da panic, tampoco al recorrer el árbol con `PrintModule`, y que todo span cae dentro del fichero.

### Nombres

La guía de estilo de Google pide no repetir el paquete (`syntax.Parse`, no `syntax.SyntaxParse`) y accesores sin `Get` (`Params`).

## Tests primero

{{cards}}

Úsalas: escribe el test de la tarjeta, míralo fallar, implementa lo justo para el verde, y solo entonces despliega la referencia.

Describen el final del capítulo. Unas pocas tienen antes otra forma, en `git show ref/ch06-sK:book/tests/ch06.tests`: cada sesión dice cuáles.

:::more Dónde va cada test
`parser_test.go`, `ast_test.go`, `ast_printer_test.go`, `recover_test.go` y `parser_fuzz_test.go` son `package syntax_test` y solo usan lo exportado. Los `*_internal_test.go` son `package syntax`, porque llaman a métodos sin exportar como `p.expr(0)` y `p.letStmt()`.

Los ayudantes no se exportan, así que `syntax_test` no ve los de `package syntax`, y `recover_test.go` tiene copias de algunos.

Si un test de la referencia usa uno que no conoces, búscalo en `ref/ch06-sK`: suele estar al principio de su fichero o en el mismo subtest.
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

2. **Cambia tu `lexFile`:** un fichero vacío delante da al de prueba el id 1, que `lexFile` comprueba.

{{diff ch05-s4..ch06-s1 internal/syntax/lexer_test.go}}

3. **Escribe T06.13 como en `ref/ch06-s1`:** hoy el parser no informa de nada.
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

Fallan sin panic; los de la estructura del AST y el del módulo vacío ya pasan.

#### El código

Las reglas de la gramática van en los comentarios: entre comillas, un token; `[ x ]`, opcional; `{ x }`, cero o más veces; `|`, alternativas; `( )` agrupa. `lower`, `string` y `Newline` son clases de token.

{{file ch06-s1 internal/syntax/parser.go}}

Sin un `{` en la línea de la firma, `pub fn print(x: Int)` no tiene cuerpo: es una declaración externa.

{{file ch06-s1 internal/syntax/ast_printer.go}}

{{reftest T06.14}}

{{reftest T06.16}}

Si te atascas o tu versión se desvía demasiado, copia el fichero de `ref/ch06-s1` y sigue.

Al terminar: tus tests en verde, commit y etiqueta `ch06-s1`; compárate con `ref/ch06-s1`.

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

2. **Escribe T06.45, T06.49 y T06.50 como en `ref/ch06-s2`:** dependen de las sesiones 3 y 4.
3. **Míralos fallar.** No compilan (`r.p.expr undefined`). Con la interfaz y estos stubs:

```go
func (p *parser) expr(minBP int) ExprID { return p.m.Exprs.Push(Expr{Kind: ExprError}) }
func (p *parser) skipStatement()        {}
```

Fallan sin panic todos los subtests de expresiones:

```text
"1 + 2 * 3": the expression stops at token 0 (Int), want it to use the whole source
```

#### El código

{{file ch06-s2 internal/syntax/parser_expr.go}}

{{diff ch06-s1..ch06-s2 internal/syntax/parser.go}}

- **`pratt`** es el algoritmo de la teoría, y **`binaryOp`**, la tabla.
- **La cadena.** Los prefijos no cuentan: son asunto del anidamiento. Pasado el límite, `countChain` informa E0205 una vez, `expr` salta el resto de la sentencia y la expresión queda **abandonada**: un nodo de error.

`Parse` aún no llama a `expr`: todo cuerpo es `{}`.

{{reftest T06.34}}

{{reftest T06.30}}

Si te atascas o tu versión se desvía demasiado, copia el fichero de `ref/ch06-s2` y sigue.

Al terminar: tus tests en verde, commit y etiqueta `ch06-s2`; compárate con `ref/ch06-s2`.

### Sesión 3: bloques, `let` e `if` (≈ 2,5 h)

#### Los tests, en rojo

1. **Escribe T06.51–T06.72,** en `parser_stmt_internal_test.go`. La interfaz nueva es interna:

```go
func (p *parser) letStmt() StmtID                            // T06.54 reads a let with it
func (p *parser) block() ExprID                              // moves to parser_stmt.go
func (p *parser) ifExpr() ExprID
func (p *parser) exprOrSkip(minBP int) (e ExprID, abandoned bool)
func (p *parser) expected(what string)                       // E0201 at the cursor
func (p *parser) expect(k TokenKind, what string) (Token, bool)
func (tb *TokenBuffer) text(t Token) string                  // Lex keeps the source in tb
```

El parser empieza a informar E0201, `expected <what>, found <token>`, y tras un error, por ahora, sigue en el token siguiente.

2. **Lleva T06.13 a su forma final:** quita su fila `fn f() {`; cada fuente debe dar un E0201, un E0202 o el E0104 del lexer.
3. **Escribe T06.65, T06.70 y T06.71 como en `ref/ch06-s3`:** dependen de la sesión 4. De T06.69, de 41 filas, escribe las primeras de cada mensaje y copia el resto de `ref/ch06-s4`.
4. **Míralos fallar.** No compila (`r.p.letStmt undefined`). Añade un `letStmt` que devuelva `p.m.Stmts.Push(Stmt{Kind: StmtError})`.
5. **Dos subtests de la referencia hacen panic en rojo:** indexan sin comprobar la longitud. En tu versión, pon delante un `t.Fatalf` que la compruebe; con la referencia, usa `-skip`, el contrario de `-run`:

```console
$ go test ./internal/syntax -skip 'TestParseStmt/(let_missing_equals_keeps_the_name|found_text_is_the_source_text)$'
```

Fallan todos los de sentencias y el de entrada mal formada.

#### El código

{{file ch06-s3 internal/syntax/parser_stmt.go}}

{{diff ch06-s2..ch06-s3 internal/syntax/parser.go}}

{{diff ch06-s2..ch06-s3 internal/syntax/parser_expr.go}}

{{diff ch06-s2..ch06-s3 internal/syntax/token.go}}

{{diff ch06-s2..ch06-s3 internal/syntax/lexer.go}}

- **`block`** decide por lo que sigue a la expresión. Un `;` la hace sentencia; un `Newline`, también, salvo que detrás venga el `}`: entonces es la cola, el valor del bloque. Sin ese `&& p.at(RBrace)`, `{ f()⏎ g() }` da `(block (call g))`: `f()` se toma como cola y `g()` la pisa.
- **`found`** cita el token con `tb.text`: por eso `Lex` guarda el fichero en el buffer.

{{reftest T06.60}}

{{reftest T06.64}}

Si te atascas o tu versión se desvía demasiado, copia el fichero de `ref/ch06-s3` y sigue.

Al terminar: tus tests en verde, commit y etiqueta `ch06-s3`; compárate con `ref/ch06-s3`.

### Sesión 4: recuperación, límite de anidamiento y `ki emit ast` (≈ 2,5 h)

#### Los tests, en rojo

1. **Escribe T06.73–T06.105:** recuperación, fuzz y los tres golden. La interfaz:

```go
const MaxNesting = 256
// usage: ki emit tokens|ast [--diagnostics=human|short|json] FILE

// unexported: chainState loses from and tooLong, and gains abandoned (E0205,
// E0204 or an abandoned condition); skipStatement becomes syncStatement.
```

2. **Lleva a su forma final** T06.45, T06.49, T06.50, T06.65, T06.70 y T06.71, como en estos diffs, y la línea de uso de la CLI. Escribe T06.100 y T06.103 antes de leer el último diff.

{{diff ch06-s3..ch06-s4 internal/syntax/parser_expr_internal_test.go}}

{{diff ch06-s3..ch06-s4 internal/syntax/parser_stmt_internal_test.go}}

{{diff ch06-s3..ch06-s4 internal/driver/cli_test.go}}

Lo demás de estos diffs ya lo tienes: la referencia tenía esas tarjetas en otra forma.

3. **Míralos fallar.** No compila (`undefined: syntax.MaxNesting`). Con la constante, sin panics, fallan casi todos los de recuperación, los golden y unos pocos de expresiones, sentencias y CLI; el fuzz ya pasa.

#### Recuperación y anidamiento

{{file ch06-s4 internal/syntax/recover.go}}

{{diff ch06-s3..ch06-s4 internal/syntax/parser.go}}

{{diff ch06-s3..ch06-s4 internal/syntax/parser_expr.go}}

{{diff ch06-s3..ch06-s4 internal/syntax/parser_stmt.go}}

- **El separador.** Tras una sentencia, `block` lo pide solo en la misma línea (`!p.tok().NewlineBefore`): tras un `(` sin cerrar no hay `Newline` hasta el `}`, y basta el salto de línea del fichero.
- **Abandono.** E0204 abandona la expresión, igual que E0205, y una condición abandonada arrastra al `if` y a lo que lo contiene: lo que esperaban ya se saltó.
- **Anidamiento.** `enter` y `leave` rodean cada paréntesis, argumento, prefijo, condición, rama y bloque.
- **Las tres guardas «Cannot happen today»** no tienen test: puedes omitirlas.

{{reftest T06.86}}

{{reftest T06.83}}

#### `ki emit ast` y `ki check`

{{diff ch06-s3..ch06-s4 internal/driver/emit.go}}

{{diff ch06-s3..ch06-s4 internal/driver/check.go}}

`finishEmit` da el volcado por stdout, los diagnósticos por stderr y al final el código de salida: se ve el árbol entero de un fichero con errores. `ki check` pasa ahora por el parser.

Dos de los golden:

{{file ch06-s4 tests/errors/ch06/three_errors.ki}}

{{file ch06-s4 tests/golden/ch06/fib_ast.ki}}

#### El fuzz

{{reftest T06.99}}

`sp == (base.Span{})` compara con el valor cero; sin paréntesis, Go leería la `{` como el cuerpo del `if`.

Ejecútalo; debe dar unas 10⁶ entradas, `PASS` y nada en `testdata/`:

```console
$ go test ./internal/syntax -run='^$' -fuzz=FuzzParse -fuzztime=10s
```

:::more Un bucle que el fuzz no caza
1. **Provoca un bucle que no consume tokens:** en `block`, cambia por `continue` el `break` que sigue a `p.unclosed(open)`.
2. **Repite con `-parallel=1`,** un solo proceso. El contador se para en `gathering baseline coverage: k/n completed` (con la caché vacía, en `execs: N (0/sec)`), y termina en `PASS`: una entrada que no vuelve no es un fallo.
3. **Lo caza el tiempo máximo,** que corta el test de la llave sin cerrar con `panic: test timed out after 20s`:

```console
$ go test -run TestRecover -timeout 20s ./internal/syntax
```

Deshaz el cambio.
:::

Si te atascas o tu versión se desvía demasiado, copia el fichero de `ref/ch06-s4` y sigue.

Al terminar: tus tests en verde, commit y etiqueta `ch06-s4`; compárate con `ref/ch06-s4`.

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

Un error por función, y un `(error)` en su sitio. Y diez mil paréntesis:

```console
$ printf 'fn main() {\n  let x = %s\n}\n' "$(printf '(%.0s' $(seq 10000))" > deep.ki
$ build/bin/ki check --diagnostics=short deep.ki
deep.ki:2:267: error[E0204]: expression is nested too deeply
```

La columna 267 es el paréntesis 257: el primero está en la 11.

## Ejercicios

:::exercise
**1. Un fix-it para E0203.** Añade al E0203 un fix-it (`Fixits`, capítulo 3) con la cadena reescrita y los operandos reales: para `x + 1 < y < z`, `x + 1 < y && y < z`.
:::

:::solution
El fix-it necesita la cadena entera, que se lee después de informar. Pero informar al final cambiaría el orden: el E0205 del tercer operando saldría antes que el E0203. La solución decide en el operador, como antes, y guarda lo que la cadena informe en un sink aparte para echarlo después (añade `strings` a los imports):

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

Los operandos son el texto entre operadores, no los spans de los nodos, que dejan fuera los paréntesis de un grupo. Una cadena con otro error dentro no lleva fix-it.

Ningún renderizador muestra aún los fix-its, pero los tests comparan el diagnóstico entero. Un ayudante `withFix(d, sp, text)` pone el esperado; el caso `a < b < c` pide `a < b && b < c` sobre `[0, 9)`; la tabla de formas de la cadena gana los campos `chain` (el texto que se reemplaza) y `fix` y una fila, `(a) < (b + 1)  <  c`, que conserva paréntesis y quita espacios. Sin el sink aparte, los casos de un E0203 con un E0205 detrás fallan por el orden.
:::

:::exercise
**2. `ki emit ast --pretty`.** Añade `PrintModulePretty` y el flag: cuerpos, ramas, sentencias y colas en su propia línea, dos espacios más adentro. Con su test.
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

El cuerpo de la función pasa a ser `pr.child(func() { … })`, las ramas `pr.child(func() { pr.expr(e.Then) })`, y cada sentencia y la cola de un bloque, igual. `PrintModule` y `PrintModulePretty` llaman al mismo bucle con `pretty` a `false` o a `true`: el formato compacto no cambia, y los tests de siempre lo vigilan.

El flag es el patrón de `--stats` del capítulo 5. `fileArgs` recibe una función que añade flags (`check` pasa `nil`; si no hiciste el ejercicio 1 del capítulo 5, añade antes a `fileArgs` su parámetro `more func(*flag.FlagSet)`, y `emit tokens`, `nil`). `emitAST` registra `fs.BoolVar(&pretty, "pretty", false, …)` y elige la función de volcado.

La línea de uso pasa a ser `ki emit tokens|ast [--pretty] [--diagnostics=human|short|json] FILE`, también en el test de la CLI. El test, en `parser_test.go`, parsea `fib`, un `extern` y un `main`, y exige:

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
**3. Sin `MaxNesting`.** Demuestra, sin dejarlo en el repo, qué pasaría: sube `MaxNesting` a `1 << 30`, limita la pila con `debug.SetMaxStack(1 << 20)` y parsea 10⁵ paréntesis. Lee el `fatal error`.
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

y `FAIL`. Se lee de arriba abajo: el límite (1 MiB), dónde estaba la pila, la causa, la pila del runtime que lo detectó y la de la goroutine. La última repite `primary`, `group`, `groupBody`, `pratt` y `prefix` hasta que la traza omite unos 2 600 (`...N frames elided...`). El `recover` no ejecutó nada: el binario de test sale con 2.
:::

:::exercise
**4. Cien mil funciones.** Mide `Parse` sobre un fichero de 10⁵ funciones (`b.Loop`, `b.SetBytes`, `-benchmem`) y explica las reservas con el perfil de memoria.
:::

:::solution
En `parser_bench_test.go` (`package syntax_test`), `manyFunctions(n)` escribe funciones como `fn f7(n: Int) -> Int {⏎  let m = n * 2 + 7⏎  if m < 10 { m } else { f7(m - 1) + g(m, 3) }⏎}`. El benchmark lexa con `Lex` una vez fuera del bucle, llama a `b.SetBytes(int64(len(src)))` y mide solo `Parse`, dentro de `for b.Loop() { … }`, con una comprobación de que salen 100 000 items.

Unas medidas, con 10,4 MB de fuente: unos 120–210 ms por `Parse` (49–84 MB/s), 1,39 GB y unas 400 000 reservas por operación.

Con `-memprofile` y `go tool pprof -sample_index=alloc_space -top`, el 82–86 % de los bytes es `IdVec.Push` de la tabla de expresiones: un `append` que, con millones de elementos, crece un 25 % cada vez y copia todo el array de `Expr` de 104 bytes. Reserva en total unas cinco veces el tamaño final, y copia unas cuatro.

Las reservas (`alloc_objects`) son los slices locales de `callArgs`, `params` y `block`, uno por lista, unas cuatro por función. Una capacidad inicial para las tablas evitaría esas copias.
:::

:::exercise
**5. `|>` en el nivel 0.** Añádelo con potencia 0, solo al parser, con tests de precedencia.
:::

:::solution
En `ast.go`, `BinPipe` tras `BinOr` y su texto, `BinPipe: "|>"`. En `parser_expr.go`, `bpPipe = 0` y `case Pipe: return BinPipe, bpPipe, true` en `binaryOp`. Nada más: `expr(0)` acepta potencias desde 0, y el operando derecho se lee con `pratt(1)`. El test, con los de expresiones:

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

Con `bpPipe = 1`, la cuarta fila da `(|| (|> a f) g)`. Y la fila `BinOp(13)` de los nombres de operador falla hasta que pase a `BinOp(14)`: `BinOp(13).String() = "|>", want "BinOp(13)"`. Convertir `e |> f(…)` en `f(e, …)` es trabajo de la elaboración, el pase que traduce el AST a Core (capítulo 9), no del parser.
:::

## Preguntas de repaso

:::review
¿Por qué los límites de anidamiento y de cadena son del parser y no de cada pase?
:::

:::answer
Porque un desbordamiento de pila en Go es un `fatal error` que nada captura: ningún pase podría convertirlo en un diagnóstico. El parser es el pase que crea el árbol; si el árbol que entrega está acotado, el impresor, el comprobador y el intérprete pueden recorrerlo con una pila acotada.
:::

:::review
¿Por qué `pratt` lee el operando derecho con `bp + 1` y no con `bp`?
:::

:::answer
Porque así un operador de la misma potencia no entra en el operando derecho: lo deja para el bucle de fuera, que lo aplica a lo ya construido, y la asociatividad es por la izquierda.
:::

:::review
¿Por qué la supresión cuenta tokens que no son `Newline`, y no líneas?
:::

:::answer
Porque un `Newline` lo pone la regla de línea, no quien programa: contarlo, o contar líneas, haría que el mismo código diera errores distintos según cómo se reparta en líneas.
:::

:::review
¿Por qué `syncStatement` para en un `let` que empieza una línea?
:::

:::answer
Porque tras un `=` la línea no ha acabado y la regla de línea no pone `Newline`: en `let =⏎let y }`, el salto no encontraría separador antes del `}` y se comería la sentencia siguiente. Y solo una sentencia empieza por `let`: es un final seguro.
:::

:::review
¿Por qué `callArgs` junta los argumentos en un slice local antes de añadirlos al pool?
:::

:::answer
Porque un argumento puede ser otra llamada, que añade sus propios argumentos al pool mientras se lee: los de dentro quedarían intercalados con los de fuera, y un `ListRange` tiene que ser contiguo.
:::

:::review
¿Qué gana `ki check` con que el parser siga tras un error, y por qué necesita la supresión?
:::

:::answer
Gana informar en una pasada de varios errores, no solo del primero. Pero seguir en el token siguiente produce cascadas: un `let =` daba tres mensajes sobre el mismo `=`. La sincronización salta lo que ya no tiene sentido y la supresión calla lo que es el mismo error visto otra vez.
:::

## Resumen

- Desbordar la pila es un `fatal error` que nada captura: el parser acota anidamiento y cadenas.
- Descenso recursivo sin *backtracking*, y Pratt para las expresiones: `pratt(bp + 1)` asocia por la izquierda.
- Un error deja un nodo de error; el parser salta lo roto y calla los errores a menos de 3 tokens del anterior.
- La búsqueda acotada mira hacia delante sin consumir nada.
- El AST en tablas: nodos sin punteros, tablas laterales por id, volcados estables.
- `default: panic("unreachable")` hace ruidoso lo que un `switch` no comprueba.
- Un closure captura variables, no valores.
- `FuzzParse` vigila panics y spans; un bucle sin fin lo caza `-timeout`.

## Go en el trabajo

**Parsers a mano.** Lo son `go/parser` y el del compilador, que lee las expresiones binarias con un bucle, como `pratt`.

**Errores en el editor.** Casi siempre ve programas a medias: un parser que se rinde no le sirve.

:::more Cómo se recupera `go/parser`
`go/ast`, el AST de `gofmt` y otras herramientas, tiene nodos de error: `BadExpr`, `BadStmt` y `BadDecl`.

`go/parser` se recupera como Ki, con otras piezas: `advance(stmtStart)` salta hasta una palabra clave que empieza sentencia, y un contador, `syncCnt`, garantiza que el parser avanza.

Sin el modo `AllErrors`, descarta todo error en la misma línea que el anterior: `f(1 2); f(1 2); f(1 2)` da un error, y repartido en tres líneas, tres. `ki check` da tres en los dos.
:::

**Variables de bucle.** ¿Qué da llamar a cada función de `fs`?

```go
for i := 0; i < 3; i++ {
	fs = append(fs, func() int { return i })
}
```

Con `go 1.21` en el `go.mod`, da `3 3 3`: los closures capturan la única variable. Con `go 1.22`, `0 1 2`. Es el `var` frente al `let` de un `for` en JavaScript.
