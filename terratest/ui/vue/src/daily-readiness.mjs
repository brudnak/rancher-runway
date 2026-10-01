export function dailyBuildGroups(entries=[]){
 const groups=new Map();
 for(const entry of entries){
  if(entry.closed||entry.qaNone||entry.verdict!=='ready')continue;
  for(const build of entry.builds||[]){
   const key=[build.reference,build.serverDigest,build.agentReference,build.agentDigest].join('|');
   if(!groups.has(key))groups.set(key,{...build,issues:new Set()});
   groups.get(key).issues.add(entry.issueUrl);
  }
 }
 return [...groups.values()].map(group=>({...group,count:group.issues.size}));
}
export function dailyEntries(entries=[],filter='all'){
 return entries.filter(entry=>filter==='all'||filter==='green'&&!entry.closed&&!entry.qaNone&&entry.verdict==='ready'&&entry.workflow?.greenLight===true||filter==='ready'&&!entry.closed&&!entry.qaNone&&entry.verdict==='ready'||filter==='plans'&&!entry.closed&&!entry.qaNone&&entry.needsPlan||filter==='qa'&&!entry.closed&&!entry.qaNone&&entry.qaComplete&&entry.qaState!=='found'||filter==='unchecked'&&entry.verdict==='pending');
}
export const dailyScopeLabel=config=>config?`${config.repo} · ${config.scope==='mine'?'@'+config.user:config.scope==='unassigned'?'Unassigned':'Entire milestone'}${config.label?' · '+config.label:''}`:'';
