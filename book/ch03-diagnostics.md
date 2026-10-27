---
chapter: 3
title: Tipos suma sin tipos suma: interfaces selladas y errores como valores
part: I
hours: 7
milestone: `ki check` muestra un diagnóstico con caret
---

## Objetivo

Al terminar, `ki` tiene sus dos primeros comandos de verdad. `ki check` lee un fichero y señala lo que está mal con un caret:

```console
$ build/bin/ki check tests/errors/ch03/invalid_utf8_render.ki; echo $?
error[E0101]: invalid UTF-8 byte 0xFF
  --> tests/errors/ch03/invalid_utf8_render.ki:10:9
   |
10 | let x = �
   |         ^ not valid UTF-8
1
```

Y `ki test` ejecuta los programas de prueba y compara lo que imprimen con lo que esperan sus cabeceras. Aún no hay lexer, así que el único error posible es un fichero que no es texto válido, pero el modelo de diagnósticos, sus tres formatos y el runner son los definitivos, camino del hito M1 (capítulo 10).

## Teoría

### Un error de tu programa es un dato

Un error del programa se muestra de tres formas: una persona quiere el fragmento con el caret; un editor, `fichero:línea:columna: mensaje`; el runner de tests, JSON. Por eso, en Ki, es un **valor**, un `diag.Diagnostic`, que el pase entrega a un `diag.Sink`, y otra pieza decide cómo se muestra. El `error` de Go se reserva para lo que falla fuera del programa (un fichero ilegible), y `panic` para un fallo del propio compilador.

Gravedad (error, warning o nota) y formato son tipos suma, «uno de estos tres», que Rust escribiría con un `enum`. Go no los tiene, y el título dice cómo se compensa: constantes con `iota` para los conjuntos cerrados de valores, interfaces selladas para los de tipos, y `(T, error)` en lugar de `Result`.

### Tipos definidos, `iota` y `String()`

```go
type Severity uint8

const (
	Error Severity = iota // 0
	Warning               // 1
	Note                  // 2
)
```

En un bloque `const`, `iota` vale 0 en la primera línea y aumenta en uno con cada línea, y una línea sin expresión repite la anterior. Como `Severity` es un tipo propio (capítulo 2), no se mezcla con otro de la misma forma: con `type DiagFormat uint8` y `const FormatShort DiagFormat = 1`, `var s Severity = FormatShort` da `cannot use FormatShort (constant 1 of uint8 type DiagFormat) as Severity value in variable declaration`.

Lo que Go **no** garantiza es que el conjunto esté cerrado: `Severity(7)` compila, y un `switch` que no cubre un caso no avisa. Por eso `String()` termina, tras su `switch`, con `return fmt.Sprintf("Severity(%d)", uint8(s))`: un valor que solo puede salir de un fallo se ve como tal.

:::from Rust
`iota` da los valores de un `enum` sin datos, pero sin su garantía: en Rust no existe un `Severity` fuera de sus variantes y `match` exige cubrirlas todas. En Go es un `uint8` con nombre, y la exhaustividad es asunto tuyo. `String()` hace de `impl Display`.
:::

:::from Python
Se parece a `enum.auto()`, pero `Severity(7)` en Python da `ValueError: 7 is not a valid Severity`; en Go es un valor más.
:::

### `fmt`: verbos, `Stringer` y `Formatter`

`fmt.Stringer` es la interfaz `String() string`, y `fmt` la usa con los verbos de texto: `%v`, `%s`, `%q`, `%x`. `Code` guarda una letra y un número, y su `String()` es `fmt.Sprintf("%c%04d", c.Letter, c.Number)` (`%c`, un carácter; `%04d`, cuatro cifras rellenas con ceros), así que `%v` de `E(101)` da `E0101`. Los verbos numéricos no la usan: `%d` de `Warning` da `1`, y de un `Code`, `{69 101}`.

`%+v` añade los nombres de los campos solo a un struct sin `String()`: sobre `Code` da `E0101`. `%#v` escribe la sintaxis de Go y nunca llama a `String()` (su interfaz es `GoStringer`): `diag.Code{Letter:0x45, Number:0x65}`, y para `Warning`, `0x1`.

`fmt.Formatter`, `Format(f fmt.State, verb rune)`, es la interfaz general: un tipo que la cumple decide la salida de **todos** los verbos salvo `%T` y `%p`, también `%#v`, y su `String()` deja de usarse. `%02X` y `%x` escriben hexadecimal, con dos cifras en mayúsculas (`FF`) o en minúsculas sin relleno (`1f`).

:::from Rust
`%v` con `String()` es `{}` con `Display`, y `%#v` se parece a `{:?}` con `#[derive(Debug)]`, que da `Code { letter: 69, number: 101 }`.
:::

### Interfaces y satisfacción implícita

Una interfaz es un conjunto de métodos, y un tipo la cumple si los tiene, sin declararlo: es lo que el capítulo 1 hizo con `io.Writer`. El compilador lo comprueba donde asignas el valor, y dice qué falta: `U does not implement io.Writer (missing method Write)`, o `(method Write has pointer receiver)` si el método es de `*T` y pasas un `T`.

Un valor de interfaz guarda dos cosas: el tipo concreto y el valor. La **aserción de tipo** `w.(T)` pregunta por el tipo: con un resultado, hace panic si no coincide; con dos, `v, ok := w.(T)` da `ok == false`. `T` puede ser una interfaz escrita en el sitio, y así pregunta el runner si su salida sabe vaciarse:

```go
if f, ok := out.(interface{ Flush() error }); ok {
	_ = f.Flush()
}
```

`interface{ Flush() error }` es un tipo interfaz sin nombre. Un `*os.File` no tiene `Flush`, así que con `os.Stdout` el `ok` es falso. El **type switch**, `switch v := x.(type) { case A: … }`, hace varias aserciones a la vez, y en cada `case` `v` tiene ese tipo.

:::from Rust
Una interfaz es un trait sin `impl Trait for T`: basta con los métodos, y un método con la firma equivocada se descubre en el sitio de uso. Un valor de interfaz es un `dyn Trait` (los genéricos llegan en el capítulo 4), y `w.(T)` hace de `downcast_ref::<T>()` sobre un `dyn Any`: en Rust, un `dyn Trait` cualquiera no lo tiene.
:::

### La interfaz sellada

Para un conjunto cerrado de **tipos**, Go usa una interfaz con un método sin exportar:

```go
package shape

import "fmt"

// Shape is sealed: only this package can implement it.
type Shape interface{ isShape() }

type Circle struct{ R float64 }
type Rect struct{ W, H float64 }

func (Circle) isShape() {}
func (Rect) isShape()   {}

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

Fuera de `shape`, `isShape` no se puede nombrar: un `Tri` de otro paquete con ese método da `Tri does not implement shape.Shape (unexported method isShape)`. (`func (Circle) isShape() {}` tiene un receptor sin nombre, porque no lo usa; `%T` escribe el tipo dinámico.)

El sello tiene una grieta: un struct de otro paquete que **incrusta** la interfaz, `type Sneaky struct{ shape.Shape }`, la cumple. Incrustar es declarar un campo sin nombre, cuyo tipo da nombre al campo y cuyos métodos se **promocionan** al struct; llamar a uno con el campo a `nil` hace panic. Y el type switch no es exhaustivo: si añades `Tri` a `shape` y olvidas su `case`, compila. Por las dos razones, todo type switch sobre una interfaz sellada del libro termina en `default: panic("unreachable")`: `Area(outside.Sneaky{})` hace panic con `unreachable: outside.Sneaky` en lugar de inventar un área.

:::from Rust
Es `enum Shape { Circle(f64), Rect(f64, f64), Tri }` sin la red: en Rust, un `match` sin `Tri` da ``error[E0004]: non-exhaustive patterns: `&Shape::Tri` not covered``; en Go lo descubre el `default` al ejecutarse. Mira la forma de ese error: la flecha `-->`, la barra y el caret son los que imita el renderizador de Ki.
:::

:::from TS
Una unión discriminada con `const never: never = s` en el `default` sí falla al compilar si añades una variante (`Type '{ kind: "tri"; }' is not assignable to type 'never'`). Go no tiene equivalente.
:::

### La trampa del nil tipado

Un valor de interfaz solo es `nil` si sus dos mitades lo son. Un puntero `nil` guardado en una interfaz no lo es:

```go
func find(bad bool) error {
	var p *MyErr // MyErr implements error with a pointer receiver
	if bad {
		p = &MyErr{"bad"}
	}
	return p // never a nil error
}
```

`find(false) == nil` es `false`, aunque `fmt.Println(find(false))` imprima `<nil>`: el `error` lleva el tipo `*MyErr` y un puntero nulo (la FAQ de Go lo explica en «Why is my nil error value not equal to nil?»). La regla: cuando no hay error, devuelve el literal `nil`, nunca una variable de un tipo concreto.

:::from Rust
`Option<Box<dyn Error>>` no tiene esta trampa: un `Box` nunca es nulo, así que hay `None` o un error de verdad. Una interfaz de Go es el par (tipo, valor), y su `nil` exige las dos mitades vacías.
:::

### `errors.Join` y lo que ya sabes

El capítulo 2 enseñó `%w`, `errors.Is` y `errors.As`. Este capítulo usa `errors.As` al revés de envolver: la función `reason` del runner quita el `*fs.PathError` de un error antes de mostrarlo, porque repite una ruta absoluta.

`errors.Join` reúne varios errores: `errors.Join(fs.ErrNotExist, nil, fs.ErrPermission)` tiene el texto `"file does not exist\npermission denied"` (los `nil` se descartan), y `errors.Is` encuentra los dos, también a través de otro `%w`. `errors.Join(nil, nil)` es `nil`: acumular en un bucle y devolver el `Join` da `nil` si no hubo ninguno. `fmt.Errorf` admite también varios `%w`. `errors.AsType`, la versión genérica de `errors.As`, espera al capítulo 4.

:::from Python
`errors.Join` recuerda a `ExceptionGroup`, que reúne varias excepciones y deja que `except*` atrape cada clase. Aquí no hay nada que atrapar: el grupo es un valor más que se devuelve.
:::

### Sin `Option` ni `Result`

Lo que falla devuelve `(T, error)`. Lo opcional se escribe con **el valor cero**, cuando cero no es válido (`Runner.Root == ""` es «el directorio de trabajo», y `var s Sink` ya es un sink vacío), o con **un puntero**, cuando sí lo es: en un golden, `expect-exit: 0` y no decir nada son cosas distintas, así que `Spec.ExpectExit` es un `*int` y `nil` es «no lo dice».

:::from Rust
`*int` hace de `Option<i32>`, sin `match` que obligue a mirar: desreferenciar un `nil` es un panic (`invalid memory address or nil pointer dereference`). `(T, error)` es un `Result` sin `?` y que nadie te obliga a comprobar.
:::

### JSON con etiquetas de struct

`encoding/json` convierte un struct en un objeto con sus campos exportados, en el orden en que se declaran. Una **etiqueta de struct** es una cadena tras el tipo del campo, que el paquete lee en ejecución; `json:"severity"` fija la clave:

```go
type jsonDiagnostic struct {
	Severity string   `json:"severity"`
	Notes    []string `json:"notes"`
	Help     *string  `json:"help"`
	// …
}
```

Tres reglas de `json.Marshal` dan forma a la salida de Ki: un slice `nil` sale `null`, pero uno vacío (`[]string{}`), `[]`; un puntero `nil`, `null`; y escapa `"` como `\"`, y `<`, `>` y `&` como `\u003c`, `\u003e` y `\u0026`. En sentido contrario, `json.Unmarshal` acepta una clave que solo difiera en mayúsculas (un campo `Line` sin etiqueta recibe `"line"`) e ignora las que no tienen campo.

:::from Rust
`#[serde(rename = "severity")]` lo lee la macro `derive` al compilar, y una errata (`renam`) no compila: `unknown serde field attribute`. La etiqueta de Go es una cadena que nadie mira hasta ejecutar: con `jsn:"severity"`, la clave sale `"Severity"`, el nombre del campo, sin aviso, y `go vet` solo rechaza una etiqueta mal formada (`json:severity`, sin comillas).
:::

### `strings.Builder`, `filepath.WalkDir` y el directorio de trabajo

- **`strings.Builder`** acumula texto con `WriteString` y `WriteRune` (un code point en UTF-8) y lo da con `String()`; su valor cero sirve.
- **`filepath.WalkDir(root, fn)`** llama a `fn(path, d, err)` por cada entrada del árbol: `d` es un `fs.DirEntry` (`d.IsDir()`), y `err` no es `nil` si un directorio no se pudo leer. Visita cada directorio en orden léxico, que no es el de las rutas completas: visita `a/y.ki` antes que `a-b/x.ki`, pero como cadenas `a-b/x.ki` va primero (el `-` es 0x2D y la `/`, 0x2F). Por eso el runner ordena lo que encuentra.
- **`os.Getwd` y `os.Chdir`** leen y cambian el directorio de trabajo, que es **del proceso entero**: el runner entra en su raíz para mostrar rutas relativas y tiene que volver. (En un test, Go 1.27 tiene `t.Chdir`, que lo restaura solo.)

:::from Python
`os.chdir` también es del proceso entero, y `t.Chdir` hace de `monkeypatch.chdir` de pytest, que lo deshace al acabar el test. Como afecta a todo el proceso, `t.Chdir` no se puede usar en un test paralelo.
:::

### UTF-8 y los ficheros golden

`utf8.Valid(b)` responde si `b` es UTF-8 válido, y nada más. Un diagnóstico necesita el **offset** del primer byte malo para su span, y por eso el capítulo 2 escribió `FirstInvalidUTF8`. Los controles ASCII son UTF-8 válido, pero cada uno es un byte menor que 0x80, que nunca aparece dentro de una secuencia de varios bytes: un bucle sobre los bytes los encuentra.

Un test **golden** compara una salida larga con la guardada en un fichero de `testdata/`, un directorio que el comando `go` ignora (no es un paquete ni entra en `./...`). Escribirlos a mano es lento, así que el test los genera con un flag propio:

```go
var update = flag.Bool("update", false, "rewrite golden files")
```

`flag.Bool` registra el flag en el binario de test y devuelve un `*bool`. Va **después** del paquete, `go test ./internal/diag -update`: según `go help testflag`, la lista de paquetes va antes de cualquier flag que `go test` no conozca. Y `go test ./... -update` no sirve: cada paquete sin el flag responde `flag provided but not defined: -update`. `-update` escribe lo que el código produce *ahora*, bueno o malo: después lees cada fichero, y revisas el diff antes del commit.

:::from TS
Es el *snapshot testing* del runner de Node: `t.assert.snapshot(valor)` compara con un fichero `.snapshot`, y `node --test --test-update-snapshots` lo reescribe. Aquí el flag lo declaras tú, y el formato del fichero también.
:::

### Compiladores: diagnósticos como datos

- **Códigos estables.** Cada clase de diagnóstico tiene un código que no se reutiliza nunca, el que buscas en la documentación y comparan los tests. La centena dice la etapa: `E01xx` el lexer (y la comprobación previa), `E02xx` el parser, `E03xx` los nombres, `E04xx` los tipos.
- **Un límite.** El front end se detiene tras 100 errores (`diag.MaxErrors`): lo que sigue suele ser una cascada del primero.
- **Una comprobación previa al léxico.** `syntax.CheckSource` rechaza UTF-8 inválido (E0101) y los controles que no son tabulador, `\n` o `\r` (E0102), solo el primero; así el lexer del capítulo 5 recibe texto limpio.
- **Un runner con cabecera.** Cada programa de `tests/` dice en sus primeros comentarios cómo se ejecuta y qué debe pasar, con la gramática estricta de abajo: una cabecera mal escrita e ignorada daría un test que no comprueba nada. Si el fichero marca sus errores en línea (`// ERROR(E0101) @9`), se comparan los pares (código, línea, columna) y no el texto: reformular un mensaje no rompe cien golden.

**La gramática de la cabecera.** La cabecera son las líneas iniciales que empiezan por `//`; la primera que no, la cierra.

- Una clave se escribe `// clave: valor`, con un espacio tras `//` y otro tras `:` (`// run:check` es un error). Hay seis, y cada una aparece una vez como mucho: `test` (el id de la tarjeta) y `run`, obligatorias; `args`, `expect-exit`, `expect-stdout` y `expect-stderr`, opcionales. Otra palabra seguida de `:` es texto libre.
- `run` vale `check`, `run`, `emit tokens`, `emit ast` o `emit core`; `args` son flags separados por espacios, que van entre la orden y el fichero; `expect-exit`, un entero.
- `expect-stdout` y `expect-stderr` dan la salida entera en una línea (`// expect-stdout: 6765` es `"6765\n"`) o, sin nada tras `:`, en un bloque: `// | texto` por línea y `// |` para una vacía.
- Lo que no se dice tiene valor por omisión: stdout y stderr vacíos, y el código de salida 1 si alguna anotación es un error y 0 si no.
- Una anotación, en cualquier línea fuera de un bloque, es `// ERROR(código) @COL` (en esa línea) o `@LINE:COL`, con un código de `E` o `W` y cuatro cifras.

**Dos clases de columna.** La posición cuenta code points, como en el capítulo 2, que es lo que entiende un editor; el caret tiene que caer bajo el carácter en pantalla, y cuenta **columnas visuales**, con el tabulador hasta el siguiente múltiplo de 4. Con un tabulador y `let x = ` delante, el byte malo está en la columna 10 y el caret, en la 13:

```console
$ printf 'fn main() {\n\tlet x = \xff\n}\n' > build/bad.ki
$ build/bin/ki check build/bad.ki
error[E0101]: invalid UTF-8 byte 0xFF
 --> build/bad.ki:2:10
  |
2 |     let x = �
  |             ^ not valid UTF-8
```

Lo invisible se muestra con un sustituto de ancho conocido: un byte inválido, U+FFFD; un control ASCII, su imagen de control (*control picture*) del bloque U+2400 (`␁`, `␍`, `␡`); un control C1 o bidireccional, su código, `<U+202E>`. Los bidireccionales reordenan el texto en pantalla (el ataque *Trojan Source*).

## Tests primero

Las tarjetas del capítulo, con valores exactos:

{{cards}}

Úsalas como en los capítulos anteriores: escribe el test de la tarjeta, míralo fallar (rojo), implementa lo justo para que pase (verde), y solo entonces despliega el test de referencia con su `{{reftest}}`. Las cuatro tarjetas `diag` (T03.57–T03.60) son ficheros `.ki` con su cabecera, no tests de Go.

Los tests de `internal/diag` e `internal/driver` están en el paquete que prueban; los de `internal/syntax` e `internal/kitest`, en `syntax_test` y `kitest_test`, porque solo usan lo exportado. `tests/golden_test.go` es el único fichero de su paquete: uno con solo ficheros `_test.go` es válido para `go test`.

## Implementación

### Sesión 1: el modelo de diagnósticos (≈ 2 h)

#### Los tests, en rojo

T03.01–T03.09 describen `internal/diag`. Su interfaz, en forma compacta: Go acepta `;` explícitos entre declaraciones, y `gofmt` los pasa a líneas separadas.

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

En la referencia, `^uint32(0)` (el complemento de cero, el mayor `uint32`) detiene el bucle `for off := uint32(2); off != ^uint32(0); off--` cuando `off--` da la vuelta por debajo de cero. El rojo:

```console
$ go test ./internal/diag/
# github.com/aldezex/kilang/internal/diag [github.com/aldezex/kilang/internal/diag.test]
internal/diag/sink_test.go:12:14: undefined: Code
internal/diag/sink_test.go:12:61: undefined: Diagnostic
…
internal/diag/diagnostic_test.go:33:14: too many errors
FAIL	github.com/aldezex/kilang/internal/diag [build failed]
FAIL
```

#### El código

{{file ch03-s1 internal/diag/diagnostic.go}}

- `Code` tiene dos campos comparables, así que dos códigos se comparan con `==`.
- Los `With…` tienen receptor **por valor**: cambian una copia y la devuelven (`d = d.WithNote("…")`), y un diagnóstico guardado en otro sitio no cambia.
- La copia es superficial (capítulo 2): `Notes` comparte el array del original, y si sobra capacidad, `append` escribiría en él. `slices.Clip` obliga a reservar uno nuevo.

Quita el `slices.Clip` de `WithNote` y T03.03 ve el aliasing:

```text
--- FAIL: TestDiagnostic (0.00s)
    --- FAIL: TestDiagnostic/with_methods_return_copies (0.00s)
        diagnostic_test.go:90: Notes of two copies = ["old" "b"] and ["old" "b"], want [old a] and [old b]
```

Deshaz el cambio.

{{file ch03-s1 internal/diag/sink.go}}

- Los campos del `Sink` no se exportan, y su valor cero es un sink vacío: no necesita constructor.
- `Report` guarda los 100 primeros errores, descarta el 101 y marca `truncated`; desde ahí lo descarta todo, también los warnings.
- `slices.SortStableFunc` ordena con una función que devuelve un negativo si `a` va antes, un positivo si va después y cero si son iguales, y conserva el orden de entrada de los iguales: dos errores en el mismo offset salen siempre en el orden en que se informaron. Las comparaciones van con `<` y `>`: restar dos `uint32` daría la vuelta en lugar de un negativo.

¿Por qué T03.06 ordena 90 diagnósticos? Porque con pocos, la ordenación no estable es estable por casualidad: `slices.SortFunc` usa *insertion sort* hasta 12 elementos (`maxInsertion = 12` en `slices/zsortanyfunc.go`, Go 1.27). Cambia `SortStableFunc` por `SortFunc`:

```text
--- FAIL: TestSink (0.00s)
    --- FAIL: TestSink/sort_by_position_is_stable (0.00s)
        sink_test.go:104: order of 90 diagnostics at 3 offsets differs from position then report order:
             got ["o0-10" "o0-29" "o0-00" "o0-28" "o0-27" "o0-01" …
            want ["o0-00" "o0-01" "o0-02" "o0-03" "o0-04" "o0-05" …
```

Y si `Report` cortara en `MaxErrors-1`, fallan los tres subtests del máximo: T03.07 y T03.08 miran a los dos lados del borde. Deshaz los cambios y compara:

{{reftest T03.03}}

{{reftest T03.06}}

{{reftest T03.08}}

El mensaje de la etiqueta: `ch03-s1: diagnostics as values`.

Si te atascas o tu versión se desvía demasiado, copia el fichero de `ref/ch03-s1` y sigue.

Al terminar: tus tests en verde, commit y etiqueta `ch03-s1`; compárate con `ref/ch03-s1`.

### Sesión 2: los renderizadores human, short y json (≈ 2,5 h)

#### Los tests, en rojo

T03.10–T03.27 describen tres funciones, cuyo resultado termina en `\n`:

```go
func RenderHuman(d Diagnostic, sm *base.SourceManager) string
func RenderShort(d Diagnostic, sm *base.SourceManager) string
func RenderJSON(d Diagnostic, sm *base.SourceManager) string
```

El formato humano, con `g` las cifras del mayor número de línea mostrado: la cabecera; `" "*g + "--> " + path:line:col` y `" "*g + " |"`; cada línea con etiquetas como `número | texto`, sin espacios finales; `" "*g + " ..."` entre dos líneas no consecutivas; bajo cada línea, la marca `^` de la primaria y las `-` de las secundarias por columna; y las notas y la ayuda (`= note:`, `= help:`). El corto y el JSON son los de T03.24 y T03.25.

En `render_test.go`, T03.10 y T03.11 comparan con el texto del test; el resto, con `testdata/`, a través de `var update = flag.Bool(…)`, de `checkGolden(t, name, got)`, que con `*update` escribe `testdata/<name>.golden` y sin él compara, y de `spanOf(t, src, sub)`, el span de `sub` (`strings.Index`), que falla si no está. Los textos con controles usan escapes: `"\x01"` es un byte y `"\u2400"`, un code point. T03.23 cuenta los golden con `filepath.Glob` contra la constante `wantGoldens`, para que borrar uno no haga que se compruebe menos.

El primer rojo es `undefined: RenderHuman`. Con `render.go` escrito y sin golden, el segundo dice qué hacer:

```text
--- FAIL: TestRenderHuman (0.00s)
    --- FAIL: TestRenderHuman/secondary_same_line (0.00s)
        render_test.go:137: reading testdata/secondary_same_line.golden (run go test ./internal/diag -update to create it): open testdata/secondary_same_line.golden: no such file or directory
…
```

#### El código

{{file ch03-s2 internal/diag/render.go}}

`RenderHuman`, por partes:

- **Las etiquetas.** `type placed struct {…}` es un tipo declarado dentro de la función. Se ordenan por línea y, en cada una, la primaria primero y las secundarias por columna.
- **La comparación.** Aquí sí se restan líneas y offsets, pero convertidos antes a `int`, de 64 bits en tu máquina: la diferencia de dos `uint32` cabe y no da la vuelta.
- **El margen.** Tras ordenar, `g` son las cifras de la línea de la última etiqueta (`strconv.Itoa`).
- **Las líneas.** El bucle externo, `for i := 0; i < len(all); {`, no avanza `i`: lo hace el interno, `for ; i < len(all) && all[i].line == line; i++`, que consume las etiquetas de la línea. `prev` decide el `...`.
- **El recorte.** Un span que sigue en otras líneas (un `{` sin cerrar, T03.22) se corta al final de la primera con `min` y `max`, y una marca ocupa al menos una columna, para que se vea un span vacío.

`displayLine` es la pieza delicada. Recorre la línea con `utf8.DecodeRuneInString` y devuelve el texto que se imprime y `cols`, con una entrada por **byte** más una: `cols[i]` es la columna visual donde empieza el byte `i`. Como los spans cuentan bytes, el caret de `[lo, hi)` empieza en `cols[lo]` y mide `cols[hi] - cols[lo]`. En el `switch`, el orden importa: el primer caso separa un byte inválido de un U+FFFD escrito de verdad (capítulo 2); un tabulador mide `tabWidth - width%tabWidth`; `0x2400 + r` es aritmética de code points, porque el bloque de imágenes sigue el orden de ASCII. Un `case` con varias expresiones separadas por comas casa si casa cualquiera. `strings.TrimRight` quita los espacios finales del texto, pero `cols` los sigue contando.

`RenderJSON` usa los structs con etiquetas de la teoría: `Notes` empieza como `[]string{}`, así que sale `[]` aunque no haya notas, y `Help` solo apunta a algo si hay ayuda.

Cuando compile, genera los golden y **léelos** contra las tarjetas:

```console
$ go test ./internal/diag -update
ok  	github.com/aldezex/kilang/internal/diag	0.004s
```

Desde aquí, cada golden protege lo que validaste. Cambia la condición del `...`, `line != prev+1`, por `line > prev+2`:

```text
--- FAIL: TestRenderHuman (0.00s)
    --- FAIL: TestRenderHuman/two_lines_with_gap (0.00s)
        render_test.go:156: testdata/gap_of_one_line_with_wider_margin.golden differs
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

`two_lines_with_gap.golden` salta dos líneas y no lo distingue; con la 9 y la 11 se salta una. Deshaz el cambio y compara:

{{reftest T03.11}}

{{reftest T03.20}}

{{reftest T03.25}}

Una cadena *raw* no admite `` ` ``: el JSON esperado concatena una normal, `` ` + "`z`" + ` ``.

El mensaje de la etiqueta: `ch03-s2: human, short and json renderers`.

Si te atascas o tu versión se desvía demasiado, copia el fichero de `ref/ch03-s2` y sigue.

Al terminar: tus tests en verde, commit y etiqueta `ch03-s2`; compárate con `ref/ch03-s2`.

### Sesión 3: la comprobación previa al léxico, `ki check` y el runner golden (≈ 2,5 h)

#### Los tests, en rojo

T03.28–T03.67 cubren cuatro piezas. Su interfaz:

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
```

Los tests van en `TestCheckSource` (`internal/syntax/source_check_test.go`), `TestParseHeader` y `TestRunner` (`internal/kitest/`), `TestReportAll` (`internal/driver/session_test.go`) y subtests nuevos de `TestCLI`. Piezas nuevas en sus ayudantes:

- `func ptr[T any](v T) *T { return &v }`, en `header_test.go`, es una función **genérica**: `ptr(101)` da un `*int` y `ptr("6765\n")` un `*string`, para escribir los campos opcionales de un `Spec` en un literal. Cópiala tal cual; el capítulo 4 explica `[T any]`.
- `reflect.DeepEqual(got, want)` compara dos `Spec` entrando en slices y punteros; `==` no sirve, porque un struct con slices no es comparable.
- El `Exec` falso de `runner_test.go` guarda sus llamadas en un slice recibido por puntero: `*calls = append(*calls, …)` cambia el slice de quien llama, `(*calls)[0]` lee el primero, y `new([]call)` crea uno vacío y da su dirección.
- `t.Skip` salta un test sin sentido en esa máquina (`runtime.GOOS == "windows"`), y `os.SameFile`, sobre los `FileInfo` de dos `os.Stat`, dice si dos rutas son el mismo directorio.

Sin el código, los paquetes nuevos ni compilan: `no non-test Go files in …/internal/kitest`.

#### La comprobación previa al léxico

{{file ch03-s3 internal/syntax/source_check.go}}

`CheckSource` busca el primer byte inválido y después los controles, pero solo **antes** de él: si el inválido va primero, gana. En la condición, `&&` liga más que `||`: `(b < 0x20 && b != '\t' && b != '\n' && b != '\r') || b == 0x7F`. El mensaje escribe el byte con `%04X` (`U+0001`) y la etiqueta con `%x` (`\u{1}`), como se escribe un escape en Ki.

{{reftest T03.33}}

#### La sesión, `ki check` y la tabla de comandos

{{diff ch03-s2..ch03-s3 internal/driver/session.go}}

- `DiagFormat` es otro conjunto cerrado con `iota`. El `switch` de `render` es sobre un entero, no un type switch, pero termina igual, en un panic con el valor (`driver: unknown diagnostic format 9`, T03.65).
- `Sources` y `Sink` son valores dentro de la `Session`, y `&s.Sources` da la dirección del campo a quien la necesita.
- `ReportAll` ordena, escribe cada diagnóstico en un `strings.Builder` (con una línea en blanco entre dos en formato humano), añade la nota de truncado si hizo falta e ignora el error de escritura: stderr es el último sitio donde contarlo.

{{diff ch03-s2..ch03-s3 internal/driver/commands.go}}

`newCommand` generaliza el `versionCommand` del capítulo 1: envuelve la función del comando en el closure que captura su `command`. `parseFlags` devuelve `(code, done)`: si `done` es verdadero, la línea de órdenes ya tiene respuesta (ayuda o error de uso) y el comando devuelve `code`.

{{file ch03-s3 internal/driver/check.go}}

`fs.String(…)` registra un flag de texto y devuelve un `*string`, que se lee con `*diagnostics` tras `Parse`. El `case 1:` vacío casa y no hace nada: un `case` de Go no cae al siguiente. Los flags van antes del fichero, porque `flag` para en el primer argumento que no lo es: `ki check x.ki --diagnostics=short` responde `ki check: unexpected argument "--diagnostics=short"` y sale con 2. Un fichero ilegible sale con 1: la línea de órdenes estaba bien.

#### La cabecera de los golden

{{file ch03-s3 internal/kitest/header.go}}

- `ParseHeader` anota el BOM y el `\r\n` de la primera línea, para que `ki test --update` (capítulo 5) reescriba el fichero igual.
- La cabecera son las líneas iniciales que empiezan por `//`. `splitKey` usa `strings.CutPrefix`, que quita un prefijo y dice si estaba, y `strings.Cut`, que parte por la primera aparición de un separador y devuelve antes, después y si lo encontró.
- `seen` es un `map[string]bool`, y `seen[key]` de una clave ausente da el valor cero, `false`. Una clave repetida es un error: con dos `// run:` ganaría la segunda sin avisar.
- `setKey` tiene receptor puntero porque rellena el `Spec`, y su `switch` termina en `default: panic("unreachable")`: `splitKey` ya filtró las claves. `strconv.Atoi` lee un entero y `strings.Fields` parte por espacios.
- `outputValue` lee las dos formas de una salida que da la gramática. La sangría tras `// | ` es texto: un diagnóstico humano empieza con espacios (T03.36).
- `setPosition` lee `@COL` o `@LINE:COL` con `strconv.ParseUint(s, 10, 32)`, que rechaza lo que no cabe en 32 bits, y `headerError(format string, args ...any)` reenvía sus argumentos con `args...`.

{{reftest T03.36}}

#### El runner

{{file ch03-s3 internal/kitest/runner.go}}

- **Un campo función.** `Exec` es «cómo se ejecuta `ki`»: en proceso desde `go test`, como subproceso desde `ki test`.
- **Entrar y volver.** `enterRoot` guarda el directorio, entra en la raíz y devuelve un closure, `restore`, que quien llama difiere con `defer restore()`.
- **Un fichero.** `runHere` monta la orden con `append(slices.Clone(spec.Run), spec.Args...)` y clona otra vez antes de añadir el fichero en cada ejecución: los dos `append` sobre una misma base del capítulo 1. El código de salida por omisión sale de `slices.ContainsFunc`. `firstDifference` parte las salidas con `strings.SplitAfter`, que conserva el `\n` de cada línea, y `showLine` lo quita con `strings.CutSuffix`.
- **Posiciones, no texto.** Con anotaciones, una segunda ejecución con `--diagnostics=json` da los errores, y `annotationDifferences` los lee con `json.Unmarshal` sobre un struct anónimo. Compara multiconjuntos: ordena los dos lados y los recorre a la vez; lo que sobra en un lado es un `missing` y en el otro, un `unexpected`. `cmp.Compare` da -1, 0 o 1, y `cmp.Or` el primero de sus argumentos que no es cero: «por línea; si empatan, por columna; si no, por código». `got` es suyo y se ordena en el sitio; `want` es del `Spec` de quien llama, así que `slices.SortedFunc(slices.Values(want), …)` ordena una copia (`slices.Values` da un *iterador* sobre el slice, y `SortedFunc` lo recoge en uno nuevo).
- **El resumen, diferido.** `RunPaths` tiene resultados con nombre y un `defer` que los escribe. `return 0, 1` los asigna **antes** de que corran las funciones diferidas, así que el resumen es correcto también en el camino de error.
- **Una línea, un flush.** `write` escribe la línea `PASS`/`FAIL`, vacía la salida si sabe (la aserción de la teoría) y después las diferencias.
- **Nada no es un éxito.** `kiFiles` devuelve los `.ki` ordenados y, como fallos, un subdirectorio ilegible o un directorio sin ningún `.ki`: «no se ejecutó nada» no es «todo pasa».
- **El proceso de verdad.** `ExecSubprocess` conecta los `io.Writer` a `cmd.Stdout` y `cmd.Stderr` y devuelve `cmd.ProcessState.ExitCode()`; si el programa no arrancó, `ProcessState` es `nil` y devuelve -1.

Quita el `defer restore()` de `RunPaths`. El primer subtest deja el proceso dentro de un directorio temporal que `t.TempDir` borra, y fallan ocho subtests, casi todos lejos de la causa:

```text
--- FAIL: TestRunner (0.00s)
    --- FAIL: TestRunner/exit_code_diff_first (0.00s)
        runner_test.go:144: exit code first, then stdout: got ok=false and "FAIL c.ki\n- cannot enter the root `/tmp/…/001`: getwd: no such file or directory\n", want …
```

Aislado con `-run 'TestRunner/restores_cwd'`, T03.49 lo dice en una línea: `the working directory is "/tmp/…/001" after RunPaths, was "…/internal/kitest"`. Deshaz el cambio.

{{reftest T03.48}}

{{reftest T03.49}}

#### `ki test` y los dos modos

{{file ch03-s3 internal/driver/test.go}}

`ki test` ejecuta cada golden como subproceso de su propio ejecutable (`os.Executable()`). Bajo `go test`, ese ejecutable es el binario de **test**, y por eso existe `testExec`, una variable que solo los tests cambian (`runGoldensInProcess` la apunta a `Main` y la devuelve a `nil` con `t.Cleanup`). El runner escribe sin mirar errores, y `outWriter` recuerda el primero para que `runTest` salga con 1 si el informe se perdió (T03.67).

{{reftest T03.66}}

El mismo `Runner` corre en proceso desde `tests/golden_test.go`:

{{file ch03-s3 tests/golden_test.go}}

Su `t.Run(name, …)` recibe un nombre calculado, la excepción a la regla del literal: un subtest por fichero, `TestKi/errors/ch03/invalid_utf8`, que `bookx` enlaza con la tarjeta de `ref` `tests/errors/ch03/invalid_utf8.ki`. `Main` crea una `Session` nueva en cada llamada, así que ningún caso ve el estado de otro. `filepath.Rel` y `filepath.ToSlash` dan la ruta relativa a la raíz y con barras `/` también en Windows, la forma que muestra el runner.

#### Los golden y la CI

El 0xFF y el 0x01 se escriben con `printf`, no con el editor (`printf '…let x = \xff\n' > …`):

{{file ch03-s3 tests/errors/ch03/invalid_utf8.ki}}

{{file ch03-s3 tests/errors/ch03/invalid_utf8_render.ki}}

{{file ch03-s3 tests/errors/ch03/control_char.ki}}

{{file ch03-s3 tests/errors/ch03/valid_file.ki}}

Dos marcan su error en línea; `invalid_utf8_render.ki` fija el texto entero, con el margen de dos cifras de la línea 10; `valid_file.ki` es el caso «sí», sin el que un `check` que siempre falla pasaría los demás. `ci-local.sh` ejecuta los golden también por el binario:

{{diff ch03-s2..ch03-s3 ci/ci-local.sh}}

El mensaje de la etiqueta: `ch03-s3: source check, ki check and the golden runner`.

Si te atascas o tu versión se desvía demasiado, copia el fichero de `ref/ch03-s3` y sigue.

Al terminar: tus tests en verde, commit y etiqueta `ch03-s3`; compárate con `ref/ch03-s3`.

## Ejecútalo

Desde la raíz de tu repo, con la sesión 3 terminada; `ci/ci-local.sh` acaba con estas mismas cinco líneas de `ki test` y `verify: OK`:

```console
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
**2. Tres líneas a golden.** Añade a `TestRenderHuman` un subtest `labels_on_three_lines`: un E0301 sobre un nombre de la línea 4 de un fichero de cinco, con secundarias en las líneas 1 y 2. Genera su golden y léelo. ¿Qué otra línea del test cambia?
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

Los ayudantes son los de `render_test.go` en `ref/ch03-s2`: `newSource` crea `test.ki`, `unknownName` el E0301, y `spanWithin(t, src, ctx, sub)` busca `sub` dentro de `ctx`, para un texto que se repite. Primero falla por el golden que falta. Con `-update` se escribe, pero el paquete sigue en rojo, también en esa ejecución: `testdata has 15 golden files, want 14`. Es T03.23: `wantGoldens` pasa a 15. El golden:

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

```sh
printf '// test: T03.68\n// run: check\n// args: --diagnostics=short\n// expect-exit: 1\n// expect-stderr: tests/errors/ch03/short_format.ki:6:9: error[E0101]: invalid UTF-8 byte 0xFF\nlet x = \xff\n' > tests/errors/ch03/short_format.ki
```

`ki test --root . tests` da `5 passed, 0 failed`. Con `6:8` en la expectativa falla con `- …:6:8: …` y `+ …:6:9: …`; sin la línea `args:`, con `+ error[E0101]: invalid UTF-8 byte 0xFF`.
:::

:::exercise
**4. Sin límite.** Imagina un `CheckSource` que informara de cada byte inválido, sobre un fichero de 100 000 bytes 0xFF en una sola línea. ¿Qué pasaría con el límite de 100 de `Report`? ¿Y sin él?
:::

:::solution
Cada diagnóstico humano muestra la línea entera, aquí 100 000 U+FFFD de 3 bytes: unos 300 KB. La referencia imprime uno solo, de ese tamaño. Medido con esa variante: con el límite de 100, unos 30 MB de stderr en 0,15 s; con un límite de 1000, 300 MB, 1,8 s y unos 850 MiB de memoria, porque `ReportAll` construye toda la salida en un `strings.Builder` antes de escribirla. Sin límite, unos 30 GB en memoria: un fallo antes que una salida. `MaxErrors` protege de cualquier pase que se desboque; `CheckSource` ni llega a él, porque en un fichero que no es texto basta el primer byte malo.
:::

:::exercise
**5. Una ruta que no existe.** Hoy, `ki test --root . nope` responde `FAIL nope` y `- cannot read file: no such file or directory`, y sale con 1. Haz que sea un error de uso, con un mensaje claro, la línea `usage:` y salida 2, sin ejecutar nada. Escribe el test.
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

`os.ErrNotExist` es el mismo valor que `fs.ErrNotExist`, que aquí no se puede escribir: en `runTest`, `fs` es el `FlagSet`, una variable local que tapa el paquete `io/fs`. El test, en `TestCLI`:

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

`goldenDir` y `runCLI` son ayudantes de `cli_test.go` en `ref/ch03-s3`: el primero crea un directorio temporal con `pass.ki`, que pasa, y `fail.ki`, que falla. La segunda fila exige que no se ejecute nada aunque la ruta mala sea la última, y `runGoldensInProcess` evita que, sin la comprobación, `pass.ki` lance el binario de test. Sin el `if`, las dos filas fallan con `got 1 "FAIL nope\n- cannot read file: …"`.
:::

## Preguntas de repaso

:::review
¿Por qué un error del programa Ki es un `Diagnostic` que va a un `Sink` y no un `error` de Go?
:::

:::answer
Porque hay muchos y no deben cortar la ejecución: el pase sigue, y el sink los reúne, ordena y corta en 100. Un valor con código, spans y notas se pinta de tres formas y se compara por código y posición. `error` queda para lo que falla fuera del programa.
:::

:::review
¿Por qué un type switch sobre una interfaz sellada termina en `default: panic("unreachable")`, si el sello impide implementarla desde fuera?
:::

:::answer
Porque el sello tiene una grieta (un struct que incrusta la interfaz la cumple) y, sobre todo, porque el type switch no es exhaustivo: añadir un tipo y olvidar su `case` compila. El `default` convierte el olvido en un panic inmediato con el tipo en el mensaje.
:::

:::review
¿Por qué `WithNote` hace `append(slices.Clip(d.Notes), n)` si el receptor ya es una copia?
:::

:::answer
Porque la copia comparte el array de `Notes` con el original, y con capacidad de sobra `append` escribiría en él: dos copias del mismo diagnóstico se pisarían. `Clip` obliga a reservar un array propio.
:::

:::review
¿Por qué la posición cuenta code points pero el caret se coloca en columnas visuales?
:::

:::answer
Sirven a dos usos. La posición la lee un editor, para el que un tabulador es un carácter. El caret cae bajo el carácter en la terminal, donde un tabulador ocupa hasta cuatro columnas y `<U+202E>`, ocho; `displayLine` traduce cada offset de byte a su columna visual.
:::

:::review
¿Por qué, con anotaciones, el runner compara (código, línea, columna) y no el texto de stderr?
:::

:::answer
Porque el texto de un mensaje mejora con el tiempo, y lo que identifica a un error es qué es y dónde está: comparar texto rompería todos los golden de error con cada cambio de redacción. Para fijar el texto queda un golden sin anotaciones, como `invalid_utf8_render.ki`.
:::

:::review
¿Por qué `ki test` ejecuta cada golden como subproceso, y para qué existe `testExec`?
:::

:::answer
Para probar lo que vería una shell: el binario real y su código de salida. Pero bajo `go test`, `os.Executable()` es el binario de test, no `ki`, así que los tests de `ki test` cambian `testExec` para ejecutar `Main` en proceso.
:::

## Resumen

- Un error del programa es un `Diagnostic` (gravedad, código estable, etiquetas, notas, ayuda) que los pases entregan a un `Sink`, que se detiene tras 100 errores.
- `iota` numera las constantes de un tipo definido, y `String()` las nombra; Go no cierra el conjunto, así que hay que cubrir el valor imposible.
- `%v` y `%s` usan `String()`, `%#v` la sintaxis de Go, y `Formatter` toma el control de todos los verbos salvo `%T` y `%p`.
- Una interfaz se cumple con tener sus métodos; `v, ok := w.(T)` pregunta por un tipo o una interfaz anónima, y un puntero `nil` dentro de una interfaz no es una interfaz `nil`.
- La interfaz sellada da conjuntos cerrados de tipos sin exhaustividad: cada type switch termina en `default: panic("unreachable")`.
- `errors.Join` reúne errores; `(T, error)`, los punteros opcionales y el valor cero hacen de `Result` y `Option`.
- Las etiquetas de struct fijan las claves JSON; un slice vacío sale `[]`, uno `nil` y un puntero `nil`, `null`.
- El caret va en columnas visuales: tabuladores a 4, un U+FFFD por byte inválido, imágenes de control y `<U+XXXX>` para lo invisible.
- Los golden viven en `testdata/` y se regeneran con `go test ./paquete -update`; después se leen.
- El runner de Ki aplica una cabecera estricta, compara posiciones cuando hay anotaciones, restaura el directorio y prueba el binario real.

## Go en el trabajo

**Errores como valores frente a excepciones.** La FAQ de Go («Why does Go not have exceptions?») da dos razones: acoplar las excepciones a una estructura de control como `try-catch-finally` «results in convoluted code», y empuja a tratar como excepcionales errores corrientes, «such as failing to open a file». En Ki, los errores del programa compilado ni siquiera son `error`: son datos que se acumulan y se pintan.

**Envolver en las fronteras.** «Working with Errors in Go 1.13» (Damien Neil y Jonathan Amsterdam, blog de Go, 2019) responde cuándo usar `%w`: «Wrap an error to expose it to callers. Do not wrap an error when doing so would expose implementation details», porque «wrapping an error makes that error part of your API». `Load` envuelve a propósito, para que quien carga un fichero pregunte `errors.Is(err, fs.ErrNotExist)`. La práctica: envolver una vez, en la frontera del paquete, con el contexto que solo él conoce, y comprobar con `errors.Is` o `errors.As`, nunca comparando texto.

**Golden en el propio repo de Go.** En Go 1.27.3, `src/cmd/gofmt/testdata/` tiene 31 parejas `.input`/`.golden`, y `gofmt_test.go` declara el flag de este capítulo: `var update = flag.Bool("update", false, "update .golden files")`. Un `.input` que necesita opciones las lleva en un comentario `//gofmt flags` de sus primeras 20 líneas (`//gofmt -s`; 18 de los 31 tienen uno), y `TestRewrite` comprueba además la idempotencia: formatea el `.golden` y exige que no cambie.
