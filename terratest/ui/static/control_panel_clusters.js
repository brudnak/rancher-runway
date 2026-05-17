import {
  badge,
  clusterItems,
  escapeHtml,
  podsFor
} from './control_panel_utils.js'

const statusFor = cluster => {
  if (cluster.reachable) {
    return {
      label: 'Reachable',
      className: 'bg-emerald-100 text-emerald-700 dark:bg-emerald-500/15 dark:text-emerald-300'
    }
  }
  if (cluster.provisioning) {
    return {
      label: 'Provisioning',
      className: 'bg-amber-100 text-amber-800 dark:bg-amber-500/15 dark:text-amber-200'
    }
  }
  if (cluster.available) {
    return {
      label: 'Unavailable',
      className: 'bg-amber-100 text-amber-800 dark:bg-amber-500/15 dark:text-amber-200'
    }
  }
  return {
    label: 'Missing',
    className: 'bg-zinc-100 text-zinc-600 dark:bg-white/[0.06] dark:text-zinc-300'
  }
}

const emptyPodsText = cluster => cluster.type === 'downstream'
  ? 'Pods are unavailable until the downstream kubeconfig is reachable.'
  : 'Pods are unavailable until kubeconfig exists and kubectl can reach the cluster.'

const metaItem = (label, value) => `
  <div class="min-w-0">
    <div class="text-xs font-semibold uppercase tracking-wide text-zinc-500 dark:text-zinc-400">${escapeHtml(label)}</div>
    <div class="mt-1 break-words text-sm font-medium text-zinc-800 [overflow-wrap:anywhere] dark:text-zinc-200">${value}</div>
  </div>
`

const clusterRunKey = cluster => String(cluster?.runId || 'default')

const clusterHAKey = cluster => String(cluster?.haIndex || 0)

const runLabelForClusterGroup = (runKey, workspace) => {
  const runs = Array.isArray(workspace?.runs) ? workspace.runs : []
  const run = runs.find(item => String(item.runId || 'default') === runKey)
  if (run?.runId) {
    return `Run ${run.runId}`
  }
  if (run?.slotId) {
    return run.slotId.replace(/^slot-/, 'Slot ')
  }
  if (runKey !== 'default') {
    return `Run ${runKey}`
  }
  return 'Default slot'
}

const buildClusterGroups = (items, workspace) => {
  const runOrder = []
  const groups = new Map()
  const runs = Array.isArray(workspace?.runs) ? workspace.runs : []

  runs.forEach(run => {
    const runKey = String(run.runId || 'default')
    if (!groups.has(runKey)) {
      groups.set(runKey, {
        runKey,
        label: runLabelForClusterGroup(runKey, workspace),
        run,
        haOrder: [],
        has: new Map()
      })
      runOrder.push(runKey)
    }
  })

  items.forEach(cluster => {
    const runKey = clusterRunKey(cluster)
    if (!groups.has(runKey)) {
      groups.set(runKey, {
        runKey,
        label: runLabelForClusterGroup(runKey, workspace),
        run: null,
        haOrder: [],
        has: new Map()
      })
      runOrder.push(runKey)
    }

    const group = groups.get(runKey)
    const haKey = clusterHAKey(cluster)
    if (!group.has.has(haKey)) {
      group.has.set(haKey, {
        haKey,
        haIndex: cluster.haIndex || 0,
        local: null,
        downstreams: []
      })
      group.haOrder.push(haKey)
    }

    const ha = group.has.get(haKey)
    if (cluster.type === 'downstream') {
      ha.downstreams.push(cluster)
    } else {
      ha.local = cluster
    }
  })

  return runOrder
    .map(runKey => groups.get(runKey))
    .filter(Boolean)
    .map(group => ({
      ...group,
      has: group.haOrder
        .map(haKey => group.has.get(haKey))
        .filter(Boolean)
        .sort((left, right) => (left.haIndex || 0) - (right.haIndex || 0))
    }))
}

export const createClusterPanel = ({
  clustersEl,
  getActionState,
  getActiveSelection,
  setActiveSelection,
  getLastState,
  renderLastState,
  cleanupResultDismissed
}) => {
  let previousLeaders = new Map()
  const pendingLeaderHighlights = new Map()
  const collapsedClusters = new Map()
  const collapsedPods = new Map()
  const collapsedGPUCommands = new Map()
  const initializedCollapseState = new Set()
  const kubeconfigPathActionFeedback = new Map()
  const kubeconfigPathActionTimers = new Map()
  const gpuCommandCopyFeedback = new Map()
  const gpuCommandCopyTimers = new Map()

  const initializeCollapseState = cluster => {
    if (initializedCollapseState.has(cluster.id)) {
      return
    }
    initializedCollapseState.add(cluster.id)
    if (cluster.type === 'downstream') {
      collapsedClusters.set(cluster.id, true)
      collapsedPods.set(cluster.id, true)
    }
    if (cluster.type === 'local' && cluster.gpuWorkerIp) {
      collapsedGPUCommands.set(cluster.id, false)
    }
  }

  const pathActionKey = (action, id) => `${action}:${id}`

  const flashKubeconfigPathAction = (clusterId, action, status) => {
    const key = pathActionKey(action, clusterId)
    window.clearTimeout(kubeconfigPathActionTimers.get(key))
    kubeconfigPathActionFeedback.set(key, status)
    renderLastState()
    kubeconfigPathActionTimers.set(key, window.setTimeout(() => {
      if (kubeconfigPathActionFeedback.get(key) === status) {
        kubeconfigPathActionFeedback.delete(key)
        renderLastState()
      }
    }, 1800))
  }

  const kubeconfigPathActionContent = (clusterId, action, idleLabel, activeLabel, successLabel, errorLabel) => {
    const actionState = getActionState()
    const active = action === 'open'
      ? actionState.activeOpenKubeconfigPathClusterId === clusterId
      : actionState.activeCopyKubeconfigPathClusterId === clusterId
    if (active) {
      return `<span class="spinner mr-2 !h-3 !w-3 !border-[1.5px]"></span>${activeLabel}`
    }

    const feedback = kubeconfigPathActionFeedback.get(pathActionKey(action, clusterId))
    if (feedback === 'success') {
      return successLabel
    }
    if (feedback === 'error') {
      return errorLabel
    }
    return idleLabel
  }

  const renderKubeconfigActions = cluster => {
    if (!cluster.available) {
      return '<span class="text-sm text-zinc-500 dark:text-zinc-400">Kubeconfig unavailable</span>'
    }

    const actionState = getActionState()
    const downloading = actionState.activeDownloadClusterId === cluster.id
    const copying = actionState.activeCopyClusterId === cluster.id
    const copyingHelm = actionState.activeCopyHelmClusterId === cluster.id
    const copyingHelmUpgrade = actionState.activeCopyHelmUpgradeClusterId === cluster.id
    const downloadSpinner = downloading ? '<span class="spinner mr-2"></span>' : ''
    const copySpinner = copying ? '<span class="spinner mr-2"></span>' : ''
    const copyHelmSpinner = copyingHelm ? '<span class="spinner mr-2"></span>' : ''
    const copyHelmUpgradeSpinner = copyingHelmUpgrade ? '<span class="spinner mr-2"></span>' : ''
    const downloadLabel = cluster.type === 'downstream' ? 'Download downstream kubeconfig' : 'Download kubeconfig'
    const copyHelmButtons = cluster.type === 'local'
      ? `<div class="flex max-w-full flex-wrap gap-2 rounded-xl border border-zinc-200 bg-zinc-50 p-2 dark:border-white/10 dark:bg-white/[0.03]">
           <button type="button" data-action="copy-helm-command" data-cluster="${escapeHtml(cluster.id)}"${copyingHelm ? ' disabled' : ''} title="Copy the Helm command used during setup." class="inline-flex min-h-11 max-w-full items-center justify-center whitespace-normal rounded-lg border border-zinc-200 bg-white px-4 py-2 text-center text-sm font-semibold text-zinc-700 hover:bg-zinc-50 disabled:cursor-default disabled:opacity-70 dark:border-white/10 dark:bg-white/[0.06] dark:text-zinc-200 dark:hover:bg-white/[0.1]">${copyHelmSpinner}${copyingHelm ? 'Copying' : 'Copy install command'}</button>
           <button type="button" data-action="copy-helm-upgrade-command" data-cluster="${escapeHtml(cluster.id)}"${copyingHelmUpgrade ? ' disabled' : ''} title="Copy the setup command converted to helm upgrade --install for editing before an upgrade." class="inline-flex min-h-11 max-w-full items-center justify-center whitespace-normal rounded-lg border border-sky-200 bg-sky-50 px-4 py-2 text-center text-sm font-semibold text-sky-800 hover:bg-sky-100 disabled:cursor-default disabled:opacity-70 dark:border-sky-500/25 dark:bg-sky-500/10 dark:text-sky-200 dark:hover:bg-sky-500/15">${copyHelmUpgradeSpinner}${copyingHelmUpgrade ? 'Copying' : 'Copy upgrade draft'}</button>
         </div>`
      : ''

    return `
      <button type="button" data-action="download" data-cluster="${escapeHtml(cluster.id)}"${downloading ? ' disabled' : ''} class="inline-flex min-h-11 max-w-full items-center justify-center whitespace-normal rounded-lg bg-emerald-500 px-4 py-2 text-center text-sm font-semibold text-white shadow-sm shadow-emerald-500/20 hover:bg-emerald-400 disabled:cursor-default disabled:opacity-70">${downloadSpinner}${downloading ? 'Preparing kubeconfig' : downloadLabel}</button>
      <button type="button" data-action="copy-kubeconfig" data-cluster="${escapeHtml(cluster.id)}"${copying ? ' disabled' : ''} class="inline-flex min-h-11 items-center justify-center rounded-lg border border-zinc-200 bg-white px-4 py-2 text-sm font-semibold text-zinc-700 hover:bg-zinc-50 disabled:cursor-default disabled:opacity-70 dark:border-white/10 dark:bg-white/[0.06] dark:text-zinc-200 dark:hover:bg-white/[0.1]">${copySpinner}${copying ? 'Copying' : 'Copy kubeconfig'}</button>
      ${copyHelmButtons}
    `
  }

  const renderGPUWorkerPanel = cluster => {
    if (cluster.type !== 'local' || !cluster.gpuWorkerIp) {
      return ''
    }

    const instanceType = cluster.gpuWorkerInstanceType || 'GPU instance'
    const kubeconfigPath = cluster.kubeconfigPath || '/path/to/kube_config.yaml'
    const recommendedLizModel = instanceType === 'p5.4xlarge' ? 'gpt-oss:120b' : 'gpt-oss:20b'
    const modelDetail = instanceType === 'p5.4xlarge'
      ? 'The large GPU profile matches the gpt-oss:120b requirement from the Liz quick start. Use gpt-oss:20b instead if you only need a cheaper smoke test.'
      : 'The standard GPU profile is sized for gpt-oss:20b, the smaller local model in the Liz quick start.'
    const commands = [
      {
        title: 'Use this kubeconfig',
        tone: 'Required',
        detail: 'Sets this terminal session to the local HA cluster.',
        command: `export KUBECONFIG="${kubeconfigPath}"`
      },
      {
        title: 'Choose the local Ollama model',
        tone: 'Required',
        detail: modelDetail,
        command: `export LIZ_MODEL="${recommendedLizModel}"`
      },
      {
        title: 'Confirm the GPU worker joined',
        tone: 'Check',
        detail: 'Shows the worker label and EC2 GPU instance type.',
        command: 'kubectl get nodes -L ha-rancher-rke2/gpu-worker -L ha-rancher-rke2/gpu-instance-type'
      },
      {
        title: 'Add the Liz GPU worker label',
        tone: 'Required',
        detail: 'Marks only this worker as eligible for Ollama/Liz GPU scheduling.',
        command: `GPU_NODE="$(kubectl get nodes -l ha-rancher-rke2/gpu-worker=true -o jsonpath='{.items[0].metadata.name}')"
test -n "$GPU_NODE"
kubectl label node "$GPU_NODE" liz-ai.suse.com/gpu-worker=true --overwrite`
      },
      {
        title: 'Install the NVIDIA GPU Operator for RKE2',
        tone: 'Required',
        detail: 'Uses the RKE2 HelmChart path, RKE2 containerd socket, and disables driver install because the GPU AMI already provides NVIDIA drivers.',
        command: `kubectl apply -f - <<'EOF'
apiVersion: helm.cattle.io/v1
kind: HelmChart
metadata:
  name: gpu-operator
  namespace: kube-system
spec:
  repo: https://helm.ngc.nvidia.com/nvidia
  chart: gpu-operator
  version: v25.10.1
  targetNamespace: gpu-operator
  createNamespace: true
  valuesContent: |-
    driver:
      enabled: false
    toolkit:
      env:
      - name: CONTAINERD_SOCKET
        value: /run/k3s/containerd/containerd.sock
EOF`
      },
      {
        title: 'Watch GPU Operator pods',
        tone: 'Verify',
        detail: 'Waits for the RKE2 toolkit and device plugin daemonsets before scheduling GPU workloads.',
        command: `until kubectl -n gpu-operator get ds/nvidia-container-toolkit-daemonset ds/nvidia-device-plugin-daemonset >/dev/null 2>&1; do
  sleep 10
done
kubectl -n gpu-operator rollout status ds/nvidia-container-toolkit-daemonset --timeout=30m
kubectl -n gpu-operator rollout status ds/nvidia-device-plugin-daemonset --timeout=30m
kubectl -n gpu-operator get pods -o wide`
      },
      {
        title: 'Verify GPU capacity on the worker',
        tone: 'Verify',
        detail: 'Stops early if Kubernetes still has not advertised allocatable GPU capacity.',
        command: `GPU_NODE="$(kubectl get nodes -l ha-rancher-rke2/gpu-worker=true -o jsonpath='{.items[0].metadata.name}')"
GPU_COUNT="$(kubectl get node "$GPU_NODE" -o jsonpath='{.status.allocatable.nvidia\\.com/gpu}')"
test "\${GPU_COUNT:-0}" -ge 1
echo "GPU node $GPU_NODE has $GPU_COUNT allocatable NVIDIA GPU(s)."
kubectl describe node "$GPU_NODE" | grep -A8 "nvidia.com/gpu"`
      },
      {
        title: 'Run a CUDA smoke test',
        tone: 'Verify',
        detail: 'Runs only after allocatable GPU exists, waits for completion, then prints the benchmark output.',
        command: `GPU_NODE="$(kubectl get nodes -l liz-ai.suse.com/gpu-worker=true -o jsonpath='{.items[0].metadata.name}')"
GPU_COUNT="$(kubectl get node "$GPU_NODE" -o jsonpath='{.status.allocatable.nvidia\\.com/gpu}')"
test "\${GPU_COUNT:-0}" -ge 1
kubectl delete pod nbody-gpu-benchmark --ignore-not-found
kubectl apply -f - <<'EOF'
apiVersion: v1
kind: Pod
metadata:
  name: nbody-gpu-benchmark
  namespace: default
spec:
  restartPolicy: OnFailure
  nodeSelector:
    liz-ai.suse.com/gpu-worker: "true"
  containers:
  - name: cuda-container
    image: nvcr.io/nvidia/k8s/cuda-sample:nbody
    args: ["nbody", "-gpu", "-benchmark"]
    resources:
      limits:
        nvidia.com/gpu: 1
EOF
kubectl wait --for=jsonpath='{.status.phase}'=Succeeded pod/nbody-gpu-benchmark --timeout=10m || {
  kubectl describe pod nbody-gpu-benchmark
  kubectl logs pod/nbody-gpu-benchmark --all-containers --tail=100 || true
  exit 1
}
kubectl logs pod/nbody-gpu-benchmark --all-containers
kubectl delete pod nbody-gpu-benchmark --ignore-not-found`
      },
      {
        title: 'Deploy Ollama on the GPU worker',
        tone: 'Required',
        detail: 'Creates an in-cluster Ollama service named ollama in the Rancher AI agent namespace, matching the SUSE quick start URL.',
        command: `kubectl apply -f - <<'EOF'
apiVersion: v1
kind: Namespace
metadata:
  name: cattle-ai-agent-system
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: ollama
  namespace: cattle-ai-agent-system
spec:
  replicas: 1
  selector:
    matchLabels:
      app: ollama
  template:
    metadata:
      labels:
        app: ollama
    spec:
      nodeSelector:
        liz-ai.suse.com/gpu-worker: "true"
      containers:
      - name: ollama
        image: ollama/ollama:latest
        env:
        - name: OLLAMA_HOST
          value: 0.0.0.0:11434
        ports:
        - name: http
          containerPort: 11434
        resources:
          limits:
            nvidia.com/gpu: 1
        volumeMounts:
        - name: ollama-data
          mountPath: /root/.ollama
      volumes:
      - name: ollama-data
        emptyDir: {}
---
apiVersion: v1
kind: Service
metadata:
  name: ollama
  namespace: cattle-ai-agent-system
spec:
  selector:
    app: ollama
  ports:
  - name: http
    port: 11434
    targetPort: 11434
EOF`
      },
      {
        title: 'Pull the model into Ollama',
        tone: 'Required',
        detail: 'This can take a while; the model is stored in the Ollama pod volume for this test run.',
        command: `kubectl -n cattle-ai-agent-system rollout status deploy/ollama --timeout=15m
kubectl -n cattle-ai-agent-system exec deploy/ollama -- ollama pull "$LIZ_MODEL"
kubectl -n cattle-ai-agent-system exec deploy/ollama -- ollama list`
      },
      {
        title: 'Create Rancher AI agent values',
        tone: 'Required',
        detail: 'Matches the SUSE quick start values, using the local Ollama service and selected model.',
        command: `cat > /tmp/rancher-ai-values.yaml <<EOF
ollamaLlmModel: "\${LIZ_MODEL:-gpt-oss:20b}"
ollamaUrl: "http://ollama:11434"
activeLlm: "ollama"
EOF`
      },
      {
        title: 'Install the Rancher AI agent',
        tone: 'Required',
        detail: 'Deploys the agent and MCP chart from the SUSE Rancher AI quick start.',
        command: `helm upgrade --install rancher-ai-agent \\
  --namespace cattle-ai-agent-system \\
  --create-namespace \\
  -f /tmp/rancher-ai-values.yaml \\
  oci://registry.suse.com/rancher/charts/rancher-ai-agent`
      },
      {
        title: 'Verify the Rancher AI backend',
        tone: 'Verify',
        detail: 'Checks that Ollama and the Rancher AI agent pods are up.',
        command: `kubectl -n cattle-ai-agent-system wait --for=condition=Ready pod -l app=ollama --timeout=10m
kubectl -n cattle-ai-agent-system wait --for=condition=Ready pod -l app.kubernetes.io/instance=rancher-ai-agent --timeout=10m
kubectl -n cattle-ai-agent-system get pods -o wide`
      },
      {
        title: 'Open Rancher to install the UI extension',
        tone: 'Manual',
        detail: 'Required by the SUSE quick start: Extensions > add official repositories > install AI Assistant > reload Rancher UI.',
        command: cluster.rancherUrl ? `open "${cluster.rancherUrl}/dashboard/c/local/extensions"` : 'echo "Open Rancher UI > Extensions, add official repositories, install AI Assistant, then reload the Rancher UI."'
      }
    ]
    const collapsed = collapsedGPUCommands.get(cluster.id) === true
    const copyButtonClass = 'inline-flex min-h-9 items-center justify-center rounded-md border px-3 py-1.5 text-xs font-semibold disabled:cursor-default disabled:opacity-70'
    const commandRows = commands.map((item, index) => {
      const feedback = gpuCommandCopyFeedback.get(`${cluster.id}:${index}`)
      const copied = feedback === 'success'
      const failed = feedback === 'error'
      const buttonTone = failed
        ? 'border-rose-300 bg-rose-50 text-rose-700 dark:border-rose-500/30 dark:bg-rose-500/10 dark:text-rose-200'
        : copied
          ? 'border-emerald-300 bg-emerald-50 text-emerald-700 dark:border-emerald-500/30 dark:bg-emerald-500/10 dark:text-emerald-200'
          : 'border-zinc-300 bg-white text-zinc-700 hover:bg-zinc-50 dark:border-white/15 dark:bg-white/[0.08] dark:text-zinc-100 dark:hover:bg-white/[0.12]'
      const toneClass = item.tone === 'Required'
        ? 'bg-rose-100 text-rose-800 dark:bg-rose-500/15 dark:text-rose-200'
        : item.tone === 'Verify'
          ? 'bg-emerald-100 text-emerald-800 dark:bg-emerald-500/15 dark:text-emerald-200'
          : 'bg-sky-100 text-sky-800 dark:bg-sky-500/15 dark:text-sky-200'

      return `
        <div data-gpu-command-row class="grid gap-3 rounded-lg border border-zinc-200 bg-white p-3 shadow-sm dark:border-white/10 dark:bg-zinc-950/70">
          <div class="flex min-w-0 flex-wrap items-start justify-between gap-3">
            <div class="min-w-0">
              <div class="flex flex-wrap items-center gap-2">
                <span class="text-sm font-semibold text-zinc-950 dark:text-zinc-50">${escapeHtml(item.title)}</span>
                <span class="rounded-full px-2 py-0.5 text-[11px] font-bold uppercase tracking-wide ${toneClass}">${escapeHtml(item.tone)}</span>
              </div>
              <div class="mt-1 text-xs leading-5 text-zinc-600 dark:text-zinc-300">${escapeHtml(item.detail)}</div>
            </div>
            <button type="button" data-action="copy-gpu-command" data-cluster="${escapeHtml(cluster.id)}" data-command-index="${index}" class="${copyButtonClass} ${buttonTone}">${failed ? 'Copy failed' : copied ? 'Copied' : 'Copy'}</button>
          </div>
          <pre class="max-w-full overflow-auto whitespace-pre rounded-md border border-zinc-200 bg-zinc-50 p-3 font-mono text-xs leading-6 text-zinc-900 dark:border-white/10 dark:bg-black/35 dark:text-zinc-100"><code data-gpu-command-text>${escapeHtml(item.command)}</code></pre>
        </div>
      `
    }).join('')

    return `
      <div class="mt-4 overflow-hidden rounded-xl border border-rose-200 bg-rose-50 text-zinc-950 dark:border-rose-500/30 dark:bg-rose-950/30 dark:text-zinc-50">
        <div class="flex flex-wrap items-center justify-between gap-3 px-4 py-3">
          <span>
            <span class="block text-sm font-semibold text-rose-900 dark:text-rose-100">Active GPU worker node in slot ${escapeHtml(cluster.runId || 'current')}</span>
            <span class="mt-1 block text-sm leading-6 text-rose-800/85 dark:text-rose-100/75">${escapeHtml(instanceType)} at ${escapeHtml(cluster.gpuWorkerIp)}${cluster.gpuWorkerSubnetId ? ` in ${escapeHtml(cluster.gpuWorkerSubnetId)}` : ''}${cluster.gpuWorkerAmi ? ` using ${escapeHtml(cluster.gpuWorkerAmi)}` : ''}. Do not leave running unused.</span>
          </span>
          <button type="button" data-action="toggle-gpu-commands" data-cluster="${escapeHtml(cluster.id)}" class="inline-flex min-h-10 items-center justify-center rounded-lg bg-rose-500 px-3.5 py-2 text-sm font-semibold text-white shadow-sm shadow-rose-500/20 hover:bg-rose-400">
            ${collapsed ? 'Show GPU setup commands' : 'Hide GPU setup commands'}
          </button>
        </div>
        ${collapsed ? '' : `
        <div class="border-t border-rose-200 p-4 dark:border-rose-500/25">
          <div class="mb-3 text-sm leading-6 text-rose-900 dark:text-rose-100">
            Run these in order to prepare the RKE2 GPU worker, run Ollama on it, install the Rancher AI agent, and open the required Rancher UI extension step.
          </div>
          <div class="grid gap-3">${commandRows}</div>
        </div>
        `}
      </div>
    `
  }

  const renderPodRows = (cluster, pods, changedLeader) => {
    if (!pods.length) {
      const message = cluster.error ? cluster.error : emptyPodsText(cluster)
      return `<tr><td colspan="8" class="px-3 py-4 text-sm text-zinc-500 dark:text-zinc-400">${escapeHtml(message)}</td></tr>`
    }

    return pods.map(pod => {
      const rowClass = changedLeader && changedLeader === pod.name
        ? 'bg-emerald-50 dark:bg-emerald-500/10'
        : pod.leader
          ? 'bg-emerald-50/70 dark:bg-emerald-500/5'
          : ''
      const leaderBadge = pod.leader && pod.leaderLabel ? badge(pod.leaderLabel) : ''

      return `
        <tr class="${rowClass}">
          <td class="break-words px-3 py-3 align-top text-sm text-zinc-600 dark:text-zinc-400">${escapeHtml(pod.namespace || '')}</td>
          <td class="px-3 py-3 align-top">
            <div class="flex flex-wrap items-center gap-2 text-sm font-semibold text-zinc-900 dark:text-zinc-100">
              <span>${escapeHtml(pod.name)}</span>
              ${leaderBadge}
            </div>
            <div class="mt-2 flex flex-wrap gap-2">
              <button type="button" data-action="tail" data-cluster="${escapeHtml(cluster.id)}" data-namespace="${escapeHtml(pod.namespace || 'cattle-system')}" data-pod="${escapeHtml(pod.name)}" class="rounded-lg border border-zinc-200 bg-white px-3 py-1.5 text-xs font-semibold text-zinc-700 hover:bg-zinc-50 dark:border-white/10 dark:bg-white/[0.06] dark:text-zinc-200 dark:hover:bg-white/[0.1]">Tail</button>
              <button type="button" data-action="live" data-cluster="${escapeHtml(cluster.id)}" data-namespace="${escapeHtml(pod.namespace || 'cattle-system')}" data-pod="${escapeHtml(pod.name)}" class="rounded-lg border border-zinc-200 bg-white px-3 py-1.5 text-xs font-semibold text-zinc-700 hover:bg-zinc-50 dark:border-white/10 dark:bg-white/[0.06] dark:text-zinc-200 dark:hover:bg-white/[0.1]">Live</button>
            </div>
          </td>
          <td class="break-words px-3 py-3 align-top text-sm text-zinc-700 dark:text-zinc-300">${escapeHtml(pod.ready)}</td>
          <td class="break-words px-3 py-3 align-top text-sm text-zinc-700 dark:text-zinc-300">${escapeHtml(pod.status)}</td>
          <td class="break-words px-3 py-3 align-top text-sm text-zinc-700 dark:text-zinc-300">${pod.restarts}</td>
          <td class="break-words px-3 py-3 align-top text-sm text-zinc-700 dark:text-zinc-300">${escapeHtml(pod.age)}</td>
          <td class="break-words px-3 py-3 align-top text-sm text-zinc-700 dark:text-zinc-300">${escapeHtml(pod.node || '')}</td>
          <td class="break-words px-3 py-3 align-top text-sm text-zinc-700 dark:text-zinc-300">${escapeHtml(pod.containers)}</td>
        </tr>
      `
    }).join('')
  }

  const renderPodsTable = (cluster, pods, changedLeader) => {
    const podsCollapsed = collapsedPods.get(cluster.id) === true
    const toggleText = podsCollapsed ? 'Show pods' : 'Hide pods'

    if (cluster.provisioning) {
      return `
        <div class="mt-4 rounded-xl border border-amber-200 bg-amber-50 px-4 py-3 text-sm font-medium text-amber-800 dark:border-amber-500/20 dark:bg-amber-500/10 dark:text-amber-200">
          <span class="spinner mr-2"></span>${escapeHtml(cluster.provisioningMessage || 'Provisioning downstream cluster')}
        </div>
      `
    }

    return `
      <div class="mt-4 flex items-center justify-between gap-3">
        <div class="text-sm font-semibold text-zinc-950 dark:text-zinc-100">Pods <span class="text-zinc-500 dark:text-zinc-400">${pods.length}</span></div>
        <button type="button" data-action="toggle-pods" data-cluster="${escapeHtml(cluster.id)}" class="rounded-lg border border-zinc-200 bg-white px-3 py-2 text-xs font-semibold text-zinc-700 hover:bg-zinc-50 dark:border-white/10 dark:bg-white/[0.06] dark:text-zinc-200 dark:hover:bg-white/[0.1]">${toggleText}</button>
      </div>
      ${podsCollapsed ? '' : `
        <div class="mt-3 max-w-full overflow-hidden rounded-xl border border-zinc-200 dark:border-white/10">
          <table class="w-full table-fixed border-collapse text-left">
            <colgroup>
              <col class="w-[9rem]" />
              <col class="w-[24rem]" />
              <col class="w-[5rem]" />
              <col class="w-[7rem]" />
              <col class="w-[6rem]" />
              <col class="w-[5rem]" />
              <col class="w-[12rem]" />
              <col />
            </colgroup>
            <thead class="bg-zinc-50 dark:bg-white/[0.04]">
              <tr>
                ${['Namespace', 'Pod', 'Ready', 'Status', 'Restarts', 'Age', 'Node', 'Containers'].map(label => `<th class="px-3 py-2 text-xs font-semibold uppercase tracking-wide text-zinc-500 dark:text-zinc-400">${label}</th>`).join('')}
              </tr>
            </thead>
            <tbody class="divide-y divide-zinc-200 dark:divide-white/10">
              ${renderPodRows(cluster, pods, changedLeader)}
            </tbody>
          </table>
        </div>
      `}
    `
  }

  const renderCluster = cluster => {
    initializeCollapseState(cluster)

    const actionState = getActionState()
    const pods = podsFor(cluster)
    const currentLeader = pods.find(pod => pod.leader && pod.leaderLabel === 'Leader') || pods.find(pod => pod.leader)
    const changedLeader = pendingLeaderHighlights.get(cluster.id)
    const isDownstream = cluster.type === 'downstream'
    const status = statusFor(cluster)
    const clusterCollapsed = collapsedClusters.get(cluster.id) === true
    const toggleText = clusterCollapsed ? 'Show details' : 'Hide details'
    const version = cluster.version ? ` <span class="text-zinc-500 dark:text-zinc-400">(${escapeHtml(cluster.version)})</span>` : ''
    const typeBadge = badge(isDownstream ? 'Downstream' : 'Local')
    const contextParts = isDownstream
      ? [`Downstream from HA ${cluster.haIndex}`]
      : [`Management cluster for HA ${cluster.haIndex}`]

    if (isDownstream && cluster.namespace) {
      contextParts.push(`namespace ${cluster.namespace}`)
    }
    if (isDownstream && cluster.managementClusterId) {
      contextParts.push(cluster.managementClusterId)
    }

    const rancherURL = cluster.rancherUrl
      ? `<a href="${escapeHtml(cluster.rancherUrl)}" data-external-url="${escapeHtml(cluster.rancherUrl)}" class="text-emerald-600 hover:text-emerald-500 dark:text-emerald-300">${escapeHtml(cluster.rancherUrl)}</a>`
      : '<span class="text-zinc-500 dark:text-zinc-400">Unavailable</span>'
    const loadBalancer = cluster.loadBalancer ? escapeHtml(cluster.loadBalancer) : '<span class="text-zinc-500 dark:text-zinc-400">Unavailable</span>'
    const openingKubeconfigPath = actionState.activeOpenKubeconfigPathClusterId === cluster.id
    const copyingKubeconfigPath = actionState.activeCopyKubeconfigPathClusterId === cluster.id
    const openPathFeedback = kubeconfigPathActionFeedback.get(pathActionKey('open', cluster.id))
    const copyPathFeedback = kubeconfigPathActionFeedback.get(pathActionKey('copy', cluster.id))
    const pathButtonBaseClass = 'inline-flex min-h-8 items-center justify-center rounded-md border px-2.5 py-1.5 text-xs font-semibold disabled:cursor-default disabled:opacity-70'
    const openPathToneClass = openPathFeedback === 'error'
      ? 'border-rose-200 bg-rose-50 text-rose-700 dark:border-rose-500/25 dark:bg-rose-500/10 dark:text-rose-200'
      : openPathFeedback === 'success'
        ? 'border-emerald-200 bg-emerald-50 text-emerald-700 dark:border-emerald-500/25 dark:bg-emerald-500/10 dark:text-emerald-200'
        : 'border-zinc-200 bg-white text-zinc-700 hover:bg-zinc-50 dark:border-white/10 dark:bg-white/[0.06] dark:text-zinc-200 dark:hover:bg-white/[0.1]'
    const copyPathToneClass = copyPathFeedback === 'error'
      ? 'border-rose-200 bg-rose-50 text-rose-700 dark:border-rose-500/25 dark:bg-rose-500/10 dark:text-rose-200'
      : copyPathFeedback === 'success'
        ? 'border-emerald-200 bg-emerald-50 text-emerald-700 dark:border-emerald-500/25 dark:bg-emerald-500/10 dark:text-emerald-200'
        : 'border-zinc-200 bg-white text-zinc-700 hover:bg-zinc-50 dark:border-white/10 dark:bg-white/[0.06] dark:text-zinc-200 dark:hover:bg-white/[0.1]'
    const kubeconfig = cluster.kubeconfigPath ? `
      <div class="space-y-2">
        <div>${escapeHtml(cluster.kubeconfigPath)}</div>
        <div class="flex flex-wrap gap-2">
          <button type="button" data-action="open-kubeconfig-folder" data-cluster="${escapeHtml(cluster.id)}"${openingKubeconfigPath ? ' disabled' : ''} class="${pathButtonBaseClass} ${openPathToneClass}">${kubeconfigPathActionContent(cluster.id, 'open', 'Open folder', 'Opening', 'Opened', 'Open failed')}</button>
          <button type="button" data-action="copy-kubeconfig-path" data-cluster="${escapeHtml(cluster.id)}"${copyingKubeconfigPath ? ' disabled' : ''} class="${pathButtonBaseClass} ${copyPathToneClass}">${kubeconfigPathActionContent(cluster.id, 'copy', 'Copy path', 'Copying', 'Copied', 'Copy failed')}</button>
        </div>
      </div>
    ` : '<span class="text-zinc-500 dark:text-zinc-400">Generated on download</span>'
    const namespace = cluster.namespace ? metaItem('Namespace', escapeHtml(cluster.namespace)) : ''
    const clusterID = cluster.managementClusterId ? metaItem('Cluster ID', escapeHtml(cluster.managementClusterId)) : ''
    const leaderSummary = currentLeader
      ? `<div class="mt-4 text-sm text-zinc-600 dark:text-zinc-400"><strong class="text-zinc-950 dark:text-zinc-100">Active Leader</strong> ${escapeHtml(currentLeader.name)}</div>`
      : '<div class="mt-4 text-sm text-zinc-500 dark:text-zinc-400">Leader not detected yet.</div>'
    const downstreamClasses = isDownstream
      ? 'border-l-4 border-l-emerald-500 bg-emerald-50/50 dark:bg-emerald-500/[0.04]'
      : 'bg-white dark:bg-white/[0.03]'

    return `
      <article class="min-w-0 overflow-hidden rounded-2xl border border-zinc-200 ${downstreamClasses} p-4 shadow-sm dark:border-white/10">
        <div class="flex min-w-0 flex-col gap-4 lg:flex-row lg:items-start lg:justify-between">
          <div class="min-w-0">
            <div class="flex flex-wrap items-center gap-2 text-lg font-semibold tracking-tight text-zinc-950 dark:text-zinc-50">
              <span>${escapeHtml(cluster.name)}${version}</span>
              ${typeBadge}
            </div>
            <div class="mt-1 break-words text-sm font-medium text-zinc-500 dark:text-zinc-400">${escapeHtml(contextParts.join(' • '))}</div>
          </div>
          <div class="flex min-w-0 flex-wrap items-center gap-2 lg:max-w-sm lg:justify-end">
            ${renderKubeconfigActions(cluster)}
            <button type="button" data-action="toggle-cluster" data-cluster="${escapeHtml(cluster.id)}" class="rounded-lg border border-zinc-200 bg-white px-3 py-2 text-sm font-semibold text-zinc-700 hover:bg-zinc-50 dark:border-white/10 dark:bg-white/[0.06] dark:text-zinc-200 dark:hover:bg-white/[0.1]">${toggleText}</button>
            <span class="inline-flex items-center rounded-full px-3 py-1.5 text-xs font-semibold ${status.className}">${cluster.provisioning ? '<span class="spinner mr-2"></span>' : ''}${status.label}</span>
          </div>
        </div>
        ${clusterCollapsed ? '' : `
          <div class="mt-4 grid min-w-0 gap-3 sm:grid-cols-2 xl:grid-cols-[repeat(3,minmax(0,1fr))]">
            ${metaItem('Rancher URL', rancherURL)}
            ${metaItem('Load Balancer', loadBalancer)}
            ${metaItem('Kubeconfig', kubeconfig)}
            ${namespace}
            ${clusterID}
          </div>
          ${renderGPUWorkerPanel(cluster)}
          ${leaderSummary}
          ${renderPodsTable(cluster, pods, changedLeader)}
        `}
      </article>
    `
  }

  const renderClusters = state => {
    const cleanup = state?.cleanup || {}

    if (cleanup.running) {
      clustersEl.innerHTML = `
        <div class="rounded-2xl border border-sky-200 bg-sky-50 p-6 text-center dark:border-sky-500/20 dark:bg-sky-500/10">
          <div class="mx-auto flex h-12 w-12 items-center justify-center rounded-full bg-sky-100 text-sky-700 dark:bg-sky-500/15 dark:text-sky-300">
            <span class="spinner"></span>
          </div>
          <h3 class="mt-4 text-lg font-semibold tracking-tight text-sky-950 dark:text-sky-100">Infrastructure is being torn down</h3>
          <p class="mx-auto mt-2 max-w-2xl text-sm leading-6 text-sky-800/80 dark:text-sky-200/80">
            Destroy is removing Terraform resources for the selected run. Cluster details are paused so the panel does not show stale unavailable infrastructure.
          </p>
          <button type="button" data-action="open-cleanup-logs" class="mt-4 rounded-lg border border-sky-200 bg-white px-4 py-2 text-sm font-semibold text-sky-800 shadow-sm hover:bg-sky-50 dark:border-sky-500/30 dark:bg-white/[0.06] dark:text-sky-200 dark:hover:bg-white/[0.1]">Open destroy logs</button>
        </div>
      `
      return
    }

    const items = clusterItems(state)

    if (!items.length) {
      if (cleanup.finishedAt && !cleanup.error && !cleanupResultDismissed(cleanup)) {
        clustersEl.innerHTML = `
          <div class="rounded-xl border border-emerald-200 bg-emerald-50 p-4 text-sm font-medium text-emerald-800 dark:border-emerald-500/20 dark:bg-emerald-500/10 dark:text-emerald-200">
            Destroy finished for the selected run. Cluster records were cleared after Terraform destroy succeeded.
          </div>
        `
        return
      }
      clustersEl.innerHTML = '<div class="rounded-xl border border-zinc-200 bg-zinc-50 p-4 text-sm text-zinc-600 dark:border-white/10 dark:bg-white/[0.04] dark:text-zinc-400">No clusters discovered yet.</div>'
      return
    }

    const groups = buildClusterGroups(items, state?.workspace)
    if (!groups.length) {
      clustersEl.innerHTML = '<div class="rounded-xl border border-zinc-200 bg-zinc-50 p-4 text-sm text-zinc-600 dark:border-white/10 dark:bg-white/[0.04] dark:text-zinc-400">No clusters discovered yet.</div>'
      return
    }

    const selection = getActiveSelection()
    if (!selection.runKey || !groups.some(group => group.runKey === selection.runKey)) {
      selection.runKey = groups[0].runKey
    }
    const activeRunGroup = groups.find(group => group.runKey === selection.runKey) || groups[0]
    if (!selection.haKey || !activeRunGroup.has.some(ha => ha.haKey === selection.haKey)) {
      selection.haKey = activeRunGroup.has[0]?.haKey || ''
    }
    setActiveSelection(selection)
    const activeHA = activeRunGroup.has.find(ha => ha.haKey === selection.haKey) || activeRunGroup.has[0]

    const runTabs = groups.map(group => {
      const active = group.runKey === activeRunGroup.runKey
      const clusterCount = group.has.reduce((count, ha) => count + (ha.local ? 1 : 0) + ha.downstreams.length, 0)
      return `
        <button type="button" data-action="select-cluster-run" data-run-key="${escapeHtml(group.runKey)}" class="${active ? 'rounded-md border border-emerald-200 bg-emerald-50 px-3 py-1.5 text-sm font-semibold text-emerald-800 shadow-sm dark:border-emerald-500/25 dark:bg-emerald-500/15 dark:text-emerald-200' : 'rounded-md border border-zinc-200 bg-white px-3 py-1.5 text-sm font-semibold text-zinc-700 shadow-sm hover:bg-zinc-50 dark:border-white/10 dark:bg-white/[0.06] dark:text-zinc-200 dark:hover:bg-white/[0.1]'}">
          ${escapeHtml(group.label)}
          <span class="${active ? 'ml-2 text-emerald-600 dark:text-emerald-300' : 'ml-2 text-zinc-500 dark:text-zinc-400'}">${clusterCount}</span>
        </button>
      `
    }).join('')

    const haTabs = activeRunGroup.has.map(ha => {
      const active = ha.haKey === activeHA?.haKey
      const downstreamCount = ha.downstreams.length
      const version = ha.local?.version ? ` • ${ha.local.version}` : ''
      return `
        <button type="button" data-action="select-cluster-ha" data-ha-key="${escapeHtml(ha.haKey)}" class="${active ? 'rounded-md border border-emerald-200 bg-emerald-50 px-3 py-1.5 text-sm font-semibold text-emerald-800 shadow-sm dark:border-emerald-500/25 dark:bg-emerald-500/15 dark:text-emerald-200' : 'rounded-md border border-zinc-200 bg-white px-3 py-1.5 text-sm font-semibold text-zinc-700 shadow-sm hover:bg-zinc-50 dark:border-white/10 dark:bg-white/[0.06] dark:text-zinc-200 dark:hover:bg-white/[0.1]'}">
          HA ${escapeHtml(ha.haIndex || ha.haKey)}${escapeHtml(version)}
          <span class="${active ? 'ml-2 text-emerald-600 dark:text-emerald-300' : 'ml-2 text-zinc-500 dark:text-zinc-400'}">${downstreamCount} downstream</span>
        </button>
      `
    }).join('')

    const localCluster = activeHA?.local
    const downstreams = activeHA?.downstreams || []
    const localHTML = localCluster
      ? renderCluster(localCluster)
      : '<div class="rounded-xl border border-zinc-200 bg-zinc-50 p-4 text-sm text-zinc-600 dark:border-white/10 dark:bg-white/[0.04] dark:text-zinc-400">No local cluster record found for this HA yet.</div>'
    const downstreamHTML = downstreams.length
      ? downstreams.map(renderCluster).join('')
      : '<div class="rounded-xl border border-zinc-200 bg-zinc-50 p-4 text-sm text-zinc-600 dark:border-white/10 dark:bg-white/[0.04] dark:text-zinc-400">No downstream clusters discovered for this HA yet.</div>'

    clustersEl.innerHTML = `
      <div class="grid gap-4">
        <div class="rounded-lg border border-zinc-200 bg-zinc-50 p-3 dark:border-white/10 dark:bg-white/[0.03]">
          <div class="text-xs font-semibold uppercase tracking-wide text-zinc-500 dark:text-zinc-400">Run slot</div>
          <div class="mt-2 flex flex-wrap gap-2">${runTabs}</div>
        </div>
        <div class="rounded-lg border border-zinc-200 bg-zinc-50 p-3 dark:border-white/10 dark:bg-white/[0.03]">
          <div class="text-xs font-semibold uppercase tracking-wide text-zinc-500 dark:text-zinc-400">HA cluster</div>
          <div class="mt-2 flex flex-wrap gap-2">${haTabs}</div>
        </div>
        <div class="grid gap-4">
          <div>
            <div class="mb-2 text-sm font-semibold text-zinc-950 dark:text-zinc-100">Management cluster</div>
            ${localHTML}
          </div>
          <div>
            <div class="mb-2 text-sm font-semibold text-zinc-950 dark:text-zinc-100">Downstream clusters</div>
            <div class="grid gap-4">${downstreamHTML}</div>
          </div>
        </div>
      </div>
    `
  }

  const updateLeaderTracking = state => {
    const messages = []
    const nextLeaders = new Map()

    clusterItems(state).forEach(cluster => {
      const pods = podsFor(cluster)
      const currentLeader = pods.find(pod => pod.leader && pod.leaderLabel === 'Leader') || pods.find(pod => pod.leader)
      const currentLeaderName = currentLeader ? currentLeader.name : ''
      const previousLeaderName = previousLeaders.get(cluster.id) || ''

      if (currentLeaderName) {
        nextLeaders.set(cluster.id, currentLeaderName)
      }

      if (currentLeaderName && previousLeaderName && previousLeaderName !== currentLeaderName) {
        pendingLeaderHighlights.set(cluster.id, currentLeaderName)
        window.setTimeout(() => {
          if (pendingLeaderHighlights.get(cluster.id) === currentLeaderName) {
            pendingLeaderHighlights.delete(cluster.id)
          }
        }, 4500)
        messages.push(`${cluster.name} leader changed to ${currentLeaderName}`)
      }
    })

    previousLeaders = nextLeaders
    return messages.join(' • ')
  }

  const toggleCluster = clusterId => {
    collapsedClusters.set(clusterId, collapsedClusters.get(clusterId) !== true)
    renderLastState()
  }

  const togglePods = clusterId => {
    collapsedPods.set(clusterId, collapsedPods.get(clusterId) !== true)
    renderLastState()
  }

  const toggleGPUCommands = clusterId => {
    collapsedGPUCommands.set(clusterId, collapsedGPUCommands.get(clusterId) !== true)
    renderLastState()
  }

  const flashGPUCommandCopy = (clusterId, commandIndex, status) => {
    const key = `${clusterId}:${commandIndex}`
    window.clearTimeout(gpuCommandCopyTimers.get(key))
    gpuCommandCopyFeedback.set(key, status)
    renderLastState()
    gpuCommandCopyTimers.set(key, window.setTimeout(() => {
      if (gpuCommandCopyFeedback.get(key) === status) {
        gpuCommandCopyFeedback.delete(key)
        renderLastState()
      }
    }, 1800))
  }

  return {
    flashKubeconfigPathAction,
    flashGPUCommandCopy,
    renderClusters,
    toggleCluster,
    toggleGPUCommands,
    togglePods,
    updateLeaderTracking
  }
}
