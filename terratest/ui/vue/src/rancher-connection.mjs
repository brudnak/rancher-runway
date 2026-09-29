// Keep connection parsing shared so discovery never silently changes a target.
export function rancherConnectionURL(raw) {
 const text=String(raw||'').trim();
 if(!text)return '';
 try {
  const url=new URL(text.includes('://')?text:`https://${text}`);
  if(!['https:','http:'].includes(url.protocol)||!url.hostname||url.username||url.password||url.search||url.hash)return '';
  return url.href.replace(/\/+$/,'');
 }catch{return '';}
}
export function rancherTestHost(raw) {
 const normalized=rancherConnectionURL(raw);if(!normalized)return '';
 const url=new URL(normalized);
 // rancher/tests takes a hostname and always constructs an HTTPS URL.
 return url.protocol==='https:'&&(!url.pathname||url.pathname==='/')?url.host:'';
}
export function rancherConnectionFingerprint(url,insecure,caPem='') {
 return JSON.stringify([rancherConnectionURL(url),insecure===true,String(caPem||'')]);
}
export function filterRancherTargets(targets,query='') {
 const search=query.trim().toLowerCase();
 return targets.filter(t=>`${t.name} ${t.url} ${t.version||''} ${t.runId||''} ${t.role||''}`.toLowerCase().includes(search));
}

// A saved credential may only be reused for its original Rancher destination.
export function rancherCredentialAvailable(token,draft,saved) {
 if(String(token||'').trim())return true;
 const url=rancherConnectionURL(draft?.url);
 return !!(url&&draft?.kind==='rancher'&&saved?.kind==='rancher'&&saved.connected&&url===rancherConnectionURL(saved.url));
}
