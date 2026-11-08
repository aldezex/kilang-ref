package interp

import (
	"io"
	"testing"

	"github.com/aldezex/kilang/internal/base"
	"github.com/aldezex/kilang/internal/core"
)

func TestMatchFrames(t *testing.T) {
	t.Run("a_guard_frame_is_charged_to_the_slots", func(t *testing.T) {
		// `match 0 { _ | _ | _ if f() => 1; _ => 2 }` where f() is false, so the three
		// alternatives are tried in turn. While the guard of the first runs, the
		// guardFrame holds the scrutinee and the one choice that is left (the other two
		// alternatives): two slots, which the call of f, with an environment of one,
		// has to fit with. So a limit of two slots overflows the stack there (exit code
		// 103) and a limit of three does not; the frame gives them back when the guard
		// is done, so a run that ends leaves none charged, whatever the frame held.
		sm := new(base.SourceManager)
		sp := base.Span{File: sm.Add("a.ki", []byte("x")), Lo: 0, Hi: 1}
		p := &core.Program{}
		b := core.Builder{P: p}
		wild := func() core.PatID { return b.Pat(core.Pat{Kind: core.PatWildcard, Span: sp}) }
		no := p.Funs.Push(core.Fun{Body: b.LitBool(sp, false), HasBody: true, FrameSize: 1})
		or := b.Pat(core.Pat{Kind: core.PatOr, Span: sp, Args: base.AppendRange(&p.PatPool, []core.PatID{wild(), wild(), wild()})})
		guard := b.Call(sp, b.FunRef(sp, no), nil)
		arms := []core.Arm{{Pat: or, Guard: guard, HasGuard: true, Body: b.LitInt(sp, 1)}, {Pat: wild(), Body: b.LitInt(sp, 2)}}
		f := p.Funs.Push(core.Fun{Body: b.Match(sp, b.LitInt(sp, 0), arms), HasBody: true})

		for _, row := range []struct {
			limit int
			ok    bool
		}{{2, false}, {3, true}} {
			m := New(p, sm, io.Discard, io.Discard)
			m.maxSlots = row.limit
			v, ok := m.Call(f, nil)
			if ok != row.ok || (ok && v != (IntV{V: 2})) || (!ok && *m.halt != (Halt{Code: 103})) {
				t.Errorf("limit %d: Call = (%#v, %v), halt %v, want ok = %v, the value 2 or the halt 103", row.limit, v, ok, m.halt, row.ok)
			}
			if ok && m.slots != 0 {
				t.Errorf("limit %d: %d slots are still charged after the run, want none", row.limit, m.slots)
			}
		}
	})
}
