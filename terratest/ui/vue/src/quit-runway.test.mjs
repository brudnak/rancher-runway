import test from 'node:test';
import assert from 'node:assert/strict';
import {quitRunway} from './quit-runway.mjs';

test('native quit leaves shutdown to the app so a cancelled close preserves its server', async()=>{
 const calls=[];
 const runtime={Quit(){assert.equal(this,runtime);calls.push('native');}};
 assert.equal(await quitRunway({runtime,shutdown:()=>calls.push('shutdown'),closeBrowser:()=>calls.push('browser')}),'native');
 assert.deepEqual(calls,['native']);
});
test('a native quit failure never falls through to destructive server shutdown',async()=>{
 const calls=[];
 await assert.rejects(quitRunway({runtime:{Quit(){throw new Error('native unavailable');}},shutdown:()=>calls.push('shutdown'),closeBrowser:()=>calls.push('browser')}),/native unavailable/);
 assert.deepEqual(calls,[]);
});
test('browser shutdown completes before closing and failed shutdown leaves the page open',async()=>{
 const calls=[];
 assert.equal(await quitRunway({shutdown:async()=>{await Promise.resolve();calls.push('shutdown');},closeBrowser:()=>calls.push('browser')}),'browser');
 assert.deepEqual(calls,['shutdown','browser']);
 await assert.rejects(quitRunway({shutdown:async()=>{throw new Error('operation running');},closeBrowser:()=>calls.push('unexpected close')}),/operation running/);
 assert.deepEqual(calls,['shutdown','browser']);
});
