---
chapter: 5
title: El lexer y la regla de línea
part: I
hours: 8
milestone: `ki emit tokens`
---

## Objetivo

Al terminar, `ki` tiene su primera etapa de compilador: un lexer que convierte bytes en tokens, decide qué saltos de línea terminan una sentencia y sigue adelante tras cada error. Lo enseña un comando nuevo:

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
**Fe de erratas (2026-10-28).** Los mensajes de error de este capítulo mejoran en el capítulo 7.
:::

## Teoría

### Bytes, runas y el maximal munch

El lexer trabaja casi siempre con bytes: lo que tiene significado es ASCII, y ningún byte de un carácter multibyte baja de 0x80.

Solo decodifica, con `utf8.DecodeRune(b)` (capítulo 2), donde importa el carácter entero:

- un carácter inesperado: byte a byte, `é` daría dos errores;
- un escape inválido: `\é` consume la `é` entera;
- un literal de carácter: `'é'` debe ser un code point.

Un `\u{…}` solo admite escalares Unicode, code points que no son sustitutos: lo comprueba `utf8.ValidRune(r)`.

:::from Rust
`char` de Rust solo admite escalares; un `rune` de Go es un `int32` cualquiera, y la comprobación es tuya.
:::

**Maximal munch.** Ante `|>=>`, el lexer toma siempre el token más largo: `|>`, después `=>`. Ninguna puntuación de Ki pasa de dos caracteres: basta mirar un byte más.

:::more Clasificar bytes con una tabla
Muchos lexers clasifican cada byte con una tabla `[256]uint8`. El tamaño es parte del tipo: indexada con un `byte`, nunca se sale. A Ki, con pocas clases, le basta un `switch`.

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

`classes` se calcula una vez, al iniciar el paquete, con una función literal que se llama en el acto. El compilador también sabe que el índice no se sale: en `class` no deja comprobación de límites, y con un parámetro `int` sí.
:::

### Números con `strconv`

`strconv.ParseInt(s, base, 64)` y `strconv.ParseFloat(s, 64)` convierten texto en `int64` y `float64`. Sus errores son `*strconv.NumError`: `Err` guarda la causa (`strconv.ErrRange`, `strconv.ErrSyntax`), y `errors.Is` (capítulo 2) la encuentra.

Dos comportamientos de `strconv` deciden el diseño:

- **Un desbordamiento da el máximo (o `+Inf`) junto al error.** Guardarlo daría valor a un literal rechazado; por eso el token se queda con 0.
- **Los `_` de `1_000`.** `ParseInt` en base 10 los rechaza, así que el lexer los quita él.

:::more `ParseInt` y `ParseFloat` en los límites
Lo que devuelve cada llamada. Un underflow no es un error, y por eso `1e-999` vale 0 sin diagnóstico.

| Llamada | Resultado |
|---|---|
| `ParseInt("9223372036854775808", 10, 64)` | `9223372036854775807` y `value out of range` |
| `ParseInt("1_000", 10, 64)` | `0` y `invalid syntax` (con base 0, `1000`) |
| `ParseFloat("1e999", 64)` | `+Inf` y `value out of range` |
| `ParseFloat("1e-999", 64)` | `0` y `nil` |
| `ParseFloat("1_000.5", 64)` | `1000.5` y `nil`; con `1__0.5`, `invalid syntax` |
:::

### `go generate`: el generador escrito a mano

Los tipos de token viven en una sola lista, `token_kinds.txt`, y un programa Go genera con ella `token_kind.go`: las constantes, los nombres de `String` y el mapa de palabras clave.

- **La directiva.** `//go:generate go run ./gen -o token_kind.go`, sin espacio tras `//`. `go generate ./internal/syntax` la ejecuta en el directorio del paquete; `go build` y `go test`, nunca, y por eso lo generado se versiona.
- **La cabecera.** `// Code generated … DO NOT EDIT.` avisa a personas y herramientas de que no se edita a mano.
- **`go/format`.** `format.Source(src)` formatea como `gofmt`: el generador no alinea nada.
- **El test.** Como nada regenera solo, un test ejecuta `go run ./gen` con `os/exec` (capítulo 1) y compara su stdout con `token_kind.go`.

:::more Leer por líneas con `bufio.Scanner`
`gen` lee la lista entera; para leer por líneas, lo habitual es `sc := bufio.NewScanner(r)`, `for sc.Scan() { line := sc.Text() }` y `sc.Err()` al final. El `Scanner` quita el `\r` de un CRLF, y una línea de 64 KiB o más (`bufio.MaxScanTokenSize`) detiene el bucle con un error.
:::

### Los modos del lexer y la interpolación

En `"a${x}b"`, la `x` es código y la `b`, texto: el lexer cambia de modo. Un `${…}` puede contener otro string, así que los modos se apilan (`modeString`, `modeCode`). Con dos interpolaciones, `"a${x}b${y}c"` da `StringHead`, `x`, `StringMid`, `y`, `StringTail`.

Lo difícil es la `}`: en `"${ g({ y }) }"`, la primera cierra una llave y la segunda, la interpolación. Por eso hay una pila de llaves: cada `{` apila una entrada normal y cada `${`, un **centinela** (`InterpSentinel`). Con el centinela en la cima, la `}` vuelve al texto.

Solo las llaves entran, porque solo la `}` tiene que decidir: en `${ ( }`, cierra igual la interpolación.

### La regla de línea y la de Go

Ki no pide `;`. El lexer anota cada salto en el token siguiente (`NewlineBefore`) e inserta un `Newline` donde acaba una sentencia. El parser nunca mira líneas.

El `\r\n` es un solo salto, y su `Newline` ocupa los dos bytes. Un `\r` suelto es un espacio.

Un salto acaba una sentencia si:

1. el delimitador abierto más interno es una llave, o no hay ninguno (dentro de `(` y `[`, es un espacio);
2. el token anterior puede cerrar una expresión: un nombre, un literal, `)`, `]`, `}`, `?`, `_`, `true`, `false`, `return`, `break`, `continue`;
3. y la línea siguiente no empieza por `|>`, `.`, `&&`, `||` o `else`.

Nunca hay `Newline` ante el `Eof`.

Go inserta `;` mirando solo el final de la línea; Ki mira también los delimitadores y la línea siguiente, y acepta lo que Go rechaza (`⏎`: salto de línea):

- una línea que empieza por `.`;
- `}` y `else` en dos líneas;
- `f(1, 2⏎)`.

:::from TS
JavaScript inserta `;` sobre todo si el token siguiente no puede continuar la línea: `let x = a⏎-b` es una resta. En Ki, dos sentencias.
:::

### La recuperación

Recuperarse es seguir lexando tras un error sin inventar nada:

- Un carácter inesperado da un `Error` (E0104), y el lexer sigue.
- Un literal no admite saltos de línea: una comilla olvidada estropea una línea, no el fichero.
- Un literal roto deja un token en su sitio (`Int` con 0, `Char` con `utf8.RuneError`, `Error` en un string sin cerrar), para que el parser no añada otro error.

**Un token `Error` nunca cierra una expresión.** Tras un error no se sabe qué quería decir la línea, y un `Newline` tras un `$` suelto daría al parser un segundo error.

### Fuzzing nativo

A un lexer, con cualquier entrada, no se le pide el resultado exacto, sino **propiedades**:

- que no haga panic;
- que el último token sea `Eof`;
- que cada span cumpla `Lo <= Hi <= len`;
- que los `Lo` no decrezcan.

Un test de fuzzing es una función `FuzzXxx(f *testing.F)`. `f.Add(…)` registra semillas y `f.Fuzz(func(t *testing.T, src string) { … })` recibe el objetivo (el *fuzz target*).

`go test` solo corre las semillas. `go test -run='^$' -fuzz=FuzzLex -fuzztime=10s` genera entradas, guiado por la cobertura. Una entrada que hace fallar el objetivo va a `testdata/fuzz/FuzzLex/` y pasa a ser una semilla.

## Tests primero

{{cards}}

Escribe el test de cada tarjeta, míralo fallar, implementa y solo entonces despliega su referencia. Describen el final del capítulo; cada sesión dice qué filas aplaza.

:::more Los ayudantes de test
Están al principio de cada fichero de test, en `ref/ch05-sK`.

- **`lexer_test.go`.** `lexAll` y `lexFile` lexan un texto y devuelven los tokens y el sink (`lexFile`, además, el id del fichero y el interner); `lexKinds` y `wantKinds` comparan la lista de tipos; `dumpTokens` devuelve el volcado. `wantOne`, de `source_check_test.go` (capítulo 3), exige un único diagnóstico con su código, mensaje, span y etiqueta.
- **`numbers_test.go`.** `intValues` y `floatValues` sacan los valores de los literales, y `wantNoDiags` exige cero diagnósticos.
- **`strings_test.go`.** `shape` escribe cada token como en el volcado y `wantShape` compara esa lista.
- **`line_rule_test.go`.** Una fila `lineRow` lleva el texto, la forma de sus tokens, los spans de sus `Newline` y cuántos diagnósticos da, y `checkLineRows` comprueba las tres cosas.
- **`update_test.go`.** `runUpdate` ejecuta el runner con `Update` sobre un fichero temporal y devuelve el fichero resultante y el informe.
:::

## Implementación

### Sesión 1: tokens, `go generate` y el esqueleto del lexer (≈ 2 h)

#### Los tests, en rojo

1. **Escribe T05.01–T05.13,** en `package syntax_test`. Describen esta interfaz:

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
5. **Añade dos stubs** para que compile: `Lex` devuelve `TokenBuffer{Tokens: []Token{{Kind: Eof}}}` y `DumpTokens`, `""`. Pasan las tres primeras tarjetas; las demás fallan sin panic si compruebas la longitud con `t.Fatalf` antes de indexar.

#### El código

{{file ch05-s1 internal/syntax/token_kinds.txt}}

{{file ch05-s1 internal/syntax/gen/main.go}}

`gen` usa el `FlagSet` global de `flag` (`flag.String`, `flag.Parse`), que ante un flag malo sale con 2. Aquí sirve, porque nadie lo ejecuta en proceso.

En la ayuda de `-o`, las comillas invertidas nombran el argumento: `-h` imprime `-o file`.

{{file ch05-s1 internal/syntax/token.go}}

`Token` no guarda texto y ocupa 24 bytes: `Payload` apunta a las tablas de `TokenBuffer`, la idea del capítulo 4.

`token_kind.go` declara sus nombres con `[...]string{KwFn: "KwFn", …}`, un literal de array con claves: el compilador cuenta el tamaño, y reordenar la lista no desplaza la tabla.

{{file ch05-s1 internal/syntax/lexer.go}}

- **`switch c := lx.src[lx.pos]; {`** lleva sentencia inicial: `c` se ve en todos los `case`, y sin expresión, cada `case` es una condición.
- **`punctuation`** es el maximal munch. `return two('.', DotDot, Dot)` devuelve los tres resultados del closure `two`.
- **`ident`** busca con `keywords[string(text)]`, una conversión que no reserva memoria (capítulo 4, ejercicio 3).
- **`visible`** escribe como `<U+XXXX>`, en los mensajes, lo que `unicode.IsPrint(r)` rechaza.
- **`DumpTokens`** cita los textos con `strconv.Quote` y escribe los `Float` con `strconv.FormatFloat(v, 'g', -1, 64)`, la forma más corta que vuelve a leerse igual. Hasta la sesión 4 es cuadrático en una línea larga: `LineCol` la recuenta en cada token.

{{reftest T05.04}}

{{reftest T05.11}}

Si te atascas o tu versión se desvía demasiado, copia el fichero de `ref/ch05-s1` y sigue.

Al terminar: tus tests en verde, commit y etiqueta `ch05-s1`; compárate con `ref/ch05-s1`.

### Sesión 2: números (≈ 2 h)

#### Los tests, en rojo

1. **Escribe T05.14–T05.26.**
2. **Míralos fallar.** Compilan, y fallan sin panic: hoy cada dígito es un E0104.

#### El código

{{diff ch05-s1..ch05-s2 internal/syntax/lexer.go}}

{{file ch05-s2 internal/syntax/numbers.go}}

- **`digitRun`** recibe la clase de dígito como valor de tipo función, `isDigit func(byte) bool`, y sirve a las tres bases. En sus resultados, `_` deja el primero sin nombre.
- **El punto y la `e`.** Un `.` es del número solo si lo sigue un dígito (si no, `1..2` empezaría por un `Float`); una `e`, solo si la siguen dígitos, con signo opcional.
- **`emitInt`** marca un invariante con `panic`: con los dígitos ya comprobados, solo cabe el error de rango.

{{reftest T05.17}}

{{reftest T05.19}}

Si te atascas o tu versión se desvía demasiado, copia el fichero de `ref/ch05-s2` y sigue.

Al terminar: tus tests en verde, commit y etiqueta `ch05-s2`; compárate con `ref/ch05-s2`.

### Sesión 3: caracteres, strings e interpolación (≈ 2 h)

#### Los tests, en rojo

1. **Escribe T05.27–T05.41.**
2. **Deja tres tarjetas sin los `Newline` de la sesión 4.** Escribe estas filas sin ellos y con un `false` menos en `NewlineBefore`:
   - en T05.32, las cuatro que acaban en `x` tras el salto;
   - en T05.36, `a\n  "abc\nx` y `a\n"abc`;
   - en T05.41, las seis cuyo último token antes del salto cierra una expresión, y `a\n"${\nx`.
3. **Míralos fallar.** Fallan sin panic: hoy una comilla es E0104.

#### El código

{{diff ch05-s2..ch05-s3 internal/syntax/lexer.go}}

{{file ch05-s3 internal/syntax/strings.go}}

- **Escapes.** Uno inválido aporta U+FFFD. `utf8.AppendRune(b, r)` añade la codificación de un escalar a un `[]byte`.
- **`unterminated`** sustituye los `StringHead` y `StringMid` sin final por un único `Error`, **en el sitio del primer token**: al final, los spans retrocederían. Como `append(toks[:outer.first], Token{…})` reutiliza el array, antes copia lo que conserva en `kept`.

{{reftest T05.38}}

{{reftest T05.39}}

Si te atascas o tu versión se desvía demasiado, copia el fichero de `ref/ch05-s3` y sigue.

Al terminar: tus tests en verde, commit y etiqueta `ch05-s3`; compárate con `ref/ch05-s3`.

### Sesión 4: la regla de línea, `ki emit tokens`, `--update` y el primer fuzz (≈ 2 h)

#### Los tests, en rojo

1. **Devuelve a T05.07, T05.32, T05.36 y T05.41 sus `Newline`.** T05.07 pasa a cuatro tokens y `NewlineBefore` en `false, false, true, false`; las otras, como en este diff:

{{diff ch05-s3..ch05-s4 internal/syntax/strings_test.go}}

2. **Escribe T05.42–T05.67.** Dos llevan un paquete propio:
   - T05.65, en `line_rule_internal_test.go` con `package syntax`, porque `bracketStack` no se exporta, y su propio `TestXxx`.
   - T05.67, en `gen/main_test.go` con `package main`.

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

3. **Míralos fallar.** `internal/kitest` e `internal/syntax` no compilan, fallan cinco subtests de la CLI y el golden `ch05/line_rule.ki` sale con 2 en lugar de 0. Ya pasa `gen`.

#### La regla de línea

{{file ch05-s4 internal/syntax/line_rule.go}}

- **Una pasada al final.** `insertSeparators` recorre los tokens con su propia pila, que sí lleva `(` y `[`, y pone cada `Newline`.
- **`bracketStack`** guarda los delimitadores abiertos y un contador por tipo. `*s.count(open)++` incrementa el contador a través del puntero que devuelve `count`.
- **Un cierre** sin pareja (contador a 0) no hace nada. Con pareja, saca lo de encima con `for s.pop() != want {}`, un bucle de cuerpo vacío. Ningún delimitador entra ni sale dos veces: coste lineal.
- **`enter` y `leave`.** `StringHead` abre un nivel y `StringTail` lo cierra: sin `leave`, `"${ f(a }"` dejaría el `(` abierto y apagaría los saltos del resto del fichero.

{{diff ch05-s3..ch05-s4 internal/syntax/lexer.go}}

`DumpTokens` pasa a ser lineal: sin `\n` entre dos tokens, suma a la columna los code points que los separan (`utf8.RuneCount`).

Su test se salta la fila larga si falló una corta (`if t.Failed() { return }`): un volcado lento agotaría el tiempo y escondería los mensajes.

{{reftest T05.52}}

#### `ki emit tokens` y `ki check`

{{file ch05-s4 internal/driver/emit.go}}

{{diff ch05-s3..ch05-s4 internal/driver/commands.go}}

{{diff ch05-s3..ch05-s4 internal/driver/check.go}}

`ki check` llama ahora a `Lex`, que ya ejecuta `CheckSource`: llamar a las dos daría cada E0101 dos veces.

`NewSession` crea el `Interner` con `base.NewInterner()`: su valor cero no sirve, `Intern` haría panic con un puntero nulo.

Un golden comprueba el comando entero:

{{file ch05-s4 tests/golden/ch05/line_rule.ki}}

#### `ki test --update`

`--update` reescribe las salidas esperadas de un golden con lo impreso.

{{file ch05-s4 internal/kitest/update.go}}

{{diff ch05-s3..ch05-s4 internal/kitest/runner.go}}

- Solo toca `expect-stdout` y `expect-stderr`: si ajustara el `expect-exit`, `--update` aprobaría un panic nuevo.
- Solo reescribe si `exit >= 0`: una ejecución que no llegó a salir (-1) no tiene salida que guardar.

:::more Cómo reescribe `--update` un fichero
`rewriteExpectations` corta el fichero con `bytes.SplitAfter`, que deja cada `\n` pegado a su línea. No edita en el sitio: anota cada cambio como un `edit` (dónde, cuántas líneas quita, cuáles pone). Los ordena con `slices.SortStableFunc` (estable, para que stdout siga delante de stderr si van al mismo sitio) y copia el fichero intercalándolos. Lo demás pasa tal cual, con su BOM y sus CRLF.

`var errText *string` es un puntero que puede ser `nil`: con anotaciones, stderr no se escribe.
:::

#### El primer fuzz

{{reftest T05.62}}

1. **Ejecútalo una vez:** `go test ./internal/syntax -run='^$' -fuzz=FuzzLex -fuzztime=10s` termina en `PASS` y no escribe nada en `testdata/`.
2. **Rómpelo.** En `lineEndAt`, cambia `i+1 < len(lx.src)` por `<=`.
3. **Ejecútalo otra vez.** En segundos, encuentra una entrada que hace panic, la guarda en `testdata/fuzz/FuzzLex/` e imprime `go test -run=FuzzLex/<nombre>`.
4. **Deshaz el cambio y borra el fichero.**

Si te atascas o tu versión se desvía demasiado, copia el fichero de `ref/ch05-s4` y sigue.

Al terminar: tus tests en verde, commit y etiqueta `ch05-s4`; compárate con `ref/ch05-s4`.

## Ejecútalo

Desde la raíz de tu repo, con `ki` construido (`go build -o build/bin/ki ./cmd/ki`), el programa del objetivo:

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

No hay `Newline` tras `LBrace`, que no cierra una expresión. Con errores, el volcado sale por stdout, los diagnósticos por stderr y el código es 1:

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

Y `--update` sobre un golden desfasado:

```console
$ cat up/a.ki
// test: T00.01
// run: emit tokens
// expect-stdout: 1:1 Eof
let a = 1
$ build/bin/ki test --update up
FAIL up/a.ki
- 4:1 KwLet
+ 9:1 KwLet
ki test: 0 passed, 1 failed
$ build/bin/ki test --update up
PASS up/a.ki
ki test: 1 passed, 0 failed
```

La primera pasada añade cinco líneas, que mueven el código de la línea 4 a la 9; el mismo comando repite la ejecución y falla. La segunda lo deja estable.

## Ejercicios

:::exercise
**1. `ki emit tokens --stats`.** Añade un flag que, en lugar del volcado, imprima una línea `<Kind> <count>` por tipo de token, ordenadas por nombre. Con su test.
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

`fs.BoolVar`, como `fs.String` pero booleano, escribe en `stats`. Un `[256]int` indexado por el tipo hace de mapa sin orden aleatorio. La línea de uso pasa a ser `ki emit tokens [--stats] [--diagnostics=human|short|json] FILE`, también en la constante `usage` del test de `emit` con un destino desconocido. El test, en `TestCLI`:

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

Sin el `SortFunc` (y sin el import de `slices`, que se queda sin uso), las líneas salen en el orden de las constantes y el test falla.
:::

:::exercise
**2. `-` y `+` frente a `|>`.** Explica con ejemplos por qué una línea que empieza por `-` o `+` no continúa la anterior y una que empieza por `|>` sí.
:::

:::solution
`-` y `+` también son prefijos: `-b` es una expresión completa. Si continuaran la línea, `let x = a⏎-b` sería una resta, y el significado de un bloque dependería de dónde se parte una línea. En Ki son dos sentencias; para partir una suma, el operador va al final: `a +⏎b` no recibe `Newline`.

`|>` no puede empezar una expresión, así que una línea que empieza por él solo puede continuar la anterior: `xs⏎  |> sum` es una sentencia. Igual con `.`, `&&`, `||` y `else`; no con `|` solo, que empieza un brazo de patrón.
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

Lo que vigila es que el resultado dependa solo del texto: un estado que sobreviva entre dos llamadas a `Lex`, o un volcado que imprima números de símbolo en lugar de nombres. Este último mutante, `(%d)` con `tok.Payload` en `DumpTokens`, falla en las tres semillas.
:::

:::exercise
**4. Un mega de Ki.** Mide `Lex` con un fichero de 1 MB (`b.Loop`, `-benchmem`) y explica las reservas que quedan con el perfil de memoria: `-memprofile` y `go tool pprof -sample_index=alloc_space`.
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

`b.SetBytes` añade la columna MB/s. Salen unos 20 ms por operación, 65,8 MB y 28 655 reservas.

Con `-memprofile mem.out` y `go tool pprof -sample_index=alloc_space -top`, dos tercios de los bytes son el `append` de `emit`, que hace crecer `Tokens` copiándolo varias veces. Algo más de una cuarta parte es `insertSeparators`, que reserva `out` con `len(toks)` y crece porque cada `Newline` es un token de más.

Las reservas salen, según `alloc_objects` (con `-lines`), de tres líneas: la conversión `string(text)`, que copia cada trozo de string, y dos `append` que parten de un slice vacío, el `digitRun(nil, …)` de un hexadecimal y el del escape `\n`.
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

Veintiún sietes son 63 bits a uno, el mayor `Int`; un 1 y 21 ceros, 2⁶³. `code` es `uint16` porque `diag.E` lo pide. `0o8` da E0107 sobre `0o` y después `Int(8)`, como `0b2`. Con `c <= '8'`, `0o8` llega al `panic` de `emitInt`.
:::

## Preguntas de repaso

:::review
¿Por qué un literal que desborda deja un token `Int` con valor 0, y no el valor de `ParseInt` ni ningún token?
:::

:::answer
Sin token, el parser daría un segundo error por la misma equivocación. Con el valor de `ParseInt`, el máximo, un programa rechazado llevaría un número que nadie escribió. El 0 marca el sitio; el diagnóstico ya dice qué pasó.
:::

:::review
¿Por qué `token_kind.go` se versiona y un test lo compara con la salida del generador?
:::

:::answer
Porque `go generate` no corre solo: sin versionar, el paquete no compilaría hasta generarlo. Y si alguien cambia la lista sin regenerar, una palabra clave nueva se lee como un nombre sin que nada avise; el test lo convierte en un fallo que pide regenerar.
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
¿Por qué el `Error` de un string sin cerrar va en el sitio de su primer token y no al final?
:::

:::answer
Porque así los `Lo` no decrecen, que es lo que piden los diagnósticos ordenados por posición y el fuzz target. Al final, el `Error`, que empieza en la comilla, quedaría detrás de tokens que empiezan después.
:::

:::review
¿Qué gana un fuzz target frente a una tabla de casos?
:::

:::answer
La tabla prueba las entradas que alguien pensó, con su resultado exacto. El fuzz target prueba propiedades con entradas que genera él, guiado por la cobertura: con `<=` en `lineEndAt` encontró por sí mismo un `\r` como último byte del fichero, dentro de un string sin cerrar. Y lo que encuentra se queda en `testdata/fuzz/` como semilla, que `go test` ejecuta siempre.
:::

## Resumen

- El lexer trabaja con bytes y solo decodifica donde importa el carácter entero. Cada token es el más largo posible.
- `errors.Is(err, strconv.ErrRange)` detecta un desbordamiento; su valor no se guarda.
- `go generate` solo corre a mano; lo generado se versiona y un test lo vigila.
- Una pila de modos y un centinela parten los strings con interpolación.
- La regla de línea mira el delimitador más interno, el token anterior y la línea siguiente.
- Un token `Error` nunca cierra una expresión, y un literal roto deja un token en su sitio.
- Un fuzz target corre semillas con `go test` y busca entradas con `-fuzz`.
- `ki test --update` reescribe solo las salidas esperadas.

## Go en el trabajo

**Funciones de estado.** En «Lexical Scanning in Go» (2011), Rob Pike presentó el lexer de `text/template`: cada estado es una función que devuelve el siguiente (`type stateFn func(*lexer) stateFn`), y los tokens van por un canal.

Hoy ese lexer ejecuta los estados en un bucle, sin goroutines ni canales. El de Ki es un bucle con un `switch`: la interpolación anidada pide una pila de modos de todas formas.

La biblioteca trae dos scanners: `go/scanner`, el del propio Go, y `text/scanner`, genérico.

**`go generate` en repositorios reales.** Más de cien ficheros de `$GOROOT/src` lo usan. Según «Generating code» (Rob Pike, blog de Go, 2014), es para el autor del paquete, no para sus clientes. Para llegarles, lo generado «must be checked into the source code repository».
