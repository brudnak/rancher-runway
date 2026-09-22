import test from 'node:test';
import assert from 'node:assert/strict';
import { readJSON } from './read-json.mjs';

test('a stalled request times out, aborts, and permits a later successful check', async () => {
  let signal;
  await assert.rejects(readJSON(s => {
    signal = s;
    return new Promise(() => {});
  }, { label: 'Status check', timeoutMs: 10 }), /Status check did not respond/);
  assert.equal(signal.aborted, true);
  assert.deepEqual(await readJSON(async () => ({ json: async () => ({ ready: true }) })), { ready: true });
});

test('the deadline also covers a stalled response body', async () => {
  let signal;
  await assert.rejects(readJSON(async s => {
    signal = s;
    return { json: () => new Promise(() => {}) };
  }, { timeoutMs: 10 }), /did not respond/);
  assert.equal(signal.aborted, true);
});

test('completed reads clear their timers and retain network and parsing errors', async () => {
  let signal;
  await readJSON(async s => {
    signal = s;
    return { json: async () => ({}) };
  }, { timeoutMs: 10 });
  await new Promise(resolve => setTimeout(resolve, 20));
  assert.equal(signal.aborted, false);
  for (const request of [
    async () => { throw new Error('Network unavailable'); },
    async () => ({ json: async () => { throw new Error('Invalid JSON'); } }),
  ]) await assert.rejects(readJSON(request), /Network unavailable|Invalid JSON/);
});
