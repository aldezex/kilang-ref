# R3: Perceus reference counting over an own heap inside a Go process

**Topic.** Perceus RC (reuse analysis, drop specialization, borrowing), 63-bit immediate `Int` with `BoxedInt` overflow, over an own heap that Go's garbage collector never manages, shared by the register VM (Go) and the native backend (Go/Plan 9 assembly linked by `go build`). The CEK oracle may use Go's GC.

**Date:** 2026-10-24 · **Toolchain:** `go1.26.5 linux/amd64`, `GOAMD64=v1`, GOROOT `/home/scami/.local/opt/go` · **Machine:** AMD Ryzen 7 7800X3D (8 cores / 16 threads), WSL2, kernel 6.18, `vm.overcommit_memory=1`.

**Labels used throughout.**
- **[VERIFIED]**: I ran a program on this machine; the result is summarized in the text and in Appendix A.
- **[DOCUMENTED]**: stated in the Go source tree (file:line are for the installed Go 1.26.5 tree under `$GOROOT/src` unless another root is given), in go.dev docs, in a paper or in a header of another runtime.
- **[INFERENCE]**: my reasoning from the above. It is not tested.

Scope note: the Ki language does not change (base design §5, §6, §8.1–8.2, §8.9). This report replaces the C++ runtime assumptions of base design §8.1–8.3 and parts of §9.2–9.3 that depend on the host. Fibers, stacks and continuations as a mechanism belong to another research topic. Here they appear only where they constrain the heap.

---

## 0. Verdict in one paragraph

An own Perceus heap inside a Go process is feasible and fast, and the rules that make it safe are few and checkable. Ki values are plain `uint64` words, never pointer-typed. Heap memory is one `mmap` reservation the Go runtime does not know about, and it never contains a Go pointer. Native assembly never runs a loop without a safe point.

On this machine, the own heap with Perceus-style RC beat Go-GC-allocated objects by 4–6× on allocation-heavy workloads with idle cores, and by 5–8× when the GC had no idle cores to hide in (GOMAXPROCS=1). In-place reuse (`rc == 1`) beat Go by 16× on `map` over a unique list.

The price is that every Go safety net goes blind on the Ki heap:
- The race detector ignores it, and atomics on it don't even count as synchronization (verified).
- checkptr tolerates it only because it is not Go memory.
- `go vet` flags each accessor.
- There is no ASan.

So Ki must ship two things from the first runtime chapter:
- its own debug allocator (poison, quarantine, guard pages with `SetPanicOnFault`, allocation sites, exit census, verified to work);
- a debug build that places the same heap inside a pinned Go-heap `[]uint64`. That build keeps the same absolute-address Value encoding through a single `//go:nocheckptr` accessor (verified), so `-race` again sees the VM and the runtime.

The two real long-term risks are both Go internals:
- uintptr→Pointer for non-Go memory is tolerated by the implementation but is not one of the six blessed `unsafe` patterns;
- the native backend's back-edge preemption poll reads `g.stackguard0` at a fixed offset.

Both need a canary test that runs on every Go upgrade.

---

## 1. What carries over from the C++ design, and what Go changes

| C++-era decision (base §8) | In a Go host |
|---|---|
| 64-bit value word, bit0=1 immediate, bit0=0 pointer, never null | **Keeps.** In Go it must be a `uint64` type, never `unsafe.Pointer` (§2.1, V1). |
| 8-byte header, Lean RC encoding `int32 rc` (>0 local, <0 shared, 0 immortal), `kind`, `tag`, `scan` | **Keeps** unchanged (§3, §4). |
| Perceus in Core S4: dup/drop, `dropReuse`, borrowing, suspension sets | **Keeps.** It is backend-independent. |
| mimalloc-style arenas: 4 MiB arenas, 64 KiB pages, ~40 size classes, per-worker free lists, `thread_free` CAS list, `madvise` | **Keeps,** built on `syscall.Mmap`/`Mprotect`/`Madvise` (all in the standard `syscall` package) [VERIFIED V9]. |
| `KI_ALLOC=system` (forward to malloc so ASan sees objects) | **Disappears.** There is no ASan for own memory. It is replaced by the Go-heap debug heap (`-race`-visible) and own debug modes (§6). |
| No `thread_local`, context in `r15` | Go has no user TLS anyway. Go's ABI has **no callee-saved registers** (`cmd/compile/abi-internal.md:221`), so a pinned context register must be reloaded after every call into Go (§5). `R15` is clobbered in dynlink mode (`abi-internal.md:408–416`, table). |
| TSan with fiber annotations, `seq_cst` variant | `-race` works only for Go-heap memory (§2.5). Go atomics are always sequentially consistent (§4). |
| Backend links with `as`/`ld` and `libkirt.a` | The native code is Go assembly in a Go package. `go build` links it with a runtime written in Go (§5). |

---

## 2. The ground rules: what the Go GC, `unsafe`, checkptr, vet and the race detector tolerate

### 2.1 What the GC looks at, and what makes it crash

- **Only pointer-typed slots are scanned.** Pointer bitmaps cover stack frames, globals and heap objects. A `uint64` field is never interpreted [DOCUMENTED: the GC guide says pointer-free values are segregated and "The GC will stop scanning values at the last pointer in the value", <https://go.dev/doc/gc-guide>].
- **A pointer into memory that was never Go heap is ignored.** In `runtime.findObject`, `spanOf(p)` returns nil for such addresses. The comment reads: "If s is nil, the virtual address has never been part of the heap. This pointer may be to some mmap'd region, so we allow it." [DOCUMENTED `runtime/mbitmap.go:1361–1373`]. A pointer into a Go span in the wrong state calls `badPointer` → `throw("found bad pointer in Go heap (incorrect use of unsafe or cgo?)")` [DOCUMENTED `mbitmap.go:1376–1388`, `1308–1334`].
- **Small integers in pointer-typed locals crash the stack copier.** When a goroutine stack grows, `adjustpointers` throws `invalid pointer found on stack` if a pointer-typed local holds a value in (0, 4096) [DOCUMENTED `runtime/stack.go:676–681`; `minLegalPointer = 4096` in `runtime/malloc.go:329`; `GODEBUG=invalidptr=1` is the default, `runtime/extern.go:177–181`]. Two details make this nastier:
  - argument slots are adjusted with an empty `funcInfo{}`, so the check is skipped for them (`stack.go:746`);
  - the write barrier silently filters such values (`runtime/mwbbuf.go:231`).

  A tagged immediate in a pointer-typed variable is therefore an **intermittent** fatal error. It depends on whether the value happens to sit in a local slot when the stack happens to grow.
- **[VERIFIED V1]**
  - A `Value uint64` holding the immediate `42<<1|1` in a heap object survived 5 forced GCs.
  - An odd value ≥ 4096 stored in an `unsafe.Pointer` heap field survived a GC (spanOf nil).
  - `unsafe.Pointer(uintptr(3))` (Ki `Int 1`) passed down as an *argument* through 2,000 and 20,000 growing frames did **not** crash.
  - The same value held in a pointer-typed *local* across a recursive call did crash: `runtime: bad pointer in frame main.grow at 0x25e55cba3d60: 0x3` / `fatal error: invalid pointer found on stack`, exit 2.

**Rule 1.** A Ki value is `type Value uint64` everywhere in Go: in structs, slices, locals, channels and VM registers. An `unsafe.Pointer` exists only as a transient inside the heap accessors (`ptr(v)` and the field loads and stores built on it).

### 2.2 The six valid `unsafe.Pointer` patterns, and where Ki stands

From `unsafe/unsafe.go:39–170` [DOCUMENTED]:
1. `*T1` → `Pointer` → `*T2`, when the layouts are compatible (e.g. `math.Float64bits`).
2. `Pointer` → `uintptr`, but not back. "A uintptr is an integer, not a reference … the garbage collector will not update that uintptr's value if the object moves, nor will that uintptr keep the object from being reclaimed."
3. `Pointer` → `uintptr` → arithmetic → `Pointer`, **in one expression**, staying inside the original allocation. One past the end is invalid. Storing the `uintptr` in a variable first is invalid.
4. `Pointer` → `uintptr` in the argument list of `syscall.Syscall`-like assembly calls.
5. The result of `reflect.Value.Pointer`/`UnsafeAddr` converted immediately.
6. `reflect.SliceHeader`/`StringHeader` `Data` ↔ `Pointer`, which is deprecated in favour of `unsafe.Slice`, `unsafe.String`, `unsafe.SliceData` and `unsafe.StringData`.

`unsafe.Add(ptr, len)` is pattern 3 without the `uintptr` dance ("The rules for valid uses of Pointer still apply", `unsafe.go:221–227`).

**Where Ki stands.** A Ki pointer value is an integer stored in memory and converted back to a `Pointer` later. That is exactly what patterns 2 and 3 forbid **for Go memory**. For memory that is not Go memory (an `mmap` region), none of the six patterns applies: the doc says "Code not using these patterns is likely to be invalid today or to become invalid in the future" (`unsafe.go:30–33`). In practice the runtime tolerates it on purpose (the `findObject` comment above, plus `reflect_verifyNotInHeapPtr` right after it in `mbitmap.go:1397–1400`). This is the foundational risk of the whole approach (§10, risk R1). It is the same bet cgo-heavy programs and `mmap`-based databases make every day [INFERENCE].

### 2.3 checkptr (`-d=checkptr`, implied by `-race`, `-msan`, `-asan`)

- **What is instrumented.**
  - Every `uintptr → unsafe.Pointer` conversion goes through `walkCheckPtrArithmetic` [DOCUMENTED `cmd/compile/internal/walk/convert.go:26–28, 481–540`].
  - So does `unsafe.Add` (`walk/expr.go:134–142`).
  - `unsafe.Slice` and `unsafe.String` (`walk/builtin.go:790–796, 892–896`).
  - `(*T)(p)` conversions get `checkptrAlignment`.
- **Implied by the sanitizers.** `-race`, `-msan` and `-asan` imply `-d=checkptr` unless it is set explicitly (`cmd/compile/internal/base/flag.go:351–356`). The history is golang/go#34964 (enable with -race/-msan) and #22218 (the original flag).
- **What is checked** (`runtime/checkptr.go`):
  - `checkptrArithmetic` throws "pointer arithmetic computed bad pointer value" for results in (0, 4096).
  - If the result lies in a Go heap object, stack, data or bss, one of the *original* pointer operands must point into the same object, or it throws "pointer arithmetic result points to invalid allocation" (`checkptr.go:50–70`).
  - If `checkptrBase(p) == 0`, i.e. the result is not Go memory, it returns silently (`checkptr.go:59–61, 89–115`).
  - `checkptrAlignment` rejects misaligned conversions only when `T` contains pointers (issue #37298), and rejects conversions that straddle two allocations (`checkptr.go:9–28`).
- **Escape hatch.** `//go:nocheckptr` on a function disables the instrumentation in that function. The flag's original commit describes it, and I verified that it is honoured in user code (V8).

**[VERIFIED V2]** One binary per case, run with and without `-gcflags=all=-d=checkptr`:

| Case | checkptr | plain build |
|---|---|---|
| (a) absolute address into `mmap` memory, stored as `Value`, converted back | ok | ok |
| (b) absolute address into a Go-heap `[]uint64` chunk, stored, converted back | `fatal error: checkptr: pointer arithmetic result points to invalid allocation` | ok (silently "works") |
| (c) `unsafe.Add(chunkBase, off)` with `chunkBase` a real `unsafe.Pointer` | ok | ok |
| (d) `unsafe.Add(chunkBase, len+4096)` (escapes the chunk) | `fatal error: … invalid allocation` | ok (silently wrong) |
| (e) dereferencing an immediate (`Int 1` = `0x3`) | `fatal error: checkptr: pointer arithmetic computed bad pointer value` | `panic: … nil pointer dereference` |
| (f) `(*node)(unsafe.Pointer(&mm[3]))`, misaligned, `node` has a pointer | `fatal error: checkptr: misaligned pointer conversion` | ok |

**[VERIFIED V8]** With `//go:nocheckptr` on the single accessor `ptr(v Value) unsafe.Pointer`, case (b) runs clean under `-race`, and the race detector still reports races through it.

Consequences:
- An `mmap` heap with absolute addresses is checkptr-clean.
- A Go-heap heap with absolute addresses needs exactly one `//go:nocheckptr` accessor.
- Case (e) is a free debug check on small immediates only, and only for conversions outside a `//go:nocheckptr` function. Since the recommended `ptr` accessor is `//go:nocheckptr` (§9), add an explicit `if v&1 != 0 { panic }` in debug builds of `ptr`.

### 2.4 `go vet` (`unsafeptr`)

The analyzer "reports likely incorrect uses of unsafe.Pointer to convert integers to pointers … because that word will be invisible to stack copying and to the garbage collector" [DOCUMENTED `cmd/vendor/golang.org/x/tools/go/analysis/passes/unsafeptr/doc.go`; registered in `cmd/vet/main.go:44, 97`].

**[VERIFIED V1, V2, V5]** It flags every `unsafe.Pointer(uintptr(v))` accessor (`possible misuse of unsafe.Pointer`). `go vet -unsafeptr=false` passed on the assembly package, which means `asmdecl` (the frame-offset checker for `.s` files with Go prototypes) was clean.

**Rule 2.** All integer→pointer conversions live in one package (`kirt/heap`, a few dozen lines). CI runs `go vet ./...` with full analyzers on everything except that package, and `go vet -unsafeptr=false ./kirt/heap`. No other package may import `unsafe` (a grep test).

### 2.5 The race detector

- **Addresses outside the Go heap and data are ignored.** `isvalidaddr` accepts only `[racearenastart, racearenaend)` (grown by `racemapshadow` when Go maps heap arenas) or `[racedatastart, racedataend)` [DOCUMENTED `runtime/race.go:442–445, 528–536`]. The amd64 instrumentation skips other addresses before calling TSan (`runtime/race_amd64.s:153–165`).
- **Atomics on such addresses are executed between `__tsan_go_ignore_sync_begin/end`.** That means they create **no happens-before edge** (`race_amd64.s:405–423`).

**[VERIFIED V3]** `-race`, two goroutines:

| Case | Result |
|---|---|
| unsynchronized `+=` on a word in an `mmap` heap | **no report**, exit 0 |
| same on a word in a Go-heap `[]uint64` | `WARNING: DATA RACE`, exit 66 |
| correct publication of a Go variable through an atomic flag that lives in the `mmap` heap | **false positive** `DATA RACE` on the Go variable, exit 66 |
| same with the flag in the Go-heap chunk | clean |

Consequences:
- In release layout, `-race` cannot be Ki's TSan.
- Worse, any Go-side synchronization that goes through Ki-heap atomics (a shared thunk's CAS publishing Go-side state, a `thread_free` list feeding Go structures) produces false reports.
- Hence the debug Go-heap build (§9). The race detector **never** instruments assembly, so the native backend is not covered in any mode. The VM and the Go runtime are.

### 2.6 Heap representations compared (task item 1)

| | **A. one big Go `[]uint64`, handles = word index** | **B. `mmap` reservation, Value = absolute address** | **C. Go-heap chunks, Value = absolute address** |
|---|---|---|---|
| GC scanning | none (noscan object); the whole array is one live object | none; invisible | none (noscan) |
| Moves? | Go's heap is non-moving ("Go has a non-moving GC", GC guide); growing by `append` **copies**, so handles survive and raw addresses don't | never | never; `runtime.Pinner` gives a documented "not moved or freed" guarantee (`runtime/pinner.go:38–49`) while the Pinner stays reachable (`pinner.go:24–27`) |
| checkptr | clean if accessed by index | clean (V2a) | needs a `//go:nocheckptr` accessor (V2b, V8) |
| `-race` visibility | yes | **no** (V3) | yes (V3, V8) |
| GOGC pacing | **distorted**: counts as live heap (V7: a 256 MiB arena → heap goal 512 MiB; benchmarks: 1.3 → 0.027 GC/op) | unaffected | distorted, as A |
| GOMEMLIMIT | counted | **not counted** (V7: 256 MiB touched, runtime total unchanged at 12 MiB) | counted |
| Bounds checks | yes; cost not measurable here (4.34 vs 4.49 ms with `-B`, noise) | none | none |
| Asm addressing | base register + index (fine on x86-64; arm64 needs an extra ADD for base+index+disp) | `[v+disp]`, one register on both arches | as B |
| Growth | realloc (stop the world) or pre-size | commit on demand (`mprotect`), uncommit with `madvise` (V9) | add chunks; needs a contiguous reservation or a chunk table |

**Recommendation:** B for release, C for debug, with **the same Value encoding** (absolute addresses), so that no other code differs between the two (§9). A is the right model for the CEK oracle's `--level=rc` checked heap (ids never reused, tombstones), because there safety beats speed.

On `syscall.Munmap`: the `mmapper` only unmaps exactly the slice that `Mmap` returned (`syscall/syscall_unix.go:43–48, 72–84`). Ki should reserve once, never unmap, and use `Mprotect` and `Madvise(MADV_DONTNEED)` per arena [VERIFIED V9: a 64 GiB `PROT_NONE` reservation cost 0 RSS; committing and touching 4 MiB added 4 MiB RSS; `MADV_DONTNEED` returned it, and the page then read back as 0].

Linux overcommit: this machine runs `vm.overcommit_memory=1`, so even a 64 GiB read-write `MAP_NORESERVE` mapping succeeded. Under strict accounting (mode 2) the kernel ignores `MAP_NORESERVE`, so the reservation must be `PROT_NONE` with `mprotect` per arena, as the Go runtime itself does (Linux `Documentation/mm/overcommit-accounting.rst`; I did not verify this here, since it needs mode 2).

---

## 3. Object layout for Ki values (task item 2)

### 3.1 The value word

```go
type Value uint64 // bit0 = 1: immediate; bit0 = 0: address of an 8-aligned own-heap object (never 0)
```

- **Immediates:** `Int` that fits in 63 bits as `n<<1 | 1`; `Bool`, `Char`, `Byte`, `()` and nullary constructors as small indices encoded the same way (base §8.1). The encoding is canonical, so equality of immediates is word equality. Signed order is preserved: `2a+1 < 2b+1 ⇔ a < b`, so `<` on two immediates is one signed compare [INFERENCE, elementary].
- **Pointers:** absolute addresses inside the Ki reservation. In the debug build they are absolute addresses inside the Go-heap arena.
- **Forbidden:** pointers into the Go heap, the Go stack, or Go data or bss (§3.4).

### 3.2 The header (8 bytes, unchanged from base §8.1)

```go
type header struct {
	rc   int32  // >0 fiber-local (plain ops); <0 thread-shared, magnitude -rc (atomic); 0 immortal
	kind uint8  // Ctor, Closure, String, Array, BoxedInt, BoxedFloat, Thunk, Cont, Chan, Resource, (debug) Freed
	tag  uint8  // constructor tag
	scan uint16 // number of Value fields from the kind's first scanned field
}
```

- This is Lean 4's encoding, quoted from `lean.h`: "`m_rc` … encodes whether the object is single threaded (> 0), multi threaded (< 0), or reference counting is not needed (== 0)". `lean_is_exclusive` is `rc == 1` on single-threaded objects, and `lean_dec_ref` takes the fast path when `rc > 1`, else `lean_dec_ref_cold` when `rc != 0` [DOCUMENTED <https://github.com/leanprover/lean4/blob/master/src/include/lean/lean.h>].
- Koka differs: refcount 0 means *unique*, > 0 is a non-thread-shared count, and < 0 is "thread-shared or sticky" (overflow makes it sticky, never freed). Its header is `{uint8 scan_fsize; uint8 _field_idx; uint16 tag; _Atomic(int32) refcount}` [DOCUMENTED <https://github.com/koka-lang/koka/blob/master/kklib/include/kklib.h>].
- Ki keeps Lean's encoding as decided (base §15 #29). The `rc == 1` test for uniqueness costs the same as Koka's `rc == 0` on x86 (`CMPL $1`). Saturation at `INT32_MAX` makes the object immortal: a leak, never a use-after-free (base §8.1).

**Layout as code.** In Go the struct is 8 bytes with these offsets. The **single source of truth** should be a `layout.go` with `const` offsets and sizes. A unit test compares them with `unsafe.Offsetof` and `unsafe.Sizeof`, and the code generator writes the same constants into the generated `.s`. `go_asm.h` (the assembler's auto-generated header) only covers types of the *same* package, so it cannot serve the generated program package [INFERENCE from how `go_asm.h` is generated per package].

### 3.3 Kinds

| Kind | Layout (words after the header) | Notes |
|---|---|---|
| Ctor | `f0 … f(n-1)` (all Values; `scan = n`) | `scan` lets one generic drop handle every ADT; drop specialization (§3.5) avoids reading it. |
| BoxedInt | `int64` (`scan = 0`) | Only for values outside [−2⁶², 2⁶²−1], so it is canonical. Equality must compare payloads when both are boxed; an immediate and a box are never equal. |
| BoxedFloat | `float64` bits | Boxed in v0 as in base §8.1; `math.Float64bits` is pattern 1. |
| String | `len uint64`, then bytes, padded to 8 | Go interop: `unsafe.Slice((*byte)(unsafe.Add(ptr(v), 16)), len)` passed to `os.File.Write` is fine (a non-Go pointer inside a slice is ignored by the GC). **Never** create a Go `string` with `unsafe.String` that can outlive the Ki string (e.g. a map key): when the Ki string is freed, the Go string dangles [INFERENCE from `unsafe.go:262–263`]. |
| Array | `len`, `cap`, then Values | `scan` is u16, so arrays use the length word, not `scan`. |
| Closure | `code`, `info` (arity, flags), then captured Values | Captures come after two raw words. Drop needs a per-kind "first scanned field" (Ctor 0, Closure 2), or else code addresses would need tagging. `code` holds a function index in the VM and a code address in native code. **[VERIFIED V5]** An asm code address taken with `LEAQ ·kiDouble(SB), AX`, stored as an integer and called with `CALL AX` works. |
| Thunk | `state` (unevaluated / evaluating / done), `fn` (closure Value), `result` | A shared thunk is published by CAS on `state` (base §5.10). |
| Cont | own-heap address of the first captured segment, saved Ki-SP and PC, handler record | Segments are own-heap blocks of frames. Their owned slots are described by the suspension maps (base §6.1 S4). Dropping a live Cont walks those maps. The exact frame representation depends on the fiber topic (§5.5). |
| Chan, Resource | `handle uint64` index into a **Go-side table** (`scan = 0`) | Go objects (mutexes, waiter queues, `*os.File`) cannot live in the own heap (§3.4). The proxy's last drop frees the table slot. |

### 3.4 Never a Go pointer inside the Ki heap

**[VERIFIED V7]** A Go object whose only reference was its address stored in the `mmap` heap was collected after two `runtime.GC()` calls (observed with `runtime.AddCleanup`).

The same happens if the Ki heap is a noscan `[]uint64`. Hence the handle table for Chan and Resource: Go objects stay reachable from Go. Go's internal not-in-heap marker (`internal/runtime/sys.NotInHeap`) is not importable outside the standard library, so this rule cannot be enforced by types. It must be enforced by keeping every struct overlaid on Ki memory pointer-free. A test can walk those structs with `reflect` and fail if any field is a pointer, slice, map, string, interface or func [INFERENCE].

### 3.5 Perceus pieces that touch the runtime

- **dup/drop inline.** Local `dup` is `if rc > 0 { rc++ } else if rc != 0 { atomic.AddInt32(rc, -1) }`. **[VERIFIED]** The Go inliner gives this cost 34 out of a budget of 80, so it inlines into the VM loop.
- **Drop specialization** (Perceus §2.3; Reinking et al., PLDI 2021, MSR-TR-2020-42 <https://www.microsoft.com/en-us/research/publication/perceus-garbage-free-reference-counting-with-reuse/>). For a known constructor the compiler emits "if unique, drop the children and free, else decrement", with no reading of `scan` and no worklist. **[VERIFIED B1]** A specialized list drop was 4.59 ms vs 4.80 ms for the generic drop per 10⁶-cell list. The gain is small here because the generic drop is already tight.
- **Generic drop is iterative.** The worklist is threaded through the dead cells' header words (base §8.2). **[VERIFIED]** The benchmark implementation frees a 2¹⁹-node tree and 10⁶-element lists with no recursion and no Go allocation.
- **Reuse.** `dropReuse x` yields a token (the cell itself) when `rc == 1`. `K@tok(…)` initializes it in place. Reuse is only for `rc == 1` local cells (base §8.2). **[VERIFIED B2]** `map (+1)` over a unique list took 1.35 ms with reuse, 3.27 ms without it (allocate + free), and 22.2 ms with Go GC objects.

  Later improvements are compiler-side and change nothing in the runtime: drop-guided, frame-limited reuse (Lorenzen & Leijen, ICFP 2022, doi:10.1145/3547634) and FIP functions (Lorenzen, Leijen & Swierstra, ICFP 2023, doi:10.1145/3607840).
- **Borrowing** (Ullrich & de Moura, "Counting Immutable Beans", IFL 2019, arXiv:1908.05647) removes dup/drop pairs on borrowed parameters. In the benchmarks `ownSum`/`ownCheck` are borrowed traversals with zero RC traffic.

### 3.6 The 63-bit `Int` fast path in Go and in asm

Go has no overflow intrinsic for signed add. For tagged operands `a = 2x+1` and `b = 2y+1`, compute `r = a + (b−1)`; it overflows iff `((a^r) & ((b−1)^r)) < 0`.

**[VERIFIED]** A unit test covers the boundaries ±2⁶², ±2⁶²−1 and mixed cases. It checks that results fitting 63 bits are exact and that all others take the slow path.

**[VERIFIED, inlining]** The first version, `func addTagged(a, b) uint64 { …; return addSlow(a, b) }`, is **not inlinable** (`cannot inline addTagged: function too complex: cost 103 exceeds budget 80`): a call to a non-inlinable function costs too much. Splitting it into `addFast(a, b) (uint64, bool)` (inlinable) and a caller-side `if !ok { r = addSlow(a, b) }` inlines.

Measured per add over 65,536 values:

| Variant | ns/add |
|---|---|
| raw `int64` | 0.53 |
| tagged, inlined | **0.79** |
| tagged, not inlined | 1.48 |

So **inlining matters more than the tag check**. This is a teaching point for the VM chapters: check with `-gcflags=-m=2`, and pin the decision with a CI grep.

In native code the fast path is `MOVQ a, CX; ANDQ b, CX; TESTQ $1, CX; JZ slow; LEAQ -1(b), DX; ADDQ a, DX; JO slow` [INFERENCE; this is the base §9.3 plan in Go asm syntax]. Multiplication: untag one operand (`SARQ $1`), `IMULQ` with `JO`. Division checks for zero and `MIN/−1` first (base §9.3). The slow path is a Go function: it computes in 64 bits, normalizes to an immediate or a `BoxedInt`, and panics on 64-bit overflow, which keeps the semantics of base §5.10.

---

## 4. Atomic vs non-atomic RC for values shared across fibers and threads (task item 3)

- **Model (unchanged from base §8.2).** Objects are local (`rc > 0`) until `ki_share` (at `spawn`, `send`, and shared-thunk publication) walks them and flips them to shared (`rc → −rc`). Shared never points to local. Reuse needs `rc == 1`. Fiber migration needs no atomics, because handoffs are release/acquire edges. In Go that means the deque CAS or the channel mutex, all seq_cst (Go memory model).
- **Go atomics are seq_cst only.** "All the atomic operations executed in a program behave as though executed in some sequentially consistent order … the same semantics as C++'s sequentially consistent atomics" [DOCUMENTED `$GOROOT/doc/go_mem.html:598–609`]. There are no `relaxed` or `acq_rel` variants in `sync/atomic`. The runtime's own `internal/runtime/atomic` has `…Acq`/`…Rel` variants, but they are internal.

  **[VERIFIED by disassembly, amd64]**

  | Operation | Instruction | Remark |
  |---|---|---|
  | `atomic.AddInt32` | `LOCK XADDL` | the same instruction an `acq_rel` RMW would use, so no loss for RC |
  | `atomic.LoadInt32` | plain `MOVL` | |
  | `atomic.StoreInt32` | `XCHGL` | more expensive than a release `MOV` |

  The asm backend may emit anything (plain `INCL`, `LOCK XADDL`), so the atomic/non-atomic split lives in Ki's `rc` sign, not in Go's API. On arm64, Go's atomics use LSE instructions (`SWPAL`, `LDADDAL`, …) when `ARM64HasATOMICS` is set, else `LDAXR`/`STLXR` loops [DOCUMENTED `internal/runtime/atomic/atomic_arm64.s:125–140`].
- **Costs measured here [VERIFIED B3]** (per dup, over 10⁶ objects, 32 MB, inside L3):

  | Variant | ns |
  |---|---|
  | plain `rc++` | 0.69 |
  | Lean check, local objects | **0.69** (the sign check is free) |
  | Lean check, all shared (uncontended `LOCK XADD`) | 2.47 |
  | always atomic | 3.21 |

  Contention, measured as each goroutine doing atomic −1/+1 on **the same** object:

  | Goroutines | ns per atomic |
  |---|---|
  | 1 | 1.8 |
  | 4 | 20.9 |
  | 16 | **119** |
  | 16, each on its own 128-byte-separated object | 3.8 |

  This is why widely shared immutable values (a global `Map`, a dictionary record) should become **immortal** (`rc == 0`) when possible: top-level constants after initialization, as in base §8.1. Otherwise they become a cache-line ping-pong under 16 workers [INFERENCE from the numbers].
- **Freeing a shared object.** It is freed by whichever thread drops it to zero. That thread pushes the cell onto the owner page's `thread_free` list with `atomic.CompareAndSwapUint64` on an own-heap word. Atomics on arbitrary aligned own-heap addresses work (benchmarks and V3). Remember §2.5: in the release layout these atomics are invisible to `-race`.

---

## 5. How native Go-asm code, the Go runtime and the VM share the heap (task item 4)

### 5.1 Calling conventions that matter

- **Asm is ABI0.** Ki-generated `.s` files are ABI0 (stack-based Go ABI). Only `runtime` may use ABI selectors: `ABI selector only permitted when compiling runtime` [DOCUMENTED `cmd/asm/internal/asm/parse.go:899`]. ABI0 asm may freely trash `R14` (g) and `X15` (zero). The generated wrapper restores them on the way into Go [DOCUMENTED `abi-internal.md:400–425`].

  **[VERIFIED V5]** An asm function set `R14=0xdeadbeef` and `X15=−1`, then `CALL ·goDouble(SB)`. The Go function, which allocates and calls `fmt.Sprint`, worked. The wrapper `main.goDouble.abi0` contains `XORPS X15, X15; MOVQ FS:0xfffffff8, R14; CALL main.goDouble(SB)`.
- **No callee-saved registers** in Go's ABI (`abi-internal.md:221`). Every call from Ki asm into Go clobbers all registers, so Ki's pinned registers (worker context, heap base if any) must be reloaded from the frame after each runtime call. Ki-to-Ki calls are asm-to-asm, so KiCC (base §9.3) remains Ki's own business inside asm: register arguments and pinned registers are invisible to Go and need no stack maps, because they hold no Go pointers.
- **Frame and stack-map rules for asm** [DOCUMENTED `$GOROOT/doc/asm.html`, section "Runtime coordination", around lines 560–610]:
  - Every asm function needs a Go prototype; `go vet`'s asmdecl checks the offsets against it.
  - A function with a frame that calls anything must declare `NO_LOCAL_POINTERS`. "Because stack resizing is implemented by moving the stack, the stack pointer may change during any function call: even pointers to stack data must not be kept in local variables." (`asm.html:607–610`).

  For Ki this is easy, because frames hold only `uint64` Values and own-heap addresses, which are not Go pointers. It also forbids Ki from holding raw pointers *into its own goroutine stack* across a call (frame pointer chains must be recomputed, not stored) [INFERENCE].
- **The assembler inserts the Go prologue automatically.** For non-`NOSPLIT` functions it emits `MOVQ (TLS), R14; CMPQ SP, 16(R14); JBE morestack`, plus `PUSHQ BP; MOVQ SP, BP` when the frame is non-zero [VERIFIED by `go tool objdump`]. Small leaf functions with no calls and a frame < `StackSmall` are **silently marked NOSPLIT** (`cmd/internal/obj/x86/obj6.go:662–692`), so they get no prologue at all.

### 5.2 Preemption and stop-the-world: the hidden coupling with the Go GC

- **Async preemption never applies to assembly.** `isAsyncSafePoint` returns false when `f.flag & FuncFlagAsm != 0` ("This is assembly code. Don't assume it's well-formed.") [DOCUMENTED `runtime/preempt.go:450–458`; the design is golang/go#24543]. Cooperative preemption happens in function prologues, through `g.stackguard0 = stackPreempt` [DOCUMENTED `runtime/runtime2.go:476–483`: "stackguard0 … can be StackPreempt to trigger a preemption … offset known to liblink"]. Sysmon requests preemption of a goroutine after 10 ms (`proc.go:6628`, `forcePreemptNS`).
- **[VERIFIED V4]**

  | Variant | `runtime.GC()` from another goroutine |
  |---|---|
  | `NOSPLIT` asm loop, no calls, ~1.7 s | took **1.64 s** (the whole STW waited) |
  | same loop polling `MOVQ (TLS), R14; CMPQ SP, 16(R14); JLS yield` on its back-edge, where `yield` calls a Go function that does `runtime.Gosched()` | **1.12 ms** (GOMAXPROCS=16), 20 ms at GOMAXPROCS=1, 85 yields over 1.7 s |

- **Consequences.**
  1. Every Ki native function that is not a tiny leaf keeps the Go prologue (do not mark it `NOSPLIT`). That gives Go's stack growth and preemption at each call for free.
  2. Every loop back-edge (`joinrec`, self tail calls) polls `g.stackguard0` the way the prologue does. This replaces base §8.5's own stack-limit trick, and Ki's 10 ms fiber quantum can simply reuse Go's sysmon request [INFERENCE].
  3. The poll depends on the **offset 16 of `g.stackguard0`** and on TLS access from user asm. Both work today and are what the assembler itself hardcodes, but neither is a public API (§10, risk R2).
  4. A single missing poll in native code stalls Go's GC STW, and with it every goroutine of the process. kigen should include long non-allocating loops, with a test that times `runtime.GC()` while they run.

### 5.3 The allocation fast path: in asm, with Go as the slow path

- **Worker context in own memory.** Each worker has a context struct holding size-class free-list heads, bump and limit pointers, page bookkeeping and the preemption flag. It lives **in own memory**. Go code reaches it as `(*workerCtx)(ptr)` with integer-only fields, and asm reaches it through a pinned register reloaded after calls.
- **[VERIFIED V5]** Building and then dropping a 10⁶-cell list from asm:

  | Variant | ns/cell |
  |---|---|
  | inlined pop or bump in asm | **2.04–2.08** |
  | asm calling a Go `allocGo` for every cell | 3.56–3.68 (the ABI0 wrapper and call add ~1.5 ns per allocation) |

  The slow path (`refill`, a Go function) was called 31 times per run without problems. Inline the fast path; call Go only to refill a page.
- **Allocation in Go.** On the Go side (VM and runtime), `alloc()` with free-list pop and bump had inliner cost 41 [VERIFIED `-m=2`], so it inlines into the VM's `ALLOC` opcode handler.

### 5.4 How the VM uses the heap

- **VM state.** Registers, frames and the constant pool are `[]Value` (noscan Go slices) or own-heap register windows (base §8.4, "VM fibers"). Field access goes through `heap.Field(v, i)` and the like. These are inlinable accessors around `ptr(v)`, with `uintptr(v) + 8 + 8*i`.
- **Parking.** A VM fiber parks by returning `Park` to the dispatch loop. Its live Values sit in own-heap segments or in noscan slices, and neither needs anything from the GC.
- **Benchmark pitfall [VERIFIED].** Calls written syntactically inside a `for b.Loop() { … }` body were not inlined. The body's variables are kept alive with a compiler-inserted `runtime.KeepAlive` (`testing/benchmark.go:485–491`). Hot inner loops must be moved into helper functions, or the measurement lies.

### 5.5 Where native Ki frames live: a constraint passed to the fiber topic

Two options are compatible with this heap design. Either:
- (i) Ki frames live on goroutine stacks; Go moves and grows them, and continuations need one goroutine per handler segment or a frame copy; or
- (ii) Ki frames live in own-heap segments addressed by a dedicated Ki stack register, while `RSP` stays on the goroutine stack for calls into Go. Then a `Cont` is just a chain of own-heap segments, exactly as in the VM.

Pointing `RSP` itself at own memory would break Go's assumptions (stack bounds in morestack, tracebacks, signal handling) [INFERENCE; not tested here]. The heap requirement either way: provide segment allocation, and have drop of a `Cont` walk the suspension maps.

---

## 6. Detecting leaks and use-after-free in tests (task item 5)

There is no ASan for an own heap:
- `-race` ignores it (V3).
- Go's `-asan` and `-msan` need cgo and a C sanitizer runtime. Poisoning own memory would need `__asan_poison_memory_region` through cgo, which breaks the "standard library only" rule [INFERENCE].
- Go 1.25+ has Valgrind client requests in the runtime behind `-tags valgrind` (`runtime/valgrind.go`, written in asm without cgo). They describe Go's own allocator only. Ki could emit memcheck client requests the same way, but Valgrind is not installed here, so this is unverified and optional.

What Ki should build, all of it hand-written with the standard library:

1. **`KI_ALLOC=debug`** (base §8.3, adapted):
   - on free, set `kind = Freed` and poison the payload with `0xDD…`. Do not poison the rc word: `0xDDDDDDDD` as `int32` is negative and would look like a valid shared count;
   - in debug builds, every dup and drop checks `kind != Freed`, and a hit panics with the allocation and free sites;
   - FIFO quarantine of N MiB before reuse;
   - double free detected by freeing a `Freed` block;
   - allocation-site ids: `runtime.Callers` on the Go side; on the asm side, an immediate site id passed to a debug-only slow allocation call;
   - **exit census**: live objects by site must be zero, as an observable of the checked modes (base §6.3).
2. **Electric-fence mode** (`KI_ALLOC=fence`): one object per page, the page `PROT_NONE` on free, and `debug.SetPanicOnFault(true)` on each worker goroutine. The setting is per goroutine (`runtime/debug/garbage.go:139–155`).

   **[VERIFIED V6]** A read of a freed object became a recoverable `runtime error: invalid memory address or nil pointer dereference` whose `Addr()` equals the object's address exactly. The recovery handler can therefore map the address to its allocation site.
3. **Debug Go-heap build** (`-tags kiheapgo`, §9): the same allocator inside a pinned `[]uint64`, so `go test -race` covers the VM and the Go runtime (V3 chunk, V8). The accessor `ptr` is `//go:nocheckptr`, which disables checkptr inside it. Dereferencing an immediate is therefore caught by the explicit `if v&1 != 0 { panic }` in the debug `ptr`, not by checkptr. checkptr still catches small immediates (V2 e) in any conversion written outside `ptr`.
4. **The oracle's `--level=rc` checked heap** (base §9.1): Core+RC is interpreted over a Go-GC-managed object table with ids never reused, tombstones, and provenance per allocation and per drop. It is independent of `kirt/heap`, so a bug in the real allocator cannot hide behind the same bug in the checker.
5. **Differential testing:** `interp`, `interp-rc`, `vm` and `native` must agree on the observables and on a zero census in the checked modes (base §6.3). kigen should abandon continuations at every `perform` (base §10a).

---

## 7. Performance: own RC heap vs letting Go's GC manage Ki values (task item 6)

All numbers are [VERIFIED] on this machine: medians of 5 runs (3 for GOMAXPROCS=1), `go test -bench`, `testing.B.Loop`, default GOGC=100, Green Tea GC (the 1.26 default: `GreenTeaGC: true` in `internal/buildcfg/exp.go:88`, <https://go.dev/doc/go1.26>).

The own heap here is deliberately minimal: one 32-byte size class, a free list, bump allocation and Lean RC checks, with no remote frees and no page management. The Go side uses ordinary `*cons` (16 B) and `*node` (16 B) objects.

| Workload (per op) | Go GC, 16 Ps | Own heap | Go GC, GOMAXPROCS=1 | Own, GOMAXPROCS=1 |
|---|---|---|---|---|
| build + sum + drop a 10⁶-cell list | 26.6 ms (1.3 GC/op, 3.8 % GC CPU) | 4.59 ms (spec. drop), 4.80 ms (generic drop), 4.26 ms (`[]uint64` handles) | 36.6 ms (**34.5 % GC CPU**) | 4.68 ms |
| `map (+1)` over a unique 10⁶ list | 22.2 ms | **1.35 ms** with reuse; 3.27 ms without | 37.8 ms (**40.6 % GC CPU**) | n/m |
| binary tree depth 18 (524,287 nodes): build, check, drop | 10.9 ms (1.4 GC/op) | 2.96 ms | 14.8 ms (~15 % GC CPU) | 2.90 ms |
| same, with 8 M **live** objects (depth-22 tree) retained | 11.0 ms (0.06 GC/op, but each GC marks 8 M objects) | 3.02 ms | 15.6 ms (~23 % GC CPU) | n/m |

**How to read the table.**
- **Allocation cost dominates.** With the GC almost never running (the KI_DISTORT run: a 512 MiB noscan arena in the Go heap raised the heap goal, and GC/op fell from 1.3 to 0.027), the Go list still took 18.0 ms. Most of Go's cost is `mallocgc` for pointerful objects: size-class lookup, heap-bitmap/span bookkeeping and zeroing. Marking is the smaller part [INFERENCE from the two runs].
- **The idle-core effect.** At 16 Ps the GC's background workers run on otherwise idle cores, so wall time hides them (3.8 % of total CPU). At GOMAXPROCS=1, the regime of a Ki program whose work-stealing scheduler keeps every core busy, GC CPU is **34–41 %** on the list workloads. A Ki program with all 16 workers busy would pay that on top [INFERENCE; the GC guide states the GC takes 25 % of CPU in the mark phase plus assists, <https://go.dev/doc/gc-guide>].
- **Live heap size.** Go's cost grows with the live heap (marking), while RC cost grows with the RC operations performed. Perceus with borrowing keeps those operations low (here: zero on traversals).
- **Reuse is the big win.** It is 16× vs Go and 2.4× vs Perceus without reuse, because the cells never leave the cache.
- **Cheap parts.** Bounds checks on the `[]uint64` variant were not measurable (4.34 vs 4.49 ms with `-gcflags=-B`). The Lean sign check on local objects is free (0.69 vs 0.69 ns). Uncontended atomics cost ~3.5×, and contended ones up to 170× (§4).

**What these benchmarks do not show.**
- Real Ki code has more dup/drop traffic than these hand-written loops. The drop of a deep structure is O(n) at the moment of the last drop: a latency spike Go's concurrent GC would spread out.
- The own heap never returned memory to the OS here.
- Treat the factors as an upper bound for allocation-bound code, not a promise for whole programs [INFERENCE].

**The oracle on Go's GC** is the right call: no RC, no unsafe, Go structs and interfaces, and it stays slow and correct (base §9.1). Running the VM on Go-GC objects with counts on top was not benchmarked. It would keep exact counts (so reuse would still work), but you would pay Go's allocation price on every non-reused cell and get no benefit from freeing. That combines both costs [INFERENCE].

---

## 8. What the book teaches about Go along the way

Proposed placements map onto the base §12.5 parts (Go replaces the "C++ nuevo" column):

| Part / chapter (base numbering) | Go topics forced by this design |
|---|---|
| VI-34, the memory model (before the first atomic) | `go_mem.html`: happens-before, data races, **only seq_cst atomics** (vs C++ and Rust), `sync/atomic` typed atomics (`atomic.Int32`), the race detector and **what it cannot see** (V3 as a lab), litmus tests in Go. |
| VI-35, the object model | `unsafe.Sizeof/Offsetof/Alignof`, the **six patterns**, `unsafe.Add/Slice/String/SliceData/StringData`, `-d=checkptr`, `go vet unsafeptr`, `GODEBUG=invalidptr`, "never an integer in a pointer slot" (V1 lab), `math.Float64bits`, layout tests, build tags for the debug heap. |
| VI-36, your own allocator | `syscall.Mmap/Mprotect/Madvise`, `MAP_NORESERVE`, overcommit, Treiber stack with `CompareAndSwapUint64`. **Go's GC from the inside:** tri-color concurrent mark-sweep, non-moving, hybrid write barrier, pacer formula (live + (live + roots)·GOGC/100), GOMEMLIMIT and what it does not count (V7), Green Tea, `runtime/metrics`, `GODEBUG=gctrace=1`. **Escape analysis** with `-gcflags=-m` for keeping the Go runtime allocation-free on hot paths. Benchmarks with `b.Loop` and its inlining pitfall. |
| VI-37, RC in the runtime | iterative drop, Lean encoding, `atomic.AddInt32` codegen (`LOCK XADD`) read with `go tool objdump`, contention (B3 as a lab), cache lines and padding. |
| VI-39, the VM loop | the inliner budget (80) with `-gcflags=-m=2`, fast and slow path splitting (the tagged-add lesson), bounds-check elimination (`-d=ssa/check_bce`), `pprof` CPU profiles. |
| VIII-46…54, native | Plan 9 asm syntax, ABI0 vs ABIInternal, wrappers, `TEXT` flags (`NOSPLIT`), frame sizes, `NO_LOCAL_POINTERS`, Go prototypes and `go vet` asmdecl, stack growth and moving stacks, cooperative vs async preemption (V4 as a lab), TLS and `g`, `go tool objdump`/`nm`, `runtime.SetPanicOnFault`. |
| Epilogue / "Go at work" boxes | `runtime.Pinner` and cgo rules (why Ki doesn't use cgo), `AddCleanup`/`weak` (Go 1.24), the shelved `arena` experiment (#51317, on indefinite hold per the memory-regions design doc <https://go.googlesource.com/proposal/+/HEAD/design/70257-memory-regions.md>), the internal `runtime.freegc` behind `GOEXPERIMENT=runtimefreegc` (`runtime/malloc.go:1895–1929`, "The intended caller is the compiler"). |

---

## 9. Recommended design

1. **`kirt/heap`: the only package that imports `unsafe`.**
   - `type Value uint64`.
   - Accessors `ptr(v)`, `Load(v, off)`, `Store(v, off, x)`, `RC(v) *int32`, `Bytes(v) []byte`, all inlinable (cost ≤ 80, pinned by a CI check on `-gcflags=-m` output).
   - `ptr` is `//go:nocheckptr` and, in debug builds, panics on immediates.
   - vet runs with `-unsafeptr=false` only here.
2. **Release reservation.** One `syscall.Mmap(PROT_NONE, MAP_PRIVATE|MAP_ANON|MAP_NORESERVE)` of `KI_HEAP_GB` (default 64 GiB), never unmapped. 4 MiB arenas are committed with `Mprotect(RW)` and uncommitted with `Madvise(MADV_DONTNEED)`. Pages are 64 KiB with the header at `p &^ 0xFFFF` and about 40 size classes (base §8.3). Values are **absolute addresses**.
3. **Debug reservation (`-tags kiheapgo`).** The same allocator over one `make([]uint64, N)` held by a global and pinned with a `runtime.Pinner` that is kept alive. Fresh memory is not touched, so a 4 GiB arena cost 2.8 ms and no RSS [VERIFIED V9]. Values are still absolute addresses. `-race` sees the VM and runtime, and Ki-heap atomics synchronize correctly. Accept the distorted GC pacing in this mode.
4. **Header and RC.**
   - The base §8.1 header with the Lean encoding.
   - Inline local dup and drop.
   - Shared path with `sync/atomic` on the Go side and `LOCK XADDL` in asm.
   - `ki_share` flips to negative counts.
   - Saturation goes to immortal.
   - Top-level constants immortal.
5. **No Go pointer in Ki memory, ever.** Chan and Resource are own-heap proxies holding indices into Go-side tables. Structs overlaid on Ki memory are pointer-free, enforced by a `reflect` test.
6. **Allocator fast path inline** in asm and in Go. The Go slow path refills pages. Remote frees go through a CAS `thread_free` list per page.
7. **Native code rules.**
   - Non-leaf Ki functions keep the Go prologue.
   - Back-edges poll `g.stackguard0` and call a Go `yield`.
   - `NO_LOCAL_POINTERS` everywhere.
   - Reload pinned registers after every Go call.
   - Never use `R15` for pinning if `-dynlink` is ever possible.
   - Asm-level layout constants are generated from `layout.go`.
8. **Debug tooling from the first runtime chapter:** `KI_ALLOC=debug|fence`, the exit census, and differential tests including `interp-rc`.
9. **The oracle** uses Go's GC; its checked RC heap is an independent table implementation.
10. **Own memory accounting.** Expose `KI_HEAP_MAX` and Ki-heap metrics, because GOMEMLIMIT doesn't see the Ki heap (V7). Process RSS = Go runtime memory + Ki committed arenas.

---

## 10. Risks

| # | Risk | Evidence | Mitigation |
|---|---|---|---|
| R1 | uintptr→Pointer for non-Go memory is **not** one of the six patterns. It is tolerated by the implementation (`mbitmap.go:1363–1364`) but formally "likely to be invalid … in the future" (`unsafe.go:30–33`). | §2.2 | One accessor package; canary tests (V1, V2, V3, V7) re-run on every Go upgrade; the debug Go-heap variant as a fallback that is closer to the rules. |
| R2 | The preemption poll reads `g.stackguard0` at offset 16 via TLS from user asm: internal ("offset known to liblink", `runtime2.go:482`). | V4 | A canary test that requests a GC while native loops run and asserts the latency; a fallback poll on a Ki-owned flag set by a monitor goroutine (worse latency). |
| R3 | One missing back-edge poll stalls Go's STW for the whole process (1.64 s in V4). Asm is never async-preemptible (`preempt.go:450`). | V4 | Codegen invariant checked by an MIR verifier ("every loop has a poll"); a kigen loop corpus; a `runtime.GC()` latency test. |
| R4 | `-race` is blind on the release heap, and Ki-heap atomics yield **false positives** on Go data. | V3 | Debug Go-heap build; never publish Go-side state through Ki-heap atomics in the release layout. |
| R5 | No ASan: use-after-free and leaks are found only by Ki's own tools. | §6 | Debug, fence and census modes; interp-rc; kigen; V6 shows faults can name the address. |
| R6 | A Go pointer stored in Ki memory is a silent use-after-free of a Go object. | V7 | Handle tables; pointer-free overlay structs enforced by a `reflect` test; no `unsafe` outside `kirt/heap`. |
| R7 | A tagged immediate in a pointer-typed Go variable gives an **intermittent** fatal error (only in local slots, only on stack growth). | V1 | Rule 1 (`Value uint64`); vet; code review rule. |
| R8 | Hot helpers silently stop inlining as they grow (cost 103 > 80). | §3.6 | CI greps of `-gcflags=-m` for the named helpers; fast/slow split as the house style. |
| R9 | Benchmarks are micro and synthetic, and the 16-P numbers hide GC work on idle cores. | §7 | Re-measure with real Ki programs in Part IX. Report GOMAXPROCS=1 and fully loaded numbers. |
| R10 | Overcommit mode 2 or containers with low limits: a 64 GiB reservation, and Ki memory invisible to GOMEMLIMIT and cgroup-aware tuning. | V9, V7 | `PROT_NONE` reservation, configurable size, own `KI_HEAP_MAX`. |
| R11 | Ports. Windows has no `syscall.Mmap`: use `CreateFileMapping` + `MapViewOfFile` (in `syscall`) or `VirtualAlloc` through `syscall.NewLazyDLL("kernel32.dll")`; `SetPanicOnFault` behaviour differs. AArch64 has no base+index+displacement addressing, and LSE atomics depend on the CPU. | `syscall/zsyscall_windows.go:497, 951`; `atomic_arm64.s:132` | Absolute addresses (no base register); keep the OS layer in one file per OS. |
| R12 | The fiber design (frames on goroutine stacks vs own segments) changes the `Cont` layout and the cost of capture. | §5.5 | Decide in the fiber topic before Part VI. The heap provides segments either way. |

---

## 11. What must be decided early (before the first runtime chapter)

1. **Value = absolute address** (recommended) vs base-relative offset. This affects asm addressing on arm64, the debug heap, and any future 32-bit compression.
2. **Two reservations behind one API** (`mmap` release, pinned Go-heap debug), with the build-tag names and the CI matrix (`-race` runs only the debug heap).
3. **The `kirt/heap` boundary**: the only `unsafe` importer, the vet policy, and the `//go:nocheckptr` accessor.
4. **Header layout and per-kind first-scanned-field rule** (Closure captures after `code`/`info`), as `layout.go` constants plus the generated asm constants and a layout test.
5. **Go-object references**: handle tables for Chan and Resource (and anything else Go-native), with their lifetime tied to the proxy's RC.
6. **The preemption protocol for native code**: the Go prologue on non-leaf functions plus `g.stackguard0` polls on back-edges, with the canary test from R2.
7. **The fiber and frame placement decision** (§5.5), because `Cont` and segment allocation in the heap depend on it.
8. **Debug allocator modes and the census as test observables** from day one, since there is no ASan.
9. **Inlining discipline for VM hot paths** (fast/slow split, CI checks).
10. **Memory limits and metrics for the Ki heap** (`KI_HEAP_GB`, `KI_HEAP_MAX`), since GOMEMLIMIT is blind to it.

---

## Appendix A. Verification transcripts (summarized)

All programs lived in `/tmp/claude-1000/-home-scami/144ea2a3-9681-4392-8a95-4f8de2e00caa/scratchpad/r3-perceus/` (module `kiscratch`). That directory was deleted at the end.

- **V1** `go build ./v1 && ./v1/v1` → `uint64 immediates in heap objects: survived 5 GCs, v = 85`; `odd non-heap value in unsafe.Pointer heap slot: survived GC`.
  - The argument-slot variant through 2,000 and 20,000 frames returned normally.
  - The local-slot variant `./v1b/v1b` → `runtime: bad pointer in frame main.grow at 0x25e55cba3d60: 0x3`, `fatal error: invalid pointer found on stack`, exit 2.
  - `go vet` → `possible misuse of unsafe.Pointer` on both conversions.
- **V2** checkptr matrix: see the table in §2.3. Built with `-gcflags=all=-d=checkptr` and without.
- **V3** `go build -race ./v3`:
  - `mmap` → `mmap done 3`, exit 0 (no report);
  - `chunk` → `WARNING: DATA RACE … Found 1 data race(s)`, exit 66;
  - `mmap-sync` → `WARNING: DATA RACE`, exit 66 (false positive);
  - `chunk-sync` → clean.
- **V4**
  - `./v4/v4 nosplit` → `runtime.GC() took 1.641599s while loop ran 1.691s (yields=0)`;
  - `./v4/v4 poll` → `runtime.GC() took 1.12ms while loop ran 1.657s (yields=85)`;
  - `GOMAXPROCS=1 … poll` → `20.438ms`.
  - `go tool objdump` showed the auto-prologue `MOVQ FS:0xfffffff8, R14; CMPQ SP, 0x10(R14); JBE`.
- **V5**
  - `callCode(&kiDouble, 21) = 42 (R14/X15 trashed before the call)`;
  - `inline build+drop 1e6 cells: best 2.04–2.08 ms`; `callGo … 3.56–3.68 ms`; `refills=31`.
  - The `-race` build ran (30 / 52 ns per cell: Go-side drop instrumented but filtered).
  - The wrapper `main.goDouble.abi0` contains `XORPS X15, X15; MOVQ FS:0xfffffff8, R14`.
  - `go vet -unsafeptr=false` was clean (asmdecl ok).
- **V6** → `recovered: runtime error: invalid memory address or nil pointer dereference` / `fault addr=0x717c9c829ff0 obj=0x717c9c829ff0 same=true`.
- **V7**
  - `baseline runtime-total=12 MiB heap-goal=4 MiB`;
  - after the 1 GiB `mmap` with 256 MiB touched: unchanged;
  - after `make([]uint64)` of 256 MiB: `runtime-total=268 MiB heap-goal=512 MiB heap-live=256 MiB`;
  - `Go object referenced only from the Ki heap was collected: true`.
- **V8** `-race` with the `//go:nocheckptr` accessor into a pinned Go-heap arena → `absolute-address Value into Go-heap arena under checkptr: 5`, exit 0; with racing writers, `WARNING: DATA RACE`.
  - Disassembly: `atomic.StoreInt32` → `XCHGL`; `atomic.AddInt32` → `MOVL $-0x1, CX; LOCK XADDL`; `atomic.LoadInt32` → `MOVL`.
- **V9**
  - `vm.overcommit_memory=1`;
  - `64 GiB PROT_NONE reservation: true rss 2576 kB`;
  - `mprotect RW + touch 4 MiB: true rss 6696 kB`;
  - `madvise DONTNEED: true rss 2604 kB page still reads 0`;
  - `64 GiB RW MAP_NORESERVE: true`;
  - `make 4 GiB []uint64: 2.759ms rss 7596 kB`.
- **Benchmarks** (`bench/bench_test.go`). `go test -run TestAddTagged` → ok. Benchmark binaries run in separate processes:
  - GoGC alone;
  - GoGC with `KI_DISTORT=1`;
  - Own/Dup/Shared/Add;
  - `-test.cpu 1`;
  - a `-gcflags=-B` build.

  Medians are in §3.6, §4 and §7. Two mistakes in my first run were found and corrected before these numbers:
  - a 512 MiB Go-heap arena allocated at package init distorted the GC pacing of the Go benchmarks (now a deliberate experiment);
  - calls written directly inside `b.Loop` bodies were not inlined.

## Appendix B. Sources

**Go source tree (1.26.5, installed).** Paths relative to `$GOROOT/src`, except the two `doc/` files, which are relative to `$GOROOT`:
- `unsafe/unsafe.go:20–170, 221–264`
- `runtime/checkptr.go:9–115`
- `runtime/mbitmap.go:1308–1400`
- `runtime/stack.go:652–681, 738–746`
- `runtime/mwbbuf.go:222–240`
- `runtime/malloc.go:324–329, 353–355, 1895–1929`
- `runtime/race.go:417–445, 528–536`
- `runtime/race_amd64.s:152–166, 385–423`
- `runtime/preempt.go:346–362, 390–458`
- `runtime/runtime2.go:475–487, 514`
- `runtime/proc.go:6626–6628`
- `runtime/pinner.go:14–49, 399–402`
- `runtime/extern.go:177–181`
- `runtime/debug/garbage.go:139–155`
- `runtime/valgrind.go`
- `cmd/compile/internal/base/flag.go:345–375`
- `cmd/compile/internal/walk/convert.go:20–32, 481–540`
- `cmd/compile/internal/walk/expr.go:128–145`
- `cmd/compile/internal/walk/builtin.go:790–796, 892–896`
- `cmd/compile/abi-internal.md:221, 400–425`
- `cmd/internal/obj/x86/obj6.go:662–696, 873–898`
- `cmd/asm/internal/asm/parse.go:899`
- `cmd/vet/main.go:44, 97`
- `cmd/vendor/golang.org/x/tools/go/analysis/passes/unsafeptr/doc.go`
- `syscall/syscall_unix.go:43–48, 72–84`
- `syscall/zsyscall_linux_amd64.go:1005, 1021`
- `syscall/zsyscall_windows.go:497, 951`
- `internal/buildcfg/exp.go:83–89`
- `internal/goexperiment/flags.go:116–117`
- `internal/runtime/atomic/atomic_arm64.s:125–140`
- `testing/benchmark.go:479–501`
- `$GOROOT/doc/asm.html` ("Runtime coordination", around lines 560–610)
- `$GOROOT/doc/go_mem.html:598–609`

**Go docs.**
- Go 1.26 release notes: Green Tea default, heap base randomization, cgo overhead. <https://go.dev/doc/go1.26>
- A Guide to the Go Garbage Collector. <https://go.dev/doc/gc-guide>
- The Green Tea blog post. <https://go.dev/blog/greenteagc>
- Memory regions design (arenas on hold). <https://go.googlesource.com/proposal/+/HEAD/design/70257-memory-regions.md>
- Non-cooperative preemption design. <https://go.googlesource.com/proposal/+/HEAD/design/24543-non-cooperative-preemption.md>

**Go issues.**
- golang/go#34964 (checkptr with -race and -msan) and #22218 (the original `-d=checkptr`)
- #37298 (alignment check only for pointerful types)
- #24543 (non-cooperative preemption)
- #46787 (runtime.Pinner)
- #67535 (AddCleanup)
- #73581 (Green Tea)
- #51317 (arena proposal)

**Papers.**
- Reinking, Xie, de Moura & Leijen, "Perceus: Garbage Free Reference Counting with Reuse", PLDI 2021 / MSR-TR-2020-42 (v4, 2021-06-07). <https://www.microsoft.com/en-us/research/publication/perceus-garbage-free-reference-counting-with-reuse/>
- Ullrich & de Moura, "Counting Immutable Beans", IFL 2019, arXiv:1908.05647.
- Lorenzen & Leijen, "Reference Counting with Frame Limited Reuse", ICFP 2022, doi:10.1145/3547634.
- Lorenzen, Leijen & Swierstra, "FP²: Fully in-Place Functional Programming", ICFP 2023, doi:10.1145/3607840.

**Other runtimes.**
- Lean 4 `src/include/lean/lean.h` (`lean_object`, `m_rc` encoding, `lean_inc_ref`/`lean_dec_ref`/`lean_is_exclusive`). <https://github.com/leanprover/lean4/blob/master/src/include/lean/lean.h>
- Koka `kklib/include/kklib.h` (`kk_header_t`, refcount encoding, `kk_block_is_unique`, `kk_block_is_thread_shared`). <https://github.com/koka-lang/koka/blob/master/kklib/include/kklib.h>
