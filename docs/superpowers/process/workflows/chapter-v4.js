export const meta = {
  name: 'ki-chapter-v4',
  description: 'Ki book, one chapter: sessions and session reviews, code gate, frozen session tags, draft, lint and doctest, one blind pass, one fix, a check of the diff (at most 2 turns), integrate, verify. Fails closed. See docs/superpowers/process/pipeline.md',
  phases: [{ title: 'Sessions' }, { title: 'Code gate' }, { title: 'Draft' }, { title: 'Blind pass' }, { title: 'Fix' }, { title: 'Integrate' }, { title: 'Second pass' }],
}
// args: pipeline.md has the full list and an example.

// <shared> Same text in chapter-v4.js and part-gate.js; dry-run.test.mjs checks it.
const A = args || {}
const DRY = !!A.dryRun
const TRACE = []
const REPO = A.repo || "/home/scami/kilang-ref"
const PLAN = A.plan || "docs/superpowers/plans/2026-10-24-ki-plan-01-part1.md"
const W = REPO + "/.superpowers/sdd/" + PLAN.split('/').pop().replace(/\.md$/, '')
const SCRATCH = REPO + "/build/review"
const PROC = REPO + "/docs/superpowers/process"
const L = (...l) => l.filter((line) => line).join("\n")
const pad = (n) => String(n).padStart(2, '0')
const uniq = (xs) => [...new Set(xs)]
const finish = (r) => (DRY ? { ...r, trace: TRACE } : r)

// A stop carries where and why, and nothing can turn it into a clean review.
class Stop extends Error {
  constructor(where, why) { super(where + ': ' + why); this.where = where; this.why = why }
}
const need = (ok, label, why) => { if (!ok) throw new Stop(label, why) }

const BEHAVIOUR = ['message', 'fixit', 'output', 'limit', 'memory-death']
const KINDS = ['text', ...BEHAVIOUR, 'test-gap', 'other']
const FINDING = {
  type: 'object',
  properties: {
    id: { type: 'string', description: 'unique: <lens>-<number>, and the same id in your report file' },
    severity: { type: 'string', enum: ['Critical', 'Important', 'Minor', 'Nit'] },
    owner: { type: 'string', enum: ['prose', 'code'], description: 'prose: the chapter text or the wording of a card, and where starts with book/. code: anything else, and where does not start with book/ch' },
    kind: { type: 'string', enum: KINDS, description: 'text: owner prose. For owner code: message, fixit, output, limit or memory-death when the reader or a user of ki would see the failure; test-gap when a test does not check what is claimed; other otherwise' },
    visible: { type: 'boolean', description: 'the reader sees this on the page: text, card, listing, shown message. Always true for owner prose' },
    where: { type: 'string', description: 'repo-relative file:line; the first location decides' },
    claim: { type: 'string' },
    evidence: { type: 'string', description: 'the command and its output, or the primary source and its words' },
  },
  required: ['id', 'severity', 'owner', 'kind', 'visible', 'where', 'claim', 'evidence'],
}
const FIND = {
  type: 'object',
  properties: {
    findings: { type: 'array', items: FINDING },
    checked: { type: 'integer', description: 'how many claims, steps or checks you really went through; a report of nothing is not a clean review' },
    report_path: { type: 'string' },
  },
  required: ['findings', 'checked', 'report_path'],
}
const DONE = { type: 'object', properties: { status: { type: 'string' }, head: { type: 'string' }, report_path: { type: 'string' }, concerns: { type: 'array', items: { type: 'string' } } }, required: ['status', 'head', 'report_path', 'concerns'] }
const RESOLVED = {
  type: 'object',
  properties: {
    ...DONE.properties,
    resolutions: { type: 'array', items: { type: 'object', properties: { id: { type: 'string' }, action: { type: 'string', enum: ['fixed', 'narrowed', 'deleted', 'rejected'] }, note: { type: 'string' } }, required: ['id', 'action', 'note'] } },
  },
  required: [...DONE.required, 'resolutions'],
}
const serious = (f) => f.severity === 'Critical' || f.severity === 'Important'
const isBehaviour = (f) => BEHAVIOUR.includes(f.kind)
const ids = (fs) => fs.map((f) => f.id).join(', ')

// Validators: a dead agent, an empty result or a wrong shape all stop the chapter.
const checkFind = (o) => (r, label) => {
  need(r && typeof r === 'object' && Array.isArray(r.findings), label, 'empty result: no findings array')
  need(Number.isInteger(r.checked) && r.checked > 0, label, 'empty result: it says it checked nothing, which is not a clean review')
  need(r.report_path === o.path, label, 'report_path is ' + JSON.stringify(r.report_path) + ', want ' + o.path)
  const seen = new Set()
  for (const f of r.findings) {
    const name = label + ' ' + JSON.stringify(f && f.id)
    need(f && typeof f.id === 'string' && f.id.startsWith(o.lens + '-') && !seen.has(f.id), name, 'the id must be unique and start with ' + o.lens + '-')
    seen.add(f.id)
    need(['Critical', 'Important', 'Minor', 'Nit'].includes(f.severity), name, 'bad severity ' + f.severity)
    need(['prose', 'code'].includes(f.owner), name, 'owner must be prose or code, got ' + f.owner)
    need(KINDS.includes(f.kind), name, 'bad kind ' + f.kind)
    need(typeof f.visible === 'boolean', name, 'visible must be a boolean')
    for (const k of ['where', 'claim', 'evidence']) need(typeof f[k] === 'string' && f[k].trim() !== '', name, k + ' is empty')
    if (f.owner === 'prose') {
      need(f.where.startsWith('book/'), name, 'owner prose but where is ' + f.where)
      need(f.kind === 'text', name, 'owner prose needs kind text')
      need(f.visible === true, name, 'owner prose is always visible')
      need(!o.codeOnly, name, 'this lens may only report owner code')
    } else {
      need(!f.where.startsWith('book/ch'), name, 'owner code but where is the chapter text: ' + f.where)
      need(f.kind !== 'text', name, 'owner code cannot have kind text')
    }
    need(f.severity !== 'Nit' || o.nits === true, name, 'only the truth readers report Nits')
  }
}
const checkDone = (path) => (r, label) => {
  need(r && typeof r === 'object' && typeof r.status === 'string', label, 'empty result: no status')
  need(/^DONE/.test(r.status), label, 'status ' + r.status + ': ' + JSON.stringify(r.concerns || []))
  need(typeof r.head === 'string' && r.head.trim() !== '', label, 'empty result: no head')
  need(!path || r.report_path === path, label, 'report_path is ' + JSON.stringify(r.report_path) + ', want ' + path)
}
const checkResolved = (path, required, allowed) => (r, label) => {
  checkDone(path)(r, label)
  need(Array.isArray(r.resolutions), label, 'empty result: no resolutions')
  for (const x of r.resolutions) need(allowed.includes(x.action), label, x.id + ': action ' + x.action + ' is not allowed here; allowed: ' + allowed.join(', '))
  const got = new Set(r.resolutions.map((x) => x.id))
  const left = required.filter((f) => !got.has(f.id))
  need(!left.length, label, 'left silently, with no resolution: ' + ids(left))
}

// Every agent goes through here. In a dry run its prompt is a stub that says what to answer.
const spawn = (label, prompt, o) => {
  if (!DRY) return agent(prompt, { label, phase: o.phase, schema: o.schema, model: o.model })
  TRACE.push({ label, model: o.model, phase: o.phase, file: o.file, prompt })
  const canned = (A.dryRun.answers || {})[label]
  const answer = canned === undefined ? o.dry : { ...o.dry, ...canned }
  return agent(L("DRY RUN of " + label + ". Run no command and read no file.", "RESULT_FILE: " + o.file, "Return exactly this as your structured output:", JSON.stringify(answer)), { label, phase: o.phase, schema: o.schema, model: 'haiku' })
}
const run = async (label, prompt, o) => {
  let res
  try { res = await spawn(label, prompt, o) } catch (e) { throw new Stop(label, 'the agent failed: ' + (e && e.message)) }
  need(res !== null && res !== undefined && res !== '', label, 'the agent died or was skipped, and left no result')
  try { o.check(res, label) } catch (e) {
    if (e instanceof Stop) e.why += ' [if ' + o.file + ' holds this answer, delete it before running again]'
    throw e
  }
  return res
}
// parallel() turns a throw into null and never rejects, so each slot is looked at here.
const gather = async (items) => {
  const stops = []
  const out = await parallel(items.map(([label, fn]) => async () => {
    try {
      const r = await fn()
      if (r === null || r === undefined) stops.push(label + ': no result')
      return r
    } catch (e) { stops.push(e instanceof Stop ? e.message : label + ': ' + (e && e.message)); return null }
  }))
  items.forEach(([label], i) => { if ((out[i] === null || out[i] === undefined) && !stops.some((s) => s.startsWith(label))) stops.push(label + ': no result') })
  if (stops.length) throw new Stop(stops.map((s) => s.split(': ')[0]).join(' + '), stops.join(' | '))
  return out
}
let mainLock = Promise.resolve()
const onMain = (fn) => { const p = mainLock.then(fn); mainLock = p.then(() => null, () => null); return p }

// The result file is what makes a re-run safe, in this session or in any other.
const doneRule = (file, input) => "RESUME RULE. If " + file + " exists and its \"input\" field is exactly \"" + input + "\", this step already finished: return its \"result\" field as your structured output and do nothing else. Otherwise an earlier attempt may have died half way: look at what it left before you start (git status, git log, an open rebase, the report file) and continue from it. Uncommitted work may be yours from that attempt: read it, keep what is right. Never git reset --hard, git clean, git stash or a second worktree. As your LAST act, once everything else has succeeded (a BLOCKED answer writes no file, so that a re-run tries again), mkdir -p its directory and write {\"input\": \"" + input + "\", \"result\": <your structured output>} to " + file + ".tmp and mv it to " + file + "."
const CAP = "ci/mutate, bookx cascade and bookx doctest cap their own memory. Any other mutant, fuzz or stress run goes under systemd-run --user --scope -q -p MemoryMax=4G -p MemorySwapMax=0, one go test at a time; an unbounded mutant otherwise kills the whole machine. Bound every long command. Never push, never stash."
const READER = "Do not modify the repo, its tags or any worktree. Never run anything in the main checkout. Work in a NEW dir under " + SCRATCH + " that you delete at the end. ci/mutate needs a git repository, so in your copy first run git init -q && git add -A && git -c user.name=x -c user.email=x@x commit -qm copy; then check what a mutant does with ci/mutate --apply. " + CAP + " Report a finding only with evidence you produced: the command and its output, or the primary source and its words. The book is short on purpose: a claim that is true and merely not exhaustive is fine."
const FORMAT = (path, lens) => "Write " + path + ": what you checked and how, and every finding under its id. Structured output: findings (id starting with " + lens + "-, severity, owner, kind, visible, where, claim, evidence; empty if nothing is wrong), checked (how many you really went through) and report_path exactly " + path + "."
const dryFind = (path) => ({ findings: [], checked: 1, report_path: path })
const dryDone = (path, extra) => ({ status: 'DONE', head: 'dry0000', report_path: path, concerns: [], ...(extra || {}) })

// A step has a result file and a report next to it, named after it, in c.V.
const stepFind = (c, label, name, input, o, ...body) => {
  const path = (o.path || c.V + "/" + name + ".md"), file = c.V + "/" + name + ".json"
  return run(label, L(doneRule(file, input), ...body, FORMAT(path, o.lens)), { schema: FIND, model: o.model, phase: o.phase, file, dry: dryFind(path), check: checkFind({ path, lens: o.lens, codeOnly: o.codeOnly, nits: o.nits }) }).then((r) => { r.findings.forEach((f) => { f.from = path }); return r })
}
const stepDone = (c, label, name, input, o, ...body) => {
  const path = (o.path || c.V + "/" + name + ".md"), file = c.V + "/" + name + ".json"
  return run(label, L(doneRule(file, input), ...body, "Report " + path + ". Structured output: status (DONE, DONE_WITH_CONCERNS or BLOCKED), head, report_path exactly " + path + ", concerns."), { schema: o.schema || DONE, model: o.model, phase: o.phase, file, dry: { ...dryDone(path, { head: 'sha-' + name }), ...(o.dry || {}) }, check: o.check || checkDone(path) })
}
const froms = (fs) => uniq(fs.map((f) => f.from)).join(', ')
// </shared>

const FIRST_STRICT = 11, LAST_CONTROL = 13
const PLANRULE = "Once a session is tagged its code rules over the plan, except the promised theory topics and the rows of each diagnostic: in a doubt, first try to make the promise true rather than weaken it. Do not amend the plan for finished tasks."
const PKG = "$(command ls -d /home/scami/.claude/plugins/cache/claude-plugins-official/superpowers/*/skills/subagent-driven-development/scripts/review-package | sort -V | tail -1)"

const chapter = (x) => {
  const NN = pad(x.n)
  return { n: x.n, NN, PP: pad(x.n - 1), part: x.part, sessions: x.sessions || [], diagnostics: x.diagnostics, proseTask: x.proseTask, tagFallback: x.tagFallback, V: W + "/v4-ch" + NN, WT: REPO + "/.claude/worktrees/prose-ch" + NN, parked: W + "/parked-part-" + x.part + ".md", tag: 'ch' + NN, branch: 'prose/ch' + NN }
}
const argError = () => {
  if (!Number.isInteger(A.n) || A.n < 1) return 'n must be the chapter number'
  if (typeof A.part !== 'string' || !A.part) return 'part must be a name such as II: the parked file goes by it'
  if (A.proseTask === undefined) return 'proseTask is the plan task of the chapter prose'
  if (!Array.isArray(A.sessions)) return 'sessions must list the plan tasks of the sessions, in order'
  if (typeof A.diagnostics !== 'boolean') return 'diagnostics must say whether the chapter adds diagnostics: it decides the cascade and fix-it hunt'
  if (typeof A.tagFallback !== 'string' || !A.tagFallback) return 'tagFallback is the chapter tag message if the plan gives none'
  if (A.next && (!Number.isInteger(A.next.n) || !Array.isArray(A.next.sessions) || typeof A.next.diagnostics !== 'boolean')) return 'next needs n, sessions and diagnostics'
  return null
}
// ================= sessions =================
const implPrompt = (c, t) => L(
  "You own the main checkout of the Ki reference repo " + REPO + " for this job: implement Tarea " + t + ".",
  "First read " + PROC + "/implementer.md (it binds you). Brief: cd " + REPO + " && bash docs/superpowers/process/task-brief.sh " + PLAN + " " + t + " (values verbatim). Base: current main HEAD.",
  "If the session tag this task names already exists and its commit subject starts with that tag, the code is done: only finish the report. head is the SHA of the session commit.",
  PLANRULE,
  "Tests kill plausible mutants: ci/mutate --pkg <pkg> --since <the previous chapter tag>, then --apply for what its operators do not cover; a card claims what a mutant does only if that mutant ran. A card's why aims at 25 words and bookx verify fails it above 35 from chapter " + FIRST_STRICT + " on: one sentence that names the bug the test prevents. Comments and cards claim exactly what code and tests do. Soft ceiling: about 400 test lines per reader hour of the session, diagnostics exempt; over it, merge rows into tables or say in one line why not. No wall-clock assertions, bounded inputs. Test files that drive unexported identifiers are *_internal_test.go. Card ids: next free in book/tests/ch" + c.NN + ".tests.",
  "Never touch .claude/worktrees/. " + CAP)
const reviewPrompt = (c, t, head) => L(
  "Review Tarea " + t + " of the Ki reference repo " + REPO + ": commit " + head + ", base " + head + "~1. Judge committed content only, by SHA: other agents rewrite main meanwhile.",
  "First read " + PROC + "/reviewer.md (Cost discipline included). Brief " + W + "/task-" + t + "-brief.md (or generate it), report " + W + "/task-" + t + "-report.md, package: cd " + REPO + " && bash " + PKG + " " + PLAN + " " + head + "~1 " + head + ".",
  "All your findings have owner code, even a card's wording. Mark visible what the reader will see on the page: a card, a listing, a message.",
  READER)

// Batches fold into the tagged session commits; this is the whole procedure, nothing else to read.
const FOLD = (c) => "Procedure: one 'fixup! <exact session subject>' commit per fix; git -c sequence.editor=: rebase -i --autosquash --update-refs " + c.tag + "-s1^; then ci/retag-all.sh (later commits on main get re-parented and their tags move with them). If you find a rebase open from an earlier attempt: git rebase --abort, the fixups are still there, start the fold again. A fixup already folded needs nothing."
const batch = (c, label, name, list, phaseName, extra) => {
  const path = c.V + "/" + name + ".md"
  const required = list.filter((f) => serious(f) || f.visible)
  return onMain(() => stepDone(c, label, name, ids(list), { model: 'sonnet', phase: phaseName, path, schema: RESOLVED, dry: { resolutions: list.map((f) => ({ id: f.id, action: 'fixed', note: 'dry' })) }, check: checkResolved(path, required, ['fixed', 'rejected']) },
    "You own the main checkout of " + REPO + " for this job: a chapter-" + c.n + " code batch folded IN PLACE into the tagged " + c.tag + "-sK session commits. Read " + PROC + "/implementer.md (it binds you).",
    FOLD(c),
    "Fix these findings, by id: " + ids(list) + ". They are in: " + froms(list) + ". An id you cannot find there means BLOCKED, not a guess.",
    extra,
    PLANRULE,
    "Never leave a finding open as someone else's call: choose what makes code, comments and cards agree. A rejection needs evidence in the report. Do not promise exact counts or exhaustive lists unless a test enumerates them. Card ids stay stable and text claims exactly what its test checks; a why stays within 25 words as far as it can, 35 at most. Every new check kills its mutant (ci/mutate --apply). ci/ci-local.sh in a git-archive copy of every moved tag and at HEAD; go build -o build/bin/bookx ./cmd/bookx && build/bin/bookx verify. Range-diff in the report, old->new for every tag, every card and message changed. Never touch " + c.WT + " or branch " + c.branch + ". " + CAP,
    "Structured output also has resolutions: one per id, action fixed or rejected, and a note."))
}
const rereview = (c, label, name, list, batchName, phaseName) => stepFind(c, label, name, ids(list), { lens: name, model: 'sonnet', phase: phaseName, codeOnly: true },
  "Scoped re-review of a chapter-" + c.n + " code batch in " + REPO + ". Read " + PROC + "/reviewer.md, section 6 for a re-review. Batch report: " + c.V + "/" + batchName + ".md. Findings it addressed, by id: " + ids(list) + ", in " + froms(list) + ".",
  "Each is fixed or soundly rejected, each named mutant dies (run each once with ci/mutate --apply), cards exact, nothing regressed. Only this scope: no fresh review. Rate Important only what would mislead a reader or let a plausible real bug through. Findings have owner code. " + READER)

// ================= code gate =================
const gateLenses = (c, i, input, phaseName) => {
  const lastRange = c.tag + "-s1^.." + input
  const list = [
    ['code', 'sonnet', L("CODE LENS, the whole of chapter " + c.n + ": git -C " + REPO + " diff " + lastRange + " (sessions, cards, goldens). Correctness bugs, weak tests, overclaiming comments, cards claiming more than their tests, performance traps, portability. Read the survivors of ci/mutate --pkg <pkg> --since " + c.tag + "-s1^ for each package touched and name what they mean; beyond its operators, name plausible wrong implementations and run each with ci/mutate --apply. Rate Important only what would mislead a reader or let a plausible real bug through; a mutant no careless implementer would write is Minor.")],
    ['truth', 'opus', L("WHY AND COMMENT READER, chapter " + c.n + ". Every why, case and expect of book/tests/ch" + c.NN + ".tests at " + input + ", and every comment in the code this chapter adds (git -C " + REPO + " diff " + lastRange + "). Do not read them for plausibility: RUN each one. The reason given must be the real reason: a mutant it names runs with ci/mutate --apply and dies the way it says, a behaviour it claims runs in a small program. A wrong reason is Important even if the conclusion holds. A card or comment that claims more than its test or code is Important. All, not a sample.")],
  ]
  if (c.diagnostics) list.push(['hunt', 'sonnet', L("CASCADE AND FIX-IT HUNT, chapter " + c.n + ", which adds diagnostics. Build ki at " + input + " in your copy. (1) bookx cascade --ki <bin> --limit 400 <file.ki>... on realistic programs of what this chapter's language can do (3 to 5, some your own; cmd/bookx/testdata/cascade is a start). Its exit code means nothing: read what it lists. A missing } costing one E0201 per later item is a known limit in internal/syntax/parser_stmt.go: report it only if this chapter makes it worse; report other cascades, and errors that point at correct text after reading a sample. (2) For each diagnostic this chapter adds: one program that triggers it, an exact render compared with its golden and docs/errors, and its fix-it applied by hand: the result must compile, or the help must say why not. Label, help and note as rustc would give them. Any wrong message or fix-it is Important, kind message or fixit.")])
  return gather(list.map(([key, model, p]) => [c.tag + ':gate' + i + ':' + key, () => stepFind(c, c.tag + ':gate' + i + ':' + key, 'gate' + i + '-' + key, input, { lens: 'gate' + i + '-' + key, model, phase: phaseName, codeOnly: true },
    p, "You are blind to the session reviews and to any other reader: do not read .superpowers/, except the result file this prompt names, which you look for first and write last, and your own report. Code: tags " + c.tag + "-s* of " + REPO + ". Conventions: " + PROC + "/reviewer.md sections 3 and 5. All your findings have owner code; a card's wording too, because the batch fixes it. Mark visible what the reader will see. Get the tree with git -C " + REPO + " archive " + input + " | tar -x -C <your new dir>. " + READER)]))
}

// Sessions, reviews, chapter batch, gate, freeze. Returns the frozen tags and what stays open.
const codeTrack = async (c) => {
  const P = 'Sessions'
  if (c.n === A.n && A.planAmend) {
    await onMain(() => stepDone(c, c.tag + ':plan', 'plan-amend', A.planAmend, { model: 'fable', phase: P },
      "You own the main checkout of " + REPO + " for this job: write these rulings into the plan " + PLAN + " for tasks not yet done (Spanish, [decisión del controlador]), and into the spec only if it says the opposite. Keep the spec's chapter tables exactly as cmd/bookx/outline.go parses them. Commit as 'docs: plan — rulings before chapter " + c.n + "'; if that commit exists, the job is done.", "Rulings: " + A.planAmend, CAP))
  }
  const pending = [], reviews = []
  for (const t of c.sessions) {
    const path = W + "/task-" + t + "-report.md"
    const impl = await onMain(() => stepDone(c, c.tag + ':impl:T' + t, 'impl-T' + t, 'Tarea ' + t, { model: 'sonnet', phase: P, path }, implPrompt(c, t)))
    const rev = stepFind(c, c.tag + ':review:T' + t, 'review-T' + t, impl.head, { lens: 'review-T' + t, model: 'sonnet', phase: P, path: W + "/task-" + t + "-review.md", codeOnly: true }, reviewPrompt(c, t, impl.head))
    pending.push([c.tag + ':review:T' + t, () => rev])
    rev.catch(() => null) // looked at in gather below; a dead review must not end the process before that
  }
  if (pending.length) reviews.push(...await gather(pending))
  const fromReviews = reviews.flatMap((r) => r.findings)
  const parked = fromReviews.filter((f) => !(serious(f) || f.visible))
  const chapterBatch = fromReviews.filter((f) => serious(f) || f.visible)
  if (chapterBatch.length) await batch(c, c.tag + ':batch', 'batch', chapterBatch, P, "These come from the session reviews; the gate reads the result, so no re-review follows.")

  const G = 'Code gate'
  const lastTag = () => c.tag + "-s" + c.sessions.length
  let carry = [], openBehaviour = [], iterations = 0
  for (let iter = 1; iter <= 2; iter++) {
    iterations = iter
    const res = await gateLenses(c, iter, lastTag(), G)
    const all = res.flatMap((r) => r.findings)
    const list = [...openBehaviour, ...all.filter((f) => serious(f) || f.visible)]
    parked.push(...all.filter((f) => !(serious(f) || f.visible)))
    log(c.tag + ' gate ' + iter + ': ' + all.filter(serious).length + ' Critical/Important of ' + all.length)
    const behaviourFound = list.some((f) => serious(f) && isBehaviour(f))
    openBehaviour = []
    if (list.length) {
      await batch(c, c.tag + ':gate' + iter + ':batch', 'gate' + iter + '-batch', list, G)
      const rr = await rereview(c, c.tag + ':gate' + iter + ':rereview', 'gate' + iter + '-rereview', list, 'gate' + iter + '-batch', G)
      const open = rr.findings.filter(serious)
      parked.push(...rr.findings.filter((f) => !serious(f)))
      openBehaviour = open.filter(isBehaviour)
      carry.push(...open.filter((f) => !isBehaviour(f)))
    }
    // A second iteration only for a failure someone would see, unless the control lens has made it fixed.
    if (!(A.fixedSecondIteration || behaviourFound || openBehaviour.length)) break
  }
  need(!openBehaviour.length, c.tag + ':gate', 'a visible failure is still open after the second iteration: ' + ids(openBehaviour) + ' in ' + froms(openBehaviour) + '. Álvaro decides')
  const fz = await freeze(c, 'freeze', c.tag + ':freeze', parked, G)
  log(c.tag + ': session tags frozen at ' + fz.head)
  return { frozen: fz.head, carry, iterations, parked: parked.length }
}
const freeze = (c, name, label, parked, phaseName) => stepDone(c, label, name, c.tag + '-s' + c.sessions.length + ' ' + ids(parked), { model: 'sonnet', phase: phaseName },
  "Clerk's job in " + REPO + "; change no code and no tag. (1) Write " + c.V + "/freeze.txt: one line per tag " + c.tag + "-s*, in order: tag, commit, patch-id (git show <tag>^{commit} --format= | git patch-id --stable | cut -d' ' -f1). A patch-id survives re-parenting, so it says whether a session's content moved. Replace the file if it exists. (2) Check that every tag's commit is an ancestor of main and has no 'fixup!' above it, and that there are exactly " + c.sessions.length + " tags, the last " + c.tag + "-s" + c.sessions.length + "; otherwise BLOCKED.",
  parked.length ? "(3) Parked findings, which trigger nothing: append to " + c.parked + " one line per id (id, severity, where, claim, copied from its report), skipping any id already there: " + ids(parked) + ". Reports: " + froms(parked) + "." : "",
  "head is the commit of the last session tag.")

// ================= machine checks =================
const MACHINE_OK = (path) => (r, label) => {
  need(r && r.status === 'DONE', label, 'status ' + (r && r.status) + ': ' + JSON.stringify((r && r.concerns) || []))
  checkDone(path)(r, label)
}
const machine = (c, label, name, input, phaseName) => {
  const path = c.V + "/" + name + ".md"
  return stepDone(c, label, name, input, { model: 'sonnet', phase: phaseName, path, check: MACHINE_OK(path) },
    "Machine checks of chapter " + c.n + " in the worktree " + c.WT + ", at " + input + ": go build -o build/bin/bookx ./cmd/bookx; build/bin/bookx verify; build/bin/bookx web --all build/web; build/bin/bookx lint book/ch" + c.NN + "-*.md; build/bin/bookx doctest book/ch" + c.NN + "-*.md. Change nothing but build/, and remove build/ at the end. Paste each command's last lines and exit code in the report.",
    "status is DONE only if every exit code is 0. Any other exit is status BLOCKED, with the first problem of each in concerns. head is " + input + ".")
}
// Lint and doctest are not a reader: they get one writer's fix and one more run.
const machineGate = async (c, tag, head, phaseName) => {
  try { await machine(c, c.tag + ':machine:' + tag, 'machine-' + tag, head, phaseName); return head } catch (e) {
    if (!(e instanceof Stop) || !/^status/.test(e.why)) throw e
    log(c.tag + ': lint or doctest failed at ' + head + ', one fix: ' + e.why)
  }
  const fixed = await proseStep(c, c.tag + ':machine-fix:' + tag, 'machine-fix-' + tag, head, phaseName, "The machine checks failed: read " + c.V + "/machine-" + tag + ".md and make them pass. A word over the budget is paid by cutting trivia, never by moving text to a fold.")
  await machine(c, c.tag + ':machine2:' + tag, 'machine2-' + tag, fixed.head, phaseName)
  return fixed.head
}

// ================= prose =================
const PROSEBASE = (c) => L(
  "You maintain the Spanish prose of chapter " + c.n + " of the Ki book: book/ch" + c.NN + "-*.md on branch " + c.branch + " in the worktree " + c.WT + ". You own that worktree only; never run anything in the main checkout.",
  "Read " + PROC + "/prose.md (binding). The budget is 2 500 flow words (bookx words <file>): a fix does not grow the running text, and each thing it adds says what it takes out.",
  "Commit only the chapter file, and the wording of its cards in book/tests/ch" + c.NN + ".tests if a finding asks for it, as 'fixup! book: chapter " + c.NN + " prose'. Uncommitted changes in the worktree may come from an attempt that died: read them and keep what is right. Regenerate (do not commit) book/tests/ch" + c.NN + ".list and book/verified-tags.txt with ci/check-tags.sh --list-as " + c.tag + " <last session tag> and ci/check-tags.sh '" + c.tag + "-s*' so bookx verify passes; remove build/ at the end.")
const proseStep = (c, label, name, input, phaseName, ...task) => stepDone(c, label, name, input, { model: 'opus', phase: phaseName }, PROSEBASE(c), ...task, "head is the branch SHA after your commit. Say the word count in concerns.")
const draft = (c, frozenTag, carry) => stepDone(c, c.tag + ':draft', 'draft', frozenTag, { model: 'opus', phase: 'Draft', path: W + "/task-" + c.proseTask + "-report.md" },
  "Write Tarea " + c.proseTask + " of the Ki book: the Spanish prose of chapter " + c.n + ". Read " + PROC + "/prose.md first (binding: template, verification on the machine, «tú» with no gendered words). Budget: 2 500 flow words, whatever the hours. Brief: cd " + REPO + " && bash docs/superpowers/process/task-brief.sh " + PLAN + " " + c.proseTask + ".",
  "Create the worktree yourself, from the frozen code: git -C " + REPO + " worktree add " + c.WT + " -b " + c.branch + " " + frozenTag + "; if it exists, continue from what is there, committed or not. Work only there: another agent modifies the main checkout meanwhile.",
  "The code is at tags " + c.tag + "-s*, frozen after its gate: trust them and book/tests/" + c.tag + ".tests, not the plan's card ranges. " + PLANRULE,
  carry.length ? "These findings stayed open after the gate and no code will fix them: do not claim what they deny. Ids " + ids(carry) + " in " + froms(carry) + "." : "",
  "Failures that keep coming back: a construct, flag or test helper used before it is explained; a quoted message only one mutant produces, without naming it; a red phase whose stub panics and cuts the test binary; a claim about what a test catches broader than the test; plan jargon in reader-facing text. A claim about a mutant comes from running it with ci/mutate --apply. Do not re-teach what earlier chapters taught.",
  "Steps c.1 to c.4 of prose.md's section 2, lint and doctest included: every console block is compared. Commit only the chapter file as ONE commit 'book: chapter " + c.NN + " prose' on " + c.branch + "; generate (do not commit) book/tests/" + c.tag + ".list and the verified-tags lines; remove build/. Never touch main, never merge, tag or push. " + CAP,
  A.proseNote ? "Controller notes for this chapter: " + A.proseNote : "",
  "head is the SHA of that commit. Say the word count in concerns.")

// ================= blind pass =================
const BLIND = "You are blind: do NOT read anything under .superpowers/, any review, panel or fix report, or the writer's notes, and you do not know what any other reader finds. The only exceptions are the two files this prompt names: its result file, which you look for first and write last, and your own report."
const copyOf = (ref, lens, c) => "The text under review is frozen at " + ref + ". Copy it with mkdir -p <dir> && git -C " + REPO + " archive " + ref + " | tar -x -C <dir>, where <dir> is a NEW dir under " + SCRATCH + " named " + c.tag + "-" + lens + "; its book/ch" + c.NN + "-*.md is the chapter, and its book/tests/" + c.tag + ".tests are the cards. Listings: git -C " + REPO + " show " + c.tag + "-sK:<path>. Plan: Tarea " + c.proseTask + " of " + PLAN + "."
const readers = (c, ref, prefix, phaseName, withControl) => {
  const lastSession = c.tag + "-s" + c.sessions.length
  const list = [
    ['facts', 'opus', true, "FACTS. List every claim about Go, the toolchain, the runtime, Linux, compilers, type theory, parsing, Unicode, other languages and real projects, folds, solutions, answers and «Go en el trabajo» included, and check EVERY one: run it here on the Go that go.mod names, or find it in a primary source (the Go spec, go.dev, the source in GOROOT, the paper, the project's repository). Not a sample, not the likeliest ones. A false or overstated claim is Important. The ledger of every claim and how you checked it goes in your report."],
    ['whys', 'opus', true, "WHYS. Every explanation of why something works or is designed so, and every statement about what this chapter's code or tests do, in the prose, the cards (why, case, expect) and the comments the page shows: check that the reason given is the real reason by running what it depends on, ci/mutate --apply for a mutant and a small program for a behaviour. A wrong reason is Important even if the conclusion holds. A claim about what a test catches broader than the test is Important. All of them, not a sample."],
    ['apprentice', 'opus', false, "APPRENTICE. Read cold, in order, as someone who knows TS/JS, Python and Rust, has read the earlier chapters (book/ch*.md in your copy) and is new to everything else in Go and compilers. What is used before it is explained; what you could not follow; what reads as a list of facts; a helper with no file; an exercise you cannot start from its statement; a question that asks for a definition; Spanish (Spain), «tú», no gendered words about the reader. A concept used before it is explained, or a dense or tedious stretch (name its lines), is Important. Budgets and fold counts are the machine's: do not count them."],
    ['follower', 'sonnet', false, "FOLLOWER, from the reader's tree. In a second new dir, git -C " + REPO + " archive ch" + c.PP + " | tar -x, delete cmd/bookx, add nothing to PATH: that is the tree the reader has before this chapter. Do its sessions in order as the reader does: write what the steps and cards say, see each red, reach each green, run every command and compare every output as printed. Do every exercise from its statement, then check against its solution. At the end compare your tree with " + lastSession + ". A step that cannot be followed, a red or output that differs, an exercise that cannot be started or whose solution fails is Important. Doctest already compares the commands against the reference tree: what it cannot see is what the reader's own tree does."],
  ]
  if (withControl) list.push(['control', 'sonnet', false, "CONTROL CODE LENS, report only, on the frozen tags " + c.tag + "-s*: the whole chapter's code (git -C " + REPO + " diff " + c.tag + "-s1^.." + lastSession + "). Correctness bugs, wrong messages, fix-its and output, broken limits, overclaiming comments. All your findings have owner code. It measures what the gate let through."])
  return gather(list.map(([key, model, nits, p]) => {
    const name = prefix + key, label = c.tag + ':' + prefix.replace(/-$/, '') + ':' + key
    return [label, () => stepFind(c, label, name, ref, { lens: name, model, phase: phaseName, nits, codeOnly: key === 'control' },
      "A blind reader of chapter " + c.n + " of the Ki book (Spanish), one pass over the whole chapter on frozen text. There is no second round: what you miss, nobody else may see.", p, copyOf(ref, key, c), BLIND, "Severity: " + PROC + "/reviewer.md section 5 only. A finding in the chapter text or the wording of a card has owner prose; a defect that needs code or a test changed has owner code. " + READER)]
  }))
}

// ================= what happens to a finding =================
// A visible failure unfreezes, a test gap narrows the sentence, a Minor the reader sees travels before the tag.
const route = (all) => ({
  thaw: all.filter((f) => f.owner === 'code' && ((serious(f) && isBehaviour(f)) || (f.severity === 'Minor' && f.visible))),
  narrow: all.filter((f) => f.owner === 'code' && serious(f) && !isBehaviour(f)),
  prose: all.filter((f) => f.owner === 'prose' && (serious(f) || f.severity === 'Minor')),
  optional: all.filter((f) => f.severity === 'Nit' && f.owner === 'prose'),
  parked: all.filter((f) => !serious(f) && (f.owner === 'code' ? !(f.severity === 'Minor' && f.visible) : f.severity === 'Nit')),
})

const proseTrack = async (c, carry) => {
  const frozenTag = c.tag + "-s" + c.sessions.length
  const d = await draft(c, frozenTag, carry)
  const text = await machineGate(c, 'draft', d.head, 'Draft')
  log(c.tag + ': text frozen at ' + text)

  const pass = await readers(c, text, 'pass-', 'Blind pass', A.controlLens !== undefined ? A.controlLens : (c.n >= FIRST_STRICT && c.n <= LAST_CONTROL))
  const all = pass.flatMap((r) => r.findings)
  const passReports = pass.map((r) => r.report_path)
  log(c.tag + ' pass: ' + all.filter(serious).length + ' Critical/Important of ' + all.length)
  const controlVisible = all.filter((f) => f.id.startsWith('pass-control-') && isBehaviour(f) && serious(f)).length

  let r = route(all), head = text, parked = [...r.parked], errata = [...r.narrow], thaws = 0, turn = 1, deletedIds = []
  const fixTurn = async (t) => {
    let thawed = ''
    if (r.thaw.length) {
      thaws++
      need(thaws <= 2, c.tag + ':thaw', 'a visible failure is still open after two unfreezes: ' + ids(r.thaw) + ' in ' + froms(r.thaw))
      await batch(c, c.tag + ':thaw' + t + ':batch', 'thaw' + t + '-batch', r.thaw, 'Fix', "The session tags were frozen: this is an unfreeze before publishing, for failures the reader would see.")
      const rr = await rereview(c, c.tag + ':thaw' + t + ':rereview', 'thaw' + t + '-rereview', r.thaw, 'thaw' + t + '-batch', 'Fix')
      const open = rr.findings.filter((f) => serious(f) && isBehaviour(f))
      need(!open.length, c.tag + ':thaw', 'the unfreeze did not close ' + ids(open) + ' in ' + rr.report_path)
      await freeze(c, 'refreeze' + t, c.tag + ':refreeze' + t, [], 'Fix')
      thawed = "Code moved: read " + c.V + "/thaw" + t + "-batch.md, discard the regenerated book/verified-tags.txt and book/tests/" + c.tag + ".list, rebase " + c.branch + " onto the new " + frozenTag + " (git rebase --onto " + frozenTag + " <old base> " + c.branch + ") and make the prose exact again for what changed, and only that."
    }
    const required = [...r.prose, ...r.narrow]
    if (!required.length && !thawed) return
    const list = [...required, ...r.optional]
    const path = c.V + "/fix" + t + ".md"
    const fixed = await stepDone(c, c.tag + ':fix' + t, 'fix' + t, head + ' ' + ids([...list, ...r.thaw]), { model: 'opus', phase: 'Fix', path, schema: RESOLVED, dry: { resolutions: required.map((f) => ({ id: f.id, action: 'fixed', note: 'dry' })) }, check: checkResolved(path, required, ['fixed', 'narrowed', 'deleted', 'rejected']) },
      PROSEBASE(c),
      thawed,
      r.prose.length ? "Fix these prose findings, by id: " + ids(r.prose) + ". They are in: " + froms(r.prose) + "." : "",
      r.narrow.length ? "These are code findings no code will fix after the freeze: " + ids(r.narrow) + ", in " + froms(r.narrow) + ". Narrow or delete the sentence or card wording that claims what they deny, so the page says only what the frozen code does." : "",
      r.optional.length ? "If it costs no words, also: " + ids(r.optional) + ", in " + froms(r.optional) + "." : "",
      "An id you cannot find there means BLOCKED, not a guess. You may narrow or delete a claim. You may not add a claim you have not checked on the machine or in a primary source, and each thing you add says what it takes out. A rejection needs evidence in your report.",
      "Structured output also has resolutions: one per id, action fixed, narrowed, deleted or rejected, and a note. head is the branch SHA after your commit.")
    head = fixed.head
    head = await machineGate(c, 'fix' + t, head, 'Fix')
  }
  while (turn <= 2 && (r.thaw.length || r.prose.length || r.narrow.length)) {
    await fixTurn(turn)
    const chk = await stepFind(c, c.tag + ':check' + turn, 'check' + turn, head, { lens: 'check' + turn, model: 'opus', phase: 'Fix' },
      "Check of the diff of a fix to chapter " + c.n + " of the Ki book. Not a fresh reading: only git -C " + REPO + " diff " + text + " " + head + " -- book/ and what the fix report " + c.V + "/fix" + turn + ".md claims" + (thaws ? ", plus the unfreeze reports in " + c.V : "") + ".",
      "Findings it answers: " + (turn === 1 ? passReports.join(', ') : c.V + "/check1.md") + ". For each, by its resolution: fixed, narrowed or deleted as it says, or rejected with evidence that holds when you run it. Anything the diff adds that is unchecked or wrong, by running or reading a primary source. Severity: " + PROC + "/reviewer.md section 5. " + READER)
    r = route(chk.findings)
    parked.push(...r.parked)
    errata.push(...r.narrow)
    log(c.tag + ' check ' + turn + ': ' + chk.findings.filter(serious).length + ' Critical/Important of ' + chk.findings.length)
    turn++
  }
  // Two turns are the limit. What is still open is not left silently.
  const stillVisible = r.thaw.filter(serious)
  need(!stillVisible.length, c.tag + ':check', 'a visible failure is still open after 2 turns, so this chapter does not publish: ' + ids(stillVisible) + ' in ' + froms(stillVisible))
  const openSerious = [...r.prose, ...r.narrow].filter(serious)
  if (openSerious.length) {
    const path = c.V + "/delete.md"
    const del = await stepDone(c, c.tag + ':delete', 'delete', head + ' ' + ids(openSerious), { model: 'opus', phase: 'Fix', path, schema: RESOLVED, dry: { resolutions: openSerious.map((f) => ({ id: f.id, action: 'deleted', note: 'dry' })) }, check: checkResolved(path, openSerious, ['deleted', 'narrowed']) },
      PROSEBASE(c),
      "Two turns are over and these Importants are still open: " + ids(openSerious) + ", in " + froms(openSerious) + ". Delete each sentence or card wording they point at, or narrow it to what you verify now. Add nothing. Keep the page true and the steps followable: if deleting it breaks a step the reader needs, answer BLOCKED.",
      "Structured output also has resolutions: one per id, action deleted or narrowed, and a note. head is the branch SHA after your commit.")
    head = await machineGate(c, 'delete', del.head, 'Fix')
    deletedIds = openSerious.map((f) => f.id)
  }
  parked.push(...r.thaw, ...r.prose.filter((f) => !serious(f)), ...r.optional)

  await stepDone(c, c.tag + ':ledger', 'ledger', head + ' ' + ids(parked), { model: 'sonnet', phase: 'Integrate' },
    "Clerk's job; change no code and no prose. Append to " + c.parked + " one line per id (id, severity, where, claim, copied from its report; skip an id already there): " + (ids(uniq(parked)) || "none") + ". Reports are in " + c.V + ".",
    errata.length ? "Also add, under «Erratas hacia delante», these code findings the prose was narrowed around, for the next session's {{diff}} or for the part gate: " + ids(errata) + "." : "",
    "head is " + head + ".")

  const integ = await onMain(() => integrate(c))
  const v = await stepFind(c, c.tag + ':verify', 'verify', integ.head, { lens: 'verify', model: 'sonnet', phase: 'Integrate' },
    "Verify the integrated chapter " + c.n + " of the Ki book in " + REPO + ". In a git-archive copy of tag " + c.tag + " go build -o bookx ./cmd/bookx, then bookx verify --repo " + REPO + " and bookx web --all <dir> --repo " + REPO + ": every {{…}} directive resolved, the rendered cards match book/tests/" + c.tag + ".tests at that tag. bookx lint and bookx doctest of the chapter exit 0. Also bookx verify at main HEAD, read-only. Compare the chapter at tag " + c.tag + " with the text at " + head + ": the same. No worktree left for this chapter and build/tags empty. Findings have owner code unless they are in the chapter's text. " + READER)
  const bad = v.findings.filter(serious)
  need(!bad.length, c.tag + ':verify', 'integrated but not verified, so do not publish: ' + ids(bad) + ' in ' + v.report_path)
  return { text, head, integrated: integ.head, counts: { pass: all.length, passImportant: all.filter(serious).length, thaws, turns: turn - 1, deleted: deletedIds, parked: uniq(parked.map((f) => f.id)).length, controlVisible, errata: errata.map((f) => f.id) } }
}

const integrate = (c) => stepDone(c, c.tag + ':integrate', 'integrate', c.tag, { model: 'sonnet', phase: 'Integrate' },
  "You own the main checkout of " + REPO + " for this job: integrate chapter " + c.n + " into main. Read " + PROC + "/implementer.md first. Do the steps in order; each may already be done by an attempt that died, so look first.",
  "(0) Interrupted state. A rebase open: git rebase --abort. HEAD detached: main has not moved, since only step 4 moves it, so git restore book/verified-tags.txt book/tests/" + c.tag + ".list if they differ, git switch main, and delete tag " + c.tag + " if it exists and is not on main. If tag " + c.tag + " is on main and 'book: verify " + c.tag + "' is on main, go to step 5.",
  "(1) Frozen. For each line of " + c.V + "/freeze.txt the patch-id of the tag now must be the one written. A difference: BLOCKED, naming the tag; only a range-diff that shows nothing but changed context lets you go on, and say so.",
  "(2) In " + c.WT + ": discard the uncommitted book/verified-tags.txt and book/tests/" + c.tag + ".list, then squash " + c.branch + " into ONE commit 'book: chapter " + c.NN + " prose' (git -c sequence.editor=: rebase -i --autosquash <its base>). It may touch only book/ch" + c.NN + "-*.md and the wording of book/tests/" + c.tag + ".tests.",
  "(3) The insertion point I is the last session commit " + c.tag + "-s" + c.sessions.length + "^{commit}, or, if 'book: verify ch" + c.PP + "' lies above it on main, that commit. git checkout --detach I. ci/check-tags.sh --list-as " + c.tag + " " + c.tag + "-s" + c.sessions.length + " and ci/check-tags.sh '" + c.tag + "-s*'; commit book/tests/" + c.tag + ".list and book/verified-tags.txt as 'book: verify " + c.tag + " sessions'. Cherry-pick the squashed prose commit. Tag it annotated '" + c.tag + "' with the message the plan gives in Tarea " + c.proseTask + " (or '" + c.tagFallback + "' if none). ci/check-tags.sh " + c.tag + "; commit book/verified-tags.txt as 'book: verify " + c.tag + "'. Call that commit S.",
  "(4) git rebase --onto S I main. Then ci/retag-all.sh: " + c.tag + "-s* must not move; session tags of a later chapter may. So S stays below any unfinished chapter, and only S is ever pushed.",
  "(5) ci/ci-local.sh at HEAD ends in 'verify: OK'; go build -o build/bin/bookx ./cmd/bookx && build/bin/bookx verify.",
  "(6) If the worktree still exists: the prose commit equals the cherry-picked one (git diff), then git worktree remove " + c.WT + " and git branch -D " + c.branch + ". git worktree list holds nothing of this chapter.",
  "Never push or stash; Álvaro pushes finished chapters only. head is S, the full SHA of 'book: verify " + c.tag + "'. Say the final SHAs of " + c.tag + " and of any later chapter's session tag in the report.")

// ================= the run =================
const guard = async (fn) => {
  try { return await fn() } catch (e) {
    if (!(e instanceof Stop)) throw e
    log('STOPPED ' + e.message)
    return { stopped: e.where, reason: e.why }
  }
}
const argsWrong = argError()
if (argsWrong) return finish({ stopped: 'args', reason: argsWrong })
const C = chapter(A)

if (A.mode === 'second-pass') {
  // After publishing: a second blind pass that only reports. It starts no fix, and what it finds goes to the part gate.
  const r = await guard(async () => {
    const res = await readers(C, C.tag, 'second-', 'Second pass', false)
    const all = res.flatMap((x) => x.findings)
    await stepDone(C, C.tag + ':second:ledger', 'second-ledger', ids(all), { model: 'sonnet', phase: 'Second pass' },
      "Clerk's job; change no code and no prose. Append to " + C.parked + " under «Segunda pasada de " + C.tag + "» one line per id (id, severity, where, claim, copied from its report; skip an id already there): " + (ids(all) || "none") + ". Reports are in " + C.V + ". head is " + C.tag + ".")
    return { important: all.filter(serious).map((f) => f.id), total: all.length, reports: res.map((x) => x.report_path) }
  })
  return finish(r.stopped ? r : { status: 'second-pass-done', n: A.n, ...r, note: 'Report only: the part gate reads them from the parked file.' })
}

const nextC = A.next ? chapter({ part: A.part, ...A.next }) : null
const code = await guard(() => codeTrack(C))
if (code.stopped) return finish(code)
const [prose, nextCode] = await parallel([
  () => guard(() => proseTrack(C, code.carry)),
  () => (nextC ? guard(() => codeTrack(nextC)) : Promise.resolve({ skipped: true })),
])
if (!prose) return finish({ stopped: C.tag + ':prose', reason: 'the prose track died and left no result', next: nextCode })
if (prose.stopped) return finish({ ...prose, next: nextCode })
if (!nextCode) return finish({ stopped: nextC.tag + ':code', reason: 'the next chapter code track died and left no result', chapter: { n: A.n, ...prose } })
const secondPassDue = A.secondPass !== undefined ? A.secondPass : ((A.n >= FIRST_STRICT && A.n <= LAST_CONTROL) || A.newKind === true)
return finish({
  status: 'ready-to-publish',
  n: A.n,
  code: { iterations: code.iterations, parked: code.parked, carried: code.carry.map((f) => f.id) },
  ...prose,
  next: nextCode,
  ask_verdict: A.n === FIRST_STRICT || A.newKind === true,
  second_pass_due: secondPassDue,
  push: "git -C " + REPO + " push origin " + prose.integrated + ":refs/heads/main refs/tags/" + C.tag + " 'refs/tags/" + C.tag + "-s*:refs/tags/" + C.tag + "-s*'",
})
