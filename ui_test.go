package main

import (
	"encoding/json"
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

func TestDashboardRevivePopupIsDirectClickAndValidated(t *testing.T) {
	dashboard := string(dashboardPageHTML())
	if strings.Count(dashboard, "window.open('about:blank','cpa-phoenix-oauth')") != 1 {
		t.Fatal("Revive must open exactly one named about:blank window")
	}
	for _, required := range []string{
		"reviveTab.opener=null",
		"reviveTab.focus()",
		"phoenixValidatedOAuthURL",
		"const raw=String(value||''),decoded=raw.replace(/&amp;/g,'&')",
		"return decoded",
		"phoenixOpenReviveLogin(validated)",
		"reviveNavigated",
		"reviveTab.location.replace(url)",
		"Login window opened; complete sign-in there",
		"OAuth window blocked; allow pop-ups and click Revive again",
		"phoenixSetActionAvailability(result.fresh,result.invalid,result.active,result.repair_queue)",
	} {
		if !strings.Contains(dashboard, required) {
			t.Fatalf("dashboard missing %q", required)
		}
	}
	if strings.Contains(dashboard, "window.open(x.oauth_url") || strings.Contains(dashboard, "phoenixPrepareReviveTab();if(reviveTab)reviveTab.location.replace") {
		t.Fatal("OAuth navigation must not open a second asynchronous popup")
	}
	openAt := strings.Index(dashboard, "window.open('about:blank','cpa-phoenix-oauth')")
	navigateAt := strings.Index(dashboard, "reviveTab.location.replace(url)")
	detachAt := strings.Index(dashboard, "reviveTab.opener=null")
	if openAt < 0 || detachAt <= openAt || navigateAt <= detachAt {
		t.Fatal("Revive must detach the opener before validated OAuth navigation starts")
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
	for _, required := range []string{"<svg viewBox=\"0 0 24 24\"", "id=\"fresh\" class=\"count-badge\"", "id=\"invalid\" class=\"count-badge\"", "Ignite sends one minimal request per fresh exact seat.", "Revive replaces invalid credentials sequentially through guided login.", "--success-bg:#064e3b4d", "--error-bg:#c657463d", ".status-badge.neutral"} {
		if !strings.Contains(dashboard, required) {
			t.Fatalf("dashboard missing polish contract %q", required)
		}
	}
	if strings.Contains(dashboard, ">live<") {
		t.Fatal("redundant live pills must be removed")
	}
}

func TestDashboardActiveLoginRecoveryAnchor(t *testing.T) {
	dashboard := string(dashboardPageHTML())
	for _, required := range []string{"id=\"current-login\"", "Open current login", "target=\"_blank\"", "rel=\"noopener noreferrer\"", "active_job", "phoenixShowLoginLink(validated,result.email,result.seat)", "phoenixClearLoginLink()", "[hidden]{display:none!important}", "setAttribute('href',url)", "removeAttribute('href')"} {
		if !strings.Contains(dashboard, required) {
			t.Fatalf("dashboard missing recovery affordance %q", required)
		}
	}
	if strings.Contains(dashboard, "window.open(x.oauth_url") {
		t.Fatal("recovery must not open an asynchronous second popup")
	}
}

func TestDashboardOAuthValidationExecutableEntityBoundary(t *testing.T) {
	dashboard := string(dashboardPageHTML())
	start := strings.Index(dashboard, "function phoenixValidatedOAuthURL")
	end := strings.Index(dashboard[start:], "\nfunction phoenixNavigateReviveTab")
	if start < 0 || end < 0 {
		t.Fatal("validator function missing")
	}
	fn := dashboard[start : start+end]
	script := fn + `
const values=[
  'https://login.test/a?client_id=x&code_challenge=abc%2Bxyz&redirect_uri=http%3A%2F%2F127.0.0.1%3A1455%2Fcallback&state=s&login_hint=fixture%40example.test&prompt=login&x=%2F',
  'https://login.test/a?client_id=x&amp;code_challenge=abc%2Bxyz&amp;redirect_uri=http%3A%2F%2F127.0.0.1%3A1455%2Fcallback&amp;state=s&amp;login_hint=fixture%40example.test&amp;prompt=login&amp;x=%2F',
  'https://LOGIN.TEST:443/a/../authorize?client_id=x&state=s&x=%2F',
  'https://login.test/a?client_id=x&amp;amp;state=s',
  'https://login.test/a?client_id=x&amp;amp;state',
  'http://login.test/a?state=s',
  'https://user:pass@login.test/a?state=s',
  'not a url'
];
console.log(JSON.stringify(values.map(phoenixValidatedOAuthURL)));`
	out, err := exec.Command("node", "-e", script).Output()
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatal(err)
	}
	want := "https://login.test/a?client_id=x&code_challenge=abc%2Bxyz&redirect_uri=http%3A%2F%2F127.0.0.1%3A1455%2Fcallback&state=s&login_hint=fixture%40example.test&prompt=login&x=%2F"
	preserved := "https://LOGIN.TEST:443/a/../authorize?client_id=x&state=s&x=%2F"
	if got[0] != want || got[1] != want || got[2] != preserved || got[3] != "" || got[4] != "" || got[5] != "" || got[6] != "" || got[7] != "" {
		t.Fatalf("unexpected executable validation results: %#v", got)
	}
	if strings.Count(got[1], "login_hint=") != 1 || strings.Count(got[1], "prompt=") != 1 || strings.Contains(got[1], "amp;") {
		t.Fatalf("browser validator altered entity/query contract: %q", got[1])
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

func TestDashboardAdvancesWorkspaceWithoutDetectedCode(t *testing.T) {
	dashboard := string(dashboardPageHTML())
	script := strings.Split(strings.Split(dashboard, "<script>")[1], "</script>")[0]
	script = strings.Replace(script, "\nscan()\n", "\n", 1)
	harness := `
const assert=require('node:assert/strict');
const elements=new Map();
global.document={querySelector(selector){
  if(!elements.has(selector))elements.set(selector,{value:'',textContent:'',hidden:false,classList:{add(){},remove(){}},setAttribute(k,v){this[k]=v},getAttribute(k){return this[k]},removeAttribute(k){delete this[k]}});
  return elements.get(selector);
}};
let opened=[],scheduled=[],blockPopup=false;
function popup(){return {closed:false,location:{replace(url){this.url=url}},focus(){},close(){this.closed=true}}}
global.window={open(){if(blockPopup)return null;const tab=popup();opened.push(tab);return tab}};
global.setTimeout=fn=>scheduled.push(fn);
`
	harness += script + `
(async()=>{
  let workspace=1;
  phoenixPolledJob='fixture';
  authenticatedFetch=async path=>{
    if(path.includes('/revive/code'))return new Promise(()=>{}); // Code lookup never finishes.
    return {ok:true,json:async()=>({state:'awaiting_user',oauth_url:'https://login.test/?state='+workspace,email:'same@example.test',seat:'Seat '+workspace})};
  };
  phoenixPrepareReviveTab();
  await poll('fixture',true);
  assert.equal(opened[0].location.url,'https://login.test/?state=1');
  opened[0].closed=true; // The first login's completion page closes its window.
  workspace=2;
  await scheduled.shift()();
  assert.equal(opened.length,2,'next workspace must reopen a closed login window');
  assert.equal(opened[1].location.url,'https://login.test/?state=2');
  workspace=3;
  await scheduled.shift()();
  assert.equal(opened.length,2,'reuse a window that remains available');
  assert.equal(opened[1].location.url,'https://login.test/?state=3');
  opened[1].closed=true;
  blockPopup=true;
  workspace=4;
  await scheduled.shift()();
  assert.match(document.querySelector('#status').textContent,/click Open current login/);
  assert.equal(document.querySelector('#current-login').hidden,false);
  assert.equal(phoenixPolledJob,'fixture');
  assert.equal(scheduled.length,1,'blocked popup must keep polling');
  await scheduled.shift()();
  assert.match(document.querySelector('#status').textContent,/click Open current login/,'recovery guidance must persist on the next poll');
  blockPopup=false;
  let prevented=false;
  document.querySelector('#current-login').onclick({preventDefault(){prevented=true}});
  assert.equal(prevented,true);
  assert.equal(reviveTab,opened[2],'recovery link must retain the window handle');
  assert.equal(reviveTab.location.url,'https://login.test/?state=4');
  const inaccessible=reviveTab;
  inaccessible.location.replace=()=>{throw new Error('navigation denied')};
  blockPopup=true;
  workspace=5;
  await scheduled.shift()();
  assert.equal(inaccessible.closed,true);
  assert.equal(reviveTab,null);
  assert.equal(phoenixPolledJob,'fixture');
  assert.equal(document.querySelector('#current-login').href,'https://login.test/?state=5');
  assert.equal(scheduled.length,1,'navigation failure must keep polling');
  assert.match(document.querySelector('#status').textContent,/click Open current login/);
})().catch(error=>{console.error(error);process.exitCode=1});
`
	if out, err := exec.Command("node", "-e", harness).CombinedOutput(); err != nil {
		t.Fatalf("workspace handoff failed: %v\n%s", err, out)
	}
}
