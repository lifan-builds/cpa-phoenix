package main

import (
	"os/exec"
	"strings"
	"testing"
)

// TestNewUserJourneyWithMockAccounts follows the dashboard's real first-use
// path without loading CPA or touching a live account. The embedded page is
// exercised as a user sees it: no key, connect, scan, warm up, repair, done.
func TestNewUserJourneyWithMockAccounts(t *testing.T) {
	script := strings.Split(strings.Split(dashboardHTML, "<script>")[1], "</script>")[0]
	script = strings.Replace(script, "\nscan()\n", "\n", 1)
	harness := `
const assert=require('node:assert/strict'),elements=new Map(),storageValues={},timers=[];
function node(selector){
  if(elements.has(selector))return elements.get(selector);
  const child={textContent:'',dataset:{}};
	const value={selector,value:'',textContent:'',innerHTML:'',hidden:false,disabled:false,checked:false,href:'',focused:false,dataset:{},
    classList:{values:new Set(),add(v){this.values.add(v)},remove(v){this.values.delete(v)},contains(v){return this.values.has(v)}},
	    querySelector(){return child},setAttribute(k,v){this[k]=String(v)},removeAttribute(k){delete this[k]},scrollIntoView(){},focus(){this.focused=true},
    oninput:null,onchange:null,onclick:null,onkeydown:null,onsubmit:null};
  elements.set(selector,value);return value;
}
const storage={getItem(k){return storageValues[k]||null},setItem(k,v){storageValues[k]=String(v)}};
global.window={localStorage:storage,sessionStorage:null,location:{host:'127.0.0.1:8317'},matchMedia(){return {matches:false}}};
global.navigator={userAgent:'phoenix-fixture'};
global.document={querySelector:node};
global.setTimeout=(fn)=>{timers.push(fn);return timers.length};
global.clearTimeout=()=>{};
let phase='first-scan',igniteCalls=0,reviveCalls=0,requests=[];
const accounts=[
  {number:1,email:'healthy@example.test',seat:'seat-healthy',status:'healthy',quota:'active'},
  {number:2,email:'fresh@example.test',seat:'seat-fresh',status:'healthy',quota:'fresh'},
  {number:3,email:'invalid@example.test',seat:'seat-invalid',status:'invalid',quota:'unknown'}
];
const freshQueue=[{number:3,email:'invalid@example.test',seat:'seat-invalid',state:'queued'}];
function response(body,status=200){return {ok:status>=200&&status<300,status,json:async()=>body};}
global.fetch=async(url,options={})=>{
  requests.push({url,options});
  assert.equal(options.headers.Authorization,'Bearer fixture-management-key');
  if(url.endsWith('/scan')){
    if(phase==='after-repair')return response({fresh:0,invalid:0,active:false,accounts:accounts.map(row=>({...row,status:'healthy',quota:'active'})),repair_queue:[]});
    if(phase==='after-ignite')return response({fresh:0,invalid:1,active:false,accounts:accounts.map(row=>row.email==='fresh@example.test'?{...row,quota:'active'}:row),repair_queue:freshQueue});
    return response({fresh:1,invalid:1,active:false,accounts,repair_queue:freshQueue});
  }
  if(url.endsWith('/ignite/schedule'))return response({enabled:false,time:'09:00',timezone:'America/Los_Angeles',next_run:0,last_run:0,last_outcome:''});
  if(url.endsWith('/quota-floor'))return response({enabled:false,target_seat:'',threshold_percent:90,last_status:'unknown',accounts:accounts.map(row=>({email:row.email,seat:row.seat,status:row.status}))});
  if(url.endsWith('/otp/status'))return response({preferred:'agent_autofill',agent_autofill:'ready',service:'cpa-phoenix',fallback:'thunderbird',fallback_always:true});
  if(url.endsWith('/ignite')){igniteCalls++;phase='after-ignite';return response({job_id:'ignite-fixture',state:'running',total:1},202)}
  if(url.includes('/state?id=ignite-fixture'))return response({job_id:'ignite-fixture',state:'completed',done:1,total:1});
  if(url.endsWith('/revive')){reviveCalls++;phase='repairing';return response({job_id:'revive-fixture',state:'running',total:1},202)}
  if(url.includes('/revive/poll?id=revive-fixture')){phase='after-repair';return response({job_id:'revive-fixture',state:'completed',done:1,total:1,automatic:true});}
  throw new Error('unexpected fixture request '+url);
};
` + script + `
async function settle(){for(let i=0;i<8;i++)await Promise.resolve();await new Promise(resolve=>setImmediate(resolve));for(let i=0;i<8;i++)await Promise.resolve();}
(async()=>{
  // Fresh visit: the guide and connection prompt are visible before a key exists.
  await scan();
  assert.equal(document.querySelector('#setup-guide').hidden,false);
  assert.equal(document.querySelector('#auth-fallback').classList.contains('on'),true);
	assert.equal(document.querySelector('#management-key').focused,true);
  assert.match(document.querySelector('#status').textContent,/CPA authentication required/);

  // Connect and load a mixed synthetic inventory.
  phoenixManagementKey='fixture-management-key';
  await scan(); await settle();
  assert.equal(document.querySelector('#total-count').textContent,3);
  assert.equal(document.querySelector('#healthy-count').textContent,2);
  assert.equal(document.querySelector('#fresh-count').textContent,1);
  assert.equal(document.querySelector('#invalid-count').textContent,1);
  assert.equal(document.querySelector('#ignite').disabled,false);
  assert.equal(document.querySelector('#revive').disabled,false);
  assert.equal(document.querySelector('#next-action-title').textContent,'Repair needs attention');
	assert.match(document.querySelector('#setup-progress').textContent,/Fix invalid accounts to finish setup/);

  // Warm up the one fresh seat, then refresh into the repair state.
  await run('ignite'); await settle();
  assert.equal(igniteCalls,1);
  assert.equal(phase,'after-ignite');
  assert.equal(document.querySelector('#fresh-count').textContent,0);
  assert.equal(document.querySelector('#invalid-count').textContent,1);

  // Repair the invalid seat through the automatic browser path in fixture mode.
  await run('revive'); await settle();
  assert.equal(reviveCalls,1);
  assert.equal(phase,'after-repair');
  assert.equal(document.querySelector('#invalid-count').textContent,0);
  assert.equal(document.querySelector('#next-action-title').textContent,'You are up to date');
  assert.match(document.querySelector('#status').textContent,/No maintenance needed|Accounts checked/);

  // The completion marker is the only onboarding write, and the guide can reopen.
  phoenixMarkOnboardingSeen();
  assert.equal(storageValues['cpa-phoenix:onboarding:v1'],'seen');
  assert.equal(document.querySelector('#setup-guide').hidden,true);
  phoenixOpenOnboarding();
  assert.equal(document.querySelector('#setup-guide').hidden,false);
  assert.equal(Object.keys(storageValues).length,1);
  assert.ok(requests.length>=8,'journey should exercise scan, optional loads, actions, and polls');
})().catch(error=>{console.error(error);process.exitCode=1});
`
	if out, err := exec.Command("node", "-e", harness).CombinedOutput(); err != nil {
		t.Fatalf("mock new-user journey failed: %v\n%s", err, out)
	}
}
