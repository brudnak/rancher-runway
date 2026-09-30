import test from 'node:test';
import assert from 'node:assert/strict';
import {libraryGroups,moveLibraryPackage,reorderLibraryBucket} from './test-package-library.mjs';
const lib=()=>({version:1,revision:'rev',buckets:[{id:'a',name:'2.16',packageIds:['one','two']},{id:'b',name:'2.17',packageIds:[]}],unfiled:['three']});
test('moving slipped issues retains identity and the source library',()=>{const before=lib(),after=moveLibraryPackage(before,'one','b');assert.deepEqual(before.buckets[0].packageIds,['one','two']);assert.deepEqual(after.buckets[0].packageIds,['two']);assert.deepEqual(after.buckets[1].packageIds,['one']);assert.equal(after.revision,before.revision);});
test('package and milestone reordering is deterministic',()=>{assert.deepEqual(moveLibraryPackage(lib(),'two','a',0).buckets[0].packageIds,['two','one']);assert.deepEqual(reorderLibraryBucket(lib(),'b',-1).buckets.map(b=>b.id),['b','a']);});
test('deleted packages are hidden and newly imported packages appear unfiled',()=>{const groups=libraryGroups(lib(),[{id:'one'},{id:'three'},{id:'new'}]);assert.deepEqual(groups[0].packages.map(p=>p.id),['one']);assert.deepEqual(groups[2].packages.map(p=>p.id),['three','new']);});
