import test from 'node:test';
import assert from 'node:assert/strict';
import {insertCaseStep, removeCaseStep, restoreCaseStep, caseWritingSummary, MAX_CASE_STEPS} from './issue-package-case-editor.mjs';

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
