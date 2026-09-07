package main

import (
	"os/exec"
	"strings"
	"testing"
)

func TestDashboardHasExactlyTwoActions(t *testing.T) {
	if strings.Count(dashboardHTML, "<button") != 2 {
		t.Fatal("dashboard must have exactly two buttons")
	}
	if strings.Count(string(dashboardPageHTML()), "<button") != 2 {
		t.Fatal("enhanced dashboard must retain exactly two buttons")
	}
	dashboard := string(dashboardPageHTML())
	if strings.Count(dashboard, "<script>") != 1 || strings.Count(dashboard, "async function scan()") != 1 || strings.Count(dashboard, "async function run(kind)") != 1 || strings.Count(dashboard, "async function poll(id,revive=false)") != 1 {
		t.Fatal("rendered dashboard must have one authoritative script")
	}
	for _, forbidden := range []string{"account picker", "scheduler", "analytics", "export"} {
		if strings.Contains(strings.ToLower(dashboardHTML), forbidden) {
			t.Fatalf("dashboard contains forbidden %q", forbidden)
		}
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
		"Number(invalid||0)<=0",
		"eligible '+(result.eligible||0)",
		"sent '+(result.sent||0)",
		"ambiguous '+(result.unknown||0)",
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

func TestDashboardRepairQueueSummaryAndPrivacy(t *testing.T) {
	dashboard := string(dashboardPageHTML())
	for _, required := range []string{
		"String((queue||[]).length)+' in repair queue · '+String(invalid||0)+' still invalid in CPA",
		"button.textContent=active?'Repair In Progress':(hasQueue?'Resume Repair Queue':'Revive Invalid Accounts')",
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
	for _, required := range []string{"<svg viewBox=\"0 0 24 24\"", "id=\"fresh\" class=\"count-badge\"", "id=\"invalid\" class=\"count-badge\"", "Ignite sends one minimal request per fresh exact seat.", "Revive opens Chrome, enters email codes, and repairs each workspace automatically.", "--success-bg:#064e3b4d", "--error-bg:#c657463d", ".status-badge.neutral"} {
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
