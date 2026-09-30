import test from 'node:test';
import assert from 'node:assert/strict';
import {clusterName,matchingCluster,groupClusterRecords} from './cluster-workspace.mjs';

test('nicknames group references without changing record identity',()=>{
 const records=[{id:'result-1',clusterId:'c1'},{id:'result-2',clusterId:'c2'},{id:'old-result'}];
 const clusters=[{id:'c1',name:'HA 1',nickname:'mycluster1'},{id:'c2',name:'HA 2'}];
 assert.equal(clusterName(clusters[0]),'mycluster1');
 const group=groupClusterRecords(records,clusters).find(g=>g.id==='c1');
 assert.equal(group.name,'mycluster1');assert.equal(group.records[0],records[0]);
 clusters[0].nickname='staging';assert.equal(groupClusterRecords(records,clusters).find(g=>g.id==='c1').name,'staging');
 clusters[0].nickname='';assert.equal(clusterName(clusters[0]),'HA 1');
 assert.equal(groupClusterRecords(records,clusters).at(-1).id,'');
});

test('auto-match only a unique exact Rancher endpoint; never guess between histories',()=>{
 const known={id:'c1',url:'https://rancher.example.test'};
 assert.equal(matchingCluster([known],'rancher.example.test/'),known);
 assert.equal(matchingCluster([known,{...known,id:'rebuilt-cluster'}],known.url),null);
 assert.equal(matchingCluster([known],'https://other.example.test'),null);
 assert.equal(matchingCluster([known],'http://rancher.example.test'),null);
 assert.equal(matchingCluster([known],'https://user:secret@rancher.example.test'),null);
});
