import { parseFieldValue } from './helmlab.mjs';

export const releaseChannels = {
  prime: [{ id: 'ga', label: 'Release' }, { id: 'rc', label: 'Release candidate' }, { id: 'alpha', label: 'Alpha' }, { id: 'head', label: 'Development head' }],
  community: [{ id: 'ga', label: 'Release' }, { id: 'rc', label: 'Release candidate' }, { id: 'alpha', label: 'Alpha' }],
};

export function effectiveValue(fields, overrides, path) {
  const field = fields.find(item => item.path === path);
  if (!field) return undefined;
  try { return Object.hasOwn(overrides, path) ? parseFieldValue(field, overrides[path]) : field.value; }
  catch { return undefined; }
}

export function planFindings(config, fields, overrides, env) {
  const get = path => effectiveValue(fields, overrides, path);
  const findings = [];
  const host = get('hostname');
  if (!host && config.action === 'install') findings.push({ id: 'hostname', level: 'error', title: 'Give Rancher an address', detail: 'Set the hostname people will use to reach this release.', field: 'hostname' });
  else if (host && (typeof host !== 'string' || host.length > 253 || !host.split('.').every(part => /^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$/i.test(part)))) findings.push({ id: 'hostname', level: 'error', title: 'Use a DNS hostname', detail: 'Remove the protocol, port, spaces, or path.', field: 'hostname' });
  if (env.some(item => (item.name || item.value) && !/^[A-Za-z_][A-Za-z0-9_]*$/.test(item.name))) findings.push({ id: 'env', level: 'error', title: 'Name each variable', detail: 'Use letters, numbers, and underscores; begin with a letter or underscore.', section: 'environment' });
  const names = env.filter(item => item.name).map(item => item.name);
  if (new Set(names).size !== names.length) findings.push({ id: 'env-duplicate', level: 'error', title: 'Remove duplicate variables', detail: 'Each environment variable needs a unique name.', section: 'environment' });
  if (!config.context.trim()) findings.push({ id: 'context', level: 'note', title: 'Current terminal context', detail: 'The command will use the kube context active in your terminal.', section: 'target' });
  const gateway = get('networkExposure.type') === 'gateway';
  const tlsPath = gateway ? 'gateway.gatewayClass.tls' : 'ingress.tls';
  const source = get(`${tlsPath}.source`);
  const ingress = get('ingress.enabled') !== false && get('tls') !== 'external' && !['gateway', 'none'].includes(get('networkExposure.type'));
  if ((ingress || gateway) && ['rancher', 'letsEncrypt'].includes(source)) findings.push({ id: 'certs', level: 'note', title: 'cert-manager required', detail: 'Install cert-manager in the target cluster before applying this certificate configuration.', field: `${tlsPath}.source` });
  if ((ingress || gateway) && source === 'letsEncrypt' && !get('letsEncrypt.email') && config.action === 'install') findings.push({ id: 'email', level: 'error', title: 'Add your certificate email', detail: 'Let’s Encrypt needs an email for certificate registration.', field: 'letsEncrypt.email' });
  if ((ingress || gateway) && source === 'secret') findings.push({ id: 'secret', level: 'note', title: 'Bring your TLS secret', detail: `Create ${get(`${tlsPath}.secretName`) || 'tls-rancher-ingress'} in ${config.namespace || 'the release namespace'} before applying.`, field: `${tlsPath}.secretName` });
  if (ingress && get('service.disableHTTP') && get('ingress.servicePort') !== 443) findings.push({ id: 'https-port', level: 'error', title: 'Match the HTTPS service port', detail: 'Set ingress.servicePort to 443 when service.disableHTTP is enabled.', field: 'ingress.servicePort' });
  if (gateway) findings.push({ id: 'gateway', level: 'note', title: 'Gateway controller required', detail: 'The target cluster needs Gateway API CRDs and a controller for the chosen GatewayClass.', field: 'gateway.gatewayClass.name' });
  if (get('replicas') === 1) findings.push({ id: 'replicas', level: 'note', title: 'One Rancher replica', detail: 'A compact test footprint. Use multiple replicas when availability matters.', field: 'replicas' });
  return findings;
}

export function suggestedFields(fields, get) {
  const gateway = get('networkExposure.type') === 'gateway';
  const tlsPath = gateway ? 'gateway.gatewayClass.tls' : 'ingress.tls';
  const important = new Set(['hostname', 'bootstrapPassword', 'agentTLSMode', 'networkExposure.type', `${tlsPath}.source`,
    ...(get(`${tlsPath}.source`) === 'secret' ? [`${tlsPath}.secretName`, 'privateCA'] : []),
    ...(get(`${tlsPath}.source`) === 'letsEncrypt' ? ['letsEncrypt.email', 'letsEncrypt.environment'] : []),
    ...(gateway ? ['gateway.gatewayClass.name'] : ['ingress.ingressClassName']),
    'replicas', 'antiAffinity', 'resources', 'image.repository', 'image.tag', 'image.pullPolicy',
    'rancherImage', 'rancherImageTag', 'rancherImagePullPolicy', 'debug', 'auditLog.enabled', 'auditLog.level', 'auditLog.destination']);
  return fields.filter(field => important.has(field.path) || field.group === 'Advanced');
}

// Bounded, memory-only history: secret-bearing edits never enter a URL or storage.
export function createEditHistory(initial, limit = 60) {
  let entries = [JSON.stringify(initial)], cursor = 0, lastAt = 0, lastGroup = '';
  return {
    record(value, group = '', now = Date.now()) {
      const next = JSON.stringify(value);
      if (next === entries[cursor]) return;
      const coalesce = group && group === lastGroup && now - lastAt < 600 && cursor === entries.length - 1 && cursor > 0;
      entries = entries.slice(0, cursor + 1);
      if (coalesce) entries[cursor] = next;
      else { entries.push(next); cursor++; }
      if (entries.length > limit) { entries.shift(); cursor--; }
      lastAt = now; lastGroup = group;
    },
    undo() { lastGroup = ''; if (cursor > 0) cursor--; return JSON.parse(entries[cursor]); },
    redo() { lastGroup = ''; if (cursor < entries.length - 1) cursor++; return JSON.parse(entries[cursor]); },
    get canUndo() { return cursor > 0; },
    get canRedo() { return cursor < entries.length - 1; },
  };
}

export function editGroup(previous, next) {
  const changed = [];
  for (const section of ['config', 'overrides']) {
    for (const key of new Set([...Object.keys(previous[section]), ...Object.keys(next[section])])) {
      if (previous[section][key] !== next[section][key]) changed.push(`${section}.${key}`);
    }
  }
  if (JSON.stringify(previous.env) !== JSON.stringify(next.env)) changed.push('environment');
  return changed.length === 1 ? changed[0] : '';
}

export function randomBootstrapPassword(cryptoProvider = globalThis.crypto) {
  const bytes = new Uint8Array(24);
  cryptoProvider.getRandomValues(bytes);
  // Hex encoding keeps entropy uniform and avoids shell-special characters.
  return Array.from(bytes, byte => byte.toString(16).padStart(2, '0')).join('');
}
