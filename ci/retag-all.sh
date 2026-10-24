#!/usr/bin/env bash
# After a fixup rebase of main, put every book tag back on its commit:
#   chNN-sK  the commit on main whose subject starts with "chNN-sK: "
#   chNN     the commit on main whose subject is "book: chapter NN prose"
#   mN       the commit of its chapter tag (m1: ch10)
# Run it after `git -c sequence.editor=: rebase --autosquash --update-refs <base>`,
# never before: it refuses while main still holds fixup!/squash!/amend! commits.
#
# Prints "<tag> <old7> -> <new7>" for every tag it moves and nothing for the
# tags already in place. It checks every tag before moving any: a subject that
# is missing from main or appears twice, a tag of unknown shape (ch*/m*) or a
# milestone without a known chapter is an error and nothing moves.
#
# Usage: ci/retag-all.sh
# Exit codes: 0 done; 1 a check failed; 2 usage error.
set -euo pipefail

if [ $# -ne 0 ]; then
  echo "usage: ci/retag-all.sh" >&2
  exit 2
fi

root=$(git rev-parse --show-toplevel)
cd "$root"

# The chapter whose tag each milestone shares a commit with. Later plans add
# their milestones here (spec 12.4).
declare -A milestone_chapter=([m1]=ch10)

log=$(git log main --format='%H%x09%s')

# Hashes of the commits on main whose subject starts with $1 (prefix) or is $1
# (exact). Fixed strings, not regular expressions.
with_prefix() {
  printf '%s\n' "$log" | awk -v p="$1" '{ t = index($0, "\t"); if (index(substr($0, t + 1), p) == 1) print substr($0, 1, t - 1) }'
}
with_subject() {
  printf '%s\n' "$log" | awk -v p="$1" '{ t = index($0, "\t"); if (substr($0, t + 1) == p) print substr($0, 1, t - 1) }'
}

pending=$(printf '%s\n' "$log" | cut -f2- | grep -cE '^(fixup|squash|amend)! ' || true)
if [ "$pending" -ne 0 ]; then
  echo "retag-all.sh: main has $pending fixup!/squash!/amend! commit(s); run the autosquash rebase first" >&2
  exit 1
fi

declare -A target=()
failed=0

for tag in $(git tag --list); do
  if [[ $tag =~ ^ch[0-9]{2}-s[0-9]+$ ]]; then
    what="whose subject starts with '$tag: '"
    mapfile -t found < <(with_prefix "$tag: ")
  elif [[ $tag =~ ^ch([0-9]{2})$ ]]; then
    what="whose subject is 'book: chapter ${BASH_REMATCH[1]} prose'"
    mapfile -t found < <(with_subject "book: chapter ${BASH_REMATCH[1]} prose")
  elif [[ $tag =~ ^m[0-9]+$ ]]; then
    continue # placed below, once its chapter tag is
  elif [[ $tag == ch* || $tag == m* ]]; then
    echo "retag-all.sh: $tag: unknown tag shape (want chNN-sK, chNN or mN)" >&2
    failed=1
    continue
  else
    continue
  fi
  if [ "${#found[@]}" -ne 1 ]; then
    echo "retag-all.sh: $tag: want exactly one commit on main $what, found ${#found[@]}" >&2
    failed=1
    continue
  fi
  target[$tag]=${found[0]}
done

for tag in $(git tag --list 'm*'); do
  [[ $tag =~ ^m[0-9]+$ ]] || continue
  chapter=${milestone_chapter[$tag]:-}
  if [ -z "$chapter" ]; then
    echo "retag-all.sh: $tag: no chapter known for this milestone (add it to milestone_chapter)" >&2
    failed=1
  elif [ -z "${target[$chapter]:-}" ]; then
    echo "retag-all.sh: $tag: its chapter tag $chapter is missing or was not placed" >&2
    failed=1
  else
    target[$tag]=${target[$chapter]}
  fi
done

if [ "$failed" -ne 0 ]; then
  echo "retag-all.sh: nothing moved" >&2
  exit 1
fi

for tag in $(printf '%s\n' "${!target[@]}" | sort -V); do
  old=$(git rev-parse "$tag^{commit}")
  new=${target[$tag]}
  [ "$old" != "$new" ] || continue
  "$root/ci/retag.sh" "$tag" "$new" >/dev/null
  echo "$tag ${old:0:7} -> ${new:0:7}"
done
