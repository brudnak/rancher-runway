export const homeLayouts = [
  {id:'continue',title:'Continue working',description:'Pick up recent issue packages and return to your last workspace. Your readiness briefing stays close by.',icon:'undo'},
  {id:'ready',title:'Ready to test',description:'Lead with verified fixes, workflow status, and issues that need test plans. Recent work is one step away.',icon:'check'},
];
export const normalizeHomeLayout = value => homeLayouts.some(item=>item.id===value)?value:'continue';
export function homeBriefing(report,snapshot){
 if(!report)return {available:false,reason:'Scan your milestone to see what’s ready.',entries:[],green:0,builds:0};
 if(!snapshot||['repo','milestone','scope','user','label'].some(key=>report.config?.[key]!==snapshot.config?.[key]))return {available:false,reason:'Your milestone scope has changed. Scan it for an updated briefing.',entries:[],green:0,builds:0};
 const current=new Map((snapshot.issues||[]).map(issue=>[String(issue.html_url).toLowerCase(),issue]));
 const entries=(report.entries||[]).filter(entry=>{
  const issue=current.get(String(entry.issueUrl).toLowerCase());
  return issue&&issue.state!=='closed'&&!entry.closed&&!entry.qaNone&&!(issue.labels||[]).some(label=>label.name?.trim().toLowerCase()==='qa/none');
 }).map(entry=>({...entry,changed:Date.parse(current.get(String(entry.issueUrl).toLowerCase()).updated_at)>Date.parse(report.startedAt)}));
 const proven=entries.filter(entry=>!entry.changed&&entry.verdict==='ready');
 return {available:true,entries,green:proven.filter(entry=>entry.workflow?.greenLight===true).length,builds:proven.length,reason:''};
}
