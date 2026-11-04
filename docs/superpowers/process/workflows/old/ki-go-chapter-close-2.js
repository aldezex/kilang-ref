export const meta = {
  name: 'ki-go-chapter-close-2',
  description: 'Ki (Go) chapter close-out after the panel cap: controller rulings batch, its re-review, plan text, prose re-sync, scoped confirmation (≤3 loops), integrate, verify the rendered chapter at its tag',
  phases: [{ title: 'Rulings' }, { title: 'Confirm' }, { title: 'Integrate' }],
}
// args: { n, proseTask, lastRound, ruling, planText, tagFallback }
const A = args
const NN = String(A.n).padStart(2, '0'), PP = String(A.n - 1).padStart(2, '0')
const REPO = "/home/scami/kilang-ref"
const WT = REPO + "/.claude/worktrees/prose-ch" + NN
const W = REPO + "/.superpowers/sdd/2026-10-24-ki-plan-01-part1"
const SCRATCH = "/home/scami/kilang-ref/build/review"
const L = (...l) => l.join("\n")
const FIND = { type: 'object', properties: { findings: { type: 'array', items: { type: 'object', properties: { severity: { type: 'string', enum: ['Critical', 'Important', 'Minor', 'Nit'] }, where: { type: 'string' }, claim: { type: 'string' }, fix: { type: 'string' } }, required: ['severity', 'where', 'claim', 'fix'] } }, report_path: { type: 'string' } }, required: ['findings', 'report_path'] }
const DONE = { type: 'object', properties: { status: { type: 'string' }, head: { type: 'string' }, report_path: { type: 'string' }, concerns: { type: 'array', items: { type: 'string' } } }, required: ['status', 'head', 'report_path', 'concerns'] }
const RULES = "Do not modify anything. Never run anything in the main checkout; use git-archive copies in a NEW dir under " + SCRATCH + " (delete it afterwards). Memory-cap every mutant run: systemd-run --user --scope -q -p MemoryMax=4G -p MemorySwapMax=0 go test …, one at a time. Never push or stash."
const serious = (f) => f.severity === 'Critical' || f.severity === 'Important'
const isProse = (f) => new RegExp('ch' + NN + '-[a-z0-9-]+\\.md').test(f.where)
const batch = (label, what, report, ph) => agent(L(
  "You own the main checkout of " + REPO + " (Go edition, Plan 01) for this job: fold fixes IN PLACE into the tagged ch" + NN + "-sK session commits (fixup! <exact subject>, 'git -c sequence.editor=: rebase -i --autosquash --update-refs <parent of ch" + NN + "-s1>', ci/retag-all.sh; later commits get re-parented), as in " + W + "/fix-batch-ch" + NN + "-panel" + (A.lastBatchRound || A.lastRound) + "-report.md.",
  "Read " + REPO + "/docs/superpowers/process/implementer.md. Never leave anything open as a 'controller's call'. Every new check kills its mutant (memory-capped); cards exact, ids stable. ci/ci-local.sh at every moved tag (git-archive) and HEAD; bookx verify. Never touch " + WT + ". Never push or stash.",
  what,
  "Report " + report + ": old->new for every tag, cards changed, messages changed. Structured output: status, head, report_path, concerns."), { label, phase: ph, schema: DONE, model: 'sonnet' })
const prose = (label, task, ph) => agent(L(
  "You maintain the Spanish prose of chapter " + A.n + " of the Ki book: book/ch" + NN + "-*.md on branch prose/ch" + NN + " in " + WT + " (you own only that worktree). Read " + REPO + "/docs/superpowers/process/prose.md and the end of " + W + "/task-" + A.proseTask + "-report.md. The budget is 3 000 flow words (bookx words <file>), see the concision rules in prose.md; fixes must not grow the running text.",
  task,
  "Re-verify what you touch (git-archive copies, memory-capped mutants; never run anything in the main checkout). Commit only the chapter file as 'fixup! book: chapter " + NN + " prose'; regenerate (do not commit) book/tests/ch" + NN + ".list and book/verified-tags.txt with ci/check-tags.sh so bookx verify passes; remove build/. Append to " + W + "/task-" + A.proseTask + "-report.md. Structured output: status, head, report_path, concerns (word count)."), { label, phase: ph, schema: DONE, model: 'opus' })

phase('Rulings')
if (A.preLens) await agent(L("LENS 1 — Go technical accuracy, chapter " + A.n + " of the Ki book, round " + A.lastRound + ". Check EVERY technical claim by running it on Go 1.27.3 here or against a primary source. A wrong or overstated claim, or a claim about what a test catches broader than the test, is Important.", "Prose book/ch" + NN + "-*.md on branch prose/ch" + NN + " in " + WT + "; code at tags ch" + NN + "-s*; earlier rounds " + W + "/panel-ch" + NN + "-accuracy-{1.." + (A.lastRound - 1) + "}.md; conventions " + REPO + "/docs/superpowers/process/{prose.md,reviewer.md}. Confirm each previous-round finding is fixed, then do a full fresh pass. " + RULES, "Write " + W + "/panel-ch" + NN + "-accuracy-" + A.lastRound + ".md. Structured output: findings (empty if nothing) and report_path."), { label: 'panel:accuracy#' + A.lastRound, phase: 'Rulings', schema: FIND, model: 'opus' })
const R = W + '/fix-batch-ch' + NN + '-ruling-report.md'
await batch('fix:ruling', "CONTROLLER RULINGS to apply: " + A.ruling + (A.planText ? "\nALSO, you may edit docs/superpowers/plans/2026-10-24-ki-plan-01-part1.md for exactly this, in a separate 'docs: …' commit at HEAD: " + A.planText : ""), R, 'Rulings')
const rr = await agent(L("Scoped re-review of a chapter-" + A.n + " fix batch in " + REPO + ". Read " + REPO + "/docs/superpowers/process/reviewer.md. Batch report: " + R + ". Rulings it applies: " + A.ruling, "Each ruling applied; each named mutant dies (run once); cards exact; nothing regressed. Rate Important only what would mislead a reader or let a plausible real bug through. " + RULES, "Write " + W + "/fix-batch-ch" + NN + "-ruling-rereview-1.md. Structured output: findings (open or new only) and report_path."), { label: 'rereview:ruling', phase: 'Rulings', schema: FIND, model: 'sonnet' })
if (rr && rr.findings.some(serious)) await batch('fix:ruling-rr', "Fix these findings of the re-review: " + JSON.stringify(rr.findings.filter(serious)), W + '/fix-batch-ch' + NN + '-ruling2-report.md', 'Rulings')
await prose('prose:resync', "Code just changed: read " + R + " (and fix-batch-ch" + NN + "-ruling2-report.md if it exists). Rebase prose/ch" + NN + " onto main's current 'book: verify ch" + PP + "' commit if its base moved (git rebase --onto <new> <old merge-base> prose/ch" + NN + "). Make the prose exact again for whatever changed, and apply these controller notes: " + (A.proseNote || 'none') + ". Then fix any round-" + A.lastRound + " prose Minor/Nit in " + W + "/panel-ch" + NN + "-*-" + A.lastRound + ".md still open, if words allow.", 'Rulings')

phase('Confirm')
const SCOPE = "SCOPED CONFIRMATION, not a fresh full pass: the chapter-" + A.n + " panel already ran " + A.lastRound + " full rounds. Check ONLY (1) that every round-" + A.lastRound + " finding in " + W + "/panel-ch" + NN + "-*-" + A.lastRound + ".md is fixed or soundly declined, and (2) that the fixes since then (" + W + "/fix-batch-ch" + NN + "-panel" + (A.lastBatchRound || A.lastRound) + "-report.md, " + R + ", the end of " + W + "/task-" + A.proseTask + "-report.md) introduced nothing wrong. Prose on prose/ch" + NN + " in " + WT + "; code at tags ch" + NN + "-s*. Report Important only for something wrong in that scope that would mislead a reader or let a plausible real bug through."
const lenses = [
  { key: 'accuracy', model: 'opus', p: "LENS accuracy: every technical claim those fixes touched holds on Go 1.27.3 here." },
  { key: 'fidelity', model: 'sonnet', p: "LENS fidelity: every quote, count, command and card those fixes touched reproduces in git-archive copies of the tags." },
  { key: 'code', model: 'sonnet', p: "LENS code: the code changes of those fixes are correct, each new check kills its mutant, comments promise only what tests check." },
]
let loop = 0, open = []
while (loop < 3) {
  const res = await parallel(lenses.map((l) => () => agent(L(l.p, SCOPE, RULES, "Write " + W + "/confirm-ch" + NN + "-" + l.key + "-" + (loop + 1) + ".md. Structured output: findings (empty if all confirmed) and report_path."), { label: 'confirm:' + l.key + '#' + (loop + 1), phase: 'Confirm', schema: FIND, model: l.model })))
  const all = res.filter(Boolean).flatMap((x) => x.findings)
  open = all.filter(serious)
  log('ch' + NN + ' confirmation ' + (loop + 1) + ': ' + open.length + ' Critical/Important of ' + all.length)
  if (!all.length) break
  const codeF = all.filter((f) => !isProse(f)), proseF = all.filter(isProse)
  if (codeF.length) await batch('fix:confirm#' + (loop + 1), "Fix these findings: " + JSON.stringify(codeF), W + '/fix-batch-ch' + NN + '-confirm' + (loop + 1) + '-report.md', 'Confirm')
  await prose('prose:confirm#' + (loop + 1), L(codeF.length ? "Code changed: read " + W + "/fix-batch-ch" + NN + "-confirm" + (loop + 1) + "-report.md; rebase if the base moved and make the prose exact again." : "No code changed.", proseF.length ? "Fix these prose findings: " + JSON.stringify(proseF) : ""), 'Confirm')
  loop++
  if (!open.length) break
}
if (open.length) return { stopped: 'confirm-not-clean', open }

phase('Integrate')
const integ = await agent(L(
  "You own the main checkout of " + REPO + " (Go edition, Plan 01) for this job: integrate chapter " + A.n + " into main exactly as chapter 7 was integrated (" + W + "/integrate-ch07-report.md).",
  "Read " + REPO + "/docs/superpowers/process/implementer.md first. Steps: (1) in " + WT + ", discard the uncommitted book/verified-tags.txt and book/tests/ch" + NN + ".list, then squash prose/ch" + NN + " into ONE commit 'book: chapter " + NN + " prose' (git -c sequence.editor=: rebase -i --autosquash <its base>); it must contain ONLY book/ch" + NN + "-*.md. (2) On main: the insertion point is the first non-session commit after the last ch" + NN + "-s* session commit (for example 'book: verify ch" + PP + "'); if the last session commit is itself followed by nothing relevant, insert right after it. Detach there; run ci/check-tags.sh --list-as ch" + NN + " <last session tag> and ci/check-tags.sh 'ch" + NN + "-s*'; commit book/tests/ch" + NN + ".list + book/verified-tags.txt as 'book: verify ch" + NN + " sessions'; cherry-pick the squashed prose commit; tag it annotated 'ch" + NN + "' with the message the plan gives in Tarea " + A.proseTask + " (or '" + A.tagFallback + "' if none); rebase the rest of main on top with git rebase --onto; ci/retag-all.sh (ch" + NN + "-s* must not move). (3) ci/check-tags.sh ch" + NN + " and commit the verified-tags update at HEAD as 'book: verify ch" + NN + "'. (4) ci/ci-local.sh at HEAD 'verify: OK'; go build -o build/bin/bookx ./cmd/bookx && build/bin/bookx verify. (5) Check the worktree's prose commit equals the cherry-picked chapter file, then git worktree remove " + WT + " and git branch -D prose/ch" + NN + ".",
  "Never push or stash. Report " + W + "/integrate-ch" + NN + "-report.md. Structured output: status, head, report_path, concerns (final SHAs)."), { label: 'integrate', phase: 'Integrate', schema: DONE, model: 'sonnet' })
const v = await agent(L("Verify the integrated chapter " + A.n + " of the Ki book in " + REPO + ": in a git-archive copy of the tag ch" + NN + " (NEW dir under " + SCRATCH + ", deleted afterwards) go build -o bookx ./cmd/bookx, bookx verify, bookx web --all <dir>; every {{…}} directive resolved and the rendered cards match book/tests/ch" + NN + ".tests at the last session tag. Also bookx verify at main HEAD (read-only). Do not modify anything; never push.", "Write " + W + "/verify-ch" + NN + ".md. Structured output: findings (empty if all good) and report_path."), { label: 'verify', phase: 'Integrate', schema: FIND, model: 'sonnet' })
return { loops: loop, integ, verify: v }