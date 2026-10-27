package syntax_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/aldezex/kilang/internal/base"
	"github.com/aldezex/kilang/internal/diag"
	"github.com/aldezex/kilang/internal/syntax"
)

// parseFile lexes and parses a file made of src. It returns the module, the id
// of the file, the interner that holds the names and the sink that both passes
// reported to. The id is 1, not 0 (see lexFile), so a Module or a span that
// lost its File does not pass for a right one.
func parseFile(t *testing.T, src string) (*syntax.Module, base.FileID, *base.Interner, *diag.Sink) {
	t.Helper()
	tb, sink, id, in := lexFile(t, src)
	return syntax.Parse(&tb, id, in, sink), id, in, sink
}

// printModule parses src and returns what PrintModule writes. src must be
// free of lexical errors: a test that wants a malformed file calls parseFile.
func printModule(t *testing.T, src string) string {
	t.Helper()
	m, _, in, sink := parseFile(t, src)
	if n := len(sink.All()); n != 0 {
		t.Fatalf("%q: lexing and parsing reported %d diagnostics, want none: %v", src, n, sink.All())
	}
	return syntax.PrintModule(m, in)
}

// wantModule fails the test unless PrintModule gives exactly want for src.
// what names the case in the failure message.
func wantModule(t *testing.T, what, src, want string) {
	t.Helper()
	if got := printModule(t, src); got != want {
		t.Errorf("%s: %q printed\n%s\nwant\n%s", what, src, got, want)
	}
}

func TestParser(t *testing.T) {
	t.Run("fn_signature", func(t *testing.T) {
		wantModule(t, "example", "fn fib(n: Int) -> Int {}",
			"(fn fib (params (n Int)) (ret Int) (block))\n")
	})
	t.Run("pub_and_attrs", func(t *testing.T) {
		wantModule(t, "example", `#[extern("ki_print_int")] pub fn print(x: Int)`,
			"(pub fn print (attrs (extern \"ki_print_int\")) (params (x Int)) (ret _) none)\n")
	})
	t.Run("unit_type", func(t *testing.T) {
		wantModule(t, "example", "fn f() -> () {}",
			"(fn f (params) (ret ()) (block))\n")
	})
	t.Run("bool_type", func(t *testing.T) {
		wantModule(t, "example", "fn f(b: Bool) {}",
			"(fn f (params (b Bool)) (ret _) (block))\n")
	})
	t.Run("untyped_param", func(t *testing.T) {
		wantModule(t, "example", "fn f(x) {}",
			"(fn f (params (x _)) (ret _) (block))\n")
	})
	t.Run("two_items", func(t *testing.T) {
		wantModule(t, "example", "fn a() {}\nfn b() {}",
			"(fn a (params) (ret _) (block))\n(fn b (params) (ret _) (block))\n")
	})

	t.Run("many_params", func(t *testing.T) {
		// Three parameters, typed and not, so that a parser that stops after
		// the first, drops the last or loses the types shows up.
		wantModule(t, "three", "fn f(a: Int, b, c: Bool) {}",
			"(fn f (params (a Int) (b _) (c Bool)) (ret _) (block))\n")
		// Two items that both have a list: the second list does not start at
		// the start of its pool, and each list must be its own.
		wantModule(t, "two items", "fn f(x: Int, y) {}\nfn g(z: Bool) -> Int {}",
			"(fn f (params (x Int) (y _)) (ret _) (block))\n(fn g (params (z Bool)) (ret Int) (block))\n")
	})
	t.Run("attr_forms", func(t *testing.T) {
		rows := []struct{ name, src, want string }{
			{"no arguments", "#[test] fn f() {}",
				"(fn f (attrs (test)) (params) (ret _) (block))\n"},
			{"two arguments", `#[a("x", "y")] fn f() {}`,
				"(fn f (attrs (a \"x\" \"y\")) (params) (ret _) (block))\n"},
			{"two attributes", `#[a] #[b("1")] fn f() {}`,
				"(fn f (attrs (a) (b \"1\")) (params) (ret _) (block))\n"},
			// The arguments are printed quoted as Go quotes, so a quote, a
			// backslash and a line feed show up escaped. The é stays as it is.
			{"escapes", `#[a("q\"\n\\é")] fn f() {}`,
				"(fn f (attrs (a \"q\\\"\\n\\\\é\")) (params) (ret _) (block))\n"},
			// The attributes of the second item are not those of the first.
			{"second item", "#[a(\"1\")] fn f() {}\n#[b] #[c(\"2\", \"3\")] pub fn g() {}",
				"(fn f (attrs (a \"1\")) (params) (ret _) (block))\n(pub fn g (attrs (b) (c \"2\" \"3\")) (params) (ret _) (block))\n"},
		}
		for _, row := range rows {
			wantModule(t, row.name, row.src, row.want)
		}
	})
	t.Run("attrs_on_their_own_lines", func(t *testing.T) {
		// The prelude is written like this. The line break after a `]` is a
		// Newline token, and the attribute still belongs to the item below.
		wantModule(t, "one", "#[extern(\"ki_print_int\")]\npub fn print(x: Int)\n",
			"(pub fn print (attrs (extern \"ki_print_int\")) (params (x Int)) (ret _) none)\n")
		wantModule(t, "two", "#[a]\n#[b(\"1\")]\nfn f() {}\n",
			"(fn f (attrs (a) (b \"1\")) (params) (ret _) (block))\n")
	})
	t.Run("extern_then_item", func(t *testing.T) {
		// A function has a body when a `{` follows on the same line. Without
		// one it ends at the line break, and the next item is a new item.
		wantModule(t, "no return type", "pub fn a(x: Int)\nfn b() {}",
			"(pub fn a (params (x Int)) (ret _) none)\n(fn b (params) (ret _) (block))\n")
		wantModule(t, "return type", "fn a() -> Int\nfn b() {}",
			"(fn a (params) (ret Int) none)\n(fn b (params) (ret _) (block))\n")
		wantModule(t, "last item", "fn a() {}\nfn b() -> ()",
			"(fn a (params) (ret _) (block))\n(fn b (params) (ret ()) none)\n")
	})
	t.Run("empty_module", func(t *testing.T) {
		for _, src := range []string{"", "\n\n", "// only a comment\n", "/* a */ // b\n"} {
			m, _, in, _ := parseFile(t, src)
			if len(m.Items) != 0 || m.ItemNodes.Len() != 0 {
				t.Errorf("%q: %d items and %d item nodes, want none", src, len(m.Items), m.ItemNodes.Len())
			}
			if got := syntax.PrintModule(m, in); got != "" {
				t.Errorf("%q printed %q, want nothing", src, got)
			}
		}
	})
	t.Run("spans", func(t *testing.T) {
		const src = "\n  #[ext(\"sym\")] pub fn fib(n: Int, zed) -> Bool {}\n"
		m, id, _, sink := parseFile(t, src)
		if sink.HasErrors() || len(m.Items) != 1 {
			t.Fatalf("got %d items and diagnostics %v, want 1 item and none", len(m.Items), sink.All())
		}
		if m.File != id {
			t.Errorf("Module.File = %d, want %d", m.File, id)
		}
		// spanAt is the span of the n bytes that start at the first occurrence
		// of text in s; span is the whole of text, found in src. Each text used
		// below occurs once, or its first occurrence is the right one.
		spanAt := func(s, text string, n int) base.Span {
			lo := strings.Index(s, text)
			if lo < 0 {
				t.Fatalf("%q is not in %q", text, s)
			}
			return base.Span{File: id, Lo: uint32(lo), Hi: uint32(lo + n)}
		}
		span := func(text string) base.Span { return spanAt(src, text, len(text)) }
		check := func(what string, got, want base.Span) {
			t.Helper()
			if got != want {
				t.Errorf("%s: span %v, want %v", what, got, want)
			}
		}
		it := m.ItemNodes.Get(m.Items[0])
		check("item (attribute to closing brace)", it.Span, span(`#[ext("sym")] pub fn fib(n: Int, zed) -> Bool {}`))
		check("item name", it.NameSpan, span("fib"))
		attrs := m.Attrs(it.Attrs)
		if len(attrs) != 1 {
			t.Fatalf("%d attributes, want 1", len(attrs))
		}
		check("attribute", attrs[0].Span, span(`#[ext("sym")]`))
		if want := []string{"sym"}; !slices.Equal(attrs[0].Args, want) {
			t.Errorf("attribute arguments %q, want %q", attrs[0].Args, want)
		}
		params := m.Params(it.Params)
		if len(params) != 2 {
			t.Fatalf("%d parameters, want 2", len(params))
		}
		check("typed parameter", params[0].Span, span("n: Int"))
		check("type of the parameter", m.Types.Get(params[0].Type).Span, span("Int"))
		check("untyped parameter", params[1].Span, span("zed"))
		if !it.HasRet || !it.HasBody || !it.Pub {
			t.Errorf("HasRet %v, HasBody %v, Pub %v, want all true", it.HasRet, it.HasBody, it.Pub)
		}
		check("return type", m.Types.Get(it.Ret).Span, span("Bool"))
		body := m.Exprs.Get(it.Body)
		check("body", body.Span, span("{}"))
		if body.Kind != syntax.ExprBlock || body.Stmts.Count != 0 || body.HasTail {
			t.Errorf("body is %+v, want an empty block", body)
		}

		// The span of `()` is its two characters, not the `()` of the
		// parameter list that comes first in the text.
		const unit = "fn g() -> () {}"
		m, id, _, _ = parseFile(t, unit)
		it = m.ItemNodes.Get(m.Items[0])
		lo := uint32(strings.Index(unit, "-> ()") + len("-> "))
		if got, want := m.Types.Get(it.Ret).Span, (base.Span{File: id, Lo: lo, Hi: lo + 2}); got != want {
			t.Errorf("`()` return type: span %v, want %v", got, want)
		}
		// An item without a body ends at the end of its signature, not at the
		// line break that follows it.
		const extern = "fn h(x: Int)\n"
		m, id, _, _ = parseFile(t, extern)
		it = m.ItemNodes.Get(m.Items[0])
		if got, want := it.Span, (base.Span{File: id, Lo: 0, Hi: uint32(len("fn h(x: Int)"))}); got != want {
			t.Errorf("item without a body: span %v, want %v", got, want)
		}

		// The error nodes. A type that is missing has an empty span where it
		// should start, so the `)` that follows it is not part of it; one that
		// starts with `(` covers that parenthesis, which is consumed; an item
		// that stops making sense covers every token read until then; and a
		// token that cannot start an item is an item of its own.
		const noType = "fn f(x: ) {}"
		m, id, _, _ = parseFile(t, noType)
		it = m.ItemNodes.Get(m.Items[0])
		if got, want := m.Types.Get(m.Params(it.Params)[0].Type).Span, spanAt(noType, ")", 0); got != want {
			t.Errorf("missing type: span %v, want %v", got, want)
		}
		const parenType = "fn f() -> (Int) {}"
		m, id, _, _ = parseFile(t, parenType)
		it = m.ItemNodes.Get(m.Items[0])
		if got, want := m.Types.Get(it.Ret).Span, spanAt(parenType, "(Int", 1); got != want {
			t.Errorf("type that starts with a parenthesis: span %v, want %v", got, want)
		}
		const cut = "fn f(x"
		m, id, _, _ = parseFile(t, cut)
		if got, want := m.ItemNodes.Get(m.Items[0]), (syntax.Item{Kind: syntax.ItemError, Span: spanAt(cut, "fn f(x", len(cut))}); got != want {
			t.Errorf("item cut short: %+v, want %+v", got, want)
		}
		// The Newline after an attribute is skipped to find the item, but it
		// is not part of any node: the item below stops at the `]`.
		const attrOnly = "#[a]\n1"
		m, id, _, _ = parseFile(t, attrOnly)
		if got, want := m.ItemNodes.Get(m.Items[0]), (syntax.Item{Kind: syntax.ItemError, Span: spanAt(attrOnly, "#[a]", 4)}); got != want {
			t.Errorf("attribute with no item: %+v, want %+v", got, want)
		}
		const stray = "  1 fn g() {}"
		m, id, _, _ = parseFile(t, stray)
		if got, want := m.ItemNodes.Get(m.Items[0]), (syntax.Item{Kind: syntax.ItemError, Span: spanAt(stray, "1", 1)}); got != want {
			t.Errorf("stray token: %+v, want %+v", got, want)
		}
		// The rest of the places where a signature stops making sense: the
		// attributes, the name and the `(`. In each the item covers every token
		// read until then, from the first to the last one consumed, and not
		// the token found instead of the one that was needed.
		for _, row := range []struct{ name, src, covers string }{
			{"attribute without its `]`", "#[a fn f() {}", "#[a"},
			{"no name", "fn 1", "fn"},
			{"no `(`", "fn f x) {}", "fn f"},
		} {
			m, id, _, _ = parseFile(t, row.src)
			if len(m.Items) == 0 {
				t.Errorf("%s: %q has no items", row.name, row.src)
				continue
			}
			want := syntax.Item{Kind: syntax.ItemError, Span: spanAt(row.src, row.covers, len(row.covers))}
			if got := m.ItemNodes.Get(m.Items[0]); got != want {
				t.Errorf("%s: %q: first item %+v, want %+v", row.name, row.src, got, want)
			}
		}
	})
	t.Run("malformed_input_still_parses", func(t *testing.T) {
		// Each of these stops making sense somewhere inside an item, or does
		// not start with one. The parser must come back, never read past the
		// end of the tokens, never print the text as if it were a correct
		// program, report it as an E0201 (a file that is not a program must
		// not parse in silence), and leave the items in order and inside the
		// file. What it reports besides the E0201, and how many times, is not
		// checked: the recovery of a later session changes that.
		srcs := []string{
			"fn", "fn f", "fn f(", "fn f(x", "fn f(x:", "fn f(x: )", "fn f(x,", "fn f(x,) {}", "fn f() ->",
			"fn (x) {}", "fn F() {}", "fn f(X) {}", "fn f(1) {}", "fn 1", "fn f() $",
			"#", "#[", "#[a", "#[a(", `#[a("x"`, `#[a("x",`, `#[a(1)] fn f() {}`, "#[a]", "#[a]\n",
			// Each of these would be a correct item if one token were not
			// missing: the opening bracket of an attribute, its name, a
			// closing bracket or parenthesis, a comma, the `fn` after `pub` or
			// after the attributes, the `(` or the `)` of the parameters.
			"# a] fn f() {}", "#[] fn f() {}", "#[a fn f() {}", `#[a("x" fn f() {}`, `#[a("x" "y")] fn f() {}`,
			"fn f x) {}", "fn f(x {}", "fn f(x y) {}", "pub f() {}", "#[a] f() {}", `#[a("x"] fn f() {}`,
			"pub", "pub pub fn f() {}", "pub 1", "1", ")", "}", "{ }", "let x = 1", "fn f() {}}",
			// A type is written with a capital letter, so a name in lower case is
			// not one: the likeliest slip of someone who writes `int`.
			"fn f(x: int) {}",
		}
		for _, src := range srcs {
			m, id, in, sink := parseFile(t, src)
			if !strings.Contains(syntax.PrintModule(m, in), "(error)") {
				t.Errorf("%q printed %q, want it to show an (error)", src, syntax.PrintModule(m, in))
			}
			if sink.Count(diag.E(201)) == 0 {
				t.Errorf("%q: no E0201 reported, want at least one; diagnostics %v", src, sink.All())
			}
			prevHi := uint32(0)
			for _, itemID := range m.Items {
				sp := m.ItemNodes.Get(itemID).Span
				if sp.File != id || sp.Lo < prevHi || sp.Hi < sp.Lo || int(sp.Hi) > len(src) {
					t.Errorf("%q: item span %v after one that ends at %d, want an increasing span inside [0,%d)",
						src, sp, prevHi, len(src))
				}
				prevHi = sp.Hi
			}
		}

		// The parser goes on after an error node: what follows it is read as
		// items. A parser that gave up at the first error node would print the
		// (error) alone. In the first source the error is a token that cannot
		// start an item; in the second the signature stops at the `fn` of the
		// next item, which is not consumed.
		for _, row := range []struct{ src, want string }{
			{"1 fn g() {}", "(error)\n(fn g (params) (ret _) (block))\n"},
			{"fn f( fn g() {}", "(error)\n(fn g (params) (ret _) (block))\n"},
		} {
			m, _, in, _ := parseFile(t, row.src)
			if got := syntax.PrintModule(m, in); got != row.want {
				t.Errorf("%q printed %q, want %q", row.src, got, row.want)
			}
		}

		// A list that goes wrong adds nothing to its pool. Each list below
		// fails after something was read right (the attribute `a`, the
		// parameter `x`), so a parser that added each attribute or parameter to
		// the pool as soon as it read it would leave that one behind. An
		// attribute can fail in five places, one row each: the `[` after the
		// `#`, the name, the first argument, the `)` that closes the arguments
		// and the `]`. A parser that reported the failure at any of them as a
		// success would add the half-read attribute to the pool, so every
		// place needs its own row.
		for _, src := range []string{
			"#[a] # a", "#[a] #[", "#[a] #[b(1)]", `#[a] #[b("x"`, "#[a] #[b",
		} {
			m, _, _, _ := parseFile(t, src)
			if n := len(m.AttrPool); n != 0 {
				t.Errorf("AttrPool has %d attributes after %q, want none", n, src)
			}
		}
		const params = "fn f(x,) {}"
		m, _, _, _ := parseFile(t, params)
		if n := len(m.ParamPool); n != 0 {
			t.Errorf("ParamPool has %d parameters after %q, want none", n, params)
		}
	})
}
