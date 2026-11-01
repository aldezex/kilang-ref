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

`ki` no cambia; ejecutas sus tests y sus benchmarks:

```console
$ go test -count=1 ./internal/base/
ok  	github.com/aldezex/kilang/internal/base	0.085s
$ go test ./internal/base -run '^$' -bench 'Intern|UniqueMake' -benchmem
…
BenchmarkIntern-16        	72985978	        15.80 ns/op	       0 B/op	       0 allocs/op
BenchmarkUniqueMake-16    	46069453	        26.58 ns/op	       0 B/op	       0 allocs/op
```

Tus números variarán, pero no el orden de magnitud ni los `0 allocs/op`. Desde el capítulo 5, el lexer interna cada identificador, y desde el 6, el parser guarda sus nodos en `IdVec`s, camino de M1 (capítulo 10).

## Teoría

### Parámetros de tipo y restricciones

El capítulo 3 te pidió copiar `func ptr[T any](v T) *T { return &v }` sin explicarla. Entre corchetes van los **parámetros de tipo**, cada uno con su **restricción**; `any`, alias de `interface{}`, la interfaz sin métodos, admite cualquier tipo.

`ptr(101)` da un `*int` y `ptr("x")`, un `*string`: la función se comprueba una vez, con `T` como incógnita, y vale para todos.

Una restricción es una interfaz, que puede contener un **conjunto de tipos**. `~uint32` es «todo tipo cuyo subyacente es `uint32`»: `uint32`, `FileID`, `ExprID`. Sin la tilde, `FileID` no entra, y el compilador la sugiere:

```text
FileID does not satisfy uint32 (possibly missing ~ for uint32 in uint32)
```

Un **tipo genérico** lleva parámetros igual que una función, y sus métodos los nombran en el receptor: `type IdVec[I ~uint32, T any] struct{ items []T }`, `func (v *IdVec[I, T]) Push(x T) I`. Se usa instanciado: `IdVec[FileID, string]`; ni `IdVec` a secas ni `IdVec[int, string]`.

La restricción decide qué puedes hacer **dentro**. Con `~uint32`, `I(len(v.items))` convierte un `int` en un id y `v.items[i]` indexa con un id, porque todos los tipos del conjunto son enteros. Con `any`, las dos líneas fallan.

Dos restricciones de la biblioteca:

- `comparable` admite `==` y `!=`; un `[]int` no la cumple.
- `cmp.Ordered` es la unión `~int | … | ~string`, los tipos con `<`.

:::from Rust
`[T cmp.Ordered]` se parece a `<T: Ord>`, pero un bound es una lista de traits implementados a mano; un conjunto de tipos se describe por la forma, y ningún tipo declara que lo cumple.
:::

### Inferencia e instanciación explícita

Go deduce los argumentos de tipo de los **argumentos** de la llamada, nunca del resultado: `Max(1, 2.5)`, con `func Max[T cmp.Ordered](a, b T) T`, infiere `float64`. `errors.AsType`, la versión genérica de `errors.As` que el capítulo 3 dejó pendiente, es `func AsType[E error](err error) (E, bool)`: `E` solo aparece en el resultado, así que hay que escribirlo.

```go
_, err := os.Open("/no/such/file.ki")
pe, ok := errors.AsType[*fs.PathError](err) // true, pe.Op == "open"
```

Frente a `errors.As(err, &target)`, no hace falta una variable previa ni un puntero a puntero, y un tipo que no es un `error` no compila.

:::from Rust
Es el *turbofish* de `"5".parse::<i32>()`. Rust también infiere del resultado (`let n: i32 = s.parse()?`); Go, nunca.
:::

### Métodos genéricos y lo que Go no tiene

Go 1.27 añade **métodos con parámetros propios** sobre tipos concretos; hasta Go 1.26 solo podían usar los de su tipo:

```go
type Set struct{ items []int }

func (s *Set) Map[U any](f func(int) U) []U { … }
```

`s.Map(f)` infiere `U`, y `s.Map[float64](f)` lo da explícito. Tres límites:

- Exigen `go 1.27` en el `go.mod`.
- Un valor de método, el método sin llamar, necesita los argumentos de tipo: `f := s.Map[int]`, no `f := s.Map`.
- Una interfaz no puede declararlos: un valor de interfaz llama a sus métodos por una tabla fija, y uno genérico tendría una entrada por cada `U` posible.

Lo que Go no tiene: tipos de orden superior (no hay un `Functor[F[_]]`), especialización, ni acceso a campos a través de una restricción.

:::from Rust
Un trait con `fn map<U>(&self, …)` deja de servir como `dyn`; Go lo prohíbe un paso antes, en la declaración. Rust tampoco tiene tipos de orden superior; los GAT cubren parte de sus usos.
:::

:::from TS
`interface Mapper { map<U>(…) }` compila: con los tipos borrados, no hay tabla que rellenar por cada `U`.
:::

:::more Errores con genéricos, y `%T`: Go no borra los tipos
**Restricciones.** Una interfaz con un conjunto de tipos solo sirve de restricción.

- `var v IdVec`: `cannot use generic type IdVec[I ~uint32, T any] without instantiation`.
- `IdVec[int, string]`: `int does not satisfy ~uint32 (int missing in ~uint32)`.
- Con `[I any, T any]`: `cannot convert len(v.items) (value of type int) to type I` y `invalid argument: index i (variable of type I constrained by any) must be integer`.
- `a == b` con `any`: `invalid operation: a == b (incomparable types in type set)`.
- `comparable` con un `[]int`: `[]int does not satisfy comparable`.
- `v.Get(u)` con una variable `uint32`: `cannot use u (variable of type uint32) as base.FileID value in argument to v.Get`.
- `cmp.Ordered` es `~int | ~int8 | … | ~float64 | ~string`, la restricción de `slices.BinarySearch[S ~[]E, E cmp.Ordered]` del capítulo 2.
- `var x cmp.Ordered`: `cannot use type cmp.Ordered outside a type constraint: interface contains type constraints`.

**Go no borra los tipos.** TS los borra al compilar (`<T extends Foo>` restringe por forma, como Go). En Go, un `IdVec[FileID, string]` y un `IdVec[ExprID, string]` siguen siendo tipos distintos al ejecutar, y en un `package main` que declare los tres tipos, `%T` imprime `main.IdVec[main.FileID,string]` y `main.IdVec[main.ExprID,string]`.

**Inferencia.** `errors.AsType` sin corchetes, también si asignas el resultado a un `pe` ya declarado como `*fs.PathError`: `in call to errors.AsType, cannot infer E (declared at $GOROOT/src/errors/wrap.go:167:13)`.

**Métodos genéricos.** `TestToolchainFacts/generic_method_on_concrete_type` fija que compilan y corren (`docs/toolchain.md` de la referencia).

- Con `go 1.26` en el `go.mod`: `generic method requires go1.27 or later (-lang was set to go1.26; check go.mod)`.
- `f := s.Map`: `cannot use generic function s.Map without instantiation`.
- En una interfaz: `interface method must have no type parameters`, un error de sintaxis que también da `gofmt`.
- `&dyn Mapper`, en Rust: ``error[E0038]: the trait `Mapper` is not dyn compatible`` (rustc 1.97).
- `interface Mapper { map<U>(f: (x: number) => U): U[] }`, en TS, compila con `tsc --strict`.

**Lo que Go no tiene.**

- `type Functor[F[_] any] interface{}`: `undefined array length _ or missing type constraint`, porque el parser lee `F[_]` como un array.
- `t.X`, con `T interface{ ~struct{ X int } }`: `t.X undefined (type T has no field or method X)`.
:::

### Ids tipados

Un `IdVec[FileID, file]` tiene `Get(i FileID)`, y pasarle un id de otro tipo con el mismo subyacente no compila; es lo que fija `testdata/badids` (sesión 1):

```text
testdata/badids/main.go:14:12: cannot use i (variable of uint32 type otherID) as base.FileID value in argument to v.Get
```

Entre dos tipos con nombre que no son interfaces, asignar (y pasar un argumento es asignar) exige que sean el mismo tipo. Dos tipos definidos son distintos aunque su subyacente coincida. Confundir un índice de expresiones con uno de tipos será un error de compilación, no una expresión con el tipo de otra.

- **Una constante sin tipo es la excepción** (capítulo 2): el `0` de `v.Get(0)` toma el tipo `FileID`; una variable `uint32`, no.
- **`FileID(i)` compila:** los ids tipados protegen de una equivocación, no de una conversión a propósito.

:::from Rust
Es el `IndexVec<I: Idx, T>` de rustc. Cada newtype necesita un `impl Idx`, que escribe la macro `newtype_index!`; en Go basta `type ExprID uint32`.
:::

### `iter.Seq2` y `range` sobre funciones

`iter.Seq2[K, V]` es un tipo función: `func(yield func(K, V) bool)`. Un iterador llama a `yield` con cada par y para si `yield` devuelve `false`. `iter.Seq[V]` es la versión de un valor, como el `slices.Values` del capítulo 3.

`All` de `IdVec` devuelve un `iter.Seq2[I, T]`, y `for i, x := range v.All()` convierte el cuerpo del bucle en esa función `yield`: un `break` la hace devolver `false`. Si el iterador ignora el `false` y sigue, el runtime lo detiene:

```text
panic: runtime error: range function continued iteration after function for loop body returned false
```

:::from Rust
`Iterator` de Rust es de extracción: quien consume llama a `next()`. `iter.Seq2` es de empuje, como `for_each`; el `bool` de `yield` hace de `ControlFlow::Break` en `try_for_each`.
:::

:::from Python
Se parece a un generador, pero `yield` no es una palabra clave que suspende: es un parámetro, una función que el iterador llama.
:::

### Memoria sin punteros y el recolector

El recolector (capítulo 2) marca lo vivo recorriendo punteros. Un objeto cuyo tipo no contiene punteros no hace falta recorrerlo.

Lo mide esta tabla, con un árbol binario de 4 000 000 de nodos. `GOMAXPROCS` es el límite de hilos del sistema que ejecutan código Go a la vez, y el recolector reparte el marcado entre ellos:

| Forma | Objetos en el heap | Heap | Tiempo de `runtime.GC()`, `GOMAXPROCS=1` | con 16 hilos |
|---|---|---|---|---|
| `struct{ Left, Right *PNode; Val int64 }`, un `&PNode{}` por nodo | 4 000 118 | 91 MiB | 20–21 ms | 4,2–4,3 ms |
| `[]INode`, con `struct{ Left, Right uint32; Val int64 }` | 118 | 61 MiB | 35–43 µs | 0,19–0,31 ms |

Un `[]INode` es un solo objeto sin punteros: el recolector no entra en él. Es la razón de los ids. `Arena` aplica la idea a los bytes: los textos van seguidos en slabs, bloques `[]byte` de 64 KiB por omisión, con una reserva por slab y no por texto.

**La arena sin `unsafe`.** Solo reparte `[]byte`. El paquete `unsafe` podría reinterpretar esos bytes como nodos, `(*Node)(unsafe.Pointer(&slab[0]))`, pero el recolector sigue viendo un `[]byte`. Si `Node` tiene un puntero, lo que apunta se libera aunque el nodo lo use. Además, habría que alinear a mano. Por eso `unsafe` vive solo en `kirt/heap` (Parte VI).

:::from Rust
En `rustc_arena`, el borrow checker impide que la referencia de `alloc` viva más que la arena. Go no lo garantiza: la de Ki existe por la localidad y por reservar menos, no para liberar antes.
:::

:::more Cómo se midió la tabla, `GOMAXPROCS`, `unsafe` y el paquete `arena`
**El runtime.** El recolector parte de las raíces, las variables globales y las pilas de las goroutines. `runtime/mgcmark.go`, en Go 1.27, da el objeto sin punteros por marcado y recorrido de una vez: «If this is a noscan object, fast-track it to black instead of greying it». En su jerga, negro es «marcado y recorrido», y gris, «marcado, con hijos por recorrer».

**La medida.** Todo se mide tras un `runtime.GC()`. Los nodos se reservan en orden y se enlazan después (el hijo izquierdo del nodo `i` es el `2i+1`, y el derecho, el `2i+2`). El tiempo es el mejor de cinco `runtime.GC()` seguidos, y los objetos salen de `runtime.ReadMemStats` con `GOMAXPROCS=1`.

**`GOMAXPROCS`** se fija con la variable de entorno del mismo nombre o con `runtime.GOMAXPROCS`. Si no lo fijas, el runtime lo calcula a partir de las CPU lógicas, la afinidad del proceso y, en Linux, la cuota de CPU del cgroup (16 en esta máquina).

**El interner** sí guarda una cabecera `[]byte` por texto, y cada cabecera lleva un puntero; los slabs a los que apuntan no se recorren.

**`unsafe`.** La liberación se comprueba con `runtime.AddCleanup`, que registra una función que corre cuando su objeto ya es inalcanzable y el recolector lo libera: sobre un `*string` guardado solo en el campo `Name` del nodo, tras dos `runtime.GC()` el cleanup corre, y `go vet` no dice nada.

**Alinear** es hacer que cada nodo empiece en una dirección múltiplo de su alineación (`unsafe.Alignof`, 8 en amd64 para un struct con punteros).

**El paquete `arena`.** Go 1.27 lo trae, pero solo con `GOEXPERIMENT=arenas`, y Ki no usa ningún `GOEXPERIMENT`: es una regla del proyecto.
:::

### `hash/maphash`, semillas y el orden de los mapas

El interner busca cada nombre por su hash. `maphash.Bytes(seed, b)` da un hash de 64 bits; `maphash.MakeSeed()`, una semilla aleatoria. Con la misma semilla, el mismo texto da el mismo hash; entre dos ejecuciones, no.

Los mapas también se siembran al azar, cada uno con su semilla. Además, cada `range` empieza en un punto al azar.

De ahí una regla del proyecto: ninguna salida depende del orden de un mapa. El interner numera los textos por orden de llegada, así que nada visible depende de la semilla.

:::from Python
Es `PYTHONHASHSEED`, pero un `dict` conserva el orden de inserción, y un mapa de Go, ninguno.
:::

:::more Los valores de `maphash`, el orden de un `range` y el `HashMap` de Rust
Entre dos ejecuciones, `"fib"` dio `471cc675105a8475` y `122dc048e782e3e7`.

Seis `for k := range m` sobre el mismo mapa de cinco claves dieron `abcde deabc abcde bcdea bcdea bcdea`.

El `HashMap` de Rust usa `RandomState`: claves aleatorias por hilo que `RandomState::new` incrementa en cada mapa nuevo, así que dos mapas con las mismas claves pueden iterar en órdenes distintos («arbitrary order», dice su documentación).
:::

### `unique.Make` frente a un interner a mano

La biblioteca ya trae un interner: `unique.Make[T comparable](value T) Handle[T]`. Dos handles son iguales si y solo si lo son sus valores, y compararlos es comparar punteros. Es seguro entre goroutines, y el recolector borra sus entradas, punteros débiles, cuando no queda ningún handle.

El compilador quiere otra cosa: un entero de 4 bytes sin punteros, numerado en orden, que indexe un `IdVec` y salga igual en cada ejecución. Un `Handle` ocupa 8 bytes, es un puntero que el recolector recorre y no tiene número.

Con 1000 textos conocidos, `Intern` da 15,8 ns por llamada y `unique.Make`, 26,6 ns, sin reservar. La diferencia está en la búsqueda, según un perfil de CPU (`go test -cpuprofile`, leído con `go tool pprof -top -cum`).

:::from Rust
El interner de rustc se parece: un `Symbol` de 32 bits y los textos en una `DroplessArena`, con un `unsafe` que alarga sus referencias a `'static`. En Go, el recolector mantiene vivos los slabs, y no hace falta.
:::

:::more Por qué `unique.Make` tarda más, y el interner de rustc
**`Handle`.** `Handle[T]` es un `struct{ value *T }`, y sus entradas son `weak.Pointer` en `unique/canonmap.go`.

**La conversión.** Los dos benchmarks recorren textos guardados como `[]byte`. Un `[]byte` no es `comparable` (`unique.Make` de uno da `[]byte does not satisfy comparable`), así que el segundo convierte cada texto con `string(words[i])`. Los dos dan `0 B/op` y `0 allocs/op`: ninguno reserva, y la diferencia no está en la conversión, que no escapa. Lo dice este comando:

`go test -c -o /dev/null -gcflags=-m ./internal/base 2>&1 | grep 'string(words'`

- `go test -c` compila el binario de test sin ejecutarlo, y `-o /dev/null` lo tira.
- `-gcflags=-m`, del capítulo 2, escribe por stderr las decisiones de escape, también las de los tests.
- `2>&1` las pasa a stdout, que es lo que lee `grep`. El resultado: `string(words[i]) does not escape`.

**El perfil.** Pone en `canonMap.Load`, contando lo que llama, la mayor parte del tiempo, alrededor del 80 %, con bastante variación entre ejecuciones. En torno a una cuarta parte del tiempo total (también variable) se va en convertir en fuerte el puntero débil de la entrada que encuentra, un paso que el interner de Ki no tiene: sus vistas son slices normales.

**En rustc.** En `compiler/rustc_span/src/symbol.rs`, el índice de `Symbol`, `SymbolIndex`, se genera con `newtype_index!`, y el interner guarda en un `Vec` la referencia de cada símbolo. Su `unsafe` lleva el comentario «we only access these while the arena is still alive». En Go, los slabs siguen vivos mientras las vistas `[]byte` del interner los apunten.
:::

### Tests `Example`, `b.Loop`, `AllocsPerRun` y `os/exec`

- **`ExampleXxx`.** Una función `ExampleInterner` (`Example` + lo que ilustra) que termina en un comentario `// Output:` es un test: `go test` compara su stdout con él. Va en `package base_test`, porque es documentación y debe usar solo lo exportado.
- **`b.Loop`** (capítulo 2, ejercicio 3). Dentro de `for b.Loop() { … }`, los argumentos y resultados de las llamadas se mantienen vivos, así que el compilador no puede borrar un `in.Intern(words[i])` cuyo resultado nadie usa.
- **`testing.AllocsPerRun(runs, f)`** calienta con una ejecución de `f` y devuelve la media de reservas de las `runs` siguientes, con `GOMAXPROCS` a 1.
- **`os/exec` desde un test** (`exec.Command`, capítulo 1). `cmd.Stderr = &stderr` conecta el stderr del proceso a un `strings.Builder`, donde T04.05 busca dos textos del compilador. `cmd.Run()` da un error si el código de salida no es 0.

:::from Rust
Un `Example` es un doctest cuyo `assert_eq!` es el comentario `// Output:`, y el test que compila `testdata/badids` es un doctest `compile_fail`.
:::

:::more Detalles de `AllocsPerRun`, de `Example` y de `os/exec`: `CombinedOutput` y `*exec.ExitError`
**`AllocsPerRun`** devuelve siempre un número entero.

**`Example`.** Si la salida difiere, imprime `got:` y `want:`; `// Unordered output:` ignora el orden de las líneas, y sin comentario de salida se compila pero no se ejecuta.

**`os/exec`.** `exec.Command` y `cmd.Env` son del capítulo 1.

- En `cmd.Env = append(os.Environ(), "GOTOOLCHAIN=local")`, si una clave se repite gana la última.
- `cmd.Stderr = &stderr` lleva `&` porque el `Write` es de `*strings.Builder`.
- `os.DevNull` es `/dev/null` en Linux y `NUL` en Windows.
- T04.05 captura solo stderr porque busca en él dos textos del compilador, y le basta con saber que falló.
- Para otros usos: `out, err := cmd.CombinedOutput()` devuelve stdout y stderr mezclados, y `ee, ok := errors.AsType[*exec.ExitError](err)` da el código con `ee.ExitCode()`, que para el `go build` de `badids` es 1.
:::

### Compiladores: tablas, ids e interning

- **Almacenamiento orientado a datos.** Cada clase de nodo vive en una tabla (`IdVec[ExprID, Expr]`) y nombra a otro por su id; las listas de hijos son un `ListRange{First, Count}` sobre un pool compartido. Un árbol entero son unos pocos arrays grandes.
- **Ids frente a punteros.** Un id ocupa 4 bytes, el recolector no lo recorre, y su tipo impide confundir tablas.
- **Interning.** Cada nombre se guarda una vez como un `Symbol`: comparar nombres es comparar enteros, y el `0` es «sin nombre».

## Tests primero

{{cards}}

Úsalas: escribe el test de la tarjeta, míralo fallar, implementa lo justo para el verde, y solo entonces despliega la referencia con su `{{reftest}}`. Salvo `ExampleInterner`, todo va en `package base`, porque varios tests miran campos sin exportar.

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

2. **Piezas nuevas.** `type cell struct{ x int }` es un tipo declarado dentro de `TestIdVec`, visible en sus subtests; `for i := range first` recorre solo los índices de un slice; `%#x` escribe `0xab`.
3. **T04.02** usa `mustPanic` (capítulo 2) para exigir el panic exacto con un id que no existe: un `FileID` equivocado no se convierte en silencio en un fichero vacío.
4. **T04.04** mete su `break` en una función literal con un `recover()` diferido (T02.07): si `All` no para, el subtest falla con un mensaje y no corta el binario.
5. **Escribe el programa que T04.05 lee (por qué, abajo) y compila.** No debe compilar, y `go` no lo ve en `./...`, por estar bajo `testdata`:

{{file ch04-s1 internal/base/testdata/badids/main.go}}

6. **Míralos fallar.** No compilan:

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

7. **Escribe las firmas con cuerpos de valor cero,** sin tocar `SourceManager`, para un rojo de comportamiento. Un `nil` cortaría el binario, así que tres devuelven otra cosa: `Ptr`, `new(T)`; `NewArena`, `&Arena{}`; `All`, `func(yield func(I, T) bool) {}`. Cada subtest nuevo falla con su mensaje salvo `distinct_id_types_do_not_mix`: comprueba una regla del compilador y le basta la firma `Get(i I)`.

#### El código

{{file ch04-s1 internal/base/idvec.go}}

- `Get` devuelve una copia; `Ptr`, la dirección del elemento, válida solo hasta el siguiente `Push`: es la trampa del `&files[i]` del capítulo 2, ahora escrita en el contrato.
- `All` respeta el `false` de `yield`. Cambia el `if !yield(…) { return }` por un `yield(…)` sin mirar: tras el `break` de T04.04, el runtime corta el iterador con un panic, y el test lo recupera y falla. Deshazlo.

`IdVec` cambia `SourceManager`, como anunció el capítulo 2:

{{diff ch03-s3..ch04-s1 internal/base/source.go}}

- `files` pasa a `IdVec[FileID, file]`: los ficheros van seguidos en un array, sin un puntero por fichero, y `Push` puede moverlos. Por eso ningún método guarda un `*file`: cada uno pide `m.files.Ptr(id)`, lo usa y lo suelta.
- `Ptr` y no `Get`, porque `lineStarts` escribe `f.lines` la primera vez, y en una copia esa tabla se perdería.
- `line_table.go` y `load.go` cambian igual (`m.files[id]` → `m.files.Ptr(id)`), y los tests del capítulo 2 no se tocan, salvo una línea de `table_is_built_once`.

{{file ch04-s1 internal/base/arena.go}}

- `Alloc(0)` no abre un slab: pedir cero bytes no cuesta 64 KiB.
- Una petición mayor que el slab recibe uno propio, que no pasa a ser `cur`: el slab actual sigue sirviendo peticiones pequeñas.
- Una petición pequeña que no cabe en lo que queda de `cur` abre un slab nuevo y empieza en 0.
- `a.cur[a.used : a.used+n : a.used+n]` es la expresión de corte completa del capítulo 2: un `append` de quien recibe los bytes no pisa la petición siguiente.

{{reftest T04.04}}

{{reftest T04.05}}

{{reftest T04.11}}

:::more El `go build` de T04.05, `for range`, los stubs, `Get` en `SourceManager` y los bordes de T04.11
**El `go build` de T04.05.** `go test` corre en el directorio del paquete, así que `./testdata/badids` es una ruta relativa válida.

- El test lee primero `main.go` con `os.ReadFile`: `go test` guarda en caché un resultado en verde mientras no cambien los ficheros que el propio test abre, y sin esa lectura, una edición que hiciera compilar el programa dejaría un `ok (cached)` falso.
- `-buildvcs=false`: sin él, `go build` pregunta a git por el estado del checkout para grabarlo en el binario (que aquí se tira), y si git rechaza el directorio, por ser de otro usuario como en la CI del capítulo 1, se para con `error obtaining VCS status` antes de compilar.

**`All`.** `for range v.All()`, sin variables, recorre sin mirar los valores. En la traza de un panic, el cuerpo del bucle aparece como `main.main-range1`: una función que el compilador sacó del bucle, sin goroutines. Si `All` ignora el `false` de `yield`, T04.04 dice `All kept iterating after the loop body left the loop with break: runtime error: range function continued iteration after function for loop body returned false`.

**Los stubs.** Los subtests que fallan lo hacen con su mensaje, como `Push("b") = 0, want 1`, `Alloc(5): len = 0, cap = 0, want 5 and 5` o `Get(0): did not panic, want panic "runtime error: index out of range [0] with length 0"`. Sobre ellos, si tocaras `SourceManager`, cada `Ptr` daría un `file` vacío nuevo, y `TestLineTable/first_char` haría panic con `base: offset 1 out of range (file has 0 bytes)`. Un panic sin recuperar se lleva por delante los subtests que faltan.

**`Get` en `SourceManager`.** `m.files.Get(id).lineStarts()` ni compila, porque el valor que devuelve `Get` no tiene dirección: `cannot call pointer method lineStarts on file`.

**Bordes de la arena.**

- T04.11: 20 + 20 en 32 bytes abre otro slab, 12 justos no; 32, el tamaño exacto, es pequeño y llena un slab que pasa a ser el actual, así que el siguiente byte abre un tercero; 33 es grande.
- Quita `a.used = 0` y T04.11 revienta: `slice bounds out of range [::40] with capacity 32`.
- T04.06 compara `Alloc(0)` con `nil`, porque un `[]byte{}` también tiene `len` y `cap` a 0: si lo devolviera, `Alloc(0) = []byte{}, want nil`.
- Sin el tercer índice de la expresión de corte: `Alloc(5): len = 5, cap = 65536, want 5 and 5`.
:::

Si te atascas o tu versión se desvía demasiado, copia el fichero de `ref/ch04-s1` y sigue.

Al terminar: tus tests en verde, commit y etiqueta `ch04-s1` (mensaje: `ch04-s1: IdVec, typed ids and the arena`); compárate con `ref/ch04-s1`.

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

2. **Piezas nuevas.** `map[Symbol]string{}` es un mapa vacío escrito como literal; `prev, dup := seen[sym]` es la búsqueda con dos resultados, el valor y si la clave estaba.
3. **T04.18** sustituye `in.hash` por `func([]byte) uint64 { return 42 }` para forzar colisiones que con un hash real casi nunca ocurren.
4. **Míralos fallar.** El rojo es `undefined: NewInterner` y `undefined: Symbol`.
5. **Escribe las firmas con cuerpos de valor cero,** salvo `NewInterner`, que devuelve `&Interner{arena: NewArena(0)}` para que T04.17 no corte el binario. De los nuevos, solo pasa `intern_existing_does_not_allocate`, por la razón equivocada: un `Intern` que siempre devuelve `NoSymbol` tampoco reserva.

#### El código

{{file ch04-s2 internal/base/interner.go}}

- **El mapa.** `make(map[uint64]Symbol)` crea un mapa vacío, como el literal. Un slice `nil` admite `append`, pero un mapa `nil`, su valor cero, se puede leer y no escribir (`assignment to entry in nil map`).
- **La cadena.** `index` lleva cada hash a su **primer** símbolo, y `next[sym]` encadena los demás. `find` decide la igualdad con `bytes.Equal` contra la copia guardada, nunca con el hash solo, y devuelve el último eslabón, para que `Intern` enganche el nuevo sin recorrerla.
- **El hueco 0.** `next` y `texts` empiezan con un elemento para `NoSymbol`: el primer texto es el 1, y un símbolo indexa los dos slices directamente.
- **La copia.** `Intern` copia los bytes a la arena, porque el buffer de quien llama puede cambiar. El texto vacío se guarda como `[]byte{}`: la vista de un símbolo de `Intern` no es nunca `nil`, y `Copy` lo devolvería.
- **El hash.** Es un closure que lee `in.seed`, el único sitio donde vive la semilla; como captura `in`, `NewInterner` lo asigna después de crear `in`.
- **La vista.** `Bytes` devuelve el slice guardado sin copiarlo, con `cap == len` porque viene de `Arena.Copy`.
- **Lo que ya existe no gasta nada.** T04.20 mira también la arena, que `AllocsPerRun` no ve.

Cambia `bytes.Equal(in.texts[s], b)` por `bytes.HasPrefix(b, in.texts[s])`. T04.18 falla: con el hash constante, todo texto encuentra el símbolo de `""`, que interna primero. Deshaz el cambio y compara:

{{reftest T04.18}}

{{reftest T04.20}}

{{reftest T04.21}}

:::more Los textos de T04.18, qué miran T04.17 y T04.20, los benchmarks y otros mutantes
**T04.18.** Con `HasPrefix`, el mensaje es `symbol of "ab" = 1, want the new symbol 2`, y `Lookup` también encuentra textos que nunca se internaron, como `"a"`. Los textos están elegidos para que se delate una igualdad que tome un atajo:

- mirar solo los primeros o los últimos bytes de dos textos de la misma longitud, hasta siete (los textos que se solapan delatan uno o dos, y las ocho copias de `"abcdefgh"` con una letra en mayúscula, de tres a siete);
- mirar la longitud con el primer y el último byte;
- saltarse un byte de un texto de tres.

Unos comparten longitud y primer byte (`"ab"`, `"ac"`, `"aa"`), otros longitud y último byte (`"aa"`, `"ba"`), `"abc"` alarga `"ab"`, y otros textos de tres bytes difieren de `"abc"` en uno solo, como `"axc"` en el del medio. La función de 42 no nombra su parámetro porque no lo usa, como el receptor de `isShape` en el capítulo 3.

**T04.17** comprueba `cap == len` con un `append`, y también que dos llamadas den vistas del mismo array (`&v[0]` igual en las dos), que es lo que distingue una vista de una copia, y que los textos estén de verdad en la arena: guardarlos con `make` y `copy` en lugar de `in.arena.Copy(b)` da `after interning two 2-byte texts: arena used = 0 bytes in 0 slabs, want 4 bytes in 1`.

**T04.20** mira `in.arena.used` y `Slabs()`. Solo dice algo junto a los tests que fijan qué símbolo sale.

**Los benchmarks.** Las dos tarjetas `bench` no tienen umbral y no se ejecutan sin `-bench`. Recorren 1000 textos conocidos con un índice que vuelve a 0 al llegar al final.

**`const`.** T04.19 declara `const n = 300` dentro de la función: un `const` también puede ir ahí.

**Mutantes.**

- Sin el hueco 0 (los dos slices empiezan vacíos), fallan siete subtests y el ejemplo; `first_symbol_is_one` dice `Len of a new Interner = -1, want 0`, porque `Len` es `len(in.texts) - 1`.
- Con `stored = b`, T04.15 lo ve: `after changing the input, String = "Xbc", want "abc"` (T04.17 también falla, con su propio mensaje).
- Sin la rama del texto vacío, T04.15 dice `Bytes(1) of "" is nil, want a non-nil view`.
- Con `return 7` como hash, el interner sigue siendo correcto, pero todo cae en una cadena: solo T04.19 lo nota (`300 texts use only 1 hash buckets, want nearly one each` y `300 texts with a common tail use only 1 hash buckets, want nearly one each`).
- Con `in.arena.Copy(b)` como primera línea de `Intern`, sin usar el resultado, T04.20 dice `Intern of a known text used the arena: used 76 -> 3914 bytes, slabs 1 -> 1, want no change` (T04.17 también falla, con su propio mensaje).
- Cambia una línea del `// Output:` del ejemplo y `go test` falla e imprime `got:` y `want:`.
:::

Si te atascas o tu versión se desvía demasiado, copia el fichero de `ref/ch04-s2` y sigue.

Al terminar: tus tests en verde, commit y etiqueta `ch04-s2` (mensaje: `ch04-s2: the interner`); compárate con `ref/ch04-s2`.

## Ejecútalo

Desde la raíz de tu repo, con la sesión 2 terminada (los números varían; los `0 allocs/op`, no):

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

Mata tres mutantes: un `Reset` que crea una arena nueva (no reutiliza), uno sin `clear` (`again[0] = 0xff`) y uno que guarda también el slab grande, que como último en `free` sería el primero en salir.

Los slices de antes de `Reset` siguen apuntando a la misma memoria y verán los textos nuevos: el aliasing del capítulo 2, del que el recolector no te protege. Por eso el comentario lo prohíbe.
:::

:::exercise
**3. ¿Compensa el interner?** Mide el interner frente a un mapa `map[string]Symbol` a secas con 100 000 identificadores: construirlo y buscar textos ya conocidos.
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

En esta máquina:

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
- `v.Map` como valor necesita `v.Map[int]`;
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

- Un genérico declara parámetros de tipo con una restricción, que decide qué se puede hacer dentro.
- `~uint32` son los tipos con subyacente `uint32`; `comparable` permite `==`; `cmp.Ordered`, `<`.
- Go infiere los argumentos de tipo de los argumentos, nunca del resultado: `errors.AsType` lleva corchetes.
- Go 1.27 admite métodos con parámetros propios, no en interfaces; no hay tipos de orden superior.
- Mezclar dos ids tipados es un error de compilación, y `testdata/badids` lo fija.
- `range` convierte el cuerpo del bucle en `yield`, y un iterador debe parar cuando devuelve `false`.
- El recolector no entra en lo que no tiene punteros: tablas con ids y slabs `[]byte` le ahorran casi todo.
- Una arena con `unsafe` escondería punteros al recolector; la de Ki no lo usa.
- `maphash` y los mapas se siembran al azar: ninguna salida depende de ellos.
- Un `Example` con `// Output:` es documentación que `go test` comprueba; `AllocsPerRun` y `-benchmem` miden «no reserva».

## Go en el trabajo

**Cuándo usar genéricos.** En «When To Use Generics» (blog de Go), Ian Lance Taylor los recomienda para funciones sobre slices, mapas y canales, estructuras de datos de propósito general y un método común idéntico en todos los tipos. No, para sustituir interfaces. `IdVec` encaja: el mismo código para todas sus tablas.

**El `IndexVec` de rustc como patrón.** Usar el índice equivocado «will fail at compile time», y su `push` devuelve el índice, como el nuestro.

**Grafos sin punteros.** Según la guía del recolector (go.dev/doc/gc-guide), las estructuras con índices, «while less well-typed, may perform better»: la tabla de la teoría lo mide. Los ids tipados recuperan buena parte de lo «less well-typed», pero no el rango: un id fuera de su tabla es un panic al ejecutar, no un error de compilación.

:::more Las citas completas y las fuentes
**Taylor** (blog de Go, 12 de abril de 2022).

- De su `SliceFn` dice que «the methods look exactly the same for all slice types».
- Sobre interfaces: «If all you need to do with a value of some type is call a method on that value, use an interface type, not a type parameter»; si la implementación cambia con cada tipo, interfaz.
- Su regla final: «If you find yourself writing the exact same code multiple times, where the only difference between the copies is that the code uses different types, consider whether you can use a type parameter».

**`IndexVec`.** «An owned contiguous collection of `T`s, indexed by `I` rather than by `usize`». Está en `compiler/rustc_index/src/vec.rs`, en la rama `main` de rust-lang/rust (commit `16d1ffac17ad`, del 26 de octubre de 2026), como `IndexVec<I: Idx, T>`, y explica que usar el índice equivocado «will fail at compile time»; su doctest lleva `&vec1[idx2]; // Compile error!`. Los índices los genera la macro `newtype_index!`, sobre un `u32` con máximo `0xFFFF_FF00` por omisión. Es el patrón del que sale el de Ki.

**La guía del recolector.** Lo dice entre sus optimizaciones: «Pointer-free values are segregated from other values», así que puede convenir quitar punteros de las estructuras que no los necesitan.

Y sigue: «data structures that rely on indices over pointer values, while less well-typed, may perform better. This is only worth doing if it's clear that the object graph is complex and the GC is spending a lot of time marking and scanning».
:::
