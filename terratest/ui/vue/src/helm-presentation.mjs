// Runway labels complement the exact chart key, which stays visible and searchable.
const descriptions = {
  hostname: ['Rancher hostname', 'The address people will use to open Rancher. Leave out https:// and any path.', 'rancher.example.com'],
  bootstrapPassword: ['Bootstrap password', 'Leave inherited for a Rancher-generated password, or generate a strong one here.'],
  agentTLSMode: ['Agent certificate trust', 'Choose how Rancher agents verify the server certificate. Empty uses Rancher’s default.'],
  'networkExposure.type': ['Expose Rancher through', 'Choose an Ingress, a Gateway, or manage network access yourself.'],
  'ingress.tls.source': ['TLS certificate source', 'Use Rancher-generated certificates, Let’s Encrypt, or your own TLS secret.'],
  'ingress.ingressClassName': ['Ingress class', 'Leave empty to use your cluster’s default ingress class.', 'Cluster default'],
  'ingress.tls.secretName': ['TLS secret name', 'An existing TLS secret in the release namespace.'],
  'letsEncrypt.email': ['Certificate email', 'Email address for Let’s Encrypt registration and renewal notices.', 'admin@example.com'],
  'letsEncrypt.environment': ['Certificate environment', 'Use staging to test your setup before requesting a production certificate.'],
  'gateway.gatewayClass.name': ['Gateway class', 'Match a GatewayClass supported by a controller in your cluster.'],
  'gateway.gatewayClass.tls.source': ['Gateway certificate source', 'Choose how the Gateway receives its TLS certificate.'],
  'gateway.gatewayClass.tls.secretName': ['Gateway TLS secret', 'The certificate secret used by the Gateway.'],
  replicas: ['Rancher replicas', 'Choose how many Rancher server pods to run.'],
  antiAffinity: ['Pod placement', 'Prefer separate nodes, or require them for stronger separation.'],
  resources: ['CPU & memory', 'Set container resource requests and limits as YAML.'],
  'image.repository': ['Rancher image', 'The image repository for Rancher server.'],
  'image.tag': ['Image tag', 'Leave empty to use the application version from the chart.', 'Chart application version'],
  'image.pullPolicy': ['Image pull policy', 'Control when Kubernetes checks the registry for an image.'],
  rancherImage: ['Rancher image', 'The image repository for Rancher server.'],
  rancherImageTag: ['Image tag', 'Leave inherited to use the chart’s Rancher image version.'],
  rancherImagePullPolicy: ['Image pull policy', 'Control when Kubernetes checks the registry for an image.'],
  privateCA: ['Private certificate authority', 'Enable when your Rancher certificate is signed by a private CA.'],
  additionalTrustedCAs: ['Additional trusted CAs', 'Trust certificates from the tls-ca-additional secret.'],
  debug: ['Debug logging', 'Enable additional Rancher server diagnostic output.'],
  'auditLog.enabled': ['API audit logging', 'Record requests to the Rancher API.'],
  'auditLog.level': ['Audit detail', 'Higher levels record more request and response detail.'],
  'auditLog.destination': ['Audit destination', 'Send audit records to a sidecar container or a host path.'],
};
export function fieldPresentation(field) {
  const [label, hint, placeholder] = descriptions[field.path] || [];
  const words = field.path.split('.').map(part => part.replace(/([a-z0-9])([A-Z])/g, '$1 $2').replace(/[_-]/g, ' '));
  const fallback = words.join(' · ');
  return { label: label || fallback.charAt(0).toUpperCase() + fallback.slice(1), hint: hint || (field.description?.length <= 190 ? field.description : ''), placeholder: placeholder || '', documentation: field.description && field.description !== hint && (hint || field.description.length > 190) ? field.description : '' };
}
export function matchingFields(fields, overrides, { search = '', editedOnly = false, section = 'Access' } = {}) {
  const words = search.trim().toLowerCase().split(/\s+/).filter(Boolean);
  return fields.filter(field => {
    if (editedOnly && !Object.hasOwn(overrides, field.path)) return false;
    if (words.length) {
      const presentation = fieldPresentation(field);
      const haystack = `${field.path} ${field.group} ${presentation.label} ${presentation.hint} ${field.description || ''}`.toLowerCase();
      return words.every(word => haystack.includes(word));
    }
    return editedOnly || field.group === section;
  });
}
export function releaseIssues(metadata, fieldErrors, findings, unavailable) {
  const labels = {release:'Release name',namespace:'Namespace',context:'Kube context',repo:'Repository alias',action:'Release behavior',delivery:'Values delivery',timeout:'Readiness timeout'};
  const issues = Object.entries(metadata).map(([key,detail])=>({id:`config-${key}`,title:labels[key],detail,configKey:key,section:['repo','delivery','timeout'].includes(key)?'review':'target'}));
  for (const [field, detail] of Object.entries(fieldErrors)) issues.push({id:`value-${field}`,title:`Check ${field}`,detail,field});
  for (const finding of findings.filter(item=>item.level==='error')) {
    if (!finding.field || !issues.some(item=>item.field===finding.field)) issues.push(finding);
  }
  if (unavailable.length) issues.push({id:'unavailable',title:'Review unsupported overrides',detail:`These settings are absent from this chart: ${unavailable.join(', ')}.`,section:'unavailable'});
  return issues;
}
