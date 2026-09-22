import test from 'node:test';
import assert from 'node:assert/strict';
import { buildIssueRadarReport, buildIssueRadarBrief, visibleIssueLanes, parseUsers } from './issue-radar.mjs';

const issue = (number, users = [], labels = [], milestone = 'v2.14.0') => ({ number, title: `Issue ${number}`, assignees: users.map(login => ({ login })), labels: labels.map(name => ({ name })), milestone: milestone ? { title: milestone } : null });
const snapshot = (issues = []) => ({ config: { repo: 'rancher/rancher', label: 'team/frameworks', users: ['Alice', 'BOB'], milestone: '', noMilestone: false }, generatedAt: '2026-09-22T12:00:00Z', issues });
const fixture = () => buildIssueRadarReport(snapshot([
  issue(1, ['Alice', 'developer'], ['QA/M', 'kind/bug']),
  issue(2, ['alice', 'bob'], ['QA/L', 'kind/enhancement']),
  issue(3, ['outsider'], ['QA/S']),
  issue(4, [], ['kind/bug'], ''),
  issue(5, [], ['QA/None']),
  issue(6, ['bob'], ['qa/xs', 'kind/enhancement']),
]));

test('usernames normalize case, @ prefixes, duplicate entries and whitespace', () => {
  assert.deepEqual(parseUsers('@Alice, bob\nALICE  @Bob'), ['alice', 'bob']);
});

test('assignment lanes preserve original rules and distinguish unassigned from outside-team issues', () => {
  const report = fixture();
  assert.deepEqual(report.totals, { fetched: 6, categorized: 5, qaNone: 1, exactlyOne: 2, missingOwner: 2, overAssigned: 1, unassigned: 1, outsideTeam: 1, noMilestone: 1 });
  assert.deepEqual(report.buckets.find(lane => lane.id === 'solo-alice').issues.map(issue => issue.number), [1]);
  assert.deepEqual(report.buckets.find(lane => lane.id === 'missing-owner').issues.map(issue => issue.number), [3, 4]);
  assert.equal(report.qaNone.issues[0].qaSize, 'QA/None');
  assert.equal(report.userCards[0].issueCount, 1);
  assert.equal(report.userCards[0].sharedCount, 1);
  assert.equal(report.userCards[0].totalAssigned, 2);
});

test('summary totals count every issue once, excluding QA/None and shared-owner double counting', () => {
  const report = fixture();
  const total = report.summaryRows.at(-1);
  assert.equal(total.total, 5);
  assert.deepEqual(total.counts, [1, 1, 1, 1, 0, 1]);
  assert.equal(total.bug + total.enhancement + total.other, 5);
  assert.equal(report.summaryRows.slice(0, -1).reduce((sum, row) => sum + row.total, 0), 5);
});

test('pull requests and duplicate issue numbers are excluded', () => {
  const report = buildIssueRadarReport(snapshot([issue(1), issue(1), { ...issue(2), pull_request: {} }]));
  assert.equal(report.totals.fetched, 1);
});

test('board views and searches filter cards without changing snapshot totals', () => {
  const report = fixture();
  const ids = options => visibleIssueLanes(report, options).flatMap(lane => lane.issues.map(issue => issue.number));
  assert.deepEqual(ids({ filter: 'unassigned' }), [4]);
  assert.deepEqual(ids({ filter: 'no-milestone' }), [4]);
  assert.deepEqual(ids({ filter: 'missing-size' }), [4]);
  assert.deepEqual(ids({ filter: 'problems' }), [3, 4, 2]);
  assert.deepEqual(ids({ filter: 'missing-owner' }), [3, 4]);
  assert.deepEqual(ids({ filter: 'shared' }), [2]);
  assert.deepEqual(ids({ filter: 'clean' }), [1, 6]);
  assert.deepEqual(ids({ filter: 'qa-none' }), [5]);
  assert.deepEqual(ids({ owner: 'bob' }), [2, 6]);
  assert.deepEqual(ids({ query: '#3' }), [3]);
  assert.deepEqual(ids({ query: 'OUTSIDER' }), [3]);
  assert.deepEqual(ids({ query: 'QA/M' }), [1]);
  assert.equal(report.totals.fetched, 6);
  assert.equal(visibleIssueLanes(report, { query: '#4' })[0].total, 2);
});

test('empty boards retain all selected owner lanes and reconcile zero totals', () => {
  const report = buildIssueRadarReport(snapshot());
  assert.equal(report.buckets.length, 4);
  assert.equal(report.userCards.length, 2);
  assert.equal(report.summaryRows.at(-1).total, 0);
  assert.match(buildIssueRadarBrief(report), /Fetched \| 0/);
});

test('assignment reports include every view and use the report snapshot rather than board filters', () => {
  const report = fixture();
  visibleIssueLanes(report, { query: 'not found' });
  const brief = buildIssueRadarBrief(report);
  for (const marker of ['Owner workload', 'QA sizes', 'Issue kinds', 'Assignment board', 'Truly unassigned', 'Assigned outside the team only', 'QA/None', report.generatedAt]) assert.ok(brief.includes(marker), marker);
  for (let n = 1; n <= 6; n++) assert.ok(brief.includes(`https://github.com/rancher/rancher/issues/${n}`));
  assert.doesNotMatch(brief, /Recent closed issue samples|freeze|countdown/i);
});

test('history failures remain distinguishable from owners with no closed issues', () => {
  const brief = buildIssueRadarBrief(fixture(), { limit: 30, generatedAt: 'now', history: { alice: [] }, warnings: ['History for @bob could not be loaded.'] });
  assert.match(brief, /No matching closed issues/);
  assert.match(brief, /History unavailable/);
  assert.match(brief, /History for @bob could not be loaded/);
  assert.match(brief, /across all milestones/);
});

test('untrusted titles and history cannot inject Markdown links into exported reports', () => {
  const item = { ...issue(7), title: '[look](https://evil.invalid)\n# forged header', html_url: 'javascript:alert(1)' };
  const report = buildIssueRadarReport(snapshot([item]));
  const brief = buildIssueRadarBrief(report, { limit: 30, generatedAt: 'now', warnings: [], history: { alice: [{ ...item, body: '<script>bad()</script>', labels: [] }], bob: [] } });
  assert.ok(brief.includes('https://github.com/rancher/rancher/issues/7'));
  assert.ok(brief.includes('\\[look\\]\\(https://evil.invalid\\)'));
  assert.ok(brief.includes('\\<script\\>'));
  assert.doesNotMatch(brief, /javascript:|\n# forged/);
});
