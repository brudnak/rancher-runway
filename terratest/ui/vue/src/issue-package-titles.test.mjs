import test from 'node:test';
import assert from 'node:assert/strict';
import {parseCaseTitles, caseTitlesError, MAX_PLAN_CASES, MAX_CASE_TITLE_BYTES} from './issue-package-titles.mjs';

test('pasted numbered, bulleted, and task lists become case titles in their original order', () => {
  const source = '\r\n1. Open the cluster\r\n2) Update its settings\n- [ ] Verify the change\n* [x] Open another cluster\n+ Inspect the logs\n• Check the UI\n[X] Finish testing\n Plain title \n';
  assert.deepEqual(parseCaseTitles(source), ['Open the cluster', 'Update its settings', 'Verify the change', 'Open another cluster', 'Inspect the logs', 'Check the UI', 'Finish testing', 'Plain title']);
});

test('blank lines and unfinished list markers do not create empty cases', () => {
  assert.deepEqual(parseCaseTitles('\n  \n1.\n2) \n-\n* [ ]\n[ ]\n- [x] \nReady\n'), ['Ready']);
});

test('versions and punctuation within titles survive normalization', () => {
  assert.deepEqual(parseCaseTitles('2.15 upgrades correctly\n1. 2.15 → 2.16 upgrade\n-flag behavior\n+1 node\n[IPv6] networking\nTest - [ ] syntax'), ['2.15 upgrades correctly', '2.15 → 2.16 upgrade', '-flag behavior', '+1 node', '[IPv6] networking', 'Test - [ ] syntax']);
});

test('repeated titles are preserved so distinct scenarios are never silently removed', () => {
  assert.deepEqual(parseCaseTitles('1. Login\n2. Login'), ['Login', 'Login']);
});

test('case capacity includes the cases already in the plan', () => {
  assert.equal(caseTitlesError(Array(MAX_PLAN_CASES).fill('Title')), '');
  assert.equal(caseTitlesError(['Last'], MAX_PLAN_CASES - 1), '');
  assert.match(caseTitlesError(['One', 'Two'], MAX_PLAN_CASES - 1), /room for 1 more case/);
  assert.match(caseTitlesError(['One'], MAX_PLAN_CASES), /already has 250 cases/);
  assert.match(caseTitlesError(Array(MAX_PLAN_CASES + 1).fill('Title')), /250-case plan limit/);
});

test('title validation follows the backend UTF-8 byte limit without truncating text', () => {
  const ascii = 'a'.repeat(MAX_CASE_TITLE_BYTES);
  const emoji = '🧪'.repeat(MAX_CASE_TITLE_BYTES / 4);
  assert.equal(caseTitlesError([ascii, emoji, 'é'.repeat(MAX_CASE_TITLE_BYTES / 2)]), '');
  assert.match(caseTitlesError(['Valid', `${emoji}a`]), /Case 2 is too long/);
  assert.match(caseTitlesError([`${ascii}a`]), /240 bytes/);
  assert.equal(parseCaseTitles(`1. ${ascii}a`)[0].length, MAX_CASE_TITLE_BYTES + 1);
});

test('invalid titles are rejected before creating a plan', () => {
  assert.match(caseTitlesError(['Valid', 'Bad\0title']), /Case 2 contains an unsupported character/);
  assert.match(caseTitlesError(['  ']), /needs a title/);
});
