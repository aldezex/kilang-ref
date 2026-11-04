#!/usr/bin/env bash
# Proof, in a clone, of decision 5: the chapter tag on a side commit, with two
# chapters in a row and the second one's sessions already on main when the first
# one lands. It replays chapters 9 and 10: their real sessions and their real
# prose as they were at the tags. It touches nothing but the directory it is
# given, and leaves it for the caller to delete. Results: side-commit-tag.md.
#
# Usage: prove-side-commit.sh <new-scratch-dir>
# Exit codes: 0 every check held; 1 a check failed; 2 usage.
set -euo pipefail

if [ $# -ne 1 ] || [ -e "$1" ]; then
  echo "usage: prove-side-commit.sh <new-scratch-dir>" >&2
  exit 2
fi
repo=$(git -C "$(dirname "${BASH_SOURCE[0]}")" rev-parse --show-toplevel)
mkdir -p "$1"
work=$(cd "$1" && pwd)
export GOTOOLCHAIN=local
ok() { printf 'ok    %s\n' "$*"; }
fail() { printf 'FAIL  %s\n' "$*"; exit 1; }

# ---------- the clone, back at the last session of chapter 9
git clone -q "$repo" "$work/clone"
cd "$work/clone"
git config user.name proof
git config user.email proof@example.invalid
git config advice.detachedHead false
x9=$(git rev-parse 'ch09-s3^{commit}')
declare -a sessions=()
for k in 1 2 3 4; do sessions+=("$(git rev-parse "ch10-s$k^{commit}")"); done
mkdir -p "$work/orig"
git show ch09:book/ch09-core.md >"$work/orig/ch09-core.md"
git show ch10:book/ch10-cek-interpreter.md >"$work/orig/ch10-cek-interpreter.md"
msg9=$(git tag -l --format='%(contents:subject)' ch09)
msg10=$(git tag -l --format='%(contents:subject)' ch10)
declare -A sessionmsg=()
for k in 1 2 3 4; do sessionmsg[$k]=$(git tag -l --format='%(contents:subject)' "ch10-s$k"); done
git tag -d ch09 ch10 m1 ch10-s1 ch10-s2 ch10-s3 ch10-s4 >/dev/null
git checkout -q -B main "$x9"
git init -q --bare "$work/remote.git"
git remote set-url origin "$work/remote.git"
git push -q origin "$x9:refs/heads/main"

# ---------- the side commit of a chapter, and its landing as one forward commit on main
# merge_lines A B: add to A the lines of B it does not have, in B's order.
merge_lines() { grep -vxFf "$1" "$2" >>"$1" || true; }
side() { # nn last-session prose-file message
  local nn=$1 last=$2 file=$3 message=$4
  git checkout -q --detach "$last"
  ci/check-tags.sh --list-as "ch$nn" "$last" >/dev/null
  ci/check-tags.sh "ch$nn-s*" >/dev/null
  cp "$work/orig/$file" "book/$file"
  git add book
  git commit -q -m "book: chapter $nn prose"
  git tag -a "ch$nn" -m "$message"
  ci/check-tags.sh "ch$nn" >/dev/null
  mkdir -p "$work/land-$nn"
  cp book/verified-tags.txt "$work/land-$nn/verified-tags.txt"
  cp "book/tests/ch$nn.list" "$work/land-$nn/ch$nn.list"
  git restore book/verified-tags.txt "book/tests/ch$nn.list"
  git checkout -q main
}
land() { # nn prose-file
  local nn=$1 file=$2
  git show "ch$nn:book/$file" >"book/$file"
  cp "$work/land-$nn/ch$nn.list" "book/tests/ch$nn.list"
  merge_lines book/verified-tags.txt "$work/land-$nn/verified-tags.txt"
  git add book
  git commit -q -m "book: land ch$nn"
}

# ---------- chapter 10's sessions go on main while chapter 9 is still in prose
for k in 1 2 3 4; do
  git cherry-pick "${sessions[$((k - 1))]}" >/dev/null
  git tag -a "ch10-s$k" -m "${sessionmsg[$k]}"
done
parent=$(git rev-parse 'ch10-s1^{commit}^')
[ "$parent" = "$x9" ] && ok "ch10-s1's parent is ch09-s3, not chapter 9's prose" || fail "ch10-s1's parent"

# Chapter 9 lands. Nothing below it moves: no rebase and no retag.
before=$(git for-each-ref --format='%(refname) %(objectname)' 'refs/tags/ch10-s*')
side 09 ch09-s3 ch09-core.md "$msg9"
land 09 ch09-core.md
after=$(git for-each-ref --format='%(refname) %(objectname)' 'refs/tags/ch10-s*')
[ "$before" = "$after" ] && ok "chapter 9 landed and no ch10-s* tag moved" || fail "tags moved"
git merge-base --is-ancestor 'ch09^{commit}' main && fail "ch09 is on main" || ok "ch09 is on a side commit, not on main"
[ "$(git rev-parse 'ch09^{commit}^')" = "$x9" ] && ok "ch09's parent is ch09-s3" || fail "ch09's parent"

# Finished chapters only, and no force: the push of chapter 9 is an ordinary one.
land9=$(git rev-parse HEAD)
git push -q origin "$land9:refs/heads/main" refs/tags/ch09 'refs/tags/ch09-s*:refs/tags/ch09-s*'
ok "pushed chapter 9 with no force"

# ---------- chapter 10, the second in a row
side 10 ch10-s4 ch10-cek-interpreter.md "$msg10"
land 10 ch10-cek-interpreter.md
git push -q origin "$(git rev-parse HEAD):refs/heads/main" refs/tags/ch10 'refs/tags/ch10-s*:refs/tags/ch10-s*'
ok "pushed chapter 10 with no force"
git merge-base --is-ancestor 'ch09^{commit}' 'ch10^{commit}' && fail "ch10 descends from ch09" || ok "ch10's tree holds ch10's code and prose; ch09's prose is on main only"

# ---------- what check-tags, verify and the page say
ci/check-tags.sh ch09 'ch10-s*' ch10 >"$work/check-tags.out" 2>&1 && ok "ci/check-tags.sh ch09 'ch10-s*' ch10: all pass" || { tail -20 "$work/check-tags.out"; fail "check-tags"; }
git status --short >"$work/status.out"
if [ -s "$work/status.out" ]; then
  # verified-tags dates may change if the day changes; the lists must not.
  git diff --stat -- book/tests | grep -q . && fail "a re-check changed a test list"
  git restore book/verified-tags.txt
fi
go build -o build/bin/bookx ./cmd/bookx
build/bin/bookx verify | grep -q 'bookx verify: OK' && ok "bookx verify at main: OK" || fail "bookx verify"
build/bin/bookx web build/web >/dev/null
for nn in 09 10; do
  [ -f "build/web/book/ch$nn.md" ] || fail "ch$nn is not on the page"
  grep -q "T$nn.01" "build/web/book/ch$nn.md" && ok "page: ch$nn is published and shows card T$nn.01" || fail "ch$nn has no cards"
done

# The same pages as the real history, byte for byte.
git clone -q "$repo" "$work/real"
git -C "$work/real" checkout -q 'ch10^{commit}'
git -C "$work/real" fetch -q origin 'refs/tags/*:refs/tags/*'
(cd "$work/real" && go build -o "$work/real-bookx" ./cmd/bookx && "$work/real-bookx" web --all "$work/real-web" >/dev/null)
build/bin/bookx web --all "$work/side-web" >/dev/null
for nn in 09 10; do
  if cmp -s "$work/side-web/book/ch$nn.md" "$work/real-web/book/ch$nn.md"; then
    ok "page of ch$nn identical to the real history's"
  else
    diff "$work/real-web/book/ch$nn.md" "$work/side-web/book/ch$nn.md" | head -20
    fail "page of ch$nn differs"
  fi
done

# ---------- the reader: tags come with a fetch though the chapter's commit is not on main
git init -q "$work/reader"
git -C "$work/reader" remote add --no-tags ref "$work/remote.git"
git -C "$work/reader" fetch -q ref 'refs/tags/*:refs/tags/ref/*'
git -C "$work/reader" cat-file -e ref/ch09:book/ch09-core.md && git -C "$work/reader" cat-file -e ref/ch10:book/ch10-cek-interpreter.md && ok "the reader's fetch brings ref/ch09 and ref/ch10 with their prose" || fail "reader"

# ---------- what ci/retag-all.sh does today
if out=$(ci/retag-all.sh 2>&1); then
  fail "retag-all.sh accepted a side commit: $out"
else
  ok "retag-all.sh today refuses, and moves nothing: $(printf '%s' "$out" | head -1)"
fi

# The change it needs: a chapter tag whose commit is not on main stays where it
# is, and so does a milestone on it. Earlier chapter tags are still checked.
sed -i "/elif \[\[ \$tag =~ \^ch(\[0-9\]{2})\\$ \]\]; then/a\\    git merge-base --is-ancestor \"\$tag^{commit}\" main || continue # a side commit does not move with main" ci/retag-all.sh
out=$(ci/retag-all.sh 2>&1) && ok "retag-all.sh with the change: runs, moves: ${out:-nothing}" || fail "retag-all.sh with the change: $out"

git tag -a m2 'ch10^{commit}' -m 'a milestone on a side commit'
sed -i 's/declare -A milestone_chapter=(\[m1\]=ch10)/declare -A milestone_chapter=([m2]=ch10)/' ci/retag-all.sh
if out=$(ci/retag-all.sh 2>&1); then fail "retag-all.sh placed a milestone it cannot see: $out"; else ok "a milestone on a side commit needs a second line: $(printf '%s' "$out" | head -1)"; fi
sed -i 's/^  chapter=\${milestone_chapter\[\$tag\]:-}$/&\n  git merge-base --is-ancestor "$tag^{commit}" main || continue # on a side commit, like its chapter tag/' ci/retag-all.sh
out=$(ci/retag-all.sh 2>&1) && ok "retag-all.sh with both lines: runs, moves: ${out:-nothing}" || fail "retag-all.sh with both lines: $out"
[ "$(git rev-parse 'm2^{commit}')" = "$(git rev-parse 'ch10^{commit}')" ] || fail "m2 moved"
git tag -d m2 >/dev/null
git diff -- ci/retag-all.sh >"$work/retag.diff"
cat "$work/retag.diff"
git restore ci/retag-all.sh

git log --format='%h %s' main -7
ok "all checks held"
