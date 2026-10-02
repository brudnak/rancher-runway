<script setup>
import { onBeforeUnmount, ref, watch } from 'vue';
const props = defineProps({ image: String, request: { type: Function, required: true } });
const data = ref(null), error = ref(''), loading = ref(false);
let generation = 0;
async function load() {
  const current = ++generation;
  data.value = null; error.value = ''; loading.value = Boolean(props.image);
  if (!props.image) return;
  try {
    const result = await props.request('image-details', { image: props.image });
    if (current === generation) data.value = result;
  } catch (err) { if (current === generation) error.value = err.message; }
  finally { if (current === generation) loading.value = false; }
}
watch(() => props.image, load, { immediate: true });
onBeforeUnmount(() => { generation++; });
</script>
<template>
  <section v-if="image" class="image-evidence" aria-label="Selected image provenance" :aria-busy="loading">
    <h5>Selected image · {{ data?.registry || image.split('/')[0] }}</h5>
    <p class="image-ref">{{ image }}</p>
    <p v-if="loading" role="status">Resolving exact image digest and build metadata…</p>
    <div v-else-if="error" role="status"><p>{{ error }}</p><button type="button" @click="load">Retry image lookup</button></div>
    <template v-else-if="data">
      <dl>
        <dt>Rancher build</dt><dd>{{ data.version || 'Not declared as an exact version' }}</dd>
        <dt>Version evidence</dt><dd>{{ data.versionSource || 'Unavailable' }}</dd>
        <dt>Image digest</dt><dd>{{ data.digest || 'Unavailable' }}</dd>
        <dt>Source commit</dt><dd><a v-if="data.commitURL" :href="data.commitURL" target="_blank" rel="noopener noreferrer">{{ data.revision }}</a><template v-else>{{ data.revision || 'Not declared' }}</template></dd>
        <template v-if="data.ossRevision"><dt>OSS commit</dt><dd>{{ data.ossRevision }}</dd></template>
        <template v-if="data.canonicalReference"><dt>Canonical image</dt><dd>{{ data.canonicalReference }}</dd></template>
        <template v-if="data.versionLabel"><dt>OCI version label</dt><dd>{{ data.versionLabel }}</dd></template>
        <template v-if="data.source"><dt>Source repository</dt><dd>{{ data.source }}</dd></template>
        <template v-if="data.created && !data.created.startsWith('0001-')"><dt>Built</dt><dd>{{ new Date(data.created).toLocaleString() }}</dd></template>
      </dl>
      <p v-if="data.versionNote" class="image-note">{{ data.versionNote }}</p>
      <p>Metadata is for the selected image. Preflight still checks the upgrade path, readiness and matching agent.</p>
    </template>
  </section>
</template>
<style scoped>
.image-evidence{margin:18px 0;padding:16px;border:1px solid var(--up-border);border-radius:12px;background:var(--up-tint);font-size:12px}.image-evidence h5{font-weight:650;margin:0 0 8px}.image-evidence p{color:var(--up-muted);line-height:1.6;margin:8px 0}.image-ref,.image-evidence dd{overflow-wrap:anywhere;font-family:ui-monospace,monospace}.image-evidence dl{display:grid;grid-template-columns:125px minmax(0,1fr);gap:9px 15px;margin:16px 0}.image-evidence dt{color:var(--up-muted)}.image-evidence a{color:var(--up-accent);text-decoration:underline}.image-evidence button{border:1px solid var(--up-border);padding:7px 12px;border-radius:7px}.image-note{border-left:2px solid #d9a15e;padding-left:10px}@media(max-width:640px){.image-evidence dl{grid-template-columns:minmax(0,1fr);gap:5px}.image-evidence dt{margin-top:8px}}
</style>
