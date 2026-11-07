---
chapter: 5
title: El lexer y la regla de línea
part: I
hours: 8
milestone: `ki emit tokens`
---

## Objetivo

Al terminar, `ki` tiene su primera etapa de compilador: un lexer que convierte bytes en tokens, decide qué saltos de línea terminan una sentencia y sigue tras cada error. Lo enseña `ki emit tokens`, que vuelca los tokens de un fichero como este:

```ki
fn main() {
  let n = fib(20)
    |> show
  print("fib = ${n}\n")
}
```

Al ejecutarlo no sale `Newline` antes de `|>`, que continúa la línea, y el string se parte en cabeza, expresión y cola. El parser del capítulo 6 leerá estos tokens, camino del hito M1 (capítulo 10).

:::note
**Fe de erratas (2026-10-28).** Los mensajes de error de este capítulo mejoran en el capítulo 7.
:::

## Teoría

### Bytes, runas y el maximal munch

El lexer trabaja casi siempre con bytes: lo que tiene significado es ASCII, y ningún byte de un carácter multibyte baja de 0x80.

Solo decodifica el carácter entero (capítulo 2) donde importa:

- un carácter inesperado: byte a byte, `é` daría dos errores;
- un escape inválido: `\é` consume la `é` entera;
- un literal de carácter: `'é'` debe ser un code point.

Un `\u{…}` solo admite escalares Unicode, code points que no son sustitutos.

:::from Rust
`char` de Rust solo admite escalares; un `rune` de Go es un `int32` cualquiera, y la comprobación es tuya.
:::

**Maximal munch.** Ante `|>=>`, el lexer toma siempre el token más largo: `|>`, después `=>`.

```math
token = el p ∈ Puntuación más largo que es prefijo de la entrada
|p| ≤ 2   ∀ p ∈ Puntuación
```

Como `|p| ≤ 2`, basta mirar un byte más.

### Los literales numéricos

La gramática de los números (`{ x }`: cero o más veces; `[ x ]`: opcional):

```math
Int   = D  |  0x H  |  0b B
Float = D . D [ Exp ]  |  D Exp
Exp   = ( e | E ) [ + | - ] D

D = d { d | _ }          d ∈ 0…9
H = { _ } h { h | _ }    h ∈ 0…9, a…f, A…F
B = { _ } b { b | _ }    b ∈ { 0, 1 }
```

Una racha de dígitos que acaba en `_` es E0107. El `.` exige un dígito detrás: si no, `1..2` empezaría por un `Float`.

Dos comportamientos de `strconv`, que convierte el texto, deciden el diseño:

- **Un desbordamiento da el máximo (o `+Inf`) junto al error.** Guardarlo daría valor a un literal rechazado; por eso el token se queda con 0.
- **Los `_` de `1_000`.** La conversión en base 10 los rechaza, así que el lexer los quita él.

### Código generado

Los tipos de token viven en una sola lista, `token_kinds.txt`, y un programa Go genera con ella `token_kind.go`: las constantes, sus nombres y el mapa de palabras clave.

- **Solo a mano.** `go generate` lo ejecuta cuando se lo pides; `go build` y `go test`, nunca. Por eso lo generado se versiona.
- **Un test lo vigila.** Como nada regenera solo, un test compara la salida del generador con el fichero versionado.

### Los modos del lexer y la interpolación

En `"a${x}b"`, la `x` es código y la `b`, texto: el lexer cambia de modo. Un `${…}` puede contener otro string, así que los modos se apilan. Con dos interpolaciones, `"a${x}b${y}c"` da `StringHead`, `x`, `StringMid`, `y`, `StringTail`.

Lo difícil es la `}`: en `"${ g({ y }) }"`, la primera cierra una llave y la segunda, la interpolación. Por eso hay una pila de llaves, con un **centinela** por cada `${`:

```pseudo
en código, al leer:
  «"»  →  apila el modo texto y lee el texto
  «{»  →  apila una llave normal; emite LBrace
  «}»  →  si la cima de la pila de llaves es un centinela entonces
            desapila el centinela y el modo código; sigue en el texto
          si no
            desapila una llave, si hay alguna; emite RBrace
  salto de línea o fin de fichero, dentro de una interpolación  →  string sin cerrar (E0110)
en texto, al leer:
  fin de línea o de fichero  →  string sin cerrar (E0110)
  «"»   →  emite String (StringTail si hubo «${»); desapila el modo
  «${»  →  emite StringHead (StringMid si hubo otro); apila un centinela y el modo código
  «\»   →  resuelve el escape
  otro  →  lo añade al texto
```

Solo las llaves entran en la pila, porque solo la `}` tiene que decidir: en `${ ( }`, cierra igual la interpolación.

### La regla de línea

Ki no pide `;`. El lexer anota cada salto en el token siguiente (`NewlineBefore`) e inserta un `Newline` donde acaba una sentencia. El `\r\n` es un solo salto, y su `Newline` ocupa los dos bytes. Un `\r` suelto es un espacio.

Un salto ante el token `t`, tras el token `a`, acaba una sentencia si:

```math
Newline antes de t  ⟺  t.NewlineBefore  ∧  t ≠ Eof
                       ∧  cima(abiertos) ∈ { «{», ninguno }     (1)
                       ∧  a ∈ Cierran                           (2)
                       ∧  t ∉ Continúan                         (3)

Cierran   = { LowerIdent, UpperIdent, Int, Float, Char, String, StringTail,
              «)», «]», «}», «?», «_», true, false, return, break, continue }
Continúan = { «|>», «.», «&&», «||», else }
```

Se lee así: (1) dentro de `(` y `[`, un salto es un espacio; (2) la línea ya está completa; (3) la siguiente no la continúa.

Una pasada al final aplica la regla:

```pseudo
función insertarSeparadores(toks):
  abiertos ← pila vacía, con un contador por tipo de delimitador
  para cada token t, que sigue al token a:
    si t es el primero tras el salto que cortó un string sin cerrar entonces
      cierra el nivel de interpolación de ese string
    si la regla se cumple entonces emite un Newline (el salto tras a)
    emite t
    si t es «(», «[» o «{»  →  lo apila
    si t es «)», «]» o «}»  →  si el contador de su tipo marca una abierta,
                                desapila lo que hay encima de ella, y ella; si no, nada
    si t es StringHead      →  abre un nivel de interpolación
    si t es StringTail      →  cierra el nivel y lo que se abrió dentro de él
    si t es el Error de un string sin cerrar  →  abre un nivel
```

Ningún delimitador entra ni sale dos veces: coste lineal. El nivel de interpolación evita que `"${ f(a }"` deje el `(` abierto y apague los saltos del resto del fichero.

Go inserta `;` mirando solo el final de la línea; Ki mira también los delimitadores y la línea siguiente, y no corta donde Go corta (`⏎`: salto de línea):

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
- Un literal roto deja un token en su sitio, para que el parser no añada otro error: `Int` con 0, `Char` con U+FFFD, y un `Error` que sustituye a los trozos de un string sin cerrar **en el sitio del primero**.

**Un token `Error` nunca cierra una expresión:** no está en `Cierran`. Tras un error no se sabe qué quería decir la línea, y un `Newline` tras un `$` suelto daría al parser un segundo error.

### Fuzzing

A un lexer, con cualquier entrada, no se le pide el resultado exacto, sino **propiedades**:

```math
∀ src, con toks = Lex(src) y n = |toks|:
  Lex(src) no hace panic
  toks[n−1].Kind = Eof
  ∀ i:      toks[i].Lo ≤ toks[i].Hi ≤ len(src)
  ∀ i < j:  toks[i].Lo ≤ toks[j].Lo
  ∀ i:      la línea i del volcado empieza por la posición de toks[i]   (src ≤ 16 KiB)
```

Un fuzzer genera las entradas guiado por la cobertura. La que rompe una propiedad se guarda como semilla, y los tests la ejecutan siempre.

## Preguntas de repaso

:::review
¿Por qué un literal que desborda deja un token `Int` con valor 0, y no el que da `strconv` ni ningún token?
:::

:::answer
Sin token, el parser daría un segundo error por la misma equivocación. Con ese valor, el máximo, un programa rechazado llevaría un número que nadie escribió. El 0 marca el sitio; el diagnóstico ya dice qué pasó.
:::

:::review
¿Por qué `token_kind.go` se versiona y un test lo compara con la salida del generador?
:::

:::answer
Porque `go generate` no corre solo: sin versionar, el paquete no compilaría hasta generarlo. Y si alguien cambia la lista sin regenerar, una palabra clave nueva se lee como un nombre sin que nada avise; el test lo convierte en un fallo que pide regenerar.
:::

:::review
¿Por qué la pila de llaves del lexer guarda solo llaves y centinelas, y no `(` ni `[`?
:::

:::answer
Porque solo la `}` tiene que decidir algo: si cierra una llave o una interpolación, y para eso basta mirar si la cima es un centinela. Un `(` abierto no cambia la respuesta: en `${ ( }`, la `}` cierra la interpolación igual. Los paréntesis los sigue la regla de línea, con su propia pila.
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
Porque así los `Lo` no decrecen, como pide el fuzz target: al final, el `Error`, que empieza en la comilla, quedaría detrás de tokens que empiezan después. Y el parser lee el `Error` donde estaba el string, antes que el código de dentro: al final, ese código le daría errores de más.
:::

:::review
¿Qué gana un fuzz target frente a una tabla de casos?
:::

:::answer
La tabla prueba las entradas que alguien pensó, con su resultado exacto. El fuzz target prueba propiedades con entradas que genera él, guiado por la cobertura: un error de límite en el lexer lo encontró por sí mismo, un `\r` como último byte del fichero dentro de un string sin cerrar (lo verás en la sesión 4). Y lo que encuentra se queda en `testdata/fuzz/` como semilla, que `go test` ejecuta siempre.
:::

## Resumen

- El lexer trabaja con bytes y solo decodifica donde importa el carácter entero.
- Cada token es el más largo posible.
- Un literal que desborda deja un token con 0, y los `_` se quitan antes de convertir.
- El código generado se versiona, y un test lo vigila.
- Una pila de modos y un centinela parten los strings con interpolación.
- La regla de línea mira el delimitador más interno, el token anterior y la línea siguiente, en una pasada lineal.
- Un token `Error` nunca cierra una expresión, y un literal roto deja un token en su sitio.
- Un fuzz target comprueba propiedades con entradas guiadas por la cobertura.

## Go en el trabajo

**Funciones de estado.** En «Lexical Scanning in Go» (2011), Rob Pike presentó el lexer de `text/template`: cada estado es una función que devuelve el siguiente (`type stateFn func(*lexer) stateFn`), y los tokens van por un canal.

Hoy ese lexer ejecuta los estados en un bucle, sin goroutines ni canales. El de Ki es un bucle con un `switch`: la interpolación anidada pide una pila de modos de todas formas.

La biblioteca trae dos scanners: `go/scanner`, el del propio Go, y `text/scanner`, genérico.

**`go generate` en repositorios reales.** Más de cien ficheros de `$GOROOT/src` lo usan. Según «Generating code» (Rob Pike, blog de Go, 2014), es para el autor del paquete, no para sus clientes. Para llegarles, lo generado «must be checked into the source code repository».

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

#### Go: `go generate`

- **La directiva.** `//go:generate go run ./gen -o token_kind.go`, sin espacio tras `//`. `go generate ./internal/syntax` la ejecuta en el directorio del paquete.
- **La cabecera.** `// Code generated … DO NOT EDIT.` avisa a personas y herramientas de que no se edita a mano.
- **`go/format`.** `format.Source(src)` formatea como `gofmt`: el generador no alinea nada.
- **El test.** Ejecuta `go run ./gen` con `os/exec` (capítulo 1).

:::more Leer por líneas con `bufio.Scanner`
`gen` lee la lista entera; para leer por líneas, lo habitual es `sc := bufio.NewScanner(r)`, `for sc.Scan() { line := sc.Text() }` y `sc.Err()` al final. El `Scanner` quita el `\r` de un CRLF, y una línea de 64 KiB o más (`bufio.MaxScanTokenSize`) detiene el bucle con un error.
:::

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

```console doctest="red"
$ go test ./internal/syntax/
# github.com/aldezex/kilang/internal/syntax_test [github.com/aldezex/kilang/internal/syntax.test]
internal/syntax/lexer_test.go:13:47: undefined: syntax.TokenBuffer
…
internal/syntax/lexer_test.go:65:52: too many errors
FAIL	github.com/aldezex/kilang/internal/syntax [build failed]
FAIL
```

4. **Escribe la lista, el generador y `token.go`,** y ejecuta `go generate ./internal/syntax`.
5. **Añade dos stubs** para que compile: `Lex` devuelve `TokenBuffer{Tokens: []Token{{Kind: Eof}}}` y `DumpTokens`, `""`. Pasan las tres primeras tarjetas; las demás fallan sin panic si compruebas la longitud con `t.Fatalf` antes de indexar.

#### El código

{{file ch05-s1 internal/syntax/token_kinds.txt}}

{{file ch05-s1 internal/syntax/gen/main.go}}

`gen` usa el `FlagSet` global de `flag` (`flag.String`, `flag.Parse`), que ante un flag malo sale con 2. Aquí sirve, porque nadie lo ejecuta en proceso.

{{file ch05-s1 internal/syntax/token.go}}

`Token` no guarda texto y ocupa 24 bytes: `Payload` apunta a las tablas de `TokenBuffer`, la idea del capítulo 4.

`token_kind.go` declara sus nombres con `[...]string{KwFn: "KwFn", …}`, un literal de array con claves: el compilador cuenta el tamaño, y reordenar la lista no desplaza la tabla.

{{file ch05-s1 internal/syntax/lexer.go}}

- **`switch c := lx.src[lx.pos]; {`** lleva sentencia inicial: `c` se ve en todos los `case`, y sin expresión, cada `case` es una condición.
- **`punctuation`** es el maximal munch de la Teoría. `return two('.', DotDot, Dot)` devuelve los tres resultados del closure `two`.
- **`ident`** busca con `keywords[string(text)]`, una conversión que no reserva memoria (capítulo 4, ejercicio 3).
- **`unexpected`** decodifica con `utf8.DecodeRune(b)` (capítulo 2).
- **`visible`** escribe como `<U+XXXX>`, en los mensajes, lo que `unicode.IsPrint(r)` rechaza.
- **`DumpTokens`** cita los textos con `strconv.Quote` y escribe los `Float` con `strconv.FormatFloat(v, 'g', -1, 64)`, la forma más corta que vuelve a leerse igual. Hasta la sesión 4 es cuadrático en una línea larga: `LineCol` la recuenta en cada token.

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

`source_check.go` cambia un comentario:

{{diff ch04-s2..ch05-s1 internal/syntax/source_check.go}}

{{reftest T05.04}}

{{reftest T05.11}}

Si te atascas o tu versión se desvía demasiado, copia el fichero de `ref/ch05-s1` y sigue.

Al terminar: tus tests en verde, commit y etiqueta `ch05-s1`; compárate con `ref/ch05-s1`.

### Sesión 2: números (≈ 2 h)

#### Go: `strconv`

`strconv.ParseInt(s, base, 64)` y `strconv.ParseFloat(s, 64)` convierten texto en `int64` y `float64`. Sus errores son `*strconv.NumError`: `Err` guarda la causa (`strconv.ErrRange`, `strconv.ErrSyntax`), y `errors.Is` (capítulo 2) la encuentra.

:::more `strconv` en los límites
Un underflow no es un error, y por eso `1e-999` vale 0 sin diagnóstico.

| Llamada | Resultado |
|---|---|
| `ParseInt("9223372036854775808", 10, 64)` | `9223372036854775807` y `value out of range` |
| `ParseInt("1_000", 10, 64)` | `0` y `invalid syntax` (con base 0, `1000`) |
| `ParseFloat("1e999", 64)` | `+Inf` y `value out of range` |
| `ParseFloat("1e-999", 64)` | `0` y `nil` |
| `ParseFloat("1_000.5", 64)` | `1000.5` y `nil`; con `1__0.5`, `invalid syntax` |
:::

#### Los tests, en rojo

1. **Escribe T05.14–T05.26.**
2. **Míralos fallar.** Compilan, y fallan sin panic: hoy cada dígito es un E0104.

#### El código

{{diff ch05-s1..ch05-s2 internal/syntax/lexer.go}}

{{file ch05-s2 internal/syntax/numbers.go}}

- **`digitRun`** recibe la clase de dígito como valor de tipo función, `isDigit func(byte) bool`, y sirve a las tres bases. En sus resultados, `_` deja el primero sin nombre.
- **`number`** sigue la gramática de la Teoría.
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

- **Modos y centinela.** `modeString`, `modeCode` e `InterpSentinel` son los de la Teoría; `endInterpolation` y `trackBraces` deciden la `}`.
- **Escapes.** Uno inválido aporta U+FFFD. `utf8.ValidRune(r)` comprueba que un `\u{…}` es un escalar, y `utf8.AppendRune(b, r)` añade su codificación a un `[]byte`.
- **`unterminated`** pone el `Error` en el sitio del primer token, como pide la Teoría. Como `append(toks[:outer.first], Token{…})` reutiliza el array, antes copia lo que conserva en `kept`.

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

3. **Actualiza tres tests de la CLI,** por el comando y el flag nuevos: T01.04 acaba en `emit`; T03.56 lleva `[--update]` en su línea de uso; T03.66 suma un caso `--update`, que arregla `fail.ki` y sale con 0.
4. **Míralos fallar.** `internal/kitest` e `internal/syntax` no compilan, fallan cinco subtests de la CLI y el golden `ch05/line_rule.ki` sale con 2 en lugar de 0. Ya pasa `gen`.

#### La regla de línea

{{file ch05-s4 internal/syntax/line_rule.go}}

- **`insertSeparators`** es la pasada de la Teoría: `endsExpression` y `continuesLine` son sus conjuntos, y `enter` y `leave`, los niveles de interpolación.
- **`bracketStack`** lleva los contadores por tipo de la Teoría. `*s.count(open)++` incrementa el contador a través del puntero que devuelve `count`, y `for s.pop() != want {}` es un bucle de cuerpo vacío.

{{diff ch05-s3..ch05-s4 internal/syntax/lexer.go}}

`DumpTokens` pasa a ser lineal: sin `\n` entre dos tokens, suma a la columna los code points que los separan (`utf8.RuneCount`).

{{reftest T05.52}}

#### `ki emit tokens` y `ki check`

{{file ch05-s4 internal/driver/emit.go}}

{{diff ch05-s3..ch05-s4 internal/driver/commands.go}}

{{diff ch05-s3..ch05-s4 internal/driver/check.go}}

`ki check` llama ahora a `Lex`, que ya ejecuta `CheckSource`: llamar a las dos daría cada E0101 dos veces.

{{diff ch05-s3..ch05-s4 internal/driver/session.go}}

`NewSession` crea el `Interner` con `base.NewInterner()`: su valor cero no sirve, `Intern` haría panic con un puntero nulo.

Un golden comprueba el comando entero:

{{file ch05-s4 tests/golden/ch05/line_rule.ki}}

#### `ki test --update`

`--update` reescribe las salidas esperadas de un golden con lo impreso.

{{file ch05-s4 internal/kitest/update.go}}

{{diff ch05-s3..ch05-s4 internal/kitest/runner.go}}

- Solo toca `expect-stdout` y `expect-stderr`: si ajustara el `expect-exit`, `--update` aprobaría un panic nuevo.
- Solo reescribe si `exit >= 0` (ejercicio 6).

{{diff ch05-s3..ch05-s4 internal/driver/test.go}}

:::more Cómo reescribe `--update` un fichero
`rewriteExpectations` corta el fichero con `bytes.SplitAfter`, que deja cada `\n` pegado a su línea. No edita en el sitio: anota cada cambio como un `edit` (dónde, cuántas líneas quita, cuáles pone). Los ordena con `slices.SortStableFunc` (estable, para que stdout siga delante de stderr si van al mismo sitio) y copia el fichero intercalándolos. Lo demás pasa tal cual, con su BOM y sus CRLF.

`var errText *string` es un puntero que puede ser `nil`: con anotaciones, stderr no se escribe.
:::

#### Go: fuzzing nativo

Un test de fuzzing es una función `FuzzXxx(f *testing.F)`: `f.Add(…)` registra semillas y `f.Fuzz(func(t *testing.T, src string) { … })` recibe el objetivo (el *fuzz target*). `go test` solo corre las semillas; `-fuzz` busca más y guarda en `testdata/fuzz/` las que fallan.

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
$ cat fib.ki
fn main() {
  let n = fib(20)
    |> show
  print("fib = ${n}\n")
}
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
`-` también es prefijo: `-b` es una expresión completa. Si continuara la línea, `let x = a⏎-b` sería una resta, y el significado de un bloque dependería de dónde se parte una línea. En Ki son dos sentencias.

`+` no es prefijo, pero sigue la misma regla, porque no está en la lista: el parser del capítulo 6 da E0201 con `a⏎+b`, en lugar de una suma silenciosa. Para partir una suma, el operador va al final: `a +⏎b` no recibe `Newline`.

`|>` no puede empezar una expresión, así que una línea que empieza por él solo puede continuar la anterior: `xs⏎  |> sum` es una sentencia. Igual con `.`, `&&`, `||` y `else`; no con `|` solo, que no está en la lista: `a⏎| b` recibe `Newline`.
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

:::exercise
**6. Una ejecución que no salió.** ¿Por qué `--update` no guarda la salida de una ejecución que dio -1?
:::

:::solution
Porque -1 significa que el programa no llegó a salir: no arrancó o murió por una señal. Lo que imprimió es el motivo del fallo o una salida a medias, y guardarlo haría de un fallo el golden esperado. El fichero se queda como estaba y falla por el código de salida.
:::
