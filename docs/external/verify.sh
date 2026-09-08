#!/usr/bin/env bash
# Enforces the SPEC 4.7 pre-send checklist against the generated .docx.
# Run before sending anything to a design partner. Exit code is the gate.
set -u
D="${1:-Automation-Testing-Platform-Summary.docx}"
[ -f "$D" ] || { echo "missing $D — run: npm install && npm run build"; exit 2; }

# The summary must point at the CURRENT SPEC version. This used to be a literal
# grep for '3.5', which failed on every legitimate version bump and passed on any
# stray "3x5" (unanchored regex, '.' matches anything). The risk SPEC 4.7 is
# guarding against is sending a summary generated from a stale spec, so read the
# version from SPEC.md and require the document to carry it.
SPEC="${SPEC_MD:-../../SPEC.md}"
[ -f "$SPEC" ] || { echo "missing $SPEC — cannot verify the version pointer"; exit 2; }
SPECVER=$(grep -m1 -oE 'Phiên bản:\*\* v[0-9]+\.[0-9]+' "$SPEC" | grep -oE '[0-9]+\.[0-9]+')
[ -n "$SPECVER" ] || { echo "cannot read a version from $SPEC"; exit 2; }

T=$(mktemp)
unzip -p "$D" word/document.xml \
  | sed -e 's/<\/w:p>/\n/g' -e 's/<[^>]*>//g' -e 's/^[[:space:]]*//' \
  | grep -v '^$' > "$T"

ok=0; fail=0
chk() { printf "%-44s " "$1"; if eval "$2"; then echo "PASS"; ok=$((ok+1)); else echo "FAIL"; fail=$((fail+1)); fi; }

chk "no literal ** markers"            "! grep -q '\*\*' $T"
chk "no markdown headings"             "! grep -qE '^#{1,6} ' $T"
chk "no code fences"                   "! grep -q '\`\`\`' $T"
chk "no markdown table pipes"          "! grep -qE '^\|.*\|\$' $T"
chk "MVP item 1 is the Reporter SDK"   "[ \"\$(grep -A1 '^MVP\$' $T | tail -1)\" = 'Reporter SDK cho Playwright' ]"
chk "roadmap shows P0 through P6"      "for p in P0 P1 P2 P3 P4 P5 P6; do grep -qx \"\$p\" $T || exit 1; done"
chk "no Production Safety Guard"       "! grep -qi 'production safety guard' $T"
chk "no Local Agent inside MVP"        "! sed -n '/^MVP\$/,/^Roadmap\$/p' $T | grep -qi 'local agent'"
chk "no unverified competitor claims"  "! grep -q 'CHƯA KIỂM CHỨNG' $T"
# The code span around SPEC.md becomes its own paragraph in the .docx, so the
# pointer arrives split across three lines. Flatten before matching.
chk "points at current SPEC (v$SPECVER)" "tr '\n' ' ' < $T | tr -s ' ' | grep -qF 'SPEC.md v$SPECVER'"
chk "P5/P6 labelled gated by demand"   "grep -qi 'gated by demand' $T"

rm -f "$T"
echo
if [ "$fail" -eq 0 ]; then echo "$ok/$ok passed — cleared to send (SPEC 4.7)"; exit 0
else echo "$fail check(s) failed — DO NOT SEND"; exit 1; fi
