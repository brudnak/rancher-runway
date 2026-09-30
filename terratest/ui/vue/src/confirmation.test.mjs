import test from 'node:test';
import assert from 'node:assert/strict';
import { CONFIRMATION_TEXT, isConfirmed } from './confirmation.mjs';

test('typed confirmations use the same exact lowercase phrase as the API', () => {
  assert.equal(CONFIRMATION_TEXT, 'confirm');
  assert.equal(isConfirmed('confirm'), true);
  for (const value of ['', null, undefined, true, 'CONFIRM', 'Confirm', ' confirm', 'confirm ', 'confirm\n', 'DELETE RUN', 'RUN rancher.example.test']) {
    assert.equal(isConfirmed(value), false, `Unexpected confirmation: ${JSON.stringify(value)}`);
  }
});
