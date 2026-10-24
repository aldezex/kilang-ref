# Comprobación técnica del diseño de Ki (edición Go)

**Documento revisado:** `2026-10-24-ki-design-go.md` · **Contra:** R1 (backend en ensamblador), R2 (efectos y fibras), R3 (Perceus y heap propio), R4 (currículo) · **Fecha:** 2026-10-24 · **Toolchain usado para verificar:** `go1.26.5 linux/amd64`, `GOAMD64=v1`, `CGO_ENABLED=1`.

**Cómo leerlo.** Cada hallazgo da el apartado del documento, lo que dice, el hecho correcto, la evidencia y la corrección. La evidencia es una de tres: un informe (`R2 P3`), una prueba que he ejecutado hoy en esta máquina (`[V-tN]`, resumen en el apéndice A) o el código fuente de Go 1.26.5 (`[S]`, fichero:línea bajo `$GOROOT/src`). Los programas de prueba vivieron en un directorio nuevo del scratchpad, ya borrado.

**Resumen.**
- **9 errores** (apartado 1): hechos de Go equivocados, mecanismos nativos incompletos y afirmaciones de verificación que van más allá de lo que el diseño cubre.
- **25 inconsistencias menores**, cada una con su corrección (apartado 2).
- **6 riesgos que faltan** en el apartado 13 (apartado 3).
- **Horas** (apartado 4): las sumas cuadran al dígito; lo discutible es el razonamiento de la Parte VIII.
- **Nada nuevo para Álvaro.** Ningún hallazgo abre una bifurcación que tenga que decidir él. Lo único que cambia es la premisa de la pregunta 2 del apartado 17 (la versión de Go; ver E5).
- **Lo comprobado y correcto** está en el apartado 5, para que se pueda confiar en el resto.

---

## 1. Errores

### E1. Ciclos de llamadas de cola entre hojas: no enlazan (§9.3 «Hojas» y «Llamadas de cola», canario C4, §8.5 capa 1)

- **Lo que dice:** «Toda función Ki con bucle lleva un `CALL` (el camino lento de su poll) y el problema no aparece»; «cada salto reentra el prólogo del destino, así que los bucles por llamada de cola son preemptibles gratis». C4 solo prueba la hoja que se llama *a sí misma*.
- **Hecho correcto:** una función con marco < 128 bytes y sin `CALL` queda `NOSPLIT` sin prólogo, aunque termine en `RET ·g(SB)`, porque el `RET` con destino no cuenta como `CALL`. Dos hojas que se llaman en cola entre sí (`isEven`/`isOdd`, máquinas de estados, recursión mutua) forman un ciclo nosplit que el enlazador rechaza. Si enlazara, tampoco tendría ningún punto seguro. Ninguna de las dos tiene un «bucle» en el sentido de una arista de retroceso local, así que la regla del documento no les pone el poll.
- **Evidencia:** R2 P3 lo verificó con el caso mutuo y R2 §1.1 dice «the code generator must enforce this for every function that loops through tail calls». [V-t3]: `main.kiEven: nosplit stack over 792 byte limit … grows 8 bytes, calls main.kiOdd<0> … infinite cycle`. Con un `CALL` inalcanzable en cada una, enlaza, recupera el prólogo (`MOVQ FS:-8, R14; CMPQ SP, 0x10(R14); JBE`) y da el resultado correcto.
- **Corrección:**
  - La regla local queda así: «toda función que contiene una llamada de cola (`RET ·f(SB)` o la indirecta de E2) o una arista de retroceso lleva al menos un `CALL`, el camino lento del poll o uno inalcanzable, o un marco ≥ 128 bytes».
  - El verificador de MIR o del LIR lo exige.
  - C4 cubre también el ciclo mutuo.

### E2. Las llamadas de cola nativas solo cubren destinos estáticos de ≤ 6 argumentos, así que la garantía de cola se pierde (§9.3 «Llamadas de cola»)

- **Lo que dice:** el mecanismo es `RET ·callee(SB)`; «las llamadas de cola de hasta 6 argumentos (contexto incluido) siempre funcionan y las demás caen a una llamada normal». No dice nada de las llamadas de cola a un closure.
- **Hecho correcto:**
  - El ensamblador solo convierte `RET` en `JMP` cuando el destino es un símbolo (`cmd/internal/obj/x86/obj6.go:853`, `if p.To.Sym != nil { // retjmp`) [S]. Una llamada de cola indirecta (`f(x)` en posición de cola con `f` un closure, el estilo CPS, `apply`, combinadores) no tiene mecanismo en el documento.
  - El oráculo y la VM sí hacen esas llamadas sin marco (§9.1, §9.2), y el diseño de C++ las garantizaba (R2 §1.2: «Guaranteed tail calls … Kept»).
  - Resultado: un programa que corre en espacio constante en el oráculo y la VM agota la pila en nativo, y como el desbordamiento es «inconcluso» en el diferencial, nadie lo detecta.
  - Lo mismo pasa con las llamadas de cola de más de 6 argumentos.
- **Evidencia:** [V-t11]. Una cola indirecta escrita a mano (`ADJSP $-F; POPQ BP; JMP CX`) **mata el proceso en el primer escaneo de pila del GC** (`SIGSEGV … runtime.(*unwinder).next … scanstack`). El bloque `morestack` que el ensamblador añade al final supone que el desplazamiento de SP acumulado al final de la función es el del marco, y mi epílogo manual lo dejó a 0. Con dos instrucciones inalcanzables de compensación tras el salto (`PUSHQ BP; ADJSP $F`), 10⁸ colas indirectas mutuas corren en ~200 ms con `runtime.GC()` concurrente, `GODEBUG=gccheckmark=1,invalidptr=1` y la pila plana (StackInuse ≈ 700 KiB), tres veces seguidas.
- **Corrección:**
  - (1) Como v0 compila programas enteros, el área de salida mínima de cada sitio de llamada es la **aridad máxima de cualquier destino de cola del programa**, no 6, y se quita el «caen a una llamada normal».
  - (2) Las colas indirectas usan la secuencia verificada (`ADJSP $-F; POPQ BP; JMP CX`, seguida de `PUSHQ BP; ADJSP $F` inalcanzables), con `DX` = closure y destino `NEEDCTXT`.
  - (3) Un canario nuevo, C13, con la prueba de [V-t11]. Depende de un detalle no documentado del ensamblador (el `spfix` del bloque `morestack`) y va también al riesgo 1.

### E3. `after(d)` no es «igual que `time.After` en Go» (§5.11)

- **Lo que dice:** «`after(d)` devuelve un `Chan<()>` con buffer 1 …, igual que `time.After` en Go».
- **Hecho correcto:**
  - Desde Go 1.23, con `go ≥ 1.23` en `go.mod`, los canales de temporizador no tienen buffer: `cap(time.After(d)) == 0`.
  - En Go 1.27 son siempre síncronos, porque `asynctimerchan` se eliminó: «Channels created by package time are now always unbuffered (synchronous)», según go.dev/doc/go1.27.
  - El capítulo 26 del propio documento lo enseña bien («canales de temporizador sin buffer (1.23)»).
- **Evidencia:** [V-t1]: `cap(time.After) = 0` y `cap(NewTimer.C) = 0` en un módulo `go 1.26`.
- **Corrección:** el buffer 1 puede quedarse, porque es una decisión de Ki: el temporizador nunca bloquea al servicio de temporizadores. Pero se dice como diferencia con Go, no como igualdad, y el capítulo 26 la explica.

### E4. `b.Loop` sí inlinea en 1.26.5 (§9.2 «Dispatch y velocidad»; capítulo 37 «`b.Loop` y su trampa de inlining»)

- **Lo que dice:** «Un `b.Loop` con el bucle caliente escrito dentro del cuerpo no inlinea: el benchmark mentiría [R3 §5.4]».
- **Hecho correcto:**
  - En Go 1.26.5 las llamadas escritas dentro del cuerpo de `for b.Loop() { … }` **se inlinean**.
  - Lo que sigue siendo cierto, según `go doc testing.B.Loop`, es que los argumentos, los resultados y las variables asignadas dentro del cuerpo se mantienen vivos con un `runtime.KeepAlive` intrínseco.
  - R4 §3.2 lo decía bien: «since 1.26 it no longer prevents inlining in the loop body». R3 se equivoca en esto.
- **Evidencia:** [V-t2], con `go test -c -gcflags=-m`:
  - `b_test.go:15:19: inlining call to addFast` dentro de `for b.Loop()`;
  - `c_test.go:24:10: inlining call to inc`;
  - `c_test.go:38:16: inlining call to addTagged`, que además inlinea `addFast` y `addSlow`.
- **Corrección:**
  - La «trampa» que enseña el capítulo 37 es otra: los valores del cuerpo se mantienen vivos (un `KeepAlive` por resultado), así que el cuerpo mide también ese coste. Y en un `b.N` clásico el compilador puede borrar trabajo cuyo resultado no se usa.
  - Se quita la frase de §9.2 y se cita R4 §3.2 en lugar de R3 §5.4.

### E5. 1.26.5 no es la última versión de la línea 1.26 (§3, §11.4, G18, §17 pregunta 2)

- **Lo que dice:**
  - El documento fija `toolchain go1.26.5`, «la versión que tienes instalada».
  - Su propia política dice «los parches menores se adoptan en cuanto salen».
  - Lista 1.27.0–1.27.3, pero no los parches de 1.26.
- **Hecho correcto:** go.dev/doc/devel/release (leído hoy a través de un resumen automático de la página, con las líneas citadas) da `go1.26.10 (released 2026-10-19)`, con correcciones de seguridad (`crypto/x509`, y antes el comando `go`). La 1.26.5 va cinco parches por detrás. Las fechas de 1.27.x del documento sí son correctas: 1.27.0 el 2026-08-19 y 1.27.3 el 2026-10-19.
- **Corrección:**
  - Antes del capítulo 1, se instala 1.26.10 (es manual, porque `GOTOOLCHAIN=local` impide la descarga).
  - Se reejecutan los canarios y las verificaciones de R1–R4 que vayan a ser tests, y se fija `toolchain go1.26.10`.
  - La pregunta 2 del apartado 17 pasa a ser «1.26.10 o 1.27.3».
  - El resto de los hechos verificados sobre 1.26.5 casi seguro no cambian entre parches, pero eso lo dicen los canarios, no el documento.

### E6. El `fatal error: all goroutines are asleep` de Go sí puede adelantarse a Ki bajo el portador A (§5.11 «Fin del programa»)

- **Lo que dice:** «Como las fibras no son goroutines, el `fatal error: all goroutines are asleep` de Go no puede adelantarse a este informe».
- **Hecho correcto:**
  - Bajo A, los *workers* sí son goroutines y duermen en un `chan struct{}` por worker (§8.5).
  - `checkdead` (`runtime/proc.go:6367-6470`) [S] mata el proceso cuando no hay ningún M en marcha (los Ms en una syscall cuentan como en marcha) y ningún P tiene temporizadores. Pasa, por ejemplo, en los capítulos 44–45, con la VM multinúcleo antes de que exista el poller.
  - Al revés, con el poller bloqueado en `EpollWait(-1)` Go nunca declara el bloqueo, y si Ki no lo detecta, el proceso **se cuelga para siempre** en lugar de salir con 102.
- **Evidencia:** [V-t7]. Cuatro goroutines «worker» aparcadas en su token: `fatal error: all goroutines are asleep - deadlock!`, salida 2. La misma prueba con una goroutine en `syscall.EpollWait(ep, ev, -1)`: colgada, y `timeout 3` termina con 124.
- **Corrección:**
  - Invariante: **el último worker que va a dormir ejecuta la detección de bloqueo de Ki antes de bloquearse**. La detección cuenta las fibras vivas y aparcadas, los intereses de E/S armados en el poller y los temporizadores. Si todo está aparcado sin nada pendiente, sale con 102 y vuelca los sitios de aparcamiento.
  - Un test por cada configuración (sin poller, con poller, con monitor) exige 102 y que el proceso no se cuelgue.
  - Se reescribe la frase de §5.11 y el riesgo 23 deja de ser «solo bajo B».

### E7. Los fallos de S5 en el burbujeo de `Async`/`IO` no aparecen «al nivel de Core» (§6.3 capa 1, §6.1 «S5 en detalle», §9.2)

- **Lo que dice:**
  - «Un error de S5 (un conjunto de suspensión mal copiado, un prompt sin reinstalar…) aparece aquí, al nivel de Core, antes de que exista la VM o el nativo».
  - La VM, que ejecuta S5, es «oráculo de depuración del código nativo al mismo nivel de IR».
- **Hecho correcto:**
  - `Async` e `IO` solo pueden ceder «en nativo bajo el portador A» (§6.1).
  - El oráculo es una CEK que aparca en el sitio, y en la VM las comprobaciones de S5 para `Async` son «código muerto» (§9.2, R2 §5.1).
  - Por tanto, la suspensión más frecuente de todo el sistema (cada canal, cada `sleep`, cada E/S bloqueante que burbujea hasta la raíz de la fibra, y su reproducción con `kiResume`) solo se ejecuta en el backend nativo.
  - `--check-passes` S4↔S5 solo cubre las cesiones a handlers de usuario. Un conjunto de suspensión mal copiado en un sitio que solo puede ceder por `Async` no lo ve nadie hasta el capítulo 53–54. R2 §8 tiene el mismo hueco.
- **Corrección:**
  - (1) En el modo S5 del oráculo, `perform Async.op` y `perform IO.op` se implementan como `yieldTo(raíz de la fibra)`, con burbujeo y reproducción, igual que el nativo.
  - (2) El flag de la VM para «burbujear como el nativo» (capítulo 41) se vuelve un backend de la matriz diferencial (`vm-bubble`), no un flag de depuración.
  - (3) kigen genera bloqueos de canal a profundidad variable.
  - Hecho esto, la frase de §6.3 vale; antes no.

### E8. La retoma del slot rompe la exclusividad de `ctx.worker`, y con ella las free lists del allocator (§8.4 «El contexto de la fibra», §8.5 capa 3, G13)

- **Lo que dice:**
  - «`kiResume` escribe `ctx.worker` …; es seguro porque una fibra solo migra mientras está aparcada».
  - Capa 3: «una goroutine de repuesto se queda con el deque, y la atascada termina su cálculo puro sin slot».
- **Hecho correcto:**
  - El código puro asigna memoria por el camino rápido en línea, a través de `ctx.worker`, con free lists por clase sin atómicos (§8.3), y libera en la lista local del dueño.
  - Si el repuesto hereda el slot con su contexto de worker, dos goroutines usan las mismas free lists sin sincronización: corrupción del heap.
  - Si no lo hereda, el documento no dice de dónde saca el suyo, ni qué pasa con las páginas que posee el contexto de la goroutine atascada.
  - La fibra no ha migrado, pero el slot sí. R2 §4.4 lo marca como «not prototyped», y el documento lo repite sin ver el choque.
- **Corrección:**
  - El *slot* (deque, `runnext`, token de sueño) y el *contexto de allocator* son objetos distintos.
  - La retoma traspasa el slot y da al repuesto un contexto de allocator nuevo, de un pool con más de `GOMAXPROCS` contextos.
  - La goroutine atascada conserva el suyo hasta que su fibra se suspende. Entonces ese contexto se retira: sus páginas pasan al pool, y lo liberado mientras tanto por otros entra por `thread_free`.
  - Un test de estrés lo cubre con el heap `kiheapgo` y `-race`.

### E9. Ni el prólogo de Go ni el poll en línea ven `ctx.preempt` (§8.5 capa 2, §9.3 «Polls», G13)

- **Lo que dice:** «Los prólogos y los polls de las funciones que pueden ceder lo prueban y realizan `Async.yieldNow` burbujeando»; «`kirt.Poll` … comprueba `ctx.preempt`».
- **Hecho correcto:**
  - El prólogo lo escribe el ensamblador y solo compara `SP` con `g.stackguard0`.
  - Cuando falla por preempción, salta a `morestack → newstack → gopreempt_m` sin pasar por Ki (R1 §4.1).
  - El poll en línea (`CMPQ SP, 16(R14); JLS slow`) solo toma el camino lento si Go ha pedido preempción o la pila está baja. El monitor de Ki escribe `ctx.preempt` sin tocar `stackguard0`, así que nada de lo descrito lo lee.
  - La capa 2 solo funciona de dos maneras, y el documento no elige ninguna:
    - (a) Ki emite una comprobación propia, `MOVQ ctx+0(FP), R; CMPB preempt(R), $0; JNE yield`, tras el prólogo de toda función que puede ceder y en sus polls. Son dos cargas y una rama por llamada, sin medir en R1–R3.
    - (b) Ki aprovecha la preempción de sysmon, que pone `stackPreempt` a los 10 ms. Esto solo llega a Ki en los **polls**, nunca en los prólogos, así que el código recursivo sin bucles no cede a nivel de Ki (es la inferencia de R3 §5.2).
  - Además, el flag está en dos sitios: §8.3 lo pone en el contexto del worker y §8.4/§8.5 en el de la fibra. El monitor es otra goroutine, así que su escritura y la lectura del lado Go tienen que ser atómicas, o `-race` (con `kiheapgo`) lo señala.
- **Corrección:**
  - Se elige (a) para las funciones que pueden ceder (en las puras no hace falta: no pueden ceder) y se mide en el capítulo 58; o se elige (b) y se documenta su límite.
  - El flag va en un solo sitio, el contexto de la fibra, con escritura atómica del monitor.
  - Se corrige el texto de §8.5, §9.3 y G13.

---

## 2. Inconsistencias menores, con su corrección

### Índice y orden de los capítulos

1. **El capítulo 32 promete kigen antes de que exista.** Su hito dice «kigen abandonando continuaciones en cada `perform`», pero kigen se construye en el capítulo 33. Al insertar S5 en el 32, el resto se desplazó y esta frase no. *Corrección:* pasarla al hito del 33 o al M6, en el 34.
2. **Los capítulos nativos 49–52 compilan S5 antes del capítulo que lo enseña (el 53).**
   - El problema:
     - `main` es `uses only {IO, Async}` y `print` es `uses {IO}`. Bajo el portador A, toda llamada a `print` lleva la comprobación `isYielding` y su `join_k`.
     - Lo mismo cualquier llamada a una función con variable de fila (`map`, `sum`, `|>`), que es justo el corpus de la Parte II del hito del capítulo 52.
     - Los closures nativos llegan en el 52 y S5 nativo en el 53. Así, ni «`fib` en nativo» (capítulo 49, M9) ni el capítulo 52 son alcanzables tal como están escritos. «Puros y secuenciales» no basta, porque `main` no es pura.
   - *Corrección:* hasta el capítulo 53, el backend emite la comprobación con un camino lento que hace panic («yield not supported natively yet»). También se puede clasificar `ki_write`, que no aparca, como primitiva que no cede.
3. **G15 no puede decidirse por medición en el capítulo 45.** La ventana O(profundidad) entre aparcar y soltar solo existe bajo el portador A nativo; en el 45 la VM aparca en el sitio y la ventana es constante. *Corrección:* medir con la VM en modo burbujeo (capítulo 41; ver E7) o aplazar la decisión al 54.
4. **La retoma del slot en el capítulo 44.** La VM se preempta por presupuesto de instrucciones, así que allí la retoma no se puede ejercitar con Ki real, y el 54 la vuelve a listar. *Corrección:* construirla en el 54 (con E8 resuelto). En el 44, como mucho, el monitor.
5. **Capítulo 51, «tablas binarias con `sort.Search`».** Es un resto de la frametable de R4 (su capítulo 50). §6.1 dice que la frametable ya no existe. *Corrección:* quitarlo o darle un uso real, por ejemplo una tabla PC → posición Ki para el informe de panics nativo.
6. **Go desde cero: prerrequisitos.**
   - Capítulo 1: los tests de tabla y los subtests (`t.Run(name, func(t *testing.T){…})`, `t.Cleanup(func(){…})`) necesitan literales de slice y de struct, `range` sobre slices y literales de función. Eso se enseña en el 2 (slices, structs) y en el 6 (closures).
   - Capítulo 3: `errors.AsType[*fs.PathError](err)` necesita la instanciación explícita de genéricos, que llega en el 4.
   - Capítulo 9: usa `recover` en la frontera del pase, pero `defer` no aparece como tema en ningún capítulo. R4 §3.1 pone `defer`/`panic`/`recover` en el 10.
   - *Corrección:* en el capítulo 1, una sección mínima de «lo justo para testear» (literal de slice de structs, `range`, `func` literal), y el resto en el 2 y el 6. `errors.AsType` pasa al 4 o al 19. «`defer` (LIFO, evaluación de argumentos, `recover` solo en una llamada diferida)» entra explícitamente en el 9.
7. **El capítulo 32 da como lectura guiada el prototipo de R2, que ya no existe.** «El prototipo de R2 (`Ctx`, `Perform`, `Extend`, `Handle`, `Resume`, unas 140 líneas) como lectura guiada»: R2 §12 dice que los prototipos se borraron. Lo mismo vale para todas las fuentes de los canarios C1–C12 (R1 n14, n2b, n5, n13; R2 P3; R3 V1, V3, V7, V8): solo quedan los resúmenes. *Corrección:* reescribirlos en `kilang-ref` (`kirt/canary`, `docs/research/proto/`) antes del capítulo que los use, y decirlo en §11.4.
8. **Estilo de `panic`.** §7.2 y G21 dicen «`panic` solo para ICE», y el capítulo 21 dice «`panic` y `recover` solo para los panics de Ki». *Corrección:* en la CEK un panic de Ki es un estado `Halt{code: 101}`, sin `panic` de Go, y la regla de §7.2 se queda.

### Arquitectura, ABI y build

9. **La compilación cruzada de `./cmd/ki` para windows/amd64 se rompe en el capítulo 37 (§11.4, G20).**
   - El problema: `ki` importa la VM, la VM importa `kirt/heap` y `kirt/poll`, y esos llaman a `syscall.Mmap` y `EpollCreate1`. Desde el 37 hasta el 67, la comprobación «desde el capítulo 1, en cada push» falla.
   - Evidencia: [V-t4], `GOOS=windows go build`: `undefined: syscall.Mmap`, `undefined: syscall.PROT_NONE`…
   - *Corrección:* cada fichero dependiente del sistema nace con su pareja `_windows.go`. Puede ser un stub que devuelve «no soportado todavía» o el poller de goroutines, y la reserva por `VirtualAlloc` vía `NewLazyDLL` puede adelantarse. El M14 sigue igual.
10. **La afirmación sobre las comprobaciones de límites de la VM (§9.2, canario C11) no encaja con su propio formato.**
    - El problema: el formato es `{u16 op, u16 a, u16 b, u16 c}` sobre `regs []Value` con `base`, y la afirmación habla de «`[256]T` indexado por `uint8`».
    - Evidencia: [V-t5]. `*[256]Value` con `uint8` no tiene ningún `IsInBounds`. Con operandos `uint16`, o con slice más `base`, hay tres por instrucción. C11 fallaría.
    - *Corrección:* operandos de registro de 8 bits (≤ 256 registros por marco), con la ventana convertida a `*[256]Value` (`(*[256]Value)(regs[base:base+256])`, un solo chequeo al entrar en el marco) y palabras de extensión para lo que no quepa. Si no, aceptar los chequeos y quitar la afirmación.
11. **El contrato del módulo generado (§9.3).**
    - (a) `kirt` no puede importar `main`, así que `kiResume` tiene que llegarle como valor `func`: `kirt.Run(kimain, kiResume)`, no solo `kimain`.
    - (b) Los puntos de entrada que llama Go (`kimain`, `kiResume`) tienen un argsize de prototipo fijo y nunca pueden hacer llamadas de cola con más palabras que las de ese prototipo. La regla del área de salida mínima solo vale entre funciones Ki.
    - (c) Las llamadas a trampolines son ABI0 completo: los resultados vuelven **en la pila**, en el área de salida tras los argumentos, no en `AX` (el wrapper hace `MOVQ AX, 0x20(SP)`, R2 P11). El área de salida tiene que cubrir los argumentos y los resultados.
    - *Corrección:* escribir (a)–(c) en `docs/abi.md`.
12. **El oráculo y `ki_write`.** §8 y §9.1 dicen que el oráculo solo importa `kirt/splitmix` y `kirt/fmtnum`, y §8.7 dice que `ki_write` es «un único escritor … que comparten todos los backends». *Corrección:* añadir `kirt/io/write`, que es una hoja sin planificador, a la lista blanca del oráculo, o darle al oráculo su propio escritor con la misma política de vaciado y decir que son dos.
13. **Descriptores (§8.7).**
    - (a) `os.NewFile` sobre un fd no bloqueante lo hace *pollable* y lo registra en el netpoller de Go (`os/file_unix.go:88-99, 155, 195-219`) [S]. Un socket que `kirt/poll` vigila con su propio epoll no debe ser un `*os.File`.
    - (b) `runtime.AddCleanup` como «red de seguridad» no protege de nada: la tabla del lado Go mantiene vivo el objeto hasta que el último drop del proxy libera la entrada, y ese drop ya puede cerrar. Además, `*os.File` ya tiene su propio cierre al recogerse (doc de `File.Fd`).
    - *Corrección:* los sockets son `int` crudos con `syscall.Close` explícito en el drop del proxy, y `*os.File` queda solo para el pool bloqueante. Los recursos filtrados los detecta el censo, no un cleanup.
14. **«`unsafe` solo en `kirt/heap`» (§7.2, §8) tiene excepciones que no se dicen.**
    - El futex crudo del capítulo 44 (`Syscall6(SYS_FUTEX, uintptr(unsafe.Pointer(&w)), …)`).
    - Los canarios C7 y C8, que guardan la dirección de un objeto de Go en memoria de Ki.
    - Los ejercicios de `unsafe.Sizeof`/`Offsetof` del capítulo 11.
    - *Corrección:* que vivan en `kirt/heap` o en sus `_test.go`, y que el test de imports permita `unsafe` solo en `kirt/heap` y sus tests.
15. **`embed.go` no compila en las etiquetas tempranas (§11.1).** `//go:embed kirt stdlib docs/errors` falla mientras falte alguno de esos directorios: [V-t10] da `pattern docs/errors: no matching files found`. Va contra el anti-objetivo «un listado que no compile en su etiqueta». *Corrección:* `embed.go` nace en el capítulo 16 con `stdlib`, suma `docs/errors` en el 19 y `kirt` en el 48. Cada etiqueta compila.
16. **La inmortalidad no es transitiva (§8.1 «Objetos inmortales», §8.2 «Compartición»).**
    - El problema:
      - Un objeto *local* cuyo contador satura a `INT32_MAX` pasa a `rc = 0`, pero sus hijos siguen siendo locales.
      - `share()` «se para en lo … inmortal», así que si ese objeto llega a otra fibra, sus hijos locales reciben `dup`/`drop` planos desde dos fibras: contador corrupto y use-after-free, no una fuga.
      - Lo mismo pasa con «las constantes ya inicializadas» si no se inmortalizan recursivamente.
    - *Corrección:*
      - Inmortalizar es un recorrido como `share`, con todo lo alcanzable a `rc = 0`, como el `mark_persistent` de Lean. Si no, la saturación de un objeto local primero lo comparte y luego lo deja pegado.
      - De paso: las constantes inicializadas en tiempo de ejecución no van en `RODATA` (§9.3 «Datos»), solo los literales.
17. **«Los objetos mayores tienen su propio mapeo» (§8.3)** choca con «una sola reserva que nunca se desmapea» (Munmap exacto) y con la cabecera de página en `ptr &^ 0xFFFF`. *Corrección:* los objetos grandes ocupan páginas contiguas *dentro* de la reserva, con su cabecera en la primera página de 64 KiB.
18. **`ki build` debe exportar `CGO_ENABLED=0` (§9.3).** `kirt/poll_goroutine.go` importa `net` y compila también en Linux; con el `CGO_ENABLED=1` de esta máquina, el ejecutable sale con cgo y enlazado dinámicamente contra glibc. Choca con el anti-objetivo «ni cgo» y además exige gcc para un `ki build`. Evidencia: [V-t6], `dynamically linked, interpreter /lib64/ld-linux-x86-64.so.2`, frente a `statically linked` con `CGO_ENABLED=0`; `go list` muestra los `CgoFiles` de `net`. *Corrección:* `ki build` exporta `CGO_ENABLED=0`, salvo en una build `-race` explícita de depuración. `ki doctor` lo comprueba.
19. **Los canarios C10 y C3.**
    - C10 ejecuta una build `GOARCH=arm64`, pero en esta máquina no hay qemu ([V-t0]: `qemu-aarch64 not found`) y no lo habrá hasta el capítulo 63, con `sudo`. El test del capítulo 11 en §5.10 tiene el mismo problema.
    - C3 («`runtime.GC()` < 10 ms») falla con `GOMAXPROCS=1`: R3 V4 midió 20 ms.
    - *Corrección:* la pata arm64 de C10 es estática (`GOARCH=arm64 go build -gcflags=-S` sobre las primitivas de `Float` y un grep que exige que no aparezca `FMADDD|FMSUBD|FNMADDD|FNMSUBD`) hasta que qemu exista. La de `GOAMD64=v3` sí puede ejecutarse aquí. C3 fija `GOMAXPROCS ≥ 2`, o el umbral es 50 ms con un P.
20. **El formato de `Float` (§5.10) no está definido fuera de los ejemplos.**
    - El hecho: `strconv.FormatFloat(f, 'g', -1, 64)` pasa a exponente en `|f| ≥ 1e6` y `< 1e-4`, con exponente de dos dígitos y signo. Evidencia: [V-t1] da `"100000"`, `"1e+06"`, `"1.23456789e+08"`, `"1e+21"`, `"0.0001"`, `"1e-05"`, `"NaN"`, `"+Inf"`, `"-0"`.
    - «Más la regla del `.0`» aplicada a ciegas da `1e+06.0` o `NaN.0`.
    - *Corrección:* `kirt/fmtnum` especifica la forma de Ki. Por ejemplo: dígitos más cortos de `'e', -1` y forma fija en un rango acordado (`1000000.0`), `.0` solo si la salida casa con `^-?[0-9]+$`, y `NaN`/`inf`/`-inf`/`-0.0` explícitos. Todo con golden tests. Es una decisión de diseño menor que no necesita a Álvaro.
21. **Los fatal errors de Go se llevan el buffer de stdout (§8.10) y la regla del runner es estrecha (§6.3).**
    - (a) Un fatal error de Go (desbordamiento, `out of memory`, `concurrent map writes`) termina sin pasar por Ki, así que los hasta 64 KiB de `ki_write` sin vaciar (stdout es una pipe bajo `ki run`) se pierden. El `ki run` nativo da el código 103 traducido, pero menos salida que la VM.
    - (b) Un panic de Go en `kirt` sale con 2 y `panic: …`, no con `fatal error:`.
    - *Corrección:* documentar (a). Si se quiere el mismo stdout, la vía es `--stack-check=count`. Para (b), el runner falla ante **cualquier salida 2** que no case exactamente con la firma de desbordamiento.
22. **Procedencia (§9.1 `--level=rc`, §8.3 `KI_ALLOC=debug`).** `runtime.Caller`/`Callers` da posiciones Go del intérprete o del bucle de la VM, no el sitio Ki que asigna. *Corrección:* registrar el span del nodo de Core (en el oráculo) o el pc de bytecode más su tabla de líneas (en la VM). `runtime.Callers` queda para el código Go de `kirt`.
23. **Redacción.**
    - §10(d) y el capítulo 47 llaman «error fatal» a `nosplit cycle`, que es un error del enlazador, no un `fatal error:` en ejecución.
    - «`go vet ./...` (con `-unsafeptr=false` solo en `kirt/heap`)» no se puede expresar en una sola invocación: son dos, `go vet $(go list ./... | grep -v /kirt/heap$)` y `go vet -unsafeptr=false ./kirt/heap`, como dice R3 §2.4.
    - Delve con `@latest` contradice la política de versiones: hay que fijar la versión de `dlv` que soporte la toolchain.
    - En G8 y en la pregunta 1 del apartado 17, los «~0,7 ns por llamada» y el «30 %» son una estimación, no una medida. R3 §4 midió `rc++` sobre 10⁶ objetos en L3, no un contador por llamada.
    - En §8.5 capa 1, «La VM no lo necesita: su pila es datos» da la razón equivocada. La VM no necesita polls para la vida del proceso porque es código Go, que Go preempta de forma asíncrona. «Su pila es datos» es la razón de la preempción *a nivel de Ki*.
24. **R2 se equivoca en un detalle que el documento no hereda.** R2 §10.8 dice que en arm64 no existe `syscall.EpollCreate`. [V-t4]: compila con `GOARCH=arm64`, y `go doc` lo lista. El documento usa `EpollCreate1`, así que no le afecta, pero el capítulo 63 no debe repetirlo.
25. **La razón de «`-race` solo tiene sentido con el heap de depuración» está incompleta (§11.4, G20, riesgo 3).** La regla de CI está bien; su justificación no. Según R3 §2.5:
    - (1) `-race` **nunca instrumenta el ensamblador**, en ningún modo, tampoco con `kiheapgo`. Las carreras propias del nativo no tienen detector: el `dup`/`drop` local en línea (`INCL` plano) sobre un objeto mal clasificado como local, o las aristas de migración en código emitido.
    - (2) Sin `kiheapgo`, `-race` sobre `kirt` no es solo inútil: da **falsos positivos** cuando estado de Go se publica por atómicos del heap de Ki (R3 V3).
    - (3) `-race` sí tiene sentido sin `kiheapgo` para el compilador y para las estructuras de `kirt` del lado Go (deques de `atomic.Pointer[Fiber]`, mutex y colas de los canales, poller), que es lo que R2 P6 y P12 probaron.
    - *Corrección:* la regla `-race` siempre con `-tags kiheapgo` se queda, con (2) como razón. (1) entra como riesgo explícito (riesgo 3.1).

---

## 3. Riesgos que faltan en el apartado 13

1. **`-race` nunca ve el código nativo** (punto 25 del apartado 2): ningún detector cubre las carreras del código emitido por Ki. Mitigación: tests de tornasol y estrés sobre binarios nativos con 16 workers forzando migraciones, el modelo independiente del oráculo y la matriz a tres bandas. Es lo mismo que R4 §11.4 pedía.
2. **El prototipo del portador A llega tarde para un libro que se publica congelado.**
   - El riesgo 4 y §18 ponen el prototipo de punta a punta «al abrir el capítulo 53», hacia las 590 h.
   - Pero el capítulo 32 (S5 con `Async`/`IO` capaces de ceder), el 41 y los 42–46 (`kirt` compartido por la VM y el nativo) están construidos para A, y la historia de lo ya publicado está congelada (§11.2).
   - Si A falla en el 53, la corrección son fe de erratas a lo largo de las Partes V–VII, justo lo que el anti-objetivo «arquitectura … que haya que tirar» quiere evitar.
   - Mitigación: el prototipo mínimo de A en ensamblador (`kiResume`, burbujeo hasta la raíz, aparcar y despertar, crecimiento de pila y GC durante el burbujeo, un canal) se hace **antes de publicar el capítulo 32**, como tarea de la referencia, y entra como canario.
3. **La cola indirecta depende de la contabilidad interna del ensamblador** (E2): el `spfix` del bloque `morestack`. Va al riesgo 1 con el canario C13.
4. **La deriva de parches de la toolchain** (E5): la línea fijada ya va cinco parches por detrás, con correcciones de seguridad. Va al riesgo 19.
5. **Se pierde stdout en los fatal errors de Go** (punto 21 del apartado 2).
6. **Los programas de verificación de R1–R3 ya no existen** (punto 7 del apartado 2). Los canarios hay que reescribirlos desde los resúmenes, y un canario reescrito mal puede pasar en verde sin comprobar el hecho.

---

## 4. Horas

**Las cuentas cuadran.** Las he recalculado desde las tablas del apartado 12.5:

| Parte | Doc | Suma | | Parte | Doc | Suma |
|---|---|---|---|---|---|---|
| I | 78 | 78 | | VII | 68 | 68 |
| II | 102 | 102 | | VIII | 110 | 110 |
| III | 56 | 56 | | IX | 52 | 52 |
| IV | 28 | 28 | | X | 46 | 46 |
| V | 90 | 90 | | XI | 61 | 61 |
| VI | 85 | 85 | | **Total** | **776** | **776** |

- Los hitos acumulados son exactos: M1 78, M2 103, M3 152, M4 236, M5 264, M6 354, M7 439, M8 507, M9 558, M10 617, M11 669, M12 715, M13 750 y M14 776.
- El mayor tramo entre hitos es M5→M6, con 90 h; la reserva da 892 h; y el calendario, el «7 % menos» y la cifra de 3,6 meses para M1 a 5 h por semana son correctos.

**Lo que no sigue el razonamiento de R4 §7:**
- **El −16 h por la fusión y el −4 h de los efectos en la VM son cifras del diseño**, no de R4 ni de R2. R2 §10 propone la fusión sin dar horas. El documento las presenta «con el método de R4 §7».
- R4 subió los capítulos «donde Go pelea con el diseño». Los capítulos 53 (S5 nativo más el prototipo de A) y 54 (`kiResume` de punta a punta, la retoma del slot con E8 resuelto, señales, `FlightRecorder`, Delve/gdb) se quedan **justo en el tope de 16 h** y llevan el trabajo menos verificado del libro. Eso es el tope, no una estimación.
- **La barra de ±10 h de la Parte VIII era de R4 para otro mecanismo** (la F1 opción A: pilas propias de Ki), que ya no existe. El documento la mantiene para un diseño distinto y menos probado.
- *Corrección:*
  - Etiquetar −16 y −4 como «estimación del diseño».
  - Estimar el 53 y el 54 sin el tope y, si pasan de 16, partirlos (por ejemplo, «54a: portador A en un worker» y «54b: 16 workers, la retoma del slot y la matriz»), de modo que el tramo M9→M10 siga por debajo de 100 h.
  - Ensanchar la barra de la Parte VIII a ±15–20 h, o reducirla después del prototipo temprano del riesgo 3.2.
  - Sumar las horas de E7 (el oráculo con `Async` que burbujea y el backend `vm-bubble`, unas 3–5 h entre el 32 y el 41) y de E2 (la cola indirecta, unas 2 h en el 51).

---

## 5. Lo comprobado y correcto (selección)

Lo verifiqué hoy o lo contrasté con los informes, y el documento lo cuenta bien:

- **Ensamblador y runtime:**
  - el prólogo y el `R14 = g`;
  - `NO_LOCAL_POINTERS`;
  - los marcos `$F-0` sin prototipo (R1 n14);
  - `DX` en `NEEDCTXT`;
  - que no se puede cambiar de pila ni escribir `SP`;
  - que el ensamblador nunca se preempta de forma asíncrona y los números de latencia del GC;
  - que `RET ·f(SB)` es epílogo más `JMP`.
- **La convención de `#line`.** [V-t8]: `#line 7` atribuye la siguiente instrucción a la línea 6, y `#line 20` a la 19. La regla «escribir N+1» y el canario C9 son correctos.
- **Que el ensamblador rechaza una ruta de módulo con punto**, la razón de los trampolines. [V-t9]: `k_amd64.s:5: expected '(', found .` con `github.com∕aldezex∕kilang∕kirt·Alloc`.
- **El `go.mod` de §11.1.** [V-t10]: `go 1.26` más `toolchain go1.26.5` más `ignore ./build` funcionan con `GOTOOLCHAIN=local`, y un fichero roto bajo `build/` no rompe `go build ./...`.
- **`syscall`.**
  - [V-t0]–[V-t4]: Windows no tiene `VirtualAlloc`, `VirtualProtect`, `VirtualFree` (de ahí `NewLazyDLL`), `Mmap` ni `EpollCreate1`.
  - Windows sí tiene `CreateIoCompletionPort`, `GetQueuedCompletionStatus`, `PostQueuedCompletionStatus`, `WSARecv`, `CreateFileMapping` y `MapViewOfFile`.
  - linux/arm64 tiene `EpollCreate1/Ctl/Wait`, `Mmap`, `Mprotect`, `Madvise`, `SYS_EVENTFD2`, `SYS_FUTEX` y `Syscall6`, pero no `SYS_EVENTFD`, `MADV_FREE` ni `Eventfd`.
  - `EpollWait` usa `Syscall6` (`zsyscall_linux_amd64.go:1591`) [S].
- **Go 1.27** (go.dev/doc/go1.27, leído a través de un resumen automático con las frases citadas): trae métodos genéricos sobre tipos concretos (no en interfaces), el perfil de fugas de goroutines en GA (se borra el `GOEXPERIMENT`) y `synctest.Sleep`. Las fechas de 1.27.0–1.27.3 son correctas.
- **El entorno:** `GOAMD64=v1` y `CGO_ENABLED=1`; gdb, dlv, qemu y perf no están instalados.
- **Heap y RC:** la codificación de Lean, la «Regla 1», «nunca un puntero de Go en memoria de Ki», el heap `kiheapgo` con su accesor `//go:nocheckptr`, la ceguera de `-race` sobre el heap `mmap` y los modos `debug`/`fence`. Coincide con R3; aparte, ver los puntos 16, 17 y 22 del apartado 2.
- **Planificador:** la forma C11 de Chase–Lev con atómicos SC, los slots `atomic.Pointer`, workers sin `LockOSThread` y el protocolo de aparcado de `proc.go`. Coincide con R2.

---

## Apéndice A. Verificaciones ejecutadas hoy (Go 1.26.5, linux/amd64)

Todo se hizo en `…/scratchpad/techcheck/` (borrado al terminar). Solo se usó la caché de build de la toolchain.

| Id | Qué | Resultado |
|---|---|---|
| V-t0 | `go version`, `go env GOAMD64 CGO_ENABLED`, `which gdb dlv qemu-x86_64 qemu-aarch64 perf` | `go1.26.5`, `v1`, `1`; ninguna herramienta instalada |
| V-t1 | `cap(time.After(time.Second))`, `cap(time.NewTimer(..).C)`; `FormatFloat(x,'g',-1,64)` | `0`, `0`; `"1" "6" "0.1" "100000" "1e+06" "1.23456789e+08" "1e+21" "0.0001" "1e-05" "NaN" "+Inf" "-Inf" "0" "-0"` |
| V-t2 | `go test -c -gcflags=-m` con llamadas dentro de `for b.Loop()` (tres formas) | `inlining call to addFast`, `inc`, `addTagged` (+ `addFast` + `addSlow`) dentro del cuerpo |
| V-t3 | Dos hojas `$8-0` que se llaman en cola (`RET ·kiOdd(SB)` / `RET ·kiEven(SB)`) | `nosplit stack over 792 byte limit … infinite cycle`. Con un `CALL` inalcanzable: enlaza, prólogo presente, resultado correcto |
| V-t4 | `go doc syscall.X` con `GOOS=windows` y `GOARCH=arm64`; build cruzado de `Mmap` + `EpollCreate[1]` | Tabla del apartado 5; arm64 compila (`EpollCreate` incluido); windows: `undefined: syscall.Mmap` |
| V-t5 | `-d=ssa/check_bce/debug=1` sobre registros de la VM | `*[256]Value[uint8]`: 0 chequeos; `[uint16]`: 3; `regs[base+int(u16)]`: 3; `[uint8(u16)]`: 0 |
| V-t6 | Ejecutable que importa `net`, con y sin `CGO_ENABLED=0` | `dynamically linked, interpreter /lib64/ld-linux-x86-64.so.2` frente a `statically linked` |
| V-t7 | 4 goroutines aparcadas en su token `chan struct{}`, con y sin una goroutine en `EpollWait(-1)` | `fatal error: all goroutines are asleep - deadlock!`, salida 2; con poller: colgado (`timeout` → 124) |
| V-t8 | `#line 7 "x.ki"` / `#line 20 "x.ki"` en un `.s`; `go tool objdump` | Primera instrucción → `x.ki:6`; tras `#line 20` → `x.ki:19` |
| V-t9 | `CALL github.com∕aldezex∕kilang∕kirt·Alloc(SB)` desde `main` | `expected '(', found .` |
| V-t10 | `go.mod` con `go 1.26`, `toolchain go1.26.5`, `ignore ./build`; `//go:embed kirt stdlib docs/errors` | Falta un directorio: `pattern docs/errors: no matching files found`. Con todos: `go build ./...` ok y `build/` ignorado |
| V-t11 | Cola indirecta `ADJSP $-56; POPQ BP; JMP CX` entre dos funciones, 10⁸ saltos, `runtime.GC()` concurrente, `gccheckmark=1,invalidptr=1` | Sin compensación: `SIGSEGV … runtime.(*unwinder).next … scanstack`. Con `PUSHQ BP; ADJSP $56` inalcanzables tras el `JMP`: `result 0 in ~200ms`, StackInuse ≈ 700 KiB, salida 0, ×3 |
| web | go.dev/doc/devel/release, go.dev/doc/go1.27 (leídas a través de un resumen automático; líneas citadas por el resumen) | `go1.26.10 (released 2026-10-19)`; 1.27: métodos genéricos, perfil de fugas en GA, `synctest.Sleep`, canales de `time` siempre síncronos |
| fuente | `runtime/proc.go:6367-6470` (`checkdead`), `syscall/zsyscall_linux_amd64.go:1584-1597`, `os/file_unix.go:88-99,144-219`, `os/file.go:705-724`, `cmd/internal/obj/x86/obj6.go:815-857` | Citados en E2, E6 y en los puntos 13 y 24 del apartado 2 |
