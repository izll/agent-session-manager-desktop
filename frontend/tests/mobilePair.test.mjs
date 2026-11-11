import { test } from 'node:test';
import assert from 'node:assert/strict';
import { pairingCode, deviceName } from '../../mobile/web/pair.js';

test('the pairing code is read from the address, and only a real one', () => {
  assert.equal(pairingCode('#pair=AbC_def-1234567890xyz'), 'AbC_def-1234567890xyz');
  assert.equal(pairingCode(''), '');
  assert.equal(pairingCode('#pair='), '');
  assert.equal(pairingCode('#pair=short'), '');
  assert.equal(pairingCode('#pair=<script>alert(1)</script>xxxxxxxx'), '');
  assert.equal(pairingCode('#other=AbC_def-1234567890xyz'), '');
});

test('a device is named by what it is', () => {
  assert.equal(deviceName('Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X)'), 'iPhone');
  assert.equal(deviceName('Mozilla/5.0 (Linux; Android 14; Pixel 8)'), 'Android');
  assert.equal(deviceName('Mozilla/5.0 (iPad; CPU OS 17_0 like Mac OS X)'), 'iPad');
  assert.equal(deviceName('Mozilla/5.0 (X11; Linux x86_64)'), 'Linux');
  assert.equal(deviceName(''), 'Browser');
});
