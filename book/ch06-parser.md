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

Una línea por función, en S-expresiones: cada nodo entre paréntesis, con su clase delante. `_` es un tipo que el programa no escribió. Que `print` no exista no le importa al parser; los nombres son del capítulo 7. Es el hito del capítulo, camino de M1 (capítulo 10).

## Teoría

### La pila de una goroutine y la recursión

Un parser de **descenso recursivo** tiene una función por regla de la gramática, y las llamadas se anidan como se anida el texto: en `((1))`, `group` lleva a `pratt`, que acaba llamando otra vez a `group`… Cada nivel del texto es un nivel de la pila de Go.

La pila de una goroutine empieza pequeña y crece cuando hace falta: `newstack`, en `runtime/stack.go`, reserva una del doble de tamaño y copia en ella la vieja. El límite, según la documentación de `debug.SetMaxStack` en Go 1.27, es «1 GB on 64-bit systems» (en `runtime/proc.go`, `maxstacksize = 1000000000`). Como los tamaños son potencias de dos, la mayor pila que llega a existir es de 512 MiB: duplicarla otra vez ya pasa del límite, y entonces no hay panic, sino un error fatal:

```text
runtime: goroutine stack exceeds 1000000000-byte limit
runtime: sp=… stack=[…]
fatal error: stack overflow
```

Un `fatal error` no se puede capturar: un `recover()` en una función diferida no lo ve, y el proceso termina con código 2. Sin límite, el parser lee 10⁵ paréntesis anidados (cada uno son cinco llamadas y unos 1 900 bytes de pila), y 10⁶ matan el proceso con ese mensaje.

Por eso los límites son del parser: `MaxNesting` (256 niveles, E0204) y `MaxChain` (1 024 operadores binarios o llamadas en una expresión de sentencia, E0205). Una cadena `1 + 1 + … + 1` no anida en el texto y `pratt` la lee en un bucle, pero su árbol tiene un nivel por operador: por eso `MaxChain` existe aparte. El parser es el único pase que ve el texto sin forma, y su árbol queda acotado: como mucho unos 256 × 1 024 niveles, porque cada bloque o condición anidados tiene su propia cadena. El impresor de este capítulo lo recorre con menos de 256 MiB de pila; un pase posterior no se desborda mientras gaste poca pila por nivel.

:::from Rust
Rust tampoco se recupera de un desbordamiento de pila: `catch_unwind` no lo atrapa, y el programa aborta con `thread 'main' (…) has overflowed its stack` y `fatal runtime error: stack overflow, aborting`. Cambia la distancia: un hilo nuevo de Rust recibe 2 MiB de pila (en las plataformas de primer nivel), y una goroutine crece hasta 512 MiB antes de rendirse.
:::

:::from Python
Python 3.14 corta antes y deja capturarlo: pasado `sys.getrecursionlimit()`, 1 000, la recursión lanza `RecursionError: maximum recursion depth exceeded`, que `except` recoge.
:::

:::from TS
En Node 24 tampoco hay fatal error: la recursión sin fondo lanza `RangeError: Maximum call stack size exceeded`, y `catch` la recoge.
:::

### Descenso recursivo y Pratt

Las reglas van en los comentarios del código, con la notación de la especificación de Ki («el spec», `docs/superpowers/specs/2026-10-24-ki-design-go.md` en la referencia, apartado 5.3): `item = { attr } [ 'pub' ] 'fn' lower '(' …`. Entre comillas va un token; `[ x ]` es x opcional; `{ x }`, x cero o más veces; `|` separa alternativas y `( )` agrupa. `lower` (`LowerIdent`), `string` y `Newline` son clases de token, no reglas. Para las firmas y las sentencias basta mirar el token actual: `item` decide por `#`, `pub` o `fn`; `block`, por `let` o por cualquier otra cosa. Ki no hace *backtracking* (spec 5.3): cada decisión se toma con el token que hay delante y no se deshace.

Las expresiones necesitan más. Con una función por nivel de precedencia, `1` pasaría por seis llamadas antes de llegar a `primary`. **Pratt** pone los niveles en una tabla de **potencias de enlace**: cuanto mayor, más fuerte sujeta el operador a sus operandos.

| Potencia | Operadores |
|---|---|
| 1 | `\|\|` |
| 2 | `&&` |
| 3 | `==` `!=` `<` `<=` `>` `>=`, que no se encadenan |
| 5 | `+` `-` |
| 6 | `*` `/` `%` |
| 7 | prefijos `-` y `!` |

La llamada, postfija, va por encima de todos. El 0 y el 4 quedan libres para `|>` y `..`, que llegarán más adelante. El algoritmo entero:

```text
pratt(minBP):
  lhs = prefix()                       // a literal, a name, a group, -x, !x
  loop:
    if the token is `(`: lhs = call(lhs, arguments); continue
    if the token is an operator with power bp >= minBP:
      consume it; rhs = pratt(bp + 1); lhs = (op lhs rhs)
    else: return lhs
```

Con `1 - 2 * 3`, `pratt(0)` lee `1` y ve `-` (5): pide el operando derecho con `pratt(6)`, que lee `2`, ve `*` (6 ≥ 6), lo consume y devuelve `(* 2 3)`. El resultado es `(- 1 (* 2 3))`.

El `bp + 1` decide la **asociatividad**. En `1 - 2 - 3`, el `pratt(6)` del primer operando derecho ve el segundo `-` (5 < 6) y vuelve, y el bucle de fuera lo toma: `(- (- 1 2) 3)`, por la izquierda. Con `pratt(bp)`, el segundo `-` entraría en el operando derecho y saldría `(- 1 (- 2 3))`, que vale 2 en lugar de -4.

**Las comparaciones no se encadenan.** En Python, `1 < 2 < 3` es `1 < 2 and 2 < 3`; en Go, `a < b < c` compara un `bool` con un `int` (`invalid operation: a < b < c (mismatched types untyped bool and int)`). Ki hace lo de Rust, que la rechaza con el mismo mensaje: `comparison operators cannot be chained` (rustc 1.97.1). Tras una comparación, si el token siguiente es otra comparación, el parser informa E0203 sobre ese segundo operador, una sola vez, con la ayuda «use `&&`: `a < b && b < c`», lee el resto de la cadena y la convierte entera en un nodo de error.

### Recuperación, sincronización y supresión

Un parser que se detiene en el primer error obliga a compilar una vez por error. El de Ki sigue, con tres piezas:

- **Nodos de error.** Donde el texto deja de tener sentido queda un `ItemError`, `StmtError`, `TypeError` o `ExprError`, que el volcado escribe `(error)`. El árbol está completo, y los pases posteriores no deben informar sobre un nodo de error: ya lo hizo el parser.
- **Sincronización.** Cuando `block` recibe una sentencia con un error, salta hasta su final: el siguiente `Newline` o `;` que no esté dentro de un delimitador abierto por lo que salta, un `}` que no cierre una llave abierta por lo saltado (así cierra el bloque y, de paso, un `(` que la sentencia dejó abierto), o un `let` que empieza una línea, fuera de esos delimitadores. El `let` está porque tras un `=` la línea no ha acabado y la regla de línea no pone `Newline`: en `let =⏎let y`, el salto se comería la sentencia siguiente. Uno a media línea se salta: `foo(let x = 1)` da un solo error (T06.86). Tras un error en un item, salta hasta el siguiente `fn`, `pub`, `#`, `type`, `trait`, `impl`, `effect` o `import` fuera de delimitadores.
- **Supresión.** El spec (7.3) pide no emitir errores nuevos «a menos de 3 tokens del anterior». Un error tan cerca del anterior suele ser el mismo visto desde otro sitio. Uno suprimido no mueve el punto desde el que se mide.

Sin estas piezas, seguir en el token siguiente, como en la sesión 3, da seis diagnósticos para `fn f() { let =⏎let = }`: «expected identifier», «expected newline» y «expected expression» sobre cada `=`. Con ellas, uno.

La promesa es no dar cascadas: un segundo error de la sentencia sale si el parser llega a él a 3 tokens o más del primero (`g(,) + h(,)` da dos; `f(1 2) + g(,)`, uno: un hueco no corta la expresión, pero tras el `)` que falta el salto se come `g(,)`). Solo una entrada cuesta más sin límite: una `{` sin cerrar se traga todo lo de detrás, porque un `fn` dentro de un bloque será una sentencia (funciones locales), y cada línea que empieza un item da un E0201; con 100 líneas o más, el E0202, que se informa el último, pasa del límite de `diag.MaxErrors` (T06.101). Otras cuestan uno o dos errores más, como una `{` que, en la línea siguiente a la condición de un `if`, ya no es su bloque, con un `else` detrás (T06.102), o `if b == == {`, cuyo segundo `==` toma el bloque por operando.

**La distancia se cuenta en tokens que no son `Newline`.** Un `Newline` no es texto del programa, sino una marca de la regla de línea: si contara, el mismo código daría errores distintos según cómo se reparta en líneas. `go/parser` suprime por líneas: sin el modo `AllErrors`, descarta todo error en la misma línea que el anterior. En Go 1.27, `ParseFile` con `f(1 2); f(1 2); f(1 2)` en una línea da un error; repartido en tres líneas, tres. `ki check` da tres en los dos casos.

Dos reglas más. Un delimitador abierto al final del fichero es E0202, `unclosed delimiter`, con la etiqueta en el delimitador («this `{` is never closed») y no en el final, y solo del más interno: `fn f() { (1` informa del `(`; un error a menos de 3 tokens del final lo suprime: `fn f() {⏎  g(,)⏎` solo da el E0201. Y un token `Error` del lexer, como un `$` suelto, ya tiene su diagnóstico: el parser lo cuenta como error de la sentencia, pero no dice nada más.

### La búsqueda acotada

Ki tendrá lambdas, `(x, y) => x + y`, y al ver `(` el parser no sabe si empieza una lambda o un grupo, `(x + y) * 2`. El spec (5.3) lo resuelve con una **búsqueda acotada**: al ver `(`, buscar el `)` emparejado contando niveles y mirar si lo sigue `=>`. Es barata: recorre los tokens sin consumir nada. No es *backtracking*: se decide antes de leer. Llega en el capítulo 15.

### El AST en tablas: `IdVec`, pools y `ListRange`

El capítulo 4 construyó `IdVec` y `ListRange` y midió qué gana el recolector con objetos sin punteros. El AST los usa así: un `Module` guarda cuatro `IdVec` (items, sentencias, expresiones y tipos) y cuatro pools; un nodo nombra a otro por su id, y una lista de hijos es un `ListRange` sobre un pool. Lo nuevo:

- **Contigüidad.** Los argumentos de `f(g(1), 2)` son expresiones con argumentos propios. Si `callArgs` añadiera cada argumento al pool al leerlo, los de `g` quedarían intercalados con los de `f`. Por eso cada lista se junta en un slice local y `appendRange` la añade de una vez. Recibe `*[]T`, un puntero al slice, porque `append` devuelve una cabecera nueva y el `Module` tiene que quedarse con ella (en Rust, `&mut Vec<T>`).
- **El id 0 es un id.** Un hijo opcional lleva una marca al lado: `Ret` y `HasRet`, `Else` y `HasElse`.
- **Vistas recortadas.** `ExprList` devuelve `pool[First:end:end]` sobre el pool recortado a su longitud: un `append` de quien recibe la lista realoja en lugar de pisar la lista siguiente.

¿Por qué ids y no punteros en un lenguaje con GC? **Compacidad**: un `Expr` ocupa 104 bytes (`unsafe.Sizeof`, en esta máquina), y el mismo struct con punteros y slices en lugar de ids y rangos, 176. **Nodos sin punteros**: ningún campo de `Expr` es un puntero, así que el array de `Exprs` es de los que el recolector no recorre. **Tablas laterales**: un pase posterior guarda lo que sabe de cada nodo en sus propias tablas indexadas por id, sin tocar el árbol. **Volcados estables**: un id es el orden de creación, no una dirección de memoria, así que dos ejecuciones dan el mismo volcado.

:::from Rust
En Rust, un árbol de `Box<Expr>` se libera solo y el *borrow checker* decide quién puede cambiarlo; un árbol de ids en un `Vec` cambia esas reglas por índices que el compilador no comprueba. En Go el recolector ya resuelve la memoria de un árbol de punteros, así que los ids no se eligen por corrección sino por lo de arriba.
:::

### `switch` sobre `Kind` y `panic("unreachable")`

`Expr` no es una interfaz sellada (capítulo 3) sino un struct con un campo `Kind`, porque un valor de interfaz lleva un puntero: el array de `Exprs` dejaría de estar libre de punteros y cada nodo sería una reserva. La clase dice qué campos significan algo, y los demás quedan a cero. Cada `switch` sobre `Kind` termina en `default: panic("unreachable")`. El `switch` de Go no es exhaustivo: si mañana existe `ExprMatch` y el impresor no tiene su `case`, compila, y el `default` convierte el olvido en un panic con traza en lugar de un volcado al que le falta un nodo.

:::from Rust
Un `match` sobre un `enum` sí es exhaustivo: olvidar una variante no compila, ``error[E0004]: non-exhaustive patterns: `Kind::Error` not covered``. En Go, esa red es el `default` y los tests que recorren cada clase.
:::

### Closures en los ayudantes de los tests

Un closure (capítulo 1) ve las variables de la función que lo contiene, y las ve **como variables**, no como copias de su valor: con `n := 1; get := func() int { return n }; n = 2`, `get()` devuelve 2. Los tests lo usan en ayudantes de un solo subtest, como `spanAt` y `span` en `TestParser/spans`. `TestPrinter` va más lejos: cada fila de su tabla lleva un campo `build func(b *tree) syntax.ExprID`, una función que construye el árbol de esa fila.

:::from Rust
El mismo ejemplo no compila en Rust: ``error[E0506]: cannot assign to `n` because it is borrowed``, porque el closure toma prestada `n` mientras vive. En Go, el closure y la función comparten la variable, y el recolector la mantiene viva mientras alguno la use.
:::

### El impresor y las S-expresiones

`PrintModule` acumula el texto en un `strings.Builder` (capítulo 3) dentro de un struct `printer` y devuelve un `string`; quien lo escribe en un `io.Writer` es `ki emit`. Las S-expresiones no tienen ambigüedad (cada nodo lleva sus paréntesis), y un golden las compara con `diff`.

### Fuzzing del parser y nombres

`FuzzParse` hace con el parser lo que `FuzzLex` hacía con el lexer: cualquier entrada pasa por `Lex` y `Parse`, y se comprueba que no hay panic, tampoco al recorrer el árbol con `PrintModule`, y que todo span del árbol y de los diagnósticos cae dentro del fichero.

Los nombres siguen la guía de estilo de Google, que pide no repetir en un nombre exportado lo que ya dice su paquete. Por eso es `syntax.Parse`, no `syntax.SyntaxParse`, y los accesores de `Module` son `Params` y `Attrs`, sin `Get`.

## Tests primero

{{cards}}

Úsalas como siempre: escribe el test de la tarjeta, míralo fallar, implementa lo justo para el verde, y solo entonces despliega la referencia con su `{{reftest}}`.

Los tests viven en dos paquetes. `parser_test.go`, `ast_test.go`, `ast_printer_test.go`, `recover_test.go` y `parser_fuzz_test.go` son `package syntax_test` y solo usan lo exportado. `parser_internal_test.go`, `parser_expr_internal_test.go` y `parser_stmt_internal_test.go` son `package syntax`, porque llaman a métodos sin exportar como `p.expr(0)` y `p.letStmt()`. Los ayudantes no se exportan, así que `syntax_test` no ve los de `package syntax`, y `recover_test.go` tiene copias de algunos.

Las tarjetas describen el final del capítulo. Unas pocas dependen de los bloques de la sesión 3 o de la recuperación de la 4, y antes se escriben de otra forma: cada sesión dice cuáles, y esa forma está en `git show ref/ch06-sK:book/tests/ch06.tests` y en el test de `ref/ch06-sK`.

## Implementación

### Sesión 1: el AST y las firmas de función (≈ 3 h)

#### Los tests, en rojo

T06.01–T06.19 describen esta interfaz: el AST entero, que es estructura de datos y se copia tal cual,

{{file ch06-s1 internal/syntax/ast.go}}

y estas funciones:

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

Sobre `ch05-s4` no compilan: `parser_internal_test.go:19:8: undefined: newParser`. Copia `ast.go` y añade stubs: `Parse` devuelve `&Module{File: file}`, `PrintModule` devuelve `""`, y el parser es un struct con `tb`, `pos` y `end` cuyos `advance`, `at` y `eat` devuelven `Token{}`, `false` y `Token{}, false`. Entonces `TestAST` y `empty_module` pasan; fallan, sin panic, `TestParserCursor`, los tres subtests de `TestPrinter` y doce de `TestParser`.

Los ayudantes: en `parser_test.go`, `parseFile` lexa con el `lexFile` del capítulo 5 y parsea; `printModule` exige cero diagnósticos y devuelve el volcado; `wantModule` lo compara. En `ast_printer_test.go`, `tree` construye un `Module` a mano (`intLit`, `call`, `binary`…), para probar el impresor sin el parser, y `printBody` y `wantBody` imprimen una función `f` con ese cuerpo.

Cambia tu `lexFile`: un fichero vacío delante da al de prueba el id 1, que `lexFile` comprueba (T06.12):

{{diff ch05-s4..ch06-s1 internal/syntax/lexer_test.go}}

T06.13 se escribe hoy como en `ref/ch06-s1`: con 46 fuentes, `fn f() {` entre ellas (hoy un cuerpo solo puede ser `{}`), y sin mirar diagnósticos, porque el parser aún no informa de nada.

#### El código

{{file ch06-s1 internal/syntax/parser.go}}

- **El cursor.** `advance` nunca consume el `Eof`: un bucle que olvide comprobarlo no sale del buffer. `end` es el final del último token consumido sin contar los `Newline`, y `spanFrom` lo usa para que el span de un nodo acabe en su último token y no en el salto de línea.
- **`item`** consume siempre al menos un token, y donde la firma deja de tener sentido devuelve un `ItemError` con lo leído. Una función tiene cuerpo si un `{` sigue a su firma en la misma línea: en la siguiente, la regla de línea pone antes un `Newline`. `pub fn print(x: Int)` sin cuerpo es una declaración externa.
- **`attrs`** salta los `Newline` tras cada atributo, porque `]` cierra una expresión y la regla de línea pone uno. Quita ese `skipNewlines` y T06.09 lo ve: el atributo de `#[extern("ki_print_int")]⏎pub fn print(x: Int)` sale como un `(error)` aparte.
- **`attrs` y `params`** juntan la lista en un slice local y solo la añaden al pool si sale bien: una lista que falla no deja nada (T06.13).
- **`typeExpr`** ante un error no consume nada salvo un `(`: el `)` que cierra los parámetros sigue ahí para quien llama.

{{file ch06-s1 internal/syntax/ast_printer.go}}

`item`, `optType`, `stmt` y `expr` son un `switch` por clase con su `default: panic("unreachable")`. Cada `case` escribe su forma y baja a cada hijo. En `ast.go`, si quitas de `slice` la línea que recorta el pool, T06.16 falla: `ExprList of a range inside the capacity but past the length did not panic`.

{{reftest T06.14}}

{{reftest T06.16}}

El mensaje de la etiqueta: `ch06-s1: the AST and function signatures`.

Si te atascas o tu versión se desvía demasiado, copia el fichero de `ref/ch06-s1` y sigue.

Al terminar: tus tests en verde, commit y etiqueta `ch06-s1`; compárate con `ref/ch06-s1`.

### Sesión 2: expresiones con Pratt y el límite de cadena (≈ 3 h)

#### Los tests, en rojo

T06.20–T06.50 van en `parser_expr_internal_test.go`. La interfaz nueva es interna:

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

Sobre `ch06-s1` no compilan (`r.p.expr undefined`). Con `MaxChain`, el tipo, el campo, un `expr` que devuelve `p.m.Exprs.Push(Expr{Kind: ExprError})` y un `skipStatement` vacío, los 31 subtests de `TestParseExpr` fallan sin panic: `"1 + 2 * 3": the expression stops at token 0 (Int), want it to use the whole source`.

Los ayudantes están al principio del fichero en `ref/ch06-s2`: `newExprRun` pone el parser en el primer token de un texto de id 1 (`newSource`); `parseExprSexpr` lee una expresión con `p.expr(0)`, exige cero diagnósticos y todo el texto usado, y la imprime; `r.sexpr` imprime una expresión; `r.wantAt`, dónde quedó el cursor; `lastIndex`, dónde aparece un texto por última vez; `wantExpr` y `wantText` comparan; `wantOnlyDiag` exige un único diagnóstico, con `reflect.DeepEqual`; `chainedDiag` y `tooLongDiag` escriben el E0203 y el E0205 esperados; `plusChain(n)` da `1` y `n` veces ` + 1`, y `wrapPlus`, su volcado.

T06.45 sin su fila `call with a semicolon inside`, y T06.49 y T06.50 con el estado de `chain` puesto a mano (T06.50, con el nombre `skip_statement_counts_consumed_braces`), se escriben hoy como en `ref/ch06-s2`.

#### El código

{{file ch06-s2 internal/syntax/parser_expr.go}}

{{diff ch06-s1..ch06-s2 internal/syntax/parser.go}}

- **`binaryOp`** es la tabla de potencias, como un `switch` que devuelve tres valores.
- **`pratt`** es el algoritmo de la teoría. Con `p.pratt(bp)` en lugar de `bp + 1`, T06.21 da `(- 1 (- 2 3))`; si `prefix` leyera su operando con `pratt(0)`, T06.22 daría `(neg (* a b))`.
- **E0203.** Tras construir una comparación, si el token siguiente es otra, `chainedComparison` informa y lee el resto. Quita esa comprobación y T06.24 recibe `(< (< a b) c)` sin diagnóstico.
- **La cadena.** `countChain` cuenta cada operador binario y cada llamada de la expresión de sentencia, también dentro de paréntesis y argumentos; los prefijos no cuentan, porque su profundidad es asunto del límite de anidamiento. Al pasar de 1 024 informa E0205 una vez y marca `tooLong`; desde ahí todos los bucles paran, y `expr` salta el resto de la sentencia y devuelve un solo nodo de error. `expr` guarda la cuenta de fuera y la restaura: una sentencia dentro de un bloque dentro de una expresión tendrá la suya.
- **`group`** no crea nodo: devuelve la expresión de dentro. `callArgs` junta los argumentos y los añade al pool al final, por la contigüidad.

`Parse` aún no llama a `expr`: un cuerpo sigue siendo `{}`.

{{reftest T06.34}}

{{reftest T06.30}}

El mensaje de la etiqueta: `ch06-s2: expressions with Pratt parsing and the chain limit`.

Si te atascas o tu versión se desvía demasiado, copia el fichero de `ref/ch06-s2` y sigue.

Al terminar: tus tests en verde, commit y etiqueta `ch06-s2`; compárate con `ref/ch06-s2`.

### Sesión 3: bloques, `let` e `if` (≈ 2,5 h)

#### Los tests, en rojo

T06.51–T06.72 van en `parser_stmt_internal_test.go`. La interfaz exportada no cambia; la interna:

```go
func (p *parser) letStmt() StmtID                            // T06.54 reads a let with it
func (p *parser) block() ExprID                              // moves to parser_stmt.go
func (p *parser) ifExpr() ExprID
func (p *parser) exprOrSkip(minBP int) (e ExprID, abandoned bool)
func (p *parser) expected(what string)                       // E0201 at the cursor
func (p *parser) expect(k TokenKind, what string) (Token, bool)
func (tb *TokenBuffer) text(t Token) string                  // Lex keeps the source in tb
```

El parser empieza a informar E0201, `expected <what>, found <token>`. `<what>` es `expression`, `identifier`, `type`, `item`, `` `=` ``, `` `)` ``, `` `{` ``, `` `}` `` o `newline`, y en la firma y los atributos también `` `(` ``, `` `[` ``, `` `]` `` y `string`. `` `{` `` solo lo pide la rama de un `if`: un `fn` sin `{` en su línea es una función sin cuerpo, no un error. El token hallado se escribe con su texto del fichero entre comillas invertidas, o `newline`, o `end of file`.

T06.13 toma su forma final: la fila `fn f() {` se va (un bloque sin cerrar es ya otro asunto) y cada fuente debe dar un E0201, un E0202 o el E0104 del lexer.

Sobre `ch06-s2` no compila (`r.p.letStmt undefined`). Con un `letStmt` que devuelve `p.m.Stmts.Push(Stmt{Kind: StmtError})`, fallan los 22 subtests de `TestParseStmt` y `TestParser/malformed_input_still_parses`. Pero dos subtests de la referencia indexan sin comprobar la longitud, y en rojo hacen panic y cortan el binario: `let_missing_equals_keeps_the_name` (`index out of range [0] with length 0`) y `found_text_is_the_source_text` (`index out of range [1] with length 1`). En tu versión, pon delante un `t.Fatalf` que compruebe la longitud; con la referencia, ejecuta el rojo con `-skip`, el contrario de `-run`: `go test ./internal/syntax -skip 'TestParseStmt/(let_missing_equals_keeps_the_name|found_text_is_the_source_text)$'`.

T06.65, T06.70 y T06.71 se escriben hoy como en `ref/ch06-s3`, porque dependen de la recuperación de la sesión 4: aquí el `$` da dos errores (el del lexer y un E0201), una llave sin cerrar es E0201 `` expected `}`, found end of file ``, un `if` abandonado no arrastra lo que lo contiene y tras un error el parser sigue en el token siguiente. Hay tablas largas (T06.69 tiene 41 filas): escribe las primeras filas de cada mensaje, míralas fallar, y copia el resto de `ref/ch06-s4`.

Los ayudantes están al principio de `parser_stmt_internal_test.go` en `ref/ch06-s3` y reutilizan los de la sesión 2: `parseLetSexpr` lee un `let` con `p.letStmt()`; `parseModule` parsea un texto sin errores léxicos y devuelve un `moduleRun` con `text`, `messages` y `body` (el cuerpo de la primera función); `wantModuleText` compara el volcado y exige cero diagnósticos; `e0201` escribe un E0201; `nth` y `after` dan posiciones en el texto.

#### El código

{{file ch06-s3 internal/syntax/parser_stmt.go}}

{{diff ch06-s2..ch06-s3 internal/syntax/parser.go}}

{{diff ch06-s2..ch06-s3 internal/syntax/parser_expr.go}}

{{diff ch06-s2..ch06-s3 internal/syntax/token.go}}

{{diff ch06-s2..ch06-s3 internal/syntax/lexer.go}}

- **`block`** decide qué es cada sentencia por lo que la sigue. Un `;` la hace sentencia; un `Newline`, también, salvo que detrás venga el `}`: entonces es la cola, el valor del bloque. Si en el `Newline` quitas `&& p.at(RBrace)`, T06.60 da `(block (call g))` para `{ f()⏎ g() }`: `f()` se toma como cola y `g()` la pisa.
- **`letStmt`** sin `=` sigue siendo un `let` que liga el nombre, con un nodo de error vacío por inicializador: las sentencias de después pueden usar `x` sin un error más.
- **`ifExpr`** lee la condición con `exprOrSkip`, que es `expr` y además dice si la expresión se abandonó: entonces el resto de la sentencia, ramas incluidas, ya se saltó, y el `if` no debe buscar su `{`.
- **`found`** escribe el texto del token con `tb.text`, que corta `src`, el fichero que `Lex` guarda ahora en el buffer: `0xFF` sale como `` `0xFF` ``, no como 255.

{{reftest T06.60}}

{{reftest T06.64}}

El mensaje de la etiqueta: `ch06-s3: blocks, let and if`.

Si te atascas o tu versión se desvía demasiado, copia el fichero de `ref/ch06-s3` y sigue.

Al terminar: tus tests en verde, commit y etiqueta `ch06-s3`; compárate con `ref/ch06-s3`.

### Sesión 4: recuperación, límite de anidamiento y `ki emit ast` (≈ 2,5 h)

#### Los tests, en rojo

T06.73–T06.105 añaden `TestRecover`, `FuzzParse`, los tres golden, `TestCLI/emit_ast` y `TestCLI/emit_dump_before_diagnostics`. La interfaz:

```go
const MaxNesting = 256
// usage: ki emit tokens|ast [--diagnostics=human|short|json] FILE

// unexported: chainState loses from and tooLong, and gains abandoned (E0205,
// E0204 or an abandoned condition); skipStatement becomes syncStatement.
```

Antes, lleva a su forma final T06.45, T06.49, T06.50, T06.65, T06.70 y T06.71, como en estos diffs, y cambia la línea de uso en `TestCLI`. `emit_ast` y `emit_dump_before_diagnostics` son nuevos: escríbelos desde T06.100 y T06.103 antes de leer el último diff:

{{diff ch06-s3..ch06-s4 internal/syntax/parser_expr_internal_test.go}}

{{diff ch06-s3..ch06-s4 internal/syntax/parser_stmt_internal_test.go}}

{{diff ch06-s3..ch06-s4 internal/driver/cli_test.go}}

Las demás líneas de los diffs (T06.48, T06.59, T06.69, T06.72 y comentarios), y T06.13 en `parser_test.go`, son de la referencia, que escribió esas tarjetas antes en otra forma; las tuyas ya están así. Una llave sin cerrar deja de ser E0201 y pasa a E0202.

Sobre `ch06-s3`, el paquete de test no compila (`undefined: syntax.MaxNesting`). Con la constante, fallan 22 subtests de `TestRecover`, `chain_skips_to_statement_end` de `TestParseExpr`, tres de `TestParseStmt` (`if_condition_chain_limit`, `found_text_is_the_source_text` y `recovery_in_blocks`), tres de `TestCLI` (`emit_unknown_target`, `emit_ast` y `emit_dump_before_diagnostics`) y los tres golden; ninguno hace panic. `FuzzParse` y cinco de `TestRecover` ya pasan. `suppression_at_distance_two` enseña el problema entero: seis diagnósticos donde quiere uno.

Los ayudantes, al principio de `recover_test.go` en `ref/ch06-s4`: `recParse` lexa, parsea y devuelve un `recRun`; `at` da el span de la enésima aparición de un texto; `wantDiags` compara la lista entera de diagnósticos; `wantText`, el volcado; `recE0201`–`recE0204` escriben los esperados; `wrapLet` pone una expresión como inicializador de un `let` en `main`.

#### Recuperación y anidamiento

{{file ch06-s4 internal/syntax/recover.go}}

{{diff ch06-s3..ch06-s4 internal/syntax/parser.go}}

{{diff ch06-s3..ch06-s4 internal/syntax/parser_expr.go}}

{{diff ch06-s3..ch06-s4 internal/syntax/parser_stmt.go}}

- **Supresión.** `advance` cuenta en `index` los tokens que no son `Newline`, y `tokIndex` da a un error hallado en un `Newline` el índice del token al que sigue. `admit` cuenta el error con `detect` y decide si se informa; `report` y `expected` pasan por ella, así que vale para todos los códigos (T06.85, con E0205 y E0204). Si `advance` contara también los `Newline`, T06.78 lo ve: en `fn f() { let x⏎⏎⏎let = }` la distancia pasaría de 2 a 3 y saldría un segundo error.
- **Sincronización.** `block` compara `p.errs` antes y después de cada sentencia, y si cambió llama a `syncStatement`. `trackBracket` lleva los delimitadores con la `bracketStack` de la regla de línea. Quita la parada en `let` y T06.77 recibe un solo diagnóstico: el salto se come `let y`. Tras una sentencia, `block` pide separador solo en la misma línea: tras un `(` sin cerrar no hay `Newline` hasta el `}`, y vale el salto del fichero. Sin `!p.tok().NewlineBefore`, la primera fuente de T06.104 da cuatro errores.
- **Anidamiento.** `enter` y `leave` rodean cada paréntesis, argumento, operando de prefijo, condición, rama y bloque. `depth` no es de ninguna expresión: una sentencia dentro de un bloque anidado empieza a la profundidad del bloque (T06.96). Con `p.depth > MaxNesting` en lugar de `==`, T06.79 acepta 257 paréntesis.
- **Abandono.** E0204 marca la expresión como abandonada, igual que E0205; `condition` propaga el abandono al `if` y a lo que lo contiene, porque lo que esperaban ya se saltó. Las tres guardas «Cannot happen today» no tienen test que las alcance: puedes omitirlas.
- **`expected`** calla ante un token `Error`, pero lo cuenta. Sin ese `if`, T06.91 ve ``expected expression, found `$` `` tras el E0104 del lexer.

{{reftest T06.86}}

{{reftest T06.83}}

#### `ki emit ast` y `ki check`

{{diff ch06-s3..ch06-s4 internal/driver/emit.go}}

{{diff ch06-s3..ch06-s4 internal/driver/check.go}}

`finishEmit` termina los dos destinos igual: el volcado por stdout y luego los diagnósticos por stderr (T06.103), y el código de salida al final, así que el árbol de un fichero con errores se ve entero. `ki check` pasa ahora por el parser, que no se para en el primer error:

{{file ch06-s4 tests/errors/ch06/three_errors.ki}}

{{file ch06-s4 tests/golden/ch06/fib_ast.ki}}

#### El fuzz

{{reftest T06.99}}

Ejecútalo con `go test ./internal/syntax -run='^$' -fuzz=FuzzParse -fuzztime=10s`: del orden de un millón de entradas, `PASS`, y nada en `testdata/`. Las semillas incluyen el texto del golden, leído con una ruta relativa (`../../tests/…`), que funciona porque `go test` ejecuta el binario en el directorio del paquete. `sp == (base.Span{})` compara con el valor cero; sin los paréntesis, Go leería la `{` como el cuerpo del `if`.

Un bucle que no consume tokens es otra cosa. Cambia en `block` el `break` que sigue a `p.unclosed(open)` por `continue` y repite el fuzz con `-parallel=1`, un solo proceso de fuzzing: el contador se para en `execs: N (0/sec)`, o en `gathering baseline coverage: k/n completed` si la caché ya guarda el corpus de antes, y la ejecución termina en `PASS`: una entrada que no vuelve no cuenta como fallo. Lo caza el tiempo máximo de `go test`: `go test -run TestRecover -timeout 20s ./internal/syntax` corta T06.74 con `panic: test timed out after 20s`.

El mensaje de la etiqueta: `ch06-s4: error recovery, nesting limit and ki emit ast`.

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

- La pila de una goroutine crece por copia hasta 512 MiB (el límite es 1 GB); pasarse es `fatal error: stack overflow`, que `recover` no captura. Por eso el parser acota el anidamiento (256, E0204) y las cadenas (1 024, E0205).
- Descenso recursivo: una función por regla, decidida por el token actual, sin *backtracking*.
- Pratt: una tabla de potencias y un bucle; `pratt(bp + 1)` da asociatividad por la izquierda. Las comparaciones no se encadenan (E0203).
- Un error deja un nodo `Error`; el parser salta al final de la sentencia o del item y no informa de errores a menos de 3 tokens del anterior, contados sin los `Newline`.
- Un delimitador abierto al final del fichero es E0202 sobre él; un token `Error` ya informado no da otro diagnóstico.
- La búsqueda acotada mira hacia delante sin consumir nada; llega con las lambdas.
- El AST vive en `IdVec` y pools con `ListRange`: nodos sin punteros y más pequeños, tablas laterales por id y volcados estables.
- Un `switch` sobre `Kind` no es exhaustivo; `default: panic("unreachable")` hace ruidoso el olvido.
- Un closure captura variables, no valores.
- `PrintModule` escribe S-expresiones en un `strings.Builder`; `ki emit ast` las saca por stdout, y `ki check` pasa ya por el parser.
- `FuzzParse` vigila que no haya panic y que todo span esté en el fichero; un bucle que no consume tokens no lo caza el fuzzer, sino el `-timeout` de `go test`.

## Go en el trabajo

**Parsers a mano.** Los dos parsers de Go en Go 1.27.3 están escritos a mano. `cmd/compile/internal/syntax/parser.go`, el del compilador, lee las expresiones binarias con un bucle `for … p.prec > prec` que llama a `p.binaryExpr(nil, tprec)` con la precedencia del operador, el mismo esquema que `pratt`. `go/parser` acota su recursión con `maxNestLev = 1e5`, `exceeded max nesting depth`, y un comentario que dice para qué: «to prevent stack exhaustion». Su recuperación es la de Ki con otras piezas: `advance(stmtStart)` salta hasta una palabra clave que empieza sentencia, y un contador, `syncCnt`, garantiza que el parser avanza.

**Errores en el editor.** Un editor que analiza el código mientras se escribe ve casi siempre un programa a medias, y un parser que se rinde en el primer error no le sirve. `go/ast`, el AST que comparten `go/printer`, `go/format` y `cmd/gofmt`, tiene sus nodos de error: `BadExpr`, «a placeholder for an expression containing syntax errors for which a correct expression node cannot be created», `BadStmt` y `BadDecl`.

**Variables de bucle y closures.** Un clásico de entrevista: `for i := 0; i < 3; i++ { fs = append(fs, func() int { return i }) }` y después llamar a cada `f`. Con `go 1.21` en el `go.mod`, compilado con Go 1.27.3, imprime `3 3 3`: todas las iteraciones compartían una variable, y los closures la capturan, no su valor. Con `go 1.22`, `0 1 2`. Es el `var` frente al `let` de un `for` en JavaScript.
