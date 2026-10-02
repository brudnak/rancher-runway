// Confluence's Word export is MIME-wrapped HTML, not a binary Word document.
// Decode text only: no HTML is inserted into the page and no resources are loaded.
export function confluencePlanText(raw){
 let html=raw;
 if(/MIME-Version:/i.test(raw.slice(0,2000))){
  const boundary=raw.match(/boundary\s*=\s*"?([^"\r\n;]+)/i)?.[1];
  if(!boundary)throw new Error('This Word export has no readable MIME boundary. Try exporting it again.');
  const part=raw.split(`--${boundary}`).find(p=>/^\s*Content-Type:\s*text\/html/im.test(p));
  if(!part)throw new Error('This Word export contains no HTML planner.');
  const split=part.search(/\r?\n\r?\n/),headers=part.slice(0,split);
  html=part.slice(split).replace(/^\s*\r?\n/,'').trim();
  if(/Content-Transfer-Encoding:\s*quoted-printable/i.test(headers)){
   const bytes=[];const data=html.replace(/=\r?\n/g,'');
   for(let i=0;i<data.length;i++){if(data[i]==='='&&/^[0-9a-f]{2}$/i.test(data.slice(i+1,i+3))){bytes.push(parseInt(data.slice(i+1,i+3),16));i+=2;}else bytes.push(data.charCodeAt(i));}
   html=new TextDecoder(headers.match(/charset\s*=\s*"?([^"\s;]+)/i)?.[1]||'utf-8').decode(new Uint8Array(bytes));
  }else if(/Content-Transfer-Encoding:\s*base64/i.test(headers))html=new TextDecoder().decode(Uint8Array.from(atob(html.replace(/\s/g,'')),c=>c.charCodeAt(0)));
 }
 if(!/<(?:html|body|table)\b/i.test(html))throw new Error('This is a binary .doc file. Use a Confluence Word export, DOCX, or PDF instead.');
 const text=html.replace(/<(script|style)\b[^>]*>[\s\S]*?<\/\1>/gi,'').replace(/<!--[\s\S]*?-->/g,'')
  .replace(/<h[1-6]\b[^>]*>/gi,'\n## ').replace(/<\/(?:h[1-6]|p|div|tr|td|th|li|ul|ol)>|<br\b[^>]*>/gi,'\n')
  .replace(/<[^>]*>/g,'').replace(/&(#x[0-9a-f]+|#\d+|nbsp|amp|lt|gt|quot|apos);/gi,(_,entity)=>{
   if(entity[0]==='#'){const n=entity[1].toLowerCase()==='x'?parseInt(entity.slice(2),16):Number(entity.slice(1));return n>0&&n<=0x10ffff?String.fromCodePoint(n):'';}
   return {nbsp:' ',amp:'&',lt:'<',gt:'>',quot:'"',apos:"'"}[entity.toLowerCase()];
  });
 return text.split(/\r?\n/).map(line=>line.trim()).filter(Boolean).join('\n');
}
