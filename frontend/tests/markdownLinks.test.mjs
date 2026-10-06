import test from 'node:test';
import assert from 'node:assert/strict';

// Where a click in a rendered Markdown file goes: a browser, a heading, another
// file of the repository — or nowhere, for anything that would leave the
// browsed directory or that the webview would act on by itself.

const links = await import('../src/lib/utils/markdownLinks.ts');

test('Markdown files are recognised by extension, in any case', () => {
  for (const path of ['README.md', 'docs/Guide.MD', 'notes.markdown', 'a/b.mdown', 'x.mkd']) {
    assert.equal(links.isMarkdownPath(path), true, path);
  }
  for (const path of ['README', 'md', '.md', 'docs/md/file.txt', 'page.mdx.bak', 'a.mdx']) {
    assert.equal(links.isMarkdownPath(path), false, path);
  }
});

test('web and mail links leave the app; other schemes go nowhere', () => {
  assert.deepEqual(links.classifyLink('https://example.com/x', 'README.md'),
    { kind: 'external', url: 'https://example.com/x' });
  assert.deepEqual(links.classifyLink(' mailto:a@b.c ', 'README.md'),
    { kind: 'external', url: 'mailto:a@b.c' });
  for (const href of ['javascript:alert(1)', 'JaVaScRiPt:x', 'file:///etc/passwd', 'data:text/html,x',
    'vscode://file/x', '//evil.example/x', '', '   ']) {
    assert.deepEqual(links.classifyLink(href, 'README.md'), { kind: 'ignore' }, href);
  }
});

test('an anchor scrolls within the file', () => {
  assert.deepEqual(links.classifyLink('#getting-started', 'docs/a.md'), { kind: 'anchor', slug: 'getting-started' });
  assert.deepEqual(links.classifyLink('#%C3%A9kezet', 'docs/a.md'), { kind: 'anchor', slug: 'ékezet' });
});

test('a relative link opens the file it names, never one outside the root', () => {
  assert.deepEqual(links.classifyLink('guide.md', 'docs/a.md'), { kind: 'file', path: 'docs/guide.md', slug: '' });
  assert.deepEqual(links.classifyLink('./img/../guide.md#setup', 'docs/a.md'),
    { kind: 'file', path: 'docs/guide.md', slug: 'setup' });
  assert.deepEqual(links.classifyLink('../CHANGELOG.md', 'docs/a.md'), { kind: 'file', path: 'CHANGELOG.md', slug: '' });
  assert.deepEqual(links.classifyLink('/src/main.go?plain=1', 'docs/deep/a.md'),
    { kind: 'file', path: 'src/main.go', slug: '' });
  assert.deepEqual(links.classifyLink('My%20Notes.md', 'README.md'), { kind: 'file', path: 'My Notes.md', slug: '' });
  assert.deepEqual(links.classifyLink('../../etc/passwd', 'docs/a.md'), { kind: 'ignore' });
  assert.deepEqual(links.classifyLink('../x.md', 'README.md'), { kind: 'ignore' });
});

test('relative paths resolve against the file\'s directory', () => {
  assert.equal(links.resolveRelativePath('docs/a.md', 'img/logo.png'), 'docs/img/logo.png');
  assert.equal(links.resolveRelativePath('README.md', 'docs//img/./logo.png'), 'docs/img/logo.png');
  assert.equal(links.resolveRelativePath('docs/a.md', '/logo.png'), 'logo.png');
  assert.equal(links.resolveRelativePath('docs/a.md', '..'), null);
  assert.equal(links.resolveRelativePath('docs/a.md', '../..'), null);
  assert.equal(links.resolveRelativePath('docs/a.md', ''), null);
});

test('headings get GitHub\'s anchors, numbered when repeated', () => {
  assert.equal(links.headingSlug('Getting Started!'), 'getting-started');
  assert.equal(links.headingSlug('  API: v2 (beta)  '), 'api-v2-beta');
  assert.equal(links.headingSlug('Ékezetes Cím'), 'ékezetes-cím');
  assert.equal(links.headingSlug('snake_case & kebab-case'), 'snake_case--kebab-case');
  assert.deepEqual(links.headingSlugs(['Usage', 'Notes', 'Usage', 'Usage']),
    ['usage', 'notes', 'usage-1', 'usage-2']);
});
