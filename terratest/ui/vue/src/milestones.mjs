const versionKey=value=>String(value||'').trim().toLowerCase().replace(/^v(?=\d+\.\d)/,'');
export function filterMilestones(items,query=''){
 const needle=versionKey(query);
 return items.filter(item=>versionKey(item.title).includes(needle)||String(item.number)===needle.replace(/^#/,''));
}
export function resolveMilestone(items,value){
 const text=String(value||'').trim();
 if(!text)return null;
 const unique=[...new Map(items.map(item=>[item.number,item])).values()];
 const exact=unique.filter(item=>item.title===text);
 if(exact.length===1)return exact[0];
 const matches=/^#?[1-9]\d*$/.test(text)?unique.filter(item=>String(item.number)===text.replace(/^#/,'')):unique.filter(item=>versionKey(item.title)===versionKey(text));
 return matches.length===1?matches[0]:null;
}
