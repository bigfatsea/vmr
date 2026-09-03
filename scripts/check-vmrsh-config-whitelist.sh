#!/usr/bin/env bash
# Ver 2026-09-03 11:30, by Sonnet 5
set -euo pipefail

# check-vmrsh-config-whitelist.sh asserts that the subcommands whitelisted for
# -c injection in vmr.sh's passthrough() exactly match the cmd/vmr subcommands
# that declare a -c flag (fs.String("c", "config.yaml", ...)).

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

# 1. Derive `want`: cmd/vmr/cmd_*.go files defining a -c flag.
want=()
while IFS= read -r line; do
  [[ -n "$line" ]] && want+=("$line")
done < <(
  for f in "$REPO_ROOT"/cmd/vmr/cmd_*.go; do
    if grep -q 'fs\.String("c", "config\.yaml"' "$f"; then
      base="$(basename "$f")"
      sub="${base#cmd_}"
      sub="${sub%.go}"
      echo "$sub"
    fi
  done | sort -u
)

if [[ ${#want[@]} -eq 0 ]]; then
  echo "ERROR: found zero -c subcommands in $REPO_ROOT/cmd/vmr/cmd_*.go" >&2
  exit 1
fi

# 2. Derive `got`: extracted whitelist in vmr.sh passthrough().
raw_line="$(grep -B1 'has_c_flag "$@" || args=(-c' "$REPO_ROOT/vmr.sh" | head -n1)"
raw_line="${raw_line#"${raw_line%%[![:space:]]*}"}"
raw_line="${raw_line%"${raw_line##*[![:space:]]}"}"
raw_line="${raw_line%)}"

got=()
while IFS= read -r line; do
  [[ -n "$line" ]] && got+=("$line")
done < <(tr '|' '\n' <<< "$raw_line" | sed -e 's/^[[:space:]]*//' -e 's/[[:space:]]*$//' | grep -v '^$' | sort -u)

if [[ ${#got[@]} -eq 0 ]]; then
  echo "ERROR: failed to extract -c whitelist from $REPO_ROOT/vmr.sh" >&2
  exit 1
fi

# 3. Compare want and got.
if ! diff -u <(printf '%s\n' "${want[@]}") <(printf '%s\n' "${got[@]}"); then
  echo "ERROR: vmr.sh -c whitelist does not match cmd/vmr subcommands declaring -c flag." >&2
  echo "want (from cmd/vmr/cmd_*.go): ${want[*]}" >&2
  echo "got  (from vmr.sh):          ${got[*]}" >&2
  exit 1
fi

echo "OK: vmr.sh -c whitelist matches all ${#want[@]} cmd/vmr -c subcommands (${want[*]})"
