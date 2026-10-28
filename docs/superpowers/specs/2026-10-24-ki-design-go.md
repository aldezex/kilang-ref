# Ki: documento de diseño del lenguaje y del libro (edición Go)

**Fecha:** 2026-10-24 · **Revisión:** 2026-10-24, tarde, tras dos comprobaciones independientes del borrador (la del constructor, `builder-check.md`, y la técnica, `tech-check.md`, verificada sobre Go 1.26.5 en tu máquina); el apéndice A lista lo que cambió · **Estado:** listo para tu aprobación. Hasta que lo apruebes no se construye nada: ni repositorios, ni código, ni capítulos nuevos del libro.

**Actualización:** 2026-10-24, con tus respuestas a las preguntas 1 a 4 del apartado 17 (un push por capítulo, Go 1.27.3, error fatal de Go en el desbordamiento nativo, `testing`), que pasan de decisiones del diseño a decisiones tuyas (apartados 4 y 16); las preguntas 5 y 6 (`sudo` y título) no se plantearon y siguen con el defecto del diseño. Ya no queda ninguna pregunta abierta y la versión de partida es 1.27.3; el apéndice A describe la revisión anterior, hecha antes de tus respuestas.

**Actualización:** 2026-10-28, durante el capítulo 7, con cinco decisiones tomadas contigo: diagnósticos al nivel de rustc como restricción global (apartado 7.3 y una sesión nueva del capítulo 7); un servidor de lenguaje en el tronco (capítulos 62–64, con los antiguos 62–68 renumerados a 65–71); secciones «Proyectos en Ki» al final de las Partes II, III, VII, X y XI; llamadas coloreadas con `await` (apartado 5.6); y la compilación dirigida por los efectos (`IO` a solas ya no cede, especialización de efectos, la fila de `main` inferida, un runtime que solo se enlaza y arranca cuando hace falta: apartados 5.6, 6.1, 8.5, 8.7 y 9.3). Lo que elegiste tú está en el apartado 4; el detalle del diseño, en el apartado 15.3 y en el apéndice A («Enmiendas del 2026-10-28»). El tronco pasa de 794 a 856 h y de 68 a 71 capítulos.

**Cómo leerlo.**
- Lo que decidiste tú aparece como tuyo, con tus palabras cuando las hay (apartado 4). Las decisiones de ayer siguen vigentes salvo las que cambiaste hoy al elegir Go.
- Lo que he decidido yo lleva la marca **[decisión del diseño]**. Las nuevas o cambiadas por Go están resumidas en el apartado 15 para que las revises de un vistazo; las heredadas del documento de C++ se citan ahí por grupos.
- Las seis preguntas del borrador están en el apartado 17: respondiste las cuatro primeras el 2026-10-24 y las otras dos llevan el defecto del diseño. Ninguna queda abierta. Las opciones de cada una siguen en `questions-for-alvaro.md`.
- Los hechos sobre Go llevan su fuente entre corchetes: `[R1 §4.2]` es el informe R1, apartado 4.2. R1–R4 son los cuatro informes de investigación de hoy (backend en ensamblador de Go, efectos y fibras, heap propio con Perceus, y currículo), verificados sobre Go 1.26.5 en tu máquina. `[T V-t3]` es una prueba ejecutada hoy por la comprobación técnica (su apéndice A) y `[T S]` una cita del código fuente de Go 1.26.5.

**De dónde sale.**
- El documento de diseño de la era C++ (2026-10-23), aprobado por ti. El lenguaje Ki, la pedagogía del libro, el proceso con etiquetas y la mayor parte de la arquitectura del compilador se conservan. Todo lo que dependía del anfitrión está reescrito.
- Los informes R1–R4. Donde el índice borrador de R4 choca con lo que R2 y R1 verificaron (capítulos 21, 40, 41 y 52–54 en la numeración de R4, que es la de ayer, y el capítulo S5 que faltaba), ganan R1 y R2, porque están verificados; R2 §10 documenta cada choque. Este documento renumera los capítulos 32–53 (apartado 12.5).
- El dossier de lenguajes anfitriones del 23 de octubre, como contexto para Go.
- Las dos comprobaciones del borrador de hoy. La del constructor encontró cinco contradicciones y doce cuestiones menores; la técnica, nueve hechos de Go o mecanismos nativos equivocados, veinticinco inconsistencias y seis riesgos que faltaban. Todo está corregido aquí; lo que cada una dejaba al constructor está decidido y marcado **[decisión del diseño]**, y lo que solo podías decidir tú está resuelto en el apartado 17: lo decidiste en cuatro casos (apartados 4 y 16) y en los otros dos queda el defecto del diseño.

---

## 1. Intención

Quieres aprender un lenguaje nuevo «de 0 a profesional experto» construyendo el tuyo propio, Ki: funcional, con los conceptos centrales de Haskell, centrado en concurrencia, multihilo y rendimiento, y no implementado en Haskell. Ayer el lenguaje que aprendías construyéndolo era C++. Hoy es **Go**: «vamos a dejar C++ para usar Go. Lo hemos estado pensando y es lo mejor para nosotros, ya que Go se acerca más a nuestra meta profesional». Cuando el diseño esté cerrado, el resultado es un libro web completo, «de 0 a 100», paso a paso, respaldado por un repositorio de referencia cuyos tests pasan en cada capítulo. El libro se publica ya y crece capítulo a capítulo: «no esperes a tener todo el libro para ir montándolo, de hecho empieza ya».

Hay dos productos que se sostienen mutuamente:

1. **Ki v0.** Compilador y runtime escritos a mano en Go, con tres backends que se comprueban entre sí:
   - un intérprete de referencia, que hace de oráculo;
   - una máquina virtual de bytecode;
   - código nativo x86-64, emitido como ensamblador de Go (sintaxis Plan 9) que `go build` ensambla y enlaza con el runtime de Ki, `kirt`, escrito en Go.
2. **El libro.** El camino para que construyas Ki y, por el camino, aprendas Go desde cero hasta nivel profesional.

Lo que cambia respecto a ayer no es Ki, sino la casa en la que vive. Y la casa impone reglas: el código nativo de Ki corre sobre pilas de goroutines, que el runtime de Go mueve y recorre, así que las fibras y las continuaciones no pueden ser trozos de pila desprendidos como en la era C++. Se vuelven **estados en el heap** y un **paso de evidencia** en el Core (apartados 6.1 y 8.4). Eso es lo que R1 y R2 verificaron, y lo que más cambia en este documento.

## 2. Objetivos, no-objetivos y anti-objetivos

### Objetivos
- **El lenguaje.** Ki es funcional y puro: estricto con `lazy` explícito, efectos algebraicos con handlers, Hindley-Milner con typeclasses y tipos de orden superior, ADTs con pattern matching exhaustivo, y errores que se leen.
- **La sintaxis.** Ki se escribe como TypeScript, Rust o Go: llaves, `nombre: Tipo`, `f(x, y)`, palabras clave y operadores familiares, `|>`. Funcional por dentro, familiar por fuera.
- **Concurrencia al estilo Go.** Fibras, canales y `select` sobre un planificador multinúcleo con work stealing escrito por ti, con un modo determinista para depurar. Y ahora con una ventaja: el modelo que copias es el del lenguaje que aprendes, y puedes abrir `runtime/proc.go` para comparar [R4 §5.4].
- **Rendimiento real.** RC de Perceus con reutilización en el sitio sobre un heap propio que el recolector de Go no ve, y código nativo con selección de instrucciones y asignación de registros propias.
- **Arquitectura definitiva desde el capítulo 1.** Cada hito rellena etapas del pipeline final y ninguna se tira.
- **Go.** Aprender Go desde cero hasta nivel profesional; ante la duda, gana la empleabilidad.
- **El libro.** Tests antes del código, ejercicios, preguntas de repaso y resumen en cada capítulo. Publicado de forma incremental.

### No-objetivos de v0 (van al epílogo, o a ninguna parte)
- Continuaciones multi-shot, handlers de usuario para `IO` y `Async`, y cancelación estructurada.
- Nada en el sistema de tipos más allá de Haskell 98 + HKT + filas de efectos: ni clases multiparámetro, ni dependencias funcionales, ni familias de tipos, ni GADTs, ni tipos de rango N.
- Macros, FFI (cgo), gestor de paquetes, compilación incremental, JIT, WebAssembly. (El servidor de lenguaje dejó de ser un no-objetivo el 2026-10-28: es tuyo, y va en el tronco, capítulos 62–64. La compilación incremental basada en consultas sigue en el epílogo.)
- Un recolector de ciclos; como mucho, en el epílogo, uno restringido a canales.
- Actores, STM o paralelismo de datos como características del lenguaje. No los elegiste; el paralelismo va como biblioteca.

### Anti-objetivos (sería un fracaso aunque funcionara)
- Un zoo de extensiones al estilo GHC, o errores de tipos crípticos.
- Una sintaxis superficial con aspecto de Haskell u ML.
- Arquitectura provisional que haya que tirar después.
- Depender de módulos de terceros dentro de Ki: ni `golang.org/x/*` (`x/sys`, `x/tools`, `x/exp`, `x/sync`, `x/text`), ni LLVM, ni cgo, ni ningún módulo fuera de la biblioteca estándar [R4 §5.5].
- Que Ki «use» el runtime de Go en lugar de construir el suyo: que las fibras sean goroutines por defecto, que el heap de Ki sea el heap de Go, o que `select` de Ki sea `select` de Go. El dossier avisó de ese riesgo, y el apartado 8 es la respuesta.
- Un listado del libro que no compile, o cuyos tests no pasen en la etiqueta que cita.
- Un libro que vuelque código sin explicarlo.
- Basar algo en el Ki de agosto (`~/ki`), o tocarlo. Tampoco en el trabajo de C++ que borraste hoy.
- Dejar basura fuera del repo: clones, worktrees, builds temporales o directorios de `go build` olvidados.

## 3. Restricciones

- **Anfitrión.**
  - Go 1.27.3 linux/amd64, instalado en `~/.local/opt/go1.27.3` (`GOROOT`; `~/.local/opt/go` apunta a él), con `go` en el `PATH` (`go version` da `go1.27.3`) y `GOAMD64=v1` y `CGO_ENABLED=1` por defecto. Es la versión del libro (tu decisión, apartado 4) y la última estable de su línea hoy. R1–R4 y las dos comprobaciones verificaron sus hechos sobre Go 1.26.5, que sigue en `~/.local/opt/go1.26.5` pero ya no es la versión de trabajo: esa procedencia se conserva en las citas y en las marcas «1.26.5» de más abajo, y no fija ninguna versión a usar. Antes del capítulo 1 se reverifican sobre 1.27.3 los hechos que usan las Partes I y II (toolchain, `go mod init`, la directiva `ignore`, los flags de `testing`, los métodos genéricos sobre tipos concretos); los hechos de runtime y ensamblador de R1–R3 los revalidan los canarios (apartado 11.4) en los capítulos que los usan.
  - Go 1.27 (1.27.0 el 2026-08-19; 1.27.3 el 2026-10-19) añade métodos genéricos sobre tipos concretos, el perfil de fugas de goroutines en GA y `synctest.Sleep`, y hace siempre síncronos los canales de `time` (dossier, `cand-go.status`; go.dev/doc/go1.27). Los capítulos 4, 17, 26 y 45 las enseñan. La política de versiones está en el apartado 11.4 **[decisión del diseño]**.
  - Ningún `GOEXPERIMENT` en el tronco (`jsonv2` y `simd` se mencionan, no se usan; el perfil de fugas de goroutines ya no es un experimento en 1.27) [R4 V25–V27].
- **Todo a mano.**
  - Solo la biblioteca estándar de Go. El paquete `syscall` cuenta como el sistema operativo, no como biblioteca: `Mmap`, `Mprotect`, `Madvise`, `EpollCreate1`, `EpollWait`, `Syscall6` para `eventfd` y `futex` [R4 V10, R3 V9, R2 §6].
  - Herramientas, no bibliotecas enlazadas: el toolchain de Go y sus `go tool` (`asm`, `compile`, `link`, `objdump`, `nm`, `pprof`, `trace`, `vet`), Delve (se instala con `go install` a una versión fijada; es una herramienta), gdb (opcional), qemu-user, GitHub Actions. Nada de eso entra en `go.mod`.
  - El runner de tests golden de Ki, el formato de tarjetas, el generador de programas, el reductor, el extractor de listados y el comprobador del proyecto se escriben en Go dentro del repo. El framework de tests unitarios es `testing`, de la biblioteca estándar (apartado 11.1) **[decisión tuya, 2026-10-24: apartado 4]**.
- **Máquina.**
  - Ubuntu 26.04 LTS sobre WSL2 (kernel 6.18), 16 CPUs lógicas y unos 30 GB de RAM.
  - gcc 15.2 instalado: hace falta para `-race`, que necesita cgo [R4 V9]. No hace falta `sysctl vm.mmap_rnd_bits` [R4 V9]. Fuera de `-race`, `ki build` compila siempre con `CGO_ENABLED=0` (apartado 9.3).
  - `vm.overcommit_memory=1`, así que una reserva de 64 GiB `MAP_NORESERVE` funciona; bajo el modo 2 haría falta `PROT_NONE` más `mprotect` por arena, que es lo que hace el diseño de todos modos [R3 §2.6].
  - No están instalados gdb, Delve, qemu-user ni perf (comprobado hoy). El apartado 11.5 dice qué se instala y cuándo.
- **Plataforma.** Linux x86-64 en todo el libro; Windows x64 y ARM64 solo en los capítulos finales. El ABI entre funciones Ki ya no es System V sino el ABI0 de Go (apartado 9.3), que es el mismo en los tres sistemas [R1 §9.1, R4 V7].
- **Repositorios.** Dos nuevos (apartado 11.1), en ext4 bajo `/home`, no en `/mnt/c`. `aldezex/kilang-ref` y `aldezex/kilang` están libres en GitHub y en local hoy (comprobado 2026-10-24). `aldezex/ki` sigue ocupado por el proyecto antiguo.
- **Libro.**
  - Prosa en español. El código, los identificadores, los comentarios, las rutas del repo y los mensajes del compilador, en inglés.
  - Web con índice y progreso guardado, solo para escritorio. Un solo lector: tú.
  - Se publica de forma incremental, capítulo a capítulo, desde el 2026-10-24, en el artifact ya existente.
- **Ritmo.** De 5 a 10 h por semana, con un horizonte de dos años o más.

## 4. Lo que decidiste tú

| Tema | Tu decisión |
|---|---|
| Lenguaje anfitrión | **Go** (2026-10-24): «vamos a dejar C++ para usar Go. Lo hemos estado pensando y es lo mejor para nosotros, ya que Go se acerca más a nuestra meta profesional». Ayer era C++; yo había recomendado Rust |
| Punto de partida en Go | Desde cero, como lo era en C++, apoyándote en Rust, TS/JS y Python para comparar |
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
| Nativo | **(2026-10-24)** Selección de instrucciones y asignación de registros propias que emiten ensamblador de Go (Plan 9); `go build` lo ensambla y lo enlaza con un runtime de Ki escrito en Go |
| Hecho a mano | «quiero construir todas las áreas del compilador a mano, intérprete, etc. Usar librerías lo más mínimo» |
| Ritmo | Arquitectura definitiva desde el principio, aunque tarde semanas en imprimir algo. Aceptas unos 2–3 meses hasta el primer programa |
| Capítulos | Tests antes del código, ejercicios con solución plegada, preguntas de repaso y resumen de teoría |
| Código en el libro | Mixto: completo para lo nuevo; pista y solución plegada para lo que ya sabes hacer |
| Tests | El libro dice qué probar y por qué; tú escribes el test; la versión de referencia está plegada |
| Repo de referencia | «Sí, pero quiero escribir yo los tests» |
| Entrega | **(2026-10-24)** Incremental: «no esperes a tener todo el libro para ir montándolo, de hecho empieza ya». El libro web ya existe y crece capítulo a capítulo |
| Formato | Web con índice y progreso guardado, solo para escritorio |
| Idioma | Español, con el código en inglés |
| Plataformas | Linux x86-64 en WSL primero; Windows y ARM64 en los capítulos finales |
| Experiencia en compiladores | «Nada aún»: todo desde cero, con listados completos en lo que sea de compiladores |
| `Int` | 64 bits completos, con panic al desbordar. Yo había recomendado 63 bits; el apartado 8.1 explica cómo lo implemento sin su coste |
| Repositorios | Dos, públicos en GitHub y con licencia MIT: `aldezex/kilang` (el tuyo) y `aldezex/kilang-ref` (la referencia). **(2026-10-24)** Todo el trabajo de C++ está borrado; `kilang-ref` empieza de cero con el mismo nombre |
| Aspecto | TS + Rust: camelCase, lambdas `(x) => e`, `=>` en el match e imports `import { … } from …` |
| Efectos | `uses {Log}` en las firmas y `handle { … } with { … }` para manejarlos |
| Traits | Como en Rust, con `Self`: `trait Show { … }` e `impl Show for Shape` |
| Llamadas con punto | Por el tipo, como un método |
| Lectores | **(2026-10-24)** Uno: tú |
| Pushes del repositorio de referencia | **(2026-10-24)** Un push por capítulo, hecho por ti, antes de publicar cada capítulo. Un capítulo se publica solo con GitHub Actions en verde sobre su etiqueta `chNN`. Las etiquetas de sesión pasan por `ci/ci-local.sh`. Las correcciones antes de publicar pueden mover etiquetas aún no publicadas (`--force`) |
| Versión de Go | **(2026-10-24)** Go 1.27.3, ya instalado en `~/.local/opt/go1.27.3` con `go` en el `PATH` (`go1.27.3`). `go.mod` con `go 1.27` + `toolchain go1.27.3`. Antes del capítulo 1 se reverifican sobre 1.27.3 los hechos de las Partes I y II (toolchain, `go mod init`, la directiva `ignore`, los flags de `testing`, los métodos genéricos sobre tipos concretos); los de runtime y ensamblador los revalidan los canarios en sus capítulos |
| Desbordamiento de pila en nativo | **(2026-10-24)** Aceptar el error fatal de Go (salida 2), que `ki run` y `ki test` traducen a 103; en las pruebas diferenciales el resultado es inconcluso. `--stack-check=count` es opcional |
| Tests unitarios | **(2026-10-24)** `testing`, de la biblioteca estándar. Siguen escritos a mano el runner de golden de Ki, kigen, el reductor, bookx y kicheck |
| Errores | **(2026-10-28)** Al nivel de rustc: cada error explica, señala el sitio relacionado, propone una acción concreta y, cuando la corrección es mecánica, la escribe como sugerencia; cada código tiene su entrada en el catálogo (`docs/errors`) con un ejemplo erróneo y otro corregido, los dos probados. Es una restricción global desde el capítulo 7 (apartado 7.3) |
| Servidor de lenguaje | **(2026-10-28)** Un LSP en el tronco: diagnósticos en vivo, hover, ir a la definición, referencias, autocompletado, símbolos y tokens semánticos; Neovim primero (su cliente integrado), Zed después. Capítulos 62–64 |
| Proyectos | **(2026-10-28)** Secciones «Proyectos en Ki» al final de las partes, con programas completos ordenados por lo que el lenguaje ya sabe hacer: calculadora de expresiones, juego de la vida, Markdown→HTML; parser de JSON con buenos errores, generador de sitios estáticos; chat TCP con `select`, caché clave-valor en red, limitador de tasa; Mandelbrot o raytracer paralelo a PPM con la aceleración medida; y, de cierre, un mini-Lisp en Ki |
| Llamadas que suspenden | **(2026-10-28)** Coloreadas: `await` marca toda llamada que puede suspender la fibra (la que tiene `Async` en su fila instanciada); el comprobador lo exige y lo prohíbe donde no toca. Es un marcador estático: la semántica, S5 y el runtime no cambian (apartado 5.6) |
| El runtime, a medida | **(2026-10-28)** «Lo más óptimo posible; que el binario sepa cuándo cargar qué»: los efectos del programa deciden qué se comprueba, qué se enlaza y qué arranca (apartados 5.6, 6.1, 8.5, 8.7 y 9.3) |

---

## 5. Ki v0: el lenguaje

El lenguaje no cambia con el anfitrión. Este apartado es el de ayer, apretado; solo el apartado 5.10 (floats) tiene reglas nuevas, porque las de ayer eran flags de GCC.

### 5.1 Un vistazo

Este es tu ejemplo, ampliado con un trait, un efecto, un handler, `spawn`, `select` y `await`, con la sintaxis que elegiste. Los `await` marcan las llamadas que pueden suspender la fibra (apartado 5.6): `send` lo lleva; `channel`, `after`, `area` y `print` no, porque nunca aparcan; y los `recv` de los brazos de un `select` no lo necesitan, porque el `select` ya lo es.

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

  let results: Chan<Float> = channel(2)   // channel never parks: no await
  for s in shapes {
    spawn { await send(results, area(s)) }      // one fiber per shape; send can park
  }
  select {                                      // select is the marker: no await in its arms
    r = recv(results) => print("first: ${r}")      // r: Option<Float>
    recv(after(ms(500))) => print("timed out")     // after arms a timer and returns at once
  }
}
// measuring circle r=1.0
// measuring rect 2.0x3.0
// total area: 9.141592653589793
// first: Some(3.141592653589793)      (o Some(6.0): depende de la planificación)
```

### 5.2 Léxico y la regla de línea

**Identificadores.** Los que empiezan por mayúscula nombran tipos, constructores, traits y efectos; los que empiezan por minúscula, valores, funciones y variables de fila. Así un patrón distingue un constructor de una variable sin consultar nada. Son ASCII. Valores y funciones en camelCase (lo elegiste tú).

**Comentarios:** `//` y `/* */`.

**Literales:** enteros `42`, `1_000`, `0xFF`, `0b1010`; flotantes `3.14`, `1e-9`; caracteres `'a'`, `'\u{1F600}'`; strings `"x = ${x}\n"`, con interpolación `${expr}`.

**Regla de línea (los `;` son opcionales)** **[decisión del diseño]**.
- Dentro de `()` y `[]`, el salto de línea es espacio.
- Dentro de `{}` o en el nivel superior, un salto de línea termina la sentencia, el item o el brazo, salvo en dos casos: la línea acaba en un token que no puede cerrar una expresión (un operador binario, `,`, `(`, `=`, `=>`…), o la línea siguiente empieza por `|>`, `.`, `&&`, `||` o `else`.
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
tyarg     = ty | '_' | lower                                   // lower = an effect-row variable (kind "row")
          | '{' [ eff { ',' eff } ] [ [ ',' ] '..' lower ] '}'    // a concrete row:  Gen<Int, {Log}>   Gen<Int, {Log, ..e}>
block     = '{' { stmt sep } [ expr ] '}'           sep = NEWLINE | ';'
stmt      = 'let' pat [ ':' ty ] '=' expr | 'var' lower [ ':' ty ] '=' expr | lower '=' expr
          | 'for' pat 'in' expr block | 'while' expr block | 'break' | 'continue' | 'return' [ expr ]
          | fn | expr
expr      = Pratt, loosest first:  |>   ||   &&   == != < <= > >= (non-associative)   ..   + -   * / %
            prefix: ! -  'await' (its operand must be a call: f(x), x.m(y), (e)(x); see 5.6)
            postfix: call (args), .field, .0, .name(args) (a method call, resolved by type), ?
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

Reglas que mantienen el parser sencillo **[decisión del diseño]**:
- **Sin argumentos de tipo explícitos en las expresiones.** En una expresión, `<` siempre es «menor que». Si hace falta fijar un tipo, se anota el `let`.
- **Restricción de Rust para los literales de record.** Un literal `Name { … }` no puede ir directamente en la cabeza de un `if`, `match`, `for` o `while`. Así `if x == Origin {` no es ambiguo.
- **Lambdas por búsqueda acotada.** Al ver `(`, el parser busca el `)` emparejado y mira si lo sigue `=>`.
- **Sin backtracking.** El parser es descenso recursivo más Pratt.
- **`a.b(c)` nunca es ambiguo.** Si `a` nombra un módulo, es una llamada cualificada. Si no, es una llamada a método (apartado 5.5). Para llamar a una función guardada en un campo se escribe `(a.b)(c)`, como en Rust.
- **Los parámetros de fila tienen kind «fila».** Un parámetro en minúscula de una declaración (`type Gen<T, e>`) solo se instancia con una variable de fila o con una fila concreta entre llaves (`Gen<Int, {Log}>`), y un parámetro en mayúscula nunca con una fila; el comprobador de kinds lo exige **[decisión del diseño]**.
- **`await` es un prefijo con la precedencia de los unarios, y su operando es una llamada** **[decisión del diseño, 2026-10-28]**. `await f(x)` y `await xs.map(f)` se leen como el resto de los prefijos; un `await` seguido de algo que no es una llamada (`await x`, `await 1 + 2` donde el operando resulta ser `1`) es E0206 `` `await` must be followed by a call `` con la ayuda «write the call after `await`: `await f(x)`». En una tubería, `await` va en el operando derecho, `xs |> await map(fetch)`, y el desazucarado lo mueve con la llamada: `await map(xs, fetch)`. `await` es palabra reservada desde el capítulo que introduce `Async` (el 25).

**Desazucarado de `|>`** **[decisión del diseño]**: la tubería inserta el operando izquierdo como *primer* argumento. `e |> f(a, b)` es `f(e, a, b)` y `e |> f` es `f(e)`; `e |> x.m(a)` es `x.m(e, a)` tras resolver el método. Es la forma que casa con `name(e, …)`, el orden de parámetros de la biblioteca estándar (`map(xs, f)`) y la sugerencia «propone `name(e, …)` o `e |> name`» del apartado 5.5. El desazucarado ocurre en la elaboración; el parser solo ve un operador binario.

### 5.4 Tipos e inferencia

**Tipos predefinidos:** `Int` (entero con signo de 64 bits), `UInt64`, `Float` (IEEE binary64), `Bool`, `Char` (un escalar Unicode), `Byte` (u8), `String` (UTF-8 inmutable), `()`; tuplas `(A, B)`; funciones `(A, B) -> C uses {…}`.

El preludio añade `List<T>`, `Option<T>`, `Result<T, E>`, `Ordering`, `Range`, `Lazy<T>` y `Chan<T>`. La biblioteca estándar, `Array<T>`, `Map<K, V>`, `Set<T>` y `Duration`.

**Reglas de inferencia.** Son las seis que mantienen el HM real (lo pediste) y a la vez predecible y con errores locales (también lo pediste):

1. **HM bidireccional.** Unificación con union-find y niveles, al estilo de Rémy y OCaml. Se usa el modo de comprobación siempre que el tipo esperado ya se conoce, así que los parámetros de una lambda pasada a `map` no necesitan anotación. El acceso a campos necesita que el tipo del receptor se conozca en ese punto de la lectura de izquierda a derecha.
2. **Las funciones `pub` llevan la firma completa:** parámetros, retorno y efectos. Los errores no salen de la función y las interfaces de módulo son explícitas.
3. **Las funciones privadas de nivel superior pueden omitir anotaciones.** Se infieren y se generalizan por componente fuertemente conexa del grafo de llamadas (Tarjan). Las restricciones de traits se convierten en parámetros de diccionario. La recursión polimórfica necesita firma.
4. **Lo local nunca se generaliza** (`let`, `fn` local y lambdas), como MonoLocalBinds en GHC. Usarlo con dos tipos da un error con la solución:
   `local function 'id' is used at (Int) -> Int (4:9) and (String) -> String (5:9); move it to the top level to make it generic`.
5. **Literales.** Un literal entero es `Int`, o `Byte` o `UInt64` si el tipo esperado ya es uno de ellos (con comprobación de rango). Un literal flotante es `Float`. No hay ningún otro defaulting: una ambigüedad es un error con una anotación propuesta.
6. **Lo que no hay.** Subtipos, conversiones implícitas, sobrecarga fuera de los traits, rango N y cómputo a nivel de tipos. Las funciones no se currifican y cada llamada tiene aridad exacta.

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

**Presupuesto de extensiones (es todo):** un parámetro por trait; la cabeza de un impl es un constructor de tipo aplicado a variables distintas, o a huecos `_` al final; los contextos solo restringen variables de tipo; las superclases son acíclicas; no hay impls solapados ni huérfanos (un impl vive en el módulo del trait o en el del tipo de la cabeza); hay métodos por defecto y `#[derive(Eq, Ord, Show)]`. Con esto, la resolución de instancias termina por construcción y nunca hace backtracking.

**Kinds.** Se declaran con huecos (`F<_>` es `* -> *`) y nunca se infieren. Los constructores de tipo están currificados, así que los huecos solo rellenan los últimos parámetros y la unificación de orden superior sigue siendo de primer orden. Por eso `Map<K, _>` puede ser un Functor y `Result<T, E>`, que conserva el orden de Rust, no lo es: los errores se componen con el efecto `Fail<E>` **[decisión del diseño]**. Nota para el libro: Go no tiene tipos de orden superior ni métodos genéricos (1.27 los añade solo sobre tipos concretos, nunca en interfaces), así que esto es exactamente lo que el comprobador de Ki hace y Go no puede; se compila igual, con diccionarios [R4 §4, dossier].

**Operadores.** Son métodos de traits del preludio: `+` `Add` (String también lo implementa, para concatenar), `-` `Sub`, `*` `Mul`, `/` `Div`, `%` `Rem`, `-` prefijo `Neg`, `==` `Eq`, `<` `Ord`. Cuando los tipos son concretos, que es lo normal, la elaboración llama directamente a la primitiva.

**`Show`** es la representación para humanos: lo que imprime `print` y lo que inserta `${}`. Un `String` se muestra sin comillas, salvo dentro de un constructor derivado, porque `showNested` las añade: `print("hi")` imprime `hi` y `print(Some("hi"))` imprime `Some("hi")` **[decisión del diseño]**.

**Compilación.** El paso de diccionarios es la semántica. Una restricción resuelta por una instancia conocida se convierte en una llamada directa. Con `-O2`, un especializador acotado clona funciones aplicadas a diccionarios conocidos (caché por función e ids de diccionario, profundidad 8, presupuesto de tamaño). Los diccionarios nunca se cierran sobre sí mismos: un método recursivo de una instancia llama directamente a la función de la instancia; un método por defecto recibe el diccionario como parámetro explícito; un impl con contexto se convierte en una función que construye un record de punteros a código y diccionarios de contexto. Así el heap sigue siendo acíclico (apartado 8.9).

**Llamadas con punto: métodos por tipo** (lo elegiste tú). `e.name(args)` es una llamada a método cuando `e` no nombra un módulo.
- El comprobador sintetiza primero el tipo de `e`, que tiene que conocerse en ese punto. Si no se conoce, el error propone una anotación.
- `name` se busca en este orden: (1) funciones `name` del módulo que declara el tipo de `e`, visibles desde la llamada, cuyo primer parámetro es de ese tipo; (2) métodos `name` de los traits visibles en la llamada, si hay un impl para el tipo de `e` (incluye receptores genéricos con su restricción: con `x: T` y `T: Show`, `x.show()` funciona); (3) si no hay nada, el error dice qué tipo y qué nombre faltan, sugiere nombres parecidos y, si existe una función libre con ese nombre, propone `name(e, …)` o `e |> name`.
- Dos candidatos del mismo nivel son un error de ambigüedad, y se desambigua con `Show.show(x)`.
- Cada tipo del preludio vive en su propio módulo con sus funciones (`List` en `std/list`, `String` en `std/string`, `Chan` en `std/chan`…), y el preludio lo reexporta. Así `xs.map(f)`, `s.len()` o `ch.send(v)` funcionan sin imports.
- No se recurre a «cualquier función a la vista» (UFCS). Las auxiliares locales se llaman como `f(x)` o con `|>` **[decisión del diseño]**.

### 5.6 Efectos en las firmas

**Declarar y realizar.** Los efectos se declaran como interfaces y se realizan como llamadas normales:

```ki
effect Log      { fn log(msg: String) }
effect State<S> { fn get() -> S; fn put(s: S) }
effect Fail<E>  { fn fail<A>(err: E) -> A }      // result type ∀A: abortive by typing
effect Yield<T> { fn yield(value: T) }

pub fn load(path: String) -> Config uses {IO, Fail<ParseError>} { ... }
```

**Filas.** Son filas con etiquetas con ámbito (*scoped labels*), las de Leijen que usa Koka: las etiquetas casan por nombre y se permiten duplicadas; cada fila tiene una sola cola; la unificación es decidible y principal, sin restricciones *lacks*; al usuario se le muestran como conjuntos.

**Regla ambiente.** Dada la firma `fn f<…>(p1: T1, …) -> R uses {L}`:
- Hay una variable de fila implícita ε, generalizada. La fila de `f` es `{L | ε}`.
- Un tipo función de un parámetro escrito sin `uses` toma la fila entera de la firma, `{L | ε}`. Uno escrito `uses {M}` toma `{M, L | ε}`. `uses only {M}` es una fila cerrada, `{M}`, sin ε.
- Dentro del cuerpo, ε es rígida.
- Consecuencias: `fn area(s: Shape) -> Float` es pura; `fn map<A, B>(xs: List<A>, f: (A) -> B) -> List<B>` es polimórfica en efectos sin decir nada; en `collectLogs`, `body` tiene la fila `{Log | ε}`.
- Las variables de fila explícitas (`<A, e>` y `uses {Log, ..e}`) dan filas independientes cuando hacen falta.
- Dentro de una declaración de tipo, los tipos función son cerrados por defecto. Para guardar una reanudación de un productor con efectos se escribe la variable: `type Gen<T, e> { Done, Next(value: T, rest: () -> Gen<T, e> uses {..e}) }`.

**Reglas de uso.** Una función `pub` escribe su fila; en las privadas se infiere. Una función referida por nombre se abre al instanciarla, así que una función menos efectiva siempre encaja donde se espera una más efectiva.

**Efectos primitivos.** `IO` cubre consola, ficheros, red, reloj, entorno y aleatoriedad. `Async` cubre `spawn`, `send`, `recv`, `select`, `sleep` y `yieldNow`, y también toda operación de `IO` que puede aparcar la fibra (abajo). Los maneja el runtime en la raíz de cada fibra; el código de usuario no puede manejarlos en v0. El cuerpo de un `spawn` debe ser `uses only {IO, Async}`, porque una fibra empieza sin handlers de usuario. El de un `lazy`, `uses only {}`. Los panics no son efectos.

**La fila de `main` se infiere** **[decisión del diseño, 2026-10-28]**, como la de cualquier función privada, y tiene que ser un subconjunto de `{IO, Async}`: un efecto de usuario que llega a `main` sin manejar es el error de efectos de siempre («performs Log but does not declare it»), con las dos soluciones. Antes era implícitamente `uses only {IO, Async}`; ahora `main` es tan efectiva como lo que hace, y de su fila, y de las filas de lo que alcanza, el compilador deduce qué parte del runtime enlaza y arranca (apartado 9.3). Un programa que no usa `Async` no lleva planificador.

Dentro de la fila `IO` hay dos clases de operaciones **[decisión del diseño, T §2.2; reescrita el 2026-10-28]**. Las que pueden **aparcar** la fibra son `perform`s en Core y llevan en su firma **las dos etiquetas, `uses {IO, Async}`**: `readLine`, `readFile`, `writeFile`, la red de `std/net` y la resolución de nombres, todo lo que pasa por el poller o por el pool bloqueante (apartado 8.7). Las que **nunca aparcan** son `prim`s y llevan solo `uses {IO}`: `ki_write` (la escritura de `print`, que va al buffer de `kirt/out`), el reloj, el entorno, la aleatoriedad, y las tres operaciones de la biblioteca de concurrencia que crean o alteran un recurso del runtime sin esperar por nadie, `channel`, `close` y `after`. S5 marca la *fila*, y la marca es **`Async`**: la comprobación `isYielding` se emite tras cada llamada a una función cuya fila contiene `Async`, un efecto de usuario o una variable de fila, y tras ninguna otra; solo un `perform` pone `yielding`, y lo hace de forma idéntica en el oráculo en S5, en `vm-bubble` y en nativo. Una fila con `IO` a solas no cede nunca: `print`, el reloj, el entorno y la aleatoriedad no cuestan ni la carga ni la rama, y los capítulos 49–53 pueden compilar programas con `print` antes de que exista el burbujeo nativo (apartado 12.5). Las operaciones de `Async` son todas `perform`s, salvo las tres `prim`s nombradas, que por eso no están en `Async`.

**Llamadas coloreadas: `await`** **[tu decisión, 2026-10-28; las reglas son del diseño]**. Toda llamada que puede suspender la fibra se escribe con el prefijo `await`, y el comprobador de tipos lo exige y lo prohíbe:
- «Puede suspender» significa: **la fila instanciada del callee contiene `Async`**. Da igual si la operación aparca de verdad en esa ejecución; la marca es estática y por fila, así que una función que solo hace `spawn` también la necesita en sus llamadores, porque `spawn` pone `Async` en su fila.
- Falta el `await` en una llamada que puede suspender: E0406 `` call to `f` can suspend ``, con la primaria sobre la llamada («its effects include `Async`»), la secundaria en la firma de `f` cuando está en el programa del usuario («`Async` comes from here»), la ayuda «write `await f(x)`» y el fix-it que inserta `await `. Sobra en una que no puede: E0407 `` `await` on a call that cannot suspend ``, con la primaria sobre `await` («`f` has no `Async` in its row»), la ayuda «remove `await`» y el fix-it que lo borra. Los dos códigos quedan reservados desde hoy, aunque nazcan en el capítulo 25.
- Dentro de una función polimórfica en efectos, una llamada a través de una variable de fila abstracta (`f(x)` dentro de `map`) **no** lleva `await`: su fila no contiene `Async`, contiene `ε`. Lo lleva el llamador de la instancia: `await map(xs, fetch)`.
- `send`, `recv`, `sleep` y `yieldNow` llevan `await`; `spawn` no (nunca suspende a quien lo ejecuta, y además es una forma, no una llamada); `select { … }` es él mismo el marcador, y los `recv` y `send` de sus brazos no llevan otro. `channel`, `close` y `after` no lo llevan porque no están en `Async`.
- Es solo un marcador: la elaboración lo quita, Core no lo tiene y S5, la semántica y el runtime no cambian. Lo que compra es lo que compra en Rust o en JS: leer dónde puede cambiar la fibra, y que un `send` dentro de un bucle no pase inadvertido. Los códigos E0406 y E0407 nacen con la barra del apartado 7.3 y su entrada en `docs/errors`.

**Handlers.** `handle { body } with { cláusulas }` es una expresión. Las cláusulas tienen tres formas, y la forma dice lo que cuesta **[decisión del diseño]**:

| Forma | Significa |
|---|---|
| `op(x) => e` | tail-resumptive: equivale a `op(x), k => resume(k, e)` (`get() => s`, `log(m) => print(m)`) |
| `op(x), k => e` | de control: `k: Cont<R, U>` y `resume(k, v): U` |
| `op(x), _ => e` | abortiva: suelta `k` sin reanudarlo |
| `return(x) => e` | opcional; si falta, es la identidad |

**Tipado de los handlers.** El cuerpo tiene tipo `T uses {L | r}`, y toda cláusula, incluida `return`, produce `U uses {..r}`. Como el cuerpo se tipa en `{L | r}`, una función genérica no puede capturar en silencio un efecto que su callback no declaró:
`'retry' handles Fail<String> around 'f', but 'f' may perform any effect its caller allows; declare 'f: () -> A uses {Fail<String>}'`.
Los errores de efectos nombran la operación, muestran la fila declarada y proponen las dos soluciones: declarar el efecto o manejarlo.

**Continuaciones.** Son one-shot: reanudar dos veces es un panic. Se pueden guardar en una estructura y reanudar más tarde dentro de la misma fibra, aunque el `handle` ya haya devuelto (así funcionan los generadores). Son locales a su fibra: enviarlas por un canal o capturarlas en un `spawn` es un panic **[decisión del diseño]**. Que se puedan reanudar fuera de su `handle` es lo que obliga a usar el paso de evidencia *generalizado* de Xie y Leijen y no el de «Effect Handlers, Evidently», que limita las reanudaciones a su ámbito [R2 §3.1].

### 5.7 Datos y pattern matching

**Declaraciones con `type`** (tu elección de la muestra; no hay `struct` ni `enum`): sumas `type Option<A> { None, Some(A) }`; records `type Point { x: Float, y: Float }`, que se construyen como `Point { x: 1.0, y: 2.0 }`, se leen como `p.x` y se actualizan como `Point { x: 3.0, ..p }` (en el sitio si `p` es único); alias `type Name = String`.

**Tuplas y listas.** Las tuplas se indexan con `.0`. `List<A>` tiene azúcar `[]`, `[a, b]` y `[h, ..t]`, que también sirve para anteponer en una expresión. `0..n` es un `Range`.

**Patrones:** `_`, variables y literales; constructores posicionales, y records con punning y `..`; tuplas y listas; or-patterns `A | B` y guardas `if`.

**Comprobación de `match`.** `match` es una expresión y debe ser exhaustivo. Lo comprueba el algoritmo de utilidad de Maranget; las guardas no cuentan para la cobertura. Un caso que falta es un error con testigo (`pattern Rect(_, _) not covered`); un brazo inalcanzable, un warning. Los patrones de `let` deben ser irrefutables.

**Compilación de `match`.** Los matches se compilan a árboles de decisión de Maranget, y los cuerpos de los brazos se comparten mediante join points. El intérprete conserva el matching ingenuo, de arriba abajo, así que el compilador de matches queda comprobado contra él desde el primer día.

### 5.8 Control de flujo familiar sobre un núcleo puro

`var`, `for`, `while`, `break`, `continue` y `return` existen como azúcar **[decisión del diseño, en tus propias palabras]**.
- La elaboración los convierte en join points y bucles `joinrec` sobre un Core puro, enhebrando el estado de los `var` como parámetros. Te da la intuición de SSA gratis.
- Un closure no puede capturar un `var`; es un error con explicación. Así no existen referencias mutables y el heap sigue acíclico.
- `if` sin `else` es válido como sentencia (vale `()`).
- En v0, `for` recorre `List`, `Array` y rangos de `Int`. Para cualquier `Foldable` está `each(xs, f)`. v0 no tiene tipos asociados con los que expresar «el tipo de elemento de un iterador».

### 5.9 Módulos

**Un fichero es un módulo**, con el nombre de su ruta: `geometry/vec.ki` es `geometry/vec` y `std/…` es la biblioteca incluida.

**Formas de import**, al estilo de TS (lo elegiste tú): `import std/list` da acceso cualificado (`list.map(xs, f)`); `import std/list as l` renombra; `import { map, filter } from std/list` trae nombres sin cualificar.

**Visibilidad y alcance.** Todo es privado salvo lo marcado `pub`. Un `pub type` exporta también sus constructores. El preludio es implícito y se puede sombrear. Los impls son globales, limitados por la regla de huérfanos.

**Ciclos e inicialización.** Un ciclo de imports es un error que muestra el ciclo (como en Go). Eso fija el orden de inicialización: las constantes `let` de nivel superior se evalúan una vez, en orden topológico de módulos y luego de declaración; las dependencias entre constantes se siguen también a través de los cuerpos de las funciones, como hace Go. Un ciclo entre constantes es un error de valor recursivo.

**Compilación.** v0 compila programas enteros. Como las firmas `pub` son obligatorias, las interfaces de módulo se pueden calcular sin los cuerpos, lo que deja la puerta abierta a compilación separada. La raíz del proyecto es el directorio del fichero de entrada: no hay `ki.toml` en v0 **[decisión del diseño]**.

### 5.10 Números, texto y pereza

**`Int`**: entero con signo de 64 bits completos, como en Rust o Go (lo elegiste tú).
- Todo desbordamiento es un panic, también `MIN / -1`: `panic: integer overflow at f.ki:3:9`, código de salida 101.
- `/` trunca hacia cero y `%` toma el signo del dividendo. Dividir por cero es un panic.
- La aritmética que da la vuelta va en funciones explícitas (`wrappingAdd`…).
- `UInt64`, sin signo, existe desde el capítulo 16 con las mismas reglas.
- Cómo se representa lo explica el apartado 8.1 (inmediato si cabe en 63 bits, caja si no). El programa no lo nota. En Go, a diferencia de C++, el desbordamiento con signo está definido (da la vuelta) [R4 V12], así que el intérprete lo detecta con comprobaciones explícitas y `math/bits`, no con intrínsecos del compilador.

**`Float`**: IEEE binary64. No hay conversión implícita: `toFloat(i)` y `truncate(f)`. Los operadores de comparación siguen IEEE; `compare` usa el orden total.

**Formato de `Float`** **[decisión del diseño]**. Lo fija `kirt/fmtnum`, no `strconv`: `FormatFloat(f, 'g', -1, 64)` pasa a exponente en `|f| ≥ 1e6` y `< 1e-4` y escribe `1e+06`, `NaN` o `+Inf` [T V-t1], y «añadir `.0`» a ciegas daría `1e+06.0`. La regla de Ki, con tests golden:
- los dígitos son los más cortos que dan la vuelta (los de `strconv.FormatFloat(f, 'e', -1, 64)`, que es el único uso de `strconv` en el formato);
- forma fija cuando `1e-6 ≤ |f| < 1e21`, exponencial fuera (`1e21`, `1.5e-7`, `-2.5e300`: sin `+` ni ceros de relleno en el exponente), los mismos umbrales y la misma forma que `Number.prototype.toString` de JavaScript, que ya conoces;
- `.0` solo cuando la forma fija no tiene punto, es decir, cuando la salida casa con `^-?[0-9]+$`: `1.0`, `6.0`, `0.1`, `1000000.0`, `0.001`;
- especiales explícitos: `NaN`, `inf`, `-inf` y `-0.0`.
Es la forma que imprime `print`, la que inserta `${}` y la que `Show` deriva; los tres backends la comparten (apartado 6.3).

**Floats idénticos en todos los backends** **[decisión del diseño, reescrita para Go]**. Go no tiene `-ffp-contract=off`: la especificación permite fusionar `x*y + z` en un FMA, y una conversión explícita `float64(x*y)` lo impide [R4 V11]. En esta máquina (`GOAMD64=v1`) no se fusiona, con `GOAMD64=v3` sí (`VFMADD231SD`), y **arm64 fusiona por defecto** (`FMADDD`) [R4 V11]. Por tanto:
- toda primitiva de `Float` del intérprete, de la VM y de `kirt` se escribe con una operación por expresión o con conversiones `float64()` explícitas;
- `cmd/kicheck`, un comprobador sobre `go/ast`, lo exige desde que existe (capítulo 65); hasta entonces, un test unitario compara los resultados de una build `GOAMD64=v3` con los de la build por defecto, y la pata arm64 es **estática** hasta que exista qemu (capítulo 66): `GOARCH=arm64 go build -gcflags=-S` sobre las primitivas de `Float` y un `grep` que exige que no aparezca `FMADDD|FMSUBD|FNMADDD|FNMSUBD` [R4 §5.9; T §2.19] **[decisión del diseño]**;
- el código nativo emite `MULSD`/`ADDSD` separados y nunca una instrucción FMA;
- una sola primitiva de formato, `kirt/fmtnum`, compartida por los tres backends.

**`String`**: bytes UTF-8 inmutables. `len` cuenta bytes, como en Go y Rust; `chars(s)` da `Char`. `+` concatena, en el sitio cuando el operando izquierdo es único. El corte es por rangos de bytes, comprobados contra los límites de carácter.

**Pereza.** `lazy { e }` tiene tipo `Lazy<T>` y `force(t)` lo evalúa a lo sumo una vez y memoriza el resultado.
- El cuerpo debe ser puro (`uses only {}`).
- Si una fibra fuerza un thunk que se está evaluando en esa misma fibra, salta el panic `lazy value depends on itself`. Con las reglas de v0 ese estado parece inalcanzable (`let` no es recursivo y los ciclos entre constantes se rechazan también a través de los cuerpos de las funciones, apartados 5.9 y 8.9): se mantiene como defensa, y su test construye a mano un programa Core que lo provoca, porque ningún programa Ki puede **[decisión del diseño]**.
- Si dos fibras lo fuerzan a la vez, ambas pueden evaluarlo. El primer CAS publica el resultado y la otra suelta el suyo; como el cuerpo es puro, nadie lo nota **[decisión del diseño]**. Con fibras en el heap (apartado 8.4) esta regla es además necesaria: el código puro no tiene puntos de suspensión y no podría esperar [R2 §11 R8].
- `std/stream` construye `Stream<T> { Empty, Cons(head: T, tail: Lazy<Stream<T>>) }`.

### 5.11 Concurrencia en el lenguaje

**`spawn`**, al estilo `go` de Go: `spawn { bloque }` o `spawn f(a, b)` (en la segunda forma, `f` y sus argumentos se evalúan en la fibra actual y la llamada corre en una fibra nueva). Devuelve `()`. El cuerpo es `uses only {IO, Async}`. Lo que captura pasa a ser compartido. No lleva `await`: nunca suspende a quien lo ejecuta (apartado 5.6).

**`Chan<T>`**, con la semántica de Go: `channel(cap)`, con capacidad 0 es un rendezvous; `await send(ch, v)`, enviar a un canal cerrado es un panic, y cerrar dos veces también; `await recv(ch) -> Option<T>` da `None` cuando el canal está cerrado y vacío; el cierre es siempre explícito, con `close(ch)`, nunca al soltar el último extremo, porque eso haría observable el momento del drop. `send` y `recv` llevan `await` porque pueden aparcar; `channel` y `close` no, porque nunca lo hacen (son `prim`s `uses {IO}`, apartado 5.6).

**`select`**: brazos `recv`, `send` y `default`; `after(d)` devuelve un `Chan<()>` con buffer 1 que alimenta el servicio de temporizadores. El buffer 1 es una decisión de Ki, no una copia de Go **[decisión del diseño]**: el servicio de temporizadores nunca se bloquea al disparar, y un `after` que nadie lee no deja nada aparcado. En Go es al revés desde 1.23: los canales de `time.After` y `NewTimer` **no tienen buffer** (`cap == 0` con `go ≥ 1.23` en `go.mod` [T V-t1]) y en 1.27 son siempre síncronos porque `asynctimerchan` desapareció; el capítulo 26 explica la diferencia y por qué Go la hizo (recoger temporizadores sin referencias).

**Tiempo y cesión:** `await sleep(d)` y `await yieldNow()`. `after(d)` arma un temporizador y devuelve su canal sin esperar, así que no lleva `await`; quien espera es el `recv` de ese canal, o el `select` que lo lee.

**Fin del programa.** El proceso termina cuando `main` devuelve, como en Go. Si todas las fibras están bloqueadas y no hay E/S ni temporizadores pendientes, el runtime informa del bloqueo, con los sitios donde está aparcada cada fibra, y sale con 102. Que las fibras no sean goroutines no basta para garantizarlo: los *workers* sí lo son, y si todos duermen en su token sin que ningún P tenga temporizadores, el `checkdead` de Go mata el proceso con `fatal error: all goroutines are asleep - deadlock!` y salida 2 [T V-t7, `runtime/proc.go:6367-6470`]; al revés, con el poller bloqueado en `EpollWait(-1)` Go nunca lo declara y el proceso se colgaría para siempre. Por eso la detección es de Ki y va **antes** de dormir: el último worker que va a aparcar cuenta las fibras vivas y aparcadas, los intereses de E/S armados y los temporizadores pendientes, y si no queda nada que pueda despertar a nadie, imprime el informe y sale con 102 (apartado 8.5) **[decisión del diseño]**. Bajo el portador B la regla es la misma [R2 §4.2].

**Paralelismo y espera, como bibliotecas** **[decisión del diseño]**: `std/par` ofrece `parMap(xs, grain, f)` y `parReduce`; `std/task` ofrece `task(f)` y `join(t)`, construidos sobre `spawn` y un canal de buffer 1.

### 5.12 Biblioteca estándar v0

Está escrita en Ki sobre primitivas del runtime:
- **Preludio:** los tipos Option, Result, List, Ordering y Range; los traits Eq, Ord, Show, Add, Sub, Mul, Div, Rem, Neg, Num, Functor, Foldable y Monad (`pure`, `flatMap`); las funciones `print`, `each`, `map`, `filter`, `fold`, `sum`, `len`, `panic` y `assert`, y la constante `pi`; `Chan` y sus funciones `channel`, `send`, `recv` y `close`, reexportadas de `std/chan`. `spawn` y `select` son palabras clave.
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

**Qué es** **[decisión del diseño]**: un cálculo lambda n-ario, estricto, con binders únicos y sin azúcar. No tiene tipos: se borran tras la elaboración. Lo comprueba Core Lint después de cada pase.

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
    | dup x | drop x | tok = dropReuse x | K@tok(ā)       -- S4+: Perceus
    | evLookup L | yieldTo(m, op, ē) | isYielding | yieldExtend(@j, S) | prompt h e | restoreEv w   -- S5
H ::= { return(x) => e; op_i(x̄, k) => e … }             -- clause sugar has already been expanded
```

Por la elaboración ya han pasado los traits (los diccionarios son records), las operaciones de efectos (resueltas a etiqueta e índice) y todo el azúcar: pipes, `.f()`, interpolación, update de records, operadores y bucles.

**Etapas.** Todas son subconjuntos o extensiones de la misma IR, y el intérprete puede ejecutar cualquiera **[decisión del diseño]**:

| Etapa | Qué es |
|---|---|
| S0 | Core elaborado, con patrones anidados |
| S1 | ANF: todos los argumentos son átomos |
| S2 | árboles de decisión: `case` plano y proyecciones |
| S3 | sin `letfn`, con closures de entorno explícito |
| S4 (Core+RC) | S3 más `dup x`, `drop x`, `tok = dropReuse x`, `K@tok(ā)` y marcas de préstamo en los parámetros. Cada llamada, `perform`, `handle` y `resume` lleva su *conjunto de suspensión*: las variables poseídas y vivas y los tokens de reutilización en ese punto |
| **S5 (Core+RC+EV)** | **Nueva** **[decisión del diseño, R2 §3.2]**. S4 más el paso de evidencia generalizado con *yield bubbling* de Xie y Leijen (ICFP 2021), la técnica del backend en C de Koka. Es la forma que ejecutan la VM y el nativo, y el oráculo la comprueba contra S4 |

**S5 en detalle.** Para toda función cuya fila *puede ceder* (contiene un efecto de usuario, una variable de fila o `Async`; `IO` a solas no cuenta desde el 2026-10-28, porque las operaciones de `IO` que aparcan llevan también `Async`, apartado 5.6), S5 reescribe cada llamada no final a una función así como:

```text
let r = f(ā)                        -- fast path, unchanged
if isYielding {                     -- one load and one well-predicted branch
    yieldExtend(@join_k, S)         -- S = the S4 suspension set of this call site
    return <dummy>
}
jump join_k(S, r)                   -- the rest of the function, shared by both paths
```

- `join_k` es una función de nivel superior `(env, value)`. Su entorno es **exactamente el conjunto de suspensión de S4**: las variables poseídas se mueven dentro y las prestadas se `dup`ean solo en el camino lento. Lo que ayer alimentaba la frametable nativa hoy es la lista de capturas de un closure; la regla «nunca se infiere un préstamo a través de un prompt» es lo que hace correcto ese `dup` [R2 §3.2].
- `handle e with H` empuja un registro de handler (marcador, cláusulas y la evidencia bajo la que se definió) en la evidencia del contexto de la fibra, ejecuta el cuerpo y pasa por un `prompt`: en un retorno normal, quita el handler y aplica `return`; si está cediendo hacia *este* marcador, construye `Cont{frames}` y ejecuta la cláusula fuera del handler (semántica profunda); si cede hacia otro marcador, añade a la continuación un marco «reinstala este prompt» y sigue burbujeando.
- `perform L.op(v̄)` con cláusula de control busca el handler en la evidencia (desplazamiento estático si la fila es cerrada y canónica; búsqueda si no) y ejecuta `yieldTo(marcador, op, v̄)`.
- `resume(k, v)` comprueba one-shot, reinstala los prompts capturados en `k` (de fuera adentro) y ejecuta sus marcos de dentro afuera. Si el código reanudado cede otra vez, los marcos aún no ejecutados se añaden a la *nueva* continuación. El prototipo de R2 falló justo aquí la primera vez y R2 §12 P5 documenta la corrección.
- Las cláusulas tail-resumptive se ejecutan como llamada directa con la evidencia *enmascarada* al contexto exterior del handler, con un marco `restoreEv` para que una captura dentro de la cláusula restaure la máscara al reanudar. El oráculo nunca toma este atajo; la VM y el nativo lo activan en el capítulo 55, tras pruebas diferenciales.
- **Una sola S5, independiente del destino** **[decisión del diseño]**. `Async` cuenta siempre como «puede ceder», en los tres backends: la IR no sabe quién la ejecuta. (`IO` a solas ya no cuenta: toda operación de `IO` que aparca lleva `Async` en su fila, apartado 5.6, así que la regla por fila sigue siendo exacta y `print` deja de pagar la comprobación.) Lo que cambia por backend es qué hace la *raíz de la fibra* con un `perform Async.op` o `perform IO.op` (solo los `perform` de la fila, apartado 5.6; los `prim` como `ki_write` nunca ceden en ningún backend): en nativo (portador A) la operación burbujea hasta la raíz con el mismo mecanismo que un efecto de usuario, y la raíz devuelve `Park`; el oráculo, cuando ejecuta S5, hace **exactamente lo mismo** (su raíz de fibra es un prompt cuyo «handler» es su planificador, y `perform Async.op` es `yieldTo(raíz, op, v̄)`, con burbujeo y reproducción al despertar); en S0–S4 el oráculo aparca en el sitio, como siempre. Así `--check-passes` S4↔S5 compara aparcar-en-el-sitio con burbujear-hasta-la-raíz en cada canal, cada `sleep` y cada E/S bloqueante del corpus, que es justo el camino del que dependen los capítulos 53 y 54, y la promesa del apartado 6.3 («un error de S5 aparece al nivel de Core») es verdad también para `Async`. La VM aparca en el sitio por defecto y tiene un backend hermano, `vm-bubble`, que burbujea como el nativo (apartado 9.2). El borrador decía «solo en nativo bajo el portador A», lo que dejaba la suspensión más frecuente del sistema sin ejecutar hasta el capítulo 53 [T E7; constructor E1].
- La pureza es gratis: una fila cerrada `uses {}` no lleva comprobaciones, y tampoco una fila con `IO` a solas. Lo que sí las lleva antes del capítulo 55 es toda función polimórfica en efectos (`map`, `sum`, el `|>` desazucarado), porque su cuerpo se compila una vez con la variable de fila abierta. La **especialización de efectos** del optimizador (capítulo 55) **[decisión del diseño, 2026-10-28]** clona esas funciones por sitio de llamada cuando la fila instanciada no puede ceder (caché por función e instancia de fila, con el mismo presupuesto que el especializador de diccionarios del apartado 5.5), y el clon no lleva comprobaciones: `xs |> map(area) |> sum` en un programa sin `Async` queda tan limpio como si estuviera escrito a mano. Un test fija que el Core S5 emitido para ese programa no contiene ningún `isYielding`.
- Por qué después de Perceus y no antes, como hace Koka: S4 ya calcula los conjuntos de suspensión, así que S5 los reutiliza en lugar de rehacer el análisis de variables libres, y el censo de RC sobre S5 comprueba la propiedad de la propia transformación [R2 §3.2].
- Coste medido en el prototipo Go de R2: la comprobación en el camino directo no es medible (154 frente a 155 ns en una recursión de 100 niveles); una captura y reanudación cuesta unos 7 ns por marco burbujeado (65 / 135 / 803 ns a profundidad 1 / 10 / 100) [R2 §3.3].

Los mapas de llamada de la VM se traducen desde los conjuntos de suspensión, y cada slot lleva su clase: valor o token de reutilización. La frametable nativa de ayer ya no existe: una continuación es una cadena de closures en el heap y soltarla es un `drop` normal [R2 §1.2].

**Lint por etapa** comprueba ámbitos y aridades; la forma de los constructores; ANF desde S1; que un `jump` solo aparezca en posición de cola dentro del ámbito de su join, nunca cruzando un `fn` ni un `handle`; que los `case` sean completos; en S4, la linealidad (en cada camino, cada variable poseída se consume exactamente una vez); en S5, que toda llamada a una función que puede ceder vaya seguida de su comprobación y que el entorno de cada `join_k` coincida con el conjunto de suspensión de su sitio.

### 6.2 Semántica small-step de los handlers

Está escrita en `docs/semantics.md`, y el intérprete la implementa literalmente. S5 es una *traducción* de esta semántica, no otra semántica: el oráculo ejecuta S4 y S5 y exige los mismos observables.

**Configuración.** Valores: literales, constructores, closures y referencias a continuaciones κ. Contextos de evaluación: `E ::= □ | let x = E; e | #h{E}`, donde `#h{·}` es el marco de un handler. `E⁻ᴸ` es un contexto sin ningún marco que maneje L. El almacén σ asigna a cada κ el estado `Live(F)` o `Used`.

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

**Reglas del modelo.** Los handlers son profundos: F contiene `#h`, y la cláusula corre fuera de `#h`, así que los efectos que realiza van a los handlers exteriores. `op(x̄) => e` es `op(x̄, k) => resume(k, e)`, y `op(x̄), _ => e` suelta `k`. El camino rápido tail-resumptive (cláusula como llamada directa con el handler enmascarado) es equivalente porque κ se reanuda una vez y en el acto; la máscara la guarda y la restaura un marco, de modo que si un handler exterior captura dentro de la cláusula, al reanudar se restaura también. `perform IO.op` y `perform Async.op` llegan siempre a la raíz de su fibra y dan un paso con δ_IO y δ_Async. `force` es atómico en la semántica: el cuerpo es puro y no tiene puntos de planificación.

**Concurrencia.** La configuración se extiende a `⟨F₁ ∥ … ∥ Fₙ, σ, C, T, reloj⟩`, con canales C y temporizadores T. Spawn, las operaciones de canal, `select` y `sleep` son reglas δ sobre C y T. En cada punto de planificación, una función de elección decide la siguiente fibra y el caso de `select`; en modo determinista es la del apartado 8.8. Los efectos de usuario nunca cruzan fibras.

### 6.3 Qué es observable y cómo se comparan los backends

**Observable:**
- los bytes de stdout;
- la primera línea de stderr de un panic;
- el código de salida: 0; 1 si hay errores de compilación; 101 panic; 102 deadlock; 103 desbordamiento de pila (en nativo, con la regla del apartado 8.10);
- en modo determinista, la traza del planificador, con eventos semánticos (`spawn`, `run`, `block`, `wake`, `select-choice`, `timer-fire`, `exit`) y nunca mecánicos («burbujeados 7 marcos») [R2 §7];
- en los modos con RC comprobado, un censo de fugas igual a cero.

**Nunca observable, pero siempre un fallo:** cualquier salida con código **2** que no case *exactamente* con la firma del desbordamiento de pila de Go (apartado 8.10) es un fallo del generador de código o del runtime, y el runner lo hace fallar con ruido: los `fatal error:` (`missing stackmap`, `unexpected SPWRITE`, `invalid pointer found on stack`, `all goroutines are asleep`, `concurrent map writes`, `out of memory`) y también cualquier `panic:` de Go dentro de `kirt`, que sale igualmente con 2 [R2 §8.2; T §2.21] **[decisión del diseño]**.

**Floats entre plataformas.** Las operaciones básicas (`+ - * /` y `sqrt`) son exactas según IEEE en cualquier plataforma, y el formato de impresión (`kirt/fmtnum`) también. Las funciones trascendentes (`sin`, `exp`, `log`…) salen del paquete `math` de Go, que en algunas funciones usa ensamblador por arquitectura, así que pueden diferir entre amd64 y arm64. Un test que las usa lleva `// libm: platform` en su cabecera y solo se compara dentro de la misma plataforma **[decisión del diseño]**.

**No observable:** el momento de un drop, las direcciones, los tiempos y la profundidad a la que se desborda la pila. Si un backend se desborda y otro no, el resultado es inconcluso; y en nativo, un desbordamiento puede además perder hasta 64 KiB de stdout sin vaciar, porque el error fatal de Go no pasa por Ki (apartado 8.10). El cierre de un descriptor al soltarlo tampoco es observable: la red no entra en las pruebas diferenciales, y en modo determinista es un panic.

**Tres capas de comparación** **[decisión del diseño]**:
1. **Entre pases.** `ki test --check-passes` ejecuta S0, S1, S2, S3, S4 y S5 (S4 y S5 también sobre el heap comprobado) y exige observables idénticos. Un fallo queda localizado en el pase que lo introdujo. Un error de S5 (un conjunto de suspensión mal copiado, un prompt sin reinstalar, una máscara rota) aparece aquí, al nivel de Core, antes de que exista la VM o el nativo, **también en los sitios que solo ceden por `Async`** (la E/S que aparca incluida, porque lleva `Async` en su fila), porque el oráculo en S5 los burbujea hasta la raíz de la fibra igual que el nativo (apartado 6.1) [R2 §8; T E7].
2. **Entre backends.** `interp`, `interp-rc`, `vm`, `vm-bubble` y `native` ejecutan el mismo programa con `--sched=det --seed=s` para muchas semillas, y la salida debe ser idéntica byte a byte. `vm-bubble` es la VM burbujeando `Async` e `IO` hasta la raíz como el nativo, con el runtime real debajo: es el oráculo de depuración más cercano al código nativo. Los programas multinúcleo solo se comparan si su cabecera declara `schedule: independent`.
3. **Entradas.** El corpus escrito a mano en cada capítulo; `kigen`, un generador de programas bien tipados (ADTs, closures, recursión con combustible, las tres formas de cláusula, continuaciones guardadas y abandonadas en cada `perform`, pereza, canales y `select` en modo determinista, **bloqueos de canal a profundidad variable** (para que el burbujeo de `Async` cruce 1, 10 y 100 marcos) y **bucles largos sin asignar**, que es lo que detecta un poll que falta [R3 §5.2]); los fallos se encogen por delta debugging y se quedan para siempre en `tests/diff/corpus/`.

**Puntos ciegos, y qué los cubre.** Un fallo del front end que comparten todos los backends: las salidas golden escritas a mano, los tests golden de errores y los tests de propiedades de ida y vuelta del parser. Un fallo compartido por la VM y el nativo (comparten S5 y `kirt`): el planificador y el modelo de canales propios del intérprete, escritos desde la misma especificación pero de forma independiente, más los tests unitarios, de estrés y con `-race` del runtime.

---

## 7. El compilador en Go

### 7.1 Etapas

| # | Etapa | Entrada → salida | Responsabilidades |
|---|---|---|---|
| 0 | Driver | argv, ficheros → sesión | `ki run`, `build`, `check`, `test`, `emit <etapa>`, `explain`, `doctor`, `fmt` y `clean`. Elige el backend, gestiona los volcados, limita el número de errores y fija los códigos de salida |
| 1 | Lexer | bytes UTF-8 → tokens `{Kind, Span, payload}` | Maximal munch; regla de línea con pila de corchetes; modos para la interpolación; comprobación de literales; tokens de error, y sigue adelante |
| 2 | Parser | tokens → AST en `IdVec` | Descenso recursivo y Pratt; búsqueda acotada para las lambdas; restricción de literales de record; nodos Error y resincronización en separadores, `}` y palabras clave de item |
| 3 | Grafo de módulos y resolución de nombres | ASTs → DefTable y tablas laterales | Ficheros e imports; ciclos; preludio; ámbitos y visibilidad; `a.b` cuando `a` nombra un módulo; «¿quisiste decir…?»; dónde pueden aparecer `var`, `return` y `break`; captura de `var` prohibida |
| 4 | Declaraciones y kinds | items → entornos de tipos, traits, impls y efectos | Kinds, ciclos de alias, expansión de `derive`, coherencia y huérfanos, grafo de dependencias de las constantes |
| 5 | Tipos y efectos | cuerpos (por SCC) → tipo de cada nodo, evidencia y filas | HM bidireccional con niveles, unificación de filas, regla ambiente, resolución de traits, búsqueda de métodos por tipo, generalización, procedencia de cada restricción |
| 6 | Patrones | matches tipados → diagnósticos | Exhaustividad y redundancia (Maranget), testigos |
| 7 | Elaboración | AST tipado → Core S0 | Azúcar fuera; paso de diccionarios; `perform`, `handle` y `resume` explícitos; bucles a join points; orden de inicialización. Solo corre sobre programas sin errores |
| 8 | Pases de Core | S0 → S1 → S2 → S3 | ANF, árboles de decisión, lambda lifting y conversión de closures. Más tarde, el optimizador (cap. 55). Lint después de cada pase |
| 9 | Perceus | S3 → S4 | Inferencia de préstamos, dup/drop, especialización de drop, reuse, conjuntos de suspensión, Lint de linealidad |
| 10 | Evidencia (EV) | S4 → S5 | Marcas «puede ceder» por fila; comprobaciones `isYielding`; `join_k` levantados con los conjuntos de suspensión como entorno; prompts; Lint de S5 |
| 11 | Intérprete | cualquier Sx → ejecución | El oráculo (apartado 9.1) |
| 12 | MIR | S5 → SSA de primer orden con parámetros de bloque | Asignación, campos, tags, RC, evidencia y llamadas al runtime explícitos; **verificador de MIR**, que exige además la regla de los puntos seguros del apartado 9.3: un poll en cada arista de retroceso sin prólogo, al menos un `CALL` (o un marco ≥ 128 bytes) en toda función con llamada de cola o bucle, y ninguna llamada de cola desde un punto de entrada |
| 13 | Emisor de bytecode | MIR → módulo | Asignación de registros por liveness, pool de constantes, mapas de llamada con clases de slot, verificador y desensamblador |
| 14 | Backend x86-64 | MIR → `prog_amd64.s` | Selección de instrucciones, liveness, linear scan y su verificador, marcos ABI0 con el área de salida del programa, llamadas de cola estáticas e indirectas, polls y la comprobación de `preempt`, `#line` |
| 15 | `ki build` | `.s` + `kirt` → ejecutable | Escribe el módulo generado (`go.mod`, `main.go`, `ki_gen.go`, `kiabi.h`, el `.s`) y la copia de `kirt`, importando solo los subpaquetes de `kirt` que las filas de efectos del programa exigen (sin `Async`, ni planificador ni poller: apartado 9.3), y ejecuta `go build`; los fallos de `go` se convierten en diagnósticos (apartado 9.3) |
| 16 | Test runner | tests → informe | Golden, matriz backend × semilla, `--check-passes`, entrega al reductor; ejecuta binarios construidos, nunca `go run`, porque `go run` enmascara el código de salida [R4 V13] |

### 7.2 Estructuras de datos y estilo de Go

**Ids y almacenamiento.**
- Todo se direcciona con ids de 32 bits tipados: `type ExprID uint32`, `type TypeID uint32`, `type DefID uint32`… son *defined types* (sufijo `ID`, como manda la guía de estilo de Google), así que el compilador de Go no deja mezclarlos.
- Cada id indexa un `IdVec[I ~uint32, T any]`, un envoltorio genérico de `[]T` que solo se puede indexar con su propio tipo de id, como el `IndexVec` de rustc.
- Las listas de hijos de los nodos del AST y de Core son rangos `{first, count}` sobre pools compartidos; las listas que no son de nodos (`Attr.Args`, `Type.Params`, `Row.Labels`, `core.Fun.Params`) son slices, inmutables tras construirse.
- Nada de punteros entre nodos. En un lenguaje con GC la razón ya no es la propiedad, sino la compacidad (estructuras sin punteros que el GC no recorre), las tablas laterales y los volcados estables [R4 §8 cap. 6].

**Nodos.** Son structs pequeños con un campo `Kind` en un `IdVec` por categoría, y se despachan con `switch` sobre el `Kind`. Donde la forma «un tipo por caso» es más clara (los valores y los marcos del oráculo), se usa una **interfaz sellada** (`type Frame interface{ frame() }`, con un método no exportado que cierra el conjunto) y un *type switch* [R2 §2.1]. Go no comprueba la exhaustividad de un type switch; cada uno termina en `default: panic("unreachable")`, y `cmd/kicheck` tiene como ejercicio *stretch* un comprobador de exhaustividad sobre `go/types` **[decisión del diseño]**.

**Arena.** Slabs de 64 KiB de tipos sin punteros, con asignación por desplazamiento. Sirve para la localidad y para no pagar una asignación por nodo, no para la liberación: eso lo hace el GC de Go.

**Interner.** Los bytes viven en la arena; el índice es un `map[uint64]Symbol` sobre `hash/maphash` con cadenas de colisión, porque un `map[string]Symbol` sin `unsafe` copiaría cada texto al internarlo. Se escribe a mano, y el capítulo 4 lo compara con `unique.Make` de la biblioteca estándar.

**TypeTable.** Los tipos están hash-consed (claves struct comparables), así que la igualdad de tipos es igualdad de ids. Un union-find sobre slices guarda `{parent, rank, level, kind, Origin}` por variable. `Origin` (span, razón y nodo relacionado) es lo que produce las notas «porque…».

**SourceManager.** Contenido de los ficheros, tablas de líneas construidas bajo demanda y `Span{FileId, lo, hi}`.

**Estructuras propias.** `SmallVec` (array en línea más slice) y `BitSet` (`[]uint64` con `math/bits`), escritas a mano.

**Reglas de estilo** **[decisión del diseño]**:
- `gofmt` sin discusión; `go vet` limpio en dos invocaciones, porque `-unsafeptr=false` no se puede limitar a un paquete dentro de `./...`: `go vet $(go list ./... | grep -v /kirt/heap$)` y `go vet -unsafeptr=false ./kirt/heap` (`ci/ci-local.sh` las encapsula) [R3 §2.4]; nombres según la guía de estilo de Google (`syntax.Lexer`, no `syntax.SyntaxLexer`).
- Los pases no mutan su entrada.
- Ninguna salida depende del orden de iteración de un `map`, que Go aleatoriza a propósito [R4 V29]; se ordena por id.
- `unsafe` solo en `kirt/heap` y en sus `_test.go` (apartado 8); goroutines solo en `kirt` (workers, poller, pool, monitor), en el test runner (paralelismo entre procesos) y en los tests. El compilador es secuencial en v0.
- Un diagnóstico es un valor que va a un `DiagSink`; `error` se reserva para la E/S y las herramientas (`os`, `exec`, `go build`); `panic` solo para errores internos del compilador, que un `recover` en la frontera de cada pase convierte en un informe con `runtime/debug.Stack()` y el código `ICE`. Un panic **de Ki** nunca es un `panic` de Go: en el oráculo es un estado `Halt{code: 101}` de la máquina CEK, en la VM un retorno del bucle y en nativo una llamada al trampolín `kiPanic`, los tres con el mismo vaciado de `kirt/out` y el mismo `os.Exit(101)` **[decisión del diseño]**.
- Los tipos «solo movibles» de C++ (una `Cont` que se consume) se vuelven un flag `used` más un campo `noCopy` que `go vet` (copylocks) vigila [R4 V30].
- Las funciones calientes se escriben en dos mitades, rápida (inlineable, coste ≤ 80) y lenta (una llamada), y la CI comprueba con `-gcflags=-m` que las nombradas siguen inlineándose [R3 §3.6].

### 7.3 Diagnósticos

Un diagnóstico es un valor:

```go
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
```

**Códigos estables** **[decisión del diseño]**: `E01xx` lexer, `E02xx` parser, `E03xx` nombres y módulos, `E04xx` tipos, `E05xx` traits, `E06xx` efectos, `E07xx` patrones, `E08xx` kinds, `W…` warnings. `ki explain E0401` imprime `docs/errors/E0401.md`, incrustado en el binario con `//go:embed` desde el `embed.go` de la raíz del módulo (apartado 11.1).

**Renderizadores:** humano, con color solo en una terminal (el color llega con `ki explain` en el capítulo 19; hasta entonces el renderizador humano no usa color); corto (`file:line:col: error[E0401]: msg`); líneas JSON con `encoding/json` (`json/v2` sigue siendo experimental en 1.26.5 y no se usa [R4 V25]).

Los tests comprueban códigos y spans; el texto renderizado tiene su propio conjunto pequeño de golden en `testdata/`, regenerable con `-update`.

**Errores legibles:** cada llamada a `unify` lleva una razón; los tipos se imprimen con los nombres del usuario; los mensajes nunca muestran diccionarios, niveles ni variables de inferencia; hay como mucho una línea de ayuda, con fix-it si existe.

**El listón: errores al nivel de rustc** **[tu decisión, 2026-10-28; el detalle es del diseño]**. Es una restricción global desde el capítulo 7 (sesión `ch07-s4`), para todos los códigos que existen y para cada uno que nazca después, y la vigilan los revisores de cada sesión y el panel de cada capítulo. Un código cumple el listón cuando:
1. **La etiqueta primaria dice qué está mal en ese sitio**, no solo dónde: «expected `)`», «not found in this scope», «this `{` is never closed». Nunca una primaria sin texto.
2. **Las etiquetas secundarias señalan el otro sitio que explica el error** cuando existe: «unclosed `(` opened here» en la apertura que dejó a un `)` sin pareja, «first defined here» en la primera definición, «the parameter is declared here» en el parámetro, «`x` is bound by this `let`, which cannot see itself» en el `let` cuyo inicializador lo usa.
3. **Una nota explica la regla** cuando el mensaje solo no la hace evidente: «the largest `Int` is 9223372036854775807», «a `let` is visible only after its own statement».
4. **La ayuda propone una acción concreta** siempre que exista una («close it with `)`», «write `await f(x)`», «did you mean `fib`?»), y **no existe ayuda genérica**: cuando no hay nada concreto que proponer, no hay ayuda, porque una ayuda que no ayuda enseña a ignorarlas.
5. **Un fix-it cuando la corrección es mecánica**: insertar el delimitador que falta, borrar el atributo que sobra, sustituir el nombre por el sugerido, duplicar la barra de un escape desconocido. Un fix-it siempre acompaña a una ayuda que lo explica: el renderizador exige la ayuda (un parche sin explicación no es una sugerencia).
6. **Una entrada `docs/errors/EXXXX.md`** con la explicación del error, un ejemplo erróneo y su versión corregida, los dos probados: un test recorre el catálogo y exige que cada ejemplo erróneo produzca exactamente su código y que cada corregido pase `ki check` limpio. El catálogo nace en el capítulo 7 con los códigos de la Parte I y crece con cada código; `ki explain` lo incrusta y lo imprime desde el 19.
7. **Un código nace completo**: la sesión que lo introduce lo entrega con los seis puntos y sus tests. Ningún código se publica «para pulirlo después».

**Fix-its como sugerencias de rustc** **[decisión del diseño, 2026-10-28]**. Un `FixIt{Span, Replacement}` es una edición: sustituir el texto del span por `Replacement` (span vacío: inserción; `Replacement` vacío: borrado). Cada fix-it cae dentro de una sola línea del fichero de la primaria; otra cosa es un error del compilador y el renderizador hace panic, como con una secundaria en otro fichero. El renderizador humano los muestra en la forma «diff» de rustc, debajo de la línea de ayuda, con las mismas reglas de visualización que las líneas de código (`display`, anchos, ventana): un marco `" "*g + " |"`, y por cada línea afectada, en orden, `rjust(line, g) + " - " + display(original)` y `rjust(line, g) + " + " + display(parcheada)`, con los fix-its de esa línea aplicados de derecha a izquierda. El ejemplo del apartado 7.3 termina así:

```text
   = help: write `2.0`; Ki never converts Int to Float implicitly
   |
12 -     Rect(w, h) => w * 2
12 +     Rect(w, h) => w * 2.0
```

El renderizador JSON añade la clave `fixits` tras `help`, siempre un array, con objetos `{"line","col","end_line","end_col","replacement"}` (posiciones de 1 en adelante, columnas en code points, el final exclusivo). El renderizador corto no los muestra. Los tests de un código con fix-it comprueban el span y el texto de la sustitución, y los golden humanos, el diff.

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

**Recuperación:** el lexer salta los bytes inválidos; el parser resincroniza e inserta nodos Error, sin emitir errores nuevos a menos de 3 tokens del anterior; los nombres desconocidos se ligan a una definición de error, y `?err` unifica en silencio; cada item de nivel superior se comprueba por separado; el front end se detiene tras 100 errores, y las etapas posteriores solo corren sobre entrada sin errores.

---

## 8. El runtime: kirt

`kirt` es un paquete de Go (con subpaquetes) dentro del módulo `kilang`, en la raíz del repo y no bajo `internal/`, porque los ejecutables nativos son módulos generados que lo importan (apartado 9.3) [R4 §5.7]. Lo usan la VM, dentro del proceso `ki`, y los ejecutables nativos, que `go build` enlaza con él. Tres reglas lo gobiernan, las tres comprobadas por un test que hace `grep` sobre los imports **[decisión del diseño]**:
- **`kirt` solo importa la biblioteca estándar y otros paquetes de `kirt`.** Así la copia que `ki build` escribe en el directorio de construcción es autocontenida.
- **Solo `kirt/heap`, y sus `_test.go`, importan `unsafe`.** Es el único sitio donde un entero se convierte en puntero [R3 §2.4]. La regla tiene dientes **[decisión del diseño]**: `alloc`, `rc`, `str`, `array` y la VM no ven nunca un `unsafe.Pointer`; `kirt/heap` exporta `Load`/`Store` de palabras por desplazamiento, `AtomicAdd32`/`CAS64` sobre desplazamientos, `Bytes(v, n)` (el `[]byte` sobre memoria propia para escribir un `String`) y los *overlays* tipados (`heap.AllocCtx(p)`, `heap.FiberCtx(p)`), y el capítulo 38 enseña el patrón `(*atomic.Int32)(unsafe.Pointer(p))` **dentro** de `kirt/heap`. Lo demás que necesita `unsafe` vive ahí también: el experimento del futex crudo del capítulo 44 (`Syscall6(SYS_FUTEX, uintptr(unsafe.Pointer(&w)), …)`), que es un benchmark en `kirt/heap/futex_linux_test.go`, y los canarios C7 y C8, que guardan la dirección de un objeto de Go en memoria de Ki. El ejercicio de layout del capítulo 11 usa `reflect` (`Type.Size`, `StructField.Offset`), no `unsafe.Sizeof`.
- **El oráculo solo importa `kirt/splitmix`, `kirt/fmtnum` y `kirt/out`.** Son tres hojas sin planificador ni heap: el generador determinista, el formato de números y el escritor de stdout/stderr (buffer, regla de vaciado y un mutex). Compartir el escritor evita diferencias falsas en el observable principal, y ninguno de los tres tiene lógica concurrente que el oráculo deba contrastar; si usara el planificador o los canales de `kirt`, no podría encontrar sus fallos **[decisión del diseño]**.

**No hay `thread_local` y tampoco hay almacenamiento local por goroutine en Go** (decisión de diseño del lenguaje: ni ids de goroutine ni TLS). El contexto de allocator y el de la fibra se pasan explícitamente (el segundo apunta al primero), y en el código nativo se recargan desde el marco después de cada llamada, porque el ABI de Go no tiene registros preservados por el llamado [R1 §5.3, R3 §5.1]. Misma decisión que ayer, por una razón nueva.

**Workers.** Son `GOMAXPROCS` goroutines normales. **Nunca `runtime.LockOSThread`**: con los dos extremos fijados a hilos, un traspaso entre goroutines costó 126 µs frente a 217 ns sin fijar, unas 580 veces más [R2 §5.4]. Fijar no compra ninguna corrección, porque Ki no depende de estado por hilo del sistema.

### 8.1 Representación de valores

```go
type Value uint64 // bit 0 = 1: immediate; bit 0 = 0: address of an 8-aligned Ki-heap object (never 0)
```

- **Bit 0 = 1: inmediato.** Un `Int` que cabe en 63 bits se codifica como `(n << 1) | 1`. `Bool`, `Char`, `Byte`, `()` y los constructores sin campos son índices pequeños codificados igual. El orden con signo se conserva, así que `<` entre dos inmediatos es una comparación con signo.
- **Bit 0 = 0: puntero** a un objeto del heap de Ki alineado a 8 bytes. Es una **dirección absoluta** **[decisión del diseño, R3 §11.1]**, no un desplazamiento: una sola instrucción de direccionamiento en amd64 y arm64, y la misma codificación en el heap de depuración.
- **Regla 1 de R3.** Un valor de Ki es `Value`, un entero, en todo el código Go: structs, slices, locales, registros de la VM. Nunca `unsafe.Pointer`, y nunca un inmediato en una variable de tipo puntero: un `0x3` en un slot de puntero mata el proceso cuando la pila de la goroutine crece (`invalid pointer found on stack`), y solo a veces [R3 V1]. `unsafe.Pointer` existe solo como transitorio dentro de los accesores de `kirt/heap`.

**`Int` de 64 bits en una palabra uniforme** **[decisión del diseño, para cumplir tu elección sin su coste]**: un `Int` que cabe en 63 bits es siempre inmediato y uno que no cabe es siempre un `BoxedInt`; la forma es canónica. La aritmética tiene un camino rápido en línea (dos inmediatos y el resultado cabe), y si no, una función del runtime calcula en 64 bits, normaliza y hace panic si desborda los 64 bits. En Go el camino rápido es `r = a + (b-1)` con la prueba de desbordamiento `((a^r) & ((b-1)^r)) < 0`, partido en `addFast` (inlineable) y `addSlow`: inlineado cuesta 0,79 ns por suma frente a 0,53 del `int64` crudo y 1,48 sin inlinear, así que **inlinear importa más que la etiqueta** [R3 §3.6]. El capítulo 56 quita además el chequeo de etiqueta en el código monomórfico. `UInt64` usa el mismo esquema.

**Cabecera de 8 bytes**, con la codificación de Lean 4 (`m_rc` de `lean.h`) [R3 §3.2]:

```go
type header struct {
	rc   int32  // >0 fiber-local (plain ops); <0 thread-shared, magnitude -rc (atomic); 0 immortal
	kind uint8  // Ctor, Closure, String, Array, BoxedInt, BoxedFloat, Thunk, Cont, Chan, Resource, (debug) Freed
	tag  uint8  // constructor tag
	scan uint16 // number of Value fields from the kind's first scanned field
}
```

**Kinds** [R3 §3.3, R2 §3.2]:

| Kind | Palabras tras la cabecera | Notas |
|---|---|---|
| Ctor | `f0 … f(n-1)` | `scan = n`; la especialización de drop evita leer `scan` |
| BoxedInt | `int64` | solo fuera de ±2⁶²; un inmediato y una caja nunca son iguales |
| BoxedFloat | bits de `float64` | empaquetado en v0; `math.Float64bits` |
| String | `len`, bytes, relleno a 8 | para escribir se pasa un `[]byte` construido con `unsafe.Slice` sobre memoria no Go, que el GC ignora; **nunca** se crea un `string` de Go que pueda sobrevivir al String de Ki |
| Array | `len`, `cap`, Values | usa `len`, no `scan` |
| Closure | `code`, `info` (aridad, flags), capturas | las capturas empiezan en la palabra 2; `code` es un índice de función en la VM y una dirección de código en nativo |
| Thunk | `state`, `fn`, `result` | publicado por CAS sobre `state` |
| Cont | `state` (Live/Used), cadena de entornos `join_k`, lista de prompts a reinstalar | la forma que S5 impone: closures ordinarios en el heap, soltar es `drop` [R2 §3.2]. No hay segmentos de pila |
| Chan, Resource | `handle` | índice en una **tabla del lado Go**; el último drop del proxy libera la entrada |

**Nunca un puntero de Go dentro del heap de Ki.** Un objeto de Go cuya única referencia vive en el heap de Ki se recoge: R3 lo observó con `runtime.AddCleanup` [R3 V7]. Por eso los canales, los ficheros y los temporizadores viven en tablas del lado Go indexadas por entero, y los structs que se superponen a memoria de Ki no contienen punteros, slices, maps, strings, interfaces ni funcs; un test con `reflect` lo comprueba [R3 §3.4].

**Una sola fuente de verdad para el layout.** `kirt/layout` es un paquete de constantes (desplazamientos y tamaños) sin `unsafe`. Un test en `kirt/heap` las compara con `unsafe.Offsetof` y `unsafe.Sizeof`, y el backend nativo las escribe en el `kiabi.h` del módulo generado, porque el `go_asm.h` que genera el ensamblador solo cubre el propio paquete [R1 §1.6, R3 §3.2].

**Representación uniforme.** En v0, `Float` va empaquetado, así que el código polimórfico no necesita monomorfización. El capítulo 56 añade `i64` y `f64` crudos en el código monomórfico, usando las anotaciones `rep`.

**Objetos inmortales.** Son inmortales los literales, los closures estáticos, los diccionarios sin contexto y las constantes ya inicializadas. **Inmortalizar es un recorrido, no un bit** **[decisión del diseño, T §2.16]**: como el `mark_persistent` de Lean, pone a `rc = 0` todo lo alcanzable desde el objeto, igual que `share` recorre todo lo alcanzable. Si solo se marcara la raíz, un objeto *local* con hijos locales podría llegar a otra fibra (porque `share` se para en lo inmortal) y sus hijos recibirían `dup`/`drop` planos desde dos fibras: contador corrupto y use-after-free, no una fuga. Por la misma razón, un contador que llega a `INT32_MAX` primero comparte el objeto (`share`) y luego lo inmortaliza con el recorrido: una fuga acotada, nunca un use-after-free. Las constantes inicializadas en tiempo de ejecución se inmortalizan al terminar su inicialización y viven en el heap de Ki, no en `RODATA` (apartado 9.3). Hay una razón nueva para hacer inmortales los valores muy compartidos: dieciséis workers haciendo `dup`/`drop` atómicos sobre el *mismo* objeto costaron 119 ns por operación, frente a 1,8 ns con uno solo y 3,8 ns con objetos en líneas de caché distintas [R3 §4].

### 8.2 Perceus en el runtime

**Caminos rápidos y lentos.** El código generado y la VM incluyen en línea el caso local de `dup` y `drop` (`if rc > 0 { rc++ } else if rc != 0 { atomic }`; el inliner de Go le da coste 34 de 80 [R3 §3.5]). El runtime atiende los objetos compartidos (`atomic.AddInt32`, que compila a `LOCK XADDL`, la misma instrucción que usaría un `acq_rel`) y la liberación. Liberar es iterativo: la lista de trabajo va enhebrada dentro de las propias celdas liberadas, así que soltar una lista de 10⁷ elementos no desborda ninguna pila [R3 §3.5].

**Compartición.** `share(v)` corre en `spawn`, en `send` y al publicar un thunk compartido. Recorre `v` con una pila explícita y pasa cada objeto local alcanzable a compartido (`rc → -rc`). Se para en lo que ya es compartido o inmortal, porque un objeto compartido nunca apunta a uno local. Si encuentra una continuación, hace panic.

**Reutilización.** Solo cuando `rc == 1`. Reutilizar una celda compartida rompería el invariante «lo compartido no apunta a lo local» **[decisión del diseño]**. Medido: `map (+1)` sobre una lista única de 10⁶ elementos tardó 1,35 ms con reutilización, 3,27 ms sin ella y 22,2 ms con objetos del GC de Go [R3 §3.5].

**Migración.** Que una fibra migre de worker no exige atómicos en sus objetos locales: solo esa fibra los toca, y cada traspaso (un robo de deque o el cerrojo de un canal) es una arista de sincronización. En Go todos los atómicos son secuencialmente consistentes, así que la arista existe sin más [R3 §4].

**Atómico frente a no atómico, medido** [R3 §4]: `rc++` plano 0,69 ns; con la comprobación de signo de Lean sobre objetos locales 0,69 ns (la comprobación es gratis); todo compartido sin contención 2,47 ns; siempre atómico 3,21 ns.

### 8.3 Allocator

Sigue el diseño de mimalloc, escrito a mano sin tomar su código, sobre el paquete `syscall` [R3 §9].
- **Reserva.** Una sola `syscall.Mmap(PROT_NONE, MAP_PRIVATE|MAP_ANON|MAP_NORESERVE)` de `KI_HEAP_GB` (64 GiB por defecto), que nunca se desmapea. Las arenas de 4 MiB se comprometen con `Mprotect(RW)` y se devuelven con `Madvise(MADV_DONTNEED)`. Verificado: 64 GiB `PROT_NONE` cuestan 0 de RSS; comprometer y tocar 4 MiB añade 4 MiB; `DONTNEED` los devuelve [R3 V9]. `Munmap` solo acepta el slice exacto que devolvió `Mmap`, por eso una reserva única [R3 §2.6].
- **Arenas y páginas.** Páginas de 64 KiB; `ptr &^ 0xFFFF` encuentra la cabecera de página: `{clase, free list local, free list atómica thread_free, dueño, usados}`.
- **Clases de tamaño.** Unas 40: de 8 en 8 bytes hasta 128, y luego cuatro por potencia de dos hasta 32 KiB. Los objetos mayores ocupan páginas de 64 KiB contiguas **dentro de la misma reserva**, con su cabecera en la primera, de modo que `ptr &^ 0xFFFF` sigue encontrando una cabecera de página y nada se desmapea nunca (un mapeo propio chocaría con la reserva única y con el `Munmap` exacto) **[decisión del diseño, T §2.17]**.
- **Contexto de allocator en memoria propia.** Guarda las cabezas de free list por clase, los punteros bump y límite y la contabilidad de páginas; **no** guarda el flag de preempción, que vive solo en el contexto de la fibra (apartado 8.4). Hay un *pool* de más de `GOMAXPROCS` contextos, porque una goroutine que ejecuta Ki puede quedarse sin *slot* de worker y seguir asignando (apartado 8.5, capa 3): el contexto de allocator pertenece a la goroutine que ejecuta, el slot al planificador, y son objetos distintos **[decisión del diseño, T E8]**. Go lo ve como el overlay `heap.AllocCtx(p)` con campos enteros; el ensamblador llega a él por el contexto de la fibra.
- **Camino rápido en línea**, en Go y en ensamblador; el camino lento (`refill`) es una función de Go. Medido desde ensamblador: 2,04 ns por celda con el pop en línea frente a 3,56 ns llamando a Go en cada asignación [R3 §5.3].
- **Liberación.** El worker dueño libera en la lista local. Los demás hacen CAS sobre `thread_free` con `atomic.CompareAndSwapUint64` sobre una palabra del heap propio, y el dueño recoge esa lista cuando la local se vacía.
- **Contabilidad.** `GOMEMLIMIT` y el *pacer* del GC de Go no ven el heap de Ki [R3 V7]. Por eso existen `KI_HEAP_MAX` y métricas propias; el RSS del proceso es la memoria del runtime de Go más las arenas comprometidas.

**Modos** **[decisión del diseño, R3 §6]**. Ningún sanitizer de Go ve memoria que no es de Go: `-race` ignora sus direcciones y sus atómicos ni siquiera crean *happens-before* (y dan **falsos positivos** si publican estado de Go) [R3 V3]; `-asan` y `-msan` necesitan cgo; `checkptr` solo se queja de lo que es memoria de Go. Así que Ki construye sus propias redes desde el primer capítulo de runtime:
- `KI_ALLOC=debug`: sitios de asignación con **procedencia Ki, no Go** **[decisión del diseño, T §2.22]**: `runtime.Callers` daría la posición Go del bucle de la VM, no el sitio Ki que asigna, así que la VM registra el pc del bytecode (y su tabla de líneas lo traduce a `file:line:col` de Ki), el ensamblador pasa un id de sitio inmediato, y `runtime.Callers` queda para las asignaciones que hace el propio código Go de `kirt`; `kind = Freed` y envenenado con `0xDD` **salvo la palabra `rc`** (un `0xDDDDDDDD` parecería un contador compartido válido), cuarentena FIFO, doble free detectado, y un **censo al salir** por sitio de asignación que debe ser cero y nombra los ciclos de canales.
- `KI_ALLOC=fence`: un objeto por página, la página `PROT_NONE` al liberar y `debug.SetPanicOnFault(true)` en cada goroutine worker; una lectura de un objeto liberado se vuelve un panic recuperable cuyo `Addr()` es la dirección exacta del objeto, que el handler traduce a su sitio de asignación [R3 V6].
- **Heap de depuración en el heap de Go** (build tag `kiheapgo`): el mismo allocator sobre un `[]uint64` fijado con `runtime.Pinner`, con **la misma codificación de direcciones absolutas**, de modo que ningún otro código cambia; un único accesor `ptr` marcado `//go:nocheckptr`, que en depuración además hace panic ante un inmediato. Con él, `-race` vuelve a ver la VM y el runtime, y los atómicos del heap de Ki sincronizan de verdad [R3 V8]. Reserva de 4 GiB: 2,8 ms y 0 RSS mientras no se toca [R3 V9]. El `KI_ALLOC=system` de ayer desaparece: no hay ASan al que reenviar.

### 8.4 Fibras, contextos y continuaciones

**Lo que el runtime de Go prohíbe al código nativo** (todo verificado por R1 y R2, cada uno con su programa que muere):
- mover `SP` fuera del marco, `PUSH`/`POP` o `ADJSP`: la función queda marcada `SPWRITE` y el siguiente recorrido de pila del GC es fatal (`traceback: unexpected SPWRITE`) [R1 §2.6, R2 P10];
- cambiar a una pila que el runtime no posee: por debajo de la pila de la goroutine, `split stack overflow`; por encima, fatal en el primer GC [R1 §2.7];
- guardar la dirección de un slot de pila a través de una llamada: la pila se **copia** al crecer, y la lectura vuelve a la pila antigua [R1 §2.6];
- esperar que un registro sobreviva a `morestack`: solo `DX` sobrevive, y solo en funciones `NEEDCTXT` [R1 §2.5].

Luego el modelo de ayer (segmentos `MAP_NORESERVE`, `ki_switch`, una `Cont` con una cadena de segmentos y un `rsp` guardado) **no puede ejecutarse bajo `go build`** [R1 §2.7, R2 §1]. Toda suspensión nativa es *sin pila*: los marcos se desenrollan en objetos del heap. Eso es S5.

**Portador A: fibras como estados en el heap sobre el planificador de Ki** **[decisión del diseño, R2 §4.2–4.3]**.
- Una fibra del oráculo es un estado CEK. Una fibra de la VM es un estado de la VM (`regs []Value` y `frames []vmFrame`, slices de Go sin punteros) y aparca *en el sitio*, devolviendo `Park` al bucle del worker. Una fibra nativa es una **continuación raíz**: las operaciones bloqueantes de `Async` e `IO` son efectos cuyo handler es la raíz de la fibra, exactamente lo que la semántica ya decía, así que burbujean hasta ella con el mecanismo de S5 y la raíz devuelve `Park`.
- La interfaz es una para los dos backends compilados: `Run(fiber) → Park | Done | Panic`. Un solo planificador, un solo modelo de canales y `select`, un poller, un detector de deadlock y un `DetScheduler` sirven a la VM y al nativo.
- Despertar una fibra es empujarla a un deque. Ejecutarla, en nativo, es que el worker llame a la entrada `kiResume(ctx, fiber, value)`, que reproduce los closures sobre la pila de la goroutine del worker.
- Costes: una fibra suspendida ocupa sus closures, no una pila; `spawn` es una asignación más un push (14–22 ns por fibra en el árbol de 635 621 fibras de R2, frente a 77–218 ns con una goroutine por fibra); bloquearse cuesta O(profundidad) (65 / 135 / 803 ns a profundidad 1 / 10 / 100) [R2 §3.3, §5.2]. A profundidades realistas (~10) empata con un cambio de goroutine (~110 ns); pierde con pilas profundas que se bloquean a menudo [R2 §4.3].
- El coste que importa: **el código puro no puede ser preemptado a nivel de Ki** bajo A, porque no tiene código de burbujeo. El apartado 8.5 explica la mitigación.

**Por qué A y no B.** Tu decisión («el runtime lo escribes tú: planificador multinúcleo con work stealing») y la coherencia entre backends apuntan a A. Bajo B (una goroutine por fibra, el planificador de Go) el work stealing que escribes solo existiría en la VM [R2 §4.3]. B queda como **respaldo documentado**: la interfaz del *parker* de `kirt` (`park(f, unlock)` / `ready(f)`) admite las dos implementaciones, así que B es un adaptador y no una reescritura, y el capítulo 58 lo mide como sección opcional («¿y si las fibras fueran goroutines?») [R2 §4.3, §10].

**El contexto de la fibra** **[decisión del diseño, R2 §3.1 y R1 §11.2]**. Es un objeto del heap de Ki con: el vector de evidencia (los handlers instalados), el flag `yielding` y el destino del yield, los marcos acumulados de la continuación en construcción, el flag `preempt` de Ki (el **único** sitio donde vive; el monitor lo escribe con un atómico y el código Go lo lee con otro, o `-race` con `kiheapgo` protestaría) y un puntero `alloc` al **contexto de allocator** de la goroutine que la está ejecutando (free lists, apartado 8.3). En nativo viaja como **argumento oculto 0** de toda función Ki (una dirección del heap de Ki, nunca un inmediato) y se recarga desde el marco después de cada llamada. `kiResume` escribe `ctx.alloc` con el contexto de allocator de *su* goroutine antes de reproducir la continuación; es seguro porque una fibra solo migra mientras está aparcada y un contexto de allocator nunca lo usan dos goroutines a la vez (apartado 8.5, capa 3) [T E8]. Sin este puntero el camino rápido del allocator en ensamblador no tendría de dónde sacar celdas.

**Continuación.** Es un objeto `Cont` que posee la cadena de entornos `join_k` desde el `perform` hasta el handler incluido, más los prompts que hay que reinstalar y su estado `Live`/`Used`. Capturar es burbujear; reanudar es reinstalar y reproducir; soltar una `Cont` viva es un `drop` ordinario de Perceus que libera exactamente lo que cada entorno posee, sin ejecutar código Ki. En el oráculo la misma `Cont` es un trozo de slice de marcos (apartado 9.1).

### 8.5 Planificador

Hay N workers, `GOMAXPROCS` goroutines (16 en esta máquina), y `KI_WORKERS` lo cambia. Desde Go 1.25, `GOMAXPROCS` respeta los límites de CPU de los contenedores [R4 §3.5]. **Arrancan tarde** **[decisión del diseño, 2026-10-28]**: el planificador solo existe en los programas cuya fila alcanzable contiene `Async` (apartado 9.3), y aun en ellos los workers nacen en el primer `spawn`, no al arrancar; hasta entonces `main` corre sola en la goroutine principal con el mismo contexto de fibra. Un test cuenta las goroutines de un programa con `Async` que nunca hace `spawn` (`runtime.NumGoroutine()` desde un trampolín de prueba): la principal y ninguna más.

**Deque de Chase–Lev por worker**, en la forma C11 de Lê et al. (PPoPP 2013), con `sync/atomic` **[decisión del diseño, R2 §5.3]**:
- índices `top` y `bottom` `atomic.Int64` con signo, porque un `uint` da la vuelta en `take()` cuando `bottom` es 0, en líneas de caché distintas con relleno a mano (`_ [64]byte`);
- un anillo fijo de 1024 slots `atomic.Pointer[Fiber]`: un ladrón puede leer un slot que el dueño sobrescribe tras dar la vuelta, y aunque su CAS fallará, una lectura plana sería una carrera;
- cuando se llena, la mitad pasa a la cola global protegida por `sync.Mutex`, así que nunca se redimensiona;
- las vallas `seq_cst` de la versión C11 están implícitas: todo atómico de Go es secuencialmente consistente, y no hay variantes relajadas ni acquire/release en `sync/atomic` (las del runtime son internas) [R2 §5.3]. Coste: cada `Store` es un `XCHGQ` y cada CAS un `LOCK CMPXCHGQ`; push+pop sin contención 10,3 ns frente a 7,8 ns con un mutex. El deque sin cerrojo gana bajo contención, porque los ladrones nunca bloquean al dueño [R2 P6].
- `-race` detecta accesos no atómicos en conflicto, pero con atómicos SC **no hay fallos de orden que encontrar**: la corrección exige además un test de estrés de linealizabilidad (cada fibra tomada exactamente una vez). R2 lo pasó con 20 rondas × 200 000 fibras × 15 ladrones bajo `-race` [R2 P6].
- La cola de ejecución de Go **no** es Chase–Lev (anillo de 256, consumo por la cabeza con CAS, los ladrones toman la mitad); el libro contrasta los dos diseños [R2 §5.3].

**`runnext`.** Un slot que guarda la fibra que la actual acaba de despertar.

**Bucle del worker**, en este orden: `runnext`, pop local (LIFO), la cola global, robos a víctimas aleatorias por el extremo FIFO, spin, y aparcar. Cada 61 vueltas mira primero la cola global, como `findRunnable` [R2 §5.4].

**Aparcar sin perder despertares**, el protocolo del comentario de `proc.go:36-81` portado tal cual: como mucho N/2 workers hacen spin; el que deja de hacer spin lo anuncia, se publica como ocioso, revisa otra vez todas las colas y duerme; hacer ejecutable una fibra despierta a un worker si hay ociosos y ninguno está en spin. El sueño es una recepción en un `chan struct{}` de capacidad 1 por worker (un *token*: un despertar nunca se pierde). El par de Dekker (el que publica empuja y lee `nidle`; el ocioso incrementa `nidle` y lee los deques) es correcto porque ambos lados son atómicos SC. 300 ejecuciones con `GOMAXPROCS` de 1 a 16 y 60 bajo `-race` sin cuelgues ni carreras [R2 P12]. El futex crudo (`syscall.Syscall6(SYS_FUTEX…)`) se enseña y se mide en el capítulo 44, pero no es el mecanismo por defecto **[decisión del diseño]**: dentro del runtime de Go, el sueño de una goroutine ya lo paga Go.

**El último en dormir detecta el bloqueo** **[decisión del diseño, T E6]**. Antes de bloquearse en su token, el worker que ve `nidle == N-1` ejecuta la detección de bloqueo de Ki: si no hay fibras ejecutables, ningún interés de E/S armado en el poller y ningún temporizador pendiente, imprime los sitios de aparcamiento y sale con 102. Sin esto pasan dos cosas malas, las dos comprobadas: sin poller (capítulos 44–45), `checkdead` de Go ve que ningún M corre y mata el proceso con `fatal error: all goroutines are asleep - deadlock!` y salida 2 antes de que Ki diga nada; con el poller dormido en `EpollWait(-1)`, Go nunca declara nada y el proceso se cuelga para siempre [T V-t7]. Un test por configuración (sin poller, con poller, con monitor) exige 102 y que el proceso termine.

**Preempción, en tres capas** **[decisión del diseño, R2 §4.4, R1 §4, R3 §5.2]**:
1. **Vida del proceso (obligatoria).** El ensamblador de Go nunca es preemptado asíncronamente (`preempt.go:450`); un bucle de Ki sin punto seguro detiene cada *stop-the-world* del GC para **todo el proceso** y, con un solo P, mata hasta los temporizadores (769 ms de latencia de GC en R1, 2,02 s en R2, 1,64 s en R3; con poll, ~1 ms) [R1 §4.2, R2 P2, R3 V4]. La regla, que el verificador de MIR exige **[decisión del diseño, T E1]**: toda función con una arista de retroceso o con una llamada de cola (estática o indirecta) contiene **al menos un `CALL`** (el camino lento de su poll, o uno inalcanzable) **o un marco ≥ 128 bytes**, para que el ensamblador le deje el prólogo; y toda arista de retroceso que no pasa por un prólogo lleva un poll. No basta con «toda función con bucle lleva un `CALL`»: una hoja sin bucle local que termina en `RET ·g(SB)` sigue siendo `NOSPLIT` (el `RET` con destino no cuenta como `CALL`), y dos hojas así que se llaman en cola entre sí (`isEven`/`isOdd`, máquinas de estados) forman un ciclo que el enlazador rechaza, y que, si enlazara, no tendría ningún punto seguro [T V-t3; R2 P3]. La VM no necesita nada de esto para la vida del proceso porque es código Go, que Go preempta de forma asíncrona; que su pila sea datos es lo que le da, además, la preempción *a nivel de Ki* con un presupuesto de instrucciones.
2. **Quantum de Ki.** Una goroutine monitor, cada 10 ms (el mismo `forcePreemptNS` de Go), pone `ctx.preempt` (atómico) en las fibras que llevan más de 10 ms corriendo. Ni el prólogo del ensamblador ni el poll en línea lo ven: los dos comparan `SP` con `g.stackguard0`, que solo escribe Go, y cuando el prólogo falla por preempción salta a `morestack → gopreempt_m` sin pasar por Ki [R1 §4.1; T E9]. Por eso el código emitido lleva una comprobación **propia** **[decisión del diseño, T E9 (a)]**: tras el prólogo de toda función que puede ceder y en cada uno de sus polls, `MOVQ ctx+0(FP), R; CMPB preempt(R), $0; JNE yield`, dos cargas y una rama bien predicha, y `yield` realiza `Async.yieldNow` burbujeando. Las funciones puras no la llevan: no pueden ceder. El coste no está medido en R1–R3; se mide en el capítulo 58, y la alternativa documentada, `--kipreempt=polls` (confiar en el `stackPreempt` que sysmon pone cada 10 ms, que solo llega a Ki en los polls y nunca en los prólogos, así que el código recursivo sin bucles no cedería), queda como respaldo si el coste resultara visible.
3. **Equidad con código puro largo.** El work stealing ya mueve las demás fibras del worker atascado a workers ociosos. Si todos están atascados, el monitor **retoma el slot**: una goroutine de repuesto se queda con el *slot* (deque, `runnext` y token de sueño), y la atascada termina su cálculo puro sin slot y vuelve al pool en la siguiente suspensión. Copia el `retake`/`handoffp` de Go para syscalls largas. **El slot y el contexto de allocator son objetos distintos** **[decisión del diseño, T E8]**: si el repuesto heredara el contexto de allocator con el slot, dos goroutines usarían las mismas free lists sin sincronización y el heap se corrompería. La retoma traspasa solo el slot; el repuesto toma un contexto de allocator nuevo del pool (hay más de `GOMAXPROCS`); la goroutine atascada conserva el suyo hasta que su fibra se suspende, y entonces lo retira: sus páginas vuelven al pool y lo que otros liberaron mientras tanto entra por `thread_free`. Un test de estrés con el heap `kiheapgo` y `-race` fuerza retomas con fibras asignando. No está prototipado, y se construye en el capítulo 54, no en el 44: en la VM, que se preempta por presupuesto, la retoma no se puede ejercitar con Ki real [T §2.4]. En modo determinista no hay monitor.

**Orden de memoria.** Todo atómico es SC porque Go no da otra cosa. Las variantes `acq_rel` y `relaxed` sobreviven como teoría (capítulo 35) y en el código **emitido** por Ki para arm64 (capítulo 67), que sí elige instrucciones.

### 8.6 Canales y select

**`Chan<T>`**, en el estilo de `hchan` de Go [R2 §5.5]: un `sync.Mutex`; un anillo de palabras poseídas; colas FIFO `sendq` y `recvq` de `*waiter{fiber, slot, case, sel}` intrusivos; una marca de cerrado. Si hay una pareja esperando, recibe el valor directamente y su fibra pasa a `runnext`.

**Aparcar y después soltar el cerrojo**, como `gopark` con `unlockf` en Go. La fibra se encola bajo el cerrojo, pero el cerrojo se suelta solo cuando la fibra ha dejado de correr: en la VM, cuando `Park` llega al bucle del worker; en nativo, cuando el burbujeo llega a la raíz. Con `sync.Mutex` puede desbloquear otra goroutine («a locked Mutex is not associated with a particular goroutine»), así que la regla de ayer «lo suelta el hilo que lo tiene» desaparece [R2 §5.5].
- **Bifurcación bajo el portador A** **[a decidir por medición en el capítulo 45 con `vm-bubble`, y a confirmar en el 54 con el nativo]**: la ventana aparcar-soltar ya no son unas instrucciones sino O(profundidad) marcos con sus asignaciones. La alternativa es marcar el waiter `parking` y soltar enseguida; un despertador que gana `done` mientras el waiter sigue `parking` solo lo pasa a `ready-pending`, y la raíz, al llegar a `Park`, se reencola a sí misma si ve ese flag. Así ningún despertador ve una fibra a medio aparcar y el tiempo de cerrojo vuelve a ser constante. La medición necesita `vm-bubble` (apartado 9.2): la VM normal aparca en el sitio y su ventana es constante, así que con ella no habría nada que medir [T §2.3].

**`select`**, con el algoritmo de `selectgo` [R2 §5.5]:
1. Evaluar los operandos en orden de fuente.
2. Sortear un orden de sondeo (el generador por worker en multinúcleo, splitmix64 en modo determinista), y fijar un orden de bloqueo **por id monótono de canal** (Go ordena por dirección; el id es estable y determinista). Bloquear todos.
3. Primera pasada: tomar el primer caso listo en orden de sondeo, o `default` si existe.
4. Segunda pasada: encolar un waiter por caso, todos compartiendo un `done` atómico. Aparcar y soltar.
5. Un despertador tiene que ganar `done` con CAS, y se salta los waiters de un `select` que otro ya ganó.
6. El selector despertado vuelve a bloquear en el orden de bloqueo y desencola sus demás waiters.

Los timeouts son canales normales (`after`).

### 8.7 E/S y temporizadores

**El poller**, `kirt/poll`, con dos implementaciones detrás de una interfaz **[decisión del diseño, R2 §6]**:
- `poll_epoll_linux.go`, la tuya y la de defecto en Linux: una goroutine poller es dueña de la instancia de `epoll`, de un `eventfd` (sin wrapper en `syscall`: `Syscall(SYS_EVENTFD2, …)`) y de un montículo 4-ario de temporizadores `{deadline, seq, destino}`. El timeout de `EpollWait` es el deadline más próximo, redondeado a milisegundos como hace `netpoll_epoll.go`. Añadir un temporizador anterior al primero, o apagar el runtime, escribe en el `eventfd`. Verificado: la disponibilidad de una pipe y el despertar del `eventfd` llegaron tras los 20 ms esperados [R2 P8]. `EpollWait` usa `Syscall6`, así que el runtime de Go cede el P mientras espera.
- `poll_goroutine.go`, una goroutine auxiliar por operación pendiente sobre `net`/`os`: la línea base medida, y el camino portable para Windows, que **no tiene epoll** (`undefined: syscall.EpollCreate1` con `GOOS=windows`) [R2 P8].

**Sockets.** Son no bloqueantes: primero se intenta la syscall; con `EAGAIN` la fibra aparca y el interés `EPOLLONESHOT` se arma desde el callback posterior al aparcado; cuando el poller la despierta, la fibra vuelve por la cola global y reintenta.

**Lo que `epoll` no puede vigilar.** Los ficheros regulares, stdin y la resolución de nombres van a un pool acotado de goroutines con llamadas bloqueantes; Go cede el P en una syscall bloqueante, así que no roban workers.

**Arranque perezoso** **[decisión del diseño, 2026-10-28]**. La goroutine del poller, el montículo de temporizadores y el pool bloqueante arrancan en su primer uso (la primera operación de red, el primer `after` o `sleep`, la primera lectura de un fichero o de stdin), con `sync.Once` o un CAS sobre su estado, y nunca al arrancar el programa; en un programa sin `Async` ni siquiera están enlazados (apartado 9.3). Un test ejecuta un programa que usa canales y temporizadores pero no red y comprueba que no existe la instancia de `epoll` (ningún descriptor de `epoll` en `/proc/self/fd`, y la goroutine del poller sin arrancar); otro, el detector de bloqueo con el poller sin arrancar, que cuenta cero intereses armados.

**Salida estándar.** stdout y stderr pasan por un único escritor, el paquete hoja `kirt/out` (la primitiva `ki_write` es su cara hacia Ki), que comparten los tres backends: el oráculo lo importa también, porque está en su lista blanca (apartado 8) **[decisión del diseño, constructor E2]**. Existe desde el capítulo 10, con `kirt` naciendo ahí. Tiene un buffer de 64 KiB y se vacía con cada salto de línea si es una terminal, al salir y en un panic. Como `os.Exit` se salta los `defer`, el panic vacía el buffer antes de salir [R4 V13]. Lo que no puede vaciar es un `fatal error:` de Go, que termina el proceso sin pasar por Ki (apartado 8.10). Las escrituras de `kirt/out` nunca aparcan la fibra: un `print` jamás pone `yielding`.

**Descriptores** **[decisión del diseño, T §2.13]**. Los sockets que vigila `kirt/poll` son **enteros crudos** (`int`) en la tabla de recursos del lado Go, nunca `*os.File`: `os.NewFile` sobre un fd no bloqueante lo registra en el netpoller **de Go** (`os/file_unix.go`), y dos pollers sobre el mismo fd es justo lo que no queremos [T S]. `*os.File` queda solo para el pool bloqueante (ficheros regulares, stdin). El cierre es explícito, `close`, y el `drop` del último proxy hace `syscall.Close` sobre el entero; no hay `runtime.AddCleanup`: no protegería de nada, porque la tabla mantiene vivo el objeto hasta ese último drop, que ya puede cerrar. Un recurso que se filtra lo encuentra el censo del modo de depuración, no un cleanup.

### 8.8 Modo determinista

`--sched=det --seed=N` funciona en los tres backends: un worker, sin poller, sin monitor y sin preempción.

**Procedimiento de decisión.** Forma parte de la especificación (`docs/semantics.md`): los ids de fibra son secuenciales, con `main = 0`; `spawn` nunca cambia de fibra; los despertares son FIFO; cuando la fibra actual se bloquea, cede o termina, la siguiente es `runnable[rng() % n]`, con el conjunto de ejecutables ordenado por id; un `select` con k casos listos toma el caso `rng() % k`, contando en orden de fuente; `rng` es splitmix64, de `kirt/splitmix`, escrito a mano (no `math/rand/v2`, para que el oráculo y el runtime compartan exactamente una fuente). El reloj es virtual: cuando no hay nada ejecutable, salta al temporizador más próximo, y los empates se rompen por orden de registro. La E/S de ficheros es síncrona; la de red es un panic.

**Go no aporta nada determinista, y no hay que intentar que lo haga** [R2 §7]: su planificador se aleatoriza a propósito bajo `-race`; `select` es aleatorio por especificación; `map` itera en orden aleatorio; `testing/synctest` virtualiza el tiempo pero no las intercalaciones (6 órdenes distintos en 300 burbujas) [R2 P9]. Lo prohibido en el oráculo, en `DetScheduler` y en la impresora de trazas: goroutines, `select` de Go, iterar un `map`, `math/rand`, `time.Now`.

**Herramientas.** `--sched-trace` imprime cada decisión. `ki test --seeds=1..1000` convierte el modo en un fuzzer de planificaciones, y una semilla que falla se reproduce idéntica en los tres backends.

**Dos implementaciones independientes** **[decisión del diseño]**: la del intérprete (unas 300–400 líneas) y `kirt.DetScheduler`, detrás de la misma interfaz que el planificador con work stealing y compartida por la VM y el nativo. Que la salida y la traza sean idénticas para la misma semilla es una obligación de los tests.

### 8.9 Política de ciclos

El heap es acíclico por construcción, salvo a través de los canales. Esta es la condición que hace válido usar RC sin recolector de ciclos: sin referencias mutables compartidas y sin definiciones recursivas de valores.

**Prohibido:** las definiciones recursivas de valores (`let` no es recursivo, y un ciclo entre constantes de nivel superior es un error); capturar un `var` en un closure; que una continuación sea alcanzable desde otra fibra (panic).

**Se resuelve con lambda lifting.** Los grupos de `fn` locales recursivas se vuelven funciones de nivel superior que reciben sus capturas como parámetros, antes de Perceus, así que ningún closure se apunta a sí mismo. Los métodos de instancia recursivos llaman a la función de la instancia. Los bucles `joinrec` no son closures. Los `join_k` de S5 son funciones de nivel superior y sus entornos solo contienen valores anteriores a la captura.

**Thunks.** Su cuerpo es puro y no puede nombrarse a sí mismo, así que memorizarlo no cierra ningún ciclo. **Continuaciones.** Los marcos capturados antes de existir `k` no pueden referirse a `k`.

**Lo que puede fugarse:** los ciclos a través del buffer de un canal, y las fibras bloqueadas para siempre en canales que nadie más alcanza, como las goroutines en Go.

**Detección en depuración:** el censo de salida por sitio de asignación; una pasada de *trial deletion* con raíces solo en los canales vivos; `KI_DEBUG_CHAN_CYCLES=1`, que comprueba en cada `send` si el valor alcanza el canal de destino; el detector de deadlock; el intérprete en modo RC.

### 8.10 Desbordamiento de pila y códigos de salida

Las pilas también cambian de dueño. El oráculo y la VM tienen la pila como datos (slices de marcos) y el desbordamiento es suyo: `KI_STACK_MB` fija el límite y el panic sale con 103, como ayer. El código nativo corre sobre pilas de goroutines que Go hace crecer por copia hasta 1 GB (`maxstacksize`), y pasarse es un **error fatal, no un panic**: `fatal error: stack overflow`, código de salida 2, sin ningún gancho para interceptarlo [R1 §2.4, R2 §1.1]. Verificado con `debug.SetMaxStack(64<<20)` y 2×10⁶ marcos [R1 ok overflow, R2 P11].

**Decisión** **[decisión tuya, 2026-10-24: apartado 4; el detalle de la mecánica es del diseño]**:
- `KI_STACK_MB` se traduce en nativo a `debug.SetMaxStack`, que es un límite **de todo el proceso** bajo el portador A, porque todas las fibras comparten las pilas de las goroutines worker.
- Por defecto, el binario nativo acepta el error fatal de Go. `ki run` y `ki test` ejecutan los binarios como subprocesos (de todos modos deben hacerlo: `go run` enmascara los códigos de salida [R4 V13]) y traducen «salida 2 más la firma exacta `fatal error: stack overflow` / `goroutine stack exceeds` en stderr» a **103**, para que un `ki run` dé el mismo código en los tres backends. Un binario ejecutado a mano sale con 2 y el mensaje de Go.
- **Se pierde stdout.** Un error fatal de Go no pasa por Ki, así que los hasta 64 KiB de `kirt/out` sin vaciar (stdout es una pipe bajo `ki run`) se pierden: el nativo da el código 103 traducido, pero puede imprimir menos que la VM antes de morir [T §2.21]. Si hace falta el mismo stdout, la vía es `--stack-check=count`.
- En las pruebas diferenciales, esa combinación se clasifica como `overflow`, inconclusa, igual que cualquier desbordamiento (la profundidad nunca fue observable). Cualquier **otra** salida 2, sea `fatal error:` o `panic:`, falla con ruido (apartado 6.3).
- `ki build --stack-check=count` añade a cada prólogo Ki un contador de profundidad en el contexto de la fibra (`INCQ`, `CMPQ`, `JA` y el `DECQ` en cada salida), con un límite inferior al de Go, de modo que el panic de Ki llega primero y sale con 103 exacto y con el stdout vaciado. Su coste es una **estimación, no una medida**: del orden de 0,7 ns por llamada (un RMW en memoria, como el `rc++` que R3 §4 midió sobre 10⁶ objetos, que no es lo mismo que un contador por llamada) sobre llamadas de ~2 ns; parece demasiado para ser el defecto, y el capítulo 58 lo mide.

**Códigos de salida de Ki:** 0 normal; 1 errores de compilación; 101 panic; 102 deadlock; 103 desbordamiento de pila. Y los de Go que nunca deben verse en un programa correcto: 2 con `fatal error:` (apartado 6.3).

---

## 9. Los tres backends

### 9.1 El intérprete (el oráculo)

`--backend=interp` es una máquina CEK sobre Core en cualquier etapa, de S0 a S5, escrita en Go idiomático y sobre el GC de Go: sin RC, sin `unsafe`, structs e interfaces [R3 §7].

**Valores y pila propios.**
- Los valores son una interfaz sellada (`type Value interface{ value() }`) con un struct por forma; los marcos, otra (`letFrame`, `argFrame`, `matchFrame`, `handlerFrame{h, id}`, `promptReturnFrame`, `restoreEvFrame`…), despachados con *type switch*. Es la lección de interfaces del libro, y la velocidad no importa [R2 §2.1].
- Su pila de continuación es un dato: `stack []Frame`. La recursión profunda vive en el heap de Go, las llamadas de cola salen gratis y los handlers son literales: `perform` busca hacia abajo el `handlerFrame` de su etiqueta (sin máscaras: el oráculo nunca toma el atajo), copia los marcos por encima con `k := &Cont{frames: slices.Clone(stack[h:])}`, hace `clear(stack[h:])` para que el GC no los retenga y trunca; `resume` comprueba `Used` y hace `stack = append(stack, k.frames...)`. Captura y reanudación son O(profundidad hasta el handler), que para un oráculo está bien [R2 §2.1].
- La trampa que Rust habría detectado y que el capítulo 21 enseña a propósito: `k.frames = stack[h:]` *sin* clonar comparte el array subyacente, y el siguiente `append` sobre `stack` sobrescribiría los marcos capturados [R2 §2.1].
- Los estados de κ (`Live`/`Used`) viven en una tabla de objetos cuyos ids nunca se reutilizan.

**Concurrencia propia.** Sus fibras son estados CEK que mueven su propio planificador determinista y su propio modelo de canales, escritos desde `docs/semantics.md`. **Ni una goroutine, ni un canal de Go, ni un `select` de Go**: así el determinismo se cumple por construcción y no por disciplina [R2 §2.2]. No tiene modo multinúcleo.

**Lo único que comparte con `kirt`:** `kirt/splitmix`, `kirt/fmtnum` y `kirt/out` (apartado 8).

**S5 en el oráculo.** Con `--stage=S5` la raíz de cada fibra es un prompt del planificador del oráculo: `perform Async.op` y `perform IO.op` son `yieldTo(raíz)`, burbujean por los `join_k` y los prompts intermedios, la raíz recibe la `Cont` y da el paso δ, y al despertar reanuda reproduciendo. Es el mismo camino que recorre el nativo, ejecutado sobre marcos de Go, y lo que hace que `--check-passes` S4↔S5 cubra la suspensión por `Async` (apartado 6.1) **[decisión del diseño, T E7]**.

**Modos:** `--level=core` (el oráculo); `--stage=S0…S5`; `--level=rc`, que ejecuta S4 y S5 sobre un heap comprobado (una tabla de objetos gestionada por el GC de Go, con ids nunca reutilizados, lápidas, contadores explícitos y la **procedencia Ki** de cada asignación y cada drop: el span del nodo de Core que la ejecuta, no `runtime.Caller`, que solo daría una línea del intérprete [T §2.22]) y exige un censo de cero al salir. Esa tabla es independiente de `kirt/heap`, así que un fallo del allocator real no puede esconderse detrás del mismo fallo en el comprobador [R3 §6].

**Velocidad.** Es de 10 a 50 veces más lento que la VM, y no importa: queda en `ki` para siempre.

### 9.2 La máquina virtual

`--backend=vm` corre dentro del proceso `ki` sobre el runtime real: objetos, allocator, RC, planificador, canales y E/S.

**Estado por fibra.** `regs []Value` (un slice sin punteros para el GC de Go, porque los valores son enteros) y `frames []vmFrame{fn, pc, base}`. Se usan **índices, nunca `&regs[i]`**: `append` puede reubicar y un puntero al array antiguo queda obsoleto en silencio [R2 §5.1].

**Formato** **[decisión del diseño, T §2.10]**. Instrucciones fijas de 64 bits, `{u8 op, u8 a, u8 b, u8 c, u32 k}` (`k` es un inmediato, un índice del pool o un desplazamiento de salto), con palabras de extensión para listas de argumentos y tablas de saltos; **operandos de registro de 8 bits**, así que un marco tiene a lo sumo 256 registros, y el emisor vuelca al heap de la fibra lo que no cabe; una ventana de registros por marco, que el bucle convierte al entrar en el marco en un `*[256]Value` (`(*[256]Value)(regs[base:base+256])`, un solo chequeo de límites por entrada) para que cada acceso `win[a]` con `a uint8` no pague ninguno; un pool de constantes; un mapa de slots poseídos, con su clase, en cada sitio de llamada. Con operandos `uint16`, o indexando el slice con `base+a`, cada instrucción pagaría tres `IsInBounds` [T V-t5].

**Opcodes** (unos 70): aritmética de `Int` comprobada y operaciones de `Float`; switch por tag y carga de campos; `ALLOC` y `REUSE`, `DUP` y `DROP`, `BOX` y `UNBOX`; llamadas de aridad exacta, llamadas de cola por reutilización de marco y llamadas a closures; las primitivas de S5 (`EVLOOKUP`, `YIELDTO`, `ISYIELD`, `YIELDEXT`, `PROMPT`, `RESTOREEV`, `RESUME`); llamadas al runtime.

**Efectos en la VM = ejecutar S5** **[decisión del diseño, R2 §5.1]**. La VM no captura segmentos de su propia pila ni recorre marcos con mapas de slots para soltar una `Cont` (lo más arriesgado del diseño de ayer): ejecuta la misma S5 que el nativo, con lo que se convierte en un oráculo de depuración del código nativo *al mismo nivel de IR*. La variante con segmentos queda como ejercicio. Ante un `perform Async.op`, la raíz de la fibra de la VM **aparca en el sitio** por defecto (devuelve `Park` al bucle del worker con `regs` y `frames` intactos), de modo que las comprobaciones `isYielding` no se disparan nunca en `--backend=vm`; **`--backend=vm-bubble`** es la misma VM con la raíz burbujeando como el nativo: pone `yielding`, las comprobaciones toman el camino lento, la `Cont` se construye con los entornos `join_k` y la fibra se reanuda con `RESUME`. No es un flag de depuración sino un backend de la matriz diferencial (apartado 6.3), porque es el único que ejecuta sobre el runtime real, en código Go visible para `-race`, el camino que el nativo recorre en ensamblador **[decisión del diseño, T E7]**.

**Preempción.** Un presupuesto de instrucciones: la pila es datos, así que se puede parar en cualquier punto.

**Herramientas.** Un verificador comprueba el bytecode antes de ejecutarlo (y se fuzzea con `go test -fuzz`), y un desensamblador sirve a `ki emit bytecode`.

**Dispatch y velocidad.** Un `switch` denso, que el compilador de Go convierte en tabla de saltos (verificado: 9 casos → `jump3`, tabla `SRODATA` de 72 bytes) [R4 V24]. Go no tiene goto computado ni `musttail`; el capítulo 58 mide el `switch` frente a una tabla de funciones y a dispatch enhebrado con closures. Lo que sí hay que vigilar, con tests de CI que leen `-gcflags=-m=2` y `-d=ssa/check_bce`: que los accesores de `kirt/heap` y el camino rápido de la aritmética sigan inlineándose (presupuesto 80), y que el acceso a registros no pague comprobaciones de límites (la ventana `*[256]Value` indexada por `uint8`) [R3 §3.6, R4 V23]. Sobre `b.Loop`: en 1.26 las llamadas escritas dentro de `for b.Loop() { … }` **sí se inlinean** [T V-t2; R4 §3.2] (R3 §5.4 decía lo contrario y se equivoca); lo que hay que saber es que los argumentos, los resultados y las variables asignadas en el cuerpo se mantienen vivos con un `runtime.KeepAlive` intrínseco, así que el cuerpo mide también ese coste, y que con el `b.N` clásico el compilador puede borrar trabajo cuyo resultado no se usa.

### 9.3 Código nativo x86-64: ensamblador de Go y `go build`

**Pipeline.** MIR → LIR por maximal munch → liveness → linear scan → marcos → texto en sintaxis Plan 9 (un solo `prog_amd64.s` por programa, con `#include "textflag.h"`, `"funcdata.h"` y `"kiabi.h"`) → módulo generado → `go build`.

**El módulo generado y `kirt`** **[decisión del diseño, R1 §7.1 y R4 F4]**. `ki build` escribe en `$KI_CACHE/build/<hash del programa>/` (por defecto `~/.cache/ki/build/`) un módulo mínimo:

```text
go.mod         module kiprog ; go 1.27 ; toolchain <go env GOVERSION of the ki that built it>
               require github.com/aldezex/kilang v0.0.0
               replace github.com/aldezex/kilang => ../../kirt/<content hash of the embedded kirt tree>
main.go        package main; func main() { sched.Run(kimain, kiResume) } when the program's reachable rows contain
               Async, and func main() { direct.Run(kimain) } when they do not (kirt/direct runs kimain on the
               main goroutine with a minimal fiber context and imports no scheduler, poller, timer heap or
               blocking pool): kirt cannot import main, so the entry points reach it as func values; kimain
               and kiResume are bodyless Go prototypes (kiResume only in the sched form)
ki_gen.go      the prototypes of the entry points called from Go, and one same-package
               trampoline per kirt entry the program uses: func kiAlloc(n uintptr) uintptr { return kirt.Alloc(n) }
kiabi.h        #defines generated from kirt/layout (offsets, sizes, constants)
prog_amd64.s   every Ki function
```

- La copia de `kirt` (los fuentes que `ki` lleva incrustados, más un `go.mod` sintetizado con el mismo *module path*) se escribe una vez por **hash del contenido del árbol incrustado** en `~/.cache/ki/kirt/<hash>/`, así que dos builds de desarrollo de `ki` sin sello de versión no chocan. La incrustación la hace un único `embed.go` en la raíz del módulo (`package kilang`, con `//go:embed kirt stdlib docs/errors`): un patrón `//go:embed` no puede subir a un directorio padre, así que ni `internal/build` podría incrustar `kirt/` ni `internal/driver` el preludio; y como los programas nativos importan `kirt`, no la raíz, no cargan con los fuentes. Como el contenido y la ruta no cambian, la caché de `go build` reutiliza el archivo compilado de `kirt` entre programas: R1 construyó dos módulos generados y la acción de `kirt` no ejecutó ningún comando la segunda vez [R1 §7.1]. Compilador y runtime siempre coinciden, porque la copia sale del mismo binario `ki`. `ki clean` vacía la caché: nada queda tirado en el proyecto ni en `/tmp`.
- `ki build` invoca `go build -trimpath -mod=mod -o <salida> .` con `GOTOOLCHAIN=local`, **`CGO_ENABLED=0`** y `GOFLAGS` limpio, así que un `go.mod` nunca dispara una descarga de toolchain, y los errores de `go` vuelven como diagnósticos. `CGO_ENABLED=0` es obligatorio **[decisión del diseño, T §2.18]**: `kirt/poll_goroutine.go` importa `net`, que con el `CGO_ENABLED=1` de tu máquina compila sus `CgoFiles` y produce un ejecutable enlazado dinámicamente contra glibc (`interpreter /lib64/ld-linux-x86-64.so.2` [T V-t6]), lo que exige gcc para un `ki build` y rompe el anti-objetivo «ni cgo»; con `CGO_ENABLED=0` sale `statically linked`. La única excepción es una build de depuración `-race` explícita, que necesita cgo. `ki doctor` lo comprueba.
- La línea `toolchain` del `go.mod` generado la escribe `ki` con la versión del toolchain local (`go env GOVERSION`), nunca una constante compilada en `ki` **[decisión del diseño, constructor §3.11]**: así un parche de Go nuevo (apartado 11.4) no obliga a recompilar `ki`, y `GOTOOLCHAIN=local` nunca encuentra un `go.mod` que pida algo distinto de lo instalado.
- **El runtime a medida del programa** **[decisión del diseño, 2026-10-28, a partir de tu «que el binario sepa cuándo cargar qué»]**. `ki build` conoce el programa entero (apartado 5.9) y sus filas de efectos, así que elige la forma del módulo generado: si ninguna fila alcanzable desde `main` contiene `Async`, `main.go` llama a `direct.Run(kimain)`, que ejecuta `kimain` en la goroutine principal con un contexto de fibra mínimo, y el módulo no importa `kirt/sched`, `kirt/chan`, `kirt/poll`, `kirt/timer` ni el pool bloqueante; como el enlazador de Go elimina lo que no se alcanza, el binario tampoco los lleva. Con `Async`, `main.go` llama a `sched.Run(kimain, kiResume)` y el módulo importa lo que las filas piden (la red solo si el programa usa `std/net`, por ejemplo). La lista de imports de `ki_gen.go` y `main.go` sale de las filas y de las primitivas que el programa referencia; un test construye un programa puro, uno con solo `IO` y uno con `Async`, y comprueba con `go tool nm` que los símbolos de `kirt/sched` y `kirt/poll` están solo en el tercero, y compara los tamaños. Hasta el capítulo 54 la forma `direct` es la única que existe (`kiResume` es un stub que hace panic), y las primitivas de `IO` terminan de forma síncrona por su trampolín y nunca ponen `yielding` (apartado 12.5, capítulos 49–53); el 54 añade la forma `sched`, y la `direct` se queda para siempre como el camino de los programas sin `Async`.
- **Por qué trampolines y no `linkname`**: el ensamblador de Go no admite un punto en la ruta de un símbolo (`github.com∕aldezex∕kilang∕kirt·Alloc` se rechaza con `expected '(', found .` [T V-t9]), y una referencia a `kirt.Alloc` a secas no enlaza (`not defined for ABI0`) [R2 P3, R1 §1.3]. El trampolín en el mismo paquete hace que el compilador genere el wrapper ABI0 de forma automática. Cuesta ~1,2 ns más que una llamada asm→asm (2,39 frente a 1,22 ns) [R2 P11], y solo se paga en los caminos lentos, porque los rápidos están en línea. Una llamada a un trampolín es **ABI0 completo**: los resultados vuelven **en la pila**, en el área de salida después de los argumentos, no en `AX` (el wrapper hace `MOVQ AX, 0x20(SP)` [R2 P11]), así que el área de salida de un sitio de llamada a trampolín cubre argumentos y resultados. `docs/abi.md` recoge esto, las firmas `direct.Run(kimain)` y `sched.Run(kimain, kiResume)` y la regla de los puntos de entrada de más abajo [T §2.11].
- El código generado **nunca llama al runtime de Go directamente** (`CALL runtime·Gosched(SB)` no enlaza) [R1 §1.3]. Llama a trampolines, y los trampolines a `kirt`, y `kirt` al runtime.
- Coste de construcción medido: un programa de 10⁴ funciones (300 000 líneas de asm) se reconstruye en ~1,0 s con `kirt` en caché; un cambio nulo, 0,10 s [R1 §7.3]. Un `.s` y sin prototipos es la variante más rápida.

**KiABI-A0, la convención entre funciones Ki** **[decisión del diseño, el contrato de R1 §11]**:
- Toda función Ki es `TEXT ·k<mangled>(SB), $F-0` en ABI0: ABIInternal está prohibido fuera del runtime de Go (`ABI selector only permitted when compiling runtime`) [R1 §1.1]. `F` es múltiplo de 8 y cubre spills, locales y el área de salida.
- **Sin prototipos de Go para las funciones Ki.** Con argsize 0, las palabras de argumento viven en el marco del llamador, que el runtime cuenta como locales suyas, sin escanear (son enteros) y copiadas tal cual al crecer la pila. Verificado con 10⁶ marcos, crecimiento, `runtime.GC()` y `gccheckmark`, en Linux y Windows [R1 §3.2–3.3]. Solo tienen prototipo los puntos de entrada que Go llama (`kimain`, `kiResume`) y los trampolines; `ki build --emit-protos` escribe prototipos y argsizes reales para que `go vet` (asmdecl) compruebe los desplazamientos `FP` del backend en la CI [R1 §7.4]. **Los puntos de entrada nunca hacen llamadas de cola** **[decisión del diseño, constructor §3.4]**: su argsize es el real de su prototipo, no el área de salida del programa, así que `kimain`, `kiResume` y lo que `kiResume` reproduce en su propio marco llaman siempre con `CALL`; el verificador de MIR lo exige.
- **Argumentos:** el llamador guarda el argumento *i* en `8*i(SP)` (el `SP` hardware, el fondo de su marco) y el llamado lo lee en `a_i+8*i(FP)`. El argumento 0 es el contexto de la fibra (apartado 8.4).
- **`DX`:** el puntero de closure o entorno (una dirección del heap de Ki, o 0; nunca un inmediato, porque `g.sched.ctxt` se escanea como puntero) [R1 §3.4]. Los puntos de entrada de closures llevan `NEEDCTXT`, que es lo que hace que `morestack` preserve `DX`; el llamador carga el código desde `0(DX)` y hace `CALL CX`. Es la convención de closures del propio Go [R1 §6.3].
- **Resultados:** en `AX` (`BX`, `CX`… si hay más). Son seguros en `RET` porque nada corre entre el epílogo del llamado y el llamador.
- **Ningún registro sobrevive a un `CALL`.** Todo lo vivo se vuelca al marco. Es lo que impone el ABI de Go («there are no callee-save registers»), y lo que ayer ya hacía KiCC a través de las llamadas suspensivas [R1 §5.1].
- **Marcos y GC:** toda función con marco que contiene un `CALL` ejecuta `NO_LOCAL_POINTERS`, sin excepción (sin él: `fatal error: missing stackmap` en la primera copia de pila) [R1 §3.2]. Ningún valor de Ki es un puntero de Go. Ni `GO_ARGS` ni `GO_RESULTS_INITIALIZED`.
- **Pila:** el prólogo del ensamblador (`MOVQ FS:-8, R14; CMPQ SP, 16(R14); JBE morestack`, más `PUSHQ BP; MOVQ SP, BP` si el marco no es cero) se acepta tal cual; nunca se toca `BP`, nunca se escribe `SP` fuera del marco, nunca se cambia de pila, nunca se guarda una dirección de pila a través de una llamada [R1 §2, §11.4]. No hay prólogo `push rbp` propio: lo escribe el ensamblador.
- **Hojas.** Una función con marco < 128 bytes y sin `CALL` queda `NOSPLIT` en silencio y pierde el prólogo, **aunque termine en `RET ·g(SB)`**: el `RET` con destino no cuenta como `CALL`. Si se llama a sí misma en cola, o si dos hojas así se llaman en cola entre sí, el enlazador las rechaza (`nosplit stack over 792 byte limit … infinite cycle` [T V-t3]), y si enlazaran no tendrían ningún punto seguro. La regla **[decisión del diseño, T E1]**: toda función que contiene una llamada de cola (`RET ·f(SB)` o la indirecta de abajo) o una arista de retroceso lleva al menos un `CALL` (el camino lento de su poll, o uno inalcanzable tras el salto) o un marco ≥ 128 bytes; el verificador de MIR lo exige, y el canario C4 cubre el ciclo mutuo, no solo la hoja que se llama a sí misma. Con un `CALL` inalcanzable, el par `isEven`/`isOdd` enlaza, recupera el prólogo (`MOVQ FS:-8, R14; CMPQ SP, 0x10(R14); JBE`) y da el resultado correcto [T V-t3].
- **Llamadas de cola estáticas:** `RET ·callee(SB)` tras escribir los argumentos del destino en nuestra propia área de entrada; el ensamblador lo convierte en epílogo más `JMP` (solo cuando el destino es un símbolo: `cmd/internal/obj/x86/obj6.go:853` [T S]), y cada salto reentra el prólogo del destino, así que los bucles por llamada de cola son preemptibles gratis (10⁸ llamadas de cola mutuas en 105 ms con la pila plana) [R1 §6.1, R2 P3]. Como el área de entrada la reservó nuestro llamador, el destino solo puede tener hasta tantos argumentos como nosotros. Por eso **todo sitio de llamada reserva un área de salida de tamaño fijo para el programa: la aridad máxima (contexto incluido) de cualquier destino de cola, estático o indirecto** **[decisión del diseño, T E2]**. v0 compila programas enteros, así que el backend la conoce; para los destinos indirectos cuenta la aridad máxima de cualquier closure del programa. Ninguna llamada de cola «cae a una llamada normal»: la garantía de cola es la misma que en el oráculo y la VM, porque un programa que corre en espacio constante allí y agota la pila en nativo nunca lo detectaría el diferencial, que clasifica el desbordamiento como inconcluso.
- **Llamadas de cola indirectas** (a un closure: CPS, `apply`, combinadores) **[decisión del diseño, T E2]**: el ensamblador no las convierte, así que el backend emite la secuencia verificada `ADJSP $-F; POPQ BP; JMP CX`, con `DX` = closure y el destino `NEEDCTXT`, **seguida de dos instrucciones inalcanzables de compensación, `PUSHQ BP; ADJSP $F`**. Sin ellas el bloque `morestack` que el ensamblador añade al final de la función supone un desplazamiento de SP que ya no es el del marco, y el primer escaneo de pila del GC mata el proceso (`SIGSEGV … runtime.(*unwinder).next … scanstack`); con ellas, 10⁸ colas indirectas mutuas corren en ~200 ms con `runtime.GC()` concurrente, `GODEBUG=gccheckmark=1,invalidptr=1` y la pila plana [T V-t11]. Depende de un detalle no documentado del ensamblador (el `spfix` del bloque `morestack`): es el canario C13 y entra en el riesgo 1.
- **Registros:** `R14` es `g` justo después del prólogo y se recarga con `MOVQ (TLS), R14` tras cada `CALL`; `X15` se puede pisar (las transiciones lo ponen a cero); `R12`/`R13` son scratch (`R12` lo usa el prólogo para marcos > 128); `R15` se evita por si algún día hay `-dynlink` [R1 §5.1]. `SP` solo está alineado a 8 bytes: nada de SSE alineado sobre la pila.
- **Floats** viajan en registros generales; en v0 están empaquetados de todos modos.
- **Polls.** Toda arista de retroceso que no pasa por un prólogo (`joinrec`, bucles locales, llamadas de cola a sí misma compiladas como salto) lleva un poll. Las tres investigaciones coinciden en que el poll en línea cuesta cero medible (0,36 frente a 0,37 ns por iteración [R1 §4.2]; 1,98 frente a 2,07 s [R2 P2]) y en que depende del desplazamiento 16 de `g.stackguard0`, que el propio ensamblador codifica a fuego pero que ningún documento promete. R1 §4.4 y R3 §9.7 lo recomiendan como defecto con un canario; R2 §4.4 prefiere como defecto el `CALL` a un stub con prólogo y el poll en línea como optimización medida (el stub cada 1024 iteraciones fue 2× más lento en un bucle de un ciclo). **Decisión** **[decisión del diseño]**: `--poll=inline` por defecto, `--poll=call` como respaldo documentado, la CI ejecuta la suite con los dos y un canario mide la latencia de `runtime.GC()` con bucles nativos corriendo.

  ```asm
  CMPQ SP, 16(R14)          // R14 = g; reload with MOVQ (TLS), R14 after any CALL
  JLS  slow                 // stackguard0 == stackPreempt, or the stack is genuinely low
  ...
  slow: spill live values; CALL ·kiPoll(SB); MOVQ (TLS), R14; reload; JMP back
  ```
  `kiPoll` es un trampolín a `kirt.Poll`, que llama a `runtime.Gosched()`. El poll en línea solo sirve a Go (punto seguro del GC y preempción de Go): compara `SP` con `g.stackguard0`, que el monitor de Ki no toca. El quantum **de Ki** es la comprobación aparte del apartado 8.5, capa 2: `MOVQ ctx+0(FP), R; CMPB preempt(R), $0; JNE yield` tras el prólogo de toda función que puede ceder y en cada uno de sus polls; las funciones puras no la llevan [T E9].
- **Llamadas a una función que puede ceder:** `CALL`, y después `CMPB yielding(ctx), $0; JNE bubble_k`, donde `bubble_k` asigna el entorno de `join_k`, llama a `kiYieldExtend` y retorna. El flag de acarreo que sugiere Koka no sirve: el epílogo del ensamblador (`ADDQ $n, SP`) pisa los flags [R2 §4.5]. Hasta el capítulo 53, `bubble_k` es un trampolín a un trap (`kiTrapNoYield`, ICE «yield not supported natively yet») que nunca se dispara, porque nada pone `yielding` antes de ese capítulo (apartado 12.5).
- **Datos:** `RODATA` para los literales, los closures estáticos, los diccionarios sin contexto y las tablas de direcciones de código (funcionan, también en PIE); `NOPTR` para datos mutables; nada con punteros de Go se define en ensamblador [R1 §1.6, §3.6]. Las constantes que se inicializan en tiempo de ejecución no van en `RODATA`: viven en el heap de Ki y se inmortalizan al terminar su inicialización (apartado 8.1).
- **Posiciones:** `#line N "file.ki"` antes de cada función y sentencia; `go tool objdump` y los tracebacks de Go atribuyen entonces el código a `overflow.ki:7`. En 1.26.5 la primera instrucción tras `#line N` sale atribuida a `N-1`: el emisor escribe `N+1` y un test fija la convención [R1 §7.5, R4 V6]. No hay DWARF para los locales de ensamblador: el depurador ve líneas de Ki pero no variables.
- **KiABI-B′** (argumentos en registros, funciones `NOSPLIT` con chequeo manual y un ayudante `kigrow` de 4 KiB con prólogo): ~35 % menos coste de llamada en `fib` (1,3 frente a 2,0 ns), pero depende además de que el enlazador trate las llamadas indirectas como partibles. Va detrás de `--kiabi=b` en el capítulo 58, con un test de humo (crecimiento, GC, preempción) en cada actualización de Go, y A0 sigue siendo el defecto [R1 §10].

**Asignación de registros: una escalera de corrección** (sin cambios): (1) un asignador que lo vuelca todo (`--regalloc=spill-all`), correcto por construcción y conservado para siempre; (2) linear scan de Poletto–Sarkar con partición en las llamadas, que aquí es obligatoria y no una elección; (3) en el capítulo 57, Wimmer–Franz. Un verificador reejecuta abstractamente el código asignado en las builds de depuración.

**Generación de código.** La aritmética de `Int` usa el camino rápido en línea (`MOVQ a, CX; ANDQ b, CX; TESTQ $1, CX; JZ slow; LEAQ -1(b), DX; ADDQ a, DX; JO slow`), `IMULQ` con `JO` tras desetiquetar un operando, e `IDIVQ` comprueba antes el cero y `MIN/-1` [R3 §3.6]. El camino lento es un trampolín a Go. El `dup`/`drop` local y el pop del allocator van en línea. Los floats nunca usan FMA (apartado 5.10).

**Lo que desaparece respecto a ayer:** la frametable y sus símbolos de sección, `ki_switch`, `ki_handle`/`ki_perform`/`ki_resume` en ensamblador, los trampolines de entrada de fibra, el `r15` fijado, la línea de enlace capturada con `g++ -###` y `nm -u`. Lo que los sustituye es S5, `kirt` en Go y `go build`.

---

## 10. Las seis partes difíciles

**(a) Efectos one-shot idénticos en los tres backends, con Perceus.**
- **Postura:** una sola semántica en Core y una sola regla de propiedad. Una `Cont` posee todo lo que la computación suspendida retiene; `resume` la consume; soltar una `Cont` viva libera sus entornos sin ejecutar código Ki. El oráculo mueve marcos de un slice; la VM y el nativo ejecutan S5, y sus continuaciones son cadenas de closures cuyos entornos son los conjuntos de suspensión. Nunca se infiere un préstamo a través de un prompt.
- **Coste:** sin multi-shot; O(profundidad) por captura; código extra en cada sitio de llamada que puede ceder (lineal, por los join points compartidos).
- **Riesgo y mitigación:** un conjunto de suspensión mal copiado produce una fuga o un doble free al soltar una continuación. Lo mitigan `--check-passes` S4↔S5 en el oráculo, el censo de `interp-rc` sobre S5 y kigen abandonando continuaciones en cada `perform` [R2 §8].

**(b) HM, typeclasses, HKT y filas de efectos sin zoo.** Sin cambios: tipos de rango 1; traits de un parámetro con instancias de Haskell 98; kinds declarados con huecos, solo al final; filas de Leijen; los traits recorren tipos, nunca filas. Cada pieza es decidible por separado. Coste: sin estilo mtl, sin tipos asociados, más anotaciones que en Haskell. Riesgo: mensajes confusos cuando un efecto cruza un callback; plantillas dedicadas y un golden por código.

**(c) Fibras, canales, `select` y work stealing en Go, con modo determinista.**
- **Postura:** el planificador no es un handler de Ki; `IO` y `Async` son primitivos. La política vive en `kirt/sched`, detrás de una interfaz con dos implementaciones. Canales al estilo Go con aparcar-y-soltar. Chase–Lev con `sync/atomic`. Workers sin `LockOSThread`. El modo determinista implementado dos veces y sin tocar el planificador de Go.
- **Coste:** sin planificadores escritos en Ki para `Async` en v0; sin preempción de código puro a nivel de Ki bajo el portador A.
- **Riesgo y mitigación:** despertares perdidos y carreras en `select`. Los mitigan los barridos de semillas, `-race` con el heap de depuración `kiheapgo`, los tests de estrés con conteo (cada fibra exactamente una vez), el estrés con 16 workers forzando migraciones y el modelo independiente del oráculo [R2 §11 R9].

**(d) El backend x86-64 bajo el runtime de Go.**
- **Postura:** todo hasta el texto en Plan 9. Maximal munch; linear scan con verificador y la escalera; KiABI-A0 (apartado 9.3); polls en cada arista sin prólogo, exigidos por el verificador de MIR.
- **Coste:** argumentos en memoria (Go mismo vivió con ABI0 hasta 1.17); unos 1,3× el coste de llamada del ABIInternal de Go en `fib` [R1 §10].
- **Riesgo y mitigación:** cuatro fallos nuevos, tres errores fatales en ejecución (`missing stackmap`, `SPWRITE`, `invalid pointer`) y uno del **enlazador** (`nosplit cycle`, que no es un `fatal error:` sino un `go build` que falla), y la dependencia de detalles internos (desplazamiento 16, hoja auto-`NOSPLIT`, tolerancia a funciones sin prototipo, el `spfix` del bloque `morestack` en las colas indirectas). Los mitigan el verificador, `--emit-protos` con `go vet`, el asignador spill-all, el reductor, la matriz a tres bandas y los canarios en cada actualización de Go. Los cuatro fallos se reproducen a propósito como ejercicios del capítulo 47 [R2 §10].

**(e) Un heap propio dentro de un proceso con GC.**
- **Postura:** `Value uint64`, direcciones absolutas, una reserva `mmap` que el GC de Go no conoce, ningún puntero de Go en memoria de Ki, `unsafe` en un solo paquete.
- **Coste:** todas las redes de Go se quedan ciegas sobre el heap de Ki (`-race`, `checkptr`, vet); hay que escribir las propias.
- **Riesgo y mitigación:** la conversión entero→puntero sobre memoria que no es de Go no es uno de los seis patrones válidos de `unsafe`, aunque el runtime la tolera a propósito (`findObject`: «This pointer may be to some mmap'd region, so we allow it») [R3 §2.2]. Canarios en cada actualización, el heap `kiheapgo` como variante más cercana a las reglas, `KI_ALLOC=debug|fence`, censo e `interp-rc`. R3 midió 4–6× frente a objetos del GC de Go con núcleos ociosos y 5–8× con `GOMAXPROCS=1`, y 16× con reutilización [R3 §7].

**(f) Ciclos con RC.** La postura es la del apartado 8.9. El coste es que no hay datos cíclicos. El riesgo es que una característica futura reintroduzca ciclos; la regla es que cualquier característica que añada mutación debe actualizar `docs/v0.md#cycles` y añadir tests de censo.

---
## 11. Verificación y repositorio

### 11.1 Estructura del repositorio

Hay dos repositorios nuevos, los dos públicos en GitHub con licencia MIT y los dos en ext4 bajo `/home` (lo decidiste tú). Ninguno lee ni reutiliza `~/ki` ni el trabajo de C++ borrado.
- **El tuyo: `~/kilang` → `aldezex/kilang`.** Lo creas tú en el capítulo 1, y empieza vacío. Escribes en él todo el código y todos los tests a partir del libro (kigen, el runner y kicheck incluidos), y su CI la montas tú en ese mismo capítulo. Es tu portfolio. Como los pushes y la creación del repo los hace tu cuenta, los ejecutas tú.
- **El de referencia: `~/kilang-ref` → `aldezex/kilang-ref`.** Lo construyo yo antes de publicar cada capítulo. De él salen los listados, y lleva las etiquetas, `book/`, `bookx` y `docs/research/`. Tú solo lo usas para compararte o para recuperarte de un atasco. Los pushes, con la cadencia que decidiste (apartado 4: uno por capítulo, antes de publicarlo), los haces tú, igual que la creación del repo en tu cuenta: el sistema de permisos me bloquea `gh repo create` y `git push`, y hasta hoy cada push lo has ejecutado tú.

**Los dos repos tienen el mismo *module path***, `github.com/aldezex/kilang` (la línea `module` del `go.mod` de abajo es la misma en los dos), así que cualquier fichero de la referencia encaja en tu repo sin cambiar un import; `kilang-ref` es el nombre del repositorio, no del módulo.

Esta es la estructura del de referencia **[decisión del diseño, con el esbozo de R4 §5.7]**. La del tuyo es la misma, sin `book/`, `cmd/bookx/` ni `docs/research/`.

```text
kilang/
  go.mod                 module github.com/aldezex/kilang ; go 1.27 ; toolchain go1.27.3 ; ignore ./build
  embed.go               package kilang: //go:embed stdlib docs/errors kirt — the FS that `ki` carries
                         (an embed pattern cannot reach a parent directory, so it lives at the root; native
                         programs import kirt, not this package, so they never carry the sources; the pattern
                         grows with the book: stdlib in ch. 7, docs/errors in ch. 19, kirt in ch. 48, because
                         a pattern naming a missing directory does not compile)
  cmd/ki/                the CLI; the only non-test package that imports internal/driver (tests/ and internal/testsupport, test-only code, import it too)
  cmd/kigen/             well-typed program generator + delta-debugging reducer
  cmd/kicheck/           project checks on go/ast + go/types: float rule, unsafe/import policy, inlining pins
  cmd/bookx/             (reference repo only) listing extractor + verifier
  internal/base/         Id types, IdVec, Arena, Interner, SmallVec, BitSet, SourceManager
  internal/diag/         Diagnostic, DiagSink, renderers, docs/errors embed
  internal/syntax/       lexer, parser, AST
  internal/sema/         names, kinds, types + effects, traits, patterns
  internal/core/         Core IR, Lint, passes S0→S5 (anf, trees, lift, perceus, ev), optimizer
  internal/interp/       the CEK oracle: values, frames, det scheduler, channel model, checked heap
  internal/mir/          MIR, verifier
  internal/vm/           bytecode emitter, verifier, disassembler, dispatch loop
  internal/x64/          isel, LIR, liveness, regalloc (+ verifier), frames, Plan 9 emitter   (internal/arm64/ in Part XI)
  internal/build/        generated module writer, writes the kirt copy from the root embed FS, `go build` driver, doctor, clean
  internal/kitest/       golden runner: test headers, backend × seed matrix, --check-passes, reducer handoff
  internal/lsp/          the language server (ch. 62–64): JSON-RPC 2.0 over stdio by hand, document sync, the
                         queries over the front end's side tables, debounce + cancellation
  internal/driver/       the commands
  internal/testsupport/  (test-only) compiles a source up to the stage a test needs; imported by external test packages only
  kirt/                  the runtime, Go only (stdlib + kirt/*): layout/ heap/ (the only unsafe importer) alloc/ rc/
                         census/ str/ array/ lazy/ ev/ fiber/ sched/ (ws + det) chan/ poll/ (epoll_linux, goroutine)
                         timer/ io/ out/ (stdout/stderr writer; leaf) panic/ splitmix/ fmtnum/ canary/
                         every system-dependent file is born with its _windows.go twin (a stub until Part XI)
  stdlib/                prelude.ki  std/*.ki   (embedded into ki through the root embed.go)
  tests/                 golden/chNN/*.ki  errors/chNN/*.ki  projects/pNN/*.ki (the «Proyectos en Ki» of Part NN)
                         errors_docs_test.go (every docs/errors example, erroneous and corrected, run through ki check)
                         diff/corpus/*.ki  stress/  litmus/  bench/  canary/
  docs/                  semantics.md (Core rules + det-scheduling spec)  abi.md (KiABI-A0)  bytecode.md
                         toolchain.md (every Go version change, with the canaries that ran)
                         v0.md (feature freeze, cycles)  errors/E*.md
                         research/ (reference repo only: R1–R4, the dossier, both checks, and proto/: the rewritten
                         verification programs behind the canaries and the carrier-A prototype, each its own module)
                         superpowers/specs/ (reference repo only)
  book/                  (reference repo only) chNN-slug.md (Spanish prose with listing references)
                         pNN-proyectos.md (the «Proyectos en Ki» page of Part NN; bookx learns it in Plan 02)
                         tests/chNN.tests (test cards)  tests/chNN.list  verified-tags.txt  (the page lives in cmd/bookx/web/)
  ci/                    ci-local.sh  check-tags.sh  fuzz-nightly.sh  canaries.sh
  .github/workflows/     ci.yml  nightly.yml   (two sets: yours, written in ch. 1 and 34; the reference's adds bookx)
```

Notas sobre el layout:
- `kirt/` va en la raíz y no bajo `internal/`, porque los módulos generados lo importan desde fuera del árbol [R4 §5.7]. Los `internal/*` del compilador sí pueden importar `kirt` (la VM lo hace), pero `kirt` nunca importa `internal/`.
- `ignore ./build` (directiva de Go 1.25) deja fuera de `./...` los árboles de etiquetas que `bookx` extrae en `build/tags/<tag>`.
- Los tests unitarios viven junto a su paquete (`*_test.go`) con sus ficheros golden en `testdata/`, como manda Go; los programas Ki de prueba viven en `tests/`.
- Los módulos de `docs/research/proto/` tienen su propio `go.mod`, así que `./...` no los ve. Son las reescrituras de los programas con los que R1–R3 verificaron sus hechos (los originales se borraron: R2 §12), y la fuente de la que salen los canarios.
- **La prosa vive en `main`** **[decisión del diseño, constructor §3.7]**. En la era C++ la web iba en una rama aparte y la prosa en un worktree; aquí `book/chNN-slug.md` y `book/tests/chNN.tests` se escriben **dentro del rango del capítulo, antes de la etiqueta `chNN`**, y `bookx verify` exige que existan en ella. La historia sigue siendo lineal y cada etiqueta de capítulo es autocontenida: código, tests, tarjetas y prosa. Una fe de erratas sobre un capítulo publicado es un commit hacia delante que solo toca `book/`. El libro publicado (el artifact) se genera desde `book/` con `bookx web` (la página vive en `cmd/bookx/web/`), y no se versiona el resultado.

**Herramientas de test** **[decisión del diseño]**.
- **El framework de tests unitarios es `testing`, de la biblioteca estándar**, no uno escrito a mano **[decisión tuya, 2026-10-24]**. El `ki_test` de C++ existía porque GoogleTest y Catch2 eran de terceros; `testing` es la biblioteca estándar, que es exactamente lo que tu regla permite, y es lo que cualquier empleador espera ver: `TestXxx`, subtests, tests de tabla, `t.Helper`, `t.Cleanup`, `go test -run/-count/-shuffle/-json/-cover`, `-fuzz`, `-bench` con `b.Loop`, `T.ArtifactDir` [R4 §3.2]. Lo que sí se escribe a mano, porque Go no lo trae: el runner de golden de Ki (`ki test`), el formato de tarjetas, kigen, el reductor, bookx y kicheck. R4 lo señaló como decisión tuya y la tomaste el 2026-10-24: `testing`.
- Los tests golden en Ki llevan una cabecera que se lee por máquina (en inglés, porque es un comentario), sin cambios:

```ki
// test: T21.07
// backends: interp interp-rc vm vm-bubble native
// seeds: 1..20     schedule: independent
// expect-exit: 0
// expect-stdout:
// | error: boom
// | 0
```

- Los tests de diagnóstico marcan el error esperado en línea: `// ERROR(E0401) @3:17`.
- `ki test` ejecuta los binarios nativos como subprocesos con `os/exec` y lee su código de salida; nunca `go run` [R4 V13].

### 11.2 Etiquetas, sesiones y extracción de listados

**Historia y etiquetas.** La historia de `main` es lineal. Cada capítulo termina en una etiqueta anotada `chNN` (el 54 tiene dos, `ch54a` y `ch54b`, porque se publica en dos mitades: apartado 12.3). Cada sesión de 2–3 h dentro de un capítulo termina en verde con un commit y una etiqueta de punto de control `chNN-sK`. Cada hito tiene su etiqueta `mN`. Una etiqueta de sesión vale cuando `ci/ci-local.sh` está en verde sobre ella en esta máquina; una etiqueta de capítulo, cuando además la CI de GitHub lo está, y entonces se anota en `book/verified-tags.txt`. La cadencia es decisión tuya (apartado 4): un push por capítulo, hecho por ti, antes de publicarlo, y un capítulo solo se publica con la CI de GitHub en verde sobre su etiqueta `chNN`.

**Marcadores en el código fuente:** `// @book <region> full|hint "text"|solution|test` … `// @end` delimita una región; las líneas `@elide` se muestran como `// ...`; los nombres de región son estables entre capítulos.

**Referencias desde el libro:** `{{file chNN-sK path}}`, `{{listing chNN-sK region}}` y `{{diff chNN-sK..chMM-sJ path}}` citan etiquetas de sesión (la etiqueta `chNN` no existe mientras se escribe la prosa del capítulo); `{{reftest TNN.MM}}` y las regiones de modo `solution` se muestran plegados; `{{cards}}` es la tabla de tarjetas.

**`bookx`**, ahora en Go y dentro del repo (`cmd/bookx`):
- Lee cada fichero con `git show <tag>:<ruta>`, sin worktrees ni clones.
- Para construir y probar una etiqueta en la CI, usa `git archive <tag> | tar -x -C build/tags/<tag>`, ejecuta `go test ./...` ahí (la caché de `go build` hace que las etiquetas consecutivas compartan casi todo) y borra el directorio al acabar.
- Falla si una región falta, está duplicada o desequilibrada; si la etiqueta no está verificada; si en la etiqueta `chNN` faltan la prosa o las tarjetas del capítulo; si un caso de test no tiene test de referencia, o al revés; o si un hash no coincide con el libro publicado.

**Cómo te comparas con la referencia.** Traes sus etiquetas a tu repo con un prefijo, para que no choquen con las tuyas:

```text
git remote add --no-tags ref https://github.com/aldezex/kilang-ref.git
git fetch ref 'refs/tags/*:refs/tags/ref/*'
git diff ref/ch05 -- internal/syntax/
```

El repo de referencia contiene todas las soluciones. Si no quieres verlas antes de tiempo, no lo mires; el libro ya las pliega.

**Correcciones** **[decisión del diseño]**. Antes de publicar un capítulo: si un capítulo posterior destapa un fallo de código anterior, lo corrijo en su sitio con commits `--fixup` y un `rebase --autosquash --update-refs` no interactivo, muevo las etiquetas y la CI vuelve a verificarlas todas (en GitHub eso es un `push --force` de `main` y de las etiquetas movidas, que solo puede tocar etiquetas aún no publicadas; tu decisión del apartado 4 lo permite). Si el fallo es didáctico, lo dejo como un recuadro «Bug encontrado» en el capítulo que lo descubre. **Como el libro se publica de forma incremental, la historia de los capítulos ya publicados está congelada**: una corrección que los toque va hacia delante, con fe de erratas en el capítulo publicado y la corrección explicada en el capítulo donde aparece. Eso es lo que cambia con tu decisión de publicar ya.

### 11.3 Formato de los casos de test

Cada capítulo tiene `book/tests/chNN.tests`, y el libro lo muestra como la tabla «Tests primero». Las claves están en inglés porque es un formato de fichero; el texto, en español porque es prosa del libro.

```text
[T05.07] lexer.pipe_continues_line                         level: core
kind:    unit (go test)      file: internal/syntax/lexer_test.go
case:    una línea que empieza por `|>` continúa la expresión de la línea anterior
input:   "let total = xs\n  |> sum\n"
expect:  ningún Newline entre `xs` y `|>`; después, Newline y Eof
why:     tus tuberías ponen `|>` al principio de la línea; si el lexer insertara aquí un Newline,
         la sentencia terminaría en `xs` y el error saldría una línea más abajo, lejos de su causa
ref:     TestLexer/pipe_continues_line                       ← folded in the book
```

**Tipos (`kind`):** `unit` (test unitario con `testing`); `golden` (volcados de IR en `testdata/`); `diag` (instantánea del diagnóstico más la anotación en línea); `run` (salida y código de salida); `diff` (todos los backends); `det` (rangos de semillas); `stress` (estrés); `prop` (generador más propiedad, o un fuzz target); `bench` (rendimiento; nunca bloquea la CI); `canary` (un hecho del runtime de Go del que depende Ki; apartado 11.4).

**Nivel (`level`):** `core`, debe pasar antes de seguir; `stretch`, opcional si vas justo de tiempo.

**Cómo se usa.** Escribes cada test a partir de `case`, `why` y `expect` antes de implementar, lo ves fallar, implementas, y solo entonces despliegas la referencia para comparar.

### 11.4 CI, toolchain y política de versiones de Go

**Toolchain** **[decisión del diseño]**.
- `go.mod` fija la línea `go` y la línea `toolchain` a la versión con la que empieza el libro: **`go 1.27` + `toolchain go1.27.3`** (tu decisión, apartado 4). Esa versión ya está instalada en `~/.local/opt/go1.27.3`, con `go` en el `PATH`. Antes del capítulo 1 se reverifican sobre ella los hechos que usan las Partes I y II: el toolchain, `go mod init` (en 1.26.5 escribía la versión completa en la línea `go` [R4 V15]; se comprueba qué hace 1.27.3; el capítulo 1 lo deja como `go 1.27` + `toolchain go1.27.3`), la directiva `ignore`, los flags de `testing` y los métodos genéricos sobre tipos concretos. Los hechos de runtime y ensamblador de R1–R3 se revalidan con los canarios, cada uno en el capítulo que lo usa; las marcas «1.26.5» del documento son la procedencia de la investigación, no una versión a usar. `docs/toolchain.md` empieza con la versión de partida y lo reverificado sobre ella.
- La CI usa `setup-go` con `go-version-file: go.mod` y `GOTOOLCHAIN=local`, así que nunca descarga un toolchain en silencio. `ki build` también exporta `GOTOOLCHAIN=local` al invocar `go build`, y escribe en el `go.mod` generado la versión local (apartado 9.3).
- **Política de actualización.** Go publica dos versiones mayores al año y mantiene las dos últimas; 1.27 deja de recibir parches cuando salga 1.29 (hacia agosto de 2027, al ritmo de dos versiones al año), y el libro dura más que eso. La regla: Ki siempre está en una línea con soporte; los parches menores se adoptan en cuanto salen, con los canarios en verde; una versión mayor se adopta solo **al final de una parte**, junto a la replanificación de horas, y solo si la suite de canarios pasa. El libro empieza en 1.27, así que la primera candidata a un cambio mayor es 1.28 (hacia febrero de 2027), al final de la parte en curso. Cada cambio de versión deja una línea en `docs/toolchain.md` con los canarios que corrieron. Los experimentos (`GOEXPERIMENT`) no entran nunca en el tronco [R4 §11.6]. Delve se instala a una versión fijada que soporte el toolchain, anotada en el mismo fichero, nunca con `@latest`.
- La prosa del libro dice «en Go 1.27» cuando un hecho es de versión, y cada hecho de versión que aparece en el libro está fijado a un test, como los listados [R4 §11.5].

**Canarios** **[decisión del diseño, R1 §12, R2 §11, R3 §10]**. Ki depende de hechos del runtime de Go que funcionan hoy y que ningún documento promete. Cada uno tiene un test en `kirt/canary` o `tests/canary`, corre en cada push y es la puerta de cada actualización. Los programas con los que R1–R3 verificaron estos hechos **ya no existen** (R2 §12 dice que se borraron; solo quedan los resúmenes), así que cada canario se reescribe en `kilang-ref` desde su resumen (`docs/research/proto/`, con el canario en `kirt/canary`) **antes del capítulo que lo usa**, y su primera ejecución tiene que reproducir el número o el fallo que el informe cita; un canario reescrito mal pasaría en verde sin comprobar nada (riesgo 34) [T §2.7]:

| # | Hecho | Fuente |
|---|---|---|
| C1 | Una función ABI0 sin prototipo (`$F-0`) con argumentos en `FP` sobrevive a 10⁶ marcos, crecimiento de pila, `runtime.GC()` y `GODEBUG=gccheckmark=1,invalidptr=1` | R1 n14 |
| C2 | `DX` sobrevive a `morestack` en una función `NEEDCTXT` | R1 n2b |
| C3 | Con bucles nativos corriendo y el poll en línea (desplazamiento 16 de `stackguard0`), `runtime.GC()` desde otra goroutine tarda < 10 ms con `GOMAXPROCS ≥ 2`, y < 50 ms con un solo P (R3 V4 midió 20 ms con un P) | R1 §4.2, R3 V4 |
| C4 | Una hoja que se llama a sí misma en cola sin `CALL`, **y un par de hojas que se llaman en cola entre sí**, son rechazadas (`nosplit … infinite cycle`), y con un `CALL` inalcanzable enlazan y dan el resultado correcto | R1 n5, R2 P3, T V-t3 |
| C5 | Un trampolín en el mismo paquete produce el wrapper `.abi0` y la llamada desde ensamblador enlaza y corre | R2 P3 |
| C6 | `RET ·f(SB)` deja la pila plana en 10⁸ llamadas de cola mutuas | R2 P3 |
| C7 | Un `Value` con dirección de memoria `mmap` sobrevive a GCs; un objeto de Go referenciado solo desde el heap de Ki se recoge | R3 V1, V7 |
| C8 | `-race` ignora el heap `mmap` y ve el heap `kiheapgo`; el accesor `//go:nocheckptr` pasa `checkptr` | R3 V3, V8 |
| C9 | La convención de `#line` (primera instrucción tras `#line N` atribuida a N−1 en 1.26.5) | R1 n13, T V-t8 |
| C10 | Las primitivas de `Float` dan los mismos bits en builds `GOAMD64=v1` y `GOAMD64=v3` (ejecutadas); la pata `GOARCH=arm64` es estática (`-gcflags=-S` sin `FMADDD|FMSUBD|FNMADDD|FNMSUBD`) hasta que exista qemu en el capítulo 66, y desde entonces también se ejecuta | R4 V11 |
| C11 | Los accesores de `kirt/heap` y los caminos rápidos nombrados se inlinean (`-gcflags=-m`) y el acceso a la ventana `*[256]Value` de la VM con operandos `uint8` no tiene `IsInBounds` | R3 §3.6, R4 V23, T V-t5 |
| C12 | (solo si `--kiabi=b` está activo) humo de B′: crecimiento, GC y preempción | R1 §10 |
| C13 | Una cola indirecta `ADJSP $-F; POPQ BP; JMP CX` con la compensación inalcanzable `PUSHQ BP; ADJSP $F` sobrevive a 10⁸ saltos con `runtime.GC()` concurrente y `gccheckmark=1,invalidptr=1`, y sin la compensación muere en el primer escaneo de pila | T V-t11 |
| C14 | El portador A de punta a punta en ensamblador: `kiResume`, burbujeo hasta la raíz, aparcar y despertar, crecimiento de pila y GC durante el burbujeo, y un canal entre dos fibras | prototipo de la referencia, antes de publicar el capítulo 32 (riesgo 4) |

**Dos juegos de workflows** **[decisión del diseño, constructor E3]**. Los de **tu repo** los escribes tú: `ci.yml` en el capítulo 1 y `nightly.yml` cuando existe kigen (capítulo 34). Los de **kilang-ref** son los mismos más lo que solo tiene sentido con `book/` y `bookx`: la verificación de listados en cada push y, de noche, la reconstrucción de todas las etiquetas de capítulo y `bookx verify`. `ci/ci-local.sh` ejecuta la matriz de un push en esta máquina, y es la puerta de cada etiqueta de sesión de la referencia.

**Matriz por cada push** (`ci.yml`, un contenedor `ubuntu:26.04` como WSL, runner `ubuntu-latest` amd64):
- `gofmt -l` vacío; `go vet` en sus dos invocaciones (apartado 7.2); `cmd/kicheck`.
- `go test ./...` normal, y `go test -race -tags kiheapgo ./...` para la VM y el runtime. La razón de «`-race` siempre con `kiheapgo`» es que sin el heap de depuración `-race` no es solo ciego sobre el heap `mmap`: da **falsos positivos** cuando estado de Go se publica por atómicos del heap de Ki [R3 V3, §2.5]. Sin `kiheapgo`, `-race` sí sirve para el compilador y para las estructuras del lado Go de `kirt` (deques, mutex y colas de los canales, poller), que es lo que R2 P6 y P12 probaron. Lo que ningún modo cubre es el código **emitido** por Ki: `-race` nunca instrumenta ensamblador (riesgo 32) [T §2.25].
- Compilación cruzada de `./cmd/ki` para `windows/amd64` y `linux/arm64` desde el capítulo 1: cuesta segundos y detecta la deriva de portabilidad mucho antes de la Parte XI [R4 §5.8]. Para que siga en verde desde el capítulo 37, cuando `kirt/heap` empieza a usar `syscall.Mmap`, `Mprotect` y `Madvise`, que no existen con `GOOS=windows` [T V-t4], cada fichero dependiente del sistema nace con su pareja `_windows.go` **[decisión del diseño, constructor E4]**: `heap_windows.go` es un stub que falla al arrancar con «Windows heap arrives in chapter 70», el poller se elige por sufijo (`poll_goroutine.go` compila en todas partes) y el futex crudo va en un `_linux.go`. El capítulo 70 sustituye los stubs por `VirtualAlloc` vía `NewLazyDLL`.
- unit, golden, diag y run en cada backend que exista en esa etiqueta; `--poll=inline` y `--poll=call` en los tests nativos; censo de fugas cero; semillas 1..200; estrés ×50; canarios; en kilang-ref, verificación de listados.

**Nightly** (`nightly.yml`): kigen de 30 a 60 minutos × los backends que existan; semillas 1..100000; `go test -fuzz` sobre el lexer, el parser y el verificador de bytecode con `-fuzztime`; benchmarks a un CSV, sin bloquear nunca. En kilang-ref, además: todas las etiquetas de capítulo reconstruidas y probadas (`git archive` en `build/tags/<tag>`) y `bookx verify`.

**Más adelante:** `ubuntu-24.04-arm` (hardware ARM real, para los tests de orden de memoria, porque QEMU no muestra los reordenamientos) desde el capítulo 67, y `windows-latest` desde el 69. Como los repos son públicos, los runners estándar son gratis y sin límite de minutos.

**Cómo se verifica el repo de referencia** **[decisión del diseño]**. Son unas 300 etiquetas de sesión: cada etiqueta de sesión y de capítulo compila y pasa `ci/ci-local.sh`, en orden, con la caché de `go build` compartida en esta máquina; solo en las etiquetas de hito corren kigen durante horas, los barridos de semillas grandes y el estrés largo con `-race`; tras una corrección, solo se reverifican las etiquetas posteriores. Un capítulo solo se publica con la CI de GitHub de kilang-ref en verde sobre su etiqueta `chNN`, lo que exige que esa etiqueta esté empujada: es el push por capítulo que decidiste (apartado 4).

### 11.5 Requisitos de la máquina

Lo que ya hay: Go 1.27.3 en `~/.local/opt/go1.27.3` (instalado a mano; `~/.local/opt/go` apunta a él y `go` está en el `PATH`), gcc 15.2 (para `-race`), git y `gh`, 16 CPUs, 30 GB.

Lo que se instala, y cuándo **[decisión del diseño; lo que lleva `sudo` no se te preguntó y queda con el defecto del diseño: lo instalas tú cuando toque]**:

| Qué | Cómo | Cuándo | sudo |
|---|---|---|---|
| Delve (`dlv`), el depurador de Go | `go install github.com/go-delve/delve/cmd/dlv@vX.Y.Z`, a la versión fijada en `docs/toolchain.md` que soporte el toolchain (es una herramienta; no entra en `go.mod`; nunca `@latest`) | capítulo 10 | no |
| gdb | `sudo apt install gdb` | opcional, capítulo 47 (depurar ensamblador generado) | sí |
| qemu-user y binfmt | `sudo apt install qemu-user qemu-user-binfmt` | capítulo 66 | sí |
| perf | no se instala: `pprof` y `go tool trace` lo sustituyen; perf queda como opcional en el capítulo 58 | — | — |

No hace falta ningún `sysctl` (TSan ya no existe en el plan) ni ningún toolchain cruzado: `GOOS`/`GOARCH` bastan. Los binarios de Windows se prueban ejecutando el `.exe` a través de la interoperabilidad de WSL, como hizo R1.

---

## 12. El libro

### 12.1 Formato

**Título** **[decisión del diseño, provisional: no se te preguntó y se mantiene hasta que propongas otro]**: *Ki: de cero a experto. Construye tu propio lenguaje funcional en Go*.

**Publicación.**
- Es un artifact web de varios ficheros, el que ya existe: `https://claude.ai/artifact/F9s6xctJx1vRvagDt2aQUL` (privado, con las capacidades `db` y `user`; hoy muestra el aviso «El libro se está rehaciendo en Go», comprobado al revisar este documento). Esa URL y su base de datos se conservan: el libro se lee antes de cada publicación y se republica en el mismo sitio, nunca en un artifact nuevo. Índice lateral, solo para escritorio y con diagramas SVG en línea.
- **Se publica de forma incremental** (lo decidiste hoy): cada capítulo aparece cuando su etiqueta está verificada y su prosa revisada; el índice completo está visible desde el principio con los capítulos pendientes marcados.
- Prosa en español; código, identificadores, comentarios y mensajes en inglés.
- Sigue en la «Wiki de artefactos».

**Progreso guardado.** Usa la base de datos persistente del artifact, no `localStorage`, que podría borrarse en dos años **[decisión del diseño]**. Por capítulo guarda si lo has leído, qué sesiones has hecho, qué tests has escrito, qué ejercicios has resuelto y las horas reales. Con esas horas se recalibra el plan. Un solo lector: la base de datos guarda un solo progreso.

**Resaltado de código.** Dos tokenizadores pequeños escritos para el libro, uno de Ki y otro del ensamblador de Plan 9 (las bibliotecas de resaltado de CDN no traen una gramática del ensamblador de Go, y la sintaxis `·`, `(SB)`, `$F-0` y `#line` merece colores propios), más una biblioteca de resaltado de CDN para Go. La regla de «todo a mano» es para Ki, no para la página web del libro **[decisión del diseño, constructor §3.9]**.

### 12.2 Plantilla de capítulo

1. **Objetivo y hito:** el comando que podrás ejecutar al terminar.
2. **Teoría**, con recuadros «Si vienes de Rust / TS / Python». El de Rust es el que más pesa: propiedad frente a GC, traits frente a interfaces, `Result` frente a `error`, enums frente a interfaces selladas [R4 §5.2].
3. **Tests primero:** las tarjetas de casos (caso, por qué, esperado, nivel). Tú escribes los tests y la referencia está plegada.
4. **Implementación por sesiones de 2–3 h:** listados completos para los conceptos nuevos y pista con solución plegada para lo conocido. Cada sesión termina en verde con un commit, y puedes compararte con la etiqueta `chNN-sK`.
   - Como empiezas de cero en compiladores, un concepto va siempre completo la primera vez que aparece **[decisión del diseño, a partir de tu respuesta]**. La pista con solución plegada queda para cuando se repite un patrón que el libro ya enseñó y para lo que ya dominas por Rust, TS o Python.
   - Las interfaces se dan siempre completas, aunque la implementación vaya con pista: tipos exportados, estructuras de IR y APIs **[decisión del diseño]**. Así tu código encaja con los listados posteriores aunque lo hayas escrito a tu manera.
   - Si te atascas o tu versión se desvía demasiado, copias el fichero de la etiqueta de referencia y sigues.
5. **Ejecútalo:** la salida que tienes que ver.
6. **Ejercicios**, con soluciones plegadas.
7. **Preguntas de repaso**, con respuestas plegadas.
8. **Resumen de teoría.**
9. **«Go en el trabajo»:** cómo se usa en la industria lo que acabas de aprender. Sustituye a «C++ en el trabajo» y es el recurso para tu desempate, la empleabilidad. Ningún capítulo dice «nada nuevo de Go». Los temas, por parte [R4 §9]:
   - **I:** `gofmt` como contrato social; Effective Go y la guía de estilo de Google (nombres de paquete sin tartamudeo); tests de tabla y golden como estilo de casa del propio repo de Go; `go vet` y `-race` en toda CI; los clásicos de entrevista (aliasing tras `append`, nil tipado, orden de los maps, variables de bucle antes y después de 1.22).
   - **II:** «acepta interfaces, devuelve structs»; cuándo usar genéricos; `internal/` como frontera real; comentarios de documentación; errores envueltos en las fronteras.
   - **III:** iteradores en APIs públicas (convenciones de 1.23); el lexer de funciones de estado de Rob Pike; `sync.OnceValue`; máquinas de estados en protocolos.
   - **IV:** pipelines y fan-out/fan-in; las fugas de goroutines como el fallo de producción número uno y cómo verlas; `synctest` en lugar de `sleep`; `context` de punta a punta.
   - **V:** fuzzing nativo en proyectos reales; `go test -json` en paneles de CI; `T.ArtifactDir` para guardar entradas que fallan; tests de subprocesos.
   - **VI:** dónde usa `unsafe` y `mmap` el Go de producción (bases de datos embebidas sobre B+trees mapeados); cachés fuera del heap; `GOMEMLIMIT` en contenedores; leer `-gcflags=-m` y `-S` en revisiones de rendimiento.
   - **VII:** G/M/P, `GOMAXPROCS` y la cesión del P en syscalls como material estándar de entrevista para backend e infraestructura; `GOMAXPROCS` consciente del contenedor (1.25); mutex frente a canal; perfiles de mutex/bloqueo.
   - **VIII:** ensamblador de Go en la biblioteca estándar (`crypto`, `math/big`); generadores que emiten ensamblador de Go (`math/big/internal/asmgen` dentro del propio árbol de Go [R1 §8]) como el patrón que el backend de Ki reproduce; `GOSSAFUNC=f` para leer la SSA del compilador; contribuir a `cmd/compile`.
   - **IX:** PGO en producción (perfiles de prod, `default.pgo` en el repo); profiling continuo; `GODEBUG=gctrace=1`; optimizar solo con un perfil delante.
   - **X:** CLIs con `flag` y subcomandos; servidores `net/http` con timeouts explícitos; apagado ordenado; `slog`; `net/http/pprof` tras autenticación; `-trimpath`, `-ldflags -X`, `go version -m`, builds reproducibles, govulncheck; y, con el servidor de lenguaje (62–64), cómo se escribe un servidor sobre stdio que no bloquea al editor: `context` para cancelar el trabajo que ya no interesa, un *debounce* con `time.Timer`, goroutines que no se fugan, y `gopls` como el LSP de Go que ya usas desde Neovim.
   - **XI:** la compilación cruzada de un comando como argumento comercial de Go; `CGO_ENABLED=0` y contenedores distroless; arm64 en la nube; ficheros por plataforma y matrices de CI.
   - Las afirmaciones sobre prácticas de proyectos concretos (Kubernetes, etcd) que R4 anotó de memoria se comprueban antes de escribir cada recuadro [R4 §11.9].

### 12.3 Presupuesto de horas y calendario

Las horas son **mi estimación para ti, no datos medidos**, con el método de R4 §7: los capítulos de algoritmos conservan sus horas; los de mecánica del anfitrión bajan un 30–50 % (sin CMake, sin framework propio, sin sanitizers, sin ODR, sin plantillas, sin ELF/`ld`, sin MinGW); los capítulos donde Go pelea con el diseño suben (allocator en memoria que el GC no ve, ensamblador de Plan 9 y ABI0, fibras nativas bajo el runtime de Go).

- **Tronco (2026-10-24):** unas **794 h** en 68 capítulos (el 54 en dos mitades) y 11 partes. R4 estimó 780 h. El borrador de hoy bajó a 776 con tres ajustes por R2: +16 h por el capítulo de S5, que sí sigue el método de R4, y −4 h en «efectos en la VM» (ejecutar S5 en vez de capturar segmentos) y −16 h por fundir los dos capítulos nativos de pilas y efectos en uno, que son **estimaciones del diseño**, no cifras de R2 ni de R4 (R2 §10 propone la fusión sin dar horas) [T §4]. La revisión suma 18 h: +2 en el 33 y +2 en el 41, el oráculo y la VM burbujeando `Async` y kigen a profundidad variable (T E7); +2 en el 51, las colas indirectas y C13 (T E2); y +12 al estimar el capítulo 54 **sin el tope** y partirlo en dos mitades de 14 h, 54a y 54b, porque llevaba el trabajo menos verificado del libro justo en el tope de 16 h, que es un tope y no una estimación. El 32 absorbe la raíz de fibra del oráculo (unas 2 h) a cambio de la lectura guiada del prototipo de R2, que ya no existe. Es un 5 % menos que las 836 h de la edición C++: el libro era ya sobre todo compiladores y runtime, y eso no cambia [R4 §7].
- **2026-10-28, +62 h:** el tronco pasa a **≈ 856 h en 71 capítulos**. +3 en el capítulo 7 (la sesión `ch07-s4` de diagnósticos al nivel de rustc: 2,5 h de sesión; el índice cuenta horas enteras y redondea al alza); +30 en los tres capítulos nuevos del servidor de lenguaje (62, 63 y 64: 9, 11 y 10 h); +29 en las cinco secciones «Proyectos en Ki» (5 h al final de la Parte II, 5 en la III, 7 en la VII, 5 en la X y 7 en la XI), que son programas que escribes con lo que ya tienes y no capítulos, así que **sus horas están en la cabecera de su parte y en ninguna fila del índice**. Los antiguos capítulos 62–68 pasan a 65–71 sin cambiar de horas. Las dos decisiones que faltan (el `await` y la compilación dirigida por los efectos) se reparten dentro de las horas de los capítulos 20, 25, 26, 32, 44, 46, 48, 53, 54 y 55, que ya cubrían esos mecanismos; si al cerrar sus partes se ven cortas, se recalibran entonces, como manda la regla de abajo.
- **Reserva:** un 15 % para depurar, unas 128 h, hasta **≈ 984 h**, dentro de tu margen de 500–1000 h, ya justo en su borde: el siguiente añadido al tronco tiene que salir de un recorte. Rango plausible del tronco: 835–905 h. La Parte VIII lleva la barra más ancha, **±20 h**: la barra de ±10 h de R4 era para otro mecanismo (las pilas propias de Ki, la opción A de su F1), que ya no existe, y el diseño que la sustituye está menos probado; se estrecha cuando el prototipo del portador A del riesgo 4 esté hecho. La Parte I puede salir por debajo [R4 §7, §11.7; T §4].
- **Topes:** ningún capítulo pasa de 16 h, con una excepción nombrada: el 54 se estima en 28 h y se publica en **dos mitades de 14 h, 54a y 54b**, cada una con su etiqueta (`ch54a`, `ch54b`) y su cierre en verde, porque partirlo en dos números movería los capítulos 55–71 y todas sus referencias. Ningún tramo entre hitos pasa de unas 100 h (el mayor sigue siendo M5→M6, 92 h; M11→M12 pasa a 76 h con los tres capítulos del LSP).
- **Re-planificación:** al final de cada parte, comparando con las horas reales, y en el mismo momento se decide la actualización de Go (apartado 11.4).

| Ritmo | Primer programa (M1, 81 h) | Libro entero (856 h) | Con reserva (984 h) |
|---|---|---|---|
| 5 h/semana | ≈ 3,7 meses | ≈ 3,3 años | ≈ 3,8 años |
| 7,5 h/semana | ≈ 2,5 meses | ≈ 2,2 años | ≈ 2,5 años |
| 10 h/semana | ≈ 1,9 meses | ≈ 1,6 años | ≈ 1,9 años |

A 7,5–10 h por semana, M1 cae dentro de los 2–3 meses que aceptaste. A 5 h, llega hacia el cuarto mes.

**Orden de recorte**, si las horas reales se disparan:
1. el capítulo 57 (linear scan II) pasa al epílogo;
2. el servidor HTTP del capítulo 61;
3. el capítulo 56 (representaciones sin caja); `Int` y `UInt64` seguirían teniendo 64 bits, solo perderían velocidad;
4. solo si el portador A no se sostiene en el capítulo 54 (y el prototipo del riesgo 4 no lo ha dicho antes): el hito M10 pasa a «nativo con un worker, multinúcleo en la VM» y el portador B entra como adaptador [R4 §7].

**Nunca se recortan:** las herramientas de test, los errores legibles (con su listón del apartado 7.3), los canarios ni los capítulos finales de Windows y ARM64. Si hiciera falta recortar lo añadido el 2026-10-28, el orden es: primero el tercer proyecto de cada sección «Proyectos en Ki», después el 64 (la integración con Zed pasa al epílogo y el 64 se queda con la capacidad de respuesta y Neovim), y nunca los diagnósticos ni el `await`.

### 12.4 Hitos

| Hito | Capítulo, horas acumuladas | Qué ves |
|---|---|---|
| **M1** | cap. 10, ≈ 81 h | `ki run fib.ki` imprime `6765` por el pipeline definitivo. Los diagnósticos con caret funcionan, al nivel de rustc y con su catálogo `docs/errors`, y un desbordamiento da un panic con salida 101 |
| **M2** | cap. 12, ≈ 106 h | Tu programa `Shape`/`area`/`match`, con errores golden: un brazo que falta (con testigo), brazos de tipos distintos, un nombre desconocido y un argumento mal tipado |
| **M3** | cap. 16, ≈ 155 h | Tu ejemplo original exacto: `[Circle(1.0), Rect(2.0, 3.0)] \|> map(area) \|> sum \|> print` → `9.141592653589793` |
| **M4** | cap. 24, ≈ 244 h | La parte de efectos del ejemplo ampliado, más streams perezosos (tras los 5 h de «Proyectos en Ki» de la Parte II) |
| **M5** | cap. 26, ≈ 277 h | El ejemplo ampliado completo en el oráculo, `await` incluido, con los mismos bytes para cada semilla (tras los 5 h de proyectos de la Parte III) |
| **M6** | cap. 34, ≈ 369 h | Programas aleatorios bien tipados coinciden en todas las etapas S0–S5, S5 incluida y con `Async` burbujeando hasta la raíz en el oráculo; kigen abandona continuaciones en cada `perform` y bloquea canales a profundidad variable; los casos encogidos quedan en el corpus |
| **M7** | cap. 41, ≈ 456 h | Toda la suite secuencial corre en la VM, idéntica al oráculo y sin fugas, con los efectos ejecutando S5; `vm-bubble` entra en la matriz |
| **M8** | cap. 46, ≈ 524 h | El ejemplo ampliado y un servidor echo TCP en la VM con 16 workers. El modo determinista del runtime coincide con el del intérprete en cada semilla. Después, los 7 h de proyectos de la Parte VII (chat TCP, caché clave-valor, limitador de tasa) |
| **M9** | cap. 50, ≈ 582 h | Los primeros binarios nativos, construidos por `go build` a partir del ensamblador emitido por Ki más `kirt`, sin planificador ni poller dentro. «Secuenciales y sin efectos de usuario»: sin `Async` ni handlers, con `print` permitido (apartado 12.5, capítulos 49–53) |
| **M10** | cap. 54b, ≈ 655 h | Intérprete, VM y nativo coinciden en la suite y en el fuzzer, y el servidor echo corre en nativo con 16 workers sobre tu planificador, que arranca en el primer `spawn` |
| **M11** | cap. 58, ≈ 707 h | Mejoras de velocidad medidas con pprof y PGO, con la salida todavía idéntica; `map`, `sum` y `\|>` sin comprobaciones de cesión en los programas sin `Async` |
| **M12** | cap. 65, ≈ 783 h | Ki v0.1: una CLI paralela, un servidor HTTP, `ki lsp` sirviendo a Neovim y a Zed, `ki fmt`, `ki explain` y `kicheck`. Después, los 5 h del Mandelbrot paralelo (proyectos de la Parte X) |
| **M13** | cap. 68, ≈ 823 h | La suite completa pasa en AArch64, y `ki` mismo corre en ARM64 |
| **M14** | cap. 71, ≈ 849 h | Un `.exe` de Ki corre en Windows, y `ki` mismo también. Después, los 7 h del mini-Lisp en Ki (proyectos de la Parte XI), que cierran el libro en ≈ 856 h |

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
- **Intérprete CEK.** Pila de marcos explícita como slice, llamadas de cola sin marco y enteros de 64 bits con comprobación explícita de desbordamiento (el desbordamiento con signo está definido en Go, así que la comprobación es aritmética, no un intrínseco).
- **`print` es la primera forma de un mecanismo permanente.** El preludio mínimo, `stdlib/prelude.ki`, la declara como `#[extern("ki_print_int")] pub fn print(x: Int)`, y `ki` lo lleva incrustado desde el capítulo 7 con el `embed.go` de la raíz (`//go:embed stdlib`, el patrón crece en los capítulos 19 y 48). Las primitivas `extern` del preludio no se tiran nunca; en el capítulo 14, el propio preludio cambia `print` a `pub fn print<T: Show>(x: T) uses {IO}`, que llama a la primitiva `ki_write` de `kirt/out`, el escritor que ya existe desde este capítulo.
- **Errores.** `ki check` muestra `E0301` (nombre desconocido `fibb`, «did you mean `fib`?», con el fix-it que lo sustituye), `E0401` (ramas del `if` `Int` frente a `Bool`, con las dos etiquetas) y varios `E02xx` en una sola pasada, todos al nivel del apartado 7.3 y con su entrada en `docs/errors`.
- **Panic.** `print(9223372036854775807 + 1)` sale con `panic: integer overflow at overflow.ki:2:29` y código 101, con el buffer de salida vaciado antes de `os.Exit`.
- **Alrededor.** `go test ./...` también con `-race`, `go vet`, `gofmt -l`, el runner de golden (sobre binarios construidos, no `go run`), y la CI de GitHub con la comprobación de compilación cruzada para windows/amd64 y linux/arm64 [R4 §7].

### 12.5 Índice

Cada fila lleva el hito del capítulo, el Go nuevo que enseña, los conceptos de compiladores y las horas estimadas. Go se enseña desde cero: las bases van cargadas al principio de la Parte I; goroutines, canales y `select` en la Parte IV, donde Ki copia ese modelo; las entrañas del runtime de Go (G/M/P, GC, preempción, copia de pilas, `unsafe`) justo antes de los capítulos de runtime, en el capítulo 35; el ensamblador de Go y el ABI antes de la parte nativa, en el 47 [R4 §5]. Los títulos marcados con ✎ cambian respecto a la edición C++. Los capítulos 32–53 van desplazados en uno respecto a ayer: entra «S5» en el 32 y se funden los dos capítulos nativos de pilas y efectos en el 53. El 54 se publica en dos mitades, 54a y 54b, con etiqueta y cierre propios (apartado 12.3). Las Partes IX y X conservan su numeración hasta el 61; el 2026-10-28 entran los tres capítulos del servidor de lenguaje, 62–64, y los antiguos 62–68 pasan a ser 65–71 (G22).

**«Proyectos en Ki»** **[tu decisión, 2026-10-28]**. Al final de las Partes II, III, VII, X y XI hay una sección con programas completos escritos en Ki con lo que el lenguaje ya sabe hacer en ese punto, ordenados por las características disponibles: una página propia del libro web cada una (`book/pNN-proyectos.md`, que `bookx` aprende a renderizar y verificar en el plan de la Parte II; sus programas son goldens de `tests/projects/pNN/`), sin número de capítulo ni etiqueta `chNN` propia: sus programas entran en el repo en una sesión más del último capítulo de la parte, antes de la etiqueta de ese capítulo, y la página se publica con él. Sus horas van en la cabecera de la parte y no en ninguna fila: para las cinco partes con proyectos, la cabecera suma sus filas más la sección.

Los capítulos nativos 49–53 necesitan una regla **[decisión del diseño, constructor §3.1 y T §2.2; reescrita el 2026-10-28]**: la fila de `main` se infiere (apartado 5.6) y `print` es `uses {IO}`, e `IO` a solas no cede, así que S5 **no** pone ninguna comprobación tras `print`; la pone tras cada llamada a una función con variable de fila (`map`, `sum`, `|>`), porque su cuerpo se compila una vez con la fila abierta, hasta que la especialización de efectos del 55 clone esas funciones para las filas que no ceden (apartado 6.1). El camino lento (`bubble_k`, `kiYieldExtend`) no llega hasta el 53, ni la raíz de fibra con `kiResume` hasta el 54. Hasta el 53, el backend emite esas comprobaciones con `bubble_k` bajado a un trap de ICE («yield not supported natively yet»), `direct.Run` ejecuta `kimain` en la goroutine principal con un contexto de fibra mínimo (la forma que los programas sin `Async` conservan para siempre, apartado 9.3), y las primitivas de `IO` terminan de forma síncrona por su trampolín sin poner nunca `yielding`, así que el trap no se dispara. «Secuencial y sin efectos» en el hito M9 y en el capítulo 51 significa **sin efectos de usuario ni `Async`**; `print` sí está permitido. El 53 sustituye el trap por el `bubble_k` real. Un test del 49 fija que el Core S5 de `fib` con su `print` no contiene ningún `isYielding`.

#### Parte I. Fundamentos y primer programa (≈ 81 h → M1)

| # | Capítulo | Hito | Go nuevo | Compiladores | h |
|---|---|---|---|---|---|
| 1 | ✎ El taller: el toolchain de Go, módulos, `go test` y la CI | tu repo `aldezex/kilang` creado; `ki --version` (desde `debug.ReadBuildInfo`); `go test ./...`, `go vet ./...` y `gofmt -l` en verde, también con `-race`, en local y en la CI de GitHub; comprobación de compilación cruzada para windows/amd64 y linux/arm64 | Go desde cero: instalar el toolchain (Go 1.27.3; tu máquina ya lo tiene) y `go version`; `go mod init`, `go.mod` (líneas `go` y `toolchain`, `GOTOOLCHAIN`), paquetes y nombres exportados, `cmd/` + `internal/`; tipos, funciones, retornos múltiples, `for` (incluido `range` sobre un entero), `switch`, comparados con Rust, TS y Python; **«lo justo para testear»**: un literal de slice de structs, `range` sobre él y un literal de función, solo lo que los tests de tabla y los subtests necesitan (los slices, los structs y los closures de verdad llegan en el 2 y el 6); `gofmt`; `go vet`; `testing`: `TestXxx`, subtests, tests de tabla, `t.Helper`, `t.Cleanup`; `flag`; la directiva `ignore`; `setup-go` con `go-version-file` | el mapa del pipeline; TDD rojo-verde; tests golden | 8 |
| 2 | ✎ Valores, punteros, slices y el recolector | SourceMap guarda los ficheros en memoria; la búsqueda de línea y columna está testeada | semántica de valor y copias; punteros sin aritmética; structs; receptores por valor y por puntero; **las entrañas de los slices** (len/cap, aliasing de `append`, `a[i:j:k]`); `string` frente a `[]byte`; `os.ReadFile`; `slices.BinarySearch`; qué hace el GC y de qué no te libra; primer `-gcflags=-m` (frente al borrow checker de Rust) | ficheros fuente, spans, tablas de líneas | 8 |
| 3 | ✎ Tipos suma sin tipos suma: interfaces selladas y errores como valores | `ki check` muestra un diagnóstico con caret (golden): aún no hay lexer, así que el primer `Diagnostic` real lo emite la comprobación previa al léxico, UTF-8 inválido o un byte de control en el fichero, con su golden **[decisión del diseño, constructor §3.6]** | tipos definidos, `iota` + `String()`; interfaces; interfaz sellada + type switch y el `default: panic`; la trampa del nil tipado; `error`, `%w`, `errors.Is/As/Join` (`errors.AsType` espera a los genéricos del 4); verbos de `fmt`, `Stringer`, `Formatter`; `unicode/utf8.Valid`; ficheros golden en `testdata/` con `-update` | modelo de diagnósticos, códigos de error, renderizado | 7 |
| 4 | ✎ Genéricos y la caja de herramientas del compilador | `IdVec[I, T]`, `Arena` e `Interner`, testeados y medidos | parámetros de tipo, restricciones, `~uint32`, `comparable`, `cmp.Ordered`, inferencia, la instanciación explícita (`errors.AsType[*fs.PathError](err)`); métodos genéricos: 1.27 los añade solo sobre tipos concretos (no existían en 1.26); ids tipados como tipos definidos; `unique.Make` frente a un interner a mano; slabs sin punteros y el GC; `hash/maphash`; tests `Example`; `b.Loop` + `-benchmem` | almacenamiento orientado a datos, ids tipados, interning | 6 |
| 5 | El lexer y la regla de línea | `ki emit tokens` | bytes frente a runas, `unicode/utf8`, tablas de clases `[256]uint8`, `strconv.ParseInt` + `*NumError`/`ErrRange`; `go generate` con un generador escrito a mano (sustituye a las X-macros); primer objetivo de fuzzing (spans dentro de límites, nunca un panic) | maximal munch, inserción de separadores, modos del lexer, recuperación | 8 |
| 6 | El parser: descenso recursivo, Pratt y recuperación | `ki emit ast` (S-expresiones) | recursión; por qué ids y no punteros *en un lenguaje con GC* (compacto, sin punteros, tablas laterales, volcados estables); closures (lo básico); `panic("unreachable")`; `strings.Builder`, `io.Writer`; fuzzing del parser; nombres según la guía de estilo de Google | precedencias, búsqueda acotada, nodos Error, sincronización | 11 |
| 7 | ✎ Nombres, ámbitos y errores al nivel de rustc | `unknown name 'fibb'; did you mean 'fib'?`, con el fix-it que lo sustituye; los fix-its se muestran como sugerencias de rustc; todos los códigos E01xx–E03xx al nivel del apartado 7.3 y el catálogo `docs/errors` con sus ejemplos probados | maps a fondo: iteración aleatoria y la regla «ordena por id», tablas suizas (1.24); `maps` y `slices`; `iter.Seq` para recorrer ámbitos; `slices.Sorted(maps.Keys(m))`; programación dinámica sobre slices (distancia de edición); `stdlib/prelude.ki` incrustado con el `embed.go` de la raíz (`//go:embed stdlib`, lo mínimo; el 16 lo profundiza); un test que lee Markdown con `bufio.Scanner` y `strconv.Unquote` para probar los ejemplos del catálogo | tablas de símbolos, DefIds, tablas laterales; el diseño de un diagnóstico (primaria, secundarias, nota, ayuda, fix-it) y su catálogo | 9 |
| 8 | Tipos I: comprobar contra firmas, con errores legibles | errores de tipo con dos etiquetas y su origen | union-find sobre slices; hash-consing con claves struct comparables; inmutabilidad por convención (Go no tiene valores `const` compuestos); campos no exportados; embedding de structs | unificación, comprobación bidireccional, procedencia | 9 |
| 9 | Core: la referencia semántica | `ki emit core`; Core Lint en verde | APIs builder; interfaces pequeñas; recorridos como `iter.Seq` (el `std::generator` de Go); **`defer`** (LIFO, evaluación de los argumentos en el `defer`, `recover` solo dentro de una llamada diferida) y con él la comprobación de invariantes con `panic` + `runtime/debug.Stack()` y `recover` en la frontera del pase (los ICE) | diseño de Core, elaboración, invariantes | 6 |
| 10 | El intérprete CEK | **M1** | uniones etiquetadas a mano; `int64` con desbordamiento definido más comprobaciones explícitas y `math/bits`; desplazamientos; pilas explícitas como slices; un panic de Ki como estado `Halt{101}` de la máquina, nunca un `panic` de Go; códigos de salida (un panic de Go sale con 2, `os.Exit` se salta los `defer`, vaciar antes; `go run` enmascara los códigos); nace `kirt` con `kirt/out` (el escritor con buffer, `bufio.Writer` + `sync.Mutex`) (el `embed.go` existe desde el 7); Delve, a su versión fijada | máquina CEK, llamadas de cola, primitivas, panics | 9 |

#### Parte II. El núcleo funcional en el oráculo (≈ 107 h → M2, M3)

| # | Capítulo | Hito | Go nuevo | Compiladores | h |
|---|---|---|---|---|---|
| 11 | Datos: tipos algebraicos, records y tuplas | valores `Shape`, records con update, `Float` impreso con la regla de `kirt/fmtnum` (`1.0`, `0.1`, `1000000.0`, `1e21`, `NaN`) | layout y relleno de structs con `reflect` (`Type.Size`, `Type.Align`, `StructField.Offset`; `unsafe.Sizeof` llega en el 36, donde `unsafe` está permitido); almacenamiento plano frente a un slice de interfaces (el coste del boxing); `strconv.FormatFloat(f, 'e', -1, 64)` como fuente de los dígitos más cortos y por qué `'g'` no sirve (`1e+06`, `+Inf`); la regla de FMA de la especificación y el `float64()` explícito; `new(expr)` (1.26) | tipos nominales, constructores, records; el formato de `Float` del apartado 5.10 | 11 |
| 12 | match y exhaustividad | **M2** | recursión sobre matrices de patrones; bitsets como `[]uint64` + `math/bits`; los subslices comparten el array (`slices.Clone`); `slices.Collect` | utilidad de Maranget, testigos, guardas; matching ingenuo en el oráculo | 14 |
| 13 | Polimorfismo: genéricos, generalización y niveles | funciones genéricas, funciones privadas inferidas, error legible de «local no generalizado» | Tarjan iterativo; **cómo implementa Go los genéricos** (stenciling por forma de GC más diccionarios) junto a los diccionarios de Ki y frente a la monomorfización; la trampa de captura en closures y su arreglo de 1.22 | variables de tipo, niveles, generalización por SCC | 14 |
| 14 | Traits I: coherencia y paso de diccionarios | `impl Describe for Shape`; operadores por traits; `print<T: Show>` | interfaces a fondo: satisfacción implícita, method sets, itabs (`runtime/iface.go`), embedding; «métodos solo en el paquete del tipo» como la regla de huérfanos de Go; interfaces de restricción frente a interfaces de runtime frente a traits de Rust | resolución de instancias, superclases, huérfanos, diccionarios acíclicos | 16 |
| 15 | Closures, lambdas, pipes y métodos | `xs \|> map((x) => x * 2)` y `s.describe()` | valores `func`; closures y capturas que escapan; method values y method expressions; tipos `func` con métodos (`http.HandlerFunc`); el registro de contexto de closure (`DX`) como anticipo de la Parte VIII | variables libres, closures, azúcar de `\|>`, búsqueda de métodos por tipo | 10 |
| 16 | Listas, strings, interpolación y `UInt64` | **M3**; además, `UInt64` | strings como bytes inmutables; `range` sobre un string; `strings` y `bytes`; `uint64` y conversiones explícitas; `//go:embed` y `embed.FS` a fondo (`fs.Sub`, `fs.ReadFile`, por qué el `embed.go` que nació en el 10 vive en la raíz y por qué un patrón que no casa con nada no compila) | azúcar de listas, preludio en Ki, interpolación | 9 |
| 17 | Traits II: tipos de orden superior, derive y el preludio | Functor, Foldable y Monad; `sum` sobre cualquier Foldable; `#[derive]` | **lo que los genéricos de Go no pueden expresar** (sin HKT, sin parámetros de tipo variádicos, sin especialización) y cómo Ki lo compila igual; restricciones autorreferentes (1.26); alias de tipo genéricos (1.24); los «core types» quitados de la especificación (1.25); nota: 1.27 añade métodos genéricos sobre tipos concretos | kinds, constructores currificados, derive | 14 |
| 18 | Módulos e imports | proyectos de varios ficheros; preludio en Ki, con cada tipo en su módulo; métodos entre módulos; ciclos de import con mensaje | paquetes a escala; `internal/`; los ciclos de import son errores de compilación (como en Ki); orden de inicialización de paquetes; `io/fs`, `os.DirFS`; **`os.Root`** para confinar el proyecto al directorio del fichero de entrada; `path/filepath`; `errors.Is(err, fs.ErrNotExist)`; `go list -deps` | grafo de módulos, visibilidad, orden de inicialización | 7 |
| 19 | Errores que se leen: `ki explain`, color y el catálogo completo | un snapshot por cada código de error; `ki explain E0401` imprime su entrada del catálogo, que existe desde el 7 | `docs/errors` entra en el patrón del `embed.go` de la raíz; `errors.AsType` en la frontera de E/S del driver; `go generate` para el índice de códigos; detección de terminal; colores ANSI; una tabla de anchura Unicode generada (sin `x/text`); `encoding/json` para las líneas JSON (`json/v2` sigue experimental); `runtime/debug.Stack` para los ICE | procedencia en cadena, diseño de mensajes | 7 |

**Proyectos en Ki (≈ 5 h, en la cabecera de la parte).** Con tipos algebraicos, `match`, listas, strings e interpolación, módulos y errores que se leen: una **calculadora de expresiones** (lexer, parser Pratt y evaluador escritos en Ki, con sus propios mensajes de error), el **juego de la vida** (una rejilla inmutable que se imprime generación a generación) y un **conversor de Markdown a HTML** (párrafos, encabezados, énfasis y listas; su salida se compara con ficheros golden). Cada programa es un golden `run` de `tests/projects/p02/` y corre en el oráculo.

#### Parte III. Efectos, control y pereza (≈ 61 h → M4)

| # | Capítulo | Hito | Go nuevo | Compiladores | h |
|---|---|---|---|---|---|
| 20 | Filas de efectos | `uses` comprobado; «performs Log but does not declare it», con las dos soluciones; la fila de `main` inferida y acotada a `{IO, Async}`; las operaciones de `IO` que aparcan nacen tipadas `uses {IO, Async}` y las que no, `uses {IO}` (apartado 5.6) | slices ordenados pequeños (`slices.BinarySearchFunc`, `Insert`, `Compact`); un `SmallVec` en Go (array en línea más slice) y lo que dice `-m` de él | etiquetas con ámbito, unificación de filas, regla ambiente | 14 |
| 21 | Handlers y continuaciones one-shot | `handle`, `perform` y `resume` en el oráculo; Fail, State y Log | sin tipos solo-movibles: un flag `used` más `noCopy` vigilado por vet (copylocks); **el aliasing de los slices como lección**: capturar es `slices.Clone` + `clear` + truncar, reanudar es `append`, y sin el clon el siguiente `append` pisaría los marcos capturados [R2 §2.1]; por qué un panic de Ki es un estado `Halt{101}` de la CEK y no un `panic` de Go, que queda para los ICE (apartado 7.2); máquinas de estados con `iota` | reglas small-step, handlers profundos, soltar continuaciones | 16 |
| 22 | Generadores, estado y `?` | iteradores con Yield, `runState`, `catch`/`toResult` y `e?` | iteradores push frente a `iter.Pull`: el iterador pull de Go es una **corrutina one-shot con pila** del runtime (`runtime/coro.go`), es decir, exactamente lo que es Yield; `defer stop()` | continuaciones guardadas y reanudadas fuera de su `handle` (no acotadas), `resume` que no está en cola | 10 |
| 23 | Azúcar imperativo sobre un núcleo puro | `var`, `for`, `while`, `break`, `continue` y `return` | `break` y `continue` con etiqueta; `goto` y sus reglas como la cara de Go de los join points; `slices.All` y `Backward` | join points, `joinrec`, intuición de SSA | 11 |
| 24 | Pereza explícita | **M4** | `sync.Once` y `OnceValue` como los thunks memorizados de Go, y por qué los de Ki no toman cerrojo | call-by-need como biblioteca, comprobación de pureza | 5 |

**Proyectos en Ki (≈ 5 h, en la cabecera de la parte).** Con efectos, handlers, generadores, `?` y streams perezosos: un **parser de JSON con buenos errores** (`Fail<ParseError>` con posición y «expected `,` or `}`», `toResult` en la frontera, y sus propios tests de errores golden) y un **generador de sitios estáticos** (lee un directorio de Markdown con el conversor de la Parte II, lo recorre como `Stream`, aplica una plantilla y escribe el HTML con `std/fs`). Goldens `run` en `tests/projects/p03/`.

#### Parte IV. Concurrencia en el oráculo (≈ 28 h → M5)

| # | Capítulo | Hito | Go nuevo | Compiladores | h |
|---|---|---|---|---|---|
| 25 | Fibras y el planificador determinista | primero, un planificador de juguete escrito como handler de Ki (`Coop { fork, yield }`); después, `spawn` con `--sched=det --seed` y deadlocks detectados; `await` como palabra reservada, con E0206, E0406 y E0407 nacidos al nivel del apartado 7.3 (`await sleep(d)`, `await yieldNow()`; `spawn` sin `await`) | **goroutines**, el modelo que Ki copia; `sync.WaitGroup` + `WaitGroup.Go` (1.25); vet `waitgroup`; un buffer circular genérico; listas intrusivas (frente a `container/list`); splitmix64 a mano (`math/rand/v2` existe, pero la regla de la fuente única compartida se mantiene); por qué el oráculo no usa ni una goroutine | fibras como estados, puntos de planificación | 13 |
| 26 | Canales, select y tiempo virtual | **M5**; `await send`, `await recv`, `select` como marcador propio; `channel`, `close` y `after` sin `await` | **canales y `select` en Go**: con y sin buffer, `close`, recibir de un canal cerrado, enviar a uno cerrado hace panic, canales nil, `default`, `time.After`; los canales de temporizador de Go **no tienen buffer** desde 1.23 (y en 1.27 son siempre síncronos) frente al buffer 1 del `after` de Ki, y por qué cada uno eligió lo suyo; **`testing/synctest`** como la burbuja de tiempo virtual de Go, comparada con el modo determinista de Ki (y por qué no sirve para él: 6 órdenes distintos en 300 burbujas); `slices.SortStableFunc`, `container/heap` | rendezvous, cierre, `select`, reloj virtual, trazas | 15 |

#### Parte V. Un middle end verificado (≈ 92 h → M6)

| # | Capítulo | Hito | Go nuevo | Compiladores | h |
|---|---|---|---|---|---|
| 27 | ANF y comprobar cada pase | `ki test --check-passes` | reescritura de IR con worklists; algoritmos de `slices`; iteradores sobre la IR | ANF, Lint por pase, diferencial entre pases | 8 |
| 28 | Árboles de decisión | los matches compilados coinciden con el matcher ingenuo | memoización con claves struct o `maphash.Comparable` (1.24) | árboles de decisión de Maranget, compartición | 10 |
| 29 | Lambda lifting y conversión de closures | ningún closure se contiene a sí mismo | conjuntos de variables libres como `[]uint64` con métodos iteradores | lifting, entornos explícitos | 8 |
| 30 | Perceus I: propiedad y el heap comprobado | toda la suite, sin fugas, con `--level=rc` | flujo de datos sobre `[]uint64`; tabla de objetos con lápidas; procedencia con `runtime.Caller`; **el GC de trazado de Go como contraste**: marcado tricolor concurrente, barreras de escritura, `GOGC`, Green Tea (defecto en 1.26); qué compra Perceus (reutilización, determinismo) | RC preciso, dup/drop, linealidad | 16 |
| 31 | Perceus II: préstamos, fronteras y reutilización | `map` y `filter` en el sitio; regla de frontera | `weak.Pointer` y `runtime.AddCleanup` (1.24) frente a `shared_ptr` y `weak_ptr`, y por qué Ki no usa ninguno; punto fijo sobre SCCs | inferencia de préstamos, especialización de drop, FBIP | 14 |
| 32 | ✎ S5: paso de evidencia y yield bubbling | `--check-passes` S4↔S5 en el oráculo, con `Async` (la E/S que aparca incluida) burbujeando hasta la raíz de la fibra (la raíz como prompt del planificador del oráculo); ninguna comprobación tras una fila con `IO` a solas (test sobre el Core S5 de un programa con `print`); censo de `interp-rc` sobre S5 | closures y valores `func` a fondo; por qué un closure que escapa asigna (`-gcflags=-m`); los números del prototipo de R2 (65 / 135 / 803 ns a profundidad 1 / 10 / 100) como objetivo a reproducir con la propia implementación (el prototipo se borró, R2 §12) | evidencia generalizada (Xie y Leijen, ICFP 2021), join points compartidos, prompts, reanudaciones no acotadas, la raíz de fibra como prompt, la máscara tail-resumptive (todavía apagada) | 16 |
| 33 | Un generador de programas bien tipados | kigen genera programas que compilan y terminan, abandona continuaciones en cada `perform` y bloquea canales a profundidad variable (1, 10, 100 marcos) | generadores recursivos acotados por tamaño (closures frente a interfaces); `math/rand/v2` con semillas explícitas; el fuzzer a nivel de bytes de Go como complemento, y por qué hace falta igualmente un kigen dirigido por tipos | generación dirigida por tipos; distribuciones: bucles largos sin asignar, continuaciones abandonadas, profundidad de bloqueo | 13 |
| 34 | Encoger fallos y el corpus de regresión | **M6** | `os/exec`, `exec.CommandContext` + `context.WithTimeout`, pipes; `t.Context()` (1.24); `T.ArtifactDir` + `-artifacts` (1.26) para los casos encogidos; `log/slog` con `JSONHandler` para los logs de triaje | delta debugging, triaje | 7 |

#### Parte VI. El runtime y la máquina virtual (≈ 87 h → M7)

| # | Capítulo | Hito | Go nuevo | Compiladores | h |
|---|---|---|---|---|---|
| 35 | ✎ El modelo de memoria de Go y el runtime que te hospeda (antes del primer atómico) | tests de tornasol con `-race` | el modelo de memoria de Go (DRF-SC, happens-before, `sync/atomic` **solo secuencialmente consistente**, las carreras sobre valores multipalabra corrompen memoria); el detector de carreras (necesita cgo) y lo que no ve; **las reglas del runtime anfitrión**: G/M/P, sysmon, preempción cooperativa y asíncrona (nunca dentro de ensamblador), stop-the-world, crecimiento de pila por copia, `LockOSThread`; lectura guiada de `runtime2.go`, `proc.go`, `stack.go` y `preempt.go` | memoria débil, linealizabilidad | 11 |
| 36 | ✎ El modelo de objetos de kirt | tests de la codificación de valores, `BoxedInt` incluido; test de layout contra `kirt/layout` | `unsafe`: los seis patrones válidos de `Pointer`, `unsafe.Add/Slice/String/StringData`, «un `uintptr` no es una referencia», `runtime.KeepAlive`, `-d=checkptr`, vet `unsafeptr`, `//go:nocheckptr`; aserciones de layout; `structs.HostLayout`; **por qué los objetos de Ki viven fuera del heap de Go y nunca contienen punteros de Go** (los laboratorios V1 y V7 de R3); build tags para el heap de depuración | representación uniforme, tagging, `Int` de 64 bits canónico, inmortales | 9 |
| 37 | Tu propio allocator | estrés del allocator; modos `debug`, `fence` y `kiheapgo` | `syscall.Mmap`, `Mprotect` y `Madvise`, `MAP_NORESERVE`, overcommit; free lists intrusivas sobre memoria propia; push de Treiber con CAS `atomic.Uint64`; `runtime.Pinner`; `debug.SetPanicOnFault`; **el GC de Go desde dentro**: el pacer, `GOMEMLIMIT` y lo que no cuenta, `runtime/metrics`, `GODEBUG=gctrace=1`; análisis de escape para mantener el runtime sin asignaciones de Go en los caminos calientes; `b.Loop` y lo que de verdad hay que saber de él (inlinea; mantiene vivos los valores del cuerpo con `KeepAlive`); ficheros por sistema: `heap_windows.go` nace como stub para que la compilación cruzada siga en verde | free lists segregadas, objetos grandes dentro de la reserva, liberación remota, censo | 16 |
| 38 | Conteo de referencias en el runtime | soltar una lista de 10⁷ elementos sin recursión; censo; `share` e inmortalizar como recorridos | atómicos sobre palabras fuera del heap de Go (`(*atomic.Int32)(unsafe.Pointer(p))`, alineación), escritos **dentro de `kirt/heap`** y exportados como `AtomicAdd32`/`CAS64` sobre desplazamientos, para que `rc` no importe `unsafe`; `atomic.AddInt32` leído con `go tool objdump` (`LOCK XADDL`); el coste de los atómicos SC y el argumento escrito; contención y líneas de caché medidas | RC local, compartido e inmortal; `share`; inmortalizar | 10 |
| 39 | MIR: una IR de bajo nivel en SSA | MIR para toda la suite; verificador, incluida la regla «todo bucle lleva un poll» | diseño de APIs de IR en Go (structs concretos frente a interfaces pequeñas, subslices `[]Operand`); comentarios de documentación y `go doc` | SSA con parámetros de bloque, layouts | 14 |
| 40 | Bytecode y el bucle de la VM | `--backend=vm` con programas secuenciales | dispatch con un `switch` denso (tabla de saltos); **eliminación de comprobaciones de límites** (`-d=ssa/check_bce`: la ventana `(*[256]Value)(regs[base:base+256])` indexada por operandos `uint8`, un chequeo por marco y ninguno por instrucción); el presupuesto del inliner (`-m=2`) y la partición rápida/lenta; análisis de escape en el bucle caliente; la tabla de líneas pc → posición Ki para la procedencia; primer `-cpuprofile` + `go tool pprof`; `go generate` para las tablas de opcodes; fuzzing del verificador de bytecode | VM de registros con instrucciones `{u8 op, u8 a, u8 b, u8 c, u32 k}`, convención de llamada, caminos rápido y lento de la aritmética | 15 |
| 41 | ✎ Efectos en la VM: ejecutar S5 | **M7**; `--backend=vm-bubble` entra en la matriz diferencial | pools de objetos (`sync.Pool` frente a free lists propias); handles con `Close` + `defer`; la raíz de la fibra que aparca en el sitio frente a la que burbujea (`vm-bubble`), y por qué la segunda es el espejo del nativo en código Go que `-race` sí ve | evidencia, prompts y continuaciones como closures en la VM; los segmentos de pila como ejercicio | 12 |

#### Parte VII. Concurrencia multinúcleo (≈ 75 h → M8)

Va antes que el nativo **[decisión del diseño]**. La concurrencia es tu interés principal, así llega unas 110 h antes, y sus fallos nunca se mezclan con los de generación de código. Las fibras de la VM aparcan devolviendo `Park`, y todo lo que se construye aquí (`kirt/sched`, `chan`, `poll`, `det`) lo usa después el nativo tal cual.

| # | Capítulo | Hito | Go nuevo | Compiladores | h |
|---|---|---|---|---|---|
| 42 | Fibras de la VM y el planificador determinista del runtime | las trazas de la VM son idénticas a las del intérprete | interfaces frente a genéricos para el planificador (despacho dinámico frente a estático); goroutines worker **sin** `LockOSThread` (la medición de 580× como contraejemplo); sin almacenamiento local por goroutine → `ctx` explícito | interfaz del planificador, `Park`, `Run(fiber)` | 11 |
| 43 | El deque de Chase–Lev | pasa estrés de linealizabilidad y `-race` | Go sin cerrojos con `atomic.Int64` y `atomic.Pointer[T]`; bucles CAS; ABA; índices con signo; relleno de líneas de caché a mano; false sharing medido; lectura de `runqget` y `runqsteal` | work stealing | 14 |
| 44 | El planificador work-stealing | 16 workers que arrancan en el primer `spawn` (test: un programa con `Async` y sin `spawn` tiene una sola goroutine); fib paralelo escala; un programa bloqueado sale con 102 y no con el `fatal error: all goroutines are asleep` de Go | aparcar: tokens por canal frente a `futex` crudo por `syscall.Syscall6` (el experimento vive en `kirt/heap/futex_linux_test.go`, el único sitio con `unsafe`), y lo que cuesta cada uno dentro del planificador de Go (`entersyscall`); lectura de `findRunnable` (la regla 1 de 61, la regla de los spinners) y de `checkdead`; **el último worker que duerme detecta el bloqueo de Ki**; la goroutine monitor y `ctx.preempt` atómico (la retoma del slot espera al 54b, donde se puede ejercitar con Ki real); `go tool trace` sobre tus workers; `GOMAXPROCS` en contenedores | spinning y parking, detección de deadlock, preempción | 14 |
| 45 | Canales y select en el runtime | el modo determinista del runtime coincide con el del intérprete en cada semilla; decidido G15 (aparcar-y-soltar frente a `parking`) midiendo con `vm-bubble` | `sync.Mutex` + `defer Unlock`; orden de bloqueo por id; `done` con CAS; lectura de `hchan`, `selectgo` y el `unlockf` de `gopark`; perfiles de mutex y de bloqueo; el perfil de fugas de goroutines (GA en 1.27) | hchan, aparcar y soltar frente a la marca `parking` (medido con la raíz que burbujea), `select` en dos fases | 16 |
| 46 | E/S y temporizadores con epoll | **M8**; el detector de bloqueo cuenta los intereses armados y los temporizadores (test con el poller dormido en `EpollWait(-1)`); el poller, el montículo y el pool arrancan en su primer uso (test: un programa con canales y temporizadores y sin red no crea la instancia de `epoll`) | `syscall.EpollCreate1`, `EpollCtl` y `EpollWait`; `eventfd` por `SYS_EVENTFD2`; `SetNonblock`; `syscall.Errno` con `errors.Is`; fds como enteros crudos y por qué `os.NewFile` sobre un fd no bloqueante lo entregaría al netpoller de Go; `syscall.Close` en el drop del proxy y por qué `runtime.AddCleanup` no protegería de nada; un montículo 4-ario; un pool de goroutines bloqueantes con `*os.File`; `context` para el apagado; `synctest` para los tests de temporizadores; comparación con el netpoller de Go (`netpoll_epoll.go`) | E/S por disponibilidad, temporizadores | 13 |

**Proyectos en Ki (≈ 7 h, en la cabecera de la parte).** Con fibras, canales, `select`, temporizadores y `std/net` sobre el poller, en la VM con 16 workers: un **chat TCP** (un `select` por conexión entre el socket, el canal de difusión y un temporizador de inactividad), una **caché clave-valor en red** (protocolo de líneas, un `Map` detrás de una fibra dueña que atiende por canal, expiración con `after`) y un **limitador de tasa** (token bucket con una fibra de relleno; se prueba con el modo determinista y una semilla por escenario). Goldens `det` y `stress` en `tests/projects/p07/`; el generador de carga, en Go.

#### Parte VIII. Código nativo x86-64 (≈ 124 h → M9, M10)

| # | Capítulo | Hito | Go nuevo | Compiladores | h |
|---|---|---|---|---|---|
| 47 | ✎ x86-64 y el ensamblador de Go (Plan 9) desde cero | un `.s` escrito a mano llama a kirt a través de un trampolín, construido con `go build`; los cuatro fallos (tres errores fatales en ejecución, `missing stackmap`, `SPWRITE` e `invalid pointer`, y el error del enlazador `nosplit cycle`) reproducidos a propósito; los canarios C1–C6 y C13 reescritos desde sus resúmenes | la ISA desde cero, más la sintaxis Plan 9: orden de operandos, `FP`, `SB`, `SP` y `PC`, `TEXT ·f(SB), NOSPLIT, $frame-args`, `textflag.h`, `funcdata.h` y `NO_LOCAL_POINTERS`; **ABI0 frente a ABIInternal** (el selector se rechaza fuera del runtime), los wrappers `.abi0`, los papeles de los registros (`R14` = g, `X15` = 0, `DX` = closure; sin registros preservados); el **chequeo de pila que inserta el ensamblador**; vet asmdecl; `go tool objdump`; `-gcflags=-S` | ISA, direccionamiento, flags, convenciones de llamada | 14 |
| 48 | ✎ `go build` como enlazador: `ki build` y el módulo generado | `ki build` enlaza un stub, estático (`CGO_ENABLED=0`); `ki doctor` comprueba `go version`, `GOROOT` y que el ejecutable salga `statically linked`; `ki clean`; el módulo generado importa solo los subpaquetes de `kirt` que las filas del programa piden (test con `go tool nm`: sin `Async`, ningún símbolo de `kirt/sched` ni `kirt/poll`) | el módulo generado (`go.mod` con la línea `toolchain` copiada de `go env GOVERSION`, `main.go` con `direct.Run(kimain)` o `sched.Run(kimain, kiResume)` según las filas, `ki_gen.go`, `kiabi.h`, el `.s`); la eliminación de código muerto del enlazador de Go, leída con `go tool nm`; `kirt` entra en el patrón del `embed.go` de la raíz; `replace`; `CGO_ENABLED=0` y por qué `net` lo exige (`go list -f '{{.CgoFiles}}' net`); por qué kirt no puede estar en `internal/`; `go build -x -work`; `go tool asm`, `compile`, `pack` y `link`; la caché de build; `-ldflags` (`-X`, `-s -w`); `-trimpath`; sufijos de fichero y `//go:build`; `go version -m`; las restricciones de `//go:linkname` (1.23) | enlazado, arranque del programa, KiABI-A0 | 7 |
| 49 | Selección de instrucciones | `fib` en nativo con todo en memoria (spill-all), con su `print` y sin ninguna comprobación tras él (`IO` a solas no cede: test sobre el `.s` emitido); las comprobaciones de las funciones con variable de fila se emiten con el trap que el 53 sustituye | tablas de patrones como datos de Go; `bufio.Writer` + `fmt.Fprintf` para emitir texto; el diseño de la LIR; mnemónicos y orden de operandos de Plan 9 en el emisor; `#line`; el id de sitio inmediato que recibe el trampolín de panic para dar `file:line:col` de Ki | tiling de árboles, maximal munch, fusión de flags | 14 |
| 50 | Liveness y linear scan, con su verificador | **M9** | BitSet escrito a mano; `container/heap`; comparadores con `slices.SortFunc`; tests de propiedades con tus propios generadores o fuzz targets | intervalos de vida, Poletto–Sarkar con partición obligatoria en las llamadas, verificador | 16 |
| 51 | ✎ Marcos, llamadas, tail calls y polls | todos los tests secuenciales sin efectos de usuario pasan en nativo (`print` incluido); objdump atribuye líneas de Ki; el test de apertura: la latencia de `runtime.GC()` con y sin poll; `isEven`/`isOdd` y una cola indirecta en espacio constante | el marco ABI0 y el área de salida del programa (la aridad máxima de los destinos de cola); `RET ·f(SB)`, la hoja forzada y la regla «toda función con cola o bucle lleva un `CALL`», con el ciclo `nosplit` del par mutuo como ejercicio; la cola indirecta `ADJSP $-F; POPQ BP; JMP CX` y su compensación inalcanzable (C13); los puntos de entrada nunca hacen cola; `MOVQ (TLS), R14`; `--poll=inline\|call` y el desplazamiento 16; la comprobación `CMPB preempt(ctx)` en las funciones que pueden ceder; `--emit-protos` + vet | marcos, llamadas de cola estáticas e indirectas, polls obligatorios | 16 |
| 52 | Closures, ADTs y RC nativos | el corpus de la Parte II (sin efectos de usuario ni `Async`) corre en nativo sin fugas | leer el código generado con `go tool objdump`; caminos rápidos en línea (aritmética etiquetada, dup/drop, pop del allocator); `NEEDCTXT` y `DX`; aserciones de alineación | caminos rápidos de RC, de asignación y de la aritmética de `Int` | 13 |
| 53 | ✎ Efectos nativos: compilar S5 | el corpus de efectos es idéntico en los tres backends; el trap de los capítulos 49–52 se sustituye por el `bubble_k` real | tomar la dirección de un `TEXT` (`LEAQ ·join_k(SB)`) y la llamada indirecta; el contexto de la fibra como argumento 0 y su recarga tras cada llamada; depurar burbujeos con `GOTRACEBACK` y objdump; el prototipo del portador A de la referencia (C14) como lectura guiada | `yieldExtend`, prompts y `resume` en ensamblador; la comprobación tras cada llamada que puede ceder | 16 |
| 54a | ✎ Fibras nativas I: `kiResume` y el portador A en un worker | el ejemplo ampliado corre en nativo con un worker, y la suite de efectos y canales coincide con `vm-bubble` | `kiResume` y el portador A de punta a punta (la raíz de fibra que devuelve `Park`, el canal, el despertar); el runtime de Go es dueño de las señales: `GOTRACEBACK`, `debug.SetCrashOutput`, `os/signal`; `debug.SetMaxStack` y la regla del desbordamiento (y el stdout que se pierde); Delve y gdb con goroutines | la raíz de fibra en ensamblador, reproducción de continuaciones, el desbordamiento nativo | 14 |
| 54b | ✎ Fibras nativas II: 16 núcleos, la retoma del slot y la prueba a tres bandas | **M10** | la retoma del slot con el contexto de allocator separado (pool, retirada al suspender, estrés con `kiheapgo` y `-race`); migraciones forzadas con 16 workers; `trace.FlightRecorder` para el forense de fallos; la matriz `interp`/`vm`/`vm-bubble`/`native` × semillas; la confirmación de G15 en nativo | riesgos de la migración, matriz a tres bandas, la retoma del slot | 14 |

#### Parte IX. Rendimiento (≈ 52 h → M11)

| # | Capítulo | Hito | Go nuevo | Compiladores | h |
|---|---|---|---|---|---|
| 55 | El optimizador de Core | inlining, especialización, case-of-known-constructor, el camino rápido tail-resumptive con máscara y la especialización de efectos por sitio de llamada (apartado 6.1: `map`, `sum` y `\|>` sin `isYielding` en un programa sin `Async`, con su test sobre el Core S5 emitido), con la suite todavía idéntica | worklists; memoización; perfilar el propio compilador con pprof (CPU y asignaciones) | simplificador, modelos de coste | 16 |
| 56 | Representaciones sin caja | nbody varias veces más rápido (medido); `Int`, `UInt64` y `Float` crudos en el código monomórfico | `math.Float64bits` y `Float64frombits`; NaN-boxing como comparación; conciencia de SIMD y el experimental `simd/archsimd` (1.26, solo amd64; no se usa) | elección de representación, worker/wrapper | 12 |
| 57 | Linear scan II: partir intervalos | menos volcados en la suite de benchmarks | estructuras de intervalos ordenadas sin `std::set`: slices ordenados o un árbol balanceado a mano | huecos de vida, resolución de movimientos (Wimmer–Franz) | 14 |
| 58 | ✎ Medir de verdad: pprof, dispatch, PGO y los experimentos del ABI | **M11** | sin goto computado ni `musttail`: medir el `switch` con tabla de saltos frente a una tabla de funciones y al dispatch con closures; `pprof` (flame graph por defecto en 1.26); `go tool trace`; perf opcional; **PGO** con `default.pgo`; `GOGC` y `GOMEMLIMIT` sobre el proceso `ki`; estadística de benchmarks; **experimentos**: `--kiabi=b` (KiABI-B′), `--stack-check=count` (la estimación de ~0,7 ns por llamada, medida por fin), la comprobación `CMPB preempt(ctx)` frente a `--kipreempt=polls`, y la sección opcional «¿y si las fibras fueran goroutines?» (el portador B como adaptador) | metodología de benchmarks, dispatch enhebrado | 10 |

#### Parte X. Programas reales, el servidor de lenguaje y Ki v0.1 (≈ 81 h → M12)

| # | Capítulo | Hito | Go nuevo | Compiladores | h |
|---|---|---|---|---|---|
| 59 | La biblioteca estándar en Ki | Map, Set, Array y String con FBIP | primitivas del runtime para strings y arrays sobre `bytes.IndexByte` y `bytes.Compare` (que en la propia biblioteca de Go están en ensamblador); utilidades UTF-8 | diseño de bibliotecas FBIP | 14 |
| 60 | Paralelismo como biblioteca | curvas de escalado de `parMap`, `parReduce` y un sort paralelo en 16 núcleos | perfilar la contención (mutex y block); false sharing en la práctica; patrones estilo errgroup a mano (`x/sync` no está permitido); `GOMAXPROCS` en contenedores | control de granularidad, fork-join sobre fibras | 10 |
| 61 | Programas de verdad: una CLI paralela y un servidor HTTP | un clon de grep paralelo; un servidor HTTP/1.1 reducido bajo carga | el generador de carga y un servidor de referencia escritos en Go: `net/http`, plazos con `context`, apagado ordenado con `signal.NotifyContext`, `slog` + `NewMultiHandler`, `net/http/pprof`, backpressure | programas de punta a punta | 12 |
| 62 | ✎ El servidor de lenguaje I: el protocolo y los diagnósticos en vivo | `ki lsp` habla LSP por stdio: `initialize` con sus capacidades, sincronización de documentos y los diagnósticos del front end publicados en cada edición, con `related information` para las secundarias, `data` con el código y el fix-it como `codeAction` de tipo `quickfix`; transcripciones JSON golden con un cliente en el mismo proceso | JSON-RPC 2.0 a mano sobre `bufio` con el framing `Content-Length` (sin `x/tools`); `encoding/json` con `json.RawMessage` y campos opcionales por puntero para un protocolo con uniones; `positionEncoding` `utf-8` frente a `utf-16` y la conversión de columnas; un servidor sobre stdin/stdout que registra en un fichero con `slog`; `io.Pipe` para el cliente de prueba | el front end como servicio: reanalizar un módulo entero por edición, diagnósticos como datos | 9 |
| 63 | ✎ El servidor de lenguaje II: hover, definición, referencias, autocompletado, símbolos y tokens semánticos | en Neovim, sobre `shapes.ki`: el tipo de cada nombre al pasar por encima (con su fila de efectos), ir a la definición y a las referencias desde las tablas del resolutor, autocompletado desde los ámbitos visibles más el «¿quisiste decir…?», los símbolos del documento y el resaltado por tokens semánticos | mapas inversos span → nodo (`slices.BinarySearchFunc` sobre spans ordenados); `sort` estable de resultados; `strings.Builder` para el Markdown del hover; tablas de tipos de tokens como `iota` + `String()` | consultas sobre las tablas laterales del compilador: posición → nodo → definición → usos | 11 |
| 64 | ✎ El servidor de lenguaje III: capacidad de respuesta, Neovim y Zed | el servidor nunca bloquea al editor: el reanálisis se agrupa con un *debounce* y se cancela con `context` cuando llega otra edición; presupuesto medido por fichero; transcripciones golden con las cancelaciones; la configuración de Neovim con su cliente integrado (`vim.lsp.start` en Lua, con los tokens semánticos del 63 como resaltado) y, después, una extensión de Zed (`extension.toml` más la configuración del lenguaje; el capítulo comprueba si Zed exige una gramática de Tree-sitter para la extensión y, si la exige, una gramática mínima va como ejercicio y apéndice) | goroutines y canales en un servidor real: una goroutine lectora, una analizadora, `context.WithCancel` por petición, `time.Timer` para el debounce, `sync.WaitGroup`; medir la latencia con `testing.B` y `go tool trace`; el perfil de fugas de goroutines (1.27) sobre el servidor; leer `gopls` como referencia de arquitectura | cancelación cooperativa en los pases, presupuestos de latencia; el editor como cliente | 10 |
| 65 | Herramientas y release | **M12**: `ki fmt`, `ki explain`, `ki lsp`, `cmd/kicheck`, etiqueta v0.1 | `go install`; archivos de release compilados en cruzado; `-ldflags -X`, `debug.ReadBuildInfo`, el sello de VCS; `go version -m`; semver y etiquetas de módulo; comentarios de documentación, `go doc -http`; `go fix`; `cmd/kicheck` sobre `go/parser` + `go/types` (regla de floats, política de `unsafe` e imports); revisión de código con las guías de Google y Uber; pretty printing de Wadler en Go | impresión bonita, documentación | 10 |

**Proyectos en Ki (≈ 5 h, en la cabecera de la parte).** Con `std/par`, `ki build` y 16 núcleos: un **Mandelbrot paralelo a PPM** (o un raytracer de esferas, a elegir), con `parMap` por filas de la imagen, la aceleración medida de 1 a 16 workers (una tabla y una curva en el libro, generadas por un script de Go) y la imagen comparada byte a byte entre el oráculo, la VM y el nativo. Golden `diff` y `bench` en `tests/projects/p10/`.

#### Parte XI. Otras plataformas (≈ 68 h → M13, M14)

Alcance **[decisión del diseño]**:
- `ki` compila en cruzado desde Linux: `ki build --target=linux/arm64` y `--target=windows/amd64` son `GOOS`/`GOARCH` sobre el módulo generado, sin toolchains cruzados. Los ejecutables corren en el destino y su salida se compara con la del oráculo en Linux: ARM64 con qemu-user para la corrección y el runner ARM64 de GitHub para el orden de memoria; Windows con el propio Windows de tu PC a través de WSL y un runner de Windows en la CI.
- **Novedad frente a ayer:** como `ki` mismo (compilador, oráculo y VM) es un programa de Go, también se cruza con `GOOS`/`GOARCH` casi gratis; lo que falta por sistema es la reserva del heap (`VirtualAlloc`) y el poller. Así que los hitos M13 y M14 incluyen `ki` corriendo en ARM64 y en Windows, lo que ayer estaba en el epílogo. La comprobación de compilación cruzada de la CI desde el capítulo 1 es lo que lo hace barato [R4 §5.8].

| # | Capítulo | Hito | Go nuevo | Compiladores | h |
|---|---|---|---|---|---|
| 66 | AArch64 I: A64, el ensamblador de Go para arm64 y selección de instrucciones | `fib` en ARM64 bajo qemu-user; la pata arm64 de C10 pasa de estática a ejecutada | `GOARCH=arm64` (sin toolchain cruzado); Plan 9 para arm64 (`MOVD`, `RSP` alineado a 16, `R28` = g, `R26` = closure, `R27` scratch del ensamblador, los argumentos ABI0 empiezan en `8(RSP)` [R1 §9.2]); `_arm64.s`; qemu-user y binfmt desde apt (el `sudo` lo pones tú); lo que `syscall` sí tiene en linux/arm64 (`EpollCreate1`, y también `EpollCreate`, al contrario de lo que dice R2 §10.8 [T V-t4]; `Mmap`, `SYS_FUTEX`) y lo que no (`SYS_EVENTFD`, `MADV_FREE`) | selección de instrucciones A64, reutilizar el asignador | 12 |
| 67 | AArch64 II: el runtime en memoria débil | la aritmética etiquetada, el dup/drop y los polls en ARM64 | los atómicos de Go siguen siendo SC en arm64, así que el Go de kirt porta sin cambios; lo débil es el código **emitido** por Ki (caminos rápidos de RC, aristas de migración; `LDAR`/`STLR`, LSE); tests de tornasol en el runner ARM real (QEMU no muestra los reordenamientos); **arm64 fusiona `a*b+c`** y la regla de floats [R4 V11] | runtime en memoria débil | 13 |
| 68 | AArch64 III: la suite completa en ARM | **M13**: la suite completa en ARM, y `ki` mismo ejecutándose en ARM64 | CI en `ubuntu-24.04-arm`; build constraints para las capas de portabilidad | portabilidad del backend | 10 |
| 69 | ✎ Windows x64 I: el mismo ABI0, otro sistema | un `.exe` de Ki mínimo, compilado desde WSL | `GOOS=windows`; **el mismo `.s` de Plan 9 enlaza y corre** [R1 §9.1, R4 V7]; el ABI Win64 (shadow space, `xmm6`–`15` preservados) solo existe dentro de la capa de syscalls de Go, se lee pero no se implementa; PE/COFF desde el enlazador de Go; `MOVQ (TLS)` como secuencia de tres instrucciones por `GS` | segundo ABI (solo en la frontera del sistema), PE/COFF | 7 |
| 70 | Windows x64 II: el runtime en Windows | kirt en Windows: heap, planificador y fibras | `syscall.NewLazyDLL("kernel32.dll")` para `VirtualAlloc` y `VirtualProtect` (no están en `syscall`) [R4 V10]; ficheros `_windows.go`; `stackSystem` = 4096 y el margen de pila; `SetPanicOnFault` en Windows | runtime portable | 9 |
| 71 | Windows x64 III: IOCP y la suite en Windows | **M14**: IOCP y la suite en Windows, y `ki` mismo ejecutándose en Windows | `syscall.CreateIoCompletionPort`, `GetQueuedCompletionStatus`, `PostQueuedCompletionStatus`, `Overlapped`, `WSARecv`; el poller de goroutines como línea base; comparación con el poller de Windows del propio Go | E/S por completado frente a por disponibilidad | 10 |

**Proyectos en Ki (≈ 7 h, en la cabecera de la parte): el cierre.** Un **mini-Lisp escrito en Ki**: lector, entorno, `eval` con formas especiales, closures, una REPL sobre `readLine` y un pequeño conjunto de programas Lisp de prueba. Es el programa más grande del libro, usa casi todo el lenguaje (ADTs recursivos, `match`, `Map`, efectos para los errores y el estado, `lazy` para los streams de tokens) y corre, con la misma salida, en el oráculo, en la VM y en nativo, en Linux x86-64, en ARM64 bajo qemu y en Windows. Goldens `diff` en `tests/projects/p11/`.

**Epílogo** (fuera del tronco):
- **Del runtime y la memoria:** tail recursion modulo cons; continuaciones multi-shot (copiar la cadena de closures, que bajo S5 es posible); handlers de usuario para `IO` y `Async` (mocks y concurrencia estructurada); un recolector de ciclos restringido a canales; io_uring; **las fibras como goroutines** (el portador B completo, con su modo determinista por paso de testigo [R2 P7]).
- **Del backend:** optimizaciones SSA sobre MIR (GVN, LICM); un asignador por coloreado de grafos; KiABI-B′ como defecto si los canarios aguantan; backend WebAssembly.
- **De las herramientas:** compilación incremental basada en consultas (el servidor de lenguaje de los capítulos 62–64 reanaliza el módulo entero; esta sería su segunda versión); renombrado y más acciones de código en el LSP; paquetes, compilación separada e interfaces; FFI con cgo.
- **Otros:** planificación heartbeat; autohospedar el front end en Ki; y, para tu objetivo de contribuir a open source, **contribuir a Go**: Gerrit, `go.dev/doc/contribute`, `cmd/compile` y el runtime [R4 §8].

---

## 13. Riesgos

Ordenados por gravedad y probabilidad. Los primeros cuatro son nuevos y vienen de Go; del 25 al 31 son los de siempre; los tres últimos (32–34) los añadió la comprobación técnica, y otros tres de los suyos entraron en filas que ya existían (el `spfix` de las colas indirectas en el 1, la deriva de parches en el 19 y el prototipo temprano del portador A en el 4). Están todos los riesgos abiertos de R1–R4; el 24 solo aplica a caminos que el diseño no toma por defecto, y el 23 ya no es «solo bajo B».

| # | Riesgo | Fuente | Mitigación |
|---|---|---|---|
| 1 | **Dependencia de detalles internos del runtime y del ensamblador de Go**: el desplazamiento 16 de `g.stackguard0`, la regla de la hoja auto-`NOSPLIT`, el tratamiento de `SPWRITE`, la generación de wrappers, la tolerancia a funciones ABI0 sin prototipo, la conversión entero→puntero sobre memoria que no es de Go (fuera de los seis patrones de `unsafe`, tolerada a propósito por `findObject`), y el `spfix` del bloque `morestack` del que depende la cola indirecta (T E2). Una versión de Go puede cambiarlos | R1 §12.2, §12.5; R2 §11 R1; R3 §10 R1–R2; T E2 | Toolchain fijado; los catorce canarios del apartado 11.4 en cada push y como puerta de cada actualización (C13 para la cola indirecta); los caminos documentados como respaldo (`--poll=call`, `--emit-protos`, el heap `kiheapgo`) |
| 2 | **Un poll que falta en una arista de retroceso** no es lento: detiene cada stop-the-world del GC para todo el proceso y, con un P, lo mata de hambre (0,8–2 s de latencia medidos) | R1 §4.2, §12.3; R2 §1.1; R3 §10 R3 | Invariante del verificador de MIR («todo bucle sin llamada lleva un poll»); corpus de bucles largos en kigen; el canario C3 de latencia de `runtime.GC()` |
| 3 | **Ningún sanitizer de Go ve el heap de Ki.** `-race` lo ignora y da falsos positivos si estado de Go se publica por atómicos del heap de Ki; no hay ASan; `checkptr` calla; `vet` protesta | R3 §2.3–2.5, §10 R4–R5 | El heap de depuración `kiheapgo` para `-race`; `KI_ALLOC=debug|fence`; el censo; `interp-rc`; nunca publicar estado de Go por atómicos del heap de Ki |
| 4 | **El portador A no está verificado de punta a punta en ensamblador** (sí lo están sus piezas: S5 en Go, el planificador, `RET ·f`, `NEEDCTXT`/`DX`); la retoma del slot tampoco está prototipada. Y llega tarde para un libro que se publica congelado: el capítulo 32 (S5 con `Async` que cede), el 41 y los 42–46 (`kirt` compartido por la VM y el nativo) están construidos para A, y si A fallara en el 54 la corrección serían fe de erratas por las Partes V–VII, justo lo que el anti-objetivo «arquitectura que haya que tirar» prohíbe | R2 §12 («not verified»); T §3.2 | **El prototipo mínimo de A en ensamblador se hace antes de publicar el capítulo 32**, como tarea de la referencia (`docs/research/proto/carrier-a/`): `kiResume`, burbujeo hasta la raíz, aparcar y despertar, crecimiento de pila y GC durante el burbujeo, un canal; entra como canario C14 y como lectura del 53. Además, el portador B como adaptador detrás de la misma interfaz del parker, y el cuarto punto del orden de recorte (M10 con un worker nativo) |
| 5 | **El código puro no puede ser preemptado a nivel de Ki** bajo el portador A: un cálculo puro largo se queda su worker | R2 §11 R3 | Work stealing mueve el resto; la retoma del slot del monitor (capítulo 54b); la vida del proceso la garantizan los prólogos y los polls de Go; las funciones que sí pueden ceder llevan la comprobación `CMPB preempt(ctx)` (apartado 8.5) |
| 6 | **Coste O(profundidad) de la suspensión** para pilas profundas que se bloquean a menudo (803 ns a profundidad 100, más una asignación por marco) | R2 §11 R4 | Medir con los programas reales de la Parte IX; el portador B como adaptador si importa; las «short-cut resumptions» de Koka dieron ~10 % |
| 7 | **El desbordamiento de pila nativo es un error fatal de Go** (salida 2), no el 103 de Ki, y se lleva el stdout sin vaciar | R1 §12.4; R2 §11 R6; T §2.21 | La regla del apartado 8.10 (traducción en `ki run`, inconcluso en diferencial, `--stack-check=count` opcional); es decisión tuya (apartado 4) |
| 8 | **Floats: no existe `-ffp-contract=off`.** La identidad byte a byte entre backends y plataformas descansa en una regla de código; arm64 fusiona por defecto | R4 V11, §11.3 | Conversiones `float64()` explícitas; `cmd/kicheck`; el canario C10 con builds `v3` y `arm64`; el nativo nunca emite FMA |
| 9 | **Un puntero de Go dentro de memoria de Ki es un use-after-free silencioso**; un inmediato en una variable de tipo puntero es un error fatal intermitente | R3 §10 R6–R7 | `Value uint64` en todo el Go; tablas de handles; el test con `reflect` sobre los structs superpuestos; `unsafe` solo en `kirt/heap`; vet |
| 10 | **Crecimiento de código por los `join_k`** en cada sitio de llamada que puede ceder | R2 §11 R5 | Selectivo por filas (el código puro no se toca); join points compartidos; especialización de efectos en el optimizador |
| 11 | **Argumentos en memoria (A0)**: cada llamada Ki paga tráfico de memoria (~1,3× el ABIInternal de Go) | R1 §10; R2 §11 R2 | Aceptarlo en v0 (Go vivió con ABI0 hasta 1.17); B′ como experimento medido; inlining propio de Ki |
| 12 | **Despertares perdidos y carreras en `select`**; `-race` solo ve las estructuras del lado Go | R2 §11 R9 | El protocolo de Go al pie de la letra; tokens por worker; barridos de semillas; `-race` con `kiheapgo`; estrés con conteo; el modelo independiente del oráculo |
| 13 | **Los ayudantes calientes dejan de inlinearse en silencio** al crecer (coste 103 > 80) | R3 §10 R8 | El canario C11 sobre `-gcflags=-m`; la partición rápida/lenta como estilo de casa |
| 14 | **`syscall` está congelado y Windows no tiene epoll**; `eventfd` y `futex` solo como syscalls crudas | R2 §11 R10; R4 V10 | El poller de goroutines como camino portable; IOCP en el capítulo 71 |
| 15 | **arm64 no está verificado en ejecución** (sin qemu en esta máquina); todo lo de arm64 es documentado o solo compilado | R1 §12.8 | Primera tarea del capítulo 66; el runner ARM real para el orden de memoria |
| 16 | **El tiempo de construcción crece con el programa** (~1 s por 10⁴ funciones; `asm` usa ~16 KB de RSS por función) y la política de `linkname` puede endurecerse | R1 §12.6–12.7 | Un `.s` sin prototipos; partir en paquetes los programas enormes; Ki solo referencia `kirt`, nunca el runtime |
| 17 | **Depuración limitada del nativo:** `#line` con el desfase de uno; sin DWARF para los locales de ensamblador; Delve no entiende los valores de Ki | R1 §12.9 | Test que fija la convención de `#line`; objdump como herramienta principal; el oráculo y la VM como depuradores del mismo programa |
| 18 | **El salto de «Go desde cero» a las entrañas del runtime en la Parte VIII** es empinado para quien aprendió Go en este mismo libro | R2 §11 R12 | Los cuatro fallos (tres errores fatales y el del enlazador) reproducidos como ejercicios guiados; `runtime/HACKING.md` como lectura; capítulos guiados por objdump; el capítulo 35 como puente |
| 19 | **Los documentos y el toolchain pueden discrepar, y Go publica dos versiones al año durante un libro de 2–3 años**; los experimentos nombrados cambiarán; y la línea de parches se mueve sola: la 1.26.5 de la investigación ya iba cinco parches de seguridad por detrás de su línea | R4 V15, §11.5–11.6; T E5 | Cada hecho de versión fijado a un test; `go.mod` con `toolchain`; parches en cuanto salen, con los canarios en verde; actualización mayor solo al final de cada parte; empezar en la última de su línea (1.27.3, tu decisión del apartado 4) |
| 20 | **Los benchmarks son micro y sintéticos**, y los números con 16 Ps esconden el trabajo del GC en núcleos ociosos | R3 §10 R9; R1 §12.11 | Remedir con programas Ki reales en la Parte IX; informar `GOMAXPROCS=1` y con todos los workers ocupados |
| 21 | **Overcommit en modo 2 o contenedores con límites bajos**: una reserva de 64 GiB, y memoria de Ki invisible a `GOMEMLIMIT` y a la contabilidad de cgroups | R3 §10 R10 | Reserva `PROT_NONE`; `KI_HEAP_GB` configurable; `KI_HEAP_MAX` y métricas propias |
| 22 | **Las prácticas de proyectos concretos** (Kubernetes, etcd) de los recuadros «Go en el trabajo» salieron de memoria | R4 §11.9 | Comprobarlas antes de escribir cada recuadro |
| 23 | **El `fatal error: all goroutines are asleep` de Go (salida 2) puede adelantarse al informe de deadlock de Ki (102) también bajo el portador A**, porque los workers son goroutines y duermen en su token (verificado: cuatro workers aparcados mueren con salida 2; con el poller en `EpollWait(-1)`, en cambio, el proceso se cuelga para siempre [T V-t7]). Solo bajo B, además: una `Cont` soltada sin `resume` deja su goroutine aparcada para siempre con todos los objetos de Ki que sus marcos poseen, porque el runtime de Go nunca desenrolla una goroutine aparcada; las corrutinas de `iter.Pull` comprueban el estado de `LockOSThread` en cada cambio | R2 §11 R11; R1 §11.12, §12.10; T E6 | El último worker que va a dormir ejecuta la detección de bloqueo de Ki antes de bloquearse (apartado 8.5), con un test por configuración: sin poller, con poller, con monitor; soltar una `Cont` bajo B significa reanudarla con un token de desenrollado; nunca fijar hilos; censo y recuento de goroutines a cero al salir |
| 24 | **Portabilidad del heap y de los atómicos:** Windows no tiene `syscall.Mmap` ni `VirtualAlloc` en `syscall` (solo por `NewLazyDLL`), y `SetPanicOnFault` se comporta distinto; en arm64 los atómicos LSE dependen de la CPU y no hay direccionamiento base+índice+desplazamiento | R3 §10 R11 | Direcciones absolutas (sin registro base); la capa de sistema en un fichero por sistema (`_linux.go`, `_windows.go`); los tests de tornasol en hardware ARM real |
| 25 | Alcance y aguante durante más de dos años | base | 14 hitos, ninguno a más de ~100 h del anterior; sesiones de 2–3 h que acaban en verde; horas registradas; replanificación por partes; orden de recorte publicado |
| 26 | Deriva semántica entre backends | base | Una sola semántica escrita, el oráculo, `--check-passes` S0–S5, kigen a tres bandas |
| 27 | Que el sistema de tipos crezca hacia un zoo | base | Las reglas de v0 se congelan en `docs/v0.md`; una característica nueva necesita justificación escrita, sus mensajes y un argumento de decidibilidad antes del código |
| 28 | Que el libro y el repo se separen, ahora con capítulos ya publicados | base, 11.2 | Listados extraídos solo de etiquetas verificadas; `bookx verify`; CI nocturna sobre todas las etiquetas; historia congelada de lo publicado, correcciones hacia delante |
| 29 | Estimaciones de horas equivocadas; los capítulos 53 y 54 llevan el trabajo menos verificado del libro | base; R4 §11.7; T §4 | Etiquetadas como estimación; reserva del 15 %; horas reales en el progreso del libro; el 54 estimado sin el tope y partido en dos mitades; la Parte VIII con ±20 h, que se estrecha tras el prototipo del riesgo 4 |
| 30 | Empleabilidad: saber Ki no es saber Go vendible | base; R4 §10 | El recuadro «Go en el trabajo» en cada capítulo; el repo como portfolio; las Partes VI–VIII cubren lo que se pregunta en entrevistas de infraestructura y rendimiento |
| 31 | Que Ki acabe «usando» el runtime de Go en vez de construir el suyo | dossier (`cand-go.risks`) | El portador A, el heap propio y el oráculo sin goroutines son la respuesta; el anti-objetivo del apartado 2 lo vigila |
| 32 | **`-race` nunca ve el código nativo.** El detector no instrumenta ensamblador en ningún modo, tampoco con `kiheapgo`: las carreras propias del código emitido por Ki (un `INCL` plano de `dup`/`drop` sobre un objeto mal clasificado como local, una arista de migración mal sincronizada) no tienen detector | R3 §2.5; T §2.25, §3.1 | Tests de tornasol y de estrés sobre binarios nativos con 16 workers forzando migraciones; el modelo independiente del oráculo; la matriz a tres bandas con `vm-bubble`, que ejecuta el mismo camino en código Go que `-race` sí ve; el censo |
| 33 | **Un error fatal de Go se lleva el stdout sin vaciar** (hasta 64 KiB de `kirt/out` bajo una pipe): un desbordamiento nativo imprime menos que la VM antes de morir, y un fallo del runtime podría esconder la salida que lo explica | T §2.21, §3.5 | Documentado en los apartados 6.3 y 8.10; `--stack-check=count` cuando hace falta el mismo stdout; el runner guarda stderr completo y falla con ruido ante cualquier salida 2 que no sea el desbordamiento |
| 34 | **Los programas de verificación de R1–R3 ya no existen** y los canarios se reescriben desde los resúmenes: un canario reescrito mal puede pasar en verde sin comprobar el hecho | R2 §12; T §2.7, §3.6 | Cada canario reproduce primero el número o el fallo que su informe cita (el `infinite cycle`, el `SIGSEGV` sin compensación, los 20 ms con un P) antes de darse por bueno; la fuente completa queda en `docs/research/proto/`; los canarios de ensamblador se escriben en el capítulo 47, antes de emitir nada |
| 35 | **El servidor de lenguaje reanaliza el módulo entero en cada edición** (no hay compilación incremental en v0): en un fichero grande puede notarse en el editor, y un servidor que bloquea es peor que ninguno. Y la integración con Zed depende de lo que su API de extensiones exija (una gramática de Tree-sitter, quizá) | decisión del 2026-10-28 | El debounce y la cancelación con `context` del capítulo 64, con un presupuesto de latencia medido por fichero; la compilación incremental basada en consultas queda en el epílogo como segunda versión; Neovim primero, que solo necesita el servidor; Zed después, con la gramática mínima como ejercicio si hace falta; el 64 es lo primero que se recorta de lo añadido ese día (apartado 12.3) |
| 36 | **El listón de los diagnósticos cuesta horas en cada código nuevo** (secundarias, nota, ayuda, fix-it, entrada del catálogo con dos ejemplos probados), y la tentación de «pulirlo después» crece con el ritmo | decisión del 2026-10-28 | La regla «un código nace completo» (apartado 7.3), que los revisores y el panel comprueban en cada sesión; el test del catálogo falla si un código no tiene entrada; los goldens de errores se escriben con el código, no después |

## 14. Una nota honesta sobre «experto»

Este libro te da algo poco común entre quienes programan en Go: construir de verdad un compilador, un runtime concurrente, un allocator, un backend nativo y su verificación, con disciplina profesional (`testing`, `-race`, `vet`, CI, pprof, revisión), y leer con fluidez el runtime de Go, su ensamblador, su modelo de memoria y la salida de su compilador. Esas son las habilidades de los puestos de rendimiento e infraestructura, y de contribuir al propio Go [R4 §10].

Go es un lenguaje pequeño, y eso corta en los dos sentidos. Este libro puede cubrir de forma plausible *todo* el lenguaje, las áreas de la biblioteca estándar que usa un ingeniero de sistemas y el toolchain. Pero un puesto sénior de Go espera además lo que un proyecto de compiladores en solitario no da: bases de código grandes y compartidas, arquitectura de servicios, las bibliotecas del ecosistema (gRPC, los clientes de Kubernetes, que son de terceros) y guardias. El mercado de Go es estable y de nivel medio, concentrado en backend, infraestructura en la nube y herramientas de DevOps; el trabajo de compiladores en Go es raro pero ya visible (TypeScript 7 de Microsoft está escrito en Go) (dossier, `cand-go.jobs`). Los recuadros «Go en el trabajo» señalan el lado de los servicios, y el epílogo, cómo contribuir a `golang/go`.

---

## 15. Decisiones tomadas por el diseño

Revísalas mientras lees; cualquiera se puede cambiar. Primero las nuevas o cambiadas por Go, que son las que importan hoy; después, las heredadas de ayer.

Cuatro cosas que figuraban aquí las resolviste tú el 2026-10-24 y viven ahora en los apartados 4 y 16 como decisiones tuyas: `testing` (antes G1), el desbordamiento de pila nativo (antes G8), la versión de Go (parte de G18) y la cadencia de pushes (parte de G45). G1 y G8 salen de la tabla sin renumerar el resto, y G18 y G45 conservan solo lo que sigue siendo del diseño.

### 15.1 Nuevas o cambiadas por Go

| # | Decisión | Por qué | Fuente |
|---|---|---|---|
| G2 | **S5 = Core+RC+EV**, una etapa nueva después de Perceus, ejecutada por el oráculo y compilada por la VM y el nativo; un capítulo nuevo (32) | Las fibras y continuaciones nativas no pueden ser trozos de pila bajo `go build`; el paso de evidencia generalizado es la técnica del backend en C de Koka y reutiliza los conjuntos de suspensión de S4 | R1 §2.7; R2 §3 |
| G3 | **Portador A: fibras como estados en el heap sobre tu planificador**, también en nativo; el portador B (una goroutine por fibra) como adaptador documentado y sección opcional del capítulo 58 | Tu decisión de escribir el planificador, y un solo runtime para los dos backends compilados; rendimiento parejo a profundidades realistas | R2 §4.2–4.3 |
| G4 | **La VM ejecuta S5** en vez de capturar segmentos de su pila; los segmentos quedan como ejercicio | Elimina el recorrido de marcos con mapas de slots para soltar una `Cont` y convierte la VM en oráculo del nativo al mismo nivel de IR | R2 §5.1 |
| G5 | **KiABI-A0** como convención nativa: ABI0, argumentos en memoria, `DX` para el closure, resultado en `AX` entre funciones Ki (en la pila en las llamadas a trampolines), `NO_LOCAL_POINTERS`, sin prototipos salvo puntos de entrada y trampolines, **área de salida fija por programa igual a la aridad máxima de cualquier destino de cola** (ya no «6 palabras y las demás caen a llamada normal»), colas indirectas con `ADJSP $-F; POPQ BP; JMP CX` más la compensación inalcanzable, puntos de entrada sin colas; **KiABI-B′** detrás de `--kiabi=b` | Es el único contrato que el runtime de Go sostiene; la garantía de cola tiene que ser la misma que en el oráculo y la VM porque el diferencial no vería perderla; B′ depende de dos hechos no prometidos | R1 §11, §10; T E2, §2.11 |
| G6 | **Trampolines en el mismo paquete** para llamar a `kirt`, en vez de `linkname` o referencias con ruta | El ensamblador no admite puntos en la ruta de un símbolo y una referencia plana no enlaza | R2 P3; R1 §1.3 |
| G7 | **Polls en línea (`--poll=inline`) por defecto, `--poll=call` como respaldo**, la CI con los dos y el canario C3 | R1 y R3 lo recomiendan con canario; R2 prefería el stub por defecto; el coste del en línea es cero medible | R1 §4.4; R2 §4.4; R3 §9.7 |
| G9 | **Heap propio:** `Value uint64` con direcciones absolutas; reserva `mmap` `PROT_NONE` de 64 GiB; `kirt/heap` como único importador de `unsafe`; ningún puntero de Go en memoria de Ki; `KI_HEAP_GB`/`KI_HEAP_MAX` | El GC de Go no debe ver ni escanear los objetos de Ki; una sola codificación para el heap real y el de depuración | R3 §9, §11 |
| G10 | **Modos del allocator:** `KI_ALLOC=debug|fence` y el heap de depuración `kiheapgo` (pinned `[]uint64`, `//go:nocheckptr`); desaparece `KI_ALLOC=system` | No hay ASan; `-race` solo ve memoria de Go | R3 §6 |
| G11 | **El contexto de la fibra** (evidencia, `yielding`, destino, marcos acumulados, `preempt` atómico, puntero `alloc` al contexto de allocator de la goroutine que la ejecuta) como argumento oculto 0, recargado tras cada llamada; `kiResume` fija `ctx.alloc` | Sin registros preservados ni TLS, es el único canal; el allocator en línea lo necesita; el flag de preempción vive en un solo sitio | R1 §5.3, §11.2; R2 §3.1; R3 §5.3; T E8–E9 |
| G12 | **Chase–Lev con `sync/atomic` (SC)**, slots `atomic.Pointer`, workers sin `LockOSThread`, aparcado con tokens por canal; el futex crudo solo se enseña y se mide | Go no tiene atómicos relajados; fijar hilos costó 580×; dentro de Go, el sueño de una goroutine ya lo paga Go | R2 §5.3–5.4 |
| G13 | **Preempción en tres capas:** (1) vida del proceso con la regla «toda función con cola o bucle lleva un `CALL` o un marco ≥ 128 bytes» y un poll en cada arista de retroceso sin prólogo; (2) monitor de 10 ms que escribe `ctx.preempt` con un atómico, leído por una comprobación **propia** `CMPB preempt(ctx)` tras el prólogo y en los polls de las funciones que pueden ceder (ni el prólogo de Go ni el poll en línea lo ven), con `--kipreempt=polls` como respaldo documentado y la medida en el capítulo 58; (3) retoma del slot para código puro largo, con el slot y el contexto de allocator como objetos distintos, construida en el 54b | El ensamblador nunca se preempta de forma asíncrona; el código puro no burbujea; `stackguard0` solo lo escribe Go; heredar el contexto de allocator con el slot corrompería el heap | R2 §4.4; R1 §4; T E1, E8, E9 |
| G14 | **Poller con dos implementaciones** (`epoll` propio en Linux; goroutines auxiliares como línea base y en Windows); IOCP propio en el capítulo 71 | `syscall` está congelado y no tiene epoll en Windows | R2 §6 |
| G15 | **Aparcar-y-soltar frente a la marca `parking`:** se decide por medición en el capítulo 45 con `vm-bubble`, y se confirma en el 54b con el nativo | Bajo A, la ventana de cerrojo pasa a ser O(profundidad); la VM que aparca en el sitio no la tiene, así que no serviría para medir | R2 §5.5; T §2.3 |
| G16 | **Floats:** conversiones `float64()` explícitas, `cmd/kicheck`, canario con builds `v3` y `arm64`, nada de FMA en el nativo | Go no tiene `-ffp-contract=off`; arm64 fusiona | R4 V11 |
| G17 | **Repositorio:** un módulo `github.com/aldezex/kilang` en los dos repos, `cmd/{ki,kigen,kicheck,bookx}`, `internal/*`, `kirt/` en la raíz (con `kirt/out` desde el capítulo 10), un `embed.go` en la raíz (`package kilang`) cuyo patrón crece con el libro (`stdlib` en el 10, `docs/errors` en el 19, `kirt` en el 48), `ignore ./build`, `docs/research/proto/` como módulos propios; **la prosa y las tarjetas en `main`, dentro del rango de su capítulo, antes de `chNN`**; `ki build` en `~/.cache/ki/` con la copia de `kirt` por hash de contenido y `replace`; `ki clean` | `kirt` debe ser importable desde módulos generados; un patrón `//go:embed` no sube a directorios padre y no compila si nombra un directorio que falta; compilador y runtime siempre coinciden; cada etiqueta de capítulo es autocontenida; nada queda tirado | R4 §5.7, F4; R1 §7.1; constructor §3.5, §3.7; T §2.15 |
| G18 | **Política de versiones** (se parte de Go 1.27.3: decisión tuya, apartado 4): `go 1.27` + `toolchain go1.27.3`; `GOTOOLCHAIN=local` en la CI y en `ki build`, que escribe la versión local en el `go.mod` generado; parches en cuanto salen; una versión mayor solo al final de una parte y con los catorce canarios en verde; Delve a versión fijada; nunca `GOEXPERIMENT` ni `x/*` | Go publica dos versiones al año y el libro dura más que el soporte de 1.27; lo que la investigación verificó es 1.26.5, y lo que usan las Partes I y II se reverifica sobre 1.27.3 antes del capítulo 1 | R4 §11.6; dossier; T E5 |
| G19 | **Canarios** como tipo de test (`kind: canary`) y como puerta de cada actualización; se reescriben en `kilang-ref` desde los resúmenes de R1–R3, que son lo único que queda, antes del capítulo que los usa, y cada uno reproduce primero el número o el fallo citado | Catorce hechos no prometidos sostienen el diseño; los programas originales se borraron | R1 §12; R2 §11; R3 §10; T §2.7 |
| G20 | **`-race` solo con `-tags kiheapgo`**, porque sin el heap de depuración da falsos positivos (no solo es ciego); compilación cruzada de `ki` para windows/amd64 y linux/arm64 desde el capítulo 1, sostenida desde el 37 con un `_windows.go` (stub) por cada fichero dependiente del sistema | `-race` es ciego sobre el heap real y se confunde con sus atómicos; la deriva de portabilidad se ve en segundos; `syscall.Mmap` no existe en Windows | R3 §2.5; R4 §5.8; T §2.9, §2.25; constructor E4 |
| G21 | **Estilo de Go:** ids tipados e `IdVec` genérico; nodos con `Kind` en slices, interfaces selladas solo donde aclaran; `panic`/`recover` solo para ICE; `error` solo para E/S y herramientas; `noCopy` para lo que se consume; rápida/lenta como estilo | Lo que Go da y lo que no da (ni tipos suma ni exhaustividad) | R4 §4; R2 §2.1; R3 §3.6 |
| G22 | **Numeración:** 11 partes; S5 entra en el 32; los dos capítulos nativos de pilas y efectos se funden en el 53; los capítulos 32–53 se desplazan en uno; el 54 se publica en dos mitades, 54a y 54b, con etiquetas propias; las Partes IX y X conservan su número hasta el 61. **Desde el 2026-10-28, 71 capítulos:** entran el 62, el 63 y el 64 (el servidor de lenguaje, G59) y los antiguos 62–68 pasan a 65–71 en todo el documento, los hitos M12–M14 incluidos | Mantiene la forma del libro; el LSP tiene que ir después del servidor HTTP del 61 (necesita el front end completo y la biblioteca estándar) y antes de la release del 65, que lo incluye | R2 §10; T §4; decisión del 2026-10-28 |
| G23 | **Horas:** ≈ 794 h de tronco y ≈ 913 con reserva el 2026-10-24; **≈ 856 h y ≈ 984 con reserva desde el 2026-10-28** (+3 en el 7, +30 en el 62–64, +29 en las secciones «Proyectos en Ki», apartado 12.3); M1 ≈ 81 h; el −4 y el −16 del borrador son estimaciones del diseño, no de R2; la revisión suma +2 (33), +2 (41), +2 (51) y +12 (54 estimado sin el tope: 54a y 54b, 14 h cada una); tope de 16 h con el 54 como única excepción nombrada; Parte VIII ±20 h; cuarto punto del orden de recorte si A no se sostiene; lo añadido el 2026-10-28 tiene su propio orden de recorte (apartado 12.3) | Método de R4 §7, con los ajustes de T §4 | R4 §7; T §4; decisión del 2026-10-28 |
| G24 | **Parte XI incluye `ki` mismo corriendo en ARM64 y Windows** | En Go es casi gratis; ayer no cabía en 82 h | R4 §4 (fila de Windows) |
| G25 | **Herramientas:** Delve por `go install` a versión fijada en el capítulo 10; gdb opcional; qemu-user en el 66; perf no se instala (pprof y trace). Lo que lleva `sudo` (qemu-user y qemu-user-binfmt en el 66, gdb opcional en el 47) lo instalas tú cuando toque: no se te preguntó, es el defecto del diseño | Son herramientas, no bibliotecas; sin `sysctl` ni toolchains cruzados; `@latest` rompería la política de versiones | R4 §2; T §2.23 |
| G26 | **Publicación incremental con historia congelada de lo publicado** y correcciones hacia delante con fe de erratas | Tu decisión de publicar ya hace imposible el rebase de lo ya leído | — |
| G27 | **El título**, provisional: *Ki: de cero a experto. Construye tu propio lenguaje funcional en Go* (no se te preguntó: se mantiene hasta que propongas otro) | — | — |
| G28 | Las continuaciones del oráculo se capturan con `slices.Clone` + `clear` + truncar y se reanudan con `append` (O(profundidad)), no con empalmes O(1) | Más simple y obviamente correcto; el oráculo no necesita velocidad | R2 §2.1, §10 |
| G29 | Un `cmd/kicheck` propio sobre `go/ast` y `go/types` para la regla de floats, la política de `unsafe` e imports y los pines de inlining; la exhaustividad de type switches como *stretch* | Los linters que lo hacen son de terceros | R4 §5.9 |
| G30 | Las primitivas trascendentes de `Float` salen de `math` y se comparan solo dentro de su plataforma (`// libm: platform`) | `math` usa ensamblador por arquitectura en algunas funciones | base |

Las siguientes las añadió la revisión del 2026-10-24: resuelven lo que las dos comprobaciones dejaban al constructor.

| # | Decisión | Por qué | Fuente |
|---|---|---|---|
| G31 | **Una sola S5, independiente del destino:** `Async` siempre «puede ceder» (e `IO` a solas ya no, desde el 2026-10-28: la E/S que aparca lleva `Async`, G61); el oráculo en S5 lo burbujea hasta la raíz de la fibra (un prompt de su planificador) como el nativo; la VM aparca en el sitio y `vm-bubble` burbujea, como quinto backend de la matriz; kigen bloquea canales a profundidad variable | Si no, la suspensión más frecuente del sistema solo se ejecutaría en nativo, a partir del capítulo 53, y la promesa de `--check-passes` sería falsa para `Async` | T E7; constructor E1 |
| G32 | **Nativo antes de S5 (capítulos 49–53):** las comprobaciones que quedan (tras las funciones con variable de fila; ninguna tras `print`, G61) se emiten con `bubble_k` bajado a un trap de ICE; `direct.Run` ejecuta `kimain` en la goroutine principal, y esa forma se queda para siempre como la de los programas sin `Async`; «sin efectos» significa sin efectos de usuario ni `Async`, con `print` permitido | `main` usa `IO` y `print` también; sin la regla, ni `fib` ni el corpus de la Parte II serían alcanzables en nativo antes del 53 | constructor §3.1; T §2.2; decisión del 2026-10-28 |
| G33 | **`kirt/out`**, el escritor de stdout/stderr, es una hoja de `kirt` compartida por los tres backends y está en la lista blanca del oráculo; `kirt` nace con él en el capítulo 10 | Un escritor por backend daría diferencias falsas en el observable principal; la hoja no tiene lógica concurrente que el oráculo deba contrastar | constructor E2; T §2.12 |
| G34 | **El último worker que va a dormir ejecuta la detección de bloqueo de Ki** (fibras, intereses de E/S armados, temporizadores) antes de bloquearse; un test por configuración | Si no, `checkdead` de Go mata el proceso con salida 2 antes que Ki, o, con el poller dormido, el proceso se cuelga para siempre | T E6 |
| G35 | **Slot y contexto de allocator son objetos distintos**; la retoma traspasa el slot y da al repuesto un contexto nuevo de un pool mayor que `GOMAXPROCS`; el atascado retira el suyo al suspenderse; estrés con `kiheapgo` y `-race`; se construye en el 54b | Heredar las free lists con el slot sería dos goroutines sobre las mismas listas sin sincronización | T E8, §2.4 |
| G36 | **Comprobación de preempción propia** `CMPB preempt(ctx)` tras el prólogo y en los polls de las funciones que pueden ceder; el flag solo en el contexto de la fibra, escrito con un atómico; `--kipreempt=polls` como respaldo; coste medido en el 58 | Ni el prólogo de Go ni el poll en línea leen nada que escriba Ki | T E9; constructor §3.12 |
| G37 | **Regla de los puntos seguros:** toda función con llamada de cola (estática o indirecta) o arista de retroceso contiene al menos un `CALL` o un marco ≥ 128 bytes; el verificador de MIR la exige; C4 cubre el par de hojas mutuas | Dos hojas que se llaman en cola entre sí forman un ciclo `nosplit` que el enlazador rechaza, y sin prólogo no habría punto seguro | T E1 |
| G38 | **Formato de `Float`** fijado por `kirt/fmtnum`: dígitos más cortos, forma fija en `1e-6 ≤ |f| < 1e21` y exponencial fuera (los umbrales de JavaScript), `.0` solo si la salida es `^-?[0-9]+$`, `NaN`/`inf`/`-inf`/`-0.0` | `strconv` con `'g'` da `1e+06` y `+Inf`, y «añadir `.0`» a ciegas daría `1e+06.0` | T §2.20 |
| G39 | **VM:** instrucciones `{u8 op, u8 a, u8 b, u8 c, u32 k}`, operandos de registro de 8 bits, ventana `(*[256]Value)(regs[base:base+256])` con un chequeo por marco | Con operandos `uint16` o indexando el slice habría tres `IsInBounds` por instrucción y C11 fallaría | T §2.10, V-t5 |
| G40 | **`unsafe` solo en `kirt/heap` y sus `_test.go`**, con accesores exportados (`Load`/`Store`, `AtomicAdd32`/`CAS64`, `Bytes`, overlays); el futex crudo y los canarios C7/C8 viven ahí; el ejercicio de layout del capítulo 11 usa `reflect` | La regla tenía excepciones no dichas; el test de imports las habría señalado | T §2.14; constructor §3.2 |
| G41 | **Sockets como enteros crudos** con `syscall.Close` en el drop del proxy; `*os.File` solo en el pool bloqueante; sin `runtime.AddCleanup` | `os.NewFile` sobre un fd no bloqueante lo entrega al netpoller de Go; el cleanup no protegería de nada | T §2.13 |
| G42 | **Inmortalizar es un recorrido** como `share`; la saturación comparte y luego inmortaliza; las constantes de tiempo de ejecución viven en el heap de Ki, no en `RODATA` | La inmortalidad no transitiva daría contadores corruptos y use-after-free en los hijos | T §2.16 |
| G43 | **Objetos grandes dentro de la reserva**, en páginas contiguas con cabecera en la primera | Un mapeo propio chocaría con la reserva única, el `Munmap` exacto y `ptr &^ 0xFFFF` | T §2.17 |
| G44 | **`ki build` exporta `CGO_ENABLED=0`** (salvo una build `-race` explícita) y `ki doctor` lo comprueba; el `go.mod` generado lleva la versión de `go env GOVERSION` | `net` con cgo da un ejecutable dinámico contra glibc y exige gcc; una versión constante en `ki` chocaría con la política de parches | T §2.18; constructor §3.11 |
| G45 | **Dos juegos de workflows**, los tuyos (capítulos 1 y 34) y los de kilang-ref con bookx. Las puertas (`ci/ci-local.sh` para las etiquetas de sesión, la CI de GitHub de kilang-ref en verde para cada capítulo publicado) y la cadencia de pushes son decisión tuya (apartado 4) | `bookx` y `book/` solo existen en la referencia; el borrador decía «la nocturna es la de tu repo» | constructor E3 |
| G46 | **Canarios y prototipos reescritos en `docs/research/proto/`** antes del capítulo que los usa; el **prototipo del portador A antes de publicar el 32**, como C14 | Los programas de R1–R3 se borraron; A llega tarde para un libro congelado | T §2.7, §3.2, §3.6 |
| G47 | **Gramática:** `tyarg` admite variables de fila y filas concretas entre llaves, con kind «fila»; `e \|> f(a)` es `f(e, a)` | `Gen<T, e>` no era expresable; el desazucarado de `\|>` no estaba escrito | constructor E5, §3.3 |
| G48 | **Capítulo 3:** el primer diagnóstico real lo emite la comprobación previa al léxico (UTF-8 inválido, byte de control) | El lexer no llega hasta el 5 | constructor §3.6 |
| G49 | **Un panic de Ki nunca es un `panic` de Go:** `Halt{101}` en la CEK, retorno del bucle en la VM, trampolín en nativo | `panic`/`recover` quedan solo para los ICE, como dice el estilo | T §2.8 |
| G50 | **Procedencia Ki** en `interp-rc` (span de Core) y en `KI_ALLOC=debug` (pc de bytecode y tabla de líneas; id inmediato desde ensamblador); `runtime.Callers` solo para el Go de `kirt` | `runtime.Caller` daría la línea del intérprete, no el sitio Ki | T §2.22 |
| G51 | **El runner falla con cualquier salida 2** que no case exactamente con la firma del desbordamiento, también los `panic:` de Go; el stdout perdido en los errores fatales queda documentado | Un `panic` en `kirt` sale con 2 sin `fatal error:`; el nativo imprime menos que la VM al desbordarse | T §2.21 |
| G52 | **`after(d)` con buffer 1 es una decisión de Ki**, distinta de los canales de `time` de Go (sin buffer desde 1.23, siempre síncronos en 1.27); el capítulo 26 la explica | El borrador decía «igual que `time.After`», que es falso | T E3 |
| G53 | **Prerrequisitos de Go en orden:** «lo justo para testear» en el 1, `errors.AsType` en el 4, `defer`/`recover` explícitos en el 9 | Los tests de tabla del 1 y el `recover` del 9 usaban cosas aún no enseñadas | T §2.6 |
| G54 | **C10 con la pata arm64 estática** (`-S` sin FMA) hasta que exista qemu en el 66; **C3 con `GOMAXPROCS ≥ 2`** (50 ms con un P) | No hay qemu hasta el 66 y R3 midió 20 ms con un P | T §2.19 |
| G55 | **Tokenizador de Plan 9 propio** para la web del libro, junto al de Ki | Las bibliotecas de CDN no traen esa gramática | constructor §3.9 |
| G56 | **El panic `lazy value depends on itself` se mantiene como defensa** y se prueba con un programa Core construido a mano | Con las reglas de v0 ningún programa Ki lo alcanza | constructor §3.10 |
| G57 | **La URL del libro es `https://claude.ai/artifact/F9s6xctJx1vRvagDt2aQUL`**, privada, con `db` y `user`; se lee antes de publicar y se conserva con su base de datos | El borrador no la daba; publicar en otro artifact perdería el progreso | constructor §3.8 |

### 15.2 Heredadas del documento de C++ sin cambios

Siguen vigentes tal cual: Core sin tipos con Lint y anotaciones `rep`; Core ejecutable en cada etapa y `--check-passes`; Perceus una sola vez sobre Core; el intérprete con su propio planificador y su propio modelo de canales; multinúcleo antes que nativo; el modelo de memoria antes del primer atómico; canales de Go puros con cierre explícito; el programa termina cuando `main` devuelve y el deadlock sale con 102; `spawn` como palabra clave y `task`/`join` como biblioteca; continuaciones locales a la fibra; las tres formas de cláusula; el camino rápido tail-resumptive solo en el capítulo 55; `IO` y `Async` primitivos; firmas `pub` obligatorias e inferencia por SCC; literales sin defaulting; sin tipo `Never`; `Result` en el orden de Rust; records con la restricción de Rust; `type` sin `struct` ni `enum`; el azúcar imperativo; la regla de línea; `if` sin `else`; `pub`, `#[derive]`, `#[test]` y `"${x}"`; `for` solo sobre List, Array y rangos; `Show` para humanos; los thunks sin cerrojo; palabras de 64 bits etiquetadas con `Int` canónico y `Float` empaquetado hasta el capítulo 56; la cabecera de Lean y reuse solo con `rc == 1`; el allocator estilo mimalloc; contexto explícito en vez de TLS; la escalera spill-all → Poletto–Sarkar → Wimmer–Franz con verificador; los códigos de error y de salida; el progreso en la base de datos del artifact; las tarjetas con claves en inglés y texto en español; historia lineal con etiquetas por capítulo y sesión; listados con `git show` y builds con `git archive`; `ki` compila en cruzado y los ejecutables se comparan con el oráculo; dos repos, el tuyo vacío desde el capítulo 1; listado completo la primera vez e interfaces siempre completas; `UInt64` desde el capítulo 16; `(a.b)(c)`; métodos sin UFCS; la raíz del proyecto sin `ki.toml`.

Las que cambian de *razón* pero no de *fondo*: sin TLS (ahora porque Go no tiene almacenamiento por goroutine), todos los registros caller-saved a través de las llamadas (ahora porque lo impone el ABI de Go), cross-compilación desde Linux (ahora con `GOOS`/`GOARCH`).

Las que desaparecen: segmentos de pila `MAP_NORESERVE` y `ki_switch`; KiCC con `r15` y `r10`; la frametable; la línea de enlace con `g++ -###`; `-ffp-contract=off` y SSE2 como flags; excepciones y RTTI; GCC y Clang; CMake; TSan y sus anotaciones; `ki_test`; `KI_ALLOC=system`.

### 15.3 Decisiones del 2026-10-28

Las cinco cosas que decidiste ese día están en el apartado 4 (errores, servidor de lenguaje, proyectos, llamadas que suspenden, el runtime a medida); estas filas son el detalle con el que el diseño las cumple.

| # | Decisión | Por qué | Fuente |
|---|---|---|---|
| G58 | **El listón de los diagnósticos** (apartado 7.3): primaria con texto, secundarias en el sitio relacionado, nota con la regla, ayuda solo con acción concreta, fix-it cuando es mecánico, entrada en `docs/errors` con ejemplo erróneo y corregido probados; un código nace completo; los fix-its se renderizan en la forma «diff» de rustc bajo la ayuda, y el JSON lleva `fixits`. Entra en el capítulo 7 como sesión `ch07-s4`, que pone al nivel los códigos E01xx–E03xx que ya existen y crea el catálogo; el 19 solo añade `ki explain` y el color | Tu decisión: «los errores con el cuidado de rustc». La prosa publicada de los capítulos 3, 5 y 6 se queda exacta en sus etiquetas, y lo que cambia se cuenta en el 7 (una corrección hacia delante, como manda el 11.2) | decisión del 2026-10-28 |
| G59 | **El servidor de lenguaje en el tronco**, capítulos 62 (JSON-RPC por stdio, `initialize`, sincronización, diagnósticos con sus fix-its como `quickfix`), 63 (hover, definición, referencias, autocompletado, símbolos, tokens semánticos) y 64 (debounce y cancelación con `context`, transcripciones JSON golden, Neovim con su cliente integrado primero y una extensión de Zed después, comprobando si exige una gramática de Tree-sitter); `ki lsp` en M12; sin `x/tools`: el JSON-RPC se escribe a mano | Tu decisión. Va tras el 61 porque necesita el front end completo y la biblioteca estándar, y antes de la release; Neovim es tu editor; la compilación incremental sigue en el epílogo porque el LSP no la necesita para un fichero | decisión del 2026-10-28 |
| G60 | **«Proyectos en Ki»** al final de las Partes II (calculadora, juego de la vida, Markdown→HTML; 5 h), III (parser de JSON, generador de sitios; 5 h), VII (chat TCP, caché clave-valor, limitador de tasa; 7 h), X (Mandelbrot o raytracer paralelo, con la aceleración medida; 5 h) y XI (el mini-Lisp; 7 h), como páginas propias del libro con goldens en `tests/projects/`, sin número de capítulo, con sus horas en la cabecera de su parte | Tu decisión; el orden sigue a lo que el lenguaje ya sabe hacer en cada punto, y la red y el paralelismo no existen hasta las Partes VII y X | decisión del 2026-10-28 |
| G61 | **Llamadas coloreadas y la fila como marca única:** `await` prefijo sobre una llamada cuya fila instanciada contiene `Async` (E0406 si falta, E0407 si sobra, E0206 si no sigue una llamada); las operaciones de `IO` que aparcan son `uses {IO, Async}` y las que no, `uses {IO}` (también `channel`, `close` y `after`); S5 marca «puede ceder» solo con `Async`, un efecto de usuario o una variable de fila, nunca con `IO` a solas; `spawn` sin `await`; `select` como su propio marcador; un marcador estático que la elaboración quita | Elegiste las llamadas coloreadas; con una sola marca (`Async`) el `await` del programa y la comprobación de S5 dicen lo mismo, y `print` deja de pagar una comprobación que nunca se disparaba | decisión del 2026-10-28 |
| G62 | **El runtime a medida del programa:** la fila de `main` se infiere (⊆ `{IO, Async}`); `ki build` genera `direct.Run(kimain)` sin importar planificador, canales, poller, temporizadores ni pool cuando ninguna fila alcanzable contiene `Async`, y `sched.Run(kimain, kiResume)` con los subpaquetes que las filas piden cuando sí; el poller, el montículo y el pool arrancan en su primer uso y los workers en el primer `spawn`; la especialización de efectos del 55 clona `map`, `sum` y `\|>` por sitio de llamada para las filas que no ceden; cada regla tiene su test (tamaño y símbolos con `go tool nm`, ningún `isYielding` tras `print` en el Core S5, el poller sin arrancar sin red, una sola goroutine sin `spawn`) | Tu petición: «lo más óptimo posible; que el binario sepa cuándo cargar qué». v0 compila programas enteros, así que el compilador sabe qué necesita el programa | decisión del 2026-10-28 |

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
| P10 (2026-10-24) | ¿Go se enseña desde cero o se asume? | Desde cero, como C++ |
| P11 (2026-10-24) | ¿Cómo es el backend nativo con Go? | Selección de instrucciones y asignación de registros propias que emiten ensamblador de Go; `go build` enlaza con un runtime de Ki en Go |
| P12 (2026-10-24) | ¿Qué pasa con el trabajo de C++? | Todo borrado; `kilang-ref` empieza de cero con el mismo nombre |
| P13 (2026-10-24) | ¿Cuántos lectores? | Uno: tú |
| P14 (2026-10-24) | ¿Entrega? | Incremental, publicando ya |
| P15 (2026-10-24) | Repositorio de referencia: ¿quién empuja, con qué cadencia y qué CI hace «verificada» una etiqueta? | Un push por capítulo, hecho por ti, antes de publicarlo. Un capítulo se publica solo con GitHub Actions en verde sobre su etiqueta `chNN`. Las etiquetas de sesión pasan por `ci/ci-local.sh`. Las correcciones antes de publicar pueden mover etiquetas aún no publicadas (`--force`) |
| P16 (2026-10-24) | ¿Qué versión de Go al empezar? | Go 1.27.3, ya instalada en `~/.local/opt/go1.27.3` con `go` en el `PATH` (`go1.27.3`). `go.mod` con `go 1.27` + `toolchain go1.27.3`. Antes del capítulo 1 se reverifican sobre 1.27.3 los hechos de las Partes I y II (toolchain, `go mod init`, la directiva `ignore`, los flags de `testing`, los métodos genéricos sobre tipos concretos); los de runtime y ensamblador los revalidan los canarios en sus capítulos |
| P17 (2026-10-24) | Desbordamiento de pila en nativo | Aceptar el error fatal de Go (salida 2), traducido a 103 por `ki run` y `ki test`; en diferencial, el resultado es inconcluso. `--stack-check=count` es opcional |
| P18 (2026-10-24) | ¿`testing` o un framework propio? | `testing`, de la biblioteca estándar. Siguen a mano el runner de golden de Ki, kigen, el reductor, bookx y kicheck |

P2, P5 y P6 los respondiste juntos al elegir el aspecto «TS + Rust».

Las preguntas 5 (`sudo`) y 6 (título) del borrador no se te plantearon: están en el apartado 17 con el defecto del diseño.

## 17. Lo que quedaba por responder (resuelto el 2026-10-24)

Eran seis preguntas, por importancia. Respondiste las cuatro primeras el 2026-10-24 (apartados 4 y 16); las otras dos no se te plantearon y llevan el defecto del diseño. No queda ninguna abierta. Las opciones de cada una, tal como se te presentaron, siguen en `questions-for-alvaro.md`.

| # | Pregunta | Estado |
|---|---|---|
| 1 | El repositorio de referencia: quién empuja, con qué cadencia y qué CI hace «verificada» una etiqueta (G45, en parte; §11.2 y §11.4) | **Respondida por ti (P15):** un push por capítulo, hecho por ti, antes de publicarlo |
| 2 | Versión de Go para empezar (G18, en parte) | **Respondida por ti (P16):** Go 1.27.3, ya instalada |
| 3 | Desbordamiento de pila en nativo (antes G8) | **Respondida por ti (P17):** aceptar el error fatal de Go, traducido a 103 por `ki run` y `ki test` |
| 4 | `testing` de la biblioteca estándar frente a un framework propio (antes G1; §11.1) | **Respondida por ti (P18):** `testing` |
| 5 | Instalaciones con `sudo` (G25; §11.5) | No se planteó. Defecto del diseño: las haces tú cuando toque (qemu-user y qemu-user-binfmt en el capítulo 66, gdb opcional en el 47) |
| 6 | El título provisional (G27; §12.1) | No se planteó. Defecto del diseño: se mantiene hasta que propongas otro |

## 18. Lo que queda para quien construya

- La lista exacta de opcodes y su codificación dentro del formato `{u8 op, u8 a, u8 b, u8 c, u32 k}`, incluidas las primitivas de S5.
- La tabla exacta de clases de tamaño del allocator.
- La redacción exacta de cada mensaje de error, dentro de las reglas del apartado 7.3.
- El corte exacto de cada capítulo en sesiones (el 54, en sus dos mitades).
- La lista fina de funciones de la biblioteca estándar.
- Los benchmarks concretos, y los programas Ki reales con los que se remiden los números de R1–R3 en la Parte IX; entre ellos, el coste de la comprobación `CMPB preempt(ctx)` y del contador `--stack-check=count`, que hoy son estimaciones.
- Las distribuciones de kigen: bucles largos sin asignar, continuaciones abandonadas, profundidad de los bloqueos de canal.
- La reescritura de los programas de verificación de R1–R3 en `docs/research/proto/`, canario a canario, antes del capítulo que usa cada uno.
- El prototipo del portador A de punta a punta, **antes de publicar el capítulo 32** (riesgo 4, canario C14).
- La decisión aparcar-y-soltar frente a `parking`, medida con `vm-bubble` en el capítulo 45 y confirmada en nativo en el 54b.
- La maquetación del libro web, dentro de las reglas del apartado 12.
- El texto exacto de cada entrada de `docs/errors` y de cada fix-it, dentro del listón del apartado 7.3; la lista de subpaquetes de `kirt` que cada primitiva arrastra al módulo generado (apartado 9.3); el formato de las transcripciones JSON golden del servidor de lenguaje y lo que Zed exija a su extensión (capítulo 64).

---

## Apéndice A. Qué cambió en la revisión del 2026-10-24

Lo que corrige cada comprobación, en el orden del documento. **T** es la comprobación técnica (sus errores E1–E9, sus inconsistencias §2.1–§2.25 y sus riesgos §3.1–§3.6); **C** es la del constructor (sus contradicciones E1–E5 y sus cuestiones menores §3.1–§3.12).

- **Apartado 3:** la 1.26.5 instalada no es la última de su línea (1.26.10, 2026-10-19; comprobado hoy en go.dev/dl); antes del capítulo 1 se instala 1.26.10 o 1.27.3 (T E5). Delve a versión fijada (T §2.23). `CGO_ENABLED=0` en `ki build` (T §2.18).
- **Apartado 5:** `tyarg` admite variables de fila y filas concretas, con kind «fila» (C E5); el desazucarado de `|>` (C §3.3); el formato de `Float` especificado en `kirt/fmtnum` (T §2.20); la pata arm64 del test de floats es estática hasta el 63 (T §2.19); el panic de `lazy` como defensa probada con Core a mano (C §3.10); `after(d)` con buffer 1 es una decisión de Ki, no «igual que `time.After`» (T E3); el fin del programa con la detección de bloqueo de Ki antes de dormir, porque `checkdead` sí puede adelantarse bajo A (T E6).
- **Apartado 6:** una sola S5 que siempre marca `Async` e `IO`, con el oráculo burbujeando hasta la raíz en S5, `vm-bubble` en la matriz y kigen a profundidad variable (T E7, C E1); el runner falla con cualquier salida 2 que no sea el desbordamiento, también los `panic:` de Go (T §2.21); el stdout perdido en los errores fatales (T §2.21).
- **Apartado 7:** el verificador de MIR exige la regla de los puntos seguros completa (T E1); `go vet` en dos invocaciones (T §2.23, C §3.2); `unsafe` solo en `kirt/heap` y sus tests (T §2.14); un panic de Ki nunca es un `panic` de Go (T §2.8).
- **Apartado 8:** las tres reglas de `kirt` con `kirt/out` en la lista blanca del oráculo y los accesores de `kirt/heap` (C E2, C §3.2, T §2.12, §2.14); inmortalizar como recorrido y las constantes de runtime fuera de `RODATA` (T §2.16); los objetos grandes dentro de la reserva (T §2.17); el contexto de allocator separado del slot, con pool y retirada (T E8); la procedencia Ki en `KI_ALLOC=debug` (T §2.22); el flag `preempt` en un solo sitio y la comprobación propia `CMPB preempt(ctx)` (T E9, C §3.12); la regla «toda función con cola o bucle lleva un `CALL`» (T E1); la razón correcta de por qué la VM no necesita polls (T §2.23); el último worker en dormir detecta el bloqueo (T E6); G15 medido con `vm-bubble` (T §2.3); la retoma del slot en el 54b (T §2.4); sockets como enteros crudos sin `AddCleanup` (T §2.13); el desbordamiento con su firma exacta, el stdout perdido y el coste del contador como estimación (T §2.21, §2.23).
- **Apartado 9:** la raíz del oráculo como prompt en S5 y la procedencia por span de Core (T E7, §2.22); el formato de la VM con operandos de 8 bits y la ventana `*[256]Value` (T §2.10); `vm-bubble` (T E7); `b.Loop` sí inlinea, y cuál es su trampa real (T E4); `kirt.Run(kimain, kiResume)`, la `toolchain` del `go.mod` generado desde `go env GOVERSION`, `CGO_ENABLED=0`, el stub de `kiResume` hasta el 54 (T §2.11, §2.18; C §3.11, §3.1); los trampolines con resultados en la pila (T §2.11); los puntos de entrada sin colas (C §3.4); la regla de las hojas con el par mutuo (T E1); el área de salida igual a la aridad máxima de los destinos de cola, las colas indirectas con su compensación y C13 (T E2); el poll en línea solo sirve a Go y la comprobación de Ki va aparte (T E9); el trap de `bubble_k` hasta el 53 (C §3.1, T §2.2); `RODATA` solo para lo estático (T §2.16).
- **Apartado 10:** `nosplit cycle` es un error del enlazador, no un `fatal error:` (T §2.23); el `spfix` entre los detalles internos (T E2).
- **Apartado 11:** el mismo *module path* en los dos repos, y quién empuja como pregunta 1 (C §4, C T1); el `go.mod` con la versión elegida; `embed.go` que crece por capítulo (C §3.5, T §2.15); `kirt/out` y los `_windows.go` (C E2, C E4, T §2.9); `docs/research/proto/` y `docs/toolchain.md`; la prosa en `main` antes de `chNN` (C §3.7); dos juegos de workflows y la corrección de «la nocturna es la de tu repo» (C E3); la cabecera de test con `vm-bubble`; la política de versiones con 1.26.10 o 1.27.3 y Delve fijado (T E5, §2.23); los canarios reescritos desde los resúmenes (T §2.7), C3 con `GOMAXPROCS ≥ 2`, C4 con el par mutuo, C10 estático en arm64, C11 con la ventana, y los nuevos C13 y C14 (T §2.19, E1, E2, §3.2); la razón correcta de «`-race` con `kiheapgo`» (T §2.25); la máquina con la versión a sustituir (T E5).
- **Apartado 12:** la URL del artifact (C §3.8); el tokenizador de Plan 9 (C §3.9); las horas: 794 h de tronco y 913 con reserva, el −16 y el −4 etiquetados como estimaciones del diseño, +2 en el 33 y el 41 (T E7), +2 en el 51 (T E2), el 54 estimado sin el tope y partido en 54a y 54b, la Parte VIII a ±20 h, el calendario y los hitos recalculados (T §4); la regla de los capítulos nativos 49–53 (C §3.1, T §2.2); M1 con el preludio incrustado y `kirt/out` desde el 10; los capítulos 1, 3, 4, 9 (prerrequisitos de Go: T §2.6; el chequeo pre-léxico: C §3.6), 10, 11, 16, 19, 21 (T §2.8), 26 (T E3), 32 (sin la frase de kigen y sin el prototipo de R2: T §2.1, §2.7), 33, 37 (T E4, §2.9, §2.17), 38, 40, 41, 44 (T §2.4, E6), 45 (T §2.3), 46 (T §2.13), 47 (T §2.23), 48, 49, 51 (sin `sort.Search`: T §2.5), 52, 53, 54a/54b, 58 y 63 (T §2.24).
- **Apartado 13:** el `spfix` en el riesgo 1, el prototipo temprano en el 4, el stdout en el 7, la deriva de parches en el 19, el 23 ya no es «solo bajo B», y los nuevos 32 (`-race` nunca ve el nativo), 33 (stdout perdido) y 34 (canarios reescritos) (T §3).
- **Apartado 15:** G5, G8, G11, G13, G15, G17–G20, G22, G23 y G25 actualizadas; G31–G57 nuevas, una por cada cuestión que las comprobaciones dejaban al constructor.
- **Apartado 17:** de cuatro preguntas a seis (entran el protocolo del repositorio de referencia y `testing` frente a un framework propio), cada una con opciones y recomendación; la de la versión de Go pasa a «1.26.10 o 1.27.3».
- **Apartado 18:** el prototipo del portador A antes del 32; `vm-bubble` para G15; las estimaciones que hay que medir.

**Plan 01 (2026-10-24).**

- **Apartados 11.1, 12.4 y 12.5:** el `embed.go` de la raíz nace en el capítulo 7 con el preludio (`//go:embed stdlib`, lo mínimo); el capítulo 10 ya lo encuentra hecho y el 16 lo profundiza.
- **Apartado 11.1:** `tests/` e `internal/testsupport` (código solo de test) importan también `internal/driver`; `cmd/ki` sigue siendo el único paquete que no es de test que lo importa.
- **Apartado 7.2:** los ids llevan el sufijo `ID` (`ExprID`, `TypeID`, `DefID`), como manda la guía de estilo de Google, y las listas que no son de nodos (`Attr.Args`, `Type.Params`, `Row.Labels`, `core.Fun.Params`) son slices, inmutables tras construirse.
- **Apartado 7.2:** el interner indexa con un `map[uint64]Symbol` sobre `hash/maphash` con cadenas de colisión, en lugar de un `map[string]Symbol`.
- **Apartado 7.3:** el color del renderizador humano se aplaza al capítulo 19, con `ki explain`.
- **Apartados 11.1 y 11.2:** las directivas citan etiquetas de sesión (`chNN-sK`), `{{solution}}` es el modo `solution` de las regiones y la página del libro vive en `cmd/bookx/web/`.

**Enmiendas del 2026-10-28** (cinco decisiones tomadas con Álvaro durante el capítulo 7; `.superpowers/sdd/2026-10-28-decisions-pending-amendment.md`; G58–G62). Los números de capítulo que cita el resto de este apéndice son los del 2026-10-24: el antiguo 63 es hoy el 66.

- **Cabecera y apartados 2 y 4:** la actualización del 2026-10-28; el LSP sale de los no-objetivos; cinco filas nuevas en «Lo que decidiste tú».
- **Apartado 5:** `await` en el ejemplo ampliado del 5.1 y en la gramática del 5.3 (prefijo unario sobre una llamada; E0206); en el 5.6, la fila de `main` inferida, las dos clases de operaciones de `IO` con `{IO, Async}` para las que aparcan y `{IO}` para las que no (`channel`, `close` y `after` incluidas), S5 marcando solo con `Async`, y las reglas de `await` con E0406 y E0407; el 5.11 con `await` en `send`, `recv`, `sleep` y `yieldNow`.
- **Apartado 6:** «puede ceder» sin `IO` a solas (6.1), la especialización de efectos del 55 como decisión con su test, y la comparación entre pases citando solo `Async` (6.3).
- **Apartado 7:** `ki build` importa solo los subpaquetes que las filas piden (7.1); el listón de los diagnósticos y los fix-its como sugerencias de rustc, con el JSON `fixits` (7.3).
- **Apartado 8:** los workers arrancan en el primer `spawn` (8.5); el poller, el montículo y el pool arrancan en su primer uso (8.7).
- **Apartado 9:** `direct.Run` y `sched.Run` según las filas, con el test de `go tool nm`; la forma `direct` como la permanente de los programas sin `Async` (9.3).
- **Apartados 5.10, 8.5, 11.4, 11.5, 13, 15 y 17:** los capítulos 62–68 renumerados a 65–71 (kicheck 65; qemu y arm64 66; memoria débil 67; `ubuntu-24.04-arm` 67; `windows-latest` 69; el stub del heap de Windows 70; IOCP 71).
- **Apartado 12:** las horas (856 h de tronco, 984 con reserva; M1 81 h; el calendario y los hitos recalculados; el orden de recorte de lo añadido); el capítulo 7 a 9 h con su sesión de diagnósticos; los capítulos 62–64 del servidor de lenguaje; las secciones «Proyectos en Ki» de las Partes II, III, VII, X y XI; la regla de los capítulos 49–53 reescrita (sin comprobación tras `print`; `direct.Run`); los capítulos 19, 20, 25, 26, 32, 44, 46, 48, 49 y 55 amendados; el epílogo sin el LSP. El fixture `cmd/bookx/testdata/spec-index.md` conserva el índice del 2026-10-24 (68 capítulos, 794 h) hasta que `bookx` vuelva a tocarse; la invariante «la cabecera de una parte suma sus filas» que su test comprueba deja de cumplirse en el spec para las cinco partes con «Proyectos en Ki».
- **Apartado 13:** los riesgos 35 (capacidad de respuesta del LSP y Zed) y 36 (el coste del listón).
- **Apartado 15:** G14, G22, G23, G25, G31, G32 y G54 actualizadas; el 15.3 con G58–G62.
- **Apartado 18:** el texto del catálogo y de los fix-its, los subpaquetes por primitiva y el formato de las transcripciones del LSP.
