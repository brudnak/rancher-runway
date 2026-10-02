export const checkpointPresets=[
 {kind:'bug-complete',name:'Bug complete'},
 {kind:'feature-complete',name:'Feature complete'},
 {kind:'code-freeze',name:'Code freeze'},
 {kind:'charts-qa-sign-off',name:'Charts QA sign-off'},
 {kind:'charts-unrc',name:'Charts UnRC'},
 {kind:'charts-release',name:'Charts release'},
 {kind:'release',name:'Release'},
];
// Keep saved values when toggled off; older checkpoints remain enabled.
export function checkpointOptions(checkpoints=[],isNew=false){
 const rows=checkpoints.map(c=>({...c,disabled:!!c.disabled}));
 for(const preset of checkpointPresets){
  const existing=rows.find(c=>!c.milestone&&(c.kind===preset.kind||(!c.kind&&c.name.trim().toLowerCase()===preset.name.toLowerCase())));
  if(existing){existing.kind=preset.kind;continue;}
  rows.push({id:crypto.randomUUID(),...preset,date:'',milestone:'',tentative:false,source:'',disabled:!(isNew&&preset.kind==='bug-complete')});
 }
 return rows;
}
export const milestoneKey=config=>`${config.repo.toLowerCase()}#${config.milestone}`;
export const localDay=(now=new Date())=>`${now.getFullYear()}-${String(now.getMonth()+1).padStart(2,'0')}-${String(now.getDate()).padStart(2,'0')}`;
export function validDay(value){if(!/^\d{4}-\d{2}-\d{2}$/.test(value))return false;const day=new Date(`${value}T12:00:00Z`);return !Number.isNaN(+day)&&day.toISOString().slice(0,10)===value;}
// Exclude today, include the deadline. UTC date arithmetic avoids DST transitions.
export function workingDays(target,holidays=[],today=localDay()){
 if(!validDay(target)||!validDay(today))return null;
 const start=new Date(`${today}T12:00:00Z`),end=new Date(`${target}T12:00:00Z`),direction=end>=start?1:-1,excluded=new Set(holidays);let count=0;
 while(direction>0?start<end:start>end){start.setUTCDate(start.getUTCDate()+direction);if(![0,6].includes(start.getUTCDay())&&!excluded.has(start.toISOString().slice(0,10)))count+=direction;}
 return count;
}
export function effectiveCheckpoints(tracker,key=''){
 const checkpoints=tracker.checkpoints||[];
 if(!key)return checkpoints.filter(c=>!c.disabled).sort((a,b)=>a.date.localeCompare(b.date));
 const overrides=checkpoints.filter(c=>c.milestone===key),names=new Set(overrides.map(c=>c.name.trim().toLowerCase()));
 return [...checkpoints.filter(c=>!c.milestone&&!names.has(c.name.trim().toLowerCase())),...overrides].filter(c=>!c.disabled).sort((a,b)=>a.date.localeCompare(b.date));
}
export function trackerIssues(trackers,selected='',milestone=''){
 const seen=new Map();
 for(const tracker of trackers.filter(t=>!t.archived&&(!selected||t.id===selected)))for(const link of tracker.milestones||[]){
 const key=milestoneKey(link.config);if(milestone&&milestone!==key)continue;
 for(const issue of link.snapshot?.issues||[]){const id=issue.html_url.toLowerCase();if(!seen.has(id))seen.set(id,{...issue,trackerName:tracker.name,milestoneTitle:link.snapshot.milestone.title});}
 }return [...seen.values()];
}
const months=['jan','feb','mar','apr','may','jun','jul','aug','sep','oct','nov','dec'];
export function extractPlanDates(text){
 const lines=String(text).split(/\r?\n/).map(s=>s.trim()).filter(Boolean),out=[];
 const pattern=/\b(20\d{2}-\d{2}-\d{2}|\d{1,2}(?:st|nd|rd|th)?\s+[A-Za-z]{3,9}\s+20\d{2}|[A-Za-z]{3,9}\s+\d{1,2}(?:st|nd|rd|th)?,?\s+20\d{2})\b/g;
 for(let i=0;i<lines.length;i++)for(const match of lines[i].matchAll(pattern)){
 let date=match[0];if(!/^20\d{2}-/.test(date)){
 const parts=date.replace(/(\d)(st|nd|rd|th)/g,'$1').replace(',','').split(/\s+/),day=/^\d/.test(parts[0])?parts[0]:parts[1],month=months.indexOf((/^\d/.test(parts[0])?parts[1]:parts[0]).slice(0,3).toLowerCase());
 if(month<0)continue;date=`${parts[2]}-${String(month+1).padStart(2,'0')}-${day.padStart(2,'0')}`;
 }
 if(!validDay(date))continue;
 let name=lines[i].slice(0,match.index).replace(/[\s:|•\-–]+$/,'').trim();
 if(!name)name=(lines[i-1]||'Checkpoint').replace(/[\s:|]+$/,'');
 const source=[lines[i-1],lines[i]].filter(Boolean).join('\n').slice(0,2000);
 if(out.some(c=>c.name===name&&c.date===date))continue;
 out.push({id:`import-${out.length}`,name:name.slice(0,120),date,source,tentative:/tentative/i.test(name),milestone:'',selected:true});
 }return out;
}

export function monthName(month){
 if(!/^20\d{2}-(0[1-9]|1[0-2])$/.test(month))return '';
 return new Date(`${month}-15T12:00:00`).toLocaleDateString('en-US',{month:'long',year:'numeric'});
}
export function checkpointIdentity(name){
 const text=name.toLowerCase();
 if(/charts?.*qa.*sign/.test(text))return {kind:'charts-qa-sign-off',name:'Charts QA sign-off'};
 if(/charts?.*unrc/.test(text))return {kind:'charts-unrc',name:'Charts UnRC'};
 if(/charts?.*release/.test(text))return {kind:'charts-release',name:'Charts release'};
 if(/bug\s*complete/.test(text))return {kind:'bug-complete',name:'Bug complete'};
 if(/feature\s*complete/.test(text))return {kind:'feature-complete',name:'Feature complete'};
 if(/code\s*freeze/.test(text))return {kind:'code-freeze',name:'Code freeze'};
 if(/(?:official\s*)?rc\s*cut/.test(text))return {kind:'rc-cut',name:'RC cut'};
 if(/go\s*\/\s*no.?go/.test(text))return {kind:'go-no-go',name:'Go/No-Go call'};
 if(/^(?:tentative\s+)?release(?:\s+date)?\s*$/i.test(name.trim()))return {kind:'release',name:'Release'};
 return null;
}
export function extractReleaseMonth(text,month){
 if(!monthName(month))return {checkpoints:[],versions:[],matched:false,mode:'invalid'};
 const lines=String(text).split(/\r?\n/).map(s=>s.trim()).filter(Boolean);
 const structured=lines.some(line=>/^##?\s+/.test(line));
 const fullMonths=['january','february','march','april','may','june','july','august','september','october','november','december'];
 let year='',section='',hasSections=false,matched=false;const selected=[];
 for(const line of lines){
  const heading=line.replace(/^#+\s*/,'').trim();
  if((!structured||/^#/.test(line))&&/^20\d{2}(?:$|\s|\()/.test(heading)&&heading.length<100){year=heading.slice(0,4);section='';continue;}
  const monthIndex=fullMonths.findIndex(name=>new RegExp(`\\b${name}\\b`,'i').test(heading));
  const isHeading=monthIndex>=0&&heading.length<90&&!/\d/.test(heading.replace(/\b20\d{2}\b/g,''))&&!/\d{1,2}\s+(?:Jan|Feb|Mar|Apr|May|Jun|Jul|Aug|Sep|Oct|Nov|Dec)\b|charts?|sign.off|freeze|release date/i.test(heading)&&(!structured||/^#/.test(line));
  if(isHeading){const explicit=heading.match(/\b20\d{2}\b/)?.[0];const y=explicit||year;if(y){section=`${y}-${String(monthIndex+1).padStart(2,'0')}`;hasSections=true;if(section===month)matched=true;}continue;}
  if(section===month)selected.push(line);
 }
 const scoped=hasSections?selected.join('\n'):text;
 let lastActivity='';
 const dateText=scoped.split(/\r?\n/).map(line=>{if(checkpointIdentity(line)&&!(/20\d{2}/.test(line)))lastActivity=line;if(/^UI\s*:/i.test(line)&&lastActivity)return `${lastActivity} (UI): ${line.replace(/^UI\s*:/i,'')}`;return line;}).join('\n');
 const checkpoints=extractPlanDates(dateText).filter(c=>hasSections||c.date.startsWith(month)).flatMap(c=>{
  const identity=checkpointIdentity(c.name);if(identity&&/\(UI\)/i.test(c.name)){identity.kind+='-ui';identity.name+=' (UI)';}return identity?[{...c,...identity,source:c.source,tentative:c.tentative}]:[];
 });
 const unique=[...new Map(checkpoints.map(c=>[`${c.kind}:${c.date}`,c])).values()];
 const versions=hasSections?[...new Set([...scoped.matchAll(/\bRancher\s+(v?\d+\.\d+\.\d+)\b/gi)].map(m=>m[1]))]:[];
 return {checkpoints:unique,versions,matched:hasSections?matched:unique.length>0,mode:hasSections?'section':'dates'};
}

export const sameWorkScope=(a,b)=>!!a&&!!b&&['repo','milestone','scope','user','label'].every(key=>String(a[key]||'').toLowerCase()===String(b[key]||'').toLowerCase());
export const ownsSavedWork=(tracker,snapshot)=>!!snapshot&&(tracker.milestones||[]).some(m=>sameWorkScope(m.config,snapshot.config));
export const matchingWorkMilestone=(tracker,snapshot)=>!!snapshot&&(tracker.milestones||[]).some(m=>milestoneKey(m.config)===milestoneKey(snapshot.config));
