---
chapter: 5
title: El lexer y la regla de línea
part: I
hours: 8
milestone: `ki emit tokens`
---

## Objetivo

Al terminar, `ki` tiene su primera etapa de compilador: el lexer convierte los bytes de un fichero en tokens, decide qué saltos de línea terminan una sentencia y sigue adelante tras cada error. Lo enseña un comando nuevo:

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

No hay `Newline` antes de `|>`, que continúa la línea, y el string se parte en cabeza, expresión y cola. El parser del capítulo 6 leerá estos tokens, camino del hito M1 (capítulo 10).

:::note
**Fe de erratas (2026-10-28).** Los mensajes de los errores de este capítulo mejoran en el capítulo 7 («El listón: errores al nivel de rustc»), que los explica.
:::

## Teoría

### Bytes, runas y el maximal munch

El lexer trabaja casi siempre con bytes: todo lo que tiene significado es ASCII, y ningún byte de un carácter multibyte cae por debajo de 0x80. Solo decodifica, con `utf8.DecodeRune(b)` (capítulo 2), donde importa el carácter entero:

- **Un carácter inesperado.** Byte a byte, `é` daría dos errores de medio carácter.
- **Un escape inválido.** `\é` consume la `é` entera.
- **Un literal de carácter.** `'é'` debe ser un solo code point, y `DecodeRune` lo comprueba.

Además:

- `utf8.ValidRune(r)` dice si el número de un `\u{…}` es un escalar Unicode.
- `utf8.RuneError` (U+FFFD) es el valor de un literal roto.
- En los mensajes, lo que `unicode.IsPrint(r)` no da por visible sale como `<U+XXXX>` (`visible`).

:::from Rust
`char` de Rust solo admite escalares (`char::from_u32(0xD800)` es `None`); un `rune` de Go es un `int32` cualquiera, y la comprobación es tuya.
:::

**Maximal munch.** Ante `|>=>`, el lexer toma siempre el token más largo posible: `|>`, después `=>`, y no `=` y `>`. Ninguna puntuación de Ki pasa de dos caracteres, así que basta mirar un byte más.

**Tablas de clases.** Muchos lexers clasifican cada byte con una tabla `[256]uint8`, un array cuyo tamaño es parte del tipo: indexado con un `byte`, nunca se sale. A Ki le basta un `switch`, porque sus clases son pocas.

:::more Una tabla de clases en Go, y qué es un escalar Unicode (`utf8`, `IsPrint`)
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

`classes` se calcula una vez, al iniciar el paquete, con una función literal que se llama en el acto. Que el índice nunca se salga lo sabe también el compilador: con `-gcflags=-d=ssa/check_bce/debug=1`, que informa de cada comprobación de límites que deja en el binario, `class` no aparece, y la misma función con un parámetro `int` sí (`Found IsInBounds`).

Un escalar Unicode es un code point que no es un sustituto. `utf8.ValidRune`: `0xD7FF` y `0xE000` sí; los sustitutos `0xD800` y `0xDFFF`, no; `0x10FFFF` sí y `0x110000` no. `utf8.AppendRune(b, r)` añade la codificación de un escalar a un `[]byte`.

`unicode.IsPrint` es falso para el tabulador, U+0085, U+2028, U+200B, U+202E y U+00A0 (que `unicode.IsGraphic` sí acepta). El lexer escribe esos caracteres con `fmt.Fprintf(&b, "<U+%04X>", r)`: `%04X`, el hexadecimal en mayúsculas con al menos cuatro cifras.

La sintaxis `\u{1F600}`, de 1 a 6 cifras, es la de Rust, donde el literal `"\u{D800}"` no compila (`invalid unicode character escape`).
:::

### Números con `strconv`

`strconv.ParseInt(s, base, 64)` y `strconv.ParseFloat(s, 64)` convierten texto en `int64` y `float64`. Sus errores son `*strconv.NumError`: `Err` guarda la causa (`strconv.ErrRange`, `strconv.ErrSyntax`), y `Unwrap` deja que `errors.Is` (capítulo 2) la encuentre.

Dos comportamientos deciden el diseño:

- **Un desbordamiento da el máximo (o `+Inf`) junto al error.** Guardarlo daría valor a un literal rechazado; por eso el token se queda con 0.
- **Los `_` de `1_000`.** `ParseInt` en base 10 los rechaza y `ParseFloat`, en texto decimal, solo los acepta entre dos dígitos; por eso el lexer los quita él.

:::from Python
`int("9223372036854775808")` funciona porque los enteros de Python no tienen tamaño. El `Int` de Ki tiene 64 bits.
:::

### `go generate`: el generador escrito a mano

Las 71 clases de token viven en `token_kinds.txt`, y un programa Go genera con esa lista `token_kind.go`: las constantes, los nombres de `String` y el mapa de palabras clave. En C++ serían X-macros.

- **La directiva.** `//go:generate go run ./gen -o token_kind.go`, en `token.go` y sin espacio tras `//`. `go generate ./internal/syntax` ejecuta el comando en el directorio del paquete; `go build` y `go test`, nunca.
- **La cabecera.** `// Code generated … DO NOT EDIT.` avisa a personas y herramientas de que no se edita a mano.
- **`go/format`.** `format.Source(src)` formatea como `gofmt`, así que el generador no se preocupa de alinear.
- **El test que lo vigila.** Como nada regenera solo, T05.01 ejecuta `go run ./gen` sin `-o` con `os/exec` (capítulo 1) y compara su stdout con `token_kind.go`.

:::from Rust
Lo más parecido es una `macro_rules!`, pero se expande en cada compilación, y `go generate` solo cuando lo pides: por eso lo generado se versiona.
:::

### Los modos del lexer y la interpolación

En `"a${x}b"`, la `x` es código y la `b` vuelve a ser texto: el lexer cambia de modo. Como un `${…}` puede contener otro string, los modos se apilan (`modeString`, `modeCode`), y la pila vacía es el código del fichero.

Lo difícil es la `}`: en `"${ g({ y }) }"`, la primera cierra una llave y la segunda, la interpolación. Por eso hay una pila de llaves: cada `{` apila una entrada normal y cada `${`, un **centinela** (`InterpSentinel`). Si la cima es el centinela, la `}` vuelve al texto del string; si no, cierra una llave.

Solo las llaves entran en esa pila, porque solo la `}` tiene que decidir: en `${ ( }`, la `}` cierra igual la interpolación.

:::from Python
Python 3.14 parte los f-strings con `FSTRING_START`, `FSTRING_MIDDLE` y `FSTRING_END`, y las llaves aparte; Ki las junta con el texto.
:::

### La regla de línea y la de Go

Ki no pide `;`: el lexer anota cada salto en el token siguiente (`NewlineBefore`), inserta un token `Newline` donde acaba una sentencia, y el parser nunca mira líneas. Un salto de línea acaba una sentencia si se cumplen tres condiciones:

1. el delimitador abierto más interno es una llave, o no hay ninguno (dentro de `(` y `[`, el salto es un espacio);
2. el token anterior puede cerrar una expresión: un nombre, un literal, `)`, `]`, `}`, `?`, `_`, `true`, `false`, `return`, `break`, `continue`;
3. la línea siguiente no empieza por `|>`, `.`, `&&`, `||` o `else`.

Go inserta `;` mirando solo el final de la línea. Ki mira también los delimitadores y la línea siguiente, y por eso acepta lo que Go rechaza (`⏎`: salto de línea): una línea que empieza por `.` (una cadena de métodos); `}` y `else` en dos líneas; `f(1, 2⏎)`.

:::from TS
JavaScript inserta `;` sobre todo cuando el token siguiente no puede continuar la línea: `let x = a⏎-b` es una resta. En Ki son dos sentencias.
:::

:::more La regla exacta de Go, sus errores en esos tres casos y el ejemplo de JavaScript
Dice su especificación: «a semicolon is automatically inserted into the token stream immediately after a line's final token if that token is» un identificador, un literal, `break`, `continue`, `fallthrough`, `return`, `++`, `--`, `)`, `]` o `}`. En `go/scanner`, cada token deja `insertSemi` a `true` o `false`. En Go 1.27, los tres casos dan:

- línea que empieza por `.`: `syntax error: unexpected ., expected }`;
- `}` y `else` en dos líneas: `syntax error: unexpected keyword else, expected }`;
- `f(1, 2⏎)`: `syntax error: unexpected newline in argument list; possibly missing comma or )`.

En JavaScript, `let x = a⏎-b`, con `a = 1` y `b = 2`, imprime `-1` en Node 24.
:::

### CRLF, el `\r` suelto y la recuperación

Ki cuenta el `\r\n` como un solo salto, y su `Newline` ocupa los dos bytes. Un `\r` suelto es un espacio.

Recuperarse es seguir lexando tras un error sin inventar nada:

- Un carácter inesperado da un `Error`, y el lexer sigue.
- Un literal no admite saltos de línea: uno sin cerrar acaba con su línea, y una comilla olvidada estropea una línea, no el fichero.
- Cada error de un literal da un diagnóstico, y el literal deja un token en su sitio (`Int` con 0, `Char` con U+FFFD, `Error` si es un string sin cerrar) para que el parser no añada otro error.

**Un token `Error` nunca cierra una expresión.** Tras un error léxico no se sabe qué quería decir la línea, y un `Newline` tras un `$` suelto daría al parser dos sentencias y un segundo error.

### Fuzzing nativo

A un lexer, con cualquier entrada, no se le puede pedir el resultado exacto, sino **propiedades**: que no haga panic, que el último token sea `Eof`, que cada span cumpla `Lo <= Hi <= len` y que los `Lo` no decrezcan.

Un test de fuzzing es una función `FuzzXxx(f *testing.F)`: `f.Add(…)` registra semillas y `f.Fuzz(func(t *testing.T, src string) { … })` recibe el objetivo (el *fuzz target*), que corre con cada entrada. `go test` lo ejecuta solo con las semillas; `go test -run='^$' -fuzz=FuzzLex -fuzztime=10s` genera entradas, guiado por la cobertura.

Las que abren caminos nuevos van a la caché (`$GOCACHE/fuzz`); una que hace fallar el objetivo va a `testdata/fuzz/FuzzLex/` y pasa a ser una semilla.

:::from Rust
En Rust el fuzzing vive fuera de la biblioteca (cargo-fuzz invoca a libFuzzer). En Go, viene con `go test`.
:::

## Tests primero

{{cards}}

Úsalas como siempre: escribe el test de la tarjeta, míralo fallar, implementa lo justo para el verde, y solo entonces despliega la referencia con su `{{reftest}}`. Describen el final del capítulo; cada sesión dice qué filas esperan.

:::more Los ayudantes de test, sesión por sesión
Están al principio de cada fichero de test, en `ref/ch05-sK`. Los tests del lexer van en `package syntax_test`, salvo T05.65, que mira por dentro `bracketStack`. Un directorio admite los dos paquetes de test, y la función de T05.65 no es `TestLineRule` sino `TestBracketStack`, porque en este libro cada `TestXxx` vive en un solo fichero.

- **Sesión 1, `lexer_test.go`.** `lexAll` y `lexFile` lexan un texto y devuelven los tokens y el sink (`lexFile`, además, el id del fichero y el interner); `lexKinds` y `wantKinds` comparan la lista de tipos; `dumpTokens` devuelve el volcado. `wantOne`, de `source_check_test.go` (capítulo 3), exige un único diagnóstico con su código, mensaje, span y etiqueta.
- **Sesión 2, `numbers_test.go`.** `intValues` y `floatValues` sacan los valores de los literales, `wantNoDiags` exige cero diagnósticos, `wantKindList` compara los tipos de un buffer y `wantIntsAndName`, los enteros y, opcionalmente, el nombre de un token.
- **Sesión 3, `strings_test.go`.** `shape` escribe cada token como en el volcado y `wantShape` compara esa lista; `wantSpans`, `wantNewlines`, `wantOrdered` y `wantText` comprueban spans, `NewlineBefore`, que los `Lo` no decrezcan y el texto de un string; `wantBadEscape`, `wantUnterminated` y `wantBadChar` exigen un único E0106, E0110 o E0108.
- **Sesión 4, `line_rule_test.go`.** Una fila `lineRow` lleva el texto, la forma de sus tokens, los spans de sus `Newline` y cuántos diagnósticos da, y `checkLineRows` comprueba las tres cosas.
- **Sesión 4, `update_test.go`.** `runUpdate` ejecuta el runner con `Update` sobre un fichero temporal con una salida inventada y devuelve el fichero resultante y el informe, y `checkUpdateRows` recorre filas `updateRow` con lo mismo.
:::

## Implementación

### Sesión 1: tokens, `go generate` y el esqueleto del lexer (≈ 2 h)

#### Los tests, en rojo

1. **Escribe T05.01–T05.13.** Describen esta interfaz:

```go
//go:generate go run ./gen -o token_kind.go     // in token.go
type TokenKind uint8                             // generated: KwFn … Error, and String()
var keywords map[string]TokenKind                // generated: "fn" → KwFn, …
type Token struct { Kind TokenKind; Span base.Span; Payload uint32; NewlineBefore bool }
type TokenBuffer struct { Tokens []Token; Ints []int64; Floats []float64; Strings []string }
func Lex(sm *base.SourceManager, id base.FileID, in *base.Interner, sink *diag.Sink) TokenBuffer
func DumpTokens(tb *TokenBuffer, sm *base.SourceManager, in *base.Interner) string
```

2. **Deja T05.07 sin su `Newline`,** que llega en la sesión 4: `LowerIdent LowerIdent Eof`, con `NewlineBefore` en `false, true, false`.
3. **Míralos fallar.** Sobre `ch04-s2`, no compilan:

```console
$ go test ./internal/syntax/
# github.com/aldezex/kilang/internal/syntax_test [github.com/aldezex/kilang/internal/syntax.test]
internal/syntax/lexer_test.go:13:47: undefined: syntax.TokenBuffer
…
internal/syntax/lexer_test.go:65:52: too many errors
FAIL	github.com/aldezex/kilang/internal/syntax [build failed]
```

4. **Escribe la lista, el generador y `token.go`,** y ejecuta `go generate ./internal/syntax`.
5. **Añade dos stubs,** porque el paquete de test no compila sin los dos: `Lex` devuelve `TokenBuffer{Tokens: []Token{{Kind: Eof}}}` y `DumpTokens`, `""`. T05.01, T05.02 y `empty_file_is_eof` pasan; los demás subtests de `TestLexer` fallan sin panic: `unexpected_char_E0104` usa `t.Fatalf` antes de leer `tb.Tokens[1]`.

#### El código

{{file ch05-s1 internal/syntax/token_kinds.txt}}

{{file ch05-s1 internal/syntax/gen/main.go}}

{{file ch05-s1 internal/syntax/token.go}}

`Token` no guarda texto y ocupa 24 bytes (`unsafe.Sizeof`): `Payload` apunta a las tablas de `TokenBuffer`, la idea del capítulo 4.

El `token_kind.go` generado declara sus nombres con `[...]string{KwFn: "KwFn", …}`, un literal de array con claves: el compilador cuenta el tamaño, y cambiar el orden de la lista no desplaza la tabla.

{{file ch05-s1 internal/syntax/lexer.go}}

- **`Lex`** ejecuta primero `CheckSource` (capítulo 3): un fichero rechazado da solo `Eof`, y todo buffer acaba en `Eof`, así que ningún pase posterior comprueba el final.
- **`switch c := lx.src[lx.pos]; {`** es un `switch` con sentencia inicial: `c` se ve en todos los `case`, y sin expresión, cada `case` es una condición.
- **`punctuation`** es el maximal munch. `return two('.', DotDot, Dot)` devuelve los tres resultados del closure `two`.
- **`DumpTokens`** pide a `sm.LineCol` cada posición, y `LineCol` recuenta la línea cada vez: en una línea larga, el coste es cuadrático hasta la sesión 4.

:::more Los flags de `gen`, comentarios anidados, `ident` y `bufio.Scanner`
**El generador.**

- `gen` usa el `FlagSet` global de `flag` (`flag.String`, `flag.Parse`), que ante un flag malo sale con 2: aquí sirve, porque nadie lo ejecuta en proceso. En el texto de ayuda de `-o`, ``this `file` instead of stdout``, la palabra entre comillas invertidas es el nombre del argumento: `-h` imprime `-o file`.
- `parse` corta la lista con `strings.Cut` (capítulo 3) y rechaza nombres repetidos, palabras clave sin texto o con el de otra (`keywords` tendría una clave repetida y no compilaría) y más de 256 clases, que no cabrían en un `uint8`.
- `generate` escribe en un `bytes.Buffer` con `fmt.Fprintf(&b, …)` y pasa el resultado por `format.Source`, que no comprueba tipos, solo la sintaxis: sobre `package p\nfunc {` devuelve `2:6: expected 'IDENT', found '{'`.
- Si añades `KwLoop` a la lista y no regeneras, T05.01 falla con ``token_kind.go differs from the output of `go run ./gen`: run `go generate ./internal/syntax` ``.
- La cabecera tiene que casar entera con `^// Code generated .* DO NOT EDIT\.$`, la convención de `go help generate`.

**El lexer y sus tests.**

- En `punctuation`, quita el `case '>'` del `=` y T05.04 falla.
- `at(i)` devuelve el byte `i` posiciones más allá, o 0 pasado el final; un 0 real no llega, porque `CheckSource` lo rechaza.
- `//` se para antes del `\n`, que anota el `case` del salto. `/* */` cuenta niveles: sin el `depth++`, el primer `*/` lo cierra todo, y `nested_block_comment` y `unterminated_block_comment_E0103` fallan.
- Un `&` solo no es un token: su `case` no devuelve nada y `punctuation` acaba en `return 0, 0, false`.
- `ident` decide la clase por el primer carácter y busca con `keywords[string(text)]`, una conversión que no reserva memoria porque el compilador ve que solo sirve para buscar (capítulo 4, ejercicio 3).
- `DumpTokens` cita los textos con `strconv.Quote`, como `%q`, y escribe los `Float` con `strconv.FormatFloat(v, 'g', -1, 64)`, la forma más corta que vuelve a leerse igual.
- Con `t.Errorf` en lugar de `t.Fatalf`, el índice de `unexpected_char_E0104` haría panic y cortaría el binario.

**Lo justo de `bufio.Scanner`.** El generador usa `os.ReadFile` y `strings.Split`; lo habitual para leer por líneas es `sc := bufio.NewScanner(r)`, `for sc.Scan() { line := sc.Text() }` y `sc.Err()` al final. El `Scanner` quita el `\r` de un CRLF, y una línea de 64 KiB o más (`bufio.MaxScanTokenSize`) detiene el bucle con `bufio.Scanner: token too long`.
:::

{{reftest T05.04}}

{{reftest T05.11}}

Si te atascas o tu versión se desvía demasiado, copia el fichero de `ref/ch05-s1` y sigue.

Al terminar: tus tests en verde, commit y etiqueta `ch05-s1` (mensaje: `ch05-s1: tokens, go generate and the lexer skeleton`); compárate con `ref/ch05-s1`.

### Sesión 2: números (≈ 2 h)

#### Los tests, en rojo

1. **Escribe T05.14–T05.26.**
2. **Míralos fallar.** Compilan, y los subtests de `TestNumbers` fallan sin panic: hoy cada dígito es un E0104 (`got 19 diagnostics, want none`).

#### El código

{{diff ch05-s1..ch05-s2 internal/syntax/lexer.go}}

{{file ch05-s2 internal/syntax/numbers.go}}

- **`digitRun`** recibe la clase de dígito como un valor de tipo función, `isDigit func(byte) bool`, y sirve a las tres bases. En sus resultados, `_` deja el primero sin nombre, y `trailing` nombra el segundo.
- **El punto y la `e`.** Un `.` es parte del número solo si lo sigue un dígito (si no, `1..2` empezaría por un `Float`), y una `e`, solo si la siguen dígitos, con signo opcional.
- **`emitInt`** hace `panic` para marcar un invariante: el lexer ya comprobó los dígitos, así que el único error posible es el de rango.
- **E0105 y E0109 (desbordamiento).** Sin el `v = 0`, T05.17 recibe el máximo de `ParseInt`.

:::more Números: casos raros (`0X1`, `0b102`, `12abc`) y `ParseInt` y `ParseFloat` en los límites
Los prefijos van en minúscula, así que `0X1` es `Int(0)` y el nombre `X1`. Un dígito que la base no tiene termina el literal (`0b102` son `Int(2)` e `Int(2)`), y `12abc` es `Int(12)` y `LowerIdent(abc)`, sin diagnóstico: esos pares son del parser. Y `0x_FF` vale 255. T05.14 fija esos casos; T05.20, `0b2`, `0xg`, ``0x` `` y `0x@`, con E0107 sobre el prefijo (los dos últimos, más un E0104).

Sin la condición del punto, `1..2` da `kinds [Float Dot Int Eof], want [Int DotDot Int Eof]` y `1.abs`, un `Float` seguido de un nombre. Si `radixInt` llamara siempre con base 10, `0b1010` valdría 1010, y `0xFF` llegaría al `panic` de `emitInt`: `unreachable: strconv.ParseInt: parsing "FF": invalid syntax`.

**`ParseInt` y `ParseFloat` en los límites.** Además de `Err`, `NumError` lleva `Func` (`"ParseInt"`) y `Num` (el texto). Una base o un tamaño inválidos dan otras causas. Con signo, `ParseInt` da el extremo de su signo y `ParseFloat`, ±Inf; los literales del lexer no llevan signo. En Go 1.27:

| Llamada | Resultado |
|---|---|
| `ParseInt("9223372036854775808", 10, 64)` | `9223372036854775807` y `strconv.ParseInt: parsing "9223372036854775808": value out of range` |
| `ParseInt("", 16, 64)` | `0` y `invalid syntax` |
| `ParseInt("1_000", 10, 64)` | `0` y `invalid syntax` (con base 0, `1000`) |
| `ParseFloat("1e999", 64)` | `+Inf` y `value out of range` |
| `ParseFloat("1e-999", 64)` | `0` y `nil` |
| `ParseFloat("1_000.5", 64)` | `1000.5` y `nil`; con `1__0.5`, `invalid syntax` |
:::

{{reftest T05.17}}

{{reftest T05.19}}

Si te atascas o tu versión se desvía demasiado, copia el fichero de `ref/ch05-s2` y sigue.

Al terminar: tus tests en verde, commit y etiqueta `ch05-s2` (mensaje: `ch05-s2: number literals`); compárate con `ref/ch05-s2`.

### Sesión 3: caracteres, strings e interpolación (≈ 2 h)

#### Los tests, en rojo

1. **Escribe T05.27–T05.41.**
2. **Deja tres tarjetas sin los `Newline` de la sesión 4.** Escribe estas filas sin ellos y con un `false` menos en `NewlineBefore`:
   - en T05.32, las cuatro que acaban en `x` tras el salto;
   - en T05.36, `a\n  "abc\nx` y `a\n"abc`;
   - en T05.41, las seis cuyo último token antes del salto cierra una expresión, y `a\n"${\nx`.
3. **Míralos fallar.** Los subtests de `TestStrings` fallan sin panic: hoy una comilla es E0104.

#### El código

{{diff ch05-s2..ch05-s3 internal/syntax/lexer.go}}

{{file ch05-s3 internal/syntax/strings.go}}

- **Escapes.** Uno inválido da E0106, aporta U+FFFD y consume la barra y el carácter siguiente con todos sus bytes: con `lx.pos += 2`, `"\é"` partiría la `é`. En `\u{…}`, de 1 a 6 cifras, `v<<4 | rune(hexValue(c))` no se desborda: seis cifras son 24 bits.
- **Una barra al final de la línea** no es un escape: `escape` la salta y quien llama da E0110 o E0108.
- **`${`** emite `StringHead` o `StringMid`, apila el centinela y un `modeCode`, y `Lex` sigue con tokens normales.
- **La `}`.** `endInterpolation` solo actúa con el centinela en la cima. `trackBraces` apila las llaves normales: sin él, la primera `}` de `"${ {} }"` cerraría la interpolación.
- **`unterminated`** sustituye los `StringHead` y `StringMid` sin final por un único `Error`, **en el sitio del primer token** del string más externo, con los tokens de las expresiones detrás. Con el `Error` al final, los spans retrocederían. Como `append(toks[:outer.first], Token{…})` reutiliza el array, antes copia lo que conserva en `kept`.

{{reftest T05.38}}

{{reftest T05.39}}

Si te atascas o tu versión se desvía demasiado, copia el fichero de `ref/ch05-s3` y sigue.

Al terminar: tus tests en verde, commit y etiqueta `ch05-s3` (mensaje: `ch05-s3: chars, strings and interpolation`); compárate con `ref/ch05-s3`.

### Sesión 4: la regla de línea, `ki emit tokens`, `--update` y el primer fuzz (≈ 2 h)

#### Los tests, en rojo

1. **Devuelve a T05.07, T05.32, T05.36 y T05.41 sus `Newline`.** En T05.07, cuatro tokens y `NewlineBefore` en `false, false, true, false`; las otras tres, como en este diff:

{{diff ch05-s3..ch05-s4 internal/syntax/strings_test.go}}

2. **Escribe T05.42–T05.67.** T05.65 (`TestBracketStack`) va en `line_rule_internal_test.go`, con `package syntax`, porque `bracketStack` no se exporta. La interfaz nueva:

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

3. **Míralos fallar.** Hay cuatro rojos: `internal/kitest` e `internal/syntax` no compilan, fallan cinco subtests de `TestCLI` y el golden `ch05/line_rule.ki` sale con 2 en lugar de 0.

:::more Qué falla sobre `ch05-s3`, paquete por paquete
- **`internal/kitest`** no compila: `unknown field Update in struct literal of type kitest.Runner`.
- **`internal/syntax`** tampoco mientras exista `line_rule_internal_test.go` (`undefined: bracketStack`). Sin ese fichero compila, y fallan `line_comment_skipped`, `dump_tokens`, los tres subtests de `TestStrings` que acabas de cambiar y los de `TestLineRule` que esperan algún `Newline` (todos menos `no_newline_after_operator`).
- **`internal/driver`.** Fallan cinco subtests de `TestCLI`: los de `emit`, porque el comando no existe (`unknown target: got 2 "" "usage: ki <command> [args]\ncommands: --version, version, check, test\n"`), y los que esperan `emit` en la lista de comandos o `--update` en `ki test`.
- **`tests`.** El golden `ch05/line_rule.ki` sale con 2 en lugar de 0.
- **Ya pasan** `FuzzLex`, que verás fallar al final de la sesión, y `TestGen` (T05.67, en `gen/main_test.go` con `package main`: prueba el `parse` del generador).
:::

#### La regla de línea

{{file ch05-s4 internal/syntax/line_rule.go}}

- **Una pasada al final.** Al llegar al `Eof`, `insertSeparators` pone un `Newline` donde se cumplen las tres condiciones.
- **`bracketStack`** guarda los delimitadores abiertos y un contador por clase. Un cierre sin pareja (contador a 0) no hace nada; uno con pareja saca lo de encima con `for s.pop() != want {}`, un bucle de cuerpo vacío: el trabajo lo hace la condición.
- **Coste lineal.** Cada delimitador entra y sale una vez como mucho.
- **`enter` y `leave`.** `StringHead` abre un nivel y `StringTail` lo cierra. Sin `leave`, `"${ f(a }"` dejaría el `(` abierto y apagaría los saltos de todo el resto del fichero.

{{diff ch05-s3..ch05-s4 internal/syntax/lexer.go}}

`DumpTokens` pasa a ser lineal: si entre dos tokens no hay `\n` (`bytes.IndexByte`), suma a la columna los code points que los separan (`utf8.RuneCount`).

{{reftest T05.52}}

:::more `*s.count(open)++`, `newlineAfter` y el `DumpTokens` lineal
**Regla de línea.** `endsExpression` y `continuesLine` son las dos listas de la regla. `Error` no está en `endsExpression`: añádelo y T05.52 falla. `count` devuelve un puntero al contador, y `*s.count(open)++` lo incrementa a través de él. Si cada cierre buscara su pareja por toda la pila, `go test -timeout 60s` cortaría T05.65, que no mide tiempos. `leave` saca con `pop`, que descuenta: si recortara la pila sin más, T05.65 vería mal los contadores. Un string que no termina no tiene `StringTail`: lo cierra el primer salto de línea tras su `Error`. `newlineAfter` da al `Newline` el span del primer terminador tras el token anterior: el `\n`, o el `\r\n` entero.

**`DumpTokens`.** Solo el primer token de cada línea pregunta a `LineCol`. T05.64 usa dos millones de strings `"ñ"` en una línea, y esa fila solo corre si las cortas pasaron (`if t.Failed() { return }`), para que un `DumpTokens` erróneo y lento no esconda sus mensajes tras el tiempo máximo.
:::

#### `ki emit tokens` y `ki check`

{{file ch05-s4 internal/driver/emit.go}}

{{diff ch05-s3..ch05-s4 internal/driver/commands.go}}

{{diff ch05-s3..ch05-s4 internal/driver/check.go}}

`ki check` llama ahora a `Lex`, que ya ejecuta `CheckSource`: llamar a las dos daría cada E0101 dos veces.

{{file ch05-s4 tests/golden/ch05/line_rule.ki}}

#### `ki test --update`

`--update` reescribe las salidas esperadas de un golden con lo impreso.

{{file ch05-s4 internal/kitest/update.go}}

{{diff ch05-s3..ch05-s4 internal/kitest/runner.go}}

- `updateFile` solo reescribe `expect-stdout` y `expect-stderr`: si ajustara el `expect-exit`, un panic nuevo se aprobaría con solo ejecutar `--update`.
- `runHere` solo reescribe si `exit >= 0`, porque una ejecución que no llegó a salir (-1) no tiene salida que guardar. Tras reescribir, ejecuta otra vez: las líneas añadidas mueven el código.

#### El primer fuzz

{{reftest T05.62}}

1. **Ejecútalo una vez:** `go test ./internal/syntax -run='^$' -fuzz=FuzzLex -fuzztime=10s` termina en `PASS` y no escribe nada en `testdata/`.
2. **Rómpelo.** En `lineEndAt`, cambia `i+1 < len(lx.src)` por `<=`.
3. **Ejecútalo otra vez.** En segundos, el motor encuentra una entrada que hace panic (una comilla sin cerrar y un `\r` como último byte), la guarda en `testdata/fuzz/FuzzLex/` e imprime `go test -run=FuzzLex/<nombre>`.
4. **Deshaz el cambio y borra el fichero.**

:::more Destinos de `ki emit`, cómo edita `--update` y las semillas de `FuzzLex`
**`ki emit`.** El destino va primero y sus flags después, así que `ki emit --diagnostics=short tokens` es un destino desconocido; en cambio, `-h`, `-help` y `--help` en su lugar piden ayuda, como `ki emit tokens -h`: `printUsage` escribe la línea de uso por stdout y sale con 0 (T05.61). `fileArgs` sale de `runCheck` para servir a los dos. `NewSession` crea el `Interner` con `base.NewInterner()` porque el valor cero no sirve: `var in base.Interner; in.Intern(…)` hace panic con un puntero nulo.

**`--update`.** `rewriteExpectations` corta el fichero con `bytes.SplitAfter`, que deja cada `\n` pegado a su línea. No edita en el sitio: anota cada cambio como un `edit` (dónde, cuántas líneas quita, cuáles pone), los ordena con `slices.SortStableFunc` (estable, para que stdout siga delante de stderr si van al mismo sitio) y copia el fichero intercalándolos; lo demás pasa tal cual, con su BOM y sus CRLF.

`var errText *string` es un puntero que puede ser `nil`: con anotaciones, stderr no se escribe. En `runHere`, `run` es un closure que vacía los dos buffers y ejecuta el fichero. Quita la condición `exit >= 0` y T05.66 lo ve: con un programa que no existe, el `expect-stderr` del fichero pasa a ser `ki test: cannot run /nonexistent/ki: …`.

**`FuzzLex`.** El `<=` de `lineEndAt` también lo caza T05.28, pero el fuzzer llega sin que nadie le diga dónde mirar. `a\r\nb`, `f(a\n)` y `{\na\n}` llevan a `go test` a la regla de línea, y las tres últimas semillas, strings sin cerrar con tokens en su interpolación, obligan a `unterminated` a moverlos. La quinta propiedad pide a `DumpTokens` una línea por token que empiece por la posición línea:columna de `LineCol` y el tipo; como un `LineCol` por token es cuadrático en una línea larga, solo se comprueba hasta 16 KiB.
:::

Si te atascas o tu versión se desvía demasiado, copia el fichero de `ref/ch05-s4` y sigue.

Al terminar: tus tests en verde, commit y etiqueta `ch05-s4` (mensaje: `ch05-s4: the line rule, ki emit tokens, --update and the first fuzz target`); compárate con `ref/ch05-s4`.

## Ejecútalo

Desde la raíz de tu repo, con la sesión 4 terminada y `ki` construido (`go build -o build/bin/ki ./cmd/ki`), el programa del objetivo:

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

No hay `Newline` tras `LBrace`, que no cierra una expresión, ni delante del `Eof`. Con dos errores, el volcado sale por stdout, los diagnósticos por stderr y el código es 1:

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

La primera pasada escribe cinco líneas de volcado, que mueven el código de la línea 4 a la 9, y por eso falla; la segunda lo deja estable.

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

- El lexer trabaja con bytes y solo decodifica donde importa el carácter entero. Maximal munch: cada token es el más largo posible.
- `errors.Is(err, strconv.ErrRange)` detecta un desbordamiento; el valor que lo acompaña no se guarda.
- `go generate` solo corre cuando se pide; lo generado se versiona y un test lo vigila.
- Una pila de modos y un centinela parten los strings con interpolación.
- La regla de línea mira el delimitador más interno, el token anterior y la línea siguiente; Go, solo el final de la línea.
- Un token `Error` nunca cierra una expresión, y un literal roto deja un token en su sitio.
- Un fuzz target corre sus semillas con `go test` y busca entradas con `-fuzz`; lo que falla va a `testdata/fuzz/`.
- `ki test --update` reescribe solo las salidas esperadas.

## Go en el trabajo

**Funciones de estado.** En «Lexical Scanning in Go» (2011), Rob Pike presentó el lexer de `text/template`: cada estado es una función que devuelve el siguiente, `type stateFn func(*lexer) stateFn`, con los tokens por un canal desde una goroutine.

Hoy, su `nextItem` ejecuta los estados en un bucle, sin goroutines ni canales. El lexer de Ki es un bucle con un `switch`: la interpolación anidada necesita una pila de modos de todas formas.

**Dos scanners en la biblioteca.** `go/scanner` es el del propio Go; `text/scanner` es genérico, y su campo `Mode` elige qué reconoce con bits como `ScanIdents` o `ScanInts` (por omisión, `GoTokens`).

**`go generate` en repositorios reales.** `$GOROOT/src` de Go 1.27 tiene 111 ficheros con una directiva. Según «Generating code» (Rob Pike, blog de Go, 2014), `go generate` es para el autor del paquete, no para sus clientes, y lo generado «must be checked into the source code repository».
