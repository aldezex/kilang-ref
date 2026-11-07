package sema

import (
	"math/rand/v2"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// The tests here are about the engine of the pattern check (pattern_matrix.go) on its
// own, with types that this file makes up: a Bool, an Int, a sum whose third
// constructor holds a sum again, a pair, a record. Whether an arm is useful and which
// values no arm matches are questions that have a plain answer, which is to try every
// value, and the random matrices below are compared with it.

// The types of the fake universe.
const (
	tyBool TypeID = iota + 1
	tyInt
	tySum  // A, B(Bool), C(Sum, Bool)
	tyPair // (Bool, Sum)
	tyRec  // R { x: Int, y: Bool }
	tyFlip // (Sum, Bool): the pair the other way round, so that a column of constructors with fields has a column after it
)

// fakeShapes is the typeShapes of the fake universe.
type fakeShapes struct{}

func (fakeShapes) signature(ty TypeID) signature {
	switch ty {
	case tyBool:
		return signature{kind: sigFinite, n: 2}
	case tyInt:
		return signature{kind: sigInt}
	case tySum:
		return signature{kind: sigFinite, n: 3}
	case tyPair, tyRec, tyFlip:
		return signature{kind: sigFinite, n: 1}
	}
	return signature{}
}

func (fakeShapes) fields(ty TypeID, key int64) []TypeID {
	switch ty {
	case tySum:
		return [][]TypeID{nil, {tyBool}, {tySum, tyBool}}[key]
	case tyPair:
		return []TypeID{tyBool, tySum}
	case tyRec:
		return []TypeID{tyInt, tyBool}
	case tyFlip:
		return []TypeID{tySum, tyBool}
	}
	return nil
}

func (fakeShapes) describe(ty TypeID, key int64) ctorShape {
	switch ty {
	case tyBool:
		return ctorShape{name: []string{"false", "true"}[key]}
	case tySum:
		return ctorShape{name: []string{"A", "B", "C"}[key], arity: []int{0, 1, 2}[key]}
	case tyPair, tyFlip:
		return ctorShape{arity: 2}
	}
	return ctorShape{name: "R", form: formRecord, fields: []string{"x", "y"}, arity: 2}
}

// val is a value of the fake universe: the constructor, or the integer, and its fields.
type val struct {
	key  int64
	args []val
}

// values returns every value of the type ty in which a sum holds at most depth more
// sums, and the integers 0 to 3 (the arms of the tests name 0 to 2, so 3 stands for
// the number that no arm names).
func values(ty TypeID, depth int) []val {
	switch ty {
	case tyBool:
		return []val{{key: 0}, {key: 1}}
	case tyInt:
		return []val{{key: 0}, {key: 1}, {key: 2}, {key: 3}}
	case tySum:
		out := []val{{key: 0}}
		for _, b := range values(tyBool, 0) {
			out = append(out, val{key: 1, args: []val{b}})
		}
		if depth > 0 {
			for _, s := range values(tySum, depth-1) {
				for _, b := range values(tyBool, 0) {
					out = append(out, val{key: 2, args: []val{s, b}})
				}
			}
		}
		return out
	case tyPair:
		var out []val
		for _, b := range values(tyBool, 0) {
			for _, s := range values(tySum, depth) {
				out = append(out, val{args: []val{b, s}})
			}
		}
		return out
	case tyFlip:
		var out []val
		for _, s := range values(tySum, depth) {
			for _, b := range values(tyBool, 0) {
				out = append(out, val{args: []val{s, b}})
			}
		}
		return out
	}
	var out []val
	for _, x := range values(tyInt, 0) {
		for _, y := range values(tyBool, 0) {
			out = append(out, val{args: []val{x, y}})
		}
	}
	return out
}

// matches reports whether the value v is one that the pattern p matches.
func matches(p *dpat, v val) bool {
	switch p.kind {
	case dWild:
		return true
	case dOr:
		return slices.ContainsFunc(p.args, func(a *dpat) bool { return matches(a, v) })
	}
	if p.key != v.key {
		return false
	}
	for i, a := range p.args {
		if !matches(a, v.args[i]) {
			return false
		}
	}
	return true
}

// anyMatches reports whether one of the patterns matches v.
func anyMatches(pats []*dpat, v val) bool {
	return slices.ContainsFunc(pats, func(p *dpat) bool { return matches(p, v) })
}

// randomPat draws a pattern for a value of the type ty that has at most depth levels
// of constructors: a wildcard, a constructor with patterns for its fields, an
// integer from 0 to 2, or an or-pattern of two or three of these.
func randomPat(r *rand.Rand, ty TypeID, depth int) *dpat {
	n := r.IntN(10)
	switch {
	case n < 3 || depth == 0 && n < 8:
		return wildPat
	case n >= 8:
		alts := make([]*dpat, 2+r.IntN(2))
		for i := range alts {
			alts[i] = randomPat(r, ty, depth)
		}
		return orPat(alts)
	}
	sig := fakeShapes{}.signature(ty)
	if sig.kind == sigInt {
		return ctorPat(int64(r.IntN(3)), nil)
	}
	key := int64(r.IntN(sig.n))
	tys := fakeShapes{}.fields(ty, key)
	args := make([]*dpat, len(tys))
	for i, ft := range tys {
		args[i] = randomPat(r, ft, depth-1)
	}
	return ctorPat(key, args)
}

// parseWitness reads the text of a witness of the type ty as the pattern that it
// writes, so that the values it describes can be tried: a constructor and its fields
// in parentheses, a tuple, a record whose fields that are not written are wildcards,
// an integer, `_`.
func parseWitness(t *testing.T, text string, ty TypeID) *dpat {
	t.Helper()
	pos := 0
	skip := func() {
		for pos < len(text) && text[pos] == ' ' {
			pos++
		}
	}
	word := func() string {
		skip()
		start := pos
		for pos < len(text) && (text[pos] == '_' || text[pos] == '-' || text[pos] >= '0' && text[pos] <= '9' || text[pos] >= 'a' && text[pos] <= 'z' || text[pos] >= 'A' && text[pos] <= 'Z') {
			pos++
		}
		return text[start:pos]
	}
	expect := func(s string) {
		skip()
		if !strings.HasPrefix(text[pos:], s) {
			t.Fatalf("the witness %q has %q at %d, want %q", text, text[pos:], pos, s)
		}
		pos += len(s)
	}
	var parse func(ty TypeID) *dpat
	parse = func(ty TypeID) *dpat {
		skip()
		if pos < len(text) && text[pos] == '_' {
			pos++
			return wildPat
		}
		switch ty {
		case tyInt:
			n, err := strconv.Atoi(word())
			if err != nil {
				t.Fatalf("the witness %q has no number at %d", text, pos)
			}
			return ctorPat(int64(n), nil)
		case tyBool:
			return ctorPat(int64(slices.Index([]string{"false", "true"}, word())), nil)
		case tySum:
			key := slices.Index([]string{"A", "B", "C"}, word())
			tys := fakeShapes{}.fields(tySum, int64(key))
			args := make([]*dpat, len(tys))
			if len(tys) > 0 {
				expect("(")
			}
			for i, ft := range tys {
				if i > 0 {
					expect(",")
				}
				args[i] = parse(ft)
			}
			if len(tys) > 0 {
				expect(")")
			}
			return ctorPat(int64(key), args)
		case tyPair:
			expect("(")
			b := parse(tyBool)
			expect(",")
			s := parse(tySum)
			expect(")")
			return ctorPat(0, []*dpat{b, s})
		case tyFlip:
			expect("(")
			s := parse(tySum)
			expect(",")
			b := parse(tyBool)
			expect(")")
			return ctorPat(0, []*dpat{s, b})
		}
		expect("R {")
		args := []*dpat{wildPat, wildPat}
		for {
			skip()
			if strings.HasPrefix(text[pos:], "}") {
				pos++
				return ctorPat(0, args)
			}
			if strings.HasPrefix(text[pos:], "..") {
				pos += 2
				continue
			}
			if strings.HasPrefix(text[pos:], ",") {
				pos++
				continue
			}
			name := word()
			expect(":")
			args[slices.Index([]string{"x", "y"}, name)] = parse(fakeShapes{}.fields(tyRec, 0)[slices.Index([]string{"x", "y"}, name)])
		}
	}
	p := parse(ty)
	if skip(); pos != len(text) {
		t.Fatalf("the witness %q has %q left over", text, text[pos:])
	}
	return p
}

// patText writes a pattern for a failure message.
func patText(p *dpat) string {
	switch p.kind {
	case dWild:
		return "_"
	case dOr:
		alts := make([]string, len(p.args))
		for i, a := range p.args {
			alts[i] = patText(a)
		}
		return "(" + strings.Join(alts, " | ") + ")"
	}
	if len(p.args) == 0 {
		return strconv.FormatInt(p.key, 10)
	}
	args := make([]string, len(p.args))
	for i, a := range p.args {
		args[i] = patText(a)
	}
	return strconv.FormatInt(p.key, 10) + "(" + strings.Join(args, ", ") + ")"
}

func TestMatrix(t *testing.T) {
	const depth = 3 // the depth of the values: the patterns have two levels at most
	columns := []TypeID{tyBool, tyInt, tySum, tyPair, tyRec, tyFlip}

	t.Run("agrees_with_trying_every_value", func(t *testing.T) {
		// For random matrices of one to four rows over each type of the universe, the
		// engine says what trying every value says: whether a vector is useful (a value
		// that it matches and no row does), by the search alone and by the index of an
		// armMatrix that has the rows added one at a time; whether some value is left
		// out by every row; and that each witness it writes is sound: some value is
		// described by it, and no row matches any value that it describes.
		r := rand.New(rand.NewPCG(1, 2))
		for round := range 4000 {
			ty := columns[round%len(columns)]
			rows := make([]*dpat, 1+r.IntN(4))
			for i := range rows {
				rows[i] = randomPat(r, ty, 2)
			}
			q := randomPat(r, ty, 2)
			universe := values(ty, depth)
			fail := func(format string, args ...any) {
				t.Helper()
				var shown []string
				for _, p := range rows {
					shown = append(shown, patText(p))
				}
				t.Fatalf("type %d, rows %v, vector %s: "+format, append([]any{ty, shown, patText(q)}, args...)...)
			}

			wantUseful := slices.ContainsFunc(universe, func(v val) bool { return matches(q, v) && !anyMatches(rows, v) })
			matrix := make([]row, len(rows))
			for i, p := range rows {
				matrix[i] = rowOf(p)
			}
			e := &engine{shapes: fakeShapes{}, budget: 1 << 40}
			leaves, ok := e.start(matrix, q, ty, false, 1)
			if !ok || (len(leaves) > 0) != wantUseful {
				fail("the search says useful %v (in budget %v), trying every value says %v", len(leaves) > 0, ok, wantUseful)
			}
			am := newArmMatrix(&engine{shapes: fakeShapes{}, budget: 1 << 40}, ty)
			for i, p := range rows {
				earlier := rows[:i]
				want := slices.ContainsFunc(universe, func(v val) bool { return matches(p, v) && !anyMatches(earlier, v) })
				if got, ok := am.useful(p); !ok || got != want {
					fail("the arm %d, %s, is useful: the index says %v, trying every value says %v", i, patText(p), got, want)
				}
				am.add(p)
			}
			if got, ok := am.useful(q); !ok || got != wantUseful {
				fail("the index says useful %v, trying every value says %v", got, wantUseful)
			}

			uncovered := slices.ContainsFunc(universe, func(v val) bool { return !anyMatches(rows, v) })
			witnesses, ok := am.missing(4)
			if !ok || (len(witnesses) > 0) != uncovered {
				fail("the witnesses are %v (in budget %v), trying every value says that some value is left out: %v", witnesses, ok, uncovered)
			}
			for _, text := range witnesses {
				w := parseWitness(t, text, ty)
				var described bool
				for _, v := range universe {
					if !matches(w, v) {
						continue
					}
					described = true
					if anyMatches(rows, v) {
						fail("the witness %q describes a value that a row matches", text)
					}
				}
				if !described {
					fail("the witness %q describes no value", text)
				}
			}
		}
	})

	t.Run("the_budget_holds_to_the_last_step", func(t *testing.T) {
		// A search counts its steps, and gives up when they are more than the budget,
		// not when they are as many: run without a limit it takes some number of
		// steps, a budget of exactly that many is enough, and one less is not. This is
		// so for each way in: the search of a matrix for witnesses, the search for a
		// vector that is useful, and the index of arms with its three questions.
		pair := func(b, s *dpat) *dpat { return ctorPat(0, []*dpat{b, s}) }
		sumB := func(b *dpat) *dpat { return ctorPat(1, []*dpat{b}) }
		sumC := ctorPat(2, []*dpat{wildPat, wildPat})
		tru, fls := ctorPat(1, nil), ctorPat(0, nil)
		rows := []*dpat{pair(tru, ctorPat(0, nil)), pair(fls, sumB(wildPat)), pair(orPat([]*dpat{tru, fls}), sumC), pair(wildPat, sumB(tru))}
		q := pair(orPat([]*dpat{tru, fls}), wildPat)
		matrix := make([]row, len(rows))
		for i, p := range rows {
			matrix[i] = rowOf(p)
		}
		ways := []struct {
			name string
			run  func(e *engine) bool
		}{
			{"witnesses", func(e *engine) bool { _, ok := e.start(matrix, wildPat, tyPair, true, 4); return ok }},
			{"useful", func(e *engine) bool { _, ok := e.start(matrix, q, tyPair, false, 1); return ok }},
			{"index", func(e *engine) bool {
				am := newArmMatrix(e, tyPair)
				for _, p := range rows {
					if _, ok := am.useful(p); !ok || !am.add(p) {
						return false
					}
				}
				if _, ok := am.useful(q); !ok {
					return false
				}
				_, ok := am.missing(4)
				return ok
			}},
		}
		for _, way := range ways {
			free := &engine{shapes: fakeShapes{}, budget: 1 << 40}
			if !way.run(free) || free.steps < 3 {
				t.Fatalf("%s: %d steps without a limit (done: %v), want a search with some steps", way.name, free.steps, true)
			}
			if e := (&engine{shapes: fakeShapes{}, budget: free.steps}); !way.run(e) {
				t.Errorf("%s: a budget of %d, the steps it takes, is not enough", way.name, free.steps)
			}
			// Any budget below the steps it takes runs out, wherever the step that
			// runs out falls: in the search, or in what a node builds for the next.
			for budget := range free.steps {
				if e := (&engine{shapes: fakeShapes{}, budget: budget}); way.run(e) {
					t.Errorf("%s: a budget of %d, below the %d steps it takes, is enough", way.name, budget, free.steps)
					break
				}
			}
		}
	})

	t.Run("a_step_is_a_node_a_row_or_a_cell", func(t *testing.T) {
		// What the budget counts: a node of the search, a row built for another node
		// and a cell pushed on a row, one step each, and the rows that the search
		// starts with. The counts of these eight small matrices were made by hand,
		// following the search node by node: a column that the rows do not complete
		// goes on with the default matrix (3 steps), one they complete builds a row for
		// each constructor (5), a constructor in the vector builds the rows that fit it
		// and pushes its fields (7), an or-pattern becomes a row for each alternative
		// (6), a tuple pushes its two fields (9), a wildcard row is built for a
		// constructor of the vector (11), and a wildcard row is built for each
		// constructor of a complete column (24, with two leaves). The last one is a
		// wildcard row in a complete column whose constructors have fields: it is built
		// for each of A, B and C with as many wildcards as the constructor has fields
		// (1 + 0, 1 + 1 and 1 + 2 steps), and the cells that follow stay in line. The
		// 53 steps: 4 rows to start; the node of the pair, 1, with a row built for each
		// of the 4 rows (1 + 2 each) and 2 cells, 19 so far; the node of the Sum
		// column, 1, with the rows built for A, B and C (1, 2 and 3) and the wildcard
		// row for each (1, 2 and 3) and the 3 cells of the nodes it pushes, 35; the node
		// of A, 3, where the Bool column is covered; the node of B, 6, and the one of C,
		// 9, where the wildcard row goes through the columns it has wildcards in first.
		tru, fls, a := ctorPat(1, nil), ctorPat(0, nil), ctorPat(0, nil)
		sumB := func(b *dpat) *dpat { return ctorPat(1, []*dpat{b}) }
		sumC := ctorPat(2, []*dpat{wildPat, wildPat})
		pair := func(b, s *dpat) *dpat { return ctorPat(0, []*dpat{b, s}) }
		flip := func(s, b *dpat) *dpat { return ctorPat(0, []*dpat{s, b}) }
		for _, tc := range []struct {
			name    string
			rows    []*dpat
			q       *dpat
			ty      TypeID
			witness bool
			steps   int
			leaves  int
		}{
			{"incomplete", []*dpat{tru}, wildPat, tyBool, false, 3, 1},
			{"complete", []*dpat{tru, fls}, wildPat, tyBool, false, 5, 0},
			{"a constructor and its field", []*dpat{sumB(tru)}, sumB(wildPat), tySum, false, 7, 1},
			{"an or-pattern", []*dpat{orPat([]*dpat{tru, fls})}, wildPat, tyBool, false, 6, 0},
			{"a tuple", []*dpat{pair(tru, a)}, wildPat, tyPair, true, 9, 1},
			{"a wildcard row, a constructor in the vector", []*dpat{pair(wildPat, a)}, pair(tru, a), tyPair, false, 11, 0},
			{"wildcard rows in each bucket", []*dpat{pair(tru, a), pair(fls, a), pair(wildPat, sumB(wildPat))}, wildPat, tyPair, true, 24, 2},
			{"a wildcard row in a column of constructors with fields", []*dpat{flip(a, fls), flip(sumB(wildPat), fls), flip(sumC, fls), flip(wildPat, tru)}, wildPat, tyFlip, true, 53, 0},
		} {
			matrix := make([]row, len(tc.rows))
			for i, p := range tc.rows {
				matrix[i] = rowOf(p)
			}
			e := &engine{shapes: fakeShapes{}, budget: 1 << 40}
			leaves, ok := e.start(matrix, tc.q, tc.ty, tc.witness, 4)
			if !ok || len(leaves) != tc.leaves || e.steps != tc.steps {
				t.Errorf("%s: %d leaves in %d steps (in budget: %v), want %d in %d", tc.name, len(leaves), e.steps, ok, tc.leaves, tc.steps)
			}
			for budget := range tc.steps {
				if _, ok := (&engine{shapes: fakeShapes{}, budget: budget}).start(matrix, tc.q, tc.ty, tc.witness, 4); ok {
					t.Errorf("%s: a budget of %d, below the %d steps it takes, was enough", tc.name, budget, tc.steps)
					break
				}
			}
		}
		// The index of arms counts a step for each row it adds (an or-pattern, one for
		// each alternative) and for each row that a question builds. Two arms, `true` and
		// `false`, are 2 steps; a wildcard asked of the column that they complete builds
		// a row for each constructor (4 steps in all) and finds it useless; and the
		// witnesses of the two arms are their rows, a node and a row for each constructor
		// (9), and none.
		e := &engine{shapes: fakeShapes{}, budget: 1 << 40}
		am := newArmMatrix(e, tyBool)
		am.add(tru)
		am.add(fls)
		steps := []int{e.steps}
		useful, okUseful := am.useful(wildPat)
		steps = append(steps, e.steps)
		witnesses, okMissing := am.missing(4)
		steps = append(steps, e.steps)
		if !okUseful || useful || !okMissing || len(witnesses) != 0 || !slices.Equal(steps, []int{2, 4, 9}) {
			t.Errorf("the index of two arms: a wildcard is useful: %v, witnesses %v, steps %v, want no, none and [2 4 9]", useful, witnesses, steps)
		}
		// A budget of nothing is not enough for a search that starts with a row.
		if _, ok := (&engine{shapes: fakeShapes{}, budget: 0}).start([]row{rowOf(tru)}, wildPat, tyBool, true, 4); ok {
			t.Error("a budget of 0 was enough for a matrix of one row")
		}
	})
}
