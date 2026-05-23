<template>
  <article class="min-w-0 overflow-hidden rounded-2xl border border-zinc-200 p-4 shadow-sm dark:border-white/10"
    :class="isDownstream ? 'border-l-4 border-l-emerald-500 bg-emerald-50/50 dark:bg-emerald-500/[0.04]' : (isHostedTenant && cluster.role === 'host' ? 'border-l-4 border-l-sky-500 bg-sky-50/50 dark:bg-sky-500/[0.04]' : 'bg-white dark:bg-white/[0.03]')"
  >
    <div class="flex min-w-0 flex-col gap-4 lg:flex-row lg:items-start lg:justify-between">
      <div class="min-w-0">
        <div class="flex flex-wrap items-center gap-2 text-lg font-semibold tracking-tight text-zinc-950 dark:text-zinc-50">
          <span>{{ cluster.name }} <span v-if="cluster.version" class="text-zinc-500 dark:text-zinc-400">({{ cluster.version }})</span></span>
          <span class="inline-flex items-center rounded-md bg-zinc-100 px-2 py-1 text-xs font-semibold text-zinc-600 dark:bg-white/[0.06] dark:text-zinc-300">
            {{ isDownstream ? 'Downstream' : (isHostedTenant ? 'Host' : (isLinodeDocker ? 'Linode Docker' : 'Local')) }}
          </span>
        </div>
        <div class="mt-1 break-words text-sm font-medium text-zinc-500 dark:text-zinc-400">
          <template v-if="isDownstream">Downstream from HA {{ cluster.haIndex }}<template v-if="cluster.namespace"> • namespace {{ cluster.namespace }}</template><template v-if="cluster.managementClusterId"> • {{ cluster.managementClusterId }}</template></template>
          <template v-else-if="isHostedTenant">Hosted-tenant K3s</template>
          <template v-else-if="isLinodeDocker">Docker Rancher on Linode {{ cluster.haIndex }}</template>
          <template v-else>Management cluster for HA {{ cluster.haIndex }}</template>
        </div>
      </div>
      <div class="flex min-w-0 flex-wrap items-center gap-2 lg:max-w-sm lg:justify-end">
        <template v-if="isLinodeDocker">
          <span class="text-sm text-zinc-500 dark:text-zinc-400">No kubeconfig for Docker install</span>
          <button type="button" @click="loadDockerLogs(cluster)" class="inline-flex min-h-11 items-center justify-center rounded-lg border border-zinc-200 bg-white px-4 py-2 text-sm font-semibold text-zinc-700 hover:bg-zinc-50 dark:border-white/10 dark:bg-white/[0.06] dark:text-zinc-200 dark:hover:bg-white/[0.1]">Docker logs</button>
        </template>
        <template v-else-if="!cluster.available">
          <span class="text-sm text-zinc-500 dark:text-zinc-400">Kubeconfig unavailable</span>
        </template>
        <template v-else>
          <button type="button" @click="downloadKubeconfig(cluster.id)" :disabled="activeDownloadClusterId === cluster.id" class="inline-flex min-h-11 max-w-full items-center justify-center whitespace-normal rounded-lg bg-emerald-500 px-4 py-2 text-center text-sm font-semibold text-white shadow-sm shadow-emerald-500/20 hover:bg-emerald-400">
            <span v-if="activeDownloadClusterId === cluster.id" class="spinner mr-2"></span>
            {{ activeDownloadClusterId === cluster.id ? 'Downloading...' : 'Download kubeconfig' }}
          </button>
          <button type="button" @click="copyKubeconfig(cluster.id)" :disabled="activeCopyClusterId === cluster.id" class="inline-flex min-h-11 items-center justify-center rounded-lg border border-zinc-200 bg-white px-4 py-2 text-sm font-semibold text-zinc-700 hover:bg-zinc-50 dark:border-white/10 dark:bg-white/[0.06] dark:text-zinc-200 dark:hover:bg-white/[0.1]">
            <span v-if="activeCopyClusterId === cluster.id" class="spinner mr-2"></span>
            {{ activeCopyClusterId === cluster.id ? 'Copying...' : 'Copy kubeconfig' }}
          </button>
          <div v-if="cluster.type === 'local'" class="flex max-w-full flex-wrap gap-2 rounded-xl border border-zinc-200 bg-zinc-50 p-2 dark:border-white/10 dark:bg-white/[0.03]">
            <button type="button" @click="copyHelmInstallCommand(cluster.id, 'install')" :disabled="activeCopyHelmClusterId === cluster.id" class="inline-flex min-h-9 max-w-full items-center justify-center whitespace-normal rounded-lg border border-zinc-200 bg-white px-4 py-2 text-center text-sm font-semibold text-zinc-700 hover:bg-zinc-50 dark:border-white/10 dark:bg-white/[0.06] dark:text-zinc-200 dark:hover:bg-white/[0.1]">
              <span v-if="activeCopyHelmClusterId === cluster.id" class="spinner mr-2"></span>
              {{ activeCopyHelmClusterId === cluster.id ? 'Copying...' : 'Copy install command' }}
            </button>
            <button type="button" @click="copyHelmInstallCommand(cluster.id, 'upgrade')" :disabled="activeCopyHelmUpgradeClusterId === cluster.id" class="inline-flex min-h-9 max-w-full items-center justify-center whitespace-normal rounded-lg border border-sky-200 bg-sky-50 px-4 py-2 text-center text-sm font-semibold text-sky-800 hover:bg-sky-100 dark:border-sky-500/25 dark:bg-sky-500/10 dark:text-sky-200 dark:hover:bg-sky-500/15">
              <span v-if="activeCopyHelmUpgradeClusterId === cluster.id" class="spinner mr-2"></span>
              {{ activeCopyHelmUpgradeClusterId === cluster.id ? 'Copying...' : 'Copy upgrade draft' }}
            </button>
          </div>
        </template>

        <button type="button" @click="toggleCluster" class="rounded-lg border border-zinc-200 bg-white px-3 py-2 text-sm font-semibold text-zinc-700 hover:bg-zinc-50 dark:border-white/10 dark:bg-white/[0.06] dark:text-zinc-200 dark:hover:bg-white/[0.1]">
          {{ isClusterCollapsed ? 'Show details' : 'Hide details' }}
        </button>
        <span class="inline-flex items-center rounded-full px-3 py-1.5 text-xs font-semibold" :class="statusFor(cluster).className">
          <span v-if="cluster.provisioning" class="spinner mr-2"></span>{{ statusFor(cluster).label }}
        </span>
      </div>
    </div>

    <!-- Collapsible Detailed Meta Panel -->
    <div v-if="!isClusterCollapsed" class="mt-4 border-t border-zinc-100 pt-4 dark:border-white/5">
      <div class="grid min-w-0 gap-3 sm:grid-cols-2 xl:grid-cols-3">
        <!-- Rancher URL -->
        <div class="min-w-0">
          <div class="text-xs font-semibold uppercase tracking-wide text-zinc-500 dark:text-zinc-400">Rancher URL</div>
          <div class="mt-1 break-words text-sm font-medium text-zinc-800 dark:text-zinc-200">
            <a v-if="cluster.rancherUrl" href="#" @click.prevent="openExternalURL(cluster.rancherUrl)" class="text-emerald-600 hover:text-emerald-500 dark:text-emerald-300">{{ cluster.rancherUrl }}</a>
            <span v-else class="text-zinc-500 dark:text-zinc-400">Unavailable</span>
          </div>
        </div>

        <!-- LB or Linode IP -->
        <div class="min-w-0">
          <div class="text-xs font-semibold uppercase tracking-wide text-zinc-500 dark:text-zinc-400">
            {{ isLinodeDocker ? 'Linode IP' : 'Load Balancer' }}
          </div>
          <div class="mt-1 break-words text-sm font-medium text-zinc-800 dark:text-zinc-200">
            <template v-if="isLinodeDocker && cluster.loadBalancer">
              <div class="flex flex-wrap items-center gap-2">
                <span>{{ cluster.loadBalancer }}</span>
                <button type="button" @click="handleCopyLinodeIP(cluster)" :class="pathActionFeedbackClass('copy-linode-ip')" class="inline-flex min-h-8 items-center justify-center rounded-md border px-2.5 py-1.5 text-xs font-semibold">Copy</button>
              </div>
            </template>
            <template v-else-if="cluster.loadBalancer">{{ cluster.loadBalancer }}</template>
            <template v-else-if="isHostedTenant"><span class="text-zinc-500 dark:text-zinc-400">Managed by hosted-tenant ALB</span></template>
            <template v-else><span class="text-zinc-500 dark:text-zinc-400">Unavailable</span></template>
          </div>
        </div>

        <!-- Kubeconfig Path -->
        <div v-if="!isLinodeDocker" class="min-w-0">
          <div class="text-xs font-semibold uppercase tracking-wide text-zinc-500 dark:text-zinc-400">Kubeconfig</div>
          <div class="mt-1 break-words text-sm font-medium text-zinc-800 dark:text-zinc-200">
            <template v-if="cluster.kubeconfigPath">
              <div class="space-y-2">
                <div class="truncate" :title="cluster.kubeconfigPath">{{ cluster.kubeconfigPath }}</div>
                <div class="flex flex-wrap gap-2">
                  <button type="button" @click="handleOpenKubeconfigFolder(cluster)" :class="pathActionFeedbackClass('open')" class="inline-flex min-h-8 items-center justify-center rounded-md border px-2.5 py-1.5 text-xs font-semibold">Open folder</button>
                  <button type="button" @click="handleCopyKubeconfigPath(cluster)" :class="pathActionFeedbackClass('copy')" class="inline-flex min-h-8 items-center justify-center rounded-md border px-2.5 py-1.5 text-xs font-semibold">Copy path</button>
                </div>
              </div>
            </template>
            <template v-else><span class="text-zinc-500 dark:text-zinc-400">Generated on download</span></template>
          </div>
        </div>

        <!-- GPU Worker IP -->
        <div v-if="hasGpuWorker" class="min-w-0">
          <div class="text-xs font-semibold uppercase tracking-wide text-zinc-500 dark:text-zinc-400">GPU worker</div>
          <div class="mt-1 break-words text-sm font-medium text-zinc-800 dark:text-zinc-200">
            <div class="space-y-1">
              <div>{{ cluster.gpuWorkerIp }}</div>
              <div class="text-xs text-zinc-500 dark:text-zinc-400">{{ cluster.gpuWorkerInstanceType || 'GPU worker' }} RKE2 agent<template v-if="cluster.gpuWorkerPrivateIp"> • private {{ cluster.gpuWorkerPrivateIp }}</template></div>
            </div>
          </div>
        </div>

        <!-- Namespace -->
        <div v-if="cluster.namespace" class="min-w-0">
          <div class="text-xs font-semibold uppercase tracking-wide text-zinc-500 dark:text-zinc-400">Namespace</div>
          <div class="mt-1 break-words text-sm font-medium text-zinc-800 dark:text-zinc-200">{{ cluster.namespace }}</div>
        </div>

        <!-- Cluster ID -->
        <div v-if="cluster.managementClusterId" class="min-w-0">
          <div class="text-xs font-semibold uppercase tracking-wide text-zinc-500 dark:text-zinc-400">Cluster ID</div>
          <div class="mt-1 break-words text-sm font-medium text-zinc-800 dark:text-zinc-200">{{ cluster.managementClusterId }}</div>
        </div>
      </div>

      <!-- Leader tracking summary -->
      <div v-if="!isLinodeDocker" class="mt-4 border-t border-zinc-100 pt-4 dark:border-white/5">
        <div v-if="currentLeader" class="text-sm text-zinc-600 dark:text-zinc-400">
          <strong class="text-zinc-950 dark:text-zinc-100">Active Leader</strong> {{ currentLeader.name }}
        </div>
        <div v-else class="text-sm text-zinc-500 dark:text-zinc-400">Leader not detected yet.</div>
      </div>

      <!-- GPU Worker Commands List -->
      <div v-if="hasGpuWorker" class="mt-4 overflow-hidden rounded-xl border border-rose-200 bg-rose-50 text-zinc-950 dark:border-rose-500/30 dark:bg-rose-950/30 dark:text-zinc-50">
        <div class="flex flex-wrap items-center justify-between gap-3 px-4 py-3">
          <span>
            <span class="block text-sm font-semibold text-rose-900 dark:text-rose-100">Active GPU worker node in slot {{ cluster.runId || 'current' }}</span>
            <span class="mt-1 block text-sm leading-6 text-rose-800/85 dark:text-rose-100/75">{{ cluster.gpuWorkerInstanceType }} at {{ cluster.gpuWorkerIp }}<template v-if="cluster.gpuWorkerSubnetId"> in {{ cluster.gpuWorkerSubnetId }}</template><template v-if="cluster.gpuWorkerAmi"> using {{ cluster.gpuWorkerAmi }}</template>. Do not leave running unused.</span>
          </span>
          <button type="button" @click="toggleGPUCommands" class="inline-flex min-h-10 items-center justify-center rounded-lg bg-rose-500 px-3.5 py-2 text-sm font-semibold text-white shadow-sm shadow-rose-500/20 hover:bg-rose-400">
            {{ isGPUCommandsCollapsed ? 'Show GPU setup commands' : 'Hide GPU setup commands' }}
          </button>
        </div>
        <div v-if="!isGPUCommandsCollapsed" class="border-t border-rose-200 p-4 dark:border-rose-500/25">
          <div class="mb-3 text-sm leading-6 text-rose-900 dark:text-rose-100">
            Run these in order to prepare the RKE2 GPU worker, run Ollama on it, install the Rancher AI agent, and open the Rancher UI extension step.
          </div>
          <div class="grid gap-3">
            <div v-for="(cmd, idx) in gpuCommands" :key="idx" class="grid gap-3 rounded-lg border border-zinc-200 bg-white p-3 shadow-sm dark:border-white/10 dark:bg-zinc-950/70">
              <div class="flex min-w-0 flex-wrap items-start justify-between gap-3">
                <div class="min-w-0">
                  <div class="flex flex-wrap items-center gap-2">
                    <span class="text-sm font-semibold text-zinc-950 dark:text-zinc-50">{{ cmd.title }}</span>
                    <span class="rounded-full px-2 py-0.5 text-[11px] font-bold uppercase tracking-wide"
                      :class="cmd.tone === 'Required' ? 'bg-rose-100 text-rose-800 dark:bg-rose-500/15 dark:text-rose-200' : (cmd.tone === 'Verify' ? 'bg-emerald-100 text-emerald-800 dark:bg-emerald-500/15 dark:text-emerald-200' : (cmd.tone === 'Manual' ? 'bg-violet-100 text-violet-800 dark:bg-violet-500/15 dark:text-violet-200' : 'bg-sky-100 text-sky-800 dark:bg-sky-500/15 dark:text-sky-200'))"
                    >{{ cmd.tone }}</span>
                  </div>
                  <div class="mt-1 text-xs leading-5 text-zinc-600 dark:text-zinc-300">{{ cmd.detail }}</div>
                </div>
                <button type="button" @click="handleCopyGPUCommand(idx, cmd.command)"
                  class="inline-flex min-h-9 items-center justify-center rounded-md border px-3 py-1.5 text-xs font-semibold"
                  :class="gpuCommandCopyFeedback.get(cluster.id + ':' + idx) === 'success' ? 'border-emerald-300 bg-emerald-50 text-emerald-700 dark:border-emerald-500/30 dark:bg-emerald-500/15 dark:text-emerald-200' : 'border-zinc-300 bg-white text-zinc-700 hover:bg-zinc-50 dark:border-white/15 dark:bg-white/[0.08] dark:text-zinc-100 dark:hover:bg-white/[0.12]'"
                >
                  {{ gpuCommandCopyFeedback.get(cluster.id + ':' + idx) === 'success' ? 'Copied' : 'Copy' }}
                </button>
              </div>
              <pre class="max-w-full overflow-auto whitespace-pre rounded-md border border-zinc-200 bg-zinc-50 p-3 font-mono text-xs leading-6 text-zinc-900 dark:border-white/10 dark:bg-black/35 dark:text-zinc-100"><code>{{ cmd.command }}</code></pre>
            </div>
          </div>
        </div>
      </div>

      <!-- Pods Table List -->
      <div v-if="!isLinodeDocker && !cluster.provisioning" class="mt-4 border-t border-zinc-100 pt-4 dark:border-white/5">
        <div class="flex items-center justify-between gap-3">
          <div class="text-sm font-semibold text-zinc-950 dark:text-zinc-100">Pods <span class="text-zinc-500 dark:text-zinc-400">{{ pods.length }}</span></div>
          <button type="button" @click="togglePods" class="rounded-lg border border-zinc-200 bg-white px-3 py-2 text-xs font-semibold text-zinc-700 hover:bg-zinc-50 dark:border-white/10 dark:bg-white/[0.06] dark:text-zinc-200 dark:hover:bg-white/[0.1]">
            {{ isPodsCollapsed ? 'Show pods' : 'Hide pods' }}
          </button>
        </div>
        <div v-if="!isPodsCollapsed" class="mt-3 max-w-full overflow-hidden rounded-xl border border-zinc-200 dark:border-white/10">
          <div class="overflow-x-auto">
            <table class="w-full min-w-[70rem] table-fixed border-collapse text-left">
              <colgroup>
                <col class="w-[9rem]" />
                <col class="w-[20rem]" />
                <col class="w-[5rem]" />
                <col class="w-[7rem]" />
                <col class="w-[6rem]" />
                <col class="w-[5rem]" />
                <col class="w-[12rem]" />
                <col />
              </colgroup>
              <thead class="bg-zinc-50 dark:bg-white/[0.04]">
                <tr class="border-b border-zinc-200 dark:border-white/10">
                  <th v-for="h in ['Namespace', 'Pod', 'Ready', 'Status', 'Restarts', 'Age', 'Node', 'Containers']" :key="h" class="px-3 py-2 text-xs font-semibold uppercase tracking-wide text-zinc-500 dark:text-zinc-400">{{ h }}</th>
                </tr>
              </thead>
              <tbody class="divide-y divide-zinc-200 dark:divide-white/10">
                <tr v-if="!pods.length">
                  <td colspan="8" class="px-3 py-4 text-sm text-zinc-500 dark:text-zinc-400">
                    {{ cluster.error ? cluster.error : emptyPodsText(cluster) }}
                  </td>
                </tr>
                <tr v-for="pod in pods" :key="pod.name"
                  :class="isHighlight && pod.name === currentLeader?.name ? 'bg-emerald-50 dark:bg-emerald-500/10' : (pod.leader ? 'bg-emerald-50/70 dark:bg-emerald-500/5' : '')"
                >
                  <td class="break-words px-3 py-3 align-top text-sm text-zinc-600 dark:text-zinc-400">{{ pod.namespace || '' }}</td>
                  <td class="px-3 py-3 align-top">
                    <div class="flex flex-wrap items-center gap-2 text-sm font-semibold text-zinc-900 dark:text-zinc-100">
                      <span>{{ pod.name }}</span>
                      <span v-if="pod.leader && pod.leaderLabel" class="inline-flex items-center rounded-md bg-zinc-100 px-2 py-1 text-xs font-semibold text-zinc-600 dark:bg-white/[0.06] dark:text-zinc-300">{{ pod.leaderLabel }}</span>
                    </div>
                    <div class="mt-2 flex flex-wrap gap-2">
                      <button type="button" @click="loadLogs(cluster.id, pod.namespace || 'cattle-system', pod.name)" class="rounded-lg border border-zinc-200 bg-white px-3 py-1.5 text-xs font-semibold text-zinc-700 hover:bg-zinc-50 dark:border-white/10 dark:bg-white/[0.06] dark:text-zinc-200 dark:hover:bg-white/[0.1]">Tail</button>
                      <button type="button" @click="streamLogs(cluster.id, pod.namespace || 'cattle-system', pod.name)" class="rounded-lg border border-zinc-200 bg-white px-3 py-1.5 text-xs font-semibold text-zinc-700 hover:bg-zinc-50 dark:border-white/10 dark:bg-white/[0.06] dark:text-zinc-200 dark:hover:bg-white/[0.1]">Live</button>
                    </div>
                  </td>
                  <td class="break-words px-3 py-3 align-top text-sm text-zinc-700 dark:text-zinc-300">{{ pod.ready }}</td>
                  <td class="break-words px-3 py-3 align-top text-sm text-zinc-700 dark:text-zinc-300">{{ pod.status }}</td>
                  <td class="break-words px-3 py-3 align-top text-sm text-zinc-700 dark:text-zinc-300">{{ pod.restarts }}</td>
                  <td class="break-words px-3 py-3 align-top text-sm text-zinc-700 dark:text-zinc-300">{{ pod.age }}</td>
                  <td class="break-words px-3 py-3 align-top text-sm text-zinc-700 dark:text-zinc-300">{{ pod.node || '' }}</td>
                  <td class="break-words px-3 py-3 align-top text-sm text-zinc-700 dark:text-zinc-300">{{ pod.containers }}</td>
                </tr>
              </tbody>
            </table>
          </div>
        </div>
      </div>

      <div v-else-if="cluster.provisioning" class="mt-4 rounded-xl border border-amber-200 bg-amber-50 px-4 py-3 text-sm font-medium text-amber-800 dark:border-amber-500/20 dark:bg-amber-500/10 dark:text-amber-200">
        <span class="spinner mr-2"></span>{{ cluster.provisioningMessage || 'Provisioning downstream cluster' }}
      </div>
    </div>
  </article>
</template>

<script setup>
import { computed, ref } from "vue";
import {
  apiFetch,
  kubeconfigPathActionFeedback,
  gpuCommandCopyFeedback,
  flashGPUCommandCopy,
  downloadKubeconfig,
  copyKubeconfig,
  openKubeconfigFolder,
  copyKubeconfigPath,
  copyLinodeIP,
  loadDockerLogs,
  copyHelmInstallCommand,
  loadLogs,
  streamLogs,
  copyTextToClipboard,
  pendingLeaderHighlights,
  activeDownloadClusterId,
  activeCopyClusterId,
  activeCopyHelmClusterId,
  activeCopyHelmUpgradeClusterId,
} from "./store.js";
import {
  podsFor,
} from "../../static/control_panel_utils.js";

const props = defineProps({
  cluster: {
    type: Object,
    required: true,
  },
});

const isDownstream = computed(() => props.cluster.type === "downstream");
const isHostedTenant = computed(() => props.cluster.role || props.cluster.deploymentType === "hosted-tenant-k3s");
const isLinodeDocker = computed(() => props.cluster.deploymentType === "linode-docker-cattle");
const pods = computed(() => podsFor(props.cluster));
const currentLeader = computed(() =>
  pods.value.find(p => p.leader && p.leaderLabel === "Leader") || pods.value.find(p => p.leader)
);

const hasGpuWorker = computed(() =>
  !isDownstream.value &&
  !isHostedTenant.value &&
  !isLinodeDocker.value &&
  props.cluster.gpuWorkerIp
);

const gpuCommands = computed(() => {
  if (!hasGpuWorker.value) return [];
  const instanceType = props.cluster.gpuWorkerInstanceType || "GPU instance";
  const kubeconfigPath = props.cluster.kubeconfigPath || "/path/to/kube_config.yaml";
  const recommendedLizModel = instanceType === "p5.4xlarge" ? "gpt-oss:120b" : "gpt-oss:20b";
  const modelDetail = instanceType === "p5.4xlarge"
    ? "The large GPU profile matches the gpt-oss:120b requirement from the Liz quick start. Use gpt-oss:20b instead for a cheaper smoke test."
    : "The standard GPU profile is sized for gpt-oss:20b, the smaller local model in the Liz quick start.";

  return [
    { title: "Use this kubeconfig", tone: "Required", detail: "Sets this terminal session to the local HA cluster.", command: `export KUBECONFIG="${kubeconfigPath}"` },
    { title: "Choose the local Ollama model", tone: "Required", detail: modelDetail, command: `export LIZ_MODEL="${recommendedLizModel}"` },
    { title: "Confirm the GPU worker joined", tone: "Check", detail: "Shows the worker label and EC2 GPU instance type.", command: "kubectl get nodes -L ha-rancher-rke2/gpu-worker -L ha-rancher-rke2/gpu-instance-type" },
    {
      title: "Add the Liz GPU worker label",
      tone: "Required",
      detail: "Marks only this worker as eligible for Ollama and Liz GPU scheduling.",
      command: `GPU_NODE="$(kubectl get nodes -l ha-rancher-rke2/gpu-worker=true -o jsonpath='{.items[0].metadata.name}')"
test -n "$GPU_NODE"
kubectl label node "$GPU_NODE" liz-ai.suse.com/gpu-worker=true --overwrite`,
    },
    {
      title: "Install the NVIDIA GPU Operator for RKE2",
      tone: "Required",
      detail: "Uses the RKE2 HelmChart path, RKE2 containerd socket, and disables driver install because the GPU AMI already provides NVIDIA drivers.",
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
EOF`,
    },
    {
      title: "Watch GPU Operator pods",
      tone: "Verify",
      detail: "Waits for the RKE2 toolkit and device plugin daemonsets before scheduling GPU workloads.",
      command: `until kubectl -n gpu-operator get ds/nvidia-container-toolkit-daemonset ds/nvidia-device-plugin-daemonset >/dev/null 2>&1; do
  sleep 10
done
kubectl -n gpu-operator rollout status ds/nvidia-container-toolkit-daemonset --timeout=30m
kubectl -n gpu-operator rollout status ds/nvidia-device-plugin-daemonset --timeout=30m
kubectl -n gpu-operator get pods -o wide`,
    },
    {
      title: "Verify GPU capacity on the worker",
      tone: "Verify",
      detail: "Stops early if Kubernetes still has not advertised allocatable GPU capacity.",
      command: `GPU_NODE="$(kubectl get nodes -l ha-rancher-rke2/gpu-worker=true -o jsonpath='{.items[0].metadata.name}')"
GPU_COUNT="$(kubectl get node "$GPU_NODE" -o jsonpath='{.status.allocatable.nvidia\\.com/gpu}')"
test "\${GPU_COUNT:-0}" -ge 1
echo "GPU node $GPU_NODE has $GPU_COUNT allocatable NVIDIA GPU(s)."
kubectl describe node "$GPU_NODE" | grep -A8 "nvidia.com/gpu"`,
    },
    {
      title: "Run a CUDA smoke test",
      tone: "Verify",
      detail: "Runs only after allocatable GPU exists, waits for completion, then prints the benchmark output.",
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
kubectl delete pod nbody-gpu-benchmark --ignore-not-found`,
    },
    {
      title: "Deploy Ollama on the GPU worker",
      tone: "Required",
      detail: "Creates an in-cluster Ollama service named ollama in the Rancher AI agent namespace, matching the SUSE quick start URL.",
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
EOF`,
    },
    {
      title: "Pull the model into Ollama",
      tone: "Required",
      detail: "This can take a while; the model is stored in the Ollama pod volume for this test run.",
      command: `kubectl -n cattle-ai-agent-system rollout status deploy/ollama --timeout=15m
kubectl -n cattle-ai-agent-system exec deploy/ollama -- ollama pull "$LIZ_MODEL"
kubectl -n cattle-ai-agent-system exec deploy/ollama -- ollama list`,
    },
    {
      title: "Create Rancher AI agent values",
      tone: "Required",
      detail: "Matches the SUSE quick start values, using the local Ollama service and selected model.",
      command: `cat > /tmp/rancher-ai-values.yaml <<EOF
ollamaLlmModel: "\${LIZ_MODEL:-gpt-oss:20b}"
ollamaUrl: "http://ollama:11434"
activeLlm: "ollama"
EOF`,
    },
    {
      title: "Install the Rancher AI agent",
      tone: "Required",
      detail: "Deploys the agent and MCP chart from the SUSE Rancher AI quick start.",
      command: `helm upgrade --install rancher-ai-agent \\
  --namespace cattle-ai-agent-system \\
  --create-namespace \\
  -f /tmp/rancher-ai-values.yaml \\
  oci://registry.suse.com/rancher/charts/rancher-ai-agent`,
    },
    {
      title: "Verify the Rancher AI backend",
      tone: "Verify",
      detail: "Checks that Ollama and the Rancher AI agent pods are up.",
      command: `kubectl -n cattle-ai-agent-system wait --for=condition=Ready pod -l app=ollama --timeout=10m
kubectl -n cattle-ai-agent-system wait --for=condition=Ready pod -l app.kubernetes.io/instance=rancher-ai-agent --timeout=10m
kubectl -n cattle-ai-agent-system get pods -o wide`,
    },
    {
      title: "Open Rancher to install the UI extension",
      tone: "Manual",
      detail: "Required by the SUSE quick start: Extensions > add official repositories > install AI Assistant > reload Rancher UI.",
      command: props.cluster.rancherUrl ? `open "${props.cluster.rancherUrl}/dashboard/c/local/extensions"` : 'echo "Open Rancher UI > Extensions, add official repositories, install AI Assistant, then reload the Rancher UI."',
    },
  ];
});

const isHighlight = computed(() => pendingLeaderHighlights.value.get(props.cluster.id) === currentLeader.value?.name);

const isClusterCollapsed = ref(props.cluster.type === "downstream");
const isPodsCollapsed = ref(props.cluster.type === "downstream");
const isGPUCommandsCollapsed = ref(false);

const toggleCluster = () => { isClusterCollapsed.value = !isClusterCollapsed.value; };
const togglePods = () => { isPodsCollapsed.value = !isPodsCollapsed.value; };
const toggleGPUCommands = () => { isGPUCommandsCollapsed.value = !isGPUCommandsCollapsed.value; };

const statusFor = cluster => {
  if (cluster.reachable) {
    return {
      label: "Reachable",
      className: "bg-emerald-100 text-emerald-700 dark:bg-emerald-500/15 dark:text-emerald-300",
    };
  }
  if (cluster.provisioning) {
    return {
      label: "Provisioning",
      className: "bg-amber-100 text-amber-800 dark:bg-amber-500/15 dark:text-amber-200",
    };
  }
  if (cluster.available) {
    return {
      label: "Unavailable",
      className: "bg-amber-100 text-amber-800 dark:bg-amber-500/15 dark:text-amber-200",
    };
  }
  return {
    label: "Missing",
    className: "bg-zinc-100 text-zinc-600 dark:bg-white/[0.06] dark:text-zinc-300",
  };
};

const emptyPodsText = cluster => {
  if (cluster.type === "downstream") return "Pods are unavailable until the downstream kubeconfig is reachable.";
  if (cluster.deploymentType === "hosted-tenant-k3s") {
    return "Pods are unavailable until the hosted-tenant kubeconfig exists and kubectl can reach the cluster.";
  }
  if (cluster.deploymentType === "linode-docker-cattle") {
    return "Docker Rancher does not expose Kubernetes pods from this panel.";
  }
  return "Pods are unavailable until kubeconfig exists and kubectl can reach the cluster.";
};

const pathActionFeedbackClass = (action) => {
  const feedback = kubeconfigPathActionFeedback.get(`${action}:${props.cluster.id}`);
  if (feedback === "error") {
    return "border-rose-200 bg-rose-50 text-rose-700 dark:border-rose-500/25 dark:bg-rose-500/10 dark:text-rose-200";
  }
  if (feedback === "success") {
    return "border-emerald-200 bg-emerald-50 text-emerald-700 dark:border-emerald-500/25 dark:bg-emerald-500/10 dark:text-emerald-200";
  }
  return "border-zinc-200 bg-white text-zinc-700 hover:bg-zinc-50 dark:border-white/10 dark:bg-white/[0.06] dark:text-zinc-200 dark:hover:bg-white/[0.1]";
};

const handleCopyKubeconfigPath = cluster => copyKubeconfigPath(cluster);
const handleOpenKubeconfigFolder = cluster => openKubeconfigFolder(cluster);
const handleCopyLinodeIP = cluster => copyLinodeIP(cluster);

const openExternalURL = async url => {
  if (!url) return;
  try {
    await apiFetch("/api/open-url", {
      method: "POST",
      body: JSON.stringify({ url }),
    });
  } catch (error) {
    window.open(url, "_blank", "noopener,noreferrer");
  }
};

const handleCopyGPUCommand = async (index, command) => {
  const copied = await copyTextToClipboard(command, "Copied GPU setup command to clipboard.");
  flashGPUCommandCopy(props.cluster.id, index, copied ? "success" : "error");
};
</script>
