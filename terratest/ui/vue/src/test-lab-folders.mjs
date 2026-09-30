// Folder navigation is independent of suite/test text search. Exclude the common
// validation root so typing "v" finds vai/v2prov rather than every package.
export function testFolders(entries=[]) {
 const nodes=new Map();
 for(const entry of entries){
  const parts=String(entry.package||'').replace(/^validation\//,'').split('/').filter(Boolean);
  for(let i=0;i<parts.length;i++){
   const path=parts.slice(0,i+1).join('/');
   if(!nodes.has(path))nodes.set(path,{path,name:parts[i],depth:i,parent:parts.slice(0,i).join('/'),suites:new Set()});
   nodes.get(path).suites.add(`${entry.package}::${entry.suite}`);
  }
 }
 return [...nodes.values()].sort((a,b)=>a.path.localeCompare(b.path)).map(n=>({...n,count:n.suites.size,suites:undefined,hasChildren:[...nodes.keys()].some(p=>p.startsWith(n.path+'/'))}));
}
export function filterTestFolders(folders,query='',expanded=new Set()) {
 const words=query.trim().toLowerCase().split(/\s+/).filter(Boolean);
 if(words.length)return folders.filter(folder=>words.every(word=>folder.path.toLowerCase().split('/').some(part=>part.startsWith(word))));
 return folders.filter(folder=>!folder.parent||folder.parent.split('/').every((_,i,parts)=>expanded.has(parts.slice(0,i+1).join('/'))));
}
export function folderContainsPackage(folder,pkg) {
 const path=String(pkg||'').replace(/^validation\//,'');
 return !folder||path===folder||path.startsWith(folder+'/');
}

// These paths come from the pinned catalog rather than the local filesystem.
// Keep the validation root implicit, but preserve full paths for exact selection.
function validationPath(value) {
 const path=String(value||'');
 const parts=path.split('/');
 return parts[0]==='validation'&&parts.every(part=>part&&part!=='.'&&part!=='..')&&!/[\\\0]/.test(path)?path:'';
}
const suiteID=entry=>entry.suite?`${entry.package}::${entry.suite}`:'';

export function testExplorer(entries=[],documents=[]) {
 const root={path:'validation',children:new Map(),suiteIDs:new Set()};
 function folder(path) {
  let node=root;
  for(const name of path.split('/').slice(1)){
   if(!node.children.has(name))node.children.set(name,{kind:'folder',path:`${node.path}/${name}`,name,children:new Map(),suiteIDs:new Set()});
   node=node.children.get(name);
  }
  return node;
 }
 function addSuite(path,id) {
  if(!id)return;
  let node=root;node.suiteIDs.add(id);
  for(const name of path.split('/').slice(1)){node=node.children.get(name);node.suiteIDs.add(id);}
 }
 for(const entry of entries){
  const pkg=validationPath(entry.package),path=validationPath(entry.file),id=suiteID(entry);
  if(pkg){folder(pkg);addSuite(pkg,id);}
  if(!path||!path.endsWith('.go')||!id)continue;
  const parts=path.split('/'),name=parts.pop(),parentPath=parts.join('/'),parent=folder(parentPath);
  let file=parent.children.get(name);
  if(!file){file={kind:'test-file',path,name,suiteIDs:new Set(),entries:[]};parent.children.set(name,file);}
  if(file.kind!=='test-file')continue;
  file.suiteIDs.add(id);file.entries.push(entry);addSuite(parentPath,id);
 }
 for(const document of documents){
  const path=validationPath(document.path);
  if(!path||path==='validation')continue;
  const parts=path.split('/'),name=parts.pop(),parent=folder(parts.join('/'));
  if(!parent.children.has(name))parent.children.set(name,{kind:'readme',path,name,document,suiteIDs:new Set()});
 }
 const rows=[];
 function visit(node,parents) {
  const children=[...node.children.values()].sort((a,b)=>(a.kind!=='folder')-(b.kind!=='folder')||a.name.localeCompare(b.name));
  for(const child of children){
   const {children:descendants,suiteIDs,...metadata}=child;
   rows.push({...metadata,key:`${child.kind}:${child.path}`,depth:parents.length,parent:parents.at(-1)||'',parents:[...parents],hasChildren:Boolean(descendants?.size),count:suiteIDs.size,suiteIDs:[...suiteIDs].sort()});
   if(descendants)visit(child,[...parents,child.path]);
  }
 }
 visit(root,[]);return rows;
}

export function filterTestExplorer(nodes,query='',expanded=new Set(),kind='all') {
 const words=String(query||'').trim().toLowerCase().split(/\s+/).filter(Boolean);
 const selectedKind=({tests:'test-file',readmes:'readme'})[kind]||kind;
 const visible=new Set();
 for(const node of nodes){
  if(selectedKind!=='all'&&node.kind!==selectedKind)continue;
  const parts=node.path.replace(/^validation\//,'').toLowerCase().split('/');
  const folders=node.kind==='folder'?parts:parts.slice(0,-1),filename=node.kind==='folder'?'':parts.at(-1);
  if(words.length&&!words.every(word=>folders.some(part=>part.startsWith(word))||filename.includes(word)))continue;
  visible.add(node.path);for(const path of node.parents)visible.add(path);
 }
 return nodes.filter(node=>visible.has(node.path)&&(words.length||node.parents.every(path=>expanded.has(path))));
}
