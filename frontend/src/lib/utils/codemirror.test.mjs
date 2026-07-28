/**
 * Byte-exactness tests for the CodeMirror 6 document configuration.
 *
 * WHY these exist, and why they are in the FRONTEND: session/file_edit_test.go
 * proves decodeForEdit/encodeFromEdit are inverses, but it can never see this
 * bug. The damage happens in the browser, downstream of Go, between the text
 * arriving and the text being sent back.
 *
 * CodeMirror's EditorState splits a document on CRLF, CR, LF AND the Unicode
 * line separators U+2028/U+2029, then rejoins every line with ONE normalised
 * break. A file that decodeForEdit deliberately passed through raw -- because
 * it has mixed endings and its CRs must survive to be restored on save -- is
 * therefore corrupted the instant it is opened, with no edit made and nothing
 * on screen to suggest it. EditorState.lineSeparator.of('\n') pins the split to
 * LF and disables the Unicode handling; these tests are what keeps it there.
 *
 * The "unconfigured" tests at the bottom are not padding: they demonstrate the
 * corruption is real, so a later reader cannot conclude the pinning is
 * redundant and drop it.
 *
 * Every exotic character below is written as an explicit \u escape rather than
 * as itself. A raw U+2028 in source is a line break to some parsers and is
 * silently rewritten by some editors -- a fixture that loses the character it
 * is testing would pass while proving nothing.
 *
 *   cd frontend && npm test
 */
import test from 'node:test';
import assert from 'node:assert/strict';
import { EditorState } from '@codemirror/state';

/** The one extension under test, mirroring codemirror.ts's lineSeparatorLF. */
const pinned = [EditorState.lineSeparator.of('\n')];

/** Round-trip a string through a configured EditorState. */
const roundTrip = (input) => EditorState.create({ doc: input, extensions: pinned }).doc.toString();

/** Round-trip through a DEFAULT EditorState, to show what pinning prevents. */
const roundTripUnconfigured = (input) => EditorState.create({ doc: input }).doc.toString();

// The cases required of the migration, each named for what makes it dangerous.
const CASES = {
  'mixed CRLF and LF': 'a\r\nb\nc\r\nd',
  'a lone CR': 'first\rsecond',
  'a trailing CR': 'line\r',
  'a leading CR': '\rline',
  'U+2028 line separator': 'a\u2028b',
  'U+2029 paragraph separator': 'a\u2029b',
  'a BOM character': '\ufeffpackage main',
  'an astral emoji': 'const x = "\u{1F600}\u{1F1ED}\u{1F1FA}";',
  'a NUL byte': 'a\u0000b',
  'CRLF only': 'a\r\nb\r\nc',
  'LF only': 'a\nb\nc',
  empty: '',
  'a CR immediately before a CRLF': 'a\r\r\nb',
  'combined worst case': '\ufeffa\r\nb\rc\u2028d\u{1F600}\u0000e\r',
};

for (const [name, input] of Object.entries(CASES)) {
  test(`preserves ${name} exactly`, () => {
    assert.equal(roundTrip(input), input);
  });
}

test('preserves a 5000-line document (exercises the rope, not just a flat string)', () => {
  // Long enough that CodeMirror stores the document as a tree of chunks rather
  // than one string, which is a different path through the line splitter.
  const input = Array.from({ length: 5000 }, (_, i) => `line ${i}   \u{1F600}`).join('\r\n');
  assert.equal(roundTrip(input), input);
});

test('preserves the exact code points, not merely the visible text', () => {
  const input = 'a\r\nb\rc\nd\u2028e';
  const out = roundTrip(input);
  assert.equal(out.length, input.length);
  assert.deepEqual(
    [...out].map((c) => c.codePointAt(0)),
    [...input].map((c) => c.codePointAt(0))
  );
});

test('an edit in the middle leaves the CRs on other lines untouched', () => {
  // The realistic failure: the user edits one line of a mixed-ending file and
  // the save silently rewrites every OTHER line's ending too.
  const input = 'alpha\r\nbravo\ncharlie\r\n';
  const state = EditorState.create({ doc: input, extensions: pinned });
  const at = input.indexOf('bravo');
  const next = state.update({ changes: { from: at, to: at + 5, insert: 'BRAVO' } }).state;
  assert.equal(next.doc.toString(), 'alpha\r\nBRAVO\ncharlie\r\n');
});

test('splits lines on LF alone, so a CR is content rather than a break', () => {
  // The observable consequence of pinning: "a\rb\nc" is TWO lines, not three.
  // The gutter and the caret footer both count lines this way, and they must
  // agree with what a save will actually write.
  const state = EditorState.create({ doc: 'a\rb\nc', extensions: pinned });
  assert.equal(state.doc.lines, 2);
  assert.equal(state.doc.line(1).text, 'a\rb');
});

test('the exact shape decodeForEdit hands a mixed-ending file through raw', () => {
  const fromGo = 'package main\r\n\nfunc main() {}\r\n';
  assert.equal(roundTrip(fromGo), fromGo);
});

// --- What the pinning prevents ---------------------------------------------

test('UNCONFIGURED, CodeMirror destroys CRs -- this is why lineSeparator is set', () => {
  // Should this ever start failing, CodeMirror changed its default. The pinning
  // would still be correct; removing it would need the tests above re-proven.
  assert.equal(roundTripUnconfigured('a\r\nb\nc'), 'a\nb\nc');
  assert.equal(roundTripUnconfigured('first\rsecond'), 'first\nsecond');
  assert.equal(roundTripUnconfigured('line\r'), 'line\n');
});

test('the Unicode separators survive even unconfigured, in THIS version', () => {
  // Measured, not assumed. CodeMirror's docs describe U+2028/U+2029 as line
  // breaks, and its splitting regex has carried them historically, but the
  // version installed here leaves them alone -- only the CR forms are rewritten.
  //
  // This is asserted rather than left untested so the distinction stays honest:
  // the CR cases above are what lineSeparator actually rescues, and if a future
  // upgrade starts splitting on these too, this test fails and says so while the
  // pinned round-trips keep passing.
  assert.equal(roundTripUnconfigured('a\u2028b'), 'a\u2028b');
  assert.equal(roundTripUnconfigured('a\u2029b'), 'a\u2029b');
});

test('a mixed-ending file survives configured but not unconfigured', () => {
  const fromGo = 'package main\r\n\nfunc main() {}\r\n';
  assert.equal(roundTrip(fromGo), fromGo);
  assert.notEqual(roundTripUnconfigured(fromGo), fromGo);
});
