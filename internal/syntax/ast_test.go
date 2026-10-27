package syntax_test

import (
	"slices"
	"testing"

	"github.com/aldezex/kilang/internal/base"
	"github.com/aldezex/kilang/internal/syntax"
)

func TestAST(t *testing.T) {
	t.Run("operator_strings", func(t *testing.T) {
		// The texts are written here, not taken from the code under test: a
		// table shifted by one place, or two swapped entries, must show up.
		binary := []struct {
			op   syntax.BinOp
			want string
		}{
			{syntax.BinAdd, "+"}, {syntax.BinSub, "-"}, {syntax.BinMul, "*"}, {syntax.BinDiv, "/"},
			{syntax.BinRem, "%"}, {syntax.BinEq, "=="}, {syntax.BinNe, "!="}, {syntax.BinLt, "<"},
			{syntax.BinLe, "<="}, {syntax.BinGt, ">"}, {syntax.BinGe, ">="}, {syntax.BinAnd, "&&"},
			{syntax.BinOr, "||"},
			{syntax.BinOp(13), "BinOp(13)"}, {syntax.BinOp(255), "BinOp(255)"},
		}
		for _, row := range binary {
			if got := row.op.String(); got != row.want {
				t.Errorf("BinOp(%d).String() = %q, want %q", uint8(row.op), got, row.want)
			}
		}
		unary := []struct {
			op   syntax.UnOp
			want string
		}{
			{syntax.Neg, "neg"}, {syntax.Not, "not"},
			{syntax.UnOp(2), "UnOp(2)"}, {syntax.UnOp(255), "UnOp(255)"},
		}
		for _, row := range unary {
			if got := row.op.String(); got != row.want {
				t.Errorf("UnOp(%d).String() = %q, want %q", uint8(row.op), got, row.want)
			}
		}
		// Each operator has its own number: the thirteen binary ones are 0..12
		// in the order of the list below, so a constant that moved would
		// change what an old dump means.
		order := []syntax.BinOp{syntax.BinAdd, syntax.BinSub, syntax.BinMul, syntax.BinDiv, syntax.BinRem,
			syntax.BinEq, syntax.BinNe, syntax.BinLt, syntax.BinLe, syntax.BinGt, syntax.BinGe,
			syntax.BinAnd, syntax.BinOr}
		for i, op := range order {
			if int(op) != i {
				t.Errorf("%v is %d, want %d", op, op, i)
			}
		}
	})
	t.Run("list_accessors", func(t *testing.T) {
		m := &syntax.Module{
			ExprPool:  []syntax.ExprID{10, 11, 12, 13, 14},
			StmtPool:  []syntax.StmtID{20, 21, 22},
			ParamPool: []syntax.Param{{Name: 1}, {Name: 2}, {Name: 3}},
			AttrPool:  []syntax.Attr{{Name: 4}, {Name: 5}},
		}
		ranges := []struct {
			name string
			r    base.ListRange
			want []int // the pool elements that the range selects, by index
		}{
			{"empty at the start", base.ListRange{First: 0, Count: 0}, nil},
			{"empty at the end", base.ListRange{First: 2, Count: 0}, nil},
			{"first element", base.ListRange{First: 0, Count: 1}, []int{0}},
			{"middle", base.ListRange{First: 1, Count: 1}, []int{1}},
			{"last element", base.ListRange{First: 2, Count: 1}, []int{2}},
			{"all but the last", base.ListRange{First: 0, Count: 2}, []int{0, 1}},
			{"all but the first", base.ListRange{First: 1, Count: 2}, []int{1, 2}},
		}
		for _, row := range ranges {
			// StmtPool and ParamPool have three elements, ExprPool five and
			// AttrPool two: every range above fits in the first three, so
			// StmtList and Params check them all; Attrs gets those that fit.
			stmts := m.StmtList(row.r)
			params := m.Params(row.r)
			exprs := m.ExprList(row.r)
			if len(stmts) != len(row.want) || len(params) != len(row.want) || len(exprs) != len(row.want) {
				t.Errorf("%s: lengths %d, %d and %d, want %d", row.name, len(stmts), len(params), len(exprs), len(row.want))
				continue
			}
			for i, idx := range row.want {
				if stmts[i] != m.StmtPool[idx] || params[i] != m.ParamPool[idx] || exprs[i] != m.ExprPool[idx] {
					t.Errorf("%s: element %d is (%v, %v, %v), want pool element %d", row.name, i, stmts[i], params[i], exprs[i], idx)
				}
			}
			if int(row.r.First+row.r.Count) <= len(m.AttrPool) {
				attrs := m.Attrs(row.r)
				if len(attrs) != len(row.want) {
					t.Errorf("%s: %d attributes, want %d", row.name, len(attrs), len(row.want))
				}
				for i, idx := range row.want {
					if attrs[i].Name != m.AttrPool[idx].Name {
						t.Errorf("%s: attribute %d is %v, want pool element %d", row.name, i, attrs[i].Name, idx)
					}
				}
			}
		}
		// A list is a view of the pool whose capacity ends where the list does,
		// so appending to it cannot overwrite the next list.
		r := base.ListRange{First: 1, Count: 2}
		got := m.ExprList(r)
		if cap(got) != len(got) {
			t.Errorf("ExprList cap %d, len %d, want them equal", cap(got), len(got))
		}
		_ = append(got, 99)
		if want := []syntax.ExprID{10, 11, 12, 13, 14}; !slices.Equal(m.ExprPool, want) {
			t.Errorf("append to a list changed the pool to %v, want %v", m.ExprPool, want)
		}
		// A range that leaves the pool panics instead of showing other data,
		// also when the pool has spare capacity past its last element, as a
		// pool that append has grown does: a slice expression checks its bound
		// against the capacity, so the accessor has to clip the pool first.
		mustPanic := func(what string, f func()) {
			t.Helper()
			defer func() {
				if recover() == nil {
					t.Errorf("%s did not panic", what)
				}
			}()
			f()
		}
		mustPanic("ExprList of a range past the end of the pool", func() {
			m.ExprList(base.ListRange{First: 4, Count: 2})
		})
		spare := &syntax.Module{ExprPool: append(make([]syntax.ExprID, 0, 8), 10, 11, 12)}
		mustPanic("ExprList of a range inside the capacity but past the length", func() {
			spare.ExprList(base.ListRange{First: 2, Count: 2})
		})
		if got := spare.ExprList(base.ListRange{First: 1, Count: 2}); !slices.Equal(got, []syntax.ExprID{11, 12}) {
			t.Errorf("ExprList of the last two elements of a pool with spare capacity is %v, want [11 12]", got)
		}
	})
}
