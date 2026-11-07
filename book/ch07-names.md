---
chapter: 7
title: Nombres, ámbitos y errores al nivel de rustc
part: I
hours: 9
milestone: `unknown name 'fibb'; did you mean 'fib'?`, con el fix-it que lo sustituye; los fix-its se muestran como sugerencias de rustc; todos los códigos E01xx–E03xx al nivel del apartado 7.3 y el catálogo `docs/errors` con sus ejemplos probados
---

## Objetivo

Al terminar, `ki check` sabe qué significa cada nombre. Si no significa nada, lo dice como rustc, con el arreglo. Aquí, `fibb` es un descuido:

```ki
fn fib(n: Int) -> Int {
  if n < 2 { n } else { fibb(n - 1) + fib(n - 2) }
}
```

`ki check` lo da por desconocido, propone `fib` y añade un *fix-it*, la línea antes y después del arreglo (en «Ejecútalo»).

La sesión 4 lleva a ese nivel los diecinueve códigos y crea el catálogo `docs/errors`: el hito, camino de M1.

## Teoría

### Maps a fondo

El capítulo 4 enseñó lo básico de `map[K]V`, con su `nil` que se lee y no se escribe. Falta lo que importa a un resolvedor:

- **El orden** cambia a propósito: lo que el compilador muestra se ordena antes, por id o por nombre.
- **Semántica de referencia.** Pasar un map copia un puntero a su tabla: si la función que lo recibe añade una clave, quien la llama la ve.
- **Claves comparables.** La clave admite `==`; un slice no, y `map[[]byte]int` no compila.
- **`clear(m)`** lo vacía, sin volverlo `nil`.
- **Tablas suizas.** Por dentro, grupos de 8 casillas; el orden sigue siendo aleatorio.

:::from Rust
Nadie es dueño del map: copiar la variable comparte la tabla, lo que en Rust pediría un `Rc<RefCell<HashMap<…>>>`.
:::

### `maps`, `slices` e `iter.Seq`

`slices.Sorted(maps.Keys(m))` saca de un map una lista que no depende del azar: `maps.Keys` da un `iter.Seq[K]` (capítulo 4) sin orden, y `slices.Sorted` lo recoge ordenado.

Un iterador propio entrega cada valor a `yield` y para cuando este devuelve `false`: los ámbitos tendrán uno.

### `//go:embed` y el preludio

`print` lo declara `stdlib/prelude.ki`, que viaja dentro del binario: una directiva hace que `go build` copie el directorio en el ejecutable, y el programa lo lee como un sistema de ficheros, con el directorio en la ruta.

`embed.go` vive en la raíz porque `embed` solo lee del directorio del paquete hacia abajo, y solo la raíz tiene `stdlib/` debajo.

### La distancia de edición con dos filas

La **distancia de Levenshtein** es el menor número de inserciones, borrados y sustituciones que convierten una cadena en otra: `fibb` está a una de `fib`.

Es programación dinámica. `d(i, j)` es la distancia entre los `i` primeros bytes de `a` y los `j` primeros de `b`, y sale de tres vecinas (`[aᵢ ≠ bⱼ]` vale 1 si los bytes difieren y 0 si no):

```math
d(i, 0) = i            d(0, j) = j            borra i bytes, construye j desde nada
d(i, j) = min( d(i−1, j) + 1,              borra aᵢ
               d(i, j−1) + 1,              inserta bⱼ
               d(i−1, j−1) + [aᵢ ≠ bⱼ] )   conserva o sustituye
distancia(a, b) = d(|a|, |b|)
```

Cada fila solo mira la anterior, así que bastan dos:

```pseudo
función distancia(a, b):
    prev ← [0, 1, …, |b|]                      // la fila 0
    para i de 1 a |a|:
        cur[0] ← i
        para j de 1 a |b|:
            cur[j] ← min(prev[j] + 1, cur[j−1] + 1, prev[j−1] + [aᵢ ≠ bⱼ])
        intercambia prev y cur
    devuelve prev[|b|]
```

Se cuentan bytes: `é` y `e` están a distancia 2.

### Tablas de símbolos, ámbitos y sombreado

Resolver es decidir a qué se refiere cada `ExprName`: una definición o una variable local.

- **Definiciones** (`Def`, y su `DefID`): cada función de cada módulo.
- **Variables locales** (`Local`, y su `LocalID`): cada parámetro y cada `let`. Dos `let x` seguidos son dos variables.
- **Ámbitos**: un map de nombres a `LocalID` con un puntero al de fuera. Los parámetros son uno; cada bloque, otro dentro.

```pseudo
// tops[m]: nombre → primera Def del módulo m;  rotas[m]: nombres de items ilegibles
// res: la tabla lateral, por expresión
función resolver(módulos):
    para cada item de cada módulo m:                     // pasada 1: definiciones
        si es una función entonces
            comprueba sus atributos                      // E0304, E0305
            crea su Def                                  // también si se repite
            si m ya define el nombre entonces informa E0302   // el nombre sigue con la primera
            si no, tops[m][nombre] ← la Def
        si es un item ilegible con nombre entonces rotas[m] ← rotas[m] ∪ {nombre}
    para cada Def f, en orden:                           // pasada 2: cuerpos
        ámbito ← un ámbito con los parámetros de f
        si f tiene cuerpo entonces recorre(cuerpo de f)

función recorre(e):
    si e es un nombre x entonces res[e] ← busca(x)
    si no, si e es un bloque { s₁ … sₙ cola } entonces
        ámbito ← un ámbito nuevo dentro del actual
        para cada sᵢ:
            si sᵢ es let x = ini entonces
                recorre(ini)                             // x aún no se ve
                liga x a una variable nueva en el ámbito
            si no, recorre(sᵢ)
        si hay cola entonces recorre(cola)
        ámbito ← el de fuera
    si no, recorre cada hijo de e

función busca(x):
    para cada ámbito, del actual hacia fuera:
        si liga x entonces devuelve esa variable
    si el módulo define x entonces devuelve su primera Def
    si x ∈ rotas[módulo] entonces devuelve error         // el parser ya lo dijo
    si el módulo no es el preludio y el preludio define x con pub entonces devuelve esa Def
    informa E0301:
        si x es el nombre de un let cuyo inicializador se recorre, señala el más interior
        si el sink aún no ha descartado ningún error, añade la sugerencia
    devuelve error
```

Las dos pasadas dejan que `main` llame a una función posterior. `busca` es el **sombreado**: gana el ámbito más interior, luego el módulo y al final lo `pub` del preludio; tu `print` tapa el de la biblioteca.

Un `let` liga *después* de su inicializador: en `let x = x`, la `x` de la derecha es la de fuera; si no hay ninguna, el E0301 señala el `let`. Llamar a una función de firma ilegible no suma un E0301 al error del parser.

Lo aprendido va en **tablas laterales**: los pases no mutan el árbol, escriben en slices por id, como `ExprRes[ExprID]`.

Del resolvedor: E0302, función repetida; E0304, `#[extern]` fuera de la biblioteca estándar; y E0305, atributo desconocido. E0303, sin `main`, llega con `ki run` en el capítulo 10.

### «Did you mean»: la sugerencia

```pseudo
función candidatos():
    devuelve, ordenados y sin repetir, las variables visibles,
        las Def del módulo y las pub del preludio

función sugiere(nombre, candidatos):
    si |nombre| > 128 entonces devuelve nada
    límite ← max(1, ⌊|nombre| / 3⌋)
    mejor ← nada
    para cada c en candidatos:
        si | |c| − |nombre| | > límite entonces pasa al siguiente
        d ← distancia(nombre, c)
        si d ≤ límite y (mejor = nada o d < d_mejor o (d = d_mejor y c < mejor)) entonces
            mejor ← c
    devuelve mejor
```

El umbral da una edición a un nombre corto y una más por cada tres bytes: `fibb` admite una; `zzzzzz`, dos. Como cada edición cambia el largo en uno como mucho, un candidato que difiere más que el umbral ni se mide.

**Es determinista:** la lista llega ordenada y el empate lo gana el alfabeto. No se mide un nombre de más de 128 bytes (la distancia cuesta el producto de los largos), ni cuando el sink ya ha descartado un error, pasados los 100: nadie vería la ayuda.

### El listón: errores al nivel de rustc

Un buen error nombra la cosa, señala el sitio y, si puede, dice qué hacer. Tres de rustc:

```text
error[E0425]: cannot find function `helpr` in this scope
 --> s2.rs:3:13
  |
1 | fn helper() -> i32 { 1 }
  | ------------------ similarly named function `helper` defined here
2 | fn main() {
3 |     let x = helpr();
  |             ^^^^^
  |
help: a function with a similar name exists
  |
3 |     let x = helper();
  |                 +

error: mismatched closing delimiter: `}`
 --> p.rs:1:17
  |
1 | fn c() -> i64 { (1 }
  |               - ^  ^ mismatched closing delimiter
  |               | |
  |               | unclosed delimiter
  |               closing delimiter possibly meant for this

error[E0428]: the name `f` is defined multiple times
 --> d.rs:2:1
  |
1 | fn f() {}
  | ------ previous definition of the value `f` here
2 | fn f() {}
  | ^^^^^^ `f` redefined here
  |
  = note: `f` must be defined only once in the value namespace of this module
```

Uno de Elm:

```text
Cannot find variable `List.nap`.

6|   div [] (List.nap viewUser users)
             ^^^^^^^^
`List` does not expose `nap`. Maybe you want one of the following?

    List.map
    List.any
    List.map2
    List.map3
```

Comparten la anatomía de un `Diagnostic`:

- **mensaje**: nombra la cosa;
- **etiqueta primaria**: qué está mal *en ese sitio*;
- **secundarias**: el otro sitio que explica el error;
- **nota**: la regla, si el mensaje no la hace evidente;
- **ayuda**: una acción concreta;
- **fix-it**: la corrección, si es mecánica.

**Una ayuda genérica es peor que ninguna.** «check your syntax» enseña a ignorar la línea de ayuda, también cuando sirve. Donde no hay nada concreto, Ki calla.

Desde la sesión 4, los diecinueve códigos cumplen ese listón, y cada código nuevo nace completo. Los capítulos 3, 5 y 6 no se reescriben, porque muestran la salida de sus etiquetas: una fe de erratas remite aquí.

### Los fix-its

```math
fix-it f = (span [lo, hi), texto)        lo = hi: inserción        texto = "": borrado

si un diagnóstico d lleva los fix-its F ≠ ∅:
    ayuda(d) ≠ ""
    ∀ f ∈ F:  fichero(f) = fichero(primaria(d))  ∧  f no cruza un salto de línea
    ∀ f, g ∈ F en una línea, f antes que g:  f.hi ≤ g.lo
aplicar(F) = cada f sobre las posiciones de la línea original = aplicarlos de derecha a izquierda
```

**Se muestra como el diff de rustc**, bajo la ayuda: `N - original` y `N + parcheada`. rustc lo usa cuando la edición borra algo y marca las inserciones con `+`; Ki usa siempre el par.

**Las reglas del bloque** son un contrato del pase que construye el diagnóstico: romper una es un fallo del compilador, y el renderizador hace panic con el motivo.

**Dos ediciones en una línea** usan las posiciones de la original. En `x = fibb + gammaa`, cambiar `fibb` por `f` primero correría tres bytes la otra.

**No hay edición donde no sería mecánica:** qué dígitos querías, qué nombre lleva tu variable. A veces solo vale en un caso:

```math
E0203:  a < b < c  ⟶  a < b && b < c         solo si b es un token: un nombre, un entero o un booleano
E0201:  escribe el cierre que falta           solo si el token hallado ∈ { }, ], ;, let } ∪ { comienzos de item }
```

En `print(1 2)` falta una coma, no un `)`: el `2` no está en el conjunto.

### El catálogo

Es documentación ejecutable. `docs/errors/E0301.md` explica su código y trae un ejemplo erróneo y uno corregido. Un test los ejecuta y exige:

```math
C = { los códigos que construye el fuente }        D = { c | existe docs/errors/c.md }
C = D
erróneo e de c:     c ∈ códigos(e)  ∧  códigos(e) ∖ {c} = also(e)
corregido e de c:   c ∉ códigos(e)  ∧  códigos(e) = also(e)  ∧  (also(e) = ∅ ⇒ sale con 0 y no escribe nada)
                                                         ∧  (also(e) ≠ ∅ ⇒ sale con ≠ 0)
```

Un código sin fichero, o al revés, rompe la CI. `also` son los otros códigos que un ejemplo da por fuerza.

**El test saca los códigos del fuente, leído como Go.** `parser.ParseFile` (`go/parser`) da el árbol de un fichero, y `ast.Inspect` lo recorre buscando las llamadas a `diag.E` y `diag.W`. Una expresión regular contaría comentarios y cadenas, y el fichero de un código quitado seguiría vivo.

## Preguntas de repaso

:::review
¿Por qué un `let` liga su nombre después de resolver el inicializador y no antes?
:::

:::answer
Porque así `let x = x + 1` lee la `x` de fuera, que es lo que quien lo escribe quiere, y `let x = x` sin otra `x` es un error en el origen. Si ligara antes, la `x` de la derecha sería la propia variable, todavía sin valor.
:::

:::review
¿Por qué las ayudas de «did you mean» no pueden depender del orden de un map, si el resultado «casi siempre» sale igual?
:::

:::answer
Porque «casi siempre» es un test que falla en la CI una de cada pocas veces y una ayuda que cambia entre dos ejecuciones del mismo fichero. Con empates, el primero que llega depende de la semilla; el desempate alfabético y la lista ordenada lo fijan.
:::

:::review
¿Por qué el renderizador hace panic ante un fix-it sin ayuda, en vez de pintarlo sin más?
:::

:::answer
Porque es un fallo del pase que construyó el diagnóstico, no del programa que se compila: un parche sin la frase que dice qué hace no es una sugerencia. Un panic con el motivo lo hace visible en el primer test que lo pinta, también en los fuzz del parser y del análisis, que renderizan cada diagnóstico.
:::

:::review
¿Por qué E0203 solo ofrece la edición cuando el operando central es un token?
:::

:::answer
Porque la edición copia ese operando a la derecha del `&&`. Un nombre o un número se pueden copiar sin cambiar nada; `f(x)` o un bloque se evaluarían dos veces, con sus efectos.
:::

:::review
`let a = g(1` seguido de una línea `h(2)` da el E0201 con su secundaria, sin ayuda ni edición, aunque faltaba un `)`. ¿Es un fallo del listón?
:::

:::answer
No: el listón pide la edición solo cuando es mecánica. El token hallado es `h`, un nombre, que también seguiría a una coma olvidada, `g(1, h(2))`; un `)` arreglaría un caso y estropearía el otro, como en `print(1 2)`. rustc lo resuelve con niveles de confianza (`Applicability`, en «Go en el trabajo»); Ki, callando.
:::

:::review
¿Qué gana el catálogo con ejecutar sus ejemplos, si un texto bien revisado ya los explica?
:::

:::answer
Que no envejecen en silencio: si un código desaparece o deja de salir en su ejemplo, o si el parser empieza a admitir cadenas en una expresión, la CI falla en esa sesión. La palabra `also=` está hecha para eso: hay que quitarla en cuanto el E0201 deje de salir.
:::

## Resumen

- Un map es un puntero a una tabla: copiarlo es compartirla.
- Nada visible depende del orden de un map.
- El preludio viaja en el binario.
- La distancia de edición es programación dinámica con dos filas.
- Resolver son dos pasadas, con ámbitos encadenados y tablas laterales.
- Gana el ámbito más interior; un `let` no se ve en su inicializador.
- Una sugerencia está dentro del umbral, y el alfabeto desempata.
- Un diagnóstico explica el error en su sitio y solo da ayuda concreta.
- Los fix-its usan las posiciones originales.
- `docs/errors` trae ejemplos que un test ejecuta.

## Go en el trabajo

**Un map ganó casi siempre a un slice ordenado.** Una clave string hay que hashearla y compararla byte a byte: por eso el compilador busca por `Symbol`. Lo que tardó una búsqueda en orden salteado:

| Búsqueda | Clave `string` | Clave `uint32` |
|---|---|---|
| `map`, 8 claves | unos 8 ns | 4,3 ns |
| `map`, 1 000 claves | 11 ns | 6,8 ns |
| `map`, 100 000 claves | 24 ns | 13,5 ns |
| `slices.BinarySearch` sobre un slice ordenado | perdió en los tres tamaños | solo empató o ganó por poco con 8 claves |

**La iteración aleatoria en CI.** Un test que compara las claves de un map, sin ordenar, pasa en tu portátil y a veces falla en la CI. `go test -count=100` lo destapa.

**Los mensajes como producto.** La *rustc-dev-guide* va más allá que Ki: pide evitar la pregunta «did you mean». Cada sugerencia lleva su `Applicability`, un nivel de confianza, que Ki no distingue todavía.

**`go vet` señala.** Sobre `fmt.Printf("%d\n", name)` con un `string`, dice qué y dónde, sin ayuda. Ninguna edición adivina si sobra el verbo o el argumento.

## Tests primero

{{cards}}

Escribe el test, míralo fallar, implementa lo justo, y solo entonces despliega la referencia.

Las tarjetas describen el final del capítulo. Las que cambian en la sesión 4 se escriben antes como en `git show ref/ch07-sK:book/tests/ch07.tests`, con la `K` de su sesión. Los ayudantes están en esa misma etiqueta.

## Implementación

### Sesión 1: el preludio, `embed.go` y las definiciones (≈ 2 h)

#### Go: `//go:embed` y `embed.FS`

```go
// go build copies the directory into the binary.
//
//go:embed stdlib
var Stdlib embed.FS // an fs.FS (io/fs)

prelude, err := fs.ReadFile(kilang.Stdlib, "stdlib/prelude.ki") // read at run time
```

#### Los tests, en rojo

1. **Escribe T07.01–T07.17,** con T07.02 y T07.05–T07.07 en su forma de esta sesión. `embed_test.go` es `package kilang`. `resolve_test.go` es `package sema_test`: `testsupport` importa `driver`, que importa `sema`, y desde dentro sería un ciclo. La interfaz:

```go
// package kilang (embed.go): //go:embed stdlib
var Stdlib embed.FS
// package syntax
const ExternAttr = "extern" // a function without a body needs #[extern]

// package sema
type DefID uint32; type LocalID uint32; type ModuleIdx uint32
type DefKind uint8 // DefFn, DefExternFn
type Def struct { Name base.Symbol; Kind DefKind; Module ModuleIdx; Item syntax.ItemID; Span base.Span; Pub bool; ExternName string }
type Local struct { Name base.Symbol; Span base.Span; Owner DefID }
type ResKind uint8 // ResNone, ResDef, ResLocal, ResError
type Res struct { Kind ResKind; Def DefID; Local LocalID }
type ModuleRes struct { ExprRes []Res; LetLocal []LocalID; ParamLocals [][]LocalID }
type ResolvedProgram struct { Defs base.IdVec[DefID, Def]; Locals base.IdVec[LocalID, Local]; Modules []ModuleRes; Main DefID; HasMain bool }
func Resolve(mods []*syntax.Module, user ModuleIdx, in *base.Interner, sink *diag.Sink) *ResolvedProgram

// package driver
type Frontend struct { Modules []*syntax.Module; User sema.ModuleIdx; Resolved *sema.ResolvedProgram }
func (s *Session) ParseSource(displayPath string, contents []byte) *Frontend
func (s *Session) LoadAndParse(path string) (*Frontend, bool)
func (s *Session) Analyze(f *Frontend)

// package testsupport (test-only code)
type Program struct { Session *driver.Session; Frontend *driver.Frontend; Out, Err bytes.Buffer }
func Analyze(t *testing.T, source string) *Program
func ErrorCodes(p *Program) []string
```

2. **Copia `testsupport`,** que pasa un texto por el compilador entero, y escribe el golden de T07.17:

{{file ch07-s1 internal/testsupport/compile.go}}

{{file ch07-s1 tests/errors/ch07/duplicate_definition.ki}}

3. **Cambia los tests del capítulo 6 con funciones sin cuerpo:** ahora necesitan `#[extern]`.

{{diff ch06-s4..ch07-s1 internal/syntax/parser_test.go}}

{{diff ch06-s4..ch07-s1 internal/syntax/parser_stmt_internal_test.go}}

4. **Míralos fallar:** no compila, `undefined: Stdlib`.
5. **Añade los tipos de `sema` y un `Resolve` que solo crea las tablas, con una fila por nodo.**
6. **Escribe enteros `embed.go`, `stdlib/prelude.ki` y `pipeline.go`:** con stubs, los tests hacen panic.
7. **Evita tres panics:** tres subtests leen `Sink.All()[0]` tras un `t.Errorf`. En tu versión, pon un `t.Fatalf` delante; con la referencia, `go test ./... -skip 'TestResolve/(duplicate_definition_E0302|extern_only_in_std_E0304|unknown_attribute_E0305)$'`.
8. **Mira qué falla:** cinco subtests de la resolución (ocho con tus `t.Fatalf`), uno del pipeline, dos del parser y el golden.

#### El código

{{file ch07-s1 embed.go}}

{{file ch07-s1 stdlib/prelude.ki}}

{{file ch07-s1 internal/driver/pipeline.go}}

**`parseFiles`** lexa el preludio y el fichero con un solo interner: el mismo nombre es el mismo `Symbol`, y así `print` encuentra su definición.

{{file ch07-s1 internal/sema/resolve.go}}

**`collect`** es la pasada 1 de `resolver` en la Teoría, aún sin los items ilegibles.

**El parser:** sin cuerpo, exige `#[extern]`.

{{diff ch06-s4..ch07-s1 internal/syntax/parser.go}}

**Los comandos**, menos `emit tokens`, pasan por `LoadAndParse`:

{{diff ch06-s4..ch07-s1 internal/driver/check.go}}

{{diff ch06-s4..ch07-s1 internal/driver/commands.go}}

{{diff ch06-s4..ch07-s1 internal/driver/emit.go}}

{{reftest T07.03}}

{{reftest T07.05}}

Si te atascas o tu versión se desvía demasiado, copia el fichero de `ref/ch07-s1` y sigue.

Al terminar: tus tests en verde, commit y etiqueta `ch07-s1`; compárate con `ref/ch07-s1`.

### Sesión 2: ámbitos, variables locales y sombreado (≈ 2 h)

#### Go: un iterador propio

```go
// package iter: visible returns one. It hands each value to yield and
// stops as soon as yield returns false.
type Seq[V any] func(yield func(V) bool)
```

#### Los tests, en rojo

1. **Escribe T07.18–T07.25,** con T07.20 en su forma de esta sesión. `scope_internal_test.go` es `package sema`: `scope` no se exporta. La interfaz interna:

```go
type scope struct { names map[base.Symbol]LocalID; parent *scope }
func (s *scope) bind(name base.Symbol, id LocalID)
func (s *scope) lookup(name base.Symbol) (LocalID, bool)
func (s *scope) visible() iter.Seq[base.Symbol] // for the suggestions of session 3
```

2. **Míralos fallar:** no compila, `undefined: scope`.
3. **Añade stubs:** los métodos devuelven el valor cero, y `visible`, `func(yield func(base.Symbol) bool) {}` (con `nil`, el `range` haría panic). Fallan los ocho de ámbitos; los de la sesión 1 siguen en verde.

#### El código

{{file ch07-s2 internal/sema/scope.go}}

**`visible`** recorre de dentro afuera y ordena cada ámbito por `Symbol`.

{{diff ch07-s1..ch07-s2 internal/sema/resolve.go}}

- **`bodies`** recorre las `Def`, no los items: cada variable tiene por dueña la definición que se recorre, también la segunda de un E0302.
- **`name`** es `busca`, de la Teoría, sin rotas, etiqueta del `let` en curso ni sugerencia (sesiones 3 y 4). Los pases siguientes dan su `ResError` por explicado.

Y un comentario:

{{diff ch07-s1..ch07-s2 internal/driver/check.go}}

{{reftest T07.21}}

{{reftest T07.25}}

Si te atascas o tu versión se desvía demasiado, copia el fichero de `ref/ch07-s2` y sigue.

Al terminar: tus tests en verde, commit y etiqueta `ch07-s2`; compárate con `ref/ch07-s2`.

### Sesión 3: «did you mean» y la comprobación de nombres (≈ 2 h)

#### Los tests, en rojo

1. **Escribe T07.26–T07.38,** con T07.32 y T07.33 en su forma de esta sesión. La interfaz:

```go
func EditDistance(a, b string) int                          // package base
func suggest(name string, candidates []string) (string, bool) // package sema, unexported
func (r *resolver) candidates() []string
```

2. **Escribe el golden de T07.36,** en su forma de esta sesión:

{{file ch07-s3 tests/errors/ch07/unknown_name_render.ki}}

3. **Míralos fallar:** no compila, `undefined: EditDistance`.
4. **Añade stubs** que devuelven 0, `("", false)` y `nil`. Fallan la distancia, los siete de `suggest`, dos de las sugerencias y el golden, sin panic.

#### El código

{{file ch07-s3 internal/base/edit_distance.go}}

{{file ch07-s3 internal/sema/suggest.go}}

{{diff ch07-s2..ch07-s3 internal/sema/resolve.go}}

`EditDistance` y `suggest` son `distancia` y `sugiere`, de la Teoría, sin el límite de 128 bytes (sesión 4). El freno del sink está en `name`.

Y dos comentarios:

{{diff ch07-s2..ch07-s3 internal/driver/check.go}}

{{diff ch07-s2..ch07-s3 internal/sema/scope.go}}

{{reftest T07.26}}

{{reftest T07.30}}

Si te atascas o tu versión se desvía demasiado, copia el fichero de `ref/ch07-s3` y sigue.

Al terminar: tus tests en verde, commit y etiqueta `ch07-s3`; compárate con `ref/ch07-s3`.

### Sesión 4: diagnósticos al nivel de rustc y el catálogo (≈ 2,5 h)

#### Los tests, en rojo

1. **La interfaz nueva, para T07.39–T07.96:**

```go
// package diag
func Insert(at base.Span, text string) FixIt // empty span at at.Lo
func Delete(span base.Span) FixIt
func Replace(span base.Span, text string) FixIt
func (d Diagnostic) WithFixIt(f FixIt) Diagnostic // a copy; the help line says what it does
// RenderJSON: "fixits":[{"line","col","end_line","end_col","replacement"}] after "help"

// package syntax
type Attr struct { …; NameSpan base.Span; OneLine bool } // the name; no line break from # to ]
func (p *parser) prev() Token // the last token consumed, Newlines aside
func closerIsMissing(k TokenKind) bool // found k: is the closer what is missing?
type TokenBuffer struct { …; endsInComment bool; unterminatedLiteral base.Span }
func (tb *TokenBuffer) inUnterminatedLiteral(at uint32) bool

// package sema
const maxSuggestLen = 128 // a longer unknown name gets no suggestion
```

2. **Lleva a su forma final** T07.02, T07.05–T07.07, T07.20, T07.32 y T07.33: donde exigían «sin ayuda» o «sin nota», ahora exigen la que hay.
3. **Escribe los nuevos desde las tarjetas, menos los golden.** Los cambios del capítulo 3 están en el diff de `render_test.go` (más abajo).
4. **Toma de estos diffs los tests de los capítulos 3, 5 y 6,** que cambian en muchas líneas:

{{diff ch07-s3..ch07-s4 internal/syntax/source_check_test.go}}

{{diff ch07-s3..ch07-s4 internal/syntax/lexer_test.go}}

{{diff ch07-s3..ch07-s4 internal/syntax/numbers_test.go}}

{{diff ch07-s3..ch07-s4 internal/syntax/strings_test.go}}

{{diff ch07-s3..ch07-s4 internal/syntax/parser_test.go}}

{{diff ch07-s3..ch07-s4 internal/syntax/parser_internal_test.go}}

{{diff ch07-s3..ch07-s4 internal/syntax/parser_expr_internal_test.go}}

{{diff ch07-s3..ch07-s4 internal/syntax/parser_stmt_internal_test.go}}

{{diff ch07-s3..ch07-s4 internal/syntax/recover_test.go}}

{{diff ch07-s3..ch07-s4 internal/syntax/parser_fuzz_test.go}}

5. **Míralos fallar:** no compila, `undefined: Replace`.
6. **Añade stubs** que devuelven el valor cero; `WithFixIt` devuelve `d` y `Attr` gana los dos campos. Fallan 110 subtests de 17 funciones, sin panic; sin `tests/errors_docs_test.go`, que se lista más abajo, 106 de 16. No uses `-update` todavía: congelaría el stub.
7. **Escribe el catálogo:** un fichero por código, los diecinueve, como `E0301.md`, con el formato del comentario al principio del test (más abajo). Con él, su test ya pasa: los códigos existen.

{{file ch07-s4 docs/errors/E0301.md}}

:::more Las palabras de la valla
Un ejemplo va en una valla `ki erroneous` o `ki corrected`, con estas palabras detrás:

- **`escaped`:** el bloque es un literal de cadena de Go, y el test lo decodifica con `strconv.Unquote`, que le quita las comillas. Sirve para bytes que un fichero de texto no puede llevar: `E0101.md` escribe `"…// caf\xE9\n"`.
- **`run`:** el ejemplo pasa por `ki run`, que llega en el capítulo 10.
- **`also=E0201`:** otros códigos que el ejemplo da, y cada uno tiene que aparecer. Hoy una cadena, un carácter o un flotante no pueden ir en una expresión, así que los ejemplos de E0106, E0108 y E0109, y el corregido de E0110, dan además un E0201.
:::

#### El código

**`diag`:** el fix-it y su diff.

{{file ch07-s4 internal/diag/diagnostic.go}}

{{diff ch07-s3..ch07-s4 internal/diag/render.go}}

:::more Cómo se pinta el diff
- **`groupFixits`** comprueba las reglas de los fix-its de la Teoría y agrupa por línea.
- **`showWindow` y `writeNumbered`** salen de lo que pinta las etiquetas: el diff y la línea citada usan la misma función y no pueden pintar distinto (tabuladores, U+FFFD, ventana).
:::

**El lexer:** los E01xx.

{{diff ch07-s3..ch07-s4 internal/syntax/source_check.go}}

{{diff ch07-s3..ch07-s4 internal/syntax/token.go}}

{{diff ch07-s3..ch07-s4 internal/syntax/lexer.go}}

{{diff ch07-s3..ch07-s4 internal/syntax/numbers.go}}

{{diff ch07-s3..ch07-s4 internal/syntax/strings.go}}

:::more Ediciones que crearían otro error
- **E0103**, comentario sin cerrar, inserta un `*/` por nivel abierto: uno solo dejaría el mismo error. Si el fichero acaba en una `/` suelta (`/* see a/`), empieza con un espacio, porque `/*/` abriría otro comentario.
- **E0108, literal de carácter inválido, con varios caracteres** propone la cadena, salvo que lleve una `"` o un `${` sin escapar: la ayuda dice entonces qué escapar.
- **E0108 sin cerrar** ofrece la comilla solo tras un carácter exacto: tras `'ab` daría otro E0108.
- **`danglingBackslash`**, en un literal sin cerrar, quita la edición si la línea acaba en una barra impar, que escaparía la comilla.
- **`endsInComment`.** Si el fichero acaba dentro de un comentario, el cierre de un E0202, delimitador sin cerrar, quedaría comentado, y va tras el último token.
- **`inUnterminatedLiteral`.** Si ese cierre cae en un literal sin cerrar, solo hay ayuda: dónde va depende de cómo acabe.
:::

**El parser:** los E02xx.

{{diff ch07-s3..ch07-s4 internal/syntax/ast.go}}

{{diff ch07-s3..ch07-s4 internal/syntax/parser.go}}

{{diff ch07-s3..ch07-s4 internal/syntax/parser_expr.go}}

{{diff ch07-s3..ch07-s4 internal/syntax/parser_stmt.go}}

{{diff ch07-s3..ch07-s4 internal/syntax/recover.go}}

:::more Dónde coloca el parser la edición
- **`prev`** da el último token, sin contar los `Newline`, cuyo final es el principio de la línea siguiente. El cierre que falta va tras él, no ante el token hallado.
- **`closerIsMissing`** es la lista de tokens de la teoría; la secundaria sobre el delimitador abierto sale siempre.
- **`OneLine`.** Dentro de corchetes un salto de línea es un espacio, así que un atributo puede ocupar tres líneas. Borrarlo cruzaría saltos, y entonces solo hay ayuda.
:::

**`sema`:** los E03xx.

{{diff ch07-s3..ch07-s4 internal/sema/resolve.go}}

{{diff ch07-s3..ch07-s4 internal/sema/suggest.go}}

Completan `busca` y `sugiere` de la Teoría: **`initializing`** apila los `let` en curso, **`broken`** es el conjunto de las rotas y **`maxSuggestLen`** es el límite de 128 bytes.

Y un comentario:

{{diff ch07-s3..ch07-s4 internal/driver/check.go}}

**El test del catálogo:**

{{file ch07-s4 tests/errors_docs_test.go}}

Su último subtest da a cada comprobación un catálogo malo: viendo solo buenos, pasaría sin comprobar nada.

:::more El test del catálogo por dentro
- **`parser.ParseFile`** recibe `token.NewFileSet()`, que guarda las posiciones, y `parser.SkipObjectResolution`, que omite ligar nombres.
- **`ast.Inspect`** llama a una función con cada nodo; si devuelve `false`, no baja a sus hijos.
- **`strconv.ParseUint(lit.Value, 0, 16)`** lee el literal entero de cada llamada: `0x12D` vale, `70000` no cabe.
- **`regexp.MustCompile`** compila el patrón de los nombres de fichero, `^([EW][0-9]{4})\.md$`, y haría panic al arrancar si estuviera roto. **`FindStringSubmatch`** da `["E0301.md" "E0301"]` o `nil`.
- **`parseDoc`** es un bucle con tres estados: texto, una valla `ki`, que es un ejemplo, y otra valla, que no se lee.
- **Las comprobaciones** (`shapeProblems`, `exampleProblems`, `catalogProblems`) devuelven sus problemas como texto, sin `t.Errorf`, para que ese subtest pueda probarlas. `catalogProblems` ordena lo que encuentra: ningún fallo sale en el orden de un map.
:::

**Escribe el golden de T07.90:**

{{file ch07-s4 tests/errors/ch07/unclosed_paren_render.ki}}

{{reftest T07.39}}

{{reftest T07.43}}

Compárate con los tests de `diag` y `sema`:

{{diff ch07-s3..ch07-s4 internal/diag/render_test.go}}

{{diff ch07-s3..ch07-s4 internal/sema/resolve_test.go}}

{{diff ch07-s3..ch07-s4 internal/sema/scope_test.go}}

{{diff ch07-s3..ch07-s4 internal/sema/suggest_test.go}}

{{diff ch07-s3..ch07-s4 internal/sema/suggest_internal_test.go}}

Un fuzz como el del parser, para los pases que le siguen:

{{file ch07-s4 internal/sema/analyze_fuzz_test.go}}

Con el código escrito y `ki` recompilado:

1. **Escribe los ocho golden nuevos de `diag`** con `go test ./internal/diag -update`, y léelos contra las reglas.
2. **Reescribe los dos golden que cambian de salida** con `build/bin/ki test --update tests/errors` dos veces. La primera reescribe `// expect-stderr:`, que gana líneas y empuja el programa hacia abajo; la segunda graba la línea nueva del error.

Si te atascas o tu versión se desvía demasiado, copia el fichero de `ref/ch07-s4` y sigue.

Al terminar: tus tests en verde, commit y etiqueta `ch07-s4`; compárate con `ref/ch07-s4`.

## Ejecútalo

Con `go build -o build/bin/ki ./cmd/ki`, el programa del objetivo da el error del hito y sale con 1:

```console
$ cat fib.ki
fn fib(n: Int) -> Int {
  if n < 2 { n } else { fibb(n - 1) + fib(n - 2) }
}

fn main() {
  print(fib(20))
}
$ build/bin/ki check fib.ki
error[E0301]: unknown name `fibb`
 --> fib.ki:2:25
  |
2 |   if n < 2 { n } else { fibb(n - 1) + fib(n - 2) }
  |                         ^^^^ not found in this scope
  = help: did you mean `fib`?
  |
2 -   if n < 2 { n } else { fibb(n - 1) + fib(n - 2) }
2 +   if n < 2 { n } else { fib(n - 1) + fib(n - 2) }
```

Un fichero con cuatro errores:

```console
$ cat bad.ki
#[extrn("x")]
fn twice(n: Int) -> Int { (n + n }

fn main() {
  let total = total + 1
  prnt(twice(2))
}
$ build/bin/ki check bad.ki
error[E0305]: unknown attribute `extrn`
 --> bad.ki:1:1
  |
1 | #[extrn("x")]
  | ^^^^^^^^^^^^^ not an attribute Ki knows
  = note: a program cannot use attributes yet
  = help: remove it
  |
1 - #[extrn("x")]
1 +

error[E0201]: expected `)`, found `}`
 --> bad.ki:2:34
  |
2 | fn twice(n: Int) -> Int { (n + n }
  |                                  ^ expected `)` here
  |                           - unclosed `(` opened here
  = help: close it with `)`
  |
2 - fn twice(n: Int) -> Int { (n + n }
2 + fn twice(n: Int) -> Int { (n + n) }

error[E0301]: unknown name `total`
 --> bad.ki:5:15
  |
5 |   let total = total + 1
  |               ^^^^^ not found in this scope
  |       ----- `total` is bound by this `let`, which cannot see itself
  = note: a `let` is visible only after its own statement

error[E0301]: unknown name `prnt`
 --> bad.ki:6:3
  |
6 |   prnt(twice(2))
  |   ^^^^ not found in this scope
  = help: did you mean `print`?
  |
6 -   prnt(twice(2))
6 +   print(twice(2))
$ echo $?
1
```

En JSON, el del hito:

```console
$ build/bin/ki check --diagnostics=json fib.ki
{"severity":"error","code":"E0301","message":"unknown name `fibb`","path":"fib.ki","line":2,"col":25,"labels":[{"primary":true,"line":2,"col":25,"message":"not found in this scope"}],"notes":[],"help":"did you mean `fib`?","fixits":[{"line":2,"col":25,"end_line":2,"end_col":29,"replacement":"fib"}]}
```

## Ejercicios

:::exercise
**1. `W0301`, una variable sin usar.** Añade el warning `` unused variable `x` `` para un `let` que nadie lee, al nivel del listón: primaria «never used», ayuda «prefix it with `_` to keep it, or delete the `let`», el fix-it del prefijo, tests y su `docs/errors/W0301.md`.
:::

:::solution
`diag` gana `NewWarning`, igual que `NewError` con `Severity: Warning`. El resolvedor gana tres campos:

- **`used []bool`** crece en `bind` y se marca en `name` cuando un nombre es `ResLocal`.
- **`lets []LocalID`** son las variables que `stmt` añade tras el `bind` de cada `let`. `function` lo vacía al empezar y lo recorre al acabar con `unused`.
- **`unusedLets []diag.Diagnostic`** son los warnings pendientes.

`resolve.go` importa `strings`:

```go
func (r *resolver) unused(l LocalID) {
	loc := r.rp.Locals.Get(l)
	name := r.in.String(loc.Name)
	if r.used[l] || strings.HasPrefix(name, "_") {
		return
	}
	r.unusedLets = append(r.unusedLets, diag.NewWarning(diag.W(301), "unused variable `"+name+"`",
		diag.Label{Span: loc.Span, Message: "never used"}).
		WithHelp("prefix it with `_` to keep it, or delete the `let`").
		WithFixIt(diag.Insert(loc.Span, "_")))
}
```

`unused` no informa: guarda el warning, y al final de `Resolve` se informan solo `if !sink.HasErrors()`. Junto a un error, una variable puede parecer sin usar solo porque la línea que la leía está rota.

Nada más compilar, el test del catálogo exige `docs/errors/W0301.md`. Escríbelo con `let total = 1` y `print(2)` como erróneo y `let _total = 1` como corregido. Cambia los corregidos de E0201 y E0301, `fn main() { let x = 1 }`, por `fn main() { let x = 1; print(x) }`: ahora darían el warning.

El test va en `TestScopes`, con seis filas: sin usar; dos `let x` con la primera sin leer; leída; `_total`; un parámetro; y un programa con un error. Compara el warning entero con `reflect.DeepEqual` (importa `reflect`).

Sin la guarda de `HasErrors`, falla la última fila; sin la regla del `_`, la cuarta. `ki check` sale con 0: un warning no es un error.
:::

:::exercise
**2. Un local antes que una definición.** Haz que, a igual distancia, la sugerencia prefiera una variable a una función, con test.
:::

:::solution
En `suggest.go`, y `name` llama a `r.suggestion(name)` en lugar de `suggest(name, r.candidates())`:

```go
func (r *resolver) suggestion(name string) (string, bool) {
	best, ok := suggest(name, r.candidates())
	if !ok {
		return "", false
	}
	var locals []string
	for sym := range r.scope.visible() {
		locals = append(locals, r.in.String(sym))
	}
	if local, ok := suggest(name, locals); ok && base.EditDistance(name, local) == base.EditDistance(name, best) {
		return local, true
	}
	return best, true
}
```

`best` ya es el más cercano de todos, así que un local solo gana si empata. Cambia la fila `function first in the alphabet` del test de sugerencias, que ahora espera `gammb`, y su comentario.

El subtest nuevo, `local_wins_a_tie`, pide `alphb` (parámetro) frente a `alpha` (función), `prinx` (un `let`) frente a `print`, y `counter` (función a una edición) frente a `countxx` (parámetro a dos): la distancia sigue mandando. Sobre el código de `ch07-s4` falla con `help "did you mean `alpha`?", want "did you mean `alphb`?"`.
:::

:::exercise
**3. Determinismo con un map debajo.** Escribe un test de que las sugerencias son deterministas aunque los candidatos pasen por un map.
:::

:::solution
En `TestSuggestions` (añade `fmt` a los imports):

```go
	t.Run("deterministic_with_ties", func(t *testing.T) {
		var src strings.Builder
		for c := 'a'; c <= 'p'; c++ {
			fmt.Fprintf(&src, "fn k%c() {}\n", c)
		}
		src.WriteString("fn main() {\n  kz()\n}\n")
		for range 50 {
			suggestionIn(t, "sixteen ties", src.String(), "  kz()", "kz", didYouMean("ka"))
			if t.Failed() {
				return
			}
		}
	})
```

Dieciséis funciones a una edición de `kz`, y cada análisis crea su map con su semilla. Pasa sin cambios.

Lo instructivo es qué lo rompe. Quitar solo el `slices.Sorted` de `candidates` no, ni quitar solo el desempate `c < best` de `suggest`: cada protección basta. Sin las dos, falla en cada ejecución con otra letra.
:::

:::exercise
**4. `ki emit names FILE`.** Un destino de `emit` que liste cada `Def` y cada `Local` con su span.
:::

:::solution
`runEmit` gana `case "names": return emitNames(s, c, args[1:])`, y la línea de uso pasa a `ki emit tokens|ast|names [--diagnostics=human|short|json] FILE` en `commands.go` y en los tres sitios de `cli_test.go` que la escriben; `emit.go` importa `strings` y `base`:

```go
	s.Analyze(f)
	rp := f.Resolved
	where := func(sp base.Span) string {
		lo, hi := s.Sources.LineCol(sp.File, sp.Lo), s.Sources.LineCol(sp.File, sp.Hi)
		return fmt.Sprintf("%s:%d:%d-%d", s.Sources.DisplayPath(sp.File), lo.Line, lo.Col, hi.Col)
	}
	var b strings.Builder
	for id, d := range rp.Defs.All() {
		fmt.Fprintf(&b, "def %d %s %s\n", id, s.Interner.String(d.Name), where(d.Span))
	}
	for id, l := range rp.Locals.All() {
		owner := rp.Defs.Get(l.Owner).Name
		fmt.Fprintf(&b, "local %d %s %s (%s)\n", id, s.Interner.String(l.Name), where(l.Span), s.Interner.String(owner))
	}
	return finishEmit(s, c, b.String())
```

Antes, `fileArgs` y `LoadAndParse` como en `emitAST`. Para `fib.ki` da cinco líneas, de `def 0 print std/prelude.ki:3:8-13` a `local 1 n fib.ki:1:8-9 (fib)`, y luego el E0301.

El test, `TestCLI/emit_names`, llama a `t.Chdir(t.TempDir())`, que cambia de directorio y lo restaura con `Cleanup`, para que la ruta mostrada sea `a.ki`. Compara la salida entera.
:::

:::exercise
**5. `ki check --fix FILE`.** Aplica a una copia del fichero todos los fix-its de la primera comprobación, de derecha a izquierda, y vuelve a comprobarla; con test de que `fibb` se convierte en `fib` y de que dos fix-its en una línea no se pisan.
:::

:::solution
`fileArgs` gana el parámetro `more func(*flag.FlagSet)` (`emit` pasa `nil`) y `check` registra `fs.BoolVar(&fix, "fix", false, …)`. La línea de uso pasa a `ki check [--fix] [--diagnostics=human|short|json] FILE`, también en `cli_test.go`.

Tras `Analyze`, `checkFixed` junta los fix-its del fichero que compilas, aplica, imprime la copia por stdout y la comprueba en una `Session` nueva, cuyo diagnóstico y código de salida son los que cuentan. El fichero del disco no cambia. `check.go` importa `cmp`, `flag`, `slices` y `diag`, e `io` si imprimes con `io.WriteString`.

```go
func applyFixits(src []byte, fixits []diag.FixIt) []byte {
	fixits = slices.Clone(fixits)
	slices.SortStableFunc(fixits, func(a, b diag.FixIt) int { return cmp.Compare(b.Span.Lo, a.Span.Lo) })
	out := slices.Clone(src)
	limit := uint32(len(src))
	for _, f := range fixits {
		if f.Span.Hi > limit {
			continue // overlaps one already applied
		}
		out = slices.Replace(out, int(f.Span.Lo), int(f.Span.Hi), []byte(f.Replacement)...)
		limit = f.Span.Lo
	}
	return out
}
```

`slices.Replace` sustituye `out[i:j]` por los bytes dados. De derecha a izquierda, cada edición trabaja sobre posiciones que ninguna otra ha movido todavía.

El test, `TestCLI/check_fix`, usa el `fib` del objetivo con un `main` que dice `prnt(fibb(20))`: espera la copia con `fib` y `print(fib(20))`, código 0, stderr vacío y el fichero intacto.

Ordenadas de izquierda a derecha y sin la guarda, el `fibb` → `fib` de la línea 2 acorta el texto un byte, así que `prnt` → `print` cae un byte a la derecha: `pprintfib(20))`.
:::
