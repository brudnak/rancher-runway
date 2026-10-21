import test from 'node:test';
import assert from 'node:assert/strict';
import {insertCaseStep, removeCaseStep, restoreCaseStep, caseWritingSummary, MAX_CASE_STEPS, formatCaseSteps, parseCaseSteps, reconcileCaseSteps, caseFieldWritingProblem} from './issue-package-case-editor.mjs';

test('writing limits match stored UTF-8 bytes for ASCII, accented text, CJK, and emoji', () => {
  for (const [field,limit] of [['title',240],['expected',16000],['preconditions',16000],['automationUrl',2048]]) {
    assert.equal(caseFieldWritingProblem(field, 'x'.repeat(limit)), '');
    assert.ok(caseFieldWritingProblem(field, 'x'.repeat(limit + 1)));
    for (const [character,bytes] of [['é',2],['界',3],['🧪',4]]) {
      const fitting = character.repeat(Math.floor(limit / bytes));
      assert.equal(caseFieldWritingProblem(field, fitting), '');
      assert.ok(caseFieldWritingProblem(field, fitting + character));
    }
  }
  assert.equal(caseFieldWritingProblem('title', ''), '');
});

test('the steps writer accepts numbered, bulleted, and plain action lines without creating blank steps', () => {
  assert.deepEqual(parseCaseSteps('1. Open Rancher\r\n2) Open the project\r\n\r\n- Change the role\nVerify permissions\n5. '), [
    {instruction:'Open Rancher', expected:''}, {instruction:'Open the project', expected:''},
    {instruction:'Change the role', expected:''}, {instruction:'Verify permissions', expected:''},
  ]);
  assert.deepEqual(reconcileCaseSteps([], '').steps, []);
});

test('the writer round-trips multiline actions and expectations, including literal Expected lines', () => {
  const source = [
    {instruction:'Open the project\nRun the following check:\nExpected: literal command output\n\\Expected: another literal line', expected:'Only permitted namespaces appear.\n\nThe role remains unchanged.'},
    {instruction:'Update the role', expected:''},
  ];
  assert.deepEqual(parseCaseSteps(formatCaseSteps(source)), source);
});

test('writing an inserted or renamed step preserves the identities and checks of existing steps', () => {
  const source = [{id:'a', instruction:'Open Rancher', expected:'The dashboard loads'}, {id:'b', instruction:'Open the project', expected:'The project loads'}];
  const result = reconcileCaseSteps(source, '1. Sign in\n2. Open Rancher\n   Expected: The dashboard loads\n3. Open the assigned project\n   Expected: The project loads', () => 'new');
  assert.deepEqual(result.steps.map(step => step.id), ['new', 'a', 'b']);
  assert.equal(result.steps[2].expected, 'The project loads');
  assert.equal(source[1].instruction, 'Open the project');
});

test('reordering and removing lines keep the remaining steps attached to their original identities', () => {
  const source = [{id:'a', instruction:'First', expected:'One'}, {id:'b', instruction:'Second', expected:'Two'}, {id:'c', instruction:'Third', expected:'Three'}];
  const result = reconcileCaseSteps(source, formatCaseSteps([source[2], source[0]]));
  assert.deepEqual(result.steps.map(step => step.id), ['c', 'a']);
  assert.deepEqual(result.steps.map(step => step.expected), ['Three', 'One']);
  assert.equal(source.length, 3);
});

test('duplicate actions retain distinct identities and a check can be cleared through writing', () => {
  const source = [{id:'a', instruction:'Refresh', expected:'First result'}, {id:'b', instruction:'Refresh', expected:'Second result'}];
  const result = reconcileCaseSteps(source, '1. Refresh\n2. Refresh\n   Expected: Second result');
  assert.deepEqual(result.steps.map(step => step.id), ['a', 'b']);
  assert.equal(result.steps[0].expected, '');
  assert.equal(result.steps[1].expected, 'Second result');
  const reordered = reconcileCaseSteps(source, formatCaseSteps([source[1], source[0]]));
  assert.deepEqual(reordered.steps.map(step => step.id), ['b', 'a']);
});

test('oversized writing is rejected atomically instead of silently truncating a case', () => {
  const source = [{id:'a', instruction:'Original', expected:'Preserved'}];
  assert.match(reconcileCaseSteps(source, Array.from({length:101}, (_, index) => `${index + 1}. Action`).join('\n')).error, /100 steps/);
  assert.match(reconcileCaseSteps(source, 'x'.repeat(16001)).error, /16,000/);
  assert.equal(reconcileCaseSteps(source, '🧪'.repeat(4000)).error, undefined);
  assert.match(reconcileCaseSteps(source, '🧪'.repeat(4001)).error, /16,000 bytes/);
  assert.match(reconcileCaseSteps(source, `1. Check output\n   Expected: ${'界'.repeat(5334)}`).error, /16,000 bytes/);
  assert.deepEqual(source, [{id:'a', instruction:'Original', expected:'Preserved'}]);
});

test('duplicating a step creates independent content and identity without changing the plan', () => {
  const original = {id:'original', instruction:'Open the project', expected:'Only permitted namespaces appear'};
  const source = [original];
  const result = insertCaseStep(source, 1, original, () => 'copy');
  assert.deepEqual(result.steps.map(step => step.id), ['original', 'copy']);
  result.step.instruction = 'Open the next project';
  assert.equal(original.instruction, 'Open the project');
  assert.equal(source.length, 1);
  assert.equal(result.step.expected, original.expected);
});

test('undo restores a removed step beside its surviving neighbor while preserving later edits', () => {
  const steps = [{id:'a', instruction:'First'}, {id:'b', instruction:'Second'}, {id:'c', instruction:'Third'}];
  const result = removeCaseStep(steps, 'b');
  const edited = [{id:'new', instruction:'New first step'}, {...result.steps[0], instruction:'Updated first'}, result.steps[1]];
  const restored = restoreCaseStep(edited, result.removed);
  assert.deepEqual(restored.steps.map(step => step.id), ['new', 'a', 'b', 'c']);
  assert.equal(restored.steps[1].instruction, 'Updated first');
  assert.equal(steps[0].instruction, 'First');
  assert.equal(restored.steps[2].instruction, 'Second');
});

test('multiple removed steps can be restored in reverse order with original identities', () => {
  const source = [{id:'a'}, {id:'b'}, {id:'c'}];
  const first = removeCaseStep(source, 'b');
  const second = removeCaseStep(first.steps, 'c');
  const restoreSecond = restoreCaseStep(second.steps, second.removed);
  const restoreFirst = restoreCaseStep(restoreSecond.steps, first.removed);
  assert.deepEqual(restoreFirst.steps.map(step => step.id), ['a', 'b', 'c']);
  assert.equal(restoreCaseStep(restoreFirst.steps, first.removed), null);
});

test('insert and undo respect the portable plan step limit', () => {
  const full = Array.from({length:MAX_CASE_STEPS}, (_, index) => ({id:String(index)}));
  assert.equal(insertCaseStep(full, 1), null);
  assert.equal(restoreCaseStep(full, {step:{id:'another'}, index:0}), null);
  assert.equal(removeCaseStep(full, 'unknown'), null);
});

test('case writing hints distinguish missing actions from optional expectations', () => {
  assert.deepEqual(caseWritingSummary({steps:[{instruction:' Open ', expected:''}, {instruction:' \n', expected:'Visible'}]}), {total:2, expectations:1, missingActions:[1]});
});
