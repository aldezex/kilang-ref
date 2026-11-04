package main

import (
	"fmt"
	"go/ast"
	"go/build"
	"go/constant"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

// This file writes the mutants of `bookx mutate`: small wrong versions of a
// package, made by the operators below. It only reads source and returns edits.
//
// The operators are the ones the panels' hand-made mutants most often were:
//
//	binop        a comparison, `&&`/`||` or arithmetic turned into its neighbour
//	drop-lhs     one side of `&&` or `||` replaced by what leaves only the other
//	drop-rhs
//	if-negate    an `if` condition negated
//	for-false    a `for` condition replaced by false
//	assign-op    `+=` for `-=` and back
//	incdec       `++` for `--` and back
//	branch       `break` for `continue` and back
//	call-unwrap  f(x) replaced by x, when the result has the type of x
//	case-drop    one value taken out of a `case` list
//	field-drop   one `key: value` taken out of a composite literal
//	int+1        an integer literal plus one
//	bool         true for false and back
//	del-call     a call statement deleted
//	del-assign   an assignment deleted
//	del-incdec   a `++` or `--` deleted
//	del-defer    a `defer` deleted
//	swap         a value replaced by another of its type in reach: a field of the
//	             same struct, or a local variable of the same scope
//
// It leaves out shapes that a reading of the survivors found equivalent, and
// counts them: a dropped field whose value is the zero value anyway, literal or
// constant, a capacity
// hint in `make`, and the number in `return 0, false`, which the caller is told
// not to read.

// mutant is one edit of one file: the bytes lo..hi of File become New.
type mutant struct {
	ID     string // M0001, stable while the package's source does not change
	File   string // relative to the root of the tree, with slashes
	Line   int
	Op     string
	Old    string
	New    string
	lo, hi int
}

// apply returns src with the mutant's edit made.
func (m mutant) apply(src []byte) []byte {
	out := make([]byte, 0, len(src)+len(m.New))
	out = append(out, src[:m.lo]...)
	out = append(out, m.New...)
	return append(out, src[m.hi:]...)
}

// oneLine shows the text of an edit on one line, and cuts it when it is long.
func oneLine(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > 60 {
		return string(r[:60]) + "…"
	}
	return s
}

// String is how a report names the mutant.
func (m mutant) String() string {
	return fmt.Sprintf("%s %s:%d %s: `%s` -> `%s`", m.ID, m.File, m.Line, m.Op, oneLine(m.Old), oneLine(m.New))
}

var (
	binarySwaps = map[token.Token]string{
		token.LSS: "<=", token.LEQ: "<", token.GTR: ">=", token.GEQ: ">",
		token.EQL: "!=", token.NEQ: "==", token.LAND: "||", token.LOR: "&&",
		token.ADD: "-", token.SUB: "+", token.MUL: "/", token.QUO: "*", token.REM: "*",
	}
	assignSwaps = map[token.Token]string{token.ADD_ASSIGN: "-=", token.SUB_ASSIGN: "+="}
)

// packageMutants returns the mutants of the package in dir: its Go files for
// this system, without tests or generated files, in the order of their files and
// positions, with their ids. root is what File is relative to. keep, if not nil,
// says which lines of a file count. filtered is how many were left out as
// equivalent.
func packageMutants(root, dir string, keep func(file string, line int) bool) (ms []mutant, filtered int, err error) {
	pkg, err := build.Default.ImportDir(dir, 0)
	if err != nil {
		return nil, 0, err
	}
	fset := token.NewFileSet()
	var files []*ast.File
	sources := map[*ast.File][]byte{}
	names := map[*ast.File]string{}
	for _, name := range pkg.GoFiles {
		p := filepath.Join(dir, name)
		src, err := os.ReadFile(p)
		if err != nil {
			return nil, 0, err
		}
		f, err := parser.ParseFile(fset, p, src, parser.SkipObjectResolution|parser.ParseComments)
		if err != nil {
			return nil, 0, err
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return nil, 0, err
		}
		files = append(files, f)
		sources[f], names[f] = src, filepath.ToSlash(rel)
	}
	info := &types.Info{
		Types:  map[ast.Expr]types.TypeAndValue{},
		Uses:   map[*ast.Ident]types.Object{},
		Defs:   map[*ast.Ident]types.Object{},
		Scopes: map[ast.Node]*types.Scope{},
	}
	// An error here only costs operators that need types: the tests decide
	// whether the package is sound.
	conf := types.Config{Importer: importer.ForCompiler(fset, "source", nil), Error: func(error) {}}
	checked, _ := conf.Check(pkg.ImportPath, fset, files, info)

	for _, f := range files {
		if ast.IsGenerated(f) {
			continue
		}
		e := &mutantWriter{fset: fset, file: names[f], src: sources[f], info: info, pkg: checked, skip: map[token.Pos]bool{}}
		e.walk(f)
		e.swaps(f)
		filtered += e.filtered
		ms = append(ms, e.out...)
	}
	slices.SortStableFunc(ms, func(a, b mutant) int {
		if c := strings.Compare(a.File, b.File); c != 0 {
			return c
		}
		if a.lo != b.lo {
			return a.lo - b.lo
		}
		if a.hi != b.hi {
			return a.hi - b.hi
		}
		if c := strings.Compare(a.Op, b.Op); c != 0 {
			return c
		}
		return strings.Compare(a.New, b.New)
	})
	ms = slices.CompactFunc(ms, func(a, b mutant) bool {
		return a.File == b.File && a.lo == b.lo && a.hi == b.hi && a.New == b.New
	})
	for i := range ms {
		ms[i].ID = fmt.Sprintf("M%04d", i+1)
	}
	if keep != nil {
		ms = slices.DeleteFunc(ms, func(m mutant) bool { return !keep(m.File, m.Line) })
	}
	return ms, filtered, nil
}

// mutantWriter collects the mutants of one file.
type mutantWriter struct {
	fset     *token.FileSet
	file     string
	src      []byte
	info     *types.Info
	pkg      *types.Package
	skip     map[token.Pos]bool // integer literals that a filter leaves out
	out      []mutant
	filtered int
}

func (e *mutantWriter) offset(p token.Pos) int { return e.fset.Position(p).Offset }

func (e *mutantWriter) add(op string, lo, hi token.Pos, text string) {
	m := mutant{File: e.file, Op: op, lo: e.offset(lo), hi: e.offset(hi), New: text, Line: e.fset.Position(lo).Line}
	m.Old = string(e.src[m.lo:m.hi])
	e.out = append(e.out, m)
}

func (e *mutantWriter) text(n ast.Node) string {
	return string(e.src[e.offset(n.Pos()):e.offset(n.End())])
}

// isString reports whether x is a string, so its `+` joins and does not add.
func (e *mutantWriter) isString(x ast.Expr) bool {
	tv, ok := e.info.Types[x]
	if !ok || tv.Type == nil {
		return false
	}
	b, ok := tv.Type.Underlying().(*types.Basic)
	return ok && b.Info()&types.IsString != 0
}

// isZeroLiteral reports whether x is written as the zero value of its type.
func isZeroLiteral(x ast.Expr) bool {
	switch v := x.(type) {
	case *ast.BasicLit:
		return v.Value == "0" || v.Value == `""` || v.Value == "``"
	case *ast.Ident:
		return v.Name == "false" || v.Name == "nil"
	}
	return false
}

// isZeroConstant reports whether x is a constant whose value is zero, such as the
// first name of an iota: dropping that field changes nothing either.
func (e *mutantWriter) isZeroConstant(x ast.Expr) bool {
	tv, ok := e.info.Types[x]
	return ok && tv.Value != nil && tv.Value.Kind() == constant.Int && constant.Sign(tv.Value) == 0
}

// walk adds the mutants of every operator but swap.
func (e *mutantWriter) walk(f *ast.File) {
	ast.Inspect(f, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.GenDecl:
			if x.Tok != token.DEFINE {
				return false // constants, variables, types and imports stay
			}
		case *ast.BinaryExpr:
			if text, ok := binarySwaps[x.Op]; ok && !(x.Op == token.ADD && e.isString(x)) {
				e.add("binop", x.OpPos, x.OpPos+token.Pos(len(x.Op.String())), text)
			}
			if x.Op == token.LAND || x.Op == token.LOR {
				val := "true"
				if x.Op == token.LOR {
					val = "false"
				}
				e.add("drop-lhs", x.X.Pos(), x.X.End(), val)
				e.add("drop-rhs", x.Y.Pos(), x.Y.End(), val)
			}
		case *ast.IfStmt:
			e.add("if-negate", x.Cond.Pos(), x.Cond.End(), "!("+e.text(x.Cond)+")")
		case *ast.ForStmt:
			if x.Cond != nil {
				e.add("for-false", x.Cond.Pos(), x.Cond.End(), "false")
			}
		case *ast.AssignStmt:
			if text, ok := assignSwaps[x.Tok]; ok {
				e.add("assign-op", x.TokPos, x.TokPos+2, text)
			}
		case *ast.IncDecStmt:
			text := "++"
			if x.Tok == token.INC {
				text = "--"
			}
			e.add("incdec", x.TokPos, x.TokPos+2, text)
		case *ast.BranchStmt:
			if x.Tok == token.BREAK && x.Label == nil {
				e.add("branch", x.Pos(), x.End(), "continue")
			}
			if x.Tok == token.CONTINUE && x.Label == nil {
				e.add("branch", x.Pos(), x.End(), "break")
			}
		case *ast.ReturnStmt:
			e.markUnreadResults(x)
		case *ast.CallExpr:
			e.markCapacityHint(x)
			if tv, ok := e.info.Types[x]; ok && tv.Type != nil && len(x.Args) >= 1 {
				if at, ok := e.info.Types[x.Args[0]]; ok && at.IsValue() && types.Identical(tv.Type, at.Type) {
					e.add("call-unwrap", x.Pos(), x.End(), e.text(x.Args[0]))
				}
			}
		case *ast.CaseClause:
			if len(x.List) >= 2 {
				for i, v := range x.List {
					if i+1 < len(x.List) {
						e.add("case-drop", v.Pos(), x.List[i+1].Pos(), "")
					} else {
						e.add("case-drop", x.List[i-1].End(), v.End(), "")
					}
				}
			}
			for _, st := range x.Body {
				e.deleteStmt(st)
			}
		case *ast.CompositeLit:
			for i, v := range x.Elts {
				kv, ok := v.(*ast.KeyValueExpr)
				if !ok || len(x.Elts) < 2 {
					continue
				}
				if isZeroLiteral(kv.Value) || e.isZeroConstant(kv.Value) {
					e.filtered++
					continue
				}
				if i+1 < len(x.Elts) {
					e.add("field-drop", v.Pos(), x.Elts[i+1].Pos(), "")
				} else {
					e.add("field-drop", x.Elts[i-1].End(), v.End(), "")
				}
			}
		case *ast.BasicLit:
			if x.Kind == token.INT {
				if e.skip[x.Pos()] {
					e.filtered++
				} else if v, err := strconv.ParseInt(x.Value, 0, 64); err == nil && v < 1<<62 {
					e.add("int+1", x.Pos(), x.End(), strconv.FormatInt(v+1, 10))
				}
			}
		case *ast.BlockStmt:
			for _, st := range x.List {
				e.deleteStmt(st)
			}
		case *ast.Ident:
			switch x.Name {
			case "true":
				e.add("bool", x.Pos(), x.End(), "false")
			case "false":
				e.add("bool", x.Pos(), x.End(), "true")
			}
		}
		return true
	})
}

// markUnreadResults filters the number in `return 0, false`: the false tells the
// caller not to read it.
func (e *mutantWriter) markUnreadResults(r *ast.ReturnStmt) {
	if len(r.Results) < 2 {
		return
	}
	last, ok := r.Results[len(r.Results)-1].(*ast.Ident)
	if !ok || last.Name != "false" {
		return
	}
	for _, x := range r.Results[:len(r.Results)-1] {
		if lit, ok := x.(*ast.BasicLit); ok && lit.Kind == token.INT {
			e.skip[lit.Pos()] = true
		}
	}
}

// markCapacityHint filters the capacity of `make([]T, n, capacity)` and the size
// of `make(map[K]V, size)`: they change the speed and not the result.
func (e *mutantWriter) markCapacityHint(c *ast.CallExpr) {
	if id, ok := c.Fun.(*ast.Ident); !ok || id.Name != "make" || len(c.Args) < 2 {
		return
	}
	last := c.Args[len(c.Args)-1]
	if _, isMap := c.Args[0].(*ast.MapType); !isMap && len(c.Args) != 3 {
		return
	}
	if lit, ok := last.(*ast.BasicLit); ok && lit.Kind == token.INT {
		e.skip[lit.Pos()] = true
	}
}

// deleteStmt adds the mutant that deletes a statement, if it is of a kind that
// can go. A panic stays: a compiler that goes on past one is a different bug.
func (e *mutantWriter) deleteStmt(s ast.Stmt) {
	switch y := s.(type) {
	case *ast.ExprStmt:
		if c, ok := y.X.(*ast.CallExpr); ok {
			if id, ok := c.Fun.(*ast.Ident); ok && id.Name == "panic" {
				return
			}
			e.add("del-call", s.Pos(), s.End(), "")
		}
	case *ast.AssignStmt:
		if y.Tok == token.ASSIGN || y.Tok == token.ADD_ASSIGN || y.Tok == token.SUB_ASSIGN {
			e.add("del-assign", s.Pos(), s.End(), "")
		}
	case *ast.IncDecStmt:
		e.add("del-incdec", s.Pos(), s.End(), "")
	case *ast.DeferStmt:
		e.add("del-defer", s.Pos(), s.End(), "")
	}
}

// swaps adds the mutants that put another value of the same type where a value is
// read: arguments, elements, results and right-hand sides. At most two per place.
func (e *mutantWriter) swaps(f *ast.File) {
	if e.pkg == nil {
		return
	}
	var reads []ast.Expr
	ast.Inspect(f, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.CallExpr:
			reads = append(reads, x.Args...)
		case *ast.CompositeLit:
			for _, v := range x.Elts {
				if kv, ok := v.(*ast.KeyValueExpr); ok {
					reads = append(reads, kv.Value)
				} else {
					reads = append(reads, v)
				}
			}
		case *ast.ReturnStmt:
			reads = append(reads, x.Results...)
		case *ast.AssignStmt:
			reads = append(reads, x.Rhs...)
		}
		return true
	})
	for _, x := range reads {
		switch v := x.(type) {
		case *ast.SelectorExpr:
			e.swapField(v)
		case *ast.Ident:
			e.swapLocal(v)
		}
	}
}

// swapField replaces x.f with another field of x of the same type.
func (e *mutantWriter) swapField(x *ast.SelectorExpr) {
	field, ok := e.info.Uses[x.Sel].(*types.Var)
	if !ok || !field.IsField() {
		return
	}
	tv, ok := e.info.Types[x.X]
	if !ok {
		return
	}
	t := tv.Type
	if p, ok := t.Underlying().(*types.Pointer); ok {
		t = p.Elem()
	}
	st, ok := t.Underlying().(*types.Struct)
	if !ok {
		return
	}
	n := 0
	for i := 0; i < st.NumFields() && n < 2; i++ {
		g := st.Field(i)
		if g.Name() != field.Name() && types.Identical(g.Type(), field.Type()) {
			e.add("swap", x.Sel.Pos(), x.Sel.End(), g.Name())
			n++
		}
	}
}

// swapLocal replaces a local variable with another of its scope and type that is
// visible at this use, so `y := x` never becomes `y := y`.
func (e *mutantWriter) swapLocal(x *ast.Ident) {
	obj, ok := e.info.Uses[x].(*types.Var)
	if !ok || obj.IsField() || obj.Parent() == nil || obj.Parent() == e.pkg.Scope() {
		return
	}
	scope := obj.Parent()
	n := 0
	for _, name := range scope.Names() { // sorted, so the choice is always the same
		if name == obj.Name() || name == "_" || n >= 2 {
			continue
		}
		_, found := scope.LookupParent(name, x.Pos())
		if o, ok := found.(*types.Var); ok && o.Parent() == scope && types.Identical(o.Type(), obj.Type()) {
			e.add("swap", x.Pos(), x.End(), name)
			n++
		}
	}
}
