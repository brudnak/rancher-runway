import test from 'node:test';
import assert from 'node:assert/strict';
import { readRadarJSON } from './issue-radar-request.mjs';

test('request deadline covers a stalled JSON body and aborts the underlying request', async () => {
  const controller = new AbortController();
  await assert.rejects(readRadarJSON(async signal => {
    assert.equal(signal, controller.signal);
    return { json: () => new Promise(() => {}) };
  }, controller, 'Owner history', 15), /Owner history did not respond/);
  assert.equal(controller.signal.aborted, true);
});

test('user cancellation propagates and successful parsing clears the deadline', async () => {
  const controller = new AbortController();
  const pending = readRadarJSON(signal => new Promise((resolve, reject) => signal.addEventListener('abort', () => reject(new DOMException('Cancelled', 'AbortError')))), controller, 'Radar', 1000);
  controller.abort();
  await assert.rejects(pending, { name: 'AbortError' });
  const success = new AbortController();
  assert.deepEqual(await readRadarJSON(async () => ({ json: async () => ({ issues: [] }) }), success, 'Radar', 15), { issues: [] });
  await new Promise(resolve => setTimeout(resolve, 25));
  assert.equal(success.signal.aborted, false);
});
