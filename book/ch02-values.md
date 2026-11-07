---
chapter: 2
title: Valores, punteros, slices y el recolector
part: I
hours: 8
milestone: SourceMap guarda los ficheros en memoria; la búsqueda de línea y columna está testeada
---

## Objetivo

Al terminar tendrás `internal/base`, el primer paquete del compilador. Su `SourceManager` (el «SourceMap» del hito) guarda cada fichero en memoria y convierte un offset de byte en línea y columna. `ki` no cambia; `go test ./internal/base/` pasa en verde.

En el capítulo 3, `ki check` dibujará con él su primer error, camino del hito M1.

## Teoría

### El problema: de un byte a `main.ki:3:9`

Cada token (capítulo 5) recuerda dónde está como un **offset de byte**: cortar su texto es indexar. Una persona quiere leer `main.ki:3:9`. Tres decisiones:

- **Spans semiabiertos.** Un `Span` es un trozo de fichero, `[Lo, Hi)`. Con `Hi` inclusivo, el span vacío, el del fin de fichero, exigiría `Hi = Lo − 1`, que en el offset 0 da la vuelta.
- **Tabla de líneas perezosa.** Guarda dónde empieza cada línea y se construye en la primera consulta: un fichero sin errores nunca la paga.
- **Columnas en code points** (caracteres Unicode). En `é=1`, el `=` está en el byte 2 pero es el segundo carácter.

Un span de un fichero `f` cumple:

```math
Span = [Lo, Hi)      0 ≤ Lo ≤ Hi ≤ |f|      longitud = Hi − Lo      vacío ⟺ Lo = Hi
```

Un fichero real trae tres casos:

- `\r\n` es **un** salto; un `\r` suelto **no** termina la línea.
- El BOM (`EF BB BF`) se quita al leer y se recuerda.
- Un fichero de 4 GiB o más se rechaza antes de leerlo, porque los offsets son `uint32`.

`SourceManager` guarda cada fichero en un `file`, en `files`; `Text(sp)` da el texto de un span.

### Valores y copias

Asignar copia el valor entero, y un struct es un valor (en Python y en TS, `b = a` comparte el objeto). Un array, de tamaño fijo, se copia entero, y pasar un argumento también copia.

La copia es superficial: un struct con un slice dentro comparte su array.

### Tipos definidos y structs

- **Tipos definidos.** `FileID` es un tipo nuevo cuyo tipo subyacente es `uint32`.
- **Conversiones.** Go no convierte entre tipos numéricos por su cuenta: hace falta convertir, salvo con una constante sin tipo, que toma tipo donde se usa. Para indexar vale cualquier entero.
- **Literales.** Un literal de struct nombra los campos o los da en orden.
- **Comparación.** `==` compara structs campo a campo, si todos son comparables.

:::from Rust
Un struct se comporta como uno con `#[derive(Clone, Copy)]`, y `type FileID uint32` es un newtype que conserva los operadores.
:::

### Punteros sin aritmética

`&x` es la dirección de `x` y `*p` el valor apuntado; al acceder a un campo, Go desreferencia por ti. Un puntero comparte en lugar de copiar.

No hay aritmética de punteros. Una función puede devolver la dirección de una local: el compilador la pone en el heap.

:::from Rust
Un `*T` es un `&mut T` sin tiempo de vida ni exclusividad; lo libera el recolector.
:::

### Receptores por valor y por puntero

El receptor (capítulo 1) es un parámetro más: por valor, el método recibe una **copia**; por puntero, el original.

Al llamar a un método de puntero, Go toma la dirección por ti:

```math
x de tipo T, m de receptor *T:   x.m()   ≡   (&x).m()
válido  ⟺  x es direccionable (p. ej. una variable, *p, un elemento de slice o de un
                                array direccionable, un campo de algo direccionable;
                                no un elemento de mapa ni el resultado de una llamada)
```

Un elemento de un mapa (la tabla hash de Go) no es direccionable: esa llamada no compila.

La regla del libro: por puntero si el método modifica el valor o el struct es grande; por valor si es pequeño e inmutable, como `Span`. Si un método necesita puntero, todos lo usan.

### Las entrañas de un slice

Un slice es una **cabecera** de tres palabras: un puntero a un array, una longitud y una capacidad (cuántos elementos caben hasta el final del array). Copiarlo copia la cabecera, no el array, y cortarlo da una vista (en Python, una copia):

```math
s = ⟨ptr, len, cap⟩                     0 ≤ len ≤ cap
s[i]       válido ⟺ 0 ≤ i < len
s[i:j]     válido ⟺ 0 ≤ i ≤ j ≤ cap       da ⟨ptr + i, j − i, cap − i⟩
s[i:j:k]   válido ⟺ 0 ≤ i ≤ j ≤ k ≤ cap   da ⟨ptr + i, j − i, k − i⟩
copy(d, o) copia min(len d, len o) elementos (si d es []byte, o puede ser un string)
```

Indexar mira la longitud y cortar, la **capacidad**: con `len` 5 y `cap` 100, `s[5]` falla, pero `s[:6]` es válido.

**`append`** reutiliza el array si hay sitio:

```pseudo
función append(s, x)
  si len(s) < cap(s) entonces
    escribe x en el array de s, en la posición len(s)
    devuelve ⟨s.ptr, len(s) + 1, cap(s)⟩        ▹ el mismo array
  si no
    reserva un array mayor                        ▹ cuánto, lo decide la implementación
    copia s en él y escribe x detrás
    devuelve una cabecera sobre el array nuevo
```

Reutilizar es eficiente, y es también el **aliasing**: dos `append` sobre una misma base con sitio escriben en la misma posición, y el segundo pisa al primero. Con el **corte completo** `s[:n:n]` (o `slices.Clip(s)`) la capacidad es la longitud, y el siguiente `append` tiene que copiar.

**Por qué `SourceManager` guarda `[]*file` y no `[]file`.** Un puntero a un elemento apunta **al array actual**. Tras un `append` que realoja, el elemento vive en el nuevo, y lo escrito por ese puntero se pierde sin error.

Con `[]*file`, `append` copia los punteros y cada `file` se queda donde está (ejercicio 2; en el capítulo 4, `IdVec` cambia este diseño).

### `string` frente a `[]byte`

Un `string` es una secuencia **inmutable** de bytes, y no garantiza UTF-8 válido: de ahí `FirstInvalidUTF8`. `len` cuenta bytes (el de `"é"` es 2), indexar da un byte, y recorrerlo con `range` da code points, con `U+FFFD` por cada byte inválido.

Cada code point es un `rune`, un alias de `int32`.

Convertir entre `string` y `[]byte` **copia**: si compartieran el array, cambiar el `[]byte` cambiaría un `string` inmutable. Por eso `Text` devuelve un `string`: quien lo recibe no puede tocar el fichero.

### Leer un fichero y envolver errores

`Load` rechaza un fichero sin leerlo y distingue por qué falló:

```pseudo
función Load(ruta, nombre) → (FileID, error)
  info ← stat(ruta)                                   ▹ el tamaño, sin leer nada
  si stat falla entonces error (su causa)
  si info no es un fichero regular entonces error «not a regular file»
  si info.tamaño > 2³² − 1 entonces error ErrTooLarge
  datos ← lee el fichero entero
  si falla entonces error (su causa)
  si |datos| > 2³² − 1 entonces error ErrTooLarge     ▹ por si creció entre medias
  si datos empieza por EF BB BF entonces quítalo y recuérdalo
  devuelve Add(nombre, datos)
```

Cada error sale como «cannot read file `nombre`: causa». `os.Stat` da un `fs.FileInfo`, con el tamaño como `int64`; él y `os.ReadFile` fallan con un `*fs.PathError`, que guarda operación, ruta y causa.

- **Un centinela**, como `ErrTooLarge`, es una variable de paquete creada con `errors.New` y comparada por identidad.
- **`%w`** en `fmt.Errorf` envuelve y guarda la causa; con `%v`, la causa se pierde.
- **`errors.Is`** recorre la cadena de envoltorios: distingue `fs.ErrNotExist` de «sin permiso» sin mirar el texto.
- **`errors.As`** (capítulo 1) busca un **tipo**.

La cadena, simplificada:

```pseudo
función Is(err, objetivo)
  mientras err ≠ nil
    si err = objetivo entonces devuelve sí
    err ← la causa que guardó %w
  devuelve no
▹ simplificado: también consulta un método Is propio y Unwrap() []error;
▹ As pregunta por el tipo en lugar de por el valor
```

:::from Rust
`%w` es el `source()` de `std::error::Error`.
:::

### `panic` y `recover`

La regla del libro: la entrada del usuario, también un programa mal escrito, nunca da un panic. Un bug del compilador, como pedir la línea 0, da un `panic(v)`.

El pseudocódigo lo resume:

```pseudo
función panic(v)
  para cada función G de la pila, de la más reciente a la más antigua
    para cada llamada diferida D de G, de la última a la primera
      ejecuta D; si D llama directamente a recover(), recover devuelve v
      si D recuperó así y vuelve sin un panic nuevo entonces
        el panic se detiene: se ejecutan las demás diferidas de G
        y G vuelve a quien la llamó
  imprime v y la pila; sale con código 2
▹ fuera de una función diferida, recover() devuelve nil
```

El valor de `recover()` es un `any` (de cualquier tipo).

`recover` vive en los bordes: un test, un servidor que no debe caerse por una petición.

### UTF-8 con `unicode/utf8`

UTF-8 codifica cada code point en 1 a 4 bytes, según su rango:

```math
U+0000–U+007F     0xxxxxxx
U+0080–U+07FF     110xxxxx 10xxxxxx
U+0800–U+FFFF     1110xxxx 10xxxxxx 10xxxxxx          (sin U+D800–U+DFFF)
U+10000–U+10FFFF  11110xxx 10xxxxxx 10xxxxxx 10xxxxxx
válida  ⟺  sigue una fila y usa la más corta para su code point
```

`utf8.DecodeRune` decodifica la primera secuencia y devuelve el code point y cuántos bytes ocupa:

| Bytes | Resultado | Por qué |
|---|---|---|
| `EF BF BD` | `U+FFFD`, 3 | el carácter de sustitución, escrito de verdad |
| `FF` | `U+FFFD`, 1 | `FF` no aparece nunca en UTF-8 |
| `C0 AF` | `U+FFFD`, 1 | *overlong*: `/` escrito con dos bytes |
| `E2 82` | `U+FFFD`, 1 | secuencia cortada |
| ninguno | `U+FFFD`, 0 | no hay nada |

`utf8.RuneError` es `U+FFFD`, y solo el tamaño distingue un byte inválido de un U+FFFD legítimo. Así busca `FirstInvalidUTF8` el primero:

```pseudo
función FirstInvalidUTF8(b) → (offset, encontrado)
  i ← 0
  mientras i < |b|
    si b[i] < 0x80 entonces i ← i + 1; sigue        ▹ ASCII, un byte
    (r, n) ← DecodeRune(b[i:])
    si r = U+FFFD y n = 1 entonces devuelve (i, sí)
    i ← i + n
  devuelve (0, no)
```

`utf8.RuneCount` cuenta cada byte inválido como un code point: es la regla de columnas de Ki.

### Búsqueda binaria: de un offset a línea y columna

La tabla de líneas guarda dónde empieza cada una: en 0 y tras cada `\n`. La de `"ab\ncd\nef"` es `[0 3 6]`. La línea de un offset es cuántos inicios hay hasta él:

```math
inicios(f) = [0] ++ [ i + 1 | f[i] = '\n' ]
línea(off) = |{ s ∈ inicios(f) : s ≤ off }|          0 ≤ off ≤ |f|
col(off)   = 1 + runas(f[inicios[línea − 1] : off])
```

La búsqueda binaria da la posición de `off` y `true`, o dónde iría y `false`:

| `off` | 0 | 1 | 3 | 4 | 6 | 7 |
|---|---|---|---|---|---|---|
| búsqueda | 0, true | 1, false | 1, true | 2, false | 2, true | 3, false |
| línea | 1 | 1 | 2 | 2 | 3 | 3 |

Encontrado en `i`, el offset es el inicio de la línea `i+1`. Si no, el punto de inserción está una posición más allá de la línea, y como las líneas cuentan desde 1, `i` **es** la línea:

```pseudo
función LineCol(f, off)
  si off > |f| entonces panic «offset fuera de rango»
  inicios ← f.líneas, construida en la primera consulta
  (i, encontrado) ← búsqueda binaria de off en inicios
  línea ← i + 1 si encontrado, si no i
  devuelve (línea, 1 + runas(f[inicios[línea − 1] : off]))
```

El texto de una línea no lleva su salto:

```pseudo
función LineText(f, k)
  inicio ← inicios[k − 1]                         ▹ panic si la línea k no existe
  si k es la última línea entonces fin ← |f|
  si no
    fin ← inicios[k] − 1                          ▹ la posición de su '\n'
    si fin > inicio y f[fin − 1] = '\r' entonces fin ← fin − 1
  devuelve f[inicio : fin]
```

### Qué hace el recolector y de qué no te libra

El recolector marca lo alcanzable, libera el resto y **no mueve los objetos del heap**: un puntero vale mientras exista.

```math
vivos = { o | algún camino de punteros lleva desde las variables del programa hasta o }
```

No hay `free` ni punteros colgantes. De lo que no te libra:

- **El aliasing**: para el recolector, todo es memoria viva.
- **Las referencias obsoletas**: el puntero al elemento de antes apunta al array viejo, que vive **porque** lo apuntas.
- **Las vistas que retienen arrays grandes**: una cabecera mantiene vivo el array entero. `SourceManager` guarda el fichero entero a propósito, y `Text` devuelve una copia.

:::more 100 MiB retenidos por 9 bytes
Un programa crea 100 MiB con `bytes.Repeat`, se queda con la primera línea (`data[:9]`, una vista) y suelta `data`. Tras un `runtime.GC()`, sigue teniendo 100 MiB en el heap (lo dice `runtime.ReadMemStats`).

Con `bytes.Clone(keep)`, que copia los 9 bytes a un array propio, 0 MiB.
:::

### Análisis de escape: `-gcflags=-m`

El compilador decide si una variable va en la pila o en el heap: si su dirección puede sobrevivir a la función, *escapa*. Con `-gcflags=-m`, `go build` imprime esas decisiones (ejercicio 5).

:::from Rust
El borrow checker **rechaza** devolver una referencia a una local; el análisis de escape solo pregunta «¿dónde lo pongo?».
:::

## Preguntas de repaso

:::review
¿Por qué los spans son semiabiertos?
:::

:::answer
Porque así la longitud es `Hi − Lo` y el span vacío, el del fin de fichero, es `Lo = Hi`, también en el offset 0. Con `Hi` inclusivo, el vacío exigiría `Hi = Lo − 1`, y en el offset 0 eso da la vuelta en `uint32`.
:::

:::review
¿Por qué `SourceManager` guarda `[]*file` y no `[]file`?
:::

:::answer
Porque así guardar un `*file` no tiene riesgo. Con `[]file`, un puntero a un elemento queda en el array viejo tras un `append` que realoja, y lo escrito se pierde sin ruido, porque el recolector mantiene vivo ese array. Con punteros, los `file` no se mueven. No es obligatorio: basta con no conservar un `*file` tras un `Add`, y el capítulo 4 va por ahí.
:::

:::review
¿Por qué `LineCol` acepta un offset igual al tamaño del fichero y rechaza uno más?
:::

:::answer
El tamaño es la posición tras el último byte, donde apunta el fin de fichero, y un error «se esperaba `}`» ahí tiene que poder mostrarse. Uno más no es una posición de ningún fichero: solo sale de un fallo del compilador, y el panic lo descubre en el acto.
:::

:::review
¿Por qué las columnas cuentan code points, y cada byte inválido como uno?
:::

:::answer
Porque una persona cuenta caracteres: en `é=1`, el `=` es el segundo aunque esté en el byte 2. Un byte inválido no forma ningún carácter, y cuenta como uno, igual que en `utf8.RuneCount`: la columna avanza también sobre él.
:::

:::review
¿Por qué `FirstInvalidUTF8` mira el tamaño que devuelve `utf8.DecodeRune` y no solo el code point?
:::

:::answer
Porque `DecodeRune` devuelve `utf8.RuneError` tanto para un byte inválido como para un U+FFFD escrito de verdad, y solo el tamaño los distingue: 1 frente a 3. Sin mirarlo, un `�` en un comentario daría un falso error de UTF-8 inválido (E0101).
:::

:::review
¿Por qué `Load` mira el tamaño con `os.Stat` antes de leer el fichero?
:::

:::answer
Para rechazar el fichero sin leer 4 GiB: `Stat` da el tamaño sin abrirlo. Y lo vuelve a mirar después de leer, por si el fichero creció entre medias.
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
- La entrada del usuario nunca da un panic; un fallo del compilador, sí.
- El recolector evita los punteros colgantes, no el aliasing ni las vistas que retienen arrays.
- `SourceManager`: spans semiabiertos, tabla de líneas perezosa, columnas en code points.

## Go en el trabajo

**Aliasing, en una entrevista.** Preguntan qué imprimen dos `append` sobre una misma base con sitio. Se evita con `slices.Clip(base)` o `append(slices.Clone(base), …)`.

**Vistas en una API pública.** Quien devuelve un slice decide qué puede hacer quien lo recibe: `bytes.Split` limita la capacidad de todos los trozos menos el último, y `bytes.Buffer.Bytes` documenta el aliasing como contrato.

**`string([]byte)`.** Cada conversión copia, salvo si el compilador demuestra que no hace falta: una búsqueda como `keywords[string(text)]` no paga.

**Rendimiento.** Ante una función caliente que reserva de más, empieza por `go build -gcflags=-m`; un benchmark con `-benchmem` dice si mereció la pena.

## Tests primero

{{cards}}

Úsalas como en el capítulo 1: test en rojo, código, y solo entonces la referencia. Las entradas de cada tarjeta, en `git show ref/ch02-s3:book/tests/ch02.tests`.

## Implementación

### Sesión 1: ficheros fuente y spans (≈ 3 h)

#### Go: valores, punteros y receptores

La Teoría, en código:

```go
type Point struct{ X, Y int }

a := Point{1, 2}
b := a  // a copy
b.X = 9 // a is still {1 2}
```

```go
type Counter struct{ n int }

func (c Counter) IncVal()  { c.n++ } // changes the copy
func (c *Counter) IncPtr() { c.n++ } // changes the original
```

```go
type FileID uint32

files := []string{"a.ki", "b.ki"}
n := 0
id := FileID(n)        // a conversion: Go never converts int to FileID by itself
id = id + 1            // 1 is an untyped constant: here it is a FileID
fmt.Println(files[id]) // any integer type indexes a slice

pt := Point{X: 1, Y: 1}  // fields by name; Point{1, 1} gives them in order
if pt == (Point{1, 1}) { // without the parentheses, { would open the if block
	fmt.Println("equal")
}
p := &pt
p.Y = 2 // Go dereferences: (*p).Y; p + 1 does not compile

var c Counter
c.IncPtr() // (&c).IncPtr(): c is a variable, so it has an address
counters := map[string]Counter{"a": {}}
fmt.Println(counters["a"].n) // reading an element works, but
// counters["a"].IncPtr() does not compile: a map element has no address
```

#### Go: el aliasing de `append`

Con capacidad de sobra:

```go
base := make([]int, 3, 10)
b := append(base, 1)
c := append(base, 2)
fmt.Println(b, c) // [0 0 0 2] [0 0 0 2]: both wrote position 3
// With capacity 3, each append would get an array of its own.
```

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

```console doctest="red"
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

`&file{…}` crea un `file` y da su dirección. `Add` usa el corte completo: `cap == len`.

Quita ese corte y fallan los dos tests de capacidad:

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

Al terminar: tus tests en verde, commit y etiqueta `ch02-s1`; compárate con `ref/ch02-s1`.

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

```console doctest="red"
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

- **`lineStarts` y `LineCol`** son la tabla y el pseudocódigo de la Teoría, aún con la columna en bytes. `lineStarts` tiene receptor puntero porque escribe `f.lines`, una caché perezosa; busca cada `\n` con `bytes.IndexByte` y avanza cortando, sin copiar.
- **`slices.BinarySearch`** es la búsqueda binaria, genérica para cualquier tipo ordenado. `sort.Search(n, f)`, la forma antigua, da el menor `i` con `f(i)` verdadero; si `f` es `lines[i] > off`, ese `i` es la línea.
- **`int64`** admite `uint32` e `int`, que no se comparan entre sí.

{{reftest T02.09}}

{{reftest T02.15}}

Si te atascas o tu versión se desvía demasiado, copia el fichero de `ref/ch02-s2` y sigue.

Al terminar: tus tests en verde, commit y etiqueta `ch02-s2`; compárate con `ref/ch02-s2`.

### Sesión 3: CRLF, code points, `LineText` y `Load` (≈ 2,5 h)

#### Go: `string`, `byte` y `rune`

La Teoría, en código:

```go
s := "é=1"
fmt.Println(len(s), s[0]) // 4 195: len counts bytes; indexing gives a byte
for i, r := range s {     // i: byte offset; r: a rune, an alias of int32
	fmt.Println(i, string(r))
}
var c byte = '\n' // '\n' is an untyped rune constant: it fits a byte
fmt.Println(c == '\n')
b := []byte(s) // copies the bytes
b[2] = 'X'     // s is still "é=1"
```

#### Los tests, en rojo

1. **Escribe T02.16–T02.43.** El `ref:` de cada tarjeta dice en qué `TestXxx` va; `load_test.go` y `utf8_test.go` son nuevos. La interfaz nueva:

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

```console doctest="red"
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
- **4 GiB sin escribirlos.** `os.Truncate(path, 1<<32)` alarga un fichero vacío sin escribir datos, y el sistema de ficheros lo guarda *disperso*. `os.Chmod(path, 0)` le quita los permisos. Como `root` los ignora y en Windows `chmod 0` no impide leer, el test se salta con `t.Skip` si `os.Geteuid()` es 0 o en Windows.
- **Falsos.** `stat` y `readFile` son variables para que un test ponga un fichero falso delante de `Load`. `fakeFileInfo` implementa la interfaz `fs.FileInfo` (seis métodos) con el modo y el tamaño que elige el test. `useFakeOS` cambia `stat` y `readFile` y los restaura con `t.Cleanup`, que corre al acabar el test.
:::

#### El código

{{diff ch02-s2..ch02-s3 internal/base/line_table.go}}

`LineText` es el pseudocódigo de la Teoría. Su `end > start` protege un fichero que empieza por `\n`: `end - 1` sería `0 - 1`. En `uint32` eso da la vuelta, y los enteros sin signo de Go no avisan.

{{diff ch02-s2..ch02-s3 internal/base/source.go}}

:::note
**Fe de erratas.** El comentario de `HadBOM` se equivoca: `ki test --update` busca la marca él mismo.
:::

{{file ch02-s3 internal/base/utf8.go}}

Es el `for` del pseudocódigo, sin tercera cláusula: avanza 1 (`b[i] < utf8.RuneSelf`, el 0x80 del pseudocódigo) o `size`.

{{file ch02-s3 internal/base/load.go}}

`Load` es el pseudocódigo de la Teoría.

Aplica todo salvo el cambio de `col` y su `import`, y llega el rojo:

```console doctest="skip: necesita aplicar a mano solo una parte de la sesión"
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

Al terminar: tus tests en verde, commit y etiqueta `ch02-s3`; compárate con `ref/ch02-s3`.

## Ejecútalo

```console
$ go test -count=1 ./internal/base/
ok  	github.com/aldezex/kilang/internal/base	0.003s
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
go: creating new go.mod: module example.com/stale
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

Con `[]*file`, el `append` copia el puntero, y `ptrs[0]` y `p` siguen siendo el mismo `file`. `SourceManager` lo elige por eso: guardar un `*file` no tiene riesgo. Con `[]file`, la regla es no conservar ninguno tras un `Add`; el capítulo 4 va por ahí.
:::

:::exercise
**3. Medir la tabla.** Escribe en `internal/base/line_table_bench_test.go` un benchmark que compare `LineCol` con una búsqueda lineal (contar `\n` desde el principio) sobre un fichero de 10⁶ líneas. Mide también la primera consulta, la que construye la tabla.

Un benchmark es una `func BenchmarkXxx(b *testing.B)` con un bucle `for b.Loop() { … }`, que repite el cuerpo hasta tener una medida estable y no cuenta lo que haya antes. Separa los casos con `b.Run`, como `t.Run`. `-run '^$'` no ejecuta ningún test:

```console doctest="skip: el benchmark es la solución del ejercicio"
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
- **La primera consulta,** 7,4 ms y 21 MB en 39 reservas: la tabla crece por pasos hasta un millón de `uint32` (4 MB), y cada paso deja atrás un array.

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

:::exercise
**6. El recorte de `Add`.** `Contents` devuelve el slice del fichero sin copiarlo. ¿Por qué `Add` le recorta antes la capacidad?
:::

:::solution
Porque el recorte permite no copiar sin riesgo: con `cap == len`, un `append` del llamador realoja y nunca escribe en el array del fichero. Y un corte más allá del final hace panic en lugar de devolver bytes sobrantes del buffer. Copiar costaría una reserva por llamada; recortar, nada.
:::

:::exercise
**7. Un `chmod` que fija un orden.** ¿Por qué el test del fichero de 4 GiB le quita los permisos?
:::

:::solution
Para hacer comprobable el orden de `Load`: quien leyera primero fallaría con `fs.ErrPermission`, y solo quien mira el tamaño antes devuelve `ErrTooLarge`.
:::
