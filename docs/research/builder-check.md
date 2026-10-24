# Comprobación del constructor: Ki, edición Go

**Fecha:** 2026-10-24 · **Documento:** `2026-10-24-ki-design-go.md` (leído entero) · **Contexto consultado:** R1–R4 y `base-cpp-design.md`. Un dato (quién empuja al repo de referencia, y la URL del libro) sale del registro de la sesión de la era C++, no del documento; se marca donde aparece.

**Veredicto.** Quedan cosas importantes, pero pocas.
- Seis preguntas para Álvaro. Dos cambian lo que se construye de forma sustancial: el protocolo de pushes y CI del repo de referencia, y el desbordamiento de pila nativo. Las dos últimas (sudo y título) no cambian lo que se construye; están porque el §17 las lista.
- Cinco contradicciones o huecos del documento, cada uno con un arreglo propuesto.
- Doce cuestiones menores que resuelve el constructor sin cambiar lo que se construye.
- Las horas cuadran: el total por partes, los acumulados de cada hito y el calendario son coherentes, igual que las referencias a capítulos y la renumeración 32–53 contra la edición C++.

---

## 1. Lo que solo puede responder Álvaro (por importancia)

### T1. Repositorio de referencia: quién lo crea y empuja, con qué cadencia, y qué CI hace «verificada» una etiqueta

**Por qué cambia el plan.**
- El §11.2 exige «la CI en verde» para anotar una etiqueta en `book/verified-tags.txt` y publicar el capítulo.
- El último párrafo del §11.4 verifica la referencia en local.
- Las correcciones previas a la publicación hacen `rebase --autosquash --update-refs` y mueven etiquetas, lo que en GitHub exige force-push de `main` y de las etiquetas.
- **Fuera del documento** (registro de la era C++): el sistema de permisos bloquea `gh repo create` y `git push` del agente, y Álvaro ejecutó cada push a mano.
- Con unas 300 etiquetas de sesión, la cadencia decide cuánto trabajo le toca a él, si cada publicación espera por él, dónde vive `nightly.yml` y cómo es el flujo de correcciones. Resuelve el error E3.

**Opciones.**
- **(a)** Álvaro crea `aldezex/kilang-ref` y empuja una vez por capítulo, antes de publicarlo: `main`, las etiquetas de sesión y la de capítulo, con `--force` solo si se movieron etiquetas aún no publicadas. Las etiquetas de sesión pasan por `ci/ci-local.sh`, y un capítulo solo se publica con GitHub Actions en verde sobre `chNN`. `nightly.yml`, con `bookx`, vive en kilang-ref.
- **(b)** Empuja tras cada etiqueta de sesión (unos 300 pushes), y GitHub Actions es la puerta de todas.
- **(c)** La CI local es la única puerta. Empuja una vez por parte (11 pushes), y GitHub Actions solo informa.
- **(d)** Él permite en su propia configuración los pushes a kilang-ref (es decisión y cambio suyos; el agente no toca permisos), y el agente empuja por sesión.

**Recomendación: (a).** Unos 68 pushes en dos años; cada capítulo publicado tiene CI pública en verde; las correcciones antes de publicar siguen siendo posibles.

### T2. Desbordamiento de pila en nativo (§17.1, G8, §8.10)

**Por qué cambia el plan.** Toca un observable (el código 103) y el prólogo de cada función nativa. Decide si `--stack-check=count` es el defecto, cómo se clasifica el caso en las pruebas diferenciales y qué se mide en el capítulo 58.

**Opciones.**
- **(a)** El defecto del documento: aceptar el `fatal error: stack overflow` de Go (salida 2), traducirlo a 103 en `ki run` y `ki test`, tratarlo como inconcluso en diferencial y dejar el contador como opción.
- **(b)** El contador por defecto en todo binario: 103 exacto siempre, a unos 0,7 ns por llamada (del orden del 30 % en `fib`).
- **(c)** Híbrido: contador por defecto en `ki run`, `ki test` y las builds de depuración, y apagado en `ki build --release`.

**Recomendación: (a).** La profundidad de desbordamiento ya no es observable (§6.3). Bajo `ki`, los tres backends dan el mismo código, y el 30 % sería el coste más visible del backend.

### T3. Versión de Go para empezar (§17.2, G18)

**Por qué cambia el plan.**
- Fija el `go.mod` del capítulo 1 y el texto de los capítulos 4 y 17 («sin métodos genéricos en 1.26» frente a 1.27).
- Decide qué hay que reverificar antes de empezar.
- 1.26 sale de soporte cuando se publique 1.28 (hacia febrero de 2027), más o menos al acabar la Parte I.

**Opciones.**
- **(a)** Empezar en 1.26.5 y pasar a 1.27 al final de la Parte I, con los canarios (el defecto del documento).
- **(b)** Instalar 1.27.x antes del capítulo 1 y reverificar solo los hechos que usan las Partes I y II: toolchain, `go mod init`, la directiva `ignore`, los flags de `testing` y los genéricos. Los hechos de runtime y ensamblador de R1–R3 los revalidan los canarios cuando lleguen sus capítulos.
- **(c)** Instalar 1.27.x y rehacer R1–R4 completos antes de empezar.

**Recomendación: (b), por poco.** Cuando llegue la Parte VI el toolchain será 1.28 o posterior con cualquier opción, así que lo verificado de R1–R3 sobre 1.26.5 habrá que rehacerlo igualmente. Lo único que compra 1.26.5 es la Parte I, que enseñaría una limitación que 1.27 ya quitó. Si prima «empieza ya» sin ninguna espera, (a) es razonable.

### T4. G1: `testing` de la biblioteca estándar frente a un framework propio (§11.1, §15 G1)

**Por qué cambia el plan.** R4 (§11.8, línea 554) lo marca como decisión de Álvaro. El documento la tomó, pero no la llevó al §17. Toca su regla «construir todas las áreas del compilador a mano… usar librerías lo más mínimo». Cambia el capítulo 1 (+4 h) y la columna `ref` de todas las tarjetas de test.

**Opciones.**
- **(a)** `testing` (el defecto).
- **(b)** Un framework propio mínimo escrito en el capítulo 1.
- **(c)** `testing` más un paquete propio de helpers escrito a mano (`internal/testkit`: golden con `-update` y asserts de diagnósticos).

**Recomendación: (a).** Ya deja a mano el runner golden, kigen, el reductor, bookx y kicheck, y es lo que espera un empleador, que es su criterio de desempate.

### T5. Instalaciones con sudo (§17.3)

No cambia lo que se construye salvo con la opción (b).

**Opciones.**
- **(a)** Álvaro instala `qemu-user` y `qemu-user-binfmt` antes del capítulo 63, y gdb si quiere en el 47.
- **(b)** Sin qemu: la corrección en ARM64 también se comprueba en el runner `ubuntu-24.04-arm`. El bucle de trabajo es más lento y cambia el flujo del capítulo 63.
- **(c)** Instalarlo todo ya.

**Recomendación: (a).**

### T6. Título (§17.4, G27)

No cambia lo que se construye.

**Opciones.**
- **(a)** Mantener el provisional, *Ki: de cero a experto. Construye tu propio lenguaje funcional en Go*.
- **(b)** Uno suyo.

**Recomendación: (a)** hasta que él proponga otro. Cambiarlo es un retoque de la web.

---

## 2. Contradicciones y huecos del documento

### E1. Qué marca S5 como «puede ceder» (§6.1 frente a §9.2; afecta a §6.3 capa 1 y §8.4)

**El problema.**
- El §6.1 dice que una fila puede ceder si contiene un efecto de usuario, una variable de fila «o, solo en nativo bajo el portador A, `Async` o `IO`».
- El §9.2 dice que la VM «ejecuta la misma S5 que el nativo» y que las comprobaciones de S5 para `Async` «son código muerto en la VM».
- Las dos cosas no pueden valer para una sola IR: o S5 depende del destino, o siempre marca IO y Async.
- Además, no se dice cómo ejecuta el oráculo una S5 en la que Async e IO burbujean: su raíz de fibra tendría que atender un `yieldTo` de esos efectos.
- Así, `--check-passes` S4↔S5 (capítulo 32, M6) puede no cubrir justo el camino del que dependen los capítulos 53 y 54. Eso contradice el §6.3 capa 1, que promete que un error de S5 aparece «antes de que exista la VM o el nativo».

**Arreglo propuesto.**
- Una sola S5, que siempre marca IO y Async.
- El oráculo y la VM aparcan en el sitio por defecto. Un flag (`--bubble-async`) los hace burbujear hasta la raíz como el nativo.
- `--check-passes` ejecuta S5 en el oráculo con el flag y sin él.
- El capítulo 32 añade la raíz de fibra del oráculo como prompt de Async e IO (unas 2 h más, estimación mía).

### E2. `ki_write` compartido frente al aislamiento del oráculo (§8.7 frente a la introducción del §8 y al §9.1)

**El problema.**
- El §8.7 dice que stdout y stderr pasan por un único escritor, `ki_write`, «que comparten todos los backends».
- La introducción del §8 y el §9.1 dicen que el oráculo solo importa `kirt/splitmix` y `kirt/fmtnum`, y que un test de imports lo comprueba.
- M1 (§12.4) ya necesita en el intérprete el vaciado del buffer antes de `os.Exit`.

**Arreglo propuesto.** Sacar el escritor a un paquete hoja, `kirt/out` (buffer, regla de vaciado y un mutex; sin planificador ni heap), y añadirlo a la lista permitida del oráculo y al test de imports. Existe desde el capítulo 10. Compartirlo evita diferencias falsas en el observable principal, y no tiene lógica concurrente que el oráculo deba contrastar.

### E3. La CI del repo de referencia (§11.2 y §11.4 frente al §11.1)

**El problema.**
- El §11.2 dice: «Una etiqueta solo vale cuando la CI está en verde sobre ella».
- El último párrafo del §11.4 verifica la referencia «en esta máquina» y añade que «la CI nocturna es la de tu repo».
- Pero el `nightly.yml` del §11.4 ejecuta `bookx verify` y reconstruye «todas las etiquetas de capítulo», y el `ci.yml` de cada push incluye la «verificación de listados». Las dos cosas solo existen en kilang-ref: según el §11.1, `cmd/bookx` y `book/` son solo de la referencia, y el repo de Álvaro tiene el mismo layout sin ellos.
- El documento tampoco dice quién crea el repo en GitHub ni quién empuja y fuerza los pushes tras mover etiquetas (§11.2).

**Arreglo propuesto.** Resolverlo con T1, y definir dos juegos de workflows: los de kilang-ref (con bookx y las etiquetas) y los que Álvaro escribe en kilang en el capítulo 1 (sin bookx).

### E4. Compilación cruzada desde el capítulo 1 frente a un runtime que solo es de Linux (§11.4 y capítulo 1 del §12.5 frente a §8.3, §8.7 y capítulo 44)

**El problema.**
- `cmd/ki` importa `internal/vm`, que importa `kirt`, que importa `kirt/heap`, que usa `syscall.Mmap`, `Mprotect` y `Madvise`. Ninguno existe con `GOOS=windows` (R4 V10).
- `eventfd`, `futex` y epoll también son solo de Linux.
- Desde el capítulo 37, `GOOS=windows go build ./cmd/ki` falla, y no se arregla hasta el 67.
- El documento no dice cómo sigue en verde la comprobación de la CI mientras tanto.

**Arreglo propuesto.**
- Desde el capítulo 37, un `heap_windows.go` que use el heap `kiheapgo`, que es portable, o un stub que haga panic con «llega en el capítulo 67».
- El poller se elige por sufijo de fichero (`poll_goroutine.go` en Windows).
- El `futex` crudo va en un fichero `_linux.go`.

### E5. Gramática: argumentos de fila en los tipos (§5.3 frente a §5.6)

**El problema.** `tyarg = ty | '_'` no admite una variable de fila, pero el §5.6 escribe `type Gen<T, e> { … () -> Gen<T, e> uses {..e} }`. Tampoco hay forma de instanciar ese parámetro con una fila concreta (`Gen<Int, {Log}>`).

**Arreglo propuesto.** `tyarg = ty | '_' | lower | '{' [ eff { ',' eff } ] [ [ ',' ] '..' lower ] '}'`, con kind «fila» para los parámetros en minúscula.

---

## 3. Cuestiones menores (las resuelve el constructor)

1. **Nativo antes del S5 nativo (capítulos 49–52).** `print` es IO, y bajo el portador A una fila con IO «puede ceder». Pero el burbujeo (`bubble_k`, `kiYieldExtend`) llega en el 53, y la raíz de fibra con `kiResume` en el 54.
   - Hasta el 53, `kirt.Run` ejecuta `kimain` en la goroutine principal con un contexto de fibra mínimo.
   - Las primitivas de IO terminan de forma síncrona por su trampolín y nunca ponen `yielding`.
   - El backend no emite las comprobaciones, o las baja a un trap de ICE.
   - En M9 («puros y secuenciales») y en el capítulo 51 («sin efectos»), la palabra significa sin efectos de usuario ni Async; `print` sí está permitido.
2. **«Solo `kirt/heap` importa `unsafe`»** (§8, §7.2). Eso obliga a que `alloc`, `rc`, `str` y `array`, el overlay `(*workerCtx)(ptr)` y los atómicos del capítulo 38 (`(*atomic.Int32)(unsafe.Pointer(p))`) pasen por accesores de `kirt/heap`.
   - `kirt/heap` exporta Load y Store de palabras, AtomicAdd y CAS sobre desplazamientos, `Bytes(v, n)` y overlays tipados.
   - El capítulo 38 enseña el patrón dentro de `kirt/heap`.
   - `go vet` necesita dos invocaciones: todo menos heap, y `-unsafeptr=false ./kirt/heap` (regla 2 de R3).
3. **Desazucarado de `|>`, que el documento no da.** Los ejemplos y el §5.5 («propone `name(e, …)` o `e |> name`») implican inserción como primer argumento: `e |> f(a)` equivale a `f(e, a)`, y `e |> f` a `f(e)`.
4. **Llamadas de cola desde puntos de entrada** (§9.3). La regla de que todo sitio de llamada reserva 6 palabras no vale para los marcos que llama Go (`kimain`, `kiResume` y lo que este reproduce), cuyo argsize es el real del prototipo. Los puntos de entrada nunca hacen llamada de cola, y el verificador lo exige; o declaran un argsize de al menos 6 palabras.
5. **`embed.go` y el preludio temprano.** `//go:embed kirt stdlib docs/errors` falla si un patrón no existe, y `kirt` aparece hacia el capítulo 11 (`fmtnum`) y `docs/errors` en el 19. Además, el preludio de M1 (capítulo 10) existe antes de que el 16 enseñe `//go:embed`. El patrón crece por capítulo: el 10 introduce `embed.go` con `stdlib` y una explicación mínima, y el 16 lo profundiza.
6. **Capítulo 3: «`ki check` muestra un diagnóstico con caret»** antes de que exista el lexer (llega en el 5). Se cubre con un chequeo previo al lexer (UTF-8 inválido o un byte de control) que ya emite un `Diagnostic` real con su golden.
7. **Dónde viven la prosa y la web.** El §11.1 pone `book/` (prosa, tarjetas y web) en `main`, con historia lineal y etiquetas por sesión. En la era C++, la web vivía en una rama `web` y la prosa en un worktree aparte. Hay que elegir una de dos cosas, y que `bookx` la asuma:
   - la prosa y las tarjetas de cada capítulo entran dentro de su rango, antes de la etiqueta `chNN`;
   - o van a una rama `book` que referencia etiquetas de código.
8. **El artifact del libro.** El documento no da su URL. Según el registro de la sesión, es `https://claude.ai/artifact/F9s6xctJx1vRvagDt2aQUL` (privado, con base de datos de progreso). Hay que leerlo antes de publicar, y conservar la URL y la base de datos.
9. **Resaltado de Plan 9** (§12.1). Es probable que las bibliotecas de resaltado de CDN no traigan una gramática del ensamblador de Go. Si no la traen, se escribe un tokenizador pequeño junto al de Ki.
10. **El panic `lazy value depends on itself`** (§5.10) parece inalcanzable: `let` no es recursivo, y los ciclos entre constantes se rechazan también a través de los cuerpos de las funciones (§5.9, §8.9). Se mantiene como defensa y se prueba con un programa Core construido a mano.
11. **El `go.mod` generado** (§9.3) fija `go 1.26 ; toolchain go1.26.5`. Debe escribir la versión del toolchain local (`go env GOVERSION`), para que la política de versiones del §11.4 no obligue a cambiar `ki`.
12. **Preempción de Ki en las aristas de retroceso** (§8.5 capa 2, §9.3). El poll en línea solo compara `SP` con `stackguard0`, que el monitor de Ki no toca. Las funciones que pueden ceder necesitan además `CMPB preempt(ctx)` en prólogos y aristas. Se decide y se mide en los capítulos 51 y 58.

---

## 4. Ya respondido por el documento o por las decisiones de Álvaro

- **Anfitrión:** Go, enseñado desde cero, con comparaciones con Rust, TS/JS y Python en los recuadros «Si vienes de…» (§1, §4, §12.2).
- **Backend nativo:** selección de instrucciones y asignación de registros propias, que emiten Plan 9; `go build` lo enlaza con `kirt` (§4, §9.3).
- **Reutilizar prosa o código de la era C++** para los capítulos que no dependen del anfitrión (lexer, parser, Maranget): no. Lo impiden el anti-objetivo del §2 y «todo el trabajo de C++ está borrado» (§4).
- **Module path de los dos repos:** `github.com/aldezex/kilang` (la línea de `go.mod` del §11.1 y «la del tuyo es la misma»), así que los ficheros de la referencia encajan sin cambios en el repo de Álvaro.
- **Quién escribe qué:** Álvaro escribe todo el código y todos los tests de kilang, incluidos kigen, el runner y kicheck. `bookx` y `book/` son solo de la referencia (§11.1).
- **`print` antes de que existan `Show` e `IO`:** `#[extern("ki_print_int")]` en el preludio mínimo, sustituido en el capítulo 14 (§12.4).
- **Portador A frente a B, aparcar-y-soltar frente a la marca `parking`, el prototipo del portador A, opcodes, clases de tamaño y corte en sesiones:** quedan al constructor o a la medición (§8.4, §8.6, §18).
- **Terceros:** nada dentro de Ki; biblioteca estándar más `syscall`. Las herramientas (Delve, qemu, gdb) no entran en `go.mod`. La web del libro puede usar un resaltador de CDN (§2, §3, §12.1).
- **Correcciones:** antes de publicar, fixup y rebase; después, solo hacia delante, con fe de erratas (§11.2, G26).
- **Progreso:** en la base de datos del artifact, un solo registro, con horas reales (§12.1).
- **Documentación pública:** `docs/research` (R1–R4 y el dossier) y `docs/superpowers/specs` van en el repo de referencia público, como en el layout aprobado de la era C++ (§11.1).
- **Observables, códigos de salida y especificación del modo determinista:** §6.3, §8.8 y §8.10.
- **`Int`:** 64 bits con panic al desbordar (decisión de Álvaro), con la representación canónica inmediato/`BoxedInt` (§8.1).
- **Orden de backends** (intérprete, VM, nativo) y **multinúcleo antes que nativo:** §4 y Parte VII del §12.5.
- **Plan:** 776 h más un 15 % de reserva, 14 hitos, orden de recorte, y lo que no se recorta nunca (§12.3, §12.4).
