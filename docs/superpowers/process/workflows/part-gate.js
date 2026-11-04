export const meta = {
  name: 'ki-part-gate',
  description: 'Ki book, the gate at the end of a Part, after publishing: blind theory, consistency, whole-branch review on the strongest model with hostile programs, clean clone, reduced practice; a memory death or broken limit blocks until clean (at most 3 turns), the rest gets one forward fix and errata. Replaces the triple check. See docs/superpowers/process/pipeline.md',
  phases: [{ title: 'Readers' }, { title: 'Blocking' }, { title: 'Fix' }, { title: 'Close' }],
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

const MAX_BLOCK_TURNS = 3
const argError = () => {
  if (typeof A.part !== 'string' || !A.part) return 'part must be a name such as II'
  if (typeof A.baseTag !== 'string' || !A.baseTag) return 'baseTag is the tag where this Part starts, such as m1'
  if (!Array.isArray(A.chapters) || !A.chapters.length) return 'chapters must list the chapters of the Part'
  for (const x of A.chapters) if (!Number.isInteger(x.n) || typeof x.file !== 'string' || typeof x.prev !== 'string') return 'each chapter needs n, file (book/…) and prev (the tag before it)'
  if (A.milestone && !(A.milestone.tag && A.milestone.chapter && A.milestone.message)) return 'milestone needs tag, chapter and message'
  if (A.practice !== undefined && !Array.isArray(A.practice)) return 'practice must list chapter numbers'
  return null
}
const WHY = "This gate runs after the chapters were published, and it is the only reading of the Part as a whole. You are one independent check; you have not seen what the others find, and you are blind: do NOT read anything under .superpowers/, nor any review, panel or fix report. The only file you write there is your own report."
const blocking = (f) => f.kind === 'memory-death' || f.kind === 'limit'
const TAGS = "Published tags never move. Fixes are forward commits on main, no history rewrite. If a fix changes what a chapter describes at its tag, the chapter gets one sentence of erratum, within its budget, and the file " + W + "/errata-part-" + A.part + ".md gets a line for the next session's {{diff}}."

const gate = async () => {
  const c = { V: W + "/v4-part-" + A.part, part: A.part }
  const stage = await stepDone(c, 'gate:snapshot', 'snapshot', A.baseTag, { model: 'sonnet', phase: 'Readers' },
    "Clerk's job in " + REPO + "; change nothing. head is git rev-parse main. BLOCKED if git worktree list shows anything but the main checkout, or if main holds a session tag of a chapter with no chapter tag: this gate reads finished chapters only, and it owns the main checkout.")
  const ref = stage.head
  const copy = (name) => "Copy the book as published: mkdir -p <dir> && git -C " + REPO + " archive " + ref + " | tar -x -C <dir>, with <dir> a NEW dir under " + SCRATCH + " named part-" + name + "."
  const first = A.chapters[0].n, lastN = A.chapters[A.chapters.length - 1].n
  const practice = A.practice !== undefined ? A.practice : uniq([first, lastN])
  const chapterOf = (n) => A.chapters.find((x) => x.n === n)
  const list = [
    ...A.chapters.map((x) => ['theory-ch' + pad(x.n), 'opus', true,
      "BLIND THEORY, chapter " + x.n + " (" + x.file + ", folds, solutions and answers included). List every claim about Go, the toolchain, the runtime, Linux, compilers, type theory, parsing, Unicode, other languages or real projects, every explanation of why something works, and check each one: run it on the Go that go.mod names, or find it in a primary source (the Go spec, go.dev, the source in GOROOT, the paper, the project's repository). A false or overstated claim is Important; a wrong explanation of why something works is Important even if the conclusion holds. " + copy('theory-ch' + pad(x.n))]),
    ['consistency', 'opus', true,
      "CONSISTENCY across the chapters of Part " + A.part + " (" + A.chapters.map((x) => x.file).join(', ') + ", with the earlier chapters where these refer back). Read them in order, as one book: a construct used before any chapter explains it; a reference to a chapter, section, exercise or card that no longer resolves, or a promise («el capítulo N explica…») that chapter does not keep; the same thing named or explained two ways, or two chapters that contradict each other; a claim about the language or compiler that disagrees with docs/superpowers/specs/2026-10-24-ki-design-go.md or the code at the tags; a term never defined. " + copy('consistency')],
    ['branch', 'fable', false,
      "WHOLE-BRANCH REVIEW of Part " + A.part + ", from " + A.baseTag + " to " + ref + ", every package touched, with hostile programs. Read docs/superpowers/process/reviewer.md and the spec. Sessions and chapters were reviewed one at a time, so look for what shows only across them: an interface two passes understand differently, a promise of the spec the code does not keep, invariants that hold in one pass and are assumed false in the next, dead or duplicated code, error paths that lose information, tests that pass for the wrong reason, what will hurt the next Part. Then attack it: write Ki programs and inputs that try to break every limit the spec or the book promises (stacks, environments, nesting, input size, invalid UTF-8, huge literals, runaway recursion and allocation), each run under the memory cap. A process killed by the cap is a finding of kind memory-death, a promised limit that does not hold is kind limit, and either is Critical or Important. Also GOOS=windows, linux/arm64 and -race. " + copy('branch')],
    ['clone', 'sonnet', false,
      "THE PART FROM A CLEAN CLONE. git clone " + REPO + " into a NEW dir under " + SCRATCH + " (delete it afterwards), at " + ref + ". go test ./..., go vet ./..., gofmt -l, ci/ci-local.sh; build ki and run what the milestones of this Part promise; every tag ch* from " + A.baseTag + " on builds and passes with ci/check-tags.sh in the clone; bookx doctest of each chapter of the Part exits 0. Write your own small programs that use every feature the Part claims and compare with what the book says each does: exit codes, stdout against stderr, messages. A program killed by the memory cap is kind memory-death, a promised limit that fails is kind limit."],
    ...practice.map((n) => {
      const x = chapterOf(n)
      if (!x) throw new Stop('gate:practice', 'chapter ' + n + ' is not in chapters')
      return ['practice-ch' + pad(n), 'sonnet', false,
        "REDUCED PRACTICE, chapter " + n + ", a sample of the Part. Do the chapter for real as its reader, from a clean copy of " + x.prev + " with cmd/bookx deleted and nothing added to PATH: follow every session's steps in order, write what the steps and cards say, see the reds, reach the greens, run every command and compare every output. Do every exercise from its statement, then check against its solution. A step that cannot be followed, a red or output that differs, an exercise that cannot be started or whose solution fails is Important. Chapter text: " + x.file + " at " + ref + "."]
    }),
  ]
  const res = await gather(list.map(([key, model, nits, p]) => ['gate:' + key, () => stepFind(c, 'gate:' + key, key, ref, { lens: key, model, nits, phase: 'Readers' },
    WHY, p, "Severity: " + PROC + "/reviewer.md section 5. A finding in a chapter's text has owner prose; a defect that needs code or a test changed has owner code. " + READER)]))
  const all = res.flatMap((r) => r.findings)
  const reports = res.map((r) => r.report_path)
  log('Part ' + A.part + ' gate: ' + all.filter(serious).length + ' Critical/Important of ' + all.length + ', ' + all.filter(blocking).length + ' blocking')

  // A memory death or a broken limit blocks the close until it is clean.
  let blockTurn = 0
  const blockLoop = async (found) => {
    let open = found
    while (open.length) {
      blockTurn++
      if (blockTurn > MAX_BLOCK_TURNS) throw new Stop('gate:blocking', 'still open after ' + MAX_BLOCK_TURNS + ' turns, so the Part does not close and this goes to Álvaro: ' + ids(open) + ' in ' + froms(open))
      const path = c.V + "/block-" + blockTurn + ".md"
      await stepDone(c, 'gate:block' + blockTurn + ':fix', 'block-' + blockTurn, ids(open), { model: 'opus', phase: 'Blocking', schema: RESOLVED, dry: { resolutions: open.map((f) => ({ id: f.id, action: 'fixed', note: 'dry' })) }, check: checkResolved(path, open, ['fixed']) },
        "You own the main checkout of " + REPO + " for this job: fix these failures, a memory death or a broken promised limit, in code, TDD. They block the close of the Part. Ids: " + ids(open) + ", in " + froms(open) + ". Read " + PROC + "/implementer.md. " + TAGS + " Every new check kills its mutant. ci/ci-local.sh at HEAD 'verify: OK'; bookx verify. " + CAP,
        "Structured output also has resolutions: one per id, action fixed, and a note.")
      const rec = await stepFind(c, 'gate:block' + blockTurn + ':recheck', 'block-' + blockTurn + '-recheck', ids(open), { lens: 'block' + blockTurn, model: 'fable', phase: 'Blocking' },
        "Re-check the fixes of " + c.V + "/block-" + blockTurn + ".md in " + REPO + " at main HEAD, by id: " + ids(open) + ", in " + froms(open) + ". Run each hostile program again, under the cap, and try the neighbouring ways to break the same limit. Report what is still killed or still broken, kind memory-death or limit, and anything else the fixes broke. Copy with git -C " + REPO + " archive main | tar -x -C <dir>, a NEW dir under " + SCRATCH + ". " + READER)
      open = rec.findings.filter(blocking)
      all.push(...rec.findings.filter((f) => !blocking(f)))
    }
  }
  await blockLoop(all.filter(blocking))

  // Everything else: one fix, no round. No rate of findings ever adds one.
  const rest = all.filter((f) => !blocking(f))
  const codeF = rest.filter((f) => f.owner === 'code' && (serious(f) || f.visible))
  const proseF = rest.filter((f) => f.owner === 'prose' && (serious(f) || f.severity === 'Minor'))
  const parkedFile = W + "/parked-part-" + A.part + ".md"
  const codePath = c.V + "/fix-code.md"
  await stepDone(c, 'gate:fix:code', 'fix-code', ids(codeF), { model: 'opus', phase: 'Fix', schema: RESOLVED, dry: { resolutions: codeF.map((f) => ({ id: f.id, action: 'fixed', note: 'dry' })) }, check: checkResolved(codePath, codeF.filter(serious), ['fixed', 'rejected']) },
    "You own the main checkout of " + REPO + " for this job: the one code fix of the Part gate. Read " + PROC + "/implementer.md. " + TAGS,
    codeF.length ? "Findings, by id: " + ids(codeF) + ", in " + froms(codeF) + "." : "No finding of this gate needs code.",
    "Also burn the parked list, in one forward commit of its own: " + parkedFile + " holds what the chapters parked and the «Erratas hacia delante» they left. Each entry is fixed or rejected with a reason, and the file says which; if the file does not exist, there is nothing to burn.",
    "TDD; every new check kills its mutant; cards are read from each chapter's tag, so do not edit old cards to describe new behaviour. Decide, never leave a question. A rejection needs evidence in the report. ci/ci-local.sh at HEAD 'verify: OK'; bookx verify. " + CAP,
    "Structured output also has resolutions: one per id given here, action fixed or rejected, and a note.")
  const prosePath = c.V + "/fix-prose.md"
  const proseFix = await stepDone(c, 'gate:fix:prose', 'fix-prose', ids(proseF), { model: 'opus', phase: 'Fix', schema: RESOLVED, dry: { resolutions: proseF.map((f) => ({ id: f.id, action: 'fixed', note: 'dry' })) }, check: checkResolved(prosePath, proseF.filter(serious), ['fixed', 'narrowed', 'deleted', 'rejected']) },
    "You own the main checkout of " + REPO + " for this job: the one prose fix of the Part gate. The chapters are published and prose is corrected at once: one forward commit per chapter, 'book: chapter NN, part gate'. Read " + PROC + "/prose.md: at most 2 500 flow words, pay for each addition with a cut of trivia in the same chapter, never by moving text to a fold; bookx lint and bookx doctest stay clean.",
    proseF.length ? "Findings, by id: " + ids(proseF) + ", in " + froms(proseF) + "." : "No prose finding: only what the code fix " + codePath + " says a chapter must now state, in one sentence of erratum each.",
    "You may narrow or delete a claim. You may not add one you have not checked. A rejection needs evidence in the report. A card's wording lives in its chapter tag, which does not move: correct it with a sentence of erratum in the chapter. bookx verify OK. Never push or stash.",
    "Structured output also has resolutions: one per id given here, action fixed, narrowed, deleted or rejected, and a note. head is main after your commits.")
  const re = await stepFind(c, 'gate:reverify', 'reverify', proseFix.head, { lens: 'reverify', model: 'opus', phase: 'Fix' },
    "Re-verify the one fix of the Part gate in " + REPO + ". Not a fresh reading: the diff " + ref + "..main, the reports " + codePath + " and " + prosePath + ", the findings in " + reports.join(', ') + ". For each: fixed or soundly rejected, by running the command or reading the primary source again. Anything the fixes introduced. Report only what is still wrong or newly wrong. Copy with git -C " + REPO + " archive main | tar -x -C <dir>, a NEW dir under " + SCRATCH + ". " + READER)
  await blockLoop(re.findings.filter(blocking))
  const openProse = re.findings.filter((f) => !blocking(f) && f.owner === 'prose' && serious(f))
  const openCode = re.findings.filter((f) => !blocking(f) && f.owner === 'code' && serious(f))
  if (openProse.length) {
    const path = c.V + "/delete.md"
    await stepDone(c, 'gate:delete', 'delete', ids(openProse), { model: 'opus', phase: 'Fix', schema: RESOLVED, dry: { resolutions: openProse.map((f) => ({ id: f.id, action: 'deleted', note: 'dry' })) }, check: checkResolved(path, openProse, ['deleted', 'narrowed']) },
      "You own the main checkout of " + REPO + " for this job. These Importants are still open after the gate's one fix: " + ids(openProse) + ", in " + froms(openProse) + ". Delete each sentence they point at, or narrow it to what you verify now, in one forward commit per chapter. Add nothing. bookx verify OK. Never push or stash.",
      "Structured output also has resolutions: one per id, action deleted or narrowed, and a note.")
  }
  const parked = [...rest, ...re.findings].filter((f) => !serious(f) && !(f.owner === 'code' && f.visible) && !(f.owner === 'prose' && f.severity === 'Minor'))
  const milestone = A.milestone
  const close = await onMain(() => stepDone(c, 'gate:close', 'close', ref + ' ' + ids(openCode), { model: 'sonnet', phase: 'Close' },
    "You own the main checkout of " + REPO + " for this job: close Part " + A.part + ".",
    milestone ? "Create " + milestone.tag + " on " + milestone.chapter + "^{commit} with the message '" + milestone.message + "', if it does not exist. ci/retag-all.sh must know the milestone: add it to its table if it does not, in a commit of its own." : "",
    "ci/check-tags.sh 'ch*'" + (milestone ? " " + milestone.tag : "") + "; a tag that does not pass is BLOCKED, it never moves. git add book/verified-tags.txt book/tests/*.list and commit 'book: verify Part " + A.part + " tags' if they changed. Compare the hours in the spec's index with what was built, and if they changed update the spec in a commit of its own. ci/ci-local.sh at HEAD 'verify: OK'; bookx verify; git worktree list shows only main; build/tags empty.",
    "Append to " + parkedFile + " what stays: " + (ids(parked) || "nothing") + " (reports in " + c.V + "), and under «Erratas hacia delante», for the next session: " + (ids(openCode) || "nothing") + ". Those are Importants nobody may lose: each one goes in the report, and the next chapter's session answers it.",
    CAP + " head is main after your commits.")
    )
  const perChapter = all.filter(serious).length / A.chapters.length
  return {
    status: 'closed',
    part: A.part,
    counts: { total: all.length, important: all.filter(serious).length, perChapter, blockingTurns: blockTurn, deleted: openProse.map((f) => f.id), forward: openCode.map((f) => f.id), parked: parked.length },
    advice: perChapter > 2 ? 'Over 2 Importants per chapter: add a third truth reader to the blind pass, never a round.' : '',
    head: close.head,
    republish: 'Prose moved after publishing: rebuild with bookx web and republish; then Álvaro pushes.',
    push: "git -C " + REPO + " push origin main --tags",
  }
}
const argsWrong = argError()
if (argsWrong) return finish({ stopped: 'args', reason: argsWrong })
try {
  return finish(await gate())
} catch (e) {
  if (!(e instanceof Stop)) throw e
  log('STOPPED ' + e.message)
  return finish({ stopped: e.where, reason: e.why, needs_alvaro: e.where === 'gate:blocking' })
}
