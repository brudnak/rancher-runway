import {newPackageRecordID} from './issue-packages.mjs';

export const MAX_CASE_STEPS = 100;
const MAX_STEP_TEXT = 16000;
const textBytes = value => new TextEncoder().encode(String(value ?? '')).length;
const caseTextFields = {
  title:{limit:240,label:'The case title'},
  expected:{limit:16000,label:'The expected outcome'},
  preconditions:{limit:16000,label:'The setup text'},
  automationUrl:{limit:2048,label:'The automation URL'},
};

export function caseFieldWritingProblem(field, value) {
  const rule = caseTextFields[field];
  return rule && textBytes(value) > rule.limit ? `${rule.label} is too long to save. Shorten it and try again.` : '';
}

// Numbered lines are steps; indentation keeps longer actions and expectations
// together. The escape preserves literal “Expected:” lines in existing actions.
export function formatCaseSteps(steps = []) {
  return steps.map((step, index) => {
    const action = String(step.instruction || '').split('\n');
    const lines = [`${index + 1}. ${action[0]}`, ...action.slice(1).map(line => `   ${/^(?:\\)*Expected:/i.test(line) ? '\\' : ''}${line}`)];
    if (step.expected) {
      const expected = String(step.expected).split('\n');
      lines.push(`   Expected: ${expected[0]}`, ...expected.slice(1).map(line => `   ${line}`));
    }
    return lines.join('\n');
  }).join('\n');
}

export function parseCaseSteps(text = '') {
  const steps = [];
  let current = null, field = 'instruction';
  for (const line of String(text).replace(/\r\n?/g, '\n').split('\n')) {
    const marker = line.match(/^[ \t]?(?:\d+[.)]|[-*+])(?:[ \t]+(.*)|$)/);
    if (marker || (line.trim() && !/^\s/.test(line))) {
      current = {instruction:marker ? marker[1] || '' : line, expected:''};
      steps.push(current);
      field = 'instruction';
    } else if (current && line.trim()) {
      const content = line.replace(/^(?: {1,3}|\t)/, '');
      if (field === 'instruction' && /^Expected:/i.test(content)) {
        field = 'expected';
        current.expected = content.replace(/^Expected:[ \t]?/i, '');
      } else {
        const continuation = field === 'instruction' ? content.replace(/^\\(?=(?:\\)*Expected:)/i, '') : content;
        current[field] += `\n${continuation}`;
      }
    } else if (current) {
      current[field] += '\n';
    }
  }
  return steps.map(step => ({instruction:step.instruction.trimEnd(), expected:step.expected.trimEnd()}))
    .filter(step => step.instruction.trim() || step.expected.trim());
}

export function reconcileCaseSteps(previous = [], text = '', newID = newPackageRecordID) {
  const parsed = parseCaseSteps(text);
  if (parsed.length > MAX_CASE_STEPS) return {error:`Keep this case to ${MAX_CASE_STEPS} steps. Split the rest into another case.`};
  if (parsed.some(step => textBytes(step.instruction) > MAX_STEP_TEXT || textBytes(step.expected) > MAX_STEP_TEXT)) {
    return {error:'Each action and expected result can contain up to 16,000 bytes. Some characters use more than one byte.'};
  }
  const used = new Set(), matches = new Map();
  // Reserve exact matches before matching actions whose checks have changed.
  // This distinguishes repeated actions with different expected results.
  for (const matchExpectation of [true, false]) {
    for (const [index, step] of parsed.entries()) {
      if (matches.has(index)) continue;
      const match = previous.findIndex((old, oldIndex) => !used.has(oldIndex) && old.instruction === step.instruction && (!matchExpectation || (old.expected || '') === step.expected));
      if (match >= 0) {matches.set(index, match); used.add(match);}
    }
  }
  // Pair edits inside their surviving neighbors before allocating new identities.
  for (const [index] of parsed.entries()) {
    if (matches.has(index)) continue;
    const before = [...matches.entries()].filter(([position]) => position < index).sort((a,b) => b[0] - a[0])[0]?.[1] ?? -1;
    const after = [...matches.entries()].filter(([position]) => position > index).sort((a,b) => a[0] - b[0])[0]?.[1] ?? previous.length;
    const match = previous.findIndex((_, oldIndex) => !used.has(oldIndex) && oldIndex > before && oldIndex < after);
    if (match >= 0) {matches.set(index, match);used.add(match);}
  }
  return {steps:parsed.map((step, index) => ({...(matches.has(index) ? previous[matches.get(index)] : {id:newID()}), ...step}))};
}

// Work with copies: the plan and every preserved session have independent steps.
export function insertCaseStep(steps = [], index = steps.length, source = null, newID = newPackageRecordID) {
  if (steps.length >= MAX_CASE_STEPS) return null;
  const step = {id: newID(), instruction: source?.instruction || '', expected: source?.expected || ''};
  const next = [...steps];
  next.splice(Math.max(0, Math.min(index, next.length)), 0, step);
  return {steps: next, step};
}

export function removeCaseStep(steps, id) {
  const index = steps.findIndex(step => step.id === id);
  if (index < 0) return null;
  return {
    steps: steps.filter(step => step.id !== id),
    removed: {step: {...steps[index]}, index, beforeId: steps[index - 1]?.id, afterId: steps[index + 1]?.id},
  };
}

export function restoreCaseStep(steps, removed) {
  if (!removed || steps.length >= MAX_CASE_STEPS || steps.some(step => step.id === removed.step.id)) return null;
  const afterIndex = steps.findIndex(step => step.id === removed.afterId);
  const beforeIndex = steps.findIndex(step => step.id === removed.beforeId);
  const index = afterIndex >= 0 ? afterIndex : beforeIndex >= 0 ? beforeIndex + 1 : Math.min(removed.index, steps.length);
  const next = [...steps];
  next.splice(index, 0, {...removed.step});
  return {steps: next, index};
}

export function caseWritingSummary(item) {
  const steps = item.steps || [];
  return {
    total: steps.length,
    expectations: steps.filter(step => String(step.expected || '').trim()).length,
    missingActions: steps.flatMap((step, index) => String(step.instruction || '').trim() ? [] : [index]),
  };
}
