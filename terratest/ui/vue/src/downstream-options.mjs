// Mirrors rancher/dashboard shell/utils/cluster.js and shell/machine-config/linode.vue
// (reviewed 2026-10-01). Live catalogs and explicit inputs take precedence.
// Rancher normally shows the newest patch per minor. Older patches remain opt-in.
const parts = value => /^v?(\d+)\.(\d+)\.(\d+)(-[^+]+)?\+(?:k3s|rke2r)(\d+)$/.exec(value || '');
export function compareDownstreamVersions(a, b) {
 const x = parts(a), y = parts(b);
 if (!x || !y) return String(a).localeCompare(String(b), undefined, {numeric:true});
 for (const i of [1,2,3]) { if (+x[i] !== +y[i]) return +x[i] - +y[i]; }
 if (!!x[4] !== !!y[4]) return x[4] ? -1 : 1;
 return (x[4] || '').localeCompare(y[4] || '', undefined, {numeric:true}) || +x[5] - +y[5];
}
export function downstreamVersionChoices(versions, showOlder = false, selected = '') {
 const sorted = [...new Set(versions || [])].sort((a,b) => compareDownstreamVersions(b,a));
 const seen = new Set();
 return sorted.filter(value => {
  const p = parts(value);
  if (!p || p[4]) return true;
  const minor = `${p[1]}.${p[2]}`, latest = !seen.has(minor);
  seen.add(minor);
  return showOlder || latest || value === selected;
 });
}
export function generateDownstreamName(prefix, cryptoAPI = globalThis.crypto) {
 const clean = String(prefix || '').toLowerCase().replace(/[^a-z0-9-]/g, '').replace(/^[^a-z]+/, '').replace(/-+$/, '').slice(0,33).replace(/-+$/, '');
 if (!clean) throw new Error('Enter your initials or a name prefix first.');
 const bytes = cryptoAPI.getRandomValues(new Uint8Array(3));
 return `${clean}-${Array.from(bytes, byte => byte.toString(16).padStart(2,'0')).join('')}`;
}
export function linodeMachineChoices(catalog) {
 if (!catalog) return {};
 return {region:catalog.regions || [], instanceType:catalog.types || [], image:catalog.images || []};
}
export function linodeMachineDefaults(catalog) {
 const choices = linodeMachineChoices(catalog);
 const choose = (key, value) => choices[key]?.find(item => item.id === value)?.id || '';
 return {
  region:choose('region','us-west') || choices.region?.[0]?.id || '',
  instanceType:choose('instanceType','g6-standard-2') || choices.instanceType?.find(item => item.memoryMB >= 4096)?.id || choices.instanceType?.[0]?.id || '',
  image:choose('image','linode/ubuntu20.04') || choices.image?.find(item => !item.deprecated)?.id || '',
 };
}
export const machineFieldLabel = key => ({region:'Region',instanceType:'Instance size',image:'OS image',createPrivateIp:'Private networking',sshUser:'SSH user',vpcId:'VPC',subnetId:'Subnet',ami:'AMI',zone:'Availability zone',securityGroup:'Security groups'}[key] || key);
