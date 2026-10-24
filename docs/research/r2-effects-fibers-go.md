# R2: one-shot deep handlers, fibers, channels, `select`, work stealing and deterministic mode with Go as the host

**Date:** 2026-10-24 · **Toolchain checked:** Go 1.26.5 linux/amd64 (`$GOROOT=/home/scami/.local/opt/go`), AMD Ryzen 7 7800X3D, 16 logical CPUs, WSL2 kernel 6.18 · **Scope:** the Ki language is unchanged (C++-era design §5.6, §5.11, §6.2, §8.4–8.8, §9, §10). Only the host changes: the oracle, the VM and the runtime are written in Go, and the native backend emits Go (Plan 9) assembly that `go build` links against a Ki runtime written in Go.

**How to read the evidence labels**
- **[verified]**: I ran it on this machine. Section 12 has the transcripts.
- **[source]**: Go source tree file and line under `$GOROOT/src` (Go 1.26.5), or a Go document shipped in `$GOROOT/doc`.
- **[paper]**: a published paper. Where I read the text, I quote it.
- **[inference]**: my reasoning or a design proposal that I have not prototyped.

---

## 0. The answer in one page

1. **Generated Ki assembly runs on goroutine stacks, so it can never switch stacks.** Go assembly is "ABI0 user assembly" from the runtime's point of view. Three things follow:
   - the runtime may **move** the stack at any call;
   - a function that writes `SP` breaks the unwinder (and is fatal at the next GC, **[verified]**);
   - assembly is **never async-preempted**.

   The C++ design's native continuations (segment chains plus a saved `rsp`) are impossible. Its native fibers on `mmap` stacks are impossible too. Every native suspension must be **stackless**: the frames are unwound into heap objects.
2. **Use the same effect lowering for the VM and the native code: generalized evidence passing with yield bubbling**, as in Koka's C backend (Xie & Leijen, ICFP 2021).
   - Make it a Core stage, **S5 = Core+RC+EV**, after Perceus, so the CEK oracle can run it and `--check-passes` compares S4 with S5.
   - One-shot is the easy case: a continuation is a chain of ordinary heap closures, so dropping it is an ordinary Perceus drop. The suspension set of each call site, which S4 already computes, is exactly what that call site's continuation closure captures.
   - Ki's continuations can be resumed after their `handle` has returned (generators). Those are not "scoped resumptions", so Ki needs the *generalized* scheme (ICFP 2021), not the scoped one of "Effect handlers, evidently" (ICFP 2020).
3. **Make fibers heap states run by Ki's own work-stealing scheduler in both the VM and native code** (option A, recommended).
   - A VM fiber is a VM state: its frames are Go slices, and it parks in place.
   - A native fiber is a root continuation. Blocking `Async`/`IO` operations bubble up to the fiber root, and the root returns `Park` to the worker loop.
   - So one scheduler, one channel/`select` implementation, one poller, one deadlock detector and one `DetScheduler` serve both compiled backends. The runtime `Run(fiber) → Park | Done | Panic` interface is identical for both.
   - The documented fallback is **option B**: fibers are goroutines and the Go scheduler runs them. It is simpler and portable, but the scheduler you write then exists only in the VM.
4. **The CEK oracle uses a Go slice of frames:**
   - capture = `slices.Clone` of the frames above the handler, then `clear` and truncate;
   - resume = `append` (one-shot checked through the object table);
   - fibers = CEK states on its own independent deterministic scheduler, with **no goroutines at all**.
5. **Deterministic mode must never use the Go scheduler, Go's `select`, map iteration order, `time.Now` or `math/rand`.**
   - Go has no deterministic scheduling mode.
   - `testing/synctest` virtualizes time but not interleavings: 6 different orders in 300 bubbles, **[verified]**.
   - The §8.8 procedure is implemented twice: once in the oracle and once in `kirt` (shared by the VM and native code).
6. **Measured costs (Go prototypes) [verified]:**
   - plain call: 1.1 ns;
   - tail-resumptive perform through evidence: 3.8 ns;
   - control perform+resume at depth 1, 10 and 100: 65, 135 and 803 ns;
   - the yield checks on the direct path cost nothing measurable;
   - `iter.Pull` round trip: 51 ns;
   - unbuffered channel ping-pong: 227 ns;
   - baton pass between goroutines: 217 ns. **With `runtime.LockOSThread` on both goroutines it is 126,000 ns (~580× slower), so worker goroutines must not be locked.**
   - On a spawn tree of 635,621 run-to-completion fibers, the scheduler you write took 14–22 ns per fiber; one goroutine per fiber took 77–218 ns.

The rest of this document gives the evidence, the design for each backend, the order in which the book builds it (§10) and the risks (§11).

---

## 1. The governing constraint: Ki native code is ABI0 user assembly under the Go runtime

These facts shape the whole native backend. Each one invalidates or reshapes a piece of the C++-era §8.4 and §9.3.

### 1.1 What the runtime assumes about every frame on a goroutine stack

| Fact | Evidence | Consequence for Ki |
|---|---|---|
| A goroutine stack can be **moved** (copied to a block twice as big) whenever a function's prologue finds too little space. The same check is how preemption is requested (`stackguard0 == stackPreempt`). | [source] `runtime/stack.go:1093` (`preempt := stackguard0 == stackPreempt`), `:1123-1145` (synchronous preemption → `gopreempt_m`), `:1147-1178` (`newsize := oldsize * 2`, then copy) | No Ki code may hold the address of a stack location across a call. "Capture = remember `rsp`" cannot work. [verified]: 10⁶-deep asm recursion grew the stack to 48 MiB by copying, with concurrent GCs (P1). |
| The asm doc says it directly: "Because stack resizing is implemented by moving the stack, the stack pointer may change during any function call: even pointers to stack data must not be kept in local variables." | [source] `doc/asm.html:608-610` | Same as above. |
| Every frame with locals that makes calls needs a locals pointer map; without one, stack copying or GC throws `missing stackmap`. | [source] `runtime/stkframe.go:194-195` (locals), `:221-222` (args); `doc/asm.html:561-610` | Every generated function must say `NO_LOCAL_POINTERS`, and every function with arguments needs a **Go prototype** (pointer information for the args). [verified]: the same function without `NO_LOCAL_POINTERS` died in `copystack → adjustframe → getStackMap` with `fatal error: missing stackmap` (P1). The design depends on Ki values being plain words (`uintptr`) into a Ki heap that lives **outside the Go heap**. That assumption is shared with the Perceus/allocator research. |
| Any instruction that writes `SP` other than `PUSH`/`POP`/`ADJSP` marks the function `SPWRITE`. During a precise traceback (GC, stack copy), finding a non-innermost `SPWRITE` frame is fatal. | [source] `cmd/internal/obj/x86/obj6.go:760-776` (auto-SPWRITE), `runtime/traceback.go:338-366` ("we might not even be on the stack we think we are" … `throw("traceback")`) | **Stack switching from generated code is impossible**, and even a no-op `MOVQ AX, SP` is forbidden. [verified]: `MOVQ SP, AX; MOVQ AX, SP` followed by a call that triggers GC gives `traceback: unexpected SPWRITE function main.writesSP / fatal error: traceback`, exit 2 (P10). |
| Assembly functions are never asynchronously preempted. | [source] `runtime/preempt.go:450-458` (`f.flag&abi.FuncFlagAsm != 0` → "This is assembly code. Don't assume it's well-formed." → not a safe point) | A Ki loop with no call and no prologue blocks stop-the-world for the **whole process**, and with it the GC. [verified]: a 2-second asm loop made `runtime.GC()` from another goroutine wait **2.02 s**. With a back-edge poll, GC took **1.06–1.38 ms** (P2). In the C++ design back-edge polls were an optimization. Here they are a **liveness requirement**. |
| The prologue inserted by the assembler (when the function has a frame and is not NOSPLIT) loads `g` from TLS **into R14**, compares `SP` with `g.stackguard0` (offset 16), and on failure calls `runtime.morestack_noctxt`. `morestack` saves only PC, SP, BP and DX (the closure context) in `g.sched`, and `gogo` restores only those. | [source] `obj6.go:873-900` (`loadG`: on amd64 `regg = REGG // == REG_R14`), `:914-1122` (`stacksplit`), `runtime/asm_amd64.s:628-683` (`morestack`), `:441-452` (`gogo<>`); [verified] objdump of P3: `MOVQ FS:0xfffffff8, R14; CMPQ SP, 0x10(R14); JBE …` | **No argument can arrive in a register** at a splittable Ki function. R14 is clobbered at entry, and everything except DX is lost if `morestack` runs. The C++ KiCC (args in `rdi…r9`, closure in `r10`, worker context pinned in `r15`) is **gone**. Ki-to-Ki calls pass arguments on the stack (Go's ABI0 shape). A single register (DX) could survive if the function is marked `NEEDCTXT` ([source] `obj6.go:1095-1100` picks `morestack` instead of `morestack_noctxt`) [inference]. Register arguments remain possible only into `NOSPLIT` leaves [inference]. |
| ABI selectors (`<ABIInternal>`) are only permitted when compiling the runtime. | [source] `cmd/asm/internal/asm/parse.go:899` | Ki asm is ABI0, full stop. |
| ABI0 and ABIInternal interoperate through compiler-generated **wrappers**, which are only generated for references from the *same package's* assembly (the symabis file). | [source] `cmd/compile/abi-internal.md:12-16`, `:413-424` (R14 = g, X15 = zero in ABIInternal, undefined in ABI0) | [verified] P3: `CALL kiproj∕kirt·Alloc(SB)` from package `main` failed to link: `relocation target kiproj/kirt.Alloc not defined for ABI0 (but is defined for ABIInternal)`. A same-package Go trampoline `func kiAlloc(n uintptr) uintptr { return kirt.Alloc(n) }` works. The assembler also rejected a dotted module path in a symbol (`example.com∕p3∕kirt·Alloc` → `expected '(', found .`). **So the generated package needs a Go file with prototypes, plus either same-package trampolines or a `kirt`-side `.s` reference that forces ABI0 entry points.** The companion report R1 (`r1-go-asm-backend.md`) verified both fixes independently. The wrapper costs 2.39 ns per call against 1.22 ns for asm→asm ([verified] P11; its body is `XORPS X15,X15; MOVQ FS:-8,R14; CALL main.goInc`). |
| A small frame with no `CALL` is silently made NOSPLIT. A cycle of such functions is then rejected by the linker's nosplit check. | [source] `obj6.go:661-690` (`autoffset < abi.StackSmall` and no `CALL` → `AttrNoSplit`); `internal/abi/stack.go:14` (`StackNosplitBase = 800`) | [verified] P3: two mutually tail-calling functions with `$8` frames gave `main.isEven: nosplit stack over 792 byte limit`. The fix is to make the function non-leaf (an unreachable `CALL`) or give it a frame ≥ `StackSmall` (128). The code generator must enforce this for every function that loops through tail calls. |
| **Tail calls are supported:** `RET ·target(SB)` becomes "tear down this frame, then `JMP target`". | [source] `obj6.go:818-855` (epilogue then `if p.To.Sym != nil { // retjmp p.As = obj.AJMP }`) | [verified] P3: objdump shows `ADDQ $0x8, SP; POPQ BP; JMP main.isOdd.abi0`. 10⁸ mutual tail calls ran in 105 ms with `StackInuse` constant at 320 KiB. Each hop re-enters the callee's prologue, so **self and mutual tail-call loops are Go preemption points for free**. The callee's arguments must fit in the caller's incoming argument area. The likely answer is a fixed-size argument area per arity class [inference]. |
| The assembler inserts the BP save/restore itself when the frame size is > 0. | [source] `doc/asm.html:752-755`, `obj6.go:636-647, 699-715` | Ki's C++ prologue `push rbp; mov rbp, rsp; sub rsp, N` is replaced by the `TEXT ·f(SB), $N-M` declaration. The generator must never adjust SP by hand (use `ADJSP` if ever needed: `obj6.go:810-813`). |
| Stack overflow is `throw("stack overflow")`: fatal and unrecoverable, exit code 2. The default maximum is 1 GB on 64-bit. | [source] `runtime/stack.go:1171-1178`; `runtime/proc.go:160` (`maxstacksize = 1000000000`) | [verified] P11, with `debug.SetMaxStack(64<<20)`: `runtime: goroutine stack exceeds 67108864-byte limit … fatal error: stack overflow`, exit 2. Ki's exit code 103 cannot be produced natively unless the generated code keeps its own depth counter. See risk R6. |

### 1.2 What survives from the C++ native design and what does not

| C++-era element (§8.4, §9.3) | Status with Go hosting |
|---|---|
| 16 MiB `MAP_NORESERVE` stack segments, 1 GiB for `main`, guard pages | **Gone.** The Go runtime grows goroutine stacks itself (2 KiB minimum, [source] `stack.go:78`; adaptive starting size, `runtime1.go:410`, `stack.go:1386`). |
| `ki_switch`, `ki_perform`, `ki_resume` trampolines that swap `rsp` | **Impossible** (SPWRITE, stack moving). Replaced by evidence passing and yield bubbling (§3). |
| Prologue `cmp rsp, [r15+LIMIT]` for overflow and preemption | **Provided by the assembler** for every splittable function (it checks `g.stackguard0`). Go uses it for growth and preemption. Ki's own preemption needs its own flag (§4.4). |
| `r15` pinned to the worker context | **Gone.** Nothing survives a call. Keep `ctx` in a frame slot and reload it after each call [inference]. |
| Frametable section with return-address offsets for dropping continuations | **Not needed for drops** (continuations become heap closures, §3). For backtraces, Go's own unwinder already walks asm frames (`runtime.Callers`). Mapping PCs to Ki source lines could use `#line` directives in the generated `.s` [inference, not verified]. |
| Guaranteed tail calls (`leave; jmp`) | **Kept**, as `RET ·f(SB)` [verified]. |
| All registers caller-saved across calls | **Kept.** It is forced anyway (ABI0: "There are no callee-save registers", [source] `abi-internal.md:221`). |

---

## 2. The oracle: a CEK machine in Go

The oracle is the easy case, because its continuation is an ordinary Go data structure: one-shot capture is just moving frames.

### 2.1 Recommended data structures

- **Control and environment.** The `ctl` is a Core expression node, or a value being returned. The environment is a persistent `*Env` chain, or a slice of slots indexed by de Bruijn level after S1. The oracle must stay obviously correct, so prefer the chain.
- **Continuation stack:** `stack []Frame`. A `Frame` is a closed sum written the Go way:
  - an interface with an unexported marker method (`type Frame interface{ frame() }`);
  - one struct per kind: `letFrame`, `argFrame`, `matchFrame`, `handlerFrame{h *Handler, id ContID}`, `promptReturnFrame`, …;
  - dispatch by a type switch.

  This teaches Go interfaces and type switches. Speed is irrelevant here (the oracle may be 10–50× slower than the VM).
- **`perform L.op(v̄)`:**
  1. Scan `stack` downward for the nearest `handlerFrame` whose label is L. The oracle never masks: it never takes the tail-resumptive shortcut (§6.2 of the base design).
  2. Let `h` be its index. Then:
     ```go
     k := &Cont{frames: slices.Clone(stack[h:]), state: Live}
     clear(stack[h:])     // drop references so Go's GC does not keep them alive
     stack = stack[:h]
     ```
  3. Register `k` in the object table with a fresh, never-reused id (`σ[κ ↦ Live]`), and run the clause.
- **`resume(κ, v)`:**
  1. If `σ(κ) == Used`, panic with `continuation resumed twice` (exit 101).
  2. Otherwise `stack = append(stack, k.frames...)`, `k.frames = nil`, `σ[κ ↦ Used]`, and continue with value `v`.
- **[drop] in `--level=rc`:** when the checked heap sees the last reference to a `Live` κ die, release the owned slots recorded in the suspension sets of its frames. No Ki code runs.

The cost is O(depth up to the handler) per capture and per resume. That is fine for an oracle.

**Go pitfall that Rust would have caught for you:** `k.frames = stack[h:]` *without* the clone aliases the stack's backing array. The next `append` onto `stack` would then overwrite the captured frames, because `append` "re-uses the underlying array" when capacity allows ([source] `doc/go_spec.html:7408`). Capture must copy. This is a good early lesson on slice aliasing for a reader coming from Rust.

A segmented alternative (one Go slice per `handle`, linked by parent pointers, O(1) relinking) mirrors the old native design. The oracle does not need it.

### 2.2 Fibers and concurrency in the oracle

- A fiber is a CEK state `{ctl, env, stack, id}`. The oracle multiplexes them on **its own deterministic scheduler** (≈300–400 lines, independent of `kirt`): sequential ids, FIFO wakeups, `runnable[rng() % n]` sorted by id, splitmix64, a virtual clock.
- **No goroutines, no Go channels, no Go `select`** in the oracle. Its determinism then holds by construction rather than by discipline.
- Its channel model (buffers, FIFO wait queues, closing, the two-pass `select`) is written from `docs/semantics.md` and shares no code with `kirt`. This preserves the C++-era blind-spot coverage (§6.3 of the base design: a runtime bug shared by the VM and the native code is caught by the oracle's independent model).
- Go-specific determinism hazards to ban in the oracle, the runtime's `DetScheduler` and the trace printer:
  - **map iteration order is randomized** (iterate sorted keys or use slices);
  - `select` among Go channels chooses "via a uniform pseudo-random selection" ([source] `go_spec.html:6995`);
  - `math/rand` default sources;
  - `time.Now`.

### 2.3 The oracle must also run S5

The oracle executes every stage S0–S4 (base design §6.1). Add **S5 (Core+RC+EV, §3)**: it needs a handful of extra primitives (evidence lookup, `yieldTo`, `isYielding`, `yieldExtend`, prompt). With S5 in the oracle, `--check-passes` localizes a bug in the effect lowering to the pass itself, before any VM or native code exists (§8).

---

## 3. One effect lowering for both compiled backends: generalized evidence passing with yield bubbling

### 3.1 What the technique is (from the paper)

Xie & Leijen, *Generalized Evidence Passing for Effect Handlers (or, Efficient Compilation of Effect Handlers to C)*, PACMPL 5 (ICFP 2021), Article 71, doi:10.1145/3473576 [paper; I read the text]. The paper refines effect handlers through multi-prompt delimited control, generalized evidence passing and **yield bubbling**, down to "a monadic translation into plain lambda calculus which can be compiled efficiently". Its §2.11 describes Koka's C backend almost verbatim as what Ki's native backend needs:

> "we always return results normally assuming Pure, and set a (thread-local) flag to indicate yielding (in which case the actual returned value is ignored). Moreover, every function has one extra parameter that holds the (thread-local) context ctx which contains the current evidence vector (ctx→w), and the yielding flag (ctx→is_yielding)."

The generated C checks `if (ctx->is_yielding) { yield_extend(&join2, ctx); return 0; }` after each effectful call. When the effect row is known, evidence is read at a static offset (`ctx->w[0]`, "Koka uses canonical evidence vectors"). For a polymorphic row it is found dynamically (`find_ev(ctx->w, tag)`). The paper says the check after every effectful call "seems quite cheap on modern processors and the condition can be predicted well". The rest of a function after a call is a **join point**, shared between the fast path and the resumption path, which keeps code growth linear (§2.10: from 2^N to 2N). Koka "does a type-selective transformation and leaves out monadic binds for functions that are total (since those will never yield)".

**Why Ki needs the *generalized* version.** The earlier evidence translation (Xie, Brachthäuser, Hillerström, Schuster & Leijen, *Effect Handlers, Evidently*, ICFP 2020) restricts resumptions to **scoped resumptions**. GEP §2.12: "in EPT resumptions are limited to scoped resumptions only — that is, resumptions can only be used under the same handler context as captured by the handler". Ki's design explicitly allows a continuation to be stored and "resumed later within the same fiber, even if the `handle` has already returned" (base §5.6, generators). Such a resumption is not scoped. **GEP handles it; EPT does not.**

**Why one-shot makes it easier.** Multi-shot needs resumptions that can be shared or copied (GEP's benchmarks `nqueens` and `triple` rely on that). Ki's continuations are one-shot (resuming twice panics) and fiber-local. So a continuation is a uniquely owned chain of closures. Resume consumes it, and drop releases it with ordinary RC.

### 3.2 S5 = Core+RC+EV (a new Core stage, after Perceus) [inference, design]

For every function whose effect row **may yield**:
- the row contains a user effect;
- or it contains a row variable (any effect-polymorphic function such as `map(xs, f)`);
- or, under native carrier A only (§4), it contains `Async` or `IO`.

S5 rewrites every non-tail call to such a function as:

```text
let r = f(ā)                        -- unchanged fast path
if isYielding() {                   -- one load + one well-predicted branch
    yieldExtend(join_k{S})          -- S = the suspension set of this call site (from S4)
    return <dummy>
}
jump join_k(S, r)                   -- the rest of the function, shared
```

1. **`join_k` is a lifted top-level function** taking `(env, value)`. Its environment is **exactly the S4 suspension set** of the call site: the owned live variables are *moved* in, and borrowed live variables are `dup`ed on the slow path only. Perceus already computed this set for the C++-era frametable. Here it becomes the capture list of a closure. That is the synergy that makes the transformation tractable in hand-written codegen. The rule "never infer a borrow across a prompt" (base §10a) is what makes the slow-path `dup` sound.
2. **`handle e with H`** pushes a handler record (marker, clauses, the evidence vector it was defined under) onto the fiber context's evidence, runs the body, and then goes through a **prompt** step:
   - on a normal return, pop the handler and apply `return`;
   - while yielding to *this* marker, build `Cont{frames}` and run the clause *outside* the handler (deep semantics);
   - while yielding to another marker, extend the continuation with "re-install this prompt" and keep bubbling.
3. **`perform L.op(v̄)` with a control clause:** look the handler up in the evidence (static offset if the row is closed and canonical, otherwise a search) and set `yieldTo(marker, op, v̄)`.
4. **`resume(k, v)`:** check one-shot, re-install the prompts captured in `k` (outermost first), then run its frames innermost first. If the resumed code yields again, the not-yet-run frames are appended to the *new* continuation, and the re-installed prompts keep catching the yields addressed to them. My first prototype got this wrong; §12, P5 has the fix.
5. **Tail-resumptive clauses** (`op(x) => e`) become a direct call of the clause with the evidence **masked** to the handler's outer context. In GEP §2.6, each evidence carries the context `w` under which the handler was defined. Keep the C++-era rule: the oracle never takes this path, and the VM and native code turn it on only in the optimizer chapter (base design ch. 55), after differential testing. A capture inside a masked clause must restore the mask on resume. In S5 that is one more frame kind, "restore evidence".
6. **Purity is free:** a closed row `uses {}` (and, under carrier B, `uses only {IO, Async}`) gets no checks. An optimizer chapter can add *effect specialization*: a pure clone of a row-polymorphic function, used when its closure argument is known to be pure [inference].

**Where S5 sits.** Koka runs its monadic translation before Perceus (Parc runs in the C backend). Ki's pipeline already computes suspension sets in S4, so running EV **after** S4 reuses them, and the RC census (`interp-rc` on S5) checks the lowering's ownership. The alternative (EV before closure conversion and Perceus) makes the continuation closures ordinary closures for Perceus. It is equally valid but redoes the free-variable work. I recommend after S4 [inference].

### 3.3 The Go prototype [verified, P5]

I hand-compiled the scheme in Go (`ev.go`, about 140 lines): the per-fiber `Ctx` (handler stack as evidence, yielding flag, target, accumulated frames), `Perform`, `Extend`, `Handle` with a prompt, and one-shot `Resume`. Tests:

1. **A generator over a 20-node tree, resumed outside the `handle`** (non-scoped resumption). The 20 values came out in order, with 20 captures, and the handler stack was empty afterwards.
2. **One-shot:** the second `Resume` panics with `continuation resumed twice`.
3. **A `State` (tail-resumptive) perform crossing an abortive `Fail` handler.** The abort drops `k` without running any code, and the result and state are correct.

Benchmarks (Go 1.26.5, `-benchtime=1s`):

| Operation | ns/op | allocs/op |
|---|---:|---:|
| plain non-inlined call | 1.11 | 0 |
| tail-resumptive perform (evidence lookup + clause call + mask) | 3.75 | 0 |
| control perform + immediate resume, depth 1 | 64.7 | 2 (64 B) |
| … depth 10 | 135.0 | 2 (208 B) |
| … depth 100 | 803.0 | 2 (1824 B) |
| 100-deep recursion **with** a yield check after each call, no effect performed | 154.0 | 0 |
| same recursion with no checks | 155.2 | 0 |

So: the checks on the direct path cost nothing measurable (a predicted branch), and a full capture plus resume costs about 7 ns per bubbled frame. In this prototype the per-frame closures captured nothing. Real code captures its suspension set, which adds one allocation per frame from Ki's own allocator.

---

## 4. Native code without stack switching: the four candidates

### 4.1 Comparison

| | (a) Evidence passing + yield bubbling (Koka C) | (b) CPS / selective CPS | (c) A goroutine per fiber, handlers by goroutine handoff | (d) Hybrid: fibers are goroutines, effects by evidence passing |
|---|---|---|---|---|
| Works with one-shot deep handlers? | Yes. GEP covers deep handlers and non-scoped resumptions, and one-shot removes copying. | Yes, trivially: the continuation is a value. | Yes. A coroutine is a one-shot continuation by construction. Deep: the handler loop re-installs itself. | Yes, as (a). |
| Direct-path cost | A predicted branch after effectful calls, measured as ≈0 (§3.3). Pure code (closed row) is untouched. | **An allocated continuation frame for every effectful non-tail call**, even when nothing is performed, unless the optimizer recovers direct style. | Every `handle` with control clauses creates a coroutine (`iter.Pull` create + one yield + stop: **275 ns**, [verified]). | As (a), with no checks needed for `Async`/`IO`. |
| Cost of a control perform + resume | O(depth between perform and handler): 65 / 135 / 803 ns at 1 / 10 / 100 [verified]. | O(1) capture, but you have paid on every call. | One coroswitch each way: **51 ns** round trip with `iter.Pull` [verified]. Plus one switch per intermediate coroutine when a perform crosses nested handlers. | As (a). |
| Dropping a live continuation | An ordinary Perceus drop of the closure chain. | An ordinary drop. | **Hard.** The suspended goroutine's frames hold owned Ki values that only a frame walker could release. `stop()` makes `yield` return false, so every Ki frame would need unwind code. Otherwise you leak a goroutine and its objects. | As (a). |
| Fibers, channels, `select` | Either carrier: A (bubble to the fiber root, run by Ki's scheduler) or B (goroutines). | A fiber is a continuation, so the scheduler is Ki's. | Goroutines, the Go scheduler. | Goroutines, the Go scheduler (or a Ki "token" scheduler on top, see 4.3). |
| Precedent | Koka's C backend, GEP benchmarks against OCaml 5, libhandler and Haskell libraries (ICFP 2021, §5). | Koka's earlier JS backend (Leijen, POPL 2017, type-directed selective CPS), Effekt's capability passing in CPS (Schuster, Brachthäuser & Ostermann, ICFP 2020) [paper, from memory]. | Go's own `iter.Pull` over runtime coroutines ([source] `runtime/coro.go:12-27`; `iter.Pull` API #61897, [source] `api/go1.23.txt`). | Go itself for the concurrency half. |
| Verdict | **Use it.** | Rejected: it charges every call so that captures are cheap, which is the wrong trade for one-shot handlers that are mostly tail-resumptive. | Rejected for handlers: drop semantics under Perceus and the per-`handle` goroutine cost. Useful as a teaching comparison. | Viable: it is "carrier B" in 4.3. |

### 4.2 The real design axis: what carries a native fiber

Once (a) is chosen for user effects, the remaining question is how a native fiber blocks on a channel, sleeps or waits for I/O when it is deep inside a call stack that cannot be switched.

**Carrier A: heap fibers on Ki's own scheduler (recommended)** [inference; the scheduler and EV pieces are verified separately].
- `Async` and blocking `IO` operations are treated as effects whose handler is the fiber root, exactly as the semantics already says ("`perform IO.op` and `perform Async.op` always reach the root of their fiber", base §6.2).
- A channel operation that cannot proceed registers a waiter, sets `yielding` with the fiber root as target, and the native code bubbles up. Each frame packages its suspension set into a closure. The root returns `Park` to the worker loop, which then releases the channel lock (the `gopark(unlockf)` pattern: [source] `runtime/proc.go:445-465`, `runtime/chan.go:283, 667, 748`).
- Waking the fiber means pushing it on a deque. Running it means a worker calling the asm entry `kiResume(ctx, fiber, value)`, which replays the closures on the worker goroutine's stack.
- **Consequences:**
  - the VM and native code share `Run(fiber) → Park | Done | Panic`, the work-stealing scheduler, the channels and `select`, the timers, the poller, the deadlock detector and `DetScheduler`;
  - a suspended native fiber costs only its closures, not a stack;
  - spawning is a heap allocation plus a deque push (14–22 ns per fiber in P12, measured on run-to-completion tasks);
  - blocking costs O(depth) (§3.3);
  - **pure code cannot be preempted at Ki level**, because it has no bubbling code (risk R3; mitigation in 4.4).

**Carrier B: a goroutine per fiber** [the deterministic half is verified, P7].
- `spawn` = `go kiFiberEntry(closure)`.
- A blocking operation blocks the goroutine inside `kirt` (same waiter queues, but the parker is a per-fiber wake channel or semaphore).
- I/O can call `net`/`os` directly and use Go's netpoller, which is portable to Windows for free.
- Preemption comes from Go (prologues plus back-edge polls).
- Deterministic mode = baton passing: exactly one goroutine holds the baton, and the §8.8 director picks the next.
- **Costs:**
  - ≥2 KiB of stack per fiber ([source] `stack.go:78`);
  - goroutine spawn+join 238 ns and switch ≈110 ns ([verified] P5);
  - **the work-stealing scheduler you write is not used in native code**;
  - **Go's own deadlock check can fire before Ki's**: `fatal error: all goroutines are asleep - deadlock!` exits with 2, not 102 ([source] `proc.go:6468`). Ki must detect "all fibers blocked, no I/O, no timers" itself when the last fiber parks.

### 4.3 Why A is recommended, and what B is for

- **Consistency** (the explicit requirement of this research) and **your original decision** ("Runtime: you write it — a multicore work-stealing scheduler", base §4) both point to A: one runtime, one scheduler and one deterministic mode for the two compiled backends. Under B the runtime has two parking worlds.
- **Performance is roughly even at realistic depths.** A blocking point at depth ~10 (bubble + replay ≈ 135 ns, plus captures) costs about the same as a goroutine switch (≈110 ns). Spawning is about 10× cheaper under A. Memory per suspended fiber is far lower. B wins for **deep stacks that block often** (803 ns at depth 100 against a flat ≈110 ns), for **preemption of pure code**, and for **I/O portability**.
- **B stays useful as the fallback and as an optional measurement chapter.** The `kirt` parker interface (`park(f, unlock)` / `ready(f)`) can be implemented both ways, so B is an adapter rather than a rewrite [inference].
- **Rejected middle ground, a Ki "token" scheduler over goroutines:** at most N fibers hold a worker token, and Ki's deques decide who runs next. It keeps your policy code and gives Go preemption, but pays for two schedulers on every switch (≈217 ns per baton round trip) [inference, not built].

### 4.4 Preemption and fairness under carrier A [inference]

1. **Go-level liveness (mandatory):**
   - every splittable Ki function already checks `stackguard0` in its prologue;
   - tail-call loops pass through prologues ([verified] P3);
   - loops with no prologue (`joinrec` back-edges) must poll, either with `CALL ·safepoint(SB)` to a splittable stub or with an inline check of `g.stackguard0` through TLS.

   The inline check worked and cost nothing measurable in a 1-cycle loop ([verified] P2: 1.98 s with the check against 2.07 s without), **but it depends on the unexported offset 16 of `stackguard0` in `runtime.g`**. Make the stub call the default and the TLS check a measured optimization with a version guard. (The every-1024-iterations stub variant was 2× slower in that same 1-cycle loop, an alignment-dominated worst case. Measure on real loops.)
2. **Ki-level preemption of effectful code:** a monitor goroutine sets `ctx.preempt` for fibers that have run more than 10 ms (Go's own slice: `forcePreemptNS = 10ms`, [source] `proc.go:6626-6628`). Prologues and back-edges of yield-capable functions test it and perform `Async.yieldNow` by bubbling.
3. **Fairness when pure code runs long:** work stealing already moves the other fibers queued on that worker to idle workers. If every worker is stuck, the monitor **retakes the worker's slot**: a spare worker goroutine takes over its deque, while the stuck goroutine finishes the pure computation without a slot and goes back to the spare pool at the fiber's next suspension. This copies Go's `sysmon` retake of a P from a goroutine in a long syscall ([source] `proc.go:6630-6700` `retake`, `:3131` `handoffp`). Go's scheduler then time-slices the extra goroutines over `GOMAXPROCS`. Not prototyped. In deterministic mode there is no monitor.

### 4.5 Native mapping summary (carrier A)

| Ki construct | Generated Go assembly / `kirt` |
|---|---|
| Ki function | `TEXT ·f(SB), $frame-args` with `NO_LOCAL_POINTERS`; a Go prototype in the generated `ki_gen.go` (all parameters `uintptr`); arguments and results on the stack; `ctx` in an argument slot, reloaded after calls. |
| Tail call | `RET ·g(SB)` [verified]. |
| Call to the runtime | A same-package Go trampoline (`kiAlloc`, `kiChanRecv`, …) wrapping `kirt.*` [verified]. |
| Call to a yield-capable function | `CALL`, then `MOVQ ctx…; CMPB yielding(ctx), $0; JNE bubble_k`. `bubble_k` allocates `join_k`'s environment, calls `kiYieldExtend`, and returns. Possible later optimization: a Ki→Ki callee returns its yielding status in a register the epilogue does not touch. The carry flag that GEP suggests fails here, because the assembler's epilogue `ADDQ $n, SP` clobbers flags ([verified] epilogue in P3 objdump) [inference]. |
| Continuation closure | Code pointer `LEAQ ·join_k(SB), AX` plus an environment in the Ki heap; invoked with an indirect `CALL`. Taking a TEXT symbol's address is standard asm; not separately prototyped. |
| `spawn` | Allocate a root fiber object with the closure, push it on the current worker's deque (spawn never switches). |
| `send`/`recv`/`select`/`sleep`/blocking I/O | A trampoline into `kirt`. Either it completes and returns, or it registers a waiter and sets `yielding` toward the fiber root, so the fiber bubbles to `Park`. |
| Back-edge with no call | A poll (§4.4). |

---

## 5. The VM: its own frames in Go slices, fibers as VM states, its own scheduler

### 5.1 Frames and effects

- **Per-fiber stack:** `regs []uint64` (Ki values are words; the Ki heap is off the Go heap, so this slice is `noscan` for Go's GC) and `frames []vmFrame{fn, pc, base}`. **Use indices, never `&regs[i]`**: `append` may reallocate (`go_spec.html:7408`), and a pointer into the old array then goes stale silently.
- **User effects:**
  - **Recommended:** execute **S5** like the native code (evidence, yielding flag, continuation closures). The VM then has no continuation capture, no frame walking with slot maps to drop a `Cont` (the riskiest item of base §10a), and it becomes a debugging oracle for the native code at the *same* IR.
  - **Alternative:** what the task suggested. Segment capture on the VM's own stack (one segment per `handle` with control clauses; capture = detach the chain, O(#segments); resume = relink; drop = walk the frames with the S4 slot maps).

  It is cheaper to capture and gives three really independent effect implementations. But it forks the middle-end tail (VM on S4 + slot maps, native on S5) and keeps the frame-walking drop. I recommend S5 and offering segments as an exercise [inference].
- **Fibers = VM states.** `run(f *Fiber, budget int) Status` interprets until `Park`, `Done`, `Panic` or budget exhaustion (preemption anywhere, because the stack is data). `Async` operations in the VM park **in place**, without bubbling. The S5 checks for `Async` (needed only by native carrier A) are dead in the VM. A debug flag could make the VM bubble too, to mimic native code [inference].

### 5.2 Writing the work-stealing scheduler vs delegating to the Go scheduler

| | Your own: N worker goroutines, Chase–Lev deques, Go's parking protocol | Delegate: one goroutine per VM fiber, its dispatch loop blocking in channel operations |
|---|---|---|
| What it teaches | `sync/atomic` and the Go memory model, lock-free code, `-race`, false sharing, the spinning/parking protocol, how `runtime/proc.go` works (you re-derive it) | goroutines, channels, `sync`, Go's scheduler as a user: the everyday professional Go skill set |
| Determinism | `DetScheduler` is just another implementation of the same interface (1 worker, `rng`, virtual clock) | Needs baton passing (P7); still possible |
| Overhead per fiber/task | **14–22 ns** per spawned task ([verified] P12, run-to-completion tasks) | **77–218 ns** per goroutine (P12); 238 ns spawn+join and ≈110 ns per switch (P5) |
| Scaling (635,621 fibers, about 1 µs of work per leaf) | 580 ms → 53 ms from 1 to 16 workers (10.9×) | 738 ms → 90 ms (8.2×) |
| Shared with native code | Yes, under carrier A | Only under carrier B |

**Caveat on P12:** the "own" tasks run to completion, without stacks or blocking. It measures scheduling overhead, not full fiber switches. Real VM fibers also save and restore a VM state.

**Recommendation:** write your own, as you decided, with `GOMAXPROCS` worker goroutines. Delegation becomes the measured baseline in the performance chapter.

### 5.3 Chase–Lev in Go: what the memory model gives and costs

- **Guarantee.** go.dev/ref/mem (version of June 6, 2022): "If the effect of an atomic operation A is observed by atomic operation B, then A is synchronized before B. All the atomic operations executed in a program behave as though executed in some sequentially consistent order … the same semantics as C++'s sequentially consistent atomics" ([source] `doc/go_mem.html:595-608`). So the `seq_cst` fences of the Lê et al. C11 version (PPoPP 2013, pp. 69–79) are implied by Go's atomic operations:
  - in `take`: an SC `Store(bottom)` then an SC `Load(top)`;
  - in `steal`: an SC `Load(top)` then an SC `Load(bottom)`.

  There is no relaxed or acquire/release variant in `sync/atomic`. The runtime's own run queue uses `LoadAcq`/`StoreRel`/`CasRel` from an internal package that user code cannot import ([source] `proc.go:7508-7512, 7609-7615, 7664-7665`).
- **Cost.** Every `atomic.Int64.Store` compiles to `XCHGQ` (a full barrier) and every CAS to `LOCK CMPXCHGQ` ([verified] P6 objdump). An uncontended push+pop costs 10.3 ns, against 7.8 ns for a mutex-protected slice ([verified] P6). The lock-free deque earns its keep under contention: thieves never block the owner. It does not win on the single-threaded fast path.
- **Slots must be atomic too.** A thief can read a slot the owner is overwriting after wrap-around. The thief's CAS then fails and the value is discarded, but a plain read is still a data race. Use `atomic.Pointer[Fiber]` slots.
- **Keep the C++-era choices:** `int64` indices; a fixed 1024-slot ring; half of a full deque moves to the mutex-protected global queue (no resizing, no reclamation problem); `top` and `bottom` on separate cache lines.
- **What `-race` can and cannot tell you.** It catches non-atomic conflicting accesses. With Go's SC atomics, **there are no ordering bugs for it to find**. Correctness also needs a linearizability-style stress test (every fiber taken exactly once). [verified] P6: 20 rounds × 200,000 fibers × 15 thieves under `-race` pass in 9.8 s; without `-race` they pass in 0.33 s.
- **Go's run queue is not Chase–Lev.** It is a 256-slot ring where only the owner pushes, owner and thieves consume from the head with CAS, and a `runnext` slot gives LIFO for the fiber just woken; thieves take half ([source] `runtime2.go:803-818`, `proc.go:7478-7745`). The book can contrast the two designs.

### 5.4 Parking, spinning, `LockOSThread`

- **Port the protocol in the comment at [source] `proc.go:36-81`:**
  - wake an idle worker only if nobody is spinning;
  - the last spinner that finds work wakes another;
  - at most N/2 spinners;
  - an idle worker publishes itself, **re-checks every queue**, then sleeps.

  The sleep is a receive on a per-worker `chan struct{}` with capacity 1 (a token, so a wake can never be lost). Also check the global queue every 61 ticks ([source] `proc.go:3443`). The Dekker pair (the submitter pushes then reads `nidle`; the idle worker increments `nidle` then reads the deques) is correct because both sides are SC atomics. [verified] P12: 300 runs with `GOMAXPROCS` from 1 to 16 and mixed work, plus 60 `-race` runs: no hang, no race report.
- **Do not use `runtime.LockOSThread` on workers.** [verified] P5: baton passing between two locked goroutines cost **126 µs per round trip against 217 ns unlocked**. When a locked goroutine blocks, its OS thread must sleep and another must wake ([source] `proc.go:3250` `stoplockedm`, `:3278` `startlockedm`). Locking buys no correctness for Ki (no thread-local OS state is involved). Set `GOMAXPROCS`, and justify any pinning by measurement. Also, runtime coroutines check that locked-thread state matches across switches ([source] `coro.go:119-127`), one more reason to stay unlocked.

### 5.5 Channels and `select` in `kirt` (shared by the VM and native carrier A)

- **Channel** (Go's `hchan`, [source] `chan.go:34`): a `sync.Mutex`, a ring of words, intrusive FIFO wait queues of `*waiter{fiber, slot, case, sel}`, and a `closed` flag. If a partner is waiting, the value is handed over directly and the partner goes to `runnext`.
- **Park, then unlock:** the waiter is enqueued under the lock, but the lock is released only after the fiber has stopped running. In the VM that is when `Park` reaches the worker loop. In native code it is when the bubbling reaches the root. This is `gopark(chanparkcommit, &c.lock)` ([source] `chan.go:283, 667, 748`). With `sync.Mutex`, unlocking from another goroutine is allowed: "A locked [Mutex] is not associated with a particular goroutine" ([source] `sync/mutex.go:61`), so the C++-era "unlock on the owning thread" rule disappears.
  - **Design fork under native carrier A [inference]:** the park-then-unlock window is no longer a few instructions. The channel lock (every lock of a `select`) stays held while the fiber bubbles O(depth) frames and allocates their environments.
  - The alternative is to mark the waiter `parking` and unlock immediately. A waker that wins `done` while the waiter is still `parking` only flips it to `ready-pending`. When `Park` reaches the root, the root re-enqueues the fiber itself if it finds that flag.
  - The waker then never sees a half-parked fiber, and lock hold time is constant again. Choose between the two in the channels chapter by measurement.
- **`select`** ([source] `select.go:122-245`):
  1. a random poll order, chosen with `cheaprandn` in Go;
  2. a lock order sorted by channel (Go sorts by address with a heap sort, `:208-240`); in `kirt`, sort by a monotonic channel id, which is stable and deterministic;
  3. a first pass that takes the first ready case;
  4. a second pass that enqueues one waiter per case, sharing an atomic `done` won by CAS.

  The randomness comes from the per-worker generator in multicore mode and from the shared splitmix64 in deterministic mode.

---

## 6. I/O and timers

| | Go's netpoller (through `net`/`os`) | Your own epoll through `syscall` |
|---|---|---|
| Available with stdlib only | Yes | Yes on Linux: `syscall.EpollCreate1/EpollCtl/EpollWait`, `Pipe2`, `SetNonblock`, `Socket`. `eventfd` has no wrapper: raw `Syscall(SYS_EVENTFD2, …)` ([source] `syscall/zsyscall_linux_amd64.go:1584`, `zsysnum_linux_amd64.go:297`). The package is frozen and points at `golang.org/x/sys`, which is *not* stdlib ([source] `syscall/syscall.go:20-24`). |
| Fits fibers that are not goroutines (VM, carrier A) | Only through a helper goroutine per pending operation: it does the blocking `Read` (parked cheaply in the netpoller) and then readies the Ki fiber. The netpoller's readiness API is internal. | Yes. A poller goroutine owns the epoll instance and an eventfd, and readies fibers. `EpollWait` uses `Syscall6`, so the runtime hands its P off ([source] `proc.go:6630+` retake). |
| Teaches | `net`, `io`, goroutines | Readiness I/O, `EPOLLONESHOT`, nonblocking sockets, `errno`: the C++-era chapter 45 material |
| Linux arm64 | Yes | Yes ([verified] P8 cross-builds for `linux/arm64`) |
| Windows | Yes (IOCP inside the Go runtime) | **No epoll** ([verified] `GOOS=windows go vet`: `undefined: syscall.EpollCreate1`). You would rewrite it on IOCP: `syscall.CreateIoCompletionPort`, `GetQueuedCompletionStatus` and `PostQueuedCompletionStatus` exist ([source] `syscall/syscall_windows.go:1438-1461`), but IOCP is completion-based, not readiness-based. |

**Recommendation:**
- `kirt/poll` with two implementations behind one interface:
  - `poll_epoll_linux.go`, your own, the default on Linux, for the VM and carrier A;
  - `poll_goroutine.go`, helper goroutines over `net`/`os`, used on Windows in the platforms part and as the measured baseline.
- **Timers:** a 4-ary heap owned by the poller, with the epoll timeout rounded to milliseconds exactly as Go does (`netpoll_epoll.go:103-116`). An earlier timer or shutdown wakes the poller through the eventfd ([verified] P8: pipe readiness and the eventfd wake both arrived after the expected 20 ms).
- **Regular files, stdin, name resolution:** a bounded pool of goroutines doing blocking calls (Go hands off the P on blocking syscalls).
- **stdout/stderr:** one `ki_write` buffer shared by every backend, as before.

---

## 7. A deterministic mode valid for all three backends

1. **The procedure stays the one in base §8.8:**
   - sequential fiber ids, `main = 0`;
   - `spawn` never switches;
   - FIFO wakeups;
   - the next fiber is `runnable[rng() % n]`, sorted by id;
   - a `select` with k ready cases takes `rng() % k`, counted in source order;
   - splitmix64;
   - a virtual clock that jumps to the next timer (ties broken by registration order);
   - file I/O synchronous, network I/O a panic.
2. **Two independent implementations:** the oracle's, and `kirt.DetScheduler`. The second serves the VM and native carrier A through the same `Run(fiber)` loop with **one worker goroutine**, no poller and no monitor.
3. **Go supplies nothing deterministic. Do not try to make Go's scheduler behave:**
   - its order is deliberately randomized under `-race` (`randomizeScheduler = raceenabled`, [source] `proc.go:7471`);
   - `select` is random by specification;
   - `testing/synctest` (GA in Go 1.25, #67434, [source] `api/go1.25.txt`) gives each "bubble" a fake clock that advances only when every goroutine is durably blocked ([source] `testing/synctest/synctest.go` doc), but interleavings stay nondeterministic: [verified] P9 found **6 distinct orders in 300 bubbles**. It remains a good tool for unit-testing `kirt`'s timer code, not for Ki's deterministic mode.
4. **Under carrier B, deterministic mode is baton passing.** [verified] P7: fibers as goroutines, `GOMAXPROCS=16`, built **with `-race`** (which randomizes Go's scheduler). Across 50 seeds × 4 runs, every run of the same seed produced the identical trace, the 50 seeds gave 50 distinct traces, and there was no race report. Every handoff goes through a channel, which supplies the happens-before edges.
5. **Trace events are semantic** (`spawn`, `run`, `block-on-send/recv/select`, `wake`, `select-choice`, `timer-fire`, `exit`) and emitted at the same semantic points by the oracle and `kirt`. Never emit events tied to the mechanism, such as "bubbled 7 frames".

---

## 8. Keeping differential testing against the oracle possible

1. **Between passes:** `ki test --check-passes` runs S0…S4 **and S5** in the oracle, with S4 and S5 also on the checked RC heap. An EV-lowering bug (a wrong suspension set, a missing re-installed prompt, a broken mask) then shows up as a divergence at the Core level, with the census catching leaks of abandoned continuations. kigen already generates "continuations saved and abandoned at every `perform`" (base §6.3), and that is exactly the test that matters.
2. **Between backends:** `interp`, `interp-rc`, `vm` and `native` run with `--sched=det --seed=s` and must agree byte for byte.
   - **Shared code:** the VM and native code share S5 and `kirt`, so their *shared* bugs are covered only by the oracle's independent scheduler and channel model. That is the same blind-spot coverage as the C++ design, and the reason the oracle never imports `kirt`. It shares only leaf packages (splitmix64 and the number-formatting primitive, as base §9.1 already allows), for example `kiutil/splitmix`.
   - **New observability gap, stack overflow:** native code dies with Go's `fatal error: stack overflow` and exit 2 [verified], not 103. The harness must classify exit 2 plus `goroutine stack exceeds` as *inconclusive*, like any overflow (base §6.3 already says overflow depth is not observable), or native code must keep a depth counter (risk R6).
   - **Go-level fatal errors** (`missing stackmap`, `unexpected SPWRITE`, `all goroutines are asleep`) are bugs in the code generator or the runtime and must fail the test loudly. Grep stderr for `fatal error:`.
3. **Multicore runs** are compared only for programs whose header says `schedule: independent`, as before. Network I/O stays out of differential testing.

---

## 9. Recommended coherent design (all three backends)

```text
Core S0..S4 ──► S5 = Core+RC+EV (evidence, yield checks, join_k closures = suspension sets,
                                 prompts, one-shot resume; tail-resumptive fast path from ch.55)
   │                │
   │ oracle runs    ├──► VM bytecode   (frames in Go slices; fibers = VM states that park in place;
   │ S0..S5         │                    user effects via S5; budget preemption)
   │ (CEK, own      └──► Go asm ABI0   (fixed TEXT frames, NO_LOCAL_POINTERS, generated Go prototypes
   │  det sched,                         + trampolines, RET-sym tail calls, back-edge polls;
   │  own chans,                         user effects via S5; fibers = heap root continuations,
   │  no goroutines)                     Async/IO bubble to the fiber root)
   │
kirt (Go, stdlib only), shared by VM and native:
   sched/  Chase–Lev deques (sync/atomic) + runnext + global queue + Go parking protocol,
           GOMAXPROCS worker goroutines, no LockOSThread, monitor (10 ms preempt flag + slot retake)
   det/    DetScheduler (1 worker, splitmix64, virtual clock) — same interface as sched
   chan/   hchan-style channels, park-then-unlock, two-pass select with done-CAS
   poll/   epoll+eventfd poller (Linux) | goroutine-helper poller (Windows/baseline); 4-ary timer heap
   io/     single ki_write; blocking-pool goroutines for files/stdin
Fallback (documented, optional chapter): native carrier B — fibers as goroutines, Go scheduler,
           baton-passing det mode, netpoller via net.
```

---

## 10. The order in which the book should build it

This keeps the base design's part structure. It changes chapter contents and adds or merges chapters. Go topics are listed because the book teaches Go from zero.

1. **Part III (effects in the oracle):**
   - handlers and one-shot continuations as frame moves in a Go slice: slices and aliasing, `slices.Clone`, `clear`, interfaces and type switches for frames, `panic`/`recover` only for Ki panics;
   - then generators (non-scoped resumptions).
2. **Part IV (concurrency in the oracle):** CEK fibers, the oracle's deterministic scheduler, channels, `select`, the virtual clock. No goroutines yet. That is a deliberate lesson: determinism comes from not using the Go scheduler.
3. **Part V (verified middle end):** after Perceus II, a **new chapter "S5: evidence passing and yield bubbling"** (≈16 h).
   - The oracle learns S5's primitives; `--check-passes` S4↔S5; `interp-rc` census on S5; kigen with abandoned continuations.
   - Go topics: closures and `func` values; why escaping closures allocate (`go build -gcflags=-m`).
4. **Part VI (runtime and VM):**
   - **The Go memory model before the first atomic:** go.dev/ref/mem, `sync/atomic` (SC only), happens-before through channels and mutexes, `-race`, and a contrast with C++/Rust orderings;
   - the object model and the allocator (another research topic);
   - the VM loop;
   - **"effects in the VM" = executing S5**, smaller than the C++ chapter.
5. **Part VII (multicore), unchanged in spirit:**
   - VM fibers and `DetScheduler` (traces identical to the oracle's);
   - Chase–Lev (`sync/atomic`, false sharing with padding, a stress test, `-race`, the `XCHG` cost);
   - the work-stealing scheduler (goroutines as workers, wake tokens through channels, Go's parking protocol; measure `LockOSThread` and drop it);
   - channels and `select` (`sync.Mutex`, park-then-unlock);
   - epoll and timers through `syscall`.
6. **Part VIII (native):**
   - **Go assembly from zero:** ABI0, `TEXT` flags, `FP`/`SP` pseudo-registers, `funcdata.h`, `NO_LOCAL_POINTERS`, Go prototypes, `go vet` asmdecl, `go tool objdump`, the ABI wrappers, the four fatal errors of §1 *reproduced on purpose as exercises*;
   - then instruction selection and register allocation as before;
   - frames and tail calls (`RET ·f(SB)`, the forced non-leaf rule);
   - back-edge polls (the GC-stall experiment, P2, as the chapter's opening test);
   - native closures, ADTs and RC;
   - **native effects = compiling S5**, which replaces the C++ chapters "stack segments and context switching" (ch. 52) and "native effects" (ch. 53) with a single chapter;
   - **native fibers = carrier A on the existing `kirt`**;
   - the three-way test (M10).
7. **Part IX (performance):** the tail-resumptive fast path with masking; effect specialization; a register-returned yielding flag; the slot-retake fairness mechanism; **optional measurement chapter "what if fibers were goroutines?"** (carrier B as an adapter: `iter.Pull`, baton passing, `net`).
8. **Part XI (platforms):** AArch64 Go assembly (different prologue registers, the link register; `syscall.EpollCreate1` exists on arm64, plain `EpollCreate` does not); Windows with the goroutine poller, or an IOCP poller as an advanced chapter.


**Reconciliation with R4's draft index (`r4-go-curriculum.md` §8), as read on 2026-10-24:**
- **ch. 21** lists "O(1) slice splicing". R2 recommends `slices.Clone` + `append`, which is O(depth) and simpler. O(1) needs a segmented representation.
- **ch. 40** keeps "segments and continuations" in the VM. R2 recommends executing S5, with segments as an exercise.
- **ch. 41** teaches worker goroutines with `LockOSThread`. R2 measured locked handoffs about 580× slower and recommends not locking (keep it only as the measured counter-example).
- **ch. 52–53** keep native stack segments and context switching as an open fork ("F1"). R2 shows the fork is closed: SPWRITE is fatal, and the stack moves. R2 merges both chapters into "native effects = compiling S5".
- **Part V** has no S5 chapter. R2 adds one (about 16 h), after Perceus II.

---

## 11. Risks

| # | Risk | Mitigation |
|---|---|---|
| R1 | **Dependence on Go runtime internals and assembler behavior:** the auto-NOSPLIT leaf rule, the SPWRITE handling, the wrapper generation, the prologue shape, and (if used) the `g.stackguard0` offset 16 through TLS. These are implementation details that a Go release could change. | Pin Go 1.26.x in the repo; a CI suite that reproduces each §1 fact (P1–P3, P10, P11); default to the documented path (stub `CALL` polls, trampolines); keep the TLS trick behind a version check. |
| R2 | **Arguments on the stack.** Every Ki call pays memory traffic. Register arguments are only safe into NOSPLIT leaves. | Accept it for v0 (Go itself used ABI0 until Go 1.17). Later, register arguments for leaves, and DX through `NEEDCTXT` [inference]. |
| R3 | **No Ki-level preemption of pure code under carrier A.** A long pure computation keeps its worker. | Work stealing; the slot-retake mechanism (§4.4, modelled on `retake`/`handoffp`); Go-level preemption through prologues and polls keeps the process live. Deterministic mode is unaffected. |
| R4 | **O(depth) suspension cost** for deep stacks that block often (803 ns at depth 100 in the prototype, plus one allocation per frame). | Measure on the real benchmark programs. Carrier B remains as an adapter if it matters. GEP's "short-cut resumptions" gave about 10% in its benchmarks (§2.8 of the paper). |
| R5 | **Code growth** from `join_k` functions at every effectful call site. | Selective by rows (pure code untouched); join-point sharing as in GEP §2.10; effect specialization in the optimizer. |
| R6 | **Native stack overflow is Go's fatal error** (exit 2), not Ki's 103. | Classify it as inconclusive in the harness (overflow depth is already non-observable), or add a depth counter in prologues (costs a load and a store per call). Decide when the native frame layout is fixed. |
| R7 | **Off-Go-heap Ki heap assumption.** If any Go pointer were stored in Ki memory, the GC could free the object (use-after-free). | Ki values are `uintptr` words; Go-side resources (files, timers) live behind handle tables indexed from Ki objects. This must be consistent with the Perceus/allocator research. |
| R8 | **Concurrent forcing of a shared thunk in pure code** cannot suspend under carrier A. | Allow duplicate evaluation (pure, so it is safe; the first CAS wins), or spin briefly. Document it in the semantics (base §6.2 already makes `force` atomic) [inference]. |
| R9 | **Lost wakeups and `select` races** in `kirt`. Note that `-race` sees only the Go-side scheduler and channel structures: it ignores addresses outside the Go heap, so races on Ki-heap words (for example shared RC counters) are invisible to it (R3, `r3-perceus-own-heap-go.md`, which also verified a debug build that places the Ki heap inside the Go heap). | Go's protocol verbatim; per-worker wake tokens; seed sweeps in deterministic mode; `-race` plus counting stress tests (P6, P12 patterns); R3's Go-heap debug build for Ki-heap races; the oracle's independent model. |
| R10 | **`syscall` is frozen**, and there is no epoll on Windows. | Keep the goroutine poller as the portable path; IOCP only as an advanced chapter. |
| R11 | **Under carrier B (if ever used):** Go's `all goroutines are asleep` fatal can preempt Ki's deadlock report (exit 102); and `iter.Pull` coroutines interact with `LockOSThread` ([source] `coro.go:119-127`). | Ki detects the deadlock itself when the last fiber parks; never lock threads. |
| R12 | **The book's Go-from-zero progression must reach assembly and runtime internals** (morestack, stack maps, preemption) in Part VIII. That is a steep step for a reader who learned Go in the same book. | The §1 failures reproduced on purpose as guided exercises; `runtime/HACKING.md` as assigned reading; objdump-driven chapters. |

---

## 12. Verification transcripts (summarized)

All prototypes lived in `/tmp/claude-1000/-home-scami/144ea2a3-9681-4392-8a95-4f8de2e00caa/scratchpad/r2proto/` (deleted afterwards). Each was a Go module built with Go 1.26.5 on linux/amd64.

- **P1 (stack moving + GC through asm frames).**
  - Setup: an asm `recA(n)` with `$16-16` frame, `NO_LOCAL_POINTERS`, a Go prototype, recursing 10⁶ deep; the leaf calls `runtime.GC()` while another goroutine loops on `runtime.GC()`.
  - Result: `A: rec(1000000) = 1000000 in 606ms, leaf calls 1, StackInuse 48 MiB`.
  - The same code without `NO_LOCAL_POINTERS`: `runtime: frame main.recB untyped locals …`, `fatal error: missing stackmap`, via `copystack → adjustframe → getStackMap (stkframe.go:195)`. `go vet` did not warn.
- **P2 (assembly is not async-preemptible).** Loop of 8×10⁹ iterations; `runtime.GC()` from another goroutine 50 ms in.

  | Variant | Loop time | `runtime.GC()` took |
  |---|---:|---:|
  | no poll | 2.07 s | **2.02 s** |
  | `CALL` to a splittable stub every 1024 iterations | 4.08 s | 1.06 ms |
  | inline `MOVQ (TLS), R13; CMPQ SP, 16(R13)` on every back-edge | 1.98 s | 1.38 ms |

  The stub needed an unreachable `CALL` to keep its split check. `go vet` reported `function safepoint missing Go declaration` (a vet warning, not a build error).
- **P3 (tail calls, leaf rule, cross-package calls).**
  - Leaf rule: with `$8` frames and no `CALL`, the linker failed with `main.isEven: nosplit stack over 792 byte limit`. Adding an unreachable `CALL` fixed it.
  - Tail calls: `isEven(100000001)=0 in 105ms, StackInuse=320 KiB`. Objdump: prologue `MOVQ FS:0xfffffff8, R14; CMPQ SP, 0x10(R14); JBE`; tail `ADDQ $0x8, SP; POPQ BP; JMP main.isOdd.abi0`.
  - Cross-package: `CALL kiproj∕kirt·Alloc(SB)` failed with `relocation target kiproj/kirt.Alloc not defined for ABI0 (but is defined for ABIInternal)`. A dotted path was rejected by the assembler (`expected '(', found .`). The same-package trampoline worked (`tramp: 5`).
- **P5 (EV prototype + handoff costs).**
  - The semantics tests pass (generator resumed outside its handler, one-shot panic, abortive Fail crossing State).
  - My first `Resume` was wrong: it stopped processing frames when resumed code yielded again, so the re-installed prompt never caught the yield and the test hit `continuation resumed twice`. The fix is the loop in §3.2 point 4.
  - Benchmarks are in §3.3. Handoffs (`GOMAXPROCS` default / 1):

    | Handoff | default | `GOMAXPROCS=1` |
    |---|---:|---:|
    | `iter.Pull` round trip | 50.7 ns | 50.1 ns |
    | unbuffered channel ping-pong | 226.6 ns | 208.0 ns |
    | baton pass (two cap-1 channels) | 217.2 ns | 197.0 ns |
    | baton pass with both goroutines `LockOSThread`ed | 128,239 ns (rerun: 125,093–127,234) | 97,926 ns |
    | goroutine spawn+join | 237.9 ns | 239.6 ns |
    | `iter.Pull` create + one yield + stop | 274.9 ns | — |
- **P6 (Chase–Lev).**
  - `-race`, 20 rounds × 200,000 fibers × 15 thieves: `PASS (9.81s)`, last round `popped=63280 stolen=136720 overflow=0`. Without `-race`: `ok 0.329s`.
  - Benchmarks: owner push+pop 10.29 ns; mutex slice 7.82 ns.
  - Objdump: `Push` has one `XCHGQ`; `Pop` has `XCHGQ` ×3 and `LOCK CMPXCHGQ` ×1.
- **P7 (deterministic baton passing over goroutines).**
  - `GOMAXPROCS=16`: 3 seeds × 5 runs gave identical SHA-256s per seed.
  - A `-race` build over 50 seeds × 4 runs: `seeds with differing traces under -race: 0/50`, 50 distinct traces across the seeds, 0 `WARNING: DATA RACE`.
  - Trace excerpt: `spawn f0 … f0 blocks on recv / run f3 / f3 send 30 -> f0`.
- **P8 (epoll through `syscall`).**
  - `first read: resource temporarily unavailable`, then `pipe ready after 20ms: "hello"`, then `eventfd wake after 20ms: 1`.
  - `GOOS=windows go vet`: `undefined: syscall.EpollCreate1`. `GOOS=linux GOARCH=arm64 go build`: succeeds.
- **P9 (`synctest`).** 300 bubbles × 4 goroutines × 50 locked appends, `GOMAXPROCS=16`: `distinct interleavings in 300 bubbles: 6`.
- **P10 (SPWRITE).** `MOVQ SP, AX; MOVQ AX, SP; CALL ·leaf` (which calls `runtime.GC()`): `traceback: unexpected SPWRITE function main.writesSP`, `fatal error: traceback`, exit code 2.
- **P11 (call costs, overflow).**
  - Calls: asm→asm 1.22 ns per call; asm→Go through the ABI0 wrapper 2.39 ns per call. The wrapper is `PUSHQ BP; MOVQ SP,BP; SUBQ $8,SP; MOVQ 0x18(SP),AX; XORPS X15,X15; MOVQ FS:-8,R14; CALL main.goInc; MOVQ AX,0x20(SP); …`.
  - Overflow with `SetMaxStack(64 MiB)`: `runtime: goroutine stack exceeds 67108864-byte limit`, `fatal error: stack overflow`, exit code 2.
- **P12 (own scheduler vs goroutines).**
  - 300 runs with `GOMAXPROCS` from 1 to 16 and mixed work: `0/300` hung or failed. 60 `-race` runs: `0/60`, and 0 `DATA RACE`.
  - Timings, best of 3, 635,621 fibers:

    | Work per leaf | Workers | Own scheduler | Goroutines |
    |---|---|---:|---:|
    | none | 1 | 14 ms (22 ns/fiber) | 139 ms (218 ns/fiber) |
    | none | 16 | 9 ms (14 ns/fiber) | 49 ms (77 ns/fiber) |
    | about 1 µs | 1 | 580 ms | 738 ms |
    | about 1 µs | 16 | 53 ms | 90 ms |

**Not verified here (inference or documented only):**
- carrier A end to end in Go assembly;
- the slot-retake fairness mechanism;
- `#line` directives in `.s` files for Ki source positions;
- `NEEDCTXT`/DX argument passing through `morestack`;
- register-returned yielding flags;
- `LEAQ` of a TEXT symbol plus an indirect `CALL` (standard, not separately run);
- the AArch64 and Windows paths beyond compiling P8.

---

## 13. Sources

**Go source tree (Go 1.26.5, `$GOROOT/src` unless noted):**
- `runtime/preempt.go:374-470` (`isAsyncSafePoint`, asm exclusion at :450-458)
- `runtime/stack.go:78` (`stackMin`), `:1026-1178` (`newstack`: preempt :1093-1145, growth :1147, overflow :1171-1178), `:1386` (`startingStackSize`); `runtime/runtime1.go:410` (`adaptivestackstart`); `runtime/proc.go:160` (`maxstacksize`)
- `runtime/stkframe.go:194-195, 221-222` (`missing stackmap`); `runtime/traceback.go:338-366` (SPWRITE)
- `runtime/asm_amd64.s:441-452` (`gogo<>`), `:628-688` (`morestack`, `morestack_noctxt`)
- `cmd/internal/obj/x86/obj6.go:617-868` (`preprocess`: BP :636-647, leaf NOSPLIT :661-690, auto-SPWRITE :760-776, ADJSP :810, RET epilogue and retjmp :818-855), `:873-900` (`loadG`), `:914-1122` (`stacksplit`); `cmd/internal/obj/x86/a.out.go:265-268`; `cmd/asm/internal/asm/parse.go:899`; `internal/abi/stack.go:8-24`
- `cmd/compile/abi-internal.md:12-16, 221, 398, 413-424`
- `runtime/coro.go:12-27, 119-127`; `runtime/proc.go:36-81` (parking design), `:445-465` (`gopark`), `:3131` (`handoffp`), `:3212` (`wakep`), `:3250, 3278` (locked Ms), `:3443` (`schedtick%61`), `:6468` (deadlock fatal), `:6626-6700` (`forcePreemptNS`, `retake`), `:7471` (`randomizeScheduler`), `:7478-7745` (`runqput`/`runqget`/`runqgrab`/`runqsteal`); `runtime/runtime2.go:803-818`
- `runtime/chan.go:34, 283, 667, 748`; `runtime/select.go:122-245`; `runtime/netpoll_epoll.go:99-119`
- `syscall/syscall.go:20-24`; `syscall/zsyscall_linux_amd64.go:1584-1596`; `syscall/zsysnum_linux_amd64.go:297`; `syscall/syscall_windows.go:1438-1461`
- `testing/synctest/synctest.go` (package doc); `api/go1.25.txt` (synctest #67434); `api/go1.23.txt` (iter.Pull #61897)

**Go documents shipped in `$GOROOT/doc` (online at go.dev):**
- `go_mem.html` (The Go Memory Model, version of June 6, 2022; atomics at :595-608) — https://go.dev/ref/mem
- `asm.html` (A Quick Guide to Go's Assembler; pointer maps :561-610, amd64 BP :752-755) — https://go.dev/doc/asm
- `go_spec.html` (go1.26; `select` :6995, `append` :7408) — https://go.dev/ref/spec
- Race detector article (overhead 5–10× memory, 2–20× time) — https://go.dev/doc/articles/race_detector
- Non-cooperative preemption proposal (Austin Clements, golang/go#24543) — https://go.googlesource.com/proposal/+/HEAD/design/24543-non-cooperative-preemption.md

**Papers:**
- N. Xie, D. Leijen. *Generalized Evidence Passing for Effect Handlers (or, Efficient Compilation of Effect Handlers to C).* PACMPL 5 (ICFP 2021), Article 71, doi:10.1145/3473576. https://www.microsoft.com/en-us/research/uploads/prod/2021/08/genev-icfp21.pdf (text read: §2.6, §2.8, §2.10–2.12, §5 benchmarks)
- N. Xie, J. I. Brachthäuser, D. Hillerström, P. Schuster, D. Leijen. *Effect Handlers, Evidently.* ICFP 2020 (PACMPL 4, ICFP). https://www.microsoft.com/en-us/research/publication/effect-handlers-evidently/ (scoped-resumption restriction as characterized in GEP §2.12)
- N. M. Lê, A. Pop, A. Cohen, F. Zappa Nardelli. *Correct and Efficient Work-Stealing for Weak Memory Models.* PPoPP 2013, pp. 69–79. https://research.manchester.ac.uk/en/publications/correct-and-efficient-work-stealing-for-weak-memory-models/
- D. Chase, Y. Lev. *Dynamic Circular Work-Stealing Deque.* SPAA 2005 (from memory).
- K. C. Sivaramakrishnan et al. *Retrofitting Effect Handlers onto OCaml.* PLDI 2021 (cited by GEP for OCaml 5's segmented stacks).
- D. Leijen. *Type Directed Compilation of Row-Typed Algebraic Effects.* POPL 2017 (selective CPS; from memory).
- P. Schuster, J. I. Brachthäuser, K. Ostermann. *Compiling Effect Handlers in Capability-Passing Style.* ICFP 2020 (from memory).
- A. Reinking, N. Xie, L. de Moura, D. Leijen. *Perceus: Garbage Free Reference Counting with Reuse.* PLDI 2021 (from memory).
- Russ Cox, *Coroutines for Go* (research.swtch.com/coro, 2023), background for `runtime/coro.go` (from memory, not re-fetched).

**Ki base design:** `/mnt/c/Users/scami/Downloads/ki-diseno-go/base-cpp-design.md` §1–4, §5.6, §5.11, §6.1–6.3, §8.4–8.8, §9, §10, §12.5.
