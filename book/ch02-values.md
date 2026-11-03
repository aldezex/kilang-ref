---
chapter: 2
title: Valores, punteros, slices y el recolector
part: I
hours: 8
milestone: SourceMap guarda los ficheros en memoria; la búsqueda de línea y columna está testeada
---

## Objetivo

Al terminar tendrás `internal/base`, el primer paquete del compilador. Su `SourceManager` (el «SourceMap» del hito) guarda cada fichero en memoria y convierte un offset de byte en línea y columna. `ki` no cambia; ejecutas los tests de `internal/base`:

```console
$ go test -count=1 ./internal/base/
ok  	github.com/aldezex/kilang/internal/base	0.003s
```

En el capítulo 3, `ki check` dibujará con él su primer error, camino del hito M1.

## Teoría

### El problema: de un byte a `main.ki:3:9`

Cada token (capítulo 5) recuerda dónde está como un **offset de byte**: cortar su texto es indexar. Una persona quiere leer `main.ki:3:9`. Tres decisiones:

- **Spans semiabiertos.** Un `Span` es un trozo de fichero, `[Lo, Hi)`: la longitud es `Hi - Lo` y el span vacío, el del fin de fichero, es `Lo == Hi`. Con `Hi` inclusivo, el vacío no se podría escribir.
- **Tabla de líneas perezosa.** Guarda dónde empieza cada línea y se construye en la primera consulta: un fichero sin errores nunca la paga.
- **Columnas en code points** (caracteres Unicode). En `é=1`, el `=` está en el byte 2 pero es el segundo carácter.

Un fichero real trae tres casos:

- `\r\n` es **un** salto; un `\r` suelto **no** termina la línea.
- El BOM (`EF BB BF`) se quita al leer y se recuerda.
- Un fichero de 4 GiB o más se rechaza antes de leerlo, porque los offsets son `uint32`.

`SourceManager` guarda cada fichero en un `file`, en `files`; `Text(sp)` da el texto de un span.

### Valores y copias

Asignar copia el valor entero, y un struct es un valor (en Python y en TS, `b = a` comparte el objeto):

```go
type Point struct{ X, Y int }

a := Point{1, 2}
b := a  // una copia
b.X = 9 // a sigue siendo {1 2}
```

Un array (`[3]int`, tamaño fijo) se copia entero, y pasar un argumento también copia. La copia es superficial: un struct con un slice dentro comparte su array.

### Tipos definidos y structs

- **Tipos definidos.** `type FileID uint32` crea un tipo nuevo cuyo tipo subyacente es `uint32`.
- **Conversiones.** Go no convierte entre tipos numéricos por su cuenta. Hace falta `FileID(n)`, salvo con una constante sin tipo, que toma tipo donde se usa (`id + 1`). `m.files[id]` compila porque para indexar vale cualquier entero.
- **Literales.** `LineCol{Line: 1, Col: 1}` nombra los campos; `LineCol{1, 1}` los da en orden.
- **Comparación.** `==` compara structs campo a campo, si todos son comparables. En la cabecera de un `if`, el literal va entre paréntesis, `(LineCol{1, 1})`: el parser tomaría la `{` por el bloque.

:::from Rust
Un struct se comporta como uno con `#[derive(Clone, Copy)]`, y `type FileID uint32` es un newtype que conserva los operadores.
:::

### Punteros sin aritmética

`&x` es la dirección de `x` y `*p` el valor apuntado; en `p.X` Go desreferencia por ti. Un puntero comparte en lugar de copiar.

No hay aritmética (`p + 1` no compila). Una función puede devolver la dirección de una local: el compilador la pone en el heap.

:::from Rust
Un `*T` es un `&mut T` sin tiempo de vida ni exclusividad; lo libera el recolector.
:::

### Receptores por valor y por puntero

El receptor (capítulo 1) es un parámetro más: por valor, el método recibe una **copia**.

```go
type Counter struct{ n int }

func (c Counter) IncVal()  { c.n++ } // cambia la copia
func (c *Counter) IncPtr() { c.n++ } // cambia el original
```

Con `var c Counter`, `c.IncPtr()` es `(&c).IncPtr()`: Go toma la dirección por ti, porque una variable la tiene. Un elemento de un mapa (`map[K]V`, la tabla hash de Go) no la tiene: `counters["a"].IncPtr()` no compila.

La regla del libro: por puntero si el método modifica el valor o el struct es grande; por valor si es pequeño e inmutable, como `Span`. Si un método necesita puntero, todos lo usan.

### Las entrañas de un slice

Un slice es una **cabecera** de tres palabras: un puntero a un array, una longitud (`len`) y una capacidad (`cap`, cuántos elementos caben hasta el final del array). Copiarlo copia la cabecera, no el array, y cortarlo da una vista (en Python, una copia).

`s := make([]byte, 5, 100)` crea un array de 100 bytes y una cabecera de longitud 5. Indexar comprueba la longitud y cortar, la **capacidad**: `s[5]` falla, pero `s[:6]` es válido.

**`append`.** Con capacidad libre, `append(s, x)` escribe en el array y devuelve una cabecera más larga sobre **el mismo array**. Sin ella, reserva un array mayor (cuánto es un detalle de la implementación), copia y devuelve una cabecera sobre él.

Reutilizar es eficiente, y es también el **aliasing**:

```go
base := make([]int, 3, 10)
b := append(base, 1)
c := append(base, 2)
fmt.Println(b, c) // [0 0 0 2] [0 0 0 2]
```

Los dos escriben en la posición 3: el segundo pisa al primero. Con capacidad 3, cada `append` reservaría su propio array.

**La expresión de corte completa.** `a[i:j:k]` es `a[i:j]` con capacidad `k - i`. Con `s[:n:n]` (o `slices.Clip(s)`) la capacidad es la longitud, y el siguiente `append` tiene que copiar.

`copy(dst, src)` copia `min(len(dst), len(src))` elementos; si el destino es `[]byte`, el origen puede ser un `string`.

**Por qué `SourceManager` guarda `[]*file` y no `[]file`.** `f := &files[i]` apunta a un elemento **del array actual**. Tras un `append` que realoja, `files[i]` vive en el nuevo, y lo que escribas por `f` se pierde sin error.

Con `[]*file`, `append` copia los punteros y cada `file` se queda donde está (ejercicio 2; en el capítulo 4, `IdVec` cambia este diseño).

### `string` frente a `[]byte`

Un `string` es una secuencia **inmutable** de bytes, y no garantiza UTF-8 válido: de ahí `FirstInvalidUTF8`. `len` cuenta bytes (`len("é")` es 2), indexar da un byte, y `for i, r := range s` recorre code points, con `U+FFFD` por cada byte inválido.

Ese `r` es un `rune`, un alias de `int32`. `'x'` es una constante *rune* sin tipo: por eso `c == '\n'` compila con `c` de tipo `byte`.

`string(b)` y `[]byte(s)` **copian**: si compartieran el array, cambiar el `[]byte` cambiaría un `string` inmutable. Por eso `Text` devuelve un `string`: quien lo recibe no puede tocar el fichero.

### Leer un fichero y envolver errores

`Load` debe poder rechazar un fichero sin leerlo y distinguir por qué falló.

`os.Stat(path)` da un `fs.FileInfo`, cuyo `Size()` da la longitud como `int64` sin leer nada; `os.ReadFile(path)` lee el fichero entero. Los dos fallan con un `*fs.PathError`, que guarda operación, ruta y causa.

- **Un centinela**, como `ErrTooLarge`, es una variable de paquete creada con `errors.New` y comparada por identidad.
- **`%w`** en `fmt.Errorf` envuelve y guarda la causa; con `%v`, la causa se pierde.
- **`errors.Is`** recorre la cadena de envoltorios: `errors.Is(err, fs.ErrNotExist)` distingue «no existe» de «sin permiso» sin mirar el texto.
- **`errors.As`** (capítulo 1) busca un **tipo**.

:::from Rust
`%w` es el `source()` de `std::error::Error`.
:::

### `panic` y `recover`

La regla del libro: la entrada del usuario, también un programa mal escrito, da un `error`. Un bug del compilador, como pedir la línea 0, da un `panic(v)`.

`panic` corta la función y sube por las que la llamaron, ejecutando sus llamadas diferidas. Si nadie lo detiene, el programa imprime el valor y la pila, y sale con código 2.

`recover()` detiene el panic en curso y devuelve su valor, un `any` (de cualquier tipo). Pero solo cuando la llama directamente una función diferida; si no, devuelve `nil`.

`recover` vive en los bordes: un test, un servidor que no debe caerse por una petición.

### UTF-8 con `unicode/utf8`

UTF-8 codifica cada code point en 1 a 4 bytes. `utf8.DecodeRune(b)` decodifica la primera secuencia y devuelve el code point y cuántos bytes ocupa:

| Entrada | Resultado | Por qué |
|---|---|---|
| `"\xEF\xBF\xBD"` | `U+FFFD`, 3 | el carácter de sustitución, escrito de verdad |
| `"\xff"` | `U+FFFD`, 1 | `FF` no aparece nunca en UTF-8 |
| `"\xC0\xAF"` | `U+FFFD`, 1 | *overlong*: `/` escrito con dos bytes |
| `"\xE2\x82"` | `U+FFFD`, 1 | secuencia cortada |
| vacía | `U+FFFD`, 0 | no hay nada |

`utf8.RuneError` es `U+FFFD`, y solo el tamaño distingue un byte inválido de un U+FFFD legítimo. `utf8.RuneCount(b)` cuenta cada byte inválido como un code point: es la regla de columnas de Ki.

### Búsqueda binaria: `slices.BinarySearch` y `sort.Search`

La tabla de líneas de `"ab\ncd\nef"` es `[0 3 6]`. `slices.BinarySearch(lines, off)`, genérica para cualquier tipo ordenado, devuelve la posición de `off` y `true`, o dónde habría que insertarlo y `false`:

| `off` | 0 | 1 | 3 | 4 | 6 | 7 |
|---|---|---|---|---|---|---|
| `BinarySearch` | 0, true | 1, false | 1, true | 2, false | 2, true | 3, false |
| línea | 1 | 1 | 2 | 2 | 3 | 3 |

Encontrado en `i`, el offset es el inicio de la línea `i+1`. Si no, el punto de inserción está una posición más allá de la línea, y como las líneas cuentan desde 1, `i` **es** la línea.

`sort.Search(n, f)`, la forma antigua, da el menor `i` con `f(i)` verdadero; si `f` es `lines[i] > off`, ese `i` es la línea. El código usa `slices.BinarySearch`.

### Qué hace el recolector y de qué no te libra

El recolector marca lo alcanzable, libera el resto y **no mueve los objetos del heap**: un puntero vale mientras exista. No hay `free` ni punteros colgantes. De lo que no te libra:

- **El aliasing**: para el recolector, todo es memoria viva.
- **Las referencias obsoletas**: el `&files[i]` de antes apunta al array viejo, que vive **porque** lo apuntas.
- **Las vistas que retienen arrays grandes**: una cabecera mantiene vivo el array entero. `SourceManager` guarda el fichero entero a propósito, y `Text` devuelve una copia.

:::more 100 MiB retenidos por 9 bytes
Un programa crea 100 MiB con `bytes.Repeat`, se queda con la primera línea (`data[:9]`, una vista) y suelta `data`. Tras un `runtime.GC()`, sigue teniendo 100 MiB en el heap (lo dice `runtime.ReadMemStats`).

Con `bytes.Clone(keep)`, que copia los 9 bytes a un array propio, 0 MiB.
:::

### Análisis de escape: `-gcflags=-m`

El compilador decide si una variable va en la pila o en el heap: si su dirección puede sobrevivir a la función, *escapa*. `go build -gcflags=-m` imprime esas decisiones.

:::from Rust
El borrow checker **rechaza** devolver una referencia a una local; el análisis de escape solo pregunta «¿dónde lo pongo?».
:::

## Tests primero

{{cards}}

Úsalas como en el capítulo 1: test en rojo, código, y solo entonces la referencia. Las entradas de cada tarjeta, en `git show ref/ch02-s3:book/tests/ch02.tests`.

## Implementación

### Sesión 1: ficheros fuente y spans (≈ 3 h)

#### Los tests, en rojo

1. **Escribe T02.01–T02.07** en `TestSourceManager` (`internal/base/source_test.go`). Las piezas de T02.05 y T02.07, en el pliegue. La interfaz:

```go
type FileID uint32

type Span struct {
	File   FileID
	Lo, Hi uint32
}

func (m *SourceManager) Add(displayPath string, contents []byte) FileID
func (m *SourceManager) DisplayPath(id FileID) string
func (m *SourceManager) Contents(id FileID) []byte
func (m *SourceManager) Text(sp Span) string
func (m *SourceManager) Size(id FileID) uint32
```

2. **Usa `package base`,** no el externo: varios tests de la sesión 3 tocan nombres sin exportar.
3. **Empieza cada subtest por `var m SourceManager`.** El valor cero ya sirve: `append` sobre un slice `nil` funciona.
4. **Míralos fallar.** No compilan. Tus líneas serán otras:

```console
$ go test ./internal/base/
# github.com/aldezex/kilang/internal/base [github.com/aldezex/kilang/internal/base.test]
internal/base/source_test.go:7:9: undefined: SourceManager
internal/base/source_test.go:23:9: undefined: SourceManager
…
internal/base/source_test.go:80:9: too many errors
FAIL	github.com/aldezex/kilang/internal/base [build failed]
FAIL
```

:::more Las piezas de los tests de capacidad
Hace falta un fichero con capacidad de sobra, como un buffer leído de disco: `src := make([]byte, 5, 100)` y `copy(src, "hello")`.

Para mirar más allá de la longitud, `src[:6][5]`: si sigue valiendo 0, nadie escribió en el array del fichero.

Para comprobar un panic, `recover()` en una función diferida, `defer func() { … }()`: sin él, el panic acaba con el binario de test entero.

Como `defer` actúa al salir de la función, cada fila va en una función literal que se llama en el acto: `func() { … }()`. Así, el panic de una fila no corta el bucle.

`%+v` imprime un struct con los nombres de sus campos.
:::

#### El código

{{file ch02-s1 internal/base/source.go}}

Offsets `uint32`: un `Span` ocupa 12 bytes, frente a 24 con `int`, y habrá uno por token y por nodo.

`&file{…}` crea un `file` y da su dirección.

Quita el corte completo de `Add` y fallan los dos tests de capacidad:

```text
--- FAIL: TestSourceManager (0.00s)
    --- FAIL: TestSourceManager/contents_append_does_not_alias (0.00s)
        source_test.go:63: append to the result of Contents wrote 'X' into the file's array
    --- FAIL: TestSourceManager/text_past_end_panics (0.00s)
        source_test.go:101: one_past_end: Text({File:0 Lo:0 Hi:6}) did not panic
        source_test.go:101: far_past_end: Text({File:0 Lo:0 Hi:50}) did not panic
FAIL
FAIL	github.com/aldezex/kilang/internal/base	0.004s
FAIL
```

Deshaz el cambio y compara:

{{reftest T02.04}}

{{reftest T02.05}}

{{reftest T02.07}}

Si te atascas o tu versión se desvía demasiado, copia el fichero de `ref/ch02-s1` y sigue.

Al terminar: tus tests en verde, commit y etiqueta `ch02-s1` (mensaje: `ch02-s1: source files and spans`); compárate con `ref/ch02-s1`.

### Sesión 2: la tabla de líneas y `LineCol` (≈ 2,5 h)

#### Los tests, en rojo

1. **Escribe T02.08–T02.15** en `TestLineTable` (`internal/base/line_table_test.go`):

```go
type LineCol struct{ Line, Col uint32 } // both 1-based

func (m *SourceManager) LineCol(id FileID, offset uint32) LineCol
func (m *SourceManager) LineCount(id FileID) uint32
func (m *SourceManager) LineStart(id FileID, line uint32) uint32
```

2. **La columna cuenta bytes** desde el inicio de la línea, más uno; la sesión 3 la pasa a code points.
3. **Fuera de rango** es un panic con el mensaje exacto de su tarjeta. Comprueba antes los últimos valores válidos: un `>=` en lugar de `>` rechazaría el fin de fichero.
4. **Escribe los ayudantes** `newLineTableManager` y `mustPanic` (en el pliegue).
5. **Míralos fallar:**

```console
$ go test ./internal/base/
# github.com/aldezex/kilang/internal/base [github.com/aldezex/kilang/internal/base.test]
internal/base/line_table_test.go:36:21: m.LineCol undefined (type *SourceManager has no field or method LineCol)
internal/base/line_table_test.go:36:38: undefined: LineCol
…
FAIL	github.com/aldezex/kilang/internal/base [build failed]
FAIL
```

:::more Los ayudantes de la tabla de líneas
- **`newLineTableManager(text string) (*SourceManager, FileID)`** crea un manager con un fichero de relleno con otras líneas (`"\n\n\nxx\nyy\n"`), y después el del test. Devuelve el manager y el id del segundo: un método que ignorase su `FileID` respondería sobre el relleno.
- **`mustPanic(t *testing.T, name, want string, f func())`** ejecuta `f` y falla si no hace panic con exactamente `want`. `fmt.Sprint(r)` pasa a texto el `any` de `recover()`: para un `panic(fmt.Sprintf(…))`, el mensaje. `fmt.Sprintf` formatea como `Printf` pero devuelve el texto.

Un ayudante de un `_test.go` se usa desde los demás del paquete.
:::

#### El código

{{file ch02-s2 internal/base/line_table.go}}

- **`lineStarts`** tiene receptor puntero porque escribe `f.lines`, una caché perezosa; busca cada `\n` con `bytes.IndexByte` y avanza cortando, sin copiar.
- **`int64`** admite `uint32` e `int`, que no se comparan entre sí.

{{reftest T02.09}}

{{reftest T02.15}}

Si te atascas o tu versión se desvía demasiado, copia el fichero de `ref/ch02-s2` y sigue.

Al terminar: tus tests en verde, commit y etiqueta `ch02-s2` (mensaje: `ch02-s2: line table and LineCol`); compárate con `ref/ch02-s2`.

### Sesión 3: CRLF, code points, `LineText` y `Load` (≈ 2,5 h)

#### Los tests, en rojo

1. **Escribe T02.16–T02.43.** El `ref:` de cada tarjeta, en `git show ref/ch02-s3:book/tests/ch02.tests`, dice en qué `TestXxx` va; `load_test.go` y `utf8_test.go` son nuevos. Los ficheros de `Load`, en el pliegue. La interfaz nueva:

```go
func (m *SourceManager) LineText(id FileID, line uint32) string
func (m *SourceManager) HadBOM(id FileID) bool
func FirstInvalidUTF8(b []byte) (offset int, found bool)

const MaxFileSize = 1<<32 - 1
var ErrTooLarge = errors.New("file too large (limit 4 GiB)")
func (m *SourceManager) Load(realPath, displayPath string) (FileID, error)

var errNotRegular = errors.New("not a regular file")
var (
	stat     = os.Stat
	readFile = os.ReadFile
)
func checkFile(info fs.FileInfo) error
func checkSize(size int64) error
```

2. **Míralos fallar.** No compilan:

```console
$ go test ./internal/base/
# github.com/aldezex/kilang/internal/base [github.com/aldezex/kilang/internal/base.test]
internal/base/line_table_test.go:146:5: m.LineText undefined (type *SourceManager has no field or method LineText)
internal/base/load_test.go:38:16: m.Load undefined (type SourceManager has no field or method Load)
…
internal/base/load_test.go:141:15: too many errors
FAIL	github.com/aldezex/kilang/internal/base [build failed]
FAIL
```

:::more Los ficheros de los tests de `Load`
- **Reales.** `filepath.Join(t.TempDir(), name)` da una ruta en el directorio temporal del test y `os.WriteFile(path, data, 0o644)` lo escribe. `0o644` es un literal octal: los permisos.
- **4 GiB sin escribirlos.** `os.Truncate(path, 1<<32)` alarga un fichero vacío sin escribir datos, y el sistema de ficheros lo guarda *disperso*. `os.Chmod(path, 0)` le quita los permisos. Como `root` los ignora y Windows ignora `chmod 0`, el test se salta con `t.Skip` si `os.Geteuid()` es 0 o en Windows.
- **Falsos.** `stat` y `readFile` son variables para que un test ponga un fichero falso delante de `Load`. `fakeFileInfo` implementa la interfaz `fs.FileInfo` (seis métodos, de `Name` a `Sys`) con el modo y el tamaño que elige el test. `useFakeOS` cambia `stat` y `readFile` y los restaura con `t.Cleanup`, que corre al acabar el test.
:::

#### El código

{{diff ch02-s2..ch02-s3 internal/base/line_table.go}}

En `LineText`, `end > start` protege un fichero que empieza por `\n`: `end - 1` sería `0 - 1`. En `uint32` eso da la vuelta, y los enteros sin signo de Go no avisan.

{{diff ch02-s2..ch02-s3 internal/base/source.go}}

{{file ch02-s3 internal/base/utf8.go}}

Este `for` no tiene tercera cláusula: avanza uno para ASCII (`b[i] < utf8.RuneSelf`) y `size` para una secuencia. Devuelve un offset en **bytes**.

{{file ch02-s3 internal/base/load.go}}

`Load` pregunta a `stat` y a `checkFile` antes de leer, y a `checkSize` después, por si el fichero creció.

Aplica todo salvo el cambio de `col` y su `import`, y llega el rojo:

```console
$ go test ./internal/base/
--- FAIL: TestSourceManager (0.00s)
    --- FAIL: TestSourceManager/column_counts_code_points (0.00s)
        source_test.go:142: LineCol(2) = {1 3}, want {1 2}
FAIL
FAIL	github.com/aldezex/kilang/internal/base	0.003s
FAIL
```

Con la columna, verde. Los bytes inválidos ya pasaban: uno ocupa una columna.

{{reftest T02.20}}

{{reftest T02.24}}

{{reftest T02.27}}

{{reftest T02.33}}

{{reftest T02.41}}

{{reftest T02.42}}

Si te atascas o tu versión se desvía demasiado, copia el fichero de `ref/ch02-s3` y sigue.

Al terminar: tus tests en verde, commit y etiqueta `ch02-s3` (mensaje: `ch02-s3: CRLF, code points, LineText and Load`); compárate con `ref/ch02-s3`.

## Ejecútalo

```console
$ ci/ci-local.sh
ok  	github.com/aldezex/kilang/cmd/ki	0.009s
ok  	github.com/aldezex/kilang/internal/base	0.003s
ok  	github.com/aldezex/kilang/internal/driver	0.002s
ok  	github.com/aldezex/kilang/internal/toolchain	0.462s
ok  	github.com/aldezex/kilang/cmd/ki	2.036s
ok  	github.com/aldezex/kilang/internal/base	1.014s
ok  	github.com/aldezex/kilang/internal/driver	1.011s
ok  	github.com/aldezex/kilang/internal/toolchain	1.468s
verify: OK
```

## Ejercicios

:::exercise
**1. `Span.Join`.** Añade a `source.go` un método `func (a Span) Join(b Span) Span` que devuelva el span más pequeño que cubre los dos, con su test. ¿Qué hace con dos spans de ficheros distintos?
:::

:::solution
Receptor por valor, porque `Span` es pequeño e inmutable. Dos ficheros no tienen un span común, y mezclarlos es un fallo del compilador: panic. Los builtins `min` y `max` aceptan cualquier tipo ordenado.

```go
// Join returns the smallest span that covers a and b. Both must be in the
// same file: no span covers bytes of two files.
func (a Span) Join(b Span) Span {
	if a.File != b.File {
		panic(fmt.Sprintf("base: Join of spans in files %d and %d", a.File, b.File))
	}
	return Span{File: a.File, Lo: min(a.Lo, b.Lo), Hi: max(a.Hi, b.Hi)}
}
```

`source.go` necesita `import "fmt"`. El test, en `internal/base/span_test.go` (con `"fmt"` y `"testing"`):

```go
func TestSpanJoin(t *testing.T) {
	t.Run("covers_both", func(t *testing.T) {
		rows := []struct {
			name string
			a, b Span
			want Span
		}{
			{"in_order", Span{0, 2, 4}, Span{0, 7, 9}, Span{0, 2, 9}},
			{"reversed", Span{0, 7, 9}, Span{0, 2, 4}, Span{0, 2, 9}},
			{"nested", Span{0, 2, 9}, Span{0, 4, 5}, Span{0, 2, 9}},
			{"empty_at_end", Span{0, 2, 4}, Span{0, 12, 12}, Span{0, 2, 12}},
		}
		for _, row := range rows {
			if got := row.a.Join(row.b); got != row.want {
				t.Errorf("%s: %+v.Join(%+v) = %+v, want %+v", row.name, row.a, row.b, got, row.want)
			}
		}
	})

	t.Run("different_files_panic", func(t *testing.T) {
		defer func() {
			want := "base: Join of spans in files 0 and 1"
			if r := recover(); fmt.Sprint(r) != want {
				t.Errorf("panic = %v, want %q", r, want)
			}
		}()
		Span{0, 1, 2}.Join(Span{1, 1, 2})
	})
}
```

La versión ingenua, `Lo: a.Lo, Hi: b.Hi`, pasa `in_order` y falla en `reversed` (`{File:0 Lo:7 Hi:4}`, un span invertido) y en `nested` (`{File:0 Lo:2 Hi:5}`, que pierde el final de `a`).
:::

:::exercise
**2. Una referencia obsoleta.** En un módulo de prueba bajo `build/`, declara `file`:

```console
$ mkdir -p build/stale && cd build/stale && go mod init example.com/stale
```

```go
type file struct {
	path  string
	lines []uint32
}
```

Escribe un `main` que tome `f := &files[0]` sobre un `[]file`, haga un `append` y escriba después `f.lines`; imprime `files[0].lines` y `f == &files[0]`. Repite con un `[]*file`. Crea los slices con `make(…, 1, 1)`, para que el `append` realoje, y explica las dos salidas.
:::

:::solution
```go
package main

import "fmt"

type file struct {
	path  string
	lines []uint32
}

func main() {
	// Values: the slice holds the structs themselves.
	files := make([]file, 1, 1)
	files[0] = file{path: "a.ki"}
	f := &files[0] // points into the current array
	files = append(files, file{path: "b.ki"})
	f.lines = []uint32{0, 3} // writes into the old array
	fmt.Println("[]file: ", files[0].lines, f == &files[0])

	// Pointers: the slice holds addresses; the structs never move.
	ptrs := make([]*file, 1, 1)
	ptrs[0] = &file{path: "a.ki"}
	p := ptrs[0]
	ptrs = append(ptrs, &file{path: "b.ki"})
	p.lines = []uint32{0, 3}
	fmt.Println("[]*file:", ptrs[0].lines, p == ptrs[0])
}
```

`go run .` imprime:

```text
[]file:  [] false
[]*file: [0 3] true
```

Con `[]file`, el `append` no cabe en la capacidad 1: reserva un array de dos y **copia** el `a.ki`. `f` sigue apuntando al `a.ki` del array viejo, que el recolector mantiene vivo porque `f` lo alcanza. La tabla se escribe ahí y `files[0]` no la ve, sin crash ni aviso.

Con `[]*file`, el `append` copia el puntero, y `ptrs[0]` y `p` siguen siendo el mismo `file`. Es el caso de `SourceManager`: la tabla se escribe en la primera consulta, quizá con otros ficheros ya añadidos.
:::

:::exercise
**3. Medir la tabla.** Escribe en `internal/base/line_table_bench_test.go` un benchmark que compare `LineCol` con una búsqueda lineal (contar `\n` desde el principio) sobre un fichero de 10⁶ líneas. Mide también la primera consulta, la que construye la tabla.

Un benchmark es una `func BenchmarkXxx(b *testing.B)` con un bucle `for b.Loop() { … }`, que repite el cuerpo hasta tener una medida estable y no cuenta lo que haya antes. Separa los casos con `b.Run`, como `t.Run`. `-run '^$'` no ejecuta ningún test:

```console
$ go test -run '^$' -bench BenchmarkLineCol -benchmem ./internal/base/
```
:::

:::solution
```go
package base

import (
	"bytes"
	"testing"
	"unicode/utf8"
)

// linearLineCol finds the line and column of offset by walking the text from
// the start, with no table: the method that LineCol replaces.
func linearLineCol(text []byte, offset uint32) LineCol {
	line, start := uint32(1), 0
	for i, c := range text[:offset] {
		if c == '\n' {
			line++
			start = i + 1
		}
	}
	return LineCol{Line: line, Col: uint32(1 + utf8.RuneCount(text[start:offset]))}
}

func BenchmarkLineCol(b *testing.B) {
	text := bytes.Repeat([]byte("let x = 1\n"), 1_000_000) // a million lines
	offset := uint32(len(text) - 5)                        // inside the last line

	var m SourceManager
	id := m.Add("big.ki", text)
	if got, want := m.LineCol(id, offset), linearLineCol(text, offset); got != want {
		b.Fatalf("LineCol = %v, linear = %v", got, want)
	}

	b.Run("table", func(b *testing.B) {
		for b.Loop() {
			m.LineCol(id, offset) // the table already exists
		}
	})
	b.Run("first_query", func(b *testing.B) {
		for b.Loop() {
			var fresh SourceManager
			fid := fresh.Add("big.ki", text)
			fresh.LineCol(fid, offset) // builds the table, then searches
		}
	})
	b.Run("linear", func(b *testing.B) {
		for b.Loop() {
			linearLineCol(text, offset)
		}
	})
}
```

El `b.Fatalf` comprueba antes de medir que las dos versiones coinciden. Los números varían:

```text
BenchmarkLineCol/table-16         	67309237	        17.62 ns/op	       0 B/op	       0 allocs/op
BenchmarkLineCol/first_query-16   	     163	   7434389 ns/op	21096490 B/op	      39 allocs/op
BenchmarkLineCol/linear-16        	     518	   2432998 ns/op	       0 B/op	       0 allocs/op
```

- **Con la tabla hecha,** 18 ns: unos 20 pasos de búsqueda binaria, sin reservar memoria.
- **La lineal,** 2,4 ms: recorre 10 MB cada vez.
- **La primera consulta,** 7,4 ms y 21 MB en 39 asignaciones: la tabla crece por pasos hasta un millón de `uint32` (4 MB), y cada paso deja atrás un array.

Se amortiza en unas pocas consultas, y un fichero sin errores no la paga.
:::

:::exercise
**4. `Snippet`.** Añade `func (m *SourceManager) Snippet(sp Span) string`, que devuelva el texto de la línea donde empieza `sp` con el trozo del span entre `«»`: en `fn main() {}`, el span `[3, 7)` da `fn «main»() {}`. Un span que pase del final de su línea se corta ahí.

Escribe el test con una tabla y piensa qué mutante mata cada fila.
:::

:::solution
En `line_table.go`:

```go
// Snippet returns the text of the line where sp starts, with the bytes of the
// span inside «». A span that goes past the end of that line is cut there.
func (m *SourceManager) Snippet(sp Span) string {
	line := m.LineCol(sp.File, sp.Lo).Line
	text := m.LineText(sp.File, line)
	start := m.LineStart(sp.File, line)
	lo := min(int(sp.Lo-start), len(text))
	hi := min(int(sp.Hi-start), len(text))
	return text[:lo] + "«" + text[lo:hi] + "»" + text[hi:]
}
```

Los offsets pasan a ser relativos a la línea y se recortan a la longitud de su texto, que no lleva el salto: un span que apunta al `\n` o al `\r\n` no se sale del `string`.

El test, en `internal/base/snippet_test.go`:

```go
func TestSnippet(t *testing.T) {
	t.Run("marks_the_span", func(t *testing.T) {
		rows := []struct {
			name, text string
			lo, hi     uint32
			want       string
		}{
			{"first_line", "fn main() {}", 3, 7, "fn «main»() {}"},
			{"second_line", "a\nlet x = 1\n", 6, 7, "let «x» = 1"},
			{"empty_span", "ab\ncd", 4, 4, "c«»d"},
			{"cut_at_line_end", "ab\ncd", 1, 4, "a«b»"},
			{"crlf_line", "ab\r\ncd", 1, 4, "a«b»"},
			{"after_accent", "é = 1", 3, 4, "é «=» 1"},
		}
		for _, row := range rows {
			m, id := newLineTableManager(row.text)
			if got := m.Snippet(Span{File: id, Lo: row.lo, Hi: row.hi}); got != row.want {
				t.Errorf("%s: Snippet(%d, %d) = %q, want %q", row.name, row.lo, row.hi, got, row.want)
			}
		}
	})
}
```

Qué mata cada fila:

- `second_line`, a quien use offsets del fichero en lugar de la línea (`"let x «=» 1"`).
- `cut_at_line_end` y `crlf_line`, a quien no recorte `hi` (panic).
- `empty_span`, a quien no marque un span vacío.
- `after_accent`, a quien corte por columnas en lugar de bytes (`"é« »= 1"`).
:::

:::exercise
**5. Leer el análisis de escape.** Ejecuta `go build -gcflags=-m ./internal/base/` en tu repo y explica dos líneas: la que dice `&file{...} escapes to heap` en `source.go` y la que dice `moved to heap: pe` en `load.go`.
:::

:::solution
Con la referencia, entre otras:

```text
internal/base/source.go:45:28: &file{...} escapes to heap
internal/base/load.go:98:6: moved to heap: pe
internal/base/utf8.go:9:23: b does not escape
```

- **`&file{...} escapes to heap`**, en `Add`: el `file` nuevo se guarda en `m.files`, que vive más que la llamada, así que no puede ir en la pila.
- **`moved to heap: pe`**, en `loadError`: `var pe *fs.PathError` es local, pero `errors.As(cause, &pe)` recibe su dirección como `any`, y el compilador no puede probar que no la guarde.

La tercera dice que `FirstInvalidUTF8`, el bucle caliente, no retiene `b`.
:::

## Preguntas de repaso

:::review
¿Por qué `Add` recorta la capacidad del slice que recibe, si `Contents` lo devuelve sin copiar?
:::

:::answer
Porque el recorte permite no copiar sin riesgo: con `cap == len`, un `append` del llamador realoja y nunca escribe en el array del fichero. Y un corte más allá del final hace panic en lugar de devolver bytes sobrantes del buffer. Copiar costaría una asignación por llamada; recortar, nada.
:::

:::review
¿Por qué `SourceManager` guarda `[]*file` y no `[]file`?
:::

:::answer
Porque se escribe en los `file` después de crearlos (`hadBOM`, la tabla de líneas). Con `[]file`, un puntero a un elemento queda en el array viejo tras un `append` que realoja, y lo escrito se pierde sin ruido, porque el recolector mantiene vivo ese array. Con punteros, los `file` no se mueven.
:::

:::review
¿Por qué `LineCol` acepta un offset igual al tamaño del fichero y rechaza uno más?
:::

:::answer
El tamaño es la posición tras el último byte, donde apunta el fin de fichero, y un error «se esperaba `}`» ahí tiene que poder mostrarse. Uno más no es una posición de ningún fichero: solo sale de un fallo del compilador, y el panic lo descubre en el acto.
:::

:::review
¿Por qué `FirstInvalidUTF8` mira el tamaño que devuelve `utf8.DecodeRune` y no solo el code point?
:::

:::answer
Porque `DecodeRune` devuelve `utf8.RuneError` tanto para un byte inválido como para un U+FFFD escrito de verdad, y solo el tamaño los distingue: 1 frente a 3. Sin mirarlo, un `�` en un comentario daría un falso error de UTF-8 inválido (E0101).
:::

:::review
¿Por qué `Load` llama a `os.Stat` antes de `os.ReadFile`, y por qué el test del fichero de 4 GiB le quita los permisos?
:::

:::answer
Para rechazar el fichero sin leer 4 GiB: `Stat` da el tamaño sin abrirlo. El `chmod 0` hace comprobable ese orden: quien leyera primero fallaría con `fs.ErrPermission`, y solo quien mira el tamaño antes devuelve `ErrTooLarge`.
:::

:::review
¿Por qué `go build -gcflags=-m` no es el borrow checker de Go?
:::

:::answer
Porque no rechaza nada. El borrow checker prueba que no hay aliasing peligroso ni referencias colgantes, o el programa no compila. El análisis de escape solo decide dónde vive cada variable y te informa del coste: el aliasing de `append` y las referencias obsoletas compilan igual.
:::

## Resumen

- Asignar y pasar un argumento copian, y la copia es superficial.
- Un puntero comparte. Si un método modifica, receptor por puntero en todos.
- En un slice, indexar comprueba la longitud y cortar, la capacidad.
- `append` reutiliza o realoja: de ahí el aliasing y las referencias obsoletas. `a[i:j:k]` limita la capacidad.
- `string` es inmutable y no garantiza UTF-8; las conversiones con `[]byte` copian.
- `%w` envuelve, `errors.Is` busca un valor y `errors.As`, un tipo.
- La entrada del usuario da un `error`; un fallo del compilador, un panic.
- El recolector evita los punteros colgantes, no el aliasing ni las vistas que retienen arrays.
- `SourceManager`: spans semiabiertos, tabla de líneas perezosa, columnas en code points.

## Go en el trabajo

**Aliasing, en una entrevista.** Preguntan qué imprime el ejemplo de la teoría. Se evita con `slices.Clip(base)` o `append(slices.Clone(base), …)`.

**Vistas en una API pública.** Quien devuelve un slice decide qué puede hacer quien lo recibe: `bytes.Split` limita la capacidad de los trozos, y `bytes.Buffer.Bytes` documenta el aliasing como contrato.

**`string([]byte)`.** Cada conversión copia, salvo si el compilador demuestra que no hace falta: una búsqueda como `keywords[string(text)]` no paga.

**Rendimiento.** Ante una función caliente que reserva de más, empieza por `go build -gcflags=-m`; un benchmark con `-benchmem` dice si mereció la pena.
