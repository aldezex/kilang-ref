#!/usr/bin/env bash
# Append the real weekly usage to the plan's log, with what was happening.
# It asks one small question of the cheapest model and reads the counter from
# the stream; output tokens do not predict the limit, this does. About 0.09
# units per call (.superpowers/speed/angle-interruptions.md, section 4).
#
# Usage: weekly-probe.sh <label> [log-file]
#   <label>     for example ch11-start
#   log-file    default: the plan workspace's weekly-usage.log
# Prints the line it appended. Exit codes: 0 logged; 1 no counter in the reply; 2 usage.
set -euo pipefail

if [ $# -lt 1 ] || [ $# -gt 2 ]; then
  echo "usage: weekly-probe.sh <label> [log-file]" >&2
  exit 2
fi
label=$1
log=${2:-$(git -C "$(dirname "${BASH_SOURCE[0]}")" rev-parse --show-toplevel)/.superpowers/sdd/2026-10-24-ki-plan-01-part1/weekly-usage.log}

line=$(claude -p ok --model haiku --output-format stream-json --verbose --no-session-persistence </dev/null |
  node -e '
    let text = ""
    process.stdin.on("data", (d) => { text += d })
    process.stdin.on("end", () => {
      for (const l of text.split("\n")) {
        if (!l.includes("rate_limit_event")) continue
        const w = JSON.parse(l).rate_limit_info?.unifiedWindows?.seven_day
        if (w) { console.log("seven_day=" + w.utilization + " resets=" + w.resetsAt); return }
      }
      process.exit(1)
    })') || { echo "weekly-probe.sh: no seven_day counter in the reply" >&2; exit 1; }

printf '%s %s %s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "$label" "$line" | tee -a "$log"
