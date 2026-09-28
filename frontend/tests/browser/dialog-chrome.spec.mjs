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

for (const dialog of ['bgAgents', 'servers', 'commands', 'commandPicker', 'schemeImport', 'logs']) {
  test(`${dialog} wears the same chrome as Settings`, async ({ page }) => {
    const measured = await chrome(page, dialog);
    expect(measured.overlay).toEqual(reference.overlay);
    expect(measured.panel).toEqual(reference.panel);
    expect(measured.header).toEqual(reference.header);
    expect(measured.title).toEqual(reference.title);
    expect(measured.close).toEqual(reference.close);
  });
}
