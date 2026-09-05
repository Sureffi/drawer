# drawer

A ```dot fence in Claude Code becomes a drawing, in place, as the reply
streams. One binary, one line of settings, nothing else running.

    go build -o bin/drawer ./cmd/drawer
    ./bin/drawer -install        # writes the hook into ~/.claude/settings.json, backup beside it
    ./bin/drawer -uninstall      # takes it out

Then ask for a graph. The model writes DOT; the reader sees the picture.

    ● Here's the request flow.
                                         ╭────────────╮       ╭─────╮
                                       ┌▶│API Server 1├┬────┬▶│Cache│
      ╭──────╮           ╭─────────────╮   │ ╰────────────╯│ ┌──┘ ╰─────╯
      │Client├───HTTPS──▶│Load Balancer├┬──┘               │ │
      ╰──────╯           ╰─────────────╯│    ╭────────────╮└─┼┐ ╭────────╮
                                        └───▶│API Server 2├┬─┼┴▶│Database│
                                             ╰────────────╯└─┘  ╰────────╯
      The cache and database sit downstream of both servers.

That is a live session, sonnet writing, CC 2.1.257, captured off tmux.

## how it stands there

Claude Code ships a `MessageDisplay` hook: it hands a command each piece of
an assistant message before laying it out and takes back replacement text.
The substitution is display-only — the transcript keeps the fence the model
wrote — and a hook is a fresh process per piece, so the fence is reassembled
through a small state file keyed by message id, because CC splits a reply
where it likes and the split is not repeatable. The processes are not one
after another either: measured, two pieces' processes started eleven
microseconds apart. So each takes its turn — the state counts the piece it
expects next, a lock per message serialises the readers, and a process
ahead of the count waits for the one before it, the draw included.

Layout is graphviz, compiled to WebAssembly and carried inside the binary
(`goccy/go-graphviz` through `wazero`). There is no `dot` to install. A
layout costs about a millisecond; the process spawn costs about twenty.

## what counts as a fence

A fence as markdown has it: three or more backticks or tildes at the start
of a line, indented or not, closed by a run of the same character at least
as long. Every fence is tracked and only a graph's is drawn. A fence
labelled `dot` or `graphviz` is a graph's whatever is in it, and one that
will not draw is told why over its source. An unlabelled fence is a
graph's when its first line opens one, `digraph {` or `graph {`, and prose
otherwise. A fence labelled anything else streams through as it arrives,
and so does everything inside it, so a ```dot quoted in a four-backtick
fence is the text it is. A fence under a list item draws in its indent, at
the width the indent leaves.

## the rungs

Best first, each failing open to the one below, chosen by `-render` or by
`auto`, which reads the terminal:

| rung      | draws                                             | needs                              |
|-----------|---------------------------------------------------|------------------------------------|
| `pixels`  | graphviz's own picture, in Claude Code's theme, in the transcript | kitty, `rsvg-convert` or `magick`  |
| `octants` | strokes at 2×4 per cell, solid; labels as glyphs  | a terminal that draws Unicode 16 octants itself: kitty, ghostty |
| `braille` | the same, dotted                                  | any font — every one has braille   |
| `cells`   | box-drawing characters, routed edges              | nothing but this binary            |

Pixels: the picture goes round CC rather than through it, because CC's
display wire strips a graphics escape out of hook text without a word. The
hook writes a PNG to a temp file and hands the terminal one short escape
naming it, down the parent's own tty via `/proc`; kitty reads the file,
deletes it, and shows the image in placeholder cells that ride through CC
as ordinary text. Linux, a local kitty, and not through tmux. A graph wider
than the window is laid out top-down as well, as the glyph rungs do, and
the orientation that keeps more of its type is the picture: as written
when that fits at the cell's own type, top-down where that fits and as
written would have to shrink, and where both shrink, the one that shrinks
less.

The picture is themed on the parsed graph, never in the source text. Type
is measured in Courier — the one monospace the wasm's built-in metrics know
exactly — and set in the terminal's face, at the size that puts one glyph
in one cell, so a label is the terminal's own text and a node reads as text
that grew a border. What the model painted stays painted: a shape, a
colour, a fill it asked for is kept, and around its paint graphviz's own
defaults apply, so `fillcolor=pink` gets black text as `dot` would give it.
Where it left an attribute unset, the theme applies. An edge label sits on
its line and the line stops a glyph short of it on either side, as in the
glyph rungs: the graph is rewritten before layout so the label is a node on
the edge, which is what dot does inside itself for a labelled edge anyway,
down to halving `ranksep` for the doubled ranks and doubling every other
edge's `minlen`, so a plain edge still spans a full rank gap.

A theme switch reaches the pictures already drawn. The hook keeps a ledger
per session of every picture's source and cut, and at the first delta of
the next reply, where the theme in force is not the one they were painted
in, it lays each out again and sends it under its old id: kitty repaints
the cells wherever they are, scrollback included, and nothing is printed.
The next reply rather than the keypress because Claude Code fires no hook
event in the session whose own settings write it was — every other session
on the machine gets one, and those keep their old colours anyway.

## the theme

A theme is DOT: the defaults a graph would declare for itself, declared
once for every graph the hook draws. The theme in force is Claude Code's
own. Its theme is a palette of named colours, and six of them are the
picture: a node is drawn as the user's own message is, `userMessageBackground`
for the fill and `text` for the label, with `claude` for every stroke; an
edge label is in `success`, a cluster's outline in `inactive` and its
caption in `subtle`. The four stock palettes are carried in the binary, read
from Claude Code 2.1.257; the two ansi themes name terminal palette slots
a picture cannot use and draw with their base's colours. Which theme is
read the way Claude Code reads it: the `theme` of `~/.claude/settings.json`,
or of the older `~/.claude.json` where that has none, and for `custom:NAME`
the `base` and `overrides` of `~/.claude/themes/NAME.json`. Anything the
hook cannot read is dark, `auto` included — auto is the ground Claude Code
found by asking the terminal, which a hook cannot ask.

    ./bin/drawer -show-theme

prints the theme in force as DOT, which is where a theme file starts, and
`fixtures/tokyonight.dot`, `fixtures/tokyonight-day.dot` and
`fixtures/theme.dot` are three. A theme file goes on the hook line:

    ./bin/drawer -install -theme ~/.config/drawer/theme.dot

`graph [...]` is the root and every cluster alike. A theme file is the whole
theme, not a patch on Claude Code's: what it leaves undeclared is
graphviz's default. Three attributes are rules rather than values.
`fontname` names the face the picture is set in; the layout is still
measured in Courier, so any monospace fits and a proportional face will not.
`fontsize` yields to the cell when the theme has none. A node's `fillcolor`
is a rule: a node the model filled keeps its own text colour, any other
gets the theme's fill with `filled` added to its style. A theme the hook
cannot read is Claude Code's, so the picture draws; `-theme FILE -dot`
says what is wrong with the file, and `-theme FILE -dot g.dot -png out.png`
shows what it draws, without a session.

Octants and braille: everything comes from graphviz's json output — every
polygon, ellipse, bezier and text anchor it would have painted — so
clusters, node shapes, multi-line labels, dashed edges and both heads of a
`dir=both` edge draw as written. Labels are never rasterised: eight dots
per cell is enough for a curve and hopeless for a letter, measured on the
screen, so labels are set as glyphs and the strokes are cleared beneath them.
The node outline is snapped onto the cells its label landed in.

Cells: box-drawing characters, routed orthogonally with a cost search.
Only boxes, no clusters, one-line labels; the floor.

## what the display wire keeps (measured, CC 2.1.257)

Inside a bare fence CC keeps: leading whitespace, 256-colour and basic SGR,
bold and dim, every unusual glyph tried (octants, sextants, braille, box
diagonals), and kitty's placeholder cells with their combining marks,
counted as one column each. It indents two columns and draws no caption. It
strips APC graphics escapes. Truecolor foregrounds are remapped to a
256-colour index, which is why a picture's id rides in one.

## the trade

A hook is never re-run. CC keeps what it was given and re-wraps it on a
resize without asking again, so a drawing is correct at the width it was
drawn for, shreds narrower, and comes back when the window does. `--resume`
shows every fence as source; the hook does not fire for a replayed
transcript. A terminal wrapper that sees the window could re-derive the
drawing every frame and make both of those hold, at the price of the
wrapper. The drawer takes the trade so that graphs work with nothing but
`claude`.

## offline

    ./bin/drawer -dot FILE -size WxH -render braille   # draw a file, name the size it needs
    ./bin/drawer -dot FILE -png OUT [-cell 10x24]      # the pixels rung's picture, to a file
    ./bin/drawer -deltas FILE -render cells            # replay a recorded turn; nonzero if damaged
    ./bin/drawer -hook -hooktee FILE                   # a live session writes its own fixture

`-deltas` is not a golden file: prose outside a fence must come back byte
for byte, every fence must come back untouched or as one drawn block that
fits its width, and a notice must carry the source it is about.

## known wrong

- A fence under a list item is drawn at the window's width less its
  indent. What Claude Code actually gives a code block inside a list item
  is not measured; if it is less, the drawing wraps there.
- The hook's width comes from `/proc/$PPID/fd/0`, Linux only. macOS would
  need the tty via `ps` and an open of the device; unverified.
- In the glyph rungs, record and HTML labels print their markup and node
  colours are not painted. The pixels rung draws both, but in an HTML label
  the space between two spans collapses — `<b>bold</b> and` sets as
  "boldand". That is graphviz's SVG writer.
- The theme comes from Claude Code's setting, not the terminal: the hook
  cannot ask the terminal — the reply would land in Claude Code's input,
  not the hook's. So `auto` draws dark whatever the terminal is, and a
  custom theme a plugin ships, outside `~/.claude/themes`, reads as dark.
  The stock palettes are a copy, and drift when Claude Code changes one.
- Pictures already drawn repaint at the next reply, not at the switch. A
  theme file that changes the type changes the layout, and a picture that
  no longer fits its old cut is left as it was.
- Over about twelve nodes the picture flips top-down and gets tall; past 120
  rows the source shows under a notice, except in pixels, where a picture too
  wide either way is shrunk into the window instead, type and all, and past
  about half size it is a picture of a picture.
- Edge labels that graphviz places on the stroke interrupt it; a label with
  nowhere to go in the cells rung is dropped rather than misplaced.

## embedding

The package is importable: `Stream` is the fence transducer with the
emit function as a parameter, `Mode` pairs an emit with the check `-deltas`
runs on its output, and `Cells`, `CutReason`, `DrawNotice` and `Rasterise`
are the renderer's doors. drawer began as one half of a pty compositor for
Claude Code, and that compositor still runs this hook wire in a mode of its
own: the hook books rows and the compositor paints into them every frame,
re-derived, which is the resize row of the table above.

## license

MIT. The embedded `graphviz.wasm` carries graphviz's Eclipse Public
License, and a built binary redistributes it.
