---
chapter: 5
title: El lexer y la regla de línea
part: I
hours: 8
milestone: `ki emit tokens`
---

## Objetivo

Al terminar, `ki` tiene su primera etapa de compilador: el lexer, que convierte los bytes de un fichero en tokens, decide qué saltos de línea terminan una sentencia y sigue adelante tras cada error. Lo enseña un comando nuevo:

```console
$ cat fib.ki
fn main() {
  let n = fib(20)
    |> show
  print("fib = ${n}\n")
}
$ ki emit tokens fib.ki
…
2:17 RParen
3:5 Pipe
3:8 LowerIdent(show)
3:12 Newline
…
4:9 StringHead("fib = ")
4:18 LowerIdent(n)
4:19 StringTail("\n")
…
```

No hay `Newline` antes de `|>`, que continúa la línea, y el string se parte en cabeza, expresión y cola. Es el hito del capítulo; el parser del capítulo 6 leerá estos tokens, camino del hito M1 (capítulo 10).

## Teoría

### Bytes, runas y el maximal munch

El capítulo 2 separó bytes y code points, con `utf8.DecodeRune(b)`, que devuelve el primer code point y cuántos bytes ocupa. El lexer de Ki trabaja casi siempre con bytes: todo lo que tiene significado es ASCII, y ningún byte de un carácter multibyte cae por debajo de 0x80. Solo decodifica en tres sitios:

- **Un carácter inesperado.** `é` son dos bytes; avanzando de uno en uno saldrían dos errores de medio carácter. `DecodeRune` da su tamaño, y el `Error` lo cubre entero.
- **Un escape inválido.** En `\é`, `DecodeRune` da el tamaño de la `é`, y el escape la consume entera.
- **Un literal de carácter.** `'é'` tiene dos bytes y un solo code point: el lexer junta el contenido y comprueba que `DecodeRune` lo consume entero.

En un `\u{…}` no hay nada que decodificar: `utf8.ValidRune(r)` dice si un número es un escalar Unicode (`0xD7FF` y `0xE000` sí, los sustitutos `0xD800` y `0xDFFF` no, `0x10FFFF` sí y `0x110000` no), y `utf8.AppendRune(b, r)` añade su codificación a un `[]byte`.

`utf8.RuneError` es U+FFFD, el valor que toma un literal roto. Para los mensajes, `unicode.IsPrint(r)` dice si un carácter se ve al imprimirlo; es falso para el tabulador, U+0085, U+2028, U+200B, U+202E y U+00A0 (que `unicode.IsGraphic` sí acepta). El lexer escribe esos caracteres como `<U+XXXX>` con `fmt.Fprintf(&b, "<U+%04X>", r)`: `%04X`, el hexadecimal en mayúsculas con al menos cuatro cifras.

:::from Rust
`char` de Rust solo admite escalares: `char::from_u32(0xD800)` es `None`, y el literal `"\u{D800}"` no compila (`invalid unicode character escape`, rustc 1.97). Un `rune` de Go es un `int32` cualquiera, así que la comprobación de `ValidRune` es tuya. La sintaxis `\u{1F600}`, de 1 a 6 cifras, es la de Rust.
:::

**Maximal munch.** Ante `|>=>`, el lexer toma siempre el token más largo posible: `|>`, después `=>`, y no `=` y `>`. Ningún token de puntuación de Ki pasa de dos caracteres, así que basta mirar el byte siguiente.

**Tablas de clases.** Muchos lexers deciden qué empieza en cada byte con una tabla de 256 entradas:

```go
const (
	other uint8 = iota
	letter
	digit
)

var classes = func() (t [256]uint8) {
	for c := 'a'; c <= 'z'; c++ {
		t[c], t[c-'a'+'A'] = letter, letter
	}
	t['_'] = letter
	for c := '0'; c <= '9'; c++ {
		t[c] = digit
	}
	return t
}()

func class(c byte) uint8 { return classes[c] }
```

`classes` se calcula una vez, al iniciar el paquete, con una función literal que se llama en el acto. `[256]uint8` es un array: su tamaño es parte del tipo. Indexado con un `byte`, nunca se sale, y el compilador lo sabe: con `-gcflags=-d=ssa/check_bce/debug=1`, que informa de cada comprobación de límites que deja en el binario, `class` no aparece, y la misma función con un parámetro `int` sí (`Found IsInBounds`). El lexer de Ki no usa tabla: sus clases son pocas y caben en un `switch` y en funciones de una línea como `isIdentStart`.

### Números con `strconv`

`strconv.ParseInt(s, base, 64)` convierte texto en un `int64` y `strconv.ParseFloat(s, 64)` en un `float64`. Sus errores son `*strconv.NumError`, con tres campos: `Func` (`"ParseInt"`), `Num` (el texto) y `Err`, la causa, como `strconv.ErrRange` o `strconv.ErrSyntax` (una base o un tamaño inválidos dan otras). `NumError` tiene `Unwrap`, así que `errors.Is(err, strconv.ErrRange)`, del capítulo 2, la encuentra. En Go 1.27, comprobado:

| Llamada | Resultado |
|---|---|
| `ParseInt("9223372036854775808", 10, 64)` | `9223372036854775807` y `strconv.ParseInt: parsing "9223372036854775808": value out of range` |
| `ParseInt("", 16, 64)` | `0` y `invalid syntax` |
| `ParseInt("1_000", 10, 64)` | `0` y `invalid syntax` (con base 0, `1000`) |
| `ParseFloat("1e999", 64)` | `+Inf` y `value out of range` |
| `ParseFloat("1e-999", 64)` | `0` y `nil` |
| `ParseFloat("1_000.5", 64)` | `1000.5` y `nil`; con `1__0.5`, `invalid syntax` |

Dos consecuencias. Un desbordamiento da el máximo (o `+Inf`, en un `Float`) junto al error; si el lexer lo guardara, un literal rechazado seguiría valiendo algo, y por eso el token se queda con 0. Y como `ParseInt` en base 10 rechaza `_` y `ParseFloat`, en texto decimal, lo acepta solo entre dos dígitos, el lexer no delega la regla de los guiones bajos: los quita él y pasa a `strconv` el texto sin ellos.

:::from Python
`int("9223372036854775808")` funciona, porque los enteros de Python no tienen tamaño. El `Int` de Ki tiene 64 bits, como un `int64`, y el límite se comprueba al leer el literal.
:::

### `go generate`: el generador escrito a mano

Las 71 clases de token van en `token_kinds.txt`, una por línea, y un programa Go genera con ellas `token_kind.go`: las constantes, la tabla de nombres de `String` y el mapa de palabras clave. En C++ serían X-macros; en Go, el generador es Go normal.

- **La directiva.** Una línea `//go:generate go run ./gen -o token_kind.go` en `token.go` (sin espacio entre `//` y `go`). `go generate ./internal/syntax` busca esas líneas y ejecuta el comando en el directorio del paquete. Según `go help generate`, «Go generate is never run automatically by go build, go test, and so on. It must be run explicitly».
- **La cabecera.** El fichero generado empieza con una línea que case con `^// Code generated .* DO NOT EDIT\.$`: la convención (también de `go help generate`) por la que personas y herramientas saben que no se edita a mano.
- **`go/format`.** `format.Source(src)` formatea código Go como `gofmt`, así que el generador escribe sin preocuparse de alinear. No comprueba tipos, solo la sintaxis: con `package p\nfunc {` devuelve `2:6: expected 'IDENT', found '{'`.
- **El test que lo mantiene al día.** Como nada regenera solo, T05.01 ejecuta `go run ./gen` sin `-o` con `os/exec` (capítulo 1) y compara su stdout con `token_kind.go` byte a byte. Si añades `KwLoop` a la lista y no regeneras, falla con ``token_kind.go differs from the output of `go run ./gen`: run `go generate ./internal/syntax` ``.

**Lo justo de `bufio.Scanner`.** El generador lee la lista con `os.ReadFile` y la corta con `strings.Split`. Lo habitual para leer por líneas es `sc := bufio.NewScanner(r)`, `for sc.Scan() { line := sc.Text() }` y `sc.Err()` al final: quita el `\r` de un CRLF, y una línea de 64 KiB o más (`bufio.MaxScanTokenSize`) detiene el bucle con `bufio.Scanner: token too long`.

:::from Rust
Lo más parecido es una macro `macro_rules!` que reciba la lista de tokens y escriba el `enum` y su tabla. Pero una macro se expande en cada compilación, y `go generate` solo cuando lo pides: por eso lo generado se versiona y un test lo vigila.
:::

### Los modos del lexer y la interpolación

En `"a${x}b"`, la `x` es código y la `b` vuelve a ser texto: el lexer cambia de modo. Como la expresión de un `${…}` puede contener otro string, y ese string otra interpolación, los modos se apilan: una pila de `modeString` y `modeCode` alternados, donde la pila vacía es el código del fichero.

La pregunta difícil es qué hace una `}`. En `"${ g({ y }) }"`, la primera `}` cierra una llave y la segunda, la interpolación. El lexer lleva una pila de llaves: cada `{` apila una entrada normal y cada `${` apila un **centinela**, `InterpSentinel`. Una `}` mira la cima: si es el centinela, cierra la interpolación y el lexer vuelve al texto del string; si no, cierra una llave. Solo las llaves entran en esa pila, porque solo la `}` tiene que decidir; un `(` sin cerrar dentro de `${ ( }` no impide que la `}` cierre la interpolación.

:::from Python
Python 3.14 hace lo mismo en su tokenizador. `python3 -m tokenize` sobre `y = f"a{x}b"` da `FSTRING_START 'f"'`, `FSTRING_MIDDLE 'a'`, `OP '{'`, `NAME 'x'`, `OP '}'`, `FSTRING_MIDDLE 'b'` y `FSTRING_END '"'`. Ki junta las llaves con el texto: `StringHead("a")`, `x`, `StringTail("b")`.
:::

### La inserción de separadores y la de Go

Ki no pide `;`. Un salto de línea termina una sentencia cuando se cumplen tres condiciones: el delimitador abierto más interno (paréntesis, corchete o llave) es una llave o no hay ninguno; el token anterior puede cerrar una expresión (un nombre, un literal, `)`, `]`, `}`, `?`, `_`, `true`, `false`, `return`, `break`, `continue`); y la línea siguiente no empieza por `|>`, `.`, `&&`, `||` o `else`. Entonces el lexer inserta un token `Newline`, y el parser nunca mira líneas.

Go hace algo parecido con sus `;`; dice su especificación: «a semicolon is automatically inserted into the token stream immediately after a line's final token if that token is» un identificador, un literal, `break`, `continue`, `fallthrough`, `return`, `++`, `--`, `)`, `]` o `}`. En `go/scanner` es el campo `insertSemi`, que cada token deja a `true` o `false`. La regla de Go solo mira el final de la línea; la de Ki mira además los delimitadores abiertos y el principio de la línea siguiente. Lo que eso cambia, compilado con Go 1.27.3:

- Una línea que empieza por `.`: en Go, `syntax error: unexpected ., expected }`; en Ki continúa una cadena de métodos.
- `}` y `else` en dos líneas: en Go, `syntax error: unexpected keyword else, expected }`; en Ki es válido.
- `f(1, 2⏎)`: en Go, `syntax error: unexpected newline in argument list; possibly missing comma or )`; en Ki, dentro de `(` y `[`, el salto es un espacio.

Una regla propia de Ki: **un token `Error` nunca cierra una expresión**. Tras un error léxico no se sabe qué quería decir la línea; si el lexer pusiera un `Newline` detrás de un `$` suelto, el parser vería dos sentencias y daría un segundo error por una sola equivocación.

:::from TS
JavaScript inserta `;` casi con la regla contraria: sobre todo cuando el token siguiente no puede continuar la línea. `let x = a⏎-b` es una resta: con `a = 1` y `b = 2`, Node 24 imprime `-1`. En Ki son dos sentencias, `let x = a` y `-b`.
:::

### CRLF, el `\r` suelto y la recuperación

Ki cuenta el `\r\n` de Windows como un solo salto, y su `Newline` ocupa los dos bytes. Un `\r` que no precede a un `\n` es un espacio más: no termina la línea.

Recuperarse es seguir lexando tras un error sin inventar nada. Un carácter inesperado da un `Error` y el lexer sigue. Un literal no puede contener un salto de línea, así que uno sin cerrar acaba con su línea (sin el `\r` de un CRLF): una comilla olvidada estropea una línea, no el fichero. Cada error de un literal da un diagnóstico (un string con dos escapes malos da dos E0106), y el literal deja un token en su sitio (un `Int` con valor 0, un `Char` con U+FFFD, o un `Error` si es un string sin cerrar) para que el parser no añada otro error.

### Fuzzing nativo

Un test de fuzzing es una función `FuzzXxx(f *testing.F)`. `f.Add(…)` registra semillas, y `f.Fuzz(func(t *testing.T, src string) { … })` recibe el objetivo, que se ejecuta con cada entrada. Sin flags, `go test` lo ejecuta solo con las semillas, como un test normal. Con `go test -run='^$' -fuzz=FuzzLex -fuzztime=10s`, el motor genera entradas nuevas durante diez segundos guiándose por la cobertura. Las entradas que abren caminos nuevos van a la caché de Go (`$GOCACHE/fuzz`); una que hace fallar el objetivo se escribe en `testdata/fuzz/FuzzLex/` y pasa a ser una semilla más.

A un lexer, con cualquier entrada, no se le puede pedir el resultado exacto, sino **propiedades**: que no haga panic, que el último token sea `Eof`, que cada span cumpla `Lo <= Hi <= len` y que los `Lo` no decrezcan, que es lo que necesita todo lo que viene después. En la sesión 4 verás al motor encontrar enseguida una entrada de pocos bytes que hace panic a un lexer con un error de límites.

:::from Rust
En Rust el fuzzing vive fuera de la biblioteca: la herramienta recomendada es cargo-fuzz, que según su libro «is itself not a fuzzer, but a tool to invoke a fuzzer», y el único que admite es libFuzzer. En Go, el motor viene con `go test`.
:::

## Tests primero

{{cards}}

Úsalas como siempre: escribe el test de la tarjeta, míralo fallar, implementa lo justo para el verde, y solo entonces despliega la referencia con su `{{reftest}}`. Los tests del lexer van en `package syntax_test`, salvo T05.65, que mira por dentro la pila de delimitadores: va en `line_rule_internal_test.go`, con `package syntax`, porque `bracketStack` no se exporta. Un directorio admite los dos paquetes de test, y la función no es `TestLineRule` sino `TestBracketStack`, porque en este libro cada `TestXxx` vive en un solo fichero. Las tarjetas describen el final del capítulo: T05.07, T05.32, T05.36 y T05.41 cuentan con los `Newline` de la sesión 4, y cada sesión dice qué filas esperan.

## Implementación

### Sesión 1: tokens, `go generate` y el esqueleto del lexer (≈ 2 h)

#### Los tests, en rojo

T05.01–T05.13 describen esta interfaz:

```go
//go:generate go run ./gen -o token_kind.go     // in token.go
type TokenKind uint8                             // generated: KwFn … Error, and String()
var keywords map[string]TokenKind                // generated: "fn" → KwFn, …
type Token struct { Kind TokenKind; Span base.Span; Payload uint32; NewlineBefore bool }
type TokenBuffer struct { Tokens []Token; Ints []int64; Floats []float64; Strings []string }
func Lex(sm *base.SourceManager, id base.FileID, in *base.Interner, sink *diag.Sink) TokenBuffer
func DumpTokens(tb *TokenBuffer, sm *base.SourceManager, in *base.Interner) string
```

Sobre el código de `ch04-s2`, los tests no compilan:

```console
$ go test ./internal/syntax/
# github.com/aldezex/kilang/internal/syntax_test [github.com/aldezex/kilang/internal/syntax.test]
internal/syntax/lexer_test.go:13:47: undefined: syntax.TokenBuffer
…
internal/syntax/lexer_test.go:65:52: too many errors
FAIL	github.com/aldezex/kilang/internal/syntax [build failed]
```

Escribe la lista, el generador y `token.go`, ejecuta `go generate ./internal/syntax` y añade dos stubs, porque el paquete de test no compila sin los dos: `Lex`, que devuelve `TokenBuffer{Tokens: []Token{{Kind: Eof}}}`, y `DumpTokens`, que devuelve `""`. Entonces T05.01, T05.02 y `empty_file_is_eof` pasan, y los demás subtests de `TestLexer` fallan con su mensaje, sin ningún panic (`maximal munch: lexing "|>=>..->" gave [Eof], want [Pipe FatArrow DotDot Arrow Eof]`…). `unexpected_char_E0104` exige cuatro tokens con `t.Fatalf` antes de leer `tb.Tokens[1]`; con `t.Errorf`, el índice haría panic y cortaría el binario.

Los ayudantes están al principio de `lexer_test.go` en `ref/ch05-s1`: `lexAll` y `lexFile` lexan un texto y devuelven los tokens y el sink (`lexFile`, además, el id del fichero y el interner); `lexKinds` y `wantKinds` comparan la lista de tipos; `dumpTokens` devuelve el volcado. `wantOne`, de `source_check_test.go` (capítulo 3), exige un único diagnóstico con su código, mensaje, span y etiqueta.

T05.07 espera un `Newline` entre `a` y `c` que inserta la regla de línea de la sesión 4. Hoy escribe la fila sin él: `LowerIdent LowerIdent Eof`, con `NewlineBefore` en `false, true, false`.

#### El código

{{file ch05-s1 internal/syntax/token_kinds.txt}}

{{file ch05-s1 internal/syntax/gen/main.go}}

- `gen` usa el `FlagSet` global de `flag` (`flag.String`, `flag.Parse`), que ante un flag malo sale con 2: aquí sirve, porque nadie lo ejecuta en proceso. En el texto de ayuda de `-o`, ``this `file` instead of stdout``, la palabra entre comillas invertidas es el nombre del argumento: `-h` imprime `-o file`.
- `parse` corta la lista con `strings.Cut` (capítulo 3) y rechaza nombres repetidos, palabras clave sin texto o con el de otra (`keywords` tendría una clave repetida y no compilaría) y más de 256 clases, que no cabrían en un `uint8`.
- `generate` escribe en un `bytes.Buffer` con `fmt.Fprintf(&b, …)` y pasa el resultado por `format.Source`.

{{file ch05-s1 internal/syntax/token.go}}

`Token` no guarda texto: ocupa 24 bytes (`unsafe.Sizeof`), y lo que no cabe en él va en las tablas de `TokenBuffer`, a las que apunta `Payload`. Es la idea del capítulo 4: datos en tablas, nombrados por índices. El `token_kind.go` generado declara `var kindNames = [...]string{KwFn: "KwFn", …}`: un literal de array con claves (con `[...]`, el compilador cuenta el tamaño), en el que cada constante da la posición de su nombre, así que la tabla no se desplaza aunque cambie el orden de la lista.

{{file ch05-s1 internal/syntax/lexer.go}}

- **`Lex`** ejecuta primero `CheckSource` (capítulo 3): un fichero con UTF-8 inválido o un byte de control prohibido no se lexa, y su buffer es solo `Eof`. Después alterna `skipTrivia` y `next`, y siempre acaba en `Eof`: ningún pase posterior comprueba el final del buffer.
- **`at(i)`** devuelve el byte `i` posiciones más allá, o 0 pasado el final; un 0 real no llega, porque `CheckSource` lo rechaza.
- **`switch c := lx.src[lx.pos]; {`** es un `switch` con sentencia inicial: declara `c`, visible en todos los `case`, y como no lleva expresión, cada `case` es una condición.
- **Comentarios.** `//` se para antes del `\n`, que anota el `case` del salto. `/* */` cuenta niveles; quita el `depth++` y `nested_block_comment` falla (el primer `*/` cierra todo y ` c */` sale como tokens), y `unterminated_block_comment_E0103` también: `/* a /* b */: got 0 diagnostics, want exactly 1`.
- **`punctuation`** es el maximal munch. `two` es un closure que devuelve tres valores, y `return two('.', DotDot, Dot)` devuelve los tres resultados de la llamada. Un `&` solo no es un token: su `case` no devuelve nada y la función acaba en `return 0, 0, false`. Quita el `case '>'` del `=` y T05.04 lo ve: `maximal munch: lexing "|>=>..->" gave [Pipe Eq Gt DotDot Arrow Eof], want [Pipe FatArrow DotDot Arrow Eof]`.
- **`ident`** decide la clase por el primer carácter, y busca la palabra en `keywords` con `keywords[string(text)]`, una conversión que no reserva memoria porque el compilador ve que solo sirve para buscar (capítulo 4, ejercicio 3).
- **`unexpected`** usa `utf8.DecodeRune` para cubrir el carácter entero y `visible` para escribirlo en el mensaje.
- **`DumpTokens`** cita los textos con `strconv.Quote`, como `%q`, y escribe los `Float` con `strconv.FormatFloat(v, 'g', -1, 64)`, la forma más corta que vuelve a leerse igual. Pide a `sm.LineCol` la posición de cada token. Funciona, pero `LineCol` cuenta los code points desde el principio de la línea cada vez: con un fichero de una sola línea muy larga, el coste crece con el cuadrado. La sesión 4 lo arregla.

{{reftest T05.04}}

{{reftest T05.11}}

El mensaje de la etiqueta: `ch05-s1: tokens, go generate and the lexer skeleton`.

Si te atascas o tu versión se desvía demasiado, copia el fichero de `ref/ch05-s1` y sigue.

Al terminar: tus tests en verde, commit y etiqueta `ch05-s1`; compárate con `ref/ch05-s1`.

### Sesión 2: números (≈ 2 h)

#### Los tests, en rojo

T05.14–T05.26 no cambian la interfaz: solo `Lex` aprende a leer números. Sobre `ch05-s1` compilan, y los subtests de `TestNumbers` fallan con su mensaje, sin panic, porque hoy un dígito es un carácter inesperado: `9223372036854775807: got 19 diagnostics, want none`, un E0104 por cifra. Los ayudantes nuevos están al principio de `numbers_test.go` en `ref/ch05-s2`: `intValues` y `floatValues` sacan los valores de los literales, `wantNoDiags` exige cero diagnósticos, `wantKindList` compara los tipos de un buffer y `wantIntsAndName`, los enteros y, opcionalmente, el nombre de un token.

#### El código

{{diff ch05-s1..ch05-s2 internal/syntax/lexer.go}}

{{file ch05-s2 internal/syntax/numbers.go}}

- **`digitRun`** recibe la clase de dígito como un valor de tipo función, `isDigit func(byte) bool`, y sirve a las tres bases. Acumula los dígitos sin los `_` y dice si el tramo acabó en `_`. En `(_ []byte, trailing bool)`, el nombre explica el segundo resultado, y `_` deja el primero sin nombre.
- **El punto y la `e`.** Un `.` es parte del número solo si lo sigue un dígito, y una `e` solo si la siguen dígitos, con un signo opcional. Sin la primera condición, `1..2` daría `kinds [Float Dot Int Eof], want [Int DotDot Int Eof]` y `1.abs`, un `Float` seguido de un nombre.
- **`radixInt`** salta el prefijo y llama a `ParseInt` con la base. Si llamaras siempre con base 10, `0b1010` valdría 1010, y `0xFF` llegaría al `panic` de `emitInt`: `unreachable: strconv.ParseInt: parsing "FF": invalid syntax`. Ese `panic` marca un invariante: el lexer ya comprobó los dígitos, así que el único error posible es el de rango.
- **E0105 y E0109.** El token se queda con 0. Si quitas el `v = 0`, T05.17 recibe lo que devuelve `ParseInt`: `values [9223372036854775807], want [0]`.

Lo que el lexer no hace: los prefijos van en minúscula, así que `0X1` es `Int(0)` y el nombre `X1`; un dígito que la base no tiene termina el literal (`0b102` son `Int(2)` e `Int(2)`), y `12abc` es `Int(12)` y `LowerIdent(abc)`, sin diagnóstico: esos pares son del parser. Y `0x_FF` vale 255. T05.14 fija esos casos; T05.20, `0b2`, `0xg`, ``0x` `` y `0x@`, con E0107 sobre el prefijo (los dos últimos, más un E0104).

{{reftest T05.17}}

{{reftest T05.19}}

El mensaje de la etiqueta: `ch05-s2: number literals`.

Si te atascas o tu versión se desvía demasiado, copia el fichero de `ref/ch05-s2` y sigue.

Al terminar: tus tests en verde, commit y etiqueta `ch05-s2`; compárate con `ref/ch05-s2`.

### Sesión 3: caracteres, strings e interpolación (≈ 2 h)

#### Los tests, en rojo

T05.27–T05.41 tampoco cambian la interfaz. Sobre `ch05-s2`, los subtests de `TestStrings` fallan con su mensaje, sin panic: hoy una comilla es E0104 (`'a' '\n' '\u{1F600}': got 8 diagnostics, want none`). Los ayudantes están al principio de `strings_test.go` en `ref/ch05-s3`: `shape` escribe cada token como en el volcado y `wantShape` compara esa lista; `wantSpans`, `wantNewlines`, `wantOrdered` y `wantText` comprueban spans, `NewlineBefore`, que los `Lo` no decrezcan y el texto de un string; `wantBadEscape`, `wantUnterminated` y `wantBadChar` exigen un único E0106, E0110 o E0108.

Tres tarjetas esperan `Newline` de la sesión 4. Escribe hoy esas filas sin el `Newline` y con un `false` menos en la lista de `NewlineBefore`: en T05.32, las cuatro filas que acaban en `x` tras el salto; en T05.36, `a\n  "abc\nx` y `a\n"abc`; en T05.41, las seis filas cuyo último token antes del salto cierra una expresión, y `a\n"${\nx`. La sesión 4 te pedirá devolverlas a su forma.

#### El código

{{diff ch05-s2..ch05-s3 internal/syntax/lexer.go}}

{{file ch05-s3 internal/syntax/strings.go}}

- **Escapes.** `escape` resuelve `\n`, `\t`, `\r`, `\\`, `\'`, `\"`, `\0`, `\$` y `\u{…}`. Uno inválido da E0106, aporta U+FFFD y consume la barra y el carácter siguiente con todos sus bytes: con `lx.pos += 2`, `"\é"` partiría la `é`. `unicodeEscape` acumula las cifras con `v<<4 | rune(hexValue(c))`; seis cifras son 24 bits, no se desborda, y `utf8.ValidRune` decide.
- **Una barra al final de la línea** no es un escape: `escape` solo la salta, y quien llama ve el final de la línea y da E0110 o E0108. Un error, no dos.
- **`char`** junta el contenido y comprueba que es un solo code point con `DecodeRune`: `'aé'` tiene tres bytes y dos caracteres.
- **Los modos.** `str` apila un `modeString`; `stringText` lee hasta la comilla, un `${` o el final de la línea. Un `${` emite `StringHead` o `StringMid`, apila el centinela y un `modeCode`, y el bucle de `Lex` sigue con tokens normales. `st := &lx.interp` es un puntero al campo.
- **La `}`.** `next` pregunta primero a `endInterpolation`, que solo actúa si el centinela está en la cima. `trackBraces` apila y saca las llaves normales. Sin él, ninguna llave entra en la pila y la primera `}` de `"${ {} }"` cierra la interpolación: `tokens ["StringHead(\"\")" "LBrace" "StringTail(\" }\")" "Eof"]`.
- **`unterminated`** sustituye todo lo que produjo el string más externo por un único `Error`, **en el sitio de su primer token**: los `StringHead` y `StringMid` sin final se van, y los tokens de las expresiones se quedan detrás. `append(toks[:outer.first], Token{…})` reutiliza el array desde ahí, y por eso antes copia lo que conserva en `kept`. Con el `Error` al final, los spans retrocederían, y T05.41 lo ve: `token 1 (Error) has span [0,5) after a token that starts at 4`.

{{reftest T05.38}}

{{reftest T05.39}}

El mensaje de la etiqueta: `ch05-s3: chars, strings and interpolation`.

Si te atascas o tu versión se desvía demasiado, copia el fichero de `ref/ch05-s3` y sigue.

Al terminar: tus tests en verde, commit y etiqueta `ch05-s3`; compárate con `ref/ch05-s3`.

### Sesión 4: la regla de línea, `ki emit tokens`, `--update` y el primer fuzz (≈ 2 h)

#### Los tests, en rojo

T05.42–T05.67 añaden la regla de línea, `ki emit tokens`, `ki test --update` y `FuzzLex`. Antes, devuelve a T05.07, T05.32, T05.36 y T05.41 los `Newline` de sus tarjetas: en T05.07, cuatro tokens y `NewlineBefore` en `false, false, true, false`; las otras tres, como en este diff:

{{diff ch05-s3..ch05-s4 internal/syntax/strings_test.go}}

La interfaz nueva:

```go
// package driver
type Session struct {
	Stdin          io.Reader
	Stdout, Stderr io.Writer
	Sources        base.SourceManager
	Sink           diag.Sink
	Interner       *base.Interner // new
	Format         DiagFormat
}
// usage: ki emit tokens [--diagnostics=human|short|json] FILE
// usage: ki test [--root DIR] [--update] PATH...

// package kitest
type Runner struct {
	Root   string
	Exec   func(args []string, stdout, stderr io.Writer) int
	Out    io.Writer
	Update bool // new
}

// package syntax, unexported; T05.65 uses it
type bracketStack struct {
	kinds                    []TokenKind
	levels                   []int
	parens, brackets, braces int // how many of each are in kinds
	level                    int
}
func (s *bracketStack) push(open TokenKind)
func (s *bracketStack) close(want TokenKind) // want is the opener: LParen, LBracket or LBrace
func (s *bracketStack) enter()               // an interpolation starts
func (s *bracketStack) leave()               // it ends: close the brackets opened inside it
```

Sobre `ch05-s3` hay cuatro rojos. `internal/kitest` no compila (`unknown field Update in struct literal of type kitest.Runner`), y `internal/syntax` tampoco mientras exista `line_rule_internal_test.go` (`undefined: bracketStack`); sin ese fichero compila, y fallan `line_comment_skipped`, `dump_tokens`, los tres subtests de `TestStrings` que acabas de cambiar y los de `TestLineRule` que esperan algún `Newline` (todos menos `no_newline_after_operator`). En `internal/driver` fallan cinco subtests de `TestCLI`: los de `emit`, porque el comando no existe (`unknown target: got 2 "" "usage: ki <command> [args]\ncommands: --version, version, check, test\n"`), y los que esperan `emit` en la lista de comandos o `--update` en `ki test`. Y en `tests`, el golden `ch05/line_rule.ki` sale con 2 en lugar de 0. `FuzzLex` y `TestGen` (T05.67, en `gen/main_test.go` con `package main`: prueba el `parse` del generador) ya pasan; a `FuzzLex` lo verás fallar al final de la sesión.

Los ayudantes: en `line_rule_test.go` (`ref/ch05-s4`), una fila `lineRow` lleva el texto, la forma de sus tokens, los spans de sus `Newline` y cuántos diagnósticos da, y `checkLineRows` comprueba las tres cosas; en `update_test.go`, `runUpdate` ejecuta el runner con `Update` sobre un fichero temporal con una salida inventada y devuelve el fichero resultante y el informe, y `checkUpdateRows` recorre filas `updateRow` con lo mismo.

#### La regla de línea

{{file ch05-s4 internal/syntax/line_rule.go}}

- **Una pasada al final.** Al llegar al `Eof`, `insertSeparators` copia los tokens a `out` y pone un `Newline` delante de los que cumplen las tres condiciones. `endsExpression` y `continuesLine` son las dos listas, y `Error` no está en la primera: añádelo y T05.52 falla con `"a $\nb": tokens ["LowerIdent(a)" "Error" "Newline" "LowerIdent(b)" "Eof"]`. Quita `KwElse` de la segunda y `}\nelse {` recibe un `Newline`.
- **`bracketStack`** guarda los `(`, `[` y `{` abiertos y un contador por clase. `count` devuelve un puntero al contador, y `*s.count(open)++` lo incrementa a través de él. Un cierre sin pareja (contador a 0) no hace nada; uno con pareja saca todo lo que hay encima con `for s.pop() != want {}`, un bucle de cuerpo vacío: el trabajo lo hace la condición. Cada delimitador entra y sale una vez como mucho, así que el coste es lineal. T05.65 lo fija sin medir tiempos: si cada cierre buscara su pareja por toda la pila, en esta máquina `go test -timeout 60s` cortaría la prueba con `panic: test timed out after 1m0s`.
- **`enter` y `leave`.** Los `${` y `}` de una interpolación no son tokens, pero delimitan su expresión. El `StringHead` abre un nivel y el `StringTail` lo cierra, y `leave` cierra los delimitadores que la expresión abrió y dejó abiertos: sin él, `"${ f(a }"` dejaría el `(` abierto y apagaría los saltos de todo el resto del fichero. `leave` saca con `pop`, que descuenta; si recortara la pila sin más, T05.65 lo vería: `after leave: kinds [LBracket] and 1 levels, counts (1, 2, 1), want [LBracket] and 1 with (0, 1, 0)`. Un string que no termina no tiene `StringTail`: lo cierra el primer salto de línea tras su `Error`.
- **`newlineAfter`** da al `Newline` el span del primer terminador tras el token anterior: el `\n`, o el `\r\n` entero. Sin el `start--`, T05.49 da `a\r\nb: spans [[0 1] [2 3] [3 4] [4 4]], want [[0 1] [1 3] [3 4] [4 4]]`.

{{diff ch05-s3..ch05-s4 internal/syntax/lexer.go}}

`Lex` llama a `insertSeparators`, y `DumpTokens` pasa a ser lineal: si entre un token y el anterior no hay ningún `\n` (`bytes.IndexByte`), suma a la columna los code points que los separan (`utf8.RuneCount`); solo el primero de cada línea pregunta a `LineCol`. T05.64 lo fija con dos millones de strings `"ñ"` en una línea, una fila que solo corre si las cortas pasaron (`if t.Failed() { return }`), para que un `DumpTokens` erróneo y lento no esconda sus mensajes tras el tiempo máximo.

{{reftest T05.52}}

#### `ki emit tokens` y `ki check`

{{file ch05-s4 internal/driver/emit.go}}

{{diff ch05-s3..ch05-s4 internal/driver/commands.go}}

{{diff ch05-s3..ch05-s4 internal/driver/check.go}}

El destino va primero y sus flags después, así que `ki emit --diagnostics=short tokens` es un destino desconocido; en cambio, `-h`, `-help` y `--help` en su lugar piden ayuda, como `ki emit tokens -h`: `printUsage` escribe la línea de uso por stdout y sale con 0 (T05.61). `fileArgs` sale de `runCheck` para servir a los dos. `ki check` llama ahora a `Lex`, que ya ejecuta `CheckSource`: llamar a las dos daría cada E0101 dos veces (T05.60). `NewSession` crea el `Interner` con `base.NewInterner()` porque el valor cero no sirve: `var in base.Interner; in.Intern(…)` hace panic con un puntero nulo.

El golden de la tarjeta T05.63 ejecuta el binario de punta a punta:

{{file ch05-s4 tests/golden/ch05/line_rule.ki}}

#### `ki test --update`

{{file ch05-s4 internal/kitest/update.go}}

{{diff ch05-s3..ch05-s4 internal/kitest/runner.go}}

- `updateFile` solo reescribe `expect-stdout` y `expect-stderr`, nunca el `expect-exit` ni las anotaciones: si ajustara el código de salida, un panic nuevo se aprobaría con solo ejecutar `--update`. `var errText *string` es un puntero que puede ser `nil`: con anotaciones, stderr no se escribe.
- `rewriteExpectations` corta el fichero con `bytes.SplitAfter`, que deja cada `\n` pegado a su línea. No edita en el sitio: anota cada cambio como un `edit` (dónde, cuántas líneas quita, cuáles pone), los ordena con `slices.SortStableFunc` (estable, para que stdout siga delante de stderr si van al mismo sitio) y copia el fichero intercalándolos; lo demás pasa tal cual, con su BOM y sus CRLF.
- En `runHere`, `run` es un closure que vacía los dos buffers y ejecuta el fichero. Tras reescribir, ejecuta otra vez, porque las líneas añadidas mueven el código. Y solo reescribe si `exit >= 0`: una ejecución que no llegó a salir (-1) no tiene salida que guardar. Quita esa condición y T05.66 lo ve: con un programa que no existe, el `expect-stderr` del fichero pasa a ser `ki test: cannot run /nonexistent/ki: …`.

#### El primer fuzz

{{reftest T05.62}}

`a\r\nb`, `f(a\n)` y `{\na\n}` llevan a `go test` a la regla de línea, y las tres últimas semillas, strings sin cerrar con tokens en su interpolación, obligan a `unterminated` a moverlos. La quinta propiedad pide a `DumpTokens` una línea por token que empiece por la posición línea:columna de `LineCol` y el tipo; como un `LineCol` por token es cuadrático en una línea larga, solo se comprueba hasta 16 KiB. Ejecútalo una vez con `go test ./internal/syntax -run='^$' -fuzz=FuzzLex -fuzztime=10s`; termina en `PASS` y no escribe nada en `testdata/`.

Ahora rómpelo: en `lineEndAt`, cambia `i+1 < len(lx.src)` por `<=`. Con `-fuzz`, en esta máquina, en cinco ejecuciones con la caché vacía, el motor tardó entre 0,2 y 1,2 segundos en encontrar una entrada que hace panic: siempre una comilla sin cerrar y un `\r` como último byte, a veces reducida a esos dos bytes. La guardó en `testdata/fuzz/FuzzLex/`, con su hash por nombre, e imprimió el comando `go test -run=FuzzLex/<nombre>`. La tabla también lo caza, porque T05.28 lleva esa fila; el fuzzer llega sin que nadie le diga dónde mirar. Deshaz el cambio y borra el fichero.

El mensaje de la etiqueta: `ch05-s4: the line rule, ki emit tokens, --update and the first fuzz target`.

Si te atascas o tu versión se desvía demasiado, copia el fichero de `ref/ch05-s4` y sigue.

Al terminar: tus tests en verde, commit y etiqueta `ch05-s4`; compárate con `ref/ch05-s4`.

## Ejecútalo

Desde la raíz de tu repo, con la sesión 4 terminada y `ki` construido (`go build -o build/bin/ki ./cmd/ki`). El programa del objetivo, entero:

```console
$ build/bin/ki emit tokens fib.ki
1:1 KwFn
1:4 LowerIdent(main)
1:8 LParen
1:9 RParen
1:11 LBrace
2:3 KwLet
2:7 LowerIdent(n)
2:9 Eq
2:11 LowerIdent(fib)
2:14 LParen
2:15 Int(20)
2:17 RParen
3:5 Pipe
3:8 LowerIdent(show)
3:12 Newline
4:3 LowerIdent(print)
4:8 LParen
4:9 StringHead("fib = ")
4:18 LowerIdent(n)
4:19 StringTail("\n")
4:23 RParen
4:24 Newline
5:1 RBrace
6:1 Eof
```

Ningún `Newline` tras `LBrace`, que no cierra una expresión, ni delante del `Eof`. Con dos errores, el volcado sale entero por stdout, los diagnósticos por stderr y el código es 1:

```console
$ cat bad.ki
let x = 0x
let s = "a$b
$ build/bin/ki emit tokens bad.ki
1:1 KwLet
1:5 LowerIdent(x)
1:7 Eq
1:9 Int(0)
1:11 Newline
2:1 KwLet
2:5 LowerIdent(s)
2:7 Eq
2:9 Error
3:1 Eof
error[E0107]: malformed number literal
 --> bad.ki:1:9
  |
1 | let x = 0x
  |         ^^ missing digits after `0x`

error[E0110]: unterminated string literal
 --> bad.ki:2:9
  |
2 | let s = "a$b
  |         ^ string starts here
$ echo $?
1
```

Y `--update` sobre un golden desfasado, `up/a.ki`, con `// test: T00.01`, `// run: emit tokens`, `// expect-stdout: 1:1 Eof` y el código `let a = 1`:

```console
$ build/bin/ki test --update up
FAIL up/a.ki
- 4:1 KwLet
+ 9:1 KwLet
ki test: 0 passed, 1 failed
$ build/bin/ki test --update up
PASS up/a.ki
ki test: 1 passed, 0 failed
```

La primera pasada escribe el volcado en cinco líneas, que mueven el código de la línea 4 a la 9, y la ejecución sobre el fichero ya escrito falla; la segunda lo deja estable.

## Ejercicios

:::exercise
**1. `ki emit tokens --stats`.** Añade un flag que, en lugar del volcado, imprima cuántos tokens hay de cada tipo, una línea `<Kind> <count>` por tipo, ordenadas por nombre. Con su test.
:::

:::solution
`fileArgs` crea el `FlagSet`, así que recibe una función que añada flags propios (`check` pasa `nil`):

```go
func fileArgs(s *Session, c *command, args []string, more func(fs *flag.FlagSet)) (id base.FileID, code int, done bool) {
	fs := newFlagSet(c)
	if more != nil {
		more(fs)
	}
	// … as before
```

En `emit.go` (importa además `flag`, `slices` y `strings`):

```go
	var stats bool
	id, code, done := fileArgs(s, c, args, func(fs *flag.FlagSet) {
		fs.BoolVar(&stats, "stats", false, "print how many tokens of each kind there are")
	})
	// … after Lex:
	text := syntax.DumpTokens(&tb, &s.Sources, s.Interner)
	if stats {
		text = tokenStats(&tb)
	}
	code = printOut(s, c, text)

// tokenStats returns one line `<Kind> <count>` for every kind that tb holds,
// sorted by the name of the kind.
func tokenStats(tb *syntax.TokenBuffer) string {
	var counts [256]int // a TokenKind is a uint8, so it is always in range
	for _, tok := range tb.Tokens {
		counts[tok.Kind]++
	}
	var kinds []syntax.TokenKind
	for k, n := range counts {
		if n > 0 {
			kinds = append(kinds, syntax.TokenKind(k))
		}
	}
	slices.SortFunc(kinds, func(a, b syntax.TokenKind) int {
		return strings.Compare(a.String(), b.String())
	})
	var b strings.Builder
	for _, k := range kinds {
		fmt.Fprintf(&b, "%s %d\n", k, counts[k])
	}
	return b.String()
}
```

`fs.BoolVar`, como `fs.String` pero booleano, escribe en `stats`. Un `[256]int` indexado por la clase hace de mapa sin orden aleatorio. La línea de uso pasa a ser `ki emit tokens [--stats] [--diagnostics=human|short|json] FILE`, y hay que cambiarla también en la constante `usage` de `emit_unknown_target`. El test, en `TestCLI`:

```go
t.Run("emit_tokens_stats", func(t *testing.T) {
	path := filepath.Join(t.TempDir(), "x.ki")
	if err := os.WriteFile(path, []byte("a b\n(c)"), 0o644); err != nil {
		t.Fatalf("cannot write the file: %v", err)
	}
	// Sorted by name, byte by byte: "LParen" comes before "LowerIdent".
	const want = "Eof 1\nLParen 1\nLowerIdent 3\nNewline 1\nRParen 1\n"
	code, out, err := runCLI(t, "emit", "tokens", "--stats", path)
	if code != 0 || out != want || err != "" {
		t.Errorf("got %d %q %q, want 0 %q \"\"", code, out, err, want)
	}
})
```

Sin el `SortFunc` (y sin el import de `slices`, que se queda sin uso), las líneas salen en el orden de las constantes y el test falla: `LParen 1\nRParen 1\nLowerIdent 3\nNewline 1\nEof 1\n`.
:::

:::exercise
**2. `-` y `+` frente a `|>`.** Explica con ejemplos por qué una línea que empieza por `-` o `+` no continúa la anterior y una que empieza por `|>` sí.
:::

:::solution
`-` y `+` también son prefijos: `-b` es una expresión completa. Si continuaran la línea, `let x = a⏎-b` sería una resta, y el significado de un bloque dependería de dónde se parte una línea. En Ki son dos sentencias (T05.47); para partir una suma, el operador va al final: `a +⏎b` no recibe `Newline` (T05.44). `|>` no puede empezar una expresión, así que una línea que empieza por él solo puede continuar la anterior: `xs⏎  |> sum` es una sentencia (T05.45). Igual con `.`, `&&`, `||` y `else`; no con `|` solo, que empieza un brazo de patrón.
:::

:::exercise
**3. El volcado es estable.** Escribe un test de propiedad que lexe un fichero, vuelque sus tokens, lo lexe otra vez y compruebe que el volcado es idéntico.
:::

:::solution
Como fuzz target, en `dump_fuzz_test.go` (`package syntax_test`, con `testing`, `base`, `diag` y `syntax`):

```go
// dump lexes src as a file of a new SourceManager, with in, and returns the
// dump of its tokens and how many diagnostics it gave.
func dump(src string, in *base.Interner) (string, int) {
	var sm base.SourceManager
	id := sm.Add("fuzz.ki", []byte(src))
	var sink diag.Sink
	tb := syntax.Lex(&sm, id, in, &sink)
	return syntax.DumpTokens(&tb, &sm, in), len(sink.All())
}

// FuzzDumpIsStable lexes every input twice and checks that the two dumps and
// the two counts of diagnostics are the same. The second time the interner
// already holds other names, so the symbols are different numbers: the dump
// must depend on the text of the file and nothing else.
func FuzzDumpIsStable(f *testing.F) {
	f.Add("fn main() {}")
	f.Add(`let s = "a${x}b"` + "\n")
	f.Add("a\r\nb $ 0x")
	f.Fuzz(func(t *testing.T, src string) {
		first, n1 := dump(src, base.NewInterner())
		in := base.NewInterner()
		in.InternString("unrelated")
		in.InternString("names")
		second, n2 := dump(src, in)
		if first != second || n1 != n2 {
			t.Fatalf("%q: two lexes differ:\n%s(%d diagnostics)\nand\n%s(%d diagnostics)", src, first, n1, second, n2)
		}
	})
}
```

Con el lexer de la sesión 4 pasa, también diez segundos con `-fuzz`. Lo que vigila es que el resultado dependa solo del texto: un estado que sobreviva entre dos llamadas a `Lex`, o un volcado que imprima números de símbolo en lugar de nombres. Este último mutante, `(%d)` con `tok.Payload` en `DumpTokens`, falla en las tres semillas.
:::

:::exercise
**4. Un mega de Ki.** Mide `Lex` con un fichero de 1 MB (`b.Loop`, `-benchmem`) y explica las reservas que quedan con el perfil de memoria: `-memprofile` y `go tool pprof -sample_index=alloc_space`, como el de CPU del capítulo 4.
:::

:::solution
En `lexer_bench_test.go` (`package syntax_test`, con `strings`, `base`, `diag` y `syntax`):

```go
// benchUnit is a few lines of Ki with every kind of literal.
const benchUnit = `fn fib(n) {
  // the classic
  if n < 2 { n } else { fib(n - 1) + fib(n - 2) }
}
let msg = "fib(${n}) = ${fib(n)}\n"
let big = 0xFF_FF + 1_000.5e3
`

// benchSource is about 1 MB of Ki: benchUnit, over and over.
var benchSource = strings.Repeat(benchUnit, 1<<20/len(benchUnit))

func BenchmarkLex(b *testing.B) {
	var sm base.SourceManager
	id := sm.Add("bench.ki", []byte(benchSource))
	b.SetBytes(int64(len(benchSource)))
	for b.Loop() {
		syntax.Lex(&sm, id, base.NewInterner(), &diag.Sink{})
	}
}
```

`b.SetBytes` añade la columna MB/s. En esta máquina, unos 20 ms por operación, 65,8 MB y 28 655 reservas. Con `-memprofile mem.out` y `go tool pprof -sample_index=alloc_space -top`, dos tercios de los bytes son el `append` de `emit`, que hace crecer `Tokens` copiándolo varias veces, y algo más de una cuarta parte, `insertSeparators`, que reserva `out` con `len(toks)` y crece porque cada `Newline` es un token de más. Las reservas salen, según `alloc_objects` (con `-lines`), de tres líneas: la conversión `string(text)`, que copia cada trozo de string, y dos `append` que parten de un slice vacío, el `digitRun(nil, …)` de un hexadecimal y el del escape `\n`.
:::

:::exercise
**5. Octal.** Añade el prefijo `0o` (`0o17` vale 15), con sus tests, incluido el desbordamiento.
:::

:::solution
En `numbers.go`, un `case` más en `number` y una clase de dígito:

```go
		case 'o':
			lx.radixInt(start, 8, isOctDigit)
			return

// isOctDigit reports whether c is an octal digit.
func isOctDigit(c byte) bool { return c >= '0' && c <= '7' }
```

En `TestNumbers`:

```go
t.Run("int_octal", func(t *testing.T) {
	for _, row := range []struct {
		src  string
		want int64
	}{
		{"0o17", 15},
		{"0o7_7", 63},
		{"0o777777777777777777777", 9223372036854775807}, // the largest Int
	} {
		tb, sink := lexAll(t, row.src)
		if n := len(sink.All()); n != 0 || len(tb.Ints) != 1 || tb.Ints[0] != row.want {
			t.Errorf("%s: %d diagnostics, Ints %v, want none and [%d]", row.src, n, tb.Ints, row.want)
		}
	}
	// One past the largest Int, a prefix with no digits, and a digit that
	// octal does not have.
	for _, row := range []struct {
		src   string
		code  uint16
		label string
	}{
		{"0o1000000000000000000000", 105, "does not fit in a 64-bit `Int`"},
		{"0o", 107, "missing digits after `0o`"},
		{"0o8", 107, "missing digits after `0o`"},
	} {
		_, sink := lexAll(t, row.src)
		ds := sink.All()
		if len(ds) != 1 || ds[0].Code != diag.E(row.code) || ds[0].Primary.Message != row.label {
			t.Errorf("%s: got %+v, want one E0%d with the label %q", row.src, ds, row.code, row.label)
		}
	}
})
```

Veintiún sietes son 63 bits a uno, el mayor `Int`; un 1 y 21 ceros, 2⁶³. `code` es `uint16` porque `diag.E` lo pide. `0o8` da E0107 sobre `0o` y después `Int(8)`, como `0b2` en T05.20. Sin el `case`, la primera fila da `Ints [0]`; con `c <= '8'`, `0o8` llega al `panic` de `emitInt`. `0O17` sigue siendo `Int(0)` y `UpperIdent(O17)`.
:::

## Preguntas de repaso

:::review
¿Por qué un literal que desborda deja un token `Int` con valor 0, y no el valor que devuelve `ParseInt` ni ningún token?
:::

:::answer
Sin token, el parser daría un segundo error por la misma equivocación. Con el valor de `ParseInt`, el máximo, un programa rechazado llevaría un número que nadie escribió. El 0 marca el sitio; el diagnóstico ya dice qué pasó.
:::

:::review
¿Por qué `token_kind.go` se versiona en el repositorio y un test lo compara con la salida del generador?
:::

:::answer
Porque `go generate` no corre solo: sin versionar, el paquete no compilaría hasta generarlo. Y si alguien cambia la lista sin regenerar, una palabra clave nueva se lee como un nombre sin que nada avise; el test lo convierte en un fallo con su remedio.
:::

:::review
¿Por qué `ki test --update` no escribe la salida de una ejecución que dio -1?
:::

:::answer
Porque -1 significa que el programa no llegó a salir: no arrancó o murió por una señal. Lo que imprimió es el motivo del fallo o una salida a medias, y guardarlo haría de un fallo el golden esperado. El fichero se queda como estaba y falla por el código de salida.
:::

:::review
¿Por qué `f(a⏎b)` no recibe `Newline` y `f({⏎a⏎b⏎})` recibe dos?
:::

:::answer
Manda el delimitador abierto más interno: dentro de `(` un salto es un espacio, y dentro de `{` termina sentencias, como pide un bloque pasado a una función. Por eso la regla lleva una pila y no un contador.
:::

:::review
¿Por qué el `Error` de un string sin cerrar va en el sitio de su primer token y no al final de lo que el lexer ya había emitido?
:::

:::answer
Porque así los `Lo` no decrecen, que es lo que piden los diagnósticos ordenados por posición y `FuzzLex`. Al final, el `Error`, que empieza en la comilla, quedaría detrás de tokens que empiezan después.
:::

:::review
¿Qué gana un fuzz target frente a una tabla de casos, si los dos comprueban el mismo lexer?
:::

:::answer
La tabla prueba las entradas que alguien pensó, con su resultado exacto. El fuzz target prueba propiedades con entradas que genera él, guiado por la cobertura, sin que nadie le diga dónde mirar: con `<=` en `lineEndAt` encontró por sí mismo un `\r` como último byte del fichero, dentro de un string sin cerrar. Y lo que encuentra se queda en `testdata/fuzz/` como semilla, que `go test` ejecuta siempre.
:::

## Resumen

- El lexer compara bytes y decodifica con `unicode/utf8` solo donde importa el carácter entero; `ValidRune` decide un `\u{…}` y `RuneError` es el valor de un literal roto.
- Maximal munch: cada token es el más largo posible. Una tabla `[256]uint8` indexada con un `byte` clasifica bytes sin comprobación de límites.
- Los errores de `strconv.ParseInt` y `ParseFloat` son `*strconv.NumError`; `errors.Is(err, strconv.ErrRange)` detecta el desbordamiento, en el que `ParseInt` devuelve el extremo de su signo (el máximo, para los literales del lexer, sin signo) y `ParseFloat`, ±Inf.
- `go generate` ejecuta las directivas `//go:generate` solo cuando se pide; lo generado se versiona y un test lo compara con la salida del generador.
- Una pila de modos y un centinela en la pila de llaves parten un string con interpolaciones anidadas en `StringHead`, `StringMid` y `StringTail`.
- La regla de línea inserta un `Newline` cuando el delimitador más interno es una llave o ninguno, el token anterior cierra una expresión y la línea siguiente no empieza por `|>`, `.`, `&&`, `||` o `else`. Go solo mira el final de la línea.
- Un token `Error` nunca cierra una expresión; un literal sin cerrar acaba en su línea; cada error da un solo diagnóstico, y un literal roto deja un token en su sitio.
- Un fuzz target (`testing.F`, `f.Add`, `f.Fuzz`) corre sus semillas con `go test` y busca entradas nuevas con `-fuzz`; lo que falla se guarda en `testdata/fuzz/`.
- `ki test --update` reescribe solo las salidas esperadas, nunca el código de salida ni las anotaciones.

## Go en el trabajo

**Funciones de estado.** En «Lexical Scanning in Go» (2011), Rob Pike presentó el lexer de `text/template` como una máquina de estados en la que cada estado es una función que devuelve el siguiente, `type stateFn func(*lexer) stateFn`, y que corría en una goroutine y enviaba los tokens por un canal. En Go 1.27.3, `text/template/parse/lex.go` conserva `stateFn`, pero `nextItem` ejecuta los estados en un bucle, `state = state(l)`, sin goroutines ni canales. El lexer de Ki es un bucle con un `switch`: la interpolación anidada necesita una pila de modos de todas formas.

**Dos scanners en la biblioteca.** `go/scanner` es el del propio Go, con su `insertSemi`. `text/scanner` es genérico: su campo `Mode` elige qué reconoce con bits como `ScanIdents` o `ScanInts` (por omisión, `GoTokens`).

**`go generate` en repositorios reales.** En `$GOROOT/src` de Go 1.27.3 hay 111 ficheros con una directiva `//go:generate`. La regla de versionar lo generado está en «Generating code» (Rob Pike, blog de Go, 2014): «It is intended to be used by the author of the Go package, not its clients», y una vez generado y probado, el fichero «must be checked into the source code repository».
