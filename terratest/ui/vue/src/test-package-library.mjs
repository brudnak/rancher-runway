export function libraryGroups(library,packages){
 const known=new Map(packages.map(p=>[p.id,p])),seen=new Set();
 const rows=(ids=[])=>ids.flatMap(id=>{if(!known.has(id)||seen.has(id))return [];seen.add(id);return [known.get(id)];});
 const groups=(library?.buckets||[]).map(b=>({...b,packages:rows(b.packageIds)}));
 const unfiled=rows(library?.unfiled);unfiled.push(...packages.filter(p=>!seen.has(p.id)));
 return [...groups,{id:'unfiled',name:'Unfiled',packages:unfiled}];
}
export function moveLibraryPackage(library,id,bucketId,index){
 const next=structuredClone(library);next.unfiled=next.unfiled.filter(p=>p!==id);
 for(const b of next.buckets)b.packageIds=b.packageIds.filter(p=>p!==id);
 const destination=bucketId==='unfiled'?next.unfiled:next.buckets.find(b=>b.id===bucketId)?.packageIds;
 if(!destination)throw new Error('Choose an existing milestone bucket.');
 destination.splice(index==null?destination.length:Math.max(0,Math.min(index,destination.length)),0,id);return next;
}
export function reorderLibraryBucket(library,id,delta){const next=structuredClone(library),index=next.buckets.findIndex(b=>b.id===id),target=index+delta;if(index>=0&&target>=0&&target<next.buckets.length){const [bucket]=next.buckets.splice(index,1);next.buckets.splice(target,0,bucket)}return next;}
export function deleteLibraryBucket(library,id){
 const next=structuredClone(library),index=next.buckets.findIndex(b=>b.id===id);
 if(id==='unfiled'||index<0)throw new Error('Choose an existing milestone bucket.');
 const [bucket]=next.buckets.splice(index,1);next.unfiled.push(...bucket.packageIds);return next;
}
