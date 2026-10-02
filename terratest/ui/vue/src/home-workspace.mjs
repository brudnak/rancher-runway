// The home guide describes navigation, never launches infrastructure or commands.
export const workspaceTools = [
  { id: 'setup', label: 'Setup', group: 'operate', icon: 'compass', kind: 'Cloud', description: 'Plan and review a Rancher environment.', keywords: 'AWS RKE2 K3s hosted tenant Linode deploy install' },
  { id: 'runs', label: 'Runs', group: 'operate', icon: 'layers', kind: 'Monitor', description: 'Provisioning progress, logs, and saved runs.', keywords: 'progress status Terraform safety preflight checks' },
  { id: 'clusters', label: 'Clusters', group: 'operate', icon: 'server', kind: 'Inspect', description: 'Cluster access, health, test history, and SQL cache snapshots.', keywords: 'Kubernetes logs leader endpoint nickname history tests cache snapshots' },
  { id: 'history', label: 'Retained History', group: 'operate', icon: 'bookmark', kind: 'History', description: 'Organize removed cluster workspaces by milestone or Rancher version.', keywords: 'archive deleted cluster retained tests snapshots milestone version notes trash restore' },
  { id: 'aws', label: 'AWS Inventory', group: 'operate', icon: 'globe', kind: 'Cloud', description: 'Inspect AWS resources and review cleanup candidates.', keywords: 'owner orphan resources cleanup EC2 IAM' },
  { id: 'linode', label: 'Linode Resources', group: 'operate', icon: 'globe', kind: 'Cloud', description: 'Inspect token-visible Linode resources and review single-resource cleanup.', keywords: 'Linode inventory admin account instances volumes nodebalancers firewalls delete' },
  { id: 'destroy', label: 'Destroy', group: 'operate', icon: 'trash', kind: 'Cleanup', description: 'Remove environments and review recorded cost estimates.', keywords: 'costs estimates chart graph CSV export import pricing ledger local data billing teardown delete' },
  { id: 'images', label: 'Image Lookup', group: 'investigate', icon: 'boxes', kind: 'Read-only', description: 'Rancher builds, image digests, and platform details.', keywords: 'Prime registry digest tag SHA version Docker OCI' },
  { id: 'pr-builds', label: 'PR Image Check', group: 'investigate', icon: 'branch', kind: 'Read-only', description: 'Find builds that include a pull request.', keywords: 'GitHub fix ancestry included verification' },
  { id: 'helm', label: 'Helm Lab', group: 'investigate', icon: 'diamond', kind: 'Prepare', description: 'Configure chart values and prepare Helm commands.', keywords: 'chart YAML release upgrade install command' },
  { id: 'cache', label: 'Cache Lab', group: 'investigate', icon: 'database', kind: 'Inspect', description: 'Capture, query, and compare SQL cache snapshots.', keywords: 'SQLite database vacuum diff query kubeconfig token before after' },
  { id: 'my-work', label: 'My Work', group: 'investigate', icon: 'folder', kind: 'Plan', description: 'Your milestone, test plans, and readiness briefing.', keywords: 'GitHub assigned unassigned owner milestone coverage progress remaining plans validation' },
  { id: 'issues', label: 'Issue Radar', group: 'investigate', icon: 'pulse', kind: 'Plan', description: 'Issue ownership and workload by milestone.', keywords: 'GitHub milestone triage team report history delegate' },
  { id: 'packages', label: 'Test Packages', group: 'investigate', icon: 'folder', kind: 'Validate', description: 'Organize test plans, sessions, and evidence.', keywords: 'cases steps reproduction validation notes archive private backup bundles' },
  { id: 'k3d', label: 'K3D Lab', group: 'local', icon: 'boxes', kind: 'Local', description: 'Create and manage local K3s clusters in Docker.', keywords: 'Kubernetes k3s experiment sandbox containers' },
  { id: 'steve', label: 'Steve Lab', group: 'local', icon: 'flask', kind: 'Local', description: 'Run Steve locally and inspect its API and SQL cache.', keywords: 'API source ref development metrics SQLite experiment' },
  { id: 'tests', label: 'Test Lab', group: 'local', icon: 'flask', kind: 'Validate', description: 'Run validation tests and review results.', keywords: 'test suite cattle config templates folders README documentation preflight validation GitHub Actions runner regression' },
  { id: 'settings', label: 'Settings', group: 'local', icon: 'sliders', kind: 'Preferences', description: 'Home layout, daily scans, and reminders.', keywords: 'GPU reminder configuration' },
];

export const toolGroups = [
  { id: 'operate', title: 'Deploy & operate', description: 'From a reviewed plan to a running environment.' },
  { id: 'investigate', title: 'Investigate & prepare', description: 'Understand the change before you test it.' },
  { id: 'local', title: 'Your local workspace', description: 'Fast experiments and a workspace that fits you.' },
];

export const guidedPaths = [
  { id: 'issue', label: 'Validate an issue', icon: 'folder', description: 'Follow an issue from discovery and assignment through test preparation, reproduction, validation, and closure.', steps: [
    { tab: 'my-work', title: 'Bring in your work', detail: 'Pull a milestone for yourself or prepare unassigned issues. Missing test packages get a starter in a milestone bucket.' },
    { tab: 'packages', title: 'Plan & preserve', detail: 'Write manual cases, record sessions against known versions, and attach existing lab evidence.' },
    { tab: 'packages', title: 'Report & share', detail: 'Preview a Markdown report, export a portable bundle, or back up to your private GitHub repository.' },
  ] },
  { id: 'deploy', label: 'Deploy Rancher', icon: 'globe', description: 'Plan the environment, follow the run, then connect to the cluster.', steps: [
    { tab: 'setup', title: 'Choose & review', detail: 'Select a topology and version. Resolve the plan and approve it in Setup.' },
    { tab: 'runs', title: 'Follow the run', detail: 'Watch the operation and its logs while infrastructure comes online.' },
    { tab: 'clusters', title: 'Connect & test', detail: 'Open Rancher or use the recorded kubeconfig when the cluster is ready.' },
  ] },
  { id: 'change', label: 'Test a change', icon: 'branch', description: 'Build a chain of evidence from the PR to a release you can test.', steps: [
    { tab: 'pr-builds', title: 'Find the evidence', detail: 'Check which published images declare the PR commit in their ancestry.' },
    { tab: 'images', title: 'Inspect the build', detail: 'Use Inspect server in the PR result to carry its exact digest into Image Lookup.' },
    { tab: 'helm', title: 'Prepare the release', detail: 'Choose chart values, then review and export your Helm command. Nothing executes here.' },
  ] },
  { id: 'local', label: 'Experiment locally', icon: 'flask', description: 'Choose the lab that fits the question. Each creates its own local environment.', steps: [
    { tab: 'k3d', title: 'Need Kubernetes?', detail: 'Launch a K3s cluster in Docker for a small, reusable Kubernetes environment.' },
    { tab: 'steve', title: 'Need the Steve API?', detail: 'Go straight to Steve Lab. It creates its own k3d cluster; no K3D Lab setup is needed.' },
    { tab: 'images', title: 'Need a specific build?', detail: 'Find and inspect the exact image for your test without starting another environment.' },
  ] },
];

export function workspaceTool(tab) { return workspaceTools.find(tool => tool.id === tab); }
export function resumableTab(tab) { if(tab==='settings')return ''; return workspaceTool(tab === 'lifecycle' ? 'runs' : tab)?.id || ''; }
export function filterWorkspaceTools(query) {
  const words = String(query || '').toLowerCase().trim().split(/\s+/).filter(Boolean);
  return workspaceTools.filter(tool => words.every(word => `${tool.label} ${tool.description} ${tool.keywords} ${tool.kind}`.toLowerCase().includes(word)));
}
const rows = value => Array.isArray(value) ? value : [];
const activeCloudKeys = [
  ['awsCleanup', 'AWS cleanup', 'aws'], ['cleanupBatch', 'Run cleanup', 'destroy'],
  ['setup', 'AWS provisioning', 'runs'], ['linodeSetup', 'Linode provisioning', 'runs'],
  ['readiness', 'Readiness checks', 'runs'], ['downstream', 'Downstream provisioning', 'runs'],
  ['cleanup', 'Run cleanup', 'destroy'], ['linodeCleanup', 'Linode cleanup', 'destroy'],
];
export function homeSnapshot(state = {}, { bootPending = false, error = '' } = {}) {
  state ||= {};
  const cloud = activeCloudKeys.filter(([key]) => state[key]?.running).map(([key, label, tab]) => ({ id: key, label, tab, runId: state[key]?.runId || '' }));
  const k3d = rows(state.k3d?.clusters).filter(item => ['creating', 'running'].includes(item.status)).length;
  const steve = rows(state.steve?.runs).filter(item => ['starting', 'running', 'serving'].includes(item.status)).length;
  const localBusy = Boolean(state.k3d?.operation?.running || state.steve?.operation?.running);
  const runs = rows(state.workspace?.runs).length;
  const provisioning = cloud.some(item => ['setup', 'linodeSetup', 'downstream'].includes(item.id));
  const status = error ? { tone: 'warning', label: bootPending ? 'Status unavailable' : 'Last known status', title: 'Let’s refresh the picture.', detail: bootPending ? 'The startup check did not finish. You can explore the tools while Setup waits for a fresh safety check.' : 'The last refresh failed. Activity below may have changed; refresh before acting.', action: 'runs', actionLabel: 'Review workspace checks' }
    : bootPending ? { tone: 'checking', label: 'Checking workspace', title: 'Getting your bearings.', detail: 'Reading local state and checking for active work. Explore the guide while the safety check finishes.', action: 'runs', actionLabel: 'View workspace checks' }
    : cloud.length ? { tone: 'active', label: 'Work in progress', title: provisioning ? 'Your environment is taking shape.' : 'A cloud operation is running.', detail: 'Keep Runway open. You can switch tools and follow the operation in its workspace.', action: cloud[0].tab, actionLabel: cloud[0].tab === 'runs' ? 'Follow the active run' : 'View active cleanup' }
    : { tone: 'quiet', label: 'Workspace loaded', title: runs ? 'Pick up where you left off.' : 'Choose where to begin.', detail: runs ? 'Your recorded runs and tools are ready to explore. Choose your next step below.' : 'Start with a cloud deployment, investigate a build, or try a local experiment.', action: runs ? 'runs' : 'setup', actionLabel: runs ? 'Explore your runs' : 'Plan a deployment' };
  return { ...status, cloud, provisioning, k3d, steve, localBusy, runs, checking: bootPending && !error, stale: Boolean(error), known: !bootPending, aws: rows(state.aws?.items).length, awsRefreshing: Boolean(state.aws?.refreshing) };
}
