<script setup>
import Icon from './HelmLabIcon.vue';
import {testSourceURL} from './test-lab.mjs';
defineProps({result:Object,busy:Boolean,stale:Boolean,error:String});
defineEmits(['scan','docs']);
</script>
<template>
 <section class="test-preflight" aria-label="Configuration preflight"><div class="test-card-heading"><div><span class="test-eyebrow">BEFORE THE FIRST REQUEST</span><h4>Configuration preflight</h4></div><button class="test-button" :disabled="busy" @click="$emit('scan')"><Icon name="check"/>{{ busy?'Reading source…':result?'Check again':'Check config' }}</button></div><p class="test-caption">Checks YAML, your Rancher connection fields, and configuration loads found in the selected issue packages. No test code executes.</p>
  <p v-if="error" class="test-error-text" role="alert">{{ error }}</p><p v-if="stale&&result" class="test-message test-warning">Draft or selection changed. Check again for current findings.</p>
  <template v-if="result"><div class="test-preflight-summary"><span><strong>{{ result.sections.length }}</strong> resolved sections</span><span><strong>{{ result.findings.filter(f=>f.level!=='info').length }}</strong> items to review</span><span><strong>{{ result.unresolved }}</strong> unresolved references / types</span></div><div v-if="!result.findings.length" class="test-message"><Icon name="check"/>No issues found in the checks performed. Review the coverage below.</div><div class="test-preflight-findings"><div v-for="(finding,i) in result.findings" :key="i" class="test-preflight-finding" :class="'is-'+finding.level"><Icon :name="finding.level==='info'?'file':'signal'"/><div><code>{{ finding.path }}</code><p>{{ finding.message }}</p><a v-if="finding.file" :href="testSourceURL(result.sha,finding)" target="_blank" rel="noreferrer">{{ finding.file }}:{{ finding.line }} ↗</a></div><span>{{ finding.level==='error'?'Fix':finding.level==='warning'?'Review':'Note' }}</span></div></div>
   <details class="test-preflight-coverage"><summary>What was checked · {{ result.sha.slice(0,9) }}</summary><p>{{ result.coverage }}</p><p v-if="result.sections.length">Sections: <code>{{ result.sections.join(', ') }}</code></p><p>Packages: <code>{{ result.packages.join(', ')||'None selected' }}</code></p></details><div v-if="result.readmes.length" class="test-related-guides"><span class="test-eyebrow">RELATED GUIDES</span><button v-for="path in result.readmes" :key="path" class="test-link" @click="$emit('docs',path)"><Icon name="file"/>{{ path.replace(/^validation\//,'') }} <Icon name="arrow"/></button></div>
  </template>
 </section>
</template>
