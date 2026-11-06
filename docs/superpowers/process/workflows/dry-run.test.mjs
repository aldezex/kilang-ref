// Control-flow tests of chapter-v4.js and part-gate.js, with no agents and no tokens:
//   node --test docs/superpowers/process/workflows/dry-run.test.mjs
// The scripts run as the Workflow runtime runs them: an async body with agent(),
// parallel(), phase(), log() and args as globals. They get args.dryRun, so each
// agent's prompt is a stub holding its canned answer. The world below plays the
// agents: it can kill one, crash at one, or keep the result files between two runs.
import { test } from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'

const here = dirname(fileURLToPath(import.meta.url))
const source = (name) => readFileSync(join(here, name), 'utf8')
const AsyncFunction = (async function () {}).constructor
const compile = (name) => new AsyncFunction('agent', 'parallel', 'pipeline', 'phase', 'log', 'args', 'budget', 'workflow', 'Date', 'Math',
  source(name).replace(/^export const meta\s*=/m, 'const meta ='))

const forbidden = (what) => { throw new Error(what + ' throws in the Workflow runtime') }
const FakeDate = new Proxy(function () {}, {
  construct: (_, a) => (a.length ? new Date(...a) : forbidden('new Date()')),
  get: (_, k) => (k === 'now' ? () => forbidden('Date.now()') : Date[k]),
})
const FakeMath = new Proxy(Math, { get: (t, k) => (k === 'random' ? () => forbidden('Math.random()') : t[k]) })

const MAIN_OWNER = /:(impl:|batch$|integrate$|plan$)|:gate\d:batch|:thaw\d:batch/

// kills: { label: 'null' | 'empty' | 'throw' | {custom result} }
const world = ({ kills = {}, crashAt = null, store = new Map() } = {}) => {
  const w = { called: [], worked: [], replayed: [], maxMain: 0, logs: [], store }
  let onMain = 0
  w.agent = async (prompt, o) => {
    w.called.push(o.label)
    assert.match(prompt, /^DRY RUN/, 'a dry run sends only stub prompts: ' + o.label)
    assert.ok(o.schema && o.schema.type === 'object', 'no schema on ' + o.label)
    const isMain = MAIN_OWNER.test(o.label)
    if (isMain) { onMain++; w.maxMain = Math.max(w.maxMain, onMain) }
    // An owner of the checkout stays long enough for any other track to arrive.
    for (let i = 0; i < (isMain ? 40 : 2); i++) await new Promise((r) => setImmediate(r))
    if (isMain) onMain--
    const lines = prompt.split('\n')
    const file = lines[1].replace('RESULT_FILE: ', '')
    if (store.has(file)) { w.replayed.push(o.label); return store.get(file) }
    if (o.label === crashAt) return null
    if (Object.hasOwn(kills, o.label)) {
      const k = kills[o.label]
      if (k === 'null') return null
      if (k === 'empty') return ''
      if (k === 'throw') throw new Error('killed')
      return k
    }
    const answer = JSON.parse(lines.slice(3).join('\n'))
    w.worked.push(o.label)
    if (/^DONE/.test(answer.status || 'DONE')) store.set(file, answer)
    return answer
  }
  return w
}
const parallel = (thunks) => Promise.all(thunks.map(async (t) => { try { return await t() } catch { return null } }))
const exec = async (name, args, w) => {
  const r = await compile(name)(w.agent, parallel, () => forbidden('pipeline'), () => {}, (m) => w.logs.push(m), args, {}, () => forbidden('workflow'), FakeDate, FakeMath)
  w.result = r
  return r
}

const W = '/home/scami/kilang-ref/.superpowers/sdd/2026-10-24-ki-plan-01-part1'
const V = W + '/v4-ch11'
const base = (extra = {}, answers = {}) => ({ n: 11, part: 'II', proseTask: '60', sessions: ['50', '51'], diagnostics: true, tagFallback: 'ch11: x', controlLens: false, ...extra, dryRun: { answers } })
const F = (id, severity, owner, kind, visible, where = owner === 'prose' ? 'book/ch11-x.md:3' : 'internal/x/y.go:7') => ({ id, severity, owner, kind, visible, falsehood: false, where, claim: 'SECRET-CLAIM ' + id, evidence: 'SECRET-EVIDENCE' })
const LIE = (id, severity, owner, kind, visible, where) => ({ ...F(id, severity, owner, kind, visible, where), falsehood: true })
const finds = (...fs) => ({ findings: fs })
const chapter = (extra, answers, opts) => { const w = world(opts); return exec('chapter-v4.js', base(extra, answers), w).then((r) => ({ w, r })) }
const after = (w, label) => w.called.slice(w.called.indexOf(label) + 1)
const LATER = ['ch11:fix1', 'ch11:check1', 'ch11:ledger', 'ch11:integrate', 'ch11:verify']
const none = (w, labels) => { for (const l of labels) assert.ok(!w.called.includes(l), l + ' must not run; ran: ' + w.called.join(' ')) }

const HAPPY = [
  'ch11:impl:T50', 'ch11:review:T50', 'ch11:impl:T51', 'ch11:review:T51',
  'ch11:gate1:code', 'ch11:gate1:truth', 'ch11:gate1:hunt', 'ch11:freeze',
  'ch11:draft', 'ch11:machine:draft',
  'ch11:pass:facts', 'ch11:pass:whys', 'ch11:pass:cards', 'ch11:pass:apprentice', 'ch11:pass:follower',
  'ch11:ledger', 'ch11:integrate', 'ch11:verify',
]

test('a clean chapter runs each step once, in order, and ends ready to publish', async () => {
  const { w, r } = await chapter()
  assert.deepEqual([...w.called].sort(), [...HAPPY].sort())
  for (const [a, b] of [['ch11:impl:T50', 'ch11:impl:T51'], ['ch11:freeze', 'ch11:draft'], ['ch11:machine:draft', 'ch11:pass:facts'], ['ch11:pass:follower', 'ch11:ledger'], ['ch11:ledger', 'ch11:integrate'], ['ch11:integrate', 'ch11:verify']]) {
    assert.ok(w.called.indexOf(a) < w.called.indexOf(b), a + ' before ' + b)
  }
  assert.equal(r.status, 'ready-to-publish')
  assert.equal(r.ask_verdict, true)
  assert.equal(r.second_pass_due, true)
  assert.match(r.push, /sha-integrate:refs\/heads\/main refs\/tags\/ch11 /)
  assert.ok(!/--force/.test(r.push))
})

test('killing a lens mid-round stops the chapter; the other lenses finish; nothing after them runs', async () => {
  for (const lens of ['facts', 'whys', 'cards', 'apprentice', 'follower']) {
    for (const how of ['null', 'empty', 'throw', {}]) {
      const label = 'ch11:pass:' + lens
      const { w, r } = await chapter({}, {}, { kills: { [label]: how } })
      assert.ok(r.stopped, 'must stop for ' + label + ' ' + JSON.stringify(how))
      assert.match(r.stopped, new RegExp(label))
      assert.match(r.reason, new RegExp(lens))
      assert.ok(w.called.includes('ch11:pass:facts') && w.called.includes('ch11:pass:follower'))
      none(w, LATER)
      assert.ok(!r.status)
    }
  }
})

test('a dead agent anywhere stops the chapter and the steps after it never run', async () => {
  for (const label of HAPPY.concat(['ch11:machine:draft'])) {
    const { w, r } = await chapter({}, {}, { kills: { [label]: 'null' } })
    assert.ok(r.stopped, label + ' died and the chapter went on')
    assert.ok(!r.status, label)
    assert.match(r.reason, /died|no result/, label)
    if (label === 'ch11:verify') continue
    none(w, ['ch11:verify'])
    if (label !== 'ch11:integrate') none(w, ['ch11:integrate'])
  }
})

test('a bad result is a stop too: owner, kind, Nit, emptiness, report path', async () => {
  const lens = 'ch11:pass:facts'
  const bad = {
    'owner missing': finds({ ...F('pass-facts-1', 'Minor', 'prose', 'text', true), owner: undefined }),
    'owner invented': finds({ ...F('pass-facts-1', 'Minor', 'prose', 'text', true), owner: 'both' }),
    'owner invented, the rest of a code finding': finds({ ...F('pass-facts-1', 'Important', 'code', 'output', true), owner: 'both' }),
    'owner missing, the rest of a code finding': finds({ ...F('pass-facts-1', 'Important', 'code', 'output', true), owner: undefined }),
    'prose pointing at code': finds(F('pass-facts-1', 'Important', 'prose', 'text', true, 'internal/x/y.go:1')),
    'code pointing at the text': finds(F('pass-facts-1', 'Important', 'code', 'output', true, 'book/ch11-x.md:1')),
    'code of kind text': finds(F('pass-facts-1', 'Important', 'code', 'text', true)),
    'prose not visible': finds(F('pass-facts-1', 'Minor', 'prose', 'text', false)),
    'id of another lens': finds(F('pass-whys-1', 'Important', 'prose', 'text', true)),
    'duplicate id': finds(F('pass-facts-1', 'Minor', 'prose', 'text', true), F('pass-facts-1', 'Minor', 'prose', 'text', true)),
    'no evidence': finds({ ...F('pass-facts-1', 'Important', 'prose', 'text', true), evidence: ' ' }),
    'falsehood missing': finds({ ...F('pass-facts-1', 'Minor', 'prose', 'text', true), falsehood: undefined }),
    'falsehood not a boolean': finds({ ...F('pass-facts-1', 'Minor', 'prose', 'text', true), falsehood: 'yes' }),
    'checked nothing': { findings: [], checked: 0 },
    'no findings': { findings: undefined },
    'wrong report': { report_path: '/somewhere/else.md' },
  }
  for (const [why, answer] of Object.entries(bad)) {
    const { w, r } = await chapter({}, { [lens]: answer })
    assert.ok(r.stopped, why + ' passed')
    none(w, LATER)
  }
  const nit = await chapter({}, { 'ch11:pass:apprentice': finds(F('pass-apprentice-1', 'Nit', 'prose', 'text', true)) })
  assert.ok(!nit.r.stopped, 'another reader\'s Nit is kept as a Minor, not a stop')
  const ok = await chapter({}, { [lens]: finds(F('pass-facts-1', 'Nit', 'prose', 'text', true)) })
  assert.equal(ok.r.status, 'ready-to-publish')
  assert.ok(!ok.w.called.includes('ch11:fix1'), 'a Nit starts nothing')
})

test('a session that is not DONE stops everything after it', async () => {
  const { w, r } = await chapter({}, { 'ch11:impl:T50': { status: 'BLOCKED', concerns: ['the plan contradicts the spec'] } })
  assert.equal(r.stopped, 'ch11:impl:T50')
  assert.match(r.reason, /the plan contradicts the spec/)
  none(w, ['ch11:impl:T51', 'ch11:freeze', 'ch11:draft'])
})

test('the cards reader goes card by card, runs or extrapolates every absolute, and checks promises against the plan', async () => {
  const { r } = await chapter()
  const t = r.trace.find((x) => x.label === 'ch11:pass:cards')
  assert.equal(t.model, 'opus')
  assert.equal(t.phase, 'Blind pass')
  for (const words of [/book\/tests\/ch11\.tests/, /card by card and clause by clause/, /record of each card/, /«nunca», «no acabaría», «solo», «ninguno»/, /extrapolate/, /A time limit proves nothing/, /later chapter[^\n]*docs\/superpowers\/plans\/2026-10-24-ki-plan-01-part1\.md/, /All cards, not a sample/]) {
    assert.match(t.prompt, words)
  }
  const nit = await chapter({}, { 'ch11:pass:cards': finds(LIE('pass-cards-1', 'Nit', 'prose', 'text', true, 'book/tests/ch11.tests:9')) })
  assert.equal(nit.r.status, 'ready-to-publish', 'a truth reader may report a Nit, and it points at the cards')
  const second = await chapter({ mode: 'second-pass' })
  assert.ok(second.w.called.includes('ch11:second:cards'))
})

test('a false reason is fixed whatever its severity: the fix must answer a Nit that says so, and a plain Nit stays optional', async () => {
  const lie = LIE('pass-whys-1', 'Nit', 'prose', 'text', true)
  const plain = F('pass-whys-2', 'Nit', 'prose', 'text', true)
  const { w, r } = await chapter({}, { 'ch11:pass:whys': finds(lie, plain) })
  assert.equal(r.status, 'ready-to-publish')
  const fix = r.trace.find((x) => x.label === 'ch11:fix1').prompt
  assert.match(fix, /Fix these prose findings, by id: pass-whys-1\./)
  assert.match(fix, /If it costs no words, also: pass-whys-2,/)
  assert.ok(w.called.includes('ch11:check1'))
  assert.equal(r.counts.passFalse, 1)
  assert.match(r.trace.find((x) => x.label === 'ch11:ledger').prompt, /pass-whys-2/, 'the plain Nit is parked')
  assert.ok(!/one line per id[^\n]*: pass-whys-1/.test(r.trace.find((x) => x.label === 'ch11:ledger').prompt), 'the lie is not')

  const silent = await chapter({}, { 'ch11:pass:whys': finds(lie, plain), 'ch11:fix1': { resolutions: [] } })
  assert.equal(silent.r.stopped, 'ch11:fix1')
  assert.match(silent.r.reason, /left silently.*pass-whys-1/)
  assert.ok(!/left silently.*pass-whys-2/.test(silent.r.reason))

  const alone = await chapter({}, { 'ch11:pass:whys': finds(plain) })
  none(alone.w, ['ch11:fix1', 'ch11:check1'])
})

test('a false reason in frozen code narrows the page and goes forward: no unfreeze, not parked', async () => {
  const lie = LIE('pass-whys-1', 'Minor', 'code', 'other', false)
  const { w, r } = await chapter({}, { 'ch11:pass:whys': finds(lie) })
  assert.equal(r.status, 'ready-to-publish')
  none(w, ['ch11:thaw1:batch'])
  assert.match(r.trace.find((x) => x.label === 'ch11:fix1').prompt, /no code will fix after the freeze: pass-whys-1/)
  assert.deepEqual(r.counts.errata, ['pass-whys-1'])
  assert.equal(r.counts.parked, 0)
  const nit = await chapter({}, { 'ch11:pass:facts': finds(LIE('pass-facts-1', 'Nit', 'code', 'other', true)) })
  none(nit.w, ['ch11:thaw1:batch'])
  assert.deepEqual(nit.r.counts.errata, ['pass-facts-1'])
  const seen = await chapter({}, { 'ch11:pass:whys': finds(LIE('pass-whys-1', 'Minor', 'code', 'other', true)) })
  assert.ok(seen.w.called.includes('ch11:thaw1:batch'), 'a Minor the reader sees still travels before the tag')
  const reviewed = await chapter({}, { 'ch11:pass:whys': finds(F('pass-whys-1', 'Important', 'code', 'message', true)), 'ch11:thaw1:rereview': finds(LIE('thaw1-rereview-1', 'Minor', 'code', 'other', false)) })
  assert.match(reviewed.r.trace.find((x) => x.label === 'ch11:fix1').prompt, /no code will fix after the freeze: thaw1-rereview-1/)
  assert.deepEqual(reviewed.r.counts.errata, ['thaw1-rereview-1'])
})

test('a false reason still open after two turns is deleted, like an Important; a plain Minor is parked', async () => {
  const passing = { 'ch11:pass:facts': finds(F('pass-facts-1', 'Important', 'prose', 'text', true)), 'ch11:check1': finds(F('check1-1', 'Important', 'prose', 'text', true)) }
  const lie = await chapter({}, { ...passing, 'ch11:check2': finds(LIE('check2-1', 'Minor', 'prose', 'text', true), LIE('check2-2', 'Minor', 'code', 'other', false), LIE('check2-3', 'Minor', 'code', 'other', true)) })
  assert.ok(lie.w.called.includes('ch11:delete'))
  assert.deepEqual([...lie.r.counts.deleted].sort(), ['check2-1', 'check2-2', 'check2-3'])
  assert.equal(lie.r.counts.parked, 0)
  const plain = await chapter({}, { ...passing, 'ch11:check2': finds(F('check2-1', 'Minor', 'prose', 'text', true)) })
  none(plain.w, ['ch11:delete'])
  assert.equal(plain.r.counts.parked, 1)
})

test('in the gate and the session reviews a false reason goes to the batch, not the parked file; the re-review carries it to the writer', async () => {
  const review = await chapter({}, { 'ch11:review:T50': finds(LIE('review-T50-1', 'Minor', 'code', 'other', false)), 'ch11:batch': { resolutions: [] } })
  assert.equal(review.r.stopped, 'ch11:batch')
  assert.match(review.r.reason, /left silently.*review-T50-1/)
  const nit = await chapter({}, { 'ch11:gate1:truth': finds(LIE('gate1-truth-1', 'Nit', 'code', 'other', false)) })
  assert.ok(nit.w.called.includes('ch11:gate1:batch'), 'a gate Nit is kept as a Minor: a false one goes to the batch')
  const minor = await chapter({}, { 'ch11:gate1:truth': finds(LIE('gate1-truth-1', 'Minor', 'code', 'other', false)) })
  assert.ok(minor.w.called.includes('ch11:gate1:batch'))
  assert.match(minor.r.trace.find((x) => x.label === 'ch11:gate1:batch').prompt, /gate1-truth-1/)
  assert.equal(minor.r.code.parked, 0)
  const carried = await chapter({}, { 'ch11:gate1:truth': finds(F('gate1-truth-1', 'Important', 'code', 'other', false)), 'ch11:gate1:rereview': finds(LIE('gate1-rereview-1', 'Minor', 'code', 'output', false)) })
  assert.equal(carried.r.status, 'ready-to-publish', 'a Minor false reason is no visible failure: no second iteration, no stop')
  assert.deepEqual(carried.r.code.carried, ['gate1-rereview-1'])
  none(carried.w, ['ch11:gate2:code'])
  assert.match(carried.r.trace.find((x) => x.label === 'ch11:draft').prompt, /do not claim what they deny\. Ids gate1-rereview-1/)
})

test('after the freeze: a visible failure unfreezes, a test gap narrows, a seen Minor travels, an unseen one is parked', async () => {
  const message = F('pass-whys-1', 'Important', 'code', 'message', true)
  const gap = F('pass-whys-2', 'Important', 'code', 'test-gap', false)
  const seen = F('pass-whys-3', 'Minor', 'code', 'other', true)
  const unseen = F('pass-whys-4', 'Minor', 'code', 'other', false)
  const text = F('pass-facts-1', 'Important', 'prose', 'text', true)
  const minorText = F('pass-apprentice-1', 'Minor', 'prose', 'text', true)
  const { w, r } = await chapter({}, { 'ch11:pass:whys': finds(message, gap, seen, unseen), 'ch11:pass:facts': finds(text), 'ch11:pass:apprentice': finds(minorText) })
  assert.equal(r.status, 'ready-to-publish')
  const order = after(w, 'ch11:pass:follower')
  assert.deepEqual(order, ['ch11:thaw1:batch', 'ch11:thaw1:rereview', 'ch11:refreeze1', 'ch11:fix1', 'ch11:machine:fix1', 'ch11:check1', 'ch11:ledger', 'ch11:integrate', 'ch11:verify'])
  const prompt = (label) => r.trace.find((x) => x.label === label).prompt
  assert.match(prompt('ch11:thaw1:batch'), /pass-whys-1, pass-whys-3/)
  assert.ok(!prompt('ch11:thaw1:batch').includes('pass-whys-4'))
  assert.ok(!prompt('ch11:thaw1:batch').includes('pass-whys-2'))
  assert.match(prompt('ch11:fix1'), /Fix these prose findings, by id: pass-facts-1, pass-apprentice-1/)
  assert.match(prompt('ch11:fix1'), /Narrow or delete[^\n]*/)
  assert.match(prompt('ch11:fix1'), /no code will fix after the freeze: pass-whys-2/)
  assert.ok(!prompt('ch11:fix1').includes('pass-whys-4'))
  assert.match(prompt('ch11:ledger'), /pass-whys-4/)
  assert.match(prompt('ch11:ledger'), /Erratas hacia delante[^\n]*pass-whys-2/)
  assert.deepEqual(r.counts.errata, ['pass-whys-2'])
  assert.equal(r.counts.thaws, 1)
})

test('a test gap alone does not unfreeze; only Minors in the text still get one fix', async () => {
  const gap = await chapter({}, { 'ch11:pass:whys': finds(F('pass-whys-1', 'Important', 'code', 'test-gap', false)) })
  none(gap.w, ['ch11:thaw1:batch'])
  assert.ok(gap.w.called.includes('ch11:fix1'))
  const minor = await chapter({}, { 'ch11:pass:apprentice': finds(F('pass-apprentice-1', 'Minor', 'prose', 'text', true)) })
  assert.ok(minor.w.called.includes('ch11:fix1'))
  const unseen = await chapter({}, { 'ch11:pass:whys': finds(F('pass-whys-1', 'Minor', 'code', 'other', false)) })
  none(unseen.w, ['ch11:fix1', 'ch11:thaw1:batch', 'ch11:check1'])
})

test('an Important with no resolution stops: it cannot be left silently', async () => {
  const { w, r } = await chapter({}, { 'ch11:pass:facts': finds(F('pass-facts-1', 'Important', 'prose', 'text', true)), 'ch11:fix1': { resolutions: [] } })
  assert.equal(r.stopped, 'ch11:fix1')
  assert.match(r.reason, /left silently.*pass-facts-1/)
  none(w, ['ch11:check1', 'ch11:integrate'])
})

test('the check has at most 2 turns; what stays open is deleted or stops the chapter', async () => {
  const passing = { 'ch11:pass:facts': finds(F('pass-facts-1', 'Important', 'prose', 'text', true)) }
  const clean = await chapter({}, passing)
  assert.equal(clean.w.called.filter((l) => /check/.test(l)).length, 1)

  const once = await chapter({}, { ...passing, 'ch11:check1': finds(F('check1-1', 'Important', 'prose', 'text', true)) })
  assert.deepEqual(once.w.called.filter((l) => /:fix\d|:check/.test(l) && !/machine/.test(l)), ['ch11:fix1', 'ch11:check1', 'ch11:fix2', 'ch11:check2'])
  none(once.w, ['ch11:delete'])

  const twice = await chapter({}, { ...passing, 'ch11:check1': finds(F('check1-1', 'Important', 'prose', 'text', true)), 'ch11:check2': finds(F('check2-1', 'Important', 'prose', 'text', true)) })
  assert.deepEqual(after(twice.w, 'ch11:check2'), ['ch11:delete', 'ch11:machine:delete', 'ch11:ledger', 'ch11:integrate', 'ch11:verify'])
  assert.equal(twice.r.status, 'ready-to-publish')
  assert.deepEqual(twice.r.counts.deleted, ['check2-1'])
  assert.match(twice.r.trace.find((x) => x.label === 'ch11:delete').prompt, /check2-1/)

  const refused = await chapter({}, { ...passing, 'ch11:check1': finds(F('check1-1', 'Important', 'prose', 'text', true)), 'ch11:check2': finds(F('check2-1', 'Important', 'prose', 'text', true)), 'ch11:delete': { resolutions: [{ id: 'check2-1', action: 'rejected', note: 'no' }] } })
  assert.equal(refused.r.stopped, 'ch11:delete')
  none(refused.w, ['ch11:integrate'])

  const visible = await chapter({}, { ...passing, 'ch11:check1': finds(F('check1-1', 'Important', 'prose', 'text', true)), 'ch11:check2': finds(F('check2-1', 'Important', 'code', 'fixit', true)) })
  assert.equal(visible.r.stopped, 'ch11:check')
  assert.match(visible.r.reason, /does not publish.*check2-1/)
  none(visible.w, ['ch11:integrate', 'ch11:delete'])
})

test('an integrated chapter that does not verify is not ready to publish', async () => {
  const { r } = await chapter({}, { 'ch11:verify': finds(F('verify-1', 'Important', 'code', 'output', true)) })
  assert.equal(r.stopped, 'ch11:verify')
  assert.match(r.reason, /do not publish.*verify-1/)
  assert.ok(!r.status && !r.push)
})

test('a failed unfreeze stops the chapter', async () => {
  const pass = { 'ch11:pass:whys': finds(F('pass-whys-1', 'Important', 'code', 'limit', true)) }
  const open = await chapter({}, { ...pass, 'ch11:thaw1:rereview': finds(F('thaw1-rereview-1', 'Important', 'code', 'limit', true)) })
  assert.equal(open.r.stopped, 'ch11:thaw')
  none(open.w, ['ch11:fix1', 'ch11:integrate'])
  const dead = await chapter({}, pass, { kills: { 'ch11:thaw1:rereview': 'null' } })
  assert.equal(dead.r.stopped, 'ch11:thaw1:rereview')
  none(dead.w, ['ch11:fix1', 'ch11:integrate'])
})

test('the gate: one iteration; a batch only for Important or what the reader sees; a second one only for behaviour', async () => {
  const unseen = await chapter({}, { 'ch11:gate1:code': finds(F('gate1-code-1', 'Minor', 'code', 'other', false)) })
  none(unseen.w, ['ch11:gate1:batch', 'ch11:gate1:rereview', 'ch11:gate2:code'])
  assert.match(unseen.r.trace.find((x) => x.label === 'ch11:freeze').prompt, /gate1-code-1/)

  const seen = await chapter({}, { 'ch11:gate1:truth': finds(F('gate1-truth-1', 'Minor', 'code', 'other', true, 'book/tests/ch11.tests:4')) })
  assert.ok(seen.w.called.includes('ch11:gate1:batch'))
  none(seen.w, ['ch11:gate2:code'])

  const gap = await chapter({}, { 'ch11:gate1:code': finds(F('gate1-code-1', 'Important', 'code', 'test-gap', false)) })
  assert.ok(gap.w.called.includes('ch11:gate1:rereview'))
  none(gap.w, ['ch11:gate2:code'])
  assert.ok(gap.w.called.indexOf('ch11:gate1:rereview') < gap.w.called.indexOf('ch11:freeze'))

  const behaviour = await chapter({}, { 'ch11:gate1:hunt': finds(F('gate1-hunt-1', 'Important', 'code', 'fixit', true)) })
  assert.ok(behaviour.w.called.includes('ch11:gate2:code'))
  none(behaviour.w, ['ch11:gate2:batch'])
  assert.equal(behaviour.r.code.iterations, 2)

  const fixed = await chapter({ fixedSecondIteration: true })
  assert.ok(fixed.w.called.includes('ch11:gate2:code'))

  const stuck = await chapter({}, { 'ch11:gate1:code': finds(F('gate1-code-1', 'Important', 'code', 'output', true)), 'ch11:gate2:code': finds(F('gate2-code-1', 'Important', 'code', 'output', true)), 'ch11:gate2:rereview': finds(F('gate2-rereview-1', 'Important', 'code', 'output', true)) })
  assert.equal(stuck.r.stopped, 'ch11:gate')
  none(stuck.w, ['ch11:freeze', 'ch11:draft'])

  const carried = await chapter({}, { 'ch11:gate1:code': finds(F('gate1-code-1', 'Important', 'code', 'test-gap', false)), 'ch11:gate1:rereview': finds(F('gate1-rereview-1', 'Important', 'code', 'test-gap', false)) })
  assert.deepEqual(carried.r.code.carried, ['gate1-rereview-1'])
  assert.match(carried.r.trace.find((x) => x.label === 'ch11:draft').prompt, /gate1-rereview-1/)
})

test('a gate lens may only report owner code, and a dead re-review is not a clean one', async () => {
  const prose = await chapter({}, { 'ch11:gate1:code': finds(F('gate1-code-1', 'Important', 'prose', 'text', true)) })
  assert.ok(prose.r.stopped)
  const dead = await chapter({}, { 'ch11:gate1:code': finds(F('gate1-code-1', 'Important', 'code', 'test-gap', false)) }, { kills: { 'ch11:gate1:rereview': 'null' } })
  assert.equal(dead.r.stopped, 'ch11:gate1:rereview')
  none(dead.w, ['ch11:freeze'])
  const deadReview = await chapter({}, {}, { kills: { 'ch11:review:T50': 'null' } })
  assert.match(deadReview.r.stopped, /review:T50/)
  none(deadReview.w, ['ch11:gate1:code', 'ch11:freeze'])
})

test('session reviews: findings go to one chapter batch, and an unseen Minor only to the parked file', async () => {
  const { w, r } = await chapter({}, { 'ch11:review:T50': finds(F('review-T50-1', 'Important', 'code', 'other', false), F('review-T50-2', 'Minor', 'code', 'other', false)) })
  assert.ok(w.called.includes('ch11:batch'))
  const prompt = r.trace.find((x) => x.label === 'ch11:batch').prompt
  assert.match(prompt, /review-T50-1/)
  assert.ok(!prompt.includes('review-T50-2'))
  assert.match(r.trace.find((x) => x.label === 'ch11:freeze').prompt, /review-T50-2/)
  const minors = await chapter({}, { 'ch11:review:T50': finds(F('review-T50-2', 'Minor', 'code', 'other', false)) })
  none(minors.w, ['ch11:batch'])
})

test('the hunt runs only when the chapter adds diagnostics; the control lens only in chapters 11 to 13', async () => {
  const plain = await chapter({ diagnostics: false })
  none(plain.w, ['ch11:gate1:hunt'])
  const control = await chapter({ controlLens: undefined })
  assert.ok(control.w.called.includes('ch11:pass:control'))
  const w = world()
  await exec('chapter-v4.js', { ...base({ controlLens: undefined, n: 14 }) }, w)
  assert.ok(!w.called.includes('ch14:pass:control'))
  assert.equal(w.result.second_pass_due, false)
  assert.equal(w.result.ask_verdict, false)
  const w2 = world()
  await exec('chapter-v4.js', { ...base({ controlLens: undefined, n: 14, newKind: true }) }, w2)
  assert.equal(w2.result.second_pass_due, true)
  assert.equal(w2.result.ask_verdict, true)
  const dead = await chapter({ controlLens: true }, {}, { kills: { 'ch11:pass:control': 'null' } })
  assert.ok(dead.r.stopped)
  const counted = await chapter({ controlLens: true }, { 'ch11:pass:control': finds(F('pass-control-1', 'Important', 'code', 'message', true)) })
  assert.equal(counted.r.counts.controlVisible, 1)
})

test('lint and doctest get one fix before any reader; a second failure stops', async () => {
  const fail = { status: 'BLOCKED', concerns: ['doctest: 2 differ'] }
  const once = await chapter({}, { 'ch11:machine:draft': fail })
  assert.deepEqual(after(once.w, 'ch11:machine:draft').slice(0, 2), ['ch11:machine-fix:draft', 'ch11:machine2:draft'])
  assert.equal(once.r.status, 'ready-to-publish')
  assert.equal(once.r.text, 'sha-machine-fix-draft', 'the readers read the text the machine passed')
  const twice = await chapter({}, { 'ch11:machine:draft': fail, 'ch11:machine2:draft': fail })
  assert.equal(twice.r.stopped, 'ch11:machine2:draft')
  none(twice.w, ['ch11:pass:facts'])
  const concerns = await chapter({}, { 'ch11:machine:draft': { status: 'DONE_WITH_CONCERNS', concerns: ['lint error'] } })
  assert.ok(concerns.w.called.includes('ch11:machine-fix:draft'), 'DONE_WITH_CONCERNS is not a pass for the machine')
  const dead = await chapter({}, {}, { kills: { 'ch11:machine:draft': 'null' } })
  assert.ok(dead.r.stopped, 'a dead machine check is a stop, not a failed lint')
  none(dead.w, ['ch11:machine-fix:draft'])
})

test('prompts: paths and ids, never pasted findings; blind readers; 2 500 words; a resume rule everywhere', async () => {
  const answers = { 'ch11:pass:whys': finds(F('pass-whys-1', 'Important', 'code', 'message', true), F('pass-whys-2', 'Important', 'code', 'test-gap', false)), 'ch11:pass:facts': finds(F('pass-facts-1', 'Important', 'prose', 'text', true)), 'ch11:gate1:code': finds(F('gate1-code-1', 'Important', 'code', 'output', true)), 'ch11:check1': finds(F('check1-1', 'Important', 'prose', 'text', true)) }
  const { r } = await chapter({ next: { n: 12, sessions: ['61'], diagnostics: false } }, answers)
  for (const t of r.trace) {
    assert.ok(!t.prompt.includes('SECRET'), 'finding text pasted into ' + t.label)
    assert.ok(t.prompt.startsWith('RESUME RULE.'), t.label + ' has no resume rule')
    assert.ok(t.file.startsWith(W + '/v4-ch'), t.label + ': result file ' + t.file)
    assert.ok(!/undefined|\[object |NaN/.test(t.prompt), 'broken text in ' + t.label + ': ' + t.prompt)
    assert.ok(!/3 000/.test(t.prompt), '3 000 in ' + t.label)
    assert.ok(t.model, t.label + ' names no model')
    assert.ok(!/git push/.test(t.prompt), t.label + ' tells an agent to push')
    assert.ok(!/(^|[^,r] )git stash/.test(t.prompt.replace(/Never git reset --hard, git clean, git stash/, '')), t.label + ' tells an agent to stash')
  }
  for (const key of ['facts', 'whys', 'cards', 'apprentice', 'follower']) {
    const p = r.trace.find((x) => x.label === 'ch11:pass:' + key).prompt
    assert.match(p, /You are blind/)
    assert.match(p, /its result file, which you look for first and write last/, 'blind must not cost the resume')
    assert.deepEqual(uniqueW(p), [V + '/pass-' + key + '.json', V + '/pass-' + key + '.md'].sort(), 'a reader knows no other report')
    assert.ok(!/panel-|task-\d|review-T|gate\d/.test(p))
  }
  const draft = r.trace.find((x) => x.label === 'ch11:draft').prompt
  assert.match(draft, /2 500 flow words/)
  assert.ok(!draft.includes('panel-ch'), 'the writer reads no old panel reports')
  assert.ok(r.trace.filter((t) => /:(fix|batch|thaw)/.test(t.label)).every((t) => !/fix-batch-ch06/.test(t.prompt)), 'no 55k report as a procedure')
  assert.ok(!r.trace.some((t) => /Fix ALL/i.test(t.prompt)))
})
const uniqueW = (p) => [...new Set(p.match(new RegExp(W.replace(/[.]/g, '\\.') + '[^ ."]*\\.[a-z]+', 'g')).map((x) => x.replace(/\.tmp$/, '')))].sort()

test('a crash at any step, then a run again with the same args: finished steps replay, nothing is lost', async () => {
  const answers = { 'ch11:gate1:code': finds(F('gate1-code-1', 'Important', 'code', 'test-gap', false)), 'ch11:pass:whys': finds(F('pass-whys-1', 'Important', 'code', 'message', true)), 'ch11:pass:facts': finds(F('pass-facts-1', 'Important', 'prose', 'text', true)) }
  const full = await chapter({}, answers)
  assert.equal(full.r.status, 'ready-to-publish')
  for (const label of full.w.called) {
    const store = new Map()
    const first = world({ crashAt: label, store })
    await exec('chapter-v4.js', base({}, answers), first)
    assert.ok(first.result.stopped, 'crash at ' + label + ' did not stop')
    assert.ok(!first.result.status)
    const second = world({ store })
    await exec('chapter-v4.js', base({}, answers), second)
    assert.equal(second.result.status, 'ready-to-publish', 'after a crash at ' + label + ': ' + JSON.stringify(second.result.reason))
    for (const done of first.worked) assert.ok(!second.worked.includes(done), done + ' was worked twice after a crash at ' + label)
    assert.ok(second.worked.includes(label) || label === 'ch11:never', label + ' must be done by the second run')
    // Same args, same results: the same prompts, so a Workflow resume finds its prefix cached too.
    const prompts = new Map(first.result.trace.map((t) => [t.label, t.prompt]))
    for (const t of second.result.trace) if (prompts.has(t.label)) assert.equal(t.prompt, prompts.get(t.label), 'prompt of ' + t.label + ' changed')
  }
})

test('with the next chapter on top, the main checkout stays serialized and each track stops alone', async () => {
  const next = { n: 12, sessions: ['61', '62'], diagnostics: true }
  const ok = await chapter({ next }, { 'ch11:pass:whys': finds(F('pass-whys-1', 'Important', 'code', 'output', true)) })
  assert.equal(ok.r.status, 'ready-to-publish')
  assert.equal(ok.w.maxMain, 1, 'two owners of the main checkout at once')
  assert.ok(ok.w.called.indexOf('ch11:freeze') < ok.w.called.indexOf('ch12:impl:T61'), 'the next chapter starts after the freeze')
  assert.ok(ok.w.called.includes('ch12:freeze'))
  assert.ok(!ok.w.called.some((l) => /^ch12:(draft|pass|integrate)/.test(l)), 'the next chapter stops at its own freeze')
  assert.ok(ok.r.next.frozen)

  const nextDead = await chapter({ next }, {}, { kills: { 'ch12:gate1:code': 'null' } })
  assert.equal(nextDead.r.status, 'ready-to-publish', 'chapter 11 is still whole')
  assert.match(nextDead.r.next.stopped, /ch12:gate1:code/)
  assert.ok(!nextDead.w.called.includes('ch12:freeze'))

  const proseDead = await chapter({ next }, {}, { kills: { 'ch11:pass:facts': 'null' } })
  assert.match(proseDead.r.stopped, /ch11:pass:facts/)
  assert.ok(proseDead.r.next.frozen, 'the next chapter finishes its code')
  none(proseDead.w, ['ch11:integrate'])
})

test('a chapter run after its code ran in the previous one replays that code', async () => {
  const store = new Map()
  const w1 = world({ store })
  await exec('chapter-v4.js', base({ next: { n: 12, sessions: ['61'], diagnostics: false, proseTask: '62', tagFallback: 'ch12: y' } }), w1)
  assert.equal(w1.result.status, 'ready-to-publish')
  const w2 = world({ store })
  await exec('chapter-v4.js', { ...base({ n: 12, sessions: ['61'], diagnostics: false, proseTask: '62', tagFallback: 'ch12: y' }) }, w2)
  assert.equal(w2.result.status, 'ready-to-publish')
  assert.ok(w2.replayed.includes('ch12:freeze') && w2.replayed.includes('ch12:impl:T61'))
  assert.ok(w2.worked.includes('ch12:draft') && !w2.worked.includes('ch12:impl:T61'))
})

test('bad args stop before any agent', async () => {
  for (const [why, extra] of [['n', { n: 'eleven' }], ['part', { part: '' }], ['proseTask', { proseTask: undefined }], ['sessions', { sessions: '50' }], ['diagnostics', { diagnostics: undefined }], ['tagFallback', { tagFallback: '' }], ['next', { next: { n: 12 } }]]) {
    const { w, r } = await chapter(extra)
    assert.equal(r.stopped, 'args', why)
    assert.equal(w.called.length, 0)
  }
})

test('the second pass only reports: no fix, on the tag; a dead reader says so', async () => {
  const { w, r } = await chapter({ mode: 'second-pass' }, { 'ch11:second:facts': finds(F('second-facts-1', 'Important', 'prose', 'text', true)) })
  assert.equal(r.status, 'second-pass-done')
  assert.deepEqual(r.important, ['second-facts-1'])
  assert.ok(!w.called.some((l) => /fix|integrate|draft|impl/.test(l)))
  assert.match(r.trace.find((x) => x.label === 'ch11:second:ledger').prompt, /second-facts-1/)
  assert.match(r.trace.find((x) => x.label === 'ch11:second:whys').prompt, /frozen at ch11\./)
  const dead = await chapter({ mode: 'second-pass' }, {}, { kills: { 'ch11:second:whys': 'null' } })
  assert.match(dead.r.stopped, /second:whys/)
  none(dead.w, ['ch11:second:ledger'])
})

test('without dryRun the agents get the real prompts and the same fail-closed checks', async () => {
  const calls = []
  const answer = (prompt, o) => {
    const path = (prompt.match(/report_path exactly (\S+?\.md)/) || [])[1] || ''
    if (o.schema.properties.findings) return { findings: [], checked: 3, report_path: path }
    return { status: 'DONE', head: 'abc1234', report_path: path, concerns: [], resolutions: [] }
  }
  const args = { ...base() }
  delete args.dryRun
  const agent = async (prompt, o) => { calls.push({ prompt, o }); return answer(prompt, o) }
  const run = (a) => compile('chapter-v4.js')(a, parallel, null, () => {}, () => {}, args, {}, null, FakeDate, FakeMath)
  const r = await run(agent)
  assert.equal(r.status, 'ready-to-publish')
  assert.equal(r.trace, undefined)
  assert.ok(calls.every((c) => c.prompt.startsWith('RESUME RULE.') && !c.prompt.startsWith('DRY RUN')))
  const killed = await run(async (p, o) => (o.label === 'ch11:pass:whys' ? null : answer(p, o)))
  assert.match(killed.stopped, /ch11:pass:whys/)
})

// ================= part gate =================
const part = (extra = {}, answers = {}) => ({ part: 'II', baseTag: 'm1', chapters: [{ n: 11, file: 'book/ch11-a.md', prev: 'ch10' }, { n: 12, file: 'book/ch12-b.md', prev: 'ch11' }, { n: 13, file: 'book/ch13-c.md', prev: 'ch12' }], ...extra, dryRun: { answers } })
const gate = (extra, answers, opts) => { const w = world(opts); return exec('part-gate.js', part(extra, answers), w).then((r) => ({ w, r })) }
const GATE_READERS = ['gate:theory-ch11', 'gate:theory-ch12', 'gate:theory-ch13', 'gate:consistency', 'gate:branch', 'gate:clone', 'gate:practice-ch11', 'gate:practice-ch13']

test('part gate: a clean Part runs every reader once and closes, with the parked list burned', async () => {
  const { w, r } = await gate()
  assert.deepEqual(w.called, ['gate:snapshot', ...GATE_READERS, 'gate:fix:code', 'gate:fix:prose', 'gate:reverify', 'gate:close'])
  assert.equal(r.status, 'closed')
  assert.match(r.trace.find((t) => t.label === 'gate:fix:code').prompt, /parked-part-II\.md/)
  assert.equal(r.trace.find((t) => t.label === 'gate:branch').model, 'fable')
  for (const t of r.trace) {
    assert.ok(t.prompt.startsWith('RESUME RULE.'))
    assert.ok(!/undefined|\[object /.test(t.prompt), t.label + ': ' + t.prompt)
  }
})

test('part gate: killing any reader stops it before any fix', async () => {
  for (const label of GATE_READERS) {
    const { w, r } = await gate({}, {}, { kills: { [label]: 'null' } })
    assert.match(r.stopped, new RegExp(label))
    none(w, ['gate:fix:code', 'gate:close'])
  }
  for (const label of ['gate:fix:code', 'gate:fix:prose', 'gate:reverify', 'gate:close']) {
    const { r } = await gate({}, {}, { kills: { [label]: 'null' } })
    assert.equal(r.stopped, label)
    assert.ok(!r.status)
  }
})

test('part gate: a memory death or a broken limit blocks until clean, at most 3 turns, then it goes to Álvaro', async () => {
  const death = F('branch-1', 'Important', 'code', 'memory-death', false)
  const limit = F('clone-1', 'Critical', 'code', 'limit', true)
  const again = (t) => finds(F('block' + t + '-1', 'Important', 'code', 'memory-death', false))
  const one = await gate({}, { 'gate:branch': finds(death), 'gate:clone': finds(limit) })
  assert.equal(one.r.status, 'closed')
  assert.deepEqual(after(one.w, GATE_READERS.at(-1)).slice(0, 3), ['gate:block1:fix', 'gate:block1:recheck', 'gate:fix:code'])
  assert.ok(one.w.called.indexOf('gate:block1:recheck') < one.w.called.indexOf('gate:close'))
  assert.equal(one.r.counts.blockingTurns, 1)

  const two = await gate({}, { 'gate:branch': finds(death), 'gate:block1:recheck': again(1) })
  assert.equal(two.r.status, 'closed')
  assert.equal(two.r.counts.blockingTurns, 2)

  const stuck = await gate({}, { 'gate:branch': finds(death), 'gate:block1:recheck': again(1), 'gate:block2:recheck': again(2), 'gate:block3:recheck': again(3) })
  assert.equal(stuck.r.stopped, 'gate:blocking')
  assert.equal(stuck.r.needs_alvaro, true)
  assert.match(stuck.r.reason, /block3-1/)
  assert.equal(stuck.w.called.filter((l) => /:fix$/.test(l) && /block/.test(l)).length, 3)
  none(stuck.w, ['gate:fix:code', 'gate:close', 'gate:block4:fix'])

  const late = await gate({}, { 'gate:reverify': finds(F('reverify-1', 'Important', 'code', 'limit', true)) })
  assert.ok(late.w.called.includes('gate:block1:fix'), 'a limit the fixes broke blocks too')
  assert.ok(late.w.called.indexOf('gate:reverify') < late.w.called.indexOf('gate:block1:fix'))
})

test('part gate: no rate of findings adds a round; what stays open is deleted or carried forward by name', async () => {
  const many = Array.from({ length: 9 }, (_, i) => F('consistency-' + (i + 1), 'Important', 'prose', 'text', true))
  const { w, r } = await gate({}, { 'gate:consistency': finds(...many), 'gate:reverify': finds(F('reverify-1', 'Important', 'prose', 'text', true), F('reverify-2', 'Important', 'code', 'other', false)) })
  assert.equal(r.status, 'closed')
  assert.equal(w.called.filter((l) => l === 'gate:fix:prose').length, 1)
  assert.equal(w.called.filter((l) => l === 'gate:reverify').length, 1)
  assert.match(r.advice, /third truth reader.*never a round/)
  assert.deepEqual(r.counts.deleted, ['reverify-1'])
  assert.deepEqual(r.counts.forward, ['reverify-2'])
  assert.match(r.trace.find((t) => t.label === 'gate:close').prompt, /Erratas hacia delante[^\n]*reverify-2/)
  assert.ok(!r.trace.some((t) => t.prompt.includes('SECRET')))
  const silent = await gate({}, { 'gate:theory-ch11': finds(F('theory-ch11-1', 'Important', 'prose', 'text', true)), 'gate:fix:prose': { resolutions: [] } })
  assert.equal(silent.r.stopped, 'gate:fix:prose')
  assert.match(silent.r.reason, /left silently/)
})

test('part gate: a false reason is fixed whatever its severity, and what stays open is deleted or carried by name', async () => {
  const lieP = LIE('theory-ch11-1', 'Nit', 'prose', 'text', true)
  const lieC = LIE('branch-1', 'Minor', 'code', 'other', false)
  const plainP = F('theory-ch11-2', 'Nit', 'prose', 'text', true)
  const { r } = await gate({}, { 'gate:theory-ch11': finds(lieP, plainP), 'gate:branch': finds(lieC) })
  assert.equal(r.status, 'closed')
  const trace = (label) => r.trace.find((t) => t.label === label).prompt
  assert.match(trace('gate:fix:prose'), /Findings, by id: theory-ch11-1,/)
  assert.ok(!trace('gate:fix:prose').includes('theory-ch11-2'))
  assert.match(trace('gate:fix:code'), /Findings, by id: branch-1,/)
  assert.match(trace('gate:close'), /what stays: theory-ch11-2 /)
  const silentP = await gate({}, { 'gate:theory-ch11': finds(lieP), 'gate:fix:prose': { resolutions: [] } })
  assert.equal(silentP.r.stopped, 'gate:fix:prose')
  const silentC = await gate({}, { 'gate:branch': finds(lieC), 'gate:fix:code': { resolutions: [] } })
  assert.equal(silentC.r.stopped, 'gate:fix:code')
  const open = await gate({}, { 'gate:reverify': finds(LIE('reverify-1', 'Minor', 'prose', 'text', true), LIE('reverify-2', 'Minor', 'code', 'other', false), F('reverify-3', 'Minor', 'prose', 'text', true)) })
  assert.deepEqual(open.r.counts.deleted, ['reverify-1'])
  assert.deepEqual(open.r.counts.forward, ['reverify-2'])
})

test('part gate: practice is a sample by default, an argument otherwise; milestone; bad args', async () => {
  const none_ = await gate({ practice: [] })
  assert.ok(!none_.w.called.some((l) => l.includes('practice')))
  const all = await gate({ practice: [12] })
  assert.ok(all.w.called.includes('gate:practice-ch12'))
  const wrong = await gate({ practice: [99] })
  assert.equal(wrong.r.stopped, 'gate:practice')
  const m = await gate({ milestone: { tag: 'm2', chapter: 'ch13', message: 'M2' } })
  assert.match(m.r.trace.find((t) => t.label === 'gate:close').prompt, /Create m2 on ch13/)
  for (const extra of [{ part: '' }, { baseTag: '' }, { chapters: [] }, { chapters: [{ n: 11 }] }, { milestone: { tag: 'm2' } }]) {
    const b = await gate(extra)
    assert.equal(b.r.stopped, 'args', JSON.stringify(extra))
    assert.equal(b.w.called.length, 0)
  }
})

test('part gate: a crash at any step, then a run again, is safe', async () => {
  const answers = { 'gate:branch': finds(F('branch-1', 'Important', 'code', 'memory-death', false)), 'gate:consistency': finds(F('consistency-1', 'Important', 'prose', 'text', true)) }
  const full = await gate({}, answers)
  for (const label of full.w.called) {
    const store = new Map()
    const first = world({ crashAt: label, store })
    await exec('part-gate.js', part({}, answers), first)
    assert.ok(first.result.stopped, label)
    const second = world({ store })
    await exec('part-gate.js', part({}, answers), second)
    assert.equal(second.result.status, 'closed', label)
    for (const done of first.worked) assert.ok(!second.worked.includes(done), done + ' worked twice')
  }
})

// ================= the scripts themselves =================
test('both scripts: a pure-literal meta first, the same shared block, nothing the runtime forbids', () => {
  const shared = {}
  for (const name of ['chapter-v4.js', 'part-gate.js']) {
    const s = source(name)
    assert.ok(s.startsWith('export const meta = {'), name + ' must begin with meta')
    const literal = s.slice('export const meta = '.length, s.indexOf('\n}\n') + 2)
    assert.ok(!/\$\{|\.\.\.|\(/.test(literal.replace(/'[^']*'/g, "''")), name + ': meta must be a pure literal')
    const meta = new Function('return ' + literal)()
    assert.ok(meta.name && meta.description)
    const code = s.replace(/^\s*\/\/.*$/gm, '')
    assert.ok(!/Date\.now|Math\.random|new Date\(/.test(code), name + ' uses what the runtime forbids')
    assert.ok(!/\bimport\b|\brequire\(|\bprocess\.|\bfs\.(read|write|exists)/.test(code.replace(/"[^"\n]*"/g, '""').replace(/'[^'\n]*'/g, "''")), name + ' reaches outside the runtime')
    assert.ok(!/filter\(Boolean\)/.test(code), name + ': filter(Boolean) is how a dead agent becomes a clean review')
    assert.ok(!/isProse/.test(code), name + ': no regex routing')
    const a = s.indexOf('// <shared>'), b = s.indexOf('// </shared>')
    assert.ok(a > 0 && b > a, name + ' has no shared block')
    shared[name] = s.slice(a, b)
  }
  assert.equal(shared['chapter-v4.js'], shared['part-gate.js'], 'the shared block has diverged')
})
