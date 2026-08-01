// The default renderer differs per platform, and getting it backwards is not
// something you notice on the machine you develop on. macOS and Windows get DOM
// because canvas drops accented letters, arrows and box drawing there. Linux
// keeps canvas only because it measured fastest on WebKitGTK when the selector
// was added — see defaultTerminalRenderer() for why that is the weaker half.
//
// Mirrors rendererForUserAgent() in terminal.ts. That file imports xterm, which
// pulls in browser globals node:test does not have, so the rule is restated
// here rather than imported — if you change it there, change it here.
import { test } from 'node:test';
import assert from 'node:assert/strict';

const rendererForUserAgent = (ua) =>
  /Linux/i.test(ua) && !/Android/i.test(ua) ? 'canvas' : 'dom';

// Real user agents from the webview each platform's Wails build runs.
const LINUX = 'Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/8.0 Safari/605.1.15';
const MACOS = 'Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Safari/605.1.15';
const WINDOWS = 'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36 Edg/120.0.0.0';

test('Linux keeps canvas, for the WebKitGTK paint cost', () => {
  assert.equal(rendererForUserAgent(LINUX), 'canvas');
});

test('macOS uses dom, where canvas drops characters', () => {
  assert.equal(rendererForUserAgent(MACOS), 'dom');
});

test('Windows uses dom, same canvas problem as macOS', () => {
  assert.equal(rendererForUserAgent(WINDOWS), 'dom');
});

test('an unknown user agent falls back to dom, the compatible one', () => {
  // Correctness beats speed when we cannot tell what we are running on.
  assert.equal(rendererForUserAgent(''), 'dom');
});

test('Android is not desktop Linux', () => {
  // It contains "Linux", but it is not the webview the canvas default was
  // measured on, so it must not inherit that choice.
  const android = 'Mozilla/5.0 (Linux; Android 13) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Mobile Safari/537.36';
  assert.equal(rendererForUserAgent(android), 'dom');
});
