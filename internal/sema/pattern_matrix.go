package sema

import (
	"slices"
	"strconv"
	"strings"

	"github.com/aldezex/kilang/internal/base"
)

// This file is the algorithm behind the exhaustiveness check: Maranget's utility
// of a pattern vector over a matrix of patterns ("Warnings for pattern matching",
// 2007). It knows nothing of Ki's syntax or of its types: patterns are already
// reduced to the three things that matter (a wildcard, a constructor applied to
// sub-patterns, an or-pattern), and a type is a TypeID that a typeShapes says what
// constructors it has.
//
// The matrix has a row for each arm that has no guard (a guard may fail, so the arm
// covers nothing for the arms below it) and a column for each part of the value
// that is still to be compared. Two questions are asked of it.
//
//   - Is the vector q useful? Is there a value that q matches and no row does?
//     An arm that is not useful is unreachable (W0701).
//   - Which values does no row match? The vector is then all wildcards, and each
//     answer is a witness: a pattern that the match does not cover (E0701).
//
// Both are one search. A column is looked at from its first cell. A constructor in
// q is "specialized": the rows that start with it, or with a wildcard, lose that
// cell and gain its sub-patterns, and the search goes on with those. A wildcard in q
// is that for every constructor of the type if the rows name all of them (the
// signature is complete), and otherwise only for the rows that start with a
// wildcard, because a value built with a constructor that no row names can only be
// matched by those; the witness is then one of the constructors that is missing.
//
// The search is a loop over an explicit stack of nodes, and not a recursion. Each
// step consumes a column or replaces a cell with its sub-patterns, so the depth of
// the recursion is the size of the patterns, which the program controls: a tuple of
// a hundred thousand elements is a hundred thousand and one levels. The rows are
// linked lists of cells, so that taking the first cell off a row, or putting the
// sub-patterns of a constructor in front of it, costs the length of what is
// added and not the length of the row.
//
// Usefulness is NP-complete (Maranget §7.1): a matrix of tuples of `Bool` can
// encode 3-SAT, and then no algorithm answers quickly. The engine counts its steps
// and gives up past a budget (see engine.spend), and the caller reports E0707. A
// row of only wildcards matches every value, so the node that holds one has no
// useful vector and is not searched further (§7.2): that is the cheapest prune and
// the one that makes a `_` arm free.

// sigKind says how many constructors a type has and how they are told apart.
type sigKind uint8

const (
	// sigOpaque is a type that no pattern but a wildcard can match: `Float`
	// (E0706), `()`, a function, a variable that nothing has bound.
	sigOpaque sigKind = iota
	// sigInt is `Int`: a literal is a constructor with no fields, and no set of
	// them is all of the type, so a column of literals is never complete.
	sigInt
	// sigFinite is a type with signature.n constructors, numbered from 0: a sum
	// (by its tags), a record and a tuple (one constructor each) and `Bool`.
	sigFinite
)

// signature is what the engine needs to know of a type.
type signature struct {
	kind sigKind
	n    int // sigFinite: how many constructors the type has
}

// ctorForm says how a witness writes a constructor.
type ctorForm uint8

const (
	// formCtor is `None` or `Rect(_, _)`; a tuple is one with no name, `(_, 1)`.
	formCtor ctorForm = iota
	// formRecord is `Point { x: 1, .. }`: the fields that are wildcards are left out.
	formRecord
)

// ctorShape is how a witness writes the constructor key of a type.
type ctorShape struct {
	name   string
	form   ctorForm
	fields []string // formRecord: the names of the fields, in order
	arity  int
}

// typeShapes is what the engine asks of the types of the columns. The Ki pass
// answers it with the table of types and the declarations; a test of the engine
// answers it with types it makes up.
type typeShapes interface {
	// signature describes the type ty.
	signature(ty TypeID) signature
	// fields returns the types of the fields of the constructor key of ty, a type
	// whose signature is sigFinite. The caller must not change the slice.
	fields(ty TypeID, key int64) []TypeID
	// describe returns how a witness writes the constructor key of ty.
	describe(ty TypeID, key int64) ctorShape
}

// dkind is what a dpat is.
type dkind uint8

const (
	dWild dkind = iota // `_` or a variable: it matches everything
	dCtor              // a constructor and its sub-patterns, or an Int literal
	dOr                // alternatives: it matches what any of them does
)

// dpat is a pattern as the matrix sees it: what the checker has made of the arm's
// syntax, with the names, the spans and the guards gone. A dCtor has the key of its
// constructor (the tag of a variant, 0 for a tuple or a record, 0 and 1 for
// `false` and `true`, the number itself for an Int literal) and one sub-pattern
// for each field, in the order of the fields, a wildcard for a field that a record
// pattern leaves out.
type dpat struct {
	kind dkind
	key  int64
	args []*dpat // dCtor: the fields; dOr: the alternatives
	live int     // dCtor: how many of args are not wildcards
}

// wildPat is the one wildcard. A dpat is never changed after it is made, so it is
// shared.
var wildPat = &dpat{kind: dWild}

// ctorPat makes the constructor pattern of the constructor key with sub-patterns args.
func ctorPat(key int64, args []*dpat) *dpat {
	d := &dpat{kind: dCtor, key: key, args: args}
	for _, a := range args {
		d.live += a.liveBit()
	}
	return d
}

// orPat makes an or-pattern of alts.
func orPat(alts []*dpat) *dpat { return &dpat{kind: dOr, args: alts} }

// liveBit is 1 if d is not a wildcard, and 0 if it is.
func (d *dpat) liveBit() int {
	if d.kind == dWild {
		return 0
	}
	return 1
}

// cell is a cell of a row: a pattern and the rest of the row. Rows share their
// tails, which is why nothing writes into a cell once it is made.
type cell struct {
	pat  *dpat
	next *cell
}

// row is a row of the matrix: its first cell, and how many of its cells are not
// wildcards. A row with none matches every value, whatever the columns are.
type row struct {
	cells *cell
	live  int
}

// rowOf makes the row of one cell.
func rowOf(p *dpat) row { return row{cells: &cell{pat: p}, live: p.liveBit()} }

// qcell is a cell of the vector q that is being asked about, with the type of the
// column it is in: the rows have the same columns, so one list says it for all.
type qcell struct {
	pat  *dpat
	ty   TypeID
	next *qcell
}

// pushArgs returns the cells of args in front of tail.
func pushArgs(args []*dpat, tail *cell) *cell {
	for i := len(args) - 1; i >= 0; i-- {
		tail = &cell{pat: args[i], next: tail}
	}
	return tail
}

// pushWilds returns n wildcards in front of tail.
func pushWilds(n int, tail *cell) *cell {
	for ; n > 0; n-- {
		tail = &cell{pat: wildPat, next: tail}
	}
	return tail
}

// pushQ returns the cells of args, with the types tys of their columns, in front
// of tail.
func pushQ(args []*dpat, tys []TypeID, tail *qcell) *qcell {
	for i := len(args) - 1; i >= 0; i-- {
		tail = &qcell{pat: args[i], ty: tys[i], next: tail}
	}
	return tail
}

// pushWildQ returns a wildcard for each of the types tys in front of tail.
func pushWildQ(tys []TypeID, tail *qcell) *qcell {
	for i := len(tys) - 1; i >= 0; i-- {
		tail = &qcell{pat: wildPat, ty: tys[i], next: tail}
	}
	return tail
}

// specialized returns the row r without its first cell, which is replaced by the
// sub-patterns of h, the constructor pattern that the cell holds, or by arity
// wildcards if the cell is a wildcard (h is then nil): the row as the
// specialization by one constructor sees it.
func specialized(r row, h *dpat, arity int) row {
	if h == nil {
		return row{cells: pushWilds(arity, r.cells.next), live: r.live}
	}
	return row{cells: pushArgs(h.args, r.cells.next), live: r.live - 1 + h.live}
}

// eachAlternative calls yield with a row for each alternative of the or-pattern at
// the head of r, in the order they are written, an or-pattern inside one included:
// the row r with its first cell replaced by the alternative. A head that is not an
// or-pattern is the one alternative. It stops, and returns, when yield returns
// false. It walks the alternatives with a stack of its own, so a nesting of
// or-patterns takes no depth.
func eachAlternative(r row, yield func(row) bool) {
	tail, rest := r.cells.next, r.live-1
	stack := []*dpat{r.cells.pat}
	for len(stack) > 0 {
		p := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if p.kind == dOr {
			for i := len(p.args) - 1; i >= 0; i-- {
				stack = append(stack, p.args[i])
			}
			continue
		}
		if !yield(row{cells: &cell{pat: p, next: tail}, live: rest + p.liveBit()}) {
			return
		}
	}
}

// decKind says what the search chose for one column of a witness.
type decKind uint8

const (
	decWild    decKind = iota // `_`: no row names a constructor in the column
	decCtor                   // the constructor key of the type ty, whose rows the search specialized
	decAbsent                 // the Int literal key, which no row names
	decMissing                // any constructor of ty that seen does not have: no row names it
)

// decision is one choice of a path of the search, the last of a linked list that
// goes back to the root. The witness at the end of a path is read from it.
type decision struct {
	prev *decision
	kind decKind
	ty   TypeID
	key  int64
	seen base.BitSet // decMissing: the constructors that the rows do name
}

// leaf is where the search found a vector that no row matches.
type leaf struct{ path *decision }

// node is one subproblem: the rows and the vector q that are left, which have the
// same columns, and the decisions that led to it. A node whose q is nil has no
// column left.
type node struct {
	rows []row
	q    *qcell
	path *decision
}

// engine runs the search for one `match` or `let`. Its budget is the number of
// steps it may take; a step is a node of the search, a row that a node builds for
// another, or a cell that it pushes.
type engine struct {
	shapes typeShapes
	budget int
	steps  int
	// intWitness has one entry for each witness that the last call of witnesses
	// wrote, true if that witness has an `Int` literal in it: the number is one that
	// no arm reaching its place names, and the next number is another, so the `match`
	// is not completed by adding that arm. A message tells that only about the
	// witnesses that it shows, so it asks about the first ones (see intInFirst).
	intWitness []bool
}

// intInFirst says whether one of the first n witnesses that the last call of
// witnesses wrote has an `Int` literal in it.
func (e *engine) intInFirst(n int) bool {
	return slices.Contains(e.intWitness[:min(n, len(e.intWitness))], true)
}

// spend counts n more steps, and reports whether the budget still holds. It is
// called before the work it counts, so that a node that would build a million rows
// is refused and does not build them first.
func (e *engine) spend(n int) bool {
	e.steps += n
	return e.steps <= e.budget
}

// search looks for up to want leaves from the node start, in the order of the
// constructors, and reports false if the budget ran out first. With witness the
// paths are recorded, which costs an allocation for each column; without, a leaf
// only says that a vector is useful.
func (e *engine) search(start node, witness bool, want int) ([]leaf, bool) {
	var leaves []leaf
	stack := []node{start}
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if !e.spend(1) {
			return nil, false
		}
		if len(n.rows) == 0 {
			// No row is left that could match a value of the vector: every value
			// that it describes is one that no row matches.
			leaves = append(leaves, leaf{path: n.path})
			if len(leaves) == want {
				return leaves, true
			}
			continue
		}
		if n.q == nil {
			// Rows with no column left match everything: nothing to find here.
			continue
		}
		var ok bool
		if stack, ok = e.expand(stack, n, witness); !ok {
			return nil, false
		}
	}
	return leaves, true
}

// expand does one step on the node n, which has rows, and pushes the nodes that
// come out of it. It returns false when the budget ran out. A node is pushed only
// if no row of it is made of wildcards only: that row matches everything the node
// describes, so there is no leaf in it.
func (e *engine) expand(stack []node, n node, witness bool) ([]node, bool) {
	rows, dead, ok := e.splitOr(n.rows)
	if !ok {
		return stack, false
	}
	if dead {
		return stack, true
	}
	head := n.q.pat
	switch head.kind {
	case dOr:
		// The vector is useful if one of its alternatives is.
		if !e.spend(len(head.args)) {
			return stack, false
		}
		for i := len(head.args) - 1; i >= 0; i-- {
			stack = append(stack, node{rows: rows, q: &qcell{pat: head.args[i], ty: n.q.ty, next: n.q.next}, path: n.path})
		}
		return stack, true
	case dCtor:
		return e.expandCtor(stack, n, rows, head)
	default:
		return e.expandWild(stack, n, rows, witness)
	}
}

// splitOr returns rows with each row whose first cell is an or-pattern replaced by
// one row for each alternative. It says whether a row of the result is made of
// wildcards only (an alternative that is `_`), and false in the third place if the
// budget ran out. When none of the rows starts with an or-pattern they are
// returned as they came, with no copy.
func (e *engine) splitOr(rows []row) (out []row, dead, ok bool) {
	first := slices.IndexFunc(rows, func(r row) bool { return r.cells.pat.kind == dOr })
	if first < 0 {
		return rows, false, true
	}
	out = slices.Clone(rows[:first])
	for _, r := range rows[first:] {
		if r.cells.pat.kind != dOr {
			out = append(out, r)
			continue
		}
		exhausted := false
		eachAlternative(r, func(a row) bool {
			if !e.spend(1) {
				exhausted = true
				return false
			}
			if a.live == 0 {
				dead = true
				return false
			}
			out = append(out, a)
			return true
		})
		if exhausted {
			return nil, false, false
		}
		if dead {
			return nil, true, true
		}
	}
	return out, false, true
}

// expandCtor is expand for a vector whose first cell is the constructor pattern
// head: the rows that start with the same constructor or with a wildcard,
// specialized, and the sub-patterns of head in front of the rest of the vector.
func (e *engine) expandCtor(stack []node, n node, rows []row, head *dpat) ([]node, bool) {
	ty := n.q.ty
	arity := len(head.args)
	var child []row
	for _, r := range rows {
		h := r.cells.pat
		var nr row
		switch {
		case h.kind == dWild:
			if !e.spend(1 + arity) {
				return stack, false
			}
			nr = specialized(r, nil, arity)
		case h.key == head.key:
			if !e.spend(1 + len(h.args)) {
				return stack, false
			}
			nr = specialized(r, h, 0)
		default:
			continue
		}
		if nr.live == 0 {
			return stack, true
		}
		child = append(child, nr)
	}
	if !e.spend(arity) {
		return stack, false
	}
	var tys []TypeID
	if arity > 0 {
		tys = e.shapes.fields(ty, head.key)
	}
	q := pushQ(head.args, tys, n.q.next)
	return append(stack, node{rows: child, q: q, path: n.path}), true
}

// expandWild is expand for a vector whose first cell is a wildcard. If the rows
// name every constructor of the type, the search goes on for each of them, with
// the rows specialized by it; if not, it goes on with the rows that start with a
// wildcard and the first cell dropped (Maranget's default matrix), and the witness
// is a constructor that is missing.
func (e *engine) expandWild(stack []node, n node, rows []row, witness bool) ([]node, bool) {
	ty := n.q.ty
	sig := e.shapes.signature(ty)
	var seen base.BitSet
	if sig.kind == sigFinite {
		seen = base.NewBitSet(sig.n)
	}
	var wild []row
	var ints []int64
	anyCtor := false
	for _, r := range rows {
		h := r.cells.pat
		switch {
		case h.kind == dWild:
			wild = append(wild, row{cells: r.cells.next, live: r.live})
		case sig.kind == sigFinite:
			anyCtor = true
			seen.Set(int(h.key))
		default:
			anyCtor = true
			if witness {
				ints = append(ints, h.key)
			}
		}
	}
	if sig.kind != sigFinite || seen.Count() != sig.n {
		if !e.spend(len(wild)) {
			return stack, false
		}
		path := n.path
		if witness {
			switch {
			case !anyCtor || sig.kind == sigOpaque:
				path = &decision{prev: path, kind: decWild}
			case sig.kind == sigFinite:
				path = &decision{prev: path, kind: decMissing, ty: ty, seen: seen}
			default:
				path = &decision{prev: path, kind: decAbsent, key: smallestAbsent(ints)}
			}
		}
		return append(stack, node{rows: wild, q: n.q.next, path: path}), true
	}
	// The signature is complete: one node for each constructor, with the rows
	// that start with it and those that start with a wildcard, in one pass.
	buckets := make([][]row, sig.n)
	dead := make([]bool, sig.n)
	for _, r := range rows {
		h := r.cells.pat
		if h.kind != dWild {
			c := int(h.key)
			if dead[c] {
				continue
			}
			if !e.spend(1 + len(h.args)) {
				return stack, false
			}
			if nr := specialized(r, h, 0); nr.live == 0 {
				dead[c], buckets[c] = true, nil
			} else {
				buckets[c] = append(buckets[c], nr)
			}
			continue
		}
		for c := range buckets {
			if dead[c] {
				continue
			}
			arity := len(e.shapes.fields(ty, int64(c)))
			if !e.spend(1 + arity) {
				return stack, false
			}
			buckets[c] = append(buckets[c], specialized(r, nil, arity))
		}
	}
	for c := sig.n - 1; c >= 0; c-- {
		if dead[c] {
			continue
		}
		tys := e.shapes.fields(ty, int64(c))
		if !e.spend(len(tys)) {
			return stack, false
		}
		path := n.path
		if witness {
			path = &decision{prev: path, kind: decCtor, ty: ty, key: int64(c)}
		}
		stack = append(stack, node{rows: buckets[c], q: pushWildQ(tys, n.q.next), path: path})
	}
	return stack, true
}

// smallestAbsent returns the smallest integer, from 0 up, that is not in ints. It
// sorts ints.
func smallestAbsent(ints []int64) int64 {
	slices.Sort(ints)
	next := int64(0)
	for _, v := range ints {
		if v == next {
			next++
		} else if v > next {
			break
		}
	}
	return next
}

// start searches the matrix rows for up to want leaves of the vector q, a single
// column of type ty, without a prior index: it is the answer for a matrix as it
// is, and the one that armMatrix is checked against. It reports false if the
// budget ran out.
func (e *engine) start(rows []row, q *dpat, ty TypeID, witness bool, want int) ([]leaf, bool) {
	for _, r := range rows {
		if r.live == 0 {
			return nil, true
		}
	}
	if !e.spend(len(rows)) {
		return nil, false
	}
	return e.search(node{rows: rows, q: &qcell{pat: q, ty: ty}}, witness, want)
}

// armMatrix is the matrix of the arms of a `match` that have no guard, as far as
// they have been read, kept so that the question "is the next arm useful?" does
// not look at every arm above it. Its one column is the scrutinee, and the rows are
// indexed by the constructor or the literal that they start with, so the arm
// `7 => …` of a match of a hundred thousand literal arms looks at the rows that
// start with 7, and not at the hundred thousand. A row that starts with a wildcard
// is a row of wildcards only (the matrix has one column): it covers every value, so
// the matrix keeps no more than the fact. The or-patterns at the head of a row are
// split when it is added, once, and not at each question.
//
// The questions that it answers for the first column it answers from the index,
// and hands the rest to the engine: the engine's own search of the same rows gives
// the same answer, only slower (a test checks that it does).
type armMatrix struct {
	e       *engine
	ty      TypeID
	sig     signature
	rows    []row           // every row added, as it was added
	keyed   map[int64][]row // the rows that start with a constructor, by its key
	seen    base.BitSet     // sigFinite: the constructors that keyed has rows for
	covered bool            // a row is made of wildcards only: no arm below is reachable
}

// newArmMatrix returns the empty matrix of a column of type ty.
func newArmMatrix(e *engine, ty TypeID) *armMatrix {
	m := &armMatrix{e: e, ty: ty, sig: e.shapes.signature(ty), keyed: make(map[int64][]row)}
	if m.sig.kind == sigFinite {
		m.seen = base.NewBitSet(m.sig.n)
	}
	return m
}

// add appends the arm p to the matrix. It reports false if the budget ran out.
func (m *armMatrix) add(p *dpat) bool {
	r := rowOf(p)
	m.rows = append(m.rows, r)
	ok := true
	eachAlternative(r, func(a row) bool {
		if !m.e.spend(1) {
			ok = false
			return false
		}
		h := a.cells.pat
		if h.kind == dWild {
			m.covered = true
			return true
		}
		m.keyed[h.key] = append(m.keyed[h.key], a)
		if m.sig.kind == sigFinite {
			m.seen.Set(int(h.key))
		}
		return true
	})
	return ok
}

// useful reports whether the arm p, which is the next one, matches a value that
// no arm added so far matches, and false in the second place if the budget ran
// out.
func (m *armMatrix) useful(p *dpat) (useful, ok bool) {
	if m.covered {
		return false, true
	}
	// An or-pattern is useful if one of its alternatives is.
	stack := []*dpat{p}
	for len(stack) > 0 {
		q := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if q.kind == dOr {
			for i := len(q.args) - 1; i >= 0; i-- {
				stack = append(stack, q.args[i])
			}
			continue
		}
		found, ok := m.usefulAlternative(q)
		if !ok {
			return false, false
		}
		if found {
			return true, true
		}
	}
	return false, true
}

// usefulAlternative is useful for a pattern that is not an or-pattern.
func (m *armMatrix) usefulAlternative(q *dpat) (useful, ok bool) {
	e := m.e
	if q.kind == dCtor {
		return m.usefulFor(q.key, q.args, e.fieldsOf(m.ty, q))
	}
	if m.sig.kind != sigFinite || m.seen.Count() != m.sig.n {
		// Not every constructor is named, so a value built with one that no row
		// names is matched by nothing above: a wildcard is useful.
		return true, true
	}
	for c := range m.sig.n {
		tys := e.shapes.fields(m.ty, int64(c))
		wilds := make([]*dpat, len(tys))
		for i := range wilds {
			wilds[i] = wildPat
		}
		if found, ok := m.usefulFor(int64(c), wilds, tys); !ok || found {
			return found, ok
		}
	}
	return false, true
}

// fieldsOf returns the types of the fields of the constructor pattern q of a column
// of type ty (none for an Int literal).
func (e *engine) fieldsOf(ty TypeID, q *dpat) []TypeID {
	if len(q.args) == 0 {
		return nil
	}
	return e.shapes.fields(ty, q.key)
}

// usefulFor asks whether the vector of the constructor key applied to args, whose
// fields have the types tys, is useful: it specializes the rows that start with
// key and hands them to the engine.
func (m *armMatrix) usefulFor(key int64, args []*dpat, tys []TypeID) (useful, ok bool) {
	e := m.e
	keyed := m.keyed[key]
	if !e.spend(len(keyed) + len(args)) {
		return false, false
	}
	child := make([]row, 0, len(keyed))
	for _, r := range keyed {
		nr := specialized(r, r.cells.pat, 0)
		if nr.live == 0 {
			// An arm above is this very constructor with sub-patterns that match
			// all of its fields.
			return false, true
		}
		child = append(child, nr)
	}
	leaves, ok := e.search(node{rows: child, q: pushQ(args, tys, nil)}, false, 1)
	if !ok {
		return false, false
	}
	return len(leaves) > 0, true
}

// missing returns up to want witnesses, as text, of the values that no arm added
// so far matches (none if the arms cover the type), and false in the second place
// if the budget ran out.
func (m *armMatrix) missing(want int) (witnesses []string, ok bool) {
	if m.covered {
		return nil, true
	}
	leaves, ok := m.e.start(m.rows, wildPat, m.ty, true, want)
	if !ok {
		return nil, false
	}
	return m.e.witnesses(leaves, want), true
}

// pickKind says what fills one place of a witness.
type pickKind uint8

const (
	pickWild    pickKind = iota // `_`
	pickCtor                    // a constructor of ty, applied to the places that follow
	pickMissing                 // a constructor of ty that no row names, applied to wildcards
	pickInt                     // an Int literal
)

// pick is what the witness has in one place.
type pick struct {
	kind pickKind
	ty   TypeID
	key  int64
}

// picks returns what the decision d can put in its place: one thing, or, for a
// constructor that no row names, each of the constructors that are missing, up to
// limit. A constructor that the search specialized has its fields after it, the
// places that the next decisions fill; a missing one has wildcards for its fields,
// because the search did not look inside it, and the decisions after it are for
// the places that follow it.
func (d *decision) picks(limit int) []pick {
	switch d.kind {
	case decCtor:
		return []pick{{kind: pickCtor, ty: d.ty, key: d.key}}
	case decAbsent:
		return []pick{{kind: pickInt, key: d.key}}
	case decMissing:
		var out []pick
		for i, ok := d.seen.NextClear(0); ok && len(out) < limit; i, ok = d.seen.NextClear(i + 1) {
			out = append(out, pick{kind: pickMissing, ty: d.ty, key: int64(i)})
		}
		return out
	}
	return []pick{{kind: pickWild}}
}

// witnesses writes the witnesses of leaves, up to limit of them, in the order of
// the leaves. A leaf is one path, and a column where the rows name some
// constructors and not all has one witness for each constructor that is missing,
// so a leaf gives the product of those choices, the first column changing the
// slowest. The number of witnesses is never counted: it is the number found, up to
// limit, which is why a caller that wants to say "and more" asks for one more than
// it shows.
func (e *engine) witnesses(leaves []leaf, limit int) []string {
	var out []string
	e.intWitness = e.intWitness[:0]
	for _, l := range leaves {
		var decs []*decision
		for d := l.path; d != nil; d = d.prev {
			decs = append(decs, d)
		}
		slices.Reverse(decs)
		options := make([][]pick, len(decs))
		for i, d := range decs {
			options[i] = d.picks(limit - len(out))
		}
		chosen := make([]int, len(decs))
		picks := make([]pick, len(decs))
		for {
			for i := range decs {
				picks[i] = options[i][chosen[i]]
			}
			out = append(out, e.writeWitness(picks))
			e.intWitness = append(e.intWitness, slices.ContainsFunc(picks, func(p pick) bool { return p.kind == pickInt }))
			if len(out) == limit {
				return out
			}
			// The next combination: advance the last place that has another choice.
			i := len(decs) - 1
			for ; i >= 0; i-- {
				if chosen[i]++; chosen[i] < len(options[i]) {
					break
				}
				chosen[i] = 0
			}
			if i < 0 {
				break
			}
		}
	}
	return out
}

// writeFrame is a constructor of a witness that is being written, whose
// places are not all written yet.
type writeFrame struct {
	shape     ctorShape
	remaining int  // the places that are not written yet
	index     int  // the next place
	wrote     bool // formRecord: a field has been written
	elided    bool // formRecord: a field was a wildcard and was left out
}

// writeWitness writes the witness whose places are filled by picks, in the order
// of a walk that visits a constructor and then its fields, as the search chose
// them; the places that picks does not reach are wildcards. It uses a stack of
// its own and not recursion: a witness is as deep as the pattern it comes from.
func (e *engine) writeWitness(picks []pick) string {
	var sb strings.Builder
	var stack []writeFrame
	// settle closes the constructors whose places are all written.
	settle := func() {
		for len(stack) > 0 && stack[len(stack)-1].remaining == 0 {
			top := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			switch {
			case top.shape.form == formRecord && top.elided && top.wrote:
				sb.WriteString(", .. }")
			case top.shape.form == formRecord && top.elided:
				sb.WriteString(" .. }")
			case top.shape.form == formRecord:
				sb.WriteString(" }")
			default:
				sb.WriteString(")")
			}
		}
	}
	place := func(p pick) {
		if len(stack) > 0 {
			parent := &stack[len(stack)-1]
			index := parent.index
			parent.index++
			parent.remaining--
			if parent.shape.form == formRecord {
				if p.kind == pickWild {
					parent.elided = true
					settle()
					return
				}
				if parent.wrote {
					sb.WriteString(", ")
				} else {
					sb.WriteString(" ")
				}
				sb.WriteString(parent.shape.fields[index] + ": ")
				parent.wrote = true
			} else if index > 0 {
				sb.WriteString(", ")
			}
		}
		switch p.kind {
		case pickWild:
			sb.WriteString("_")
		case pickInt:
			sb.WriteString(strconv.FormatInt(p.key, 10))
		default:
			shape := e.shapes.describe(p.ty, p.key)
			if shape.arity == 0 {
				sb.WriteString(shape.name)
				break
			}
			if p.kind == pickMissing {
				sb.WriteString(shape.name + "(" + strings.Repeat("_, ", shape.arity-1) + "_)")
				break
			}
			if shape.form == formRecord {
				sb.WriteString(shape.name + " {")
			} else {
				sb.WriteString(shape.name + "(")
			}
			stack = append(stack, writeFrame{shape: shape, remaining: shape.arity})
			return
		}
		settle()
	}
	if len(picks) == 0 {
		return "_"
	}
	for _, p := range picks {
		place(p)
	}
	for len(stack) > 0 {
		place(pick{kind: pickWild})
	}
	return sb.String()
}
