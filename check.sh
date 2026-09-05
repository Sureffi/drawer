#!/bin/sh
# check.sh — every oracle this repo has, in one command.
#
# None of these is a golden file. The delta fixtures are checked against the
# text they carry, so prose outside a fence must survive byte for byte and
# every fence must come back either untouched or as one drawn block that
# fits its width. The Go tests are one case per rule written into the code.
#
# Every stage reports through `stage`, which is the only thing here that
# may set `fail`. POSIX exempts every non-final command of an AND-OR list
# from `set -e`, so `cmd && say ok` would let a red suite end on "all clear".
set -e
cd "$(dirname "$0")"
fail=0
say() { printf '%-34s %s\n' "$1" "$2"; }

# stage LABEL CMD... — run it, keep its output, and speak either way.
stage() {
  label=$1
  shift
  if out=$("$@" 2>&1); then
    say "$label" "ok"
  else
    say "$label" "FAILED"
    printf '%s\n' "$out" | head -30 | sed 's/^/    /'
    fail=1
    return 1
  fi
}

mkdir -p bin
# A failed build makes every stage below it a lie. Stop rather than report
# on the last binary that happened to compile.
stage "build" go build -o bin/drawer ./cmd/drawer || exit 1
stage "vet" go vet ./... || true
stage "laws (go test -race)" go test -race ./... || true

for r in cells braille octants; do
  stage "dot: $r" ./bin/drawer -dot fixtures/chain.dot -size 100x14 -render $r || true
done

# A theme file is read by graphviz's parser; the fixture is a second theme
# and must load. There is no offline pixel output, so loading is the check.
stage "theme: fixtures/theme.dot" ./bin/drawer -theme fixtures/theme.dot -dot fixtures/chain.dot -size 100x14 -render cells || true

# The hook wire: recorded delta streams, replayed. `drawer -hook -hooktee`
# writes these straight off a live session, so the corpus is not limited to
# cases somebody thought of.
for r in cells braille; do
  for f in fixtures/deltas-*.jsonl; do
    n=$(basename "$f" .jsonl)
    if out=$(./bin/drawer -deltas "$f" -size 100x40 -render $r 2>&1); then
      say "deltas ($r): $n" "$out"
    else
      say "deltas ($r): $n" "DAMAGED"; echo "$out" | head -4; fail=1
    fi
  done
done

[ $fail -eq 0 ] && say "" "all clear" || say "" "FAILURES ABOVE"
exit $fail
