<script setup>
import {computed,nextTick,onBeforeUnmount,ref,useId,watch} from 'vue';
import Icon from './HelmLabIcon.vue';
import {apiFetch} from './store.js';
import {rancherConnectionURL,rancherConnectionFingerprint} from './rancher-connection.mjs';
const props=defineProps({purpose:{type:String,required:true},url:{type:String,default:''},insecure:Boolean,caPem:{type:String,default:''},disabled:Boolean,credentialAvailable:Boolean});
const emit=defineEmits(['generated','busy']);
const username=ref('admin'),password=ref(''),ttl=ref(1440),busy=ref(false),error=ref(''),result=ref(null),replacement=ref(false),showPassword=ref(false),passwordRef=ref(null),successRef=ref(null);
const useBootstrapPassword=ref(true);
const passwordNoteId=useId();
const destination=computed(()=>rancherConnectionURL(props.url));
const fingerprint=computed(()=>rancherConnectionFingerprint(props.url,props.insecure,props.caPem));
const ready=computed(()=>destination.value&&username.value.trim()&&(useBootstrapPassword.value||password.value)&&!props.disabled&&!busy.value);
const showForm=computed(()=>!props.credentialAvailable||replacement.value);
const duration=computed(()=>({60:'1 hour',480:'8 hours',1440:'24 hours',10080:'7 days',43200:'30 days'}[ttl.value]||'24 hours'));
const expiry=computed(()=>{const date=new Date(result.value?.expiresAt||'');return Number.isNaN(date.getTime())?'Check its expiry in Rancher’s API & Keys.':`Expires ${date.toLocaleString()}.`;});
let controller,disposed=false;
function replaceCredential(){if(busy.value||props.disabled)return;replacement.value=true;password.value='';error.value='';nextTick(()=>passwordRef.value?.focus({preventScroll:true}));}
function cancelReplacement(){if(busy.value)return;replacement.value=false;password.value='';showPassword.value=false;error.value='';}
watch(fingerprint,()=>{useBootstrapPassword.value=true;username.value='admin';password.value='';showPassword.value=false;error.value='';result.value=null;replacement.value=false;});
watch(()=>props.credentialAvailable,available=>{if(!available){result.value=null;replacement.value=false;}});
function choosePasswordSource(configured){useBootstrapPassword.value=configured;password.value='';showPassword.value=false;error.value='';if(configured)username.value='admin';}
async function generate(){
 if(!ready.value)return;const target=fingerprint.value;busy.value=true;emit('busy',true);error.value='';result.value=null;controller=new AbortController();const timer=setTimeout(()=>controller.abort(),60000);
 const body=JSON.stringify({url:destination.value,username:username.value,password:useBootstrapPassword.value?'':password.value,useBootstrapPassword:useBootstrapPassword.value,ttlMinutes:Number(ttl.value),purpose:props.purpose,insecure:props.insecure,caPem:props.caPem});password.value='';showPassword.value=false;
 try {
  const response=await apiFetch('/api/rancher/token',{method:'POST',body,signal:controller.signal});const created=await response.json();
  if(disposed)return;
  if(target!==fingerprint.value){error.value='The connection changed during sign-in. This token was not applied. Remove the new Runway entry in Rancher’s API & Keys before trying again.';return;}
  if(!created.token)throw new Error('Rancher did not return a token. Check API & Keys before trying again.');
  // Keep the token only in the parent connection draft, never success metadata.
  result.value={id:created.id,description:created.description,expiresAt:created.expiresAt,warning:created.warning};emit('generated',created);replacement.value=false;
  await nextTick();successRef.value?.focus({preventScroll:true});
 }catch(failure){if(!disposed)error.value=failure.name==='AbortError'?'Sign-in timed out. Check Rancher’s API & Keys before trying again; this attempt may have created a token.':failure.message;}
 finally{clearTimeout(timer);password.value='';busy.value=false;emit('busy',false);}
}
onBeforeUnmount(()=>{disposed=true;password.value='';controller?.abort();emit('busy',false);});
</script>
<template>
 <section class="rancher-token-generator" :aria-busy="busy" aria-label="Rancher password sign-in">
  <div v-if="credentialAvailable&&!showForm" ref="successRef" class="rancher-token-success" role="status" tabindex="-1"><Icon name="check"/><div><strong>{{ result?'Token ready. You can continue.':'A token is already available for this connection.' }}</strong><p v-if="result">{{ expiry }} Revoke it anytime in Rancher’s API & Keys.</p><p v-else>Continue with it, or sign in to create a replacement.</p><small v-if="result">{{ result.description }}</small><p v-if="result?.warning" class="rancher-connect-error">{{ result.warning }}</p></div><button type="button" class="rancher-connect-link" :disabled="disabled||busy" @click="replaceCredential">Sign in again</button></div>
  <div v-if="showForm" class="rancher-token-form">
   <div class="rancher-password-sources" role="group" aria-label="Password source">
    <button type="button" :aria-pressed="useBootstrapPassword" :disabled="busy||disabled" @click="choosePasswordSource(true)"><Icon name="key"/><span><strong>Use config password</strong><small>Recommended · Runway’s bootstrap password</small></span></button>
    <button type="button" :aria-pressed="!useBootstrapPassword" :disabled="busy||disabled" @click="choosePasswordSource(false)"><Icon name="lock"/><span><strong>Enter a password</strong><small>For a changed password or another local user</small></span></button>
   </div>
   <fieldset class="rancher-password-entry" :disabled="busy||disabled"><legend class="lab-sr-only">Local Rancher sign-in</legend><label v-if="!useBootstrapPassword">Rancher password<div class="rancher-password-input"><input ref="passwordRef" v-model="password" :type="showPassword?'text':'password'" :aria-describedby="passwordNoteId" autocomplete="off" autocapitalize="off" spellcheck="false" placeholder="Your local Rancher password" maxlength="16384" @keydown.enter.prevent="generate"/><button type="button" :aria-label="showPassword?'Hide password':'Show password'" @click="showPassword=!showPassword">{{ showPassword?'Hide':'Show' }}</button></div></label><div v-else class="rancher-config-password"><strong>Bootstrap password from Runway config</strong><p>Uses rancher.bootstrap_password for admin. The password stays on the backend.</p></div><button type="button" class="rancher-connect-button is-primary" :disabled="!ready" @click="generate"><Icon :name="busy?'refresh':'key'" :class="{'lab-spin':busy}"/>{{ busy?'Creating token…':useBootstrapPassword?'Sign in with config password':'Sign in & generate token' }}</button></fieldset>
   <p v-if="useBootstrapPassword" class="rancher-password-note">Uses the current Runway config. If this Rancher’s password has changed since deployment, enter its current password manually.</p>
   <p :id="passwordNoteId" class="rancher-password-note"><Icon name="lock"/>{{ useBootstrapPassword?'Config password is used only when you sign in.':'Password used once, then cleared.' }}<span v-if="!destination"> Enter the Rancher URL above to continue.</span></p>
   <details class="rancher-signin-options"><summary>Sign-in options<span>{{ username.trim()||'Local user' }} · {{ duration }}</span></summary><fieldset :disabled="busy||disabled"><legend class="lab-sr-only">Sign-in options</legend><div class="rancher-token-fields"><label>Local username<input v-model="username" :disabled="useBootstrapPassword" autocomplete="off" autocapitalize="off" spellcheck="false" maxlength="256" @keydown.enter.prevent="generate"/></label><label>Token expires after<select v-model.number="ttl"><option :value="60">1 hour</option><option :value="480">8 hours</option><option :value="1440">24 hours</option><option :value="10080">7 days</option><option :value="43200">30 days</option></select></label></div></fieldset><p>Use a local Rancher account. For SSO, choose the API token tab. The new token has this user’s permissions.</p><p v-if="destination" class="rancher-token-destination"><Icon name="globe"/>{{ destination }}</p><p>{{ insecure?'TLS verification is off for this connection.':caPem?'Uses your custom CA certificate.':'TLS certificate verification is on.' }}</p><small class="rancher-token-note">Creates a named Runway token in Rancher’s API & Keys. Generating again does not revoke earlier tokens. Saving the token is controlled by this connection’s save options.</small></details>
   <button v-if="replacement&&credentialAvailable" type="button" class="rancher-connect-link" :disabled="busy||disabled" @click="cancelReplacement">Keep the current token</button>
  </div>
  <p v-if="insecure" class="rancher-token-trust-warning"><Icon name="lock"/>TLS verification is off for this connection.</p>
  <p v-if="error" class="rancher-connect-error" role="alert">{{ error }}</p>
 </section>
</template>
