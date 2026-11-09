import { test } from 'node:test';
import assert from 'node:assert/strict';
import { describeDuplicateConversations } from '../src/lib/utils/duplicateConversations.ts';

const t = (key, params = {}) => key === 'conversations.outsideApp'
  ? 'outside'
  : `${params.id} in ${params.places}`;

test('each duplicate names its conversation and every place, outside the app too', () => {
  const text = describeDuplicateConversations([
    { conversationId: '459357eb-1241-49a0', places: ['WebErp 2 › weberp', 'WebErp 2 › crawling'] },
    { conversationId: 'abcdef12-3456', places: ['x › y', ''] },
  ], t);
  assert.equal(text, '459357eb in WebErp 2 › weberp, WebErp 2 › crawling\nabcdef12 in x › y, outside');
});
