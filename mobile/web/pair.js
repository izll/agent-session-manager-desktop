// The page a phone opens from the QR code: it pairs with the code in the address,
// then takes the code out of the address, so it is not left in the history.

/** The pairing code in the address, or '' when there is none worth sending. */
export function pairingCode(hash) {
  const match = /^#pair=([A-Za-z0-9_-]{16,64})$/.exec(hash || '');
  return match ? match[1] : '';
}

/** A name for the device, for the list of devices on the computer. */
export function deviceName(userAgent) {
  const ua = userAgent || '';
  if (/iPhone/.test(ua)) return 'iPhone';
  if (/iPad/.test(ua)) return 'iPad';
  if (/Android/.test(ua)) return 'Android';
  if (/Macintosh/.test(ua)) return 'Mac';
  if (/Windows/.test(ua)) return 'Windows';
  if (/Linux/.test(ua)) return 'Linux';
  return 'Browser';
}

const TEXT = {
  en: {
    paired: (name) => `Paired as ${name}. You can close this page.`,
    known: (name) => `This device is paired (${name}).`,
    none: 'Not paired. Scan the QR code in the app: Settings → Mobile access.',
    used: 'This code is used up or has expired. Make a new one in the app.',
    locked: 'Too many wrong codes. Try again in 15 minutes.',
    failed: 'Pairing failed.',
  },
  hu: {
    paired: (name) => `Párosítva: ${name}. Bezárhatod ezt az oldalt.`,
    known: (name) => `Ez az eszköz párosítva van (${name}).`,
    none: 'Nincs párosítva. Olvasd be a QR-kódot az alkalmazásban: Beállítások → Mobil elérés.',
    used: 'Ez a kód már elhasználódott vagy lejárt. Kérj újat az alkalmazásban.',
    locked: 'Túl sok rossz kód. Próbáld újra 15 perc múlva.',
    failed: 'A párosítás nem sikerült.',
  },
};

async function run() {
  const text = TEXT[(navigator.language || '').startsWith('hu') ? 'hu' : 'en'];
  const status = document.getElementById('status');
  const show = (message, kind = '') => { status.textContent = message; status.className = kind; };
  const code = pairingCode(location.hash);
  if (location.hash) history.replaceState(null, '', location.pathname);
  if (code) {
    const name = deviceName(navigator.userAgent);
    const response = await fetch('/api/pair', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ code, name }),
    }).catch(() => null);
    if (response?.ok) return show(text.paired(name), 'ok');
    if (response?.status === 401) return show(text.used, 'bad');
    if (response?.status === 429) return show(text.locked, 'bad');
    return show(text.failed, 'bad');
  }
  const me = await fetch('/api/me').catch(() => null);
  if (me?.ok) return show(text.known((await me.json()).name), 'ok');
  return show(text.none);
}

if (typeof document !== 'undefined') void run();
