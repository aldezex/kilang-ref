package interp

import (
	"io"
	"math"
	"testing"

	"github.com/aldezex/kilang/internal/base"
	"github.com/aldezex/kilang/internal/core"
)

// fakeFrame is a frame that this package does not define. It can only be
// written here, inside the package, because frame is unexported: that is the
// point of the sealed interface, and the only way to hand applyFrame a kind it
// has no case for.
type fakeFrame struct{}

func (fakeFrame) frame() {}

func TestMachineFrames(t *testing.T) {
	t.Run("unknown_frame_is_unreachable", func(t *testing.T) {
		m := New(&core.Program{}, new(base.SourceManager), io.Discard, io.Discard)
		var got any
		func() {
			defer func() { got = recover() }()
			m.applyFrame(fakeFrame{}, UnitV{})
		}()
		if got != "unreachable" {
			t.Errorf("applyFrame(fakeFrame{}, UnitV{}) panicked with %#v, want the string \"unreachable\"", got)
		}
	})

	t.Run("frames_balance_the_slots", func(t *testing.T) {
		// The room of the values that wait in a frame is charged to the slots when
		// the frame is pushed and given back when it is done, and it is the room
		// that was made, so a frame that held more than it was charged for (the
		// room that a slice grows to when it is not made big enough) would leave
		// the count lower after every call. Once a Call returns the count is the
		// slots of the entry's own environment, here none, whatever the arities.
		sp := base.Span{File: 1, Lo: 0, Hi: 1}
		for _, arity := range []int{1, 2, 3, 4, 5, 9} {
			b := core.Builder{P: &core.Program{}}
			p := b.P
			lits := func() []core.ExprID {
				var ids []core.ExprID
				for i := 0; i < arity; i++ {
					ids = append(ids, b.LitInt(sp, int64(i)))
				}
				return ids
			}
			ctor := p.Ctors.Push(core.Ctor{Arity: uint32(arity)})
			var params []core.VarID
			for i := 0; i < arity; i++ {
				params = append(params, p.Vars.Push(core.Var{Slot: uint32(i)}))
			}
			callee := p.Funs.Push(core.Fun{Params: params, Body: b.LitUnit(sp), HasBody: true, FrameSize: uint32(arity)})
			makes := p.Funs.Push(core.Fun{Body: b.Con(sp, ctor, lits()), HasBody: true})
			calls := p.Funs.Push(core.Fun{Body: b.Call(sp, b.FunRef(sp, callee), lits()), HasBody: true})
			for name, f := range map[string]core.FunID{"a constructor": makes, "a call": calls} {
				m := New(p, new(base.SourceManager), io.Discard, io.Discard)
				v, ok := m.Call(f, nil)
				if !ok || m.slots != 0 {
					t.Errorf("%s of %d arguments: Call returned %v and left %d slots charged, want true and none", name, arity, ok, m.slots)
				}
				if con, isCon := v.(ConV); name == "a constructor" && (!isCon || len(con.Fields) != arity) {
					t.Errorf("a constructor of %d arguments built %#v, want a ConV with that many fields", arity, v)
				}
			}
		}
	})

	t.Run("stack_limit_setting", func(t *testing.T) {
		// The limit in frames for each value of KI_STACK_MB: the setting times
		// 1<<14 when it is a positive integer, and the default (256 MiB) when it
		// is not. The largest settings are cut so that the shift cannot wrap
		// around into a negative limit, which would overflow the stack of every
		// program on its first call.
		const (
			defaultFrames = 256 << 14
			largest       = (math.MaxInt >> 14) << 14
		)
		rows := []struct {
			name, setting string
			want          int
		}{
			{"unset_or_empty", "", defaultFrames},
			{"one", "1", 1 << 14},
			{"two", "2", 2 << 14},
			{"the_default_written_out", "256", defaultFrames},
			{"above_the_default", "300", 300 << 14},
			{"zero", "0", defaultFrames},
			{"minus_one", "-1", defaultFrames},
			{"minus_256", "-256", defaultFrames},
			{"word", "abc", defaultFrames},
			{"fraction", "1.5", defaultFrames},
			{"with_a_unit", "4MB", defaultFrames},
			{"leading_space", " 4", defaultFrames},
			{"trailing_space", "4 ", defaultFrames},
			{"max_int", "9223372036854775807", largest},
			{"beyond_int", "99999999999999999999", largest},
			{"below_int", "-99999999999999999999", defaultFrames},
		}
		for _, row := range rows {
			if got := framesFor(row.setting); got != row.want {
				t.Errorf("%s: framesFor(%q) = %d, want %d", row.name, row.setting, got, row.want)
			}
		}

		// The limit of the slots reads the same setting: 1<<16 for each mebibyte,
		// four times the frames, because a slot takes 16 bytes and a frame 64. It
		// has its own cut, a little lower than that of the frames.
		const defaultSlots = 256 << 16
		for _, row := range []struct {
			setting string
			want    int
		}{
			{"", defaultSlots},
			{"1", 1 << 16},
			{"2", 2 << 16},
			{"0", defaultSlots},
			{"-1", defaultSlots},
			{"abc", defaultSlots},
			{"9223372036854775807", (math.MaxInt >> 16) << 16},
		} {
			if got := slotsFor(row.setting); got != row.want {
				t.Errorf("slotsFor(%q) = %d, want %d", row.setting, got, row.want)
			}
		}
	})
}
