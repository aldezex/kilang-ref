#!/usr/bin/env bash
# Close a book session: run ci/ci-local.sh and, if it is green, commit the whole
# working tree and put the annotated session tag on that same commit.
#
# Usage: ci/session.sh <tag> "<summary>"
#   <tag>      the session tag, chNN-sK (for example ch01-s1); it must not exist yet
#   <summary>  the text after "<tag>: " in the commit subject and the tag message
#
# Exit codes: 0 committed and tagged; 2 usage error, a tag that is not chNN-sK,
# a tag that already exists, or no ci/ci-local.sh yet (it arrives in Tarea 2);
# any other code is the one of the command that failed (ci-local.sh, git).
set -euo pipefail

if [ $# -ne 2 ]; then
  echo 'usage: ci/session.sh <tag> "<summary>"' >&2
  exit 2
fi
tag=$1
summary=$2

if ! [[ $tag =~ ^ch[0-9]{2}-s[0-9]+$ ]]; then
  echo "session.sh: bad tag '$tag': want chNN-sK, for example ch01-s1" >&2
  exit 2
fi
if [ -z "$summary" ]; then
  echo "session.sh: the summary is empty" >&2
  exit 2
fi

cd "$(git rev-parse --show-toplevel)"

if git rev-parse -q --verify "refs/tags/$tag" >/dev/null; then
  echo "session.sh: tag '$tag' already exists" >&2
  exit 2
fi
if [ ! -f ci/ci-local.sh ]; then
  echo "session.sh: ci/ci-local.sh does not exist yet (Tarea 2 writes it)" >&2
  exit 2
fi

# set -e stops here, with ci-local's exit code, if anything is red.
ci/ci-local.sh

message="$tag: $summary"
git add -A
git commit -m "$message"
git tag -a "$tag" -m "$message"
echo "session.sh: $tag on $(git rev-parse --short=7 HEAD) ($message)"
