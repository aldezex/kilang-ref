# Ki: hoja de ruta de los planes de implementación (edición Go)

**Spec:** `docs/superpowers/specs/2026-10-24-ki-design-go.md` (hasta la Tarea 0 del Plan 01 vive en `/mnt/c/Users/scami/Downloads/ki-diseno-go/2026-10-24-ki-design-go.md`). Aprobado por Álvaro el 2026-10-24 («No esperes mi aprobación, implementa todo»); sus decisiones están en los apartados 4 y 16 del spec.

El diseño cubre 71 capítulos (el 54 en dos mitades; los 62–64, el servidor de lenguaje, entraron el 2026-10-28 y los antiguos 62–68 son hoy 65–71), cinco secciones «Proyectos en Ki» al final de las Partes II, III, VII, X y XI, un compilador en Go con tres backends, un runtime concurrente propio (`kirt`) y un libro de unas 856 h de tronco (984 h con la reserva). No cabe en un plan ejecutable único, así que se divide en un plan por parte del libro, más uno de cierre. Cada plan:

- produce software que funciona y se prueba por sí solo: termina en el hito o hitos de su parte, con todas sus etiquetas verificadas;
- se escribe justo antes de ejecutarse, leyendo las interfaces reales del código en la última etiqueta del plan anterior, y no las imaginadas hoy;
- incluye la prosa en español de sus capítulos y sus tarjetas de test, termina con `bookx verify` limpio y deja cada capítulo publicado en el libro web.

## Planes

| Plan | Alcance | Horas | Hitos | Estado |
|---|---|---|---|---|
| 00 | Esta hoja de ruta | — | — | escrito |
| 01 | Infraestructura (repo, `go.mod`, CI, runner golden `ki test`, `bookx` con `web`, `check-tags`) y Parte I, capítulos 1–10; desde el 2026-10-28, la sesión `ch07-s4` (Tarea 32a) pone los diagnósticos al nivel de rustc y crea el catálogo `docs/errors` | 81 | M1 | escrito; revisado el 2026-10-24 («Registro de la revisión previa» al final del plan); enmendado el 2026-10-28 |
| 02 | Parte II, capítulos 11–19: datos, `Float` y `kirt/fmtnum`, `match`, polimorfismo, traits, closures, listas, HKT, módulos, `ki explain` y el color; los «Proyectos en Ki» de la parte (calculadora, juego de la vida, Markdown→HTML; 5 h) con la página `book/p02-proyectos.md` que `bookx` aprende a renderizar | 107 | M2, M3 | por escribir |
| 03 | Parte III, capítulos 20–24: filas de efectos (con la fila de `main` inferida y las operaciones de `IO` que aparcan tipadas `{IO, Async}`), handlers, generadores, azúcar imperativo, pereza; proyectos: parser de JSON con buenos errores y generador de sitios estáticos (5 h) | 61 | M4 | por escribir |
| 04 | Parte IV, capítulos 25–26: fibras, canales y `select` deterministas en el oráculo; `await` como palabra reservada con E0206, E0406 y E0407 | 28 | M5 | por escribir |
| 05 | Parte V, capítulos 27–34: ANF, árboles de decisión, lifting, Perceus, S5 (paso de evidencia; «puede ceder» = `Async`, efecto de usuario o variable de fila, nunca `IO` a solas), kigen y el reductor. Incluye el prototipo del portador A (canario C14) **antes de publicar el capítulo 32** | 92 | M6 | por escribir |
| 06 | Parte VI, capítulos 35–41: el runtime de Go como anfitrión, modelo de objetos, allocator sobre `mmap`, RC, MIR, VM y S5 en la VM (`vm-bubble`) | 87 | M7 | por escribir |
| 07 | Parte VII, capítulos 42–46: fibras de la VM, Chase–Lev, work stealing (workers que arrancan en el primer `spawn`), canales y `select`, epoll (poller, temporizadores y pool con arranque perezoso); proyectos: chat TCP, caché clave-valor en red, limitador de tasa (7 h) | 75 | M8 | por escribir |
| 08 | Parte VIII, capítulos 47–54b: ensamblador de Go, `ki build` (el módulo generado importa solo los subpaquetes de `kirt` que las filas piden: `direct.Run` sin `Async`, `sched.Run` con él; test con `go tool nm`), backend x86-64 completo, S5 nativo y fibras nativas (54a y 54b con etiquetas propias) | 124 | M9, M10 | por escribir |
| 09 | Parte IX, capítulos 55–58: optimizador (con la especialización de efectos por sitio de llamada), representaciones sin caja, linear scan II, pprof y PGO | 52 | M11 | por escribir |
| 10 | Parte X, capítulos 59–65: biblioteca estándar, paralelismo, programas reales, el servidor de lenguaje (62–64: protocolo y diagnósticos en vivo; hover, definición, referencias, autocompletado, símbolos y tokens semánticos; capacidad de respuesta, Neovim y Zed), `ki fmt`, `ki explain`, `ki lsp`, `kicheck` y release v0.1; proyectos: Mandelbrot o raytracer paralelo a PPM con la aceleración medida (5 h) | 81 | M12 | por escribir |
| 11 | Parte XI, capítulos 66–71: AArch64 y Windows x64, con `ki` mismo corriendo en ambos; proyectos: el mini-Lisp en Ki que cierra el libro (7 h) | 68 | M13, M14 | por escribir |
| 12 | Cierre del libro: epílogo (fuera del tronco), reconstrucción y verificación nocturna de todas las etiquetas, fe de erratas consolidada, revisión global de horas y ficha definitiva en la Wiki de artefactos | — | — | por escribir |

Las horas son las de las cabeceras de parte del índice del spec (apartado 12.5) y suman 856: las de los capítulos más las de las secciones «Proyectos en Ki», que no son filas del índice. Se revisan al cerrar cada plan.

## Reglas que valen para todos los planes

- **Publicación incremental.** Cada capítulo se publica en el libro web (artifact `https://claude.ai/artifact/F9s6xctJx1vRvagDt2aQUL`, privado, con `db` y `user`; siempre la misma URL) cuando su prosa está revisada y la CI de GitHub está en verde sobre su etiqueta `chNN`. El libro se republica con `bookx web` desde el controlador, con la herramienta Artifact, tras cada capítulo. Los capítulos pendientes aparecen en el índice marcados «En preparación».
- **Prosa y tarjetas en `main`, antes de `chNN`.** `book/chNN-<slug>.md` y `book/tests/chNN.tests` se escriben dentro del rango del capítulo (spec 11.1, G17). La etiqueta `chNN` va sobre el commit de la prosa, que es el último del rango; `m<N>` va sobre el mismo commit que la etiqueta del capítulo del hito. La primera sesión del capítulo siguiente no empieza hasta que `chNN` existe: así la historia sigue lineal y cada etiqueta de capítulo es autocontenida (código, tests, tarjetas y prosa).
- **Pushes.** Un push por capítulo, hecho por Álvaro antes de publicarlo (su decisión, spec apartado 4). El sistema de permisos bloquea `gh repo create` y `git push` a los agentes: cada plan escribe el comando exacto que hay que darle (`git -C ~/kilang-ref push origin main --tags`, con `--force` solo si se movieron etiquetas aún no publicadas) y sigue trabajando mientras tanto; la publicación de ese capítulo queda pendiente hasta que `git ls-remote origin refs/tags/chNN` lo devuelve y `gh run list --commit <sha>` está en verde.
- **Antes de escribir cada plan** se relee el spec de esa parte (índice, hitos, apartados técnicos) y el código de la última etiqueta. Si algo del spec resulta inviable, se cambia el spec en el mismo commit que el plan, y se avisa a Álvaro si cambia algo que eligió.
- **Diagnósticos al nivel de rustc (spec 7.3, decisión de Álvaro del 2026-10-28).** Todo código de diagnóstico nace completo en la sesión que lo introduce: primaria con texto, secundarias en el sitio relacionado, nota con la regla, ayuda solo con una acción concreta, fix-it cuando la corrección es mecánica y su entrada `docs/errors/EXXXX.md` con un ejemplo erróneo y otro corregido, los dos probados por `tests/errors_docs_test.go`. Cada plan lo escribe en la tarea que introduce el código, los revisores de cada sesión lo comprueban y el panel de cada capítulo también; ningún código se publica «para pulirlo después». La sesión `ch07-s4` del Plan 01 pone al nivel los códigos que ya existían.
- **«Proyectos en Ki».** Los planes 02, 03, 07, 10 y 11 llevan, tras la última sesión de código de su parte y antes de la prosa del último capítulo, una sesión más con los programas de la sección «Proyectos en Ki» del spec (goldens en `tests/projects/pNN/`) y la página `book/pNN-proyectos.md`; el Plan 02 enseña a `bookx` a renderizarla y verificarla (sin etiqueta propia: la página se publica con el último capítulo de la parte).
- **Correcciones.**
  - Antes de publicar un capítulo, un fallo que destapa una sesión posterior se corrige en su sitio: commits `--fixup`, `git -c sequence.editor=: rebase --autosquash --update-refs <base>` (no interactivo) y `ci/retag.sh` para mover las etiquetas afectadas; `ci/check-tags.sh` vuelve a verificarlas, y el push del capítulo lleva `--force` para esas etiquetas. Las correcciones de un mismo capítulo se agrupan en un lote por capítulo (una tanda de fixups, un rebase, un retag), nunca una por hallazgo.
  - Después de publicar, la historia del capítulo está congelada: la corrección va hacia delante, con una fe de erratas en el capítulo publicado (un commit que solo toca `book/`) y la explicación en el capítulo donde aparece. Si el fallo es didáctico, se queda como recuadro `:::bug` («Bug encontrado») en el capítulo que lo descubre.
- **Proceso de ejecución (artefactos que crea la Tarea 0 de cada plan y mantiene el controlador).**
  - El espacio de trabajo del plan en `.superpowers/sdd/<nombre-del-plan>/` (git-ignored): ledger `progress.md`, briefs, informes y paquetes de revisión.
  - Los ficheros de convenciones versionados en `docs/superpowers/process/`: `implementer.md`, `reviewer.md`, `prose.md`, que se adjuntan a cada despacho. Recogen las lecciones de la era C++ (tests que matan mutantes y cubren límites, comentarios que no prometen de más, tarjetas que no afirman más de lo que el test comprueba, un solo ayudante compartido por concepto, recursión acotada, CRLF y `\r` suelto, BOM y tamaño máximo, flush tras cada línea de progreso, diagnósticos ordenados, afirmaciones «inalcanzable» verificadas con un test).
  - El extractor de briefs `docs/superpowers/process/task-brief.sh`, porque los planes usan encabezados `### Tarea N` y el script `task-brief` de la skill solo reconoce `Task N`.
  - `ci/retag.sh` (mueve una etiqueta anotada conservando su mensaje) y `ci/retag-all.sh` (recoloca todas las etiquetas de sesión y de capítulo tras un rebase, buscando el commit por el prefijo de su asunto).
  - La prosa se escribe en un worktree propio bajo `.claude/worktrees/prose-chNN` (rama `prose/chNN`, creada desde la punta de `main` justo después del commit `book: verify chNN sessions`, que deja `book/tests/chNN.list` y `book/verified-tags.txt` listos; ver la plantilla del Plan 01) y entra en `main` con `git merge --ff-only`; nunca hay dos implementadores en el mismo checkout.
  - `ci/check-tags.sh`, que reconstruye cada etiqueta con `git archive` en `build/tags/<tag>`, ejecuta `ci/ci-local.sh` y anota las verificadas en `book/verified-tags.txt` (y, para las de capítulo, la lista de tests en `book/tests/chNN.list`).
  - `bookx` (`cmd/bookx`, solo en la referencia): regiones, tarjetas, directivas, `verify` (reglas 1–6), `render` y `web`.
- **Canarios y prototipos.** Los programas con los que R1–R3 verificaron sus hechos ya no existen: cada canario se reescribe en `docs/research/proto/` y `kirt/canary` antes del capítulo que lo usa, y su primera ejecución tiene que reproducir el número o el fallo que el informe cita. Calendario:
  - Plan 01 (Parte I): ninguno. Los hechos del toolchain que usa la Parte I se reverifican sobre Go 1.27.3 antes del capítulo 1 y quedan fijados a tests (`internal/toolchain`).
  - Plan 02: C10 (floats, con la pata arm64 estática) en el capítulo 11.
  - Plan 05: C14, el prototipo del portador A de punta a punta, antes de publicar el capítulo 32.
  - Plan 06: C7 y C8 en el capítulo 37; C11 en el 40.
  - Plan 07: C3 (latencia de `runtime.GC()` con `GOMAXPROCS ≥ 2`) en el 44.
  - Plan 08: C1, C2, C4 (con el par de hojas mutuas), C5, C6 y C13 en el capítulo 47; C9 en el 49.
  - Plan 09: C12 solo si `--kiabi=b` se activa (capítulo 58).
  - Plan 11: la pata arm64 de C10 pasa de estática a ejecutada en el 66.
- **Versión de Go.** Se parte de Go 1.27.3 (`go 1.27` + `toolchain go1.27.3` en `go.mod`, `GOTOOLCHAIN=local` en la CI y en `ki build`). Los parches menores se adoptan en cuanto salen, con los canarios en verde; una versión mayor solo al final de una parte, junto a la replanificación de horas. Cada cambio deja una línea en `docs/toolchain.md`. Nunca `GOEXPERIMENT` ni `golang.org/x/*`.
- **Horas.** Al cerrar cada plan se revisa si las horas del índice del spec (apartados 12.3–12.5) siguen siendo creíbles a la vista del código real. Si cambian, se actualiza el spec en el mismo commit.
- **Requisitos de máquina**, cada uno antes del plan que lo usa:
  - Plan 01, capítulo 10: Delve, sin `sudo`: `go install github.com/go-delve/delve/cmd/dlv@vX.Y.Z` a la última versión publicada que soporte go1.27 (se determina al ejecutar con `gh release list -R go-delve/delve --limit 5` y se anota en `docs/toolchain.md`; nunca `@latest`).
  - Plan 08, capítulo 47, opcional: `sudo apt install -y gdb`.
  - Plan 10, capítulo 64: Neovim (el editor de Álvaro; se comprueba con `nvim --version` al llegar al capítulo, y si falta lo instala él); Zed se instala solo si él quiere probar la extensión, y lo instala él.
  - Plan 11, capítulo 66: `sudo apt install -y qemu-user qemu-user-binfmt`.
  - Nada de `sysctl`, nada de toolchains cruzados, nada de perf (lo sustituyen `pprof` y `go tool trace`).
  - Lo que lleva `sudo` lo ejecuta Álvaro cuando toque; el plan correspondiente le da el comando.
- **Seguimiento.** Al cerrar cada plan se anota el progreso en la memoria del proyecto (`ki-nuevo-project.md`) y se le da a Álvaro un estado de una línea, sin enviarle capítulos. La ficha del libro en la Wiki de artefactos se actualiza al cerrar cada parte.
