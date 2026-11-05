package canary_test

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"
)

// C10 is the canary of the rule of spec 5.10, "Floats idénticos en todos los
// backends": Go may fuse `x*y + z` into one instruction that rounds once, and
// arm64 does it by default, so a primitive of Float that is not written with one
// operation per expression could give other bits on another machine. Go has no
// flag to turn the fusion off, so the canary asks the compiler what it did:
//
//   - on arm64, statically, because no machine can run arm64 code here until the
//     chapter of qemu: `GOARCH=arm64 go build -gcflags=-S` over the code that
//     computes with Floats, and no instruction of the fused family in what it
//     prints;
//   - on amd64, by running: the test TestC10Bits of internal/interp, built with
//     GOAMD64=v1 (which never fuses) and with GOAMD64=v3 (which does), must print
//     the same bits.
//
// A search that finds nothing proves nothing, so each leg has a control that it
// can see something. On arm64 the detector is applied to the prototype of the
// rule (docs/research/proto/c10-floats), which is written to fuse, and must find
// the fused instruction, and the instructions of the Float primitives are looked
// for in the code it clears. On amd64 the same prototype is built at both levels,
// statically (no machine has to run GOAMD64=v3 for that): the v3 build has the
// fused instruction and the v1 build has the multiplication and the addition
// apart, so the run that compares the two compares something that does differ in
// the instructions, and it is the bits that must not.
//
// The test only reads the repository and runs the go command, with the same
// settings as ci/ci-local.sh; it writes only in t.TempDir().

const (
	// repoRoot is the root of the repository seen from this directory.
	repoRoot = "../.."
	module   = "github.com/aldezex/kilang"

	// commandLimit is how long one go command may run before the test gives up on
	// it: the first build of the standard library for another architecture takes
	// a while, and a hung command must not hang the test. It is a bound, and the
	// test never checks how long anything took.
	commandLimit = 5 * time.Minute
)

// fusedMnemonics are the instructions of arm64 that multiply and add with one
// rounding, in double precision.
var fusedMnemonics = []string{"FMADDD", "FMSUBD", "FNMADDD", "FNMSUBD"}

// fusedAMD64 are the instructions of amd64 that do it, in the three operand
// orders of the FMA3 set that GOAMD64=v3 may use: the compiler writes
// VFMADD231SD for `x*y + z`.
var fusedAMD64 = []string{
	"VFMADD132SD", "VFMADD213SD", "VFMADD231SD", "VFMSUB132SD", "VFMSUB213SD", "VFMSUB231SD",
	"VFNMADD132SD", "VFNMADD213SD", "VFNMADD231SD", "VFNMSUB132SD", "VFNMSUB213SD", "VFNMSUB231SD",
}

// goEnv is the environment of every go command here: the settings of
// ci/ci-local.sh, so that nothing of this machine changes the answer, and no cgo,
// because a cross build must not ask for a C cross compiler.
func goEnv(extra ...string) []string {
	return append(os.Environ(), append([]string{
		"GOTOOLCHAIN=local", "GOWORK=off", "GOFLAGS=", "GOENV=off", "CGO_ENABLED=0",
	}, extra...)...)
}

// run runs name with args in dir and returns what it wrote to stdout and to
// stderr. It fails the test if the command fails or does not end in
// commandLimit.
func run(t *testing.T, dir string, env []string, name string, args ...string) (stdout, stderr string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), commandLimit)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir, cmd.Env = dir, env
	var out, errOut bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errOut
	if err := cmd.Run(); err != nil {
		t.Fatalf("%s %s (in %s): %v\n%s", name, strings.Join(args, " "), dir, err, errOut.String())
	}
	return out.String(), errOut.String()
}

// opcodes reads the assembly that `go build -gcflags=-S` writes on stderr and
// returns, for each function, the mnemonics of its instructions in order. A
// function starts at a line that is not indented and has STEXT as its second
// word; an instruction is an indented line whose first field is an offset and
// carries a position, "\t0x0004 00004 (file.go:10)\tFMADDD\tF0, F2, F1, F0". The
// hex dumps of the bytes ("\t0x0000 20 08 40 1f ...") have no position, the
// bookkeeping lines (TEXT, FUNCDATA, PCDATA) are not instructions, and the data
// symbols that follow the functions end the one being read, so a name in them,
// or in a comment, is never taken for an instruction.
func opcodes(asm string) map[string][]string {
	ops := map[string][]string{}
	fn := ""
	for _, line := range strings.Split(asm, "\n") {
		if !strings.HasPrefix(line, "\t") {
			fn = ""
			if f := strings.Fields(line); len(f) > 1 && f[1] == "STEXT" {
				fn = f[0]
				ops[fn] = []string{}
			}
			continue
		}
		f := strings.Split(line, "\t")
		if fn == "" || len(f) < 3 || !strings.HasPrefix(f[1], "0x") || !strings.Contains(f[1], " (") {
			continue
		}
		switch f[2] {
		case "TEXT", "FUNCDATA", "PCDATA":
		default:
			ops[fn] = append(ops[fn], f[2])
		}
	}
	return ops
}

// fused returns, as "function: MNEMONIC", every fused multiply-add instruction of
// arm64 in ops, in the order of the functions' names and of the instructions.
func fused(ops map[string][]string) []string { return fusedIn(ops, fusedMnemonics) }

// fusedIn is fused for the mnemonics in set.
func fusedIn(ops map[string][]string, set []string) []string {
	var found []string
	names := make([]string, 0, len(ops))
	for name := range ops {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		for _, op := range ops[name] {
			if slices.Contains(set, op) {
				found = append(found, name+": "+op)
			}
		}
	}
	return found
}

// arm64Assembly builds the packages at dir (the arguments of go build) for arm64
// and returns the assembly that the compiler wrote for them. -gcflags=-S without
// a pattern applies to the named packages only, not to what they import.
func arm64Assembly(t *testing.T, dir string, packages ...string) map[string][]string {
	t.Helper()
	return assembly(t, dir, goEnv("GOARCH=arm64"), packages...)
}

// amd64Assembly is arm64Assembly for amd64 at the level GOAMD64 gives: "v1" never
// fuses and "v3" does.
func amd64Assembly(t *testing.T, dir, level string, packages ...string) map[string][]string {
	t.Helper()
	return assembly(t, dir, goEnv("GOARCH=amd64", "GOAMD64="+level), packages...)
}

// assembly builds the packages with the environment env and returns the
// instructions of their functions (see opcodes).
func assembly(t *testing.T, dir string, env []string, packages ...string) map[string][]string {
	t.Helper()
	args := append([]string{"build", "-gcflags=-S"}, packages...)
	_, stderr := run(t, dir, env, "go", args...)
	return opcodes(stderr)
}

// cpuRunsGOAMD64v3 reports whether this machine can run the code that GOAMD64=v3
// makes: it needs FMA and AVX2. Only Linux says it in a way that a test can read
// without a package that is not the standard library.
func cpuRunsGOAMD64v3() bool {
	if runtime.GOARCH != "amd64" || runtime.GOOS != "linux" {
		return false
	}
	info, err := os.ReadFile("/proc/cpuinfo")
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(info), "\n") {
		if strings.HasPrefix(line, "flags") {
			flags := strings.Fields(line)
			return slices.Contains(flags, "fma") && slices.Contains(flags, "avx2")
		}
	}
	return false
}

// c10Lines returns the lines of out that TestC10Bits writes for the canary: the
// ones that start with "c10 ".
func c10Lines(out string) []string {
	var lines []string
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "c10 ") {
			lines = append(lines, line)
		}
	}
	return lines
}

func TestC10(t *testing.T) {
	t.Run("detector_reads_only_instructions", func(t *testing.T) {
		// A made-up listing: two functions, the first with a fused instruction
		// and the second with the two separate ones, a hex dump, a data symbol
		// whose name holds a fused mnemonic, and the single-precision cousin
		// that Float never uses.
		const asm = `# example
example.Naive STEXT size=16 args=0x18 locals=0x0 funcid=0x0 leaf
	0x0000 00000 (a.go:9)	TEXT	example.Naive(SB), LEAF|NOFRAME|ABIInternal, $0-24
	0x0000 00000 (a.go:9)	FUNCDATA	$0, gclocals(SB)
	0x0000 00000 (a.go:9)	PCDATA	$3, $1
	0x0000 00000 (a.go:10)	FMADDD	F0, F2, F1, F0
	0x0004 00004 (a.go:10)	RET	(R30)
	0x0000 20 08 40 1f c0 03 5f d6 00 00 00 00 00 00 00 00   .@..._.........
example.Explicit STEXT size=16 args=0x18 locals=0x0 funcid=0x0 leaf
	0x0000 00000 (a.go:19)	FMULD	F1, F0, F1
	0x0004 00004 (a.go:19)	FADDD	F2, F1, F0
	0x0008 00008 (a.go:19)	FMADDS	F0, F2, F1, F0
	0x000c 00012 (a.go:19)	FNMSUBD	F0, F2, F1, F0
example.FMSUBD.arginfo1 SRODATA static dupok size=7 align=0x1
	0x0000 00 08 08 08 10 08 ff                             .......
	0x0007 00007 (a.go:20)	FMSUBD	F0, F2, F1, F0
`
		ops := opcodes(asm)
		wantOps := map[string][]string{
			"example.Naive":    {"FMADDD", "RET"},
			"example.Explicit": {"FMULD", "FADDD", "FMADDS", "FNMSUBD"},
		}
		if len(ops) != len(wantOps) {
			t.Errorf("opcodes found the functions %v, want %v", ops, wantOps)
		}
		for name, want := range wantOps {
			if got := ops[name]; !slices.Equal(got, want) {
				t.Errorf("opcodes of %s = %v, want %v", name, got, want)
			}
		}
		want := []string{"example.Explicit: FNMSUBD", "example.Naive: FMADDD"}
		if got := fused(ops); !slices.Equal(got, want) {
			t.Errorf("fused found %v, want %v: the four double-precision mnemonics only, in the order of the names", got, want)
		}
		for _, mnemonic := range fusedMnemonics {
			if got := fused(map[string][]string{"f": {mnemonic}}); len(got) != 1 {
				t.Errorf("fused does not see %s: %v", mnemonic, got)
			}
		}
	})

	t.Run("detector_sees_fma_in_the_prototype", func(t *testing.T) {
		// The prototype of Tarea 49 is written to fuse: Naive is `x*y + z` in one
		// expression, and arm64 turns it into FMADDD. The canary has to see
		// that, or its silence about the interpreter would mean nothing. Explicit
		// has the conversion that prevents it, and must stay clean.
		ops := arm64Assembly(t, filepath.Join(repoRoot, "docs", "research", "proto", "c10-floats"), ".")
		want := []string{"c10floats.Naive: FMADDD"}
		if got := fused(ops); !slices.Equal(got, want) {
			t.Errorf("the fused instructions in the prototype are %v, want %v", got, want)
		}
		if explicit := ops["c10floats.Explicit"]; !slices.Contains(explicit, "FMULD") || !slices.Contains(explicit, "FADDD") {
			t.Errorf("c10floats.Explicit = %v, want FMULD and FADDD: the assembly was not read", explicit)
		}

		// The same for amd64, which is what the run of the next test relies on: at
		// GOAMD64=v3 the compiler fuses Naive into VFMADD231SD, and at v1 it
		// writes MULSD and ADDSD. Explicit has the same two instructions at both
		// levels.
		proto := filepath.Join(repoRoot, "docs", "research", "proto", "c10-floats")
		v3, v1 := amd64Assembly(t, proto, "v3", "."), amd64Assembly(t, proto, "v1", ".")
		if got, want := fusedIn(v3, fusedAMD64), []string{"c10floats.Naive: VFMADD231SD"}; !slices.Equal(got, want) {
			t.Errorf("the fused instructions in the prototype at GOAMD64=v3 are %v, want %v", got, want)
		}
		if got := fusedIn(v1, fusedAMD64); len(got) != 0 {
			t.Errorf("the fused instructions in the prototype at GOAMD64=v1 are %v, want none", got)
		}
		for _, row := range []struct {
			level, fn string
			ops       map[string][]string
		}{{"v1", "Naive", v1}, {"v1", "Explicit", v1}, {"v3", "Explicit", v3}} {
			if got := row.ops["c10floats."+row.fn]; !slices.Contains(got, "MULSD") || !slices.Contains(got, "ADDSD") {
				t.Errorf("c10floats.%s at GOAMD64=%s = %v, want MULSD and ADDSD: the assembly was not read", row.fn, row.level, got)
			}
		}
	})

	t.Run("arm64_code_that_computes_floats_has_no_fused_instruction", func(t *testing.T) {
		ops := arm64Assembly(t, repoRoot, "./internal/interp", "./kirt/fmtnum")
		if got := fused(ops); len(got) != 0 {
			t.Errorf("the arm64 code of internal/interp and kirt/fmtnum has fused instructions: %v", got)
		}
		// Control: the primitives are in what was read, each with its own
		// instruction, so an empty answer above is not an empty listing.
		prims := ops[module+"/internal/interp.applyPrim"]
		for _, mnemonic := range []string{"FADDD", "FSUBD", "FMULD", "FDIVD", "FNEGD", "FCMPD"} {
			if !slices.Contains(prims, mnemonic) {
				t.Errorf("applyPrim has no %s on arm64: the listing does not hold the primitives of Float (%d instructions read)", mnemonic, len(prims))
			}
		}
		if _, ok := ops[module+"/kirt/fmtnum.Float"]; !ok {
			t.Errorf("the listing has no function fmtnum.Float: kirt/fmtnum was not compiled with -S")
		}
	})

	t.Run("amd64_builds_v1_and_v3_print_the_same_bits", func(t *testing.T) {
		if !cpuRunsGOAMD64v3() {
			t.Skip("this machine cannot be known to run GOAMD64=v3 code (an amd64 Linux with fma and avx2)")
		}
		lines := map[string][]string{}
		for _, level := range []string{"v1", "v3"} {
			bin := filepath.Join(t.TempDir(), "interp-"+level+".test")
			run(t, repoRoot, goEnv("GOAMD64="+level), "go", "test", "-c", "-o", bin, "./internal/interp")
			stdout, _ := run(t, repoRoot, goEnv("KI_C10_BITS=1"), bin, "-test.run", "^TestC10Bits$", "-test.count=1")
			lines[level] = c10Lines(stdout)
		}
		// The rows of TestC10Bits write a line each: an empty answer from both
		// builds would be equal, and prove nothing.
		if len(lines["v1"]) == 0 {
			t.Fatalf("the GOAMD64=v1 build of TestC10Bits wrote no line that starts with \"c10 \"")
		}
		if !slices.Equal(lines["v1"], lines["v3"]) {
			t.Errorf("the builds print other bits:\nGOAMD64=v1:\n%s\nGOAMD64=v3:\n%s",
				strings.Join(lines["v1"], "\n"), strings.Join(lines["v3"], "\n"))
		}
	})
}
