export const COST_SERVICES=[{key:'ec2',label:'EC2 compute',color:'#72c4b0'},{key:'ebs',label:'EBS storage',color:'#799fe1'},{key:'rds',label:'RDS / Aurora',color:'#af95dc'},{key:'lb',label:'Load balancers',color:'#d9ae64'}];
export const money=value=>new Intl.NumberFormat('en-US',{style:'currency',currency:'USD',minimumFractionDigits:2,maximumFractionDigits:2}).format(Number(value)||0);
const dateKey=date=>`${date.getFullYear()}-${String(date.getMonth()+1).padStart(2,'0')}-${String(date.getDate()).padStart(2,'0')}`;
const dayNumber=key=>Date.parse(key+'T12:00:00Z')/86400000;
export function costChartModel(daily=[],range='30',region='',now=new Date()){
 const today=dateKey(now);const startDate=new Date(now.getFullYear(),now.getMonth(),now.getDate());
 if(range!=='all')startDate.setDate(startDate.getDate()-(Number(range)||30)+1);
 const start=range==='all'?'':dateKey(startDate);
 const matching=daily.filter(d=>/^\d{4}-\d{2}-\d{2}$/.test(d.date)&&d.date>=start&&d.date<=today&&(!region||d.region===region));
 const summary={total:0,records:0,partial:0,imported:0,ec2:0,ebs:0,rds:0,lb:0};
 for(const d of matching)for(const key of Object.keys(summary))summary[key]+=Number(d[key])||0;
 const first=start||matching.reduce((min,d)=>d.date<min?d.date:min,today);
 const span=dayNumber(today)-dayNumber(first)+1;
 const grain=span>3650?'year':span>120?'month':'day';
 const keyFor=key=>grain==='year'?key.slice(0,4):grain==='month'?key.slice(0,7):key;
 const points=[];const pointMap=new Map();
 if(matching.length){
  const cursor=new Date(first+'T12:00:00Z');const end=new Date(today+'T12:00:00Z');
  if(grain==='month')cursor.setUTCDate(1);if(grain==='year'){cursor.setUTCMonth(0);cursor.setUTCDate(1);}
  while(cursor<=end){const key=keyFor(cursor.toISOString().slice(0,10));const p={key,total:0,records:0,partial:0,imported:0,ec2:0,ebs:0,rds:0,lb:0};points.push(p);pointMap.set(key,p);if(grain==='day')cursor.setUTCDate(cursor.getUTCDate()+1);else if(grain==='month')cursor.setUTCMonth(cursor.getUTCMonth()+1);else cursor.setUTCFullYear(cursor.getUTCFullYear()+1);}
  for(const d of matching){const p=pointMap.get(keyFor(d.date));if(p)for(const key of Object.keys(summary))p[key]+=Number(d[key])||0;}
 }
 let cumulative=0;for(const p of points){cumulative+=p.total;p.cumulative=cumulative;}
 return {points,summary,grain,start:first,end:today};
}
export function costPeriodLabel(key){const date=new Date(key+(key.length===4?'-01-01':key.length===7?'-01':'')+'T12:00:00');return key.length===4?key:date.toLocaleDateString(undefined,key.length===7?{month:'short',year:'numeric'}:{month:'short',day:'numeric',year:'numeric'});}
export function filterCostEntries(entries,range,region,query,now=new Date()){
 const start=new Date(now.getFullYear(),now.getMonth(),now.getDate());if(range!=='all')start.setDate(start.getDate()-(Number(range)||30)+1);
 const words=query.trim().toLowerCase().split(/\s+/).filter(Boolean);
 return entries.filter(e=>{const time=new Date(e.finishedAt);return (range==='all'||time>=start)&&(!region||e.region===region)&&words.every(w=>`${e.runId} ${e.owner||''} ${e.awsPrefix||''} ${e.region}`.toLowerCase().includes(w));});
}
export const awsResourceKey=item=>JSON.stringify([item.type,item.id]);
export function filterAWSResources(items,{query='',type='',status='all'}={}){
 const words=query.trim().toLowerCase().split(/\s+/).filter(Boolean);
 return items.filter(item=>(!type||item.type===type)&&(status==='all'||(status==='candidates'?item.cleanupEligible:!item.cleanupEligible))&&words.every(word=>`${item.name} ${item.id} ${item.type} ${item.region} ${item.runId||''} ${item.owner||''} ${item.details||''} ${Object.entries(item.tags||{}).flat().join(' ')}`.toLowerCase().includes(word)));
}
export function toggleVisibleCandidates(selected,visible,checked){const keys=new Set(selected);for(const item of visible.filter(item=>item.cleanupEligible)){if(checked)keys.add(awsResourceKey(item));else keys.delete(awsResourceKey(item));}return [...keys];}
