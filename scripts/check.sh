#!/bin/sh
# scripts/check.sh — every oracle this repo has, in one command.
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
cd "$(dirname "$0")/.."
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

# The layers, and what each of them may import — every package this module
# has, the binary's included, in what it imports for the build and for its
# laws alike: a law reaching sideways is the same edge as any other. A cycle
# is the compiler's to catch; a sideways edge — a rung importing a rung,
# notice importing cells — compiles fine and is the thing this asserts.
# drawer is the top and may import any of them.
layers_table() {
  cat <<'TABLE'
  grid       (nothing internal)
  term       (nothing internal)
  layout     grid
  theme      layout
  fence      grid layout
  notice     grid layout
  cells      grid layout
  pixel      grid term layout theme
  drawer     grid term layout theme fence notice cells pixel
  cmd/drawer drawer
TABLE
}
# sideways reads the listing on stdin and prints every import that is not
# in the table. A function rather than a loop inside a substitution: the sh
# on macOS is bash 3.2, which cannot parse a case inside $( ), and the
# first push to a Mac runner stopped here with a syntax error.
sideways() {
  while read -r p rest; do
    pkg=${p#github.com/sureffi/drawer/}
    pkg=${pkg#internal/}
    allowed=$(layers_table | awk -v k="$pkg" '$1 == k { $1 = ""; print }')
    for i in $rest; do
      case $i in
      github.com/sureffi/drawer/*) ;;
      *) continue ;;
      esac
      d=${i#github.com/sureffi/drawer/}
      d=${d#internal/}
      case " $allowed " in
      *" $d "*) ;;
      *) echo "$pkg -> $d is not a layer $pkg may import" ;;
      esac
    done
  done
}
layers() {
  # The listing is taken first and on its own. A package that will not load
  # makes go list answer nothing, and nothing reads as no sideways edges —
  # the stage passing on a tree it never saw.
  list=$(go list -f '{{.ImportPath}} {{join .Imports " "}} {{join .TestImports " "}} {{join .XTestImports " "}}' \
    ./cmd/... ./internal/...) || return 1
  bad=$(printf '%s\n' "$list" | sideways | sort -u)
  [ -z "$bad" ] || { printf '%s\n\nthe layers, and what each may import:\n' "$bad"; layers_table; return 1; }
}

mkdir -p bin
# A failed build makes every stage below it a lie. Stop rather than report
# on the last binary that happened to compile.
stage "build" go build -o bin/drawer ./cmd/drawer || exit 1
stage "vet" go vet ./... || true
# macOS is a platform this repo ships and nobody here runs. The build-tagged
# files compile on every push or they compile on nobody's machine.
stage "vet: darwin" env GOOS=darwin GOARCH=arm64 go vet ./... || true
stage "layers" layers || true
gofmt_clean() { f=$(gofmt -l .); [ -z "$f" ] || { printf '%s\n' "$f"; return 1; }; }
stage "gofmt" gofmt_clean || true
stage "laws (go test -race)" go test -race ./... || true

# The two rungs, on the source the harness and the delta fixture both carry.
# The pixels rung is stood up by the -png stage below, which is the one cut
# that needs no terminal; here the glyphs draw, and draw again in the pens
# the graph asked for.
stage "dot: cells" ./bin/drawer -dot testdata/chain.dot -size 100x14 -render cells || true
stage "dot: colour cells" ./bin/drawer -dot testdata/colour.dot -size 100x20 -render cells || true

# A theme file is read by graphviz's parser; the example themes must load,
# and the theme in force — Claude Code's, derived — must print as DOT.
for f in themes/tokyonight.dot themes/tokyonight-day.dot; do
  stage "theme: $f" ./bin/drawer -theme $f -dot testdata/chain.dot -size 100x14 -render cells || true
done
stage "show-theme" ./bin/drawer -show-theme || true
stage "context" ./bin/drawer -context || true

# The plugin's manifest and marketplace, as Claude Code reads them. Only
# where claude is on the PATH; the files are still in the repo either way.
if command -v claude >/dev/null 2>&1; then
  stage "plugin: validate" claude plugin validate . --strict || true
else
  say "plugin: validate" "skipped (no claude)"
fi
# The wrapper, on this checkout: a built bin/drawer is linked into a scratch
# data dir and the context line comes back through it, then the hook route
# draws a fence through the same link.
stage "plugin: sh -n" sh -n scripts/drawer scripts/release.sh || true
rm -rf bin/pdata
session_speaks() {
  CLAUDE_PLUGIN_ROOT=. CLAUDE_PLUGIN_DATA=bin/pdata ./scripts/drawer session </dev/null |
    grep -q '^drawer: a ```dot fence'
}
hook_draws() {
  printf '{"delta":"```dot\\ndigraph{a->b}\\n```\\n","final":true,"message_id":"chk","index":0}' |
    CLAUDE_PLUGIN_ROOT=. CLAUDE_PLUGIN_DATA=bin/pdata DRAWER_STATE=bin/pdata ./scripts/drawer hook |
    grep -q displayContent
}
stage "plugin: session" session_speaks || true
stage "plugin: hook" hook_draws || true
# The hook with no binary in place — the session that never ran, because
# the plugin was installed mid-session and reloaded — puts the checkout's
# binary there itself and draws through it.
rm -rf bin/pdata-cold
hook_cold() {
  printf '{"delta":"```dot\\ndigraph{a->b}\\n```\\n","final":true,"message_id":"cold","index":0}' |
    CLAUDE_PLUGIN_ROOT=. CLAUDE_PLUGIN_DATA=bin/pdata-cold DRAWER_STATE=bin/pdata-cold ./scripts/drawer hook |
    grep -q displayContent
}
stage "plugin: hook, cold" hook_cold || true

# The pixels rung, to a file: the same cut the hook makes. Nothing gates
# this — the picture is painted in the binary, so a box that can run the
# laws can draw one.
stage "png: testdata/chain.dot" ./bin/drawer -dot testdata/chain.dot -png bin/chain.png -size 100x40 || true

# The hook wire: recorded delta streams, replayed. `drawer -hook -hooktee`
# writes these straight off a live session, so the corpus is not limited to
# cases somebody thought of.
for f in testdata/deltas-*.jsonl; do
  n=$(basename "$f" .jsonl)
  if out=$(./bin/drawer -deltas "$f" -size 100x40 -render cells 2>&1); then
    say "deltas: $n" "$out"
  else
    say "deltas: $n" "DAMAGED"; echo "$out" | head -4; fail=1
  fi
done

[ $fail -eq 0 ] && say "" "all clear" || say "" "FAILURES ABOVE"
exit $fail
