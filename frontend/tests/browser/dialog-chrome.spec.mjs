import { test, expect } from '@playwright/test';

// The background-agents, servers and saved-commands dialogs looked like other
// windows beside Settings: a smaller title, a bare "×" in a borderless button,
// tighter padding, a different corner. The source test stops local restyling;
// this measures what is actually drawn, against Settings.

async function chrome(page, dialog) {
  await page.goto(`/tests/browser/dialog-chrome-fixture.html?dialog=${dialog}`);
  await expect(page.locator('body')).toHaveAttribute('data-fixture-ready', 'true', { timeout: 15_000 });
  return page.evaluate(() => {
    const pick = (selector, props) => {
      const el = document.querySelector(selector);
      if (!el) return null;
      const style = getComputedStyle(el);
      return Object.fromEntries(props.map((p) => [p, style.getPropertyValue(p)]));
    };
    const button = document.querySelector('.dialog-header .close-btn');
    const box = button?.getBoundingClientRect();
    const icon = button?.querySelector('svg')?.getBoundingClientRect();
    return {
      overlay: pick('.dialog-overlay', ['background-color']),
      panel: pick('.dialog-content', [
        'border-top-width', 'border-top-color', 'border-top-left-radius', 'box-shadow',
        'padding-top', 'padding-left', 'background-image',
      ]),
      header: pick('.dialog-header', [
        'padding-top', 'padding-right', 'padding-bottom', 'padding-left',
        'border-bottom-color', 'background-image',
      ]),
      title: pick('.dialog-header h2', ['font-size', 'font-weight', '-webkit-text-fill-color', 'background-image']),
      close: button && {
        width: box.width,
        height: box.height,
        icon: icon ? [icon.width, icon.height] : null,
        ...pick('.dialog-header .close-btn', ['background-color', 'border-top-left-radius', 'color']),
        label: button.getAttribute('aria-label'),
      },
    };
  });
}

let reference;
test.beforeAll(async ({ browser }) => {
  const page = await browser.newPage();
  reference = await chrome(page, 'settings');
  await page.close();
});

test('Settings, the reference, has the shared chrome', () => {
  expect(reference.close).toMatchObject({ width: 32, height: 32, icon: [20, 20], label: 'Close' });
  expect(reference.title['font-size']).toBe('18px');
  expect(reference.header['padding-left']).toBe('24px');
  expect(reference.panel['border-top-left-radius']).toBe('16px');
});

for (const dialog of ['bgAgents', 'servers', 'commands', 'commandPicker', 'schemeImport', 'logs', 'whatsNew', 'interruptedWork']) {
  test(`${dialog} wears the same chrome as Settings`, async ({ page }) => {
    const measured = await chrome(page, dialog);
    expect(measured.overlay).toEqual(reference.overlay);
    expect(measured.panel).toEqual(reference.panel);
    expect(measured.header).toEqual(reference.header);
    expect(measured.title).toEqual(reference.title);
    expect(measured.close).toEqual(reference.close);
  });
}

// ── The header ────────────────────────────────────────────────────────────
//
// Every dialog with a header, measured against Settings'. Before this the
// markup was shared but the layout was not: the history's maximise button was
// a 30px box with a 15px icon, the task dialogs' microphone a bare 14px glyph
// in a 26×22 button, the new session's template action an underlined 12px
// link, and a task title — the task overview's detail, the subtask and
// dependency dialogs name their task — wrapped to two or three lines, making
// the header 81–101px tall and squeezing the ✕ to 20px wide.

const HEADER_DIALOGS = [
  'settings', 'bgAgents', 'servers', 'commands', 'commandPicker', 'schemeImport', 'logs',
  'checkpoints', 'customColor', 'customGradient', 'feedback', 'fork', 'gitHistory', 'help',
  'import', 'newGroup', 'newSession', 'newTab', 'quickJump', 'recovery', 'remoteDir',
  'resumePicker', 'saveAsTemplate', 'sessionColor', 'sessionFile', 'templates', 'tabColor',
  'update', 'whatsNew', 'taskDetail', 'taskAdd', 'taskEdit', 'taskSubtask', 'taskDependencies', 'taskPRD',
  'taskComplexity', 'extraArgs', 'interruptedWork',
];

// The one header allowed to be taller: its subtitle is a second line.
const TALLER = { recovery: 'a subtitle under the title' };

async function header(page, dialog) {
  await page.goto(`/tests/browser/dialog-chrome-fixture.html?dialog=${dialog}`);
  await expect(page.locator('body')).toHaveAttribute('data-fixture-ready', 'true', { timeout: 30_000 });
  return page.evaluate(() => {
    const all = document.querySelectorAll('.dialog-header');
    const h = all[all.length - 1];
    const style = (el, props) => {
      const s = getComputedStyle(el);
      return Object.fromEntries(props.map((p) => [p, s.getPropertyValue(p)]));
    };
    const box = (el) => el.getBoundingClientRect();
    const hb = box(h);
    const title = h.querySelector('h2');
    const tb = box(title);
    const buttons = [...h.querySelectorAll('button')].map((b) => {
      const bb = box(b);
      const icon = b.querySelector('svg');
      return {
        close: b.classList.contains('close-btn'),
        text: b.classList.contains('text'),
        label: b.getAttribute('aria-label') || b.textContent.trim(),
        left: bb.left, right: bb.right, top: bb.top, width: bb.width, height: bb.height,
        icon: icon ? [box(icon).width, box(icon).height] : null,
        ...style(b, ['background-color', 'border-top-left-radius', 'border-top-width']),
      };
    });
    return {
      height: hb.height,
      style: style(h, ['padding-top', 'padding-right', 'padding-bottom', 'padding-left', 'gap', 'border-bottom-color', 'background-image']),
      title: {
        left: tb.left - hb.left,
        middle: tb.top + tb.height / 2,
        height: tb.height,
        truncated: title.scrollWidth > title.clientWidth,
        tooltip: title.getAttribute('title'),
        text: title.textContent.trim(),
        ...style(title, ['font-size', 'font-weight', 'letter-spacing', 'white-space', 'text-overflow', 'overflow-x', '-webkit-text-fill-color', 'background-image']),
      },
      headerRight: hb.right,
      buttons,
    };
  });
}

let referenceHeader;
test.beforeAll(async ({ browser }) => {
  const page = await browser.newPage();
  referenceHeader = await header(page, 'settings');
  await page.close();
});

test('Settings, the reference, has the header convention', () => {
  expect(referenceHeader.height).toBe(73);
  expect(referenceHeader.style.gap).toBe('12px');
  expect(referenceHeader.title).toMatchObject({ left: 24, 'font-size': '18px', 'white-space': 'nowrap', 'text-overflow': 'ellipsis' });
  expect(referenceHeader.buttons).toHaveLength(1);
});

for (const dialog of HEADER_DIALOGS) {
  test(`${dialog} has the same header as Settings`, async ({ page }) => {
    const measured = await header(page, dialog);
    const ref = referenceHeader;

    expect(measured.style).toEqual(ref.style);
    if (!TALLER[dialog]) expect(measured.height).toBe(ref.height);

    // The title: the same type, at the same place, on one line.
    const { left, height, truncated, tooltip, text, middle, ...titleStyle } = measured.title;
    const { left: refLeft, height: refHeight, truncated: _t, tooltip: _tt, text: _x, middle: _m, ...refTitleStyle } = ref.title;
    expect(titleStyle).toEqual(refTitleStyle);
    expect(left).toBe(refLeft);
    expect(height).toBe(refHeight);
    // Cut short, it says the rest on hover.
    if (truncated) expect(tooltip).toBe(text);

    // The ✕: last, at the right padding, the full 32px box.
    const close = measured.buttons.at(-1);
    const refClose = ref.buttons.at(-1);
    expect(close.close).toBe(true);
    expect(measured.headerRight - close.right).toBe(ref.headerRight - refClose.right);
    expect([close.width, close.height, close.icon]).toEqual([refClose.width, refClose.height, refClose.icon]);
    // Centred on the title's line (the subtitle's header centres on the pair).
    if (!TALLER[dialog]) expect(Math.abs(close.top + close.height / 2 - middle)).toBeLessThanOrEqual(1);

    // Any other button: the ✕'s box and look, 8px apart, in line with it.
    const extras = measured.buttons.slice(0, -1);
    extras.forEach((b, i) => {
      const next = measured.buttons[i + 1];
      expect(b.close, `${b.label} is a second ✕`).toBe(false);
      expect(b.height, `${b.label} height`).toBe(refClose.height);
      if (!b.text) expect(b.width, `${b.label} width`).toBe(refClose.width);
      if (b.icon) expect(b.icon, `${b.label} icon`).toEqual([16, 16]);
      expect(b['border-top-left-radius'], `${b.label} corner`).toBe(refClose['border-top-left-radius']);
      expect(b['border-top-width'], `${b.label} border`).toBe(refClose['border-top-width']);
      expect(b['background-color'], `${b.label} background`).toBe(refClose['background-color']);
      expect(b.top, `${b.label} is out of line with the ✕`).toBe(close.top);
      expect(next.left - b.right, `gap after ${b.label}`).toBe(8);
    });
  });
}
