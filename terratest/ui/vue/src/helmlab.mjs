import { parse, stringify } from 'yaml';

export const initialConfig = () => ({
  distribution: 'prime', type: 'ga', version: '', repo: 'runway-prime-ga',
  release: 'rancher', namespace: 'cattle-system', action: 'install', delivery: 'file',
  context: '', wait: true, timeout: '10m', dryRun: false, envExplicit: false,
});
export const initialOverrides = () => ({});
export const quote = value => "'" + String(value).replaceAll("'", "'\\''") + "'";
const forbidden = new Set(['__proto__', 'constructor', 'prototype']);
const plainObject = value => value !== null && typeof value === 'object' && !Array.isArray(value);
export const sensitivePath = path => /password|token|privatekey|apikey|regcode|registration\.certificate|^proxy$/i.test(path);
export const cleanDescription = text => String(text || '')
  .replace(/\[([^\]]+)\]\([^)]+\)/g, '$1')
  .replace(/[*`]/g, '')
  .replace(/^(?:string|bool|int|map|list)\s*-\s*/i, '')
  .trim();
function assertSafeValue(value, depth = 0) {
  if (depth > 30) throw new Error('YAML nesting exceeds 30 levels.');
  if (value && typeof value === 'object') {
    for (const [key, child] of Object.entries(value)) {
      if (forbidden.has(key)) throw new Error(`Unsupported key: ${key}`);
      assertSafeValue(child, depth + 1);
    }
  }
}

export const fieldGroups = [
  { id: 'Access', hint: 'Hostname, ingress and certificates', icon: 'globe' },
  { id: 'Workload', hint: 'Replicas, scheduling and images', icon: 'layers' },
  { id: 'Observability', hint: 'Audit trails, probes and debugging', icon: 'pulse' },
  { id: 'Advanced', hint: 'Registry, integrations and chart internals', icon: 'sliders' },
];
export function fieldGroup(path) {
  if (/^(hostname|bootstrapPassword|agentTLSMode|privateCA|additionalTrustedCAs|tls|ingress|gateway|networkExposure|letsEncrypt|certmanager|service)(\.|$)/.test(path)) return 'Access';
  if (/^(replicas|image|rancherImage|rancherImageTag|rancherImagePullPolicy|resources|antiAffinity|topologyKey|nodeSelector|extraNodeSelectorTerms|extraTolerations|tolerations|affinity|podDisruptionBudget|priorityClassName|imagePullSecrets)(\.|$)/.test(path)) return 'Workload';
  if (/^(auditLog|debug|livenessProbe|readinessProbe|startupProbe)(\.|$)/.test(path)) return 'Observability';
  return 'Advanced';
}
export function fieldsFor(chart) {
  const fields = [];
  const choices = {
    'ingress.tls.source': ['rancher', 'letsEncrypt', 'secret'],
    'letsEncrypt.environment': ['production', 'staging'],
    tls: ['ingress', 'external'], antiAffinity: ['preferred', 'required'],
    'image.pullPolicy': ['Always', 'IfNotPresent', 'Never'], rancherImagePullPolicy: ['Always', 'IfNotPresent', 'Never'],
  };
  const dictionary = /(?:annotations|labels|resources|nodeSelector|extraTolerations|extraNodeSelectorTerms)$/i;
  function walk(value, schema = {}, parts = []) {
    schema ||= {};
    if (parts.some(key => forbidden.has(key))) return;
    const path = parts.join('.');
    const schemaType = (Array.isArray(schema.type) ? schema.type.find(type => type !== 'null') : schema.type);
    const keys = new Set([...Object.keys(plainObject(value) ? value : {}), ...Object.keys(schema.properties || {})]);
    if (keys.size && !dictionary.test(path) && !schema.additionalProperties && (plainObject(value) || schemaType === 'object')) {
      for (const key of keys) walk(value?.[key], schema.properties?.[key] || {}, [...parts, key]);
      return;
    }
    if (!parts.length || path === 'extraEnv') return;
    const kind = schemaType || (value === null || value === undefined ? 'string' : typeof value);
    const type = ['object', 'array'].includes(kind) || (value !== null && typeof value === 'object') ? 'yaml' : kind === 'integer' ? 'number' : kind;
    fields.push({ path, parts, value, type, group: fieldGroup(path),
      choices: (schema.enum || choices[path])?.filter(item => item !== null), minimum: schema.minimum, maximum: schema.maximum,
      integer: schemaType === 'integer' || Number.isInteger(value),
      nullable: value === null || schema.type?.includes?.('null'),
      list: schemaType === 'array' || Array.isArray(value),
      description: cleanDescription(chart.descriptions?.[path] || schema.description),
    });
  }
  walk(chart.values, chart.schema);
  // These Rancher inputs are intentionally commented out in some chart defaults.
  const optional = {
    hostname: 'The DNS name you will use to open Rancher. Enter a hostname without https:// or a path.',
    bootstrapPassword: 'Optional initial administrator password. Leave inherited to let Rancher generate it.',
  };
  if (chart.values?.letsEncrypt) optional['letsEncrypt.email'] = 'Email address for ACME certificate registration and renewal notices.';
  for (const [path, description] of Object.entries(optional)) {
    if (!fields.some(field => field.path === path)) fields.push({ path, parts: path.split('.'), type: 'string', value: undefined, description, group: fieldGroup(path) });
  }
  const priority = ['hostname', 'bootstrapPassword', 'replicas', 'image.repository', 'image.tag'];
  return fields.sort((a, b) => fieldGroups.findIndex(group => group.id === a.group) - fieldGroups.findIndex(group => group.id === b.group)
    || (priority.includes(a.path) ? priority.indexOf(a.path) - 10 : 0) - (priority.includes(b.path) ? priority.indexOf(b.path) - 10 : 0)
    || a.path.localeCompare(b.path));
}

export const displayValue = field => field.type === 'yaml' ? stringify(field.value ?? (field.list ? [] : {})).trim() : String(field.value ?? '');
export function parseFieldValue(field, raw) {
  if (raw === null && field.nullable) return null;
  if (typeof raw !== 'string') throw new Error('Enter a text value.');
  if (raw.includes('\0')) throw new Error('Values cannot contain a NUL character.');
  if (raw.length > 1024 * 1024) throw new Error('Keep each value smaller than 1 MB.');
  let value = raw;
  if (field.type === 'boolean') {
    if (!['true', 'false'].includes(raw)) throw new Error('Choose true or false.');
    value = raw === 'true';
  }
  if (field.type === 'number') {
    if (!raw.trim() || !Number.isFinite(Number(raw))) throw new Error('Value must be a number.');
    value = Number(raw);
    if (field.integer && !Number.isSafeInteger(value)) throw new Error('Enter a whole number.');
    if (field.minimum !== undefined && value < field.minimum) throw new Error(`Minimum is ${field.minimum}.`);
    if (field.maximum !== undefined && value > field.maximum) throw new Error(`Maximum is ${field.maximum}.`);
  }
  if (field.choices && !field.choices.includes(value)) throw new Error('Choose one of the listed values.');
  if (field.type === 'yaml') {
    value = parse(raw, { maxAliasCount: 50 });
    assertSafeValue(value);
    if (!value || typeof value !== 'object' || Array.isArray(value) !== (field.list ?? Array.isArray(field.value))) {
      throw new Error(`Enter a YAML ${(field.list ?? Array.isArray(field.value)) ? 'list' : 'map'}.`);
    }
  }
  return value;
}
export function changedField(field, overrides) {
  if (!Object.hasOwn(overrides, field.path)) return false;
  if (field.value !== undefined && overrides[field.path] === displayValue(field)) return false;
  try { return JSON.stringify(parseFieldValue(field, overrides[field.path])) !== JSON.stringify(field.value); }
  catch { return true; }
}
export function fieldErrors(fields, overrides) {
  return Object.fromEntries(fields.filter(field => Object.hasOwn(overrides, field.path)).flatMap(field => {
    try { parseFieldValue(field, overrides[field.path]); return []; }
    catch (error) { return [[field.path, error.message]]; }
  }));
}
function put(target, parts, value) {
  if (parts.some(key => forbidden.has(key))) throw new Error('Unsupported value path: ' + parts.join('.'));
  let node = target;
  for (const key of parts.slice(0, -1)) node = node[key] ||= {};
  node[parts.at(-1)] = value;
}
const helmPath = field => (field.parts || field.path.split('.')).map(part => part.replace(/[\\.,[\]]/g, '\\$&')).join('.');
export function releaseInputErrors(config) {
  const errors = {};
  if (!/^[a-z0-9](?:[a-z0-9-]*[a-z0-9])?(?:\.[a-z0-9](?:[a-z0-9-]*[a-z0-9])?)*$/.test(config.release || '') || config.release.length > 53) errors.release = 'Release name must be a lowercase DNS name, at most 53 characters.';
  if (!/^[a-z0-9](?:[a-z0-9-]*[a-z0-9])?$/.test(config.namespace || '') || config.namespace.length > 63) errors.namespace = 'Namespace must be a lowercase DNS label, at most 63 characters.';
  if (!/^[A-Za-z0-9][A-Za-z0-9_-]*$/.test(config.repo || '')) errors.repo = 'Repo name may contain letters, numbers, hyphens and underscores.';
  if (!['install', 'upgrade', 'upgrade-reuse'].includes(config.action)) errors.action = 'Select an install or upgrade strategy.';
  if (!['set', 'file'].includes(config.delivery)) errors.delivery = 'Select a values delivery mode.';
  if (/[\x00-\x1f\x7f]/.test(config.context || '')) errors.context = 'Kube context cannot contain control characters.';
  if (config.wait && !/^(?:\d+(?:\.\d+)?(?:ms|s|m|h))+$/.test(config.timeout || '')) errors.timeout = 'Enter a timeout such as 5m or 1m30s.';
  return errors;
}
export function buildOutput(config, channel, version, fields, overrides, env, chartName = 'rancher') {
  const settingsError = Object.values(releaseInputErrors(config))[0];
  if (settingsError) throw new Error(settingsError);
  if (!/^[0-9]+\.[0-9]+\.[0-9]+(?:-[0-9A-Za-z.-]+)?(?:\+[0-9A-Za-z.-]+)?$/.test(version || '')) throw new Error('Select an exact chart version.');
  if (!/^https:\/\//.test(channel?.repo || '') || !/^[a-z0-9][a-z0-9-]*$/.test(chartName)) throw new Error('Select a valid chart repository.');
  const unknown = Object.keys(overrides).filter(path => !fields.some(field => field.path === path));
  if (unknown.length) throw new Error(`These overrides are not available in this chart: ${unknown.join(', ')}. Remove them or select another chart.`);
  const all = {}, file = {}, flags = [];
  // An explicit value equal to a chart default must still override installed values.
  const changed = fields.filter(field => Object.hasOwn(overrides, field.path));
  const flag = (path, value) => {
    const escaped = String(value).replace(/[\\,{}]/g, '\\$&');
    flags.push(`${typeof value === 'string' ? '--set-string' : '--set'} ${quote(path + '=' + escaped)}`);
  };
  for (const field of changed) {
    let value;
    try { value = parseFieldValue(field, overrides[field.path]); }
    catch (error) { throw new Error(`${field.path}: ${error.message}`); }
    put(all, field.parts || field.path.split('.'), value);
    if (config.delivery === 'file' || field.type === 'yaml' || value === null) put(file, field.parts || field.path.split('.'), value);
    else flag(helmPath(field), value);
  }
  const entries = env.filter(entry => entry.name || entry.value).map(({ name, value }) => ({ name, value }));
  if (entries.some(entry => typeof entry.value !== 'string' || entry.value.includes('\0'))) throw new Error('Environment values must be strings without NUL characters.');
  if (entries.some(entry => !/^[A-Za-z_][A-Za-z0-9_]*$/.test(entry.name))) throw new Error('Environment variables need valid names.');
  if (new Set(entries.map(entry => entry.name)).size !== entries.length) throw new Error('Environment variable names must be unique.');
  if (entries.length || config.envExplicit) {
    all.extraEnv = entries;
    if (config.delivery === 'file' || !entries.length) file.extraEnv = entries;
    else entries.forEach((entry, index) => { flag(`extraEnv[${index}].name`, entry.name); flag(`extraEnv[${index}].value`, entry.value); });
  }
  const command = [
    `helm upgrade ${config.action === 'install' ? '--install ' : ''}${quote(config.release)} ${quote(config.repo + '/' + chartName)}`,
    `--namespace ${quote(config.namespace)}`,
    config.action === 'install' ? '--create-namespace' : config.action === 'upgrade' ? '--reset-then-reuse-values' : '--reuse-values',
    `--version ${quote(version)}`,
  ];
  if (channel.devel) command.push('--devel');
  if (config.context?.trim()) command.push(`--kube-context ${quote(config.context.trim())}`);
  if (config.wait) command.push('--wait', `--timeout ${quote(config.timeout)}`);
  if (config.dryRun) command.push('--dry-run', '--hide-secret');
  const needsFile = Object.keys(file).length > 0;
  if (needsFile) command.push('--values values.yaml');
  command.push(...flags);
  return {
    repo: `helm repo add ${quote(config.repo)} ${quote(channel.repo)}\nhelm repo update ${quote(config.repo)}`,
    command: command.join(' \\\n  '), yaml: stringify(file), allYaml: stringify(all),
    commandParts: command, needsFile, changeCount: changed.length + (entries.length ? 1 : 0),
  };
}

export function importValues(text, fields) {
  if (new TextEncoder().encode(text).length > 1024 * 1024) throw new Error('Values YAML must be smaller than 1 MB.');
  const values = parse(text, { maxAliasCount: 50 });
  assertSafeValue(values);
  if (!plainObject(values)) throw new Error('The values file must contain a YAML mapping.');
  const overrides = {}, unknown = [];
  function visit(value, parts = []) {
    const path = parts.join('.');
    if (parts.some(key => forbidden.has(key))) throw new Error(`Unsupported key: ${path}`);
    if (path === 'extraEnv') return;
    const field = fields.find(item => item.path === path);
    if (field) {
      if (field.type !== 'yaml' && value !== null && typeof value !== field.type) throw new Error(`${path}: expected ${field.type}.`);
      if (value === null && !field.nullable) throw new Error(`${path}: null is not supported for this setting.`);
      const raw = value === null ? null : field.type === 'yaml' ? stringify(value).trim() : String(value);
      parseFieldValue(field, raw);
      overrides[path] = raw;
    } else if (plainObject(value) && Object.keys(value).length) {
      for (const [key, child] of Object.entries(value)) visit(child, [...parts, key]);
    } else if (parts.length) unknown.push(path);
  }
  visit(values);
  if (unknown.length) throw new Error(`Unknown chart values: ${unknown.join(', ')}.`);
  const env = values.extraEnv ?? [];
  if (!Array.isArray(env) || env.some(item => !plainObject(item) || typeof item.name !== 'string' || typeof item.value !== 'string' || Object.keys(item).some(key => !['name', 'value'].includes(key)))) {
    throw new Error('extraEnv must be a list of name/value string pairs.');
  }
  if (env.some(item => !/^[A-Za-z_][A-Za-z0-9_]*$/.test(item.name)) || new Set(env.map(item => item.name)).size !== env.length) {
    throw new Error('Environment variables need valid, unique names.');
  }
  return { overrides, env: env.map(({ name, value }) => ({ name, value })), envExplicit: Object.hasOwn(values, 'extraEnv') };
}

// A single copyable setup script keeps its companion YAML in a private temp directory.
export function buildSetupScript(output) {
  if (!output?.command) return '';
  const lines = ['#!/usr/bin/env sh', 'set -eu', 'umask 077', '', '# Register the chart repository', output.repo, ''];
  if (output.needsFile) {
    let delimiter = 'HELM_LAB_VALUES';
    while (output.yaml.split('\n').includes(delimiter)) delimiter += '_END';
    lines.push(
      '# Write the companion values to a temporary file',
      'helmlab_values_dir=$(mktemp -d)',
      `trap 'rm -rf "$helmlab_values_dir"' 0`,
      `trap 'exit 130' INT`,
      `trap 'exit 143' TERM`,
      `cat > "$helmlab_values_dir/values.yaml" <<'${delimiter}'`,
      output.yaml.trimEnd(), delimiter, '',
    );
  }
  lines.push('# Apply the release plan', output.needsFile ? output.commandParts.map(part => part === '--values values.yaml' ? '--values "$helmlab_values_dir/values.yaml"' : part).join(' \\\n  ') : output.command, '');
  return lines.join('\n');
}

export function filterVersions(versions, query) {
  const words = query.trim().toLowerCase().split(/\s+/).filter(Boolean);
  return versions.filter(item => words.every(word => `${item.version} ${item.appVersion || ''}`.toLowerCase().includes(word)));
}

// Return text tokens, never HTML, so chart values cannot introduce markup.
export function codeTokens(text, yaml = false) {
  const expression = yaml
    ? /(^\s*[\w.-]+:)|("(?:[^"\\]|\\.)*"|'[^']*')|(^\s*#.*$)/gm
    : /(^\s*#.*$)|('[^']*'|"(?:[^"\\]|\\.)*")|(--[\w-]+)/gm;
  const tokens = [];
  let end = 0;
  for (const match of text.matchAll(expression)) {
    if (match.index > end) tokens.push({ text: text.slice(end, match.index), kind: '' });
    tokens.push({ text: match[0], kind: match[1] ? (yaml ? 'key' : 'comment') : match[2] ? 'string' : yaml ? 'comment' : 'flag' });
    end = match.index + match[0].length;
  }
  if (end < text.length) tokens.push({ text: text.slice(end), kind: '' });
  return tokens;
}
