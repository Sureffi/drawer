# drawer

[![check](https://github.com/sureffi/drawer/actions/workflows/check.yml/badge.svg)](https://github.com/sureffi/drawer/actions/workflows/check.yml)

A ```dot fence in Claude Code becomes a drawing, in place, as the reply
streams.

![a live session](demo/session.gif)

Two commands to install.

    /plugin marketplace add sureffi/drawer
    /plugin install drawer@drawer

Then ask for a graph. The model writes DOT; you see the picture.

## what you get

Linux and macOS, amd64 and arm64. Layout is graphviz, compiled into the
binary, so there is no `dot` to install. What the drawing is made of
depends on the terminal — best first, each failing open to the one below,
chosen by `auto`, which reads the terminal, or by `DRAWER_RENDER`:

| rung      | draws                                             | needs                              |
|-----------|---------------------------------------------------|------------------------------------|
| `pixels`  | graphviz's own picture, in Claude Code's theme, in the transcript | a local kitty on Linux, not through tmux |
| `octants` | strokes at 2×4 per cell, solid; labels as glyphs  | a terminal that draws Unicode 16 octants itself: kitty, ghostty |
| `braille` | the same, dotted                                  | any font — every one has braille   |
| `cells`   | box-drawing characters, routed edges              | nothing but this binary            |

The session above is the first rung, and it wants nothing on the box
either: graphviz hands back its own drawing operations and the binary
paints them, in type it carries. Every rung is the binary alone.

Two hooks. `SessionStart` puts the binary in place and prints one line
into the model's context: that a ```dot fence draws in place, and what
this terminal can draw. `MessageDisplay` draws. The line is what the
plugin is for — a model that has not heard of the hook writes mermaid, or
boxes out of hyphens, and neither is a picture.

## settings

Three, read from the environment, which reaches a hook when set for
`claude` in `settings.json`'s `env` or in the shell (measured on Claude
Code 2.1.261):

    { "env": { "DRAWER_RENDER": "braille",
               "DRAWER_THEME":  "/home/me/.config/drawer/theme.dot",
               "DRAWER_TEE":    "/home/me/drawer-deltas.jsonl" } }

`DRAWER_RENDER` picks a rung, `DRAWER_THEME` names a theme file, and
`DRAWER_TEE` records every payload the hook is handed, as a fixture
`-deltas` can replay.

`drawer -doctor` says what this terminal gets and why: the version, the
terminal, the window it found and where that number came from, the cell in
pixels, whether this is kitty, the rung in force and the one asked for, the
theme it paints in, the state directory, and the file `DRAWER_TEE` is
recording to, where it is set — read out of the same run a hook is built
from. It answers where the other doors would not:
a `-theme` file that will not read is a line in the report rather than an
exit.

**The theme.** The pixels rung draws in Claude Code's own theme, read the
way Claude Code reads it: the `theme` of `~/.claude/settings.json`, or of
the older `~/.claude.json` where that has none, and for `custom:NAME` the
`base` and `overrides` of `~/.claude/themes/NAME.json`. The four stock
palettes are carried in the binary, read from Claude Code 2.1.257, and
drift when Claude Code changes one; the two ansi themes name terminal
palette slots a picture cannot use and draw with their base's colours.

The hook cannot ask the terminal what ground it stands on — the reply
would land in Claude Code's input, not the hook's — so `auto` draws dark
whatever the terminal is, and a custom theme a plugin ships, outside
`~/.claude/themes`, reads as dark.

The theme is applied to the parsed graph, never to the source text, and
only where the model left an attribute unset. What the model painted stays
painted: a shape, a colour, a fill it asked for is kept, and around its
paint graphviz's own defaults apply, so `fillcolor=pink` gets black text
as `dot` would give it.

**A theme file.** A theme is DOT: the defaults a graph would declare for
itself, declared once for every graph the hook draws.

    ./bin/drawer -show-theme

prints the theme in force as DOT, which is where a theme file starts;
`themes/tokyonight.dot` and `themes/tokyonight-day.dot` are two. A
theme file is the whole theme, not a patch on Claude Code's: what it leaves
undeclared is graphviz's default. `graph [...]` is the root and every
cluster alike. Three attributes are rules rather than values. `fontname` may
name a font file — an absolute path, or a `.ttf`, `.otf` or `.ttc` found in
the system font directories — and the picture is then set in that face;
anything else, a family name included, and any file that will not read or
parse, leaves it in the Go Mono the binary carries. The layout is measured
in Courier whatever it says, so a monospace file fits and a proportional one
will not. `fontsize` yields to the cell when the theme has none. A node's
`fillcolor` is a rule: a node the model filled keeps its own text colour,
any other gets the theme's fill with `filled` added to its style. A theme
the hook cannot read is Claude Code's, so the picture draws; `-theme FILE
-dot g.dot` says what is wrong with the file, and `-theme FILE -dot g.dot
-png out.png` shows what it draws, without a session.

## how it stands there

Claude Code ships a `MessageDisplay` hook: it hands a command each piece
of an assistant message before laying it out and takes back replacement
text. The substitution is display-only — the transcript keeps the fence
the model wrote — and a hook is a fresh process per piece, so the fence is
reassembled through a small state file keyed by message id, because Claude
Code splits a reply where it likes and the split is not repeatable. The
processes are not one after another either: measured, two pieces'
processes started eleven microseconds apart. So each takes its turn — the
state counts the piece it expects next, a lock per message serialises the
readers, and a process ahead of the count waits for the one before it, the
draw included.

Layout is graphviz, compiled to WebAssembly and carried inside the binary
(`goccy/go-graphviz` through `wazero`). There is no `dot` to install. A
layout costs about a millisecond; the process spawn costs about twenty.

**What counts as a fence.** A fence as markdown has it: three or more
backticks or tildes at the start of a line, indented or not, closed by a
run of the same character at least as long. Every fence is tracked and
only a graph's is drawn. A fence labelled `dot` or `graphviz` is a graph's
whatever is in it, and one that will not draw is told why over its source.
An unlabelled fence is a graph's when its first line opens one, `digraph {`
or `graph {`, and prose otherwise. A fence labelled anything else streams
through as it arrives, and so does everything inside it, so a ```dot
quoted in a four-backtick fence is the text it is. A fence under a list
item draws in its indent, at the width the indent leaves.

**What the display wire keeps** (measured, Claude Code 2.1.257). Inside a
bare fence it keeps leading whitespace, 256-colour and basic SGR, bold and
dim, every unusual glyph tried (octants, sextants, braille, box diagonals),
and kitty's placeholder cells with their combining marks, counted as one
column each. It indents two columns and draws no caption. It strips APC
graphics escapes. Truecolor foregrounds are remapped to a 256-colour
index, which is why a picture's id rides in one.

**Pixels.** The picture goes round Claude Code rather than through it,
because its display wire strips a graphics escape out of hook text without
a word. The hook writes a PNG to a temp file and hands the terminal one
short escape naming it, down the parent's own tty; kitty reads the file,
deletes it, and shows the image in placeholder cells that ride through
Claude Code as ordinary text. Linux, a local kitty, and not through tmux.
The picture is drawn in Claude Code's own theme; which colours, below.

The pixels are the binary's own. graphviz hands back every polygon,
bezier, ellipse and text anchor it would have painted, and the rung paints
that list itself, so the picture is graphviz's picture and nothing has to
be installed for it.

Type is measured in Courier — the one monospace the wasm's built-in metrics
know exactly — and set in Go Mono, which the binary carries, at the size
that puts one glyph in one cell, so a label stands at the terminal's own
text size and a node reads as text that grew a border. An edge label sits on
its line and the line stops a glyph short of it on either side, as in the
glyph rungs: the graph is rewritten before layout so the label is a node on
the edge, which is what dot does inside itself for a labelled edge anyway,
down to halving `ranksep` for the doubled ranks and doubling every other
edge's `minlen`, so a plain edge still spans a full rank gap.

**Octants and braille.** Everything comes from graphviz's json output —
every polygon, ellipse, bezier and text anchor it would have painted — so
clusters, node shapes, multi-line labels, dashed edges and both heads of a
`dir=both` edge draw as written. Labels are never rasterised: eight dots
per cell is enough for a curve and hopeless for a letter, measured on the
screen, so labels are set as glyphs and the strokes are cleared beneath
them. The node outline is snapped onto the cells its label landed in.
Record and HTML labels print their markup, and node colours are not
painted.

**Cells.** Box-drawing characters, routed orthogonally with a cost search.
Only boxes, no clusters, and one line per label, a break in it
drawn as a space; the floor. An edge label rides
its own stroke, and one with nowhere to go is dropped rather than
misplaced.

**Size.** A graph wider than the window is laid out top-down as well,
because rows scroll where columns run out. In the glyph rungs the first
orientation that fits is drawn; past 120 rows the source shows under a
notice that says why. In pixels the orientation that keeps more of its
type is the picture: as written when that fits at the cell's own type,
top-down where that fits and as written would have to shrink, and where
both shrink, the one that shrinks less — type and all, so past about half
size it is a picture of a picture. A picture taller than 120 rows either
way falls to the glyph rungs, and so to the notice. Over about a dozen
nodes a graph flips top-down and gets tall, which the context line tells
the model.

**The theme, applied.** Claude Code's theme is a palette of named
colours, and six of them are the picture: a node is drawn as the user's
own message is, `userMessageBackground` for the fill and `text` for the
label, with `claude` for every stroke; an edge label is in `success`, a
cluster's outline in `inactive` and its caption in `subtle`.

A theme switch reaches the pictures already drawn. The hook keeps a ledger
per session of every picture's source and cut, and at the first delta of
the next reply, where the theme in force is not the one they were painted
in, it lays each out again and sends it under its old id: kitty repaints
the cells wherever they are, scrollback included, and nothing is printed.
The next reply rather than the keypress because Claude Code fires no hook
event in the session whose own settings write it was.

**The trade.** A hook is never re-run. Claude Code keeps what it was given
and re-wraps it on a resize without asking again, so a drawing is correct
at the width it was drawn for, shreds narrower, and comes back when the
window does. `--resume` shows every fence as source; the hook does not
fire for a replayed transcript. A terminal wrapper that sees the window
could re-derive the drawing every frame and make both of those hold, at
the price of the wrapper. The drawer takes the trade so that graphs work
with nothing but `claude`.

## known wrong

- A fence under a list item is drawn at the window's width less its
  indent. What Claude Code actually gives a code block inside a list item
  is not measured; if it is less, the drawing wraps there.
- The hook's width comes from the parent's tty: `/proc/$PPID/fd/0` on
  Linux, and on macOS the device `ps` names for the parent. CI runs the
  whole suite on macOS, but a runner has no terminal, so the macOS path
  is still unverified where it matters; if it is wrong the width falls to
  `COLUMNS` and then 100, and pixels do not reach the terminal.
- The pixels rung sets its type in Go Mono, and a script Go Mono has no
  glyph for is set in U+FFFD, the replacement character: `日本` and `☃` come
  out as one mark each, in a box graphviz sized for them. The glyph rungs
  still draw them, being the terminal's own text.
- Pictures already drawn repaint at the next reply, not at the switch. A
  theme file that changes the type changes the layout, and a picture that
  no longer fits its old cut is left as it was.
- Installed mid-session and reloaded with `/reload-plugins`, the hooks are
  registered but SessionStart does not fire, so the model is not handed the
  line until a new session. Measured on the first install of the release.
  The hook puts the binary in place itself when it finds none, so a ```dot
  fence the model does write draws.

## developing

A plugin runs no install step, so `scripts/drawer` puts the binary in the
plugin's data directory itself, at the first session, by the first of four
ways that works: a built checkout's `bin/drawer`, linked, so a rebuild is
live at the next reply; the binaries the release zip carries, one per
platform; the release binary downloaded for this platform and checked
against the sum `scripts/release.sh` pinned in the script; or `go build`,
where Go is on the PATH. A hook that finds no binary takes the first two
itself, the link and the copy, since neither reaches out. A stranger's
install is the zip. A checkout that arrived by git — an organisation pushing
the plugin to its people can only point at git — downloads the same binary
the zip would have carried. Until one of the four lands, both hooks fail
open: a session without the line is a fence that shows its source. The
script execs the binary rather than running it, because the binary reads the
terminal's size as its parent's, and its parent has to be `claude`. The hook
wire is Unix through and through and Windows does not build.

Install from the checkout: `/plugin marketplace add /path/to/drawer` then
`/plugin install drawer@drawer`. Claude Code copies the tree into its cache
but runs the hooks with the checkout as the plugin root (measured on
2.1.261: the data directory's link points into the checkout), so the
checkout's `bin/drawer` is what the next reply runs and `scripts/check.sh`
rebuilds it. A change to the script or the manifests is safest
reinstalled. A checkout loaded with `--plugin-dir` beside an installed
plugin is two hooks on every delta sharing the state files, and a fence
split across deltas comes out doubled.

`scripts/check.sh` is every oracle in one command: the build, vet, a vet
cross-compiled for macOS so the build-tagged files nobody here runs still
compile on every push, the internal import graph held to a table written
into the script so a sideways edge fails as loudly as a cycle would, the
laws under `-race`, every rung on a fixture, the theme files, the plugin's
manifests and wrapper, and the recorded delta streams replayed. GitHub runs
it on Linux and on macOS at every push, which is the only Mac this project
has. Offline:

    ./bin/drawer -dot FILE -size WxH -render braille   # draw a file, name the size it needs
    ./bin/drawer -dot FILE -png OUT [-cell 10x24]      # the pixels rung's picture, to a file
    ./bin/drawer -deltas FILE -render cells            # replay a recorded turn; nonzero if damaged
    ./bin/drawer -context                              # the line the model is handed
    ./bin/drawer -version                              # the release this binary was built from

`-deltas` is not a golden file: prose outside a fence must come back byte
for byte, every fence must come back untouched or as one drawn block that
fits its width, and a notice must carry the source it is about. A live
session writes its own fixture with `DRAWER_TEE`.

`scripts/release.sh VERSION` is the release as one command: four
binaries, a checksums file, the plugin zipped with all four inside, the
sums pinned into the script, the marketplace pointed at the zip, commit,
tag, push, GitHub release.

**The tree.** One binary; ten packages under `internal/`, and every
import points down this list.

    cmd/drawer/          the binary: os.Exit(drawer.Main(os.Args[1:]))
    internal/drawer      the flags, the hook wire, the ladder of rungs, the ledger
    internal/pixel       the pixels rung: the themed drawing, the painter, the cut, the placeholders
    internal/theme       Claude Code's theme, and a theme file
    internal/subcell     the braille and octant rung, and the octant glyphs
    internal/cells       the cells rung: a canvas of box-drawing characters, and edges routed on it
    internal/notice      why there is no drawing, drawn
    internal/fence       the transducer: a fence in, a drawing or the same bytes out
    internal/layout      graphviz: the one door, its drawing as data, and the scale from inches to cells
    internal/grid        a row of terminal cells, and what text costs in one
    internal/term        the parent's terminal: how big it is, and where its output goes
    scripts/drawer       the plugin's two hooks, one script
    scripts/check.sh     every oracle, one command
    scripts/release.sh   the release, one command
    hooks/ .claude-plugin/   the plugin's manifests
    themes/              two theme files to start from
    demo/                the session the README shows, as gif and mp4, and how it was made
    testdata/            a graph, and the recorded delta streams -deltas replays

## license

MIT. The embedded `graphviz.wasm` carries graphviz's Eclipse Public
License, and the embedded Go Mono fonts carry the Go project's own licence;
a built binary redistributes both. NOTICE says so.
