package c10floats

import (
	"bytes"
	"os"
	"os/exec"
	"slices"
	"strings"
	"testing"
)

// This test reproduces the fact of R4 V11 (docs/research/r4-go-curriculum.md)
// as it was checked on 2026-11-05 with go1.27.3: Go lets the compiler fuse
// x*y + z into one multiply-add, and an explicit float64(x*y) conversion
// prevents it.
//
//	arm64:       Naive uses FMADDD; Explicit uses FMULD and FADDD.
//	amd64 v3:    Naive uses VFMADD231SD; Explicit uses MULSD and ADDSD.
//	amd64 v1:    nothing fuses; both use MULSD and ADDSD.
//
// It is the source of the canary of Tarea 50 (kirt/canary, TestC10), which
// asks the same question about the real interpreter. It runs with
// GOTOOLCHAIN=local, from this directory: GOTOOLCHAIN=local go test ./...

// opcodes builds the package in this directory for goarch (and goamd64, when
// it is not empty) with -gcflags=-S and returns, for each function, the
// mnemonics of its instructions in order, without the bookkeeping lines
// (TEXT, FUNCDATA, PCDATA) that the compiler prints too.
//
// The compiler prints the assembly on stderr. go build replays it when the
// package is already in the build cache, so a second run sees the same text.
func opcodes(t *testing.T, goarch, goamd64 string) map[string][]string {
	t.Helper()
	cmd := exec.Command("go", "build", "-gcflags=-S", ".")
	// The same four settings as ci/ci-local.sh, so nothing of this machine
	// changes the answer; and CGO_ENABLED=0, because a cross build must not
	// ask for a C cross compiler this package does not need.
	cmd.Env = append(os.Environ(),
		"GOTOOLCHAIN=local", "GOWORK=off", "GOFLAGS=", "GOENV=off",
		"CGO_ENABLED=0", "GOARCH="+goarch)
	if goamd64 != "" {
		cmd.Env = append(cmd.Env, "GOAMD64="+goamd64)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("GOARCH=%s GOAMD64=%s go build -gcflags=-S: %v\n%s", goarch, goamd64, err, stderr.String())
	}

	ops := map[string][]string{}
	fn := ""
	for _, line := range strings.Split(stderr.String(), "\n") {
		if !strings.HasPrefix(line, "\t") {
			// A header line: "c10floats.Naive STEXT size=16 ...". Any other
			// unindented line (a data symbol, the "# c10floats" line of go
			// build) ends the function being read.
			fn = ""
			if f := strings.Fields(line); len(f) > 1 && f[1] == "STEXT" {
				fn = strings.TrimPrefix(f[0], "c10floats.")
				ops[fn] = []string{}
			}
			continue
		}
		// An instruction: "\t0x0004 00004 (fma.go:10)\tFMADDD\tF0, F2, F1, F0".
		// The hex dump of its bytes ("\t0x0000 20 08 40 1f ...") has no
		// second tab, so it is not taken for one.
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

// fused reports whether op is a fused multiply-add of arm64 (FMADDD, FMSUBD,
// FNMADDD, FNMSUBD) or of amd64 (VFMADD231SD, VFNMSUB132SD, ...).
func fused(op string) bool {
	for _, prefix := range []string{"FMADD", "FMSUB", "FNMADD", "FNMSUB", "VFMADD", "VFMSUB", "VFNMADD", "VFNMSUB"} {
		if strings.HasPrefix(op, prefix) {
			return true
		}
	}
	return false
}

// anyFused reports whether one of ops is a fused multiply-add.
func anyFused(ops []string) bool { return slices.ContainsFunc(ops, fused) }

func TestFMA(t *testing.T) {
	// fusesNaiveOnly checks the shape of the two functions under one build:
	// Naive has the fused instruction and not the separate pair, Explicit has
	// the separate pair and no fused instruction. Checking Explicit on its
	// own is what stops a build that fuses everything from passing, and the
	// pair in Explicit is what proves that the assembly was really read.
	fusesNaiveOnly := func(t *testing.T, ops map[string][]string, fusedOp, mul, add string) {
		t.Helper()
		naive, explicit := ops["Naive"], ops["Explicit"]
		if !slices.Contains(naive, fusedOp) {
			t.Errorf("Naive = %v, want %s", naive, fusedOp)
		}
		if slices.Contains(naive, mul) || slices.Contains(naive, add) {
			t.Errorf("Naive = %v, want no %s and no %s next to the fused instruction", naive, mul, add)
		}
		if !slices.Contains(explicit, mul) || !slices.Contains(explicit, add) {
			t.Errorf("Explicit = %v, want %s and %s", explicit, mul, add)
		}
		if anyFused(explicit) {
			t.Errorf("Explicit = %v, want no fused instruction: float64(x*y) must prevent the fusion", explicit)
		}
	}

	t.Run("arm64_fuses_naive_only", func(t *testing.T) {
		fusesNaiveOnly(t, opcodes(t, "arm64", ""), "FMADDD", "FMULD", "FADDD")
	})
	t.Run("amd64_v3_fuses_naive_only", func(t *testing.T) {
		fusesNaiveOnly(t, opcodes(t, "amd64", "v3"), "VFMADD231SD", "MULSD", "ADDSD")
	})
	t.Run("amd64_v1_never_fuses", func(t *testing.T) {
		ops := opcodes(t, "amd64", "v1")
		for _, name := range []string{"Naive", "Explicit"} {
			got := ops[name]
			if anyFused(got) {
				t.Errorf("%s = %v, want no fused instruction under GOAMD64=v1", name, got)
			}
			if !slices.Contains(got, "MULSD") || !slices.Contains(got, "ADDSD") {
				t.Errorf("%s = %v, want MULSD and ADDSD", name, got)
			}
		}
	})
}
