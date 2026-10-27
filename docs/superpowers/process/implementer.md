# Implementer conventions

Standing instructions for every implementer subagent in `~/kilang-ref`. Read this file first, then your task brief. The controller's dispatch adds only what the brief cannot know (interfaces from earlier tasks, rulings, the path of your report file). These conventions replace the boilerplate a dispatch would otherwise repeat: they bind you even when the dispatch does not mention them.

## 1. The project

- **Ki** is a pure functional language with algebraic effects, Hindley–Milner type inference with typeclasses and higher-kinded types, and its own concurrent runtime. Its compiler is written in Go with the standard library only. It is built chapter by chapter alongside a Spanish book, *Ki: de cero a experto. Construye tu propio lenguaje funcional en Go*, written for one reader, Álvaro, who learns Go from zero and is new to compilers (he knows Rust, TypeScript and Python).
- **Two repositories, one module path.** Both declare `module github.com/aldezex/kilang`, so any file of the reference fits the reader's tree without changing an import.
  - `~/kilang-ref` → `aldezex/kilang-ref` (public, MIT): **this repository, the reference.** Agents build it: code, tests, test cards, prose, tags, and `bookx` (`cmd/bookx`, the listing extractor and verifier, which exists only here). Every listing in the book is read from one of its tags.
  - `~/kilang` → `aldezex/kilang`: Álvaro's own repository, which he creates in chapter 1 and writes himself from the book. Never modify it and never use it as a source.
- **Off limits:** `~/ki` (the August Ki) and the C++ work deleted on 2026-10-24. Never read, reuse or modify anything there: basing anything on `~/ki`, or touching it, is an anti-goal of the spec (§2).
- **Authority.** The spec, `docs/superpowers/specs/2026-10-24-ki-design-go.md`, is binding. The plan argues from it, and your brief is the plan's text for your task. If the brief contradicts the spec or itself, do not choose silently: stop with NEEDS_CONTEXT, or finish with DONE_WITH_CONCERNS and say what you chose and why.
- **History** is linear on `main`. A code session ends in one commit `chNN-sK: <summary>` carrying the annotated tag `chNN-sK`. The chapter tag `chNN` goes on the chapter's prose commit and `m1` on the same commit as `ch10`; neither is yours. A session tag is valid when `ci/ci-local.sh` is green on it on this machine.
- **Workspace.** The controller's plan workspace is `.superpowers/sdd/<plan-name>/` (git-ignored): the ledger, the briefs, the reports and the review packages. Write only the report file the dispatch names there.

## 2. Your brief

- `docs/superpowers/process/task-brief.sh` writes it: the full text of your task, then the plan's `## Global Constraints` (which runs on through `## Review Focus`) and `## Convenciones del plan`, and, for prose tasks, `## Plantilla de las tareas de prosa`. Everything in it binds your task, even the parts your task text does not repeat.
- **Exact values live only in the brief:** paths, package and function names, signatures, test and subtest names, messages, diagnostic codes, exit codes, `usage:` and `commands:` lines, card ids, commit summaries and tags. Copy them verbatim. Never paraphrase a message, rename a test or "improve" a format.
- Never read the whole plan. When the brief cites a spec section ("spec 7.2", "11.3"), read that section of the spec.
- **Files.** Create and modify the files the brief lists, plus your cards in `book/tests/chNN.tests` and any `testdata/` the brief names. Touching another file needs a reason in your report. Never edit `docs/superpowers/` (spec, plans, process files): if one of them is wrong, report it.
- The brief's Spanish words are the plan's language; your code, identifiers, comments, commit messages and compiler messages are in English. Card text is in Spanish (section 7).

## 3. One session, one task

- A **code task** is one session of the book, `chNN-sK`, of 2–3 hours of the reader's time (its title says how many). It ends green, with its tests and their cards, in one commit made by `ci/session.sh` (section 9). Make no other commits in a code task.
- A **tool task** (`bookx`, the toolchain re-verification) is not a session: no tag and no cards. It ends with the single commit whose exact message the brief gives. Every `cmd/bookx/*_test.go` starts with `// bookx: not-in-book`.
- A **prose task** follows `docs/superpowers/process/prose.md` instead of sections 3–10 here; sections 11–13 still apply.
- Implement exactly what the task specifies: nothing missing, nothing extra (YAGNI). The architecture is final from chapter 1, so build each stage the way the brief defines it, never as a placeholder to throw away later.

## 4. TDD, red then green

1. Write the tests the brief lists, with its exact names, in the files it fixes.
2. Run them and watch them fail for the expected reason: a compile error because the package or the function does not exist yet, or `FAIL` on the behaviour. A test that passes before the implementation exists proves nothing, unless the brief says it pins an existing fact (`TestGoMod`, `TestToolchainFacts`); say so in the report.
3. Implement the least code that makes them pass and matches the brief's interfaces.
4. Run them green. While iterating, run only the focused test (`go test ./internal/syntax/ -run 'TestLexer/pipe_continues_line'`). Run the full `ci/ci-local.sh` once at the end, not after every edit.
5. Record RED (command, the relevant failing output, why that failure was expected) and GREEN (command, passing output) in your report.

## 5. Strong tests

A test is strong when a plausible wrong implementation fails it. For every test you write, name the mutants that a careless implementation would produce and make sure one of your tests kills each one:

- **Exact values.** Compare whole strings, exact exit codes, exact positions (`line:col`), exact counts ("exactly 1 diagnostic", not "at least one"). Use "contains" or "has prefix" only where the brief says so (the usage tests, for instance).
- **Boundaries.** At the limit, one past it and one before it; empty input; a single element; the first and the last element; `math.MaxInt64` and `math.MinInt64`; `MIN / -1`; a file without a final newline; the 100th and the 101st error. The brief names many of these; add none it does not ask for, but never weaken one.
- **Typical mutants:** off-by-one, `<` for `<=`, swapped operands or labels, a wrong sign, a missing sort, the last element dropped, the first match always returned, a missing flush, the wrong diagnostic code, a column counted in bytes instead of code points.
- **Never a test that passes with any implementation:** one that asserts nothing, asserts only what the test itself set up, compares a value with itself, or checks something Go already guarantees (in Go an old backing array stays alive and intact after `append` reallocates, so "the old slice is still valid" cannot fail).
- **Negative tests** assert the specific code, message and position, not just "an error happened".
- **Observable behaviour,** not internals or mocks: outputs, exit codes, diagnostics, returned values.
- **Deterministic:** no wall clock, no randomness without a fixed seed, no dependence on map order, no network. Files go in `t.TempDir()`. A test that runs `go` sets `GOTOOLCHAIN=local` in its environment.
- **Pristine output:** no stray logs or prints. `go vet` must stay clean.
- Mark helpers with `t.Helper()`; a helper that cannot do its job (read a file, build a binary) calls `t.Fatalf` with the reason.

## 6. Shape of the book's tests

`bookx` reads test names from the source and from `go test -json`, so their shape is a contract (plan, «Forma de los tests del libro»):

- A book test is one of:
  - a function `TestXxx(t *testing.T)`;
  - a subtest `t.Run("snake_case_name", func(t *testing.T) { … })` whose name is a **string literal**, never a variable or a `fmt.Sprintf`;
  - a fuzz target `FuzzXxx(f *testing.F)`;
  - an example `ExampleXxx()`;
  - a benchmark `BenchmarkXxx(b *testing.B)` (kind `bench`; `go test` does not run it without `-bench`).
- **Table tests go inside one subtest,** and the card covers that subtest and its whole table. The rows of a table **never** use `t.Run`: loop over them and report with `t.Errorf("%s: …", row.name, …)`. Then `go test -json` lists the subtest and not its rows, rule 4 of `bookx verify` can find the subtest's literal in the file, and rule 5 works on the real list of tests. A negation ("contains no `require` line") is its own `t.Run`, not a row.
- **One `TestXxx` function lives in one file,** and all of its subtests live in that file (Go rejects two declarations in one package). Each task fixes which function each `_test.go` holds (`TestLexer` in `lexer_test.go`, `TestNumbers` in `numbers_test.go`…). Never add a subtest to a function that lives in another file, and never invent a second function for a name the brief fixed.
- **External or internal.** A test that needs `internal/testsupport` lives in the external test package (`package sema_test`, `core_test`, `interp_test`). A test that touches unexported identifiers lives in a `*_internal_test.go` file of the package itself, with a different function name, and never imports `testsupport` (that would be `import cycle not allowed in test`). The tests of `internal/driver` are internal and do not use `testsupport`.
- **Lists of tests.** For each chapter tag, `ci/check-tags.sh` writes `book/tests/chNN.list` from `go test -json ./...` through `bookx testlist`. That drops the tests of `cmd/bookx`, drops a parent `T` when `T/` is a prefix of another test of the same package (`TestLexer` disappears when `TestLexer/a` exists; `TestScope` survives `TestScopes/a`), collapses `FuzzX/seed#N` and `FuzzX/<corpus>` into `FuzzX`, and keeps `ExampleX`. A parent with subtests needs no card: its subtests carry them.
- **Tests outside the book.** A test declared in a file that contains `// bookx: not-in-book` needs no card (`cmd/bookx/*_test.go`, `internal/toolchain/helper_process_test.go`). Use the marker only where the brief puts it.

## 7. Test cards

- **Every new test has a card** in `book/tests/chNN.tests`, and every card except `bench` has its test in the chapter's list. Ids are `TNN.MM`, consecutive within the chapter, without gaps, and their `NN` is the file's. The ids the plan writes literally (`T05.<n>`) are indicative: each new card takes the next free id in the file.
- **Format** (keys in English, text in Spanish):

  ```text
  [T05.07] lexer.pipe_continues_line                         level: core
  kind:    unit (go test)      file: internal/syntax/line_rule_test.go
  case:    una línea que empieza por `|>` continúa la expresión de la línea anterior
  input:   "let total = xs\n  |> sum\n"
  expect:  ningún Newline entre `xs` y `|>`; después, Newline y Eof
  why:     si el lexer insertara aquí un Newline, la sentencia terminaría en `xs`
           y el error saldría una línea más abajo, lejos de su causa
  ref:     TestLineRule/pipe_continues_line
  ```

  An indented continuation line is joined to the previous field with a space.
- **`kind`** is one of `unit` (`ref:` the full test name, `TestXxx` or `TestXxx/subtest`), `prop` (a fuzz target; `ref:` `FuzzXxx`), `example` (`ref:` `ExampleXxx`), `bench` (`ref:` `BenchmarkXxx`; exempt from rule 5 both ways), and `golden`, `diag` and `run` (`ref:` the path of the `.ki` file, whose `// test:` header line carries the card's id). An optional parenthesis may follow the kind (`unit (go test)`). `file:` is the test file, or the `.ki` file.
- **`level`** is `core` (the default: it must pass before moving on) or `stretch` (optional when the reader is short of time).
- **`case` and `expect` describe exactly what the test checks, no more and no less.** If the card says "exactly one diagnostic at 3:9", the test asserts exactly that. A card that promises more than its test (a time bound, a property the test never checks) is a defect the reviewer will flag. `input` is optional.
- **`why`** tells the reader which bug the test prevents and why it matters. It is book prose: Spanish, addressing the reader as «tú», with no gendered words referring to the reader.
- **Golden files** (`tests/golden/chNN/*.ki`, `tests/errors/chNN/*.ki`) follow the header grammar in your brief's «Convenciones del plan»; the first header line is `// test: TNN.MM`.

## 8. Code rules

These come from the spec (§7) and from the lessons of the C++ edition. The brief's Global Constraints are the authority; this list is a reminder.

- **Standard library only:** no `golang.org/x/*`, no cgo, no third-party module, no `GOEXPERIMENT`, and no `require` line in `go.mod`. Commands run with `GOTOOLCHAIN=local` (`ci/ci-local.sh` exports it).
- **Style:** `gofmt` without discussion (`gofmt -w` your files); `go vet` clean; names after the Google style guide (`syntax.Lexer`, not `syntax.SyntaxLexer`); ids are 32-bit defined types with the `ID` suffix (`ExprID`, `TypeID`, `DefID`).
- **Comments are for someone starting out,** precise and in English. Explain why, not just what. Never promise more than the code does: "never allocates", "never crashes", "innermost first" are claims, so either a test backs them or the comment says less. Every exported identifier has a doc comment that starts with its name.
- **One helper per concept.** Before writing a helper, search for an existing one (`grep -rn`). Never duplicate a block of logic. When the brief forces a duplicate (for example, `bookx` imports only the standard library), keep the copy minimal and say so in the report.
- **Bounded recursion.** Any recursion whose depth the input controls is either bounded by a limit with its diagnostic (`MaxNesting = 256`, E0204; `MaxChain = 1024`, E0205) or replaced by an explicit stack. That includes the passes over Core (elaboration, Lint, the printer, `Subexprs`): a block of 100 000 statements must not recurse in Go.
- **Input hygiene:** CRLF counts as one newline; a lone `\r` does not end a line (it is shown as `␍`); a BOM is stripped on load and preserved when a golden is rewritten; a file of 4 GiB or more is rejected before it is read; invalid UTF-8, tabs and control characters are displayed as «Posiciones» in your brief specifies.
- **Output:** the runner flushes after each `PASS`/`FAIL` line when its writer implements `Flush() error`. `os.Exit` skips `defer`, so every buffer is flushed before exiting. stdout and stderr go through `kirt/out` once it exists (chapter 10).
- **Determinism:** no output depends on map iteration order; sort by id or by position. `ki check` sorts diagnostics by position before rendering them.
- **Sealed interfaces:** every type switch over one ends in `default: panic("unreachable")`. Every "unreachable" or "cannot happen" claim (a panic text, an exit code, a fatal error) that the brief asks you to verify gets a test that verifies it.
- **Errors:** a diagnostic is a value that goes to a `DiagSink`; `error` is for I/O and tools; `panic` only for internal compiler errors, which the `recover` at each pass boundary turns into an ICE report (exit 70). A Ki panic is never a Go panic: in the oracle it is the machine state `Halt{code: 101}`.
- **Passes never mutate their input.** Goroutines only in `kirt`, in the test runner and in tests; the compiler is sequential. `unsafe` only in `kirt/heap` and its tests.
- **Exit codes of `ki`:** 0; 1 compile errors, an unreadable file or an answer ki cannot write; 2 CLI misuse; 70 ICE; 101 Ki panic; 103 interpreter stack overflow.
- **Never `go run` to test `ki`:** it masks exit codes. Tests build the binary or call `driver.Main` in-process, as the brief says.

## 9. Closing steps of a code task

Always these three, in this order, even when the brief does not repeat them:

1. **VERIFY:** `ci/ci-local.sh` ends with `verify: OK`. Paste the last lines of its output in your report.
2. **Cards:** every new test has its card in `book/tests/chNN.tests` (section 7). Check the ids by reading the whole file: consecutive, no gaps, no duplicates.
3. **Commit and tag:** `ci/session.sh chNN-sK "<summary>"`, with the tag and the summary exactly as the brief writes them. It runs `ci/ci-local.sh` again, then `git add -A`, the commit `chNN-sK: <summary>` and the annotated tag `chNN-sK` on it. Before running it, make sure `git status` shows only your task's changes, because it commits everything.

## 10. Fix rounds after review

If the controller resumes you with review findings:

- Fix them and re-run the tests that cover the amended code. Your session commit and tag already exist: never amend, rebase or reset. Commit the fix with `git add -A && git commit --fixup=<your session commit>`, run `ci/ci-local.sh` (it must end with `verify: OK`), and move your own tag onto the fix with `ci/retag.sh chNN-sK` (it keeps the tag's message). Then the tag always marks a green tree that includes the fix, and the review package can diff the round. The controller folds the fixups into the session commit later (`git -c sequence.editor=: rebase --autosquash --update-refs <base>` and `ci/retag-all.sh`, roadmap «Correcciones»).
- Do not add cards or tests beyond what the findings need, and keep the card ids consecutive if you add any.
- Append a fix report to the same report file: what you changed, the covering tests you ran, the command and its output. Reviewers do not re-run tests for you: your report is the evidence. Reply with the same short status as the first time (section 13).

## 11. Rules

- **Never push** (`git push` in any form) and never run `gh repo create`. Álvaro pushes. If your task needs a push, write the exact command in your report.
- **Never create tags other than your session's own,** and only through `ci/session.sh` (or `ci/retag.sh` for your own tag in a fix round). Never move, delete or create any other tag, and never run `ci/retag-all.sh` or `ci/check-tags.sh` unless your brief tells you to.
- **Never use `git stash`.** The checkout is shared with the controller; a stash can capture or restore work that is not yours. Never `git clean -x` either: it deletes the plan workspace in `.superpowers/` and the worktrees in `.claude/worktrees/`. Never rewrite commits that are not yours.
- **Never read or modify `~/ki`,** and never modify `~/kilang`.
- **Leave nothing outside `build/`.** Temporary files and scratch modules go under `build/` in the repository (ignored by git and by `go.mod`), never in `/tmp`, `$TMPDIR` or your home directory; `t.TempDir()` in tests is fine because `testing` removes it. Before you report, delete your scratch directories under `build/`; `build/bin/` may stay (`ci/ci-local.sh` rebuilds it), and `build/tags/` must be empty. Create no clones and no worktrees.
- **Use `command ls`, never plain `ls`:** `ls` is aliased to `eza`, which hangs in background shells.
- **Cap the memory of every mutant, fuzz or stress run.** A mutant can turn a loop into one that allocates without bound (2026-10-27: a parser-recovery mutant took 3 GB in under 3 s, and three such runs filled the 30 GB WSL VM and killed the whole machine, agents and all, long before `-timeout` fired). Run every `go test` of a mutated tree, every `-fuzz` run and every stress input inside a cgroup: `systemd-run --user --scope -q -p MemoryMax=4G -p MemorySwapMax=0 go test …` (exit 137 with "oom-kill" in `journalctl --user` counts as the mutant killed). Run mutants one at a time, never several `go test` processes in parallel, and keep a mutation harness's backups of the original files under `build/`; `-race` runs of unmutated code need no cap.
- **Resuming after a crash.** If the checkout holds uncommitted work from an interrupted attempt at *your* task, continue from it. First compare every file a mutation harness touched against the harness's backups under `build/mut*/` (or `git diff`): a crash can leave a mutant applied. Restore it before anything else.
- **Do not dispatch subagents,** neither helpers nor reviewers. Review comes from the controller after your report; a reviewer you spawn duplicates it and counts for nothing.

## 12. When to stop

Stop and report instead of guessing when the brief is ambiguous in a way two reasonable implementations would resolve differently, when it contradicts the spec, Global Constraints or an earlier task's interface, when you need code beyond your task and cannot find it, or when a file is growing well beyond the plan's intent. Use BLOCKED (you cannot finish) or NEEDS_CONTEXT (you need an answer), and put the specifics in your reply itself. Never silently produce work you are unsure about.

## 13. Report contract

Write the **full report** to the file the dispatch names (by default `.superpowers/sdd/<plan-name>/task-N-report.md`):

- status and a summary of what you implemented (or what you tried, if blocked);
- TDD evidence: RED and GREEN, each with the command and the relevant output;
- the tail of `ci/ci-local.sh` ending in `verify: OK`;
- the new tests and the cards you added (ids and refs);
- the files you created and modified, and any file outside the brief's list, with the reason;
- the commit (short SHA and subject) and the tag;
- your self-review: reread your diff (`git show --stat HEAD` and `git show HEAD`) against the brief, value by value, and against sections 5–8;
- deviations from the brief, concerns and open questions.

Then **reply with only this** (under 15 lines; the detail lives in the report file):

```text
Status: DONE | DONE_WITH_CONCERNS | BLOCKED | NEEDS_CONTEXT
Commits: <sha7> <subject>   (one line per commit)
Tag: chNN-sK -> <sha7>      (or "none" for a tool task)
Tests: <one line, e.g. "ci-local verify: OK; 3 new tests, cards T01.01–T01.03">
Concerns: <one line each, or "none">
Report: <path of the report file>
```
