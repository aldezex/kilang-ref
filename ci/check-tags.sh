#!/usr/bin/env bash
# Rebuild and test book tags, and record the ones that pass in
# book/verified-tags.txt (spec 11.2). Each tag is extracted with git archive
# into build/tags/<tag>, checked there with that tag's own ci/ci-local.sh, and
# the directory is removed again, whether the check passed or failed.
#
# Usage:
#   ci/check-tags.sh <tag-glob>...
#       Check every tag that matches one of the globs, in the order of
#       `git tag --list --sort=creatordate`. A glob is a shell pattern, so quote
#       it: ci/check-tags.sh 'ch01-s*'. A glob that matches no tag is an error.
#   ci/check-tags.sh --list-as chNN <tag>
#       Rebuild and test <tag> in the same way and write its list of tests to
#       book/tests/chNN.list, leaving verified-tags.txt alone. The prose
#       workflow uses it before the tag chNN exists.
#
# For a tag whose go.mod names a Go release other than the installed one the
# script prints "skip <tag>: needs <goversion>" and moves on, leaving that
# tag's line in book/verified-tags.txt as it is: the tag was verified with its
# own release (docs/toolchain.md lists them). With --list-as there is nothing
# to fall back to, so a tag that needs another release is an error.
#
# For a chapter tag (chNN) it also writes book/tests/chNN.list, from the output
# of `go test -json ./...` run in the extracted tree and filtered by
# `bookx testlist`. For a tag that passed, it replaces or adds the line
#   <tag> <sha40 of the commit> <date of the check, UTC> <go version>
# in book/verified-tags.txt. The SHA is the commit's (<tag>^{commit}), not the
# tag object's, because that is what `bookx verify` compares it with.
#
# Exit codes: 0 every tag was checked or skipped; 1 the first failure (the tags
# after it are not checked); 2 usage error.
set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")/.."

# Never let the go command download another toolchain behind our back.
export GOTOOLCHAIN=local

usage() {
  echo "usage: ci/check-tags.sh <tag-glob>..." >&2
  echo "       ci/check-tags.sh --list-as chNN <tag>" >&2
  exit 2
}

list_as=""
if [ "${1:-}" = "--list-as" ]; then
  [ $# -eq 3 ] || usage
  [[ $2 =~ ^ch[0-9]{2}$ ]] || usage
  list_as=$2
  globs=("$3")
else
  [ $# -ge 1 ] || usage
  globs=("$@")
fi

# What is extracted for the tag being checked; the trap removes it when the
# script ends for any reason, so a failure leaves nothing under build/tags/.
current=""
cleanup() {
  if [ -n "$current" ]; then
    rm -rf "build/tags/$current" "build/tags/$current.json" "build/tags/$current.list"
  fi
  rm -f build/verified-tags.tmp
}
trap cleanup EXIT

fail() {
  echo "check-tags: $*" >&2
  exit 1
}

# matches tells whether tag $1 is selected by the argument $2: a shell pattern,
# or, with --list-as, a tag name.
matches() {
  if [ -n "$list_as" ]; then
    [ "$1" = "$2" ]
  else
    # shellcheck disable=SC2053 # the glob is meant to be a pattern
    [[ $1 == $2 ]]
  fi
}

# Choose the tags: those that match a glob, in the order of their creation.
mapfile -t all_tags < <(git tag --list --sort=creatordate)
selected=()
for g in "${globs[@]}"; do
  found=0
  for t in "${all_tags[@]}"; do
    if matches "$t" "$g"; then found=1; fi
  done
  [ "$found" -eq 1 ] || fail "no tag matches '$g'"
done
for t in "${all_tags[@]}"; do
  for g in "${globs[@]}"; do
    if matches "$t" "$g"; then
      selected+=("$t")
      break
    fi
  done
done

# A tag name goes into paths and into rm -rf: only plain names are accepted.
for t in "${selected[@]}"; do
  [[ $t =~ ^[A-Za-z0-9_][A-Za-z0-9._-]*$ ]] || fail "tag '$t' has a name this script does not accept"
done

# bookx comes from the tree we are in, not from the tag being checked.
mkdir -p build/bin build/tags
go build -o build/bin/bookx ./cmd/bookx

# record_verified replaces or appends the line of tag $1 in verified-tags.txt.
record_verified() {
  local t=$1 line
  line="$t $(git rev-parse "$t^{commit}") $(date -u +%F) $(go version | awk '{print $3}')"
  touch book/verified-tags.txt
  awk -v t="$t" -v line="$line" '
    $1 == t { if (!done) print line; done = 1; next }
    { print }
    END { if (!done) print line }
  ' book/verified-tags.txt > build/verified-tags.tmp
  mv build/verified-tags.tmp book/verified-tags.txt
}

for t in "${selected[@]}"; do
  current=$t
  rm -rf "build/tags/$t" "build/tags/$t.json" "build/tags/$t.list"
  mkdir -p "build/tags/$t"
  git archive "$t" | tar -x -C "build/tags/$t"

  want=$(awk '/^toolchain /{print $2}' "build/tags/$t/go.mod")
  have=$(go version | awk '{print $3}')
  if [ -n "$want" ] && [ "$want" != "$have" ]; then
    if [ -n "$list_as" ]; then
      fail "$t needs $want, but $have is installed, so its list of tests cannot be written"
    fi
    echo "skip $t: needs $want"
    cleanup
    current=""
    continue
  fi

  (cd "build/tags/$t" && ci/ci-local.sh) || fail "$t: ci/ci-local.sh failed"

  if [ -n "$list_as" ] || [[ $t =~ ^ch[0-9]{2}$ ]]; then
    (cd "build/tags/$t" && go test -json ./... > "../$t.json") || fail "$t: go test -json failed"
    build/bin/bookx testlist "build/tags/$t.json" > "build/tags/$t.list" || fail "$t: bookx testlist failed"
    mv "build/tags/$t.list" "book/tests/${list_as:-$t}.list"
  fi
  if [ -z "$list_as" ]; then
    record_verified "$t"
  fi

  cleanup
  current=""
  echo "check-tags: $t OK"
done
