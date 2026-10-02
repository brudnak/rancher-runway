import {newPackageRecordID} from './issue-packages.mjs';

export const MAX_CASE_STEPS = 100;

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
