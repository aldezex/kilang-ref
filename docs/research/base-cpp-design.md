# Ki: documento de diseño del lenguaje y del libro

**Fecha:** 2026-10-23 · **Estado:** listo para tu aprobación. Hasta que lo apruebes no se construye nada: ni repositorios, ni código, ni libro.

**Cómo leerlo.**
- Lo que decidiste tú aparece como tuyo, con tus palabras cuando las hay (apartado 4).
- Lo que he decidido yo lleva la marca **[mi decisión]** y está resumido en el apartado 15, para que lo revises de un vistazo.
- Las nueve preguntas abiertas ya tienen tu respuesta. Están en el apartado 16 y en la tabla del apartado 4.

**De dónde sale.**
- Tus respuestas al cuestionario (9 rondas).
- Un dossier de investigación con fuentes sobre los lenguajes anfitriones candidatos y sobre cómo están construidos Koka, Lean 4, OCaml 5, Roc y GHC.
- Un panel de diseño: tres propuestas independientes (pedagogía primero, arquitectura primero, riesgos primero), puntuadas por tres jueces (fidelidad a tus respuestas, solidez técnica, pedagogía).
- Una revisión independiente: alguien sin contexto leyó el borrador como si tuviera que planificar la construcción. Sus preguntas acabaron en el apartado 16 o resueltas por mí en el 15.

La base es la propuesta «arquitectura primero», que recomendaron dos de los tres jueces. Le he injertado las mejores ideas de las otras dos y las correcciones que pidieron los jueces. El dossier irá a `docs/research/` en el repositorio.

---

## 1. Intención

Quieres aprender un lenguaje nuevo «de 0 a profesional experto» construyendo el tuyo propio, Ki: funcional, con los conceptos centrales de Haskell, centrado en concurrencia, multihilo y rendimiento, y no implementado en Haskell. El lenguaje que aprendes construyéndolo es C++. Cuando el diseño esté cerrado, el resultado es un libro web completo, «de 0 a 100», paso a paso, respaldado por un repositorio de referencia cuyos tests pasan en cada capítulo.

Hay dos productos que se sostienen mutuamente:

1. **Ki v0.** Compilador y runtime escritos a mano en C++23, con tres backends que se comprueban entre sí:
   - un intérprete de referencia, que hace de oráculo;
   - una máquina virtual de bytecode;
   - código nativo x86-64.
2. **El libro.** El camino para que construyas Ki y, por el camino, aprendas C++ desde cero hasta nivel profesional.

## 2. Objetivos, no-objetivos y anti-objetivos

### Objetivos
- **El lenguaje.** Ki es funcional y puro: estricto con `lazy` explícito, efectos algebraicos con handlers, Hindley-Milner con typeclasses y tipos de orden superior, ADTs con pattern matching exhaustivo, y errores que se leen.
- **La sintaxis.** Ki se escribe como TypeScript, Rust o Go: llaves, `nombre: Tipo`, `f(x, y)`, palabras clave y operadores familiares, `|>`. Funcional por dentro, familiar por fuera.
- **Concurrencia al estilo Go.** Fibras, canales y `select` sobre un planificador multinúcleo con work stealing escrito por ti, con un modo determinista para depurar.
- **Rendimiento real.** RC de Perceus con reutilización en el sitio, y código nativo con selección de instrucciones y asignación de registros propias.
- **Arquitectura definitiva desde el capítulo 1.** Cada hito rellena etapas del pipeline final y ninguna se tira.
- **C++.** Aprender C++ desde cero hasta nivel profesional; ante la duda, gana la empleabilidad.
- **El libro.** Tests antes del código, ejercicios, preguntas de repaso y resumen en cada capítulo.

### No-objetivos de v0 (van al epílogo, o a ninguna parte)
- Continuaciones multi-shot, handlers de usuario para `IO` y `Async`, y cancelación estructurada.
- Nada en el sistema de tipos más allá de Haskell 98 + HKT + filas de efectos: ni clases multiparámetro, ni dependencias funcionales, ni familias de tipos, ni GADTs, ni tipos de rango N.
- Macros, FFI con C, gestor de paquetes, LSP, compilación incremental, JIT, WebAssembly.
- Un recolector de ciclos; como mucho, en el epílogo, uno restringido a canales.
- Actores, STM o paralelismo de datos como características del lenguaje. No los elegiste; el paralelismo va como biblioteca.

### Anti-objetivos (sería un fracaso aunque funcionara)
- Un zoo de extensiones al estilo GHC, o errores de tipos crípticos.
- Una sintaxis superficial con aspecto de Haskell u ML.
- Arquitectura provisional que haya que tirar después.
- Depender de LLVM, Cranelift, Boost, fmt, GoogleTest, Catch2, Flex, Bison o cualquier biblioteca de terceros dentro de Ki.
- Un listado del libro que no compile, o cuyos tests no pasen en la etiqueta que cita.
- Un libro que vuelque código sin explicarlo.
- Basar algo en el Ki de agosto (`~/ki`), o tocarlo.
- Dejar basura fuera del repo: clones, worktrees o builds temporales.

## 3. Restricciones

- **Anfitrión.**
  - C++23 (ISO/IEC 14882:2024).
  - Nada de C++26: no está publicado, y la reflexión y los contratos solo existen como experimento en GCC 16.
  - Cabeceras, no módulos de C++20.
- **Todo a mano.**
  - Solo la biblioteca estándar de C++ (que incluye la de C), más `as` y `ld` del sistema y las herramientas de construcción.
  - Las envolturas POSIX y Linux de glibc (`mmap`, `epoll`, `futex`, `eventfd`) cuentan como el sistema operativo, no como biblioteca de terceros.
  - gdb, perf, objdump, QEMU, la CI y los compiladores cruzados de los capítulos finales (binutils y g++ para AArch64 y para MinGW-w64, paquetes de apt) son herramientas, no bibliotecas enlazadas.
  - El framework de tests, el formato de casos de test, el generador de programas y el extractor de listados se escriben en C++ dentro del repo.
- **Máquina.**
  - Ubuntu 26.04 LTS sobre WSL2 (kernel 6.18), 16 CPUs lógicas y unos 30 GB de RAM.
  - GCC 15.2 por defecto, CMake 4.2.3 y binutils 2.46.
  - Clang no está instalado; el archivo de Ubuntu ofrece clang-21.
  - perf no está instalado, y en WSL hay que compilarlo desde el árbol del kernel.
  - TSan necesita `sysctl vm.mmap_rnd_bits=28` en kernels 6.6 o posteriores.
  - GCC 15.2 usa `-ffp-contract=fast` por defecto, incluso con `-std=c++23`.
- **Plataforma.** Linux x86-64 con el ABI System V en todo el libro; Windows x64 y ARM64 solo en los capítulos finales.
- **Repositorios.** Dos nuevos (apartado 11.1), en ext4 bajo `/home`, no en `/mnt/c`. El nombre `aldezex/ki` ya está ocupado en GitHub por el proyecto antiguo.
- **Libro.**
  - Prosa en español. El código, los identificadores, los comentarios, las rutas del repo y los mensajes del compilador, en inglés.
  - Web con índice y progreso guardado, solo para escritorio.
  - Se entrega entero de una vez.
- **Ritmo.** De 5 a 10 h por semana, con un horizonte de dos años o más.

## 4. Lo que decidiste tú

| Tema | Tu decisión |
|---|---|
| Lenguaje anfitrión | C++. Yo había recomendado Rust y elegiste C++ |
| Punto de partida en C++ | Desde cero, apoyándote en Rust, Go, TS y Python para comparar |
| Qué significa «experto» | Las cuatro cosas: que te paguen por usarlo, dominarlo por sí mismo, usarlo en proyectos propios y contribuir a compiladores y open source. Si chocan, gana la empleabilidad |
| Para qué sirve Ki | Propósito general (CLIs, servidores, programas normales) y cálculo paralelo |
| Qué quitar de Haskell | El zoo de extensiones y los errores crípticos |
| Evaluación | Estricta, con `lazy` explícito |
| Efectos | Algebraicos con handlers, al estilo Koka / OCaml 5 |
| Tipos | Hindley-Milner + typeclasses + tipos de orden superior |
| Sintaxis | Llaves al estilo Rust/Gleam y, después, «La idea es que sea funcional pero con sintaxis muy similar a typescript/rust/go» |
| Concurrencia | Fibras + canales + `select`, al estilo Go |
| Runtime | Lo escribes tú: planificador multinúcleo con work stealing y gestión de memoria propia |
| Memoria | Conteo de referencias al estilo Perceus |
| Backends | Intérprete → VM → nativo, en ese orden. El intérprete forma parte de la arquitectura final (es el oráculo) |
| Nativo | Emitir ensamblador, con selección de instrucciones y asignación de registros propias. `as` y `ld` codifican y enlazan |
| Hecho a mano | «quiero construir todas las áreas del compilador a mano, intérprete, etc. Usar librerías lo más mínimo» |
| Ritmo | Arquitectura definitiva desde el principio, aunque tarde semanas en imprimir algo. Aceptas unos 2–3 meses hasta el primer programa |
| Capítulos | Tests antes del código, ejercicios con solución plegada, preguntas de repaso y resumen de teoría |
| Código en el libro | Mixto: completo para lo nuevo; pista y solución plegada para lo que ya sabes hacer |
| Tests | El libro dice qué probar y por qué; tú escribes el test; la versión de referencia está plegada |
| Repo de referencia | «Sí, pero quiero escribir yo los tests» |
| Entrega | El libro entero de una vez |
| Formato | Web con índice y progreso guardado, solo para escritorio |
| Idioma | Español, con el código en inglés |
| Plataformas | Linux x86-64 en WSL primero; Windows y ARM64 en los capítulos finales |
| Experiencia en compiladores | «Nada aún»: todo desde cero, con listados completos en lo que sea de compiladores |
| `Int` | 64 bits completos, con panic al desbordar. Yo había recomendado 63 bits; el apartado 8.1 explica cómo lo implemento sin su coste |
| Repositorios | Dos, públicos en GitHub y con licencia MIT: `aldezex/kilang` (el tuyo) y `aldezex/kilang-ref` (la referencia) |
| Aspecto | TS + Rust: camelCase, lambdas `(x) => e`, `=>` en el match e imports `import { … } from …` |
| Efectos | `uses {Log}` en las firmas y `handle { … } with { … }` para manejarlos |
| Traits | Como en Rust, con `Self`: `trait Show { … }` e `impl Show for Shape` |
| Llamadas con punto | Por el tipo, como un método |

---

## 5. Ki v0: el lenguaje

### 5.1 Un vistazo

Este es tu ejemplo, ampliado con un trait, un efecto, un handler, `spawn` y `select`, con la sintaxis que elegiste.

```ki
// shapes.ki: your sample, extended (Ki v0)
import { after, ms } from std/time

#[derive(Eq, Show)]
type Shape {
  Circle(radius: Float)
  Rect(w: Float, h: Float)
}

fn area(s: Shape) -> Float {
  match s {
    Circle(r) => pi * r * r
    Rect(w, h) => w * h
  }
}

// A trait is a typeclass; an impl is an instance
trait Describe {
  fn describe(x: Self) -> String
}

impl Describe for Shape {
  fn describe(s: Shape) -> String {
    match s {
      Circle(r) => "circle r=${r}"
      Rect(w, h) => "rect ${w}x${h}"
    }
  }
}

// A user-defined algebraic effect
effect Log {
  fn log(msg: String)
}

// map is effect-polymorphic, so Log flows into this signature
pub fn totalArea(shapes: List<Shape>) -> Float uses {Log} {
  shapes
  |> map((s) => {
       log("measuring ${s.describe()}")
       area(s)
     })
  |> sum
}

// A handler with a control clause: k is a one-shot continuation
pub fn collectLogs<A>(body: () -> A uses {Log}) -> (A, List<String>) {
  handle {
    body()
  } with {
    log(msg), k => {
      let (result, rest) = resume(k, ())
      (result, [msg, ..rest])
    }
    return(x) => (x, [])
  }
}

fn main() {
  let shapes = [Circle(1.0), Rect(2.0, 3.0)]
  let (total, lines) = collectLogs(() => totalArea(shapes))
  for line in lines { print(line) }
  print("total area: ${total}")

  let results: Chan<Float> = channel(2)
  for s in shapes {
    spawn { send(results, area(s)) }      // one fiber per shape
  }
  select {
    r = recv(results) => print("first: ${r}")      // r: Option<Float>
    recv(after(ms(500))) => print("timed out")
  }
}
// measuring circle r=1.0
// measuring rect 2.0x3.0
// total area: 9.141592653589793
// first: Some(3.141592653589793)      (o Some(6.0): depende de la planificación)
```

### 5.2 Léxico y la regla de línea

**Identificadores.**
- Los que empiezan por mayúscula nombran tipos, constructores, traits y efectos.
- Los que empiezan por minúscula nombran valores, funciones y variables de fila.
- Así, un patrón distingue un constructor de una variable sin consultar nada.
- Los identificadores son ASCII.
- Valores y funciones en camelCase (lo elegiste tú).

**Comentarios:** `//` y `/* */`.

**Literales:**
- enteros: `42`, `1_000`, `0xFF`, `0b1010`;
- flotantes: `3.14`, `1e-9`;
- caracteres: `'a'`, `'\u{1F600}'`;
- strings: `"x = ${x}\n"`, con interpolación `${expr}`.

**Regla de línea (los `;` son opcionales)** **[mi decisión]**.
- Dentro de `()` y `[]`, el salto de línea es espacio.
- Dentro de `{}` o en el nivel superior, un salto de línea termina la sentencia, el item o el brazo, salvo en dos casos:
  - la línea acaba en un token que no puede cerrar una expresión (un operador binario, `,`, `(`, `=`, `=>`…);
  - la línea siguiente empieza por `|>`, `.`, `&&`, `||` o `else`.
- Por tanto, una línea que empieza por `-y` o `+y` nunca se pega a la anterior.

### 5.3 Gramática (boceto EBNF)

```text
module    = { import } { item }
import    = 'import' path [ 'as' lower ]                       // import std/list        → list.map(xs, f)
          | 'import' '{' name { ',' name } '}' 'from' path     // import { map } from std/list
path      = lower { '/' lower }
item      = { attr } [ 'pub' ] ( fn | type | trait | impl | effect | const )
attr      = '#[' lower [ '(' args ')' ] ']'                    // #[derive(Eq, Show)]  #[test]  #[extern("ki_write")]
fn        = 'fn' lower [ generics ] '(' [ param { ',' param } ] ')' [ '->' ty ] [ uses ] block
fnsig     = 'fn' lower [ generics ] '(' [ param { ',' param } ] ')' [ '->' ty ] [ uses ]
param     = lower [ ':' ty ]                                   // ': ty' required in pub fns
generics  = '<' gen { ',' gen } '>'
gen       = Upper [ '<' '_' { ',' '_' } '>' ] [ ':' bounds ]   // T   T: Show + Eq   F<_>: Functor
          | lower                                              // an effect-row variable
bounds    = Upper { '+' Upper }
uses      = 'uses' [ 'only' ] '{' [ eff { ',' eff } ] [ [ ',' ] '..' lower ] '}'
eff       = Upper [ '<' ty { ',' ty } '>' ]
type      = 'type' Upper [ generics ] ( '=' ty                              // alias
                                      | '{' variant { vsep variant } '}'    // sum type
                                      | '{' field { vsep field } '}' )      // record
variant   = Upper [ '(' [ lower ':' ] ty { ',' [ lower ':' ] ty } ')' ]
field     = lower ':' ty
vsep      = NEWLINE | ';' | ','
trait     = 'trait' Upper [ '<' '_' { ',' '_' } '>' ] [ ':' bounds ] '{' { fnsig | fn } '}'
impl      = 'impl' [ generics ] Upper 'for' ty '{' { fn } '}'
effect    = 'effect' Upper [ generics ] '{' { fnsig } '}'
const     = 'let' lower ':' ty '=' expr                        // module constant: pure, evaluated once
ty        = Upper [ '<' tyarg { ',' tyarg } '>' ] | 'Self' [ '<' ty { ',' ty } '>' ]
          | '(' [ ty { ',' ty } ] ')' [ '->' ty [ uses ] ]     // tuple, unit, or function type
tyarg     = ty | '_'
block     = '{' { stmt sep } [ expr ] '}'           sep = NEWLINE | ';'
stmt      = 'let' pat [ ':' ty ] '=' expr | 'var' lower [ ':' ty ] '=' expr | lower '=' expr
          | 'for' pat 'in' expr block | 'while' expr block | 'break' | 'continue' | 'return' [ expr ]
          | fn | expr
expr      = Pratt, loosest first:  |>   ||   &&   == != < <= > >= (non-associative)   ..   + -   * / %
            prefix: ! -      postfix: call (args), .field, .0, .name(args) (a method call, resolved by type), ?
primary   = literal | lower | Upper [ record ] | Upper '.' lower | '(' [ expr { ',' expr } ] ')'   // Show.show(x)
          | '[' [ expr { ',' expr } [ ',' '..' expr ] ] ']'
          | lambda | block | 'if' expr block [ 'else' ( block | if ) ]
          | 'match' expr '{' { pat [ 'if' expr ] '=>' expr sep } '}'
          | 'handle' block 'with' '{' { clause sep } '}'
          | 'spawn' ( block | call ) | 'lazy' block | 'select' '{' { arm sep } '}'
lambda    = ( '(' [ lower [ ':' ty ] { ',' lower [ ':' ty ] } ] ')' | lower ) '=>' ( expr | block )
record    = '{' finit { ',' finit } [ ',' '..' expr ] '}'      // not allowed directly in an if/match/for/while head
finit     = lower ':' expr | lower                             // { x, y } is punning
clause    = lower '(' [ lower { ',' lower } ] ')' [ ',' ( lower | '_' ) ] '=>' expr
          | 'return' '(' lower ')' '=>' expr
arm       = [ pat '=' ] 'recv' '(' expr ')' '=>' expr | 'send' '(' expr ',' expr ')' '=>' expr | 'default' '=>' expr
pat       = '_' | lower | literal | Upper [ '(' pat { ',' pat } ')' ] | Upper '{' fpat { ',' fpat } [ ',' '..' ] '}'
          | '(' pat { ',' pat } ')' | '[' [ pat { ',' pat } ] [ ',' '..' ( lower | '_' ) ] ']' | pat '|' pat
```

Reglas que mantienen el parser sencillo **[mi decisión]**:
- **Sin argumentos de tipo explícitos en las expresiones.** En una expresión, `<` siempre es «menor que». Si hace falta fijar un tipo, se anota el `let`.
- **Restricción de Rust para los literales de record.** Un literal `Name { … }` no puede ir directamente en la cabeza de un `if`, `match`, `for` o `while`. Así `if x == Origin {` no es ambiguo.
- **Lambdas por búsqueda acotada.** Al ver `(`, el parser busca el `)` emparejado y mira si lo sigue `=>`.
- **Sin backtracking.** El parser es descenso recursivo más Pratt.
- **`a.b(c)` nunca es ambiguo.** Si `a` nombra un módulo, es una llamada cualificada. Si no, es una llamada a método (apartado 5.5). Para llamar a una función guardada en un campo se escribe `(a.b)(c)`, como en Rust.

### 5.4 Tipos e inferencia

**Tipos predefinidos:**
- `Int`, entero con signo de 64 bits; `UInt64`; `Float` (IEEE binary64); `Bool`; `Char` (un escalar Unicode); `Byte` (u8); `String` (UTF-8 inmutable); `()`;
- tuplas `(A, B)`;
- funciones `(A, B) -> C uses {…}`.

El preludio añade `List<T>`, `Option<T>`, `Result<T, E>`, `Ordering`, `Range`, `Lazy<T>` y `Chan<T>`. La biblioteca estándar, `Array<T>`, `Map<K, V>`, `Set<T>` y `Duration`.

**Reglas de inferencia.** Son las seis que mantienen el HM real (lo pediste) y a la vez predecible y con errores locales (también lo pediste):

1. **HM bidireccional.** Unificación con union-find y niveles, al estilo de Rémy y OCaml. Se usa el modo de comprobación siempre que el tipo esperado ya se conoce, así que los parámetros de una lambda pasada a `map` no necesitan anotación. El acceso a campos necesita que el tipo del receptor se conozca en ese punto de la lectura de izquierda a derecha.
2. **Las funciones `pub` llevan la firma completa:** parámetros, retorno y efectos. Los errores no salen de la función y las interfaces de módulo son explícitas.
3. **Las funciones privadas de nivel superior pueden omitir anotaciones.** Se infieren y se generalizan por componente fuertemente conexa del grafo de llamadas (Tarjan). Las restricciones de traits se convierten en parámetros de diccionario. La recursión polimórfica necesita firma.
4. **Lo local nunca se generaliza** (`let`, `fn` local y lambdas), como MonoLocalBinds en GHC. Usarlo con dos tipos da un error con la solución:
   `local function 'id' is used at (Int) -> Int (4:9) and (String) -> String (5:9); move it to the top level to make it generic`.
5. **Literales.**
   - Un literal entero es `Int`, o `Byte` o `UInt64` si el tipo esperado ya es uno de ellos (con comprobación de rango).
   - Un literal flotante es `Float`.
   - No hay ningún otro defaulting. Una ambigüedad es un error con una anotación propuesta.
6. **Lo que no hay.**
   - Subtipos, conversiones implícitas, sobrecarga fuera de los traits, rango N y cómputo a nivel de tipos.
   - Currificación: las funciones no se currifican y cada llamada tiene aridad exacta.

Un tipo de error interno `?err` unifica en silencio con todo, para que un solo fallo no provoque una cascada de mensajes.

### 5.5 Traits y tipos de orden superior

Los traits son typeclasses de un parámetro (`Self`), con la sintaxis de Rust (lo elegiste tú):

```ki
trait Show {
  fn show(x: Self) -> String
  fn showNested(x: Self) -> String { show(x) }   // default method; String overrides it to add quotes
}
trait Ord: Eq {
  fn compare(a: Self, b: Self) -> Ordering
  fn lt(a: Self, b: Self) -> Bool { compare(a, b) == Less }
}
impl<A: Show> Show for List<A> { fn show(xs: List<A>) -> String { ... } }

trait Functor<_> { fn map<A, B>(xs: Self<A>, f: (A) -> B) -> Self<B> }   // Self takes one argument
impl Functor for List<_> { ... }
impl<K> Functor for Map<K, _> { ... }
fn sum<F<_>: Foldable, A: Num>(xs: F<A>) -> A { fold(xs, zero(), (acc, x) => acc + x) }
```

**Presupuesto de extensiones (es todo):**
- Un parámetro por trait.
- La cabeza de un impl es un constructor de tipo aplicado a variables distintas, o a huecos `_` al final.
- Los contextos solo restringen variables de tipo.
- Las superclases son acíclicas.
- No hay impls solapados ni huérfanos: un impl vive en el módulo del trait o en el del tipo de la cabeza.
- Hay métodos por defecto y `#[derive(Eq, Ord, Show)]`.
- Con esto, la resolución de instancias termina por construcción y nunca hace backtracking.

**Kinds.**
- Se declaran con huecos (`F<_>` es `* -> *`) y nunca se infieren.
- Los constructores de tipo están currificados, así que los huecos solo rellenan los últimos parámetros y la unificación de orden superior sigue siendo de primer orden.
- Por eso `Map<K, _>` puede ser un Functor y `Result<T, E>`, que conserva el orden de Rust, no lo es: los errores se componen con el efecto `Fail<E>`. **[mi decisión]**

**Operadores.** Son métodos de traits del preludio:

| Operador | Trait |
|---|---|
| `+` | `Add` (String también lo implementa, para concatenar) |
| `-` | `Sub` |
| `*` | `Mul` |
| `/` | `Div` |
| `%` | `Rem` |
| `-` prefijo | `Neg` |
| `==` | `Eq` |
| `<` | `Ord` |

Cuando los tipos son concretos, que es lo normal, la elaboración llama directamente a la primitiva.

**`Show`** es la representación para humanos: lo que imprime `print` y lo que inserta `${}`. Un `String` se muestra sin comillas, salvo dentro de un constructor derivado, porque `showNested` las añade: `print("hi")` imprime `hi` y `print(Some("hi"))` imprime `Some("hi")`. **[mi decisión]**

**Compilación.**
- El paso de diccionarios es la semántica.
- Una restricción resuelta por una instancia conocida se convierte en una llamada directa.
- Con `-O2`, un especializador acotado clona funciones aplicadas a diccionarios conocidos: caché por (función, ids de diccionario), límite de profundidad 8 y presupuesto de tamaño.
- Los diccionarios nunca se cierran sobre sí mismos:
  - un método recursivo de una instancia llama directamente a la función de la instancia;
  - un método por defecto recibe el diccionario como parámetro explícito;
  - un impl con contexto se convierte en una función que construye un record de punteros a código y diccionarios de contexto.
  - Así el heap sigue siendo acíclico (apartado 8.9).

**Llamadas con punto: métodos por tipo** (lo elegiste tú). `e.name(args)` es una llamada a método cuando `e` no nombra un módulo.
- El comprobador de tipos sintetiza primero el tipo de `e`, que tiene que conocerse en ese punto de la lectura de izquierda a derecha. Si no se conoce, el error propone una anotación.
- `name` se busca en este orden:
  1. funciones `name` declaradas en el módulo que declara el tipo de `e`, visibles desde la llamada, cuyo primer parámetro es de ese tipo;
  2. métodos `name` de los traits visibles en la llamada (los del preludio y los importados), si hay un impl para el tipo de `e`. Esto incluye receptores genéricos con su restricción: con `x: T` y `T: Show`, `x.show()` funciona;
  3. si no hay nada, el error dice qué tipo y qué nombre faltan, sugiere nombres parecidos y, si existe una función libre con ese nombre, propone `name(e, …)` o `e |> name`.
- Si encajan dos candidatos del mismo nivel, es un error de ambigüedad, y se desambigua con `Show.show(x)`.
- Por esta regla, cada tipo del preludio vive en su propio módulo con sus funciones (`List` en `std/list`, `String` en `std/string`, `Chan` en `std/chan`…), y el preludio lo reexporta. Así `xs.map(f)`, `s.len()` o `ch.send(v)` funcionan sin imports.
- No se recurre a «cualquier función a la vista» (UFCS). Las funciones auxiliares locales se llaman como `f(x)` o con `|>` **[mi decisión]**.

### 5.6 Efectos en las firmas

**Declarar y realizar.** Los efectos se declaran como interfaces y se realizan como llamadas normales:

```ki
effect Log      { fn log(msg: String) }
effect State<S> { fn get() -> S; fn put(s: S) }
effect Fail<E>  { fn fail<A>(err: E) -> A }      // result type ∀A: abortive by typing
effect Yield<T> { fn yield(value: T) }

pub fn load(path: String) -> Config uses {IO, Fail<ParseError>} { ... }
```

**Filas.** Son filas con etiquetas con ámbito (*scoped labels*), las de Leijen que usa Koka:
- las etiquetas casan por nombre y se permiten duplicadas;
- cada fila tiene una sola cola;
- la unificación es decidible y principal, y no necesita restricciones *lacks*;
- al usuario se le muestran como conjuntos.

**Regla ambiente** (cierra el agujero que señaló el juez técnico). Dada la firma `fn f<…>(p1: T1, …) -> R uses {L}`:
- Hay una variable de fila implícita ε, generalizada. La fila de `f` es `{L | ε}`.
- Un tipo función de un parámetro escrito sin `uses` toma la fila entera de la firma, `{L | ε}`.
- Uno escrito `uses {M}` toma `{M, L | ε}`: las etiquetas M delante de la fila entera de la firma.
- `uses only {M}` es una fila cerrada, `{M}`, sin ε.
- Dentro del cuerpo, ε es rígida.
- Consecuencias:
  - `fn area(s: Shape) -> Float` es pura;
  - `fn map<A, B>(xs: List<A>, f: (A) -> B) -> List<B>` es polimórfica en efectos sin decir nada;
  - en `collectLogs`, `body` tiene la fila `{Log | ε}`.
- Las variables de fila explícitas (`<A, e>` y `uses {Log, ..e}`) dan filas independientes cuando hacen falta.
- Dentro de una declaración de tipo, los tipos función son cerrados por defecto. Para guardar una reanudación de un productor con efectos se escribe la variable:
  `type Gen<T, e> { Done, Next(value: T, rest: () -> Gen<T, e> uses {..e}) }`

**Reglas de uso.**
- Una función `pub` escribe su fila; en las privadas se infiere.
- Una función referida por nombre se abre al instanciarla, así que una función menos efectiva siempre encaja donde se espera una más efectiva.

**Efectos primitivos.**
- `IO` cubre consola, ficheros, red, reloj, entorno y aleatoriedad. `Async` cubre spawn, canales, `select`, `sleep` y `yieldNow`.
- Los maneja el runtime en la raíz de cada fibra; el código de usuario no puede manejarlos en v0.
- `main` es implícitamente `uses only {IO, Async}`.
- El cuerpo de un `spawn` debe ser `uses only {IO, Async}`, porque una fibra empieza sin handlers de usuario. El de un `lazy`, `uses only {}`.
- Los panics no son efectos.

**Handlers.** `handle { body } with { cláusulas }` es una expresión. Las cláusulas tienen tres formas, y la forma dice lo que cuesta **[mi decisión]**:

| Forma | Significa |
|---|---|
| `op(x) => e` | tail-resumptive: equivale a `op(x), k => resume(k, e)` (`get() => s`, `log(m) => print(m)`) |
| `op(x), k => e` | de control: `k: Cont<R, U>` y `resume(k, v): U` |
| `op(x), _ => e` | abortiva: suelta `k` sin reanudarlo |
| `return(x) => e` | opcional; si falta, es la identidad |

**Tipado de los handlers.**
- El cuerpo tiene tipo `T uses {L | r}`, y toda cláusula, incluida `return`, produce `U uses {..r}`.
- Como el cuerpo se tipa en `{L | r}`, una función genérica no puede capturar en silencio un efecto que su callback no declaró:
  `'retry' handles Fail<String> around 'f', but 'f' may perform any effect its caller allows; declare 'f: () -> A uses {Fail<String>}'`.
- Los errores de efectos nombran la operación, muestran la fila declarada y proponen las dos soluciones: declarar el efecto o manejarlo.

**Continuaciones.**
- Son one-shot: reanudar dos veces es un panic.
- Se pueden guardar en una estructura y reanudar más tarde dentro de la misma fibra, aunque el `handle` ya haya devuelto (así funcionan los generadores).
- Son locales a su fibra: enviarlas por un canal o capturarlas en un `spawn` es un panic. **[mi decisión, corrección del juez técnico]**

### 5.7 Datos y pattern matching

**Declaraciones con `type`** (tu elección de la muestra; no hay `struct` ni `enum`):
- **Sumas:** `type Option<A> { None, Some(A) }`.
- **Records:** `type Point { x: Float, y: Float }`.
  - Se construyen como `Point { x: 1.0, y: 2.0 }` y se leen como `p.x`.
  - Se actualizan como `Point { x: 3.0, ..p }`; si `p` es único, en el sitio.
- **Alias:** `type Name = String`.

**Tuplas y listas.**
- Las tuplas se indexan con `.0`.
- `List<A>` tiene azúcar `[]`, `[a, b]` y `[h, ..t]`, que también sirve para anteponer en una expresión.
- `0..n` es un `Range`.

**Patrones:**
- `_`, variables y literales;
- constructores posicionales, y records con punning y `..`;
- tuplas y listas;
- or-patterns `A | B` y guardas `if`.

**Comprobación de `match`.**
- `match` es una expresión y debe ser exhaustivo.
- Lo comprueba el algoritmo de utilidad de Maranget. Las guardas no cuentan para la cobertura.
- Un caso que falta es un error con testigo (`pattern Rect(_, _) not covered`); un brazo inalcanzable, un warning.
- Los patrones de `let` deben ser irrefutables.

**Compilación de `match`.** Los matches se compilan a árboles de decisión de Maranget, y los cuerpos de los brazos se comparten mediante join points. El intérprete conserva el matching ingenuo, de arriba abajo, así que el compilador de matches queda comprobado contra él desde el primer día.

### 5.8 Control de flujo familiar sobre un núcleo puro

`var`, `for`, `while`, `break`, `continue` y `return` existen como azúcar **[mi decisión, apoyada por los tres jueces en tus propias palabras]**.
- La elaboración los convierte en join points y bucles `joinrec` sobre un Core puro, enhebrando el estado de los `var` como parámetros. Te da la intuición de SSA gratis.
- Un closure no puede capturar un `var`; es un error con explicación. Así no existen referencias mutables y el heap sigue acíclico.
- `if` sin `else` es válido como sentencia (vale `()`).
- En v0, `for` recorre `List`, `Array` y rangos de `Int`. Para cualquier `Foldable` está `each(xs, f)`. La razón es que v0 no tiene tipos asociados con los que expresar «el tipo de elemento de un iterador».

### 5.9 Módulos

**Un fichero es un módulo**, con el nombre de su ruta: `geometry/vec.ki` es `geometry/vec` y `std/…` es la biblioteca incluida.

**Formas de import**, al estilo de TS (lo elegiste tú):
- `import std/list` da acceso cualificado: `list.map(xs, f)`.
- `import std/list as l` renombra el módulo.
- `import { map, filter } from std/list` trae nombres sin cualificar.

**Visibilidad y alcance.**
- Todo es privado salvo lo marcado `pub`. Un `pub type` exporta también sus constructores.
- El preludio es implícito y se puede sombrear.
- Los impls son globales, limitados por la regla de huérfanos.

**Ciclos e inicialización.**
- Un ciclo de imports es un error que muestra el ciclo.
- Eso fija el orden de inicialización: las constantes `let` de nivel superior se evalúan una vez, en orden topológico de módulos y luego de declaración. Las dependencias entre constantes se siguen también a través de los cuerpos de las funciones, como en Go.
- Un ciclo entre constantes es un error de valor recursivo.

**Compilación.**
- v0 compila programas enteros.
- Como las firmas `pub` son obligatorias, las interfaces de módulo se pueden calcular sin los cuerpos, lo que deja la puerta abierta a compilación separada y paralela.
- La raíz del proyecto es el directorio del fichero de entrada: no hay `ki.toml` en v0. **[mi decisión]**

### 5.10 Números, texto y pereza

**`Int`**: entero con signo de 64 bits completos, como en Rust o Go (lo elegiste tú).
- Todo desbordamiento es un panic, también `MIN / -1`: `panic: integer overflow at f.ki:3:9`, código de salida 101.
- `/` trunca hacia cero y `%` toma el signo del dividendo. Dividir por cero es un panic.
- La aritmética que da la vuelta va en funciones explícitas (`wrappingAdd`…).
- `UInt64`, sin signo, existe desde el capítulo 16 con las mismas reglas.
- Cómo se representa lo explica el apartado 8.1 (inmediato si cabe en 63 bits, caja si no). El programa no lo nota.

**`Float`**: IEEE binary64.
- No hay conversión implícita: `toFloat(i)` y `truncate(f)`.
- Los operadores de comparación siguen IEEE; `compare` usa el orden total.
- Se imprime con la forma más corta que da la vuelta, con `.0` si es entero (`1.0`, `6.0`, `0.1`), para distinguir Int de Float en la salida.

**Floats idénticos en todos los backends:**
- SSE2 en todas partes;
- `-ffp-contract=off`, porque GCC 15.2 usa `fast` por defecto;
- nada de `-march=native` en el intérprete ni en el runtime;
- una sola libm;
- una sola primitiva de formato, `std::to_chars`, compartida.

**`String`**: bytes UTF-8 inmutables.
- `len` cuenta bytes, como en Go y Rust; `chars(s)` da `Char`.
- `+` concatena, en el sitio cuando el operando izquierdo es único.
- El corte es por rangos de bytes, comprobados contra los límites de carácter.

**Pereza.** `lazy { e }` tiene tipo `Lazy<T>` y `force(t)` lo evalúa a lo sumo una vez y memoriza el resultado.
- El cuerpo debe ser puro (`uses only {}`).
- Si una fibra fuerza un thunk que se está evaluando en esa misma fibra, salta el panic `lazy value depends on itself`.
- Si dos fibras lo fuerzan a la vez, ambas pueden evaluarlo. El primer CAS publica el resultado y la otra suelta el suyo; como el cuerpo es puro, nadie lo nota **[mi decisión, injerto de «riesgos primero»]**.
- `std/stream` construye `Stream<T> { Empty, Cons(head: T, tail: Lazy<Stream<T>>) }`.

### 5.11 Concurrencia en el lenguaje

**`spawn`**, al estilo `go` de Go:
- `spawn { bloque }` o `spawn f(a, b)`. En la segunda forma, `f` y sus argumentos se evalúan en la fibra actual y la llamada corre en una fibra nueva.
- Devuelve `()`.
- El cuerpo es `uses only {IO, Async}`.
- Lo que captura pasa a ser compartido.

**`Chan<T>`**, con la semántica de Go:
- `channel(cap)`; con capacidad 0 es un rendezvous.
- `send(ch, v)`; enviar a un canal cerrado es un panic, y cerrar dos veces también.
- `recv(ch) -> Option<T>` da `None` cuando el canal está cerrado y vacío.
- El cierre es siempre explícito, con `close(ch)`; nunca se cierra solo al soltar el último extremo, porque eso haría observable el momento del drop (corrección del juez técnico).

**`select`**:
- tiene brazos `recv`, `send` y `default`;
- `after(d)` devuelve un `Chan<()>` con buffer 1 que alimenta el servicio de temporizadores, igual que `time.After` en Go.

**Tiempo y cesión:** `sleep(d)` y `yieldNow()`.

**Fin del programa.**
- El proceso termina cuando `main` devuelve, como en Go.
- Si todas las fibras están bloqueadas y no hay E/S ni temporizadores pendientes, el runtime informa del bloqueo, con los sitios donde está aparcada cada fibra, y sale con 102.

**Paralelismo y espera, como bibliotecas** **[mi decisión]**:
- `std/par` ofrece `parMap(xs, grain, f)` y `parReduce`.
- `std/task` ofrece `task(f)` y `join(t)`, construidos sobre `spawn` y un canal de buffer 1.

### 5.12 Biblioteca estándar v0

Está escrita en Ki sobre primitivas del runtime:
- **Preludio:**
  - los tipos Option, Result, List, Ordering y Range;
  - los traits Eq, Ord, Show, Add, Sub, Mul, Div, Rem, Neg, Num, Functor, Foldable y Monad (`pure`, `flatMap`);
  - las funciones `print`, `each`, `map`, `filter`, `fold`, `sum`, `len`, `panic` y `assert`, y la constante `pi`;
  - `Chan` y sus funciones `channel`, `send`, `recv` y `close`, reexportadas de `std/chan`, como las primitivas de canal de Go. `spawn` y `select` son palabras clave.
- **Datos:** `std/list`, `std/array` (actualizable en el sitio si es único), `std/string`, `std/char`, `std/int`, `std/float`, `std/map` y `std/set` (árboles balanceados por peso, con reutilización FBIP), `std/lazy` y `std/stream`.
- **Efectos:** `std/effects`, con Fail (y `catch` y `toResult`), State (`runState`), Reader y Yield (generadores). El `e?` de Rust es azúcar para `match e { Ok(v) => v, Err(x) => fail(x) }`.
- **Sistema:** `std/io` (print, readLine, readFile, writeFile), `std/fs`, `std/net` (TCP sobre el poller), `std/time` (Duration, now, sleep, after, ms) y `std/env` (args, getenv, exit).
- **Concurrencia:** `std/chan`, `std/task` y `std/par`.
- **Tests:** `std/test`. Las funciones se marcan `#[test]`, se comprueban con `assertEq` y se ejecutan con `ki test`.

Fuera de v0: hash maps, regex, JSON, HTTP (el capítulo 61 escribe un servidor HTTP como programa) y FFI.

---

## 6. Core: la referencia semántica

### 6.1 La IR

Core es la única referencia semántica.

**Qué es** **[mi decisión, corrección del juez técnico]**:
- Un cálculo lambda n-ario, estricto, con binders únicos y sin azúcar.
- No tiene tipos: se borran tras la elaboración.
- Lo comprueba Core Lint después de cada pase.

**Por qué no tiene tipos.** Los diccionarios de traits con tipos de orden superior tienen campos polimórficos (`Functor.map<A, B>`). Tiparlos exige System Fω, como en GHC Core, y eso es una carga enorme para un primer compilador.

**Anotaciones de representación.** Cada binder lleva una anotación `rep`, que en v0 vale siempre `val`. El pase de desempaquetado del capítulo 56 la usa para elegir `i64` o `f64` en código monomórfico; la elaboración la rellena a partir de los tipos, que en ese momento todavía conoce.

```text
a ::= x | literal | @f                                   -- atom; @f = static closure of a top-level fn
e ::= a | let x = e; e                                   -- strict, non-recursive
    | letfn f(x̄) = e and …; e                            -- local recursion (removed in S3)
    | join j(x̄) = e; e | joinrec j(x̄) = e; e | jump j(ē)   -- from if/loops/return/match sharing
    | e(ē)                                               -- exact-arity call; a tail call in tail position
    | match e { p => e | … }                             -- S0–S1: nested patterns
    | case a { K(x̄) => e | lit => e | _ => e }           -- S2+: flat tag switch
    | prim p(ē) | K(ē) | e.i | fn(x̄) => e
    | perform L.op(ē) | handle e with H | resume(e, e)
    | lazy e | force e | spawn e | chanop(ē) | select { … } | panic(code, e)
H ::= { return(x) => e; op_i(x̄, k) => e … }             -- clause sugar has already been expanded
```

Por la elaboración ya han pasado los traits (los diccionarios son records), las operaciones de efectos (resueltas a etiqueta e índice) y todo el azúcar: pipes, `.f()`, interpolación, update de records, operadores y bucles.

**Etapas.** Todas son subconjuntos o extensiones de la misma IR, y el intérprete puede ejecutar cualquiera **[mi decisión, injerto de «riesgos primero»]**:

| Etapa | Qué es |
|---|---|
| S0 | Core elaborado, con patrones anidados |
| S1 | ANF: todos los argumentos son átomos |
| S2 | árboles de decisión: `case` plano y proyecciones |
| S3 | sin `letfn`, con closures de entorno explícito |
| S4 (Core+RC) | S3 más `dup x`, `drop x`, `tok = dropReuse x`, `K@tok(ā)` y marcas de préstamo en los parámetros. Cada llamada, `perform`, `handle` y `resume` lleva su *conjunto de suspensión*: las variables poseídas y vivas y los tokens de reutilización en ese punto |

Los mapas de llamada de la VM y la frametable nativa se traducen desde esos conjuntos de suspensión, y cada slot lleva su clase: valor o token de reutilización.

**Lint por etapa** comprueba:
- ámbitos y aridades;
- la forma de los constructores;
- ANF desde S1;
- que un `jump` solo aparezca en posición de cola dentro del ámbito de su join, nunca cruzando un `fn` ni un `handle`;
- que los `case` sean completos;
- en S4, la linealidad: en cada camino, cada variable poseída se consume exactamente una vez.

### 6.2 Semántica small-step de los handlers

Está escrita en `docs/semantics.md`, y el intérprete la implementa literalmente.

**Configuración.**
- Valores: literales, constructores, closures y referencias a continuaciones κ.
- Contextos de evaluación: `E ::= □ | let x = E; e | #h{E}`, donde `#h{·}` es el marco de un handler.
- `E⁻ᴸ` es un contexto sin ningún marco que maneje L.
- El almacén σ asigna a cada κ el estado `Live(F)` o `Used`.

```text
[handle]  handle e with h                   → #h{ e }
[return]  #h{ v }                           → h.return(v)                            (identity if absent)
[perform] #h{ E⁻ᴸ[perform L.op(v̄)] }, σ     → h.op(v̄, κ), σ[κ ↦ Live(#h{E⁻ᴸ})]       κ fresh
[resume]  resume(κ, v), σ(κ) = Live(F)      → F[v], σ[κ ↦ Used]
[twice]   resume(κ, v), σ(κ) = Used         → panic "continuation resumed twice"     (exit 101)
[drop]    (S4) the last reference to κ dies while σ(κ) = Live(F)
                                            → the owned slots of F's suspension sets are released;
                                              no Ki code runs (Ki has no finalizers)
```

**Reglas del modelo:**
- **Profundidad.** Los handlers son profundos: F contiene `#h`. La cláusula corre fuera de `#h`, así que los efectos que realiza van a los handlers exteriores.
- **Azúcar de cláusulas.** `op(x̄) => e` es `op(x̄, k) => resume(k, e)`, y `op(x̄), _ => e` suelta `k`.
- **Camino rápido.** Un backend puede ejecutar una cláusula tail-resumptive como una llamada directa con el handler enmascarado (`handler_top := h.parent` mientras corre `e`).
  - Es equivalente, porque κ se reanuda una vez y en el acto.
  - La máscara la guarda y la restaura un marco en la pila, de modo que si un handler exterior captura una continuación dentro de la cláusula, al reanudarla se restaura también la máscara.
  - El oráculo nunca toma este atajo.
  - La VM y el nativo lo implementan en el capítulo 55, tras pruebas diferenciales. Antes, ejecutan la cláusula literalmente.
- **Efectos primitivos.** `perform IO.op` y `perform Async.op` llegan siempre a la raíz de su fibra y dan un paso con δ_IO y δ_Async.
- **Pereza.** `force` es atómico en la semántica: el cuerpo es puro y no tiene puntos de planificación.

**Concurrencia.** La configuración se extiende a `⟨F₁ ∥ … ∥ Fₙ, σ, C, T, reloj⟩`, con canales C y temporizadores T.
- Spawn, las operaciones de canal, `select` y `sleep` son reglas δ sobre C y T.
- En cada punto de planificación, una función de elección decide la siguiente fibra y el caso de `select`. En modo determinista, esa función es la del apartado 8.8.
- Los efectos de usuario nunca cruzan fibras.

### 6.3 Qué es observable y cómo se comparan los backends

**Observable:**
- los bytes de stdout;
- la primera línea de stderr de un panic;
- el código de salida (0; 1 si hay errores de compilación; 101 panic; 102 deadlock; 103 desbordamiento de pila);
- en modo determinista, la traza del planificador;
- en los modos con RC comprobado, un censo de fugas igual a cero.

**Floats entre plataformas.** Las operaciones básicas (`+ - * /` y `sqrt`) son exactas según IEEE en cualquier plataforma, y el formato de impresión (`std::to_chars`) también. Las funciones trascendentes (`sin`, `exp`, `log`…) dependen de la libm de cada sistema: glibc en Linux, UCRT en Windows. Un test que las usa lleva `// libm: platform` en su cabecera y solo se compara dentro de la misma plataforma **[mi decisión]**.

**No observable:** el momento de un drop, las direcciones, los tiempos y la profundidad a la que se desborda la pila. Si un backend se desborda y otro no, el resultado es inconcluso. El cierre de un descriptor al soltarlo tampoco es observable: la red no entra en las pruebas diferenciales, y en modo determinista es un panic.

**Tres capas de comparación** **[mi decisión]**:
1. **Entre pases.** `ki test --check-passes` ejecuta S0, S1, S2, S3 y S4 (este sobre el heap comprobado) y exige observables idénticos. Un fallo queda localizado en el pase que lo introdujo.
2. **Entre backends.** `interp`, `interp-rc`, `vm` y `native` ejecutan el mismo programa con `--sched=det --seed=s` para muchas semillas, y la salida debe ser idéntica byte a byte. Los programas multinúcleo solo se comparan si su cabecera declara `schedule: independent`.
3. **Entradas.**
   - El corpus escrito a mano en cada capítulo.
   - `kigen`, un generador de programas bien tipados: ADTs, closures, recursión con combustible, las tres formas de cláusula, continuaciones guardadas y abandonadas en cada `perform`, pereza, y canales y `select` en modo determinista.
   - Los fallos se encogen por delta debugging y se quedan para siempre en `tests/diff/corpus/`.

**Puntos ciegos, y qué los cubre:**
- **Un fallo del front end que comparten todos los backends.** Lo cubren las salidas golden escritas a mano, los tests golden de errores y los tests de propiedades de ida y vuelta del parser.
- **Un fallo del runtime que comparten la VM y el nativo.** Lo cubren el planificador y el modelo de canales propios del intérprete, escritos desde la misma especificación pero de forma independiente, junto con los tests unitarios, de estrés y de TSan del runtime.

---

## 7. El compilador en C++23

### 7.1 Etapas

| # | Etapa | Entrada → salida | Responsabilidades |
|---|---|---|---|
| 0 | Driver | argv, ficheros → sesión | `ki run`, `build`, `check`, `test`, `emit <etapa>`, `explain`, `doctor` y `fmt`. Elige el backend, gestiona los volcados, limita el número de errores y fija los códigos de salida |
| 1 | Lexer | bytes UTF-8 → tokens `{kind, Span, payload}` | Maximal munch; regla de línea con pila de corchetes; modos para la interpolación; comprobación de literales; tokens de error, y sigue adelante |
| 2 | Parser | tokens → AST en `IdVec` | Descenso recursivo y Pratt; búsqueda acotada para las lambdas; restricción de literales de record; nodos Error y resincronización en separadores, `}` y palabras clave de item |
| 3 | Grafo de módulos y resolución de nombres | ASTs → DefTable y tablas laterales | Ficheros e imports; ciclos; preludio; ámbitos y visibilidad; `a.b` cuando `a` nombra un módulo; «¿quisiste decir…?»; dónde pueden aparecer `var`, `return` y `break`; captura de `var` prohibida |
| 4 | Declaraciones y kinds | items → entornos de tipos, traits, impls y efectos | Kinds, ciclos de alias, expansión de `derive`, coherencia y huérfanos, grafo de dependencias de las constantes |
| 5 | Tipos y efectos | cuerpos (por SCC) → tipo de cada nodo, evidencia y filas | HM bidireccional con niveles, unificación de filas, regla ambiente, resolución de traits, búsqueda de métodos por tipo, generalización, procedencia de cada restricción |
| 6 | Patrones | matches tipados → diagnósticos | Exhaustividad y redundancia (Maranget), testigos |
| 7 | Elaboración | AST tipado → Core S0 | Azúcar fuera; paso de diccionarios; `perform`, `handle` y `resume` explícitos; bucles a join points; orden de inicialización. Solo corre sobre programas sin errores |
| 8 | Pases de Core | S0 → S1 → S2 → S3 | ANF, árboles de decisión, lambda lifting y conversión de closures. Más tarde, el optimizador (cap. 55). Lint después de cada pase |
| 9 | Perceus | S3 → S4 | Inferencia de préstamos, dup/drop, especialización de drop, reuse, conjuntos de suspensión, Lint de linealidad |
| 10 | Intérprete | cualquier Sx → ejecución | El oráculo (apartado 9.1) |
| 11 | MIR | S4 → SSA de primer orden con parámetros de bloque | Asignación, campos, tags, RC y llamadas al runtime explícitos; anotaciones de suspensión; verificador de MIR |
| 12 | Emisor de bytecode | MIR → módulo | Asignación de registros por liveness, pool de constantes, mapas de llamada con clases de slot, verificador y desensamblador |
| 13 | Backend x86-64 | MIR → `.s` | Selección de instrucciones, liveness, linear scan y su verificador, marcos, frametable, `.loc` y CFI |
| 14 | Ensamblado y enlace | `.s` y `libkirt.a` → ELF | `as`, y `ld` con la línea de enlace capturada al configurar; los fallos de las herramientas se convierten en diagnósticos |
| 15 | Test runner | tests → informe | Golden, matriz backend × semilla, `--check-passes`, entrega al reductor |

### 7.2 Estructuras de datos y estilo de C++

**Ids y almacenamiento.**
- Todo se direcciona con ids de 32 bits tipados: `template<class Tag> struct Id { std::uint32_t v; }`. Así salen `FileId`, `Symbol`, `ExprId`, `TypeId`, `DefId`, `VarId`, `BlockId` y `VReg`.
- Cada id indexa un `IdVec<Id, T>`, un `std::vector` que solo se puede indexar con su propio tipo de id, como el `IndexVec` de rustc.
- Las listas de hijos son rangos `{first, count}` sobre pools compartidos.
- No hay punteros que posean entre nodos: los árboles se mueven, se vuelcan y se verifican sin coste.

**Nodos.** Son structs pequeños en un `std::variant` por categoría, recorridos con `std::visit` y el idiom `overloaded`, porque C++23 no tiene pattern matching.

**Arena.** Bloques de 64 KiB con asignación por desplazamiento (bump). Solo admite tipos trivialmente destructibles, y un `static_assert` lo comprueba.

**Interner.** Los bytes viven en la arena, más un `std::unordered_map<std::string_view, Symbol>` con búsqueda heterogénea.

**TypeTable.**
- Los tipos están hash-consed, así que la igualdad de tipos es igualdad de ids.
- Un vector union-find guarda `{parent, rank, level, kind, Origin}` por variable. `Origin` (span, razón y nodo relacionado) es lo que produce las notas «porque…».

**SourceManager.** Contenido de los ficheros, tablas de líneas construidas bajo demanda y `Span{FileId, lo, hi}`.

**Estructuras propias.** `SmallVec` y `DenseBitSet`, escritas a mano.

**Reglas de estilo:**
- Los pases no mutan su entrada.
- Ninguna salida depende del orden de iteración de un contenedor desordenado; se ordena por id.
- No hay `new` ni `delete` a pelo fuera de las arenas y del runtime.
- Los resultados llevan `[[nodiscard]]`.
- Un fallo único se devuelve con `std::expected`; los diagnósticos van a un `DiagSink`.
- Ninguna excepción cruza la frontera de un pase.
- Los errores internos van por `KI_ASSERT`, que imprime un `std::stacktrace`.
- El compilador se construye con excepciones activadas (la biblioteca estándar puede lanzarlas). El runtime, con `-fno-exceptions -fno-rtti`.

### 7.3 Diagnósticos

Un diagnóstico es un valor:

```cpp
struct Diagnostic {
  Severity severity; Code code; std::string message;
  Label primary; std::vector<Label> secondary;
  std::vector<std::string> notes; std::optional<std::string> help; std::vector<FixIt> fixits;
};
```

**Códigos estables** **[mi decisión]**:

| Prefijo | Etapa |
|---|---|
| `E01xx` | lexer |
| `E02xx` | parser |
| `E03xx` | nombres y módulos |
| `E04xx` | tipos |
| `E05xx` | traits |
| `E06xx` | efectos |
| `E07xx` | patrones |
| `E08xx` | kinds |
| `W…` | warnings |

`ki explain E0401` imprime `docs/errors/E0401.md`.

**Renderizadores:**
- humano, con color solo en una terminal;
- corto (`file:line:col: error[E0401]: msg`);
- líneas JSON.

Los tests comprueban códigos y spans; el texto renderizado tiene su propio conjunto pequeño de golden.

**Errores legibles:**
- Cada llamada a `unify` lleva una razón (una anotación, el argumento i, las ramas de un `if`, los brazos de un `match`, un operando, el retorno o el cuerpo de un handler).
- Los tipos se imprimen con los nombres del usuario.
- Los mensajes nunca muestran diccionarios, niveles ni variables de inferencia.
- Hay como mucho una línea de ayuda, con fix-it si existe.

```text
error[E0401]: mismatched types
  --> shapes.ki:12:23
   |
12 |     Rect(w, h) => w * 2
   |                   -   ^ expected `Float`, found `Int`
   |                   |
   |                   this is `Float`, so `*` needs a `Float` on the right
   = help: write `2.0`; Ki never converts Int to Float implicitly
```

**Recuperación:**
- El lexer salta los bytes inválidos.
- El parser resincroniza e inserta nodos Error, sin emitir errores nuevos a menos de 3 tokens del anterior.
- Los nombres desconocidos se ligan a una definición de error, y `?err` unifica en silencio.
- Cada item de nivel superior se comprueba por separado.
- El front end se detiene tras 100 errores, y las etapas posteriores solo corren sobre entrada sin errores.

---

## 8. El runtime: libkirt

`libkirt.a` está escrita en C++23 con `-fno-exceptions -fno-rtti`. La usan la VM (dentro del proceso `ki`) y los ejecutables nativos (enlazados con `ld`). **No usa `thread_local`.** La razón: las fibras migran entre hilos, y GCC 15.2 a `-O2` pliega `&tls` a través de una llamada, incluso detrás de un accesor `noinline`. Por eso el contexto del worker se pasa explícitamente (y en `r15` en el código nativo). **[mi decisión, corrección del juez técnico]**

### 8.1 Representación de valores

Un valor es una palabra de 64 bits:
- **Bit 0 = 1: inmediato.** Un `Int` que cabe en 63 bits se codifica como `(n << 1) | 1`. `Bool`, `Char`, `Byte`, `()` y los constructores sin campos son índices pequeños codificados igual.
- **Bit 0 = 0: puntero** a un objeto del heap alineado a 8 bytes. Nunca es nulo.

**`Int` de 64 bits en una palabra uniforme** **[mi decisión, para cumplir tu elección sin su coste]**:
- Un `Int` que cabe en 63 bits es siempre inmediato, y uno que no cabe es siempre un `BoxedInt` en el heap. La forma es canónica, así que comparar dos inmediatos es comparar los valores.
- La aritmética tiene un camino rápido en línea: si los dos operandos son inmediatos y el resultado cabe en 63 bits, no hay ninguna llamada. Si no, una función del runtime calcula en 64 bits, normaliza el resultado (inmediato o caja) y hace panic si desborda los 64 bits.
- Los valores fuera de ±2⁶² son raros en la práctica, así que casi nunca se paga la caja. El capítulo 56 quita además el chequeo de etiqueta en el código monomórfico, con `i64` crudos en registros.
- El intérprete no ve nada de esto: usa `int64_t` con `__builtin_*_overflow`.
- `UInt64` usa el mismo esquema.

**Cabecera de 8 bytes**, con la codificación de Lean:
- `int32 rc`:
  - `> 0`: local a la fibra, contado sin atómicos;
  - `< 0`: compartido, contado atómicamente, con magnitud `-rc`;
  - `0`: inmortal.
- `uint8 kind`: Ctor, Closure, String, Array, BoxedInt, BoxedFloat, Thunk, Cont, Chan o Resource.
- `uint8 tag`.
- `uint16 scan`: cuántos campos pueden contener punteros.

**Representación uniforme.** En v0, `Float` va empaquetado (*boxed*), así que el código polimórfico no necesita monomorfización. El capítulo 56 añade `i64` y `f64` crudos en el código monomórfico, usando las anotaciones `rep`.

**Objetos inmortales.** Son inmortales los literales, los closures estáticos, los diccionarios sin contexto y las constantes ya inicializadas. Un contador que llega a `INT32_MAX` se satura y el objeto se vuelve inmortal: es una fuga, pero nunca un use-after-free.

### 8.2 Perceus en el runtime

**Caminos rápidos y lentos.**
- El código generado incluye en línea el caso local de `dup` y `drop`.
- El runtime atiende los objetos compartidos (atómicos, con `acq_rel` en el último decremento) y la liberación.
- Liberar es iterativo: la lista de trabajo va enhebrada dentro de las propias celdas liberadas, así que soltar una lista de 10⁷ elementos no desborda la pila.

**Compartición.** `ki_share(v)` corre en `spawn`, en `send` y al publicar un thunk compartido.
- Recorre `v` con una pila explícita y pasa cada objeto local alcanzable a compartido.
- Se para en lo que ya es compartido o inmortal, porque un objeto compartido nunca apunta a uno local; así, cada objeto se marca a lo sumo una vez.
- Si encuentra una continuación, hace panic.

**Reutilización.** Solo cuando `rc == 1`. Reutilizar una celda compartida rompería el invariante «lo compartido no apunta a lo local» **[mi decisión, corrección del juez técnico]**.

**Migración.** Que una fibra migre de worker no exige atómicos en sus objetos locales: solo esa fibra los toca, y cada traspaso (un robo de deque o el cerrojo de un canal) es una arista release/acquire.

### 8.3 Allocator

Sigue el diseño de mimalloc, escrito a mano sin tomar su código.
- **Arenas y páginas.** Arenas de 4 MiB alineadas, obtenidas con `mmap` y partidas en páginas de 64 KiB. `ptr & ~0xFFFF` encuentra la cabecera de página: `{clase, free list local, free list atómica thread_free, dueño, usados}`.
- **Clases de tamaño.** Unas 40: de 8 en 8 bytes hasta 128, y luego cuatro por potencia de dos hasta 32 KiB, todas alineadas a 8. Los objetos más grandes tienen su propio mapeo.
- **Camino rápido.** Cada worker guarda en su contexto una cabeza de free list por clase, así que asignar es sacar un elemento de `[r15 + off]`.
- **Liberación.** El worker dueño libera en la lista local. Los demás, porque las fibras migran, hacen CAS sobre `thread_free`, y el dueño recoge esa lista cuando la local se vacía.
- **Devolución.** Las páginas vacías se devuelven al sistema con `madvise`.
- **Modos:**
  - `KI_ALLOC=debug`: sitios de asignación, envenenado con `0xDD`, cuarentena, doble free detectado y un censo al salir que nombra los ciclos de canales;
  - `KI_ALLOC=system`: reenvía a `malloc`, para que ASan vea cada objeto.

### 8.4 Fibras, pilas y continuaciones

**Segmentos de pila nativos** **[mi decisión, corrección del juez técnico]**.
- Son reservas `MAP_NORESERVE` talladas en slabs de 4 GiB, así que hay una sola VMA por slab.
- Las páginas se comprometen al tocarlas.
- Tamaño por defecto: 16 MiB por segmento de fibra o de handler, y 1 GiB para `main`. `KI_STACK_MB` lo cambia.
- Se reciclan por worker y se recortan con `madvise` al devolverlos.
- Las builds de depuración añaden una página `PROT_NONE` de guarda.
- El crecimiento por copia va al epílogo.

**Chequeo de prólogo.** Cada función Ki empieza con `cmp rsp, [r15 + LIMIT]`. El mismo chequeo sirve para el desbordamiento (exit 103) y para la preempción.
- El límite está 64 KiB por encima del fondo del segmento, para dejar margen a los marcos de C++ del runtime.
- El trabajo pesado del runtime corre en la pila de sistema del worker.

**Una fibra** es un segmento raíz, más su contexto guardado, `handler_top`, su estado y su id.

**Handlers con segmento propio.**
- Un handler con alguna cláusula de control ejecuta su cuerpo en un segmento nuevo. La base del segmento guarda el registro del handler y un trampolín de retorno.
- Si todas sus cláusulas son tail-resumptive, desde el capítulo 55 empuja el registro en la pila actual en lugar de abrir un segmento.

**Continuación.** Es un objeto `Cont` que posee la cadena desprendida de segmentos, desde el `perform` hasta el segmento del handler incluido (los handlers son profundos), más el `rsp`, `rbp` y `rip` guardados.
- Capturar y reanudar es reenlazar punteros en O(1) y hacer un cambio de pila.
- Soltar una `Cont` viva recorre sus marcos con los mapas de suspensión y suelta exactamente los slots poseídos, según su clase (valor o token de reutilización). No ejecuta código Ki.

**Cambio de contexto.** `ki_switch` (en `switch.S`) guarda `rbx`, `rbp` y `r12`–`r14` e intercambia `rsp`.
- Nunca restaura `r15` desde el estado de la fibra.
- Las entradas del runtime que pueden suspender devuelven `{valor, ctx}` en `rax:rdx`, y quien llama recarga `r15`. Así, una fibra robada por otro worker sigue con el contexto correcto (corrección del juez técnico).

**Sanitizers.** En las builds con sanitizers, `ki_switch` anota los cambios con `__sanitizer_start_switch_fiber`, `__sanitizer_finish_switch_fiber` y `__tsan_switch_to_fiber`.

**Fibras de la VM.** Son estados de la VM (ventanas de registros en segmentos del heap). Una operación bloqueante devuelve `Park` al bucle de dispatch, así que ninguna pila de C++ se bloquea nunca.

**Fibras del intérprete.** Son estados CEK.

### 8.5 Planificador

Hay N workers: 16 por defecto en esta máquina, y `KI_WORKERS` lo cambia.

**Deque de Chase–Lev por worker**, en la forma C11 de Lê et al. (PPoPP 2013) **[mi decisión, corrección del juez técnico]**:
- índices `top` y `bottom` `int64` con signo, porque un `size_t` da la vuelta en `take()` cuando `bottom` es 0;
- un anillo fijo de 1024 slots `std::atomic<Fiber*>` con cargas y almacenamientos relaxed;
- cuando se llena, la mitad pasa a la cola global protegida por mutex, así que nunca se redimensiona y no hay problema de reclamación de memoria;
- la build de TSan usa una variante con atómicos `seq_cst`, porque TSan no modela `atomic_thread_fence`.

**`runnext`.** Un slot que guarda la fibra que la actual acaba de despertar.

**Bucle del worker**, en este orden: `runnext`, pop local (LIFO), la cola global, robos a víctimas aleatorias por el extremo FIFO, spin, y aparcar. Cada 61 vueltas mira primero la cola global.

**Aparcar sin perder despertares** (protocolo de Go):
- Como mucho N/2 workers hacen spin.
- El que deja de hacer spin lo anuncia, se publica como ocioso, revisa otra vez todas las colas y duerme sobre un futex (syscall `SYS_futex` directa).
- Hacer ejecutable una fibra despierta a un worker si hay ociosos y ninguno está en spin.

**Preempción.**
- Un hilo monitor, cada 10 ms, rebaja el límite de pila (una palabra atómica publicada en el contexto) de las fibras que llevan más de 10 ms corriendo, como `stackguard0` en Go.
- Los bucles que no pasan por un prólogo (back-edges de `joinrec` y llamadas de cola a sí misma) sondean esa palabra (corrección del juez técnico).
- La VM usa un presupuesto de instrucciones.

**Orden de memoria.** Todo atómico empieza como `seq_cst` y solo se relaja con un argumento escrito y un test de tornasol.

### 8.6 Canales y select

**`Chan<T>`**, en el estilo de `hchan` de Go:
- un `std::mutex`;
- un anillo de palabras poseídas;
- colas FIFO `sendq` y `recvq` de registros `Waiter{fibra, slot, caso, Select*}` intrusivos;
- una marca de cerrado.

Si hay una pareja esperando, recibe el valor directamente y su fibra pasa a `runnext`.

**Aparcar y después soltar el cerrojo**, como `gopark` con `unlockf` en Go.
- La fibra se encola bajo el cerrojo y cambia al contexto del planificador. El planificador suelta el cerrojo solo cuando el contexto de la fibra ya está guardado.
- Eso respeta la regla de que un `std::mutex` lo libera el hilo que lo tiene, y garantiza que ningún waker despierte a una fibra a medio aparcar.
- Toda registración visible para un waker sigue esta regla: waiters, temporizadores y armado de `epoll` (corrección del juez técnico).

**`select`**, con el algoritmo de Go:
1. Evaluar los operandos en orden de fuente.
2. Sortear un orden de sondeo, y fijar un orden de bloqueo por dirección de canal sin duplicados. Bloquear todos.
3. Primera pasada: tomar el primer caso listo en orden de sondeo, o `default` si existe.
4. Segunda pasada: encolar un waiter por caso, todos compartiendo un flag atómico `done`. Aparcar y soltar.
5. Un waker tiene que ganar `done.compare_exchange_strong(0, 1)`, y se salta los waiters de un `select` que otro ya ganó.
6. El selector despertado vuelve a bloquear en el orden de bloqueo y desencola sus demás waiters.

Los timeouts son canales normales (`after`).

### 8.7 E/S y temporizadores

**El poller.** Un hilo poller es dueño de la instancia de `epoll`, de un `eventfd` y de un montículo 4-ario de temporizadores `{deadline CLOCK_MONOTONIC, seq, destino}`.
- El timeout de `epoll_wait` es el deadline más próximo.
- Añadir un temporizador anterior al primero, o apagar el runtime, escribe en el `eventfd` para despertarlo.

**Sockets.** Son no bloqueantes.
- Primero se intenta la syscall.
- Con `EAGAIN`, la fibra aparca, y el interés `EPOLLONESHOT` se arma desde el callback posterior al cambio de contexto.
- Cuando el poller la despierta, la fibra vuelve por la cola global y reintenta.

**Lo que `epoll` no puede vigilar.** Los ficheros regulares, stdin y `getaddrinfo` van a un pool de 4 hilos bloqueantes.

**Salida estándar.** stdout y stderr pasan por un único escritor, `ki_write`, que comparten todos los backends. Tiene un buffer de 64 KiB y se vacía con cada salto de línea si es una terminal, al salir y en un panic.

**Descriptores.** Los fds son `UniqueFd` RAII dentro de objetos Resource. `close` existe y se recomienda; cerrar al soltar es una red de seguridad, no semántica observable.

### 8.8 Modo determinista

`--sched=det --seed=N` funciona en los tres backends: un worker, sin poller y sin preempción.

**Procedimiento de decisión.** Forma parte de la especificación (`docs/semantics.md`):
- Los ids de fibra son secuenciales, con `main = 0`.
- `spawn` nunca cambia de fibra.
- Los despertares son FIFO.
- Cuando la fibra actual se bloquea, cede o termina, la siguiente es `runnable[rng() % n]`, con el conjunto de ejecutables ordenado por id.
- Un `select` con k casos listos toma el caso `rng() % k`, contando en orden de fuente.
- `rng` es splitmix64, de una única cabecera compartida.

**Tiempo y E/S.**
- El reloj es virtual: cuando no hay nada ejecutable, salta al temporizador más próximo, y los empates se rompen por orden de registro.
- La E/S de ficheros es síncrona; la de red es un panic.

**Herramientas.**
- `--sched-trace` imprime cada decisión.
- `ki test --seeds=1..1000` convierte el modo en un fuzzer de planificaciones, y una semilla que falla se reproduce idéntica en los tres backends.

**Dos implementaciones independientes** **[mi decisión, injerto de «riesgos primero» y corrección del juez técnico]**:
- la del intérprete, de unas 300–400 líneas;
- `DetScheduler` en el runtime, detrás de la misma interfaz que el planificador con work stealing.

Que la salida y la traza sean idénticas para la misma semilla es una obligación de los tests.

### 8.9 Política de ciclos

El heap es acíclico por construcción, salvo a través de los canales. Esta es la condición que hace válido usar RC sin recolector de ciclos: sin referencias mutables compartidas y sin definiciones recursivas de valores.

**Prohibido:**
- las definiciones recursivas de valores: `let` no es recursivo, y un ciclo entre constantes de nivel superior es un error;
- capturar un `var` en un closure;
- que una continuación sea alcanzable desde otra fibra (panic).

**Se resuelve con lambda lifting.** Los grupos de `fn` locales recursivas se vuelven funciones de nivel superior que reciben sus capturas como parámetros, antes de Perceus, así que ningún closure se apunta a sí mismo. Los métodos de instancia recursivos llaman a la función de la instancia. Los bucles `joinrec` no son closures.

**Thunks.** Su cuerpo es puro y no puede nombrarse a sí mismo, así que su resultado solo contiene valores más antiguos o nuevos y memorizarlo no cierra ningún ciclo (inducción sobre el momento de creación).

**Continuaciones.** Los marcos capturados antes de existir `k` no pueden referirse a `k`.

**Lo que puede fugarse:**
- los ciclos a través del buffer de un canal, como un canal dentro de un mensaje guardado en su propio buffer;
- las fibras bloqueadas para siempre en canales que nadie más alcanza, como las goroutines en Go.

**Detección en depuración:**
- el censo de salida por sitio de asignación;
- una pasada de *trial deletion* con raíces solo en los canales vivos, que nombra el ciclo y su camino;
- `KI_DEBUG_CHAN_CYCLES=1`, que comprueba en cada `send` si el valor alcanza el canal de destino;
- el detector de deadlock;
- el intérprete en modo RC, que detecta fugas en Core+RC.

---

## 9. Los tres backends

### 9.1 El intérprete (el oráculo)

`--backend=interp` es una máquina CEK sobre Core en cualquier etapa, de S0 a S4.

**Valores y pila propios.**
- Tiene su propia representación de valores, en una tabla de objetos cuyos ids nunca se reutilizan.
- Su pila de continuación es un dato (`std::vector<Frame>`). La recursión profunda vive en el heap, las llamadas de cola salen gratis y los handlers son literales: `perform` mueve el trozo de marcos a una `Cont` y `resume` lo devuelve.

**Concurrencia propia.** Sus fibras son estados CEK que mueven su propio planificador determinista y su propio modelo de canales, escritos desde la especificación. No tiene modo multinúcleo.

**Lo único que comparte con libkirt:** la cabecera de splitmix64 y la primitiva de formato.

**Modos:**
- `--level=core` (el oráculo);
- `--stage=S0…S4`;
- `--level=rc`: ejecuta S4 sobre un heap comprobado (contadores explícitos, lápidas envenenadas, procedencia de cada asignación y cada drop) y exige un censo de cero al salir.

**Velocidad.** Es de 10 a 50 veces más lento que la VM, y no importa: queda en `ki` para siempre.

### 9.2 La máquina virtual

`--backend=vm` corre dentro del proceso sobre el runtime real: objetos, allocator, RC, planificador, canales y E/S.

**Formato.**
- Instrucciones fijas de 64 bits, `{u16 op, u16 a, u16 b, u16 c}`, con palabras de extensión para listas de argumentos y tablas de saltos.
- Una ventana de registros por marco.
- Un pool de constantes.
- Un mapa de slots poseídos, con su clase, en cada sitio de llamada.

**Opcodes** (unos 70):
- aritmética de `Int` comprobada y operaciones de `Float`;
- switch por tag y carga de campos;
- `ALLOC` y `REUSE`, `DUP` y `DROP`, `BOX` y `UNBOX`;
- llamadas de aridad exacta, llamadas de cola por reutilización de marco y llamadas a closures;
- `PERFORM`, `HANDLE` y `RESUME`;
- llamadas al runtime.

**Herramientas.** Un verificador comprueba el bytecode antes de ejecutarlo, y un desensamblador sirve a `ki emit bytecode`.

**Dispatch.** Empieza con un `switch` portable. El goto computado y `[[gnu::musttail]]` / `[[clang::musttail]]`, que son extensiones de fabricante, van detrás de una macro con el `switch` como respaldo, y se miden en el capítulo 58.

### 9.3 Código nativo x86-64

**Pipeline.** MIR → LIR por maximal munch → liveness → linear scan → marcos → texto GNU as (`.intel_syntax noprefix`, un `.s` por módulo) → `as` → `ld`.

**Enlace.**
- `ld` enlaza contra `libkirt.a`, libstdc++, libgcc y libc.
- La línea de enlace se captura al configurar con `g++ -no-pie -###`, y `ki doctor` la revisa.
- La CI vigila `nm -u libkirt.a` para que no aparezcan dependencias inesperadas. Esto resuelve, por ejemplo, el `__popcountdi2` que GCC emite sin `-mpopcnt`.

**KiCC, la convención entre funciones Ki** **[mi decisión, con las correcciones del juez técnico]**:
- Los argumentos 1 a 6 van en `rdi`, `rsi`, `rdx`, `rcx`, `r8` y `r9`, como en System V, así que llamar al runtime no requiere barajar registros.
- El closure llamado va en `r10`.
- Los argumentos 7 en adelante van a un área de desbordamiento del contexto, y la función llamada los copia a su marco antes del chequeo de pila y preempción, dentro de un margen garantizado.
- El resultado vuelve en `rax`.
- `rbp` es siempre el puntero de marco, y `r15` está fijado al contexto del worker; como en System V es callee-saved, el C++ del runtime lo preserva.
- Todos los demás registros son caller-saved a través de llamadas Ki y de llamadas suspensivas al runtime. En las llamadas hoja al runtime rige System V, y `rbx` y `r12`–`r14` sobreviven.
- Los floats viajan en registros generales.

**Llamadas al runtime.**
- Son funciones `extern "C" noexcept` System V que reciben el contexto primero.
- Las que pueden cambiar de fibra devuelven `{valor, ctx}`.
- Las entradas en ensamblador son `ki_handle`, `ki_perform`, `ki_resume`, `ki_switch` y los trampolines de entrada de fibra.

**Frametable.** Cada sitio de llamada suspensiva tiene una entrada en la sección `ki_frametable`:
- el desplazamiento relativo de 32 bits de su dirección de retorno, sin relocaciones dinámicas, así que el PIE queda posible;
- el tamaño del marco;
- los offsets de los slots poseídos, con su clase;
- la posición en el fuente.

Se localizan al arrancar con `__start_ki_frametable` y `__stop_ki_frametable`. Sirven para soltar continuaciones, para los informes de deadlock y para los backtraces. `--verify-frames`, en depuración, recorre la pila en cada `perform`.

**Asignación de registros: una escalera de corrección:**
1. un asignador que lo vuelca todo a memoria, correcto por construcción y conservado para siempre (`--regalloc=spill-all`);
2. linear scan de Poletto–Sarkar con partición en las llamadas;
3. en el capítulo 57, Wimmer–Franz, con huecos de vida y partición de intervalos.

Un verificador reejecuta abstractamente el código asignado en las builds de depuración.

**Generación de código.**
- La aritmética de `Int` usa el camino rápido en línea del apartado 8.1 (dos inmediatos, y `jo` hacia la llamada lenta), e `idiv` comprueba antes el cero y `MIN/-1`.
- Las llamadas de cola están garantizadas: movimiento paralelo, `leave` y `jmp`.
- El marco es `push rbp; mov rbp, rsp; sub rsp, N`, sin red zone.
- Las directivas `.loc` y CFI dejan a gdb y perf mostrar líneas de Ki.
- La salida es no-PIE, pero el código direccionado relativo a RIP deja el PIE posible más adelante.

---

## 10. Las cinco partes difíciles

**(a) Efectos one-shot idénticos en los tres backends, con Perceus.**
- **Postura:** hay una sola semántica en Core y una sola regla de propiedad. Una `Cont` posee todo lo que la computación suspendida retiene; `resume` la consume; soltar una `Cont` viva libera sus marcos sin ejecutar código Ki.
  - El intérprete mueve el trozo de marcos.
  - La VM, una cadena de segmentos de registros.
  - El nativo, una cadena de segmentos de pila de máquina, como OCaml 5.
  - En los tres, los mapas de suspensión llevan la clase de cada slot.
  - Nunca se infiere un préstamo a través de un prompt.
- **Coste:** sin multi-shot, y un segmento por handler activo con cláusulas de control.
- **Riesgo y mitigación:** un mapa erróneo produce una fuga o un doble free al soltar una continuación. Lo mitigan el intérprete en modo RC, el censo, `--verify-frames` y kigen abandonando continuaciones en cada `perform`.

**(b) HM, typeclasses, HKT y filas de efectos sin zoo.**
- **Postura:**
  - tipos de rango 1;
  - traits de un parámetro con instancias de Haskell 98, sin solapamiento ni huérfanos;
  - kinds declarados con huecos, solo al final;
  - filas de Leijen;
  - los traits recorren tipos, nunca filas, y las filas no llevan restricciones.
  - Cada pieza es decidible por separado.
  - Errores legibles gracias a la procedencia, a `?err` y a los fix-its.
- **Coste:** sin estilo mtl, sin tipos asociados, y más anotaciones que en Haskell (aproximadamente las de Rust, TS o Go).
- **Riesgo y mitigación:** mensajes confusos cuando un efecto cruza un callback. Se mitiga con plantillas de mensaje dedicadas y un golden por código.

**(c) Fibras, canales, `select` y work stealing en C++, con modo determinista.**
- **Postura:** el planificador no es un handler de Ki: `IO` y `Async` son primitivos. La política vive en `runtime/sched`, detrás de una interfaz con dos implementaciones.
  - Canales al estilo Go, con aparcar-y-soltar.
  - Chase–Lev con índices con signo.
  - Sin `thread_local`.
  - El modo determinista se implementa dos veces, de forma independiente.
- **Coste:** sin planificadores escritos en Ki para `Async` en v0.
- **Riesgo y mitigación:** despertares perdidos y carreras en `select`. Los mitigan los barridos de semillas, TSan con anotaciones de fibra, el estrés con 16 workers y los tests de tornasol.

**(d) El backend x86-64.**
- **Postura:** todo hasta el texto ensamblador.
  - Maximal munch.
  - Linear scan con verificador y la escalera de corrección.
  - Todos los registros caller-saved a través de las llamadas suspensivas, así que los mapas de pila son triviales.
  - Puntero de marco siempre.
  - Llamadas de cola garantizadas.
  - Línea de enlace descubierta al configurar.
- **Coste:** más volcados a memoria alrededor de las llamadas (OCaml hace el mismo trato).
- **Riesgo y mitigación:** fallos de asignación de registros que revientan lejos de su causa. Los mitigan el verificador, el asignador spill-all, el reductor y la matriz a tres bandas.

**(e) Ciclos con RC.** La postura es la del apartado 8.9: un heap acíclico por construcción, salvo los canales, que se detectan en depuración. El coste es que no hay datos cíclicos: se usan índices o mapas. El riesgo es que una característica futura reintroduzca ciclos; la regla es que cualquier característica que añada mutación debe actualizar `docs/v0.md#cycles` y añadir tests de censo.

---

## 11. Verificación y repositorio

### 11.1 Estructura del repositorio

Hay dos repositorios nuevos, los dos públicos en GitHub con licencia MIT y los dos en ext4 bajo `/home` (lo decidiste tú). Ninguno lee ni reutiliza `~/ki`.
- **El tuyo: `~/kilang` → `aldezex/kilang`.** Lo creas tú en el capítulo 1, y empieza vacío. Escribes en él todo el código y todos los tests a partir del libro, y su CI la montas tú en ese mismo capítulo. Es tu portfolio.
- **El de referencia: `~/kilang-ref` → `aldezex/kilang-ref`.** Lo construyo yo antes de publicar el libro. De él salen los listados, y lleva las etiquetas, `book/` y `bookx`. Tú solo lo usas para compararte o para recuperarte de un atasco.

Los dos nombres estaban libres en GitHub y en local el 2026-10-23.

Esta es la estructura del de referencia. La del tuyo es la misma, sin `book/` ni `tools/bookx/`.

```text
kilang/
  CMakeLists.txt  CMakePresets.json
  cmake/          Warnings.cmake  Sanitizers.cmake  ToolchainPaths.cmake (link-line capture)
  docs/           semantics.md (Core rules + det-scheduling spec)  abi.md  bytecode.md  v0.md (feature freeze, cycles)
                  errors/E*.md   research/ (the dossier)   superpowers/specs/ (this document)
  compiler/       base/ diag/ syntax/ sema/ core/ interp/ mir/ vm/ x64/ driver/     one static library each + `ki`
  runtime/        include/kirt/  src/{object,alloc,rc,census,str,array,lazy,cont,fiber,segment,
                  sched_ws,sched_det,chan,select,poll,timer,io,panic}/  arch/x86_64/{switch.S,entry.S}  → libkirt.a
  stdlib/         prelude.ki  std/*.ki
  tests/          unit/<stage>/*_test.cpp  golden/chNN/*.ki  errors/chNN/*.ki  diff/corpus/*.ki
                  stress/  litmus/  bench/
  tools/          ki_test/ (test framework)  kigen/ (generator + reducer)  bookx/ (listing extractor + verifier)
  book/           chNN-slug.md (Spanish prose with listing references)  tests/chNN.tests (test cards)
                  verified-tags.txt
  ci/             ci-local.sh  tsan-env.sh  check-tags.sh  fuzz-nightly.sh
  .github/workflows/  ci.yml  tsan.yml  nightly.yml
```

**Herramientas de test.**
- `ki_test` es un framework de una sola cabecera: `KI_TEST("lexer.pipe_continues_line") { KI_CHECK_EQ(…); }`. Usa `std::source_location`, tiene `--filter` y `--list`, y se registra en CTest.
- Los tests golden en Ki llevan una cabecera que se lee por máquina (en inglés, porque es un comentario):

```ki
// test: T21.07
// backends: interp interp-rc vm native
// seeds: 1..20     schedule: independent
// expect-exit: 0
// expect-stdout:
// | error: boom
// | 0
```

- Los tests de diagnóstico marcan el error esperado en línea: `// ERROR(E0401) @3:17`.

### 11.2 Etiquetas, sesiones y extracción de listados

**Historia y etiquetas.**
- La historia de `main` es lineal.
- Cada capítulo termina en una etiqueta anotada `chNN`.
- Cada sesión de 2–3 h dentro de un capítulo termina en verde con un commit y una etiqueta de punto de control `chNN-sK`.
- Cada hito tiene su etiqueta `mN`.
- Una etiqueta solo vale cuando la CI está en verde sobre ella, y entonces se anota en `book/verified-tags.txt`.

**Marcadores en el código fuente:**
- `// @book <region> full|hint "text"|solution|test` … `// @end` delimita una región;
- las líneas `@elide` se muestran como `// ...`;
- los nombres de región son estables entre capítulos.

**Referencias desde el libro:**
- `{{listing chNN region}}`;
- `{{diff chA..chB region-or-path}}`;
- `{{solution …}}` y `{{reftest …}}`, que se muestran plegados.

**`bookx`**, en C++ y dentro del repo:
- Lee cada fichero con `git show <tag>:<ruta>`, sin worktrees ni clones.
- Para construir y probar una etiqueta en la CI, usa `git archive <tag> | tar -x -C build/tags/<tag>` y borra ese directorio al acabar.
- Falla si una región falta, está duplicada o desequilibrada; si la etiqueta no está verificada; si un caso de test no tiene test de referencia, o al revés; o si un hash no coincide con el libro publicado.

**Cómo te comparas con la referencia.** Traes sus etiquetas a tu repo con un prefijo, para que no choquen con las tuyas:

```text
git remote add --no-tags ref https://github.com/aldezex/kilang-ref.git
git fetch ref 'refs/tags/*:refs/tags/ref/*'
git diff ref/ch05 -- compiler/syntax/
```

El repo de referencia contiene todas las soluciones. Si no quieres verlas antes de tiempo, no lo mires; el libro ya las pliega.

**Correcciones** **[mi decisión]**.
- Antes de publicar: si un capítulo posterior destapa un fallo de código anterior, lo corrijo en su sitio con commits `--fixup` y un `rebase --autosquash --update-refs` no interactivo, muevo las etiquetas y la CI vuelve a verificarlas todas.
- Si el fallo es didáctico, lo dejo como un recuadro «Bug encontrado» en el capítulo que lo descubre.
- Después de publicar, la historia se congela y las correcciones van hacia delante, con fe de erratas.

### 11.3 Formato de los casos de test

Cada capítulo tiene `book/tests/chNN.tests`, y el libro lo muestra como la tabla «Tests primero». Las claves están en inglés porque es un formato de fichero; el texto, en español porque es prosa del libro.

```text
[T05.07] lexer.pipe_continues_line                         level: core
kind:    unit (ki_test)      file: tests/unit/syntax/lexer_test.cpp
case:    una línea que empieza por `|>` continúa la expresión de la línea anterior
input:   "let total = xs\n  |> sum\n"
expect:  ningún Newline entre `xs` y `|>`; después, Newline y Eof
why:     tus tuberías ponen `|>` al principio de la línea; si el lexer insertara aquí un Newline,
         la sentencia terminaría en `xs` y el error saldría una línea más abajo, lejos de su causa
ref:     KI_TEST("lexer.pipe_continues_line")                  ← folded in the book
```

**Tipos (`kind`):**

| Tipo | Qué es |
|---|---|
| `unit` | test unitario |
| `golden` | volcados de IR |
| `diag` | instantánea del diagnóstico más la anotación en línea |
| `run` | salida y código de salida |
| `diff` | todos los backends |
| `det` | rangos de semillas |
| `stress` | estrés |
| `prop` | generador más propiedad |
| `bench` | rendimiento; nunca bloquea la CI |

**Nivel (`level`):**
- `core`: debe pasar antes de seguir;
- `stretch`: opcional si vas justo de tiempo.

**Cómo se usa.** Escribes cada test a partir de `case`, `why` y `expect` antes de implementar, lo ves fallar, implementas, y solo entonces despliegas la referencia para comparar.

### 11.4 CI y toolchain

**Toolchain.**
- GCC 15.2 como compilador principal (no la instantánea de gcc-16) y Clang 21 como segundo. Clang se instala en el capítulo 1 con `sudo apt install clang-21`.
- `-std=c++23`, cabeceras y nada de C++26.
- Los presets de CMake son la única fuente de flags.

**Flags:**
- en todas las builds: `-Wall -Wextra -Wpedantic -Wshadow -Werror -ffp-contract=off -fno-omit-frame-pointer`;
- `-Wconversion`, desde el capítulo 35, cuando ya se han enseñado las conversiones;
- en depuración: ASan, UBSan y `-D_GLIBCXX_ASSERTIONS`;
- en el runtime: `-fno-exceptions -fno-rtti`.

**Matriz por cada push:**
- {gcc-15, clang-21} × {debug con sanitizers, release};
- unit, golden, diag y run en cada backend que exista en esa etiqueta;
- censo de fugas cero;
- semillas 1..200;
- estrés ×50;
- verificación de listados.

**TSan.** El job corre en el host después de `sysctl vm.mmap_rnd_bits=28`, porque un contenedor no puede cambiarlo. Usa la variante del deque con `seq_cst` y las anotaciones de fibra.

**Nightly:**
- kigen de 30 a 60 minutos × 3 backends × 2 compiladores;
- semillas 1..100000;
- todas las etiquetas de capítulo reconstruidas y probadas;
- `bookx verify`;
- benchmarks a un CSV, sin bloquear nunca (los runners compartidos dan resultados inestables).

**Dónde corre.** En GitHub Actions, en un contenedor `ubuntu:26.04` idéntico a WSL. Como los repos son públicos, los runners estándar son gratis y sin límite de minutos. Eso incluye los ARM64 (`ubuntu-24.04-arm`, 4 vCPU) para el capítulo 64 y los de Windows para los capítulos 66–68. `ci/ci-local.sh` (`cmake --workflow --preset ci-local`) reproduce la misma matriz en local.

**Cómo se verifica el repo de referencia** **[mi decisión]**. Son unas 300 etiquetas de sesión, así que no todo corre en todas:
- **Cada etiqueta de sesión y de capítulo** compila y pasa sus propios tests. Se verifican en orden, en una sola carpeta de build incremental, en esta máquina.
- **Solo en las etiquetas de hito** corren kigen durante horas, los barridos de semillas grandes y TSan.
- **Tras una corrección**, solo se reverifican las etiquetas posteriores al commit corregido.
- **La CI nocturna es la de tu repo.** Como es público, los minutos son ilimitados.

---

## 12. El libro

### 12.1 Formato

**Título** **[mi decisión]**: *Ki: de cero a experto. Construye tu propio lenguaje funcional en C++*.

**Publicación.**
- Es un único artifact web de varios ficheros, entregado entero.
- Tiene un índice lateral, es solo para escritorio y lleva diagramas SVG en línea.
- Prosa en español; código, identificadores, comentarios y mensajes en inglés.
- Se añade a la «Wiki de artefactos».

**Progreso guardado.** Usa la base de datos persistente del artifact, no `localStorage`, que podría borrarse en dos años **[mi decisión]**. Por capítulo guarda:
- si lo has leído;
- qué sesiones has hecho;
- qué tests has escrito;
- qué ejercicios has resuelto;
- las horas reales.

Con esas horas se recalibra el plan.

**Resaltado de código.** Un resaltador de Ki escrito para el libro, más una biblioteca de resaltado de CDN para C++, CMake y ensamblador. La regla de «todo a mano» es para Ki, no para la página web del libro **[mi decisión]**.

### 12.2 Plantilla de capítulo

1. **Objetivo y hito:** el comando que podrás ejecutar al terminar.
2. **Teoría**, con recuadros «Si vienes de Rust / Go / TS / Python».
3. **Tests primero:** las tarjetas de casos (caso, por qué, esperado, nivel). Tú escribes los tests y la referencia está plegada.
4. **Implementación por sesiones de 2–3 h:** listados completos para los conceptos nuevos y pista con solución plegada para lo conocido. Cada sesión termina en verde con un commit, y puedes compararte con la etiqueta `chNN-sK`.
   - Como empiezas de cero en compiladores, un concepto va siempre completo la primera vez que aparece **[mi decisión, a partir de tu respuesta]**. La pista con solución plegada queda para cuando se repite un patrón que el libro ya enseñó (un nodo más en el parser, otro opcode, otro pase sobre la IR) y para lo que ya dominas por Rust, Go, TS o Python.
   - Las interfaces se dan siempre completas, aunque la implementación vaya con pista: cabeceras, estructuras de IR y APIs públicas **[mi decisión]**. Así, tu código encaja con los listados de capítulos posteriores aunque lo hayas escrito a tu manera.
   - Si te atascas o tu versión se desvía demasiado, copias el fichero de la etiqueta de referencia y sigues.
5. **Ejecútalo:** la salida que tienes que ver.
6. **Ejercicios**, con soluciones plegadas.
7. **Preguntas de repaso**, con respuestas plegadas.
8. **Resumen de teoría.**
9. **«C++ en el trabajo»:** cómo se usa en la industria lo que acabas de aprender (CMake, testing, sanitizers, perf, concurrencia, ABI, revisión de código). Es el recurso para tu desempate, la empleabilidad. Ningún capítulo dice «nada nuevo de C++».

### 12.3 Presupuesto de horas y calendario

Las horas son **mi estimación para ti, no datos medidos**.

- **Tronco:** unas **836 h** en 68 capítulos y 11 partes.
- **Reserva:** un 15 % para depurar, unas 125 h, hasta **≈ 961 h**, dentro de tu margen de 500–1000 h.
- **Topes:** ningún capítulo pasa de 16 h y ningún tramo entre hitos pasa de unas 100 h.
- **Re-planificación:** al final de cada parte, comparando con las horas reales.

| Ritmo | Primer programa (M1, 95 h) | Libro entero (836 h) | Con reserva (961 h) |
|---|---|---|---|
| 5 h/semana | ≈ 4,4 meses | ≈ 3,2 años | ≈ 3,7 años |
| 7,5 h/semana | ≈ 2,9 meses | ≈ 2,1 años | ≈ 2,5 años |
| 10 h/semana | ≈ 2,2 meses | ≈ 1,6 años | ≈ 1,8 años |

Aviso honesto: a 5 h por semana, el primer programa llega hacia el cuarto mes y medio, no en 2–3 meses.

**Orden de recorte**, si las horas reales se disparan:
1. el capítulo 57 (linear scan II) pasa al epílogo;
2. el servidor HTTP del capítulo 61;
3. el capítulo 56 (representaciones sin caja). `Int` y `UInt64` seguirían teniendo 64 bits; solo perderían velocidad.

**Nunca se recortan:** las herramientas de test, los errores legibles ni los capítulos finales de Windows y ARM64.

### 12.4 Hitos

| Hito | Capítulo, horas acumuladas | Qué ves |
|---|---|---|
| **M1** | cap. 10, ≈ 95 h | `ki run fib.ki` imprime `6765` por el pipeline definitivo. Los diagnósticos con caret funcionan y un desbordamiento da un panic con salida 101 |
| **M2** | cap. 12, ≈ 121 h | Tu programa `Shape`/`area`/`match`, con errores golden: un brazo que falta (con testigo), brazos de tipos distintos, un nombre desconocido y un argumento mal tipado |
| **M3** | cap. 16, ≈ 173 h | Tu ejemplo original exacto: `[Circle(1.0), Rect(2.0, 3.0)] \|> map(area) \|> sum \|> print` → `9.141592653589793` |
| **M4** | cap. 24, ≈ 263 h | La parte de efectos del ejemplo ampliado, más streams perezosos |
| **M5** | cap. 26, ≈ 289 h | El ejemplo ampliado completo en el oráculo, con los mismos bytes para cada semilla |
| **M6** | cap. 33, ≈ 365 h | Programas aleatorios bien tipados coinciden en todas las etapas S0–S4 |
| **M7** | cap. 40, ≈ 455 h | Toda la suite secuencial corre en la VM, idéntica al oráculo y sin fugas |
| **M8** | cap. 45, ≈ 525 h | El ejemplo ampliado y un servidor echo TCP en la VM con 16 workers. El modo determinista del runtime coincide con el del intérprete en cada semilla |
| **M9** | cap. 49, ≈ 577 h | Los primeros binarios nativos (puros y secuenciales) |
| **M10** | cap. 54, ≈ 648 h | Intérprete, VM y nativo coinciden en la suite y en el fuzzer, y el servidor echo corre en nativo con 16 workers |
| **M11** | cap. 58, ≈ 702 h | Mejoras de velocidad medidas, con la salida todavía idéntica |
| **M12** | cap. 62, ≈ 754 h | Ki v0.1: una CLI paralela, un servidor HTTP, `ki fmt` y `ki explain` |
| **M13** | cap. 65, ≈ 794 h | La suite completa pasa en AArch64 |
| **M14** | cap. 68, ≈ 836 h | Un `.exe` de Ki corre en Windows |

**El hito 1 en detalle.** Cada etapa es ya la definitiva, solo que con menos clases de nodo:

```ki
fn fib(n: Int) -> Int {
  if n < 2 { n } else { fib(n - 1) + fib(n - 2) }
}

fn main() {
  let x = fib(20)
  print(x)
}
```

- **Driver.** `run`, `check`, `emit tokens|ast|core`.
- **Lexer.** Palabras clave, identificadores internados, enteros con comprobación de rango, operadores, la regla de línea y tokens de error.
- **Parser.** Funciones con parámetros tipados y retorno, bloques, `let`, `if`/`else` como expresión, llamadas, Pratt y recuperación.
- **Resolución.** Dos pasadas (primero las funciones, luego los cuerpos, así que la recursión y las llamadas hacia delante funcionan), sombreado, «¿quisiste decir `fib`?» y `main` ausente.
- **Tipos.** Una TypeTable con `Int`, `Bool`, `()` y funciones. Las firmas salen de las anotaciones y la comprobación pasa por `unify` con razones. La interfaz ya es la final: las variables de tipo y las filas existen en las estructuras, aunque todavía no se usan.
- **Core S0 y Lint.** El `if` pasa a ser un `match` sobre `Bool` y los operadores, primitivas comprobadas.
- **Intérprete CEK.** Pila de marcos explícita, llamadas de cola sin marco y enteros de 64 bits comprobados con `__builtin_*_overflow`.
- **`print` es la primera forma de un mecanismo permanente.** El preludio mínimo la declara como `#[extern("ki_print_int")] pub fn print(x: Int)`. Las primitivas `extern` del preludio no se tiran nunca; en el capítulo 14, el propio preludio cambia `print` a `pub fn print<T: Show>(x: T) uses {IO}`, que llama a la primitiva `ki_write`. En M2, el preludio añade `printFloat` del mismo modo, y desaparece cuando llega `print<T: Show>`.
- **Errores.** `ki check` muestra `E0301` (nombre desconocido `fibb`, «did you mean `fib`?»), `E0401` (ramas del `if` `Int` frente a `Bool`, con las dos etiquetas) y varios `E02xx` en una sola pasada.
- **Panic.** `print(9223372036854775807 + 1)` sale con `panic: integer overflow at overflow.ki:2:29` y código 101.
- **Alrededor.** `ki_test`, el runner de golden, los presets de CMake y la CI con GCC y Clang, ASan y UBSan.

### 12.5 Índice

Cada fila lleva el hito del capítulo, el C++ nuevo que enseña, los conceptos de compiladores y las horas estimadas.

#### Parte I. Fundamentos y primer programa (≈ 95 h → M1)

| # | Capítulo | Hito | C++ nuevo | Compiladores | h |
|---|---|---|---|---|---|
| 1 | El taller: compilar, enlazar, CMake y tu propio framework de tests | tu repo `aldezex/kilang` creado; `ki --version`; `ki_test` se prueba a sí mismo, en verde con ASan y UBSan y en la CI de GitHub | instalar el toolchain (GCC 15, clang-21); tipos, funciones y control de flujo comparados con Rust, Go, TS y Python; unidades de traducción, cabeceras, declaración y definición, ODR básica; namespaces; `std::print`; targets y presets de CMake; warnings; ASan y UBSan; macros con `std::source_location`; registro estático | el mapa del pipeline; TDD rojo-verde; tests golden | 12 |
| 2 | Objetos, vida y propiedad en C++ | SourceMap guarda los ficheros en memoria; la búsqueda de línea y columna está testeada | semántica de valor, referencias, `const`, clases con constructores y destructores, RAII, move, regla del cero, `std::string` y `string_view`, `std::vector`, `unique_ptr`, `std::span`; punteros frente a referencias; UB; referencias colgantes reproducidas con ASan (frente al borrow checker de Rust y el GC de Go) | ficheros fuente, spans, tablas de líneas | 12 |
| 3 | Tipos suma y errores como datos | `ki check` muestra un diagnóstico con caret (golden) | `enum class`, structs e inicializadores designados, `std::optional`, `std::expected` (frente a `Result`), `std::variant`, `std::visit` y el idiom `overloaded`, `std::format` y especialización de `formatter`, `[[nodiscard]]` | modelo de diagnósticos, códigos de error, renderizado | 8 |
| 4 | Plantillas y la caja de herramientas del compilador | `Id<Tag>`, `IdVec`, `Arena` e `Interner`, testeados | plantillas de función y de clase, `static_assert` y type traits, introducción a concepts, alineación y placement new, `std::hash` y búsqueda heterogénea, `operator<=>` | almacenamiento orientado a datos, ids tipados, interning | 8 |
| 5 | El lexer y la regla de línea | `ki emit tokens` | `char` frente a `unsigned char`, decodificar UTF-8, `switch`, tablas `constexpr`, X-macros, `std::from_chars` | maximal munch, inserción de separadores, modos del lexer, recuperación | 9 |
| 6 | El parser: descenso recursivo, Pratt y recuperación | `ki emit ast` (S-expresiones) | recursión, por qué ids y no punteros, lambdas y capturas (lo básico), `std::unreachable`, imprimir con `std::visit` | precedencias, búsqueda acotada, nodos Error, sincronización | 12 |
| 7 | Nombres y ámbitos | `unknown name 'fibb'; did you mean 'fib'?` | `std::unordered_map`, guardas de ámbito RAII, ranges y views, invalidación de iteradores, programación dinámica sobre vectores (distancia de edición) | tablas de símbolos, DefIds, tablas laterales | 7 |
| 8 | Tipos I: comprobar contra firmas, con errores legibles | errores de tipo con dos etiquetas y su origen | union-find sobre vectores, hash-consing, tipos valor, corrección de `const` | unificación, comprobación bidireccional, procedencia | 10 |
| 9 | Core: la referencia semántica | `ki emit core`; Core Lint en verde | builders, *deducing this* (C++23), `std::generator` para recorridos (C++23), aserciones y `std::stacktrace` | diseño de Core, elaboración, invariantes | 7 |
| 10 | El intérprete CEK | **M1** | uniones etiquetadas escritas a mano, `<cstdint>`, operaciones de bits, `std::bit_cast`, el UB del desbordamiento con signo y `__builtin_*_overflow`, pilas explícitas | máquina CEK, llamadas de cola, primitivas, panics | 10 |

#### Parte II. El núcleo funcional en el oráculo (≈ 110 h → M2, M3)

| # | Capítulo | Hito | C++ nuevo | Compiladores | h |
|---|---|---|---|---|---|
| 11 | Datos: tipos algebraicos, records y tuplas | valores `Shape`, records con update, `Float` impreso con `.0` | layout y padding de structs, `offsetof`, almacenamiento plano frente a un vector de variants, `std::to_chars`, `-ffp-contract` | tipos nominales, constructores, records | 12 |
| 12 | match y exhaustividad | **M2** | recursión sobre matrices de patrones, `std::bitset`, subrangos de `std::span`, `ranges::to` | utilidad de Maranget, testigos, guardas; matching ingenuo en el oráculo | 14 |
| 13 | Polimorfismo: genéricos, generalización y niveles | funciones genéricas, funciones privadas inferidas, error legible de «local no generalizado» | Tarjan iterativo; plantillas frente a polimorfismo paramétrico (monomorfización frente a diccionarios); trampas de captura en lambdas | variables de tipo, niveles, generalización por SCC | 14 |
| 14 | Traits I: coherencia y paso de diccionarios | `impl Describe for Shape`; operadores por traits; `print<T: Show>` | funciones virtuales y vtables frente a diccionarios frente a concepts de C++20 (frente a los traits de Rust y las interfaces de Go) | resolución de instancias, superclases, huérfanos, diccionarios acíclicos | 16 |
| 15 | Closures, lambdas, pipes y métodos | `xs \|> map((x) => x * 2)` y `s.describe()` | lambdas a fondo, `std::function` y borrado de tipos, punteros a función, `std::move_only_function` | variables libres, closures, azúcar de `\|>`, búsqueda de métodos por tipo | 12 |
| 16 | Listas, strings, interpolación y `UInt64` | **M3**; además, `UInt64` | `char8_t` y `std::u8string`, iterar UTF-8, small-string optimization, vida de los `string_view`, incrustar ficheros en la build (el preludio en Ki) | azúcar de listas, preludio en Ki, interpolación | 10 |
| 17 | Traits II: tipos de orden superior, derive y el preludio | Functor, Foldable y Monad; `sum` sobre cualquier Foldable; `#[derive]` | parámetros de plantilla de plantilla (el HKT de C++), plantillas variádicas, fold expressions | kinds, constructores currificados, derive | 14 |
| 18 | Módulos e imports | proyectos de varios ficheros; preludio en Ki, con cada tipo en su módulo; métodos entre módulos; ciclos de import con mensaje | cabeceras a escala, la ODR a fondo, variables `inline`, `std::filesystem`, `std::error_code`, partir el código en bibliotecas de CMake | grafo de módulos, visibilidad, orden de inicialización | 10 |
| 19 | Errores que se leen: catálogo, `ki explain` y fix-its | un snapshot por cada código de error | formatters a medida, `isatty` y colores ANSI, ancho de visualización Unicode, `std::stacktrace` para errores internos | procedencia en cadena, diseño de mensajes | 8 |

#### Parte III. Efectos, control y pereza (≈ 58 h → M4)

| # | Capítulo | Hito | C++ nuevo | Compiladores | h |
|---|---|---|---|---|---|
| 20 | Filas de efectos | `uses` comprobado; «performs Log but does not declare it», con las dos soluciones | vectores ordenados pequeños, `std::flat_map` y `flat_set` (C++23), por dentro de `SmallVec` | etiquetas con ámbito, unificación de filas, regla ambiente | 14 |
| 21 | Handlers y continuaciones one-shot | `handle`, `perform` y `resume` en el oráculo; Fail, State y Log | tipos solo-movibles, empalmes O(1) de vectores, máquinas de estados | reglas small-step, handlers profundos, soltar continuaciones | 16 |
| 22 | Generadores, estado y `?` | iteradores con Yield, `runState`, `catch`/`toResult` y `e?` | corrutinas de C++20 y `std::generator` frente a Yield (sin pila frente a con pila) | continuaciones guardadas, `resume` que no está en cola | 10 |
| 23 | Azúcar imperativo sobre un núcleo puro | `var`, `for`, `while`, `break`, `continue` y `return` | structured bindings, `views::enumerate` y `zip` (C++23) | join points, `joinrec`, intuición de SSA | 12 |
| 24 | Pereza explícita | **M4** | máquinas de estados con `std::variant`, memoización | call-by-need como biblioteca, comprobación de pureza | 6 |

#### Parte IV. Concurrencia en el oráculo (≈ 26 h → M5)

| # | Capítulo | Hito | C++ nuevo | Compiladores | h |
|---|---|---|---|---|---|
| 25 | Fibras y el planificador determinista | primero, un planificador de juguete escrito como handler de Ki (`Coop { fork, yield }`); después, `spawn` con `--sched=det --seed` y deadlocks detectados | buffers circulares, listas intrusivas, splitmix64 escrito a mano (por qué no `<random>`: sus distribuciones dependen de la implementación) | fibras como estados, puntos de planificación | 12 |
| 26 | Canales, select y tiempo virtual | **M5** | ordenación estable, `std::priority_queue`, iteración determinista | rendezvous, cierre, `select`, reloj virtual, trazas | 14 |

#### Parte V. Un middle end verificado (≈ 76 h → M6)

| # | Capítulo | Hito | C++ nuevo | Compiladores | h |
|---|---|---|---|---|---|
| 27 | ANF y comprobar cada pase | `ki test --check-passes` | reescritura de IR con worklists, algoritmos de `std::ranges` | ANF, Lint por pase, diferencial entre pases | 8 |
| 28 | Árboles de decisión | los matches compilados coinciden con el matcher ingenuo | memoización con hash a medida, claves compuestas en `unordered_map` | árboles de decisión de Maranget, compartición | 10 |
| 29 | Lambda lifting y conversión de closures | ningún closure se contiene a sí mismo | análisis de variables libres con bitsets, operaciones de conjuntos con ranges | lifting, entornos explícitos | 8 |
| 30 | Perceus I: propiedad y el heap comprobado | toda la suite, sin fugas, con `--level=rc` | flujo de datos sobre `vector<uint64_t>`, tabla de objetos con lápidas, procedencia con `source_location` | RC preciso, dup/drop, linealidad | 16 |
| 31 | Perceus II: préstamos, fronteras y reutilización | `map` y `filter` en el sitio; regla de frontera | RC intrusivo frente a `shared_ptr` y `weak_ptr` (y por qué Ki no los usa), punto fijo sobre SCCs | inferencia de préstamos, especialización de drop, FBIP | 14 |
| 32 | Un generador de programas bien tipados | kigen genera programas que compilan y terminan | generadores recursivos acotados por tamaño, estrategias con plantillas frente a `std::function` | generación dirigida por tipos | 12 |
| 33 | Encoger fallos y el corpus de regresión | **M6** | `posix_spawn`, pipes y timeouts (lanzar `ki` como subproceso) | delta debugging, triaje | 8 |

#### Parte VI. El runtime y la máquina virtual (≈ 90 h → M7)

| # | Capítulo | Hito | C++ nuevo | Compiladores | h |
|---|---|---|---|---|---|
| 34 | El modelo de memoria de C++ (antes del primer atómico) | tests de tornasol con TSan | `std::atomic`, `memory_order`, fences, happens-before, data races como UB, TSan (frente a los modelos de Rust y Go) | memoria débil, linealizabilidad | 12 |
| 35 | El modelo de objetos de libkirt | tests de la codificación de valores, `BoxedInt` incluido | `alignas`, `bit_cast`, `static_assert` sobre layouts, tipos standard-layout, `extern "C"`, strict aliasing, `-fno-exceptions`/`-fno-rtti`, bibliotecas estáticas; desde aquí, `-Wconversion` | representación uniforme, tagging, `Int` de 64 bits canónico, inmortales | 11 |
| 36 | Tu propio allocator | estrés del allocator; modos debug y system | `mmap` y `madvise`, `<bit>`, free lists intrusivas, push con CAS (Treiber), benchmarks con `std::chrono` | free lists segregadas, liberación remota | 14 |
| 37 | Conteo de referencias en el runtime | soltar una lista de 10⁷ elementos sin recursión; censo | `fetch_add` y `fetch_sub` con `acq_rel` (por qué), algoritmos sin recursión | RC local, compartido e inmortal; `mark_shared` | 10 |
| 38 | MIR: una IR de bajo nivel en SSA | MIR para toda la suite; verificador | diseño de APIs de IR, `std::span<const Operand>`, `std::to_underlying` | SSA con parámetros de bloque, layouts | 14 |
| 39 | Bytecode y el bucle de la VM | `--backend=vm` con programas secuenciales | dispatch con `switch`, `[[likely]]`, leer ensamblador de `-O2` con objdump | VM de registros, convención de llamada, caminos rápido y lento de la aritmética | 15 |
| 40 | Efectos en la VM: segmentos y continuaciones | **M7** | pools de objetos, listas intrusivas, handles RAII solo-movibles | segmentos, registros de handler, mapas de slots | 14 |

#### Parte VII. Concurrencia multinúcleo (≈ 70 h → M8)

Va antes que el nativo **[mi decisión]**. La concurrencia es tu interés principal, así llega unas 100 h antes, y sus fallos nunca se mezclan con los de generación de código. Las fibras de la VM aparcan devolviendo `Park` y no necesitan cambiar de pila nativa.

| # | Capítulo | Hito | C++ nuevo | Compiladores | h |
|---|---|---|---|---|---|
| 41 | Fibras de la VM y el planificador determinista del runtime | las trazas de la VM son idénticas a las del intérprete | interfaces con concepts frente a virtuales (polimorfismo estático y dinámico), introducción a `std::jthread` | interfaz del planificador, `Park` | 12 |
| 42 | El deque de Chase–Lev | pasa estrés de linealizabilidad y TSan | programación lock-free, bucles CAS, ABA, índices con signo, `alignas(64)`, false sharing, `hardware_destructive_interference_size` | work stealing | 14 |
| 43 | El planificador work-stealing | 16 workers; fib paralelo escala | la syscall `futex`, eventcount, ciclo de vida de los hilos, `stop_token` | spinning y parking, preempción | 14 |
| 44 | Canales y select en el runtime | el modo determinista del runtime coincide con el del intérprete en cada semilla | `std::mutex`, `lock_guard`, `scoped_lock`, la regla del dueño, orden de bloqueo por dirección, `compare_exchange_strong` frente a `weak` | hchan, aparcar y soltar, `select` en dos fases | 16 |
| 45 | E/S y temporizadores con epoll | **M8** | syscalls y `errno`, fds RAII, sockets no bloqueantes, `eventfd`, montículo 4-ario, pool de hilos | E/S por disponibilidad, temporizadores | 14 |

#### Parte VIII. Código nativo x86-64 (≈ 123 h → M9, M10)

| # | Capítulo | Hito | C++ nuevo | Compiladores | h |
|---|---|---|---|---|---|
| 46 | x86-64, System V y GNU as desde cero | un `.s` escrito a mano llama a libkirt, enlazado con `as` y `ld` | `extern "C"`, name mangling, objdump, readelf y nm, asm extendido de GCC frente a ficheros `.S`, sintaxis Intel frente a AT&T | ISA, direccionamiento, flags, convenciones de llamada | 12 |
| 47 | ELF, ld y el enlace con libkirt | `ki build` enlaza un stub; `ki doctor` | ficheros objeto, relocaciones, `ar`, visibilidad de símbolos, ficheros crt, libstdc++ estática frente a dinámica, `g++ -###` | enlazado, arranque del programa, KiCC | 10 |
| 48 | Selección de instrucciones | `fib` en nativo con todo en memoria (spill-all) | tablas de patrones `constexpr`, `std::format` para emitir texto, diseño de la LIR | tiling de árboles, maximal munch, fusión de flags | 14 |
| 49 | Liveness y linear scan, con su verificador | **M9** | BitSet escrito a mano, colas de prioridad, comparadores a medida, tests basados en propiedades | intervalos de vida, Poletto–Sarkar, verificador | 16 |
| 50 | Marcos, llamadas, tail calls y mapas de pila | todos los tests sin efectos pasan en nativo; gdb muestra líneas de Ki | tablas binarias y `std::lower_bound`, tablas de líneas DWARF (`.loc`), directivas CFI | marcos, frametable, llamadas de cola | 14 |
| 51 | Closures, ADTs y RC nativos | el corpus de la Parte II corre en nativo sin fugas | leer el ensamblador generado, caminos rápidos en línea, aserciones de alineación del ABI | caminos rápidos de RC, de asignación y de la aritmética de `Int` | 13 |
| 52 | Segmentos de pila y cambio de contexto | 1M cambios en un test de C++; `main` en un segmento | ficheros `.S` frente a asm en línea, slabs con `MAP_NORESERVE`, las APIs de fibras de los sanitizers | cadenas de segmentos, `ki_switch`, chequeo de pila | 14 |
| 53 | Efectos nativos | el corpus de efectos es idéntico en los tres backends | trampolines en ensamblador, gdb entre pilas | `ki_handle`, `ki_perform` y `ki_resume`; desenrollar con la frametable | 16 |
| 54 | Fibras nativas en 16 núcleos y prueba a tres bandas | **M10** | informes de fallo con `sigaltstack`, seguridad async-signal, gdb con hilos | riesgos de la migración, matriz a tres bandas | 14 |

#### Parte IX. Rendimiento (≈ 54 h → M11)

| # | Capítulo | Hito | C++ nuevo | Compiladores | h |
|---|---|---|---|---|---|
| 55 | El optimizador de Core | inlining, especialización, case-of-known-constructor y el camino rápido tail-resumptive, con la suite todavía idéntica | worklists, memoización con hashes a medida | simplificador, modelos de coste | 16 |
| 56 | Representaciones sin caja | nbody varias veces más rápido (medido); `Int`, `UInt64` y `Float` crudos en el código monomórfico | layout de `double`, NaN-boxing como comparación, conciencia de SIMD | elección de representación, worker/wrapper | 12 |
| 57 | Linear scan II: partir intervalos | menos volcados en la suite de benchmarks | estructuras de intervalos, `std::set` con comparadores | huecos de vida, resolución de movimientos (Wimmer–Franz) | 14 |
| 58 | Medir de verdad: perf, dispatch y PGO | **M11** | goto computado y `musttail` (extensiones), perf compilado desde el kernel de WSL, flame graphs, `-fprofile-generate` y `-fprofile-use` | metodología de benchmarks, dispatch threaded | 12 |

#### Parte X. Programas reales y Ki v0.1 (≈ 52 h → M12)

| # | Capítulo | Hito | C++ nuevo | Compiladores | h |
|---|---|---|---|---|---|
| 59 | La biblioteca estándar en Ki | Map, Set, Array y String con FBIP | primitivas del runtime para strings y arrays (`memchr`, `memcmp`), utilidades UTF-8 | diseño de bibliotecas FBIP | 14 |
| 60 | Paralelismo como biblioteca | curvas de escalado de `parMap`, `parReduce` y un sort paralelo en 16 núcleos | false sharing en la práctica, perfilar la contención, `std::latch` y `std::barrier` como comparación | control de granularidad, fork-join sobre fibras | 10 |
| 61 | Programas de verdad: una CLI paralela y un servidor HTTP | un clon de grep paralelo; un servidor HTTP/1.1 reducido bajo carga | buffers y syscalls revisados, backpressure, herramientas de carga | programas de punta a punta | 14 |
| 62 | Herramientas y release | **M12**: `ki fmt`, `ki explain`, etiqueta v0.1 | CMake install y CPack, versionado semántico, flujo de revisión de código, CHANGELOG, pretty printing de Wadler en C++ | impresión bonita, documentación | 14 |

#### Parte XI. Otras plataformas (≈ 82 h → M13, M14)

Alcance **[mi decisión]**:
- **`ki` sigue siendo una herramienta de Linux x86-64 que compila en cruzado:** `ki build --target=aarch64-linux-gnu` y `--target=x86_64-w64-mingw32`. `libkirt` se compila para cada destino con el g++ cruzado.
- **Los ejecutables corren en el destino** y su salida se compara con la del oráculo en Linux:
  - ARM64: qemu-user para la corrección, y el runner ARM64 de GitHub (`ubuntu-24.04-arm`, hardware ARM real) para los tests de orden de memoria, porque QEMU no muestra los reordenamientos;
  - Windows: el propio Windows de tu PC, que WSL ejecuta directamente, y un runner de Windows en la CI.
- **Portar el propio `ki`** (compilador, intérprete y VM) a esas plataformas va al epílogo.

| # | Capítulo | Hito | C++ nuevo | Compiladores | h |
|---|---|---|---|---|---|
| 63 | AArch64 I: A64, AAPCS64 y selección de instrucciones | `fib` en ARM64 bajo qemu-user | binutils cruzadas (`aarch64-linux-gnu-as`/`ld`), toolchain files de CMake | selección de instrucciones A64, reutilizar el asignador | 14 |
| 64 | AArch64 II: el runtime en memoria débil | `ki_switch` y el deque en ARM64 | `ldar`/`stlr`, tests de tornasol en hardware real o en un runner ARM (QEMU no muestra los reordenamientos) | runtime en memoria débil | 14 |
| 65 | AArch64 III: la suite completa en ARM | **M13** | CI en runners ARM, capas de portabilidad | portabilidad del backend | 12 |
| 66 | Windows x64 I: el ABI Win64 y PE/COFF | un `.exe` de Ki mínimo, compilado desde WSL | ABI Win64 (shadow space; `rdi`, `rsi` y `xmm6`–`15` callee-saved), PE/COFF, `as`/`ld` de MinGW-w64 | segundo ABI, PE/COFF | 14 |
| 67 | Windows x64 II: el runtime en Windows | fibras y planificador en Windows | hilos Win32, `VirtualAlloc`/`VirtualProtect`, `WaitOnAddress`, límites de pila del TEB | runtime portable | 14 |
| 68 | Windows x64 III: IOCP y la suite en Windows | **M14** | IOCP, E/S overlapped, Winsock | E/S por completado frente a por disponibilidad | 14 |

**Epílogo** (fuera del tronco):
- **Del runtime y la memoria:**
  - pilas que crecen por copia, y tail recursion modulo cons;
  - continuaciones multi-shot copiando segmentos;
  - handlers de usuario para `IO` y `Async` (mocks y concurrencia estructurada);
  - un recolector de ciclos restringido a canales;
  - io_uring.
- **Del backend:**
  - optimizaciones SSA sobre MIR (GVN, LICM);
  - un asignador por coloreado de grafos;
  - backend WebAssembly.
- **De las herramientas:**
  - LSP y compilación incremental basada en consultas;
  - paquetes, compilación separada e interfaces;
  - FFI con C;
  - portar la propia cadena `ki` (compilador, intérprete y VM) a Windows y ARM64.
- **Otros:**
  - planificación heartbeat;
  - autohospedar el front end en Ki;
  - y, para tu objetivo de contribuir a open source, cómo llevar lo aprendido a un compilador real (GCC, LLVM, Clang).

---

## 13. Riesgos

| Riesgo | Mitigación |
|---|---|
| Alcance y aguante durante más de dos años | 14 hitos, ninguno a más de unas 100 h del anterior; sesiones de 2–3 h que acaban en verde; horas registradas; re-planificación por partes; orden de recorte publicado |
| Deriva semántica entre backends | Una sola semántica escrita, el oráculo, `--check-passes`, verificadores entre pases, kigen a tres bandas |
| UB y errores de memoria en C++ | Ids en lugar de punteros, arenas, sanitizers en todas las builds de depuración, GCC y Clang con `-Werror`, tests desde el capítulo 1 |
| Fallos de concurrencia | Barridos de semillas, TSan con anotaciones y la variante `seq_cst`, tests de tornasol, `seq_cst` primero, aparcar y soltar, estrés forzando migraciones |
| Sanitizers confundidos por las pilas propias y el allocator | APIs de fibras de los sanitizers, `KI_ALLOC=system` con ASan, `ci/tsan-env.sh` |
| Que el sistema de tipos crezca hacia un zoo | Las reglas de v0 se congelan en `docs/v0.md`. Una característica nueva necesita justificación escrita, sus mensajes de error y un argumento de decidibilidad antes de escribir código |
| Línea de enlace frágil | Capturada al configurar, `ki doctor`, CI con la misma imagen |
| Que el libro y el repo se separen | Listados extraídos solo de etiquetas verificadas, `bookx verify`, CI nocturna sobre todas las etiquetas |
| Estimaciones de horas equivocadas | Etiquetadas como estimación, reserva del 15 %, horas reales en el progreso del libro |
| Empleabilidad: saber Ki no es saber C++ vendible | El recuadro «C++ en el trabajo» en cada capítulo; el repo como portfolio; las Partes VI–VIII cubren lo que se pregunta en entrevistas de sistemas |

## 14. Una nota honesta sobre «experto»

Este libro te da algo poco común: construir de verdad un compilador, un runtime concurrente, un allocator, un backend nativo y su verificación, en C++ moderno y con disciplina profesional (sanitizers, CI, revisión, perf). Al terminarlo tendrás un nivel avanzado en esas áreas y un portfolio que lo demuestra.

Pero el nivel experto en C++ que se pide en puestos senior suele llevar de 3 a 5 años o más de uso profesional: bases de código grandes y ajenas, metaprogramación de plantillas a fondo, estabilidad de ABI, herramientas de Windows y bibliotecas de dominio. El libro cubre una parte con los recuadros «C++ en el trabajo» y te señala el resto en el epílogo. Contribuir a un compilador real después es el paso natural para tu objetivo de open source.

---

## 15. Decisiones que he tomado por ti

Revísalas mientras lees; cualquiera se puede cambiar.

| # | Decisión | Por qué |
|---|---|---|
| 1 | Base «arquitectura primero», con injertos de las otras dos | La recomendaron dos de los tres jueces; es la más fiel a tu sintaxis y la que tiene el oráculo más independiente |
| 2 | Core sin tipos, con Lint y anotaciones `rep` | Un Core tipado con HKT exige System Fω; Lint y `rep` dan lo necesario con mucha menos carga |
| 3 | Core ejecutable en cada etapa (S0–S4) y `--check-passes` | Cada fallo queda localizado en el pase que lo introdujo, antes de que existan la VM o el nativo |
| 4 | Perceus una sola vez, sobre Core | Los backends ejecutan el mismo plan de dup/drop, y el oráculo lo comprueba en el heap comprobado |
| 5 | El intérprete tiene su propio planificador y su propio modelo de canales | Si usara el runtime de producción, no podría encontrar fallos del runtime |
| 6 | Multinúcleo antes que nativo (sobre las fibras de la VM) | Tu interés principal llega unas 100 h antes, y sus fallos no se mezclan con los del backend |
| 7 | El modelo de memoria antes del primer atómico | Así ningún atómico aparece antes que su teoría |
| 8 | Canales de Go puros: un solo `Chan`, cierre explícito, `send` hace panic si está cerrado | Lo pediste estilo Go, y cerrar al soltar haría observable el momento del drop |
| 9 | El programa termina cuando `main` devuelve; deadlock → salida 102 | Semántica de Go |
| 10 | `spawn` como palabra clave que devuelve `()`; `task` y `join` como biblioteca | Como `go` en Go; el join se construye con un canal |
| 11 | Continuaciones locales a la fibra | Evita recorrer pilas capturadas al compartir y cierra una clase de ciclos |
| 12 | Tres formas de cláusula: tail-resumptive, `, k` y `, _` | Lo que cuesta cada cláusula se ve en la sintaxis, y el oráculo las desazucara literalmente |
| 13 | El camino rápido tail-resumptive solo en el capítulo 55 | Primero la corrección; el oráculo nunca lo toma |
| 14 | `IO` y `Async` primitivos, no manejables en v0 | Tipos simples y un runtime multinúcleo; los handlers de usuario para ellos van al epílogo |
| 15 | Firmas `pub` obligatorias, privadas inferidas por SCC, lo local nunca generalizado | Es HM de verdad con errores locales |
| 16 | Literales: `Int`/`Float`, y `Byte` por comprobación; ningún otro defaulting | Predecible, y nada de sorpresas al estilo de Haskell |
| 17 | Sin tipo `Never`; `fail<A>` y `panic<A>` son abortivos por tipado | Ahorra una regla de coerción |
| 18 | `Result<T, E>` en el orden de Rust; sin Functor; errores con `Fail<E>` | Lo familiar para Rust y TS, y coherente con los huecos solo al final |
| 19 | Records `Point { x: 1.0 }` y update `{ x: 3.0, ..p }`, con la restricción de Rust en las cabezas | Familiar, y sin ambigüedad con `if x == Origin {` |
| 20 | `type` para sumas y records, sin `struct` ni `enum` | Es lo que usaste en tu muestra |
| 21 | `var`, `for`, `while`, `break`, `continue` y `return` como azúcar; los closures no capturan `var` | Tus palabras («sintaxis muy similar»), con un núcleo puro y un heap acíclico |
| 22 | Fin de sentencia por salto de línea con la regla de Go; `;` opcional | Tu muestra no lleva `;` |
| 23 | `if` sin `else` como sentencia | Lo normal en TS, Rust y Go |
| 24 | `pub`, `#[derive]`, `#[test]`, `"${x}"` | Rust para traits y atributos, TS para la interpolación |
| 25 | En v0, `for` solo recorre List, Array y rangos | Sin tipos asociados no hay forma general de expresar el elemento de un iterador |
| 26 | `Show` para humanos; String sin comillas salvo dentro de un constructor | `print("hi")` imprime `hi` y `print(Some("hi"))` imprime `Some("hi")` |
| 27 | Pereza: cuerpo puro; memo sin cerrojo entre fibras; reentrada en la misma fibra → panic | Sin colas de espera por thunk, y nadie puede notar la diferencia |
| 28 | Palabras de 64 bits etiquetadas; `Int` de 64 bits, inmediato si cabe en 63 bits y en caja si no; `Float` empaquetado en v0; desempaquetado en el capítulo 56 | Una sola representación en los tres backends desde el principio, con tu `Int` de 64 bits |
| 29 | Cabecera de RC de Lean; reuse solo con `rc == 1` | Mantiene el invariante de lo compartido y lo local |
| 30 | Allocator estilo mimalloc con liberación remota y devolución de páginas; modos debug y system | Los servidores productor-consumidor no crecen sin límite, y ASan lo ve todo |
| 31 | Pilas: slabs `MAP_NORESERVE`, 16 MiB por segmento, 1 GiB para `main`; sin crecimiento por copia | Simple y suficiente; la copia es arriesgada y va al epílogo |
| 32 | Sin `thread_local` en libkirt; contexto explícito, `r15` y `{valor, ctx}` | Verificado: GCC 15.2 pliega `&tls` entre llamadas |
| 33 | KiCC como en el apartado 9.3 | Mapas de pila triviales, llamadas de cola y migración segura |
| 34 | La escalera spill-all → Poletto–Sarkar → Wimmer–Franz, con verificador | Siempre tienes una línea base correcta a la que volver |
| 35 | Línea de enlace con `g++ -no-pie -###`, libstdc++ enlazada, futex directo en lugar de `atomic::wait` | Robusta frente al toolchain, y el futex enseña lo que hay debajo |
| 36 | `-ffp-contract=off`, SSE2, una libm y un formateador | Floats idénticos byte a byte en los tres backends |
| 37 | Esquema de códigos de error y de salida (0, 1, 101, 102, 103) | Observables precisos para los tests |
| 38 | 68 capítulos en 11 partes, 14 hitos, unas 836 h más un 15 % de reserva, sesiones de 2–3 h | Cumple tu margen y el ritmo de 5–10 h por semana |
| 39 | Progreso en la base de datos persistente del artifact, con horas reales | Dura dos años y permite recalibrar |
| 40 | Tarjetas de test con claves en inglés y texto en español; niveles `core` y `stretch` | El fichero es un formato; lo que lees es prosa |
| 41 | Historia lineal, etiquetas por capítulo y por sesión; correcciones previas a la publicación con fixup no interactivo | Sin `rebase -i`, sin worktrees y sin basura |
| 42 | Listados con `git show`; builds de etiquetas con `git archive`, borradas al acabar | Tu regla de no dejar basura |
| 43 | Toolchain: GCC 15.2 principal, clang-21 secundario (instalado en el capítulo 1), presets de CMake | Coincide con tu máquina |
| 44 | El compilador puede usar excepciones de la biblioteca estándar, pero ninguna cruza un pase; el runtime va sin excepciones ni RTTI | Lo habitual en la industria y lo que necesita un runtime |
| 45 | La raíz del proyecto es el directorio del fichero de entrada; no hay `ki.toml` | Ahorra un parser de TOML en v0 |
| 46 | ARM64 con qemu-user y el runner ARM64 de GitHub para el orden de memoria; Windows con las binutils y el g++ de MinGW-w64, cruzados desde WSL | Encaja con `as`/`ld`, con tu máquina y con los repos públicos |
| 47 | El título del libro | Provisional |
| 48 | En la Parte XI, `ki` compila en cruzado desde Linux; los ejecutables corren en el destino y se comparan con el oráculo | Portar todo `ki` no cabe en 82 h; lo que se aprende es el backend y el runtime de cada plataforma |
| 49 | Los tests con funciones trascendentes llevan `// libm: platform` y solo se comparan dentro de su plataforma | No puede haber «una sola libm» entre glibc y UCRT |
| 50 | Dos repos: el tuyo, que empieza vacío y es tu portfolio, y el de referencia, que construyo yo | «quiero escribir yo los tests», y un portfolio escrito por otro no te sirve para tu desempate |
| 51 | Las interfaces van siempre completas en el libro, aunque la implementación vaya con pista | Así tu código encaja con los capítulos siguientes |
| 52 | Verificación de la referencia: cada etiqueta, sus tests; lo pesado solo en las etiquetas de hito | 300 etiquetas no caben en una CI completa por etiqueta |
| 53 | `UInt64` desde el capítulo 16, con la misma representación que `Int` | Recortar el capítulo 56 no debe dejar Ki sin enteros sin signo de 64 bits |
| 54 | `(a.b)(c)` para llamar a una función guardada en un campo | Así `a.b(c)` no es ambiguo antes de los tipos |
| 55 | Métodos: primero las funciones del módulo del tipo, después los métodos de los traits visibles; sin recurrir a UFCS; cada tipo del preludio en su propio módulo | Lo que esperan Rust y Go, con una regla corta y errores claros |
| 56 | Listado completo la primera vez que aparece un concepto; pista con solución plegada cuando se repite un patrón ya enseñado en el libro | Tu regla «completo para lo nuevo», con tu «Nada aún» en compiladores |
| 57 | `aldezex/kilang` lo creas tú en el capítulo 1; `aldezex/kilang-ref` lo creo yo al construir el libro | Tu portfolio es tuyo desde el primer commit |

## 16. Lo que te pregunté y lo que respondiste

| # | Pregunta | Tu respuesta |
|---|---|---|
| P1 | Efectos en firmas y handlers | `uses {Log}` y `handle { … } with { … }` |
| P2 | Flecha del match y lambdas | `=>` en el match y `(x) => e` |
| P3 | Forma de los traits | Como en Rust, con `Self` |
| P4 | Llamadas con punto | Por el tipo, como un método (reglas en el apartado 5.5) |
| P5 | Imports | Al estilo TS: `import { map } from std/list` |
| P6 | Nombres | camelCase |
| P7 | `Int` | 64 bits completos, con panic al desbordar (representación en el apartado 8.1) |
| P8 | Repositorios | Dos, públicos y con licencia MIT: `aldezex/kilang` (el tuyo) y `aldezex/kilang-ref` (la referencia) |
| P9 | Experiencia en compiladores | «Nada aún»: todo desde cero |

P2, P5 y P6 los respondiste juntos al elegir el aspecto «TS + Rust».

## 17. Lo que queda para quien construya

- La lista exacta de opcodes y su codificación.
- La tabla exacta de clases de tamaño del allocator.
- La redacción exacta de cada mensaje de error, dentro de las reglas del apartado 7.3.
- El corte exacto de cada capítulo en sesiones.
- La lista fina de funciones de la biblioteca estándar.
- Los benchmarks concretos.
- Las distribuciones de kigen.
- La maquetación del libro web, dentro de las reglas del apartado 12.
