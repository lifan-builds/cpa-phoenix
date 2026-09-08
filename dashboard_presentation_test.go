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
assert.match(accounts.innerHTML,/Incomplete repair queue/);
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
