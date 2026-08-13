import assert from 'node:assert/strict';
import test from 'node:test';

import { transformEventsToMessages } from './transform.js';

test('attaches history tool results to their matching tool calls', () => {
  const messages = transformEventsToMessages([
    {
      type: 'message',
      timestamp: '2025-01-01T12:00:00Z',
      message: {
        role: 'assistant',
        content: [{
          type: 'toolCall',
          id: 'call-1',
          toolCallName: 'bash',
          arguments: { command: 'pwd' },
        }],
      },
    },
    {
      type: 'message',
      timestamp: '2025-01-01T12:00:01Z',
      message: {
        role: 'toolResult',
        toolCallId: 'call-1',
        toolName: 'bash',
        content: 'output',
      },
    },
  ]);

  assert.equal(messages.length, 1);
  assert.equal(messages[0].toolCalls[0].id, 'call-1');
  assert.equal(messages[0].toolCalls[0].result, 'output');
  assert.equal(messages[0].toolCalls[0].resultIsError, false);
});

test('keeps unmatched tool results standalone', () => {
  const messages = transformEventsToMessages([
    {
      type: 'message_end',
      message: {
        role: 'toolResult',
        toolCallId: 'missing-call',
        toolName: 'bash',
        content: 'output',
      },
    },
  ]);

  assert.equal(messages.length, 1);
  assert.equal(messages[0].role, 'toolResult');
});
