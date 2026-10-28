#!/usr/bin/env bash
# Extract one task of a Ki plan into a brief the implementer reads in one call,
# followed by the plan sections every task refers to, so nobody has to read the
# whole plan (Plan 01, Tarea 0; decisión del plan).
#
# It is the task-brief script of the subagent-driven-development skill, adapted
# to the Ki plans, which are written in Spanish:
#   - A task starts at a heading that matches ^#+[ \t]+Tarea[ \t]+N([^0-9a-z]|$).
#     It ends at the next "Tarea" heading or at the next heading of level 1 or 2
#     ("## Capítulo …", "## Plantilla …"), whichever comes first. Lines inside
#     ``` fences never count as headings.
#   - After the task text it appends, verbatim, the plan's "## Global Constraints"
#     and "## Convenciones del plan" sections (each from its heading up to the
#     next line "---") and, when the task text contains "prosa del capítulo",
#     "## Plantilla de las tareas de prosa" (up to the next "## " heading or the
#     end of the file).
#
# Usage: task-brief.sh PLAN_FILE N [OUTFILE]
# Default OUTFILE: $(bash <skill>/scripts/sdd-workspace PLAN_FILE)/task-N-brief.md,
# where <skill> is $SDD_SKILL_DIR when set and otherwise the newest installed
# superpowers skill ~/.claude/plugins/cache/claude-plugins-official/superpowers/
# <version>/skills/subagent-driven-development.
# Exit codes: 0 written; 2 usage error, no plan file or no skill; 3 the task or a
# required section is not in the plan.
set -euo pipefail

if [ $# -lt 2 ] || [ $# -gt 3 ]; then
  echo "usage: task-brief.sh PLAN_FILE N [OUTFILE]" >&2
  exit 2
fi

plan=$1
n=$2
[ -f "$plan" ] || { echo "no such plan file: $plan" >&2; exit 2; }
# A task number is digits with an optional lowercase letter, as in 32a (a task
# inserted after the plan was written).
[[ $n =~ ^[0-9]+[a-z]?$ ]] || { echo "the task number must be digits with an optional letter: $n" >&2; exit 2; }
n="$((10#${n%%[a-z]}))${n##*[0-9]}"

if [ $# -eq 3 ]; then
  out=$3
else
  skill=${SDD_SKILL_DIR:-}
  if [ -z "$skill" ]; then
    skill=$(printf '%s\n' "$HOME"/.claude/plugins/cache/claude-plugins-official/superpowers/*/skills/subagent-driven-development | sort -V | tail -n 1)
  fi
  [ -f "$skill/scripts/sdd-workspace" ] || {
    echo "cannot find the subagent-driven-development skill (set SDD_SKILL_DIR): $skill" >&2
    exit 2
  }
  # Invoke via bash, as the skill does: unpacking can drop the exec bits.
  dir=$(bash "$skill/scripts/sdd-workspace" "$plan")
  out="$dir/task-${n}-brief.md"
fi

# The text of task $n.
task=$(awk -v n="$n" '
  /^```/ { infence = !infence }
  !infence && /^#+[ \t]/ {
    if ($0 ~ /^#+[ \t]+Tarea[ \t]+[0-9]+/) {
      intask = ($0 ~ ("^#+[ \t]+Tarea[ \t]+" n "([^0-9a-z]|$)"))
    } else if ($0 ~ /^##?[ \t]/) {
      intask = 0
    }
  }
  intask { print }
' "$plan")
if [ -z "$task" ]; then
  echo "task ${n} not found in ${plan} (no heading matching 'Tarea ${n}')" >&2
  exit 3
fi

# section HEADING STOP: the plan section whose heading line is exactly HEADING,
# up to (not including) the next line "---" (STOP=rule) or the next "## "
# heading (STOP=h2), or the end of the file.
section() {
  awk -v h="$1" -v stop="$2" '
    /^```/ { infence = !infence }
    !insec && !infence { line = $0; sub(/[ \t\r]+$/, "", line); if (line == h) { insec = 1; print; next } }
    insec && !infence && stop == "rule" && /^---[ \t\r]*$/ { exit }
    insec && !infence && stop == "h2" && /^##[ \t]/ { exit }
    insec { print }
  ' "$plan"
}

need_section() {
  local text
  text=$(section "$1" "$2")
  if [ -z "$text" ]; then
    echo "section '$1' not found in ${plan}" >&2
    exit 3
  fi
  printf '%s\n' "$text"
}

constraints=$(need_section '## Global Constraints' rule)
conventions=$(need_section '## Convenciones del plan' rule)
template=
if grep -q 'prosa del capítulo' <<<"$task"; then
  template=$(need_section '## Plantilla de las tareas de prosa' h2)
fi

{
  printf '%s\n' "$task"
  printf '\n%s\n' "$constraints"
  printf '\n%s\n' "$conventions"
  if [ -n "$template" ]; then
    printf '\n%s\n' "$template"
  fi
} > "$out"

echo "wrote ${out}: $(wc -l < "$out" | tr -d ' ') lines"
