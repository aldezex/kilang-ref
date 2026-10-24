# R1. A native backend that emits Go (Plan 9) assembly, linked with a Ki runtime written in Go

**Question.** Can the Ki compiler emit Go assembly for a whole Ki program and have `go build` link it with a Ki runtime (`kirt`) written in Go? If so, what exactly is the contract?

**Short answer.** Yes, and it works today on linux/amd64 and windows/amd64. I built and ran it on both. There is one hard condition: **Ki native code must run on goroutine stacks and live by the goroutine rules.** That breaks three parts of the C++-era design (§8.4 own stack segments and `ki_switch`, §8.5 the r15 worker context and the native side of the scheduler, §9.3 KiCC with callee-saved registers). Section 11 gives the recommended contract. Section 12 lists the risks.

**Evidence labels used throughout.**

- **[V] verified** on this machine with a small program (transcripts summarised in §13).
- **[D] documented**: Go docs, or the Go source tree at `$GOROOT=/home/scami/.local/opt/go` for Go 1.26.5. Paths are relative to `$GOROOT/src` unless noted, and line numbers are for 1.26.5.
- **[I] inference**: my reasoning, not checked by a test.

**Environment.**

- Go 1.26.5 linux/amd64 (GOAMD64=v1), on Ubuntu/WSL2 with kernel 6.18.
- AMD Ryzen 7 7800X3D, 16 logical CPUs.
- The windows/amd64 binaries were cross-built (`GOOS=windows`) and run natively on the Windows host through WSL interop.
- I cross-built arm64, but did not run it: no qemu here.
- Every timing is a microbenchmark from one or two runs. Read them as orders of magnitude.

---

## 0. Verdict in one paragraph

**Feasible, with a narrow but stable contract.**

- **What user assembly gets.** User `.s` files are always ABI0, the stable stack-based Go assembly ABI. ABI selectors are restricted to a short list of runtime packages.
- **How calls work.**
  - Go calls the asm directly: no wrapper, in the same package or across packages.
  - The asm calls Go functions through compiler-generated ABI0 wrappers. Those exist only if the defining package asks for them.
- **What the runtime does for free.** The assembler inserts the morestack prologue, keeps the frame pointer, and writes the SP-delta tables. In return the runtime gives Ki code:
  - stack growth by copying, verified with 2×10⁶ frames;
  - precise GC scanning of asm frames;
  - a preemption point at every function entry.
- **What it costs.** Ki code must obey the goroutine rules:
  - every framed, calling function declares `NO_LOCAL_POINTERS`;
  - no Go pointers live in Ki frames, so the Ki heap lives outside the Go GC and Ki values are `uintptr`;
  - nothing survives a call in a register;
  - arguments cannot arrive in registers through a normal prologue, because morestack clobbers them (only DX survives);
  - SP is never written except by the frame, PUSH/POP or ADJSP;
  - code never switches stacks;
  - addresses of stack slots are never kept across a call;
  - loops without calls must poll, because the runtime never async-preempts assembly.
- **The baseline I recommend: "KiABI-A0".** Arguments go in memory in the caller's outgoing area (declared argsize 0, so no Go prototypes are needed). The closure or environment pointer goes in DX. Results come back in AX. Every Ki function gets a normal assembler prologue, and loop back-edges get an inline poll.
- **An optional faster variant: "KiABI-B′".** Arguments travel in registers. It uses NOSPLIT bodies with a manual check, plus a splittable 4 KiB helper for growth and preemption. In a call-heavy microbenchmark it cut call cost by about 35%. It depends on two undocumented toolchain facts: the offset of `g.stackguard0`, and how the linker's nosplit analysis treats indirect calls. It belongs in the performance part, behind a CI test, with A0 as the fallback.
- **Build cost.** A 10⁴-function program (300k lines of asm) rebuilds in about 1.0 s with `go build`; the runtime package comes from the build cache.
- **The real price is architectural.** Ki fibers must be goroutines. One-shot continuations cannot be "detach a stack segment" in native code. These decisions belong to Álvaro and to the scheduler and effects research topics.

---

## 1. ABI0 vs ABIInternal, calls in both directions, linkname, noescape, TEXT/DATA/GLOBL

### 1.1 What user `.s` files can use

- **[D]** ABIInternal is "*unstable* and will change between Go versions. If you're writing assembly code, please instead refer to Go's assembly documentation, which describes Go's stable ABI, known as ABI0" (`cmd/compile/abi-internal.md:9-12`). Both ABIs call each other "through transparent *ABI wrappers*" (`:14-16`; design doc go.dev/design/27539-internal-abi; the register ABI is go.dev/design/40724-register-calling).
- **[D]** ABI selectors such as `TEXT ·f<ABIInternal>(SB)` are rejected outside a hard-coded list:
  - the list is `runtime`, `reflect`, `syscall`, `internal/bytealg`, `internal/chacha8rand`, and the `internal/runtime/syscall/*` and `startlinetest` packages (`cmd/internal/objabi/pkgspecial.go:93-102`);
  - the parser enforces it (`cmd/asm/internal/asm/parse.go:72,897`) with "ABI selector only permitted when compiling runtime".

  **Every Ki function is therefore ABI0.**
- **[D]** In ABI0, arguments and results live on the stack, in the caller's frame. `TEXT ·f(SB),$24-8` means "a 24-byte frame and called with 8 bytes of argument, which live on the caller's frame" (`doc/asm.html:322-331`).

### 1.2 Go → asm

- **[V]** A bodyless Go declaration plus a `TEXT` in the same package compiles to a **direct** `CALL main.callIt.abi0(SB)`. Right after it, the compiler emits `XORPS X15, X15; MOVQ FS:0xfffffff8, R14` to restore the two fixed ABIInternal registers (objdump of `main.main`).
  - Source: `cmd/compile/internal/amd64/ssa.go:1295-1313`, "zeroing X15 when entering ABIInternal from ABI0 … set G register from TLS".
  - The static call picks the callee's ABI from the symabis file (`cmd/compile/internal/ssagen/ssa.go:4985-4997`).
- **[V]** It is also direct **across packages**: `main` calling the asm function `kirt.AsmId` compiled to `CALL e1/kirt.AsmId.abi0(SB)`.
- **[D]** An ABIInternal→ABI0 wrapper is still generated for every asm function that has a Go declaration. The reason given is "Assume all functions are referenced at least as ABIInternal" (`ssagen/abi.go:196-201`). It is used for func values, interfaces and reflection.
- **[V]** For 10⁴ prototypes the compiler generated 10,001 such wrappers (`-d=abiwrap`). The linker dead-strips the unused ones: the final binary contains only the `.abi0` symbols.

### 1.3 asm → Go: wrappers, when they exist, and their cost

- **[D]** An ABI0 entry point (`pkg.F.abi0`) for a Go function is generated only in two cases.
  - (a) Assembly *in the same package* references `·F`. `go tool asm -gensymabis` writes a `ref` line, and `GenABIWrappers` turns it into a wrapper (`ssagen/abi.go:61-101,191-195`).
  - (b) The function has a `//go:linkname`. "If a symbol is defined in this package … and given a linkname, it may be referenced from another package, so make it callable via any ABI" (`ssagen/abi.go:203-217`, `ABISetCallable`).
- **[V]** Generated asm in `main` doing `CALL e1∕kirt·Poll(SB)` against a plain exported `func Poll()` fails to link: `main.callIt: relocation target e1/kirt.Poll not defined for ABI0 (but is defined for ABIInternal)`. Two fixes link and run:
  - a one-argument (push) `//go:linkname PollPushed` on the definition;
  - a `.s` file *inside kirt* holding `DATA ·abi0refs+0(SB)/8, $·PollRef(SB)`.

  Both produce `e1/kirt.PollPushed.abi0` and `e1/kirt.PollRef.abi0` in `go tool nm`.
- **[V]** `CALL runtime·Gosched(SB)` from user asm fails the same way: `relocation target runtime.Gosched not defined for ABI0 (but is defined for ABIInternal)`. The rule for Ki: **generated asm never calls the Go runtime directly. It calls kirt entry points, and kirt calls the Go runtime.**
- **[V] Wrapper shape.** For a function with no arguments the wrapper is a tail jump: `XORPS X15,X15; MOVQ FS:-8,R14; JMP F`. With arguments, it loads them from the stack into registers, calls F, and stores the results back.
- **[D]** The wrappers are NOSPLIT (`ssagen/abi.go:276-305`, `fn.Pragma |= ir.Nosplit`). The Go function behind one has its own prologue.
- **[V] Cost.** A loop that spills two values and calls `kirt.Note(x) uintptr` costs **2.6–2.9 ns per iteration** on Linux and 2.3 ns on Windows. Calling a prologue'd asm function costs 1.3 ns, and a bare loop 0.37 ns. So **an asm→Go round trip is about 1.3–1.6 ns more than asm→asm** [V, `ok bench`].

### 1.4 `go:linkname` restrictions since Go 1.23

- **[D]** Go 1.23 release notes, quoted verbatim:

  > The linker now disallows using a `//go:linkname` directive to refer to internal symbols in the standard library (including the runtime) that are not marked with `//go:linkname` on their definitions. Similarly, the linker disallows references to such symbols from assembly code. … A linker command line flag `-checklinkname=0` can be used to disable this check.

  The tracking issue is golang/go#67401, "cmd/link: lock down future uses of linkname" (rsc). It wants the "handshake" form.
- **[D] Mechanism.**
  - Every named reference from an assembly object is flagged as a linkname: "Assembly reference is treated the same as linkname" (`cmd/internal/obj/objfile.go:357-362`).
  - The linker's `checkLinkname` (`cmd/link/internal/loader/loader.go:2496-2534`) only polices references *into std*. Non-std targets return early ("For now, only check for symbols defined in std"). Same-package references and ABI wrappers are allowed. There is also a `blockedLinknames` deny-list (`:2406-2427`), which includes `runtime.addmoduledata`.
- **Consequences for Ki.**
  - [I] Ki↔kirt references, whether linkname or asm, are unrestricted, because kirt is not std.
  - [V] References to *assembly-defined* runtime symbols still link today. `CALL runtime·morestack_noctxt(SB)` linked and ran in contract B, because asm definitions carry the linkname flag themselves (same `objfile.go:357`). I treat it as fragile: the runtime team can add any such symbol to `blockedLinknames`.
  - Don't ship `-checklinkname=0`.

### 1.5 `//go:noescape`

- **[D]** It applies only to bodyless declarations. It promises that the pointer arguments do not escape (`cmd/compile/doc.go:216-223`).
- **[V]** `-gcflags=-m` shows the effect:
  - without it: `leaking param: p` and `moved to heap: x`;
  - with it: `p does not escape`, and the array stays on the stack.
- **For Ki it is moot**, because every prototype takes `uintptr`. It matters only if kirt ever passes Go pointers into asm.

### 1.6 Declaring functions and data

- **[D] `TEXT` syntax.** `TEXT ·name(SB), FLAGS, $framesize-argsize` (`doc/asm.html:301-331`).
  - The argsize is mandatory unless the function is NOSPLIT.
  - The `·` (U+00B7) prefix is the package-local name.
  - A foreign package is written with U+2215 for slashes: `e2∕kirt·Poll(SB)`. [V] A plain `kirt·Poll` resolves to the wrong symbol `kirt.Poll`.
  - `name<>(SB)` makes the symbol file-local (`:180-187`).
- **[D] Flags** (`runtime/textflag.h`, `doc/asm.html:393-465`):
  - `NOSPLIT=4`: no stack check;
  - `NEEDCTXT=64`: uses the closure context register; this picks `runtime.morestack` over `morestack_noctxt`, `obj6.go:1095-1102`;
  - `NOFRAME=512`, `TOPFRAME=2048`, `WRAPPER=32`;
  - `RODATA=8`, `NOPTR=16` and `DUPOK=2` for data.
- **[D] References and labels.**
  - Argument references through FP must be named (`x+0(FP)`); "The assembler enforces this convention, rejecting plain 0(FP)" (`:195-201`).
  - Labels are function-local (`:260-267`).
  - Direct jumps and calls may target `name(SB)`, but not `name+4(SB)` (`:264-266`).
- **[D] `DATA`/`GLOBL`.** `DATA sym+off(SB)/width, value`, with offsets increasing, then `GLOBL sym(SB), flags, $size` (`:340-383`). Pointer rules (`:544-557`):
  - "It is not possible to define a symbol containing pointers in an assembly source file; such a symbol must be defined in a Go source file instead."
  - RODATA implies NOPTR.
- **[V] Tables of code addresses.** `DATA ·ktab+0(SB)/8, $·kinc(SB)` / `GLOBL ·ktab(SB), RODATA, $16` assemble and link, and indirect `CALL CX` through them works. They also work in PIE (`-buildmode=pie`) [V].
- **[V]** A Go declaration `var heads [1000]uintptr` with the definition in asm (`DATA`/`GLOBL ... RODATA`) linked as an `R` symbol and could be read from Go. I don't rely on it; an accessor function is cleaner [I].
- **[D] `go_asm.h`.** It gives constants and struct offsets of the *same* package (`doc/asm.html:484-533`).
- **[I]** Generated code cannot include kirt's `go_asm.h`. The Ki compiler, being Go, should compute kirt layouts with `unsafe.Offsetof`/`Sizeof` and emit its own `kiabi.h` with `#define`s into the generated package.

---

## 2. The stack

### 2.1 The prologue the assembler inserts

- **[D]** `cmd/internal/obj/x86/obj6.go:617-857` (preprocess) and `:914-1125` (stacksplit). It produces one of three check sequences:
  - frame ≤ `StackSmall`=128: `CMPQ SP, 16(g)`;
  - frame ≤ `StackBig`=4096: `LEAQ -(F-128)(SP), R12; CMPQ R12, 16(g)`;
  - larger: a subtraction that guards against underflow.

  The constants are in `internal/abi/stack.go`. Each sequence ends in `JLS` to an out-of-line `CALL runtime.morestack_noctxt` (or `morestack` for NEEDCTXT), followed by `JMP` back to the entry.
- **[D]** In an ABI0 function, g is loaded from TLS into **R14** (`loadG`, `obj6.go:873-912`). The temporary is R12 (`REGENTRYTMP0`, `x86/a.out.go:266`).
- **[V]** Objdump of a Ki function:

  ```text
  MOVQ FS:0xfffffff8, R14; CMPQ SP, 0x10(R14); JBE …; PUSHQ BP; MOVQ SP, BP; SUBQ $N, SP
  … CALL runtime.morestack_noctxt.abi0(SB); JMP entry
  ```
- **[D]** BP is saved and set up automatically when the frame is larger than 0, unless the function is NOFRAME or a frameless leaf (`obj6.go:636-650`; `doc/asm.html:752-756`).
- **[D] Gotcha: automatic NOSPLIT.** On amd64, a function with `autoffset < 128` and no `CALL` is marked NOSPLIT silently (`obj6.go:663-692`). It loses its prologue. This bites in two places:
  - a "poll" written as a tiny leaf asm function is not a safe point;
  - a leaf function that tail-calls itself becomes a nosplit cycle, see §6.

### 2.2 NOSPLIT limits

- **[D]** A chain of NOSPLIT functions may use `StackNosplitBase = 800` bytes × `StackGuardMultiplier`:
  - 1 normally;
  - +1 under `-race`;
  - +1 on AIX/OpenBSD.

  Sources: `internal/abi/stack.go`, `cmd/internal/objabi/stack.go:12-32`, `runtime/stack.go:91-102`. Hence `stackGuard = stackNosplit + stackSystem + StackSmall`; `stackSystem` is 4096 on Windows (`stack.go:75`).
- **[D]** The linker checks this statically (`cmd/link/internal/ld/stackcheck.go`), with a limit of 800 − 8 = 792 on amd64.
  - Cycles count as infinite height (`stackCheckCycle`).
  - Indirect calls are assumed to land in a splittable function: "Assume that indirect/closure calls are always to splittable functions" (`:186-191`).
- **[V]** A NOSPLIT function that calls itself directly fails to link:

  ```text
  main.krec: nosplit stack over 792 byte limit
  main.krec<0>
      grows 24 bytes, calls main.krec<0>
      infinite cycle
  ```

### 2.3 Frame size

- **[D]** The frame size must be a multiple of 8 (`obj6.go:718`, "unaligned stack size"). Any size gets a valid check sequence.
- **[D]** On growth, `newstack` grows the stack enough for the function's largest SP delta: `funcMaxSPDelta` (`runtime/stack.go:1150-1160`).
- **[I]** The practical limit is not the assembler. It is that every frame in a deep chain gets copied on each doubling, so Ki frames should stay small.

### 2.4 Deep recursion and growth by copying

- **[V]** Ki-style recursion `kdeep(n) = 1 + kdeep(n-1)`, with a 48-byte frame and ABI0 stack arguments, worked at:
  - 10⁶ frames on the main goroutine;
  - 2×10⁶ frames in a fresh goroutine. That stack went from a few KiB to 128 MiB (`/memory/classes/heap/stacks:bytes` read 134,676,480 at the end).

  At the bottom it ran `runtime.GC()` and a Go-side recursion that forced further copies. It passed with `GODEBUG=gccheckmark=1,invalidptr=1`, and on Windows.
- **[D] The maximum.** It is 1 GB on 64-bit (`runtime/proc.go:160`), changeable with `runtime/debug.SetMaxStack`. Going past it is a **fatal error, not a panic** (`stack.go:1171-1179`).

  **[V]** With `SetMaxStack(64<<20)`, `kdeep(2e6)` printed:

  ```text
  runtime: goroutine stack exceeds 67108864-byte limit
  fatal error: stack overflow
  ```

  [I] Ki's old "exit 103 on stack overflow" cannot be built from this. Either accept Go's fatal error with a Ki-specific message printed first, which needs a hook that doesn't exist, or add a Ki-level depth or size check. The latter would need `g.stack.hi` (offset 8, internal), or an entry SP recorded by kirt per fiber.
- **[D] What a copy adjusts.** `copystack` adjusts exactly three things:
  - the saved BP in each frame (`stack.go:701-735`);
  - pointer slots named by the stack maps (`adjustpointers`, `:652-697`);
  - `sched.ctxt` and `sched.bp` (`adjustctxt`, `:776-800`).

  Nothing else is touched.

### 2.5 Registers do not survive morestack

- **[D]** `runtime·morestack` (`runtime/asm_amd64.s:628-683`) uses CX, DI, BX, AX and SI. It saves only `DX → g.sched.ctxt` (`:640`), and `morestack_noctxt` zeroes DX first (`:686-688`).
- **[D]** `gogo` restores SP, DX, BP and R14 and jumps (`asm_amd64.s:441-452`).
- **[D]** ABIInternal works around this by spilling register arguments into caller-reserved spill slots before calling morestack: "The ABI currently reserves spill space for argument registers so the compiler can statically generate an argument spill path before calling into runtime.morestack" (`abi-internal.md:883-886`). ABI0 asm gets no such path.
- **[V]** An asm function receiving its argument in BX, behind a normal prologue, worked while the stack had room. In a fresh goroutine the first morestack replaced BX with the `m` pointer. The recursion ran away: `runtime: goroutine stack exceeds 1000000000-byte limit … fatal error: stack overflow`. Same result on Windows.
- **[V]** An argument in **DX** of a `NEEDCTXT` function survives: 10⁶ frames, with growth and GC (`n2b`). **DX is the only free argument register at a normal prologue.**

### 2.6 What generated code must never do

- **Keep a stack address across a call.** [D] "Because stack resizing is implemented by moving the stack, the stack pointer may change during any function call: even pointers to stack data must not be kept in local variables" (`doc/asm.html:608-611`). [V] `n10`:

  ```text
  slot address before call 0x3d4486cbcf80, after call 0x3d448717ff80; live slot = 0x9999, read via saved address = 0x1234
  ```

  The read went to the freed old stack. The same holds for storing a stack address into the Ki heap or a global.
- **Write SP other than by the frame, PUSH/POP or `ADJSP`.**
  - [D] Any other write marks the function `FuncFlagSPWrite` (auto-SPWRITE, `obj6.go:762-776`; `internal/abi/symtab.go:49-56`).
  - [D] A non-innermost SPWRITE frame during a GC stack scan is fatal (`runtime/traceback.go:338-367`).
  - [V] `SUBQ $16, SP; CALL ·gcNow` produced `traceback: unexpected SPWRITE function main.kspw` / `fatal error: traceback` from `runtime.scanstack`.
- **Switch to a stack the Go runtime does not own.** See §2.7.

### 2.7 Running Ki code on Ki-owned stacks: tested, and it fails

**[V]** I wrote an asm trampoline that sets SP to the top of an `mmap`'d 1 MiB region, calls a Go function there, and switches back.

| Variant | Result |
|---|---|
| Region **below** the goroutine stack (hint 0x10000000) | `runtime: split stack overflow: 0x100fff90 < 0x13ed86822000` / `fatal error: runtime: split stack overflow`: the callee's prologue saw SP < stackguard0 and `newstack` rejected it (`stack.go:1105-1120`) |
| Region **above**, Go callee doesn't trigger GC | It "works", by luck |
| Region above, callee runs `runtime.GC()` | `traceback: unexpected SPWRITE function main.onStack` / `fatal error: traceback`, from `gcBgMarkWorker → scanstack` |

[D] Async preemption also refuses any SP outside `gp.stack` (`preempt.go:406`). **Conclusion: the C++ §8.4 model cannot run under `go build`.** That model was native stack segments, `ki_switch` swapping rsp, and a `Cont` holding a detached chain of segments. Ki fibers that block in the middle of native Ki code must be goroutines. Cgo is the only sanctioned "foreign stack", and it switches to g0 with the runtime's cooperation.

---

## 3. The GC: what asm frames must declare

### 3.1 The rules

- **[D]** `doc/asm.html:560-618`:
  - A function needs no pointer information for its args if it is declared `$n-0` ("If a function has no arguments and no results, the pointer information can be omitted").
  - Otherwise "pointer information must be provided by a Go prototype … even for assembly functions not called directly from Go".
  - A frame with locals that executes `CALL` needs `NO_LOCAL_POINTERS`, and then "the local stack frame must not contain pointers".
- **[D]** The macros are in `runtime/funcdata.h`:
  - `GO_ARGS` = `FUNCDATA $0, go_args_stackmap(SB)`;
  - `NO_LOCAL_POINTERS` = `FUNCDATA $1, no_pointers_stackmap(SB)`;
  - `GO_RESULTS_INITIALIZED` = `PCDATA $1, $1`.
- **[D]** The assembler appends `FUNCDATA ArgsPointerMaps → <fn>.args_stackmap` and `ArgInfo` references to every ABI0 TEXT in the package, unconditionally (`cmd/internal/obj/plist.go:101-157`). It also synthesises the `no_pointers_stackmap` data (`:69-87`).
- **[D]** The runtime enforces this whenever it walks a frame (`runtime/stkframe.go:190-222`).

### 3.2 What happens when a rule is broken, or legitimately skipped

**[V]**

| Case | Outcome |
|---|---|
| `$48-16` frame + `CALL`, no `NO_LOCAL_POINTERS` (`n1`) | `runtime: frame main.kdeep untyped locals …+0x30` / `fatal error: missing stackmap` at the first stack copy |
| `$48-16` with **no Go prototype** (`n2`) | Links fine; vet says `function kdeepnp missing Go declaration`; at runtime `runtime: frame main.kdeepnp untyped args …+0x10` / `fatal error: missing stackmap` |
| `$16-0` / `$48-0`, no prototype, args read via `x+0(FP)` from the caller's outgoing area (`n2b`, `n14`) | Links; 10⁶ frames with growth + `runtime.GC()` + `gccheckmark` pass, on Linux and Windows. The undefined `.args_stackmap`/`.arginfo` references are tolerated |
| Prototype with all-`uintptr` params | Compiler emits an all-zero args map; `GO_RESULTS_INITIALIZED` never needed |

### 3.3 Why A0 is safe without prototypes

[I] With argsize 0, the argument words live in the *caller's* frame, so the runtime counts them as the caller's locals. They are not scanned, which is right, since they are `uintptr`. They are copied verbatim on growth. The callee reads them SP-relative after the copy. `n14` [V] exercises exactly this.

### 3.4 A Ki heap outside the Go GC

This matches the Perceus design: an `mmap`/`VirtualAlloc` arena, with Ki values as `uintptr`.

- **[V]** `kirt` allocated a 1 GiB `MAP_NORESERVE` arena (Windows: `VirtualAlloc` through `syscall.NewLazyDLL`). Asm wrote 1000 words into it through a `uintptr` returned by `kirt.Alloc`. GC was clean.
- **[D]** The GC ignores words that don't point into Go spans: `findObject` returns early when `spanOf(p) == nil` (`runtime/mbitmap.go:1362-1372`). So Ki pointers do no harm even in a slot the GC scans.
- **[V] Never put a Ki *immediate* in a Go pointer-typed slot.** `p := unsafe.Pointer(uintptr(3))` (a tagged Int) held across stack growth died with `runtime: bad pointer in frame main.hold … 0x3` / `fatal error: invalid pointer found on stack`. The check is `adjustpointers`, `stack.go:676-682`, with `minLegalPointer=4096`. **Ki values are `uintptr` in kirt's Go code too, never `unsafe.Pointer`.**
- **[I] DX.** `g.sched.ctxt` is scanned as a pointer (`mgcmark.go:963-968`). An *integer* in DX that happened to equal an address in a freed Go span could trip `invalidptr`. So DX should carry only Ki heap addresses or 0, never a raw immediate.
- **[I] Go objects Ki needs** (`*os.File`, `net.Conn`, timers) should live in kirt-side handle tables indexed by integers. The Ki heap then never holds Go pointers. Mixing Go pointers into Ki objects would require precise, PC-indexed pointer maps in every asm frame. The format exists (`FUNCDATA $1` plus `PCDATA $1` indexes into runtime-internal `stackmap` blobs), but it is undocumented and unstable.

### 3.5 Can asm frames be scanned conservatively?

- **[D] No.** Conservative scanning happens only for the frame of an asynchronously preempted function, its `asyncPreempt` frame, and `debugCallV2` (`runtime/mgcmark.go:1066-1115`).
- **[D]** Asm is never async-preempted, see §4.
- **[I]** Ki doesn't need conservative scanning anyway, because Ki frames hold no Go pointers.

### 3.6 Globals

- **[D]** `RODATA` for literals and immortal objects that are never written (the RC=0 "immortal" encoding never writes).
- **[D]** `NOPTR` for mutable asm data.
- **[D]** Anything holding Go pointers is defined in Go (`doc/asm.html:544-557`).

---

## 4. Preemption

### 4.1 How Go preempts

- **[D] Synchronous preemption.** `preemptone` sets `gp.preempt = true` and `gp.stackguard0 = stackPreempt`, a value larger than any SP (`runtime/proc.go:6866-6895`; `stack.go:133`). It then also tries `preemptM`.
- **[D]** sysmon does this for any goroutine running longer than `forcePreemptNS = 10ms` (`proc.go:6626-6628,6664`). GC's `suspendG` (`preempt.go:106`) does the same for stack scans and stop-the-world.
- **[D]** The next prologue check fails and `morestack → newstack` reaches the preempt branch (`stack.go:1093-1145`). From there:
  - `preemptPark` if `gp.preemptStop` (GC);
  - otherwise `gopreempt_m` ("Act like goroutine called runtime.Gosched").

  `execute` resets `stackguard0` when the goroutine runs again (`proc.go:3347-3348`).
- **[D] Asynchronous preemption.** It uses `SIGURG` (`runtime/signal_unix.go:74`); on Windows, `SuspendThread`/`GetThreadContext` in `preemptM` (`os_windows.go:1152`). It **refuses assembly**: "This is assembly code. Don't assume it's well-formed" (`preempt.go:450-459`, `f.flag&abi.FuncFlagAsm != 0`). It also refuses SP outside the goroutine stack (`:406`).

  When a signal hits Ki asm, the handler sees an unsafe point and returns. `gp.preempt` and `stackguard0` stay set, and the *next prologue or poll* takes the preemption.

### 4.2 Measured

**[V]** A goroutine runs a 3×10⁹-iteration asm loop. Main sleeps 50 ms, then times `runtime.GC()`.

| Loop body | GC latency (16 Ps) | `GOMAXPROCS=1`: Sleep(50ms) took | `asyncpreemptoff=1`: GC | ns/iter (Linux / Windows) |
|---|---|---|---|---|
| leaf, no poll | **769 ms** (≈ the whole loop) | **793 ms** (main starved) | 808 ms | 0.37–0.38 / 0.24 |
| inline poll on `g.stackguard0` | 0.74 ms | 60 ms | 0.79 ms | 0.36–0.39 / 0.25 |
| `CALL` a non-leaf asm fn (its prologue) | 0.81 ms | 61 ms | 16 ms | 1.34–1.38 / 1.21 |
| `CALL` a *leaf* Go fn via wrapper | 3.9 ms (luck: async hit inside Go code) | 138 ms | **1.32 s** | 2.6–2.9 / 2.3 |

Windows numbers are similar: no poll gave a 692 ms GC, the inline poll 8 ms.

**What the table shows.**

- A Ki loop without a safe point stalls every stop-the-world. With one P it starves every goroutine, timers included.
- The inline poll costs nothing measurable.
- Calling a small Go function is **not** a safe point: leaf Go functions have no prologue, and wrappers are NOSPLIT.

### 4.3 Prior art: wazero

wazero hit the same constraint. Its RATIONALE.md says:

- "runtime-generated machine code is also correctly considered unsafe for async preemption";
- it checks cancellation by calling a Go function at strategic points, because "native code never preempts".

### 4.4 What Ki code must do

- **Every non-leaf Ki function keeps its assembler prologue.** That is one preemption point per call, for free.
- **Every back-edge that does not pass through a prologue gets a poll:** `joinrec` loops, local loops, and self tail calls compiled as local jumps. (`RET ·f(SB)` tail calls re-enter f's prologue, see §6.) The poll is:

  ```asm
  CMPQ SP, 16(R14)      // R14 = g (loaded by the prologue; reload with MOVQ (TLS), R14 after any CALL)
  JLS  slowpoll         // stackguard0 == stackPreempt, or the stack is genuinely low
  ...
  slowpoll: spill live values; CALL e2∕kirt·Poll(SB)  (Go; or a non-leaf asm helper); MOVQ (TLS), R14; reload; JMP back
  ```

  `kirt.Poll` can simply call `runtime.Gosched()`. Its own prologue also services `preemptStop` for GC, since `newstack` → `preemptPark`.
- **[D] The undocumented part is the offset 16.** The g struct begins `stack stack; stackguard0 uintptr // offset known to liblink` (`runtime/runtime2.go:481-482`). The toolchain hardcodes `2*PtrSize` for "G.stackguard0" in `obj6.go:985,1014,1062` and `arm64/obj7.go:159`. It cannot change without changing the assembler, but no document promises it.
- **[V] `MOVQ (TLS), Rn` works in user asm.** The assembler rewrites it per OS:
  - linux exe: `FS:-8`;
  - PIE: `MOVQ $-8,AX; MOVQ FS:0(AX),AX`;
  - windows: `MOVQ runtime.tls_g(SB),AX; MOVQ GS:0(AX),AX; MOVQ 0(AX),AX`.

  The doc only shows `get_tls` for runtime code (`doc/asm.html:684-708`).
- **[V] Documented-only fallback:** `CALL` a non-leaf asm helper whose prologue is the safe point. It costs about 1 ns per back-edge. Keep it behind a compiler flag (`--poll=inline|call`).
- **[I] Signals need nothing from Ki code.** Handlers run on the gsignal stack and restore every register. The async attempt just fails.

---

## 5. Registers

### 5.1 amd64, inside an ABI0 Ki function body

- **[D] Free:** AX, BX, CX, DX, SI, DI, R8–R15 and X0–X15.
  - ABI0 defines R14 and X15 as undefined, and the transitions restore them (`abi-internal.md:417-421`).
  - "There are no callee-save registers, so a call may overwrite any register that doesn't have a fixed meaning" (`abi-internal.md:221-223`).
- **[V]** `kregs` sets R12, R13, R14, R15, BX and X15 to −1, calls Go through the wrapper, sets R14 and X15 to garbage again, and returns to Go. The program is correct. The wrapper and the post-call fixup restore R14 and X15.

**Conventions that matter:**

| Register | Rule for Ki code |
|---|---|
| R14 | Holds g right after an ABI0 prologue (loaded by it). ABIInternal pins it to g (`abi-internal.md:413`); after any `CALL`, reload from `(TLS)` before using it as g |
| X15 | Zero under ABIInternal; Ki may clobber it, the transitions re-zero it |
| DX | Closure context at a call (`abi-internal.md:410`); preserved across morestack **only** in `NEEDCTXT` functions |
| R12/R13 | Scratch; R12 is the prologue temp for frames > 128 (`x86/a.out.go:266-267`) |
| R15 | Free in exe/PIE. Under `-dynlink` (plugins, shared) any global access clobbers it and the assembler errors: "when dynamic linking, R15 is clobbered by a global variable access and is used here" (`obj6.go:1271-1300`). Avoid it if plugins are ever wanted |
| BP | "callee-save. The assembler automatically inserts BP save/restore when frame size is larger than zero. Using BP as a general purpose register is allowed, however it can interfere with sampling-based profiling" (`doc/asm.html:752-756`). [I] Don't use it: frame-pointer unwinding is used by the execution tracer and by profilers, `adjustframe` adjusts saved BPs, and `go vet`'s `framepointer` pass flags clobbering |
| SP | Hardware `0(SP)` vs pseudo `x-8(SP)`: different addresses (`doc/asm.html:224-234`). Writes only via frame, PUSH/POP or ADJSP (§2.6) |
| FP, SB, PC | Pseudo-registers (`doc/asm.html:146-205`) |

### 5.2 Flags, alignment and floats

- **[D]** The direction flag is clear at calls, the MXCSR control bits are fixed, and the status flags are scratch (`abi-internal.md:474-512`).
- **[D]** RSP is aligned to only **8 bytes** (`:450`). Don't use aligned SSE on stack slots.
- **[I] Floats.** The old design moved floats in general-purpose registers, and that still works. Boxed `Float` values are pointers anyway.

### 5.3 What this does to KiCC (C++ §9.3)

- **[I]** The worker context pinned in r15, and rbx/r12–r14 surviving "leaf runtime calls", are gone.
- The worker or fiber context becomes either an ordinary argument or a frame slot, reloaded after every call.
- With fibers as goroutines, the natural "context" is per goroutine. kirt can find it from g only through Go-side maps, or by passing it explicitly. [I] Passing it explicitly as a hidden argument is the cheap option.

---

## 6. Tail calls, indirect calls, closures

### 6.1 Tail calls

- **[D]** `RET target(SB)` in a framed function becomes the epilogue followed by `JMP target` ("retjmp", `obj6.go:815-856`). The parser accepts the operand (`cmd/asm/internal/asm/asm.go:609`).
- **[V] Objdump of `ktail`:**

  ```text
  MOVQ AX, 0x10(SP); MOVQ CX, 0x18(SP); POPQ BP; JMP main.ktail.abi0(SB)
  ```

  `ktail(1e8, 0)` returned 5000000050000000 on the main goroutine. A stack-growing version would need 1.6 GB and would have hit the 1 GB fatal limit, so the stack stayed flat.
- **[V]** Every such tail call re-enters the callee's prologue, so **tail-call loops are preemptible for free.**
- **The argument rule in ABI0.** The callee's arguments must fit in *our* incoming area, which our caller allocated: overwrite `a+0(FP)…` and then `RET ·callee(SB)`.

  [I] The Ki compiler needs a rule: tail-call only when callee arity ≤ caller arity. Otherwise either reserve a uniform minimum outgoing area at every call site (say 6 words, so tail calls of up to 6 arguments always work) or fall back to a normal call.
- **[V] Gotcha.** A *leaf* function with a small frame that tail-calls itself is auto-NOSPLIT (§2.1), and the linker rejects it: `main.ktail: nosplit stack over 792 byte limit … grows 8 bytes, calls main.ktail<0> … infinite cycle`. Any `CALL` in the body fixes it, even an unreachable one. Every Ki loop function has one anyway: its poll slow path.

### 6.2 Indirect calls

- **[V]** `CALL CX` and code addresses taken with `LEAQ ·f(SB), R11` or loaded from RODATA tables work.
- **[D]** The linker treats the target as splittable (`stackcheck.go:186-191`).
- **[V] Code addresses are ABI0 entry points.** Never hand them to Go as `func` values: those must point at ABIInternal code (`abi-internal.md:354-366`).

### 6.3 Closures

- **[V]** A Ki closure is an object in the Ki arena: `[code ptr][captures…]`. The caller loads the code pointer from `0(DX)` and does `CALL CX` with DX = closure.
- The callee is `NEEDCTXT`, so its morestack preserves DX (`obj6.go:1095-1102`, `asm_amd64.s:640`). It reads `8(DX)`.
- `kclosure(k=1000, 7) = 1007` [V]. The same convention held across 10⁶-deep recursion with growth (`n2b`) [V].
- **[D]** This is exactly Go's own closure convention (`abi-internal.md:354-366`).

---

## 7. Build mechanics

### 7.1 Layout per Ki program

[I] The design below. [V] means that this exact shape was built.

```text
<build dir>/go.mod        module kiprog ; go 1.26 ; require ki.dev/kirt v0.0.0 ; replace ki.dev/kirt => /path/to/kirt
<build dir>/main.go       package main; func kimain() ; func main() { kirt.Run(kimain) } (+ uintptr prototypes for entry points only)
<build dir>/kiabi.h       #defines emitted by the Ki compiler (kirt layouts, constants)
<build dir>/prog_amd64.s  all Ki functions (one file), #include "textflag.h" / "funcdata.h" / "kiabi.h"
```

Run it with `GOFLAGS=-mod=mod go build -trimpath -o prog .` [I]. kirt is a normal Go package, and its compiled archive in GOCACHE is reused across Ki programs.

[V] I built exactly this shape twice, as two separately generated program modules (`module kiprog`, `require e2 v0.0.0`, `replace e2 => ../e2`). Each one's asm calls `e2∕kirt·Poll(SB)`. In both action graphs the `e2/kirt` build action ran no command, which means it was a cache hit. Both binaries ran correctly.

### 7.2 What `go build` runs

- **[D]**
  - one `asm -gensymabis` over **all** `.s` files;
  - `compile -symabis … -asmhdr go_asm.h` over the Go files;
  - **one `asm` process per `.s` file, sequentially** (`cmd/go/internal/work/gc.go:406-420`, the loop over `sfiles`);
  - pack, then link.

### 7.3 Measured with 10⁴ generated Ki-shaped functions

[V] Each function is about 30 lines: overflow-checked arithmetic, an inline poll, a call to the next function (chains of 10), and a kirt call in the slow path. That is 300k lines and 4.8 MB of asm. The table shows wall time for an edit-rebuild cycle, with std and kirt cached, 3 runs each:

| Variant | Wall | of which build / link |
|---|---|---|
| 1 `.s`, **no prototypes** (A0) | **0.93–0.97 s** | 0.70 / 0.13 s |
| 1 `.s`, 10⁴ prototypes | 1.63–1.68 s | 1.38 / 0.15 s |
| 100 `.s`, no prototypes | 1.15–1.20 s | |
| 100 `.s`, prototypes | 1.84–1.88 s | |
| No change (cache hit) | 0.10 s | |

Running the tools by hand on the 1-file variant:

- `-gensymabis`: 0.15 s;
- `asm`: 0.51 s, 158 MB RSS, 12 MB `.o`;
- compiling 10⁴ prototypes: 1.63 s CPU and 241 MB, mostly building the 10,001 ABIInternal wrappers, which the linker then dead-strips.

The binary was 6.3 MB.

**Recommendation:** one `.s` per program, with no prototypes except the entry points.

### 7.4 `go vet` and the `asmdecl` pass

- **[V]** With prototypes, `go vet` was clean in 1.4 s and checked the offsets and sizes of all 10⁴ functions.
- **[V]** Without, it printed 10⁴ `function kfN missing Go declaration` warnings.
- **[D]** `go build` does not run vet, and the vet subset that `go test` runs excludes asmdecl.
- **[I] Suggestion:** a backend debug mode (`ki build --emit-protos`) that emits prototypes and real argsizes, so `go vet` can check the backend's FP offsets in CI.

### 7.5 Source positions

- **[V]** `#line N "file.ki"` is honoured by `go tool asm` (`cmd/asm/internal/lex/input.go:430-454`). Go tracebacks then print `examples/overflow.ki:7` for Ki frames, and `go tool objdump` attributes instructions to `overflow.ki`.
- **Observed off-by-one in 1.26.5:** the instruction immediately after `#line 7` was attributed to line **6**. The emitter should write `#line N+1`, or a test should pin the convention down.
- **[D]** The `#line` filename goes through `objabi.AbsFile(..., *flags.TrimPath)` (`input.go:453`), so with `-trimpath` tracebacks show the trimmed path. Emit paths relative to the Ki project root.

### 7.6 Other build modes

- **[V]** `-buildmode=pie` and `-race` builds of the whole test program, including contract B, run correctly.

---

## 8. Prior art

| Project | What it shows | Source |
|---|---|---|
| Go runtime asm (`runtime/asm_amd64.s`, 72 TEXTs; `NO_LOCAL_POINTERS` used 50× across `runtime/*.s`) | The canonical patterns: morestack/gogo, NOSPLIT discipline, ABI selectors (runtime-only) | [D] |
| `math/big/arith_amd64.s` | **Generated** Plan 9 asm ("Code generated by 'go generate' (with ./internal/asmgen). DO NOT EDIT."), leaf ABI0 functions `TEXT ·addVV(SB), NOSPLIT, $0` with `//go:noescape` prototypes in `arith_decl.go`. A Go program that emits Go asm is now an in-tree practice (`math/big/internal/asmgen`, one file per arch) | [D] |
| `crypto/*/_asm` (sha1, md5, sha256, sha512, aes, gcm, nistec, edwards25519, bigmod, sha3) | Std asm generated with **avo** (`crypto/sha1/_asm/go.mod`: `require github.com/mmcloughlin/avo v0.6.0`) | [D] |
| avo (github.com/mmcloughlin/avo) | Virtual registers + register allocation, generates Go stub files, auto-loads arguments/stores results; "APIs subject to change while avo is still in an experimental phase". Targets leaf kernels, not whole programs | [D] README |
| c2goasm (github.com/minio/c2goasm) | Converts clang output to Go asm; limitations: 64-bit args only, ≤14 args, "Generally no `call` statements"; the stack is reserved by the generated code. **Archived 2021-12-01**: an example of what goes wrong when you ignore the stack/GC contract | [D] README |
| wazero (tetratelabs/wazero) | A JIT/AOT under the Go runtime; native code is entered through a Go-asm trampoline, is "correctly considered unsafe for async preemption", and exits to Go for cancellation checks because "native code never preempts" | [D] RATIONALE.md |
| `cmd/internal/obj` (+ `x86/obj6.go`, `arm64/obj7.go`) | The real spec of what the assembler inserts (prologues, BP, retjmp, SPWRITE, TLS rewriting) | [D] |
| `cmd/compile/abi-internal.md`, design docs 27539 and 40724, `doc/asm.html` | ABI definitions | [D] |

[I] Nobody I found compiles a whole language to Go assembly. The closest are generated leaf kernels (math/big, avo) and wazero's JIT. Ki would be the first whole-program user of these rules. The rules are all in the source, but almost nobody has exercised the corners: no-prototype functions, retjmp, and NEEDCTXT as an argument channel.

---

## 9. arm64 and windows/amd64, for the final part

### 9.1 windows/amd64

**[V] The same `.s` file builds and runs unchanged on windows/amd64.** All of these passed:

- `basic`, 2×10⁶-frame growth, contract B and B′, preemption, overflow, `n6`, `n10`, `n14`.

The differences:

- **TLS.** `MOVQ (TLS)` becomes a 3-instruction sequence through `runtime.tls_g` and GS (`obj6.go:45-71`, `CanUse1InsnTLS` is false on Windows) [V objdump]. It is slower but transparent.
- **[D]** `stackSystem = 4096` on Windows (`stack.go:75`), so the stack guard is bigger. The nosplit limit is the same.
- **[D]** Async preemption uses `SuspendThread`/`GetThreadContext` (`os_windows.go:1152`), but `isAsyncSafePoint` is the same, so asm is still never async-preempted [V: a 692 ms GC stall with no poll].
- **[D]** The Go ABI is the same on every OS. The Win64 C ABI matters only at syscalls and DLL calls, which kirt makes through `syscall`.
- **[V]** kirt needs an OS split for the arena: `mmap` vs `VirtualAlloc` through `syscall.NewLazyDLL("kernel32.dll")`.

### 9.2 arm64

[D] `abi-internal.md:514-601`, `doc/asm.html:845-853`, `arm64/obj7.go`.

- **Reserved registers.**
  - **R28 = g in both ABIs.** In asm it is written `g`, as in `MOVD 16(g), R2` for the poll.
  - R27 = REGTMP, used by the assembler when expanding instructions.
  - R16/R17 = linker trampoline scratch, and the prologue uses R16: [V objdump] `MOVD 16(R28), R16; CMP R16, RSP; BLS …`.
  - R18 is reserved (`R18_PLATFORM`).
  - R26 = closure context.
  - R29 = frame pointer, R30 = link register (saved by the prologue), ZR.
- **The stack.**
  - RSP must be 16-byte aligned.
  - After the prologue, 0(RSP) holds the saved LR and −8(RSP) the caller's FP. **ABI0 outgoing arguments therefore start at `8(RSP)`, not `0(SP)`.** [V] The arm64 `kfib` built that way.
  - Leaf functions with no frame omit the LR save. A leaf with a small frame is auto-NOSPLIT, as on amd64 (`obj7.go:570-573`).
- **[V]** The arm64 `kfib`, `ktail` (`RET ·ktail(SB)` → restore R29/R30 → `JMP`) and `kspinpoll` cross-build with `GOARCH=arm64` and pass `go vet`. **Not run**: there is no qemu on this machine. Running them is the first task of the arm64 chapter (`apt install qemu-user`, or a real arm64 host).
- **[D]** The arm64 `morestack` likewise saves only R26 (ctxt) into `g.sched` (`runtime/asm_arm64.s:469`). Register arguments behind a normal prologue are lost there too, for the same reason as on amd64 [I].

---

## 10. Contract B′: register arguments, as an optimisation

Measured on `fib(32)`, about 7×10⁶ calls, Linux, two runs:

- Go's own ABIInternal: **1.47–1.63 ns/call**;
- Ki **A** (ABI0 stack arguments): **2.00–2.16**;
- Ki **B/B′** (register arguments): **1.28–1.63** (B′ 1.29–1.33).

Windows: Go 1.20, A 1.28, B 1.06. [V]

**How B′ works (`n15`, verified on Linux and Windows):**

- Ki functions take their arguments in AX, BX, … and return in AX.
- R14 = g is an invariant: Ki code never writes R14, entry stubs load it, and code reloads it after calls into Go.
- `TEXT ·f(SB), NOSPLIT, $F-0`, with F ≤ about 700 bytes so it fits the nosplit zone. The frame is allocated before the check, then:

  ```asm
  check: CMPQ SP, 16(R14); JLS grow
  ...
  grow:  spill arg registers to the frame; CALL ·kigrow(SB); MOVQ (TLS), R14; reload; JMP check
  ```
- `kigrow` is an ordinary *splittable* asm function, `TEXT ·kigrow(SB), $4096-0`. Its own prologue does the growth, sized by `funcMaxSPDelta` (`stack.go:1150-1160`), and the preemption.
- Ki→Ki calls are **indirect** (`LEAQ ·g(SB), R11; CALL R11`). Direct calls between NOSPLIT functions are rejected as nosplit cycles (§2.2); indirect ones are assumed splittable.
- [V] GC latency while a B′ loop polls through `kigrow`: 1.3–1.6 ms.

**Contract B** (verified too) calls `runtime·morestack_noctxt` directly instead of `kigrow`. It is not worth the extra dependency on the runtime's linkname leniency.

**B′ depends on two things that are not promised:**

1. the `g.stackguard0` offset (§4.4), which A0's inline poll also uses;
2. the linker treating indirect calls as splittable (`stackcheck.go:186-191`).

It also takes the frame-pointer and pcsp bookkeeping out of the documented path for the check itself.

[I] The ~0.7 ns per call is real for call-heavy code. Most of it should come back from Ki's own inlining and from A0's "result in AX" anyway. **Recommendation:** B′ is an experiment in the performance part, behind `--kiabi=b`, with a CI smoke test (growth, GC, preemption) on every Go upgrade. A0 is the default.

---

## 11. Recommended contract: KiABI-A0 (amd64; arm64 analogous)

### 11.1 Ki functions

- One `TEXT ·k<mangled>(SB), $F-0` per Ki function, in a single generated `prog_amd64.s`.
- `F` is a multiple of 8 and covers spill slots, locals and the outgoing area.
- No Go prototypes, except entry points called from Go. Those have `uintptr`-only signatures and real argsizes.
- Closure entry points add `NEEDCTXT`.

### 11.2 Calling convention

- **Arguments:** the caller stores argument *i* at `8*i(SP)` (hardware SP, bottom of its frame). The callee reads `a_i+8*i(FP)`.
- **DX:** the closure or environment pointer: a Ki heap address or 0, never an immediate.
- **Results:** in AX, with more values in BX, CX… if needed. Registers are safe at `RET`, because nothing runs between the callee's epilogue and the caller.
- **No register survives any `CALL`.** Spill to frame slots.

### 11.3 Frames and GC

- Every framed function that contains a `CALL` executes `NO_LOCAL_POINTERS`. Emit it unconditionally; it is free.
- Every Ki value is a `uintptr`: in asm frames, in prototypes and in kirt's Go code.
- The Ki heap lives in an OS-allocated arena outside the Go GC. Go objects are reached through integer handles.
- No `GO_ARGS` and no `GO_RESULTS_INITIALIZED` are needed.

### 11.4 Stack

- Rely on the assembler prologue: growth by copying, up to `debug.SetMaxStack`, default 1 GB.
- Never take a stack address across a call, and never store one in the heap.
- Never write SP outside frame, PUSH/POP or `ADJSP`.
- Never switch stacks.
- Leaf functions may be auto-NOSPLIT, as long as they contain no tail-call cycle.

### 11.5 Preemption

- Function entries are safe points.
- Every back-edge that bypasses a prologue gets the inline poll: `CMPQ SP, 16(R14); JLS slow`, with a reload from `(TLS)` after calls. The slow path spills and calls `kirt·Poll`, which runs `runtime.Gosched()`.
- The documented-only fallback is `CALL` to a non-leaf asm helper.
- Long runtime operations in kirt are ordinary Go, so they are preemptible by themselves.

### 11.6 Tail calls

- `RET ·callee(SB)` after writing the callee's arguments into our own incoming area.
- Allowed when callee arity ≤ ours. Otherwise the call site reserves a uniform minimum outgoing area, or falls back to a normal call.

### 11.7 Calling the runtime

- kirt exposes `func Name(args… uintptr) (uintptr…)` with a one-argument `//go:linkname Name`, or with an ABI0 reference from a kirt `.s` file, so the `.abi0` wrapper exists.
- Generated code calls `CALL ki∕kirt·Name(SB)`: full import path, `∕` and `·`, ABI0 stack arguments.
- Never call the Go runtime directly.

### 11.8 Registers

- Never touch BP. Avoid R15 if `-dynlink` is ever wanted.
- R14 is g after the prologue. X15 is free.
- Don't rely on 16-byte SP alignment.

### 11.9 Data

- `RODATA` for literals, immortal objects and code-pointer tables. `NOPTR` for mutable asm data.
- `kiabi.h` holds kirt layouts and is generated by the Ki compiler.

### 11.10 Debugging

- `#line` directives for Ki positions, with the off-by-one noted in §7.5.

### 11.11 Build

- `go build` in a generated module that `replace`s kirt to its local path.
- One `.s` per program. Expect about 1 s per edit-rebuild cycle at 10⁴ functions.

### 11.12 Consequences for the C++-era design (for Álvaro to decide)

- **§8.4 fibers, stacks, continuations.**
  - A Ki fiber that runs native code is a goroutine.
  - A `Cont` cannot own detached native stack segments.
  - Options for handlers with control clauses:
    - run the handler body in its own goroutine, with `perform`/`resume` as handoffs (one-shot fits naturally, at about 110 ns per switch; a measured round trip over two unbuffered channels is 222 ns [V]);
    - a selective CPS transform of effectful code;
    - keep the inline path for tail-resumptive handlers only.
- **§8.5 scheduler.**
  - The Go scheduler moves goroutines.
  - A Ki work-stealing scheduler for native code becomes a *gate* on top: N run tokens, Ki run queues decide which fiber holds a token, and parking is done with Go primitives.
  - The deterministic mode is one token plus seeded choice.
  - The VM and the interpreter keep fully custom scheduling, because their fibers are heap state.
- **§9.3 KiCC.**
  - The convention is replaced by A0: no r15 context, no callee-saved registers, DX for closures.
  - Overflow is Go's fatal error unless Ki adds its own depth check.
  - The frametable for dropping continuations needs a new design. **The Go runtime never unwinds a parked goroutine.** If a handler body runs in its own goroutine and the one-shot `Cont` is dropped without `resume`, that goroutine stays in `gopark` forever. Its stack, and every Ki reference its frames hold, then leak, because RC is never decremented. The only unwind primitive is `runtime.Goexit`, and only the goroutine itself can call it. So in the goroutine model, "dropping a `Cont`" must mean *resuming it with an unwind token*: the suspended code runs Ki-generated cleanup and exits. What the C++ design did by walking suspension maps now has to be done by running code [D: `runtime.Goexit` docs; I: the rest].

---

## 12. Risks

1. **Architectural.** Fibers must be goroutines and native continuations cannot be stack segments (§2.7, §11.12). This changes the scheduler and effects chapters and their performance story: a goroutine handoff costs about 110 ns, against a few ns for a raw stack switch.
2. **Undocumented offset.** `g.stackguard0` at offset 16 (A0's inline poll, B′). It is hardcoded in the Go assembler itself, so it is very stable, but not promised. Mitigation: CI test plus the documented `CALL`-poll fallback.
3. **Polls are mandatory for correctness.** A missing back-edge poll is not just slow: it stalls every GC stop-the-world, and with one P it starves the process (§4.2). This needs a compiler invariant and a test, for example a stress test that runs `runtime.GC()` in a loop with a watchdog.
4. **Fatal stack overflow.** It is not recoverable (§2.4), so Ki's "exit 103" semantics need their own mechanism, or a change to the spec.
5. **Prototype-free A0** depends on toolchain tolerance: undefined `.args_stackmap` and `.arginfo` references, and FP references beyond the declared argsize. [V] in 1.26.5. [I] It is consistent with `doc/asm.html`'s `$n-0` rule, but vet flags every function. Mitigation: the `--emit-protos` CI mode.
6. **Linkname policy can tighten.** Go 1.23 already did, in #67401. Ki only references non-std symbols (kirt) and never the runtime, so the exposure is small. Contract B's `runtime·morestack_noctxt` reference is the exposed one, which is why B′ exists.
7. **Build time scales with program size.** About 1 s per 10⁴ functions, plus 0.1 s of link, and `asm` uses about 16 KB of RSS per function. Huge programs (10⁵ functions) may want splitting into several packages so the build parallelises. That needs cross-package ABI0 references, which work (§1.3).
8. **arm64 is unverified at run time** (no qemu). Everything there is documented or cross-built only.
9. **`#line` off-by-one and debugger support.** Delve and gdb will see `.ki` positions but no Ki variables. There is no DWARF for asm locals [I].
10. **Dropped continuations leak in the goroutine model.** A `Cont` dropped without `resume` leaves its goroutine parked forever, holding its stack and every Ki object its frames own (§11.12). Every drop path needs an "unwind" resumption, plus a test: a census at exit, and a check that the goroutine count returns to zero. The effects and scheduler topics have to design this.
11. **Microbenchmark caveat.** The call-cost numbers (A ≈ 1.3× Go's ABIInternal, B′ ≈ 1.0×) come from one CPU and a fib microbenchmark. Re-measure in the performance part with real Ki programs.

---

## 13. Verification transcripts (summarised)

All experiments ran in a new scratch directory (deleted afterwards), with `GOCACHE`/`GOTMPDIR` pointed inside it. The two modules were `e1` (cross-package wrapper rules) and `e2` (`kirt` plus test packages).

| Exp | What | Command | Result |
|---|---|---|---|
| E1 | asm in `main` → plain `kirt.Poll` | `go build ./plain` | `relocation target e1/kirt.Poll not defined for ABI0 (but is defined for ABIInternal)` |
| E1 | → push-linknamed / kirt-asm-referenced | `go build ./pushed ./ref` | links; `asm returned 42 polls 1`; `nm`: `kirt.PollPushed.abi0` |
| E1 | → `runtime·Gosched` | `go build ./gosched` | `relocation target runtime.Gosched not defined for ABI0 …` |
| E1 | Go `main` → asm in `kirt` | objdump | `CALL e1/kirt.AsmId.abi0(SB)` (direct) |
| ok basic | fib, regs clobber, retjmp 1e8, table, closure, contract B, TLS, arena | `./ok.bin basic` (Linux and `.exe`) | all correct: `kfib(30) = 832040`, `kregs(41) = 42`, `ktail = 5000000050000000`, `kclosure = 1007`, `g via (TLS) == R14 at NOSPLIT entry` |
| ok deep | 1e6 / 2e6 frames + GC + Go growth | `GODEBUG=gccheckmark=1,invalidptr=1 ./ok.bin deepgo 2000000` | `goroutine kdeep: 2000000`, stacks 134,676,480 B; Windows same |
| ok overflow | `SetMaxStack(64 MiB)` | `./ok.bin overflow` | `goroutine stack exceeds 67108864-byte limit` / `fatal error: stack overflow` |
| ok preempt | GC latency table §4.2 | `./ok.bin preempt 3000000000` (+`GOMAXPROCS=1`, `asyncpreemptoff=1`, Windows) | as in §4.2 |
| ok bench | poll and call costs, fib A/B | `./ok.bin bench 300000000` ×2 + Windows | as in §1.3/§4.2/§10 |
| n1 | no `NO_LOCAL_POINTERS` | run | `untyped locals` / `fatal error: missing stackmap` |
| n2 | args, no prototype | vet + run | vet `missing Go declaration`; `untyped args` / `missing stackmap` |
| n2b / n14 | `$N-0` no prototype (DX arg / FP args) 1e6 deep + GC | run (Linux and Windows) | `1000000` |
| n3 | `SUBQ $16, SP` around a GC | run | `traceback: unexpected SPWRITE function main.kspw` |
| n4 / n5 | NOSPLIT self-call / leaf self tail call | build | `nosplit stack over 792 byte limit … infinite cycle` |
| n6 | arg in BX behind prologue | run | runaway → `goroutine stack exceeds 1000000000-byte limit` |
| n10 | stack address kept across growth | run | stale read `0x1234` vs live `0x9999`, addresses differ |
| n11 | tagged immediate in `unsafe.Pointer` | run | `invalid pointer found on stack` |
| n12 | `go:noescape` | `-gcflags=-m` | `leaking param: p` vs `p does not escape` |
| n13 | `#line` | panic traceback / objdump | `examples/overflow.ki:7`; first line after `#line 7` → 6 |
| n15 | contract B′ | run (Linux and Windows) | fib correct, GC 1.3–1.6 ms, 1.29–1.33 ns/call |
| e7 | Go code on mmap'd stack | run low / high+GC | `split stack overflow` / `unexpected SPWRITE function main.onStack` |
| prog1/prog2 | two separate generated modules with `replace e2 => ../e2` | `go build -debug-actiongraph` | kirt build action ran no command (cache hit); `kimain = 1` / `kimain = 2` |
| gen | 10⁴ functions, 4 variants | `go build -debug-actiongraph`, `go vet` | table §7.3 |
| arm | arm64 kfib/ktail/poll | `GOARCH=arm64 go build && go vet` | builds, vet clean, **not run** |
| pie/race | `-buildmode=pie`, `-race` | run | correct |
| pp | goroutine handoff | `go run ./pp` | 222–224 ns per round trip |

---

## 14. Sources

**Go source tree (1.26.5, `$GOROOT/src`):**

- Docs:
  - `../doc/asm.html` (lines 138–618, 736–853);
  - `cmd/compile/abi-internal.md` (1–26, 109–224, 354–366, 380–635, 879–919);
  - `cmd/compile/doc.go` (216–223 noescape, 262–300 linkname).
- Assembler and obj:
  - `cmd/internal/obj/x86/obj6.go` (45–71, 617–857, 870–1125, 1271–1300);
  - `cmd/internal/obj/x86/a.out.go:261-271`;
  - `cmd/internal/obj/arm64/obj7.go` (150–290, 475–590);
  - `cmd/internal/obj/plist.go:59-157`;
  - `cmd/internal/obj/objfile.go:357-362`;
  - `cmd/asm/internal/asm/parse.go:72,897`;
  - `cmd/asm/internal/asm/asm.go:609`;
  - `cmd/asm/internal/lex/input.go:430-454`;
  - `cmd/internal/objabi/pkgspecial.go:38-136`;
  - `cmd/internal/objabi/stack.go`;
  - `internal/abi/stack.go`;
  - `internal/abi/symtab.go:37-60`.
- Compiler:
  - `cmd/compile/internal/ssagen/abi.go:61-350`;
  - `cmd/compile/internal/ssagen/ssa.go:4985-4997`;
  - `cmd/compile/internal/amd64/ssa.go:185-200,1285-1313`.
- Linker:
  - `cmd/link/internal/loader/loader.go:2300-2534`;
  - `cmd/link/internal/ld/stackcheck.go`;
  - `cmd/link/internal/ld/main.go:101`.
- cmd/go: `cmd/go/internal/work/gc.go:406-450`.
- Runtime:
  - `runtime/stack.go` (75–137, 558–560, 600–800, 900, 1015–1194);
  - `runtime/stkframe.go:94-227`;
  - `runtime/asm_amd64.s` (433–452, 628–716);
  - `runtime/preempt.go:106,346-485`;
  - `runtime/proc.go` (160–168, 389, 3347–3348, 6626–6664, 6866–6895);
  - `runtime/traceback.go:330-380`;
  - `runtime/mgcmark.go:959-1115`;
  - `runtime/mbitmap.go:1362-1372`;
  - `runtime/runtime2.go:473-487`;
  - `runtime/signal_unix.go:50-74`;
  - `runtime/os_windows.go:1152`;
  - `runtime/funcdata.h`;
  - `runtime/textflag.h`.
- Prior art in the tree: `math/big/arith_amd64.s`, `math/big/arith_decl.go`, `math/big/internal/asmgen/`, `crypto/sha1/_asm/go.mod`.
- Vet analyzers: `cmd/vendor/golang.org/x/tools/go/analysis/passes/{asmdecl,framepointer}`.

**go.dev and GitHub:**

- go.dev/doc/go1.23 (Linker section);
- go.dev/doc/asm (same as `doc/asm.html`);
- go.dev/s/regabi;
- design docs go.dev/design/27539-internal-abi and go.dev/design/40724-register-calling;
- golang/go#67401;
- github.com/mmcloughlin/avo (README);
- github.com/minio/c2goasm (README, archived 2021-12-01);
- github.com/tetratelabs/wazero RATIONALE.md ("Compiler engine implementation", "context cancellation").
