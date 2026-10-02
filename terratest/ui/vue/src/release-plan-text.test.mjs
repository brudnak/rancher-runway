import test from 'node:test';
import assert from 'node:assert/strict';
import {confluencePlanText} from './release-plan-text.mjs';
import {extractReleaseMonth,monthName} from './release-trackers.mjs';
const html='<html><style>ignore this</style><h1>2026</h1><h2>Diet October</h2><table><tr><td>Rancher v2.15.3</td><td>Bug Complete &amp; Release Note Labels</td><td><p>09 Oct 2026</p></td></tr><tr><td>Code Freeze</td><td>16 Oct 2026</td></tr><tr><td>Tentative Release</td><td>28 Oct 2026</td></tr></table><p>October Charts Dates</p><ul><li>Charts QA Sign Off: 22 Oct 2026</li></ul><p>Holiday: 12 Oct 2026</p><h2>November</h2><p>Rancher v2.16.0</p><p>Feature Complete</p><p>09 Oct 2026</p><p>UI: 16 Oct 2026</p><p>Code Freeze</p><p>06 Nov 2026</p><h1>2027</h1><h2>October</h2><p>Code Freeze: 15 Oct 2027</p></html>';
test('Confluence MIME export decodes quoted printable without executing or keeping styles',()=>{
 const raw='MIME-Version: 1.0\r\nContent-Type: multipart/related; boundary="test"\r\n\r\n--test\r\nContent-Type: text/html; charset=UTF-8\r\nContent-Transfer-Encoding: quoted-printable\r\n\r\n'+html.replace('Diet October','Diet Octo=\r\nber')+'\r\n--test--';
 const text=confluencePlanText(raw);
 assert.match(text,/## Diet October/);assert.ok(!text.includes('ignore this'));
 const october=extractReleaseMonth(text,'2026-10');
 assert.deepEqual(october.checkpoints.map(c=>[c.name,c.date]),[['Bug complete','2026-10-09'],['Code freeze','2026-10-16'],['Release','2026-10-28'],['Charts QA sign-off','2026-10-22']]);
 assert.deepEqual(october.versions,['v2.15.3']);
 assert.equal(october.checkpoints[2].tentative,true);
});
test('release section controls scope even when its checkpoints fall in a different month',()=>{
 const text=confluencePlanText(html),nov=extractReleaseMonth(text,'2026-11');
 assert.deepEqual(nov.checkpoints.map(c=>[c.name,c.date]),[['Feature complete','2026-10-09'],['Feature complete (UI)','2026-10-16'],['Code freeze','2026-11-06']]);
 assert.equal(extractReleaseMonth(text,'2026-12').matched,false);
 assert.equal(extractReleaseMonth(text,'2026-12').checkpoints.length,0);
 assert.deepEqual(extractReleaseMonth(text,'2027-10').checkpoints.map(c=>c.date),['2027-10-15']);
});
test('unstructured text explicitly falls back to calendar-month matching',()=>{
 const result=extractReleaseMonth('Code freeze: 16 Oct 2026\nRelease: 24 Nov 2026','2026-10');
 assert.equal(result.mode,'dates');assert.equal(result.checkpoints.length,1);
 assert.equal(monthName('2026-10'),'October 2026');assert.equal(monthName('2026-13'),'');
});
test('unsupported binary Word inputs are rejected with an actionable message',()=>{
 assert.throws(()=>confluencePlanText('\u00d0\u00cf\u0011\u00e0'),/binary .doc/);
});

test('a standalone month-first date is not mistaken for a release heading',()=>{
 const result=extractReleaseMonth('Code freeze\nOctober 16, 2026','2026-10');
 assert.equal(result.mode,'dates');assert.equal(result.checkpoints[0].date,'2026-10-16');
});
