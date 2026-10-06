// render.test.tsx — laws for the hooks module, run by `claude plugin test .`
//
// The test's own hooks sit beneath the plugin and stand in for what the
// module reaches: the binary (process.run), the settings, and Claude
// Code's own drawing of the prose (ui.render beneath, a Text carrying the
// text it was handed). The fence rules are internal/fence's; these hold the
// port to them, and the composition to what a reader sees.

import type { On } from 'claude-code'
import { describe, expect, test, type Engine } from 'claude-code/testing'

// PIXEL is a whole PNG, one transparent pixel: the engine reads its header.
const PIXEL = 'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAAC0lEQVR4nGNgAAIAAAUAAXpeqz8AAAAASUVORK5CYII='

// Drawing is what `drawer element` prints, as the module reads it.
type Span = { t: string; c?: string; d?: boolean }
type Drawing =
  | { kind: 'image'; png: string; columns: number; rows: number; alt?: string }
  | { kind: 'text'; lines: (Span[] | null)[]; notice?: boolean }
  | { kind: 'none' }

const GLYPHS: Drawing = { kind: 'text', lines: [[{ t: '╭───╮', d: true }], [{ t: '│ ', d: true }, { t: 'a', c: '#d77757' }, { t: ' │', d: true }], [{ t: '╰───╯', d: true }]] }

// A run is what the binary does when asked: prints a drawing, exits 1
// having printed nothing (the wrapper finding no binary), or runs past
// the module's timeout, which the engine reports by rejecting. A hook
// beneath that throws is skipped, not rejected through; a deny carrying
// the engine's words is the rejection.
type Run = Drawing | 'exit 1' | 'timeout'

// stand registers the engine beneath the module: prose drawn as one Text
// with its text, a bullet marked when it opens a reply, and a binary that
// answers `element` with the given run. It returns what the binary was
// asked to draw: the source, the columns and the indent.
function stand(on: On, run: Run = GLYPHS) {
  const asked: { src?: string; cols?: string; indent?: string }[] = []
  on('ui.render', { component: 'AssistantMessage' }, async ($, e) => {
    const { Text } = $.ui.resolve(e)
    return <Text>{shown(e.props.text, e.props.isFirstOfReply)}</Text>
  })
  on('settings.read', async () => ({ value: {} }))
  on('process.run', async ($, e) => {
    const out = { stderr: '', isStdoutTruncated: false, isStderrTruncated: false }
    if (e.argv[1] !== 'element') return { value: { ...out, exitCode: 0, stdout: 'Linux\n' } }
    asked.push({ src: e.init?.stdin, cols: e.argv[2], indent: e.argv[3] })
    if (run === 'timeout') return { deny: `$.process.run(${e.argv[0]}) aborted: still running after ${e.init?.timeoutMs}ms` }
    if (run === 'exit 1') return { value: { ...out, exitCode: 1, stdout: '' } }
    return { value: { ...out, exitCode: 0, stdout: JSON.stringify(run) } }
  })
  return asked
}

// shown is the stand-in's drawing of prose: the text, the bullet before it
// when it opens a reply, and an escape as ␛, which a Text may not hold.
function shown(text: string, first: boolean) {
  return (first ? '● ' : '') + text.replaceAll('\x1b', '␛')
}

function mount($: Engine, text: string, isFirstOfReply = true) {
  return $.ui.mount({
    plugin: 'drawer',
    surface: 'terminal',
    component: 'AssistantMessage',
    props: { text, isFirstOfReply },
    viewport: { columns: 100, rows: 40 },
  })
}

describe('a fence no display hook reached', () => {
  test('is drawn in place, the prose around it drawn by Claude Code', async ($, on) => {
    const asked = stand(on)
    const ui = await mount($, 'above\n\n```dot\ndigraph { a -> b }\n```\n\nbelow')
    expect(asked).toEqual([{ src: 'digraph { a -> b }', cols: '100', indent: '0' }])
    expect(await ui.find({ type: 'Text', text: /╭───╮/ })).toBeDefined()
    expect(await ui.find({ type: 'Text', text: '● above' })).toBeDefined()
    expect(await ui.find({ type: 'Text', text: 'below' })).toBeDefined()
    expect(await ui.find({ type: 'Text', text: /digraph/ })).toBeUndefined()
  })

  test('is a picture where the binary cuts one', async ($, on) => {
    stand(on, { kind: 'image', png: PIXEL, columns: 12, rows: 3, alt: 'digraph { a -> b }' })
    const ui = await mount($, 'look:\n```graphviz\ndigraph { a -> b }\n```')
    const img = await ui.find({ type: 'Image' })
    expect(img?.props.columns).toBe(12)
    expect(img?.props.rows).toBe(3)
  })

  test('opening a reply, carries the reply bullet itself', async ($, on) => {
    stand(on)
    const ui = await mount($, '```dot\ndigraph { a -> b }\n```\nafter')
    expect(await ui.find({ type: 'Text', text: '●' })).toBeDefined()
    expect(await ui.find({ type: 'Text', text: 'after' })).toBeDefined()
  })
})

describe('what is not a graph is not touched', () => {
  test("the display hook's own drawing is handed on as it came", async ($, on) => {
    const asked = stand(on)
    const drawn = 'above\n```text\n\x1b[2m╭───╮\x1b[22m\n```\nbelow'
    const ui = await mount($, drawn)
    expect(asked).toEqual([])
    expect(await ui.find({ type: 'Text', text: shown(drawn, true) })).toBeDefined()
  })

  test('a ```dot quoted inside a longer fence is text', async ($, on) => {
    const asked = stand(on)
    await mount($, '````markdown\n```dot\ndigraph { a -> b }\n```\n````')
    expect(asked).toEqual([])
  })

  test('an unlabelled fence draws only when its first line opens a graph', async ($, on) => {
    const asked = stand(on)
    await mount($, '```\nls -la\n```\n\n~~~\ndigraph { x -> y }\n~~~')
    expect(asked).toEqual([{ src: 'digraph { x -> y }', cols: '100', indent: '0' }])
  })

  test('a fence the reply never closed stays source when it will not draw', async ($, on) => {
    stand(on, { kind: 'text', notice: true, lines: [[{ t: 'no diagram' }]] })
    const text = 'cut off:\n```dot\ndigraph { a ->'
    const ui = await mount($, text)
    expect(await ui.find({ type: 'Text', text: /no diagram/ })).toBeUndefined()
    expect(await ui.find({ type: 'Text', text: shown(text, true) })).toBeDefined()
  })

  test('a graph the binary will not draw goes back into the prose, byte for byte', async ($, on) => {
    stand(on, { kind: 'none' })
    const text = 'a\n```dot\ndigraph { a -> b }\n```\nb'
    const ui = await mount($, text)
    expect(await ui.find({ type: 'Text', text: shown(text, true) })).toBeDefined()
  })
})

test('a fence under a list item draws in its indent, from its dedented source', async ($, on) => {
  const asked = stand(on)
  const ui = await mount($, '- the flow:\n\n  ```dot\n  digraph {\n    a -> b\n  }\n  ```\n\n- done', false)
  expect(asked).toEqual([{ src: 'digraph {\n  a -> b\n}', cols: '100', indent: '2' }])
  const box = await ui.find({ type: 'Box', text: /╭───╮/ })
  expect(box).toBeDefined()
})

test('a blank row draws as a blank line, sent as [] or as null', async ($, on) => {
  stand(on, { kind: 'text', lines: [[{ t: 'top' }], [], null, [{ t: 'end' }]] })
  const ui = await mount($, '```dot\ndigraph { a -> b }\n```')
  expect(await ui.findAll({ type: 'Text', text: ' ' })).toHaveLength(2)
  expect(await ui.find({ type: 'Text', text: 'end' })).toBeDefined()
})

describe('the binary is asked', () => {
  test('with the fence indent, which keys the drawing', async ($, on) => {
    const asked = stand(on)
    await mount($, '```dot\ndigraph { a -> b }\n```')
    await mount($, '  ```dot\n  digraph { a -> b }\n  ```')
    await mount($, '    ```dot\n    digraph { a -> b }\n    ```')
    expect(asked.map((a) => a.indent)).toEqual(['0', '2', '4'])
  })

  test('with the indent measured as the display hook measures it: a space a column, a tab none', async ($, on) => {
    const asked = stand(on)
    const ui = await mount($, '\t  ```dot\n\t  digraph { a -> b }\n\t  ```')
    expect(asked).toEqual([{ src: 'digraph { a -> b }', cols: '100', indent: '2' }])
    const boxes = await ui.findAll({ type: 'Box', text: /╭───╮/ })
    expect(boxes.map((b) => b.props.paddingLeft).filter((p) => p !== undefined)).toEqual([2])
  })

  test('once about a fence it timed out on, which stays source', async ($, on) => {
    const asked = stand(on, 'timeout')
    const text = 'a\n```dot\ndigraph { a -> b }\n```\nb'
    const ui = await mount($, text)
    await mount($, text)
    expect(asked).toHaveLength(1)
    expect(await ui.find({ type: 'Text', text: shown(text, true) })).toBeDefined()
  })

  test('again on the next render when it exits non-zero', async ($, on) => {
    const asked = stand(on, 'exit 1')
    const text = 'a\n```dot\ndigraph { a -> b }\n```\nb'
    const ui = await mount($, text)
    await mount($, text)
    expect(asked).toHaveLength(2)
    expect(await ui.find({ type: 'Text', text: shown(text, true) })).toBeDefined()
  })
})
