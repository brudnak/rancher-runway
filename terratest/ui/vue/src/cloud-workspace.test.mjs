import test from 'node:test';
import assert from 'node:assert/strict';
import {costChartModel,filterCostEntries,filterAWSResources,toggleVisibleCandidates,awsResourceKey} from './cloud-workspace.mjs';
const now=new Date(2026,8,29,12);
const day=(date,region='us-east-2',n=1)=>({date,region,records:n,partial:0,imported:n,ec2:n,ebs:n*2,rds:n*3,lb:n*4,total:n*10});
test('cost charts fill missing periods and aggregate all services across regions',()=>{
 const model=costChartModel([day('2026-09-01'),day('2026-09-29'),day('2026-09-29','us-west-2',2),day('2026-08-01'),day('2026-09-30')],'30','',now);
 assert.equal(model.points.length,30);assert.equal(model.points[0].key,'2026-08-31');assert.equal(model.summary.total,40);assert.equal(model.summary.records,4);assert.equal(model.points.at(-1).cumulative,40);assert.equal(model.points.at(-1).total,30);assert.equal(model.points[2].total,0);assert.equal(model.summary.rds,12);
 const west=costChartModel([day('2026-09-29'),day('2026-09-29','us-west-2',2)],'30','us-west-2',now);assert.equal(west.summary.total,20);
});
test('cost chart switches to month and year buckets without losing totals',()=>{
 const rows=[day('2025-09-30'),day('2026-09-29')];const annual=costChartModel(rows,'365','',now);assert.equal(annual.grain,'month');assert.equal(annual.points.length,13);assert.equal(annual.points.at(-1).cumulative,20);
 const history=costChartModel([day('2010-01-01'),...rows],'all','',now);assert.equal(history.grain,'year');assert.equal(history.points.length,17);assert.equal(history.points.at(-1).cumulative,30);
 assert.equal(costChartModel([],'all','',now).points.length,0);
});
test('recent history filters use local dates, region, and all search words',()=>{
 const entries=[{runId:'r-123',owner:'Ada Lovelace',region:'us-east-2',finishedAt:new Date(2026,8,29).toISOString()},{runId:'old',region:'us-west-2',finishedAt:new Date(2025,8,1).toISOString()}];
 assert.equal(filterCostEntries(entries,'30','','ada 123',now).length,1);assert.equal(filterCostEntries(entries,'30','us-west-2','',now).length,0);assert.equal(filterCostEntries(entries,'all','','',now).length,2);
});
test('inventory filters include ownership tags but keep protected resources ineligible',()=>{
 const items=[{type:'EC2 instance',id:'i-1',name:'Staging',region:'us-east-2',tags:{Owner:'Ada'},cleanupEligible:true},{type:'IAM role',id:'role',name:'Protected',cleanupEligible:false}];
 assert.deepEqual(filterAWSResources(items,{query:'staging ada',status:'candidates'}),[items[0]]);assert.deepEqual(filterAWSResources(items,{status:'protected'}),[items[1]]);assert.equal(filterAWSResources(items,{type:'IAM role',status:'candidates'}).length,0);
});
test('select all affects visible eligible resources and preserves hidden selections',()=>{
 const a={type:'EC2 instance',id:'i-1',cleanupEligible:true},b={type:'EC2 instance',id:'i-2',cleanupEligible:true},protectedItem={type:'IAM role',id:'role',cleanupEligible:false};
 const selected=toggleVisibleCandidates([awsResourceKey(a)],[b,protectedItem],true);assert.deepEqual(selected,[awsResourceKey(a),awsResourceKey(b)]);assert.deepEqual(toggleVisibleCandidates(selected,[b,protectedItem],false),[awsResourceKey(a)]);assert.equal(toggleVisibleCandidates(selected,[b],true).length,2);
});
