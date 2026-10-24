# Prose conventions

Standing instructions for every subagent that writes a chapter of the book in `~/kilang-ref`. Read this file first, then your brief. The brief holds your chapter's task (file, title, hours, milestone, chapter tag title, theory topics, exercises, «Go en el trabajo» topic), the plan's Global Constraints and Conventions, and, verbatim, the **«Plantilla de las tareas de prosa»**. That Spanish template, at the end of `docs/superpowers/plans/2026-10-24-ki-plan-01-part1.md`, is normative; section 4 below restates it in English, and if the two ever differ the Spanish one wins. `book/README.md` documents the book's Markdown dialect.

## 1. The project and the reader

- **Ki** is a pure functional language with algebraic effects whose compiler is written in Go with the standard library only. The book is *Ki: de cero a experto. Construye tu propio lenguaje funcional en Go*, in Spanish, published incrementally as a web artifact (`bookx web`; the controller publishes it).
- **The reader** is one person, Álvaro: he learns Go from zero, is new to compilers, and already programs in Rust, TypeScript and Python. Each chapter teaches the Go it needs, with comparisons to those three languages, and builds one stage of the definitive compiler.
- **Two repositories, one module path** (`github.com/aldezex/kilang`):
  - `~/kilang-ref` → `aldezex/kilang-ref`: the reference, where you write. Its tags are where every listing comes from: `chNN-sK` closes session K of chapter NN, `chNN` goes on the chapter's prose commit (yours), `m1` on the same commit as `ch10`.
  - `~/kilang` → `aldezex/kilang`: the reader's own repository, which he creates in chapter 1 and writes from the book. The book addresses that work: «tu repo», «tus tests». Never touch it.
- **Off limits:** `~/ki` (the August Ki) and the deleted C++ work. Never read, reuse or modify anything there.
- **Authority:** the spec (`docs/superpowers/specs/2026-10-24-ki-design-go.md`, chapter template in §12.2, index and milestones in §12.4–12.5) is binding; your brief is the plan's text for your chapter.

## 2. Who does what (the worktree flow)

The controller does steps 0, a and d–j of the template; you do b and c.

- **b. Worktree.** From the main checkout, `~/kilang-ref`: `git worktree add .claude/worktrees/prose-chNN -b prose/chNN main`. Work only there, and touch nothing outside `book/` (the chapter file and, if needed, the wording of its cards; see section 6).
- **c. Write and verify.** Inside the worktree:
  1. write `book/chNN-<slug>.md` (the file name the brief gives);
  2. `go build -o build/bin/bookx ./cmd/bookx && build/bin/bookx verify` must print `bookx verify: OK`. Your chapter counts as *pending*: rules 1–5 run on the working tree, rule 6 on the tags the prose cites;
  3. `build/bin/bookx web --all build/web` must render the chapter (`build/web/book/chNN.md` exists);
  4. `wc -w book/chNN-*.md` is between 4 000 and 8 000 (section 5);
  5. `git add book && git commit -m "book: chapter NN prose"`, with `NN` in two digits (`book: chapter 01 prose`): `ci/retag-all.sh` finds the chapter's commit by that exact subject;
  6. `rm -rf build` inside the worktree (your `bookx` binary, the rendered web and your scratch modules), so the controller can remove the worktree cleanly.
- **d. Review.** A reviewer reads your chapter with `docs/superpowers/process/reviewer.md`. Corrections **amend the prose commit**: fix, `git add book && git commit --amend --no-edit`, re-run step c.2–c.4 and c.6, and append a fix report (old and new SHA of the prose commit, what changed, the `bookx verify` output) to your report file.
- Steps e–j (merge with `--ff-only`, the `chNN` tag, `ci/check-tags.sh chNN`, Álvaro's push, the GitHub CI, publication, the ledger) are the controller's. Never merge, tag or remove the worktree yourself.

## 3. TDD, strong tests and the shape of the book's tests

The chapter teaches these as a method, so the prose must model them:

- **Tests first.** The reader writes each test from its card (`case`, `why`, `expect`) before implementing, watches it fail, implements, and only then unfolds the reference test with `{{reftest TNN.MM}}`. Explain red-green the first time it appears and keep the order in every session: cards, then a failing test, then code.
- **Strong tests.** When the prose discusses a test or shows a solution with tests, show why it is strong: exact values, boundaries (the limit, one past it), the mutant it kills ("si escribieras `<` en lugar de `<=`, este caso fallaría"). Never show a test that would pass with any implementation.
- **Shape of the book's tests** (plan, «Forma de los tests del libro»): a book test is a `TestXxx`, a subtest `t.Run("snake_case_name", …)` with a literal name, a `FuzzXxx`, an `ExampleXxx` or a `BenchmarkXxx`. Table tests live inside one subtest and their rows do not use `t.Run`; they are reported with `t.Errorf("%s: …", row.name, …)`, so `go test -json` lists the subtest and not its rows. Each `TestXxx` lives in one file with all its subtests. Tests that use `internal/testsupport` are in the external test package; tests of unexported identifiers are in `*_internal_test.go` with another function name. When the chapter introduces any of this, explain the reason, not only the rule. Exercise solutions with tests follow the same shape.

## 4. The template

**Front matter:** `chapter`, `title` (in Spanish, exactly as the brief gives it), `part: I`, `hours` and `milestone` (the literal milestone of spec 12.5, as the brief quotes it).

**Fixed sections, in this order** (the headings of `book/README.md`):

1. `## Objetivo`: the command the reader can run at the end, and which milestone gets closer.
2. `## Teoría`: every topic the brief lists, with at least one `:::from Rust`, `:::from TS` or `:::from Python` box for each new Go concept. The Rust box weighs the most: ownership versus GC, traits versus interfaces, `Result` versus `error`, enums versus sealed interfaces. Every technical claim is verified on the machine first (section 5).
3. `## Tests primero`: `{{cards}}` and a sentence on how to use the cards (write the test, watch it fail, implement, and only then unfold the reference with `{{reftest}}`).
4. `## Implementación`: one `### Sesión K: <título> (≈ H h)` per session, with the hours of its task in the plan.
   - Show code with `{{file <tag> <path>}}`, `{{diff <tagA>..<tagB> <path>}}` or `{{listing <tag> <region>}}`, **citing only session tags** (`chNN-sK`, never `chNN`: the chapter tag does not exist while you write). `bookx verify` rejects a chapter tag.
   - Explain the code you show: the book never dumps code without explaining it.
   - The first time a concept appears, its code goes complete. A hint with a folded solution (a `hint` region, or a `:::solution` after the exercise) is only for patterns the book already taught.
   - **Interfaces are always complete** (exported types, IR structures and APIs), even when the implementation goes as a hint (spec 12.2), so the reader's code fits later listings.
   - Every session says «si te atascas o tu versión se desvía demasiado, copia el fichero de `ref/chNN-sK` y sigue», and ends with «Al terminar: tus tests en verde, commit y etiqueta `chNN-sK`; compárate con `ref/chNN-sK`».
5. `## Ejecútalo`: the exact output the reader must see, copied from a real run (section 5).
6. `## Ejercicios`: the brief's exercises, each in `:::exercise` followed by its `:::solution`.
7. `## Preguntas de repaso`: between 6 and 8, each in `:::review` followed by its `:::answer`. They ask why, not for definitions.
8. `## Resumen`: between 8 and 12 bullets.
9. `## Go en el trabajo`: the brief's topic. Claims about specific projects are verified before you write them (section 5).

**Containers.** `:::exercise`, `:::solution`, `:::review`, `:::answer`, `:::from <Rust|TS|Python>`, `:::note` and `:::bug` are closed with `:::`, always in sequence and **never nested**: a `:::solution` starts after the `:::` that closes its `:::exercise`. The web page does not nest containers, and rule 7 of `bookx verify` rejects nesting and unclosed containers. A `:::bug` box («Bug encontrado») is for a didactic bug a later chapter uncovered, when the controller asks for one.

**Directives** (`{{file}}`, `{{diff}}`, `{{listing}}`, `{{reftest}}`, `{{cards}}`) are resolved by `bookx`; never paste a listing by hand. A `{{listing}}` names a region marked in the code (`// @book <region> full|hint "texto"|solution|test` … `// @end`) that exists at the cited tag. If the chapter needs a region that does not exist, report it: adding markers changes code, which is outside `book/`.

## 5. Verification, length and language

- **Every technical claim about Go or the machine is checked by running it before you write it:** compiler and `go vet` messages, `go test` output, exit codes, what `-race` or `-gcflags=-m` prints, the behaviour of `append`, maps, `defer`, `os.Exit`, `bufio`… Run each check in a scratch module under `build/prose-chNN/` inside your worktree (with `GOTOOLCHAIN=local`), and record the claim, the command and its output in your report. Version facts say «en Go 1.27». The toolchain facts already verified are in `docs/toolchain.md`: cite them, and re-run any you state differently.
- **Outputs of `ki`** (`## Ejecútalo`, examples in the text) come from the binary built at the worktree's `HEAD`, which is the chapter's last session: `go build -o build/bin/ki ./cmd/ki`, run it, and copy the output exactly.
- **Exercise solutions** that contain code are verified too: export the tree (`mkdir -p build/prose-chNN/ex && git archive HEAD | tar -x -C build/prose-chNN/ex`), apply the solution there, run its test, and delete the export.
- **Claims about real projects and practice** («Go en el trabajo»: Kubernetes, CPython, Lua, a CVE…) need a primary source (the project's repository or documentation, the CVE record). Put the source in your report. If you cannot verify a claim, leave it out; never write an anecdote, number or quote from memory.
- **Length:** between 4 000 and 8 000 words (`wc -w book/chNN-*.md`, directives and code fences included), in proportion to the chapter's hours. As a guide, about 600–750 words per hour of the chapter, kept within those bounds (a 6 h chapter near 4 000–4 500, an 11 h chapter near 7 000–8 000).
- **Language:** prose in Spanish (Spain: «fichero», «ordenador»), as in the spec and the plan. Code, identifiers, comments, repository paths and compiler messages stay in English, in backticks.
- **«Tú», with no gendered words referring to the reader.** Address the reader as «tú» throughout. Never use an adjective or participle with gender that refers to the reader: not «listo», «seguro», «preparado», «cansado», «atascado», «perdido», «nuevo en Go», «bienvenido», nor «el lector». Rephrase with verbs, nouns or impersonal forms: «si te atascas», «cuando lo tengas», «tienes la certeza», «si empiezas en Go», «quien lee». Prefer neutral forms for people in general too («quien programa en Rust», «si vienes de Python») over the generic masculine.

## 6. Cards

The sessions already wrote `book/tests/chNN.tests`, and `{{cards}}` renders it as the table «Id | Caso | Por qué | Esperado | Nivel». Do not change ids, `kind`, `file` or `ref`, and do not add or remove cards: they are tied to the tests in the chapter's tag. You may improve the Spanish of `case`, `why` or `expect` only if the text stays exact to what the test checks (a card never claims more than its test), keeps «tú» and no gendered words, and you list every change in your report.

## 7. Rules

- **Never push** and never run `gh repo create`; Álvaro pushes.
- **Never create, move or delete tags.** The prose task has no session tag of its own: `chNN` is the controller's (step e).
- **Never use `git stash`,** never `git clean -x`, and never rewrite commits other than your own prose commit (which you amend in step d).
- **Never read or modify `~/ki`,** and never modify `~/kilang`.
- **Leave nothing outside `build/`:** scratch modules and outputs go under `build/` in your worktree, never in `/tmp`, `$TMPDIR` or your home directory, and `build/` is removed before you report (step c.6). Create no other worktree and no clone.
- **Use `command ls`, never plain `ls`:** `ls` is aliased to `eza`, which hangs in background shells.
- **Do not dispatch subagents,** neither writers nor reviewers.
- If the code at the last session tag is wrong, or the chapter needs something outside `book/` (a region marker, a test, a fix), do not change it: report it with status DONE_WITH_CONCERNS or BLOCKED. Code fixes go through the controller's batch of corrections (template step 0).

## 8. Report contract

Write the **full report** to the file the dispatch names (by default `.superpowers/sdd/<plan-name>/task-N-report.md`):

- the chapter file, its word count, its hours, and the number of exercises, review questions and summary bullets;
- the output of `bookx verify` and confirmation that `bookx web --all` rendered `build/web/book/chNN.md`;
- the tags the chapter cites;
- the verification log: one line per technical claim, with the command and the output that confirmed it; the sources for claims about real projects; how each exercise solution with code was checked;
- any change to the cards, word by word;
- the commit (short SHA and subject) on branch `prose/chNN`;
- concerns: anything the chapter needed and could not have, and anything in the code that looked wrong.

Then **reply with only this** (under 15 lines):

```text
Status: DONE | DONE_WITH_CONCERNS | BLOCKED | NEEDS_CONTEXT
Commit: <sha7> book: chapter NN prose (branch prose/chNN)
Words: <n>   bookx verify: OK | <first problem>
Concerns: <one line each, or "none">
Report: <path of the report file>
```
