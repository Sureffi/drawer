// render.tsx — the hooks module: a ```dot fence drawn on every screen
// Claude Code draws a reply on.
//
// The MessageDisplay hook draws the live turn of the main thread and
// nothing else. A fork's view, a subagent's, a transcript replayed by
// --resume and a background job reattached are drawn from the transcript,
// and no display hook is asked about them — measured on 2.1.283, each of
// them showed the fence as source. ui.render is asked about every
// assistant text block on every one of those screens.
//
// Where the display hook has already drawn — the main thread's own turn —
// this hook is handed the drawing, not the fence (measured: the text it
// gets is the hook's output), finds no fence in it, and hands the block on
// untouched. So the two never draw the same fence, and the live turn keeps
// drawing as it streams.
//
// A block with a graph in it is cut at its fences. The prose between them
// is Claude Code's own drawing (next, with the text cut to that stretch);
// each fence is what `drawer -element` makes of it: an Image where Claude
// Code will draw pixels, Text rows of glyphs everywhere else. Anything
// that fails leaves the block to Claude Code as it arrived.
//
// Function hooks are early access. A build that does not load hooks
// modules runs the command hooks in hooks.json alone, and there they are
// the whole plugin.

import type { Elements, EngineInterface, Register, RenderChildren } from 'claude-code'

// The fence rules are internal/fence's, for a block that has arrived
// whole: a run of three or more backticks or tildes at the start of a
// line, closed by a run of the same character at least as long with
// nothing else on the line. A fence labelled dot or graphviz is a graph's;
// an unlabelled one is when its first line opens a graph; any other is
// somebody else's, and a ```dot quoted inside it is text.
//
// They are Go's character for character. A space is unicode.IsSpace's,
// which strings.TrimSpace and strings.Fields cut at, and not JavaScript's
// \s or trim(): U+0085 is one, U+FEFF is not. RE2's . is anything but a
// newline, and its \s and \b are ASCII; its (?i) folds by Unicode's simple
// folding, so ſ is an s, which /iu does too, in a class as well: the word
// boundary after graph is read apart, without the flag. strings.ToLower
// makes İ an i.
const space = '\\t\\n\\v\\f\\r \\u0085\\u00a0\\u1680\\u2000-\\u200a\\u2028\\u2029\\u202f\\u205f\\u3000'
const edgeSpace = new RegExp(`^[${space}]+|[${space}]+$`, 'g')
const spaces = new RegExp(`[${space}]+`)
const fenceRe = /^([ \t]*)(`{3,}|~{3,})([^\n]*)$/
const graphKeyword = /^[\t\n\f\r ]*(strict[\t\n\f\r ]+)?(di)?graph/iu
const graphRest = /^(?![0-9A-Za-z_])[^{]*\{/

const trimSpace = (s: string) => s.replace(edgeSpace, '')

// graphStart is a DOT graph's first line: the keyword, a name if any, and
// the brace.
function graphStart(line: string) {
  const m = graphKeyword.exec(line)
  return m !== null && graphRest.test(line.slice(m[0].length))
}

type Opener = { indent: string; run: string; info: string }

function openerOf(line: string): Opener | null {
  const m = fenceRe.exec(line.replace(/[ \t\r]+$/, ''))
  if (!m) return null
  const [, indent = '', run = '', rest = ''] = m
  if (run[0] === '`' && rest.includes('`')) return null
  const info = (trimSpace(rest).split(spaces)[0] ?? '').replace(/İ/g, 'i').toLowerCase()
  return { indent, run, info }
}

function closes(f: Opener, line: string) {
  const t = trimSpace(line)
  return t.length >= f.run.length && [...t].every((c) => c === f.run[0])
}

// columnsOf is an indent's width as the display hook measures it,
// grid.Cells: a space is a column and a tab is none.
function columnsOf(indent: string) {
  return [...indent].filter((c) => c === ' ').length
}

// A stretch of a block: prose, or a graph's fence with its source cut out.
type Part = { raw: string; src?: string; indent?: number; closed?: boolean }

// split cuts a block into stretches of prose and graphs, by line. Every
// stretch keeps its lines exactly as they arrived, so a graph that will
// not draw goes back into the prose around it byte for byte.
function split(text: string): Part[] {
  const lines = text.split('\n')
  const parts: Part[] = []
  let prose = 0
  for (let i = 0; i < lines.length; ) {
    const f = openerOf(lines[i] ?? '')
    if (!f) {
      i++
      continue
    }
    let j = i + 1
    while (j < lines.length && !closes(f, lines[j] ?? '')) j++
    const closed = j < lines.length
    const body = lines
      .slice(i + 1, j)
      .map((l) => (l.startsWith(f.indent) ? l.slice(f.indent.length) : l))
      .join('\n')
    const first = body.split('\n').find((l) => trimSpace(l) !== '') ?? ''
    const ours = f.info === 'dot' || f.info === 'graphviz' || (f.info === '' && graphStart(trimSpace(first)))
    const end = closed ? j + 1 : lines.length
    if (ours) {
      if (i > prose) parts.push({ raw: lines.slice(prose, i).join('\n') })
      parts.push({ raw: lines.slice(i, end).join('\n'), src: body, indent: columnsOf(f.indent), closed })
      prose = end
    }
    i = end
  }
  if (prose < lines.length) parts.push({ raw: lines.slice(prose).join('\n') })
  return parts
}

// A span is a run of one style in a row of glyphs.
type Span = { t: string; c?: string; d?: boolean }

// Drawing is what `drawer element` answers for one fence: a picture, rows
// of glyphs (a row with none is blank), or none, the fence left as it
// arrived. A notice says why there is no drawing. failed is a run that
// answered nothing, which is asked again.
type Drawing =
  | { kind: 'image'; png: string; columns: number; rows: number; alt?: string; notice?: boolean }
  | { kind: 'text'; lines: (Span[] | null)[]; notice?: boolean }
  | { kind: 'none'; failed?: boolean; notice?: undefined }

// The drawings this module has asked for, newest last: a block is drawn
// again on every scroll that moves it past an edge of the screen, and on
// every width, and the answer does not change between them. The theme is
// in the key, because a picture is painted in it, and so is the indent,
// because the drawing is as wide as the screen less it.
const drawn = new Map<string, Promise<Drawing>>()
const drawnMost = 48

function drawOnce($: EngineInterface, src: string, cols: number, indent: number, theme: string) {
  const key = `${theme}\u0000${cols}\u0000${indent}\u0000${src}`
  let p = drawn.get(key)
  if (p) {
    drawn.delete(key)
    drawn.set(key, p)
    return p
  }
  p = drawElement($, src, cols, indent).then((el) => {
    if (el.kind === 'none' && el.failed) drawn.delete(key)
    return el
  })
  drawn.set(key, p)
  const oldest = drawn.keys().next().value
  if (drawn.size > drawnMost && oldest !== undefined) drawn.delete(oldest)
  return p
}

// drawElement runs the binary on one fence. A run that fails is failed,
// and asked again on the next render: the wrapper exits non-zero where it
// finds no binary, which the next session start puts in place. A run past
// runMs is killed by the engine, which rejects with "still running after";
// that is a layout too slow to wait for, and it is a none for the session.
const runMs = 15000

async function drawElement($: EngineInterface, src: string, cols: number, indent: number): Promise<Drawing> {
  const argv = [`${$.plugin.root}/scripts/drawer`, 'element', String(cols), String(indent)]
  try {
    const { exitCode, stdout } = await $.process.run(argv, { stdin: src, timeoutMs: runMs })
    if (exitCode !== 0) return { kind: 'none', failed: true }
    return JSON.parse(stdout) as Drawing
  } catch (err) {
    const said = typeof err === 'object' && err !== null && 'message' in err ? String(err.message) : String(err)
    return /still running after/.test(said) ? { kind: 'none' } : { kind: 'none', failed: true }
  }
}

// The bullet that opens a reply is Claude Code's, drawn with the first
// stretch of prose. A reply that opens with a graph has no prose there to
// carry it, and gets Claude Code's glyph from here: ⏺ on macOS, ● elsewhere.
let bullet: string | undefined

async function bulletOf($: EngineInterface) {
  if (bullet === undefined) {
    try {
      bullet = (await $.process.run(['uname', '-s'])).stdout.trim() === 'Darwin' ? '⏺' : '●'
    } catch {
      bullet = '●'
    }
  }
  return bullet
}

// picture is one drawing, standing where the display hook's would: at the
// text's own column, in the fence's indent, a row clear of the prose above.
function picture(ui: Elements['terminal'], el: Exclude<Drawing, { kind: 'none' }>, indent: number, top: number) {
  const { Box, Text, Image } = ui
  const pad = { paddingLeft: indent, marginTop: top }
  if (el.kind === 'image') {
    return (
      <Box {...pad}>
        <Image source={{ png: el.png }} columns={el.columns} rows={el.rows} alt={el.alt ?? ''} />
      </Box>
    )
  }
  return (
    <Box flexDirection="column" {...pad}>
      {el.lines.map((line) => (
        <Text wrap="truncate-end">
          {!line || line.length === 0
            ? ' '
            : line.map((s) => {
                const style: { color?: string; dimColor?: boolean } = {}
                if (s.c) style.color = s.c
                if (s.d) style.dimColor = true
                return <Text {...style}>{s.t}</Text>
              })}
        </Text>
      ))}
    </Box>
  )
}

export const register: Register = (on) => {
  on('ui.render', { component: 'AssistantMessage' }, async ($, e, next) => {
    if (e.surface !== 'terminal') return next(e)
    const text = e.props.text
    if (!text.includes('```') && !text.includes('~~~')) return next(e)
    const parts = split(text)
    if (!parts.some((p) => p.src !== undefined)) return next(e)

    const cols = e.viewport?.columns ?? 0
    const theme = String((await $.settings.read()).theme ?? '')
    const els = await Promise.all(parts.map((p) => (p.src === undefined ? null : drawOnce($, p.src, cols, p.indent ?? 0, theme))))

    // A graph that will not draw is prose again, and so is a notice on a
    // fence the reply never closed: that is a reply cut off, not a graph
    // somebody got wrong.
    const blocks: ({ prose: string; el?: undefined } | { el: Exclude<Drawing, { kind: 'none' }>; indent: number })[] = []
    let prose: string[] = []
    parts.forEach((p, i) => {
      const el = els[i]
      if (!el || el.kind === 'none' || (el.notice && !p.closed)) {
        prose.push(p.raw)
        return
      }
      blocks.push({ prose: prose.join('\n') })
      blocks.push({ el, indent: p.indent ?? 0 })
      prose = []
    })
    blocks.push({ prose: prose.join('\n') })
    if (!blocks.some((b) => b.el)) return next(e)

    const ui = $.ui.resolve(e)
    const { Box, Text } = ui
    const body: RenderChildren[] = []
    let first = e.props.isFirstOfReply
    let opened = false // a bullet is on screen, so what follows stands in its column
    for (const b of blocks) {
      if (b.el) {
        body.push(picture(ui, b.el, b.indent, body.length === 0 ? 0 : 1))
        continue
      }
      const stretch = b.prose.replace(/^(?:[ \t]*\n)+/, '').replace(/(?:\n[ \t]*)+$/, '')
      if (stretch.trim() === '') continue
      body.push(await next({ ...e, props: { ...e.props, text: stretch, isFirstOfReply: first && body.length === 0 } }))
      if (first && body.length === 1) opened = true
    }

    if (!first) return <Box flexDirection="column">{body}</Box>
    if (opened) {
      return (
        <Box flexDirection="column">
          {body[0]}
          <Box flexDirection="column" paddingLeft={2}>
            {body.slice(1)}
          </Box>
        </Box>
      )
    }
    return (
      <Box flexDirection="row" alignItems="flex-start" marginTop={1}>
        <Box minWidth={2}>
          <Text color="text">{await bulletOf($)}</Text>
        </Box>
        <Box flexDirection="column">{body}</Box>
      </Box>
    )
  })
}
