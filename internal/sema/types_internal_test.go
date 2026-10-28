package sema

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/aldezex/kilang/internal/base"
	"github.com/aldezex/kilang/internal/testsupport/within"
)

// The tests here ask a TypeTable whether a variable is bound and to what, which
// only the table's own record of the variable (tbl.vars) says. That record is
// not exported, so this file is part of package sema and does not use
// testsupport (which imports driver, and driver imports sema). It imports only
// testsupport/within, a leaf package that imports nothing of the compiler. The
// tests build their types by hand.

// varID returns the variable that the type id stands for. It fails the test if
// the node of id is not a variable.
func varID(t *testing.T, tbl *TypeTable, id TypeID) TypeVarID {
	t.Helper()
	n := tbl.Node(id)
	if n.Kind != TypeVariable {
		t.Fatalf("Node(%d).Kind = %d, want TypeVariable (%d)", id, n.Kind, TypeVariable)
	}
	return n.Var
}

// varOf returns the table's record of the variable that the type id stands for.
func varOf(t *testing.T, tbl *TypeTable, id TypeID) TypeVar {
	t.Helper()
	return tbl.vars.Get(varID(t, tbl, id))
}

// bind binds the variable that the type v stands for to the type to.
func bind(t *testing.T, tbl *TypeTable, v, to TypeID) {
	t.Helper()
	tbl.Bind(varID(t, tbl, v), to)
}

// funMaker returns a shorthand for building function types in tbl with the
// empty effect row: fn(ret, params...) is the function with those parameters
// and that result.
func funMaker(tbl *TypeTable) func(ret TypeID, params ...TypeID) TypeID {
	empty := tbl.EmptyRow()
	return func(ret TypeID, params ...TypeID) TypeID { return tbl.Fun(params, ret, empty) }
}

// namedType is a type id and the words a failure message calls it by.
type namedType struct {
	name string
	id   TypeID
}

// distinctIDs fails the test for every pair of types in all that share an id.
func distinctIDs(t *testing.T, all []namedType) {
	t.Helper()
	for i, a := range all {
		for _, b := range all[i+1:] {
			if a.id == b.id {
				t.Errorf("%s and %s have the same id, %d", a.name, b.name, a.id)
			}
		}
	}
}

// checkFunKeysTellIdsApart makes functions out of many different ids and fails
// the test unless each different pair of parameters and result has its own id
// and each id says what it was made from, and the table holds no node besides
// those. The ids go beyond one byte and have one, two and three decimal
// digits, so that a key that kept only the low byte of an id, or wrote the ids
// as decimal numbers with nothing between them ([1, 12] and [11, 2] both give
// "112"), would give two functions one id. Some ids go beyond two bytes too
// (a key that kept only the low 16 bits would join two ids 65 536 apart); three
// bytes would take 16 million types, which a test cannot make.
func checkFunKeysTellIdsApart(t *testing.T) {
	t.Helper()
	// A fresh table, so that the ids below are known: the builtins and the
	// error type come first and the 300 variables follow.
	tbl := NewTypeTable()
	empty := tbl.EmptyRow()
	pool := []TypeID{tbl.Builtin(BuiltinInt), tbl.Builtin(BuiltinBool), tbl.Builtin(BuiltinUnit), tbl.Error()}
	for range 300 {
		pool = append(pool, tbl.FreshVar(Origin{}))
	}
	if pool[260]-pool[4] != 256 {
		t.Fatalf("the test's own ids are not what it assumes: pool[260]-pool[4] = %d, want 256", pool[260]-pool[4])
	}
	// The ids to combine: the first 44 (they include 1, 2, 11 and 12) and the
	// 4 that are exactly 256 above the ids 4 to 7.
	ids := slices.Concat(pool[:44], pool[260:264])

	// The loop stops at the first clash, and says both sides of it.
	seen := make(map[TypeID]string)
	check := func(params []TypeID, ret TypeID) {
		t.Helper()
		desc := fmt.Sprintf("Fun(%v, %d)", params, ret)
		id := tbl.Fun(params, ret, empty)
		if prev, ok := seen[id]; ok && prev != desc {
			t.Fatalf("%s and %s have the same id, %d", prev, desc, id)
		}
		seen[id] = desc
		if node := tbl.Node(id); !slices.Equal(node.Params, params) || node.Ret != ret {
			t.Fatalf("%s: Node(%d) has params %v and ret %d", desc, id, node.Params, node.Ret)
		}
	}
	for _, a := range ids {
		check([]TypeID{a}, pool[0])
		for _, b := range ids {
			check([]TypeID{a, b}, pool[0])
			check([]TypeID{a}, b)
		}
	}
	// n ids: n functions of one parameter and Int as the result, n*n of two
	// parameters, and n*n of one parameter and a result of their own, minus the
	// n that the first and the third group share (those with Int as the
	// result). Calling Fun twice with the same arguments must not add one, and
	// the node count says so: len(seen) counts the ids that Fun returned, and
	// a Fun that pushed a node before it looked in its map would return the
	// right ids and still leave a node behind each time.
	n := len(ids)
	if want := n + n*n + n*n - n; len(seen) != want {
		t.Errorf("the table holds %d different functions, want %d", len(seen), want)
	}
	if got, want := tbl.types.Len(), len(pool)+len(seen); got != want {
		t.Errorf("the table holds %d types, want %d: the %d made first and one node for each of the %d different functions", got, want, len(pool), len(seen))
	}

	// Two ids that are 65 536 apart. FreshVar pushes one node, so the ids of
	// these variables follow one another.
	more := make([]TypeID, 70000)
	for i := range more {
		more[i] = tbl.FreshVar(Origin{})
	}
	low, high := more[10], more[10+65536]
	if high-low != 65536 || high < 1<<16 {
		t.Fatalf("the test's own ids are not what it assumes: %d and %d", low, high)
	}
	fLow, fHigh := tbl.Fun([]TypeID{low}, pool[0], empty), tbl.Fun([]TypeID{high}, pool[0], empty)
	if fLow == fHigh {
		t.Errorf("Fun([%d], Int) and Fun([%d], Int) have the same id, %d", low, high, fLow)
	}
	if got := tbl.Node(fHigh).Params; !slices.Equal(got, []TypeID{high}) {
		t.Errorf("Node(Fun([%d], Int)).Params = %v, want [%d]", high, got, high)
	}
}

func TestTypeTable(t *testing.T) {
	t.Run("builtins_are_distinct", func(t *testing.T) {
		tbl := NewTypeTable()
		builtins := []struct {
			name string
			b    Builtin
		}{{"Int", BuiltinInt}, {"Bool", BuiltinBool}, {"()", BuiltinUnit}}
		var all []namedType
		for _, bt := range builtins {
			all = append(all, namedType{bt.name, tbl.Builtin(bt.b)})
		}
		all = append(all, namedType{"the error type", tbl.Error()})

		// Four types, six pairs: no two share an id.
		distinctIDs(t, all)

		// Asking again gives the same id. Types are compared by id, so a table
		// that made a new Int every time would call two Ints different.
		for i, bt := range builtins {
			if got := tbl.Builtin(bt.b); got != all[i].id {
				t.Errorf("Builtin(%s) the second time = %d, the first time it was %d", bt.name, got, all[i].id)
			}
		}
		if got := tbl.Error(); got != all[3].id {
			t.Errorf("Error() the second time = %d, the first time it was %d", got, all[3].id)
		}

		// Making other types in between changes nothing.
		tbl.FreshVar(Origin{})
		tbl.Fun([]TypeID{all[0].id}, all[1].id, tbl.EmptyRow())
		for i, bt := range builtins {
			if got := tbl.Builtin(bt.b); got != all[i].id {
				t.Errorf("Builtin(%s) after making other types = %d, before it was %d", bt.name, got, all[i].id)
			}
		}
		if got := tbl.Error(); got != all[3].id {
			t.Errorf("Error() after making other types = %d, before it was %d", got, all[3].id)
		}

		// Each id's node says which type it is.
		for i, bt := range builtins {
			n := tbl.Node(all[i].id)
			if n.Kind != TypeBuiltin || n.Builtin != bt.b {
				t.Errorf("Node(%s) = kind %d, builtin %d; want kind %d, builtin %d", bt.name, n.Kind, n.Builtin, TypeBuiltin, bt.b)
			}
		}
		if n := tbl.Node(all[3].id); n.Kind != TypeErr {
			t.Errorf("Node(the error type).Kind = %d, want TypeErr (%d)", n.Kind, TypeErr)
		}
	})

	t.Run("fun_types_hash_cons", func(t *testing.T) {
		tbl := NewTypeTable()
		intT, boolT, unitT := tbl.Builtin(BuiltinInt), tbl.Builtin(BuiltinBool), tbl.Builtin(BuiltinUnit)
		empty := tbl.EmptyRow()

		// The empty row is one row: asking again gives the same id, and the row
		// has no labels and no tail.
		if again := tbl.EmptyRow(); again != empty {
			t.Errorf("EmptyRow() twice = %d and %d, want the same id", empty, again)
		}
		if row := tbl.rows.Get(empty); len(row.Labels) != 0 || row.HasTail {
			t.Errorf("the empty row is %+v, want no labels and no tail", row)
		}

		// Equal inputs give the same id; another number of parameters, another id.
		f := tbl.Fun([]TypeID{intT}, intT, empty)
		if again := tbl.Fun([]TypeID{intT}, intT, empty); again != f {
			t.Errorf("Fun([Int], Int) twice = %d and %d, want the same id", f, again)
		}
		two := tbl.Fun([]TypeID{intT, intT}, intT, empty)
		if two == f {
			t.Errorf("Fun([Int, Int], Int) = Fun([Int], Int) = %d", f)
		}

		// Each part of the key counts: the return type, one parameter, their
		// order, the number of parameters (also with the same type repeated),
		// and nullary against a one-parameter function.
		nullary := tbl.Fun([]TypeID{}, unitT, empty)
		intBool := tbl.Fun([]TypeID{intT, boolT}, intT, empty)
		distinct := []namedType{
			{"Fun([Int], Int)", f},
			{"Fun([Int, Int], Int)", two},
			{"Fun([Int, Int, Int], Int)", tbl.Fun([]TypeID{intT, intT, intT}, intT, empty)},
			{"Fun([Int], Bool)", tbl.Fun([]TypeID{intT}, boolT, empty)},
			{"Fun([Bool], Int)", tbl.Fun([]TypeID{boolT}, intT, empty)},
			{"Fun([Int, Bool], Int)", intBool},
			{"Fun([Bool, Int], Int)", tbl.Fun([]TypeID{boolT, intT}, intT, empty)},
			{"Fun([], ())", nullary},
			{"Fun([], Int)", tbl.Fun(nil, intT, empty)},
			{"Fun([()], ())", tbl.Fun([]TypeID{unitT}, unitT, empty)},
		}
		distinctIDs(t, distinct)

		// No parameters is the same function whether the slice is nil or empty.
		if viaNil := tbl.Fun(nil, unitT, empty); viaNil != nullary {
			t.Errorf("Fun(nil, ()) = %d, Fun([]TypeID{}, ()) = %d, want the same id", viaNil, nullary)
		}

		// The node keeps every part it was made from.
		if n := tbl.Node(intBool); n.Kind != TypeFun || !slices.Equal(n.Params, []TypeID{intT, boolT}) || n.Ret != intT || n.Effects != empty {
			t.Errorf("Node(Fun([Int, Bool], Int)) = %+v, want a TypeFun with params [%d %d], ret %d, effects %d", n, intT, boolT, intT, empty)
		}
		// The node of a function with no parameters has a nil Params, not an
		// empty slice, and the first request above was made with an empty one.
		if n := tbl.Node(nullary); n.Kind != TypeFun || n.Params != nil || n.Ret != unitT {
			t.Errorf("Node(Fun([], ())) = %+v, want a TypeFun with a nil Params and ret %d", n, unitT)
		}

		// The table keeps its own copy of the parameters: the caller may reuse
		// its slice, and the type must not change under it.
		params := []TypeID{intT, boolT}
		g := tbl.Fun(params, boolT, empty)
		params[0], params[1] = boolT, intT
		if got := tbl.Node(g).Params; !slices.Equal(got, []TypeID{intT, boolT}) {
			t.Errorf("after the caller changed its slice, Node(Fun([Int, Bool], Bool)).Params = %v, want [%d %d]", got, intT, boolT)
		}
		if again := tbl.Fun([]TypeID{intT, boolT}, boolT, empty); again != g {
			t.Errorf("Fun([Int, Bool], Bool) after the caller changed its slice = %d, want %d", again, g)
		}
		if swapped := tbl.Fun(params, boolT, empty); swapped == g {
			t.Errorf("Fun([Bool, Int], Bool) = %d, the id of Fun([Int, Bool], Bool)", swapped)
		}

		// Part I has only one row, so make a second one by hand: the effects
		// are part of the key too.
		other := tbl.rows.Push(Row{Labels: []TypeID{unitT}})
		eff := tbl.Fun([]TypeID{intT}, intT, other)
		if eff == f {
			t.Errorf("Fun([Int], Int) with another row = %d, the id with the empty row", eff)
		}
		if again := tbl.Fun([]TypeID{intT}, intT, other); again != eff {
			t.Errorf("Fun([Int], Int) with another row twice = %d and %d, want the same id", eff, again)
		}
		if n := tbl.Node(eff); n.Effects != other {
			t.Errorf("Node(Fun([Int], Int) with another row).Effects = %d, want %d", n.Effects, other)
		}

		// Many ids: the key tells the ids apart, whatever their size.
		checkFunKeysTellIdsApart(t)
	})

	t.Run("show_function_type", func(t *testing.T) {
		tbl := NewTypeTable()
		intT, boolT, unitT := tbl.Builtin(BuiltinInt), tbl.Builtin(BuiltinBool), tbl.Builtin(BuiltinUnit)
		fn := funMaker(tbl)

		for _, row := range []struct {
			name string
			id   TypeID
			want string
		}{
			{"Int", intT, "Int"},
			{"Bool", boolT, "Bool"},
			{"the error type", tbl.Error(), "{error}"},
			{"two parameters", fn(intT, intT, boolT), "(Int, Bool) -> Int"},
			{"one parameter", fn(boolT, intT), "(Int) -> Bool"},
			{"three parameters", fn(unitT, boolT, intT, unitT), "(Bool, Int, ()) -> ()"},
			{"a function as the only parameter", fn(intT, fn(intT, intT)), "((Int) -> Int) -> Int"},
			{"a function as the second parameter", fn(intT, boolT, fn(unitT, intT, intT)), "(Bool, (Int, Int) -> ()) -> Int"},
			{"a function as the result", fn(fn(boolT, intT), intT), "(Int) -> (Int) -> Bool"},
			{"functions everywhere", fn(fn(unitT), fn(intT, intT), boolT), "((Int) -> Int, Bool) -> () -> ()"},
		} {
			if got := tbl.Show(row.id); got != row.want {
				t.Errorf("%s: Show = %q, want %q", row.name, got, row.want)
			}
		}

		// The text is cut at MaxTypeText bytes, after the last piece that fits,
		// and the cut ends in `…`. The limit is 256, the number the card says.
		if MaxTypeText != 256 {
			t.Fatalf("MaxTypeText = %d, and the rows below (and the card) are written for 256", MaxTypeText)
		}
		// `(Int, …, Int) -> Int` takes 5n+7 bytes for n parameters, and a Bool in
		// place of an Int takes one more, so some function of Int and Bool
		// parameters has a text of any length from 12 up.
		ofLength := func(length int) (TypeID, string) {
			n := (length - 7) / 5
			bools := length - (5*n + 7)
			params := make([]TypeID, n)
			names := make([]string, n)
			for i := range params {
				params[i], names[i] = intT, "Int"
				if i < bools {
					params[i], names[i] = boolT, "Bool"
				}
			}
			return fn(intT, params...), "(" + strings.Join(names, ", ") + ") -> Int"
		}
		atLimit, atLimitText := ofLength(MaxTypeText)
		pastLimit, pastLimitText := ofLength(MaxTypeText + 1)
		// Fifty-one parameters take 255 bytes up to the last one's comma, and
		// the next piece, `Int`, does not fit: the cut leaves the comma in.
		manyInts := make([]TypeID, 100)
		for i := range manyInts {
			manyInts[i] = intT
		}
		for _, row := range []struct {
			name string
			id   TypeID
			want string
		}{
			{"a text of 256 bytes is written whole", atLimit, atLimitText},
			{"a text of 257 bytes loses its last piece", pastLimit, strings.TrimSuffix(pastLimitText, "Int") + "…"},
			{"the cut can fall right after a comma", fn(intT, manyInts...), "(" + strings.Repeat("Int, ", 51) + "…"},
		} {
			if got := tbl.Show(row.id); got != row.want {
				t.Errorf("%s: Show = %q, want %q", row.name, got, row.want)
			}
		}
		if len(atLimitText) != MaxTypeText || len(pastLimitText) != MaxTypeText+1 {
			t.Fatalf("the rows are %d and %d bytes long, want %d and %d", len(atLimitText), len(pastLimitText), MaxTypeText, MaxTypeText+1)
		}

		// A type that holds the same type twice at every level has 40 nodes and
		// 2^40 pieces of text, and Show writes the start of it: it ends in `…`,
		// is no longer than the limit plus that mark, and stops within one piece
		// of the limit (the longest piece is ` -> `, 4 bytes). within fails the
		// test if Show does not end in ten seconds.
		deep := intT
		for range 40 {
			deep = fn(intT, deep, deep)
		}
		var oracle strings.Builder
		var write func(level int)
		write = func(level int) {
			if oracle.Len() > 2*MaxTypeText {
				return
			}
			if level == 0 {
				oracle.WriteString("Int")
				return
			}
			oracle.WriteString("(")
			write(level - 1)
			oracle.WriteString(", ")
			write(level - 1)
			oracle.WriteString(") -> Int")
		}
		write(40)
		var shown string
		within.Run(t, "Show of a type of 40 levels", func() { shown = tbl.Show(deep) })
		body, cut := strings.CutSuffix(shown, "…")
		if !cut {
			t.Errorf("Show of a type of 40 levels ends in %q, want `…`", shown[max(0, len(shown)-8):])
		}
		if len(body) > MaxTypeText || len(body) < MaxTypeText-3 {
			t.Errorf("Show of a type of 40 levels writes %d bytes before `…`, want %d to %d", len(body), MaxTypeText-3, MaxTypeText)
		}
		if !strings.HasPrefix(oracle.String(), body) {
			t.Errorf("Show of a type of 40 levels starts %q, want the start of %q", body, oracle.String()[:min(oracle.Len(), 2*MaxTypeText)])
		}
	})

	t.Run("show_unit_and_nullary_fun", func(t *testing.T) {
		tbl := NewTypeTable()
		intT, unitT := tbl.Builtin(BuiltinInt), tbl.Builtin(BuiltinUnit)
		empty := tbl.EmptyRow()

		for _, row := range []struct {
			name string
			id   TypeID
			want string
		}{
			{"the unit type", unitT, "()"},
			{"no parameters, unit result", tbl.Fun(nil, unitT, empty), "() -> ()"},
			{"no parameters, Int result", tbl.Fun(nil, intT, empty), "() -> Int"},
			{"unit parameter, unit result", tbl.Fun([]TypeID{unitT}, unitT, empty), "(()) -> ()"},
		} {
			if got := tbl.Show(row.id); got != row.want {
				t.Errorf("%s: Show = %q, want %q", row.name, got, row.want)
			}
		}
	})

	t.Run("show_names_vars_in_order", func(t *testing.T) {
		tbl := NewTypeTable()
		intT, boolT, unitT := tbl.Builtin(BuiltinInt), tbl.Builtin(BuiltinBool), tbl.Builtin(BuiltinUnit)
		empty := tbl.EmptyRow()

		// The variables are made in the order in which they appear.
		x, y := tbl.FreshVar(Origin{}), tbl.FreshVar(Origin{})
		if got, want := tbl.Show(tbl.Fun([]TypeID{x, y}, x, empty)), "(a, b) -> a"; got != want {
			t.Errorf("Show(Fun([x, y], x)) = %q, want %q", got, want)
		}

		// Names go by appearance, not by the order the variables were made
		// in: p was made before q and still comes second.
		p, q := tbl.FreshVar(Origin{}), tbl.FreshVar(Origin{})
		if got, want := tbl.Show(tbl.Fun([]TypeID{q, p}, q, empty)), "(a, b) -> a"; got != want {
			t.Errorf("Show(Fun([q, p], q)) = %q, want %q", got, want)
		}
		// The same variable keeps its name, wherever it appears again.
		if got, want := tbl.Show(tbl.Fun([]TypeID{p, p, q}, p, empty)), "(a, a, b) -> a"; got != want {
			t.Errorf("Show(Fun([p, p, q], p)) = %q, want %q", got, want)
		}
		// Names belong to one call: the next call starts again at a, also for
		// a variable that an earlier call called b.
		if got, want := tbl.Show(q), "a"; got != want {
			t.Errorf("Show(q) = %q, want %q", got, want)
		}
		if got, want := tbl.Show(p), "a"; got != want {
			t.Errorf("Show(p) after Show(q) = %q, want %q", got, want)
		}

		// A bound variable shows what it is bound to, takes no name, and does
		// not shift the names of the others.
		bound := tbl.FreshVar(Origin{})
		bind(t, tbl, bound, boolT)
		if got, want := tbl.Show(tbl.Fun([]TypeID{bound, x}, bound, empty)), "(Bool, a) -> Bool"; got != want {
			t.Errorf("Show with a variable bound to Bool = %q, want %q", got, want)
		}
		// One bound to another variable shows that variable's name.
		alias := tbl.FreshVar(Origin{})
		bind(t, tbl, alias, y)
		if got, want := tbl.Show(tbl.Fun([]TypeID{alias, y}, intT, empty)), "(a, a) -> Int"; got != want {
			t.Errorf("Show with a variable bound to another one = %q, want %q", got, want)
		}

		// More than 26 variables: a to z, then a1, b1… z1, then a2.
		many := make([]TypeID, 53)
		for i := range many {
			many[i] = tbl.FreshVar(Origin{})
		}
		shown := tbl.Show(tbl.Fun(many, unitT, empty))
		list, ok := strings.CutSuffix(strings.TrimPrefix(shown, "("), ") -> ()")
		if !ok {
			t.Fatalf("Show of a function of 53 variables = %q, want it to end in `) -> ()`", shown)
		}
		names := strings.Split(list, ", ")
		if len(names) != 53 {
			t.Fatalf("Show of a function of 53 variables names %d of them, want 53: %q", len(names), shown)
		}
		for _, row := range []struct {
			index int
			want  string
		}{{0, "a"}, {1, "b"}, {24, "y"}, {25, "z"}, {26, "a1"}, {27, "b1"}, {51, "z1"}, {52, "a2"}} {
			if names[row.index] != row.want {
				t.Errorf("variable number %d is called %q, want %q", row.index, names[row.index], row.want)
			}
		}
		// No two of the 53 share a name.
		slices.Sort(names)
		if n := len(slices.Compact(names)); n != 53 {
			t.Errorf("the 53 variables have only %d different names", n)
		}
	})

	t.Run("fresh_vars_are_distinct", func(t *testing.T) {
		tbl := NewTypeTable()
		first := Origin{Span: base.Span{File: 1, Lo: 3, Hi: 4}, Note: "parameter `x`"}
		second := Origin{Span: base.Span{File: 1, Lo: 9, Hi: 10}, Note: "let `y`"}
		a, b := tbl.FreshVar(first), tbl.FreshVar(second)

		if a == b {
			t.Errorf("two FreshVar calls gave the same type id, %d", a)
		}
		if va, vb := varID(t, tbl, a), varID(t, tbl, b); va == vb {
			t.Errorf("two FreshVar calls gave the same variable, %d", va)
		}
		for _, bt := range []Builtin{BuiltinInt, BuiltinBool, BuiltinUnit} {
			if id := tbl.Builtin(bt); id == a || id == b {
				t.Errorf("a fresh variable has the id %d of the builtin %d", id, bt)
			}
		}
		if id := tbl.Error(); id == a || id == b {
			t.Errorf("a fresh variable has the id %d of the error type", id)
		}

		// Each variable is a type variable that nothing has bound yet, with the
		// rank and level that FreshVar promises, and remembers where it was born.
		for _, row := range []struct {
			name   string
			id     TypeID
			origin Origin
		}{{"first", a, first}, {"second", b, second}} {
			want := TypeVar{Kind: VarKindType, Origin: row.origin}
			if got := varOf(t, tbl, row.id); got != want {
				t.Errorf("%s variable: %+v, want %+v", row.name, got, want)
			}
		}
	})

	t.Run("resolve_follows_binding", func(t *testing.T) {
		tbl := NewTypeTable()
		intT, boolT := tbl.Builtin(BuiltinInt), tbl.Builtin(BuiltinBool)
		empty := tbl.EmptyRow()

		// Nothing is bound: a variable, a builtin and a function resolve to
		// themselves. The function holds a variable that gets bound below, and
		// still resolves to itself: Resolve looks at the head of a type only.
		a := tbl.FreshVar(Origin{})
		holder := tbl.Fun([]TypeID{a}, intT, empty)
		for _, row := range []struct {
			name string
			id   TypeID
		}{{"an unbound variable", a}, {"Int", intT}, {"a function", holder}, {"the error type", tbl.Error()}} {
			if got := tbl.Resolve(row.id); got != row.id {
				t.Errorf("%s: Resolve(%d) = %d, want itself", row.name, row.id, got)
			}
		}

		// One binding. Show looks through it too.
		bind(t, tbl, a, intT)
		if got := tbl.Resolve(a); got != intT {
			t.Errorf("Resolve of a variable bound to Int = %d, want %d", got, intT)
		}
		if got := tbl.Show(a); got != "Int" {
			t.Errorf("Show of a variable bound to Int = %q, want \"Int\"", got)
		}
		if got := tbl.Resolve(holder); got != holder {
			t.Errorf("Resolve of a function that holds a bound variable = %d, want itself, %d", got, holder)
		}

		// A chain of two: m → n → Bool. Both end at Bool.
		m, n := tbl.FreshVar(Origin{}), tbl.FreshVar(Origin{})
		bind(t, tbl, m, n)
		bind(t, tbl, n, boolT)
		if got := tbl.Resolve(m); got != boolT {
			t.Errorf("Resolve(m) with m → n → Bool = %d, want %d", got, boolT)
		}
		if got := tbl.Resolve(n); got != boolT {
			t.Errorf("Resolve(n) with m → n → Bool = %d, want %d", got, boolT)
		}

		// A chain that ends in a variable ends at that variable's type id,
		// which is not its variable id.
		r, s := tbl.FreshVar(Origin{}), tbl.FreshVar(Origin{})
		bind(t, tbl, r, s)
		if got := tbl.Resolve(r); got != s {
			t.Errorf("Resolve(r) with r → s and s unbound = %d, want %d", got, s)
		}
		if got := tbl.Resolve(s); got != s {
			t.Errorf("Resolve(s) with s unbound = %d, want itself, %d", got, s)
		}

		// A variable bound to a function resolves to the function.
		fnVar := tbl.FreshVar(Origin{})
		fn := tbl.Fun([]TypeID{intT}, boolT, empty)
		bind(t, tbl, fnVar, fn)
		if got := tbl.Resolve(fnVar); got != fn {
			t.Errorf("Resolve of a variable bound to a function = %d, want %d", got, fn)
		}

		// Binding one variable leaves the others alone.
		other := tbl.FreshVar(Origin{})
		if got := tbl.Resolve(other); got != other {
			t.Errorf("Resolve of a variable nobody bound = %d, want itself, %d", got, other)
		}
	})

	t.Run("resolve_compresses_paths", func(t *testing.T) {
		tbl := NewTypeTable()
		intT := tbl.Builtin(BuiltinInt)
		// Each variable has an origin of its own, to see that Resolve changes
		// the link and nothing else of what it records.
		fresh := func(note string) TypeID { return tbl.FreshVar(Origin{Note: note}) }

		// a → b → c, and c unbound. Before Resolve, a points at b.
		a, b, c := fresh("a"), fresh("b"), fresh("c")
		bind(t, tbl, a, b)
		bind(t, tbl, b, c)
		if got, want := varOf(t, tbl, a), (TypeVar{Bound: b, IsBound: true, Origin: Origin{Note: "a"}}); got != want {
			t.Fatalf("before Resolve, a is %+v, want %+v", got, want)
		}
		if got := tbl.Resolve(a); got != c {
			t.Errorf("Resolve(a) = %d, want c (%d)", got, c)
		}
		if got, want := varOf(t, tbl, a), (TypeVar{Bound: c, IsBound: true, Origin: Origin{Note: "a"}}); got != want {
			t.Errorf("after Resolve, a is %+v, want it bound directly to c: %+v", got, want)
		}
		if got, want := varOf(t, tbl, b), (TypeVar{Bound: c, IsBound: true, Origin: Origin{Note: "b"}}); got != want {
			t.Errorf("after Resolve, b is %+v, want it still bound to c: %+v", got, want)
		}
		// The end of the path is not bound to anything, least of all to itself.
		if got, want := varOf(t, tbl, c), (TypeVar{Origin: Origin{Note: "c"}}); got != want {
			t.Errorf("after Resolve, c is %+v, want it unbound: %+v", got, want)
		}

		// A longer chain, ending in a type: every variable on it is rebound to
		// that type, not only the first one and not only the last but one.
		chain := make([]TypeID, 5)
		for i := range chain {
			chain[i] = fresh(strconv.Itoa(i))
		}
		for i := range len(chain) - 1 {
			bind(t, tbl, chain[i], chain[i+1])
		}
		bind(t, tbl, chain[len(chain)-1], intT)
		if got := tbl.Resolve(chain[0]); got != intT {
			t.Errorf("Resolve of the head of a chain of 5 = %d, want Int (%d)", got, intT)
		}
		for i, v := range chain {
			want := TypeVar{Bound: intT, IsBound: true, Origin: Origin{Note: strconv.Itoa(i)}}
			if got := varOf(t, tbl, v); got != want {
				t.Errorf("after Resolve, variable %d of the chain is %+v, want it bound directly to Int: %+v", i, got, want)
			}
		}

		// Resolving again changes nothing.
		if got := tbl.Resolve(chain[0]); got != intT {
			t.Errorf("Resolve of the head of the chain a second time = %d, want Int (%d)", got, intT)
		}
		if got, want := varOf(t, tbl, chain[2]), (TypeVar{Bound: intT, IsBound: true, Origin: Origin{Note: "2"}}); got != want {
			t.Errorf("after the second Resolve, variable 2 of the chain is %+v, want %+v", got, want)
		}
	})

	t.Run("occurs", func(t *testing.T) {
		tbl := NewTypeTable()
		intT, boolT := tbl.Builtin(BuiltinInt), tbl.Builtin(BuiltinBool)
		fn := funMaker(tbl)

		// Five variables come first, so their variable ids (0 to 4) are small
		// numbers that some type ids share: the ids 0 to 3 are the builtins and
		// the error type, and 4 to 8 the variables themselves. A check that
		// mixed the two kinds of id would take another type for the variable.
		vars := make([]TypeID, 5)
		for i := range vars {
			vars[i] = tbl.FreshVar(Origin{})
		}
		a, b, c := vars[0], vars[1], vars[2]
		va, vb := varID(t, tbl, a), varID(t, tbl, b)

		// d stands for (a) -> Int; e for d; g for Int.
		d, e, g := tbl.FreshVar(Origin{}), tbl.FreshVar(Origin{}), tbl.FreshVar(Origin{})
		bind(t, tbl, d, fn(intT, a))
		bind(t, tbl, e, d)
		bind(t, tbl, g, intT)

		for _, row := range []struct {
			name string
			v    TypeVarID
			in   TypeID
			want bool
		}{
			{"the variable itself", va, a, true},
			{"another variable", va, b, false},
			{"Int", va, intT, false},
			{"Bool", va, boolT, false},
			{"the error type", va, tbl.Error(), false},
			{"the parameter, (a) -> Int", va, fn(intT, a), true},
			{"the other variable, (b) -> Int", va, fn(intT, b), false},
			{"the result, (Int) -> a", va, fn(a, intT), true},
			{"the result, no a, (Int) -> b", va, fn(b, intT), false},
			{"the second parameter, (Int, a) -> Int", va, fn(intT, intT, a), true},
			{"the middle parameter, (Int, a, Bool) -> Int", va, fn(intT, intT, a, boolT), true},
			{"the last of three, (Int, Bool, a) -> Int", va, fn(intT, intT, boolT, a), true},
			{"no a in three parameters, (b, Int, c) -> Bool", va, fn(boolT, b, intT, c), false},
			{"a parameter inside a parameter, ((Int) -> (a) -> Bool) -> Int", va, fn(intT, fn(fn(boolT, a), intT)), true},
			{"no a inside a parameter, ((Int) -> (b) -> Bool) -> Int", va, fn(intT, fn(fn(boolT, b), intT)), false},
			{"b in the same function", vb, fn(intT, fn(fn(boolT, b), intT)), true},
			{"through a binding, d", va, d, true},
			{"through a binding, (d) -> Bool", va, fn(boolT, d), true},
			{"through two bindings, e", va, e, true},
			{"through two bindings, (Int) -> e", va, fn(e, intT), true},
			{"b is not in d", vb, d, false},
			{"b is not in (e) -> Bool", vb, fn(boolT, e), false},
			{"a variable bound to Int, g", va, g, false},
			{"(g) -> g", va, fn(g, g), false},
		} {
			if got := tbl.Occurs(row.v, row.in); got != row.want {
				t.Errorf("%s: Occurs = %v, want %v", row.name, got, row.want)
			}
		}

		// Each of the five variables occurs in itself and in none of the other
		// four, and in none of the builtins or the error type.
		for i, x := range vars {
			for j, y := range vars {
				if got, want := tbl.Occurs(varID(t, tbl, x), y), i == j; got != want {
					t.Errorf("Occurs(variable %d, variable %d) = %v, want %v", i, j, got, want)
				}
			}
			for _, y := range []TypeID{intT, boolT, tbl.Builtin(BuiltinUnit), tbl.Error()} {
				if tbl.Occurs(varID(t, tbl, x), y) {
					t.Errorf("Occurs(variable %d, type %d) = true, want false", i, y)
				}
			}
		}

		// Types that hold the same type twice at every level: 40 levels are 40
		// function nodes in the table and 2^40 paths from the top to the bottom.
		// An Occurs that went down every path would not finish, and within
		// fails the test if it takes ten seconds; one that does not repeat the
		// walk of a function does about a hundred steps. This does not count the
		// steps, so a walk that looked at each function twice would pass. The
		// answer is false for a variable that is not there, which is the case
		// that makes a walk try every path, and true for one at the bottom.
		const levels = 40
		w := vars[3]
		onInt, onVar := intT, w
		for range levels {
			onInt = fn(intT, onInt, onInt)
			onVar = fn(intT, onVar, onVar)
		}
		shared := []struct {
			name string
			v    TypeVarID
			in   TypeID
			want bool
		}{
			{"a shared type with no variable at the bottom", va, onInt, false},
			{"a shared type with another variable at the bottom", va, onVar, false},
			{"a shared type with the variable at the bottom", varID(t, tbl, w), onVar, true},
		}
		got := make([]bool, len(shared))
		within.Run(t, "Occurs on the types of 40 levels", func() {
			for i, row := range shared {
				got[i] = tbl.Occurs(row.v, row.in)
			}
		})
		for i, row := range shared {
			if got[i] != row.want {
				t.Errorf("%s: Occurs = %v, want %v", row.name, got[i], row.want)
			}
		}
	})
}
