package main

const dashboardHTML = `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width,initial-scale=1">
  <title>CPA Phoenix</title>
  <style>
    :root{color-scheme:light;--bg:#f5f6f8;--surface:#fff;--border:#e5e7eb;--border-strong:#cdd2db;--text:#202735;--muted:#636e80;--subtle:#8992a1;--primary:#8b8680;--primary-hover:#7f7a74;--primary-active:#726d67;--success:#10b981;--success-bg:#d1fae5;--success-text:#065f46;--error:#c65746;--error-bg:#fbece9;--error-text:#a33c30;--amber:#a76413;--amber-bg:#fff2dc;--neutral-bg:#eef0f4;--neutral-text:#636e80;--accent:#cf623b;--accent-bg:#fcf0e9;--radius:16px;--shadow:0 3px 16px rgba(25,35,50,.025)}
    *{box-sizing:border-box}body{margin:0;background:var(--bg);color:var(--text);font:14px/1.55 system-ui,-apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif}button,input,select{font:inherit}a{color:var(--accent)}.shell{max-width:1200px;margin:auto;padding:0 32px 48px}.brand{display:flex;align-items:center;gap:12px;padding:24px 0;border-bottom:1px solid var(--border)}.brand-mark{display:grid;place-items:center;width:40px;height:40px;border-radius:12px;background:var(--accent);color:white}.brand-mark svg{width:28px;height:28px}.brand-name{font-size:17px;font-weight:750;letter-spacing:-.5px}.brand small{display:block;font-size:11px;color:var(--muted)}.header-note{margin-left:auto;color:var(--muted);font-size:12px}.intro{padding:32px 0 24px}.eyebrow{font-size:10px;text-transform:uppercase;letter-spacing:1.8px;font-weight:750;color:var(--accent)}h1{font-size:32px;letter-spacing:-1.2px;line-height:1.2;margin:8px 0 10px}h2,h3,p{margin:0}p{color:var(--muted)}.summary{display:grid;grid-template-columns:repeat(4,minmax(0,1fr));gap:14px;margin-bottom:28px}.metric{padding:18px 22px;background:var(--surface);border:1px solid var(--border);border-radius:var(--radius)}.metric-label{font-size:12px;color:var(--muted);display:flex;align-items:center;gap:7px}.dot{height:7px;width:7px;border-radius:50%;background:var(--subtle)}.dot.green{background:var(--success)}.dot.orange{background:var(--accent)}.dot.red{background:var(--error)}.metric strong{display:block;font-size:30px;line-height:1.2;letter-spacing:-1px;margin:10px 0 5px}.metric small{color:var(--muted);font-size:11px}.section-title{font-size:15px;letter-spacing:-.2px;margin-bottom:12px}main{display:grid;gap:18px;grid-template-columns:repeat(2,minmax(0,1fr))}section{background:var(--surface);border:1px solid var(--border);border-radius:var(--radius);padding:24px;box-shadow:var(--shadow)}.action-card{display:flex;flex-direction:column;align-items:flex-start}.card-heading{display:flex;align-items:center;gap:12px;margin-bottom:12px}.action-icon{display:grid;place-items:center;width:40px;height:40px;border-radius:12px;background:var(--accent-bg);color:var(--accent);font-size:23px}.repair-icon{background:var(--neutral-bg);color:var(--text)}section h2{font-size:17px;letter-spacing:-.4px}.action-card>p{min-height:44px;font-size:13px;margin-bottom:16px;max-width:440px}.count-badge,.status-badge{display:inline-flex;align-items:center;border-radius:6px;padding:3px 8px;font-size:11px;font-weight:650}.count-badge{background:var(--neutral-bg);color:var(--neutral-text);margin-top:5px}.status-badge.success{background:var(--success-bg);color:var(--success-text)}.status-badge.error{background:var(--error-bg);color:var(--error-text)}.status-badge.amber{background:var(--amber-bg);color:var(--amber)}.status-badge.neutral{background:var(--neutral-bg);color:var(--neutral-text)}button{padding:10px 16px;border:1px solid transparent;border-radius:8px;background:var(--accent);color:#fff;font-weight:650;cursor:pointer;transition:filter .15s,transform .15s}button:hover:not(:disabled){filter:brightness(.92)}button:active:not(:disabled){transform:translateY(1px)}button:disabled{opacity:.45;cursor:not-allowed}button:focus-visible,input:focus-visible,select:focus-visible,summary:focus-visible,a:focus-visible{outline:3px solid var(--accent);outline-offset:3px}.action-card button{width:100%;margin-top:auto}#revive{background:var(--text);color:var(--surface)}.option{display:flex;align-items:flex-start;gap:8px;font-size:12px;color:var(--muted);margin-top:14px}input[type=checkbox]{accent-color:var(--accent);width:15px;height:15px;flex-shrink:0;margin:2px 0}.action-note{font-size:11px;color:var(--muted);margin-top:14px}.schedule-panel{margin-top:18px;padding:0}summary{cursor:pointer;list-style:none;padding:20px 24px;display:flex;align-items:center;gap:12px}summary::-webkit-details-marker{display:none}.schedule-icon{font-size:22px;color:var(--muted)}summary strong{font-size:14px}summary small{display:block;font-size:12px;color:var(--muted)}.disclosure{margin-left:auto;color:var(--muted);transition:transform .15s}details[open] .disclosure{transform:rotate(180deg)}#ignite-schedule{border-top:1px solid var(--border);padding:20px 24px}.schedule-fields{display:grid;grid-template-columns:160px minmax(200px,1fr) auto;gap:16px;align-items:end;margin-top:16px}.field label{display:block;font-size:12px;font-weight:600;margin-bottom:6px}input:not([type=checkbox]),select{border:1px solid var(--border-strong);border-radius:8px;background:var(--surface);color:var(--text);padding:9px 12px;min-width:0} .field input{width:100%}#schedule-status{margin-top:14px;font-size:12px;overflow-wrap:anywhere}.schedule-help{font-size:12px;margin-top:8px;max-width:760px}.activity{display:flex;gap:12px;align-items:baseline;border:1px solid var(--border);background:var(--surface);border-radius:10px;padding:14px 18px;margin:22px 0}.activity strong{font-size:12px;white-space:nowrap}#status{font-size:12px;overflow-wrap:anywhere}#auth-fallback{display:none;margin-bottom:20px;padding:18px;border:1px solid var(--amber);border-radius:12px;background:var(--amber-bg)}#auth-fallback.on{display:block}#auth-fallback p{font-size:12px;margin-bottom:10px}#management-key{display:block;width:100%;margin-top:6px}.inventory{padding:0;overflow:hidden}.inventory-head{display:flex;align-items:center;justify-content:space-between;gap:16px;padding:22px 24px}.inventory-head h2{font-size:16px}.inventory-head p{font-size:12px;margin-top:3px}.filters{display:flex;gap:8px}#account-search{width:230px}.table-wrap{overflow-x:auto}table{border-collapse:collapse;width:100%;text-align:left;white-space:nowrap}th{font-size:10px;letter-spacing:.8px;text-transform:uppercase;color:var(--muted);background:var(--bg);font-weight:650;padding:11px 24px;border-block:1px solid var(--border)}td{padding:14px 24px;border-bottom:1px solid var(--border);font-size:12px}tbody tr:last-child td{border-bottom:0}tbody tr:hover{background:var(--bg)}.account-email{font-weight:600}.row-number{color:var(--subtle);font-weight:400;margin-right:12px;font-variant-numeric:tabular-nums}.seat{color:var(--muted)}.empty-state{padding:38px 24px;text-align:center;color:var(--muted)}.empty-state strong{display:block;color:var(--text);margin-bottom:5px}.inventory-foot{font-size:11px;color:var(--muted);padding:12px 24px;border-top:1px solid var(--border)}.queue-title{padding:18px 24px 10px;font-size:13px;border-top:1px solid var(--border)}#agent-login{background:var(--surface);padding:20px;border:1px solid var(--amber);border-radius:12px;margin-bottom:20px}#agent-login label{display:block;margin-top:12px}#verification-code{max-width:140px;margin:0 10px}footer{display:flex;justify-content:space-between;gap:16px;font-size:11px;color:var(--muted);padding-top:20px}
    @media(prefers-color-scheme:dark){:root{color-scheme:dark;--bg:#151412;--surface:#1d1b18;--border:#3a3530;--border-strong:#514b43;--text:#f6f4f1;--muted:#b5aea5;--subtle:#938b82;--success-bg:#064e3b4d;--success-text:#6ee7b7;--error-bg:#c657463d;--error-text:#f1b0a6;--amber:#f5bd69;--amber-bg:#f59e0b22;--neutral-bg:#35312c;--neutral-text:#c9c3bb;--accent:#e5845d;--accent-bg:#e5845d19}}
    @media(max-width:850px){.inventory-head{align-items:flex-start;flex-direction:column}.filters{width:100%}#account-search{flex:1;width:100%}.schedule-fields{grid-template-columns:1fr 2fr}.schedule-fields button{grid-column:1/-1}.metric{padding:16px}.shell{padding-inline:20px}}
    @media(max-width:600px){.shell{padding-inline:16px}.header-note{display:none}.intro{padding-top:24px}h1{font-size:28px}.summary{grid-template-columns:repeat(2,minmax(0,1fr));gap:10px;margin-bottom:24px}main{grid-template-columns:1fr}section{padding:20px}.metric strong{font-size:26px}.action-card>p{min-height:0}.inventory-head{padding:20px}.filters{flex-direction:column}.schedule-fields{grid-template-columns:1fr}.activity{align-items:flex-start;flex-direction:column;gap:4px}th,td{padding:12px 16px}footer{flex-direction:column;gap:4px}}
    @media(prefers-reduced-motion:reduce){*,*::before,*::after{scroll-behavior:auto!important;transition:none!important;animation:none!important}}
  </style>
</head>
<body>
  <div class="shell">
    <header class="brand"><span class="brand-mark" aria-hidden="true"><svg viewBox="0 0 24 24" fill="none" xmlns="http://www.w3.org/2000/svg"><path d="M12 2c.8 3.7 4.8 4.8 5.8 8.2.9 3.1-.8 6.4-3.8 7.7 1.3-1.9.9-4.2-.6-5.6-.2 2.5-1.8 4.1-3.9 5.1-2.4-1.2-3.8-3.5-3.4-6.1.4-2.9 3.2-4.6 5.9-9.3Z" fill="currentColor"/></svg></span><div><div class="brand-name">CPA Phoenix</div><small>Account maintenance</small></div><span class="header-note">Your accounts, ready for what’s next.</span></header>
    <div class="intro"><span class="eyebrow">Maintenance center</span><h1>A clear view of your accounts.</h1><p>Activate fresh accounts, restore access, and keep your workspaces ready.</p></div>
    <div id="auth-fallback"><strong>Connect to CPA</strong><p>Enter your management key to load accounts and enable maintenance.</p><label for="management-key">CPA Management key (kept in memory only)</label><input id="management-key" type="password" autocomplete="off" spellcheck="false" placeholder="Enter key, then press Enter"></div>
    <div class="summary" aria-label="Account overview">
      <div class="metric"><span class="metric-label"><span class="dot"></span>Total accounts</span><strong id="total-count">—</strong><small>Workspaces in CPA</small></div>
      <div class="metric"><span class="metric-label"><span class="dot green"></span>Healthy</span><strong id="healthy-count">—</strong><small>Accounts with valid access</small></div>
      <div class="metric"><span class="metric-label"><span class="dot orange"></span>Fresh &amp; eligible</span><strong id="fresh-count">—</strong><small>Ready for Ignite</small></div>
      <div class="metric"><span class="metric-label"><span class="dot red"></span>Needs repair</span><strong id="invalid-count">—</strong><small>Invalid accounts in CPA</small></div>
    </div>
    <h2 class="section-title">Maintain your accounts</h2>
    <main aria-label="Maintenance actions">
      <section class="action-card"><div class="card-heading"><span class="action-icon" aria-hidden="true">↗</span><div><h2>Ignite Fresh Accounts</h2><span id="fresh" class="count-badge" aria-label="fresh account count">Scanning…</span></div></div><p>Ignite sends one minimal request per fresh exact seat.</p><button id="ignite" disabled>Ignite Fresh Accounts</button><span class="action-note">Only eligible accounts are included. Used seats are skipped.</span></section>
      <section class="action-card"><div class="card-heading"><span class="action-icon repair-icon" aria-hidden="true">↻</span><div><h2>Revive Invalid Accounts</h2><span id="invalid" class="count-badge" aria-label="repair queue count">Scanning…</span></div></div><p>Revive opens Chrome, enters email codes, and repairs each workspace automatically.</p><button id="revive" disabled>Revive Invalid Accounts</button><label class="option"><input id="agent-mode" type="checkbox"> Use regular Chrome with the Phoenix repair skill</label></section>
    </main>
    <section class="schedule-panel"><details><summary><span class="schedule-icon" aria-hidden="true">◷</span><div><strong>Daily Ignite schedule</strong><small id="schedule-summary">Set a daily time to activate fresh accounts automatically.</small></div><span class="disclosure" aria-hidden="true">⌄</span></summary><form id="ignite-schedule"><label class="option"><input id="schedule-enabled" type="checkbox"> Ignite automatically every day</label><div class="schedule-fields"><div class="field"><label for="schedule-time">Daily time</label><input id="schedule-time" type="time" value="09:00" required></div><div class="field"><label for="schedule-timezone">Time zone</label><input id="schedule-timezone" type="text" value="America/Los_Angeles" required spellcheck="false" aria-describedby="timezone-help"></div><button id="schedule-save" type="submit" disabled>Save schedule</button></div><p id="schedule-status" role="status" aria-live="polite">Loading schedule…</p><p class="schedule-help" id="timezone-help">Use an IANA time zone, such as America/Los_Angeles. Runs while CPA is running, even with this page closed. After sleep or downtime, one missed run is caught up. Only fresh accounts are ignited.</p></form></details></section>
    <div class="activity"><strong>Activity</strong><p id="status" role="status" aria-live="polite">Scanning accounts…</p></div>
    <div id="agent-login" hidden><a id="current-login" target="_blank" rel="noopener noreferrer">Open current login</a><label>Verification code <input id="verification-code" readonly autocomplete="off"></label><span id="verification-state" role="status"></span></div>
    <section class="inventory" aria-labelledby="inventory-title"><div class="inventory-head"><div><h2 id="inventory-title">Account status</h2><p>Access and quota status across your workspaces.</p></div><div class="filters"><input id="account-search" type="search" placeholder="Search email or workspace…" aria-label="Search accounts by email or workspace"><select id="account-filter" aria-label="Filter accounts"><option value="all">All accounts</option><option value="healthy">Healthy</option><option value="invalid">Invalid</option><option value="fresh">Fresh quota</option></select></div></div><div id="accounts"><div class="empty-state">Loading account inventory…</div></div><div class="inventory-foot" id="account-results" role="status" aria-live="polite">Waiting for CPA…</div></section>
    <footer><span>CPA Phoenix · Account maintenance center</span><span id="scan-time">Counts are verified before actions are enabled.</span></footer>
  </div>
  <script>
const cpaStoragePrefix='enc::v1::',cpaSecureStorageSalt='cli-proxy-api-webui::secure-storage';
let phoenixManagementKey='',phoenixRejectedManagementKey='',phoenixActionInFlight=false,phoenixScanSequence=0,phoenixPolledJob='';
let phoenixAttempt='',phoenixCodeBusy=false;
let phoenixScheduleLoaded=false,phoenixScheduleDirty=false,phoenixScheduleSaving=false;

function renderIgniteSchedule(result){
  document.querySelector('#schedule-enabled').checked=Boolean(result.enabled);
  document.querySelector('#schedule-time').value=result.time;
  document.querySelector('#schedule-timezone').value=result.timezone;
  const format=value=>value?new Date(value*1000).toLocaleString(undefined,{timeZone:result.timezone}):'';
  let status=result.enabled?'Next run: '+format(result.next_run)+' · '+result.timezone:'Daily Ignite is off';
  if(result.last_run)status+=' · Last run: '+format(result.last_run)+(result.last_outcome?' — '+String(result.last_outcome).replaceAll('_',' '):'');
  document.querySelector('#schedule-status').textContent=status;
  document.querySelector('#schedule-summary').textContent=result.enabled?'Daily at '+result.time+' · '+result.timezone:'Off · Set a daily time to activate fresh accounts automatically.';
}
async function loadIgniteSchedule(){
  try{
    const response=await authenticatedFetch('/v0/management/plugins/cpa-phoenix/ignite/schedule');
    if(!response.ok)throw Error('unavailable');
    const result=await response.json();
    if(!phoenixScheduleDirty&&!phoenixScheduleSaving)renderIgniteSchedule(result);
    phoenixScheduleLoaded=true;
    document.querySelector('#schedule-save').disabled=false;
  }catch(e){document.querySelector('#schedule-status').textContent='Schedule unavailable. Refresh the page to retry.'}
  finally{if(phoenixScheduleLoaded)setTimeout(loadIgniteSchedule,30000)}
}
async function saveIgniteSchedule(event){
  event.preventDefault();
  if(phoenixScheduleSaving)return;
  phoenixScheduleSaving=true;
  const button=document.querySelector('#schedule-save');button.disabled=true;
  try{
    const result=await post('ignite/schedule',{acknowledge:'CPA_PHOENIX_ONE_CLICK',enabled:document.querySelector('#schedule-enabled').checked,time:document.querySelector('#schedule-time').value,timezone:document.querySelector('#schedule-timezone').value.trim()});
    renderIgniteSchedule(result);
    phoenixScheduleDirty=false;
  }catch(e){document.querySelector('#schedule-status').textContent='Could not save. Check the daily time and IANA time zone (for example America/Los_Angeles).'}
  finally{button.disabled=false;phoenixScheduleSaving=false}
}

function safeStorage(kind){try{return window[kind+'Storage']}catch(e){return null}}
function readStorageText(storage,name){try{return String(storage&&storage.getItem(name)||'').trim()}catch(e){return ''}}
function parseStoredValue(value){if(!value)return null;try{return JSON.parse(value)}catch(e){return value}}
function decodeCPAStorage(value){
  if(!value||!value.startsWith(cpaStoragePrefix))return value;
  try{
    const data=Uint8Array.from(atob(value.slice(cpaStoragePrefix.length)),c=>c.charCodeAt(0));
    const key=new TextEncoder().encode(cpaSecureStorageSalt+'|'+window.location.host+'|'+navigator.userAgent);
    const out=new Uint8Array(data.length);
    for(let i=0;i<data.length;i++)out[i]=data[i]^key[i%key.length];
    return new TextDecoder().decode(out)
  }catch(e){return ''}
}
function readCPAStorageValue(storage,name){
  const raw=readStorageText(storage,name);
  if(!raw)return null;
  return parseStoredValue(decodeCPAStorage(raw)||raw)
}
function readCPAAuthStoreKey(storage){
  const auth=readCPAStorageValue(storage,'cli-proxy-auth');
  if(!auth||typeof auth!=='object')return '';
  return String((auth.state&&auth.state.managementKey)||auth.managementKey||'').trim()
}
function cpaStoredManagementKey(){
  return String(readCPAStorageValue(safeStorage('session'),'managementKey')||readCPAStorageValue(safeStorage('local'),'managementKey')||readCPAAuthStoreKey(safeStorage('session'))||readCPAAuthStoreKey(safeStorage('local'))||'').trim()
}
function managementKey(){
  const typed=String(document.querySelector('#management-key').value||'').trim();
  if(typed){phoenixManagementKey=typed;return typed}
  if(phoenixManagementKey&&phoenixManagementKey!==phoenixRejectedManagementKey)return phoenixManagementKey;
  const stored=cpaStoredManagementKey();
  if(stored&&stored!==phoenixRejectedManagementKey)phoenixManagementKey=stored;
  return phoenixManagementKey
}
function requireManagementKey(){
  const key=managementKey();
  if(!key){
    document.querySelector('#auth-fallback').classList.add('on');
    document.querySelector('#status').textContent='CPA authentication required';
    throw Error('management_key_required')
  }
  return key
}
function clearManagementKey(rejectedKey=''){
  phoenixRejectedManagementKey=rejectedKey||phoenixManagementKey;
  phoenixManagementKey='';
  document.querySelector('#management-key').value='';
  document.querySelector('#auth-fallback').classList.add('on');
  document.querySelector('#status').textContent='CPA authentication rejected'
}
async function authenticatedFetch(url,options={}){
  const key=requireManagementKey();
  const headers=Object.assign({},options.headers||{},{'Authorization':'Bearer '+key,'Accept':'application/json'});
  const response=await fetch(url,Object.assign({},options,{headers}));
  if(response.status===401){clearManagementKey(key);throw Error('management_unauthorized')}
  return response
}
async function post(path,body={}){
  const response=await authenticatedFetch('/v0/management/plugins/cpa-phoenix/'+path,{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify(body)});
  if(!response.ok)throw Error('request_failed');
  return response.json()
}

let phoenixAccountRows=[],phoenixQueueRows=[];
function phoenixRenderAccounts(rows,queue){
  phoenixAccountRows=rows||[];phoenixQueueRows=queue||[];
  phoenixFilterAccounts();
}
function phoenixFilterAccounts(){
  const accounts=document.querySelector('#accounts');
  const query=String(document.querySelector('#account-search').value||'').trim().toLowerCase();
  const filter=document.querySelector('#account-filter').value||'all';
  const text=value=>String(value||'unknown').replace(/[&<>"']/g,c=>({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]));
  const badge=(value,kind)=>'<span class="status-badge '+kind+'">'+text(String(value||'unknown').replaceAll('_',' '))+'</span>';
  const identity=row=>'<td class="account-email"><span class="row-number">'+Number(row.number||0)+'</span>'+text(row.email)+'</td><td class="seat">'+text(row.seat)+'</td>';
  const rows=phoenixAccountRows.filter(row=>(filter==='all'||(filter==='fresh'?row.quota==='fresh':row.status===filter))&&(!query||String(row.email||'').toLowerCase().includes(query)||String(row.seat||'').toLowerCase().includes(query)));
  const table=(head,body)=>'<div class="table-wrap"><table><thead><tr><th scope="col">Account</th><th scope="col">Workspace</th>'+head+'</tr></thead><tbody>'+body+'</tbody></table></div>';
  accounts.innerHTML=rows.length?table('<th scope="col">Access</th><th scope="col">Quota</th>',rows.map(row=>'<tr>'+identity(row)+'<td>'+badge(row.status,row.status==='invalid'?'error':row.status==='healthy'?'success':'amber')+'</td><td>'+badge(row.quota,row.quota==='fresh'?'success':row.quota==='active'?'amber':'neutral')+'</td></tr>').join('')):'<div class="empty-state"><strong>'+(phoenixAccountRows.length?'No matching accounts':'No accounts found')+'</strong>'+(phoenixAccountRows.length?'Try another search or change the status filter.':'Accounts will appear here when CPA has workspace records.')+'</div>';
  if(phoenixQueueRows.length)accounts.innerHTML+='<h3 class="queue-title">Incomplete repair queue · '+phoenixQueueRows.length+'</h3>'+table('<th scope="col">Repair status</th>',phoenixQueueRows.map(row=>'<tr>'+identity(row)+'<td>'+badge(row.state,row.state==='failed'||row.state==='cancelled'?'error':row.state==='repaired'?'success':'amber')+(row.quarantined?' <span class="status-badge neutral">quarantined</span>':'')+'</td></tr>').join(''));
  document.querySelector('#account-results').textContent='Showing '+rows.length+' of '+phoenixAccountRows.length+' accounts'+(phoenixQueueRows.length?' · Repair queue shown separately':'');
}
function phoenixSetActionAvailability(fresh,invalid,active,queue){
  const locked=phoenixActionInFlight||Boolean(active),hasQueue=Boolean((queue||[]).length),button=document.querySelector('#revive');
  document.querySelector('#ignite').disabled=locked||Number(fresh||0)<=0;
  document.querySelector('#agent-mode').disabled=locked;
  button.disabled=locked||(Number(invalid||0)<=0&&!hasQueue);
  button.textContent=active?'Repair In Progress':(hasQueue?'Resume Repair Queue':'Revive Invalid Accounts');
  document.querySelector('#invalid').textContent=hasQueue?(String((queue||[]).length)+' in repair queue · '+String(invalid||0)+' still invalid in CPA'):String(invalid||0)+' actionable'
}
function phoenixRenderJob(job){
  let result=job&&job.result;
  if(typeof result==='string'){try{result=JSON.parse(result)}catch(e){result=null}}
  if(result&&typeof result==='object'&&Object.prototype.hasOwnProperty.call(result,'sent')){
    return 'Job '+job.state+' — eligible '+(result.eligible||0)+', sent '+(result.sent||0)+', skipped '+(result.skipped||0)+', ambiguous '+(result.unknown||0)
  }
  return 'Job '+job.state+' ('+(job.done||0)+'/'+(job.total||0)+')'+(job.reason?' — '+phoenixAutomationMessage(job.reason):'')
}
function phoenixAutomationMessage(status){
  return ({browser_starting:'Opening Phoenix’s Chrome window…',browser_ready:'Chrome is ready',
    browser_unavailable:'Chrome could not start. Install Google Chrome, then resume the queue.',
    browser_navigation_failed:'Could not open the login page. Check Phoenix’s Chrome window.',
    browser_automation_failed:'Finish sign-in in Phoenix’s Chrome window.',
    login_opened:'Opening sign-in…',email_submitted:'Email entered',email_code_requested:'Verification email requested',
    verification_code_waiting:'Waiting for a fresh email code in Thunderbird…',verification_code_submitted:'Verification code entered',
    verification_code_resent:'New verification email requested',workspace_selected:'Workspace selected; checking the replacement…',
    manual_workspace_selection_required:'Choose the workspace in Phoenix’s Chrome window to continue.',
    manual_password_required:'Complete the password step in Phoenix’s Chrome window.',
    manual_captcha_required:'Complete the challenge in Phoenix’s Chrome window.',
    manual_login_required:'Finish the current sign-in step in Phoenix’s Chrome window.',
    manual_recipient_mismatch:'The login page shows a different email. Switch to the queued account in Phoenix’s Chrome window.',
    callback_reached:'Login complete; validating the replacement…',login_window_closed:'Login window closed; checking authorization…'
  })[status]||String(status||'')
}
async function scan(){
  const sequence=++phoenixScanSequence;
  try{
    const result=await post('scan');
    if(sequence!==phoenixScanSequence)return;
    if(!phoenixScheduleLoaded)loadIgniteSchedule();
    document.querySelector('#fresh').textContent=Number(result.fresh||0)+' eligible';
    document.querySelector('#total-count').textContent=(result.accounts||[]).length;
    document.querySelector('#healthy-count').textContent=(result.accounts||[]).filter(row=>row.status==='healthy').length;
    document.querySelector('#fresh-count').textContent=Number(result.fresh||0);
    document.querySelector('#invalid-count').textContent=Number(result.invalid||0);
    document.querySelector('#scan-time').textContent='Last checked '+new Date().toLocaleTimeString([],{hour:'2-digit',minute:'2-digit'});
    phoenixRenderAccounts(result.accounts,result.repair_queue);
    phoenixSetActionAvailability(result.fresh,result.invalid,result.active,result.repair_queue);
    if(result.active_job&&result.active_job.kind==='revive'){
      if(phoenixPolledJob!==result.active_job.id){phoenixPolledJob=result.active_job.id;poll(result.active_job.id,true)}
    }else{
      phoenixPolledJob='';
      phoenixClearAgentLogin();
      document.querySelector('#status').textContent=result.last_job?phoenixRenderJob(result.last_job):'Accounts checked. '+(Number(result.fresh||0)+Number(result.invalid||0)>0?'Choose a maintenance action to get started.':'No maintenance needed right now.')
    }
    document.querySelector('#auth-fallback').classList.remove('on')
  }catch(e){
    if(sequence!==phoenixScanSequence)return;
    phoenixPolledJob='';
    phoenixSetActionAvailability(0,0,true,[]);
    document.querySelector('#revive').textContent='Revive Invalid Accounts';
    document.querySelector('#fresh').textContent='Count unavailable';
    document.querySelector('#invalid').textContent='Count unavailable';
    document.querySelector('#scan-time').textContent='Scan unavailable · Refresh the page to retry.';
    if(!phoenixAccountRows.length){
      document.querySelector('#accounts').innerHTML='<div class="empty-state"><strong>Waiting for account data</strong>Connect to CPA to load your workspaces.</div>';
      document.querySelector('#account-results').textContent='Account data unavailable';
    }
    if(e.message!=='management_key_required'&&e.message!=='management_unauthorized')document.querySelector('#status').textContent='Scan unavailable'
  }
}
async function run(kind){
  if(phoenixActionInFlight)return;
  phoenixActionInFlight=true;
  phoenixSetActionAvailability(0,0,true,[]);
  document.querySelector('#status').textContent='Working…';
  try{
    const body={acknowledge:'CPA_PHOENIX_ONE_CLICK'};
    if(kind==='revive'&&document.querySelector('#agent-mode').checked)body.browser_mode='agent';
    const result=await post(kind,body);
    document.querySelector('#status').textContent='Job '+(result.job_id||'started')+' is '+(result.state||'running');
    if(result.job_id){if(kind==='revive')phoenixPolledJob=result.job_id;poll(result.job_id,kind==='revive')}
  }catch(e){
    phoenixActionInFlight=false;
    if(e.message!=='management_key_required'&&e.message!=='management_unauthorized')document.querySelector('#status').textContent='Action unavailable';
    scan()
  }
}
async function poll(id,revive=false){
  const endpoint=revive?('/v0/management/plugins/cpa-phoenix/revive/poll?id='+encodeURIComponent(id)):('/v0/management/plugins/cpa-phoenix/state?id='+encodeURIComponent(id));
  try{
    const response=await authenticatedFetch(endpoint,{method:revive?'POST':'GET'});
    if(response.ok){
      const result=await response.json();
      document.querySelector('#status').textContent=phoenixRenderJob(result);
      if(revive&&result.automatic){
        phoenixClearAgentLogin();
        document.querySelector('#status').textContent=phoenixRenderJob(result)+(result.email?' · '+String(result.email):'')+(result.automation_status?' — '+phoenixAutomationMessage(result.automation_status):'');
      }
      if(revive&&!result.automatic&&result.oauth_url&&result.attempt){
        phoenixShowAgentLogin(result);
        phoenixReadAgentCode(id,result.attempt);
      }else if(revive&&!result.automatic){phoenixClearAgentLogin()}
      if(result.state==='running'||result.state==='awaiting_user'){
        setTimeout(()=>poll(id,revive),1000);
        return
      }
    }
  }catch(e){
    if(e.message!=='management_key_required'&&e.message!=='management_unauthorized')document.querySelector('#status').textContent='Job status unavailable'
  }
  phoenixPolledJob='';
  phoenixClearAgentLogin();
  phoenixActionInFlight=false;
  scan()
}

function phoenixClearAgentLogin(){
  phoenixAttempt='';
  document.querySelector('#agent-login').hidden=true;
  document.querySelector('#current-login').removeAttribute('href');
  document.querySelector('#verification-code').value='';
}
function phoenixShowAgentLogin(result){
  const url=new URL(result.oauth_url);
  if(url.origin!=='https://auth.openai.com'||url.username||url.password)throw Error('oauth_url_invalid');
  if(phoenixAttempt!==result.attempt){
    phoenixAttempt=result.attempt;
    document.querySelector('#verification-code').value='';
    document.querySelector('#verification-state').textContent='Waiting for fresh mail…';
  }
  const link=document.querySelector('#current-login');
  link.setAttribute('href',result.oauth_url);
  link.textContent='Open current login · '+String(result.email||'')+' · '+String(result.seat||'');
  document.querySelector('#agent-login').setAttribute('data-attempt',result.attempt);
  document.querySelector('#agent-login').hidden=false;
}
async function phoenixReadAgentCode(id,attempt){
  if(phoenixCodeBusy)return;
  phoenixCodeBusy=true;
  try{
    const response=await authenticatedFetch('/v0/management/plugins/cpa-phoenix/revive/code?id='+encodeURIComponent(id)+'&attempt='+encodeURIComponent(attempt),{method:'POST'});
    if(!response.ok)return;
    const result=await response.json();
    if(phoenixPolledJob!==id||phoenixAttempt!==attempt||result.attempt!==attempt)return;
    document.querySelector('#verification-code').value=result.detected?String(result.code||''):'';
    document.querySelector('#verification-state').textContent=result.detected?'Code ready':String(result.reason||'Waiting for fresh mail…');
  }catch(e){}finally{phoenixCodeBusy=false}
}

document.querySelector('#management-key').onchange=event=>{phoenixManagementKey=String(event.target.value||'').trim();scan()};
document.querySelector('#management-key').onkeydown=event=>{if(event.key==='Enter'){event.preventDefault();phoenixManagementKey=String(event.target.value||'').trim();scan()}};
document.querySelector('#account-search').oninput=phoenixFilterAccounts;
document.querySelector('#account-filter').onchange=phoenixFilterAccounts;
document.querySelector('#ignite').onclick=()=>run('ignite');
document.querySelector('#ignite-schedule').onsubmit=saveIgniteSchedule;
document.querySelector('#ignite-schedule').oninput=()=>{phoenixScheduleDirty=true};
document.querySelector('#revive').onclick=()=>run('revive');
scan()
  </script>
</body>
</html>`

func dashboardPageHTML() []byte { return []byte(dashboardHTML) }
