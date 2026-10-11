package main

import (
	"os/exec"
	"strings"
	"testing"
)

func TestDashboardSearchFiltersAndPreservesRepairQueue(t *testing.T) {
	script := strings.Split(strings.Split(dashboardHTML, "<script>")[1], "</script>")[0]
	script = strings.Replace(script, "\nscan()\n", "\n", 1)
	harness := `
const assert=require('node:assert/strict'),elements=new Map();
global.document={querySelector(s){if(!elements.has(s))elements.set(s,{value:'',textContent:'',innerHTML:''});return elements.get(s)}};
` + script + `
const search=document.querySelector('#account-search'),filter=document.querySelector('#account-filter'),accounts=document.querySelector('#accounts');
phoenixRenderAccounts([{number:1,email:'Alex@example.test',seat:'Engineering',status:'healthy',quota:'fresh'},{number:2,email:'Sam@example.test',seat:'Design',status:'invalid',quota:'unknown'}],[{number:2,email:'Sam@example.test',seat:'Design',state:'failed',quarantined:true}]);
assert.match(document.querySelector('#account-results').textContent,/Showing 2 of 2/);
search.value=' ENGINEERING ';search.oninput();
assert.match(accounts.innerHTML,/Alex@example.test/);
assert.match(accounts.innerHTML,/Unresolved repair queue/);
assert.match(document.querySelector('#account-results').textContent,/Showing 1 of 2/);
filter.value='invalid';filter.onchange();
assert.match(accounts.innerHTML,/No matching accounts/);
assert.match(accounts.innerHTML,/quarantined/);
search.value='';filter.value='fresh';filter.onchange();
assert.match(document.querySelector('#account-results').textContent,/Showing 1 of 2/);
filter.value='all';
phoenixRenderAccounts([{email:'<img src=x>',seat:'<script>',status:'healthy',quota:'fresh'}],[]);
assert.ok(!accounts.innerHTML.includes('<img'));
assert.ok(accounts.innerHTML.includes('&lt;img src=x&gt;'));
phoenixRenderAccounts([],[]);assert.match(accounts.innerHTML,/No accounts found/);
`
	if out, err := exec.Command("node", "-e", harness).CombinedOutput(); err != nil {
		t.Fatalf("dashboard filtering failed: %v\n%s", err, out)
	}
}

func TestDashboardNormalizesOAuthURLAtNavigationBoundary(t *testing.T) {
	script := strings.Split(strings.Split(dashboardHTML, "<script>")[1], "</script>")[0]
	script = strings.Replace(script, "\nscan()\n", "\n", 1)
	harness := `
const assert=require('node:assert/strict'),elements=new Map();
global.document={querySelector(s){if(!elements.has(s))elements.set(s,{value:'',textContent:'',hidden:false,setAttribute(k,v){this[k]=v},removeAttribute(k){delete this[k]},classList:{add(){},remove(){}}});return elements.get(s)}};
` + script + `
phoenixShowAgentLogin({oauth_url:'https://auth.openai.com/authorize?client_id=x&amp;state=s',attempt:'one',email:'seat@example.test',seat:'seat-12345678'});
assert.equal(document.querySelector('#current-login').href,'https://auth.openai.com/authorize?client_id=x&state=s');
assert.match(document.querySelector('#workspace-help').textContent,/seat-12345678/);
phoenixShowAgentLogin({oauth_url:'https://auth.openai.com/authorize?client_id=x&amp;state=s#nested&amp;fragment',attempt:'two',email:'seat@example.test',seat:'seat-12345678'});
assert.equal(document.querySelector('#current-login').href,'https://auth.openai.com/authorize?client_id=x&state=s#nested&amp;fragment');
assert.throws(()=>phoenixShowAgentLogin({oauth_url:'https://auth.openai.com/authorize?amp;client_id=x',attempt:'two',email:'seat@example.test',seat:'seat-12345678'}),/oauth_url_invalid/);
assert.match(phoenixAutomationMessage('provider_auth_error'),/OpenAI/);
assert.match(phoenixAutomationMessage('provider_auth_error'),/resume the repair queue/i);
`
	if out, err := exec.Command("node", "-e", harness).CombinedOutput(); err != nil {
		t.Fatalf("dashboard OAuth boundary normalization failed: %v\n%s", err, out)
	}
}

func TestDashboardSkipTargetsOnlyCurrentAgentAttempt(t *testing.T) {
	script := strings.Split(strings.Split(dashboardHTML, "<script>")[1], "</script>")[0]
	script = strings.Replace(script, "\nscan()\n", "\n", 1)
	harness := `
const assert=require('node:assert/strict'),elements=new Map(),calls=[];
global.document={querySelector(s){if(!elements.has(s))elements.set(s,{disabled:false,textContent:''});return elements.get(s)}};
` + script + `
post=async(path,body)=>{calls.push({path,body});return {state:'running'}};
(async()=>{
phoenixPolledJob='current-job';phoenixAttempt='current-attempt';
await phoenixSkipCurrent();
assert.deepEqual(calls,[{path:'revive/skip',body:{job_id:'current-job',attempt:'current-attempt',reason:'provider_auth_error'}}]);
assert.equal(document.querySelector('#skip-current').disabled,false);
phoenixAttempt='';await phoenixSkipCurrent();assert.equal(calls.length,1);
})().catch(e=>{console.error(e);process.exitCode=1});
`
	if out, err := exec.Command("node", "-e", harness).CombinedOutput(); err != nil {
		t.Fatalf("dashboard skip dispatch failed: %v\n%s", err, out)
	}
}

func TestDashboardAgentCodeUsesLiveInputValue(t *testing.T) {
	script := strings.Split(strings.Split(dashboardHTML, "<script>")[1], "</script>")[0]
	script = strings.Replace(script, "\nscan()\n", "\n", 1)
	harness := `
const assert=require('node:assert/strict'),elements=new Map();
global.document={querySelector(s){if(!elements.has(s))elements.set(s,{value:'',textContent:'',hidden:false});return elements.get(s)}};
` + script + `
authenticatedFetch=async()=>({ok:true,json:async()=>({attempt:'attempt-1',detected:true,code:'123456'})});
(async()=>{
phoenixPolledJob='job-1';phoenixAttempt='attempt-1';
await phoenixReadAgentCode('job-1','attempt-1');
assert.equal(document.querySelector('#verification-code').value,'123456');
assert.equal(document.querySelector('#verification-state').textContent,'Code ready');
})().catch(e=>{console.error(e);process.exitCode=1});
`
	if out, err := exec.Command("node", "-e", harness).CombinedOutput(); err != nil {
		t.Fatalf("dashboard code property contract failed: %v\n%s", err, out)
	}
}

func TestDashboardQuotaFloorCopyNamesPrimaryWindowAndResetRule(t *testing.T) {
	dashboard := string(dashboardPageHTML())
	for _, required := range []string{
		"5-hour Codex usage threshold",
		"Configured 5-hour usage threshold",
		"weekly window is informational",
		"primary 5-hour window reset",
	} {
		if !strings.Contains(dashboard, required) {
			t.Fatalf("dashboard missing quota scope copy %q", required)
		}
	}
	if strings.Contains(dashboard, "Phoenix-controlled") {
		t.Fatal("dashboard must not infer truthful protection from controller ownership alone")
	}
}
