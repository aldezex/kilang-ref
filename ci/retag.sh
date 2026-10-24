#!/usr/bin/env bash
# Move an existing tag to another commit as an annotated tag that keeps the
# subject of its message. Used after a fixup rebase (roadmap, «Correcciones»).
#
# Usage: ci/retag.sh <tag> [<commit>]      (<commit> defaults to HEAD)
#
# Exit codes: 0 moved; 1 the tag does not exist; 2 usage error or <commit> is
# not a commit.
set -euo pipefail

if [ $# -lt 1 ] || [ $# -gt 2 ]; then
  echo "usage: ci/retag.sh <tag> [<commit>]" >&2
  exit 2
fi
tag=$1
commit=${2:-HEAD}

if ! git rev-parse -q --verify "refs/tags/$tag" >/dev/null; then
  echo "retag.sh: tag '$tag' does not exist" >&2
  exit 1
fi
if ! target=$(git rev-parse -q --verify "$commit^{commit}"); then
  echo "retag.sh: '$commit' is not a commit" >&2
  exit 2
fi

# Read the subject before -f replaces the tag object.
subject=$(git tag -l --format='%(contents:subject)' "$tag")
git tag -f -a "$tag" "$target" -m "$subject"
