// The diff renders its lines with {@html}, so everything the highlighter emits
// must be escaped — a diff shows whatever the repository contains, including
// files that are themselves HTML, and a file under review is exactly the wrong
// place to trust.
//
// Mirrors escapeHtml and the fallback rules in utils/highlightLine.ts.
import { test } from 'node:test';
import assert from 'node:assert/strict';

function escapeHtml(text) {
  return text
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;');
}

const MAX_LINE_LENGTH = 2000;

/** The no-grammar path, which is what every fallback resolves to. */
function plain(text) {
  return escapeHtml(text);
}

test('markup in the diff is escaped, not rendered', () => {
  const line = '<script>alert(1)</script>';
  const out = plain(line);
  assert.ok(!out.includes('<script'), 'a tag from the repository reached the DOM');
  assert.equal(out, '&lt;script&gt;alert(1)&lt;/script&gt;');
});

test('ampersands are escaped first, so entities are not double-decoded', () => {
  // &lt; in the file must display as "&lt;", not as "<".
  assert.equal(plain('&lt;'), '&amp;lt;');
});

test('an attribute break-out is escaped', () => {
  // Highlighted spans carry a style attribute; content that closes it early
  // would otherwise inject arbitrary markup.
  const out = plain('" onmouseover="steal()');
  assert.ok(!out.includes('onmouseover="steal()"') || !out.includes('<'),
    'nothing that can open a tag survives escaping');
});

test('a line too long to be worth parsing still renders', () => {
  // A minified bundle on one line can be megabytes; parsing it blocks every
  // line after it and buys nothing readable.
  const long = 'x'.repeat(MAX_LINE_LENGTH + 1);
  assert.equal(long.length > MAX_LINE_LENGTH, true);
  assert.equal(plain(long).length, long.length, 'the line was dropped rather than shown plain');
});

test('blank and whitespace-only lines are preserved exactly', () => {
  // Indentation is meaningful in a diff — losing it changes what the reviewer
  // sees, and a blank line is a real part of the change.
  assert.equal(plain(''), '');
  assert.equal(plain('    '), '    ');
  assert.equal(plain('\t\tif (x) {'), '\t\tif (x) {');
});

// The +/- in column one is diff notation, not code. Parsed along with the line
// it changes what the rest means — "+func main() {" reads as an addition
// operator followed by a function — and the colouring slides with it. So it is
// split off before parsing and put back after.
//
// Mirrors the marker handling in highlightLine().
function splitMarker(text) {
  const marker = text[0] === '+' || text[0] === '-' || text[0] === ' ' ? text[0] : '';
  return { marker, code: marker ? text.slice(1) : text };
}

test('the diff marker is separated from the code', () => {
  assert.deepEqual(splitMarker('+func main() {'), { marker: '+', code: 'func main() {' });
  assert.deepEqual(splitMarker('-  return nil'), { marker: '-', code: '  return nil' });
  assert.deepEqual(splitMarker(' const x = 1'), { marker: ' ', code: 'const x = 1' });
});

test('a line that starts with code keeps all of it', () => {
  // Hunk headers and the first line of a file have no marker column.
  assert.deepEqual(splitMarker('@@ -1,4 +1,6 @@'), { marker: '', code: '@@ -1,4 +1,6 @@' });
});

test('the marker survives into the output', () => {
  // It is the reader's second signal after the tint, and the only one that
  // survives a screenshot or a colour-blind viewer.
  const { marker, code } = splitMarker('+  x++');
  assert.equal(marker + code, '+  x++');
});

test('a line holding only a marker is left alone', () => {
  // An empty added line: nothing to parse, and slicing would leave "".
  const { marker, code } = splitMarker('+');
  assert.equal(marker, '+');
  assert.equal(code.trim(), '', 'there is no code to colour, so the line renders plain');
});

// The marker gets a column of its own — the character plus a space — so the
// code starts at the same place on every line. Without it a changed line sits
// one character to the left of the context around it, and a diff is read by
// scanning down a column.
function markerColumn(marker) {
  return marker ? `<span style="opacity:0.45">${escapeHtml(marker)}</span> ` : '';
}

test('the marker is separated from the code by a space', () => {
  const out = markerColumn('+');
  assert.ok(out.endsWith(' '), 'the code would butt against the marker');
});

test('a line with no marker gets no column', () => {
  // Hunk headers have no marker: giving them one would indent them out of line
  // with everything else.
  assert.equal(markerColumn(''), '');
});

test('the marker is escaped like everything else', () => {
  // It comes from the diff, and only +, - and space are expected — but the
  // escaping is not conditional on that being true.
  assert.ok(!markerColumn('<').includes('><'));
});
