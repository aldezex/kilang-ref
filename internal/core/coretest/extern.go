// Package coretest builds small Core programs by hand, for tests that more than
// one package needs. It imports only core and base, so a test inside
// internal/driver can use it: internal/testsupport imports driver, and a test
// of driver itself could not import that without a cycle.
package coretest

import (
	"github.com/aldezex/kilang/internal/base"
	"github.com/aldezex/kilang/internal/core"
)

// ExternProgram returns a Program whose `main` calls, with the argument 9, a
// function that has no body and the extern name name, and sets the Program's
// Main. The elaboration never makes one for a name that the standard library
// does not declare, which is why a test of a name that the interpreter does not
// have takes a Program built by hand. The Program is valid Core: core.Lint finds
// nothing in it, which TestExterns checks. That is why the extern has a
// FrameSize of 1, for its one parameter, although the machine never builds the
// environment of a function that has no body.
func ExternProgram(name string) *core.Program {
	p := &core.Program{}
	b := core.Builder{P: p}
	sp := base.Span{File: 1}
	param := p.Vars.Push(core.Var{Span: sp})
	extern := p.Funs.Push(core.Fun{Params: []core.VarID{param}, ExternName: name, FrameSize: 1})
	main := p.Funs.Push(core.Fun{HasBody: true, Body: b.Call(sp, b.FunRef(sp, extern), []core.ExprID{b.LitInt(sp, 9)})})
	p.Main, p.HasMain = main, true
	return p
}

// UncoveredMatchProgram returns a Program whose `main` is `match false { true =>
// 1 }`, a match that the value false fits no arm of, and sets the Program's Main.
// The checker's pass `patterns` rejects a source that writes one (E0701), so only
// a Program built by hand has it, and it is how a test reaches the machine's
// answer to a match that falls through. The Program is valid Core: core.Lint does
// not look at whether the arms of a match cover its values, which TestLint
// checks.
func UncoveredMatchProgram() *core.Program {
	p := &core.Program{}
	b := core.Builder{P: p}
	sp := base.Span{File: 1}
	arm := core.Arm{Pat: b.Pat(core.Pat{Kind: core.PatTrue, Span: sp}), Body: b.LitInt(sp, 1)}
	main := p.Funs.Push(core.Fun{HasBody: true, Body: b.Match(sp, b.LitBool(sp, false), []core.Arm{arm})})
	p.Main, p.HasMain = main, true
	return p
}
