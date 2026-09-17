import { parse, stringify } from 'yaml';

// Metadata provider only; Helm Lab owns its UI, editing model and command generation.
export const dataRoot = 'https://tashima42.github.io/rancher-helm-playground/data/v1/';
export const initialConfig = () => ({
  distribution: 'prime', type: 'head', version: '', repo: 'rancher-prime-head',
  release: 'rancher', namespace: 'cattle-system', action: 'upgrade', delivery: 'set',
  context: '', wait: false, timeout: '5m', dryRun: false,
});
export const initialOverrides = () => ({
  bootstrapPassword: 'admin', 'image.pullPolicy': 'Always', replicas: '1',
  hostname: 'rancher.local', agentTLSMode: 'system-store',
});
export const quote = value => "'" + String(value).replaceAll("'", "'\\''") + "'";
const forbidden = new Set(['__proto__', 'constructor', 'prototype']);
const plainObject = value => value !== null && typeof value === 'object' && !Array.isArray(value);
export const sensitivePath = path => /password|token|privatekey|apikey|regcode/i.test(path);
export const cleanDescription = text => String(text || '')
  .replace(/\[([^\]]+)\]\([^)]+\)/g, '$1')
  .replace(/[*`]/g, '')
  .replace(/^(?:string|bool|int|map|list)\s*-\s*/i, '')
  .trim();
const schemaAt = (schema, parts) => parts.reduce((node, key) => node?.properties?.[key], schema);

export function fieldsFor(chart) {
  const fields = new Map();
  const options = new Map((chart.options || []).map(option => [option.path, option]));
  function walk(value, parts = []) {
    const path = parts.join('.');
    if (parts.some(key => forbidden.has(key))) return;
    const option = options.get(path);
    const schema = schemaAt(chart.schema, parts);
    const wholeMap = plainObject(option?.default) || schema?.additionalProperties;
    if (plainObject(value) && Object.keys(value).length && !wholeMap) {
      for (const [key, child] of Object.entries(value)) walk(child, [...parts, key]);
    } else if (parts.length) {
      fields.set(path, {
        path, parts, value,
        type: value === null ? 'string' : typeof value === 'object' ? 'yaml' : typeof value,
        choices: schema?.enum, minimum: schema?.minimum, maximum: schema?.maximum,
        integer: schema?.type === 'integer' || (typeof value === 'number' && Number.isInteger(value)),
      });
    }
  }
  walk(chart.values);
  for (const option of options.values()) {
    if (option.path.split('.').some(key => forbidden.has(key))) continue;
    if (!fields.has(option.path)) {
      // A documented parent map already has one editor, including all child keys.
      if ([...fields.keys()].some(path => option.path.startsWith(path + '.') && fields.get(path).type === 'yaml')) continue;
      const schema = schemaAt(chart.schema, option.path.split('.'));
      fields.set(option.path, {
        path: option.path, parts: option.path.split('.'), value: option.default,
        type: option.default === null ? 'string' : typeof option.default === 'object' ? 'yaml' : typeof option.default,
        choices: schema?.enum, integer: schema?.type === 'integer' || Number.isInteger(option.default),
        minimum: schema?.minimum, maximum: schema?.maximum,
      });
    }
    Object.assign(fields.get(option.path), { description: cleanDescription(option.description), common: option.section === 'common' });
  }
  for (const path of ['extraEnv', ...(chart.values?.image ? ['rancherImage', 'rancherImageTag', 'rancherImagePullPolicy'] : [])]) fields.delete(path);
  for (const field of fields.values()) {
    field.group = field.common || ['bootstrapPassword', 'replicas', 'image.repository', 'image.tag', 'image.pullPolicy', 'agentTLSMode'].includes(field.path)
      ? 'Essentials' : field.parts.length > 1 ? field.parts[0] : 'General';
  }
  return [...fields.values()].sort((a, b) => Number(b.group === 'Essentials') - Number(a.group === 'Essentials') || a.path.localeCompare(b.path));
}

export const displayValue = field => field.type === 'yaml' ? stringify(field.value).trim() : String(field.value ?? '');
export function parseFieldValue(field, raw) {
  if (typeof raw !== 'string') throw new Error('Enter a text value.');
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
    if (!value || typeof value !== 'object' || Array.isArray(value) !== Array.isArray(field.value)) {
      throw new Error(`Enter a YAML ${Array.isArray(field.value) ? 'list' : 'map'}.`);
    }
  }
  return value;
}
export function changedField(field, overrides) {
  if (!Object.hasOwn(overrides, field.path)) return false;
  if (overrides[field.path] === displayValue(field)) return false;
  try { return JSON.stringify(parseFieldValue(field, overrides[field.path])) !== JSON.stringify(field.value); }
  catch { return true; }
}
export function fieldErrors(fields, overrides) {
  return Object.fromEntries(fields.filter(field => changedField(field, overrides)).flatMap(field => {
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
export function buildOutput(config, channel, version, fields, overrides, env, chartName = 'rancher') {
  if (!/^[a-z0-9](?:[a-z0-9-]*[a-z0-9])?(?:\.[a-z0-9](?:[a-z0-9-]*[a-z0-9])?)*$/.test(config.release) || config.release.length > 53) throw new Error('Release name must be a lowercase DNS name, at most 53 characters.');
  if (!/^[a-z0-9](?:[a-z0-9-]*[a-z0-9])?$/.test(config.namespace) || config.namespace.length > 63) throw new Error('Namespace must be a lowercase DNS label, at most 63 characters.');
  if (!/^[A-Za-z0-9][A-Za-z0-9_-]*$/.test(config.repo)) throw new Error('Repo name may contain letters, numbers, hyphens and underscores.');
  if (!['install', 'upgrade', 'upgrade-reuse'].includes(config.action)) throw new Error('Select an install or upgrade strategy.');
  if (!['set', 'file'].includes(config.delivery)) throw new Error('Select a values delivery mode.');
  if (!version?.trim()) throw new Error('Select a chart version.');
  if (config.wait && !/^(?:\d+(?:\.\d+)?(?:ms|s|m|h))+$/.test(config.timeout || '')) throw new Error('Enter a timeout such as 5m or 1m30s.');
  const unknown = Object.keys(overrides).filter(path => !fields.some(field => field.path === path));
  if (unknown.length) throw new Error(`These overrides are not available in this chart: ${unknown.join(', ')}. Remove them or select another chart.`);
  const all = {}, file = {}, flags = [];
  const changed = fields.filter(field => changedField(field, overrides));
  const flag = (path, value) => {
    const escaped = String(value).replace(/[\\,{}]/g, '\\$&');
    flags.push(`${typeof value === 'string' ? '--set-string' : '--set'} ${quote(path + '=' + escaped)}`);
  };
  for (const field of changed) {
    let value;
    try { value = parseFieldValue(field, overrides[field.path]); }
    catch (error) { throw new Error(`${field.path}: ${error.message}`); }
    put(all, field.parts || field.path.split('.'), value);
    if (config.delivery === 'file' || field.type === 'yaml') put(file, field.parts || field.path.split('.'), value);
    else flag(helmPath(field), value);
  }
  const entries = env.filter(entry => entry.name || entry.value).map(({ name, value }) => ({ name, value }));
  if (entries.some(entry => !/^[A-Za-z_][A-Za-z0-9_]*$/.test(entry.name))) throw new Error('Environment variables need valid names.');
  if (new Set(entries.map(entry => entry.name)).size !== entries.length) throw new Error('Environment variable names must be unique.');
  if (entries.length) {
    all.extraEnv = entries;
    if (config.delivery === 'file') file.extraEnv = entries;
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
  if (config.dryRun) command.push('--dry-run');
  const needsFile = Object.keys(file).length > 0;
  if (needsFile) command.push('--values values.yaml');
  command.push(...flags);
  return {
    repo: `helm repo add ${quote(config.repo)} ${quote(channel.repo)}\nhelm repo update`,
    command: command.join(' \\\n  '), yaml: stringify(file), allYaml: stringify(all),
    commandParts: command, needsFile, changeCount: changed.length + (entries.length ? 1 : 0),
  };
}

export function importValues(text, fields) {
  const values = parse(text, { maxAliasCount: 50 });
  if (!plainObject(values)) throw new Error('The values file must contain a YAML mapping.');
  const overrides = {}, unknown = [];
  function visit(value, parts = []) {
    const path = parts.join('.');
    if (parts.some(key => forbidden.has(key))) throw new Error(`Unsupported key: ${path}`);
    if (path === 'extraEnv') return;
    const field = fields.find(item => item.path === path);
    if (field) {
      if (field.type !== 'yaml' && value !== null && typeof value !== field.type) throw new Error(`${path}: expected ${field.type}.`);
      const raw = field.type === 'yaml' ? stringify(value).trim() : String(value ?? '');
      parseFieldValue(field, raw);
      if (changedField(field, { [path]: raw })) overrides[path] = raw;
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
  return { overrides, env: env.map(({ name, value }) => ({ name, value })) };
}

export function shareState(config, overrides, env, includeSensitive = false) {
  const omitted = includeSensitive ? [] : Object.keys(overrides).filter(sensitivePath);
  if (!includeSensitive && env.length) omitted.push('extraEnv');
  return {
    config: { ...config },
    overrides: Object.fromEntries(Object.entries(overrides).filter(([path]) => !omitted.includes(path))),
    env: includeSensitive ? env.map(({ name, value }) => ({ name, value })) : [], omitted,
  };
}
export function restoreState(saved) {
  if (!plainObject(saved) || !plainObject(saved.config) || !plainObject(saved.overrides) || !Array.isArray(saved.env)) throw new Error('Invalid configuration link.');
  const config = initialConfig();
  for (const key of Object.keys(config)) if (typeof saved.config[key] === typeof config[key]) config[key] = saved.config[key];
  if (!['community', 'prime'].includes(config.distribution) || !['ga', 'rc', 'alpha', 'head'].includes(config.type)) throw new Error('Invalid chart channel in configuration link.');
  const overrides = Object.fromEntries(Object.entries(saved.overrides).filter(([key, value]) => typeof value === 'string' && !key.split('.').some(part => forbidden.has(part))));
  const env = saved.env.filter(item => plainObject(item) && typeof item.name === 'string' && typeof item.value === 'string').map(({ name, value }) => ({ name, value }));
  return { config, overrides, env, omitted: Array.isArray(saved.omitted) ? saved.omitted.filter(item => typeof item === 'string') : [] };
}

// A single copyable setup script keeps its companion YAML in a private temp directory.
export function buildSetupScript(output) {
  if (!output?.command) return '';
  const lines = ['#!/usr/bin/env sh', 'set -eu', '', '# Register the chart repository', output.repo, ''];
  if (output.needsFile) {
    let delimiter = 'HELM_LAB_VALUES';
    while (output.yaml.split('\n').includes(delimiter)) delimiter += '_END';
    lines.push(
      '# Write the companion values to a temporary file',
      'helmlab_values_dir=$(mktemp -d)',
      `trap 'rm -rf "$helmlab_values_dir"' 0`,
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
