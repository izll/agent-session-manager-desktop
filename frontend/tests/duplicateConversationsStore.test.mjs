import { test } from 'node:test';
import assert from 'node:assert/strict';
import { get } from 'svelte/store';
import { duplicateTabs, setDuplicateConversations } from '../src/lib/stores/duplicateConversations.ts';

test('each marked tab knows the other places, not its own', () => {
  setDuplicateConversations([{
    conversationId: 'c', places: ['A #0', 'A #6', ''],
    tabs: [{ sessionId: 's', windowIdx: 0, place: 0 }, { sessionId: 's', windowIdx: 6, place: 1 }],
  }]);
  const tabs = get(duplicateTabs);
  assert.deepEqual(tabs.get('s:0'), ['A #6', '']);
  assert.deepEqual(tabs.get('s:6'), ['A #0', '']);
  setDuplicateConversations([]);
  assert.equal(get(duplicateTabs).size, 0);
});
