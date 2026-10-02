export function normalizeTeamMembers(users=[]){return [...new Set(users.map(user=>String(user).trim().replace(/^@/,'').toLowerCase()).filter(Boolean))];}
export function issueOwnership(issue,team=[],me=''){
 const owners=(issue.assignees||[]).map(owner=>typeof owner==='string'?owner.toLowerCase():owner.login.toLowerCase());
 const matched=normalizeTeamMembers(team).filter(user=>owners.includes(user));
 return {owners,matched,mine:!!me&&owners.includes(me.toLowerCase().replace(/^@/,'')),group:matched.length?'team':owners.length?'outside':'unassigned'};
}
