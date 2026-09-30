// Pure presentation helpers. Preserved session records are never rewritten here.
export const packageCopy = value => JSON.parse(JSON.stringify(value));
export const PACKAGE_STATUSES = [
  {id:'planning',label:'Planning'},
  {id:'reproducing',label:'Reproducing'},
  {id:'awaiting-fix',label:'Waiting for fix'},
  {id:'validating',label:'Validating'},
  {id:'verified',label:'Verified'},
  {id:'archived',label:'Archived'},
];
export const CASE_OUTCOMES = [
  {id:'not-run',label:'Not run'},
  {id:'passed',label:'Passed'},
  {id:'failed',label:'Failed'},
  {id:'blocked',label:'Blocked'},
  {id:'skipped',label:'Skipped'},
];
export function packageStatus(value){return PACKAGE_STATUSES.find(item=>item.id===value)?.label||String(value||'Planning').replaceAll('-',' ');}
export function filterPackages(packages,query='',status=''){
  const words=query.toLowerCase().trim().split(/\s+/).filter(Boolean);
  return packages.filter(item=>(!status||item.status===status)&&words.every(word=>[item.title,item.description,item.issueUrl,item.issueTitle,item.fixUrl,item.fixTitle,item.issue?.url,item.issue?.title,...(item.tags||[])].filter(Boolean).join(' ').toLowerCase().includes(word)));
}
export function packageDate(value,full=false){if(!value||String(value).startsWith('0001-01-01'))return 'Not recorded';const date=new Date(value);if(Number.isNaN(date.getTime()))return 'Not recorded';return date.toLocaleString(undefined,full?{dateStyle:'medium',timeStyle:'short'}:{month:'short',day:'numeric'});}
export function packageBytes(bytes){const amount=Number(bytes)||0;return amount<1024?`${amount} B`:amount<1048576?`${(amount/1024).toFixed(1)} KiB`:`${(amount/1048576).toFixed(1)} MiB`;}
export function changedPlan(saved,draft){return JSON.stringify(saved)!==JSON.stringify(draft);}
export function safePackageURL(value){try{const url=new URL(value);return url.protocol==='https:'&&!url.username&&!url.password?url.href:'';}catch{return '';}}
export function sessionProgress(results=[]){const rows=Array.isArray(results)?results:Object.values(results||{});return {total:rows.length,complete:rows.filter(item=>item.outcome&&item.outcome!=='not-run'&&item.outcome!=='pending').length,passed:rows.filter(item=>item.outcome==='passed').length,failed:rows.filter(item=>item.outcome==='failed').length,blocked:rows.filter(item=>item.outcome==='blocked').length,skipped:rows.filter(item=>item.outcome==='skipped').length};}
export function packagePlanProblem(plan){
  if(!String(plan?.title||'').trim())return {message:'Give this package a name.'};
  if((plan.cases||[]).length>250)return {message:'A package can contain up to 250 cases. Split this plan into another package.'};
  for(const [index,item] of (plan.cases||[]).entries()){
    if(!String(item.title||'').trim())return {caseId:item.id,message:`Give case ${index+1} a name before saving.`};
    if((item.steps||[]).length>100)return {caseId:item.id,message:`“${item.title}” has more than 100 steps. Split it into smaller cases.`};
    for(const [stepIndex,step] of (item.steps||[]).entries())if(!String(step.instruction||'').trim())return {caseId:item.id,message:`Add an action for step ${stepIndex+1} in “${item.title}”, or remove the empty step.`};
  }
  return null;
}
export function newPackageRecordID(){return Array.from(crypto.getRandomValues(new Uint8Array(12)),byte=>byte.toString(16).padStart(2,'0')).join('');}

// The journey is derived from saved cases, preserved sessions, and the fix link
// every time it is shown. It mirrors testPackageJourneyFor in Go: advisory only,
// never a status change, a finding, or a passing case.
export const JOURNEY_STATE_LABELS={pending:'Not started',active:'In progress',done:'Done',attention:'Needs attention'};
const JOURNEY_FINDINGS={reproduced:'Issue reproduced','not-reproduced':'Issue not reproduced',validated:'Fix validated','not-validated':'Fix not validated'};
function latestJourneySession(sessions,purpose){
  const rows=(Array.isArray(sessions)?sessions:[]).filter(item=>item?.purpose===purpose);
  rows.sort((a,b)=>{const activeA=a.status==='active',activeB=b.status==='active';if(activeA!==activeB)return activeA?-1:1;return (new Date(b.startedAt).getTime()||0)-(new Date(a.startedAt).getTime()||0);});
  return rows[0]||null;
}
function journeySessionStage(id,label,sessions,positive){
  const stage={id,label,state:'pending',detail:'Not started',sessionId:''};
  const session=latestJourneySession(sessions,id);
  if(!session)return stage;
  stage.sessionId=session.id||'';
  let name=String(session.name||'').trim();
  if(session.fixCommit)name+=` · commit ${String(session.fixCommit).slice(0,10)}`;
  if(session.status==='active'){stage.state='active';stage.detail=`In progress · ${name}`;return stage;}
  stage.detail=`${JOURNEY_FINDINGS[session.finding]||'Inconclusive'} · ${name}`;
  if(session.baseline)stage.detail+=` · against baseline ${String(session.baseline.name||'').trim()}`;
  stage.state=session.finding===positive?'done':'attention';
  return stage;
}
export function packageJourney(pkg={}){
  const cases=Array.isArray(pkg.cases)?pkg.cases:[];
  const plan={id:'plan',label:'Test plan',state:cases.length?'done':'pending',detail:cases.length?`${cases.length} saved case${cases.length===1?'':'s'}`:'No cases saved yet'};
  const reproduction=journeySessionStage('reproduction','Reproduction',pkg.sessions,'reproduced');
  const fixUrl=String(pkg.fixUrl||'').trim();
  const fix={id:'fix',label:'Fix',state:fixUrl?'done':'pending',detail:fixUrl?(String(pkg.fixTitle||'').trim()||fixUrl):'No fix linked'};
  const validation=journeySessionStage('validation','Validation',pkg.sessions,'validated');
  const latestValidation=latestJourneySession(pkg.sessions,'validation');
  const canonicalFix=value=>pullRequestReference(value)?.url.toLowerCase()||String(value||'').trim();
  if(validation.state==='done'&&fixUrl&&canonicalFix(fixUrl)!==canonicalFix(latestValidation?.fixUrl)){
    validation.state='attention';
    validation.detail=`Current fix has not been validated · ${validation.detail}`;
  }
  let next,action;
  if(plan.state!=='done'){next='Write and save the first case so a session has something to preserve.';action='plan';}
  else if(reproduction.state==='active'){next='Finish the active reproduction session with an explicit finding.';action='finish-reproduction';}
  else if(validation.state==='active'){next='Finish the active validation session with an explicit finding.';action='finish-validation';}
  else if(validation.state==='done'){next='Share the report. Marking the package Verified stays your decision.';action='share';}
  else if(reproduction.state==='pending'){next='Start a reproduction session to preserve the original behavior before any fix.';action='start-reproduction';}
  else if(reproduction.state==='attention'){next='Reproduction was not established. Review the cases or record another attempt before validating.';action='start-reproduction';}
  else if(fix.state!=='done'){next='Link the fix pull request so validation records what it tested.';action='link-fix';}
  else if(validation.state==='pending'){next='Validate the fix against the preserved reproduction baseline.';action='start-validation';}
  else{next='The fix was not validated. Record what remains open or validate another candidate.';action='start-validation';}
  return {stages:[plan,reproduction,fix,validation],next,action};
}

// Mirrors parseTestPackagePullRequest in Go so the UI only offers a lookup for
// links the backend will accept. The lookup itself is an explicit action.
const PULL_REQUEST_PATTERN=/^https:\/\/github\.com\/([A-Za-z0-9](?:[A-Za-z0-9-]{0,38})?)\/([A-Za-z0-9_.-]{1,100})\/pull\/([1-9][0-9]{0,9})(?:\/(?:files|commits|checks)?)?$/;
export function pullRequestReference(value){
  const match=PULL_REQUEST_PATTERN.exec(String(value||'').trim());
  if(!match)return null;
  const [,owner,repo,number]=match;
  return {owner,repo,number:Number(number),repository:`${owner}/${repo}`,short:`${owner}/${repo}#${number}`,url:`https://github.com/${owner}/${repo}/pull/${number}`};
}
export function fixLookupState(lookup){
  if(!lookup)return '';
  if(lookup.state==='merged')return `Merged ${packageDate(lookup.mergedAt,true)}`;
  if(lookup.state==='open')return lookup.draft?'Open draft pull request':'Open pull request';
  return 'Closed without merging';
}
