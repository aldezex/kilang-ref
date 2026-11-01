---
chapter: 3
title: Tipos suma sin tipos suma: interfaces selladas y errores como valores
part: I
hours: 7
milestone: `ki check` muestra un diagnóstico con caret
---

## Objetivo

Al terminar, `ki check` señala con un caret lo que está mal en un fichero:

```console
$ build/bin/ki check tests/errors/ch03/invalid_utf8_render.ki; echo $?
error[E0101]: invalid UTF-8 byte 0xFF
  --> tests/errors/ch03/invalid_utf8_render.ki:10:9
   |
10 | let x = �
   |         ^ not valid UTF-8
1
```

`ki test` ejecuta los programas de prueba y compara lo que imprimen con lo que esperan sus cabeceras. Sin lexer, el único error posible es un fichero que no es texto válido, pero los diagnósticos y el runner ya son los definitivos, camino del hito M1 (capítulo 10).

:::note
**Fe de erratas (2026-10-28).** Los mensajes de los errores de este capítulo mejoran en el capítulo 7 («El listón: errores al nivel de rustc»), que los explica.
:::

## Teoría

### Un error de tu programa es un dato

Un error del programa se muestra de tres formas: con caret para una persona, como `fichero:línea:columna: mensaje` para un editor y en JSON para el runner. Por eso es un **valor**: cada pase (una etapa del compilador) entrega un `diag.Diagnostic` a un `diag.Sink`, que los reúne, y otra pieza decide cómo se muestra.

El `error` de Go queda para lo que falla fuera del programa (un fichero ilegible), y `panic`, para un fallo del compilador.

Gravedad (error, warning o nota) y formato son tipos suma, un `enum` en Rust. Go no los tiene: `iota` para valores, interfaces selladas para tipos, y `(T, error)` en lugar de `Result`.

### Tipos definidos, `iota` y `String()`

```go
type Severity uint8

const (
	Error Severity = iota // 0
	Warning               // 1
	Note                  // 2
)
```

En un bloque `const`, `iota` vale 0 y aumenta en uno con cada línea; una línea sin expresión repite la anterior. `Severity` es un tipo propio (capítulo 2): asignarle una constante de `type DiagFormat uint8` no compila.

Go **no** garantiza que el conjunto esté cerrado: `Severity(7)` compila, y un `switch` que no cubre un caso no avisa. Por eso `String()` termina con `return fmt.Sprintf("Severity(%d)", uint8(s))`: un valor que solo puede salir de un fallo se ve como tal.

:::from Rust
`iota` da los valores de un `enum` sin datos, sin su garantía: en Rust no existe un `Severity` fuera de sus variantes, y `match` las exige todas.
:::

### `fmt`: verbos, `Stringer` y `Formatter`

`fmt` usa la interfaz `fmt.Stringer`, `String() string`, con los verbos de texto (`%v`, `%s`, `%q`, `%x`), no con los numéricos: `%d` de `Warning` da `1`. `Code` guarda una letra y un número, y su `String()` usa `"%c%04d"` (un carácter y cuatro cifras rellenas con ceros): `%v` de `E(101)` da `E0101`.

`%#v` escribe la sintaxis de Go, sin `String()`. Un tipo que cumple `fmt.Formatter` decide la salida de **todos** los verbos salvo `%T` y `%p`.

:::from Rust
`%v` con `String()` es `{}` con `Display`, y `%#v` se parece a `{:?}` con `#[derive(Debug)]`.
:::

### Interfaces y satisfacción implícita

Un tipo cumple una interfaz si tiene sus métodos, sin declararlo (capítulo 1, `io.Writer`). El compilador lo comprueba donde asignas el valor, y dice qué falta: `U does not implement io.Writer (missing method Write)`.

Un valor de interfaz guarda el tipo concreto y el valor. La **aserción de tipo** pregunta por el tipo: si no es, `w.(T)` hace panic y `v, ok := w.(T)` da `ok == false`.

`T` puede ser una interfaz sin nombre, escrita en el sitio. Así pregunta el runner si su salida sabe vaciarse (`os.Stdout` no):

```go
if f, ok := out.(interface{ Flush() error }); ok {
	_ = f.Flush()
}
```

El **type switch**, `switch v := x.(type) { case A: … }`, hace varias aserciones a la vez, y en cada `case` `v` tiene ese tipo.

:::from Rust
Una interfaz es un trait sin `impl Trait for T`. Su valor es un `dyn Trait`, y `w.(T)` hace de `downcast_ref::<T>()` sobre un `dyn Any`.
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

Fuera de `shape`, `isShape` no se puede nombrar: un `Tri` de otro paquete con ese método da `Tri does not implement shape.Shape (unexported method isShape)`. El receptor de `func (Circle) isShape() {}` no tiene nombre porque no se usa. `%T` escribe el tipo concreto.

El sello tiene una grieta: un struct de otro paquete que **incrusta** la interfaz, `type Sneaky struct{ shape.Shape }`, la cumple. Incrustar es declarar un campo sin nombre, cuyo tipo le da nombre y cuyos métodos se **promocionan** al struct.

Además, el type switch no es exhaustivo: si añades `Tri` a `shape` y olvidas su `case`, compila. Por las dos razones, todo type switch sobre una interfaz sellada del libro termina en `default: panic("unreachable")`.

:::from Rust
En Rust, un `match` sin `Tri` da `error[E0004]`; en Go lo descubre el `default` al ejecutarse.
:::

:::more Qué imprime cada cosa, y el sello visto desde Rust, TS y Python
**Verbos.** Sobre `Code`, `%d` da `{69 101}`. `%+v` añade los nombres de los campos solo a un struct sin `String()`: sobre `Code` da `E0101`. `%#v` tiene su propia interfaz, `GoStringer`: sobre `Code` da `diag.Code{Letter:0x45, Number:0x65}`, y para `Warning`, `0x1`. `%02X` y `%x` escriben hexadecimal, con dos cifras en mayúsculas (`FF`) o en minúsculas sin relleno (`1f`).

**`Formatter`.** Es `Format(f fmt.State, verb rune)`. Decide también `%#v`, y el `String()` del tipo deja de usarse.

**Tipos definidos.** Con `const FormatShort DiagFormat = 1`, `var s Severity = FormatShort` da `cannot use FormatShort (constant 1 of uint8 type DiagFormat) as Severity value in variable declaration`.

**Interfaces.** Si el método es de `*T` y pasas un `T`, el mensaje acaba en `(method Write has pointer receiver)`. Un `*os.File` no tiene `Flush`, así que con `os.Stdout` el `ok` del runner es falso.

**El sello.** Con el campo incrustado a `nil`, llamar a un método promocionado hace panic. `Area(outside.Sneaky{})` hace panic con `unreachable: outside.Sneaky`, en lugar de inventar un área.

**En otros lenguajes.**

- Rust, `#[derive(Debug)]`: `Code { letter: 69, number: 101 }`. Rust, `match` sin `Tri`: ``error[E0004]: non-exhaustive patterns: `&Shape::Tri` not covered``. El renderizador de Ki imita la forma de ese error: flecha `-->`, barra y caret.
- TS: una unión discriminada con `const never: never = s` en el `default` sí falla al compilar si añades una variante (`Type '{ kind: "tri"; }' is not assignable to type 'never'`). Go no tiene equivalente.
- Python: `Severity(7)`, con `enum.auto()`, da `ValueError: 7 is not a valid Severity`; en Go es un valor más.
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

`find(false) == nil` es `false`, aunque `fmt.Println` imprima `<nil>`: el `error` lleva el tipo `*MyErr` y un puntero nulo. Cuando no hay error, devuelve el literal `nil`, nunca una variable de un tipo concreto.

:::from Rust
`Option<Box<dyn Error>>` no tiene esta trampa: un `Box` nunca es nulo.
:::

### `errors.Join` y lo que ya sabes

`errors.Join` reúne varios errores y descarta los `nil`, y `errors.Is` encuentra cada uno.

`errors.As` (capítulo 2) se usa al revés de envolver. `reason`, en el runner, quita el `*fs.PathError` de un error antes de mostrarlo: repite una ruta absoluta.

:::from Python
Recuerda a `ExceptionGroup`, pero el grupo es un valor más que se devuelve.
:::

### Sin `Option` ni `Result`

Lo que falla devuelve `(T, error)`. Lo opcional se escribe de dos formas:

- **El valor cero**, si cero no es válido: `Runner.Root == ""` es «el directorio de trabajo».
- **Un puntero**, si lo es: en la cabecera de un golden, `expect-exit: 0` y no decir nada son cosas distintas, así que `Spec.ExpectExit` es un `*int` y `nil` es «no lo dice».

:::from Rust
`*int` hace de `Option<i32>`, y desreferenciar un `nil` es un panic. `(T, error)` es un `Result` sin `?`, que nadie obliga a comprobar.
:::

### JSON con etiquetas de struct

`encoding/json` convierte un struct en un objeto con sus campos exportados, en orden de declaración. Una **etiqueta de struct** es una cadena tras el tipo del campo, que el paquete lee en ejecución; `json:"severity"` fija la clave:

```go
type jsonDiagnostic struct {
	Severity string   `json:"severity"`
	Notes    []string `json:"notes"`
	Help     *string  `json:"help"`
	// …
}
```

Con `json.Marshal`, un slice `nil` sale `null`, pero uno vacío (`[]string{}`), `[]`; un puntero `nil`, `null`.

:::from Rust
`#[serde(rename = …)]` lo lee la macro al compilar, y una errata no compila. Con `jsn:"severity"` en Go, la clave sale `"Severity"`, sin aviso.
:::

### `strings.Builder`, `filepath.WalkDir` y el directorio de trabajo

- **`strings.Builder`** acumula texto (`WriteString`, `WriteRune`) y lo da con `String()`; su valor cero sirve.
- **`filepath.WalkDir(root, fn)`** llama a `fn(path, d, err)` por cada entrada del árbol, con `d` un `fs.DirEntry`. Su orden no es el de las rutas completas, y por eso el runner ordena.
- **`os.Getwd` y `os.Chdir`** leen y cambian el directorio de trabajo, **del proceso entero**: el runner entra en su raíz, para mostrar rutas relativas, y tiene que volver.

:::from Python
En un test de Go, `t.Chdir` restaura el directorio al acabar, como `monkeypatch.chdir` de pytest.
:::

:::more Casos límite: el nil tipado, `errors.Join`, `strings.Builder`, `json` y `WalkDir`
**El nil tipado.** La FAQ de Go lo explica en «Why is my nil error value not equal to nil?». Desreferenciar un puntero `nil` da el panic `invalid memory address or nil pointer dereference`.

**`errors.Join`.** `errors.Join(fs.ErrNotExist, nil, fs.ErrPermission)` tiene el texto `"file does not exist\npermission denied"`, y `errors.Is` encuentra los dos también a través de otro `%w`. `errors.Join(nil, nil)` es `nil`: si acumulas en un bucle, da `nil` cuando no hubo ninguno. `fmt.Errorf` admite también varios `%w`. `errors.AsType`, la versión genérica de `errors.As`, espera al capítulo 4.

**`strings.Builder`.** `WriteRune` escribe un code point en UTF-8.

**`json.Marshal`.** Escapa `"` como `\"`, y `<`, `>` y `&` como `\u003c`, `\u003e` y `\u0026`.

**`json.Unmarshal`.** Acepta una clave que solo difiera en mayúsculas (un campo `Line` sin etiqueta recibe `"line"`) e ignora las que no tienen campo.

**Las etiquetas.** `go vet` solo rechaza una etiqueta mal formada (`json:severity`, sin comillas). En Rust, una errata como `renam` da `unknown serde field attribute`.

**`WalkDir`.** `d.IsDir()` dice si la entrada es un directorio, y `err` no es `nil` si no se pudo leer. Visita cada directorio en orden léxico: `a/y.ki` antes que `a-b/x.ki`, pero como cadenas `a-b/x.ki` va primero (el `-` es 0x2D y la `/`, 0x2F).
:::

### UTF-8 y los ficheros golden

`utf8.Valid(b)` solo responde sí o no, y un diagnóstico necesita el **offset** del primer byte malo: por eso existe `FirstInvalidUTF8` (capítulo 2).

Un test **golden** compara una salida larga con la guardada en `testdata/`, un directorio que el comando `go` ignora. Escribir esos ficheros a mano es lento, así que el test los genera con un flag propio:

```go
var update = flag.Bool("update", false, "rewrite golden files")
```

`flag.Bool` registra el flag en el binario de test y devuelve un `*bool`. Va **después** del paquete, `go test ./internal/diag -update`, porque `go test` pide los paquetes antes de un flag que no conoce; con `./...`, falla en cada paquete sin el flag.

`-update` escribe lo que el código produce *ahora*, bueno o malo: después lees cada fichero y revisas el diff.

:::from TS
Es el *snapshot testing* del runner de Node, pero el flag y el formato del fichero los decides tú.
:::

### Compiladores: diagnósticos como datos

- **Códigos estables.** Un código no se reutiliza nunca: es el que buscas en la documentación y comparan los tests. Su centena dice la etapa: `E01xx` el lexer (y la comprobación previa), `E02xx` el parser, `E03xx` los nombres, `E04xx` los tipos.
- **Un límite.** El compilador se detiene tras 100 errores (`diag.MaxErrors`): lo demás suele ser cascada del primero.
- **Una comprobación previa al léxico.** `syntax.CheckSource` rechaza el primer byte de UTF-8 inválido (E0101) o de control, salvo tabulador, `\n` y `\r` (E0102): el lexer del capítulo 5 recibe texto limpio.
- **Un runner con cabecera.** Cada programa de `tests/`, el otro tipo de golden, dice en sus primeros comentarios cómo se ejecuta y qué debe pasar, con gramática estricta: una cabecera mal escrita e ignorada no comprobaría nada.
- **Anotaciones.** Un error marcado en línea (`// ERROR(E0101) @9`) se compara por (código, línea, columna), no por su texto: reformular un mensaje no rompe cien golden.

**Dos clases de columna.** La posición cuenta code points (capítulo 2), lo que entiende un editor. El caret cuenta **columnas visuales**, para caer bajo el carácter en pantalla, con el tabulador hasta el siguiente múltiplo de 4. Aquí, el byte malo está en la columna 10 y el caret, en la 13:

```console
$ printf 'fn main() {\n\tlet x = \xff\n}\n' > build/bad.ki
$ build/bin/ki check build/bad.ki
error[E0101]: invalid UTF-8 byte 0xFF
 --> build/bad.ki:2:10
  |
2 |     let x = �
  |             ^ not valid UTF-8
```

Lo invisible lleva un sustituto de ancho conocido:

- un byte inválido, U+FFFD;
- un control ASCII, su imagen de control (*control picture*) del bloque U+2400 (`␁`, `␍`, `␡`);
- un control C1 o bidireccional, `<U+202E>`.

Los bidireccionales reordenan el texto en pantalla (el ataque *Trojan Source*).

## Tests primero

{{cards}}

Úsalas como siempre: escribe el test de la tarjeta, míralo fallar, implementa lo justo para el verde, y solo entonces despliega la referencia con su `{{reftest}}`.

:::more Los tests por dentro, sesión por sesión, y el contrato de `SortStableFunc`
Están en cada fichero de test, en `ref/ch03-sK`.

- **Sesión 1, los tests.** En `sink_test.go`, `^uint32(0)` (el complemento de cero, el mayor `uint32`) detiene el bucle `for off := uint32(2); off != ^uint32(0); off--` cuando `off--` da la vuelta por debajo de cero. Si `Report` cortara en `MaxErrors-1`, fallarían los tres subtests del máximo: T03.07 y T03.08 miran a los dos lados del borde.
- **Sesión 1, el código.** Go acepta `;` explícitos entre declaraciones, y `gofmt` los pasa a líneas separadas: por eso la interfaz va compacta. `Code` tiene dos campos comparables, así que dos códigos se comparan con `==`. La función de `SortStableFunc` devuelve un negativo si `a` va antes, un positivo si va después y cero si son iguales. Con `SortStableFunc`, dos errores en el mismo offset salen siempre en el orden en que se informaron. El límite del *insertion sort* de `slices.SortFunc` es `maxInsertion = 12` (`slices/zsortanyfunc.go`, Go 1.27).
- **Sesión 2, `render_test.go`.** T03.10, T03.11 y T03.24–T03.27 comparan con el texto del test; T03.12–T03.22, con `testdata/`. `checkGolden(t, name, got)`, con `*update`, crea `testdata/` si falta (`os.MkdirAll`) y escribe `testdata/<name>.golden`; sin él compara, y si el fichero falta, su error dice que corras `-update`. `spanOf(t, src, sub)` da el span de `sub` (`strings.Index`) y falla si no está.
- **Sesión 2, los textos.** Los textos con controles usan escapes: `"\x01"` es un byte y `"\u2400"`, un code point. T03.23 cuenta los golden con `filepath.Glob` contra la constante `wantGoldens`, para que borrar uno no haga que se compruebe menos.
- **Sesión 2, `testdata/` y `-update`.** `testdata/` no es un paquete ni entra en `./...`. El orden del flag es la regla de `go help testflag`, y `go test ./... -update` responde `flag provided but not defined: -update` en cada paquete sin el flag. En el runner de Node, `t.assert.snapshot(valor)` compara con un fichero `.snapshot`, y `node --test --test-update-snapshots` lo reescribe. Una cadena *raw* no admite `` ` ``: el JSON esperado de T03.25 concatena una normal, `` ` + "`z`" + ` ``.
- **Sesión 3, `header_test.go`.** `ptr("6765\n")` da un `*string`. `reflect.DeepEqual(got, want)` compara dos `Spec` entrando en slices y punteros; `==` no sirve, porque un struct con slices no es comparable.
- **Sesión 3, `runner_test.go`.** El `Exec` falso guarda sus llamadas en un slice recibido por puntero: `*calls = append(*calls, …)` cambia el slice de quien llama, `(*calls)[0]` lee el primero, y `new([]call)` crea uno vacío y da su dirección.
- **Sesión 3, `cli_test.go`.** `runGoldensInProcess(t)` apunta `testExec` a `Main` y la devuelve a `nil` con `t.Cleanup`: T03.66, T03.67 y T03.75 la llaman. `goldenDir(t)` crea un directorio temporal con `pass.ki`, que pasa, y `fail.ki`, que falla.
- **Sesión 3, en general.** `t.Skip` salta un test sin sentido en esa máquina (`runtime.GOOS == "windows"`), y `os.SameFile`, sobre los `FileInfo` de dos `os.Stat`, dice si dos rutas son el mismo directorio. `t.Chdir` no se puede usar en un test paralelo.
- **Los paquetes de test.** Los de `diag` y `driver` están en su paquete; los de `syntax` y `kitest`, en `syntax_test` y `kitest_test`, externos porque solo usan lo exportado. La excepción es T03.72 (`TestWalkErrors`, `runner_internal_test.go`): llama a `unreadableDirectory`, sin exportar, y va en `package kitest`. `tests/golden_test.go` es el único fichero de su paquete: uno con solo ficheros `_test.go` es válido para `go test`.
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

- Los `With…` tienen receptor **por valor**: cambian una copia y la devuelven (`d = d.WithNote("…")`).
- La copia es superficial (capítulo 2): `Notes` comparte el array, y si sobra capacidad, `append` escribiría en él. `slices.Clip` obliga a reservar uno nuevo.

Quita el `slices.Clip` de `WithNote` y T03.03 ve el aliasing:

```text
--- FAIL: TestDiagnostic (0.00s)
    --- FAIL: TestDiagnostic/with_methods_return_copies (0.00s)
        diagnostic_test.go:95: Notes of two copies = ["old" "b"] and ["old" "b"], want [old a] and [old b]
```

Deshaz el cambio.

{{file ch03-s1 internal/diag/sink.go}}

- El valor cero del `Sink` es un sink vacío: no necesita constructor.
- `Report` guarda los 100 primeros errores, descarta el 101 y marca `truncated`; desde ahí lo descarta todo, también los warnings.
- `slices.SortStableFunc` conserva el orden de entrada de los iguales. Su función compara con `<` y `>`: restar dos `uint32` daría la vuelta.

T03.06 ordena 90 diagnósticos porque, con pocos, una ordenación no estable es estable por casualidad: `slices.SortFunc` usa *insertion sort* hasta 12 elementos. Cambia `SortStableFunc` por `SortFunc`:

```text
--- FAIL: TestSink (0.00s)
    --- FAIL: TestSink/sort_by_position_is_stable (0.00s)
        sink_test.go:104: order of 90 diagnostics at 3 offsets differs from position then report order:
             got ["o0-10" "o0-29" "o0-00" "o0-28" "o0-27" "o0-01" …
            want ["o0-00" "o0-01" "o0-02" "o0-03" "o0-04" "o0-05" …
```

Deshaz el cambio y compara:

{{reftest T03.03}}

{{reftest T03.06}}

{{reftest T03.08}}

Si te atascas o tu versión se desvía demasiado, copia el fichero de `ref/ch03-s1` y sigue.

Al terminar: tus tests en verde, commit y etiqueta `ch03-s1` (mensaje: `ch03-s1: diagnostics as values`); compárate con `ref/ch03-s1`.

### Sesión 2: los renderizadores human, short y json (≈ 2,5 h)

#### Los tests, en rojo

1. **Escribe T03.10–T03.27** (`checkGolden` y `spanOf`, en el pliegue tras las tarjetas). Describen tres funciones, cuyo resultado termina en `\n`:

```go
func RenderHuman(d Diagnostic, sm *base.SourceManager) string
func RenderShort(d Diagnostic, sm *base.SourceManager) string
func RenderJSON(d Diagnostic, sm *base.SourceManager) string
```

El formato humano:

```text
<severidad>[<código>]: <mensaje>     la cabecera
" "*g + "--> " + path:line:col       g: las cifras del mayor número de línea mostrado
" "*g + " |"
número | texto                       cada línea con etiquetas, sin espacios finales
" "*g + " |" + marcas                bajo cada línea, `^` de la primaria y luego las `-` de las secundarias por columna
" "*g + " ..."                       entre dos líneas no consecutivas
" "*g + " = note: …"                 las notas, y la ayuda con = help:
corto y JSON                         los de T03.24 y T03.25
```

2. **Míralos fallar.** El primer rojo es `undefined: RenderHuman`. Con `render.go` escrito y sin golden, el segundo dice qué hacer:

```text
--- FAIL: TestRenderHuman (0.00s)
    --- FAIL: TestRenderHuman/secondary_same_line (0.00s)
        render_test.go:139: reading testdata/secondary_same_line.golden (run go test ./internal/diag -update to create it): open testdata/secondary_same_line.golden: no such file or directory
…
```

#### El código

{{file ch03-s2 internal/diag/render.go}}

`RenderHuman` ordena las etiquetas por línea, las agrupa y pinta cada grupo.

**`displayLine`** es la pieza delicada. Devuelve el texto y `cols`: `cols[i]` es la columna visual donde empieza el byte `i`. Como los spans cuentan bytes, el caret de `[lo, hi)` empieza en `cols[lo]` y mide `cols[hi] - cols[lo]`.

:::more `RenderHuman`, `RenderJSON`, las líneas largas y `displayLine` por dentro
**`RenderHuman`.**

- Ordena las etiquetas por línea y, en cada una, la primaria primero y las secundarias por columna. `type placed struct {…}` es un tipo declarado dentro de la función.
- Aquí sí se restan líneas y offsets, pero convertidos antes a `int`, de 64 bits en tu máquina: la diferencia de dos `uint32` cabe y no da la vuelta.
- Tras ordenar, `g` son las cifras de la línea de la última etiqueta (`strconv.Itoa`).
- El bucle externo, `for i := 0; i < len(all); {`, no avanza `i` en su cabecera: `group` se queda con las etiquetas de la línea, e `i += len(group)` salta tras ellas. `prev` decide el `...`.
- Un span que sigue en otras líneas (un `{` sin cerrar, T03.22) se corta al final de la primera con `min` y `max`, y una marca ocupa al menos una columna, para que se vea un span vacío.
- **Líneas largas (T03.22).** Una línea de más de `maxShownBytes` (200 bytes) se muestra como una ventana alrededor de sus etiquetas, desde `contextBytes` (40) antes de la primera, con `cutMark` (`...`) donde se cortó: el tamaño de un diagnóstico no depende del del fichero. `lineWindow` calcula la ventana y mueve cada extremo como mucho tres bytes, hasta el inicio de un carácter, para no partirlo.

**`displayLine`.** Recorre la línea con `utf8.DecodeRuneInString`, y `cols` tiene una entrada por **byte** más una. En el `switch`, el orden importa: el primer caso separa un byte inválido de un U+FFFD escrito de verdad (capítulo 2). Un tabulador mide `tabWidth - width%tabWidth`, y `0x2400 + r` es aritmética de code points, porque el bloque de imágenes sigue el orden de ASCII. Un `case` con varias expresiones separadas por comas casa si casa cualquiera. `strings.TrimRight` quita los espacios finales del texto, pero `cols` los sigue contando.

**`RenderJSON`.** `Notes` empieza como `[]string{}`, así que sale `[]` aunque no haya notas. `Help` solo apunta a algo si hay ayuda.
:::

Cuando compile, genera los golden y **léelos** contra las tarjetas:

```console
$ go test ./internal/diag -update
ok  	github.com/aldezex/kilang/internal/diag	0.004s
```

Cada golden protege lo que validaste. Cambia la condición del `...`, `line != prev+1`, por `line > prev+2`:

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

`two_lines_with_gap.golden` salta dos líneas y no lo distingue; con la 9 y la 11 se salta una. Deshaz el cambio y compara:

{{reftest T03.11}}

{{reftest T03.20}}

{{reftest T03.25}}

Si te atascas o tu versión se desvía demasiado, copia el fichero de `ref/ch03-s2` y sigue.

Al terminar: tus tests en verde, commit y etiqueta `ch03-s2` (mensaje: `ch03-s2: human, short and json renderers`); compárate con `ref/ch03-s2`.

### Sesión 3: la comprobación previa al léxico, `ki check` y el runner golden (≈ 2,5 h)

#### Los tests, en rojo

1. **Escribe T03.28–T03.75.** Su interfaz (ayudantes, en el pliegue tras las tarjetas):

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

:::more La gramática exacta de la cabecera
La cabecera son las líneas iniciales que empiezan por `//`; la primera que no, la cierra.

- Una clave se escribe `// clave: valor`, con un espacio tras `//` y otro tras `:` (`// run:check` es un error). Hay seis, y cada una aparece una vez como mucho: `test` (el id de la tarjeta) y `run`, obligatorias; `args`, `expect-exit`, `expect-stdout` y `expect-stderr`, opcionales. Otra palabra seguida de `:` es texto libre.
- `run` vale `check`, `run`, `emit tokens`, `emit ast` o `emit core`; `args` son flags separados por espacios, que van entre la orden y el fichero; `expect-exit`, un entero.
- `expect-stdout` y `expect-stderr` dan la salida entera en una línea (`// expect-stdout: 6765` es `"6765\n"`) o, sin nada tras `:`, en un bloque: `// | texto` por línea y `// |` para una vacía.
- Lo que no se dice tiene valor por omisión: stdout y stderr vacíos, y el código de salida 1 si alguna anotación es un error y 0 si no.
- Una anotación, en cualquier línea fuera de un bloque, es `// ERROR(código) @COL` (en esa línea) o `@LINE:COL`, con un código de `E` o `W` y cuatro cifras.
:::

2. **Ponlos en su sitio:** `TestCheckSource` (`internal/syntax/source_check_test.go`), `TestParseHeader` y `TestRunner` (`internal/kitest/`), `TestWalkErrors` (`internal/kitest/runner_internal_test.go`), `TestReportAll` (`internal/driver/session_test.go`), subtests nuevos de `TestCLI`, y T03.57–T03.60, ficheros `.ki` con su cabecera (más abajo). Cambia también el `want` de T01.04 a `commands: --version, version, check, test`.
3. **Copia tal cual `func ptr[T any](v T) *T { return &v }`** en `header_test.go`. Es **genérica** (capítulo 4): `ptr(101)` da un `*int`, para los campos opcionales de un `Spec`.
4. **Míralos fallar.** Los paquetes nuevos ni compilan:

```console
$ go test ./...
github.com/aldezex/kilang/internal/syntax: no non-test Go files in …/internal/syntax
github.com/aldezex/kilang/internal/kitest: no non-test Go files in …/internal/kitest
# github.com/aldezex/kilang/internal/kitest [github.com/aldezex/kilang/internal/kitest.test]
internal/kitest/runner_internal_test.go:27:14: undefined: unreadableDirectory
…
FAIL	github.com/aldezex/kilang/internal/kitest [build failed]
FAIL	github.com/aldezex/kilang/internal/syntax [build failed]
…
```

#### La comprobación previa al léxico

{{file ch03-s3 internal/syntax/source_check.go}}

`CheckSource` busca el primer byte inválido y después los controles, solo **antes** de él: si el inválido va primero, gana.

{{reftest T03.33}}

#### La sesión, `ki check` y la tabla de comandos

{{diff ch03-s2..ch03-s3 internal/driver/session.go}}

`ReportAll` escribe en un `strings.Builder` e ignora el error de escritura: stderr es el último sitio donde contarlo.

{{diff ch03-s2..ch03-s3 internal/driver/commands.go}}

{{file ch03-s3 internal/driver/check.go}}

`--diagnostics=human|short|json` elige el formato. Los flags van antes del fichero, porque `flag` para en el primer argumento que no lo es; con el flag detrás, sale con 2. Un fichero ilegible sale con 1: la línea de órdenes estaba bien.

:::more `CheckSource` y `ki check` por dentro: `&s.Sources`, `newCommand`, `parseFlags` y `fs.String`
- `DiagFormat` es otro conjunto cerrado con `iota`. `Sources` y `Sink` son valores dentro de la `Session`, y `&s.Sources` da la dirección del campo a quien la necesita.
- En la condición de `CheckSource`, `&&` liga más que `||`: se lee `(b < 0x20 && b != '\t' && b != '\n' && b != '\r') || b == 0x7F`.
- El `switch` de `render` es sobre un entero, no un type switch, pero termina igual, en un panic con el valor (`driver: unknown diagnostic format 9`, T03.65).
- `ReportAll` ordena, deja una línea en blanco entre dos diagnósticos en formato humano y añade la nota de truncado si hizo falta.
- El mensaje de `CheckSource` escribe el byte con `%04X` (`U+0001`) y la etiqueta con `%x` (`\u{1}`), como se escribe un escape en Ki. Busca los controles con un bucle sobre los bytes: son UTF-8 válido, pero un byte menor que 0x80 nunca aparece dentro de una secuencia de varios bytes.
- `newCommand` generaliza el `versionCommand` del capítulo 1: envuelve la función del comando en el closure que captura su `command`. `parseFlags` devuelve `(code, done)`: si `done`, la línea de órdenes ya tiene respuesta (la ayuda o un error de uso) y el comando devuelve `code`.
- `fs.String(…)` registra un flag de texto y devuelve un `*string`, que se lee con `*diagnostics` tras `Parse`.
- El `case 1:` vacío casa y no hace nada: un `case` de Go no cae al siguiente.
- `ki check x.ki --diagnostics=short` responde `ki check: unexpected argument "--diagnostics=short"`.
:::

#### La cabecera de los golden

{{file ch03-s3 internal/kitest/header.go}}

- `ParseHeader` anota el BOM y el `\r\n` para que `ki test --update` (capítulo 5) reescriba el fichero igual.
- Una clave repetida es un error: con dos `// run:` ganaría la segunda sin avisar.

:::more `header.go` por dentro: `splitKey`, `seen`, `setKey` y `args...`
- El BOM y el `\r\n` se miran solo en la primera línea.
- `splitKey` usa `strings.CutPrefix`, que quita un prefijo y dice si estaba, y `strings.Cut`, que parte por la primera aparición de un separador y devuelve antes, después y si lo encontró.
- `seen` es un `map[string]bool`, y `seen[key]` de una clave ausente da el valor cero, `false`.
- `setKey` tiene receptor puntero porque rellena el `Spec`, y su `switch` termina en `default: panic("unreachable")`: `splitKey` ya filtró las claves. `strconv.Atoi` lee un entero y `strings.Fields` parte por espacios.
- `outputValue` lee las dos formas de una salida que da la gramática. La sangría tras `// | ` es texto: un diagnóstico humano empieza con espacios (T03.36).
- `setPosition` lee `@COL` o `@LINE:COL` con `strconv.ParseUint(s, 10, 32)`, que rechaza lo que no cabe en 32 bits.
- `headerError(format string, args ...any)` reenvía sus argumentos con `args...`.
:::

{{reftest T03.36}}

#### El runner

{{file ch03-s3 internal/kitest/runner.go}}

- **`Exec`**, un campo función: cómo se ejecuta `ki`, en proceso desde `go test`, como subproceso desde `ki test`.
- **`enterRoot`** entra en la raíz y devuelve un closure, `restore`, que quien llama difiere con `defer restore()`.
- **`annotationDifferences`** compara las anotaciones del fichero con los errores de una segunda ejecución con `--diagnostics=json`, como multiconjuntos (dos errores iguales son dos): lo que falta en la ejecución es un `missing`; lo que sobra, un `unexpected`.
- **`RunPaths`** tiene resultados con nombre y un `defer` que los escribe. `return 0, 1` los asigna **antes** de que corran los `defer`: el resumen es correcto también al fallar.
- **`kiFiles`** da como fallo un directorio sin ningún `.ki`: «no se ejecutó nada» no es «todo pasa».

Cambia el `defer restore()` de `RunPaths` por `_ = restore` (si solo lo borras, no compila). El proceso queda dentro de un directorio temporal que `t.TempDir` borra, y fallan una decena de subtests, casi todos lejos de la causa:

```text
--- FAIL: TestRunner (0.00s)
    --- FAIL: TestRunner/exit_code_diff_first (0.00s)
        runner_test.go:144: exit code first, then stdout: got ok=false and "FAIL c.ki\n- cannot enter the root `/tmp/…/001`: getwd: no such file or directory\n", want …
```

Con `-run 'TestRunner/restores_cwd'`, T03.49 lo dice en una línea:

```text
runner_test.go:270: the working directory is "/tmp/…/001" after RunPaths, was "…/internal/kitest"
```

Deshaz el cambio.

{{reftest T03.48}}

{{reftest T03.49}}

#### `ki test` y los dos modos

{{file ch03-s3 internal/driver/test.go}}

`ki test` ejecuta cada golden como subproceso de su propio ejecutable (`os.Executable()`). Bajo `go test`, ese es el binario de **test**; por eso existe `testExec`, que solo los tests cambian.

{{reftest T03.66}}

El mismo `Runner` corre en proceso desde `tests/golden_test.go`:

{{file ch03-s3 tests/golden_test.go}}

`t.Run(name, …)` recibe un nombre calculado, la excepción a la regla del literal: un subtest por fichero, que `bookx` enlaza con su tarjeta.

:::more El runner y `ki test` por dentro: `runHere`, `cmp.Or`, `slices.Values`, `outWriter` y `ExecSubprocess`
**`runHere`.** Monta la orden con `append(slices.Clone(spec.Run), spec.Args...)` y clona otra vez antes de añadir el fichero en cada ejecución: los dos `append` sobre una misma base del capítulo 1. El código de salida por omisión sale de `slices.ContainsFunc`. `firstDifference` parte las salidas con `strings.SplitAfter`, que conserva el `\n` de cada línea, y `showLine` lo quita con `strings.CutSuffix`.

**`annotationDifferences`.** Lee los errores con `json.Unmarshal` sobre un struct anónimo, ordena los dos lados y los recorre a la vez. `cmp.Compare` da -1, 0 o 1, y `cmp.Or` el primero de sus argumentos que no es cero: «por línea; si empatan, por columna; si no, por código». `got` es suyo y se ordena en el sitio; `want` es del `Spec` de quien llama, así que `slices.SortedFunc(slices.Values(want), …)` ordena una copia (`slices.Values` da un *iterador* sobre el slice, y `SortedFunc` lo recoge en uno nuevo).

**`enterRoot`, `write` y `kiFiles`.** `enterRoot` guarda el directorio antes de entrar. `write` escribe la línea `PASS`/`FAIL`, vacía la salida si sabe (la aserción de la teoría) y después las diferencias. `kiFiles` devuelve los `.ki` ordenados, y también da como fallo un subdirectorio ilegible.

**`tests/golden_test.go`.** `Main` crea una `Session` nueva en cada llamada, así que ningún caso ve el estado de otro. `filepath.Rel` y `filepath.ToSlash` dan la ruta relativa a la raíz y con barras `/` también en Windows, la forma que muestra el runner. El subtest se llama `TestKi/errors/ch03/invalid_utf8` y su tarjeta tiene de `ref` `tests/errors/ch03/invalid_utf8.ki`.

**`outWriter`.** El runner escribe sin mirar errores, y `outWriter` recuerda el primero para que `runTest` salga con 1 si el informe se perdió (T03.67).

**`ExecSubprocess`.** Conecta los `io.Writer` a `cmd.Stdout` y `cmd.Stderr` y devuelve `cmd.ProcessState.ExitCode()`; si el programa no arrancó, `ProcessState` es `nil` y devuelve -1.
:::

#### Los golden y la CI

El 0xFF y el 0x01 se escriben con `printf`, no con el editor; la cabecera lleva U+FFFD:

```console
$ printf '// test: T03.57\n// run: check\nlet x = \xff  // ERROR(E0101) @9\n' > tests/errors/ch03/invalid_utf8.ki
$ printf '// test: T03.58\n// run: check\n// expect-exit: 1\n// expect-stderr:\n// | error[E0101]: invalid UTF-8 byte 0xFF\n// |   --> tests/errors/ch03/invalid_utf8_render.ki:10:9\n// |    |\n// | 10 | let x = \xef\xbf\xbd\n// |    |         ^ not valid UTF-8\nlet x = \xff\n' > tests/errors/ch03/invalid_utf8_render.ki
$ printf '// test: T03.59\n// run: check\nlet x = 1\x01  // ERROR(E0102) @10\n' > tests/errors/ch03/control_char.ki
```

{{file ch03-s3 tests/errors/ch03/invalid_utf8.ki}}

{{file ch03-s3 tests/errors/ch03/invalid_utf8_render.ki}}

{{file ch03-s3 tests/errors/ch03/control_char.ki}}

{{file ch03-s3 tests/errors/ch03/valid_file.ki}}

Dos marcan su error en línea; `invalid_utf8_render.ki` fija el texto entero; `valid_file.ki` es el caso «sí»: sin él, un `check` que siempre falla los pasaría. `ci-local.sh` los ejecuta también por el binario:

{{diff ch03-s2..ch03-s3 ci/ci-local.sh}}

Si te atascas o tu versión se desvía demasiado, copia el fichero de `ref/ch03-s3` y sigue.

Al terminar: tus tests en verde, commit y etiqueta `ch03-s3` (mensaje: `ch03-s3: source check, ki check and the golden runner`); compárate con `ref/ch03-s3`.

## Ejecútalo

Desde la raíz de tu repo, con la sesión 3 terminada y `ki` construido (`go build -o build/bin/ki ./cmd/ki`); `ci/ci-local.sh` acaba con las mismas líneas de `ki test` y `verify: OK`:

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

```console
$ printf '// test: T03.76\n// run: check\n// args: --diagnostics=short\n// expect-exit: 1\n// expect-stderr: tests/errors/ch03/short_format.ki:6:9: error[E0101]: invalid UTF-8 byte 0xFF\nlet x = \xff\n' > tests/errors/ch03/short_format.ki
```

`ki test --root . tests` da `5 passed, 0 failed`. Con `6:8` en la expectativa falla con `- …:6:8: …` y `+ …:6:9: …`; sin la línea `args:`, con `+ error[E0101]: invalid UTF-8 byte 0xFF`.
:::

:::exercise
**4. Sin límite.** Imagina un `CheckSource` que informara de cada byte inválido, sobre un fichero de 100 000 bytes 0xFF en una sola línea. ¿Qué pasaría con el límite de 100 de `Report`? ¿Y sin él?
:::

:::solution
Cada diagnóstico humano muestra una ventana de la línea, no la línea entera (T03.22), así que su tamaño no depende del fichero: unos 350 bytes. La referencia imprime uno solo. Medido con esa variante: con el límite de 100, unos 34 KB de stderr; con un límite de 1000, unos 375 KB; sin límite, 38 MB en unos 20 s y unos 150 MB de memoria, porque `ReportAll` construye toda la salida en un `strings.Builder` antes de escribirla. `MaxErrors` protege de cualquier pase que se desboque; `CheckSource` ni llega a él, porque en un fichero que no es texto basta el primer byte malo.
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

`goldenDir` y `runCLI` son ayudantes de `cli_test.go` en `ref/ch03-s3`: el primero crea un directorio temporal con `pass.ki`, que pasa, y `fail.ki`, que falla. La segunda fila exige que no se ejecute nada aunque la ruta mala sea la última, y `runGoldensInProcess` evita que, sin la comprobación, `pass.ki` lance el binario de test. Sin el `if`, las dos filas fallan con código 1 y `FAIL nope`.
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

- Un error del programa es un `Diagnostic` que los pases entregan a un `Sink`, que se detiene tras 100 errores.
- `iota` numera constantes de un tipo definido; Go no cierra el conjunto: cubre el valor imposible.
- `v, ok := w.(T)` pregunta por un tipo, y un puntero `nil` dentro de una interfaz no es una interfaz `nil`.
- La interfaz sellada no da exhaustividad: cada type switch termina en `default: panic("unreachable")`.
- `errors.Join` reúne errores; `(T, error)`, los punteros y el valor cero hacen de `Result` y `Option`.
- El caret va en columnas visuales, con sustitutos de ancho conocido para lo invisible.
- Los golden de `testdata/` se regeneran con `-update` y después se leen.
- El runner aplica una cabecera estricta, compara posiciones si hay anotaciones y restaura el directorio.

## Go en el trabajo

**Errores como valores.** La FAQ de Go («Why does Go not have exceptions?») dice que acoplar las excepciones a `try-catch-finally` «results in convoluted code», y que empuja a tratar como excepcionales errores corrientes, «such as failing to open a file».

**Envolver en las fronteras.** «Working with Errors in Go 1.13» (blog de Go, 2019): «Wrap an error to expose it to callers. Do not wrap an error when doing so would expose implementation details». La práctica: envolver una vez, en la frontera del paquete, y comprobar con `errors.Is` o `errors.As`.

**Golden en el repo de Go.** `src/cmd/gofmt/testdata/` tiene 31 parejas `.input`/`.golden` (Go 1.27.3), y `gofmt_test.go` declara el flag de este capítulo.

:::more El blog de Go sobre `%w`, y cómo `gofmt` lleva sus opciones y comprueba la idempotencia
**«Working with Errors in Go 1.13».** Lo firman Damien Neil y Jonathan Amsterdam, y da el motivo: «wrapping an error makes that error part of your API». `Load` envuelve a propósito, para que quien carga un fichero pregunte `errors.Is(err, fs.ErrNotExist)`. Cada paquete envuelve con el contexto que solo él conoce, y quien comprueba nunca compara texto.

**Los golden de `gofmt`.** El flag es `var update = flag.Bool("update", false, "update .golden files")`. Un `.input` que necesita opciones las lleva en un comentario `//gofmt flags` de sus primeras 20 líneas (`//gofmt -s`; 21 de los 31 tienen uno), como la cabecera de Ki. `TestRewrite` comprueba además la idempotencia: formatea el `.golden` y exige que no cambie.
:::
