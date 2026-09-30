<script setup>
import Icon from './HelmLabIcon.vue';
import Docs from './TestLabDocs.vue';
defineProps({documents:Array,sha:String,packages:Array,initialPath:String,requestNonce:Number,docsBusy:Boolean,docsError:String});
const emit=defineEmits(['close','reload','navigate']);
</script>
<template>
 <section class="test-inline-reference" aria-label="Test README reader">
  <header class="test-reference-heading"><div><span class="test-eyebrow">ALONGSIDE YOUR TESTS</span><h4>README reader</h4><code class="test-caption">{{ sha?.slice(0,9) }}</code></div><button type="button" class="test-icon-button" aria-label="Close README reader" @click="emit('close')"><Icon name="close"/></button></header>
  <Docs embedded hide-index :documents="documents" :sha="sha" :packages="packages" :initial-path="initialPath" :request-nonce="requestNonce" :busy="docsBusy" :error="docsError" @reload="emit('reload')" @close="emit('close')" @navigate="emit('navigate',$event)"/>
 </section>
</template>
