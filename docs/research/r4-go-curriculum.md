# R4 — The curriculum: teaching Go from zero to expert while building Ki

**Date:** 2026-10-24 · **Toolchain checked:** `go version go1.26.5 linux/amd64` (GOROOT `~/.local/opt/go`), WSL2 kernel 6.18, 16 logical CPUs, gcc present, no clang/qemu/perf/delve.
**Scope:** how Go maps onto the 68 chapters and 11 parts of the C++-era index (spec §12.5 of `base-cpp-design.md`). Ki itself does not change.
**Depends on:** R1–R3 (runtime, native backend, ABI). Where the curriculum forks on their decisions, this file says so (§6). It does not decide them.

**Evidence labels used throughout**
- **[V]** verified on this machine in this session (transcript summary in §2);
- **[S]** read in the installed Go source tree (file:line, under `$GOROOT/src` or `$GOROOT/doc`);
- **[D]** documented on go.dev or another primary page (fetched this session unless marked *(not re-fetched)*);
- **[I]** my inference or proposal. These are the parts Álvaro has to approve.

---

## 1. Verdict in brief

- **The book keeps its shape.** It keeps 68 chapters, 11 parts, the same 14 milestones and the same compiler content.
- **The "C++ nuevo" column becomes "Go nuevo".**
  - The Go basics are front-loaded into Part I, because Álvaro starts from zero.
  - User-level concurrency (goroutines, channels, `select`) moves into Part IV, the part where Ki copies that model.
  - The Go runtime internals (G/M/P, GC, preemption, stack copying, `unsafe`) become a prerequisite block at the start of Part VI, chapter 34, in place of "the C++ memory model before the first atomic". The runtime chapters that follow are written against those constraints.
- **Estimated hours.** Trunk ≈ **780 h** (was 836 h), plausible range 760–815 h, ≈ **897 h** with the 15 % reserve. Where the hours change:
  - **They drop** where C++ mechanics dominated: CMake, own test framework, sanitizers, ODR/headers, RAII/move, templates, ELF/`ld`/crt, CPack, MinGW and Win64.
  - **They stay** for every algorithm chapter.
  - **They rise** where Go fights the design: an allocator in memory the GC cannot see, context switching under a runtime that scans and copies goroutine stacks, the Plan 9 assembler and ABI0, and native fibers under Go's signals and stop-the-world.
- **The biggest single saving is Windows (Part XI).** The same Plan 9 `.s` file links and runs on windows/amd64 unchanged, because Ki-emitted code talks ABI0 to Go, not Win64 **[V]**.
- **The biggest single risk is Part VIII (ch 52–54).** It depends on how R1/R2 put Ki fibers on stacks under the Go runtime (§6).

---

## 2. Verification log (what I ran, what came back)

All programs lived in `/tmp/claude-1000/-home-scami/144ea2a3-9681-4392-8a95-4f8de2e00caa/scratchpad/r4verify/`, deleted at the end. Two side effects outside that folder:
- Every `go build`/`go test` wrote to the Go build cache (`go env GOCACHE`). That is the toolchain's own cache, and I left it as is.
- The fuzz run (V20) created the fuzz-cache entry `$GOCACHE/fuzz/example.com/feat`, which I removed afterwards.

| # | Claim | How | Result |
|---|---|---|---|
| V1 | A hand-written `.s` builds with plain `go build` | `add.go` + `add_amd64.s` (ABI0, `TEXT ·add(SB), NOSPLIT, $0-24`), `go build -x` | Builds and prints `42 42`. `-x` shows the sequence `asm -gensymabis` → `compile -symabis` → `asm` → `pack` → `link`, and that `go_asm.h` is generated per package |
| V2 | The assembler inserts the stack-check prologue on non-`NOSPLIT` functions | `go tool objdump -s main.callsGo` on a `$16-16` frame | `MOVQ FS:0xfffffff8, R14; CMPQ SP, 0x10(R14); JBE → CALL runtime.morestack_noctxt.abi0`, plus an automatic `PUSHQ BP; MOVQ SP, BP` |
| V3 | User assembly cannot be ABIInternal | `TEXT ·reg<ABIInternal>(SB)` | `ABI selector only permitted when compiling runtime, reference was to "main.reg"`. Ki-emitted functions are ABI0, and a `CALL ·double(SB)` from asm resolved to `main.double.abi0` (a wrapper) |
| V4 | `NO_LOCAL_POINTERS` needs `#include "funcdata.h"` | first build without it | `unrecognized instruction "NO_LOCAL_POINTERS"`; fixed by the include |
| V5 | `go vet` asmdecl checks frame offsets | changed `b+8(FP)` to `b+16(FP)` | `[amd64] add: invalid offset b+16(FP); expected b+8(FP)` |
| V6 | `#line` in Go assembly maps code to Ki source | `#line 3 "fib.ki"` before `TEXT` | `objdump` attributes the instructions to `fib.ki:3`, `fib.ki:4` and `fib.ki:5`. This replaces GNU `.loc` |
| V7 | The same amd64 `.s` serves Windows | `GOOS=windows GOARCH=amd64 go build`, then ran `app.exe` through WSL interop | `PE32+ executable … x86-64`; prints `42 42`, exit 0 |
| V8 | Each architecture needs its own `.s` (build-suffix lesson) | `GOARCH=arm64 go build` with only `_amd64.s` | `missing function body`; with an `add_arm64.s`: builds, `go vet` passes, and the prologue is `MOVD 16(R28), R16; CMP R16, RSP; BLS` (g lives in R28) |
| V9 | Race detector on this kernel | racy `TestRace` with `wg.Go`, `go test -race` | `WARNING: DATA RACE`, test fails as expected; **no `vm.mmap_rnd_bits` change needed**. `CGO_ENABLED=0 go test -race` → `-race requires cgo` |
| V10 | `syscall` covers the runtime's needs (std lib only) | `go doc syscall.X`, also with `GOOS=windows` | Linux has `Mmap`, `Munmap`, `Madvise`, `Mprotect`, `MAP_NORESERVE`, `MADV_DONTNEED`, `EpollCreate1/Ctl/Wait`, `SYS_FUTEX`, `SYS_EVENTFD2`, `Syscall6`, `RawSyscall`, `SetNonblock`, `Gettid`, `SYS_TGKILL`. It has **no** `Eventfd` wrapper, **no** `MADV_FREE` constant and **no** `Sigaltstack`. Windows has `CreateIoCompletionPort`, `GetQueuedCompletionStatus`, `PostQueuedCompletionStatus`, `Overlapped`, `WSARecv`, `LoadDLL`, `NewLazyDLL`; it has **no** `VirtualAlloc` and **no** `CreateThread` (use `NewLazyDLL("kernel32.dll")`) |
| V11 | FMA contraction | `a*b+c` vs `float64(a*b)+c`, `-gcflags=-S` | `GOAMD64=v1` (the default here): `MULSD`+`ADDSD` for both. `GOAMD64=v3`: `VFMADD231SD` for the first. `GOARCH=arm64`: `FMADDD` for the first, `FMULD`+`FADDD` for the conversion form |
| V12 | Signed overflow wraps (defined) | `int64(math.MaxInt64)+1` | `-9223372036854775808`; `bits.Add64(MaxUint64,1,0)` → `0 1` |
| V13 | Exit codes | built binary | an unrecovered `panic` exits **2**; `os.Exit(101)` exits 101 **and skips deferred calls**; `go run` prints `exit status 101` but itself returns **1** |
| V14 | Go 1.26 language/library | one program | `new(int64(300))` works; `type Adder[A Adder[A]] interface{…}` compiles and runs; `errors.AsType[*fs.PathError]` works |
| V15 | `go mod init` default `go` line | `go mod init` in scratch | writes **`go 1.26.5`**. That contradicts the 1.26 release notes ("creates … `go 1.(N-1).0`"). The installed source `cmd/go/internal/modload/init.go:1214` calls `addGoStmt(…, gover.Local())`; the change was reverted in 1.26.1 (#77653, release-branch fix #77860). Sources: [golang-nuts thread](https://groups.google.com/g/golang-nuts/c/avgRf3vl9mo), [blog post](https://huizhou92.com/go-1-26-changed-go-mod-init-and-nobody-asked-for-it/) |
| V16 | Iterators, `iter.Pull`, `unique`, `weak` | tests | pass: `iter.Pull2` over a push iterator; `unique.Make` handles are equal for equal strings; `weak.Make(p).Value()==p` while `p` is alive |
| V17 | `testing/synctest` (GA 1.25) | `time.Sleep(time.Hour)` in a bubble | passes; `time.Since(start)` is exactly `1h0m0s` of virtual time, returned instantly |
| V18 | `os.Root` (1.24) | `r.Open("../../etc/passwd")` | refused: `openat ../../etc/passwd: path escapes from parent` |
| V19 | `T.ArtifactDir` + `-artifacts` (1.26) | `go test -artifacts -outputdir=out` | `=== ARTIFACTS TestArtifact …/out/_artifacts/<n>`, file kept. **Gotcha:** `-outputdir` must already exist (`mkdir … no such file or directory` otherwise) |
| V20 | Native fuzzing | `FuzzLex`, `-fuzztime 5s` | ≈ 827 k execs on 16 workers, 55 interesting inputs, PASS |
| V21 | `B.Loop` (1.24) | `BenchmarkLex` | `1310 ns/op 4032 B/op 6 allocs/op` |
| V22 | Escape analysis | `-gcflags=-m` | `moved to heap: n` for `return &n`; `regs does not escape` |
| V23 | Bounds-check elimination | `-d=ssa/check_bce/debug=1` | only `regs[ops[i]]` keeps `IsInBounds`; `ops[i]` in `range ops` and `regs[op]` with `regs *[256]int64`, `op uint8` have none |
| V24 | Dense `switch` → jump table | 9-case `switch op` | `LEAQ …Dispatch.jump3(SB)`, a 72-byte `SRODATA` table (9 × 8 bytes) |
| V25 | `encoding/json/v2` still experimental in 1.26.5 | `go doc encoding/json/v2` with and without `GOEXPERIMENT=jsonv2` | without: `cannot find package`; with: package docs |
| V26 | Swiss-table opt-out gone in 1.26 | `GOEXPERIMENT=noswissmap go build` | `unknown GOEXPERIMENT swissmap`; `flags.go` has no SwissMap field. Introduced in 1.24 [D] |
| V27 | Defaults in 1.26 | `internal/buildcfg/exp.go:83-89` [S] | baseline enables `RandomizedHeapBase64` and `GreenTeaGC`; experiments still listed: `JSONv2`, `GoroutineLeakProfile`, `SIMD`, `RuntimeSecret`, `RuntimeFreegc`, `SizeSpecializedMalloc` (`internal/goexperiment/flags.go:95-131`) |
| V28 | Loop variables are per-iteration (1.22) | closures over `for i := range 3` | `0 1 2` |
| V29 | Map order is randomized | 20 iterations over an 8-key map | 8 distinct orders |
| V30 | `noCopy` + vet copylocks as "move-only" | `Cont` struct holding `noCopy` passed by value | `resume passes lock by value: … Cont contains … noCopy` |
| V31 | GOMAXPROCS / tools | runtime + `go tool -n` | `GOMAXPROCS: 16 NumCPU: 16`; `go tool pprof` and `go tool trace` present; `go help build` documents `-pgo=auto` → `default.pgo` |

---

## 3. Go from zero to expert: the topic list by level

The **Ch.** column says where each topic is taught first (later reinforcements in brackets). Version tags are the Go release that introduced the feature, checked against the release notes or `$GOROOT/api/go1.N.txt`.

### 3.1 Basics (Parts I–II)

| Topic | What exactly | Ch. | Source / status |
|---|---|---|---|
| Toolchain and modules | `go mod init`; `go.mod`/`go.sum`; the `go` and `toolchain` lines; `GOTOOLCHAIN=auto`; `GOPROXY`/`GOSUMDB` from `$GOROOT/go.env`; `go build/run/test/vet/fmt/env/doc/list` | 1 | [V] go.env shows `proxy.golang.org`, `sum.golang.org`; [V15] `go mod init` writes the local version |
| Packages | one directory per package; exported = capitalized; `cmd/` + `internal/` layout; `internal/` import rule | 1 [18, 47] | [D] go.dev/doc/modules/layout *(not re-fetched)* |
| Types | basic types, untyped constants, `iota`; defined types vs aliases; conversions (never implicit); zero values | 1 [3] | spec `$GOROOT/doc/go_spec.html` |
| Control flow | the three `for` forms; `range` over int (1.22); `switch` without fall-through; labeled `break`/`continue`; `goto` | 1 [23] | spec |
| Structs and methods | value vs pointer receivers, method sets, copies on assignment | 2 [8] | spec |
| Slices and their internals | array + len + cap; `append` growth and **aliasing**; `copy`; full slice expression `a[i:j:k]`; the `slices` package | 2 [12] | spec; `runtime/slice.go` |
| Maps | reference semantics; comparable keys and struct keys; `clear` (1.21); **randomized iteration**; Swiss-table implementation (1.24) | 7 [8] | [V29], [V26]; [D] go.dev/blog/swisstable *(not re-fetched)* |
| Strings, bytes, runes, UTF-8 | immutable byte strings; `range` decodes runes; `unicode/utf8`; `strings.Builder`; `strconv` | 5 [16] | spec; `unicode/utf8` |
| Interfaces | implicit satisfaction; type assertions and type switches; `any`; **typed nil ≠ nil**; sealed interfaces through an unexported method | 3 [14] | spec |
| Errors | the `error` interface; sentinels; `%w`; `errors.Is`/`As`/`Join` (1.20)/`AsType` (1.26) | 3 [19] | [V14]; api/go1.26.txt `errors, func AsType` #51945 |
| `defer`/`panic`/`recover` | LIFO defers; `recover` only inside a deferred call; a panic in any goroutine kills the process; exit code 2; `os.Exit` skips defers | 10 [3] | [V13] |
| `fmt` | verbs, `Stringer`, `Formatter`, `%v` vs `%+v` vs `%#v` | 3 | `fmt` docs |

### 3.2 Intermediate (Parts I–V)

| Topic | What exactly | Ch. | Source / status |
|---|---|---|---|
| Generics | type parameters; constraints and type sets (`~uint32`); `comparable`, `cmp.Ordered`; inference; no generic methods; generic type aliases (1.24); self-referential constraints (1.26); implementation by GC-shape stenciling + dictionaries | 4 [13, 17] | [V14]; [D] 1.24 and 1.26 notes; design doc `generics-implementation-dictionaries-go1.18.md` *(not re-fetched)* |
| Iterators | `iter.Seq`/`Seq2`, range-over-func (1.23); `iter.Pull` (built on a runtime coroutine); `slices.All/Values/Collect/Sorted`; `maps.Keys`; `strings.Lines/SplitSeq` (1.24) | 7 [9, 22] | [V16]; [S] `iter/iter.go:231-237,261`, `runtime/coro.go:42,91` |
| Closures | func values; captured variables escape; method values and expressions; per-iteration loop variables (1.22) | 15 [13] | [V28] |
| Embedding | struct and interface embedding; promotion is not inheritance | 8 [14] | spec |
| Testing | `TestXxx`; subtests; table tests; `t.Helper`/`Cleanup`/`TempDir`; `t.Context`/`t.Chdir` (1.24); golden files + `-update`; `testdata/`; examples with `// Output:`; `-run`/`-count`/`-shuffle`/`-json`/`-cover`; `T.Attr`/`Output` (1.25); `T.ArtifactDir` + `-artifacts` (1.26); `testing/fstest` | 1 [3, 4, 33] | [V19]; api/go1.24–1.26.txt |
| Fuzzing | `go test -fuzz`; `f.Add` seed corpus; `testdata/fuzz/FuzzX/`; coverage-guided | 5 [6, 32, 39] | [V20]; [D] go.dev/doc/security/fuzz *(not re-fetched)* |
| Benchmarks | `b.Loop` (1.24; since 1.26 it no longer prevents inlining in the loop body); `-benchmem`; `-cpuprofile` | 4 [36, 39, 58] | [V21]; [D] 1.24 and 1.26 notes |
| `go vet` | printf; copylocks (the `noCopy` trick); `tests` (1.24); `stdversion` (1.23); `waitgroup` and `hostport` (1.25); asmdecl; unsafeptr | 1 [21, 25, 35, 46] | [V5], [V30]; [D] notes |
| Race detector | `-race`; needs cgo; also turns on `checkptr` | 34 [42, 44] | [V9]; [S] `cmd/compile/internal/base/flag.go:352-354` ("-race, -msan and -asan imply -d=checkptr") |
| `context` | cancellation, deadlines, values (sparingly); `exec.CommandContext`; `signal.NotifyContext` (1.26 adds the cause) | 33 [45, 61] | [D] 1.26 notes |
| `log/slog` | handlers, levels, `JSONHandler`, `GroupAttrs` (1.25), `NewMultiHandler` (1.26) | 33 [61] | api/go1.25–1.26.txt |
| `embed` | `//go:embed`, `embed.FS` | 16 [19, 47] | std docs |
| `go generate` | `//go:generate go run ./internal/gen/...`; hand-written generators (stringer is `x/tools`, so not allowed) | 5 [19, 39] | std docs; [I] the rule |
| I/O | `io.Reader`/`Writer`, `bufio`, `io/fs`, `os.DirFS`, `os.Root` (1.24; more methods in 1.25) | 2 [18] | [V18] |
| `unique`, `weak`, `runtime.AddCleanup` | interning; weak pointers; cleanups instead of `SetFinalizer` | 4 [31, 45] | [V16]; api/go1.23–1.24.txt |

### 3.3 Advanced (Parts IV–IX)

| Topic | What exactly | Ch. | Source / status |
|---|---|---|---|
| Goroutines, channels, `select` | unbuffered/buffered; `close`; receive from closed; send on closed panics; nil channels in `select`; `default`; `time.After`; timers (unbuffered timer channels since 1.23) | 25–26 [44] | spec; [D] 1.23 notes |
| `sync` | `Mutex`, `RWMutex`, `Cond`, `Once`, `OnceValue` (1.21), `WaitGroup.Go` (1.25), `Pool`, `Map` | 25 [24, 40, 44] | [V9] uses `wg.Go` |
| `sync/atomic` | typed atomics (`Int64`, `Pointer[T]`); `And`/`Or` (1.23); **sequentially consistent only**, no relaxed or acquire/release | 34 [37, 42] | [S] `doc/go_mem.html:604-608` ("same semantics as C++'s sequentially consistent atomics") |
| The Go memory model | DRF-SC (`go_mem.html:59-62`); happens-before; races on multiword values (interfaces, slices, strings) "can in turn lead to arbitrary memory corruption" (`:271`) | 34 | [S]; [D] research.swtch.com/gomm *(not re-fetched)* |
| `unsafe` | the six valid `Pointer` patterns (`unsafe/unsafe.go:39-158`); `unsafe.Add/Slice/String/StringData/SliceData`; `uintptr` is not a reference; `runtime.KeepAlive`; `checkptr`; `structs.HostLayout` (1.23) | 35 [36, 37, 51] | [S] |
| Escape analysis | `-gcflags=-m`, `-m=2` | 2 [20, 39] | [V22] |
| Inlining | cost budget; `-m=2` reasons; PGO-driven hot inlining | 39 [58] | [V22]; [D] go.dev/doc/pgo *(not re-fetched)* |
| Bounds-check elimination | `-d=ssa/check_bce/debug=1`; the `[256]T` indexed by `uint8` trick; hoisted length checks | 39 | [V23] |
| GC | concurrent tri-colour mark-sweep; write barriers; `GOGC`; `GOMEMLIMIT` (1.19); Green Tea (experiment in 1.25, **default in 1.26**, opt-out `nogreenteagc`); `GODEBUG=gctrace=1` | 30 [34, 36, 58] | [V27]; [D] 1.25 and 1.26 notes; go.dev/doc/gc-guide *(not re-fetched)* |
| Profiling and tracing | `pprof` CPU/heap/allocs/mutex/block/goroutine; `net/http/pprof`; `pprof -http` opens on the flame graph (1.26); `go tool trace`; `runtime/trace.FlightRecorder` (1.25); goroutine-leak profile (experiment, 1.26) | 39 [43, 44, 54, 58, 60, 61] | [V31]; [D] notes |
| PGO | `default.pgo` in the main package; `-pgo=auto` default | 58 | [V31] |
| The runtime scheduler | G/M/P (`runtime2.go:473,618,772`); `runnext` (`:818`); `findRunnable` (`proc.go:3389`, the 1-in-61 global-queue check at `:3443`); `runqgrab`/`runqsteal` (`:7662`, `:7730`); `gopark` with `unlockf` (`:445`); `sysmon` (`:6486`) and `forcePreemptNS = 10ms` (`:6628`); `entersyscall` (`:4737`); `LockOSThread` (`:5623`) | 34 [41–45] | [S] |
| Stacks and preemption | goroutine stacks grow by **copying** (`stack.go:900` `copystack`, `:1026` `newstack`); `stackguard0` (`runtime2.go:482`) and `stackPreempt` (`stack.go:133`); async preemption **never stops assembly functions** (`preempt.go:450-458`) | 34 [52, 54] | [S], [V2] |
| Go assembly and the ABI | Plan 9 syntax; pseudo-registers `FP`/`SB`/`SP`/`PC`; `TEXT` flags (`textflag.h`); `$frame-args`; `go_asm.h`; `funcdata.h`/`NO_LOCAL_POINTERS`; ABI0 vs ABIInternal and wrappers; register roles; `go vet` asmdecl; `#line`; `go tool asm/objdump/nm`; `-gcflags=-S` | 46 [47–54, 63–66] | [V1–V8]; [S] `doc/asm.html:327,407-413,488,607`; `cmd/compile/abi-internal.md:12-16,221,398,410-415,422-424` and its arm64 table (`R28` = g, `R26` = closure context) |
| Cross-compilation | `GOOS`/`GOARCH`/`GOAMD64`/`GOARM64` (1.23) | 47 [63, 66] | [V7], [V8] |
| Linker and build flags | `-ldflags=-X`, `-s -w`, `-B` (GNU build ID by default since 1.24), `-linkmode`; `-trimpath`; `-x`/`-work`; `//go:linkname` restrictions (1.23) | 47 [62] | [V1]; [D] 1.23 and 1.24 notes |
| Build constraints | `//go:build`; `_amd64.s`/`_arm64.s`/`_windows.go` suffixes | 47 [63, 66] | [V8] |
| `syscall` (frozen) | mmap/madvise/mprotect; epoll; raw futex/eventfd; Windows `LazyDLL` and IOCP | 36 [43, 45, 67, 68] | [V10] |
| `go/ast`, `go/parser`, `go/types` | the standard library's own Go front end, used to write small project-specific checks | 62 | std docs; [I] use |
| SIMD (experimental) | `simd/archsimd` with `GOEXPERIMENT=simd`, amd64 only, unstable API | 56 | [D] 1.26 notes; [V27] flag listed |

### 3.4 Professional practice (one or more boxes per part, see §8)

| Topic | Material | Where |
|---|---|---|
| Style | `gofmt` (non-negotiable); Effective Go (go.dev/doc/effective_go); Go Code Review Comments and Go Test Comments (go.dev/wiki); **Google Go Style Guide**, three documents: *Guide* (normative and canonical), *Decisions* (normative), *Best Practices* (not normative) [D, fetched]; **Uber Go Style Guide** (github.com/uber-go/guide) *(not re-fetched)* | 1, 6, 18, 62 |
| API design | small interfaces, "accept interfaces, return structs", useful zero values, errors as part of the API, functional options; the Go 1 compatibility promise (go.dev/doc/go1compat) and GODEBUG (`$GOROOT/doc/godebug.md`); semantic import versioning | 9, 18, 38, 59, 62 |
| Package layout | go.dev/doc/modules/layout; `cmd/`, `internal/`; no `pkg/` or `util/` grab-bags | 1, 18, 47 |
| Code review | how the Go project reviews (Gerrit, readability); the reference repo diff as a reviewer | 62, and every part |
| Production profiling | `net/http/pprof` behind auth; continuous profiling; `GOMEMLIMIT` and container-aware `GOMAXPROCS` (1.25); FlightRecorder | 58, 60, 61 |
| Large-codebase practice (Kubernetes, etcd) | generated code (`go generate`); `-race` in every unit-test CI job; structured logging; API versioning; linearizability testing for distributed stores | Parts VII and X boxes. These are **[I]**, from memory, not fetched this session |
| Supply chain | `go.sum` + checksum DB; reproducible builds; VCS stamping (1.24); `go version -m [-json]` (1.25); govulncheck (a Go-team tool from `x/vuln`, a tool not a library) | 62 |
| Modern toolchain | `tool` directive (1.24); `ignore` directive (1.25); `go fix` modernizers and `//go:fix inline` (1.26); `go doc -http` (1.25) | 1, 62 |

### 3.5 New in Go 1.23–1.26: where each lands

| Version | Feature | Status in 1.26.5 | Ch. |
|---|---|---|---|
| 1.23 | range-over-func, `iter`, `slices`/`maps` iterators | GA [V16] | 7, 9, 22 |
| 1.23 | `unique` | GA [V16] | 4 |
| 1.23 | `structs.HostLayout` | GA (api file) | 35 |
| 1.23 | `runtime/debug.SetCrashOutput` | GA (api file) | 54 |
| 1.23 | `//go:linkname` pull restrictions | linker rule [D] | 47 |
| 1.23 | unbuffered timer channels; unreferenced timers collectable | GA, needs `go ≥ 1.23` in `go.mod` [D]; old mode removed in 1.27 [D 1.26 notes] | 26, 45 |
| 1.24 | generic type aliases | GA [D] | 17, 18 |
| 1.24 | `testing.B.Loop` | GA [V21] | 4, 36 |
| 1.24 | `os.Root` | GA [V18]; more methods in 1.25 | 18 |
| 1.24 | `weak`, `runtime.AddCleanup` | GA [V16] | 31, 45 |
| 1.24 | Swiss-table maps | default; opt-out removed by 1.26 [V26] | 7 |
| 1.24 | `t.Context`, `t.Chdir`; vet `tests` analyzer; `tool` directive | GA | 1, 33 |
| 1.25 | `testing/synctest` (`Test`, `Wait`) | GA [V17] | 26, 45 |
| 1.25 | `sync.WaitGroup.Go` | GA [V9] | 25 |
| 1.25 | `runtime/trace.FlightRecorder` | GA | 54, 61 |
| 1.25 | container-aware `GOMAXPROCS` | default [D] | 43, 60 |
| 1.25 | `encoding/json/v2` | **still experimental** in 1.26.5 (`GOEXPERIMENT=jsonv2`) [V25] | 19 (mention; do not use) |
| 1.25 | vet `waitgroup`, `hostport`; `go.mod` `ignore`; `go doc -http`; DWARF 5 | GA | 1, 25, 62 |
| 1.25 | "core types" removed from the spec | spec prose [D] | 17 (box) |
| 1.26 | `new(expr)`; self-referential generic constraints | GA [V14] | 11, 17 |
| 1.26 | Green Tea GC default; heap base randomization | default [V27] | 30, 36 |
| 1.26 | `errors.AsType`, `slog.NewMultiHandler`, `T.ArtifactDir`, `bytes.Buffer.Peek`, reflect iterators | GA (api file) [V14], [V19] | 3, 33, 61 |
| 1.26 | `go fix` modernizers; `pprof -http` opens on the flame graph | GA [D] | 58, 62 |
| 1.26 | goroutine-leak profile | experiment (`goroutineleakprofile`) [V27] | 44, 61 |
| 1.26 | `simd/archsimd` | experiment (`simd`), amd64 only [D] | 56 |
| 1.26 | `go mod init` writes `go 1.(N-1).0` | **reverted in 1.26.1**; 1.26.5 writes `go 1.26.5` [V15] | 1 (box: "docs and toolchain can disagree; check") |

---

## 4. What disappears, shrinks or changes from the C++ book

| C++ topic (chapters) | Fate | Go replacement | Evidence |
|---|---|---|---|
| CMake targets, presets, toolchain files (1, 18, 47, 62, 63) | disappears | `go` command, `go.mod`, `GOFLAGS`, build constraints | — |
| Own framework `ki_test` (1); the "write the test framework yourself" rule existed because GoogleTest and Catch2 are third-party | disappears **[I]** | `testing` is the standard library. The golden runner, the `.tests` card format, kigen and bookx stay hand-written | §3.2 |
| ASan, UBSan (1, 2, 36) | mostly disappears | memory safety + defined overflow [V12]; `checkptr` for `unsafe` [S]; Ki's own `KI_ALLOC=debug` stays, because no Go tool sees an mmap'd heap **[I]** | — |
| TSan, `mmap_rnd_bits`, fiber annotations (34, 42, 44, 52) | replaced | `-race` (works on this kernel without sysctl, needs cgo) [V9]. It does not understand Ki-emitted asm or Ki-owned stacks, so litmus tests and seed sweeps stay the main guard **[I]** | V9 |
| Translation units, headers, ODR, `inline` variables (1, 18) | disappears | packages; no headers; import cycles are a compile error; package initialization order is defined in the spec | spec |
| RAII, destructors, move, rule of zero, `unique_ptr` (2, 21, 40) | disappears | GC + `defer` + explicit `Close`. One-shot ownership is enforced by discipline, a consumed flag and vet copylocks on a `noCopy` field [V30] | V30 |
| Templates, concepts, variadics, fold expressions, template template parameters (4, 13, 17) | shrinks | generics with constraints. Go has **no HKT** and no variadic type parameters, which is now a feature of the lesson: Ki's typechecker does what Go's cannot, through dictionaries | V14 |
| `std::variant` + `visit` + `overloaded` (3, 6, 9) | changes | sealed interface + type switch, or tagged structs. Go has no exhaustiveness check (the `exhaustive` linter is third-party), so impossible cases get a `default: panic` | — |
| `std::expected`, `[[nodiscard]]` (3) | changes | `(T, error)`; vet and review culture; `errors.Is/As/AsType` | V14 |
| UB of signed overflow, `__builtin_*_overflow` (10) | changes | defined wrapping (spec `go_spec.html:5030`); explicit checks and `math/bits` | V12 |
| `-ffp-contract=off` (11, decision 36) | changes | no flag exists. The spec allows fusion (`go_spec.html:5050-5057`), and an explicit `float64(…)` conversion prevents it. amd64 v1 (the default) does not fuse; **arm64 fuses by default** | V11 |
| `extern "C"`, name mangling, `-fno-exceptions`/`-fno-rtti`, strict aliasing (35, 46) | changes | ABI0 at the asm boundary; `unsafe` rules; no exceptions to switch off | V3 |
| ELF, `ld`, crt files, link line captured with `g++ -###`, `nm -u` (47) | shrinks a lot | `go build` is the linker driver: `asm` → `compile` → `pack` → `link`; build cache; `go version -m` | V1 |
| GNU as with `.intel_syntax` (46) | changes | the Plan 9 assembler (its own operand order, pseudo-registers, an automatic prologue) | V1–V8 |
| `.loc` and CFI directives (50) | changes | `#line N "file.ki"` in the `.s` [V6]; Go's pcln/pcsp tables replace CFI for Go's own tracebacks | V6 |
| `__start_ki_frametable` section symbols (50) | changes **[I]** | `DATA`/`GLOBL` symbols referenced from a Go `init` (needs prototyping in R2) | — |
| No `thread_local` because GCC folds `&tls` (35, 41, decision 32) | same decision, new reason | Go has no goroutine-local storage at all; explicit `ctx` everywhere | — |
| `futex` syscall (43) | optional | raw `syscall.Syscall6(SYS_FUTEX…)` or channel / `sync.Cond` parking | V10 |
| `sigaltstack`, async-signal safety (54) | disappears | the Go runtime owns signals: `os/signal`, `GOTRACEBACK`, `debug.SetCrashOutput` | V10 (no `Sigaltstack`) |
| Computed goto, `musttail` (58) | disappears | dense `switch` → jump table [V24]; function tables; closure-threaded code; measure them | V24 |
| perf built from the WSL kernel; `-fprofile-use` (58) | shrinks | `pprof`, `go tool trace`, PGO; perf optional (Go keeps frame pointers on amd64) | V31 |
| CMake install, CPack (62) | changes | `go install`, cross-compiled release archives, `-ldflags -X`, `debug.ReadBuildInfo` | — |
| aarch64 binutils/g++ cross toolchains (63) | disappears | `GOARCH=arm64` (qemu-user still needed to run) | V8 |
| MinGW binutils/g++; Win64 ABI and PE/COFF in depth (66) | shrinks a lot | `GOOS=windows`; the same ABI0 `.s` [V7]; Win64 rules apply only inside Go's own syscall layer | V7 |
| `std::jthread`, `stop_token` (41, 43) | changes | goroutines, `context`, `LockOSThread` | — |
| `std::mutex`, `lock_guard`, `scoped_lock` (44) | changes | `sync.Mutex` + `defer mu.Unlock()`; manual lock ordering | — |
| `memory_order` (34, 37, 42) | changes | only SC atomics in Go [S]. `acq_rel`/relaxed survive as theory, and in arm64 Ki-emitted code (ch 64) | go_mem.html:604-608 |
| `alignas(64)`, `hardware_destructive_interference_size` (42) | changes | manual padding `_ [64]byte` (`x/sys/cpu.CacheLinePad` is not std lib) | — |

**New Go-specific constraints with no C++ counterpart.** The book has to teach them where they first bite:
- Ki objects live **outside** the Go heap and never hold Go pointers. The GC must not see or scan them, and `uintptr`-held addresses don't keep Go objects alive. Taught in ch 35–36.
- Assembly is never async-preempted (`preempt.go:450-458`). A Ki loop without calls would therefore stall Go's stop-the-world phases, not only Ki's fairness. Back-edge polls become mandatory (ch 34 theory, ch 54 practice) **[S + I]**.
- A GC stack scan that meets an SP-writing function as anything but the innermost frame throws `traceback: unexpected SPWRITE function` (`traceback.go:338-366`; flag defined in `internal/abi/symtab.go:49-56`). This is the core of the ch 52 fork **[S]**.
- `go run` masks exit codes [V13]. The golden runner must exec built binaries (ch 1 and 10).

---

## 5. Curriculum principles for the Go book (proposals) [I]

1. **Basics are front-loaded and never "nothing new".** Part I carries the whole basics tier (§3.1) except maps (ch 7) and embedding (ch 8). By M1 (≈ 78 h) Álvaro writes idiomatic sequential Go with tests.
2. **The "Si vienes de…" boxes become "Si vienes de Rust / TS / Python".** Go is now the language being learned. The Rust box carries the most weight: ownership vs GC, traits vs interfaces, `Result` vs `error`, enums vs sealed interfaces.
3. **User-level concurrency goes in Part IV, runtime internals in Part VI.**
   - Ki's semantics are Go's, so ch 25–26 teach goroutines, channels and `select` as *the model being copied*.
   - Ch 34 replaces "C++ memory model before the first atomic" with "Go memory model **and the runtime that hosts kirt**".
   - The "theory before its first use" rule (decision 7) carries over.
4. **Read the real runtime.** Ki's scheduler and channels were designed after Go's (`runnext`, the 1-in-61 check, `gopark`/`unlockf`, `selectgo`, `stackguard0`). In Go the reader can open the same files, in the same language. Ch 41–45 include guided readings with file:line pointers (§3.3).
5. **The "everything hand-written" rule, restated.**
   - Inside Ki: only the Go standard library. That excludes `golang.org/x/*` (`x/sys/unix`, `x/tools`, `x/exp`, `x/sync`, `x/text`) and every third-party module.
   - Tools, as gdb/perf/QEMU were before: the `go` toolchain and its `go tool` subcommands, gdb, qemu-user, GitHub Actions. Delve, staticcheck and govulncheck are optional tools that never enter `go.mod`.
   - **Needs Álvaro's approval:** in particular whether `testing` counts as "std lib, use it". I recommend yes; that is what employers expect.
6. **The test card format keeps its shape.** Example: `kind: unit (go test) file: internal/syntax/lexer_test.go ref: TestLexer/pipe_continues_line`. Golden files live in each package's `testdata/`, and `-update` regenerates them.
7. **Repository sketch, which matters for ch 1** **[I]**:
   - `go.mod` (`module github.com/aldezex/kilang`) and `cmd/ki`.
   - `internal/{base,diag,syntax,sema,core,interp,mir,vm,x64,driver}`.
   - `kirt/`, which is **not** under `internal/`: native executables are separate generated modules that import it, and the `internal/` rule would forbid that.
   - `stdlib/*.ki`, `cmd/kigen`, `cmd/bookx`, `docs/`, `book/` (reference repo only).
   - `go.mod` `ignore ./build` so extracted tag trees stay out of `./...`.
8. **CI from ch 1.**
   - `gofmt -l` must be empty; `go vet ./...`; `go test ./...` both plain and with `-race`.
   - A **cross-compile check** of `ki` for `windows/amd64` and `linux/arm64`. It costs seconds and catches portability drift long before Part XI **[I]**.
9. **The float rule (decision 36 restated).** Every float primitive in the interpreter, the VM and kirt is written with one operation per expression, or with explicit `float64()` conversions. A small `go/ast` checker in `cmd/kicheck` enforces it from ch 62. Until then a unit test compares `GOAMD64=v3` and `GOARCH=arm64` builds **[I, motivated by V11]**.

---

## 6. Design forks this curriculum depends on (for R1–R3, not decided here)

| Fork | Option A | Option B | Curriculum effect |
|---|---|---|---|
| **F1: where native Ki fibers run** | Ki fibers run on Ki-owned mmap'd stacks inside worker goroutines locked to threads. Ki functions are `NOSPLIT` with their own prologue check, because the auto-inserted check compares SP with the *goroutine's* `stackguard0` [V2]. Every call into Go passes a trampoline that returns to the goroutine stack, so no GC scan ever meets a non-innermost SPWRITE frame [S traceback.go:338-366] | Ki native code runs on goroutine stacks and uses Go's own stack-split prologue [V2]. Fibers map onto goroutines (one-shot continuations by goroutine handoff, as `iter.Pull` does with `runtime/coro.go`). **This weakens Álvaro's decision "you write the work-stealing scheduler"** for the native backend | ch 52–54 (and 41, 64, 67) are written very differently. Hours in §7 assume A, which keeps his decision; B would cut ch 52 to ≈ 10 h but needs his approval |
| **F2: the Ki heap** | off the Go heap (`syscall.Mmap`), Ki values are `uint64` words, never Go pointers | Go heap slabs (`[]uint64` kept alive by a table) | A keeps ch 36 as "your own allocator" (+2 h for `unsafe`); B makes ch 36 smaller but the GC scans and pays for it. A is assumed |
| **F3: KiCC registers** | keep `r15` = ctx inside Ki code (R15 is scratch in static builds, `abi-internal.md:398`); reload after every Go call (no callee-saved registers, `:221`) | pass ctx in a Go-visible way (e.g. ABI0 stack slot) | ch 46/50 teach the register table either way. KiCC's `r10` = closure register should be re-checked against Go's `RDX` = closure context (`:410`) |
| **F4: how `ki build` gets kirt** | embed kirt's source in `ki` (`//go:embed`) and write a generated module into a build directory | require the kilang module on disk (`replace` directive) | ch 47 teaches modules, `replace` and `embed` either way. A is friendlier to the "no junk" rule if the build dir is cleaned |
| **F5: I/O** | own epoll poller over `syscall` (as in the C++ spec) | delegate to Go's netpoller through helper goroutines | ch 45 and 68 keep their hours under A; B shrinks them and teaches `net` instead |

---

## 7. Hours: method, re-estimate, totals

**Method.** Each C++ chapter mixes host-language hours with compiler/runtime-concept hours. I applied one rule per row:
- **Algorithm chapters keep their hours.** These are 12, 13, 14, 17, 20, 21, 28, 30, 31, 38, 42, 44, 48, 49, 53, 55 and 57; the compiler work is identical. For 22, 27, 29, 37, 39, 40, 43, 51, 56, 59 and 60 I also saw no reason to move.
- **Host-mechanics chapters drop 30–50 %.** These are 1, 2, 4, 18, 47 and 62, plus the toolchain parts of 63 and 66; Go's tooling is a single command, and its language is smaller.
- **Chapters with partial host content drop 1–2 h.** These are 3, 5–11, 15, 16, 19, 23, 24, 32, 33, 35, 41, 45, 58, 61, 64, 65 and 67–68.
- **Chapters where Go fights the design rise 10–30 %.** These are 36, 46, 52 and 54. Part IV rises by 1 h per chapter, because it now also teaches Go's user-level concurrency.
- **The caps still hold:** no chapter above 16 h, and no gap between milestones above ≈ 100 h.

These are **my estimates, not measurements**, exactly like the C++ figures.

### Per-part summary

| Part | C++ h | Go h | Δ | Main reasons |
|---|---|---|---|---|
| I. Fundamentos | 95 | 78 | −17 | no CMake, own test framework or sanitizers; generics are simpler than templates |
| II. Núcleo funcional | 110 | 102 | −8 | no ODR or headers; `embed`; simpler strings and closures |
| III. Efectos | 58 | 56 | −2 | algorithms unchanged |
| IV. Concurrencia en el oráculo | 26 | 28 | +2 | adds Go's goroutines, channels, `select` and `synctest` |
| V. Middle end | 76 | 74 | −2 | `os/exec` + `context` instead of `posix_spawn` |
| VI. Runtime y VM | 90 | 89 | −1 | ch 34 now also covers the Go runtime (−1); ch 35 has no `extern "C"` (−2); the allocator in GC-opaque memory (+2) |
| VII. Multinúcleo | 70 | 68 | −2 | std lib `sync`; algorithms unchanged |
| VIII. Nativo x86-64 | 123 | 126 | +3 | Plan 9 and ABI0 (+2); `go build` replaces ELF/`ld` (−3); stacks and fibers under the Go runtime (+4) |
| IX. Rendimiento | 54 | 52 | −2 | pprof/PGO built in; no computed goto to learn |
| X. Programas reales | 52 | 46 | −6 | `net/http` for the load tools; `go install` instead of CPack |
| XI. Otras plataformas | 82 | 61 | −21 | the same ABI0 on Windows; no cross binutils; Go's portable `sync`/`atomic` |
| **Trunk** | **836** | **780** | **−56 (−6.7 %)** | |
| Reserve 15 % | 125 | 117 | | |
| **With reserve** | **961** | **≈ 897** | | |

**Range.** I'd plan for 760–815 h of trunk:
- F1 (Option A) could add 5–10 h across ch 52–54;
- F5 (Option B) could save 5–8 h;
- a real Go learner could beat the Part I estimate by 5 h or so.

**Why the saving is only 7 %.** The C++ book was already mostly compiler and runtime, and those hours do not change. Go saves where C++ was expensive to *learn and operate*, but it adds friction exactly where Ki's runtime does unusual things: its own stacks, its own heap, its own asm.

### Calendar

| Pace | M1 (78 h) | Whole book (780 h) | With reserve (897 h) |
|---|---|---|---|
| 5 h/week | ≈ 3.6 months | ≈ 3.0 years | ≈ 3.4 years |
| 7.5 h/week | ≈ 2.4 months | ≈ 2.0 years | ≈ 2.3 years |
| 10 h/week | ≈ 1.8 months | ≈ 1.5 years | ≈ 1.7 years |

At 7.5–10 h per week, M1 now lands inside the 2–3 months Álvaro accepted. At 5 h it lands at about 3.6 months, against 4.4 before.

**Cut order and "never cut".** I keep them as in spec §12.3:
1. ch 57;
2. the HTTP server of ch 61;
3. ch 56.

I would add a fourth, only if F1 forces it: ch 54's "16 cores" milestone falls back to "native on 1 worker, multicore on the VM".

### Milestones re-timed

| Milestone | Chapter | C++ cum. h | Go cum. h | What changes in what you see |
|---|---|---|---|---|
| M1 | 10 | 95 | 78 | Same program and errors. "Alrededor" becomes: `go test ./...`, `go vet`, `gofmt -l`, `-race`, the golden runner (built binaries, not `go run`), the CI with cross-compile checks |
| M2 | 12 | 121 | 103 | unchanged |
| M3 | 16 | 173 | 152 | unchanged (`prelude.ki` via `//go:embed`) |
| M4 | 24 | 263 | 236 | unchanged |
| M5 | 26 | 289 | 264 | unchanged |
| M6 | 33 | 365 | 338 | shrunk cases also saved through `T.ArtifactDir` |
| M7 | 40 | 455 | 427 | unchanged |
| M8 | 45 | 525 | 495 | "TSan" becomes `-race` + litmus |
| M9 | 49 | 577 | 546 | first native binaries **built by `go build`** from Ki-emitted Plan 9 asm + kirt |
| M10 | 54 | 648 | 621 | unchanged (depends on F1) |
| M11 | 58 | 702 | 673 | `perf` becomes `pprof` + trace + PGO |
| M12 | 62 | 754 | 719 | unchanged; plus `go install` and cross-built release archives |
| M13 | 65 | 794 | 754 | unchanged |
| M14 | 68 | 836 | 780 | unchanged |

---

## 8. Draft index (same shape as spec §12.5, with "Go nuevo")

Columns: `# | Capítulo | Hito | Go nuevo | Compiladores | h (C++ → Go)`. Chapter titles stay in Spanish, as in the book; changed titles are marked ✎. The "Compiladores" column is unchanged from the spec unless noted. "Go nuevo" is in English for this research file, and will be translated when it goes into the spec.

#### Parte I. Fundamentos y primer programa (≈ 78 h → M1)

| # | Capítulo | Hito | Go nuevo | Compiladores | h |
|---|---|---|---|---|---|
| 1 | ✎ El taller: el toolchain de Go, módulos, `go test` y la CI | repo `aldezex/kilang` created; `ki --version` (from `debug.ReadBuildInfo`); `go test ./...`, `go vet ./...` and `gofmt -l` green, also with `-race`, locally and in GitHub CI; cross-compile check for windows/arm64 | Go from zero: `go mod init`, `go.mod` (`go`/`toolchain` lines, GOTOOLCHAIN), packages and exported names, `cmd/` + `internal/`; types, funcs, multiple returns, `for` (incl. `range` over int), `switch`, compared with Rust, TS and Python; `gofmt`; `go vet` (incl. `tests`); `testing`: `TestXxx`, subtests, table tests, `t.Helper`, `t.Cleanup`; `flag`; `ignore` directive; `setup-go` with `go-version-file` | el mapa del pipeline; TDD rojo-verde; tests golden | 12 → 8 |
| 2 | ✎ Valores, punteros, slices y el recolector | SourceMap keeps files in memory; line/column lookup tested | value semantics and copies; pointers without arithmetic; structs; value vs pointer receivers; **slice internals** (len/cap, `append` aliasing, `a[i:j:k]`); `string` vs `[]byte`; `os.ReadFile`; `slices.BinarySearch`; what the GC does and doesn't free you from; first `-gcflags=-m` (vs Rust's borrow checker) | ficheros fuente, spans, tablas de líneas | 12 → 8 |
| 3 | ✎ Tipos suma sin tipos suma: interfaces selladas y errores como valores | `ki check` shows a caret diagnostic (golden) | defined types, `iota` + `String()`; interfaces; sealed interface + type switch; the typed-nil trap; `error`, `%w`, `errors.Is/As/AsType/Join`; `fmt` verbs, `Stringer`, `Formatter`; golden files in `testdata/` with `-update` | modelo de diagnósticos, códigos de error, renderizado | 8 → 7 |
| 4 | ✎ Genéricos y la caja de herramientas del compilador | `IdVec[I, T]`, `Arena`, `Interner`, tested and benchmarked | type parameters, constraints, `~uint32`, `comparable`, `cmp.Ordered`, inference, no generic methods; typed ids as defined types; `unique.Make` vs a hand-written interner; pointer-free slabs and the GC; `hash/maphash`; `Example` tests; `b.Loop` + `-benchmem` | almacenamiento orientado a datos, ids tipados, interning | 8 → 6 |
| 5 | El lexer y la regla de línea | `ki emit tokens` | bytes vs runes, `unicode/utf8`, `[256]uint8` class tables, `strconv.ParseInt` + `*NumError`/`ErrRange`; `go generate` with a hand-written generator (replaces X-macros); first fuzz target (spans in bounds, never panics) | maximal munch, inserción de separadores, modos del lexer, recuperación | 9 → 8 |
| 6 | El parser: descenso recursivo, Pratt y recuperación | `ki emit ast` (S-expressions) | recursion; why ids and not pointers *in a GC language* (compact, pointer-free, side tables, stable dumps); closures (basics); `panic("unreachable")`; `strings.Builder`, `io.Writer`; parser fuzzing; Google style guide naming | precedencias, búsqueda acotada, nodos Error, sincronización | 12 → 11 |
| 7 | Nombres y ámbitos | `unknown name 'fibb'; did you mean 'fib'?` | maps in depth: randomized iteration and the "sort by id" rule, Swiss tables (1.24); `maps`/`slices`; `iter.Seq` for scope walks; `slices.Sorted(maps.Keys(m))`; dynamic programming on slices (edit distance) | tablas de símbolos, DefIds, tablas laterales | 7 → 6 |
| 8 | Tipos I: comprobar contra firmas, con errores legibles | type errors with two labels and their origin | union-find over slices; hash-consing with comparable struct keys; immutability by convention (Go has no `const` values); unexported fields; struct embedding | unificación, comprobación bidireccional, procedencia | 10 → 9 |
| 9 | Core: la referencia semántica | `ki emit core`; Core Lint green | builder APIs; small interfaces; traversals as `iter.Seq` (Go's `std::generator`); invariant checks with `panic` + `runtime/debug.Stack()` | diseño de Core, elaboración, invariantes | 7 → 6 |
| 10 | El intérprete CEK | **M1** | hand-tagged unions; defined `int64` wrap + explicit overflow checks and `math/bits`; shifts; explicit stacks as slices; `recover` at the pass boundary; exit codes (panic = 2, `os.Exit` skips defers, flush first; `go run` masks codes) | máquina CEK, llamadas de cola, primitivas, panics | 10 → 9 |

#### Parte II. El núcleo funcional en el oráculo (≈ 102 h → M2, M3)

| # | Capítulo | Hito | Go nuevo | Compiladores | h |
|---|---|---|---|---|---|
| 11 | Datos: tipos algebraicos, records y tuplas | `Shape` values, records with update, `Float` printed with `.0` | struct layout and padding (`unsafe.Sizeof/Alignof/Offsetof`); flat storage vs a slice of interfaces (boxing cost); `strconv.FormatFloat(f,'g',-1,64)`; the spec's FMA rule and explicit `float64()`; `new(expr)` (1.26) | tipos nominales, constructores, records | 12 → 11 |
| 12 | match y exhaustividad | **M2** | recursion over pattern matrices; bitsets as `[]uint64` + `math/bits`; subslices share backing (`slices.Clone`); `slices.Collect` | utilidad de Maranget, testigos, guardas; matching ingenuo en el oráculo | 14 → 14 |
| 13 | Polimorfismo: genéricos, generalización y niveles | generic functions, inferred private functions, readable "local not generalized" error | iterative Tarjan; **how Go implements generics** (GC-shape stenciling + dictionaries) next to Ki's dictionaries vs monomorphization; the closure-capture trap and its 1.22 fix | variables de tipo, niveles, generalización por SCC | 14 → 14 |
| 14 | Traits I: coherencia y paso de diccionarios | `impl Describe for Shape`; operators via traits; `print<T: Show>` | interfaces in depth: implicit satisfaction, method sets, itabs (`runtime/iface.go`), embedding; "methods only in the type's package" as Go's orphan rule; constraint interfaces vs runtime interfaces vs Rust traits | resolución de instancias, superclases, huérfanos, diccionarios acíclicos | 16 → 16 |
| 15 | Closures, lambdas, pipes y métodos | `xs \|> map((x) => x * 2)` and `s.describe()` | func values; closures and escaping captures; method values and expressions; func types with methods (`http.HandlerFunc`); the closure-context register (RDX, `abi-internal.md:410`) as a preview | variables libres, closures, azúcar de `\|>`, búsqueda de métodos por tipo | 12 → 10 |
| 16 | Listas, strings, interpolación y `UInt64` | **M3**; also `UInt64` | strings as immutable bytes; `range` over string; `strings`/`bytes`; `uint64` and explicit conversions; `//go:embed` + `embed.FS` for `prelude.ki` | azúcar de listas, preludio en Ki, interpolación | 10 → 9 |
| 17 | Traits II: tipos de orden superior, derive y el preludio | Functor, Foldable, Monad; `sum` over any Foldable; `#[derive]` | **what Go generics cannot express** (no HKT, no variadic type parameters, no specialization) and how Ki compiles it anyway; self-referential constraints (1.26); generic type aliases (1.24); "core types" removed from the spec (1.25) | kinds, constructores currificados, derive | 14 → 14 |
| 18 | Módulos e imports | multi-file projects; prelude in Ki, each type in its module; methods across modules; import cycles with a message | packages at scale; `internal/`; import cycles are compile errors (as in Ki); package initialization order (spec); `io/fs`, `os.DirFS`; **`os.Root`** to confine the project to the input file's directory; `path/filepath`; `errors.Is(err, fs.ErrNotExist)`; `go list -deps` | grafo de módulos, visibilidad, orden de inicialización | 10 → 7 |
| 19 | Errores que se leen: catálogo, `ki explain` y fix-its | one snapshot per error code | `embed.FS` for `docs/errors/*.md`; `go generate` for the code index; terminal detection; ANSI colours; a generated Unicode width table (no `x/text`); `encoding/json` for JSON lines (json/v2 still experimental); `runtime/debug.Stack` for ICEs | procedencia en cadena, diseño de mensajes | 8 → 7 |

#### Parte III. Efectos, control y pereza (≈ 56 h → M4)

| # | Capítulo | Hito | Go nuevo | Compiladores | h |
|---|---|---|---|---|---|
| 20 | Filas de efectos | `uses` checked; "performs Log but does not declare it", with both fixes | small sorted slices (`slices.BinarySearchFunc`, `Insert`, `Compact`); a Go `SmallVec` (inline `[N]T` + slice) and what `-m` says about it | etiquetas con ámbito, unificación de filas, regla ambiente | 14 → 14 |
| 21 | Handlers y continuaciones one-shot | `handle`, `perform`, `resume` in the oracle; Fail, State, Log | no move-only types: a consumed flag + vet copylocks on `noCopy` [V30]; O(1) slice splicing; state machines with `iota` | reglas small-step, handlers profundos, soltar continuaciones | 16 → 16 |
| 22 | Generadores, estado y `?` | iterators with Yield, `runState`, `catch`/`toResult`, `e?` | push iterators vs `iter.Pull` — Go's pull iterator is a **one-shot stackful coroutine** in the runtime (`runtime/coro.go`), i.e. exactly what Yield is; `defer stop()` | continuaciones guardadas, `resume` que no está en cola | 10 → 10 |
| 23 | Azúcar imperativo sobre un núcleo puro | `var`, `for`, `while`, `break`, `continue`, `return` | labeled `break`/`continue`; `goto` and its spec rules as Go's face of join points; `slices.All`/`Backward` | join points, `joinrec`, intuición de SSA | 12 → 11 |
| 24 | Pereza explícita | **M4** | `sync.Once`/`OnceValue` as Go's memoized thunks, and why Ki's thunks take no lock | call-by-need como biblioteca, comprobación de pureza | 6 → 5 |

#### Parte IV. Concurrencia en el oráculo (≈ 28 h → M5)

| # | Capítulo | Hito | Go nuevo | Compiladores | h |
|---|---|---|---|---|---|
| 25 | Fibras y el planificador determinista | toy scheduler as a Ki handler (`Coop { fork, yield }`); then `spawn` with `--sched=det --seed` and detected deadlocks | **goroutines**, the model Ki copies; `sync.WaitGroup` + `WaitGroup.Go` (1.25); vet `waitgroup`; a generic ring buffer; intrusive lists (vs `container/list`); hand-written splitmix64 (`math/rand/v2`'s PCG/ChaCha8 exist, but the shared-header rule stays) | fibras como estados, puntos de planificación | 12 → 13 |
| 26 | Canales, select y tiempo virtual | **M5** | **channels and `select`** in Go: buffering, `close`, closed-receive, send-on-closed panic, nil channels, `default`, `time.After`, unbuffered timer channels (1.23); **`testing/synctest`** as Go's own virtual-time bubble, compared with Ki's det mode; `slices.SortStableFunc`, `container/heap` | rendezvous, cierre, `select`, reloj virtual, trazas | 14 → 15 |

#### Parte V. Un middle end verificado (≈ 74 h → M6)

| # | Capítulo | Hito | Go nuevo | Compiladores | h |
|---|---|---|---|---|---|
| 27 | ANF y comprobar cada pase | `ki test --check-passes` | IR rewriting with worklists; `slices` algorithms; iterators over IR | ANF, Lint por pase, diferencial entre pases | 8 → 8 |
| 28 | Árboles de decisión | compiled matches agree with the naive matcher | memoization with struct keys or `maphash.Comparable` (1.24) | árboles de decisión de Maranget, compartición | 10 → 10 |
| 29 | Lambda lifting y conversión de closures | no closure contains itself | free-variable sets as `[]uint64` with iterator methods | lifting, entornos explícitos | 8 → 8 |
| 30 | Perceus I: propiedad y el heap comprobado | the whole suite, leak-free, with `--level=rc` | dataflow over `[]uint64`; tombstoned object table; provenance with `runtime.Caller`; **Go's tracing GC as the foil**: tri-colour mark-sweep, write barriers, `GOGC`, Green Tea (default in 1.26); what Perceus buys (reuse, determinism) | RC preciso, dup/drop, linealidad | 16 → 16 |
| 31 | Perceus II: préstamos, fronteras y reutilización | in-place `map` and `filter`; boundary rule | `weak.Pointer` and `runtime.AddCleanup` (1.24) vs `shared_ptr`/`weak_ptr`, and why Ki uses neither; fixed point over SCCs | inferencia de préstamos, especialización de drop, FBIP | 14 → 14 |
| 32 | Un generador de programas bien tipados | kigen produces programs that compile and terminate | size-bounded recursive generators (closures vs interfaces); `math/rand/v2` with explicit seeds; Go's byte-level fuzzer as a complement, and why type-directed kigen is still needed | generación dirigida por tipos | 12 → 11 |
| 33 | Encoger fallos y el corpus de regresión | **M6** | `os/exec`, `exec.CommandContext` + `context.WithTimeout`, pipes; `t.Context()` (1.24); `T.ArtifactDir` + `-artifacts` (1.26) for shrunk cases; `log/slog` with `JSONHandler` for triage logs | delta debugging, triaje | 8 → 7 |

#### Parte VI. El runtime y la máquina virtual (≈ 89 h → M7)

| # | Capítulo | Hito | Go nuevo | Compiladores | h |
|---|---|---|---|---|---|
| 34 | ✎ El modelo de memoria de Go y el runtime que te hospeda (antes del primer atómico) | litmus tests with `-race` | Go memory model (DRF-SC, happens-before, **SC-only** `sync/atomic`, multiword races corrupt memory); race detector (needs cgo); **the host runtime's rules**: G/M/P, sysmon, cooperative + async preemption (never inside asm), stop-the-world, stack growth by copying, `LockOSThread`. Guided reading of `runtime2.go`, `proc.go`, `stack.go`, `preempt.go` | memoria débil, linealizabilidad | 12 → 11 |
| 35 | ✎ El modelo de objetos de kirt | value-encoding tests, `BoxedInt` included | `unsafe`: the six valid `Pointer` patterns, `unsafe.Add/Slice/String/StringData`, `uintptr` is not a reference, `runtime.KeepAlive`, `-d=checkptr`, vet unsafeptr; compile-time layout asserts; `structs.HostLayout`; **why Ki objects live outside the Go heap and never hold Go pointers** | representación uniforme, tagging, `Int` de 64 bits canónico, inmortales | 11 → 9 |
| 36 | Tu propio allocator | allocator stress; debug and system modes | `syscall.Mmap`/`Madvise`/`MAP_NORESERVE` (no `MADV_FREE` constant); off-heap intrusive free lists via `unsafe`; Treiber push with `atomic.Uint64` CAS; `b.Loop` + `-benchmem`; `KI_ALLOC=system` = Go-heap slabs; what no Go tool can see in an mmap'd heap | free lists segregadas, liberación remota | 14 → 16 |
| 37 | Conteo de referencias en el runtime | drop a 10⁷-element list without recursion; census | atomics on off-heap words (`(*atomic.Int32)(unsafe.Pointer(p))`, alignment); SC-only atomics: the cost and the written argument; non-recursive algorithms | RC local, compartido e inmortal; `mark_shared` | 10 → 10 |
| 38 | MIR: una IR de bajo nivel en SSA | MIR for the whole suite; verifier | IR API design in Go (concrete structs vs small interfaces, `[]Operand` sub-slices); doc comments and `go doc` | SSA con parámetros de bloque, layouts | 14 → 14 |
| 39 | Bytecode y el bucle de la VM | `--backend=vm` for sequential programs | dispatch with a dense `switch` (a jump table [V24]); **BCE** (`-d=ssa/check_bce`, `[256]` + `uint8`); inlining budget (`-m=2`); escape analysis in the hot loop; first `-cpuprofile` + `go tool pprof`; `go generate` opcode tables; fuzzing the bytecode verifier | VM de registros, convención de llamada, caminos rápido y lento de la aritmética | 15 → 15 |
| 40 | Efectos en la VM: segmentos y continuaciones | **M7** | object pools (`sync.Pool` vs own free lists); intrusive lists; handles with `Close` + `defer` | segmentos, registros de handler, mapas de slots | 14 → 14 |

#### Parte VII. Concurrencia multinúcleo (≈ 68 h → M8)

| # | Capítulo | Hito | Go nuevo | Compiladores | h |
|---|---|---|---|---|---|
| 41 | Fibras de la VM y el planificador determinista del runtime | VM traces identical to the interpreter's | interfaces vs generics for the scheduler (dynamic vs static dispatch; PGO devirtualization later); worker goroutines and `LockOSThread`; no goroutine-local storage → explicit `ctx` | interfaz del planificador, `Park` | 12 → 11 |
| 42 | El deque de Chase–Lev | passes linearizability stress and `-race` | lock-free Go with `atomic.Int64`/`atomic.Pointer[T]`; CAS loops; ABA; signed indices; cache-line padding by hand; false sharing measured; reading `runqget`/`runqsteal` | work stealing | 14 → 14 |
| 43 | El planificador work-stealing | 16 workers; parallel fib scales | parking: raw `futex` via `syscall.Syscall6` vs channel/`sync.Cond` parking, and what each costs inside Go's own scheduler (`entersyscall`); reading `findRunnable` (the 1-in-61 check, the spinning rule); `go tool trace` on your workers; container-aware `GOMAXPROCS` | spinning y parking, preempción | 14 → 14 |
| 44 | Canales y select en el runtime | runtime det mode equals the interpreter's for every seed | `sync.Mutex` + `defer Unlock`; lock order by address; CAS `done`; reading `hchan`, `selectgo`, `gopark`'s `unlockf`; mutex/block profiles; goroutine-leak profile (experiment) | hchan, aparcar y soltar, `select` en dos fases | 16 → 16 |
| 45 | E/S y temporizadores con epoll | **M8** | `syscall.EpollCreate1/Ctl/Wait`; `eventfd` through raw `SYS_EVENTFD2`; `SetNonblock`; `syscall.Errno` with `errors.Is`; a 4-ary heap; a blocking pool of goroutines; `context` for shutdown; `synctest` for timer tests; compare Go's netpoller (`netpoll_epoll.go`); `runtime.AddCleanup` as the close-on-drop safety net for Go-side fds | E/S por disponibilidad, temporizadores | 14 → 13 |

#### Parte VIII. Código nativo x86-64 (≈ 126 h → M9, M10)

| # | Capítulo | Hito | Go nuevo | Compiladores | h |
|---|---|---|---|---|---|
| 46 | ✎ x86-64 y el ensamblador de Go (Plan 9) desde cero | a hand-written `.s` calls kirt, built by `go build` | the ISA from zero, plus Plan 9 syntax: operand order, `FP`/`SB`/`SP`, `TEXT ·f(SB), NOSPLIT, $frame-args`, `textflag.h`, `funcdata.h`/`NO_LOCAL_POINTERS`; **ABI0 vs ABIInternal** (the selector is refused outside the runtime), `.abi0` wrappers, register roles (R14 = g, X15 = 0, RDX = closure; no callee-saved); the **auto-inserted stack check**; vet asmdecl; `go tool objdump`; `-gcflags=-S` | ISA, direccionamiento, flags, convenciones de llamada | 12 → 14 |
| 47 | ✎ `go build` como enlazador: `ki build` y kirt | `ki build` links a stub; `ki doctor` checks `go version` and GOROOT | the generated module (stub `main.go` + `ki_amd64.s` + `go.mod`); why kirt can't be `internal/`; `go build -x -work`; `go tool asm/compile/pack/link`; build cache; `-ldflags` (`-X`, `-s -w`, `-B`); `-trimpath`; file suffixes and `//go:build` (the arm64 build fails without `_arm64.s`); `go version -m`; `//go:linkname` restrictions | enlazado, arranque del programa, KiCC | 10 → 7 |
| 48 | Selección de instrucciones | `fib` native with everything in memory (spill-all) | pattern tables as Go data; `bufio.Writer` + `fmt.Fprintf` to emit text; LIR design; Plan 9 mnemonics and operand order in the emitter | tiling de árboles, maximal munch, fusión de flags | 14 → 14 |
| 49 | Liveness y linear scan, con su verificador | **M9** | hand-written BitSet; `container/heap`; `slices.SortFunc` comparators; property tests with your own generators or fuzz targets | intervalos de vida, Poletto–Sarkar, verificador | 16 → 16 |
| 50 | Marcos, llamadas, tail calls y mapas de pila | all effect-free tests pass natively; objdump/pprof show Ki lines (gdb via DWARF still to confirm) | binary tables with `sort.Search`; the frametable as `DATA`/`GLOBL` symbols registered from Go (no `__start_` symbols) [I]; **`#line N "file.ki"`** so pclntab maps to Ki lines [V6, checked with objdump; gdb/DWARF not verified]; ABI0 frame layout | marcos, frametable, llamadas de cola | 14 → 14 |
| 51 | Closures, ADTs y RC nativos | the Part II corpus runs natively, leak-free | reading generated code with `go tool objdump`; inline fast paths; alignment asserts | caminos rápidos de RC, de asignación y de la aritmética de `Int` | 13 → 13 |
| 52 | ✎ Segmentos de pila y cambio de contexto bajo el runtime de Go | 1M switches in a Go test; `main` on a segment | **the F1 fork**: SPWRITE functions and GC stack scans; the stack-split prologue vs foreign stacks (`NOSPLIT` + own check); `syscall.Mmap` slabs with `MAP_NORESERVE`; guard pages with `Mprotect`; why no sanitizer API helps any more | cadenas de segmentos, `ki_switch`, chequeo de pila | 14 → 16 |
| 53 | Efectos nativos | the effects corpus is identical in all three backends | asm trampolines; debugging across stacks with gdb (Delve won't understand Ki-owned stacks [I]) | `ki_handle`, `ki_perform` y `ki_resume`; desenrollar con la frametable | 16 → 16 |
| 54 | Fibras nativas en 16 núcleos y prueba a tres bandas | **M10** | the Go runtime owns signals: `GOTRACEBACK`, `debug.SetCrashOutput`, `os/signal`; **back-edge polls are mandatory** (asm is never async-preempted, so a loop without calls stalls Go's STW); `trace.FlightRecorder` for crash forensics; gdb with threads | riesgos de la migración, matriz a tres bandas | 14 → 16 |

#### Parte IX. Rendimiento (≈ 52 h → M11)

| # | Capítulo | Hito | Go nuevo | Compiladores | h |
|---|---|---|---|---|---|
| 55 | El optimizador de Core | inlining, specialization, case-of-known-constructor, tail-resumptive fast path; suite still identical | worklists; memoization; profiling the compiler itself with pprof (CPU and allocs) | simplificador, modelos de coste | 16 → 16 |
| 56 | Representaciones sin caja | nbody several times faster (measured) | `math.Float64bits`/`Float64frombits`; NaN-boxing as comparison; SIMD awareness and the experimental `simd/archsimd` (1.26, amd64) | elección de representación, worker/wrapper | 12 → 12 |
| 57 | Linear scan II: partir intervalos | fewer spills on the benchmark suite | ordered interval structures without `std::set`: sorted slices or a hand-written balanced tree | huecos de vida, resolución de movimientos (Wimmer–Franz) | 14 → 14 |
| 58 | ✎ Medir de verdad: pprof, dispatch y PGO | **M11** | no computed goto or `musttail` in Go: measure jump-table `switch` vs a function table vs closure-threaded dispatch; `pprof` (flame graph by default in 1.26); `go tool trace`; perf optional; **PGO** with `default.pgo`; `GOGC`/`GOMEMLIMIT` on the `ki` process; benchmark statistics | metodología de benchmarks, dispatch threaded | 12 → 10 |

#### Parte X. Programas reales y Ki v0.1 (≈ 46 h → M12)

| # | Capítulo | Hito | Go nuevo | Compiladores | h |
|---|---|---|---|---|---|
| 59 | La biblioteca estándar en Ki | Map, Set, Array, String with FBIP | runtime primitives for strings and arrays on `bytes.IndexByte`/`bytes.Compare` (asm-backed in Go's own std lib); UTF-8 utilities | diseño de bibliotecas FBIP | 14 → 14 |
| 60 | Paralelismo como biblioteca | scaling curves of `parMap`, `parReduce` and a parallel sort on 16 cores | contention profiling (mutex/block); false sharing in practice; errgroup-style patterns by hand (`x/sync` not allowed); `GOMAXPROCS` in containers | control de granularidad, fork-join sobre fibras | 10 → 10 |
| 61 | Programas de verdad: una CLI paralela y un servidor HTTP | a parallel grep clone; a reduced HTTP/1.1 server under load | the load generator and a reference server written in Go: `net/http` (1.22 patterns), deadlines with `context`, graceful shutdown with `signal.NotifyContext`, `slog` + `NewMultiHandler`, `net/http/pprof`, backpressure | programas de punta a punta | 14 → 12 |
| 62 | Herramientas y release | **M12**: `ki fmt`, `ki explain`, tag v0.1 | `go install`; cross-built release archives; `-ldflags -X`, `debug.ReadBuildInfo`, VCS stamping; `go version -m`; semver and module tags; doc comments, `go doc -http`; `go fix` modernizers; a small `cmd/kicheck` on `go/parser` + `go/types` (std lib); code review with the Google and Uber guides; Wadler pretty printing in Go | impresión bonita, documentación | 14 → 10 |

#### Parte XI. Otras plataformas (≈ 61 h → M13, M14)

Scope as in the spec: `ki` stays a Linux x86-64 tool that compiles across targets. Under Go this is just `GOOS`/`GOARCH` on the generated module, and the executables still run on the target and are compared with the oracle.

| # | Capítulo | Hito | Go nuevo | Compiladores | h |
|---|---|---|---|---|---|
| 63 | AArch64 I: A64, el ensamblador de Go para arm64 y selección de instrucciones | `fib` on ARM64 under qemu-user | `GOARCH=arm64` (no cross toolchain); Plan 9 arm64 (`MOVD`, `RSP`, R28 = g, R26 = closure, `_arm64.s`); qemu-user + binfmt from apt | selección de instrucciones A64, reutilizar el asignador | 14 → 12 |
| 64 | AArch64 II: el runtime en memoria débil | `ki_switch` and the deque on ARM64 | Go's atomics stay SC on arm64, so kirt's Go code ports unchanged; what is weak is **Ki-emitted** code (RC fast paths, migration edges); litmus on the real ARM runner; **arm64 fuses `a*b+c`** [V11] and the float rule | runtime en memoria débil | 14 → 13 |
| 65 | AArch64 III: la suite completa en ARM | **M13** | CI on `ubuntu-24.04-arm`; build constraints for portability layers | portabilidad del backend | 12 → 10 |
| 66 | ✎ Windows x64 I: el mismo ABI0, otro sistema | a minimal Ki `.exe`, built from WSL | `GOOS=windows`; **the same Plan 9 `.s` links and runs** [V7]; Win64 (shadow space, callee-saved `xmm6`–`15`) only exists inside Go's syscall layer, read but not implemented; PE/COFF from the Go linker | segundo ABI (solo en la frontera del sistema), PE/COFF | 14 → 7 |
| 67 | Windows x64 II: el runtime en Windows | fibers and scheduler on Windows | `syscall.NewLazyDLL("kernel32.dll")` for `VirtualAlloc`/`VirtualProtect`/`WaitOnAddress` (not in `syscall`) [V10]; `_windows.go` files; stack slabs on Windows (depends on F1) | runtime portable | 14 → 9 |
| 68 | Windows x64 III: IOCP y la suite en Windows | **M14** | `syscall.CreateIoCompletionPort`, `GetQueuedCompletionStatus`, `Overlapped`, `WSARecv` [V10]; compare with Go's own Windows poller | E/S por completado frente a por disponibilidad | 14 → 10 |

**Epílogo** (changes only):
- "Contribuir a un compilador real (GCC, LLVM, Clang)" becomes **"contribuir a Go"**: Gerrit, `go.dev/doc/contribute`, `cmd/compile`, the runtime.
- "FFI con C" becomes **cgo**.
- Add an "Option B of F1" appendix if A is chosen: fibers as goroutines.

---

## 9. «Go en el trabajo» per part

These are ideas for the boxes, item 9 of the chapter template. Sources are named where they exist. Everything about specific companies or projects is **[I]**, from memory, and should be checked when written.

- **Part I.**
  - `gofmt` as social contract; Effective Go; the Google guide's naming (package names, no stutter: `syntax.Lexer`, not `syntax.SyntaxLexer`).
  - Table tests and golden files as the house style of Go's own repo (`cmd/gofmt/testdata`, `go/printer/testdata`).
  - Interview classics: slice aliasing after `append`, typed nil in an interface, map iteration order, loop variables before and after 1.22.
  - `go vet` and `-race` in every CI.
- **Part II.**
  - "Accept interfaces, return structs" and small interfaces (`io.Reader`).
  - When to use generics (go.dev/blog/when-generics *(not re-fetched)*) and when not to.
  - `internal/` as the real encapsulation boundary in large repos; API compatibility (adding a method to an exported interface breaks users).
  - Doc comments.
  - Errors: wrap at boundaries, `errors.Is` for sentinels, structured error types for programmatic handling.
- **Part III.**
  - Iterators in public APIs (1.23 conventions: `All`, `Values`, `Backward` return `iter.Seq`).
  - Rob Pike's "Lexical Scanning in Go" (2011) state-function lexer as a Go classic.
  - `sync.OnceValue` for lazy initialization in services.
  - State machines in protocol code.
- **Part IV.**
  - Pipelines, fan-out/fan-in (go.dev/blog/pipelines *(not re-fetched)*).
  - Goroutine leaks as the #1 production concurrency bug, and how to spot them (goroutine profile, the 1.26 leak profile experiment).
  - Testing time-dependent code with `synctest` instead of sleeps.
  - Contexts end to end.
- **Part V.**
  - Native fuzzing in real projects (OSS-Fuzz accepts Go native fuzzers [I]).
  - `go test -json` into CI dashboards; test sharding.
  - `T.ArtifactDir` to keep failing inputs from CI; subprocess tests with `exec.CommandContext`.
- **Part VI.**
  - Where production Go uses `unsafe` and mmap: etcd's bbolt is an mmap'd B+tree [I].
  - Off-heap or pointer-free caches that avoid GC scanning [I].
  - `GOMEMLIMIT` in containers (the GC guide); allocation profiling; benchmark discipline before and after.
  - Reading compiler output (`-gcflags=-m`, `-S`) in performance reviews.
- **Part VII.**
  - Scheduler knowledge (G/M/P, `GOMAXPROCS`, syscalls and P handoff) is standard interview material for backend and infra roles.
  - Container-aware `GOMAXPROCS` (1.25) replaced a widely used third-party library [I: uber-go/automaxprocs].
  - Mutex vs channel guidance; mutex/block profiles in production.
  - Kubernetes and etcd run unit tests with `-race` in CI [I].
- **Part VIII.**
  - Go assembly in the wild: the std lib's crypto and `math/big`, compression libraries [I].
  - `go vet` asmdecl as the safety net; generators that emit Go asm (the third-party `avo` [I]) as the industry pattern Ki's backend mirrors.
  - Compiler Explorer supports Go; reading the compiler's own SSA (`GOSSAFUNC=f go build` writes `ssa.html`) [I].
  - Contributing to `cmd/compile`.
- **Part IX.**
  - PGO in production (go.dev/doc/pgo *(not re-fetched)*): collect from prod `pprof`, commit `default.pgo`.
  - Continuous profiling; `GODEBUG=gctrace=1` triage.
  - Escape and bounds-check work on hot paths, only after a profile says so.
- **Part X.**
  - CLI conventions with `flag` and subcommands; `net/http` servers with timeouts set explicitly; graceful shutdown; `slog` with request-scoped attributes; `net/http/pprof` behind auth.
  - Release engineering: `-trimpath`, `-ldflags -X main.version`, `go version -m` for provenance, reproducible builds, govulncheck in CI.
  - Code review with the Google/Uber guides; `go fix` modernizers before a release.
- **Part XI.**
  - One-command cross-compilation is one of Go's commercial selling points.
  - `CGO_ENABLED=0` static binaries for distroless containers; arm64 in the cloud (e.g. AWS Graviton) [I].
  - Platform files with build constraints; multi-OS CI matrices; Windows services in Go [I].

---

## 10. A revised honest note on "expert" (replaces spec §14) [I]

- **Go's ceiling is lower than C++'s, and that cuts both ways.**
  - The language and the toolchain are small enough that this book can plausibly cover *all* of the language, the standard library areas a systems engineer uses, and the toolchain.
  - A senior Go role also expects things a solo compiler project doesn't give: large shared codebases, service architecture, distributed-systems libraries (gRPC and Kubernetes client libraries are third-party), and on-call experience.
- **What the book gives instead is rare among Go developers:** fluent reading of the runtime and the compiler output, Go assembly, the memory model and real profiling. Those are the skills for performance and infra roles, and for contributing to Go itself.
- The "Go en el trabajo" boxes point at the service-side skills. The epilogue should point at contributing to `golang/go`.

---

## 11. Open risks

1. **F1 (native fibers under the Go runtime) is unproven.** The SPWRITE/GC-scan rule (`traceback.go:338-366`) and the automatic stack check [V2] make Ki-owned stacks delicate. If R1/R2 cannot make Option A robust, ch 52–54 change shape, and so may Álvaro's decision to write the native work-stealing scheduler himself.
2. **Async preemption never stops assembly** (`preempt.go:450`). A missing back-edge poll in Ki-emitted code would hang Go's stop-the-world phases, not just starve Ki fibers. It needs a test from ch 50 on.
3. **There is no `-ffp-contract=off` in Go** [V11]. Byte-identical floats across backends and platforms rests on a coding rule plus a custom checker. arm64 fuses by default.
4. **`-race` cannot see Ki-emitted asm or Ki-owned stacks.** Concurrency bugs in native code have only litmus tests, seed sweeps and stress as guards.
5. **Docs and toolchain can disagree** (V15: the `go mod init` notes vs 1.26.5). Every version-specific claim in the book should be pinned to a test, like the listings.
6. **The book will be read over 2–3 years, and Go ships twice a year.** Experiments named here (json/v2, simd, the leak profile) will graduate or change. Pin `go.mod` to a toolchain, and re-verify at each part, the way hours are re-planned.
7. **The hours are estimates.** Part VIII carries the widest error bar (±10 h); Part I may come in under.
8. **Using `testing` (std lib) instead of a hand-written test framework needs Álvaro's decision.** The ch 1 hours and the test-card format in §5 and §7 assume yes.
9. **The Kubernetes/etcd practices were answered from memory** (§3.4, §9), not fetched this session. Check them before writing those boxes.
10. **The "Go nuevo" column of §8 is in English** (this research file is in English) and will need translating when merged into the Spanish spec.

---

## 12. Sources

- **Installed toolchain** (Go 1.26.5):
  - `$GOROOT/api/go1.23.txt`–`go1.26.txt`
  - `$GOROOT/src/internal/goexperiment/flags.go:95-131`, `src/internal/buildcfg/exp.go:83-89`
  - `src/cmd/compile/abi-internal.md:12-16,221,398,410-424` (amd64), the arm64 table (≈ 545-553)
  - `doc/asm.html:327,407-413,488,607`; `doc/go_mem.html:54-62,271,604-608`; `doc/go_spec.html:5030,5050-5057`
  - `src/runtime/proc.go:24,445,3389,3443,4737,5623,6486,6628,7662,7730`; `runtime2.go:473,482,618,772,818`; `chan.go:34`; `select.go:122`; `netpoll_epoll.go:21`; `stack.go:133,900,1026`; `preempt.go:445-458`; `coro.go:42,91`; `traceback.go:336-366`; `lfstack.go:14-22`
  - `src/iter/iter.go:231-237,261`; `src/internal/abi/symtab.go:46-60`; `src/unsafe/unsafe.go:39-158`
  - `src/cmd/compile/internal/base/flag.go:352-354`; `src/cmd/go/internal/modload/init.go:1214`
- **Release notes** (fetched): [Go 1.23](https://go.dev/doc/go1.23), [Go 1.24](https://go.dev/doc/go1.24), [Go 1.25](https://go.dev/doc/go1.25), [Go 1.26](https://go.dev/doc/go1.26).
- **Style:** [Google Go Style Guide](https://google.github.io/styleguide/go/) (fetched). Not re-fetched this session: [Uber Go Style Guide](https://github.com/uber-go/guide), [Effective Go](https://go.dev/doc/effective_go), [Code Review Comments](https://go.dev/wiki/CodeReviewComments), [Test Comments](https://go.dev/wiki/TestComments), [module layout](https://go.dev/doc/modules/layout), [GC guide](https://go.dev/doc/gc-guide), [diagnostics](https://go.dev/doc/diagnostics), [PGO](https://go.dev/doc/pgo), [fuzzing](https://go.dev/doc/security/fuzz), [Go 1 compatibility](https://go.dev/doc/go1compat), [Go memory model essay](https://research.swtch.com/gomm).
- **`go mod init` revert:** [golang-nuts thread](https://groups.google.com/g/golang-nuts/c/avgRf3vl9mo), [blog post](https://huizhou92.com/go-1-26-changed-go-mod-init-and-nobody-asked-for-it/); issues #77653, #77860.
- **Spec this maps from:** `/mnt/c/Users/scami/Downloads/ki-diseno-go/base-cpp-design.md` §§1–4, 7–9, 11.4, 12.
