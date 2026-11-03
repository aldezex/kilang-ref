---
chapter: 4
title: Genéricos y la caja de herramientas del compilador
part: I
hours: 6
milestone: `IdVec[I, T]`, `Arena` e `Interner`, testeados y medidos
---

## Objetivo

Al terminar, `internal/base` tiene las tres piezas que usa el compilador:

- `IdVec[I, T]`, un slice que solo se indexa con su propio tipo de id;
- `Arena`, que reparte bytes desde bloques grandes;
- `Interner`, que convierte cada nombre en un entero.

`ki` no cambia; ejecutas los benchmarks del paquete:

```console
$ go test ./internal/base -run '^$' -bench 'Intern|UniqueMake' -benchmem
…
BenchmarkIntern-16        	72985978	        15.80 ns/op	       0 B/op	       0 allocs/op
BenchmarkUniqueMake-16    	46069453	        26.58 ns/op	       0 B/op	       0 allocs/op
```

Tus números variarán, pero no el orden de magnitud ni los `0 allocs/op`. El lexer usa `Interner` desde el capítulo 5; el parser, `IdVec` desde el 6, camino de M1 (capítulo 10).

## Teoría

### Parámetros de tipo y restricciones

El capítulo 3 te pidió copiar `func ptr[T any](v T) *T { return &v }`. Entre corchetes van los **parámetros de tipo**, cada uno con su **restricción**. `any`, alias de `interface{}`, la interfaz sin métodos, admite cualquier tipo.

Una restricción es una interfaz que puede contener un **conjunto de tipos**, y entonces solo sirve de restricción, no como tipo de una variable. `~uint32` es «todo tipo cuyo subyacente es `uint32`»: `uint32`, `FileID`, `ExprID`. Sin la tilde, `FileID` no entra, y el compilador la sugiere:

```text
FileID does not satisfy uint32 (possibly missing ~ for uint32 in uint32)
```

Un **tipo genérico** lleva parámetros igual que una función: `type IdVec[I ~uint32, T any] struct{ items []T }`. Sus métodos los nombran en el receptor: `func (v *IdVec[I, T]) Push(x T) I`. Se usa instanciado: `IdVec[FileID, string]`.

Un genérico se comprueba una vez, con `I` como incógnita: por eso la restricción decide qué puedes hacer **dentro**. Con `~uint32`, `I(len(v.items))` convierte un `int` en un id y `v.items[i]` indexa con un id, porque todos los tipos del conjunto son enteros. Con `any`, las dos expresiones fallan.

Otras dos restricciones:

- `comparable` admite `==` y `!=`, que `any` no permite.
- `cmp.Ordered` es la unión `~int | … | ~string`, los tipos con `<`.

:::from Rust
`[T cmp.Ordered]` se parece a `<T: PartialOrd>`, pero un trait se implementa a mano; un conjunto de tipos se describe por la forma.
:::

### Inferencia e instanciación explícita

Go deduce los argumentos de tipo de los **argumentos** de la llamada, nunca del resultado. `Max(1, 2.5)`, con `func Max[T cmp.Ordered](a, b T) T`, infiere `float64`.

`errors.AsType`, la versión genérica de `errors.As`, es `func AsType[E error](err error) (E, bool)`. `E` solo aparece en el resultado, así que hay que escribirlo, con la forma del *turbofish*:

```go
_, err := os.Open("/no/such/file.ki")
pe, ok := errors.AsType[*fs.PathError](err) // true, pe.Op == "open"
```

Frente a `errors.As(err, &target)`, no hace falta una variable previa, y un tipo que no es un `error` no compila.

### Métodos genéricos y lo que Go no tiene

Go 1.27 añade **métodos con parámetros propios** sobre tipos concretos:

```go
type Set struct{ items []int }

func (s *Set) Map[U any](f func(int) U) []U { … }
```

`s.Map(f)` infiere `U`, y `s.Map[float64](f)` lo da explícito. Tres límites:

- Exigen `go 1.27` en el `go.mod`.
- Sin llamar y sin un tipo función de destino, un valor de método necesita los argumentos de tipo: `f := s.Map[int]`, no `f := s.Map`.
- Una interfaz no puede declararlos: llama a sus métodos por una tabla fija, y uno genérico tendría una entrada por cada `U` posible.

Go tampoco tiene tipos de orden superior (`Functor[F[_]]`), especialización ni acceso a campos a través de una restricción.

:::from Rust
Un trait con `fn map<U>(&self, …)` deja de servir como `dyn`; Go lo prohíbe antes, en la declaración.
:::

:::from TS
`interface Mapper { map<U>(…) }` compila: con los tipos borrados, no hay tabla que rellenar. Go no los borra: `IdVec[FileID, string]` e `IdVec[ExprID, string]` son distintos al ejecutar.
:::

### Ids tipados

Confundir un índice de expresiones con uno de tipos será un error de compilación.

Dos tipos definidos son distintos aunque su subyacente coincida, y uno no se asigna al otro, salvo a una interfaz. Pasar un argumento es asignar. Por eso no compila `testdata/badids`, el programa de la sesión 1 que pasa un `otherID` a `Get`:

```text
testdata/badids/main.go:14:12: cannot use i (variable of uint32 type otherID) as base.FileID value in argument to v.Get
```

- **Una constante sin tipo es la excepción** (capítulo 2): el `0` de `v.Get(0)` toma el tipo `FileID`.
- **`FileID(i)` compila:** los ids tipados protegen de una equivocación, no de una conversión a propósito.

### `iter.Seq2` y `range` sobre funciones

`iter.Seq2[K, V]` es un tipo función: `func(yield func(K, V) bool)`. Un iterador llama a `yield` con cada par y para si devuelve `false`. `iter.Seq[V]` es la versión de un valor, como el `slices.Values` del capítulo 3.

En un generador de Python, `yield` suspende; aquí es un parámetro que el iterador llama.

`IdVec.All` devuelve un `iter.Seq2[I, T]`. `for i, x := range v.All()` convierte el cuerpo del bucle en `yield`, sin goroutines, y un `break` lo hace devolver `false`. `for range v.All()` recorre sin mirar los valores.

Si el iterador ignora el `false` y sigue, el runtime lo detiene:

```text
panic: runtime error: range function continued iteration after function for loop body returned false
```

:::from Rust
`Iterator` es de extracción: quien consume llama a `next()`. `iter.Seq2` es de empuje, como `for_each`.
:::

### Memoria sin punteros y el recolector

El recolector (capítulo 2) marca lo vivo recorriendo punteros desde las raíces: las variables globales y las pilas de las goroutines. Si el tipo de un objeto no contiene punteros, no hace falta recorrerlo.

Lo mide esta tabla, con un árbol binario de 4 000 000 de nodos:

| Forma | Objetos en el heap | Heap | Tiempo de `runtime.GC()`, `GOMAXPROCS=1` |
|---|---|---|---|
| `struct{ Left, Right *PNode; Val int64 }`, un `&PNode{}` por nodo | 4 000 118 | 91 MiB | 20–21 ms |
| `[]INode`, con `struct{ Left, Right uint32; Val int64 }` | 118 | 61 MiB | 35–43 µs |

Un `[]INode` es un solo objeto sin punteros, de esos 118: el recolector no entra en él. Es la razón de los ids.

`GOMAXPROCS` limita los hilos del sistema que ejecutan código Go a la vez, y el recolector reparte el marcado entre ellos.

:::more Cómo se midió la tabla
Todo se mide tras un `runtime.GC()`. Los nodos se reservan en orden y se enlazan después: el hijo izquierdo del nodo `i` es el `2i+1`, y el derecho, el `2i+2`.

El tiempo es el mejor de cinco `runtime.GC()` seguidos, y los objetos salen de `runtime.ReadMemStats` con `GOMAXPROCS=1`.
:::

`Arena` aplica la idea a los bytes: los textos del interner van seguidos en slabs, bloques `[]byte`, con una reserva por slab y no por texto.

**La arena no usa `unsafe`.** `unsafe` permitiría reinterpretar sus bytes como nodos: `(*Node)(unsafe.Pointer(&slab[0]))`. Pero el recolector seguiría viendo un `[]byte`: lo que apunte un nodo se libera aunque el nodo lo use. Además, habría que alinear a mano: cada nodo, en una dirección múltiplo de su alineación.

Por eso `unsafe` vive solo en `kirt/heap`, en el runtime de Ki (Parte VI).

:::from Rust
En `rustc_arena`, el borrow checker impide que una referencia viva más que la arena. Go no lo garantiza: la de Ki existe por la localidad y por reservar menos, no para liberar antes.
:::

### Compiladores: tablas, ids e interning

- **Almacenamiento orientado a datos.** Un árbol entero son unos pocos arrays grandes: cada clase de nodo vive en una tabla (`IdVec[ExprID, Expr]`) y nombra a otro por su id. Una lista de hijos es un `ListRange{First, Count}`, un trozo de un slice compartido: el pool.
- **Ids frente a punteros.** Un id ocupa 4 bytes, el recolector no lo recorre, y su tipo impide confundir tablas.
- **Interning.** Cada nombre se guarda una vez: comparar nombres es comparar enteros, y el `0` es «sin nombre».

### `hash/maphash`, semillas y el orden de los mapas

El interner busca cada nombre por su hash. `maphash.Bytes(seed, b)` da un hash de 64 bits; `maphash.MakeSeed()`, una semilla aleatoria. Misma semilla y texto, mismo hash; entre dos ejecuciones, no.

Los mapas se siembran al azar, y cada `range` empieza en un punto al azar: a diferencia de un `dict`, no conservan el orden de inserción.

Regla del proyecto: ninguna salida depende del orden de un mapa. El interner numera los textos por orden de llegada, así que nada visible depende de la semilla.

### `unique.Make` frente a un interner a mano

La biblioteca trae un interner, pero el compilador quiere otra cosa: un entero de 4 bytes sin punteros, numerado en orden, que indexe un `IdVec` y salga igual en cada ejecución.

`unique.Make[T comparable](value T) Handle[T]` da un puntero de 8 bytes que el recolector recorre, sin número. Dos handles son iguales si y solo si lo son sus valores, y compararlos es comparar punteros.

Es seguro entre goroutines, y el recolector borra sus entradas, punteros débiles, cuando no queda ningún handle.

Con 1000 textos conocidos, `Intern` gana, y la diferencia está en la búsqueda.

:::more Por qué `unique.Make` tarda más
Los dos benchmarks recorren textos guardados como `[]byte`. Un `[]byte` no es `comparable`, así que el segundo convierte cada texto con `string(words[i])`, sin reservar: los dos dan `0 allocs/op`.

El perfil (`go test -cpuprofile`, leído con `go tool pprof -top -cum`) pone en `canonMap.Load`, contando lo que llama, alrededor del 80 % del tiempo. En torno a una cuarta parte del total se va en convertir en fuerte el puntero débil de la entrada que encuentra. El interner de Ki no tiene ese paso: sus textos son slices normales.
:::

### Tests que documentan, miden y compilan

- **`ExampleXxx` es documentación que `go test` comprueba:** su stdout se compara con el comentario final `// Output:`. Sin el comentario, se compila y no se ejecuta. Va en `package base_test`, porque debe usar solo lo exportado.
- **`b.Loop` impide que el compilador borre lo que mides** (capítulo 2, ejercicio 3): dentro de su bucle, los argumentos y resultados de las llamadas siguen vivos.
- **`testing.AllocsPerRun(runs, f)` mide si algo reserva.** Devuelve la media de reservas de `runs` llamadas a `f`.
- **`os/exec` (capítulo 1) lanza el compilador desde un test,** para probar que algo no compila. `go test` corre en el directorio del paquete, así que `./testdata/badids` es una ruta válida.

## Tests primero

{{cards}}

Escribe el test de cada tarjeta, míralo fallar, implementa lo justo para el verde, y solo entonces despliega su referencia. Salvo el ejemplo, todo va en `package base`, porque varios tests miran campos sin exportar.

## Implementación

### Sesión 1: `IdVec`, ids tipados y la arena (≈ 3 h)

#### Los tests, en rojo

1. **Escribe T04.01–T04.11.** Describen esta interfaz (`64 << 10` es 64 desplazado 10 bits, 64 KiB):

```go
type IdVec[I ~uint32, T any] struct { items []T }
func (v *IdVec[I, T]) Push(x T) I
func (v *IdVec[I, T]) Get(i I) T
func (v *IdVec[I, T]) Ptr(i I) *T          // valid until the next Push
func (v *IdVec[I, T]) Len() int
func (v *IdVec[I, T]) Next() I              // the id Push would return
func (v *IdVec[I, T]) All() iter.Seq2[I, T]
type ListRange struct { First, Count uint32 }

type Arena struct { slabs [][]byte; cur []byte; used int; slabSize int }
func NewArena(slabSize int) *Arena          // <= 0 means 64 << 10; no slab until the first Alloc
func (a *Arena) Alloc(n int) []byte         // len n, cap n; Alloc(0) is nil
func (a *Arena) Copy(b []byte) []byte
func (a *Arena) Slabs() int
```

2. **Piezas de estos tests:**
   - `type cell struct{ x int }` es un tipo declarado dentro de la función de test, visible en sus subtests.
   - `for i := range first` recorre solo los índices de un slice.
   - `%#x` escribe `0xab`.
   - `mustPanic` (capítulo 2) exige el panic exacto.
   - Si `All` no para tras el `break`, un `recover()` diferido recoge el panic y el test falla con su mensaje.
3. **Escribe el programa que el test de los ids intenta compilar.** No debe compilar, y `go` no lo ve en `./...`, por estar bajo `testdata`:

{{file ch04-s1 internal/base/testdata/badids/main.go}}

4. **Míralos fallar.** No compilan:

```console
$ go test ./internal/base/
# github.com/aldezex/kilang/internal/base [github.com/aldezex/kilang/internal/base.test]
internal/base/arena_test.go:7:8: undefined: NewArena
…
internal/base/idvec_test.go:15:9: undefined: IdVec
internal/base/idvec_test.go:15:9: too many errors
FAIL	github.com/aldezex/kilang/internal/base [build failed]
FAIL
```

5. **Escribe las firmas con cuerpos de valor cero,** para un rojo por comportamiento, no por compilación:
   - `Get` hace `var zero T; return zero`.
   - `Ptr` devuelve `new(T)`; `NewArena`, `&Arena{}`; `All`, `func(yield func(I, T) bool) {}`. Un `nil` cortaría el binario.
   - De los subtests nuevos, solo pasa el de los ids: le basta la firma `Get(i I)`.

#### `IdVec`

{{file ch04-s1 internal/base/idvec.go}}

- `Ptr` vale solo hasta el siguiente `Push`: la trampa del `&files[i]` del capítulo 2, ahora en el contrato.
- `All` respeta el `false` de `yield`. Cambia el `if !yield(…) { return }` por un `yield(…)`: tras el `break`, el panic del runtime hace fallar el test. Deshazlo.

{{reftest T04.04}}

{{reftest T04.05}}

:::more El `go build` del test de los ids
Al test le basta saber que `go build` falla y qué dice el compilador: tira el binario y lee solo stderr.

`-o os.DevNull` tira el binario: es `/dev/null` en Linux y `NUL` en Windows. `cmd.Stderr = &stderr` conecta stderr a un `strings.Builder`.

Si hiciera falta más, `cmd.CombinedOutput()` da stdout y stderr mezclados, y `errors.AsType[*exec.ExitError](err)`, un error cuyo `ExitCode()` es el código.

`GOTOOLCHAIN=local` va al final de `cmd.Env`: si una clave se repite, gana la última.
:::

#### `SourceManager` pasa a `IdVec`

Los tests del capítulo 2 son la red de este cambio.

{{diff ch03-s3..ch04-s1 internal/base/source.go}}

- `files` pasa a `IdVec[FileID, file]`: los ficheros van seguidos en un array, sin un puntero por fichero, y `Push` puede moverlos. Por eso ningún método guarda un `*file`: pide `m.files.Ptr(id)`, lo usa y lo suelta.
- `Ptr` y no `Get`, porque `lineStarts` escribe `f.lines` la primera vez, y en una copia esa tabla se perdería.
- `line_table.go`, `load.go` y una línea de `line_table_test.go` cambian igual: `m.files[id]` pasa a `m.files.Ptr(id)`.

#### La arena

{{file ch04-s1 internal/base/arena.go}}

Una petición mayor que el slab recibe uno propio, y el actual sigue sirviendo a las pequeñas.

`a.cur[a.used : a.used+n : a.used+n]` es la expresión de corte completa del capítulo 2: un `append` de quien recibe los bytes no pisa la petición siguiente.

{{reftest T04.11}}

Si te atascas o tu versión se desvía demasiado, copia el fichero de `ref/ch04-s1` y sigue.

Al terminar: tus tests en verde, commit y etiqueta `ch04-s1`; compárate con `ref/ch04-s1`.

### Sesión 2: el interner (≈ 3 h)

#### Los tests, en rojo

1. **Escribe T04.12–T04.23.** Describen:

```go
type Symbol uint32
const NoSymbol Symbol = 0
type Interner struct {
	arena *Arena; seed maphash.Seed; index map[uint64]Symbol
	next []Symbol; texts [][]byte; hash func([]byte) uint64
}
func NewInterner() *Interner
func (in *Interner) Intern(b []byte) Symbol
func (in *Interner) InternString(s string) Symbol
func (in *Interner) Lookup(b []byte) (Symbol, bool)   // never inserts
func (in *Interner) Bytes(s Symbol) []byte             // zero-copy view, cap == len
func (in *Interner) String(s Symbol) string           // copies
func (in *Interner) Len() int
```

2. **Piezas de estos tests:**
   - `map[Symbol]string{}` es un mapa vacío escrito como literal.
   - `prev, dup := seen[sym]` es la búsqueda con dos resultados: el valor y si la clave estaba.
   - Un `const` también puede ir dentro de una función.
   - `func([]byte) uint64 { return 42 }`, el hash constante del test de colisiones, no nombra su parámetro porque no lo usa.
3. **Míralos fallar.** El rojo es `undefined: NewInterner`.
4. **Escribe las firmas con cuerpos de valor cero,** salvo `NewInterner`, que devuelve `&Interner{arena: NewArena(0)}` para no cortar el binario. También lo corta tomar `&v[0]` del `nil` de `Bytes`: mira antes `len(v)`. De los nuevos, solo pasa el que exige no reservar, por la razón equivocada: un `Intern` que siempre devuelve `NoSymbol` tampoco reserva.

#### El código

{{file ch04-s2 internal/base/interner.go}}

- **El mapa.** `make(map[uint64]Symbol)` crea un mapa vacío, como el literal. Una clave ausente da el valor cero: aquí, `NoSymbol`. Un slice `nil` admite `append`, pero un mapa `nil`, su valor cero, se puede leer y no escribir.
- **La cadena.** `index` lleva cada hash a su **primer** símbolo, y `next[sym]` encadena los demás. La igualdad la decide `bytes.Equal`, nunca el hash solo.
- **El hash** es un closure que lee `in.seed`; como captura `in`, `NewInterner` lo asigna después de crearlo.

Cambia `bytes.Equal(in.texts[s], b)` por `bytes.HasPrefix(b, in.texts[s])`. El test de colisiones falla: con el hash constante, todo texto encuentra el símbolo de `""`, que interna primero. Deshazlo y compara:

{{reftest T04.18}}

{{reftest T04.20}}

{{reftest T04.21}}

Si te atascas o tu versión se desvía demasiado, copia el fichero de `ref/ch04-s2` y sigue.

Al terminar: tus tests en verde, commit y etiqueta `ch04-s2`; compárate con `ref/ch04-s2`.

## Ejecútalo

Desde la raíz de tu repo:

```console
$ go test -count=1 ./internal/base/
ok  	github.com/aldezex/kilang/internal/base	0.085s
$ go test -count=1 -v -run 'ExampleInterner' ./internal/base/
=== RUN   ExampleInterner
--- PASS: ExampleInterner (0.00s)
PASS
ok  	github.com/aldezex/kilang/internal/base	0.002s
$ go test ./internal/base -run '^$' -bench 'Intern|UniqueMake' -benchmem
goos: linux
goarch: amd64
pkg: github.com/aldezex/kilang/internal/base
cpu: AMD Ryzen 7 7800X3D 8-Core Processor           
BenchmarkIntern-16        	72985978	        15.80 ns/op	       0 B/op	       0 allocs/op
BenchmarkUniqueMake-16    	46069453	        26.58 ns/op	       0 B/op	       0 allocs/op
PASS
ok  	github.com/aldezex/kilang/internal/base	2.384s
```

## Ejercicios

:::exercise
**1. `IdVec.Reserve(n)`.** Añade un método que haga sitio para `n` elementos más, un test que demuestre lo que garantiza y un benchmark que muestre su efecto con 100 000 `Push`.
:::

:::solution
`slices.Grow(s, n)` garantiza capacidad para `n` elementos más (`idvec.go` importa además `"slices"`):

```go
// Reserve makes room for n more elements, so the next n calls to Push do not
// move the elements to a new array.
func (v *IdVec[I, T]) Reserve(n int) {
	v.items = slices.Grow(v.items, n)
}
```

Lo observable es que un `Ptr` sobrevive a los `Push` reservados. Un subtest más en `idvec_test.go`:

```go
t.Run("reserve_keeps_pointers_valid", func(t *testing.T) {
	var v IdVec[FileID, cell]
	first := v.Push(cell{1})
	v.Reserve(100)
	p := v.Ptr(first)
	for i := 0; i < 100; i++ {
		v.Push(cell{i})
	}
	p.x = 42
	if got := v.Get(first).x; got != 42 {
		t.Errorf("after Reserve(100) and 100 pushes, a write through the old Ptr is lost: Get = %d, want 42", got)
	}
	if got := v.Len(); got != 101 {
		t.Errorf("Len = %d, want 101", got)
	}
})
```

Con un `Reserve` vacío falla: `… the old Ptr is lost: Get = 1, want 42`. El benchmark, en `idvec_bench_test.go`:

```go
package base

import "testing"

func BenchmarkPush(b *testing.B) {
	const n = 100_000
	b.Run("reserve", func(b *testing.B) {
		for b.Loop() {
			var v IdVec[FileID, uint64]
			v.Reserve(n) // the "no_reserve" sub-benchmark is the same without this line
			for i := range n {
				v.Push(uint64(i))
			}
		}
	})
}
```

Con el sub-benchmark `no_reserve` al lado y `-benchmem`: sin reservar, 0,80–1,08 ms, 4,1 MB y 28 reservas; con `Reserve`, 0,16–0,26 ms, 0,8 MB (100 000 × 8 bytes) y una.
:::

:::exercise
**2. `Arena.Reset()`.** Añade un `Reset` que vacíe la arena pero conserve sus slabs pequeños para reutilizarlos, con un test. ¿Qué pasa con los slices que la arena repartió antes?
:::

:::solution
Un campo nuevo, `free [][]byte`, guarda los slabs reutilizables. `Reset` los limpia con `clear` (un builtin que pone a cero los elementos de un slice sin cambiar su longitud), para que `Alloc` siga devolviendo ceros:

```go
// Reset empties the arena but keeps its small slabs, cleared, for the next
// requests, so an arena reused for many files allocates its slabs only once.
// Large slabs are dropped. Every slice handed out before Reset must no longer
// be used: later requests hand out the same memory again.
func (a *Arena) Reset() {
	for _, s := range a.slabs {
		if len(s) == a.slabSize {
			clear(s)
			a.free = append(a.free, s)
		}
	}
	clear(a.slabs)
	a.slabs = a.slabs[:0]
	a.cur, a.used = nil, 0
}
```

`clear(a.slabs)` suelta las referencias a los slabs grandes antes de recortar: si no, el array de `slabs` los mantendría vivos.

En `Alloc`, donde hoy está `a.cur = make([]byte, a.slabSize)`, el slab nuevo sale de `free` si queda alguno, y se apunta en `slabs` igual que uno nuevo:

```go
if k := len(a.free); k > 0 {
	a.cur = a.free[k-1]
	a.free = a.free[:k-1]
} else {
	a.cur = make([]byte, a.slabSize)
}
```

El test, en `arena_test.go`, compara direcciones (`&again[0] != &first[0]`: dos slices que empiezan en el mismo byte):

```go
t.Run("reset_reuses_slabs", func(t *testing.T) {
	a := NewArena(0)
	first := a.Alloc(16)
	first[0] = 0xFF
	a.Alloc(100 << 10) // large: Reset must drop it
	a.Reset()
	if got := a.Slabs(); got != 0 {
		t.Errorf("Slabs after Reset = %d, want 0", got)
	}
	again := a.Alloc(16)
	if &again[0] != &first[0] {
		t.Errorf("the first request after Reset did not reuse the old slab")
	}
	if again[0] != 0 {
		t.Errorf("reused memory was not cleared: again[0] = %#x, want 0", again[0])
	}
	big := a.Alloc(64 << 10) // fills a slab: the only spare is in use, so it is new
	if got := a.Slabs(); got != 2 || &big[0] == &first[0] {
		t.Errorf("Slabs = %d after a second slab-sized request, want 2 and a new slab", got)
	}
})
```

El test detecta tres errores: un `Reset` que crea una arena nueva no reutiliza; sin `clear`, `again[0] = 0xff`; y un slab grande guardado en `free` sería, como último, el primero en salir.

Los slices de antes de `Reset` siguen apuntando a la misma memoria y verán los textos nuevos: el aliasing del capítulo 2, del que el recolector no te protege. Por eso el comentario lo prohíbe.
:::

:::exercise
**3. ¿Compensa el interner?** Mide el interner frente a un `map[string]Symbol` con 100 000 identificadores: construirlo y buscar textos ya conocidos.
:::

:::solution
En `intern_map_bench_test.go`, con `benchWords` del `interner_test.go` de `ref/ch04-s2`:

```go
package base

import "testing"

type mapInterner struct {
	syms map[string]Symbol
}

func (m *mapInterner) Intern(b []byte) Symbol {
	if s, ok := m.syms[string(b)]; ok { // no allocation: the compiler sees a lookup
		return s
	}
	s := Symbol(len(m.syms) + 1)
	m.syms[string(b)] = s // allocates: the key must outlive b
	return s
}

func BenchmarkInternVsMap(b *testing.B) {
	words := benchWords(100_000)
	b.Run("interner_build", func(b *testing.B) {
		for b.Loop() {
			in := NewInterner()
			for _, w := range words {
				in.Intern(w)
			}
		}
	})
	b.Run("map_build", func(b *testing.B) {
		for b.Loop() {
			m := &mapInterner{syms: map[string]Symbol{}}
			for _, w := range words {
				m.Intern(w)
			}
		}
	})
	in := NewInterner()
	m := &mapInterner{syms: map[string]Symbol{}}
	for _, w := range words {
		if in.Intern(w) != m.Intern(w) {
			b.Fatalf("the two interners disagree on %q", w)
		}
	}
	b.Run("interner_known", func(b *testing.B) {
		for b.Loop() {
			for _, w := range words {
				in.Intern(w)
			}
		}
	})
	b.Run("map_known", func(b *testing.B) {
		for b.Loop() {
			for _, w := range words {
				m.Intern(w)
			}
		}
	})
}
```

El resultado:

| | tiempo | memoria | reservas |
|---|---|---|---|
| `interner_build` | 8,7 ms | 21,3 MB | 621 |
| `map_build` | 7,8 ms | 8,6 MB | 100 530 |
| `interner_known` (100 000 búsquedas) | 2,2 ms | 0 | 0 |
| `map_known` | 1,5 ms | 0 | 0 |

El mapa gana en tiempo y en memoria. Lo que compra el interner es otra cosa:

- 621 reservas frente a 100 530, porque el mapa guarda cada clave como un `string` propio, un objeto más que el recolector tiene que marcar (como en la tabla de la teoría), y el interner guarda los textos seguidos en unos pocos slabs;
- símbolos densos 1, 2, 3 que indexan un `IdVec` sin otra tabla;
- y `Bytes` sin copia.

Medir antes de decidir: la intuición «a mano será más rápido» aquí falla.
:::

:::exercise
**4. `IdVec.Map` como método genérico.** Escribe `func (v *IdVec[I, T]) Map[U any](f func(T) U) []U` con su test. ¿Qué cambia respecto a una función libre `Map(v, f)`?
:::

:::solution
```go
// Map returns f applied to every element, in id order: the result at index i
// is f(Get(I(i))).
func (v *IdVec[I, T]) Map[U any](f func(T) U) []U {
	out := make([]U, 0, len(v.items))
	for _, x := range v.items {
		out = append(out, f(x))
	}
	return out
}
```

```go
t.Run("map_applies_in_id_order", func(t *testing.T) {
	var v IdVec[FileID, string]
	for _, s := range []string{"a", "bb", "ccc"} {
		v.Push(s)
	}
	got := v.Map(func(s string) int { return len(s) })
	if len(got) != 3 || got[0] != 1 || got[1] != 2 || got[2] != 3 {
		t.Errorf("Map(len) = %v, want [1 2 3]", got)
	}
	var empty IdVec[FileID, string]
	if got := empty.Map[int](func(string) int { return 1 }); len(got) != 0 {
		t.Errorf("Map on an empty IdVec = %v, want []", got)
	}
})
```

La función libre necesita los tres parámetros, `func Map[I ~uint32, T, U any](v *IdVec[I, T], f func(T) U) []U`, y se llama `Map(&v, f)`; el método solo declara `U` y se llama `v.Map(f)`. Las dos infieren `U` de `f`. Lo que cambia:

- el método exige `go 1.27` en el `go.mod`;
- tiene que estar en el paquete de `IdVec` (una función libre puede vivir en cualquiera);
- `f := v.Map` no compila: hace falta `v.Map[int]`;
- y ninguna interfaz puede pedir un método `Map[U]`, así que no ganas polimorfismo, solo una llamada más cómoda.
:::

## Preguntas de repaso

:::review
¿Por qué `IdVec` restringe `I` con `~uint32` y no con `uint32` ni con `any`?
:::

:::answer
Con `uint32`, el conjunto tiene un solo tipo y `FileID` no lo cumple. Con `any`, el cuerpo no puede convertir un `int` en `I` ni indexar con `I`. `~uint32` admite todos los ids definidos sobre `uint32` y deja hacer con ellos lo que se hace con un entero.
:::

:::review
¿Por qué `All` tiene que mirar lo que devuelve `yield`, y qué pasa si no lo hace?
:::

:::answer
Porque `range` convierte el cuerpo del bucle en `yield`, y un `break` (o un `return`) en el cuerpo se traduce en que `yield` devuelve `false`: el bucle ya terminó. Si `All` siguiera llamando a `yield`, ejecutaría un cuerpo que ya salió. Go no lo detecta al compilar, sino al ejecutar: la siguiente llamada hace panic.
:::

:::review
¿Por qué `errors.AsType[*fs.PathError](err)` necesita los corchetes y `Max(1, 2.5)` no?
:::

:::answer
Go infiere los argumentos de tipo de los argumentos de la llamada. En `Max`, `T` aparece en los parámetros; en `AsType`, `E` solo aparece en el resultado, y del resultado Go no infiere nada.
:::

:::review
¿Por qué guardar 4 000 000 de nodos en un `[]INode` con ids hace que `runtime.GC()`, con `GOMAXPROCS=1`, pase de decenas de milisegundos a decenas de microsegundos?
:::

:::answer
Porque el recolector recorre punteros. Con un puntero por hijo hay cuatro millones de objetos que marcar y seguir; el `[]INode` es un solo objeto sin punteros, que se marca sin entrar en él.
:::

:::review
¿Por qué el interner compara con `bytes.Equal` aunque los hashes coincidan, y por qué `hash` es un campo?
:::

:::answer
Porque dos textos distintos pueden compartir hash: el hash solo elige la cadena, y la igualdad la decide el contenido. Es un campo para que un test fuerce colisiones; con un hash real de 64 bits casi nunca ocurren, y la cadena `next` quedaría sin probar.
:::

:::review
¿Por qué el compilador usa su propio interner y no `unique.Make`, si este es más simple y seguro entre goroutines?
:::

:::answer
Porque necesita un entero pequeño, sin punteros, numerado en orden y estable entre ejecuciones, que indexe tablas. Un `Handle` es un puntero de 8 bytes que el recolector recorre, sin número, y su seguridad entre goroutines no sirve a un compilador secuencial.
:::

:::review
¿Por qué `Ptr` vale solo hasta el siguiente `Push`, y qué hacen con eso los métodos de `SourceManager`?
:::

:::answer
Porque `Push` hace un `append`, que puede mover los elementos a un array nuevo, y el puntero seguiría en el viejo: las escrituras se perderían sin error. Los métodos piden el puntero, lo usan y lo sueltan dentro de la misma llamada, sin guardarlo.
:::

## Resumen

- Una restricción decide qué puede hacer un genérico por dentro: `~uint32`, `comparable`, `cmp.Ordered`.
- Go infiere de los argumentos, nunca del resultado: `errors.AsType` lleva corchetes.
- Go 1.27 admite métodos con parámetros propios, no en interfaces.
- Mezclar dos ids tipados es un error de compilación.
- `range` convierte el cuerpo del bucle en `yield`; el iterador debe parar cuando devuelve `false`.
- El recolector no entra en lo que no tiene punteros: ahí ganan los ids y los slabs.
- `maphash` y los mapas se siembran al azar: ninguna salida depende de ellos.
- `// Output:` hace de un `Example` un test; `AllocsPerRun` y `-benchmem` miden las reservas.

## Go en el trabajo

**Cuándo usar genéricos.** `IdVec` encaja: el mismo código para todas sus tablas. «When To Use Generics» (blog de Go) los recomienda para funciones sobre slices, mapas y canales, estructuras de datos de propósito general y un método idéntico en todos los tipos. Si solo llamas a un método del valor, usa una interfaz.

**El `IndexVec` de rustc es el mismo patrón.** Usarlo con el índice equivocado «will fail at compile time». Cada newtype necesita un `impl Idx`; en Go basta `type ExprID uint32`.

**Grafos sin punteros.** Según la guía del recolector (go.dev/doc/gc-guide), las estructuras con índices, «while less well-typed, may perform better». Solo compensa si el grafo es complejo y el recolector pasa mucho tiempo marcando.

Los ids tipados recuperan buena parte de lo «less well-typed», pero no el rango: un id fuera de su tabla es un panic al ejecutar.
