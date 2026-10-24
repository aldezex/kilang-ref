#!/usr/bin/env bash
# Run on this machine the checks that the CI runs on every push. It is the
# gate of every session tag: a tag is valid when this script is green on it.
#
# Usage: ci/ci-local.sh
#   It works from the root of the tree that contains it, wherever it is called
#   from. It needs no git, so it can also run inside a copy of a tag made with
#   git archive, where there is no repository.
#
# Exit codes: 0 when every check is green, and then the last line it prints is
# "verify: OK"; otherwise the exit code of the check that failed.
set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")/.."

# Keep this machine's own settings out of the checks, as the CI does. Never
# let the go command download another toolchain behind our back (GOTOOLCHAIN).
# An empty GOFLAGS is not enough: go would still read the flags saved with
# "go env -w" in its configuration file, so GOENV=off tells it to ignore that
# file. GOWORK=off keeps a go.work file out of the way.
export GOTOOLCHAIN=local GOFLAGS= GOENV=off GOWORK=off

# 1. The installed Go is the one this tree's go.mod asks for. The script reads
#    the toolchain line of go.mod instead of a literal, so an old tag is still
#    checked against its own patch release of Go.
test "$(go version | awk '{print $3}')" = "$(awk '/^toolchain /{print $2}' go.mod)" || {
  echo "ci-local: go version is $(go version | awk '{print $3}'), but go.mod asks for $(awk '/^toolchain /{print $2}' go.mod)" >&2
  exit 1
}

# 2. Every Go file is gofmt-formatted. The list comes from find, not from git
#    (a copy of a tag has no repository), and find hands the file names to
#    gofmt itself, so a name with a space in it stays one name. Two traps: gofmt
#    -l exits with 0 even when it lists files, so the script checks that its
#    output is empty; and with no file to check find would run nothing and the
#    check would pass without checking anything, so it must find at least one
#    (every tree has cmd/ki/main.go). If gofmt cannot parse a file, find exits
#    with a non-zero code and the assignment ends the script.
go_files=(find . -name '*.go' -not -path './build/*' -not -path './.claude/*' -not -path './.superpowers/*' -not -path './.git/*')
if [ -z "$("${go_files[@]}" -print -quit)" ]; then
  echo "ci-local: found no Go file to check with gofmt" >&2
  exit 1
fi
unformatted=$("${go_files[@]}" -exec gofmt -l {} +)
if [ -n "$unformatted" ]; then
  echo "ci-local: these files need gofmt -w:" >&2
  echo "$unformatted" >&2
  exit 1
fi

# 3. go vet is clean.
go vet ./...

# 4. The ki binary builds. build/bin/ki is the binary to run by hand, as in
#    build/bin/ki --version.
go build -o build/bin/ki ./cmd/ki

# 5. and 6. The tests pass, also under the race detector.
go test ./...
go test -race ./...

# 7. ki cross-compiles for windows/amd64 and linux/arm64, so portability
#    problems show up long before the chapters that target those systems.
GOOS=windows GOARCH=amd64 go build -o /dev/null ./cmd/ki
GOOS=linux GOARCH=arm64 go build -o /dev/null ./cmd/ki

echo "verify: OK"
