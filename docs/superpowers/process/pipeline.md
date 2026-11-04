# The chapter pipeline

`workflows/chapter-v4.js` takes one chapter from its first session to ready to publish. `workflows/part-gate.js` closes a Part after its chapters are published, and replaces the triple check. They live here so that no session owns them; the reasons are in `.superpowers/speed/report.md`.

```
sessions + reviews → chapter batch → CODE GATE (code ∥ whys and comments ∥ hunt if diagnostics) → one batch → scoped re-review → FREEZE
→ draft (2 500 words) → lint, doctest → BLIND PASS (facts ∥ whys ∥ apprentice ∥ follower, + control in ch11-13) → one fix → check of the diff (≤2 turns) → integrate → verify
```

A second gate iteration runs only after a failure someone would see (`message`, `fixit`, `output`, `limit`, `memory-death`), or always with `fixedSecondIteration`. After the freeze, a visible failure, or a Minor in code the reader sees, unfreezes before publishing; a test gap narrows the sentence; an Important still open after 2 turns has its sentence deleted, or stops the chapter if it is a visible failure. A Minor nobody sees is parked in `parked-part-<part>.md`, and only the gate burns that file.

## Run

The controller calls the Workflow tool. Only one script at a time owns the main checkout: never a chapter and the gate together.

```js
Workflow({ scriptPath: "docs/superpowers/process/workflows/chapter-v4.js", args: {
  n: 11, part: "II", plan: "docs/superpowers/plans/<plan>.md", proseTask: "60",
  sessions: ["50", "51", "52"],     // session tasks only, one tag chNN-sK each, in order
  diagnostics: true,                // does the chapter add diagnostics? it decides the hunt
  tagFallback: "ch11: <topic>",     // chapter tag message if the plan gives none
  newKind: false, next: null,       // next: { n, sessions, diagnostics }, its code runs after this freeze; ch11 runs alone
}})
Workflow({ scriptPath: "docs/superpowers/process/workflows/part-gate.js", args: {
  part: "II", baseTag: "m1", plan: "…", chapters: [{ n: 11, file: "book/ch11-x.md", prev: "ch10" }, …],
  milestone: { tag: "m2", chapter: "ch20", message: "…" },   // optional
  practice: [11, 20],               // optional; default the first and last chapter; [] when a tool replaces it
}})
```

Optional for a chapter: `planAmend`, `proseNote`, `controlLens` (default on in ch11-13), `fixedSecondIteration`, `secondPass`. With `next`, run chapter N+1 later with its own args: its finished code replays from its result files. `mode: "second-pass"` with the same args runs the blind pass again on the tag, report only.

## Resume

**Run it again with the same args**, in any session. Every step has a result file, `v4-chNN/<step>.json` or `v4-part-<part>/<step>.json` in the plan's workspace, with its input. An agent whose file exists returns it and does nothing else; one without looks at what an earlier attempt left (uncommitted work in the worktree, an open rebase, a detached HEAD) and goes on from it. `resumeFromRunId` also works, but only in the same session; it just saves the agents' first turn.

A stop for a bad answer names its result file: delete that file if it holds the answer, then run again. Do not edit a file to get past a stop.

## Read the results

The return value is the summary. `stopped` and `reason` mean **nothing after that step ran**: a dead reader, an empty result, a bad finding, a BLOCKED agent or an open failure, named. `{ stopped: "args" }` means no agent ran. There is no status with `stopped`.

- `status: "ready-to-publish"`: `counts` (`pass`, `passImportant`, `thaws`, `turns`, `deleted`, `parked`, `controlVisible`, `errata`), `code`, `next`, `ask_verdict`, `second_pass_due`, `push`.
- `status: "closed"` (gate): `counts` (`perChapter`, `blockingTurns`, `deleted`, `forward`, `parked`), `advice`, `republish`, `push`. `needs_alvaro: true` with a stop: a memory death or broken limit survived 3 turns, and the Part does not close.

Reports sit next to the result files, one per step, with findings under their ids: `gate1-code.md`, `pass-facts.md`, `fix1.md`, `check1.md`. Session reports stay in `task-N-report.md`, `task-N-review.md`. `errata-part-<part>.md` and «Erratas hacia delante» in the parked file are for the next session's `{{diff}}`. Prompts hold paths and ids, never findings.

## After `ready-to-publish`

The script does not publish or wait for anyone.

1. The controller publishes (`bookx web build/web`; `Artifact read`, then publish).
2. If `ask_verdict`, ask Álvaro for one line on the chapter. Nothing waits for it.
3. Álvaro runs `push`: main only up to `book: verify chNN`, and this chapter's tags, so an unfinished chapter on top of it stays unpushed. No force is needed unless a commit already pushed was rewritten.
4. If `second_pass_due`, run `mode: "second-pass"`. Its Importants go to the parked file for the gate.
5. `workflows/weekly-probe.sh ch11-end` before and after each run logs the real weekly percentage in `weekly-usage.log`, the plan workspace's.

A fix from a tag goes forward, as an erratum and a `{{diff}}` in the next session. The chapter tag on a side commit is **not** in these scripts: `workflows/side-commit-tag.md` holds the proof and what it would change.

## Test

```sh
node --test docs/superpowers/process/workflows/dry-run.test.mjs
```

No tokens: `args.dryRun` replaces each prompt with a stub that holds its canned answer, and the test plays the agents. It kills each lens mid-round, gives bad answers, crashes at every step and runs again, and checks that the main checkout stays serialized. To try the runtime itself, call Workflow with `dryRun: {}` (Haiku answers the stubs, a few tokens each) and stop a lens by hand: the run must return `stopped` and name it. `dryRun.answers` scripts findings by label. Any edit to a script reruns this.
