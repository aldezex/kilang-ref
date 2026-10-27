# Reviewer conventions

Standing instructions for every reviewer subagent in `~/kilang-ref`: task reviews of code sessions and tool tasks, reviews of a chapter's prose, and scoped re-reviews after a fix round. Read this file first. The dispatch gives you the paths you need: the task brief, the implementer's or writer's report, the review package (the diff), and the file where you write your review; for a re-review, also the findings under verification.

## 1. The project

- **Ki** is a pure functional language with algebraic effects whose compiler is written in Go with the standard library only, built chapter by chapter alongside a Spanish book, *Ki: de cero a experto. Construye tu propio lenguaje funcional en Go*. Its one reader, Álvaro, learns Go from zero, is new to compilers, and knows Rust, TypeScript and Python.
- **Two repositories, one module path** (`github.com/aldezex/kilang`): `~/kilang-ref` → `aldezex/kilang-ref`, the reference you are reviewing, built by agents (code, tests, test cards, prose, tags, `bookx`); and `~/kilang` → `aldezex/kilang`, Álvaro's own repository, which no agent touches.
- **Authority:** the spec (`docs/superpowers/specs/2026-10-24-ki-design-go.md`) is binding; the plan argues from it; the brief is the plan's text for this task, followed by the plan's `## Global Constraints` (through `## Review Focus`), `## Convenciones del plan` and, for prose, `## Plantilla de las tareas de prosa`. Everything in the brief binds the task.
- **What the work must follow:** `docs/superpowers/process/implementer.md` for code and tool tasks, `docs/superpowers/process/prose.md` for prose. Read the one that applies: its rules are review criteria too.

## 2. How to review

1. Read the brief, then the report, then the review package **once**. The package holds the commit list, the stat summary and the full diff with 10 lines of context; its context lines are the changed files. Read a changed file separately only when a hunk you must judge is cut off, and say so.
2. **Do not trust the report.** It is a set of claims; verify each against the diff. A rationale in the report ("kept simple on purpose", "YAGNI") never lowers a finding's severity.
3. **Do not re-run what the report proves.** The report carries the RED/GREEN evidence and the `ci/ci-local.sh` output. Never re-run the suite, `go test -race`, `ci/ci-local.sh`, `ci/check-tags.sh` or `bookx verify` to confirm it. Run a command only when reading the code raises a specific doubt no existing run answers, and then the smallest one (`go test ./internal/syntax/ -run 'TestLexer/pipe_continues_line'`); name the doubt and the command in your review. If the evidence looks truncated, re-read the report file; if it is missing, that is a finding, and re-running is not a substitute.
4. **Stay inside the diff.** Look at code outside it only to check a concrete risk you can name (a changed contract and its call sites), one focused check per risk, and name the risk and the check. A requirement that lives in unchanged code or spans tasks is a ⚠️ item, not a reason to crawl the repository.

## 3. What to check in a code or tool task

- **Every value of the brief against the diff,** one by one: paths, package and function names, signatures, test and subtest names (string literals, exactly), messages, diagnostic codes, exit codes, `usage:` and `commands:` lines, header grammar, dump formats, card ids, the commit subject and the tag (from the package's commit list: one commit `chNN-sK: <summary>` for a session; the exact message the brief gives for a tool task).
- **Files:** every file the brief lists to create or modify has its hunk; a listed file with no hunk is a Missing finding. An unlisted file needs the reason the report gives, or it is an Extra finding.
- **Missing, extra, misunderstood:** requirements skipped or only claimed; features nobody asked for; the right feature built the wrong way.
- **TDD and the closing steps:** the report shows RED (the expected failure, for the expected reason) before GREEN; the tail of `ci/ci-local.sh` ends in `verify: OK`; every new test has its card; a session closed with `ci/session.sh`, so the package's commit list shows one commit `chNN-sK: <summary>` (after a fix round, `fixup!` commits on top of it) and the report gives the annotated tag `chNN-sK` on the last of them (the package does not show tags; `git rev-parse chNN-sK^{commit}` is the read-only check if the report is unclear). Missing evidence is a finding.
- **Cards** (`book/tests/chNN.tests`): one per new test, ids consecutive without gaps and with the file's `NN`, every field present, `kind` and `ref` right (`TestXxx/sub` exactly as declared; the `.ki` path for `golden`, `diag` and `run`, with the matching `// test:` line), `file` right. **Compare each card's `case` and `expect` with the test's code, assertion by assertion: a card must not claim more than its test checks.** A card that promises an exact value the test only checks with "contains", a count the test never asserts, or a bound nobody measures is Important. Card text is Spanish, «tú», with no gendered words referring to the reader.
- **Surviving mutants.** For each new test, name the plausible wrong implementations (off-by-one, `<` for `<=`, swapped operands or labels, wrong sign, missing sort, last element dropped, missing flush, wrong code, bytes counted instead of code points) and check that some test fails for each. Report a surviving mutant that matters as Important, with the mutant and the test case that would kill it. A test that passes with any implementation, asserts nothing, or checks what Go already guarantees is Important.
- **Shape of the book's tests:** subtest names are literals; table rows do not use `t.Run` and report with `t.Errorf("%s: …", row.name, …)`; one `TestXxx` per file, with all its subtests in that file; tests that use `internal/testsupport` live in the external test package, tests of unexported identifiers in a `*_internal_test.go` with a distinct function name and no `testsupport`; `// bookx: not-in-book` only where the brief puts it.
- **Code rules** (implementer.md §8): standard library only and no `require` line; `gofmt` and `go vet` clean as reported; Google-style names and `ID` suffixes; comments precise and never promising more than the code does; no duplicated helper or block of logic; recursion bounded or replaced by an explicit stack, including the passes over Core; CRLF, lone `\r`, BOM and the 4 GiB limit; flush after each progress line and before `os.Exit`; no output that depends on map order; diagnostics sorted by position; every sealed type switch ends in `default: panic("unreachable")`, and the "unreachable" claims the brief asks for have their test; a Ki panic is never a Go panic; passes do not mutate their input; goroutines and `unsafe` only where allowed; exit codes as specified; no `go run` to test `ki`.
- **The Review Focus** entries the brief assigns to this task have their named tests.
- **Rules:** no push, no tag other than the session's own, no `git stash`, nothing outside `build/` left behind, nothing read from `~/ki`.

## 4. What to check in a prose review

The chapter (`book/chNN-<slug>.md`) against the brief's task text and the template (prose.md restates it):

- Front matter `chapter`, `title` (Spanish, as the brief gives it), `part`, `hours`, `milestone` (the literal milestone of spec 12.5).
- The fixed sections, in order: `## Objetivo`, `## Teoría`, `## Tests primero`, `## Implementación` with one `### Sesión K: <título> (≈ H h)` per session carrying the hours of its task, `## Ejecútalo`, `## Ejercicios`, `## Preguntas de repaso`, `## Resumen`, `## Go en el trabajo`.
- Every theory topic and every exercise the brief lists is present; at least one `:::from Rust`, `:::from TS` or `:::from Python` box per new Go concept, Rust carrying the most weight.
- `## Tests primero` has `{{cards}}` and a sentence on how to use the cards (write the test, watch it fail, implement, and only then unfold the reference with `{{reftest}}`).
- Directives `{{file}}`, `{{diff}}` and `{{listing}}` cite **session tags only** (`chNN-sK`, never `chNN`). A concept appears complete the first time; a hint with a folded `:::solution` only for patterns the book already taught; interfaces are always complete. Each session says that, if the reader gets stuck or drifts too far, copying the file from `ref/chNN-sK` is fine, and ends with «Al terminar: tus tests en verde, commit y etiqueta `chNN-sK`; compárate con `ref/chNN-sK`».
- Code is explained, never dumped (an anti-goal of the spec).
- `:::exercise`, `:::solution`, `:::review`, `:::answer`, `:::from …`, `:::note` and `:::bug` are always in sequence and never nested: a `:::solution` starts after the `:::` that closes its `:::exercise`.
- 6–8 review questions in `:::review` with their `:::answer`, asking why rather than for definitions; a summary of 8–12 bullets; `## Go en el trabajo` on the brief's topic.
- Between 4 000 and 8 000 words (`wc -w`), in proportion to the chapter's hours; the report gives the count.
- «Tú» throughout, with no gendered word referring to the reader (`listo`, `seguro`, `bienvenido`, `atascado`, `el lector`…).
- **Technical claims:** the report lists each claim about Go or the machine with the command that verified it and its output. Pick the claims most likely to be wrong (version facts, outputs, exit codes, error texts, performance statements) and check them against that list; spot-check one or two yourself when the evidence is missing or doubtful, in a scratch directory under `build/` that you delete afterwards. Version facts say «en Go 1.27». Claims about real projects (Kubernetes, CPython, Lua…) need a source in the report; an unsourced claim is a finding.
- The writer's `bookx verify` and `bookx web --all` results are in the report; the commit is `book: chapter NN prose`, on branch `prose/chNN`, and touches only `book/`.

## 5. Severity

- **Critical:** wrong behaviour that breaks later tasks or the build, data loss, a rule broken (a push, a foreign tag, `~/ki`).
- **Important:** the task cannot be trusted until fixed: incorrect or fragile behaviour, a missed requirement, a value that differs from the brief, a test that cannot fail, a surviving mutant that matters, a card that claims more than its test, a duplicated block of logic, a swallowed error, a comment that promises more than the code does, an unverified or wrong technical claim in the prose, a nested container.
- **Minor:** polish, naming taste, coverage that could be broader without a concrete mutant.
- If the brief or the plan mandates something this rubric calls a defect, it is still a finding: report it as Important and label it **plan-mandated**. The controller rules on it; do not pre-judge it away.
- Acknowledge what is done well before the issues, and cite `file:line` for every finding and for every check you would otherwise answer with a bare "yes".

## 6. Verdict format

Write the **full review** to the file the dispatch names (by default `.superpowers/sdd/<plan-name>/task-N-review.md`, and `task-N-rereview-R.md` for round R). Begin directly with the first verdict: every line is a verdict, a finding with `file:line`, or a check you ran. No preamble, no narration.

For a task or prose review:

```text
### Spec compliance
- ✅ Spec compliant | ❌ Issues found: <missing / extra / misunderstood, each with file:line>
- ⚠️ Cannot verify from diff: <requirement, and what the controller should check>   (or "none")

### Strengths
- <specific, with file:line>

### Issues
#### Critical
#### Important
#### Minor
- <file:line>: <what is wrong>; <why it matters>; <how to fix, if not obvious>   (or "none" under each)

### Checks run
- <named risk or doubt>: <what you checked or ran, and the result>   (or "none")

### Assessment
Task quality: Approved | Needs fixes
Reasoning: <1–2 sentences>
```

For a scoped re-review, your scope is the findings list and the fix diff, nothing else:

```text
### Finding verdicts
- <finding one-liner>: ADDRESSED | NOT ADDRESSED, with file:line evidence ("attempted" is not addressed)

### New breakage in the fix diff
- <severity> <file:line>: <what>   (or "none")

### Out-of-scope observations
- <file:line>: <what>   (or "none"; these never block the round)

### Verdict
Fix round: All findings addressed, no new Critical/Important breakage | Findings remain open: <list>
```

## 7. Rules

- **Read-only on the checkout:** never modify the working tree, the index, `HEAD`, branches or tags; no commits, no `git stash`, no `git checkout` or `git switch`. Scratch work, if a doubt needs it, goes in `build/review-<N>/` and is deleted before you answer.
- **Never push,** never run `gh repo create`, and never create, move or delete any tag (reviewers have no session of their own).
- **Never read or modify `~/ki`,** and never modify `~/kilang`.
- **Leave nothing outside `build/`,** and leave `build/` as you found it.
- **Use `command ls`, never plain `ls`:** `ls` is aliased to `eza`, which hangs in background shells.
- **Cap the memory of every mutant, fuzz or stress run** with `systemd-run --user --scope -q -p MemoryMax=4G -p MemorySwapMax=0 go test …`, one `go test` at a time: an unbounded-allocation mutant otherwise fills the 30 GB WSL VM in seconds and kills the machine (it happened three times on 2026-10-27). An exit 137 with "oom-kill" counts as the mutant killed.
- **Do not dispatch subagents,** and never ask another reviewer for a second opinion. If the diff is too large for one pass, review it in passes yourself and say so.

## 8. Final answer

After writing the review file, reply with **only** this (under 12 lines):

```text
Spec: ✅ | ❌ <n> issue(s); ⚠️ <n>
Issues: Critical <n>, Important <n>, Minor <n>
- <Critical|Important> <file:line>: <one line>   (one line per Critical and Important finding)
Task quality: Approved | Needs fixes
Review: <path of the review file>
```

For a re-review:

```text
Findings: <a> addressed, <b> not addressed
- NOT ADDRESSED: <one line each>
New breakage: none | <severity> <file:line>: <one line>
Fix round: all addressed | findings remain open
Review: <path of the review file>
```

## Cost discipline
- One focused check per finding: reproduce the claim with the smallest program or command that proves it. Do not run exhaustive sweeps (every code point, millions of fuzz inputs, all 2^32 values) unless a specific finding depends on one, and say which.
- Do not re-run what the implementer's report already proves with output; read the evidence instead.
