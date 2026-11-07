---
chapter: 3
title: Tipos suma sin tipos suma: interfaces selladas y errores como valores
part: I
hours: 7
milestone: `ki check` muestra un diagnóstico con caret
---

## Objetivo

Al terminar, `ki check` señala con un caret qué falla en un fichero, y `ki test`, el *runner* de Ki, ejecuta los programas de prueba y compara su salida con sus cabeceras.

Sin lexer, el único error posible es un fichero que no es texto, pero diagnósticos y runner ya funcionan.

:::note
**Fe de erratas (2026-10-28).** Estos mensajes de error mejoran en el capítulo 7.
:::

## Teoría

### Un error es un dato

Un error del programa se muestra con caret para una persona, como `fichero:línea:columna: mensaje` para un editor y en JSON para el runner.

Por eso es un **valor**: cada pase (una etapa del compilador) entrega un `diag.Diagnostic` a un `diag.Sink`, y otra pieza decide cómo mostrarlo. `error` queda para lo que falla fuera del programa, y `panic`, para un fallo del compilador.

Gravedad y formato son tipos suma (`enum` en Rust), que Go no tiene: usa `iota` para valores, interfaces selladas para tipos y `(T, error)` en lugar de `Result`.

### Tipos definidos, `iota` y `String()`

En un bloque `const`, `iota` vale 0 y aumenta en uno con cada constante; una constante sin expresión repite la anterior.

Go **no** cierra el conjunto: `Severity(7)` compila, y un `switch` incompleto no avisa. Por eso `String()` escribe `Severity(7)`: un valor que solo sale de un fallo se ve como tal.

### `fmt`: verbos, `Stringer` y `Formatter`

`fmt` llama a `String() string`, la interfaz `fmt.Stringer`, con los verbos de cadena (`%v`, `%s`, `%q`, `%x`), no con `%d`: `%d` de `Warning` da `1`. El `Code` de un error usa `"%c%04d"`, un carácter y cuatro cifras con ceros: `E0101`.

Si `String()` no basta, `fmt.Formatter` decide la salida de **todos** los verbos salvo `%T` y `%p`.

| Verbo | Qué escribe |
| --- | --- |
| `%02X` | hexadecimal con dos cifras, en mayúsculas (`FF`) |
| `%x` | hexadecimal en minúsculas, sin relleno (`1f`) |
| `%#v` | la sintaxis de Go, sin `String()` |
| `%+v` | los nombres de los campos, solo en un struct sin `String()` |

### Interfaces y satisfacción implícita

Un tipo cumple una interfaz si tiene sus métodos, sin declararlo (capítulo 1): el compilador lo comprueba al asignar:

```math
T cumple I   ⇔   métodos(I) ⊆ métodos(T)
```

Un valor de interfaz guarda el tipo concreto y el valor. La **aserción de tipo** pregunta por el tipo: si no es, `w.(T)` hace panic y `v, ok := w.(T)` da `ok == false`. `T` puede ser una interfaz sin nombre: así pregunta el runner si su salida sabe vaciarse.

El **type switch**, `switch v := x.(type)`, hace varias aserciones; en un `case` de un solo tipo, `v` lo tiene.

:::from Rust
Una interfaz es un trait sin `impl Trait for T`.
:::

### La interfaz sellada

Para un conjunto cerrado de **tipos**, Go usa una interfaz con un método sin exportar, como `isShape()`: fuera del paquete no se puede nombrar, así que nadie fuera la implementa.

**El sello tiene una grieta.** Un struct de otro paquete que **incrusta** (*embedding*) la interfaz la cumple: incrustar es declarar un campo con el nombre de su tipo, cuyos métodos se **promocionan** al struct:

```math
isShape sin exportar   ⇒   fuera de shape nadie la nombra   ⇒   nadie fuera la implementa
salvo S, un struct que incrusta Shape:   métodos(S) ⊇ métodos(Shape)   por promoción
```

**Y el type switch no es exhaustivo:** si añades `Tri` y olvidas su `case`, compila. Por eso cada type switch sobre una interfaz sellada del libro termina en `default: panic("unreachable")`.

:::from Rust
Un `match` sin `Tri` da `error[E0004]`; en Go lo descubre el `default` al ejecutarse.
:::

### La trampa del nil tipado

Un valor de interfaz solo es `nil` si sus dos mitades lo son:

```math
i = nil   ⇔   tipo(i) = nil  ∧  valor(i) = nil
```

Un puntero `nil` guardado en un `error` deja puesto el tipo: el error no es `nil`. Sin error, devuelve el literal `nil`, nunca una variable de un tipo concreto.

### `errors.Join` y `errors.As`

`errors.Join` reúne varios errores y descarta los `nil`; `errors.Is` encuentra cada uno:

```math
Join(e₁, …, eₙ) = nil              si todos los eᵢ son nil
Is(Join(e₁, …, eₙ), t)   ⇔   ∃ i: Is(eᵢ, t)
```

`errors.As` (capítulo 2) desenvuelve: el runner saca así la causa de un `*fs.PathError`, porque esa capa repite la ruta.

### Sin `Option` ni `Result`

Lo que falla devuelve `(T, error)`. Lo opcional, de dos formas:

- **El valor cero**, si cero no es válido: `Runner.Root == ""` es «el directorio de trabajo».
- **Un puntero**, si lo es: en la cabecera de un golden, `expect-exit: 0` y no decirlo son cosas distintas, así que `Spec.ExpectExit` es un `*int`.

:::from Rust
`*int` hace de `Option<i32>`, pero desreferenciar un `nil` hace panic, y `(T, error)` es un `Result` sin `?`, que nadie obliga a comprobar.
:::

### JSON con etiquetas de struct

`encoding/json` convierte un struct en un objeto con sus campos exportados, en orden de declaración. Una **etiqueta de struct**, una cadena tras el tipo del campo que el paquete lee en ejecución, fija la clave: `json:"severity"`.

`json.Marshal` escribe `null` para un slice o un puntero `nil`, y `[]` para un slice vacío. Una errata como `jsn:"severity"` no avisa: la clave sale `"Severity"`.

### `strings.Builder`, `WalkDir` y `Chdir`

- **`strings.Builder`** acumula texto y lo da con `String()`.
- **`filepath.WalkDir`** llama a una función por cada entrada de un árbol, con un `fs.DirEntry`. Su orden no es el de las rutas completas (visita `a/y.ki` antes que `a-b/x.ki`): el runner ordena.
- **`os.Getwd` y `os.Chdir`** leen y cambian el directorio de trabajo **del proceso entero**: el runner entra en su raíz, para mostrar rutas relativas, y tiene que volver.

### UTF-8 y los ficheros golden

`utf8.Valid(b)` solo responde sí o no; un diagnóstico necesita el **offset** del primer byte malo: por eso existe `FirstInvalidUTF8` (capítulo 2).

Un test **golden** compara una salida larga con la guardada en `testdata/`, que el comando `go` ignora. El flag `-update` la reescribe con lo que el código produce *ahora*, bueno o malo: después lees cada fichero. Va **después** del paquete: `go test` pide los paquetes antes de un flag desconocido.

### Compiladores: diagnósticos como datos

**Códigos estables.** No se reutilizan nunca: los buscas en la documentación y los comparan los tests. Su centena dice la etapa.

**Un límite.** Tras 100 errores, lo demás suele ser cascada del primero: el error 101 trunca el sink, y desde ahí se descarta todo:

```pseudo
función Report(d)
  si truncado entonces devuelve          (también un warning)
  si d es un error entonces
    si errores = 100 entonces
      truncado ← cierto
      devuelve
    errores ← errores + 1
  añade d a diags
```

**Un orden estable.** El sink los ordena por posición, de forma estable:

```math
d₁ antes que d₂   ⇔   (id de fichero₁, offset₁) < (id de fichero₂, offset₂)      en orden lexicográfico
                      el id: el orden en que se cargaron los ficheros
                      el offset: el inicio de su etiqueta primaria
misma posición    ⇒   el orden en que llegaron a Report
```

**Una comprobación previa.** `syntax.CheckSource` rechaza los bytes que el lexer del capítulo 5 no ve, y solo informa del primero:

```pseudo
función CheckSource(src)
  fin ← |src|
  si src tiene UTF-8 inválido entonces
    malo ← el offset del primer byte inválido
    fin ← malo
  para cada byte b, en el offset i, de src[0 : fin]
    si (b < 0x20 y b ∉ {tabulador, \n, \r}) o b = 0x7F entonces
      informa E0102 en i              (un control ASCII)
      devuelve falso
  si src tiene UTF-8 inválido entonces
    informa E0101 en malo             (UTF-8 inválido)
    devuelve falso
  devuelve cierto
```

### Dos clases de columna

La posición cuenta code points, como rustc. El caret cuenta **columnas visuales**, con sustitutos de ancho conocido, y no sabe de caracteres anchos:

```math
col(o)       = 1 + |code points de la línea antes del offset o|        el tabulador vale 1

cols[0]      = 0
cols[i]      = columnas visuales antes del carácter que contiene el byte i
cols[sig(i)] = cols[i] + ancho(c, cols[i])            c: el carácter que empieza en i;
                                                     sig(i): el byte que sigue a c
ancho(c, w)  = 4 − (w mod 4)   si c es un tabulador: hasta el siguiente múltiplo de 4
             = 8               si c es un control C1 (U+0080–U+009F) o bidireccional:
                               se ve su código, como <U+202E>
             = 1               si no: un byte inválido se ve U+FFFD,
                               y un control ASCII, su control picture (␁)

caret de [lo, hi):   empieza tras cols[lo] columnas (más el «...» inicial de una ventana)
                     y mide max(cols[hi] − cols[lo], 1)
```

Un bidireccional se sustituye porque reordena el texto en pantalla (el ataque *Trojan Source*).

### Pintar un diagnóstico

El formato humano, parecido al de rustc, pone bajo cada línea sus marcas. Una línea larga se reduce a una ventana alrededor de sus etiquetas, y el diagnóstico no crece con el fichero.

```pseudo
función RenderHuman(d)
  ordena las etiquetas por línea; en una línea, la primaria primero y las demás por columna
  g ← las cifras del mayor número de línea mostrado
  escribe «<severidad>[<código>]: <mensaje>»
  escribe g espacios, «--> » y ruta:línea:columna de la primaria
  escribe g espacios y « |»
  para cada línea con etiquetas, en orden
    si hubo otra antes y no es la anterior + 1 entonces escribe g espacios y « ...»
    escribe el número, alineado a g cifras, « | » y el texto sin espacios finales
      si la línea pasa de 200 bytes: solo una ventana alrededor de sus etiquetas,
      con «...» en cada corte; cols y lo cuentan desde la ventana,
      y las marcas se desplazan 3 columnas si se cortó el principio
    para cada etiqueta de la línea
      escribe g espacios, « | », cols[lo] espacios, sus marcas y su mensaje
                                        (marcas: ^ la primaria, - las secundarias)
  para cada nota, escribe g espacios y « = note: <nota>»
  si hay ayuda, escribe g espacios y « = help: <ayuda>»
```

### La cabecera de un golden

Cada programa de `tests/`, el otro tipo de golden, dice en sus primeros comentarios cómo se ejecuta y qué debe pasar. Es estricta: una clave repetida o ausente es error; una mal escrita, texto libre que no comprueba nada.

```math
cabecera     ::= { línea que empieza por "//" }        la primera que no, la cierra
clave        ::= "// " nombre ": " valor               el ":" justo tras el nombre
nombre       ∈   { test, run, args, expect-exit, expect-stdout, expect-stderr }
                 cada uno una vez como mucho; test y run, obligatorios
                 cualquier otra «// palabra: …» es texto libre
test         ::= el id de la tarjeta
run          ∈   { check, run, emit tokens, emit ast, emit core }
args         ::= flags separados por espacios          van entre la orden y el fichero
expect-exit  ::= entero
salida       ::= "// expect-stdout: " texto            una línea: "// expect-stdout: 6765" es "6765\n"
               | "// expect-stdout:" { espacio | tabulador } ⏎ { "// | " texto ⏎ | "// |" ⏎ }
                 un bloque, igual con expect-stderr; "// |" es una línea vacía
anotación    ::= "// ERROR(" código ") @" col          en esa misma línea
               | "// ERROR(" código ") @" línea ":" col     línea, col ≥ 1
                 en cualquier línea que no sea una clave ni parte de un bloque
código       ::= (E | W) dígito dígito dígito dígito
```

### Qué compara el runner

Un error esperado, marcado en línea (`// ERROR(E0101) @9`), se compara por (código, línea, columna), no por su texto: reformular un mensaje no rompe cien golden. Lo que la cabecera calla toma un valor por omisión:

```math
A = las anotaciones del fichero
E = los diagnósticos (errores y warnings) de una segunda ejecución con --diagnostics=json

exit esperado = expect-exit     si la cabecera lo da
              = 1               si ∃ a ∈ A cuyo código empieza por E
              = 0               si no
stdout y stderr esperados = ""  si la cabecera no los da

pasa  ⇔  exit = exit esperado  ∧  stdout = stdout esperado  ∧  C
C     =  (stderr = stderr esperado)                                   si A = ∅
C     =  (A = E como multiconjuntos de (código, línea, columna))      si no:
         dos anotaciones iguales piden dos errores
```

## Preguntas de repaso

:::review
¿Por qué un error de un programa Ki no es un `error` de Go?
:::

:::answer
Porque hay muchos y no deben cortar la ejecución: el pase sigue, y el sink los reúne, ordena y corta en 100. Un valor con código, spans y notas se pinta de tres formas y se compara por código y posición. `error` queda para lo que falla fuera del programa.
:::

:::review
¿Por qué un type switch sobre una interfaz sellada lleva un `default` que hace panic?
:::

:::answer
Porque el sello tiene una grieta (un struct que incrusta la interfaz la cumple) y, sobre todo, porque el type switch no es exhaustivo: añadir un tipo y olvidar su `case` compila. El `default` convierte el olvido en un panic inmediato.
:::

:::review
¿Por qué devolver un puntero `nil` como `error` no es devolver `nil`?
:::

:::answer
Porque un valor de interfaz solo es `nil` si su tipo y su valor lo son. `return p` guarda el tipo del puntero: quien llama ve `err != nil` y trata como fallo lo que no lo es.
:::

:::review
¿Por qué la posición y el caret cuentan columnas distintas?
:::

:::answer
Sirven a dos usos. La posición la lee un editor, para el que un tabulador es un carácter. El caret va en la terminal, donde un tabulador ocupa hasta cuatro columnas y `<U+202E>`, ocho; `cols`, el de la Teoría, traduce cada offset de byte a su columna visual.
:::

:::review
¿Por qué `CheckSource` informa solo del primer problema?
:::

:::answer
Un fichero que no es texto (un binario renombrado a `.ki`) tiene miles de bytes malos, y cien errores iguales no dirían más que el primero. Por eso busca los controles solo antes del primer byte inválido: gana el problema que va primero en el fichero.
:::

:::review
¿Por qué, con anotaciones, el runner no compara el texto de stderr?
:::

:::answer
Porque el texto de un mensaje mejora con el tiempo, y lo que identifica a un error es qué es y dónde está: comparar texto rompería todos los golden de error con cada cambio de redacción. Para fijar el texto queda un golden sin anotaciones, como `invalid_utf8_render.ki`.
:::

## Resumen

- Un error del programa es un `Diagnostic`; el `Sink` los reúne, ordena y corta tras 100 errores.
- `iota` numera constantes, pero Go no cierra el conjunto.
- Un puntero `nil` en una interfaz no la hace `nil`.
- La interfaz sellada no es exhaustiva: su type switch acaba en `default: panic`.
- `(T, error)`, los punteros y el valor cero hacen de `Result` y `Option`.
- `CheckSource` rechaza lo que no es texto antes del lexer.
- La posición cuenta code points; el caret, columnas visuales.
- Los golden se regeneran con `-update` y después se leen.
- El runner exige una cabecera estricta y compara posiciones si hay anotaciones.

## Go en el trabajo

**Errores como valores.** La FAQ de Go: acoplar las excepciones a `try-catch-finally` «results in convoluted code» y empuja a tratar como excepcionales errores corrientes.

**Envolver.** El blog de Go: «wrapping an error makes that error part of your API». Envuelve para exponer la causa a quien llama; si es un detalle interno, usa `%v`.

**Golden en Go.** `src/cmd/gofmt/testdata/` guarda parejas `.input`/`.golden`, y `gofmt_test.go` declara el mismo flag.

## Tests primero

{{cards}}

Escribe el test de cada tarjeta, míralo fallar, implementa lo justo y solo entonces despliega la referencia.

:::note
**Fe de erratas.** En las tarjetas, tras 100 errores no para el front end: el sink descarta el resto; y la ventana de 82 lleva una etiqueta de dos bytes (de uno, 81).
:::

## Implementación

### Sesión 1: el modelo de diagnósticos (≈ 2 h)

#### Los tests, en rojo

1. **Escribe T03.01–T03.09.** Su interfaz, compacta:

```go
package diag

type Severity uint8
const ( Error Severity = iota; Warning; Note )
func (s Severity) String() string   // "error", "warning", "note"

type Code struct { Letter byte; Number uint16 }
func (c Code) String() string       // "E0101", "W0702"
func E(n uint16) Code
func W(n uint16) Code

type Label struct { Span base.Span; Message string }
type FixIt struct { Span base.Span; Replacement string }
type Diagnostic struct {
	Severity  Severity
	Code      Code
	Message   string
	Primary   Label
	Secondary []Label
	Notes     []string
	Help      string // empty if none
	Fixits    []FixIt
}
func NewError(code Code, message string, primary Label) Diagnostic
func (d Diagnostic) WithSecondary(l Label) Diagnostic
func (d Diagnostic) WithNote(n string) Diagnostic
func (d Diagnostic) WithHelp(h string) Diagnostic

const MaxErrors = 100
type Sink struct { /* unexported fields */ }
func (s *Sink) Report(d Diagnostic)
func (s *Sink) All() []Diagnostic
func (s *Sink) ErrorCount() int
func (s *Sink) HasErrors() bool
func (s *Sink) Count(code Code) int
func (s *Sink) Truncated() bool
func (s *Sink) SortByPosition()
```

2. **Míralos fallar:**

```console doctest="red"
$ go test ./internal/diag/
# github.com/aldezex/kilang/internal/diag [github.com/aldezex/kilang/internal/diag.test]
internal/diag/sink_test.go:12:14: undefined: Code
internal/diag/sink_test.go:12:61: undefined: Diagnostic
…
internal/diag/diagnostic_test.go:33:14: too many errors
FAIL	github.com/aldezex/kilang/internal/diag [build failed]
FAIL
```

#### Go: `iota`

Las gravedades de la Teoría:

```go
type Severity uint8

const (
	Error Severity = iota // 0
	Warning               // 1
	Note                  // 2
)
```

#### Go: una interfaz sellada

El patrón de la Teoría:

```go
package shape

import "fmt"

// Shape is sealed: only this package can implement it.
type Shape interface{ isShape() }

type Circle struct{ R float64 }
type Rect struct{ W, H float64 }

func (Circle) isShape() {}
func (Rect) isShape()   {}

// The crack: in another package, a struct that embeds Shape satisfies it.
//	type Sneaky struct{ shape.Shape }

func Area(s Shape) float64 {
	switch s := s.(type) {
	case Circle:
		return 3 * s.R * s.R
	case Rect:
		return s.W * s.H
	default:
		panic(fmt.Sprintf("unreachable: %T", s))
	}
}
```

El receptor de `func (Circle) isShape() {}` no tiene nombre porque no se usa. `%T` escribe el tipo concreto.

#### El código

{{file ch03-s1 internal/diag/diagnostic.go}}

{{file ch03-s1 internal/diag/sink.go}}

Los `With…` tienen receptor **por valor**: cambian una copia y la devuelven. La copia comparte el array de `Notes`; `slices.Clip` obliga a `append` a reservar uno propio (ejercicio 6). `Report` y `SortByPosition` son los de la Teoría.

`slices.SortStableFunc` conserva el orden de los iguales; su función devuelve negativo si `a` va antes, positivo si va después y cero si son iguales. Compara con `<` y `>`: restar dos `uint32` daría la vuelta.

Con pocos elementos, `slices.SortFunc` es estable por casualidad: usa *insertion sort* hasta 12, y el test ordena 90. Cambia `SortStableFunc` por `SortFunc`:

```text
--- FAIL: TestSink (0.00s)
    --- FAIL: TestSink/sort_by_position_is_stable (0.00s)
        sink_test.go:104: order of 90 diagnostics at 3 offsets differs from position then report order:
             got ["o0-10" "o0-29" "o0-00" "o0-28" "o0-27" "o0-01" …
            want ["o0-00" "o0-01" "o0-02" "o0-03" "o0-04" "o0-05" …
```

Deshazlo y compara:

{{reftest T03.03}}

{{reftest T03.06}}

`^uint32(0)`, el complemento de cero (el mayor `uint32`), detiene su bucle cuando `off--` da la vuelta.

{{reftest T03.08}}

Si te atascas o tu versión se desvía demasiado, copia el fichero de `ref/ch03-s1` y sigue.

Al terminar: tus tests en verde, commit y etiqueta `ch03-s1`; compárate con `ref/ch03-s1`.

### Sesión 2: los renderizadores human, short y json (≈ 2,5 h)

#### Los tests, en rojo

1. **Escribe T03.10–T03.27.** T03.12–T03.22 comparan con `testdata/`; T03.23 cuenta esos ficheros con `filepath.Glob` contra la constante `wantGoldens`. Describen tres funciones, cuyo resultado termina en `\n`:

```go
func RenderHuman(d Diagnostic, sm *base.SourceManager) string
func RenderShort(d Diagnostic, sm *base.SourceManager) string
func RenderJSON(d Diagnostic, sm *base.SourceManager) string
```

El formato humano es el de la Teoría; el corto y el JSON, los de T03.24 y T03.25. Dos ayudantes:

- **`checkGolden(t, name, got)`**, con `*update`, crea `testdata/` si falta (`os.MkdirAll`) y escribe `testdata/<name>.golden`. Sin él compara; si falta el golden, pide `-update`.
- **`spanOf(t, src, sub)`** da el span de `sub` (`strings.Index`) y falla si no está.

2. **Míralos fallar.** El primer rojo es `undefined: RenderHuman`. Con `render.go` escrito y sin golden, el segundo dice qué hacer:

```text
--- FAIL: TestRenderHuman (0.00s)
    --- FAIL: TestRenderHuman/secondary_same_line (0.00s)
        render_test.go:139: reading testdata/secondary_same_line.golden (run go test ./internal/diag -update to create it): open testdata/secondary_same_line.golden: no such file or directory
…
```

#### Go: etiquetas de struct

En `render.go`:

```go
type jsonDiagnostic struct {
	Severity string   `json:"severity"`
	Notes    []string `json:"notes"`
	Help     *string  `json:"help"`
	// …
}
```

#### Go: el flag `-update`

```go
var update = flag.Bool("update", false, "rewrite golden files")
```

`flag.Bool` registra el flag en el binario de test y devuelve un `*bool`.

#### El código

{{file ch03-s2 internal/diag/render.go}}

`RenderHuman` sigue el pseudocódigo de la Teoría; `strconv.Itoa` da cada número de línea como texto, y `lineWindow` elige la ventana.

**`displayLine`** devuelve el texto y los `cols` de la Teoría. Su `switch` separa primero un byte inválido de un U+FFFD escrito de verdad (capítulo 2). Un `case` con comas casa si casa cualquiera.

:::more Cómo agrupa las etiquetas `RenderHuman`
- `type placed struct {…}` se declara dentro de la función.
- Al ordenar sí se restan líneas y offsets, convertidos antes a `int`, de 64 bits en tu máquina: la diferencia no da la vuelta.
- El bucle externo no avanza `i` en su cabecera: `group` se queda con las etiquetas de la línea, e `i += len(group)` salta tras ellas.
:::

Cuando compile, genera los golden y **léelos** contra las tarjetas:

```console
$ go test ./internal/diag -update
ok  	github.com/aldezex/kilang/internal/diag	0.004s
```

Cambia `line != prev+1`, la condición del `...`, por `line > prev+2`:

```text
--- FAIL: TestRenderHuman (0.00s)
    --- FAIL: TestRenderHuman/two_lines_with_gap (0.00s)
        render_test.go:197: testdata/gap_of_one_line_with_wider_margin.golden differs
            --- got
            …
             9 | let a = 1
               |     - first defined here
            11 | let a = 2
               |     ^ redefined here
            --- want
            …
             9 | let a = 1
               |     - first defined here
               ...
            11 | let a = 2
               |     ^ redefined here
```

El golden que salta dos líneas no lo distingue; el que salta una, sí. Deshazlo y compara:

{{reftest T03.11}}

{{reftest T03.20}}

{{reftest T03.25}}

Si te atascas o tu versión se desvía demasiado, copia el fichero de `ref/ch03-s2` y sigue.

Al terminar: tus tests en verde, commit y etiqueta `ch03-s2`; compárate con `ref/ch03-s2`.

### Sesión 3: la comprobación previa al léxico, `ki check` y el runner golden (≈ 2,5 h)

#### Los tests, en rojo

1. **Escribe T03.28–T03.75** (ayudantes, en el pliegue tras el paso 4). `ParseHeader` lee la gramática de la Teoría. Su interfaz:

```go
// syntax
func CheckSource(sm *base.SourceManager, id base.FileID, sink *diag.Sink) bool

// driver
type DiagFormat uint8 // FormatHuman, FormatShort, FormatJSON
type Session struct {
	Stdin          io.Reader
	Stdout, Stderr io.Writer
	Sources        base.SourceManager
	Sink           diag.Sink
	Format         DiagFormat
}
func (s *Session) ReportAll()
var testExec func(args []string, stdout, stderr io.Writer) int // unexported; tests set it, for T03.66, T03.67 and T03.75

// kitest
type Annotation struct { Code string; Line, Col uint32 }
type Spec struct {
	TestID string; Run []string; Args []string
	ExpectExit *int; ExpectStdout, ExpectStderr *string
	Errors []Annotation
	BOM bool; CRLF bool
}
func ParseHeader(text []byte) (Spec, error)
type Runner struct {
	Root string
	Exec func(args []string, stdout, stderr io.Writer) int
	Out  io.Writer
}
func (r *Runner) RunFile(path string) (ok bool, report string)
func (r *Runner) RunPaths(paths []string) (passed, failed int)
func ExecSubprocess(exe string) func(args []string, stdout, stderr io.Writer) int
func unreadableDirectory(absRoot, p string, err error) string // unexported, for T03.72
```

2. **Ponlos en su sitio.** Van en `internal/syntax/source_check_test.go`, `internal/kitest/header_test.go` y `runner_test.go`, e `internal/driver/session_test.go` y `cli_test.go`. `runner_internal_test.go` va en `package kitest`, porque llama a `unreadableDirectory`; los demás de `syntax` y `kitest` son externos, porque solo usan lo exportado. T03.57–T03.60 son ficheros `.ki`.
3. **Cambia el `want` de T01.04** a `commands: --version, version, check, test`.
4. **Copia `func ptr[T any](v T) *T { return &v }`** en `header_test.go`. Es **genérica** (capítulo 4): `ptr(101)` da un `*int`. `reflect.DeepEqual(got, want)` compara dos `Spec` entrando en slices y punteros: un struct con slices no admite `==`.

:::more Los ayudantes de la sesión 3
- **El `Exec` falso** guarda sus llamadas en un slice recibido por puntero: `*calls = append(*calls, …)` cambia el slice de quien llama, y `new([]call)` crea uno vacío y da su dirección.
- **`runGoldensInProcess(t)`** apunta `testExec` a `Main` y la devuelve a `nil` con `t.Cleanup`.
- **`goldenDir(t)`** crea un directorio temporal con `pass.ki`, que pasa, y `fail.ki`, que falla.
- **`sameDir(t, a, b)`** dice si dos rutas son el mismo directorio: `os.SameFile` sobre los `FileInfo` de dos `os.Stat`.
:::

5. **Míralos fallar.** Los paquetes nuevos ni compilan:

```console doctest="red"
$ go test ./...
…
# github.com/aldezex/kilang/internal/kitest [github.com/aldezex/kilang/internal/kitest.test]
internal/kitest/runner_internal_test.go:27:14: undefined: unreadableDirectory
…
FAIL	github.com/aldezex/kilang/internal/kitest [build failed]
FAIL	github.com/aldezex/kilang/internal/syntax [build failed]
…
```

#### Go: aserción a una interfaz anónima

La de la Teoría, en `RunPaths`:

```go
if f, ok := out.(interface{ Flush() error }); ok {
	_ = f.Flush()
}
```

#### Go: el nil tipado

```go
func find(bad bool) error {
	var p *MyErr // MyErr implements error with a pointer receiver
	if bad {
		p = &MyErr{"bad"}
	}
	return p // never a nil error
}
```

`find(false) == nil` es `false`, aunque `fmt.Println` imprima `<nil>`.

#### La comprobación previa al léxico

{{file ch03-s3 internal/syntax/source_check.go}}

Es el pseudocódigo de la Teoría, línea a línea.

{{reftest T03.33}}

#### La sesión y `ki check`

{{diff ch03-s2..ch03-s3 internal/driver/session.go}}

{{diff ch03-s2..ch03-s3 internal/driver/commands.go}}

{{file ch03-s3 internal/driver/check.go}}

`--diagnostics=human|short|json` elige el formato.

:::more Cómo lee `ki check` su línea de órdenes
- `fs.String(…)` registra un flag de texto y devuelve un `*string`, que se lee con `*diagnostics` tras `Parse`.
- `parseFlags` devuelve `(code, done)`. Si `done`, ya hay respuesta (la ayuda o un error de uso) y el comando devuelve `code`.
- Los flags van antes del fichero, porque `flag` para en el primer argumento que no lo es; detrás, sale con 2.
- El `case 1:` vacío casa y no hace nada: un `case` de Go no cae al siguiente.
- Un fichero ilegible sale con 1: la línea de órdenes estaba bien.
:::

#### La cabecera

`header.go` usa:

| Función | Qué hace |
| --- | --- |
| `strings.Cut` | parte por la primera aparición de un separador; da antes, después y si lo encontró |
| `strings.CutPrefix` | quita un prefijo y dice si estaba |
| `strings.Fields` | parte por espacios |
| `strconv.Atoi` | lee un entero |
| `strconv.ParseUint(s, 10, 32)` | lee un entero y rechaza lo que no cabe en 32 bits |

{{file ch03-s3 internal/kitest/header.go}}

`seen`, un `map[string]bool`, da `false` para una clave ausente. Sin él, con dos `// run:` ganaría la segunda sin avisar.

{{reftest T03.36}}

#### El runner

`runner.go` y `golden_test.go` usan:

| Función | Qué hace |
| --- | --- |
| `strings.CutSuffix` | quita un sufijo y dice si estaba |
| `strings.SplitAfter` | parte y conserva el separador |
| `cmp.Compare` | da -1, 0 o 1 |
| `cmp.Or` | da el primero de sus argumentos que no es cero |
| `slices.ContainsFunc` | dice si algún elemento cumple una función |
| `slices.Values` | da un *iterador* sobre el slice |
| `slices.SortedFunc` | recoge un iterador en un slice nuevo, ordenado |
| `filepath.Rel`, `filepath.ToSlash` | dan la ruta relativa y con barras `/`, también en Windows |
| `filepath.Glob` | lista las rutas que casan con un patrón |

{{file ch03-s3 internal/kitest/runner.go}}

- **`RunPaths`** tiene resultados con nombre y un `defer` que los escribe. `return 0, 1` los asigna **antes** de que corran los `defer`: el resumen es correcto también al fallar.
- **`runHere`** aplica la comparación de la Teoría (`annotationDifferences`, la de multiconjuntos) y clona la orden antes de cada `append`, como en el capítulo 1.

:::more Cómo compara `annotationDifferences`
- `json.Unmarshal` lee cada línea en un struct anónimo. `Line` y `Col` no llevan etiqueta: `Line` recibe `"line"`, porque `json.Unmarshal` acepta una clave que solo difiera en mayúsculas. Ignora las que no tienen campo.
- `byPosition` ordena por línea, columna y código.
- Los dos lados se ordenan y se recorren juntos: `got` es suyo y se ordena en el sitio; `want` es del `Spec` de quien llama, así que `slices.SortedFunc` ordena una copia.
:::

Cambia el `defer restore()` de `RunPaths` por `_ = restore`. El proceso queda en un directorio que `t.TempDir` borra, y fallan una decena de subtests lejos de la causa:

```text
--- FAIL: TestRunner (0.00s)
    --- FAIL: TestRunner/exit_code_diff_first (0.00s)
        runner_test.go:144: exit code first, then stdout: got ok=false and "FAIL c.ki\n- cannot enter the root `/tmp/…/001`: getwd: no such file or directory\n", want …
```

Con `-run 'TestRunner/restores_cwd'`, ese subtest lo dice en una línea:

```text
runner_test.go:270: the working directory is "/tmp/…/001" after RunPaths, was "…/internal/kitest"
```

Deshazlo.

{{reftest T03.48}}

{{reftest T03.49}}

#### `ki test` y los dos modos

{{file ch03-s3 internal/driver/test.go}}

`ki test` ejecuta cada golden como subproceso de `os.Executable()`, que bajo `go test` es el binario de test: `testExec` deja a los tests ejecutar `Main` en proceso (ejercicio 7).

{{reftest T03.66}}

`tests/golden_test.go` corre el mismo `Runner` en proceso con otro `Exec`, su campo función. Cada fichero es un subtest, y su nombre calculado es la excepción a la regla del literal.

{{file ch03-s3 tests/golden_test.go}}

#### Los golden y la CI

```console
$ mkdir -p tests/errors/ch03
$ printf '// test: T03.57\n// run: check\nlet x = \xff  // ERROR(E0101) @9\n' > tests/errors/ch03/invalid_utf8.ki
$ printf '// test: T03.58\n// run: check\n// expect-exit: 1\n// expect-stderr:\n// | error[E0101]: invalid UTF-8 byte 0xFF\n// |   --> tests/errors/ch03/invalid_utf8_render.ki:10:9\n// |    |\n// | 10 | let x = \xef\xbf\xbd\n// |    |         ^ not valid UTF-8\nlet x = \xff\n' > tests/errors/ch03/invalid_utf8_render.ki
$ printf '// test: T03.59\n// run: check\nlet x = 1\x01  // ERROR(E0102) @10\n' > tests/errors/ch03/control_char.ki
```

{{file ch03-s3 tests/errors/ch03/invalid_utf8.ki}}

{{file ch03-s3 tests/errors/ch03/invalid_utf8_render.ki}}

{{file ch03-s3 tests/errors/ch03/control_char.ki}}

{{file ch03-s3 tests/errors/ch03/valid_file.ki}}

`valid_file.ki` es el caso «sí»: sin él, un `check` que siempre falla pasaría. `ci-local.sh` los ejecuta también por el binario:

{{diff ch03-s2..ch03-s3 ci/ci-local.sh}}

Si te atascas o tu versión se desvía demasiado, copia el fichero de `ref/ch03-s3` y sigue.

Al terminar: tus tests en verde, commit y etiqueta `ch03-s3`; compárate con `ref/ch03-s3`.

## Ejecútalo

Desde la raíz de tu repo, con `ki` construido (`go build -o build/bin/ki ./cmd/ki`):

```console
$ build/bin/ki check tests/errors/ch03/invalid_utf8_render.ki; echo $?
error[E0101]: invalid UTF-8 byte 0xFF
  --> tests/errors/ch03/invalid_utf8_render.ki:10:9
   |
10 | let x = �
   |         ^ not valid UTF-8
1
$ build/bin/ki check tests/errors/ch03/control_char.ki; echo $?
error[E0102]: control character U+0001 in source
 --> tests/errors/ch03/control_char.ki:3:10
  |
3 | let x = 1␁  // ERROR(E0102) @10
  |          ^ not allowed in Ki source; write it as an escape such as `\u{1}`
1
$ build/bin/ki check --diagnostics=short tests/errors/ch03/control_char.ki
tests/errors/ch03/control_char.ki:3:10: error[E0102]: control character U+0001 in source
$ build/bin/ki test --root . tests; echo $?
PASS tests/errors/ch03/control_char.ki
PASS tests/errors/ch03/invalid_utf8.ki
PASS tests/errors/ch03/invalid_utf8_render.ki
PASS tests/errors/ch03/valid_file.ki
ki test: 4 passed, 0 failed
0
```

`ci/ci-local.sh` acaba con esas líneas de `ki test` y `verify: OK`.

Con un tabulador, las dos columnas de la Teoría difieren: 10 para el byte malo, 13 para el caret:

```console
$ printf 'fn main() {\n\tlet x = \xff\n}\n' > build/bad.ki
$ build/bin/ki check build/bad.ki
error[E0101]: invalid UTF-8 byte 0xFF
 --> build/bad.ki:2:10
  |
2 |     let x = �
  |             ^ not valid UTF-8
```

## Ejercicios

:::exercise
**1. `ParseCode`.** Añade a `diagnostic.go` `func ParseCode(s string) (Code, error)`, la inversa de `Code.String`, con un error reconocible con `errors.Is`. Escribe sus tests, una fila por forma de equivocarse.
:::

:::solution
Un centinela envuelto con `%w` (`diagnostic.go` importa además `"errors"` y `"strconv"`):

```go
// ErrBadCode is the error ParseCode wraps when its text is not a code.
var ErrBadCode = errors.New("bad diagnostic code")

// ParseCode reads a code in the form String writes it: E or W and exactly
// four digits, as in "E0101".
func ParseCode(s string) (Code, error) {
	if len(s) != 5 || (s[0] != 'E' && s[0] != 'W') {
		return Code{}, fmt.Errorf("%w %q: want E or W and four digits", ErrBadCode, s)
	}
	n, err := strconv.ParseUint(s[1:], 10, 16)
	if err != nil {
		return Code{}, fmt.Errorf("%w %q: want E or W and four digits", ErrBadCode, s)
	}
	return Code{Letter: s[0], Number: uint16(n)}, nil
}
```

La longitud va primero: con `""`, `s[0]` haría panic, y `||` no lo evalúa. `ParseUint` rechaza el signo de `"E+101"`. En `internal/diag/parse_code_test.go`:

```go
func TestParseCode(t *testing.T) {
	t.Run("round_trips", func(t *testing.T) {
		for _, want := range []Code{E(101), W(702), E(0), E(9999)} {
			got, err := ParseCode(want.String())
			if err != nil || got != want {
				t.Errorf("ParseCode(%q) = %v, %v; want %v, nil", want.String(), got, err, want)
			}
		}
	})
	t.Run("rejects_malformed", func(t *testing.T) {
		rows := []struct{ name, text string }{
			{"too short", "E01"},
			{"too long", "E01011"},
			{"lower-case letter", "e0101"},
			{"other letter", "X0101"},
			{"sign", "E+101"},
			{"not a digit", "E01a1"},
			{"empty", ""},
		}
		for _, row := range rows {
			got, err := ParseCode(row.text)
			if !errors.Is(err, ErrBadCode) || got != (Code{}) {
				t.Errorf("%s: ParseCode(%q) = %v, %v; want the zero Code and ErrBadCode", row.name, row.text, got, err)
			}
		}
	})
}
```

`E(0)` y `E(9999)` son los bordes. Sin la comprobación de longitud, `"E01"` se lee como `E0001` y `"E01011"` como `E1011`.
:::

:::exercise
**2. Tres líneas a golden.** Añade a `TestRenderHuman` un subtest `labels_on_three_lines`: un E0301 en la línea 4 de un fichero de cinco, con secundarias en las líneas 1 y 2. Genera su golden y léelo. ¿Qué otra línea del test cambia?
:::

:::solution
```go
t.Run("labels_on_three_lines", func(t *testing.T) {
	// Lines 1 and 2 are next to each other, so no "..." between them;
	// line 3 is skipped, so one "..." before line 4.
	const src = "fn f(a Int) {\n  let b = a\n  let c = 1\n  b + c + a2\n}\n"
	sm, _ := newSource(src)
	d := unknownName("a2", spanOf(t, src, "a2"))
	d = d.WithSecondary(Label{Span: spanWithin(t, src, "  let b = a", "b"), Message: "`b` is defined here"})
	d = d.WithSecondary(Label{Span: spanOf(t, src, "a Int"), Message: "a parameter with a similar name"})
	checkGolden(t, "labels_on_three_lines", RenderHuman(d, sm))
})
```

Los ayudantes son los de `render_test.go` en `ref/ch03-s2`: `newSource` crea `test.ki`, `unknownName` el E0301, y `spanWithin(t, src, ctx, sub)` busca `sub` dentro de `ctx`, para un texto que se repite.

Primero falla por el golden que falta. Con `-update` se escribe, pero el recuento da rojo: `testdata has 15 golden files, want 14`. La constante `wantGoldens` del test que cuenta los golden pasa a 15. El golden:

```text
error[E0301]: unknown name `a2`
 --> test.ki:4:11
  |
1 | fn f(a Int) {
  |      ----- a parameter with a similar name
2 |   let b = a
  |       - `b` is defined here
  ...
4 |   b + c + a2
  |           ^^ not found in this scope
```

Las líneas en orden aunque la primaria esté abajo, sin `...` entre la 1 y la 2, uno antes de la 4, y cinco `-` bajo `a Int`.
:::

:::exercise
**3. Un golden del formato corto.** Cubre `ki check --diagnostics=short` con `tests/errors/ch03/short_format.ki` (`args: --diagnostics=short`). ¿Por qué no puede llevar una anotación `// ERROR(…)`?
:::

:::solution
Con una anotación, el runner no compara stderr como texto sino las posiciones de la pasada en JSON, y el formato corto quedaría sin comprobar. El fichero fija stderr y también `expect-exit: 1`, porque sin anotaciones el código esperado es 0:

```console
$ printf '// test: T03.76\n// run: check\n// args: --diagnostics=short\n// expect-exit: 1\n// expect-stderr: tests/errors/ch03/short_format.ki:6:9: error[E0101]: invalid UTF-8 byte 0xFF\nlet x = \xff\n' > tests/errors/ch03/short_format.ki
```

`ki test --root . tests` da `5 passed, 0 failed`. Con `6:8` en la expectativa falla con `- …:6:8: …` y `+ …:6:9: …`.
:::

:::exercise
**4. Sin límite.** Imagina un `CheckSource` que informara de cada byte inválido, sobre 100 000 bytes 0xFF en una línea. ¿Qué pasaría con el límite de 100 de `Report`? ¿Y sin él?
:::

:::solution
Cada diagnóstico humano muestra una ventana de la línea, no la línea entera, así que mide unos 350 bytes sea cual sea el fichero. Con el límite de 100, unos 34 KB de stderr. Sin límite, 38 MB en unos 20 s y unos 150 MB de memoria, porque `ReportAll` construye toda la salida en un `strings.Builder` antes de escribirla.

`MaxErrors` protege de cualquier pase que se desboque. `CheckSource` ni llega a él: en un fichero que no es texto basta el primer byte malo.
:::

:::exercise
**5. Una ruta que no existe.** `ki test --root . nope` responde `FAIL nope` y sale con 1. Haz que sea un error de uso, con un mensaje claro, la línea `usage:` y salida 2, sin ejecutar nada. Escribe el test.
:::

:::solution
En `runTest`, tras comprobar que hay rutas (`test.go` importa además `"errors"` y `"path/filepath"`):

```go
	for _, p := range fs.Args() {
		full := p
		if !filepath.IsAbs(p) {
			full = filepath.Join(*root, p)
		}
		if _, err := os.Stat(full); errors.Is(err, os.ErrNotExist) {
			return usageError(s, c, fmt.Errorf("PATH %q does not exist below %q", p, *root))
		}
	}
```

`os.ErrNotExist` es el mismo valor que `fs.ErrNotExist`, que aquí no se puede escribir: en `runTest`, `fs` es el `FlagSet`, una variable local, y el fichero no importa `io/fs`. El test, en `TestCLI`:

```go
t.Run("test_missing_path_is_usage_error", func(t *testing.T) {
	runGoldensInProcess(t)
	dir := goldenDir(t)
	want := "ki test: PATH \"nope\" does not exist below \"" + dir + "\"\nusage: ki test [--root DIR] PATH...\n"
	for _, paths := range [][]string{{"nope"}, {"pass.ki", "nope"}} {
		code, out, err := runCLI(t, append([]string{"test", "--root", dir}, paths...)...)
		if code != 2 || out != "" || err != want {
			t.Errorf("%q: got %d %q %q, want 2 \"\" %q", paths, code, out, err, want)
		}
	}
})
```

La segunda fila exige que no se ejecute nada aunque la ruta mala sea la última, y `runGoldensInProcess` evita que, sin la comprobación, `pass.ki` lance el binario de test. Sin el `if`, las dos filas fallan con código 1 y `FAIL nope`.
:::

:::exercise
**6. Sin `slices.Clip`.** Quítalo de `WithNote` y explica el fallo.
:::

:::solution
```text
--- FAIL: TestDiagnostic (0.00s)
    --- FAIL: TestDiagnostic/with_methods_return_copies (0.00s)
        diagnostic_test.go:95: Notes of two copies = ["old" "b"] and ["old" "b"], want [old a] and [old b]
```

Deshazlo después. La copia es superficial (capítulo 2): `Notes` comparte el array con el original, y con capacidad de sobra `append` escribe en él. Dos copias del mismo diagnóstico se pisan: la segunda nota sobrescribe la primera. `slices.Clip` deja la capacidad igual a la longitud y obliga a `append` a reservar un array propio.
:::

:::exercise
**7. Un subproceso.** Explica por qué `ki test` lanza uno por golden.
:::

:::solution
Para probar lo que vería una shell: el binario real y su código de salida. Pero bajo `go test`, `os.Executable()` es el binario de **test**, no `ki`, así que los tests de `ki test` cambian `testExec` para ejecutar `Main` en proceso.
:::
