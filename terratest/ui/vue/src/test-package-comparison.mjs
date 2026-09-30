// Compare preserved records by identity and content; an outcome change is not a claim of causation.
const text = value => String(value ?? '');
const array = value => Array.isArray(value) ? value : [];
export const outcomeLabel = value => ({'not-run':'Not run',passed:'Passed',failed:'Failed',blocked:'Blocked',skipped:'Skipped'}[value] || 'Not run');
export const resultForCase = (session, id) => array(session?.results).find(item => item.caseId === id);
export const evidenceForCase = (session, id) => array(session?.evidence).filter(item => item.caseId === id);

export function comparableCase(item) {
  if (!item) return null;
  return {
    title:text(item.title), preconditions:text(item.preconditions), expected:text(item.expected),
    automation:text(item.automation), automationUrl:text(item.automationUrl),
    selection:array(item.selection),
    steps:array(item.steps).map(step => ({id:text(step.id),instruction:text(step.instruction),expected:text(step.expected)})),
  };
}

export function changedCaseFields(before, after) {
  if (!before || !after) return [];
  const left=comparableCase(before), right=comparableCase(after);
  const labels={title:'Title',preconditions:'Preconditions',expected:'Expected behavior',automation:'Automation status',automationUrl:'Automation reference',selection:'Automated selection',steps:'Steps'};
  return Object.keys(labels).filter(key => JSON.stringify(left[key]) !== JSON.stringify(right[key])).map(key => labels[key]);
}

export function compareSessionCases(baseline, candidate) {
  const before=new Map(array(baseline?.cases).map(item => [item.id,item]));
  const after=new Map(array(candidate?.cases).map(item => [item.id,item]));
  const ids=[...after.keys(),...before.keys()].filter((id,index,all) => all.indexOf(id)===index);
  return ids.map(id => {
    const baselineCase=before.get(id), candidateCase=after.get(id);
    const changes=changedCaseFields(baselineCase,candidateCase);
    const comparability=!baselineCase?'added':!candidateCase?'missing':changes.length?'changed':'same';
    const baselineResult=resultForCase(baseline,id), candidateResult=resultForCase(candidate,id);
    return {id, title:candidateCase?.title || baselineCase?.title || 'Untitled case',baselineCase,candidateCase,changes,comparability,
      baselineResult,candidateResult,
      baselineOutcome:baselineCase ? baselineResult?.outcome || 'not-run' : null,
      candidateOutcome:candidateCase ? candidateResult?.outcome || 'not-run' : null,
      baselineEvidence:evidenceForCase(baseline,id),candidateEvidence:evidenceForCase(candidate,id)};
  });
}

export function comparisonSummary(rows) {
  return {
    same:rows.filter(row => row.comparability==='same').length,
    changed:rows.filter(row => row.comparability==='changed').length,
    added:rows.filter(row => row.comparability==='added').length,
    missing:rows.filter(row => row.comparability==='missing').length,
    notRun:rows.filter(row => row.candidateOutcome==='not-run').length,
    blocked:rows.filter(row => row.candidateOutcome==='blocked').length,
    skipped:rows.filter(row => row.candidateOutcome==='skipped').length,
    outcomeChanges:rows.filter(row => row.comparability==='same' && row.baselineOutcome!=='not-run' && row.candidateOutcome!=='not-run' && row.baselineOutcome!==row.candidateOutcome).length,
  };
}

// Unknown values remain unknown even when both records omit the same field.
export function compareEnvironments(baseline={}, candidate={}) {
  const rows=[];
  const add=(key,label,left,right) => {
    const before=text(left), after=text(right);
    rows.push({key,label,before,after,status:!before || !after ? 'unknown' : before===after ? 'same' : 'changed'});
  };
  add('rancher','Rancher',baseline.rancherVersion,candidate.rancherVersion);
  add('kubernetes','Kubernetes',baseline.kubernetesVersion,candidate.kubernetesVersion);
  add('cluster','Cluster',baseline.clusterName || baseline.clusterId,candidate.clusterName || candidate.clusterId);
  if (baseline.url || candidate.url) add('url','Rancher URL',baseline.url,candidate.url);
  const details = env => {
    const grouped=new Map();
    for(const detail of array(env.details)) {
      const key=text(detail.label).trim().toLowerCase();
      const entry=grouped.get(key) || {label:detail.label,values:[]};
      entry.values.push(text(detail.value));grouped.set(key,entry);
    }
    return grouped;
  };
  const beforeDetails=details(baseline),afterDetails=details(candidate);
  for(const key of new Set([...beforeDetails.keys(),...afterDetails.keys()])) {
    const left=beforeDetails.get(key),right=afterDetails.get(key);
    add(`detail:${key}`,right?.label || left?.label,left?.values.slice().sort().join('\n'),right?.values.slice().sort().join('\n'));
  }
  if(array(baseline.images).length || array(candidate.images).length) add('images','Images & digests',[...new Set(array(baseline.images))].sort().join('\n'),[...new Set(array(candidate.images))].sort().join('\n'));
  if(baseline.helmCommand || candidate.helmCommand) add('helm','Helm command',baseline.helmCommand,candidate.helmCommand);
  if(baseline.configuration || candidate.configuration) add('configuration','Configuration & prerequisites',baseline.configuration,candidate.configuration);
  return rows;
}

export function nextCaseToCheck(session, currentId) {
  const cases=array(session?.cases);
  if(cases.length<2) return null;
  const current=cases.findIndex(item => item.id===currentId);
  const ordered=[...cases.slice(current+1),...cases.slice(0,current<0?0:current)].filter(item => item.id!==currentId);
  for(const outcome of ['not-run','blocked']) {
    const found=ordered.find(item => (resultForCase(session,item.id)?.outcome || 'not-run')===outcome);
    if(found) return {id:found.id,title:found.title,blocked:outcome==='blocked'};
  }
  return null;
}

export function caseStepProgress(item, result) {
  const steps=array(item?.steps),markers=new Map(array(result?.steps).map(marker=>[marker.stepId,marker]));
  return {total:steps.length,done:steps.filter(step=>markers.get(step.id)?.done===true).length};
}
