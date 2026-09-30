const normalizedPath=value=>String(value||'').replaceAll('\\','/').replace(/^\.\//,'').replace(/^\/+|\/+$/g,'');
export const displayDocPath=path=>normalizedPath(path).replace(/^validation\//i,'');
const directory=path=>path.includes('/')?path.slice(0,path.lastIndexOf('/')):'';
const isReadme=path=>/^readme(?:\.[^/]*)?$/i.test(path.split('/').at(-1));

// Only the path and title participate. A word buried in a code example should
// not keep every folder visible while someone searches for a particular guide.
export function filterReadmes(documents,query){
 const words=String(query||'').toLowerCase().trim().split(/\s+/).filter(Boolean);
 return documents.filter(doc=>words.every(word=>`${displayDocPath(doc.path)} ${doc.title||''}`.toLowerCase().includes(word)));
}

export function resolveReadme(documents,requestedPath='',packages=[]){
 const requested=normalizedPath(requestedPath),lookup=path=>documents.find(doc=>normalizedPath(doc.path).toLowerCase()===path.toLowerCase());
 if(requested){
  const exact=lookup(requested);if(exact)return {doc:exact,note:''};
  let parent=isReadme(requested)||/\.[^/]+$/.test(requested)?directory(requested):requested;
  while(parent){
   const closest=documents.find(doc=>directory(normalizedPath(doc.path)).toLowerCase()===parent.toLowerCase()&&isReadme(doc.path));
   if(closest)return {doc:closest,note:parent===requested?'':`No README at ${displayDocPath(requested)}. Showing the closest parent guide: ${displayDocPath(closest.path)}.`};
   parent=directory(parent);
  }
  return {doc:null,note:`No README is available for ${displayDocPath(requested)} at this revision. Choose a guide from the list.`};
 }
 const related=documents.filter(doc=>packages.some(path=>path===directory(doc.path)||path.startsWith(directory(doc.path)+'/'))).sort((a,b)=>b.path.length-a.path.length||a.path.localeCompare(b.path));
 return {doc:related[0]||documents[0]||null,note:''};
}

export function readmeTree(documents){
 const root={children:new Map(),docs:[]};
 for(const doc of documents){
  const parts=displayDocPath(doc.path).split('/');parts.pop();let node=root,path='';
  for(const name of parts){path=path?`${path}/${name}`:name;if(!node.children.has(name))node.children.set(name,{name,path,children:new Map(),docs:[]});node=node.children.get(name);}
  node.docs.push(doc);
 }
 const rows=[];
 const visit=(node,depth,parents)=>{
  for(const folder of [...node.children.values()].sort((a,b)=>a.name.localeCompare(b.name))){rows.push({kind:'folder',key:`folder:${folder.path}`,path:folder.path,name:folder.name,depth,parents});visit(folder,depth+1,[...parents,folder.path]);}
  for(const doc of [...node.docs].sort((a,b)=>a.path.localeCompare(b.path)))rows.push({kind:'document',key:doc.path,path:doc.path,name:doc.path.split('/').at(-1),depth,parents,doc});
 };
 visit(root,0,[]);return rows;
}

// The renderer has already sanitized href. Only links to this pinned revision
// and repository can become an in-app navigation; other links stay external.
export function localReadmeLink(href,sha,currentPath,documents){
 try{
  const url=new URL(href,`https://github.com/rancher/tests/blob/${sha}/${currentPath}`),prefix=`/rancher/tests/blob/${sha}/`;
  if(url.origin!=='https://github.com'||!url.pathname.startsWith(prefix))return null;
  const path=decodeURIComponent(url.pathname.slice(prefix.length)),exact=documents.find(doc=>doc.path.toLowerCase()===path.toLowerCase());
  if(!exact&&!isReadme(path)&&!url.pathname.endsWith('/'))return null;
  return {path:exact?.path||path,anchor:decodeURIComponent(url.hash.slice(1)).replace(/^user-content-/,''),resolution:resolveReadme(documents,exact?.path||path)};
 }catch{return null;}
}
