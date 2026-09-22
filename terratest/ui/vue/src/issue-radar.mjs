// Assignment categories count selected QA owners, not every GitHub assignee.
export const QA_LABELS = ['QA/XS', 'QA/S', 'QA/M', 'QA/L', 'QA/XL', 'Lacks QA Size'];
export const parseUsers = value => [...new Set(String(value).split(/[\s,]+/).map(user => user.trim().replace(/^@/, '').toLowerCase()).filter(Boolean))];
export const milestoneLabel = config => config.noMilestone ? 'No milestone' : config.milestone || 'All milestones';
const bucket = (id, title, kind) => ({ id, title, kind, issues: [] });
const kindCounts = issues => ({
  bug: issues.filter(issue => issue.kind === 'Bug').length,
  enhancement: issues.filter(issue => issue.kind === 'Enhancement').length,
  other: issues.filter(issue => issue.kind === 'Other').length,
});

export function buildIssueRadarReport(snapshot) {
  const config = { ...snapshot.config, users: parseUsers(snapshot.config.users.join(',')) };
  const seen = new Set();
  const issues = snapshot.issues.filter(issue => {
    if (issue.pull_request || seen.has(issue.number)) return false;
    seen.add(issue.number);
    return true;
  }).map(issue => {
    const labels = (issue.labels || []).map(label => label.name);
    const has = name => labels.some(label => label.toLowerCase() === name.toLowerCase());
    const assignees = [...new Set((issue.assignees || []).map(user => user.login.toLowerCase()))];
    const matchedUsers = config.users.filter(user => assignees.includes(user));
    return {
      number: issue.number, title: issue.title,
      url: `https://github.com/${config.repo}/issues/${issue.number}`,
      assignees, matchedUsers, labels, milestone: issue.milestone?.title || '',
      updatedAt: issue.updated_at,
      qaNone: has('QA/None'),
      qaSize: has('QA/None') ? 'QA/None' : QA_LABELS.find(has) || 'Lacks QA Size',
      kind: has('kind/bug') ? 'Bug' : has('kind/enhancement') ? 'Enhancement' : 'Other',
    };
  }).sort((a, b) => a.number - b.number);
  const owners = config.users.map(user => bucket(`solo-${user}`, `@${user}`, 'clean'));
  const missing = bucket('missing-owner', 'Needs a team owner', 'problem');
  const shared = bucket('over-assigned', 'Multiple team owners', 'problem');
  const none = bucket('qa-none', 'QA/None', 'qa-none');
  for (const issue of issues) {
    const target = issue.qaNone ? none : !issue.matchedUsers.length ? missing
      : issue.matchedUsers.length > 1 ? shared : owners.find(lane => lane.id === `solo-${issue.matchedUsers[0]}`);
    target.issues.push(issue);
  }
  const buckets = [...owners, shared, missing];
  const counted = issues.filter(issue => !issue.qaNone);
  const userCards = owners.map(lane => {
    const user = lane.title.slice(1);
    const sharedCount = shared.issues.filter(issue => issue.matchedUsers.includes(user)).length;
    return { user, issueCount: lane.issues.length, sharedCount,
      totalAssigned: lane.issues.length + sharedCount, ...kindCounts(lane.issues),
      lacksQaSize: lane.issues.filter(issue => issue.qaSize === 'Lacks QA Size').length };
  });
  const summaryRows = buckets.map(lane => ({ label: lane.title, total: lane.issues.length,
    counts: QA_LABELS.map(label => lane.issues.filter(issue => issue.qaSize === label).length), ...kindCounts(lane.issues) }));
  summaryRows.push({ label: 'Total in scope', total: counted.length,
    counts: QA_LABELS.map(label => counted.filter(issue => issue.qaSize === label).length), ...kindCounts(counted) });
  return {
    generatedAt: snapshot.generatedAt, config, issues, buckets, qaNone: none, userCards, summaryRows,
    totals: {
      fetched: issues.length, categorized: counted.length, qaNone: none.issues.length,
      exactlyOne: owners.reduce((sum, lane) => sum + lane.issues.length, 0),
      missingOwner: missing.issues.length, overAssigned: shared.issues.length,
      unassigned: counted.filter(issue => !issue.assignees.length).length,
      outsideTeam: missing.issues.filter(issue => issue.assignees.length).length,
      noMilestone: counted.filter(issue => !issue.milestone).length,
    },
  };
}

export function visibleIssueLanes(report, { filter = 'all', query = '', owner = '' } = {}) {
  const needle = query.trim().toLowerCase().replace(/^#(?=\d)/, '');
  const matches = issue => {
    if (owner && !issue.assignees.includes(owner)) return false;
    if (needle && ![issue.number, issue.title, issue.milestone, ...issue.labels, ...issue.assignees].join(' ').toLowerCase().includes(needle)) return false;
    if (filter === 'unassigned') return !issue.qaNone && !issue.assignees.length;
    if (filter === 'no-milestone') return !issue.qaNone && !issue.milestone;
    if (filter === 'missing-size') return !issue.qaNone && issue.qaSize === 'Lacks QA Size';
    return true;
  };
  const missing = report.buckets.find(lane => lane.id === 'missing-owner');
  const shared = report.buckets.find(lane => lane.id === 'over-assigned');
  const lanes = [missing, shared, ...report.buckets.filter(lane => lane.kind === 'clean'), report.qaNone];
  return lanes.filter(lane => filter === 'problems' ? lane.kind === 'problem'
    : filter === 'missing-owner' ? lane.id === 'missing-owner' : filter === 'shared' ? lane.id === 'over-assigned'
    : filter === 'clean' ? lane.kind === 'clean' : filter === 'qa-none' ? lane.kind === 'qa-none' : true)
    .map(lane => ({ ...lane, total: lane.issues.length, issues: lane.issues.filter(matches) }));
}

const text = value => String(value ?? '').replace(/\s+/g, ' ').trim();
const md = value => text(value).replace(/[\\`*_{}\[\]<>()#!|]/g, '\\$&');
const table = (headers, rows) => [
  `| ${headers.map(md).join(' | ')} |`, `| ${headers.map(() => '---').join(' | ')} |`,
  ...rows.map(row => `| ${row.map(md).join(' | ')} |`), '',
];

export function buildIssueRadarBrief(report, historyResult = null) {
  const lines = ['# Issue Radar · Assignment review', '',
    `Repository: ${md(report.config.repo)}`, `Milestone: ${md(milestoneLabel(report.config))}`,
    `Team labels (all required): ${md(report.config.label)}`, `Selected owners: ${report.config.users.map(user => `@${user}`).join(', ')}`,
    `Snapshot: ${report.generatedAt}`, '',
    'Assignment rule: each non-QA/None issue should have exactly one selected team owner. Other GitHub assignees may also be present.', '',
    '## Snapshot', '',
    ...table(['Measure', 'Issues'], [
      ['Fetched', report.totals.fetched], ['In QA scope', report.totals.categorized],
      ['Exactly one team owner', report.totals.exactlyOne], ['Needs a team owner', report.totals.missingOwner],
      ['Truly unassigned', report.totals.unassigned], ['Assigned outside the team only', report.totals.outsideTeam],
      ['Multiple team owners', report.totals.overAssigned], ['No milestone (in QA scope)', report.totals.noMilestone], ['QA/None', report.totals.qaNone],
    ]), '## Owner workload', '',
    ...table(['Owner', 'Single owner', 'Shared', 'Bugs', 'Enhancements', 'Other', 'Missing QA size'], report.userCards.map(row =>
      [row.user, row.issueCount, row.sharedCount, row.bug, row.enhancement, row.other, row.lacksQaSize])),
    'Kind and QA-size counts in owner rows cover single-owner issues. Shared issues are counted once in the summaries below.', '',
    '## QA sizes', '', ...table(['Assignment', ...QA_LABELS, 'Total'], report.summaryRows.map(row => [row.label, ...row.counts, row.total])),
    '## Issue kinds', '', ...table(['Assignment', 'Bugs', 'Enhancements', 'Other', 'Total'], report.summaryRows.map(row => [row.label, row.bug, row.enhancement, row.other, row.total])),
    '## Assignment board', '',
  ];
  for (const lane of visibleIssueLanes(report)) {
    lines.push(`### ${md(lane.title)} (${lane.total})`, '');
    if (!lane.issues.length) lines.push('None.', '');
    for (const issue of lane.issues) {
      lines.push(`- [#${issue.number}](${issue.url}) ${md(issue.title)}`,
        `  - Assignees: ${md(issue.assignees.join(', ') || 'Unassigned')}`,
        `  - Milestone: ${md(issue.milestone || 'None')} · ${md(issue.qaSize)} · ${md(issue.kind)}`);
    }
    lines.push('');
  }
  if (historyResult) {
    lines.push('## Recent closed issue samples', '',
      `Up to ${historyResult.limit} recently updated closed issues per owner, with the same repository and team labels, across all milestones.`,
      `History fetched: ${historyResult.generatedAt}`, '');
    for (const warning of historyResult.warnings || []) lines.push(`Note: ${md(warning)}`, '');
    for (const user of report.config.users) {
      const history = historyResult.history[user];
      lines.push(`### @${user}`, '');
      if (!history) lines.push('History unavailable. See the warning above.', '');
      else if (!history.length) lines.push('No matching closed issues.', '');
      else for (const issue of history) {
        lines.push(`- [#${issue.number}](https://github.com/${report.config.repo}/issues/${issue.number}) ${md(issue.title)}`,
          `  - Closed: ${md(issue.closed_at || 'Unknown')} · Labels: ${md((issue.labels || []).map(label => label.name).join(', ') || 'None')}`);
        if (issue.body) lines.push(`  - Context: ${md(issue.body).slice(0, 320)}`);
      }
      lines.push('');
    }
  }
  return lines.join('\n');
}
