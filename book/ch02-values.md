---
chapter: 2
title: Valores, punteros, slices y el recolector
part: I
hours: 8
milestone: SourceMap guarda los ficheros en memoria; la búsqueda de línea y columna está testeada
---

## Objetivo

Al terminar tendrás `internal/base`, el primer paquete del compilador: un `SourceManager` (el «SourceMap» del hito) que guarda en memoria cada fichero leído, le da un id y convierte un offset de byte en la línea y la columna que lee una persona. `ki` no cambia; lo que ejecutas al final son sus tests:

```console
$ go test -count=1 ./internal/base/
ok  	github.com/aldezex/kilang/internal/base	0.003s
```

Son 34 subtests que fijan los bordes: el fin de fichero, el `\r\n` de Windows, un `\r` suelto, una `é` de dos bytes, un byte que no es UTF-8, el BOM y un fichero de 4 GiB. En el capítulo 3, `ki check` los usará para dibujar su primer error, `fichero:línea:columna`, camino del hito M1.

## Teoría

### El problema: de un byte a `main.ki:3:9`

El lexer (capítulo 5) recorre el fichero byte a byte, y cada token recuerda dónde está como un **offset de byte**: cortar su texto es indexar, sin recorrer nada. Una persona, en cambio, quiere leer `main.ki:3:9`. Entre las dos cosas hay tres decisiones:

- **Spans semiabiertos.** Un `Span` es `[Lo, Hi)`: incluye `Lo` y excluye `Hi`. La longitud es `Hi - Lo`, un span vacío es `Lo == Hi` (el del fin de fichero), dos spans contiguos cumplen `a.Hi == b.Lo`, y `Hi` puede valer el tamaño del fichero. Con `Hi` inclusivo, el span vacío no se podría escribir.
- **Una tabla de líneas perezosa.** Se guarda el offset donde empieza cada línea y se busca en esa tabla con búsqueda binaria. Se construye la primera vez que alguien pregunta por una línea: un fichero sin errores nunca la paga.
- **Columnas en code points.** La columna cuenta caracteres Unicode, no bytes: en `é=1`, el `=` está en el byte 2 pero es el segundo carácter. Un byte que no es UTF-8 válido cuenta como una columna.

Y cuatro casos que trae un fichero real: el `\r\n` de Windows es **un** salto; un `\r` suelto **no** termina la línea; el BOM (los bytes `EF BB BF` que algunos editores ponen al principio) se quita al leer y se recuerda; y como los offsets son `uint32`, un fichero de 4 GiB o más se rechaza antes de leerlo.

El `SourceManager` guarda cada fichero en un `file` sin exportar, en su slice `files`: `Add` y `Load` lo guardan y le dan un id, `DisplayPath` da su ruta de pantalla, `Text(sp)` el texto de un span y `LineCol` la posición. Para escribirlo hay que saber cómo Go guarda y copia los datos.

### Valores y copias

En Go, asignar copia el valor entero, y un struct es un valor:

```go
type Point struct{ X, Y int }

a := Point{1, 2}
b := a  // una copia
b.X = 9 // a sigue siendo {1 2}
```

Un array (`[3]int`, de tamaño fijo) también se copia entero, y pasar un valor a una función lo copia: `func move(p Point) { p.X = 100 }` cambia su copia y quien llama no ve nada. Tres detalles que usa el capítulo:

- **Tipos definidos.** `type FileID uint32` crea un tipo nuevo cuyo tipo subyacente es `uint32`. Go no convierte entre tipos numéricos por su cuenta: hace falta `FileID(n)`, y con `line` de tipo `uint32`, `line > len(lines)` da `invalid operation: line > len(lines) (mismatched types uint32 and int)`. Un índice, en cambio, puede ser de cualquier tipo entero, así que `m.files[id]` no necesita `int(id)`.
- **Literales de struct.** `LineCol{Line: 1, Col: 1}` nombra los campos; `LineCol{1, 1}` los da en orden. `%v` imprime `{1 1}` y `%+v`, `{Line:1 Col:1}`.
- **Comparación.** Dos structs se comparan con `==`, campo a campo, si todos sus campos son comparables. En la cabecera de un `if`, el literal va entre paréntesis: `if got != LineCol{1, 1} {` da `syntax error: unexpected }, expected := or = or comma`, porque el parser toma la `{` por el inicio del bloque. Por eso los tests escriben `if got, want := m.LineCol(id, 0), (LineCol{1, 1}); got != want {`.

:::from Rust
Un struct de Go se comporta como uno con `#[derive(Clone, Copy)]`: asignar copia los bytes y el original sigue usable. Pero la copia es superficial aunque dentro haya un slice: las dos copias comparten su array. En Rust, un struct con un `Vec` no puede ser `Copy`, y `.clone()` copiaría los elementos.

`type FileID uint32` es un newtype como `struct FileID(u32)` que conserva los operadores de `uint32`, y una constante sin tipo se convierte sola: `id + 1` compila, pero `id + n` con `n uint32` da `mismatched types FileID and uint32`. En Rust, `id + 1` da ``error[E0369]: cannot add `{integer}` to `FileID` `` y `v[id]`, `error[E0277]`.
:::

### Punteros sin aritmética

`&x` es la dirección de `x` y `*p` el valor apuntado; con structs, en `p.X` Go desreferencia por ti. Un puntero comparte en lugar de copiar: `func movePtr(p *Point) { p.X = 100 }` cambia el original. Lo que no hay es aritmética: `p + 1` da `invalid operation: p + 1 (mismatched types *int and untyped int)`. Y una función puede devolver la dirección de una variable local: el compilador la pone en el heap.

:::from Rust
Un `*T` de Go no es `&T`, ni `&mut T`, ni `Box<T>`: se copia libremente, permite escribir desde cualquier copia, no tiene tiempo de vida y lo libera el recolector. Es un `&mut T` sin la regla de exclusividad, que es justo la que protege de los fallos de la sección de slices.
:::

### Receptores por valor y por puntero

El capítulo 1 presentó los métodos. El receptor es un parámetro más y se pasa igual: por valor, el método recibe una **copia**.

```go
type Counter struct{ n int }

func (c Counter) IncVal()  { c.n++ } // cambia la copia
func (c *Counter) IncPtr() { c.n++ } // cambia el original
```

Con `var c Counter`, `c.IncVal()` deja `c.n` en 0 y `c.IncPtr()` lo pone en 1. `c` no es un puntero, pero Go escribe `(&c).IncPtr()` por ti, porque una variable tiene dirección. Un elemento de un mapa no la tiene: `map[K]V` es la tabla hash de Go (`HashMap`, `dict`, `Map`), y con `counters := map[string]Counter{"a": {}}`, `counters["a"].IncPtr()` da `cannot call pointer method IncPtr on Counter`.

Las reglas del libro: receptor por puntero si el método modifica el valor o el struct es grande; por valor si es pequeño e inmutable, como `Span`; y si un método de un tipo necesita puntero, todos lo usan. Por eso todos los de `SourceManager` llevan `*SourceManager`, aunque `DisplayPath` solo lea.

:::from Rust
`self`, `&self` y `&mut self` se reducen a dos: por valor es `self` de un tipo `Copy`; por puntero hace a la vez de `&self` y de `&mut self`, sin exclusividad: Go no distingue un préstamo de lectura. El `(&c).IncPtr()` automático es el *autoref* de Rust.
:::

:::from Python
En Python y en TS un objeto es una referencia: `b = a` lo comparte, y `self` o `this` lo apuntan. En Go, `b := a` con un struct copia, y un receptor por valor no tiene equivalente allí.
:::

### Las entrañas de un slice

Un slice es una **cabecera** de tres palabras: un puntero a un array, una longitud (`len`) y una capacidad (`cap`, cuántos elementos caben hasta el final del array). Copiar un slice copia la cabecera, no el array.

`make([]byte, 5, 100)` crea un array de 100 bytes y una cabecera de longitud 5. Indexar comprueba la longitud y cortar, la **capacidad**: `s[5]` da `index out of range [5] with length 5`, pero `s[:6]` es válido y deja ver un byte más allá de la longitud. Es una trampa: sin recortar la capacidad, `Text(Span{id, 0, 50})` sobre un fichero de 5 bytes guardado en un array de 100 devuelve 50 bytes sin protestar.

**`append`.** Si queda capacidad, `append(s, x)` escribe `x` en el array y devuelve una cabecera más larga sobre **el mismo array**; si no, reserva un array mayor, copia los elementos y devuelve una cabecera sobre el nuevo. La especificación: «If the capacity of s is not large enough to fit the additional values, append allocates a new, sufficiently large underlying array [...]. Otherwise, append re-uses the underlying array». Cuánto crece es un detalle de la implementación que en Go 1.27 depende de dónde guarde el compilador el array mientras crece. Añadiendo uno a uno 2000 `int`, un slice local que al final se guarda en una variable global empieza en un buffer de 32 bytes de la pila, sube de uno en uno hasta llenarlo y luego pasa al heap: 1, 2, 3, 4, 8, 16, 32, 64, 128, 256, 512, 848, 1280, 1792 y 2560. Si el `append` escribe directamente en la global, el array está en el heap desde el principio y dobla: falta el 3. Un slice que no sale de su función estrena el buffer entero, 4 (`-gcflags=-m`, al final de la teoría, dice `append does not escape`). Desde 8 coinciden.

Que `append` reutilice el array es eficiente y es también el **aliasing**:

```go
base := make([]int, 3, 10)
b := append(base, 1)
c := append(base, 2)
fmt.Println(b, c) // [0 0 0 2] [0 0 0 2]
```

Los dos escriben en la posición 3 del mismo array, y el segundo pisa al primero. Con capacidad 3 no pasaría: cada `append` reservaría su propio array.

**La expresión de corte completa.** `a[i:j:k]` es `a[i:j]` con capacidad `k - i`. Con `s[:n:n]` la capacidad es la longitud, y el siguiente `append` está obligado a copiar: nadie escribe más allá de `n` a través de esa cabecera. `slices.Clip(s)` es exactamente `s[:len(s):len(s)]`. Un `k` mayor que la capacidad es un panic: `slice bounds out of range [::11] with capacity 10`.

**`copy`.** `copy(dst, src)` copia `min(len(dst), len(src))` elementos y devuelve cuántos. Su caso especial lo usan los tests: si el destino es `[]byte`, el origen puede ser un `string`, y `copy(src, "hello")` copia 5 bytes.

**Por qué `[]*file` y no `[]file`.** Con un `[]file`, `f := &files[i]` apunta a un elemento **del array actual**; tras un `append` que realoja, `files[i]` vive en el nuevo, y lo que escribas por `f` se pierde sin error. Con `[]*file`, `append` copia los punteros y cada `file` se queda donde está (ejercicio 2; en el capítulo 4, `IdVec` cambia este diseño).

:::from Rust
Es el caso que el borrow checker existe para rechazar. Con `let f = &mut files[0]; files.push(...); f.lines.push(3);`, rustc 1.97 responde ``error[E0499]: cannot borrow `files` as mutable more than once at a time``. Go lo compila, y como el recolector mantiene vivo el array viejo mientras `f` lo apunte, ni siquiera falla: escribe en una copia que nadie volverá a leer.
:::

:::from Python
Cortar una lista o unos `bytes` copia; la vista es `memoryview`: con `b = bytearray(b"abcd")`, `memoryview(b)[1:3][0] = 99` deja `b` en `bytearray(b'accd')`. En Go cortar siempre da una vista, así que el aliasing es lo normal. (En TS, `subarray` comparte memoria y `slice` copia.)
:::

### `string` frente a `[]byte`

Un `string` es una secuencia **inmutable** de bytes («once created, it is impossible to change the contents of a string», dice la especificación); `s[0] = 'H'` da `cannot assign to s[0] (neither addressable nor a map index expression)`. No tiene por qué ser UTF-8 válido: `"\xff"` es una cadena de un byte. `len` cuenta bytes (`len("é")` es 2), indexar da un byte, y `for i, r := range s` recorre code points, con `U+FFFD` por cada byte inválido: sobre `"a\xffb"`, `0:U+0061 1:U+FFFD 2:U+0062`. Ese `r` es un `rune`, un alias de `int32` para un code point, y `'x'` es una constante *rune* sin tipo: por eso `c == '\n'` compila con `c` de tipo `byte`.

`string(b)` y `[]byte(s)` **copian**, porque si compartieran el array, cambiar el `[]byte` cambiaría un `string` inmutable: con `bb := []byte("abc")` y `str := string(bb)`, después de `bb[0] = 'X'` `str` sigue siendo `abc`. Por eso `Text` devuelve un `string`: quien lo recibe no puede tocar el fichero.

:::from Rust
`string` se parece a un `Box<str>` inmutable y `[]byte` a un `Vec<u8>`, con una diferencia: `str` garantiza UTF-8 válido y un `string` de Go no garantiza nada. Por eso Ki necesita su propia comprobación, `FirstInvalidUTF8`. Lo mismo pasa con `rune` frente a `char`: `char::from_u32(0xD800)` es `None`, pero un `rune` admite cualquier `int32`, también `0xD800` o `-1`, que no son code points.
:::

:::from Python
Python separa texto (`str`) y bytes (`bytes`); Go solo separa inmutable y mutable. `len("é")` es 1 en Python y 2 en Go.
:::

### Leer un fichero y envolver errores

`os.ReadFile(path)` devuelve `([]byte, error)` con el fichero entero. `os.Stat(path)` devuelve un `fs.FileInfo`, una interfaz cuyo `Size()` da la longitud como `int64` sin leer nada. Los dos fallan con un `*fs.PathError`, que guarda operación, ruta y causa: `stat /no/such/dir/nope.ki: no such file or directory` desde `Stat`, el primero al que llama `Load`, y `open …` desde `ReadFile`. Y tres herramientas:

- **Un centinela** es una variable de paquete, `var ErrTooLarge = errors.New("file too large (limit 4 GiB)")`, comparada por identidad.
- **`%w`** en `fmt.Errorf` envuelve: ``fmt.Errorf("cannot read file `%s`: %w", path, cause)`` da el texto completo y además guarda `cause`. Con `%v` el texto es el mismo, pero la causa se pierde.
- **`errors.Is(err, target)`** recorre la cadena de envoltorios: con `%w`, `errors.Is(err, fs.ErrNotExist)` es verdadero; con `%v`, falso. Así se distingue «no existe» de «sin permiso» (`fs.ErrPermission`) sin mirar el texto. `errors.As`, del capítulo 1, busca un **tipo**: con `var pe *fs.PathError`, `errors.As(err, &pe)` rellena `pe`.

Dos detalles numéricos. `const MaxFileSize = 1<<32 - 1` es una constante **sin tipo**, exacta, que toma tipo donde se usa; en una plataforma de 32 bits no cabe en un `int`, y `GOARCH=386 go build` rechaza `len(data) > MaxFileSize` con `MaxFileSize (untyped int constant 4294967295) overflows int`. Por eso `load.go` convierte a `int64` antes de comparar. Y `0o644`, en los tests, es un literal octal: los permisos del fichero.

:::from Rust
`%w` es el `source()` de `std::error::Error` (o el `.context()` de anyhow) sin declarar un tipo; `errors.Is` y `errors.As` hacen de `matches!` y `downcast_ref` a lo largo de toda la cadena. Un centinela es lo más parecido a una variante unitaria de un `enum` de errores.
:::

### `panic` y `recover`

Un `error` es para lo que falla aunque el compilador esté bien: un fichero que no existe, un programa mal escrito. `panic(v)` es para lo que no debería pasar nunca, un fallo del propio compilador, como pedir la línea 0. Acepta un valor de cualquier tipo (el capítulo le pasa el `string` de un `fmt.Sprintf`), corta la función y sube por las que la llamaron, ejecutando sus llamadas diferidas. Si nadie lo detiene, el programa imprime `panic: ` con el valor y la pila de la goroutine que lo provocó (`GOTRACEBACK=all` las muestra todas), y sale con código 2.

`recover()` detiene el panic en curso y devuelve su valor, pero solo cuando la llama directamente una función diferida: fuera de un panic devuelve `nil`, y desde una función auxiliar a la que llama la diferida, también. La regla del libro: la entrada del usuario produce un `error`; un fallo (un bug) del compilador es un panic, y los tests lo recuperan para comprobar que ocurre.

:::from Rust
`panic(v)` es `panic!`, y la frontera es la de Rust entre `panic!` o `unwrap` y `Result`: `Result` para lo esperable, panic para un invariante roto. `recover` hace de `std::panic::catch_unwind`, y como él se usa solo en los bordes (un test, un servidor que no debe caerse por una petición). Sin recuperar, un panic sale con 2 en Go y con 101 en Rust.
:::

:::from Python
En Python y en TS, una excepción es el camino normal de un error (`open` lanza `FileNotFoundError`). En Go, lo esperable es un `error` devuelto, y un panic que sale de un paquete casi siempre es un bug.
:::

### UTF-8 con `unicode/utf8`

UTF-8 codifica cada code point en 1 a 4 bytes; los bytes por debajo de `utf8.RuneSelf` (0x80) son ASCII. `utf8.DecodeRune(b)` decodifica la primera secuencia y devuelve el code point y cuántos bytes ocupa:

| Entrada | Resultado | Por qué |
|---|---|---|
| `"\xEF\xBF\xBD"` | `U+FFFD`, 3 | el carácter de sustitución, escrito de verdad |
| `"\xff"` | `U+FFFD`, 1 | `FF` no aparece nunca en UTF-8 |
| `"\xC0\xAF"` | `U+FFFD`, 1 | *overlong*: `/` escrito con dos bytes |
| `"\xE2\x82"` | `U+FFFD`, 1 | secuencia cortada |
| vacía | `U+FFFD`, 0 | no hay nada |

`utf8.RuneError` es `U+FFFD`, y como `DecodeRune` lo devuelve tanto para un byte inválido como para un U+FFFD legítimo, lo que los distingue es el tamaño: inválido es `(RuneError, 1)`. También son inválidos los *surrogates* de UTF-16 (`ED A0 80`) y lo que pasa de U+10FFFF (`F4 90 80 80`). `utf8.RuneCount(b)` cuenta cada byte inválido como un code point: `"\xff\xfe"` da 2 y `"a\xE2\x82"`, 3. Es la regla de columnas de Ki.

:::from TS
`"😀".length` es 2 en JavaScript (unidades UTF-16); `len("😀")` es 4 en Go (bytes), y `utf8.RuneCountInString("😀")` es 1.
:::

### Búsqueda binaria: `slices.BinarySearch` y `sort.Search`

La tabla de líneas de `"ab\ncd\nef"` es `[0 3 6]`. `slices.BinarySearch(lines, off)`, una función genérica que acepta cualquier slice de un tipo ordenado, devuelve dos valores: la posición de `off` y `true` si está, o la posición donde habría que insertarlo y `false`:

| `off` | 0 | 1 | 3 | 4 | 6 | 7 |
|---|---|---|---|---|---|---|
| `BinarySearch` | 0, true | 1, false | 1, true | 2, false | 2, true | 3, false |
| línea | 1 | 1 | 2 | 2 | 3 | 3 |

Encontrado en `i`, el offset es el inicio de la línea `i+1`; si no, el punto de inserción está una posición más allá de la línea que lo contiene, y como las líneas cuentan desde 1, `i` **es** la línea. Confundir los dos casos desplaza la línea en uno en los offsets más comunes.

`sort.Search(n, f)`, la forma antigua, devuelve el menor `i` en `[0, n)` con `f(i)` verdadero. Con `func(i int) bool { return lines[i] > off }` da la línea directamente: 1, 1, 2, 2, 3, 3. El código usa `slices.BinarySearch`, que no necesita un closure.

:::from Python
`bisect.bisect_left(lines, off)` da el índice de `BinarySearch`, y `bisect.bisect_right`, el de `sort.Search` con `>`: la línea.
:::

### Qué hace el recolector y de qué no te libra

Según la cabecera de `runtime/mgc.go`, el recolector de Go es «a concurrent mark and sweep that uses a write barrier», «non-generational and non-compacting»: marca lo alcanzable mientras tu programa sigue corriendo, libera el resto y **no mueve los objetos del heap** (las pilas de las goroutines sí se copian al crecer o encoger, y el runtime corrige los punteros que las apuntan), así que un puntero vale mientras exista. No hay `free` ni punteros colgantes. De lo que no te libra:

- **El aliasing**: dos cabeceras sobre un array escriben en el mismo sitio, y para el recolector todo es memoria viva.
- **Las referencias obsoletas**: el `&files[i]` de antes no cuelga, apunta al array viejo, que vive **porque** lo apuntas. No hay crash; hay un dato perdido.
- **Las vistas que retienen arrays grandes**: mientras una cabecera apunte a un array, vive el array entero. Un programa que crea 100 MiB con `bytes.Repeat`, se queda con la primera línea (`data[:9]`, una vista) y suelta `data` sigue teniendo 100 MiB en el heap tras un `runtime.GC()` (lo dice `runtime.ReadMemStats`); con `bytes.Clone(keep)`, que copia los 9 bytes a un array propio, 0 MiB. `SourceManager` guarda el fichero entero a propósito, y `Text` devuelve una copia.

**`-gcflags=-m`.** El compilador decide al compilar si una variable va en la pila o en el heap: si su dirección puede sobrevivir a la función, *escapa*. `go build -gcflags=-m` imprime esas decisiones (y las de *inlining*) por stderr. Para una función que devuelve `&lc`, con `lc` local, dice `moved to heap: lc`.

:::from Rust
Con `fn escapes() -> &'static LineCol { let lc = ...; &lc }`, rustc dice `` error[E0515]: cannot return reference to local variable `lc` ``: el borrow checker **rechaza** el programa. El análisis de escape de Go pregunta lo contrario, «¿dónde lo pongo para que sea correcto?», y nunca rechaza nada: lo mueve al heap.
:::

## Tests primero

Las tarjetas del capítulo, con valores exactos:

{{cards}}

Úsalas como en el capítulo 1: escribe el test de la tarjeta, míralo fallar (rojo), implementa lo justo para que pase (verde), y solo entonces despliega el test de referencia con su `{{reftest}}`.

Los ficheros de test están en `package base`, como `cli_test.go` en el capítulo 1, y no en el externo `base_test`, aunque no tocan nada sin exportar. Lo del capítulo 1 es un hábito, no una regla: el externo solo es obligatorio para importar `internal/testsupport` (capítulo 7), que desde dentro daría un ciclo, y aquí gana la comodidad de escribir `LineCol{1, 1}` y no `base.LineCol{1, 1}`. Un ayudante de un `_test.go` (como `newLineTableManager`, en `line_table_test.go`) se usa desde los demás. Cada `TestXxx` vive en un solo fichero: `TestSourceManager` en `source_test.go`, `TestLineTable` en `line_table_test.go`, `TestLoad` en `load_test.go` y `TestFirstInvalidUTF8` en `utf8_test.go`.

## Implementación

### Sesión 1: ficheros fuente y spans (≈ 3 h)

#### Los tests, en rojo

T02.01–T02.07 describen el `SourceManager` mínimo. Su interfaz:

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

Escribe los subtests de `TestSourceManager` en `internal/base/source_test.go`. Cada uno empieza por `var m SourceManager`: el valor cero (un slice `nil` de ficheros) ya sirve, porque `append` sobre un slice `nil` funciona. T02.05 y T02.07 piden tres piezas:

- **Un fichero con capacidad de sobra**: `src := make([]byte, 5, 100)` y `copy(src, "hello")`, como un buffer leído de disco.
- **Mirar más allá de la longitud**: `src[:6][5]`. Si sigue valiendo 0, nadie escribió en el array del fichero.
- **Comprobar un panic**: con `recover()` en una función diferida, como en la teoría. Como `defer` actúa al salir de la función, cada fila de T02.07 va en una función literal que se llama en el acto, `func() { defer func() { … }(); m.Text(row.sp) }()`, y el panic de una fila no corta el bucle.

El rojo es un error de compilación (Go se corta tras diez). En este bloque y en los demás de salida, los números de línea son los de la referencia; los de tus ficheros serán otros:

```console
$ go test ./internal/base/
# github.com/aldezex/kilang/internal/base [github.com/aldezex/kilang/internal/base.test]
internal/base/source_test.go:7:9: undefined: SourceManager
internal/base/source_test.go:23:9: undefined: SourceManager
…
internal/base/source_test.go:77:9: too many errors
FAIL	github.com/aldezex/kilang/internal/base [build failed]
FAIL
```

#### El código

`internal/base/source.go`, completo:

{{file ch02-s1 internal/base/source.go}}

- El comentario antes de `package base` documenta el paquete.
- Los offsets son `uint32`: un `Span` ocupa 12 bytes, frente a los 24 que tendría con `int`, y el compilador guardará uno por token y por nodo. El límite que eso impone, 4 GiB, lo comprueba `Load` (sesión 3).
- `file` empieza por minúscula: fuera de `base` nadie lo nombra. `lines` y `hadBOM` se usan en las sesiones 2 y 3.
- En `Add`, `contents[:len(contents):len(contents)]` recorta la capacidad una sola vez, y desde ahí nada escribe ni lee más allá del fichero a través del slice guardado. `&file{…}` crea un `file` y da su dirección, y `FileID(len(m.files) - 1)` convierte el índice, un `int`, en el tipo del id.
- `Contents` devuelve el slice guardado sin copiar. Un `append` del llamador realoja; asignar a sus elementos escribiría en el fichero, y eso lo prohíbe el contrato del comentario.
- `Text` corta y convierte: `string(…)` copia. Un `Hi` mayor que el tamaño hace panic, porque el corte se comprueba contra la capacidad recortada. Es un fallo del compilador, no del usuario, y por eso no devuelve un `error`.

Comprueba la fuerza de T02.05 y T02.07: quita la línea del recorte de `Add` y fallan las dos:

```text
--- FAIL: TestSourceManager (0.00s)
    --- FAIL: TestSourceManager/contents_append_does_not_alias (0.00s)
        source_test.go:60: append to the result of Contents wrote 'X' into the file's array
    --- FAIL: TestSourceManager/text_past_end_panics (0.00s)
        source_test.go:98: one_past_end: Text({File:0 Lo:0 Hi:6}) did not panic
        source_test.go:98: far_past_end: Text({File:0 Lo:0 Hi:50}) did not panic
FAIL
FAIL	github.com/aldezex/kilang/internal/base	0.004s
FAIL
```

Deshaz el cambio y compara tus tests:

{{reftest T02.04}}

{{reftest T02.05}}

{{reftest T02.07}}

En T02.04, el fichero de relleno mata a un `Text` que ignorase el campo `File` (devolvería `"3456"`), y el span `[3, 7)`, a un `Hi` inclusivo (`"main("`).

El mensaje de la etiqueta: `ch02-s1: source files and spans`.

Si te atascas o tu versión se desvía demasiado, copia el fichero de `ref/ch02-s1` y sigue.

Al terminar: tus tests en verde, commit y etiqueta `ch02-s1`; compárate con `ref/ch02-s1`.

### Sesión 2: la tabla de líneas y `LineCol` (≈ 2,5 h)

#### Los tests, en rojo

T02.08–T02.15 describen la conversión de offset a posición:

```go
type LineCol struct{ Line, Col uint32 } // both 1-based

func (m *SourceManager) LineCol(id FileID, offset uint32) LineCol
func (m *SourceManager) LineCount(id FileID) uint32
func (m *SourceManager) LineStart(id FileID, line uint32) uint32
```

En esta sesión la columna cuenta bytes; la sesión 3 la pasa a code points. Fuera de rango (un offset mayor que el tamaño, la línea 0 o una mayor que `LineCount`) es un panic con el mensaje exacto de T02.15. Escribe `TestLineTable` en `internal/base/line_table_test.go`, con dos ayudantes:

- `newLineTableManager(text string) (*SourceManager, FileID)` crea un manager con un fichero de relleno de forma distinta (`"\n\n\nxx\nyy\n"`) y después el del test, y devuelve el manager y el id del segundo: un método que ignorase su `FileID` respondería sobre el relleno.
- `mustPanic(t *testing.T, name, want string, f func())` ejecuta `f` y falla si no hace panic con exactamente `want`. `recover()` devuelve un `any` (un valor de cualquier tipo), y `fmt.Sprint(r)` lo pasa a texto: para un `panic(fmt.Sprintf(…))`, el mensaje.

T02.09 y T02.13 usan un fichero de **tres** líneas. Con dos, una tabla que midiera cada inicio desde el `\n` anterior y no desde el principio del fichero pasaría todo, porque la línea 2 sale bien por casualidad; con `"ab\ncd\nef"`, ese mutante da `LineStart(3) = 3, want 6`. El rojo:

```console
$ go test ./internal/base/
# github.com/aldezex/kilang/internal/base [github.com/aldezex/kilang/internal/base.test]
internal/base/line_table_test.go:36:21: m.LineCol undefined (type *SourceManager has no field or method LineCol)
internal/base/line_table_test.go:36:38: undefined: LineCol
…
FAIL	github.com/aldezex/kilang/internal/base [build failed]
FAIL
```

#### El código

`internal/base/line_table.go`, completo:

{{file ch02-s2 internal/base/line_table.go}}

- `lineStarts` es un método de `*file` con receptor puntero porque escribe `f.lines`: es una caché perezosa. Si no existe, recorre el fichero con `bytes.IndexByte`, que busca el siguiente `\n`, y añade el offset siguiente a cada salto. `for { … }` es un bucle infinito que sale con `break`, y `rest = rest[i+1:]` avanza cortando, sin copiar. Como la primera consulta escribe en el `file`, el `SourceManager` no se puede usar desde varias goroutines a la vez (lo avisa su comentario; el compilador es secuencial).
- `LineCount` es la longitud de la tabla: el número de `\n` más uno, así que `"a\nb\n"` tiene 3 líneas, la última vacía.
- `LineStart` compara `int64(line) > int64(len(lines))`: `uint32` e `int` no se comparan, e `int64` admite los dos en cualquier plataforma. `fmt.Sprintf` formatea como `Printf` pero devuelve el texto.
- `LineCol` aplica la tabla de la teoría; la columna, de momento, es la distancia en bytes desde el inicio de la línea, más uno.

Compara tus tests:

{{reftest T02.09}}

{{reftest T02.15}}

T02.15 comprueba primero los últimos valores válidos (la línea 2 y el offset 5): un `>=` en lugar de `>` rechazaría el offset de fin de fichero con un panic sin recuperar, que aborta el binario de test entero. En una ejecución completa salta antes, en T02.11, y T02.15 no llega a correr; aislado con `-run TestLineTable/out_of_range_panics`, lo caza T02.15.

El mensaje de la etiqueta: `ch02-s2: line table and LineCol`.

Si te atascas o tu versión se desvía demasiado, copia el fichero de `ref/ch02-s2` y sigue.

Al terminar: tus tests en verde, commit y etiqueta `ch02-s2`; compárate con `ref/ch02-s2`.

### Sesión 3: CRLF, code points, `LineText` y `Load` (≈ 2,5 h)

#### Los tests, en rojo

T02.16–T02.34 completan el paquete (T02.33 y T02.34 van al final del fichero de tarjetas). La interfaz nueva:

```go
func (m *SourceManager) LineText(id FileID, line uint32) string
func (m *SourceManager) HadBOM(id FileID) bool
func FirstInvalidUTF8(b []byte) (offset int, found bool)

const MaxFileSize = 1<<32 - 1
var ErrTooLarge = errors.New("file too large (limit 4 GiB)")
func (m *SourceManager) Load(realPath, displayPath string) (FileID, error)
```

Los tests de CRLF, columnas y `LineText` (T02.16–T02.20 y T02.33) son subtests de `TestSourceManager` y usan `newLineTableManager`. `load_test.go` y `utf8_test.go` empiezan con un ayudante guardado en una variable, `writeFile := func(t *testing.T, …) string { … }` y `check := func(t *testing.T, …) { … }`: closures que solo existen dentro de su `TestXxx`.

`TestLoad` usa ficheros reales: `filepath.Join(t.TempDir(), name)` da una ruta en el directorio temporal del test y `os.WriteFile(path, data, 0o644)` lo escribe. T02.24 no escribe 4 GiB: `os.Truncate(path, 1<<32)` alarga un fichero vacío sin escribir datos, y el sistema de ficheros lo guarda *disperso* (en ext4, `du` da 0 KiB para 4 294 967 296 bytes). Después, `os.Chmod(path, 0)` le quita los permisos, y solo una implementación que mire el tamaño con `os.Stat` antes de leer puede devolver `ErrTooLarge`. Como `root` ignora los permisos, el test se salta con `t.Skip` si `os.Geteuid()` es 0.

El primer rojo, de compilación:

```console
$ go test ./internal/base/
# github.com/aldezex/kilang/internal/base [github.com/aldezex/kilang/internal/base.test]
internal/base/load_test.go:27:16: m.Load undefined (type SourceManager has no field or method Load)
…
internal/base/source_test.go:112:15: m.LineText undefined (type *SourceManager has no field or method LineText)
…
FAIL	github.com/aldezex/kilang/internal/base [build failed]
FAIL
```

Con todo escrito salvo la columna, que aún cuenta bytes, llega el rojo por la razón esperada:

```console
$ go test ./internal/base/
--- FAIL: TestSourceManager (0.00s)
    --- FAIL: TestSourceManager/column_counts_code_points (0.00s)
        source_test.go:134: LineCol(2) = {1 3}, want {1 2}
FAIL
FAIL	github.com/aldezex/kilang/internal/base	0.003s
FAIL
```

T02.19 pasa ya, porque un byte inválido ocupa un byte y una columna: su trabajo es matar a la implementación que se salte los bytes inválidos al contar.

#### El código

La columna y `LineText`:

{{diff ch02-s2..ch02-s3 internal/base/line_table.go}}

- La columna es `1 + utf8.RuneCount(…)` sobre los bytes desde el inicio de la línea: code points, con un byte inválido como uno.
- `LineText` corta la línea sin su salto. Si no es la última, la siguiente empieza justo tras su `\n`, así que `end` es ese inicio menos uno; si el byte anterior es un `\r`, mitad de un `\r\n`, también se quita. En la última línea no hay `\n`, y un `\r` final es un `\r` suelto que se queda (T02.20). `f.lines` existe porque `LineStart` acaba de construir la tabla.
- `end > start` protege la línea vacía. Sin esa condición, en un fichero que empieza por `\n`, `end` es 0 y `f.contents[end-1]` lee el índice `0 - 1`, que en `uint32` da la vuelta; con la entrada de T02.33, de 6 bytes: `panic: runtime error: index out of range [4294967295] with length 6`. Los enteros sin signo de Go no avisan al pasar por debajo de cero. T02.33 lo fija.

`HadBOM`, en `source.go`:

{{diff ch02-s2..ch02-s3 internal/base/source.go}}

La comprobación de UTF-8, en `internal/base/utf8.go`:

{{file ch02-s3 internal/base/utf8.go}}

`for i := 0; i < len(b); {` es un `for` sin la tercera cláusula: cada vuelta decide cuánto avanza, uno para ASCII (con `continue`, sin llamar a `DecodeRune`) y `size` para una secuencia. El resultado es un offset en **bytes** (T02.34), el que usará el error E0101 del capítulo 3.

Y la carga, en `internal/base/load.go`:

{{file ch02-s3 internal/base/load.go}}

- `Load` mira el tamaño con `os.Stat` antes de leer y lo vuelve a mirar después, por si el fichero creció entre medias. Un fichero de exactamente `MaxFileSize` bytes se acepta.
- `utf8BOM` es un literal de slice, y `bytes.HasPrefix` compara el principio; quitarlo es cortar, sin copiar. Tras `Add`, `Load` marca `hadBOM` a través del puntero `m.files[id]`.
- `loadError` envuelve con `%w`, así que `errors.Is` funciona con `fs.ErrNotExist`, `fs.ErrPermission` y `ErrTooLarge`. Antes, si la causa es un `*fs.PathError`, se queda con su `Err`, porque el `PathError` lleva la ruta **real** y el mensaje debe nombrar solo la de pantalla: ``cannot read file `nope.ki`: no such file or directory``. A cambio, `errors.As` ya no encuentra un `*fs.PathError` en la cadena.

Dos mutantes que tus tests deben matar: comparar solo `r == utf8.RuneError`, sin el tamaño, hace fallar T02.27 (`FirstInvalidUTF8("�") = (0, true), want (0, false)`), y leer con `os.ReadFile` antes de `os.Stat` hace fallar T02.24 con un error de permisos. Compara tus tests:

{{reftest T02.20}}

{{reftest T02.24}}

{{reftest T02.27}}

{{reftest T02.33}}

El mensaje de la etiqueta: `ch02-s3: CRLF, code points, LineText and Load`.

Si te atascas o tu versión se desvía demasiado, copia el fichero de `ref/ch02-s3` y sigue.

Al terminar: tus tests en verde, commit y etiqueta `ch02-s3`; compárate con `ref/ch02-s3`.

## Ejecútalo

Con la sesión 3 terminada (los tiempos varían, y un paquete sin cambios puede salir `(cached)`; en `ref/ch02-s3`, cada tanda lista además `cmd/bookx`, la herramienta del libro, que tu repo no tiene):

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
**1. `Span.Join`.** Añade a `source.go` un método `func (a Span) Join(b Span) Span` que devuelva el span más pequeño que cubre los dos, con su test. Su *expresión de método*, `Span.Join`, es una función de tipo `func(Span, Span) Span`: de ahí la firma `Span.Join(a, b Span) Span`. ¿Qué hace con dos spans de ficheros distintos? Los builtins `min` y `max` aceptan cualquier tipo ordenado.
:::

:::solution
Receptor por valor, porque `Span` es pequeño e inmutable. Dos ficheros no tienen un span común, y mezclarlos es un fallo del compilador: panic.

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
**2. Una referencia obsoleta.** En un módulo de prueba bajo `build/` (`mkdir -p build/stale && cd build/stale && go mod init example.com/stale`), declara `type file struct { path string; lines []uint32 }` y escribe un `main` que tome `f := &files[0]` sobre un `[]file`, haga un `append` y escriba después `f.lines`; imprime `files[0].lines` y `f == &files[0]`. Repite con un `[]*file`. Crea los slices con `make(…, 1, 1)`, para que el `append` tenga que realojar, y explica las dos salidas.
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

Con `[]file`, el `append` no cabe en la capacidad 1: reserva un array de dos y **copia** el `a.ki`. `f` sigue apuntando al `a.ki` del array viejo, que el recolector mantiene vivo porque `f` lo alcanza; la tabla se escribe ahí y `files[0]` no la ve, sin crash ni aviso (`go vet` tampoco dice nada). Con `[]*file`, el `append` copia el puntero, y `ptrs[0]` y `p` siguen siendo el mismo `file`. Es el caso de `SourceManager`: la tabla se escribe en la primera consulta, quizá con otros ficheros ya añadidos.
:::

:::exercise
**3. Medir la tabla.** Escribe en `internal/base/line_table_bench_test.go` un benchmark que compare `LineCol` con una búsqueda lineal (contar `\n` desde el principio) sobre un fichero de 10⁶ líneas, y ejecútalo con `go test -run '^$' -bench BenchmarkLineCol -benchmem ./internal/base/` (`-run '^$'` no ejecuta ningún test). Un benchmark es una `func BenchmarkXxx(b *testing.B)` con un bucle `for b.Loop() { … }` (en Go 1.27), que repite el cuerpo hasta tener una medida estable y no cuenta lo que haya antes del bucle. `b.Run("nombre", …)` crea sub-benchmarks, como `t.Run`, y `bytes.Repeat([]byte("let x = 1\n"), 1_000_000)` te da el fichero. Mide también la primera consulta, la que construye la tabla.
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

El `b.Fatalf` comprueba antes de medir que las dos versiones coinciden. En esta máquina (los números varían):

```text
BenchmarkLineCol/table-16         	71048226	        17.03 ns/op	       0 B/op	       0 allocs/op
BenchmarkLineCol/first_query-16   	     168	   7110579 ns/op	21096467 B/op	      39 allocs/op
BenchmarkLineCol/linear-16        	     288	   4173138 ns/op	       0 B/op	       0 allocs/op
```

Con la tabla hecha, 17 ns: unos 20 pasos de búsqueda binaria, sin reservar memoria. La lineal, 4,2 ms: recorre 10 MB cada vez. La primera consulta, 7,1 ms y 21 MB en 39 asignaciones: la tabla crece por pasos hasta un millón de `uint32` (4 MB), y cada paso deja atrás un array. Se amortiza en la segunda consulta, y un fichero sin errores no la paga.
:::

:::exercise
**4. `Snippet`.** Añade `func (m *SourceManager) Snippet(sp Span) string`, que devuelva el texto de la línea donde empieza `sp` con el trozo del span entre `«»`: en `fn main() {}`, el span `[3, 7)` da `fn «main»() {}`. Un span que pase del final de su línea se corta ahí. Escribe el test con una tabla y piensa qué mutante mata cada fila.
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

Los offsets pasan a ser relativos a la línea y se recortan a la longitud de su texto, que no lleva el salto, así que un span que apunta al `\n` o al `\r\n` no se sale del `string`. El test, en `internal/base/snippet_test.go`:

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

`second_line` mata a quien use offsets del fichero en lugar de la línea (`"let x «=» 1"`); `cut_at_line_end` y `crlf_line`, a quien no recorte `hi` (panic: `slice bounds out of range [:4] with length 2`); `empty_span`, a quien no marque un span vacío; y `after_accent`, a quien corte por columnas en lugar de bytes (`"é« »= 1"`).
:::

:::exercise
**5. Leer el análisis de escape.** Ejecuta `go build -gcflags=-m ./internal/base/` en tu repo y explica dos líneas: la que dice `&file{...} escapes to heap` en `source.go` y la que dice `moved to heap: pe` en `load.go`.
:::

:::solution
Con la referencia, entre otras:

```text
internal/base/source.go:41:28: &file{...} escapes to heap
internal/base/load.go:61:6: moved to heap: pe
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
Porque el recorte permite no copiar sin riesgo: con `cap == len`, un `append` del llamador realoja y nunca escribe en el array del fichero (T02.05), y un corte más allá del final hace panic en lugar de devolver bytes sobrantes del buffer (T02.07). Copiar costaría una asignación por llamada; recortar, nada.
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
El tamaño es la posición tras el último byte, donde apunta el fin de fichero, y un error «se esperaba `}`» ahí tiene que poder mostrarse (T02.11). Uno más no es una posición de ningún fichero: solo sale de un fallo del compilador, y el panic lo descubre en el acto (T02.15).
:::

:::review
¿Por qué `FirstInvalidUTF8` mira el tamaño que devuelve `utf8.DecodeRune` y no solo el code point?
:::

:::answer
Porque `DecodeRune` devuelve `utf8.RuneError` tanto para un byte inválido como para un U+FFFD escrito de verdad, y solo el tamaño los distingue: 1 frente a 3. Sin mirarlo, un `�` en un comentario daría un falso E0101 (T02.27).
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

- Asignar y pasar un argumento copian el valor; un struct con un slice dentro comparte el array.
- Un puntero comparte un valor, sin aritmética; el compilador decide si lo apuntado va en la pila o en el heap.
- Receptor por puntero si el método modifica el valor, y entonces en todos los del tipo; Go toma la dirección de una variable por ti.
- Un slice es puntero, longitud y capacidad: indexar comprueba la longitud y cortar, la capacidad.
- `append` reutiliza el array si cabe y si no realoja y copia: de ahí el aliasing y las referencias obsoletas. `a[i:j:k]` y `slices.Clip` limitan la capacidad.
- `string` es inmutable y no garantiza UTF-8; `[]byte` es mutable; las conversiones copian.
- `%w` envuelve, `errors.Is` busca un valor en la cadena y `errors.As`, un tipo; la entrada del usuario da un `error`, y un fallo del compilador, un panic.
- `utf8.DecodeRune` da `(RuneError, 1)` para un byte inválido, y `RuneCount` cuenta cada uno como un code point.
- El recolector no mueve los objetos del heap ni deja punteros colgantes, pero no te libra del aliasing ni de las vistas que retienen arrays enteros.
- `SourceManager`: spans `[Lo, Hi)` en `uint32`, tabla de líneas perezosa, columnas en code points, `\r\n` como un salto, `\r` suelto como carácter, BOM quitado y recordado, límite de 4 GiB.

## Go en el trabajo

**El aliasing de `append`, en una entrevista.** «¿Qué imprime `b := append(base, 1); c := append(base, 2); fmt.Println(b, c)`?» se responde sabiendo la capacidad de `base`: con sitio de sobra, `[0 0 0 2] [0 0 0 2]`; sin él, cada `append` tiene su array. La solución: `slices.Clip(base)`, o `append(slices.Clone(base), …)` como en el `ci_test.go` del capítulo 1.

**Vistas con la capacidad limitada en una API pública.** Quien devuelve un slice de sus datos decide qué puede hacer quien lo recibe. `bytes.Split` limita la capacidad de los trozos: en `genSplit` (`bytes/bytes.go`, Go 1.27) se lee `a[i] = s[: m+sepSave : m+sepSave]`, así que un `append` sobre un trozo no pisa el siguiente. `bytes.Buffer.Bytes`, en cambio, documenta el aliasing como contrato: «The slice is valid for use only until the next buffer modification». `Add` y `Contents` hacen lo primero.

**`string([]byte)` y sus copias.** Cada conversión copia, salvo si el compilador demuestra que no hace falta. Con `testing.AllocsPerRun` sobre un `[]byte` de 49 bytes, en Go 1.27, guardar `string(b)` en una variable global cuesta una asignación; `string(b) == "fn main()"` y la búsqueda `m[string(b)]` en un mapa, ninguna. Por eso un lexer puede buscar sus palabras clave con `keywords[string(text)]` sin pagar. Para evitar la copia de verdad, la biblioteca usa `unsafe` (`strings.Builder.String` es `unsafe.String(unsafe.SliceData(b.buf), len(b.buf))`). Y `strings.Clone` copia a propósito, porque eso, dice su documentación, «can be important when retaining only a small substring of a much larger string».

**El análisis de escape en una revisión de rendimiento.** Ante una función caliente que reserva de más, el primer paso es `go build -gcflags=-m` y buscar sus `moved to heap` y `escapes to heap`; `-gcflags=-m=2` explica además por qué camino escapa cada cosa. Un benchmark con `-benchmem` (ejercicio 3) dice después si el cambio mereció la pena.
