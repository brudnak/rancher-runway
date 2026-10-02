import {ref} from 'vue';
import {normalizeTeamMembers} from './team-members.mjs';
let saved={};try{saved=JSON.parse(localStorage.getItem('runway-team-settings')||'{}')||{};}catch{}
export const savedTeam=ref(normalizeTeamMembers(Array.isArray(saved.users)?saved.users:[]));
export const savedTeamMe=ref(saved.me||'');
export function saveTeam(users,me=''){savedTeam.value=normalizeTeamMembers(users);savedTeamMe.value=me;localStorage.setItem('runway-team-settings',JSON.stringify({users:savedTeam.value,me}));}
