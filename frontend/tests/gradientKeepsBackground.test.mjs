import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';

const read = (path) => readFileSync(new URL(path, import.meta.url), 'utf8');

// A gradient name lost its background colour.
//
// The gradient is painted with background-clip: text, which clips an element's
// background to its letters. Putting the background chip on the same element,
// or leaving it off entirely as the gradient branches did, left no chip behind.
// Measured in a browser: with the chip on an outer span and the gradient on an
// inner one, both show; with one span, only the gradient does.

const places = [
  ['session list', '../src/lib/components/Sidebar/SessionItem.svelte', 'nameStyle'],
  ['group header', '../src/lib/components/Sidebar/GroupItem.svelte', 'nameStyle'],
  ['quick jump', '../src/lib/components/Dialogs/QuickJumpDialog.svelte', 'row.style'],
  ['colour dialog preview', '../src/lib/components/Dialogs/SessionColorDialog.svelte', 'getPreviewStyle(selectedColor, selectedBgColor)'],
];

for (const [where, path, chipStyle] of places) {
  test(`${where}: a gradient name keeps its background chip`, () => {
    const src = read(path);
    const escaped = chipStyle.replace(/[.(),]/g, (c) => `\\${c}`);
    // The chip's span, with the gradient span directly inside it.
    const nested = new RegExp(`style=\\{${escaped}\\}><span [^>]*style=`);
    assert.match(src, nested,
      `the gradient name in the ${where} is not wrapped in the chip, so the ` +
      'background colour chosen with it is not drawn');
  });
}

// The quick jump list decided the chip itself, and returned nothing at all for
// a gradient name.
test('quick jump asks for the chip even when the name is a gradient', () => {
  const src = read('../src/lib/components/Dialogs/QuickJumpDialog.svelte');
  const at = src.indexOf('function sessionStyle(');
  const body = src.slice(at, src.indexOf('\n  }\n', at));
  assert.doesNotMatch(body, /isGradient\([^)]*\)\) return ''/,
    'a gradient name gets no style, so its background colour is dropped');
});

// Six places spelled the gradient-on-letters style out on their own, not all
// alike, and a fix in one did not reach the rest. They all use the helper now.
test('every gradient name is painted by the one shared style', async () => {
  const { gradientTextStyle } = await import('../src/lib/utils/rowColors.ts');
  const style = gradientTextStyle('gradient-sunset', 'font-weight: 600;');
  assert.match(style, /^background-image: linear-gradient/,
    'the shorthand resets background-clip and paints a solid bar');
  assert.match(style, /-webkit-text-fill-color: transparent/);
  assert.match(style, /font-weight: 600;$/);

  for (const path of [
    '../src/lib/components/Sidebar/SessionItem.svelte',
    '../src/lib/components/Sidebar/GroupItem.svelte',
    '../src/lib/components/Dialogs/QuickJumpDialog.svelte',
    '../src/lib/components/Dashboard/AllTasks.svelte',
    '../src/lib/components/Dialogs/SessionColorDialog.svelte',
    '../src/lib/components/Dialogs/CustomGradientDialog.svelte',
  ]) {
    const src = read(path);
    assert.match(src, /gradientTextStyle\(/, `${path} does not use the shared style`);
    // Only comments may still mention the clip; code must not spell it out.
    const code = src.replace(/<!--[\s\S]*?-->/g, '').replace(/\/\*[\s\S]*?\*\//g, '')
      .split('\n').filter(line => !line.trim().startsWith('//')).join('\n');
    const inline = code.match(/style=["{`][^\n]*background-clip/);
    assert.equal(inline, null, `${path} still writes its own clip: ${inline?.[0]}`);
  }
});
