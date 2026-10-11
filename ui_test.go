package main

import (
	"os/exec"
	"strings"
	"testing"
)

func TestDashboardHasMaintenanceActionsAndScheduleSave(t *testing.T) {
	if strings.Count(dashboardHTML, "<button") != 11 {
		t.Fatal("dashboard must have maintenance, schedule, quota-preservation, and current-login skip buttons")
	}
	if strings.Count(string(dashboardPageHTML()), "<button") != 11 {
		t.Fatal("dashboard must retain current-login skip and schedule save")
	}
	dashboard := string(dashboardPageHTML())
	if strings.Count(dashboard, "<script>") != 1 || strings.Count(dashboard, "async function scan()") != 1 || strings.Count(dashboard, "async function run(kind)") != 1 || strings.Count(dashboard, "async function poll(id,revive=false)") != 1 {
		t.Fatal("rendered dashboard must have one authoritative script")
	}
	for _, forbidden := range []string{"account picker", "analytics", "export"} {
		if strings.Contains(strings.ToLower(dashboardHTML), forbidden) {
			t.Fatalf("dashboard contains forbidden %q", forbidden)
		}
	}
}

func TestDashboardOnboardingGuideIsInlineReopenableAndNonBlocking(t *testing.T) {
	dashboard := string(dashboardPageHTML())
	for _, required := range []string{"id=\"setup-guide\"", "id=\"getting-started\"", "id=\"setup-dismiss\"", "id=\"setup-complete\"", "cpa-phoenix:onboarding:v1", "phoenixOpenOnboarding", "setup-step-connect", "setup-step-inventory", "setup-step-quota", "setup-step-schedule", "setup-step-repair"} {
		if !strings.Contains(dashboard, required) {
			t.Fatalf("dashboard missing onboarding contract %q", required)
		}
	}
	if strings.Contains(dashboard, "localStorage.setItem") || strings.Contains(dashboard, "sessionStorage.setItem") {
		t.Fatal("onboarding must use the safe storage seam and must not weaken key-storage protection")
	}
	if strings.Contains(dashboard, "window.alert") || strings.Contains(dashboard, "showModal") {
		t.Fatal("onboarding must remain an inline non-blocking guide")
	}
}

func TestDashboardOnboardingMarkerIsNonsensitiveAndReopenable(t *testing.T) {
	script := strings.Split(strings.Split(dashboardHTML, "<script>")[1], "</script>")[0]
	script = strings.Replace(script, "\nscan()\n", "\n", 1)
	harness := `
const assert=require('node:assert/strict'),elements=new Map(),values={};
const storage={getItem(key){return values[key]||null},setItem(key,value){values[key]=String(value)}};
function element(selector){
  if(elements.has(selector))return elements.get(selector);
  const number={textContent:'',dataset:{}};
  const value={hidden:true,textContent:'',dataset:{},querySelector(){return number},scrollIntoView(){}};
  elements.set(selector,value);return value;
}
global.window={localStorage:storage,sessionStorage:null};
global.document={querySelector:element};
` + script + `
const guide=document.querySelector('#setup-guide');
phoenixOnboardingSeen=false;phoenixUpdateSetupGuide();
assert.equal(guide.hidden,false);
phoenixMarkOnboardingSeen();
assert.equal(values['cpa-phoenix:onboarding:v1'],'seen');
assert.equal(guide.hidden,true);
phoenixOpenOnboarding();
assert.equal(guide.hidden,false);
assert.deepEqual(values,{['cpa-phoenix:onboarding:v1']:'seen'});
`
	if out, err := exec.Command("node", "-e", harness).CombinedOutput(); err != nil {
		t.Fatalf("onboarding marker behavior failed: %v\n%s", err, out)
	}
}

func TestDashboardOnboardingToleratesUnavailableStorageAndReducedMotion(t *testing.T) {
	script := strings.Split(strings.Split(dashboardHTML, "<script>")[1], "</script>")[0]
	script = strings.Replace(script, "\nscan()\n", "\n", 1)
	harness := `
const assert=require('node:assert/strict'),elements=new Map();
function element(selector){
  if(elements.has(selector))return elements.get(selector);
  const number={textContent:'',dataset:{}};
  const value={hidden:true,textContent:'',dataset:{},querySelector(){return number},scrollIntoView(options){this.scrollOptions=options}};
  elements.set(selector,value);return value;
}
global.window={get localStorage(){throw Error('storage blocked')},sessionStorage:null,matchMedia(){return {matches:true}}};
global.document={querySelector:element};
` + script + `
phoenixOnboardingSeen=false;
phoenixMarkOnboardingSeen();
assert.equal(elements.get('#setup-guide').hidden,true);
phoenixOpenOnboarding();
assert.equal(elements.get('#setup-guide').hidden,false);
assert.deepEqual(elements.get('#setup-guide').scrollOptions,{behavior:'auto',block:'start'});
`
	if out, err := exec.Command("node", "-e", harness).CombinedOutput(); err != nil {
		t.Fatalf("onboarding storage/motion behavior failed: %v\n%s", err, out)
	}
}

func TestDashboardSetupProgressKeepsUnavailableOptionalStepsUnresolved(t *testing.T) {
	script := strings.Split(strings.Split(dashboardHTML, "<script>")[1], "</script>")[0]
	script = strings.Replace(script, "\nscan()\n", "\n", 1)
	harness := `
const assert=require('node:assert/strict'),elements=new Map();
function element(selector){
  if(elements.has(selector))return elements.get(selector);
  const number={textContent:'',dataset:{}};
  const value={hidden:true,textContent:'',dataset:{},querySelector(){return number}};
  elements.set(selector,value);return value;
}
global.window={localStorage:null,sessionStorage:null};global.document={querySelector:element};
` + script + `
phoenixOnboardingSeen=false;
phoenixSetup={connected:true,inventory:true,quota:'unavailable',schedule:'unavailable',repair:'done'};
phoenixUpdateSetupGuide();
assert.equal(elements.get('#setup-step-connect').dataset.state,'done');
assert.equal(elements.get('#setup-step-inventory').dataset.state,'done');
assert.equal(elements.get('#setup-step-quota').dataset.state,'attention');
assert.equal(elements.get('#setup-step-schedule').dataset.state,'attention');
assert.equal(elements.get('#setup-step-repair').dataset.state,'done');
	assert.match(elements.get('#setup-progress').textContent,/Core setup complete/);
`
	if out, err := exec.Command("node", "-e", harness).CombinedOutput(); err != nil {
		t.Fatalf("onboarding unavailable-state behavior failed: %v\n%s", err, out)
	}
}

func TestDashboardRepairPathsExplainUIPassAgentSkillAndAgentAutofill(t *testing.T) {
	dashboard := string(dashboardPageHTML())
	for _, required := range []string{
		"Repair in Phoenix",
		"Repair with Agent skill",
		"Automatic browser",
		"Regular Chrome",
		"id=\"cancel-revive\"",
		"id=\"recover-skipped\"",
		"regular Chrome with the Phoenix repair skill",
		"Agent Autofill provides fresh email codes",
		"Phoenix still owns the queue",
		"id=\"repair-path-note\"",
	} {
		if !strings.Contains(dashboard, required) {
			t.Fatalf("dashboard missing repair path guidance %q", required)
		}
	}
	if !strings.Contains(dashboard, "phoenixUpdateRepairPath") || !strings.Contains(dashboard, "#agent-mode').onchange") {
		t.Fatal("dashboard must update visible repair path guidance when the agent mode changes")
	}
}

func TestDashboardRepairSetupStepReflectsQueueState(t *testing.T) {
	script := strings.Split(strings.Split(dashboardHTML, "<script>")[1], "</script>")[0]
	script = strings.Replace(script, "\nscan()\n", "\n", 1)
	harness := `
const assert=require('node:assert/strict'),elements=new Map();
function element(selector){
  if(elements.has(selector))return elements.get(selector);
  const number={textContent:'',dataset:{}};
  const value={hidden:true,textContent:'',dataset:{},querySelector(){return number}};
  elements.set(selector,value);return value;
}
global.window={localStorage:null,sessionStorage:null};global.document={querySelector:element};
` + script + `
phoenixOnboardingSeen=false;
phoenixSetup={connected:true,inventory:true,quota:'off',schedule:'off',repair:'attention'};
phoenixUpdateSetupGuide();
assert.equal(elements.get('#setup-step-repair').dataset.state,'attention');
	assert.match(elements.get('#setup-progress').textContent,/Fix invalid accounts to finish setup/);
	phoenixSetup.repair='done';phoenixUpdateSetupGuide();
	assert.equal(elements.get('#setup-step-repair').dataset.state,'done');
	assert.match(elements.get('#setup-progress').textContent,/Core setup complete/);
`
	if out, err := exec.Command("node", "-e", harness).CombinedOutput(); err != nil {
		t.Fatalf("repair setup state behavior failed: %v\n%s", err, out)
	}
}

func TestDashboardRepairPathStatusChangesWithAgentMode(t *testing.T) {
	script := strings.Split(strings.Split(dashboardHTML, "<script>")[1], "</script>")[0]
	script = strings.Replace(script, "\nscan()\n", "\n", 1)
	harness := `
const assert=require('node:assert/strict'),elements=new Map();
global.document={querySelector(s){if(!elements.has(s))elements.set(s,{checked:false,textContent:'',value:''});return elements.get(s)}};
` + script + `
phoenixUpdateRepairPath();assert.match(document.querySelector('#repair-path-note').textContent,/Automatic browser/);
document.querySelector('#agent-mode').checked=true;phoenixUpdateRepairPath();assert.match(document.querySelector('#repair-path-note').textContent,/Regular Chrome/);
`
	if out, err := exec.Command("node", "-e", harness).CombinedOutput(); err != nil {
		t.Fatalf("repair path status behavior failed: %v\n%s", err, out)
	}
}

func TestDashboardNextActionPrioritizesConnectionRepairAndFreshWork(t *testing.T) {
	script := strings.Split(strings.Split(dashboardHTML, "<script>")[1], "</script>")[0]
	script = strings.Replace(script, "\nscan()\n", "\n", 1)
	harness := `
const assert=require('node:assert/strict'),elements=new Map();
global.document={querySelector(s){if(!elements.has(s))elements.set(s,{hidden:true,textContent:'',href:'',value:''});return elements.get(s)}};
global.window={localStorage:null,sessionStorage:null};global.setTimeout=()=>{};
` + script + `
const panel=document.querySelector('#next-action'),title=document.querySelector('#next-action-title'),link=document.querySelector('#next-action-link');
phoenixSetup={connected:false,inventory:false,quota:'pending',schedule:'pending',repair:false};phoenixRenderNextAction();
assert.equal(title.textContent,'Connect to CPA');assert.equal(link.href,'#auth-fallback');
phoenixSetup={connected:true,inventory:true,quota:'off',schedule:'off',repair:false};document.querySelector('#total-count').textContent='2';document.querySelector('#invalid-count').textContent='2';phoenixQueueRows=[];phoenixRenderNextAction();
assert.equal(title.textContent,'Repair needs attention');assert.equal(link.href,'#maintenance');
phoenixActiveJob=true;phoenixRenderNextAction();
assert.equal(title.textContent,'Maintenance in progress');assert.equal(link.href,'#status');
 phoenixActiveJob=false;phoenixSetup.quota='unavailable';document.querySelector('#invalid-count').textContent='0';phoenixRenderNextAction();
 assert.equal(title.textContent,'Optional tools unavailable');assert.equal(link.href,'#advanced-settings');
phoenixSetup.quota='off';
document.querySelector('#total-count').textContent='2';document.querySelector('#invalid-count').textContent='0';phoenixQueueRows=[];document.querySelector('#fresh-count').textContent='3';phoenixRenderNextAction();
assert.equal(title.textContent,'Fresh accounts are ready');assert.equal(link.href,'#ignite');
document.querySelector('#fresh-count').textContent='0';phoenixRenderNextAction();
assert.equal(title.textContent,'You are up to date');assert.equal(panel.hidden,false);
document.querySelector('#total-count').textContent='0';phoenixRenderNextAction();
assert.equal(title.textContent,'No accounts found');assert.equal(link.href,'/management.html');
`
	if out, err := exec.Command("node", "-e", harness).CombinedOutput(); err != nil {
		t.Fatalf("next-action behavior failed: %v\n%s", err, out)
	}
}
func TestDashboardAuthenticatesEveryRequestWithoutPersistingPhoenixKey(t *testing.T) {
	dashboard := string(dashboardPageHTML())
	for _, required := range []string{
		"enc::v1::",
		"cli-proxy-api-webui::secure-storage",
		"managementKey",
		"cli-proxy-auth",
		"'Authorization':'Bearer '+key,'Accept':'application/json'",
		"authenticatedFetch(endpoint",
		"authenticatedFetch('/v0/management/plugins/cpa-phoenix/'",
		"CPA authentication rejected",
		"clearManagementKey(key)",
		"phoenixRejectedManagementKey",
	} {
		if !strings.Contains(dashboard, required) {
			t.Fatalf("dashboard missing %q", required)
		}
	}
	if strings.Contains(dashboard, "localStorage.setItem") || strings.Contains(dashboard, "sessionStorage.setItem") {
		t.Fatal("Phoenix must not persist the management key")
	}
	if strings.Count(dashboard, "authenticatedFetch(") < 3 {
		t.Fatal("scan, action, and poll requests must all use authenticatedFetch")
	}
	if strings.Contains(dashboard, "fetch('/v0/management/plugins/cpa-phoenix/scan'") {
		t.Fatal("scan must not bypass authenticatedFetch")
	}
}

func TestDashboardDisablesZeroCountsAndRendersIgniteResults(t *testing.T) {
	dashboard := string(dashboardPageHTML())
	for _, required := range []string{
		"phoenixSetActionAvailability",
		"phoenixActionInFlight",
		"phoenixScanSequence",
		"Boolean(active)",
		"Number(fresh||0)<=0",
		"!hasCurrent",
		"Warmup finished · " + "'+(result.sent||0)",
		"'+(result.skipped||0)+' skipped",
		"need a closer look",
	} {
		if !strings.Contains(dashboard, required) {
			t.Fatalf("dashboard missing %q", required)
		}
	}
	if strings.Contains(dashboard, "catch(e){if(e.message!=='management_key_required'&&e.message!=='management_unauthorized')document.querySelector('#status').textContent='Action unavailable';document.querySelector('#ignite').disabled=false") {
		t.Fatal("action errors must rescan before re-enabling zero-count buttons")
	}
	if strings.Count(dashboard, "\nscan()\n") != 1 {
		t.Fatal("dashboard must perform one initial scan")
	}
}

func TestDashboardFailedScanClearsInventoryAndDisablesActions(t *testing.T) {
	script := strings.Split(strings.Split(dashboardHTML, "<script>")[1], "</script>")[0]
	script = strings.Replace(script, "\nscan()\n", "\n", 1)
	harness := `
const assert=require('node:assert/strict'),elements=new Map(),mode={value:'success'};
function node(){return {value:'',textContent:'',innerHTML:'',disabled:false,checked:false,hidden:false,href:'',dataset:{},classList:{add(){},remove(){},toggle(){}},setAttribute(){},removeAttribute(){},querySelector(){return node()}}}
global.window={localStorage:{getItem(){return ''}},sessionStorage:{getItem(){return ''}},location:{host:''},matchMedia(){return {matches:false}}};
global.navigator={userAgent:'test'};global.atob=()=>'';global.document={querySelector(s){if(!elements.has(s))elements.set(s,node());return elements.get(s)}};
global.fetch=async(url)=>{
 if(url.endsWith('/scan')){
  if(mode.value==='failure')throw Error('network_failed');
  return {ok:true,status:200,json:async()=>({fresh:1,invalid:1,active:false,accounts:[{number:1,email:'seat@example.test',seat:'seat-1',status:'healthy',quota:'fresh'}],repair_queue:[{number:1,email:'seat@example.test',seat:'seat-1',state:'failed'}]})};
 }
 return {ok:false,status:503,json:async()=>({})};
};
` + script + `
(async()=>{
 phoenixManagementKey='fixture-key';
 await scan();
 assert.equal(phoenixAccountRows.length,1);assert.equal(phoenixQueueRows.length,1);assert.equal(document.querySelector('#total-count').textContent,1);
 mode.value='failure';await scan();
 assert.deepEqual(phoenixAccountRows,[]);assert.deepEqual(phoenixQueueRows,[]);
 assert.equal(document.querySelector('#total-count').textContent,'—');assert.equal(document.querySelector('#revive').disabled,true);
 assert.equal(phoenixSetup.inventory,false);assert.match(document.querySelector('#account-results').textContent,/unavailable/i);
 assert.match(document.querySelector('#status').textContent,/Scan unavailable/i);
})().catch(e=>{console.error(e);process.exitCode=1});
`
	if out, err := exec.Command("node", "-e", harness).CombinedOutput(); err != nil {
		t.Fatalf("dashboard failed-scan invalidation failed: %v\n%s", err, out)
	}
}

func TestDashboardRepairQueueSummaryAndPrivacy(t *testing.T) {
	dashboard := string(dashboardPageHTML())
	for _, required := range []string{
		"String(rows.length)+' unresolved in repair queue · '+String(invalid||0)+' still invalid in CPA",
		"button.textContent=active?'Fixing invalid accounts'",
		"phoenixActionInFlight||Boolean(active)",
	} {
		if !strings.Contains(dashboard, required) {
			t.Fatalf("dashboard missing repair summary/label contract %q", required)
		}
	}
	for _, forbidden := range []string{"account_id", "auth_index", "auth_path", "access_token", "refresh_token", "oauth_state"} {
		if strings.Contains(dashboard, forbidden) {
			t.Fatalf("dashboard contains private identity/credential field %q", forbidden)
		}
	}
}

func TestDashboardRepairActionCopyDistinguishesCurrentInvalidWork(t *testing.T) {
	script := strings.Split(strings.Split(dashboardHTML, "<script>")[1], "</script>")[0]
	script = strings.Replace(script, "\nscan()\n", "\n", 1)
	harness := `
const assert=require('node:assert/strict'),elements=new Map();
global.document={querySelector(s){if(!elements.has(s))elements.set(s,{value:'',textContent:'',disabled:false});return elements.get(s)}};
` + script + `
const queue=[{email:'old@example.test',seat:'seat-old',state:'failed'}],button=document.querySelector('#revive');
phoenixSetActionAvailability(0,2,false,queue);
	assert.equal(button.textContent,'Fix invalid accounts');
phoenixSetActionAvailability(0,0,false,queue);
	assert.equal(button.textContent,'Fix invalid accounts');
phoenixSetActionAvailability(0,0,false,[{email:'old@example.test',seat:'seat-old',state:'skipped'}]);
	assert.equal(button.textContent,'Review unresolved accounts');
assert.equal(button.disabled,true);
phoenixSetActionAvailability(0,2,true,queue);
	assert.equal(button.textContent,'Fixing invalid accounts');
phoenixSetActionAvailability(0,2,false,[]);
	assert.equal(button.textContent,'Fix invalid accounts');
`
	if out, err := exec.Command("node", "-e", harness).CombinedOutput(); err != nil {
		t.Fatalf("dashboard repair action copy failed: %v\n%s", err, out)
	}
}

func TestDashboardVisualTokensAndResponsiveSemantics(t *testing.T) {
	dashboard := string(dashboardPageHTML())
	for _, token := range []string{"#8b8680", "#7f7a74", "#726d67", "#10b981", "#d1fae5", "#c65746", "#1d1b18", "#151412", "#3a3530", "prefers-color-scheme:dark", "prefers-reduced-motion:reduce", "focus-visible", "grid-template-columns:1fr", "status-badge", "aria-live=\"polite\""} {
		if !strings.Contains(dashboard, token) {
			t.Fatalf("dashboard missing visual/accessibility token %q", token)
		}
	}
}

func TestDashboardPhoenixBrandCountsAndConsequences(t *testing.T) {
	dashboard := string(dashboardPageHTML())
	for _, required := range []string{"<svg viewBox=\"0 0 24 24\"", "id=\"fresh\" class=\"count-badge\"", "id=\"invalid\" class=\"count-badge\"", "Send one small request through each eligible fresh seat.", "Automatic browser", "Regular Chrome", "Agent Autofill only", "https://github.com/lifan-builds/agent-autofill", "--success-bg:#064e3b4d", "--error-bg:#c657463d", ".status-badge.neutral"} {
		if !strings.Contains(dashboard, required) {
			t.Fatalf("dashboard missing polish contract %q", required)
		}
	}
	if strings.Contains(dashboard, ">live<") {
		t.Fatal("redundant live pills must be removed")
	}
}

func TestDashboardIdentityProjectionAllowsEmailAndSeatOnly(t *testing.T) {
	dashboard := string(dashboardPageHTML())
	for _, field := range []string{"row.email", "row.seat", "account status"} {
		if !strings.Contains(strings.ToLower(dashboard), strings.ToLower(field)) {
			t.Fatalf("dashboard missing allowed identity projection %q", field)
		}
	}
	for _, forbidden := range []string{"account_id", "auth_index", "auth_path", "internal_key", "oauth_state"} {
		if strings.Contains(dashboard, forbidden) {
			t.Fatalf("dashboard exposes forbidden field %q", forbidden)
		}
	}
}

func TestDashboardAutomaticRepairDoesNotOpenPopupsOrFetchCodes(t *testing.T) {
	script := strings.Split(strings.Split(dashboardHTML, "<script>")[1], "</script>")[0]
	script = strings.Replace(script, "\nscan()\n", "\n", 1)
	harness := `
const assert=require('node:assert/strict');
const elements=new Map();
global.document={querySelector(selector){
  if(!elements.has(selector))elements.set(selector,{value:'',textContent:'',hidden:false,classList:{add(){},remove(){}},setAttribute(k,v){this[k]=v},getAttribute(k){return this[k]},removeAttribute(k){delete this[k]}});
  return elements.get(selector);
}};
global.window={open(){throw Error('automatic repair must not open dashboard popups')}};
const scheduled=[];
global.setTimeout=fn=>scheduled.push(fn);
` + script + `
(async()=>{
  post=async()=>({job_id:'fixture',state:'running'});
  let attempt=1;
  authenticatedFetch=async path=>{
    assert.ok(!path.includes('/revive/code'),'the browser controller owns code submission');
    return {ok:true,json:async()=>({automatic:true,state:'awaiting_user',done:attempt-1,total:2,
      oauth_url:'https://auth.openai.com/oauth/authorize?state='+attempt,email:'same@example.test',
      automation_status:'Entering verification code'})};
  };
  await run('revive');
  await new Promise(resolve=>setImmediate(resolve));
  assert.match(document.querySelector('#status').textContent,/Entering verification code/);
  assert.equal(document.querySelector('#agent-login').hidden,true);
  attempt=2;
  await scheduled.shift()();
  assert.match(document.querySelector('#status').textContent,/1\/2/);
  assert.equal(document.querySelector('#verification-code').value,'');
})().catch(error=>{console.error(error);process.exitCode=1});
`
	if out, err := exec.Command("node", "-e", harness).CombinedOutput(); err != nil {
		t.Fatalf("automatic dashboard flow failed: %v\n%s", err, out)
	}
}

func TestDashboardAgentModeRejectsLateCodeFromPreviousAttempt(t *testing.T) {
	script := strings.Split(strings.Split(dashboardHTML, "<script>")[1], "</script>")[0]
	script = strings.Replace(script, "\nscan()\n", "\n", 1)
	harness := `
const assert=require('node:assert/strict'),elements=new Map();
global.document={querySelector(s){if(!elements.has(s))elements.set(s,{value:'',checked:false,textContent:'',classList:{add(){},remove(){}},setAttribute(k,v){this[k]=v},removeAttribute(k){delete this[k]}});return elements.get(s)}};
global.window={};global.setTimeout=()=>{};
` + script + `
(async()=>{
  document.querySelector('#agent-mode').checked=true;
  post=async(kind,body)=>{assert.equal(kind,'revive');assert.equal(body.browser_mode,'agent');return {state:'running'}};
  await run('revive');
  phoenixPolledJob='job';
  const row=attempt=>({attempt,oauth_url:'https://auth.openai.com/oauth/authorize?state='+attempt,email:'same@example.test',seat:'seat-12345678'});
  phoenixShowAgentLogin(row('first'));
  let release;
  authenticatedFetch=()=>new Promise(resolve=>release=resolve);
  const pending=phoenixReadAgentCode('job','first');
  phoenixShowAgentLogin(row('second'));
  release({ok:true,json:async()=>({attempt:'first',detected:true,code:'111111'})});
  await pending;
  assert.equal(document.querySelector('#verification-code').value,'');
  authenticatedFetch=async()=>({ok:true,json:async()=>({attempt:'second',detected:true,code:'222222'})});
  await phoenixReadAgentCode('job','second');
  assert.equal(document.querySelector('#verification-code').value,'222222');
  phoenixClearAgentLogin();
  assert.equal(document.querySelector('#agent-login').hidden,true);
  assert.equal(document.querySelector('#verification-code').value,'');
})().catch(e=>{console.error(e);process.exitCode=1});
`
	if out, err := exec.Command("node", "-e", harness).CombinedOutput(); err != nil {
		t.Fatalf("agent code handoff failed: %v\n%s", err, out)
	}
}

func TestDashboardScheduleSaveAndFailedSavePreserveInputs(t *testing.T) {
	script := strings.Split(strings.Split(dashboardHTML, "<script>")[1], "</script>")[0]
	script = strings.Replace(script, "\nscan()\n", "\n", 1)
	harness := `
const assert=require('node:assert/strict'),elements=new Map();
global.document={querySelector(s){if(!elements.has(s))elements.set(s,{value:'',checked:false,textContent:'',classList:{add(){},remove(){}},setAttribute(k,v){this[k]=v},removeAttribute(k){delete this[k]}});return elements.get(s)}};
global.window={};global.setTimeout=()=>{};
` + script + `
(async()=>{
  const initial={enabled:false,time:'09:00',timezone:'America/Los_Angeles',next_run:0,last_run:0};
  authenticatedFetch=async()=>({ok:true,json:async()=>initial});
  await loadIgniteSchedule();
  assert.match(document.querySelector('#schedule-status').textContent,/off/);
  document.querySelector('#schedule-enabled').checked=true;
  document.querySelector('#schedule-time').value='08:30';
  document.querySelector('#ignite-schedule').oninput();
  assert.equal(phoenixSetup.schedule,'pending','unsaved schedule edits must remain unresolved');
  await loadIgniteSchedule();
  assert.equal(document.querySelector('#schedule-time').value,'08:30','refresh must preserve unsaved edits');
  post=async(path,body)=>{
    assert.equal(path,'ignite/schedule');assert.equal(body.enabled,true);assert.equal(body.time,'08:30');
    assert.equal(body.timezone,'America/Los_Angeles');assert.equal(body.acknowledge,'CPA_PHOENIX_ONE_CLICK');
    return {...body,next_run:1800000000,last_run:0};
  };
  await saveIgniteSchedule({preventDefault(){}});
  assert.match(document.querySelector('#schedule-status').textContent,/Next run/);
  post=async()=>{throw Error('offline')};
  document.querySelector('#schedule-time').value='10:00';
  await saveIgniteSchedule({preventDefault(){}});
  assert.equal(document.querySelector('#schedule-time').value,'10:00');
  assert.equal(document.querySelector('#schedule-save').disabled,false);
  assert.match(document.querySelector('#schedule-status').textContent,/Could not save/);
})().catch(e=>{console.error(e);process.exitCode=1});
`
	if out, err := exec.Command("node", "-e", harness).CombinedOutput(); err != nil {
		t.Fatalf("schedule dashboard failed: %v\n%s", err, out)
	}
}

func TestDashboardQuotaFloorSelectionAndOptionalSave(t *testing.T) {
	script := strings.Split(strings.Split(dashboardHTML, "<script>")[1], "</script>")[0]
	script = strings.Replace(script, "\nscan()\n", "\n", 1)
	harness := `
const assert=require('node:assert/strict'),elements=new Map();
global.document={querySelector(s){if(!elements.has(s))elements.set(s,{value:'',checked:false,textContent:'',innerHTML:'',disabled:false,classList:{add(){},remove(){}}});return elements.get(s)}};
global.window={};global.setTimeout=()=>{};
` + script + `
(async()=>{
  authenticatedFetch=async()=>({ok:true,json:async()=>({enabled:true,target_seat:'seat-b',threshold_percent:90,accounts:[
    {email:'one@example.test',seat:'seat-a',status:'healthy'},
    {email:'two@example.test',seat:'seat-b',status:'disabled'}
  ]})});
  await loadQuotaFloor();
  assert.equal(document.querySelector('#quota-enabled').checked,true);
  assert.equal(document.querySelector('#quota-account').value,'seat-b');
  assert.equal(document.querySelector('#quota-threshold').textContent,'Configured 5-hour usage threshold: 90%');
  assert.match(document.querySelector('#quota-account').innerHTML,/two@example.test/);
  assert.match(document.querySelector('#quota-status').textContent,/two@example.test/);
  document.querySelector('#quota-enabled').checked=false;
  document.querySelector('#quota-account').value='seat-a';
  document.querySelector('#quota-floor').onchange();
  assert.equal(phoenixSetup.quota,'pending','unsaved quota edits must remain unresolved');
  post=async(path,body)=>{assert.equal(path,'quota-floor');assert.deepEqual(body,{acknowledge:'CPA_PHOENIX_ONE_CLICK',enabled:false,target_seat:'seat-a'});return {enabled:false,target_seat:'seat-a',accounts:[{email:'one@example.test',seat:'seat-a',status:'healthy'}]}};
  await saveQuotaFloor({preventDefault(){}});
  assert.match(document.querySelector('#quota-status').textContent,/off/i);
  post=async()=>{throw Error('offline')};
  document.querySelector('#quota-enabled').checked=true;
  document.querySelector('#quota-account').value='seat-b';
  await saveQuotaFloor({preventDefault(){}});
  assert.equal(document.querySelector('#quota-account').value,'seat-b');
  assert.match(document.querySelector('#quota-status').textContent,/Could not save/);
})().catch(e=>{console.error(e);process.exitCode=1});
`
	if out, err := exec.Command("node", "-e", harness).CombinedOutput(); err != nil {
		t.Fatalf("quota-floor dashboard failed: %v\n%s", err, out)
	}
}

func TestDashboardQuotaFloorStatusProjectionIsTruthful(t *testing.T) {
	script := strings.Split(strings.Split(dashboardHTML, "<script>")[1], "</script>")[0]
	script = strings.Replace(script, "\nscan()\n", "\n", 1)
	harness := `
const assert=require('node:assert/strict'),elements=new Map();
global.document={querySelector(s){if(!elements.has(s))elements.set(s,{value:'',checked:false,textContent:'',innerHTML:'',hidden:false,disabled:false,classList:{add(){},remove(){}}});return elements.get(s)}};
global.window={};global.setTimeout=()=>{};
` + script + `
const base={enabled:true,target_seat:'seat-b',threshold_percent:95,target_status:'healthy',accounts:[{email:'two@example.test',seat:'seat-b',status:'healthy'}]};
renderQuotaFloor({...base,last_status:'disable_failed',controller_owned:true});
let status=document.querySelector('#quota-status').textContent;
assert.match(status,/could not disable/i);
assert.doesNotMatch(status,/Protecting|Phoenix-controlled/);
assert.equal(phoenixSetup.quota,'attention');
renderQuotaFloor({...base,target_status:'disabled',last_status:'disabled',controller_owned:true});
status=document.querySelector('#quota-status').textContent;
assert.match(status,/disabled by Phoenix/i);
assert.match(status,/5-hour reset/i);
renderQuotaFloor({...base,last_status:'allowed',controller_owned:false});
status=document.querySelector('#quota-status').textContent;
assert.match(status,/currently allowed below the 5-hour threshold/i);
renderQuotaFloor({...base,last_status:'enabled',controller_owned:false});
status=document.querySelector('#quota-status').textContent;
assert.match(status,/re-enabled after the primary 5-hour reset/i);
`
	if out, err := exec.Command("node", "-e", harness).CombinedOutput(); err != nil {
		t.Fatalf("quota-floor status projection failed: %v\n%s", err, out)
	}
}

func TestDashboardRepairProgressSummaryAndCancelControls(t *testing.T) {
	script := strings.Split(strings.Split(dashboardHTML, "<script>")[1], "</script>")[0]
	script = strings.Replace(script, "\nscan()\n", "\n", 1)
	harness := `
const assert=require('node:assert/strict'),elements=new Map(),calls=[];
function node(){return {value:'',textContent:'',innerHTML:'',hidden:true,disabled:false,checked:false,href:'',title:'',dataset:{},classList:{toggle(){},add(){},remove(){}},setAttribute(){},removeAttribute(){},querySelector(){return node()}}}
global.document={querySelector(s){if(!elements.has(s))elements.set(s,node());return elements.get(s)}};
global.window={};global.setTimeout=()=>{};
` + script + `
(async()=>{
 phoenixRenderJobUI({kind:'revive',state:'running',done:1,total:3,email:'mock@example.test',seat:'seat-1',automation_status:'verification_code_waiting'},true);
 assert.match(document.querySelector('#job-progress').textContent,/1\/3 complete/);
 assert.match(document.querySelector('#job-progress').textContent,/Current: mock@example.test/);
 assert.match(document.querySelector('#job-progress').textContent,/Waiting for a fresh code/);
 phoenixRenderJobUI({kind:'revive',state:'completed',done:3,total:3,summary:{repaired:2,skipped:1}},true);
 assert.equal(document.querySelector('#repair-summary').hidden,false);
 assert.match(document.querySelector('#repair-summary-text').textContent,/2 repaired · 1 skipped/);
 phoenixActiveKind='revive';phoenixActiveJob=true;phoenixActionInFlight=true;phoenixPolledJob='job-1';phoenixSetActionAvailability(0,1,true,[]);
 assert.equal(document.querySelector('#cancel-revive').hidden,false);
 assert.equal(document.querySelector('#cancel-revive').disabled,false);
 authenticatedFetch=async(url,options)=>{calls.push({url,options});return {ok:true,json:async()=>({state:'cancelled'})}};
 scan=async()=>{};
 await phoenixCancelRevive();
 assert.equal(calls.length,1);
 assert.match(calls[0].url,/revive\/cancel\?id=job-1/);
 assert.equal(calls[0].options.method,'POST');
})().catch(e=>{console.error(e);process.exitCode=1});
`
	if out, err := exec.Command("node", "-e", harness).CombinedOutput(); err != nil {
		t.Fatalf("dashboard repair progress/cancel behavior failed: %v\n%s", err, out)
	}
}

func TestDashboardRetryTimedOutUsesSelectedRepairPath(t *testing.T) {
	script := strings.Split(strings.Split(dashboardHTML, "<script>")[1], "</script>")[0]
	script = strings.Replace(script, "\nscan()\n", "\n", 1)
	harness := `
const assert=require('node:assert/strict'),elements=new Map(),calls=[];
function node(){return {value:'',textContent:'',innerHTML:'',hidden:true,disabled:false,checked:false,href:'',title:'',dataset:{},classList:{toggle(){},add(){},remove(){}},setAttribute(){},removeAttribute(){},querySelector(){return node()}}}
global.document={querySelector(s){if(!elements.has(s))elements.set(s,node());return elements.get(s)}};
global.window={};global.setTimeout=()=>{};
` + script + `
(async()=>{
 phoenixQueueRows=[{email:'timeout@example.test',seat:'seat-timeout',state:'skipped',reason:'oauth_timeout',recoverable:true},{email:'provider@example.test',seat:'seat-provider',state:'skipped',reason:'provider_auth_error'}];
 document.querySelector('#agent-mode').checked=false;
 post=async(path,body)=>{calls.push({path,body});return {job_id:'recover-job',state:'running',total:1}};
 poll=async()=>{};
 await run('recover');
 assert.deepEqual(calls,[{path:'revive/recover',body:{acknowledge:'CPA_PHOENIX_ONE_CLICK',browser_mode:'agent',seats:['seat-timeout']}}]);
})().catch(e=>{console.error(e);process.exitCode=1});
`
	if out, err := exec.Command("node", "-e", harness).CombinedOutput(); err != nil {
		t.Fatalf("dashboard timed-out retry behavior failed: %v\n%s", err, out)
	}
}

func TestDashboardRetryTimedOutBatchesByDurableJob(t *testing.T) {
	script := strings.Split(strings.Split(dashboardHTML, "<script>")[1], "</script>")[0]
	script = strings.Replace(script, "\nscan()\n", "\n", 1)
	harness := `
const assert=require('node:assert/strict'),elements=new Map(),calls=[];
function node(){return {value:'',textContent:'',innerHTML:'',hidden:true,disabled:false,checked:false,href:'',title:'',dataset:{},classList:{toggle(){},add(){},remove(){}},setAttribute(){},removeAttribute(){},querySelector(){return node()}}}
global.document={querySelector(s){if(!elements.has(s))elements.set(s,node());return elements.get(s)}};
global.window={};global.setTimeout=()=>{};
` + script + `
(async()=>{
 phoenixQueueRows=[{email:'new@example.test',seat:'seat-new',job_id:'job-new',state:'skipped',reason:'oauth_timeout',recoverable:true},{email:'old@example.test',seat:'seat-old',job_id:'job-old',state:'skipped',reason:'workspace_missing',recoverable:true}];
 post=async(path,body)=>{calls.push({path,body});return {job_id:'recover-job',state:'running',total:1}};
 poll=async()=>{};
 await run('recover');
 assert.deepEqual(calls,[{path:'revive/recover',body:{acknowledge:'CPA_PHOENIX_ONE_CLICK',browser_mode:'agent',seats:['seat-new']}}]);
})().catch(e=>{console.error(e);process.exitCode=1});
`
	if out, err := exec.Command("node", "-e", harness).CombinedOutput(); err != nil {
		t.Fatalf("dashboard cross-job retry batching failed: %v\n%s", err, out)
	}
}

func TestDashboardRepairProgressUsesCurrentProjection(t *testing.T) {
	script := strings.Split(strings.Split(dashboardHTML, "<script>")[1], "</script>")[0]
	script = strings.Replace(script, "\nscan()\n", "\n", 1)
	harness := `
const assert=require('node:assert/strict'),elements=new Map();
global.document={querySelector(s){if(!elements.has(s))elements.set(s,{textContent:'',hidden:false});return elements.get(s)}};
` + script + `
const text=phoenixRenderJobProgress({done:2,total:4,current_email:'current@example.test',current_seat:'seat-current',automation_status:'verification_code_waiting'});
assert.match(text,/2\/4 complete/);
assert.match(text,/Current: current@example.test · seat-current/);
assert.match(text,/Waiting for a fresh code/);
`
	if out, err := exec.Command("node", "-e", harness).CombinedOutput(); err != nil {
		t.Fatalf("dashboard current repair projection failed: %v\n%s", err, out)
	}
}

func TestDashboardOTPStatusExplainsAgentAutofill(t *testing.T) {
	script := strings.Split(strings.Split(dashboardHTML, "<script>")[1], "</script>")[0]
	script = strings.Replace(script, "\nscan()\n", "\n", 1)
	harness := `
const assert=require('node:assert/strict'),elements=new Map();
function node(){return {value:'',textContent:'',innerHTML:'',hidden:true,disabled:false,checked:false,href:'',title:'',dataset:{},classList:{add(){},remove(){},toggle(){}},setAttribute(){},removeAttribute(){},querySelector(){return node()}}}
global.document={querySelector(s){if(!elements.has(s))elements.set(s,node());return elements.get(s)}};
global.window={};
` + script + `
(async()=>{
 let mode='ready';
 authenticatedFetch=async path=>{assert.equal(path,'/v0/management/plugins/cpa-phoenix/otp/status');return {ok:true,json:async()=>({agent_autofill:mode,preferred:'agent_autofill',fallback:'none'})}};
 await loadOTPStatus();
 assert.match(document.querySelector('#otp-status').textContent,/Agent Autofill ready/);
 assert.match(document.querySelector('#otp-setup-link').textContent,/settings/i);
 mode='installed';
 await loadOTPStatus();
 assert.match(document.querySelector('#otp-status').textContent,/readiness is unverified/);
 assert.match(document.querySelector('#otp-setup-link').textContent,/Check Agent Autofill/);
})().catch(e=>{console.error(e);process.exitCode=1});
`
	if out, err := exec.Command("node", "-e", harness).CombinedOutput(); err != nil {
		t.Fatalf("dashboard OTP status behavior failed: %v\n%s", err, out)
	}
}
