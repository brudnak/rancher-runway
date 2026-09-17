import { parse, stringify } from 'yaml';

export const dataRoot = 'https://tashima42.github.io/rancher-helm-playground/data/v1/';
export const initialOverrides = () => ({ bootstrapPassword: 'admin', 'image.pullPolicy': 'Always', replicas: '1', hostname: 'rancher.local', agentTLSMode: 'system-store' });
export const quote = value => "'" + String(value).replaceAll("'", "'\\''") + "'";
const forbidden = new Set(['__proto__', 'constructor', 'prototype']);
export function fieldsFor(chart) {
  const fields = new Map();
  function walk(value, parts = []) {
    if (value && !Array.isArray(value) && typeof value === 'object' && Object.keys(value).length) {
      for (const [key, child] of Object.entries(value)) walk(child, [...parts, key]);
    } else if (parts.length) {
      const path = parts.join('.');
      let schema = chart.schema;
      for (const key of parts) schema = schema?.properties?.[key];
      fields.set(path, { path, value, type: value === null ? 'string' : typeof value === 'object' ? 'yaml' : typeof value, choices: schema?.enum });
    }
  }
  walk(chart.values);
  for (const option of chart.options || []) {
    if (!fields.has(option.path)) fields.set(option.path, { path: option.path, value: option.default, type: option.default !== null && typeof option.default === 'object' ? 'yaml' : typeof option.default });
    Object.assign(fields.get(option.path), { description: option.description, common: option.section === 'common' });
  }
  for (const path of ['extraEnv', ...(chart.values?.image ? ['rancherImage', 'rancherImageTag', 'rancherImagePullPolicy'] : [])]) fields.delete(path);
  return [...fields.values()].sort((a, b) => Number(Boolean(b.common || ['bootstrapPassword', 'replicas'].includes(b.path))) - Number(Boolean(a.common || ['bootstrapPassword', 'replicas'].includes(a.path))) || a.path.localeCompare(b.path));
}
export const displayValue = field => field.type === 'yaml' ? stringify(field.value).trim() : String(field.value ?? '');
function put(target, path, value) {
  const parts = path.split('.');
  if (parts.some(key => forbidden.has(key))) throw new Error('Unsupported value path: ' + path);
  let node = target;
  for (const key of parts.slice(0, -1)) node = node[key] ||= {};
  node[parts.at(-1)] = value;
}
export function buildOutput(config, channel, version, fields, overrides, env, chartName = 'rancher') {
  if (!config.release.trim() || !config.namespace.trim() || !config.repo.trim()) throw new Error('Repo name, release name, and namespace are required.');
  const all = {}, file = {}, flags = [];
  const flag = (path, value) => {
    // Escape Helm's strvals grammar as well as shell metacharacters.
    const escaped = String(value).replace(/[\\,{}]/g, '\\$&');
    flags.push(`${typeof value === 'string' ? '--set-string' : '--set'} ${quote(path + '=' + escaped)}`);
  };
  for (const field of fields) {
    if (!(field.path in overrides) || overrides[field.path] === displayValue(field)) continue;
    const raw = overrides[field.path];
    let value = raw;
    if (field.type === 'boolean') value = raw === 'true';
    if (field.type === 'number') {
      if (!String(raw).trim() || !Number.isFinite(Number(raw))) throw new Error(`${field.path} must be a number.`);
      value = Number(raw);
    }
    if (field.type === 'yaml') {
      try { value = parse(raw, { maxAliasCount: 50 }); } catch (error) { throw new Error(`${field.path}: ${error.message}`); }
      if (!value || typeof value !== 'object' || Array.isArray(value) !== Array.isArray(field.value)) throw new Error(`${field.path} must be a YAML ${Array.isArray(field.value) ? 'list' : 'map'}.`);
    }
    put(all, field.path, value);
    if (config.delivery === 'file' || field.type === 'yaml') put(file, field.path, value);
    else flag(field.path, value);
  }
  const entries = env.filter(entry => entry.name || entry.value);
  if (entries.some(entry => !/^[A-Za-z_][A-Za-z0-9_]*$/.test(entry.name))) throw new Error('Environment variables need valid names.');
  if (new Set(entries.map(entry => entry.name)).size !== entries.length) throw new Error('Environment variable names must be unique.');
  if (entries.length) {
    all.extraEnv = entries;
    if (config.delivery === 'file') file.extraEnv = entries;
    else entries.forEach((entry, index) => { flag(`extraEnv[${index}].name`, entry.name); flag(`extraEnv[${index}].value`, entry.value); });
  }
  const command = [`helm upgrade ${config.action === 'install' ? '--install ' : ''}${quote(config.release)} ${quote(config.repo + '/' + chartName)}`, `--namespace ${quote(config.namespace)}`, config.action === 'install' ? '--create-namespace' : config.action === 'upgrade' ? '--reset-then-reuse-values' : '--reuse-values', `--version ${quote(version)}`];
  if (channel.devel) command.push('--devel');
  if (Object.keys(file).length) command.push('--values values.yaml');
  command.push(...flags);
  return { repo: `helm repo add ${quote(config.repo)} ${quote(channel.repo)}\nhelm repo update`, command: command.join(' \\\n  '), yaml: stringify(file), allYaml: stringify(all) };
}
