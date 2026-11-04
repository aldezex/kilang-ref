export const meta = {
  name: 'ki-go-chapter-v3',
  description: 'Ki (Go): optional plan amendment; chapter N prose + 4-lens panel rounds until no Critical/Important (code fixes in place, prose fixes in its worktree) + integration, in parallel with chapter N+1 sessions, their reviews and fix batch; main checkout serialized',
  phases: [{ title: 'Plan' }, { title: 'Prose + panel' }, { title: 'Next sessions' }, { title: 'Integrate' }],
}
// args: { n, proseTask, preTasks: [extra chapter-N session tasks to implement+review before the prose], next: { n, tasks }, planAmend: "text or empty", maxRounds }
const A = args
const NN = String(A.n).padStart(2, '0'), PP = String(A.n - 1).padStart(2, '0'), MM = String(A.next.n).padStart(2, '0')
const REPO = "/home/scami/kilang-ref"
const WT = REPO + "/.claude/worktrees/prose-ch" + NN
const W = REPO + "/.superpowers/sdd/2026-10-24-ki-plan-01-part1"
const PLAN = "docs/superpowers/plans/2026-10-24-ki-plan-01-part1.md"
const PKG = "/home/scami/.claude/plugins/cache/claude-plugins-official/superpowers/7.0.0/skills/subagent-driven-development/scripts/review-package"
const SCRATCH = "/home/scami/kilang-ref/build/review"
const L = (...l) => l.join("\n")
const sha = (c) => String(c || '').split(' ')[0]
const FIND = { type: 'object', properties: { findings: { type: 'array', items: { type: 'object', properties: { severity: { type: 'string', enum: ['Critical', 'Important', 'Minor', 'Nit'] }, where: { type: 'string' }, claim: { type: 'string' }, fix: { type: 'string' } }, required: ['severity', 'where', 'claim', 'fix'] } }, report_path: { type: 'string' } }, required: ['findings', 'report_path'] }
const DONE = { type: 'object', properties: { status: { type: 'string' }, head: { type: 'string' }, report_path: { type: 'string' }, concerns: { type: 'array', items: { type: 'string' } } }, required: ['status', 'head', 'report_path', 'concerns'] }
const IMPL = { type: 'object', properties: { status: { type: 'string' }, commit: { type: 'string' }, tags: { type: 'array', items: { type: 'string' } }, report_path: { type: 'string' }, concerns: { type: 'array', items: { type: 'string' } } }, required: ['status', 'commit', 'tags', 'report_path', 'concerns'] }
const REV = { type: 'object', properties: { verdict: { type: 'string' }, critical_important: { type: 'array', items: { type: 'string' } }, minors: { type: 'array', items: { type: 'string' } }, review_path: { type: 'string' } }, required: ['verdict', 'critical_important', 'minors', 'review_path'] }

let mainLock = Promise.resolve()
const onMain = (fn) => { const p = mainLock.then(fn); mainLock = p.then(() => null, () => null); return p }
const RULES = "Never write in the main checkout unless this prompt says you own it; use git-archive copies in a NEW dir under " + SCRATCH + " (delete it afterwards). Cap the memory of every mutant, fuzz or stress run: systemd-run --user --scope -q -p MemoryMax=4G -p MemorySwapMax=0 go test …, one go test at a time (an unbounded mutant otherwise kills the whole machine). Bound every long command. Never push, never stash."
const isProse = (f) => new RegExp('ch' + NN + '-[a-z0-9-]+\\.md').test(f.where)
const serious = (f) => f.severity === 'Critical' || f.severity === 'Important'

// ================= plan amendment =================
phase('Plan')
if (A.planAmend) {
  await onMain(() => agent(L("You own the main checkout of " + REPO + " for this job: amend the design documents (Spanish) as the rulings below say.",
    "Rulings to write: " + A.planAmend,
    "You may edit the spec (docs/superpowers/specs/2026-10-24-ki-design-go.md), the roadmap (docs/superpowers/plans/2026-10-24-ki-plan-00-roadmap.md) and " + PLAN + ", nothing else. Keep their style and Spanish (the spec says [decisión del diseño] on design decisions). The spec's chapter tables are parsed by cmd/bookx/outline.go (parseSpecIndex): keep their exact format, renumber chapters and hours consistently everywhere (cross-references, milestones table, totals), and check with go build -o build/bin/bookx ./cmd/bookx && build/bin/bookx web build/web that the outline parses with the new chapter count (then rm -rf build/web). Verify every statement about existing code against the tags (git show <tag>:<path>). Commit each document as its own commit. Never push or stash.",
    "Report " + W + "/plan-amend-ch" + NN + ".md. Structured output: status, head, report_path, concerns."), { label: 'plan:amend', phase: 'Plan', schema: DONE, model: 'fable' }))
}

// ================= chapter N prose + panel =================
const codeBatch = (label, what, report) => onMain(() => agent(L(
  "You own the main checkout of " + REPO + " (Go edition, Plan 01) for this job: a chapter-" + A.n + " code fix batch folded IN PLACE into the tagged ch" + NN + "-sK session commits.",
  "Read first: " + REPO + "/docs/superpowers/process/implementer.md (binds you; note the memory-cap and resume rules in §11) and " + W + "/fix-batch-ch06-report.md (the procedure: one 'fixup! <exact session subject>' commit per fix, 'git -c sequence.editor=: rebase -i --autosquash --update-refs <parent of ch" + NN + "-s1>', ci/retag-all.sh). Later chapters' commits on main get re-parented; retag-all moves their tags.",
  what,
  "Never leave a finding open as a 'controller's call': choose the option that makes code, comments, cards and prose agree and say which in the report. Never promise exact diagnostic counts or exhaustive lists of exceptions in comments or cards unless a test enumerates them; state the guarantee you can test.",
  "Card ids stay stable (append new cards at the end of book/tests/ch" + NN + ".tests); card text claims exactly what its test checks. Every new check kills the mutant it targets (run each once, memory-capped). ci/ci-local.sh in a git-archive copy of every moved tag and at HEAD; go build -o build/bin/bookx ./cmd/bookx && build/bin/bookx verify. Range-diff in the report. Never touch " + WT + " or branch prose/ch" + NN + ". Never push or stash.",
  "Report " + report + ": old->new for every tag, every card changed, every failure message added or changed. Structured output: status, head, report_path, concerns."), { label, phase: 'Prose + panel', schema: DONE, model: 'sonnet' }))
const rereview = (label, report, files, out) => agent(L(
  "Scoped re-review of a chapter-" + A.n + " code fix batch in " + REPO + " (Go edition). Read " + REPO + "/docs/superpowers/process/reviewer.md (Cost discipline included). Batch report: " + report + ". Findings it addressed: " + files + ".",
  "Check each finding is fixed or soundly declined, each named mutant dies (run each once), cards exact, nothing regressed. Rate Important only what would mislead a reader or let a plausible real bug through. " + RULES,
  "Write " + out + ". Structured output: findings (only open or new; empty if clean) and report_path."), { label, phase: 'Prose + panel', schema: FIND, model: 'sonnet' })
const proseAgent = (label, task) => agent(L(
  "You maintain the Spanish prose of chapter " + A.n + " of the Ki book (Go edition): book/ch" + NN + "-*.md on branch prose/ch" + NN + " in the worktree " + WT + ". You own that worktree only.",
  "Read " + REPO + "/docs/superpowers/process/prose.md (binding) and the history in " + W + "/task-" + A.proseTask + "-report.md. The budget is 3 000 flow words (bookx words <file>), see the concision rules in prose.md; fixes must not grow the running text.",
  task,
  "Re-verify every claim you touch on the machine (git-archive copies; never run anything in the main checkout; memory-cap any mutant run). Commit only the chapter file as 'fixup! book: chapter " + NN + " prose'. Regenerate (do not commit) book/tests/ch" + NN + ".list and book/verified-tags.txt with ci/check-tags.sh --list-as ch" + NN + " <last ch" + NN + " session tag> and ci/check-tags.sh 'ch" + NN + "-s*' so bookx verify passes; remove build/. Append what you did to " + W + "/task-" + A.proseTask + "-report.md.",
  "Structured output: status, head (prose branch SHA), report_path, concerns (include the word count)."), { label, phase: 'Prose + panel', schema: DONE, model: 'opus' })
const lenses = [
  { key: 'accuracy', model: 'opus', p: "LENS 1 — Go technical accuracy. Check EVERY technical claim (Go, toolchain, stdlib, runtime, algorithms, other languages and compilers, performance figures) by running it on Go 1.27.3 here or against a primary source. A wrong or overstated claim, or a claim about what a test catches that is broader than the test, is Important." },
  { key: 'fidelity', model: 'sonnet', p: "LENS 2 — fidelity and reproduction. In git-archive copies of the tags, run every command the chapter tells the reader to run, in order, as a first-time reader: red phases and stub recipes, every ki command and its output, every quoted compiler/test/runtime message and every named mutant (memory-capped), every exercise solution applied from the chapter's own blocks; check the cards the prose cites against book/tests/ch" + NN + ".tests at the last session tag. A failing step or differing output is Important; measured figures need only the same order of magnitude with «en esta máquina»." },
  { key: 'pedagogy', model: 'opus', p: "LENS 3 — pedagogy and Spanish. Read as someone who knows TS/JS, Python and Rust, has read the earlier published chapters (main: book/ch0*.md), new to everything else in Go: every new construct explained at first use (listings and tests too), helpers named with their file, sensible order, the template's sections, exercises doable from the text alone, review questions that test understanding, «Go en el trabajo» correct, Spanish (Spain), «tú», no gendered words about the reader, easy to follow, within 3 000 flow words (bookx words) and at most about 8 :::more; a stretch still dense or tedious is Important. A concept used before it is explained, or an exercise that needs its solution to start, is Important." },
  { key: 'code', model: 'sonnet', p: "LENS 4 — code review of chapter " + A.n + "'s code: git -C " + REPO + " diff ch" + PP + " ch" + NN + "-s<last> (all sessions, cards, goldens). Correctness bugs, weak tests (surviving mutants that matter: verify each with one focused, memory-capped mutant), overclaiming comments, cards claiming more than their tests, performance traps, portability. Rate Important only what would mislead a reader or let a plausible real bug through; mutants no careless implementer would write are Minor." },
]
const lensPrompt = (l, r) => L(l.p,
  "Material: prose book/ch" + NN + "-*.md on branch prose/ch" + NN + " in the worktree " + WT + " (read its HEAD); code at tags ch" + NN + "-s* of " + REPO + "; plan " + REPO + "/" + PLAN + " (Tarea " + A.proseTask + " and the Plantilla); conventions " + REPO + "/docs/superpowers/process/{prose.md,reviewer.md}; session reviews and fix reports for this chapter in " + W + ";" + (r > 1 ? " earlier rounds " + W + "/panel-ch" + NN + "-" + l.key + "-{1.." + (r - 1) + "}.md; what changed since: the end of " + W + "/task-" + A.proseTask + "-report.md and the latest fix-batch-ch" + NN + "-panel*-report.md." : ""),
  (r > 1 ? "Round " + r + ": (1) confirm each previous-round finding of your lens is fixed; (2) do a FULL fresh pass of your lens over the whole chapter." : "Round 1: a full pass of your lens over the whole chapter.") + " Do not modify anything. " + RULES + " One focused check per finding.",
  "Write " + W + "/panel-ch" + NN + "-" + l.key + "-" + r + ".md. Structured output: findings (severity, where file:line, claim, fix; empty if nothing) and report_path.")

let preFailed = false; let resolvePre; const preDone = new Promise((r) => { resolvePre = r })
async function chapterN() {
  try { for (const n of (A.preTasks || [])) {
    const impl = await onMain(() => agent(implPrompt(n, NN), { label: 'impl:T' + n, phase: 'Prose + panel', schema: IMPL, model: 'sonnet' }))
    if (!impl || !/^DONE/.test(impl.status)) { preFailed = true; return { stopped: 'T' + n, impl } }
    const head = sha(impl.commit)
    const rv = await agent(reviewPrompt(n, head + '~1', head), { label: 'review:T' + n, phase: 'Prose + panel', schema: REV, model: 'sonnet' })
    if (rv && (rv.critical_important.length || rv.minors.length)) {
      await onMain(() => agent(L("You own the main checkout of " + REPO + " for this job: apply EVERY finding of " + rv.review_path + " in place at the ch" + NN + " session commit of Tarea " + n + " (fixup! <exact subject>, autosquash rebase from its parent, ci/retag-all.sh), as in " + W + "/fix-batch-ch06-report.md.", "Read " + REPO + "/docs/superpowers/process/implementer.md. Memory-cap mutant runs. Never touch .claude/worktrees/. Never push or stash.", "Report " + W + "/fix-batch-T" + n + "-report.md. Structured output: status, head, report_path, concerns."), { label: 'fix:T' + n, phase: 'Prose + panel', schema: DONE, model: 'sonnet' }))
    }
  } } finally { resolvePre() }
  const p = await agent(L(
    "Write Tarea " + A.proseTask + " of the Ki book (Go edition): the Spanish prose of chapter " + A.n + ".",
    "Read " + REPO + "/docs/superpowers/process/prose.md first (binding: worktree flow, template, verification on the machine, «tú» with no gendered words, length; the budget is 3 000 flow words (bookx words <file>), see the concision rules in prose.md; fixes must not grow the running text). Brief: cd " + REPO + " && bash docs/superpowers/process/task-brief.sh " + PLAN + " " + A.proseTask + ".",
    "Create the worktree yourself: git -C " + REPO + " worktree add " + WT + " -b prose/ch" + NN + " main (if it already exists with an uncommitted draft from an interrupted run, continue from that draft). Work only there; another agent modifies the main checkout meanwhile, never run anything in it.",
    "Read the published chapters book/ch0*.md on main: chapters 4 and 5 each passed several rounds of a four-lens panel (accuracy, fidelity/reproduction, pedagogy/Spanish, code). Read their panel files " + W + "/panel-ch04-*.md and panel-ch05-*.md to avoid the recurring failure modes from the start: a construct, tool flag or test helper used before it is explained (name each session's helpers, what they check and their file); quoted messages that only one specific mutant produces without naming it; red-phase stub recipes that panic and cut the test binary; claims about what a test catches broader than the test; unbounded measured figures without «en esta máquina»; cards claiming more than their tests; plan jargon («Tarea N») in reader-facing text. Do not re-teach what earlier chapters taught.",
    "Chapter " + A.n + "'s code is at tags ch" + NN + "-s*, after reviews and a fix batch: trust the tags and book/tests/ch" + NN + ".tests, not the plan's card ranges. Decisions and rulings: " + W + "/task-*-review.md and fix-batch-ch" + NN + "-report.md (and fix-batch-ch" + NN + "-rereview-1.md)" + (A.planAmend ? ", plus the plan amendment just committed (" + W + "/plan-amend-ch" + NN + ".md)" : "") + ". Generate (do not commit) book/tests/ch" + NN + ".list and the verified-tags lines with ci/check-tags.sh so bookx verify passes. Commit only the chapter file as ONE commit 'book: chapter " + NN + " prose' on prose/ch" + NN + "; remove build/. Never touch main, never merge, tag or push. Memory-cap every mutant run.",
    (A.proseNote ? "Controller notes for this chapter: " + A.proseNote + "\n" : "") + "Report " + W + "/task-" + A.proseTask + "-report.md. Structured output: status, commit (short SHA + subject), tags (empty), report_path, concerns (include the word count)."), { label: 'prose:ch' + NN, phase: 'Prose + panel', schema: IMPL, model: 'opus' })
  if (!p || !/^DONE/.test(p.status)) return { stopped: 'prose', p }
  let open = [], round = 1, last = null
  while (round <= (A.maxRounds || 6)) {
    if (open.length) {
      await codeBatch('fix:ch' + NN + '-code-rr#' + round, "Fix these findings from the re-review of the previous chapter-" + A.n + " batch: " + JSON.stringify(open), W + '/fix-batch-ch' + NN + '-panel' + round + '-pre-report.md')
      await proseAgent('prose:ch' + NN + '-resync-pre#' + round, "A code batch just landed: read " + W + "/fix-batch-ch" + NN + "-panel" + round + "-pre-report.md. Rebase prose/ch" + NN + " onto main's current 'book: verify ch" + PP + "' commit if its base moved (git rebase --onto <new> <old merge-base> prose/ch" + NN + "), and make the prose exact again for whatever changed.")
      open = []
    }
    const res = await parallel(lenses.map((l) => () => agent(lensPrompt(l, round), { label: 'panel:' + l.key + '#' + round, phase: 'Prose + panel', schema: FIND, model: l.model })))
    const all = res.filter(Boolean).flatMap((x) => x.findings)
    last = { round, all }
    const dry = !all.some(serious)
    const proseF = all.filter(isProse), codeF = all.filter((f) => !isProse(f))
    log('ch' + NN + ' panel round ' + round + ': ' + all.filter(serious).length + ' Critical/Important, ' + all.length + ' total')
    if (codeF.length) {
      await codeBatch('fix:ch' + NN + '-code#' + round, "Fix these chapter-" + A.n + " panel round-" + round + " findings (code, tests, cards, goldens; files " + W + "/panel-ch" + NN + "-*-" + round + ".md): " + JSON.stringify(codeF) + ". Minor/Nit findings may be declined only with a stated reason.", W + '/fix-batch-ch' + NN + '-panel' + round + '-report.md')
      const rr = await rereview('rereview:ch' + NN + '-code#' + round, W + '/fix-batch-ch' + NN + '-panel' + round + '-report.md', W + '/panel-ch' + NN + '-*-' + round + '.md', W + '/fix-batch-ch' + NN + '-panel' + round + '-rereview-1.md')
      open = rr ? rr.findings.filter(serious) : []
    }
    await proseAgent('prose:ch' + NN + '-fix#' + round, L(
      codeF.length ? "A code batch just landed: read " + W + "/fix-batch-ch" + NN + "-panel" + round + "-report.md; rebase prose/ch" + NN + " onto main's current 'book: verify ch" + PP + "' commit if its base moved, and make the prose exact again for whatever changed." : "No code changed this round.",
      proseF.length ? "Fix ALL these prose findings of panel round " + round + " (files " + W + "/panel-ch" + NN + "-*-" + round + ".md): " + JSON.stringify(proseF) : "No prose findings this round."))
    round++
    if (dry && !open.length) return { dry: true, rounds: round - 1, last }
  }
  return { dry: false, rounds: round - 1, last }
}

// ================= chapter N+1 sessions =================
const implPrompt = (n, CH) => L("You own the main checkout of the Ki reference repo " + REPO + " (Go edition, Plan 01) for this job: implement Tarea " + n + ".",
  "First read " + REPO + "/docs/superpowers/process/implementer.md (binds you, incl. the memory-cap and resume rules in §11: if the checkout holds uncommitted work from an interrupted attempt at THIS task, continue from it after restoring any mutation-harness backups). Brief: cd " + REPO + " && bash docs/superpowers/process/task-brief.sh " + PLAN + " " + n + " (values verbatim). Base: current main HEAD. Read the earlier reports of this chapter (" + W + "/task-<N>-report.md) and the previous chapter's fix-batch report for the interfaces built so far.",
  "Tests kill plausible mutants (run each once, memory-capped), comments and cards claim exactly what the code and tests do, no wall-clock assertions, bounded inputs. Test files that drive unexported identifiers are package-internal and named *_internal_test.go. Card ids: next free in book/tests/ch" + (CH || MM) + ".tests (create it in ch05.tests' format if missing). Never touch .claude/worktrees/. Never push or stash.",
  "Report " + W + "/task-" + n + "-report.md. Structured output: status, commit (short SHA + subject), tags, report_path, concerns.")
const reviewPrompt = (n, base, head) => L("Review Tarea " + n + " of the Ki reference repo " + REPO + " (Go edition, Plan 01): commit " + head + ", base " + base + ".",
  "First read " + REPO + "/docs/superpowers/process/reviewer.md (Cost discipline included). Brief " + W + "/task-" + n + "-brief.md (or generate it), report " + W + "/task-" + n + "-report.md, package: cd " + REPO + " && bash " + PKG + " " + PLAN + " " + base + " " + head + ". Judge committed content only, by SHA (other agents rewrite main meanwhile). " + RULES,
  "Full review to " + W + "/task-" + n + "-review.md. Structured output: verdict, critical_important, minors, review_path.")
async function nextChapter() {
  if (!A.next || !A.next.tasks || !A.next.tasks.length) return { skipped: true }
  await preDone
  if (preFailed) return { skipped: 'chapter N sessions stopped' }
  const revs = []
  let pendingRev = null
  for (const n of A.next.tasks) {
    if (A.rulings && String(n) === String(A.rulingsBefore)) {
      await onMain(() => agent(L("You own the main checkout of " + REPO + " for this job: write these controller rulings into the plan " + PLAN + " (Spanish, [decisión del controlador]) wherever the plan states the opposite or omits them, and nothing else; commit as 'docs: plan 01 — rulings before Tarea " + n + "'.", A.rulings, "Verify statements about existing code against the tags. Never push or stash. Report " + W + "/plan-rulings-T" + n + ".md. Structured output: status, head, report_path, concerns."), { label: 'plan:rulings', phase: 'Next sessions', schema: DONE, model: 'fable' }))
    }
    const impl = await onMain(() => agent(implPrompt(n, MM) + (A.rulings && parseInt(n, 10) >= parseInt(A.rulingsBefore, 10) ? "\nController rulings already written into the plan (read them there; they bind you): " + A.rulings : ''), { label: 'impl:T' + n, phase: 'Next sessions', schema: IMPL, model: 'sonnet' }))
    if (pendingRev) revs.push(await pendingRev)
    if (!impl || !/^DONE/.test(impl.status)) return { stopped: 'T' + n, impl, revs }
    const head = sha(impl.commit)
    pendingRev = agent(reviewPrompt(n, head + '~1', head), { label: 'review:T' + n, phase: 'Next sessions', schema: REV, model: 'sonnet' })
  }
  if (pendingRev) revs.push(await pendingRev)
  const files = revs.filter(Boolean).map((r) => r.review_path)
  const B = await onMain(() => agent(L("You own the main checkout of " + REPO + " (Go edition, Plan 01) for this job: the chapter-" + A.next.n + " fix batch, folded IN PLACE into the ch" + MM + "-sK session commits.",
    "Read " + REPO + "/docs/superpowers/process/implementer.md and " + W + "/fix-batch-ch06-report.md (procedure: fixup! <exact session subject>, 'git -c sequence.editor=: rebase -i --autosquash --update-refs <parent of ch" + MM + "-s1>', ci/retag-all.sh).",
    "Apply EVERY Critical, Important and Minor finding of these session reviews: " + files.join(', ') + ". Decline only with a stated reason; anything that needs a plan or process-doc change goes to Concerns with proposed text (you may not edit docs/superpowers/). Every new check kills its mutant (run each once, memory-capped); cards exact; card ids stable. ci/ci-local.sh in a git-archive copy of every moved ch" + MM + " tag and at HEAD; bookx verify. Never touch .claude/worktrees/. Never push or stash.",
    "Report " + W + "/fix-batch-ch" + MM + "-report.md with old->new for every tag and the range-diff. Structured output: status, head, report_path, concerns."), { label: 'fix:ch' + MM + '-batch', phase: 'Next sessions', schema: DONE, model: 'sonnet' }))
  const RR = B ? await agent(L("Scoped re-review of the chapter-" + A.next.n + " fix batch in " + REPO + ". Read " + REPO + "/docs/superpowers/process/reviewer.md. Batch report " + W + "/fix-batch-ch" + MM + "-report.md; findings " + files.join(', ') + ". Each finding fixed or soundly declined; each named mutant dies (run once); cards exact; nothing regressed. " + RULES, "Write " + W + "/fix-batch-ch" + MM + "-rereview-1.md. Structured output: findings (open or new only) and report_path."), { label: 'rereview:ch' + MM + '-batch', phase: 'Next sessions', schema: FIND, model: 'sonnet' }) : null
  if (RR && RR.findings.some(serious)) {
    const B2 = await onMain(() => agent(L("You own the main checkout of " + REPO + " for this job: fix the open findings of the chapter-" + A.next.n + " batch re-review, in place, same procedure as " + W + "/fix-batch-ch" + MM + "-report.md: " + JSON.stringify(RR.findings), "Read " + REPO + "/docs/superpowers/process/implementer.md. Memory-cap mutant runs. Never touch .claude/worktrees/. Never push or stash.", "Report " + W + "/fix-batch-ch" + MM + "-2-report.md. Structured output: status, head, report_path, concerns."), { label: 'fix:ch' + MM + '-batch2', phase: 'Next sessions', schema: DONE, model: 'sonnet' }))
    return { revs, B, RR, B2 }
  }
  return { revs, B, RR }
}

phase('Prose + panel')
const [CN, CM] = await parallel([() => chapterN(), () => nextChapter()])
if (!CN || !CN.dry) return { stopped: 'ch' + NN + '-not-dry', CN, CM }

phase('Integrate')
const integ = await onMain(() => agent(L(
  "You own the main checkout of " + REPO + " (Go edition, Plan 01) for this job: integrate chapter " + A.n + " into main exactly as chapter 5 was integrated (" + W + "/integrate-ch05-report.md).",
  "Read " + REPO + "/docs/superpowers/process/implementer.md first. Steps: (1) in " + WT + ", discard the uncommitted book/verified-tags.txt and book/tests/ch" + NN + ".list, then squash prose/ch" + NN + " into ONE commit 'book: chapter " + NN + " prose' (git -c sequence.editor=: rebase -i --autosquash <its base>). (2) On main: detach at the commit whose subject is 'book: verify ch" + PP + "'; run ci/check-tags.sh --list-as ch" + NN + " <last ch" + NN + " session tag> and ci/check-tags.sh 'ch" + NN + "-s*'; commit book/tests/ch" + NN + ".list + book/verified-tags.txt as 'book: verify ch" + NN + " sessions'; cherry-pick the squashed prose commit; tag it annotated 'ch" + NN + "' with the message the plan gives in Tarea " + A.proseTask + " (or a short 'ch" + NN + ": <topic>' if none); git rebase --onto <prose commit> <'book: verify ch" + PP + "' commit> main; ci/retag-all.sh (ch" + MM + " tags move; ch" + NN + "-s* must not). (3) ci/check-tags.sh ch" + NN + " and commit the verified-tags update at HEAD as 'book: verify ch" + NN + "'. (4) ci/ci-local.sh at HEAD 'verify: OK'; go build -o build/bin/bookx ./cmd/bookx && build/bin/bookx verify. (5) Check the worktree's prose commit equals the cherry-picked chapter file (git diff), then git worktree remove " + WT + " and git branch -D prose/ch" + NN + ".",
  "Never push or stash. Report " + W + "/integrate-ch" + NN + "-report.md. Structured output: status, head, report_path, concerns (final SHAs of ch" + NN + " and of every ch" + MM + " session tag)."), { label: 'integrate:ch' + NN, phase: 'Integrate', schema: DONE, model: 'sonnet' }))
return { CN: { rounds: CN.rounds }, CM, integ }