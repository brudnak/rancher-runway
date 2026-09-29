import {parseDocument} from 'yaml';
export const CONFIG_LIMIT=128*1024;
export function inspectConfig(text){
 if(new TextEncoder().encode(text).length>CONFIG_LIMIT)return {valid:false,message:'Configuration exceeds 128 KiB.'};
 try{const doc=parseDocument(text,{prettyErrors:false,uniqueKeys:true});if(doc.errors.length){const error=doc.errors[0];const line=text.slice(0,error.pos?.[0]||0).split('\n').length;return {valid:false,line,message:`YAML needs attention at line ${line} (${error.code||'syntax'}).`};}const value=doc.toJS({maxAliasCount:100});if(!value||typeof value!=='object'||Array.isArray(value))return {valid:false,message:'Use a single YAML mapping.'};return {valid:true,value,doc,message:'Valid YAML'};}catch{return {valid:false,message:'YAML could not be read. Check aliases and document structure.'};}
}
export function setRancherField(text,key,value){const result=inspectConfig(text);if(!result.valid)throw new Error(result.message);result.doc.setIn(['rancher',key],value);return String(result.doc);}
export function indentSelection(text,start,end,outdent=false){
 const begin=text.lastIndexOf('\n',start-1)+1;
 if(start===end&&!outdent)return {text:text.slice(0,start)+'  '+text.slice(end),start:start+2,end:start+2};
 const stop=end>start&&text[end-1]==='\n'?end-1:end;
 const selected=text.slice(begin,stop);let removedFirst=0,total=0;
 const replacement=selected.split('\n').map((line,i)=>{const n=outdent?Math.min(2,line.match(/^ */)[0].length):0;if(i===0)removedFirst=n;total+=outdent?-n:2;return outdent?line.slice(n):'  '+line;}).join('\n');
 return {text:text.slice(0,begin)+replacement+text.slice(stop),start:Math.max(begin,start+(outdent?-removedFirst:2)),end:Math.max(begin,end+total)};
}
export function safeDocURL(href,sha,path){
 try{if(!/^[a-f0-9]{40}$/.test(sha)||/[\u0000-\u0020\\]/.test(href))return '';const url=new URL(href,`https://github.com/rancher/tests/blob/${sha}/${path}`);if(!['https:','http:'].includes(url.protocol)||url.username||url.password)return '';return url.href;}catch{return '';}
}
const escape=value=>String(value).replaceAll('&','&amp;').replaceAll('<','&lt;').replaceAll('>','&gt;').replaceAll('"','&quot;').replaceAll("'",'&#39;');
// Small, deliberately inert Markdown renderer: raw HTML stays text and images
// stay links. No remote assets load just because a README was opened.
export function markdownInline(text,sha,path){
 const pattern=/(`+)([^`\n]+)\1|!?\[([^\]\n]+)\]\(([^\s)]+)\)|\*\*([^*\n]+)\*\*|__([^_\n]+)__|\*([^*\n]+)\*|https?:\/\/[^\s<>]+/g;
 let out='',last=0;for(const m of text.matchAll(pattern)){out+=escape(text.slice(last,m.index));if(m[1])out+=`<code>${escape(m[2])}</code>`;else if(m[3]){const url=safeDocURL(m[4],sha,path);out+=url?`<a href="${escape(url)}" target="_blank" rel="noreferrer noopener">${escape(m[3])}</a>`:escape(m[3]);}else if(m[5]||m[6])out+=`<strong>${escape(m[5]||m[6])}</strong>`;else if(m[7])out+=`<em>${escape(m[7])}</em>`;else{const url=safeDocURL(m[0],sha,path);out+=url?`<a href="${escape(url)}" target="_blank" rel="noreferrer noopener">${escape(m[0])}</a>`:escape(m[0]);}last=m.index+m[0].length;}return out+escape(text.slice(last));
}
export function markdownBlocks(text){
 const lines=text.replaceAll('\r\n','\n').split('\n'),blocks=[];
 const special=line=>/^\s*$|^\s*(```|~~~)|^#{1,6}\s|^\s*([-*+] |\d+\. )|^>\s?|^\s*([-*_])(?:\s*\1){2,}\s*$/.test(line);
 for(let i=0;i<lines.length;){const line=lines[i];if(!line.trim()){i++;continue;}const fence=line.match(/^\s*(`{3,}|~{3,})(.*)$/);if(fence){const code=[];i++;while(i<lines.length&&!lines[i].trimStart().startsWith(fence[1]))code.push(lines[i++]);i++;blocks.push({type:'code',language:fence[2].trim(),text:code.join('\n')});continue;}
 const heading=line.match(/^(#{1,6})\s+(.+?)\s*#*$/);if(heading){blocks.push({type:'heading',level:heading[1].length,text:heading[2],id:heading[2].toLowerCase().replace(/[^\p{L}\p{N}\s-]/gu,'').replaceAll(' ','-')});i++;continue;}
 if(/^\s*([-*_])(?:\s*\1){2,}\s*$/.test(line)){blocks.push({type:'rule'});i++;continue;}
 if(line.includes('|')&&/^\s*\|?\s*:?-{3,}/.test(lines[i+1]||'')){const cells=v=>v.trim().replace(/^\||\|$/g,'').split('|').map(s=>s.trim());const header=cells(line),rows=[];i+=2;while(i<lines.length&&lines[i].includes('|')&&lines[i].trim())rows.push(cells(lines[i++]));blocks.push({type:'table',header,rows});continue;}
 if(/^\s*(?:[-*+] |\d+\. )/.test(line)){const ordered=/^\s*\d+\./.test(line),items=[];while(i<lines.length&&/^\s*(?:[-*+] |\d+\. )/.test(lines[i]))items.push(lines[i++].replace(/^\s*(?:[-*+] |\d+\. )/,''));blocks.push({type:'list',ordered,items});continue;}
 if(/^>/.test(line)){const quote=[];while(i<lines.length&&/^>/.test(lines[i]))quote.push(lines[i++].replace(/^>\s?/,''));blocks.push({type:'quote',text:quote.join(' ')});continue;}
 const paragraph=[line];i++;while(i<lines.length&&!special(lines[i])){if(lines[i].includes('|')&&/^\s*\|?\s*:?-{3,}/.test(lines[i+1]||''))break;paragraph.push(lines[i++]);}blocks.push({type:'paragraph',text:paragraph.join('\n')});
 }return blocks;
}
export function relatedReadmes(documents,packages){return documents.filter(doc=>{const dir=doc.path.slice(0,doc.path.lastIndexOf('/'));return packages.some(p=>p===dir||p.startsWith(dir+'/'));});}
