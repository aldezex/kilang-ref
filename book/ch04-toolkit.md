---
chapter: 4
title: Genéricos y la caja de herramientas del compilador
part: I
hours: 6
milestone: `IdVec[I, T]`, `Arena` e `Interner`, testeados y medidos
---

## Objetivo

Al terminar, `internal/base` tiene las tres piezas sobre las que se apoyará todo el compilador: `IdVec[I, T]`, un slice que solo se indexa con su propio tipo de id; `Arena`, que reparte bytes desde bloques grandes; e `Interner`, que convierte cada nombre en un entero. `ki` no cambia; lo que ejecutas al final son sus tests y sus benchmarks:

```console
$ go test -count=1 ./internal/base/
ok  	github.com/aldezex/kilang/internal/base	0.085s
$ go test ./internal/base -run '^$' -bench 'Intern|UniqueMake' -benchmem
…
BenchmarkIntern-16        	72985978	        15.80 ns/op	       0 B/op	       0 allocs/op
BenchmarkUniqueMake-16    	46069453	        26.58 ns/op	       0 B/op	       0 allocs/op
```

Los números son de esta máquina; los tuyos variarán, pero no el orden de magnitud ni los `0 allocs/op`. Desde el capítulo 5, el lexer interna cada identificador, y desde el 6, el parser guarda sus nodos en `IdVec`s, camino del hito M1 (capítulo 10).

## Teoría

### Parámetros de tipo y restricciones

El capítulo 3 te pidió copiar `func ptr[T any](v T) *T { return &v }` sin explicarla. Entre corchetes van los **parámetros de tipo**, cada uno con su **restricción**: `T any` dice «`T` puede ser cualquier tipo» (`any` es un alias de `interface{}`, la interfaz sin métodos, que todo tipo cumple). Quien llama a `ptr(101)` obtiene un `*int` y con `ptr("x")`, un `*string`: la función se comprueba una vez, con `T` como incógnita, y vale para todos.

Una restricción es una interfaz, y en ella se puede escribir un **conjunto de tipos**. `~uint32` es «todo tipo cuyo tipo subyacente es `uint32`»: `uint32`, `FileID`, `ExprID`.

Un **tipo genérico** lleva parámetros igual que una función, `type IdVec[I ~uint32, T any] struct{ items []T }`, y sus métodos los nombran en el receptor: `func (v *IdVec[I, T]) Push(x T) I`. Un tipo genérico no se usa sin instanciar: `var v IdVec` da `cannot use generic type IdVec[I ~uint32, T any] without instantiation`, y `IdVec[FileID, string]` sí es un tipo; `IdVec[int, string]`, no: `int does not satisfy ~uint32 (int missing in ~uint32)`. Sin la tilde, con `[I uint32, T any]`, el conjunto tendría un solo tipo, `uint32`, y tampoco entraría `FileID`; el compilador sugiere la tilde:

```text
FileID does not satisfy uint32 (possibly missing ~ for uint32 in uint32)
```

La restricción decide qué puedes hacer **dentro**. En `IdVec`, con `[I ~uint32, T any]`, `I(len(v.items))` convierte un `int` en un id y `v.items[i]` indexa con un id, porque todos los tipos del conjunto son enteros. Con `[I any, T any]`, las dos líneas fallan: `cannot convert len(v.items) (value of type int) to type I` y `invalid argument: index i (variable of type I constrained by any) must be integer`.

Dos restricciones de la biblioteca: `comparable` admite `==` y `!=` (con `any`, `a == b` da `invalid operation: a == b (incomparable types in type set)`, y un `[]int`, `[]int does not satisfy comparable`); y `cmp.Ordered` es la unión `~int | ~int8 | … | ~float64 | ~string`, los tipos con `<`, la de `slices.BinarySearch[S ~[]E, E cmp.Ordered]` del capítulo 2. Una interfaz con un conjunto de tipos solo sirve de restricción: `var x cmp.Ordered` da `cannot use type cmp.Ordered outside a type constraint: interface contains type constraints`.

:::from Rust
`[T any]` es `<T>`, y la restricción hace de *trait bound*: `[T cmp.Ordered]` se parece a `<T: Ord>`. La diferencia es de qué está hecha. Un bound de Rust es una lista de traits implementados a mano; un conjunto de tipos de Go se describe por la forma (`~uint32`, una unión), y ningún tipo declara que lo cumple. Lo más parecido a `~uint32` es el trait `Idx` de rustc, que implementa cada newtype de índice.
:::

:::from TS
`<T extends Foo>` restringe por forma, como Go, pero TS borra los tipos al compilar. Go no los borra: un `IdVec[FileID, string]` y un `IdVec[ExprID, string]` siguen siendo tipos distintos al ejecutar, y en un `package main` que declare los tres tipos, `%T` imprime `main.IdVec[main.FileID,string]` y `main.IdVec[main.ExprID,string]`.
:::

### Inferencia e instanciación explícita

Go deduce los argumentos de tipo a partir de los **argumentos** de la llamada: `Max(1, 2.5)`, con `func Max[T cmp.Ordered](a, b T) T`, infiere `float64` y da `2.5`. Nunca los deduce del resultado. `errors.AsType`, la versión genérica de `errors.As` que el capítulo 3 dejó pendiente, es `func AsType[E error](err error) (E, bool)`: `E` solo aparece en el resultado, así que hay que escribirlo.

```go
_, err := os.Open("/no/such/file.ki")
pe, ok := errors.AsType[*fs.PathError](err) // true, pe.Op == "open"
```

Sin los corchetes: `in call to errors.AsType, cannot infer E (declared at $GOROOT/src/errors/wrap.go:167:13)`, también si asignas el resultado a un `pe` ya declarado como `*fs.PathError`. Frente a `errors.As(err, &target)`, no hace falta declarar antes una variable ni pasar un puntero a puntero, y un tipo que no es un `error` no compila.

:::from Rust
`errors.AsType[*fs.PathError](err)` es el *turbofish* de `"5".parse::<i32>()`. Rust también infiere del uso del resultado (`let n: i32 = s.parse()?`); Go, nunca.
:::

### Métodos genéricos, nuevos en Go 1.27, y lo que Go no tiene

Hasta Go 1.26, un método solo podía usar los parámetros de su tipo. Go 1.27 añade **métodos con parámetros propios** sobre tipos concretos (`docs/toolchain.md`, fijado por `TestToolchainFacts/generic_method_on_concrete_type`):

```go
type Set struct{ items []int }

func (s *Set) Map[U any](f func(int) U) []U { … }
```

`s.Map(func(x int) string { … })` infiere `U`, y `s.Map[float64](…)` lo da explícito. Tres límites, comprobados en Go 1.27.3:

- Con `go 1.26` en el `go.mod`: `generic method requires go1.27 or later (-lang was set to go1.26; check go.mod)`.
- Un valor de método necesita los argumentos: `f := s.Map` da `cannot use generic function s.Map without instantiation`; `s.Map[int]` sí vale.
- Una interfaz no puede declarar un método genérico: `interface method must have no type parameters`, un error de sintaxis que también da `gofmt`. Un valor de interfaz llama a sus métodos por una tabla fija, y un método genérico tendría una entrada por cada `U` posible.

Lo que Go no tiene: tipos de orden superior (no hay un `Functor[F[_]]`; `type Functor[F[_] any] interface{}` da `undefined array length _ or missing type constraint`, porque el parser lee `F[_]` como un array), especialización, ni acceso a campos a través de una restricción: con `T interface{ ~struct{ X int } }`, `t.X` da `t.X undefined (type T has no field or method X)`.

:::from Rust
Rust deja declarar `fn map<U>(&self, …)` en un trait, pero ese trait deja de servir como `dyn`: `&dyn Mapper` da ``error[E0038]: the trait `Mapper` is not dyn compatible`` (rustc 1.97). Go prohíbe lo mismo un paso antes, en la declaración. Rust tampoco tiene tipos de orden superior; los GAT cubren parte de sus usos.
:::

:::from TS
`interface Mapper { map<U>(f: (x: number) => U): U[] }` compila con `tsc --strict`: con los tipos borrados, no hay tabla que rellenar por cada `U`.
:::

### Ids tipados: `FileID` y `ExprID` no se mezclan

El capítulo 2 presentó `type FileID uint32`. Un `IdVec[FileID, file]` tiene `Get(i FileID)`, y pasarle un id de otro tipo con el mismo subyacente no compila; es lo que fija `testdata/badids`:

```text
testdata/badids/main.go:14:12: cannot use i (variable of uint32 type otherID) as base.FileID value in argument to v.Get
```

Entre dos tipos con nombre que no son interfaces, asignar (y pasar un argumento es asignar) exige que sean el mismo tipo, y dos tipos definidos son distintos aunque su subyacente coincida. (Una constante sin tipo es la excepción, como en el capítulo 2: el `0` de `v.Get(0)`, que escribirás en T04.02, toma el tipo `FileID` que pide el parámetro. Una variable `uint32` no: `cannot use u (variable of type uint32) as base.FileID value in argument to v.Get`.) La conversión explícita, `FileID(i)`, sí compila: los ids tipados protegen de una equivocación, no de una conversión hecha a propósito. Cuando el parser tenga `IdVec[ExprID, Expr]` y el comprobador `IdVec[TypeID, Type]`, confundir los dos índices será un error de compilación y no una expresión con el tipo de otra.

:::from Rust
Es `rustc_index`: `IndexVec<I: Idx, T>` y los índices que genera `newtype_index!`. En Rust, cada newtype necesita un `impl Idx`, que escribe por ti la macro; en Go basta una línea, `type ExprID uint32`, porque nadie declara que cumple `~uint32`: la restricción acepta todos los ids por su forma.
:::

### `iter.Seq2` y `range` sobre funciones

`iter.Seq2[K, V]` es un tipo función: `func(yield func(K, V) bool)`. Un iterador llama a `yield` con cada par y para si `yield` devuelve `false`. `for i, x := range v.All()`, con un `All` de `IdVec` que devuelve un `iter.Seq2[I, T]`, convierte el cuerpo del bucle en esa función `yield`: un `break` la hace devolver `false`. Si el iterador ignora el `false` y sigue, el runtime lo detiene:

```text
panic: runtime error: range function continued iteration after function for loop body returned false
```

En la traza, el cuerpo del bucle aparece como `main.main-range1`: es una función que el compilador sacó del bucle, sin goroutines. `for range v.All()`, sin variables, recorre sin mirar los valores; `iter.Seq[V]` es la versión de un valor, como el `slices.Values` del capítulo 3.

:::from Rust
`Iterator` de Rust es de extracción: quien consume llama a `next()`. `iter.Seq2` es de empuje, como `for_each`; el `bool` de `yield` hace de `ControlFlow::Break` en `try_for_each`. Lo que en Rust devuelve `impl Iterator<Item = (I, &T)>`, aquí devuelve un closure.
:::

:::from Python
Se parece a un generador, pero `yield` no es una palabra clave que suspende la función: es un parámetro, una función que el iterador llama.
:::

### Memoria sin punteros y el recolector

El recolector (capítulo 2) marca lo vivo recorriendo punteros desde las raíces, las variables globales y las pilas de las goroutines. Un objeto cuyo tipo no contiene punteros no hace falta recorrerlo: `runtime/mgcmark.go`, en Go 1.27, lo da por marcado y recorrido de una vez («If this is a noscan object, fast-track it to black instead of greying it»: en su jerga, negro es «marcado y recorrido», y gris, «marcado, con hijos por recorrer»).

Se midió en esta máquina un árbol binario de 4 000 000 de nodos, con los nodos reservados en orden y enlazados después (el hijo izquierdo del nodo `i` es el `2i+1`, y el derecho, el `2i+2`). Todo se mide tras un `runtime.GC()`: el tiempo es el mejor de cinco `runtime.GC()` seguidos, y los objetos salen de `runtime.ReadMemStats` con `GOMAXPROCS=1`. `GOMAXPROCS` es el límite de hilos del sistema que ejecutan código Go a la vez; se fija con la variable de entorno del mismo nombre o con `runtime.GOMAXPROCS`. Si no lo fijas, el runtime lo calcula a partir de las CPU lógicas, la afinidad del proceso y, en Linux, la cuota de CPU del cgroup (16 en esta máquina), y el recolector reparte el marcado entre esos hilos; por eso la tabla tiene dos columnas de tiempo:

| Forma | Objetos en el heap | Heap | `runtime.GC()`, `GOMAXPROCS=1` | con 16 hilos |
|---|---|---|---|---|
| `struct{ Left, Right *PNode; Val int64 }`, un `&PNode{}` por nodo | 4 000 118 | 91 MiB | 20–21 ms | 4,2–4,3 ms |
| `[]INode`, con `struct{ Left, Right uint32; Val int64 }` | 118 | 61 MiB | 35–43 µs | 0,19–0,31 ms |

Un `[]INode` es un solo objeto sin punteros: el recolector no entra en él. Es la razón de los ids. `Arena` aplica la misma idea a los bytes: los textos van seguidos en slabs, bloques `[]byte` de 64 KiB por omisión, y cuesta una reserva por slab y no una por texto. (El interner sí guarda una cabecera `[]byte` por texto, y cada cabecera lleva un puntero; los slabs a los que apuntan no se recorren.)

**La arena sin `unsafe`, y lo que costaría con él.** La nuestra solo reparte `[]byte`. Con `unsafe` podría repartir nodos tipados, `n := (*Node)(unsafe.Pointer(&slab[0]))`, pero el recolector sigue viendo un `[]byte` y no mira dentro: si `Node` tiene un puntero, lo que apunta se libera aunque `n` lo use. Comprobado con `runtime.AddCleanup`, que registra una función que corre cuando su objeto ya es inalcanzable y el recolector lo libera. Sobre un `*string` guardado solo en `n.Name`, tras dos `runtime.GC()` el cleanup corre, y `go vet` no dice nada. Habría además que alinear a mano, es decir, que cada nodo empiece en una dirección múltiplo de su alineación (`unsafe.Alignof`, 8 en amd64 para un struct con punteros). Por eso `unsafe` vive solo en `kirt/heap` (Parte VI). Go 1.27 trae un paquete `arena`, pero solo con `GOEXPERIMENT=arenas`, y Ki no usa ningún `GOEXPERIMENT`: es una regla del proyecto.

:::from Rust
rustc también reparte con arenas (el crate `rustc_arena`): `alloc(&self, object: T) -> &mut T` ata la referencia devuelta al préstamo de la arena, y el borrow checker impide que viva más que ella. Go no tiene esa garantía: la arena sin `unsafe` existe por la localidad y por reservar menos, no para liberar antes.
:::

### `hash/maphash`, semillas y el orden de los mapas

`maphash.Bytes(seed, b)` da un hash de 64 bits; `maphash.MakeSeed()`, una semilla aleatoria. Con la misma semilla, el mismo texto da el mismo hash; entre dos ejecuciones, no: `"fib"` dio `471cc675105a8475` y `122dc048e782e3e7`. Los mapas de Go también se siembran al azar, cada uno con su semilla, y además cada `range` empieza en un punto al azar: seis `for k := range m` seguidos sobre el mismo mapa de cinco claves, en un solo proceso, dieron `abcde deabc abcde bcdea bcdea bcdea`. De ahí una regla del proyecto: ninguna salida depende del orden de un mapa. El interner numera los símbolos en el orden en que llegan los textos, así que nada visible depende de la semilla.

:::from Python
Es `PYTHONHASHSEED`: el hash de un `str` cambia entre procesos. Pero un `dict` conserva el orden de inserción, y un mapa de Go no conserva ninguno.
:::

:::from Rust
El `HashMap` de Rust usa `RandomState`: claves aleatorias por hilo que `RandomState::new` incrementa en cada mapa nuevo, así que dos mapas con las mismas claves pueden iterar en órdenes distintos («arbitrary order», dice su documentación).
:::

### `unique.Make` frente a un interner a mano

La biblioteca ya trae un interner: `unique.Make[T comparable](value T) Handle[T]`. Dos handles son iguales si y solo si lo son sus valores, y compararlos es comparar punteros: `Handle[T]` es un `struct{ value *T }`. Es seguro entre goroutines y, como sus entradas son punteros débiles (`weak.Pointer` en `unique/canonmap.go`), el recolector las borra cuando no queda ningún handle.

El compilador quiere otra cosa: un entero de 4 bytes sin punteros, numerado 1, 2, 3 en orden, que pueda indexar un `IdVec` y que salga igual en cada ejecución. Un `Handle` ocupa 8 bytes, es un puntero que el recolector recorre y no tiene número. En velocidad, `BenchmarkIntern` y `BenchmarkUniqueMake` recorren los mismos 1000 textos conocidos, guardados como `[]byte`. Un `[]byte` no es `comparable` (`unique.Make` de uno da `[]byte does not satisfy comparable`), así que el segundo convierte cada texto con `string(words[i])`. En esta máquina dan 15,8 ns frente a 26,6 ns por llamada, y los dos `0 B/op` y `0 allocs/op`: ninguno reserva memoria. La diferencia no está en las reservas ni en la conversión, que no escapa. Lo dice `go test -c -o /dev/null -gcflags=-m ./internal/base 2>&1 | grep 'string(words'` (`go test -c` compila el binario de test sin ejecutarlo, `-o /dev/null` lo tira, y `-gcflags=-m`, del capítulo 2, escribe por stderr las decisiones de escape, también las de los tests, y `2>&1` las pasa a stdout, que es lo que lee `grep`): `string(words[i]) does not escape`.

La diferencia está en la búsqueda misma: un perfil de CPU del benchmark (`go test -cpuprofile`, leído con `go tool pprof -top -cum`) pone en `canonMap.Load`, contando lo que llama, la mayor parte del tiempo, alrededor del 80 %, con bastante variación entre ejecuciones. En torno a una cuarta parte del tiempo total (también variable) se va en convertir en fuerte el puntero débil de la entrada que encuentra, un paso que el interner de Ki no tiene: sus vistas son slices normales.

:::from Rust
rustc tiene su propio interner, y se parece al de Ki: en `compiler/rustc_span/src/symbol.rs`, un `Symbol` envuelve un índice de 32 bits, `SymbolIndex`, generado con `newtype_index!`, y el interner copia cada texto a una `DroplessArena` y guarda en un `Vec` la referencia de cada símbolo. Para eso necesita un `unsafe`, que alarga esas referencias a `'static` («we only access these while the arena is still alive»). En Go, el recolector mantiene vivos los slabs mientras las vistas `[]byte` del interner los apunten, y no hace falta.
:::

### Tests `Example`, `b.Loop`, `AllocsPerRun` y `os/exec`

- **`ExampleXxx`.** Una función `Example` + el nombre de lo que ilustra (`ExampleInterner`, del tipo `Interner`) que termina en un comentario `// Output:` es un test: `go test` la ejecuta y compara su stdout con el comentario. Va en `package base_test`, porque es documentación y debe usar solo lo exportado. Si la salida difiere, imprime `got:` y `want:`; `// Unordered output:` ignora el orden de las líneas, y sin comentario de salida se compila pero no se ejecuta.
- **`b.Loop`.** El capítulo 2 lo enseñó con `-benchmem`. Lo nuevo: dentro de `for b.Loop() { … }`, los argumentos y resultados de las llamadas se mantienen vivos, así que el compilador no puede borrar un `in.Intern(words[i])` cuyo resultado nadie usa.
- **`testing.AllocsPerRun(runs, f)`** ejecuta `f` una vez para calentar y devuelve la media de reservas de las `runs` siguientes, siempre un número entero, con `GOMAXPROCS` a 1.
- **`os/exec` desde un test.** `exec.Command` y `cmd.Env` son del capítulo 1. En `cmd.Env = append(os.Environ(), "GOTOOLCHAIN=local")`, si una clave se repite gana la última. `cmd.Stderr = &stderr` conecta el stderr del proceso a un `strings.Builder` (con `&`: su `Write` es de `*strings.Builder`), y `os.DevNull` es `/dev/null` en Linux y `NUL` en Windows. `cmd.Run()` devuelve un error si el proceso sale con un código distinto de 0. Para otros usos: `out, err := cmd.CombinedOutput()` devuelve stdout y stderr mezclados, y `ee, ok := errors.AsType[*exec.ExitError](err)` da el código con `ee.ExitCode()`, que para el `go build` de `badids` es 1. El test captura solo stderr porque busca en él dos textos del compilador, y le basta con saber que falló.

:::from Rust
Un `Example` es un doctest cuyo `assert_eq!` es el comentario `// Output:`. Y el test que compila `testdata/badids` es un doctest `compile_fail`: el de `IndexVec` lleva uno, `&vec1[idx2]; // Compile error!`.
:::

### Compiladores: datos en tablas, ids e interning

- **Almacenamiento orientado a datos.** Cada clase de nodo vive en una tabla (`IdVec[ExprID, Expr]`) y nombra a otro nodo por su id; las listas de hijos son un `ListRange{First, Count}` sobre un pool compartido. Un árbol entero son unos pocos arrays grandes.
- **Ids frente a punteros.** Un id ocupa 4 bytes, el recolector no lo recorre, y su tipo impide confundir tablas.
- **Interning.** Cada nombre se guarda una vez como un `Symbol`: comparar nombres es comparar enteros, y el `0` queda para «sin nombre».

## Tests primero

Las tarjetas del capítulo, con valores exactos:

{{cards}}

Úsalas como siempre: escribe el test de la tarjeta, míralo fallar, implementa lo justo para el verde, y solo entonces despliega la referencia con su `{{reftest}}`. Todo va en `package base`, porque varios tests miran campos sin exportar (`slabSize`, `in.hash`, `in.index`), salvo `ExampleInterner`, en `base_test`. Las dos tarjetas `bench` no tienen umbral: `go test` no las ejecuta sin `-bench`.

## Implementación

### Sesión 1: `IdVec`, ids tipados y la arena (≈ 3 h)

#### Los tests, en rojo

T04.01–T04.11 describen esta interfaz:

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

`64 << 10` es 64 desplazado 10 bits, 65 536: 64 KiB. Piezas nuevas en los tests: `type cell struct{ x int }` es un tipo declarado dentro de `TestIdVec`, visible en sus subtests; `for i := range first` recorre solo los índices de un slice; `%#x` escribe `0xab`. T04.02 reutiliza `mustPanic`, el ayudante de `line_table_test.go` del capítulo 2, para exigir que `Get` y `Ptr` de un id que no existe hagan panic con el mensaje exacto del runtime, `runtime error: index out of range [2] with length 2`: así un `FileID` equivocado no se convierte en silencio en un fichero vacío. T04.04 mete su bucle con `break` en una función literal que se llama en el acto y difiere una función que llama a `recover()`, el patrón de T02.07: si `All` no para, el panic del runtime se recupera y el subtest falla con un mensaje, en lugar de cortar el binario de test.

Para T04.05 escribes `internal/base/testdata/badids/main.go`, un programa que no debe compilar y que el comando `go` no ve en `./...`, porque está bajo `testdata`. El test lee primero ese fichero con `os.ReadFile`, aunque solo lo compile el `go build` hijo: `go test` guarda en caché un resultado en verde mientras no cambien los ficheros que el propio test abre, y sin esa lectura, una edición que hiciera compilar el programa dejaría un `ok (cached)` falso. El `go build` hijo lleva `-buildvcs=false`: sin él, `go build` pregunta a git por el estado del checkout para grabarlo en el binario (que aquí se tira), y si git rechaza el directorio, por ser de otro usuario como en la CI del capítulo 1, se para con `error obtaining VCS status` antes de compilar. El programa:

{{file ch04-s1 internal/base/testdata/badids/main.go}}

`go test` corre en el directorio del paquete, así que `./testdata/badids` es una ruta relativa válida. El rojo:

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

Para un rojo de comportamiento, escribe primero las firmas con cuerpos que devuelvan el valor cero, con tres excepciones que no pueden ser `nil`: `Ptr` devuelve `new(T)`, `NewArena` devuelve `&Arena{}` y `All` devuelve un iterador que no produce nada, `func(yield func(I, T) bool) {}`. Con un `nil` en cualquiera de las tres, un test lo desreferencia o lo recorre con `range` y hace panic, y un panic sin recuperar corta el binario de test entero y se lleva por delante los subtests que faltan. No toques todavía `SourceManager`: sobre estos stubs, cada `Ptr` daría un `file` vacío nuevo, los tests del capítulo 2 fallarían, y `TestLineTable/first_char` haría panic con `base: offset 1 out of range (file has 0 bytes)` y cortaría el binario. Así, sin tocarlo, compila, y cada subtest falla con su mensaje (`Push("b") = 0, want 1`, `Alloc(5): len = 0, cap = 0, want 5 and 5`, `Get(0): did not panic, want panic "runtime error: index out of range [0] with length 0"`…) menos uno: `distinct_id_types_do_not_mix` ya pasa, porque lo que comprueba es una regla del compilador y le basta la firma `Get(i I)`.

#### El código

{{file ch04-s1 internal/base/idvec.go}}

- `I(len(v.items))` y `v.items[i]` son posibles por `~uint32`, como en la teoría. `Push` devuelve el id que `Next` anunciaba.
- `Get` devuelve una copia; `Ptr`, la dirección del elemento, válida solo hasta el siguiente `Push`: es la trampa del `&files[i]` del capítulo 2, ahora escrita en el contrato.
- `All` devuelve un closure que respeta el `false` de `yield`. Quita el `if !yield(…) { return }` y llama a `yield` sin mirar: el `break` de T04.04 hace que `yield` devuelva `false`, el iterador vuelve a llamarlo, y el runtime de Go lo corta con un panic. El test lo recupera y falla con `All kept iterating after the loop body left the loop with break: runtime error: range function continued iteration after function for loop body returned false`.

El capítulo 2 anunció que `IdVec` cambiaría `SourceManager`:

{{diff ch03-s3..ch04-s1 internal/base/source.go}}

`files` pasa de `[]*file` a `IdVec[FileID, file]`: los ficheros van seguidos en un array, sin un puntero por fichero, y `Push` puede moverlos. Por eso ningún método guarda un `*file` entre dos llamadas: cada uno pide `m.files.Ptr(id)`, lo usa y lo suelta. `Ptr` y no `Get`, porque `lineStarts` escribe `f.lines` la primera vez y en una copia esa tabla se perdería; de hecho, `m.files.Get(id).lineStarts()` ni compila, porque el valor que devuelve `Get` no tiene dirección: `cannot call pointer method lineStarts on file`. `line_table.go` y `load.go` cambian igual (`m.files[id]` → `m.files.Ptr(id)`), y los tests del capítulo 2 siguen en verde sin tocarlos, salvo una línea de `table_is_built_once`.

{{file ch04-s1 internal/base/arena.go}}

- `Alloc(0)` devuelve `nil` sin abrir un slab: pedir cero bytes no cuesta 64 KiB. T04.06 lo compara con `nil`, y no solo mira `len` y `cap`, porque un `[]byte{}` también tiene los dos a 0: si `Alloc(0)` devolviera `[]byte{}`, fallaría con `Alloc(0) = []byte{}, want nil`.
- Una petición mayor que el slab recibe uno propio, que se apunta en `slabs` pero no pasa a ser `cur`: el slab actual sigue sirviendo peticiones pequeñas.
- Una petición pequeña que no cabe en lo que queda de `cur` abre un slab nuevo y empieza en 0. Quita `a.used = 0` y T04.11 revienta: `slice bounds out of range [::40] with capacity 32`.
- `a.cur[a.used : a.used+n : a.used+n]` es la expresión de corte completa del capítulo 2: la capacidad es `n`, y un `append` de quien recibe los bytes no pisa la petición siguiente. Sin el tercer índice, T04.06 lo ve: `Alloc(5): len = 5, cap = 65536, want 5 and 5`.

T04.11 mira los dos lados de cada borde: 20 + 20 en 32 bytes abre otro slab, 12 justos no; 32, el tamaño exacto, es pequeño y llena un slab que pasa a ser el actual, así que el siguiente byte abre un tercero; 33 es grande.

{{reftest T04.04}}

{{reftest T04.05}}

{{reftest T04.11}}

El mensaje de la etiqueta: `ch04-s1: IdVec, typed ids and the arena`.

Si te atascas o tu versión se desvía demasiado, copia el fichero de `ref/ch04-s1` y sigue.

Al terminar: tus tests en verde, commit y etiqueta `ch04-s1`; compárate con `ref/ch04-s1`.

### Sesión 2: el interner (≈ 3 h)

#### Los tests, en rojo

T04.12–T04.23 describen:

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

En los tests: `map[Symbol]string{}` es un mapa vacío escrito como literal, y `prev, dup := seen[sym]` es la búsqueda con dos resultados, el valor y si la clave estaba; `const n = 300` puede ir dentro de una función, y `n*9/10` es 270. T04.18 sustituye `in.hash` por `func([]byte) uint64 { return 42 }`, una función que devuelve siempre 42 (su parámetro no lleva nombre porque no lo usa, como el receptor de `isShape` en el capítulo 3), para forzar colisiones que con un hash real casi nunca ocurren. El rojo es `undefined: NewInterner` y `undefined: Symbol`. Con las firmas y cuerpos de valor cero, salvo `NewInterner`, que devuelve `&Interner{arena: NewArena(0)}` (con `nil`, o con la arena a `nil`, T04.17 haría panic al leer `in.arena.used` y cortaría el binario), fallan todos los subtests y el ejemplo salvo `intern_existing_does_not_allocate`, que pasa por la razón equivocada: un `Intern` que siempre devuelve `NoSymbol` da el mismo símbolo en cada llamada y tampoco reserva. Ese test solo dice algo junto a los que fijan qué símbolo sale.

#### El código

{{file ch04-s2 internal/base/interner.go}}

- **El índice.** `make(map[uint64]Symbol)` crea un mapa vacío, igual que el literal `map[uint64]Symbol{}`. Un slice `nil` admite `append`, pero un mapa `nil`, su valor cero, se puede leer y no escribir (`assignment to entry in nil map`), así que `NewInterner` lo crea. `index` lleva cada hash a su **primer** símbolo, y `next[sym]` encadena los demás símbolos con ese hash. `find` recorre la cadena y decide la igualdad con `bytes.Equal` contra la copia guardada; nunca con el hash solo. Devuelve también el último eslabón, para que `Intern` enganche el nuevo sin recorrerla otra vez.
- **El hueco 0.** `next` y `texts` empiezan con un elemento para `NoSymbol`: el primer texto es el 1, un símbolo indexa los dos slices directamente y `Len` es `len(in.texts) - 1`. Si quitas ese hueco (los dos slices empiezan vacíos), fallan siete subtests y el ejemplo; `first_symbol_is_one` dice `Len of a new Interner = -1, want 0`.
- **La copia.** `Intern` copia los bytes a la arena: el buffer de quien llama puede cambiar. Con `stored = b`, T04.15 lo ve: `after changing the input, String = "Xbc", want "abc"` (T04.17 también falla, con su propio mensaje). El texto vacío se guarda como `[]byte{}`, un slice vacío que no es `nil`, sin pedir nada a la arena: `Copy` devolvería `nil`, y la vista de un símbolo que `Intern` dio no es nunca `nil`, tampoco la de `""`. Sin esa rama, T04.15 dice `Bytes(1) of "" is nil, want a non-nil view`.
- **El hash.** `hash` es un closure que lee `in.seed` y llama a `maphash.Bytes`, así que `in.seed` es el único sitio donde vive la semilla; `NewInterner` asigna `in.hash` después de crear `in`, porque el closure necesita `in`. Con `return 7`, el interner sigue siendo correcto, pero todo cae en una cadena: solo T04.19 lo nota, con `300 texts use only 1 hash buckets, want nearly one each` y `300 texts with a common tail use only 1 hash buckets, want nearly one each`.
- **La vista.** `Bytes` devuelve el slice guardado sin copiarlo; tiene `cap == len` porque viene de `Arena.Copy`, y T04.17 lo comprueba con un `append`, y también que dos llamadas den vistas del mismo array (`&v[0]` igual en las dos), que es lo que distingue una vista de una copia. Además mira que los textos estén de verdad en la arena: guardarlos con `make` y `copy` en lugar de `in.arena.Copy(b)` da `after interning two 2-byte texts: arena used = 0 bytes in 0 slabs, want 4 bytes in 1`.
- **Lo que ya existe no gasta nada.** T04.20 mira también `in.arena.used` y `Slabs()`, porque `AllocsPerRun` no ve la arena (su tarjeta explica el mutante). Pon `in.arena.Copy(b)` como primera línea de `Intern`, sin usar el resultado, y falla: `Intern of a known text used the arena: used 76 -> 3914 bytes, slabs 1 -> 1, want no change` (T04.17 también falla, con su propio mensaje).

Cambia `bytes.Equal(in.texts[s], b)` por `bytes.HasPrefix(b, in.texts[s])`. Con el hash constante, T04.18 interna primero `""` y después varios textos que se solapan, y como todo texto empieza por `""`, cada uno encuentra el símbolo de `""`: `symbol of "ab" = 1, want the new symbol 2`, lo mismo para los demás, y `Lookup` encuentra textos que nunca se internaron, como `"a"`. Los textos están elegidos para que se delate una igualdad que tome un atajo: mirar solo los primeros o los últimos bytes de dos textos de la misma longitud, hasta siete (los textos que se solapan delatan uno o dos, y las ocho copias de `"abcdefgh"` con una letra en mayúscula, de tres a siete), o la longitud con el primer y el último byte, o saltarse un byte de un texto de tres. Unos comparten longitud y primer byte (`"ab"`, `"ac"`, `"aa"`), otros longitud y último byte (`"aa"`, `"ba"`), `"abc"` alarga `"ab"`, y otros textos de tres bytes difieren de `"abc"` en uno solo, como `"axc"` en el del medio. Deshaz los cambios y compara:

{{reftest T04.18}}

{{reftest T04.20}}

{{reftest T04.21}}

Cambia una línea del `// Output:` del ejemplo (`main 2` por `main 3`) y `go test` imprime las dos salidas, `got:` con `main 2` y `want:` con `main 3`, y falla. Los benchmarks recorren 1000 textos conocidos con un índice que vuelve a 0 al llegar al final; sus números están en la teoría y en «Ejecútalo».

El mensaje de la etiqueta: `ch04-s2: the interner`.

Si te atascas o tu versión se desvía demasiado, copia el fichero de `ref/ch04-s2` y sigue.

Al terminar: tus tests en verde, commit y etiqueta `ch04-s2`; compárate con `ref/ch04-s2`.

## Ejecútalo

Desde la raíz de tu repo, con la sesión 2 terminada. Los tests del paquete, el ejemplo y los dos benchmarks (los números son de esta máquina; los tuyos variarán, pero los `0 allocs/op`, no):

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

Lo observable es que un `Ptr` sobrevive a los `Push` reservados. En `TestIdVec`:

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

Con el sub-benchmark `no_reserve` al lado y `-benchmem`, en varias ejecuciones en esta máquina: sin reservar, 0,80–1,08 ms, 4,1 MB y 28 reservas; con `Reserve`, 0,16–0,26 ms, 0,8 MB (100 000 × 8 bytes) y una.
:::

:::exercise
**2. `Arena.Reset()`.** Añade un `Reset` que vacíe la arena pero conserve sus slabs pequeños para reutilizarlos, con un test. ¿Qué pasa con los slices que la arena repartió antes?
:::

:::solution
Un campo nuevo, `free [][]byte`, guarda los slabs reutilizables. `Reset` los limpia con `clear` (un builtin de Go 1.21: pone a cero los elementos de un slice sin cambiar su longitud), para que `Alloc` siga devolviendo ceros:

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

`clear(a.slabs)` suelta las referencias a los slabs grandes antes de recortar: si no, el array de `slabs` los mantendría vivos. En `Alloc`, donde hoy está `a.cur = make([]byte, a.slabSize)`, el slab nuevo sale de `free` si queda alguno (`if k := len(a.free); k > 0 { a.cur = a.free[k-1]; a.free = a.free[:k-1] } else { … make … }`), y se apunta en `slabs` igual que uno nuevo.

El test, en `TestArena`, compara direcciones (`&again[0] != &first[0]`: dos slices que empiezan en el mismo byte):

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

Mata tres mutantes: un `Reset` que crea una arena nueva (no reutiliza), uno sin `clear` (`again[0] = 0xff`) y uno que guarda también el slab grande, que como último en `free` sería el primero en salir. Los slices de antes de `Reset` siguen apuntando a la misma memoria y verán los textos nuevos: el aliasing del capítulo 2, del que el recolector no te protege. Por eso el comentario lo prohíbe.
:::

:::exercise
**3. ¿Compensa el interner?** Mide el interner frente a un mapa `map[string]Symbol` a secas con 100 000 identificadores: construirlo y buscar textos ya conocidos.
:::

:::solution
En `intern_map_bench_test.go`, con `benchWords` de `interner_test.go`:

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

En esta máquina:

| | tiempo | memoria | reservas |
|---|---|---|---|
| `interner_build` | 8,7 ms | 21,3 MB | 621 |
| `map_build` | 7,8 ms | 8,6 MB | 100 530 |
| `interner_known` (100 000 búsquedas) | 2,2 ms | 0 | 0 |
| `map_known` | 1,5 ms | 0 | 0 |

El mapa gana en tiempo y en memoria. Lo que compra el interner es otra cosa: 621 reservas frente a 100 530, porque el mapa guarda cada clave como un `string` propio, un objeto más que el recolector tiene que marcar (como en la tabla de la teoría), y el interner guarda los textos seguidos en unos pocos slabs; símbolos densos 1, 2, 3 que indexan un `IdVec` sin otra tabla; y `Bytes` sin copia. Medir antes de decidir: la intuición «a mano será más rápido» aquí falla.
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

La función libre necesita los tres parámetros, `func Map[I ~uint32, T, U any](v *IdVec[I, T], f func(T) U) []U`, y se llama `Map(&v, f)`; el método solo declara `U` y se llama `v.Map(f)`. Las dos infieren `U` de `f`. Lo que cambia: el método exige `go 1.27` en el `go.mod`; tiene que estar en el paquete de `IdVec` (una función libre puede vivir en cualquiera); `v.Map` como valor necesita `v.Map[int]`; y ninguna interfaz puede pedir un método `Map[U]`, así que no ganas polimorfismo, solo una llamada más cómoda.
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
Porque `range` convierte el cuerpo del bucle en `yield`, y un `break` (o un `return`) en el cuerpo se traduce en que `yield` devuelve `false`: el bucle ya terminó. Si `All` siguiera llamando a `yield`, ejecutaría un cuerpo que ya salió. Go no lo detecta al compilar, sino al ejecutar: la siguiente llamada hace panic con `range function continued iteration after function for loop body returned false`.
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

- Una función o un tipo genérico declaran parámetros de tipo con una restricción; la restricción decide qué operaciones se pueden hacer dentro.
- `~uint32` es el conjunto de tipos con subyacente `uint32`; `comparable` permite `==`; `cmp.Ordered` es la unión de los tipos con `<`.
- Go infiere los argumentos de tipo de los argumentos, nunca del resultado: `errors.AsType[*fs.PathError](err)` lleva corchetes.
- Go 1.27 admite métodos con parámetros de tipo propios, pero no en interfaces, y su valor de método se instancia; no hay tipos de orden superior.
- Los ids tipados son tipos definidos: mezclar dos es un error de compilación, y `testdata/badids` lo fija con un `go build` desde el test.
- `iter.Seq2` es una función que llama a `yield`; `range` convierte el cuerpo del bucle en `yield`, y un iterador debe parar cuando devuelve `false`.
- El recolector no entra en lo que no tiene punteros: tablas indexadas por ids y slabs `[]byte` le ahorran casi todo el trabajo.
- Una arena con `unsafe` escondería punteros al recolector; la de Ki no lo usa.
- `maphash` y los mapas se siembran al azar (cada mapa con su semilla, y cada `range` empieza donde cae): ninguna salida depende de ellos, y los símbolos se numeran por orden de llegada.
- Un `Example` con `// Output:` es documentación que `go test` comprueba; `AllocsPerRun` y `-benchmem` convierten «no reserva» en un número.

## Go en el trabajo

**Cuándo usar genéricos.** En «When To Use Generics» (blog de Go, 12 de abril de 2022), Ian Lance Taylor los recomienda para funciones sobre slices, mapas y canales, para estructuras de datos de propósito general y para un método común cuya implementación es la misma en todos los tipos (de su `SliceFn` dice que «the methods look exactly the same for all slice types»); y no para sustituir interfaces: «If all you need to do with a value of some type is call a method on that value, use an interface type, not a type parameter». Si la implementación cambia con cada tipo, interfaz. Su regla final: «If you find yourself writing the exact same code multiple times, where the only difference between the copies is that the code uses different types, consider whether you can use a type parameter». `IdVec` la cumple: el mismo código para ficheros, expresiones, tipos y definiciones, y solo el tipo cambia.

**El `IndexVec` de rustc como patrón.** `compiler/rustc_index/src/vec.rs`, en la rama `main` de rust-lang/rust (commit `16d1ffac17ad`, del 26 de octubre de 2026), define `IndexVec<I: Idx, T>` como «An owned contiguous collection of `T`s, indexed by `I` rather than by `usize`», y explica que usar el índice equivocado «will fail at compile time»; su `push` devuelve el índice, como el nuestro. Los índices los genera la macro `newtype_index!`, sobre un `u32` con máximo `0xFFFF_FF00` por omisión. Es el patrón del que sale el de Ki.

**Grafos sin punteros por rendimiento.** La guía del recolector de Go (go.dev/doc/gc-guide) lo dice entre sus optimizaciones: «Pointer-free values are segregated from other values», así que puede convenir quitar punteros de las estructuras que no los necesitan, y «data structures that rely on indices over pointer values, while less well-typed, may perform better. This is only worth doing if it's clear that the object graph is complex and the GC is spending a lot of time marking and scanning». La tabla de la teoría es esa frase medida. Los ids tipados recuperan buena parte de lo que la guía llama «less well-typed»: un id de una tabla no entra en otra. Lo que no recuperan es el rango: un id puede quedarse fuera de su tabla, y eso es un panic de índice al ejecutar, no un error de compilación.
