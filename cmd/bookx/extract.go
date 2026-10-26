package main

import (
	"go/scanner"
	"go/token"
	"strconv"
	"strings"
)

// goToken is a token of a Go source file and where it starts.
type goToken struct {
	tok    token.Token
	lit    string // the text of identifiers and literals; empty for the rest
	offset int    // byte offset in the source
}

// tokenize splits Go source into tokens with the scanner of the standard
// library. It does not parse, so it works on a file that has errors, and it
// skips comments and reads strings, raw strings and runes whole: that is what
// lets extractGoTest count only the braces that are code.
func tokenize(source []byte) []goToken {
	fset := token.NewFileSet()
	file := fset.AddFile("", fset.Base(), len(source))
	var s scanner.Scanner
	s.Init(file, source, nil, 0)
	var toks []goToken
	for {
		pos, tok, lit := s.Scan()
		if tok == token.EOF {
			return toks
		}
		toks = append(toks, goToken{tok: tok, lit: lit, offset: file.Offset(pos)})
	}
}

// testParamTypes maps the prefix of the name of a test function to the type
// of its only parameter; Example functions take none.
var testParamTypes = []struct{ prefix, param string }{
	{"Test", "T"},
	{"Fuzz", "F"},
	{"Benchmark", "B"},
	{"Example", ""},
}

// extractGoTest finds a test of the book in the source of a Go file and returns
// its text, from the `func` keyword or the `t` of `t.Run` to the closing brace
// or parenthesis, with the indentation the file gives it.
//
//	"TestXxx"      the whole function `func TestXxx(t *testing.T) {…}`
//	"TestXxx/sub"  the statement `t.Run("sub", func(t *testing.T) {…})` that is
//	               directly in the body of TestXxx, not inside another subtest
//	"FuzzXxx", "ExampleXxx", "BenchmarkXxx"
//	               the whole function
//
// The braces are counted by the Go scanner, so those inside strings, runes and
// comments do not count. ok is false when the test is not there, or its
// function has the wrong parameter, or never closes.
func extractGoTest(source []byte, ref string) (text string, ok bool) {
	name, sub, hasSub := strings.Cut(ref, "/")
	param, known := "", false
	for _, p := range testParamTypes {
		if strings.HasPrefix(name, p.prefix) {
			param, known = p.param, true
			break
		}
	}
	if !known || (hasSub && !strings.HasPrefix(name, "Test")) {
		return "", false
	}

	toks := tokenize(source)
	for i := range toks {
		start, bodyStart, recv, found := matchFuncHead(toks, i, name, param)
		if !found {
			continue
		}
		bodyEnd := matchingClose(toks, bodyStart, token.LBRACE, token.RBRACE)
		if bodyEnd < 0 {
			return "", false
		}
		end := toks[bodyEnd].offset + 1
		if !hasSub {
			return string(source[start:end]), true
		}
		return extractSubtest(source, toks[bodyStart+1:bodyEnd], recv, sub)
	}
	return "", false
}

// matchFuncHead reports whether toks[i:] starts the declaration of the test
// function name: `func name(recv *testing.<param>) {`, with the parameter left
// out when param is empty. It returns the offset of `func`, the index of the
// opening brace of the body and the name of the parameter.
func matchFuncHead(toks []goToken, i int, name, param string) (start, bodyStart int, recv string, ok bool) {
	at := func(k int) goToken {
		if i+k < len(toks) {
			return toks[i+k]
		}
		return goToken{tok: token.EOF}
	}
	if at(0).tok != token.FUNC || at(1).tok != token.IDENT || at(1).lit != name || at(2).tok != token.LPAREN {
		return 0, 0, "", false
	}
	k := 3
	if param != "" {
		// recv *testing.T )
		recv = at(k).lit
		if at(k).tok != token.IDENT || at(k+1).tok != token.MUL ||
			at(k+2).tok != token.IDENT || at(k+2).lit != "testing" ||
			at(k+3).tok != token.PERIOD ||
			at(k+4).tok != token.IDENT || at(k+4).lit != param {
			return 0, 0, "", false
		}
		k += 5
	}
	if at(k).tok != token.RPAREN || at(k+1).tok != token.LBRACE {
		return 0, 0, "", false
	}
	return toks[i].offset, i + k + 1, recv, true
}

// matchingClose returns the index of the token that closes the bracket
// toks[open], or -1 if it never closes.
func matchingClose(toks []goToken, open int, opening, closing token.Token) int {
	depth := 0
	for i := open; i < len(toks); i++ {
		switch toks[i].tok {
		case opening:
			depth++
		case closing:
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

// extractSubtest finds `recv.Run("sub", …)` among the tokens of a function
// body, where it is a statement of the body itself (brace depth 0 here) and not
// of a function inside it, and returns the text of the call.
func extractSubtest(source []byte, body []goToken, recv, sub string) (string, bool) {
	depth := 0
	for i, t := range body {
		switch t.tok {
		case token.LBRACE:
			depth++
		case token.RBRACE:
			depth--
		}
		if depth != 0 || t.tok != token.IDENT || t.lit != recv || i+4 >= len(body) {
			continue
		}
		if i > 0 && body[i-1].tok == token.PERIOD {
			continue // x.t.Run(...): the identifier is a field of something else
		}
		run, open, name := body[i+2], body[i+3], body[i+4]
		if body[i+1].tok != token.PERIOD || run.tok != token.IDENT || run.lit != "Run" ||
			open.tok != token.LPAREN || name.tok != token.STRING {
			continue
		}
		if got, err := strconv.Unquote(name.lit); err != nil || got != sub {
			continue
		}
		closeIdx := matchingClose(body, i+3, token.LPAREN, token.RPAREN)
		if closeIdx < 0 {
			return "", false
		}
		return string(source[t.offset : body[closeIdx].offset+1]), true
	}
	return "", false
}
