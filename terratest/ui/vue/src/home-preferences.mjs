import {ref} from 'vue';
import {normalizeHomeLayout} from './home-layout.mjs';
const key='rancherRunwayHomeLayout';
let saved;try{saved=localStorage.getItem(key);}catch{}
export const homeLayout=ref(normalizeHomeLayout(saved));
export const homeLayoutError=ref('');
export function setHomeLayout(value){homeLayout.value=normalizeHomeLayout(value);try{localStorage.setItem(key,homeLayout.value);homeLayoutError.value='';}catch{homeLayoutError.value='Layout changed for this visit. Your browser could not save the preference.';}}
