import {markdownBlocks,safeDocURL} from './test-lab-workspace.mjs';
const escape=value=>String(value).replaceAll('&','&amp;').replaceAll('<','&lt;').replaceAll('>','&gt;').replaceAll('"','&quot;').replaceAll("'",'&#39;');
const decode=value=>String(value).replace(/&(lt|gt|amp|quot|#39);/g,(_,entity)=>({lt:'<',gt:'>',amp:'&',quot:'"','#39':"'"}[entity]));
const unescape=value=>String(value).replace(/\\([\\`*_{}\[\]()#+.!|>-])/g,'$1');
// Protect escaped pipes while the shared block reader identifies table cells.
export const packageMarkdownBlocks=text=>markdownBlocks(String(text).replaceAll('\\|','\uE000'));
// Render generated Markdown literally where its author escaped punctuation.
// Raw HTML and remote images stay inert, including after entity decoding.
export function packageMarkdownInline(text){
 const source=decode(String(text).replaceAll('\uE000','|'));
 const pattern=/\\([\\`*_{}\[\]()#+.!|>-])|(`+)([^`\n]+)\2|!?\[([^\]\n]+)\]\(([^\s)]+)\)|\*\*([^*\n]+)\*\*|__([^_\n]+)__|\*([^*\n]+)\*|https?:\/\/[^\s<>]+/g;
 let out='',last=0;
 for(const match of source.matchAll(pattern)){
  out+=escape(source.slice(last,match.index));
  if(match[1])out+=escape(match[1]);
  else if(match[2])out+=`<code>${escape(match[3])}</code>`;
  else if(match[4]){const href=safeDocURL(unescape(match[5]),'0'.repeat(40),'');out+=href?`<a href="${escape(href)}" target="_blank" rel="noreferrer noopener">${escape(unescape(match[4]))}</a>`:escape(unescape(match[4]));}
  else if(match[6]||match[7])out+=`<strong>${escape(unescape(match[6]||match[7]))}</strong>`;
  else if(match[8])out+=`<em>${escape(unescape(match[8]))}</em>`;
  else{const url=unescape(match[0]),href=safeDocURL(url,'0'.repeat(40),'');out+=href?`<a href="${escape(href)}" target="_blank" rel="noreferrer noopener">${escape(url)}</a>`:escape(url);}
  last=match.index+match[0].length;
 }
 return out+escape(source.slice(last));
}
