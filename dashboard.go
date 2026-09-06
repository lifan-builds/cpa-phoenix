package main

const dashboardHTML = `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width,initial-scale=1">
  <title>CPA Phoenix</title>
  <style>
    :root{color-scheme:light;--bg:#fff;--surface:#f6f6f6;--border:#e5e5e5;--border-strong:#d9d9d9;--text:#2d2a26;--muted:#6d6760;--subtle:#a29c95;--primary:#8b8680;--primary-hover:#7f7a74;--primary-active:#726d67;--success:#10b981;--success-bg:#d1fae5;--success-text:#065f46;--error:#c65746;--error-bg:#c6574624;--error-text:#8a3a30;--amber:#d97706;--amber-bg:#d9770624;--neutral-bg:#e5e5e5;--neutral-text:#6d6760;--radius:8px;--shadow:0 1px 2px rgba(45,42,38,.08)}
    @media(prefers-color-scheme:dark){:root{color-scheme:dark;--bg:#1d1b18;--surface:#151412;--border:#3a3530;--border-strong:#4a433d;--text:#f6f4f1;--muted:#c9c3bb;--subtle:#938b82;--success-bg:#064e3b4d;--success-text:#6ee7b7;--error-bg:#c657463d;--error-text:#f1b0a6;--amber:#f59e0b;--amber-bg:#f59e0b33;--neutral-bg:#3a3530;--neutral-text:#c9c3bb}}
    *{box-sizing:border-box}body{margin:0;background:var(--bg);color:var(--text);font:14px/1.5 system-ui,-apple-system,sans-serif}body>div,body>h1,body>p,body>main,body>#accounts,body>#status{max-width:960px;margin-left:auto;margin-right:auto}.shell{padding:2.5rem 1.25rem}.brand{display:flex;align-items:center;gap:.7rem;margin-bottom:1.8rem}.brand-mark{display:grid;place-items:center;width:2rem;height:2rem;border-radius:8px;background:var(--primary);color:#fff}.brand-mark svg{width:1.25rem;height:1.25rem}.brand h1{font-size:1.25rem;margin:0;letter-spacing:-.01em}.brand small{display:block;color:var(--muted);font-size:.78rem}main{display:grid;gap:1rem;grid-template-columns:repeat(2,minmax(0,1fr))}section{background:var(--surface);border:1px solid var(--border);border-radius:var(--radius);padding:1.25rem;box-shadow:var(--shadow)}section h2{font-size:1rem;margin:0 0 .35rem}section p{color:var(--muted);margin:.25rem 0 1rem}.count-badge,.status-badge{display:inline-flex;align-items:center;border-radius:999px;padding:.15rem .5rem;font-size:.75rem;font-weight:650}.count-badge{background:var(--neutral-bg);color:var(--neutral-text)}.status-badge.success{background:var(--success-bg);color:var(--success-text)}.status-badge.error{background:var(--error-bg);color:var(--error-text)}.status-badge.amber{background:var(--amber-bg);color:var(--amber)}.status-badge.neutral{background:var(--neutral-bg);color:var(--neutral-text)}button{width:100%;padding:.7rem 1rem;border:1px solid transparent;border-radius:var(--radius);background:var(--primary);color:#fff;font-weight:650;cursor:pointer;box-shadow:var(--shadow);transition:background .15s ease,transform .15s ease}button:hover:not(:disabled){background:var(--primary-hover)}button:active:not(:disabled){background:var(--primary-active);transform:translateY(1px)}button:disabled{opacity:.45;cursor:not-allowed}button:focus-visible,input:focus-visible{outline:2px solid var(--amber);outline-offset:2px}#status{margin-top:1rem;padding:.75rem 1rem;border:1px solid var(--border);border-radius:var(--radius);background:var(--surface);color:var(--muted);min-height:2.5rem}#auth-fallback{display:none;max-width:960px;margin:1rem auto;padding:.85rem 1rem;border:1px solid #d9770688;border-radius:var(--radius);background:#d9770612}#auth-fallback.on{display:block}#management-key{width:100%;padding:.6rem;margin:.4rem 0;border:1px solid var(--border-strong);border-radius:var(--radius);background:var(--bg);color:var(--text)}#accounts{margin-top:1.25rem}#accounts h2,#accounts h3{font-size:.9rem;margin:1.25rem 0 .5rem;color:var(--muted)}#accounts>div{display:flex;justify-content:space-between;gap:.75rem;padding:.55rem .7rem;border-bottom:1px solid var(--border);color:var(--muted)}@media(max-width:680px){.shell{padding:1.5rem .9rem}main{grid-template-columns:1fr}}@media(prefers-reduced-motion:reduce){*,*::before,*::after{scroll-behavior:auto!important;transition:none!important;animation:none!important}}
    #current-login{display:inline-flex;align-items:center;gap:.35rem;margin-top:.75rem;padding:.55rem .8rem;border:1px solid var(--border-strong);border-radius:var(--radius);background:var(--bg);color:var(--text);font-weight:650;text-decoration:none;box-shadow:var(--shadow)}#current-login:hover{background:var(--neutral-bg)}#verification-panel{display:none;margin-top:1rem;padding:.75rem;border:1px solid var(--border-strong);border-radius:var(--radius);background:var(--bg)}#verification-panel.on{display:block}#verification-code{width:8rem;padding:.55rem .7rem;border:1px solid var(--border-strong);border-radius:var(--radius);background:var(--surface);color:var(--text);font:600 1rem ui-monospace,monospace;letter-spacing:.15em}#verification-state{margin-left:.5rem;color:var(--muted)}[hidden]{display:none!important}
  </style>
</head>
<body>
  <div class="shell"><header class="brand"><span class="brand-mark" aria-hidden="true"><svg viewBox="0 0 24 24" fill="none" xmlns="http://www.w3.org/2000/svg"><path d="M12 2c.8 3.7 4.8 4.8 5.8 8.2.9 3.1-.8 6.4-3.8 7.7 1.3-1.9.9-4.2-.6-5.6-.2 2.5-1.8 4.1-3.9 5.1-2.4-1.2-3.8-3.5-3.4-6.1.4-2.9 3.2-4.6 5.9-9.3Z" fill="currentColor"/></svg></span><div><h1>CPA Phoenix</h1><small>Account maintenance center</small></div></header>
  <p>Two deliberate account-maintenance actions. Counts refresh before either action is enabled.</p>
  <div id="auth-fallback">
    <label for="management-key">CPA Management key (kept in memory only)</label>
    <input id="management-key" type="password" autocomplete="off" spellcheck="false">
  </div>
  <main>
    <section>
      <h2>Ignite Fresh Accounts</h2>
      <p><span id="fresh" class="count-badge" aria-label="fresh account count">Scanning…</span></p><p>Ignite sends one minimal request per fresh exact seat.</p>
      <button id="ignite" disabled>Ignite Fresh Accounts</button>
    </section>
    <section>
      <h2>Revive Invalid Accounts</h2>
      <p><span id="invalid" class="count-badge" aria-label="repair queue count">Scanning…</span></p><p>Revive replaces invalid credentials sequentially through guided login.</p>
      <button id="revive" disabled>Revive Invalid Accounts</button>
    </section>
  </main>
  <div id="accounts"></div>
  <p id="status" role="status" aria-live="polite"></p><a id="current-login" href="#" target="_blank" rel="noopener noreferrer" hidden>Open current login</a><div id="verification-panel"><label for="verification-code">Verification code (copy into the login window)</label><div><input id="verification-code" type="text" inputmode="numeric" autocomplete="one-time-code" readonly aria-readonly="true"><span id="verification-state" role="status" aria-live="polite">Waiting for a fresh code…</span></div></div></div>
  <script>
const cpaStoragePrefix='enc::v1::',cpaSecureStorageSalt='cli-proxy-api-webui::secure-storage';
let phoenixManagementKey='',phoenixRejectedManagementKey='',phoenixActionInFlight=false,phoenixScanSequence=0,phoenixPolledJob='',phoenixCodeRow='',phoenixCodePollInFlight=false;
let reviveTab=null,reviveOAuthURL='',reviveNavigated=false;

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

function phoenixRenderAccounts(rows,queue){
  const accounts=document.querySelector('#accounts');
  const safe=(value)=>String(value||'unknown').replace(/[^a-z0-9_-]/gi,'');
  const text=(value)=>String(value||'unknown').replace(/[&<>"']/g,c=>({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]));
  accounts.innerHTML='<h2>Account status</h2>'+(rows||[]).map(row=>'<div><span>'+Number(row.number||0)+'. '+text(row.email)+'</span><span>'+text(row.seat)+' </span><span class="status-badge '+(row.status==='invalid'?'error':row.status==='healthy'?'success':'amber')+'">'+safe(row.status)+'</span><span class="status-badge '+(row.quota==='fresh'?'success':row.quota==='active'?'amber':'neutral')+'">quota '+safe(row.quota)+'</span></div>').join('')+((queue||[]).length?'<h3>Incomplete repair queue</h3>'+queue.map(row=>'<div><span>'+Number(row.number||0)+'. '+text(row.email)+'</span><span>'+text(row.seat)+' </span><span class="status-badge '+(row.state==='failed'||row.state==='cancelled'?'error':row.state==='repaired'?'success':'amber')+'">'+safe(row.state)+(row.quarantined?' · quarantined':'')+'</span></div>').join(''):'')
}
function phoenixSetActionAvailability(fresh,invalid,active,queue){
  const locked=phoenixActionInFlight||Boolean(active),hasQueue=Boolean((queue||[]).length),button=document.querySelector('#revive');
  document.querySelector('#ignite').disabled=locked||Number(fresh||0)<=0;
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
  return 'Job '+job.state+' ('+(job.done||0)+'/'+(job.total||0)+')'+(job.reason?' — '+String(job.reason):'')
}
function phoenixPrepareReviveTab(){
  if(reviveTab&&!reviveTab.closed)return reviveTab;
	try{
		reviveTab=window.open('about:blank','cpa-phoenix-oauth');
		if(reviveTab){
			reviveNavigated=false;
			try{reviveTab.opener=null}catch(e){}
			try{reviveTab.focus()}catch(e){}
		}
  }catch(e){reviveTab=null}
  return reviveTab
}
function phoenixValidatedOAuthURL(value){
  try{
    const raw=String(value||''),decoded=raw.replace(/&amp;/g,'&');
    const parsed=new URL(decoded);
    if(parsed.protocol!=='https:'||!parsed.hostname||parsed.username||parsed.password)return '';
    for(const key of parsed.searchParams.keys()){if(key.startsWith('amp;'))return ''}
    return decoded
  }catch(e){return ''}
}
function phoenixNavigateReviveTab(url){
	try{
		reviveTab.location.replace(url);
		reviveNavigated=true;
		try{reviveTab.opener=null}catch(e){}
		try{reviveTab.focus()}catch(e){}
    document.querySelector('#status').textContent='Login window opened; complete sign-in there';
    return true
  }catch(e){return false}
}
function phoenixOpenReviveLogin(url){
  if(reviveTab&&!reviveTab.closed&&phoenixNavigateReviveTab(url))return true;
  if(reviveTab){try{reviveTab.close()}catch(e){}}
  reviveTab=null;
  reviveNavigated=false;
  if(phoenixPrepareReviveTab()&&phoenixNavigateReviveTab(url))return true;
  if(reviveTab){try{reviveTab.close()}catch(e){}}
  reviveTab=null;
  reviveNavigated=false;
  document.querySelector('#status').textContent='OAuth window unavailable; click Open current login to continue';
  return false
}
function phoenixClearLoginLink(){const link=document.querySelector('#current-login');link.hidden=true;link.removeAttribute('href')}
function phoenixShowLoginLink(url,email,seat){const link=document.querySelector('#current-login');link.setAttribute('href',url);link.textContent='Open current login'+(email?' · '+String(email):'')+(seat?' · '+String(seat):'');link.hidden=false}
function phoenixClearCode(){
  phoenixCodeRow='';
  document.querySelector('#verification-code').value='';
  document.querySelector('#verification-panel').classList.remove('on');
  document.querySelector('#verification-state').textContent='Waiting for a fresh code…'
}
async function phoenixPollCode(id){
  if(!id||phoenixPolledJob!==id||phoenixCodePollInFlight)return;
  phoenixCodePollInFlight=true;
  try{
    const response=await authenticatedFetch('/v0/management/plugins/cpa-phoenix/revive/code?id='+encodeURIComponent(id),{method:'POST'});
    if(!response.ok)return;
    const result=await response.json();
    if(phoenixPolledJob!==id)return;
    if(result.detected&&result.code){
      document.querySelector('#verification-code').value=String(result.code);
      document.querySelector('#verification-state').textContent='Code detected';
      document.querySelector('#verification-panel').classList.add('on')
    }else if(result.reason==='no_active_row'){phoenixClearCode()}
  }catch(e){}finally{phoenixCodePollInFlight=false}
}

async function scan(){
  const sequence=++phoenixScanSequence;
  try{
    const result=await post('scan');
    if(sequence!==phoenixScanSequence)return;
    document.querySelector('#fresh').textContent=Number(result.fresh||0)+' eligible';
    phoenixRenderAccounts(result.accounts,result.repair_queue);
    phoenixSetActionAvailability(result.fresh,result.invalid,result.active,result.repair_queue);
    if(result.active_job&&result.active_job.kind==='revive'){
      if(phoenixPolledJob!==result.active_job.id){phoenixPolledJob=result.active_job.id;poll(result.active_job.id,true)}
    }else{
      phoenixPolledJob='';
      phoenixClearLoginLink();
      phoenixClearCode();
      if(result.last_job)document.querySelector('#status').textContent=phoenixRenderJob(result.last_job)
    }
    document.querySelector('#auth-fallback').classList.remove('on')
  }catch(e){
    if(sequence!==phoenixScanSequence)return;
    phoenixPolledJob='';
    reviveOAuthURL='';
    phoenixClearLoginLink();
    phoenixSetActionAvailability(0,0,true,[]);
    if(e.message!=='management_key_required'&&e.message!=='management_unauthorized')document.querySelector('#status').textContent='Scan unavailable'
  }
}
async function run(kind){
  if(phoenixActionInFlight)return;
  if(kind==='revive'&&!phoenixPrepareReviveTab()){
    document.querySelector('#status').textContent='OAuth window blocked; allow pop-ups and click Revive again';
    scan();
    return
  }
  phoenixActionInFlight=true;
  phoenixSetActionAvailability(0,0,true,[]);
  document.querySelector('#status').textContent='Working…';
  try{
    const result=await post(kind,{acknowledge:'CPA_PHOENIX_ONE_CLICK'});
    document.querySelector('#status').textContent='Job '+(result.job_id||'started')+' is '+(result.state||'running');
    if(result.job_id){if(kind==='revive')phoenixPolledJob=result.job_id;poll(result.job_id,kind==='revive')}
  }catch(e){
    phoenixActionInFlight=false;
    phoenixClearLoginLink();
    if(kind==='revive'&&reviveTab){try{reviveTab.close()}catch(closeError){}reviveTab=null;reviveNavigated=false}
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
      if(revive&&result.oauth_url&&result.oauth_url!==reviveOAuthURL){
        const validated=phoenixValidatedOAuthURL(result.oauth_url);
        if(!validated){
          document.querySelector('#status').textContent='OAuth URL rejected';
          phoenixPolledJob='';
          reviveOAuthURL='';
          phoenixClearLoginLink();
          phoenixActionInFlight=false;
          scan();
          return
        }
        reviveOAuthURL=validated;
        phoenixShowLoginLink(validated,result.email,result.seat);
        const opened=phoenixOpenReviveLogin(validated);
        if(opened&&result.seat){
          document.querySelector('#status').textContent='This email has multiple seats; choose '+String(result.seat)+' in the login window';
        }
        const rowKey=String(result.email||'')+'|'+String(result.seat||'');
        if(rowKey!==phoenixCodeRow){phoenixCodeRow=rowKey;document.querySelector('#verification-code').value='';document.querySelector('#verification-state').textContent='Waiting for a fresh code…'}
        document.querySelector('#verification-panel').classList.add('on');
      }
      if(revive&&result.oauth_url&&(!reviveTab||reviveTab.closed))document.querySelector('#status').textContent='OAuth window unavailable; click Open current login to continue';
      if(revive&&(result.state==='running'||result.state==='awaiting_user'))phoenixPollCode(id);
      if(result.state==='running'||result.state==='awaiting_user'){
        setTimeout(()=>poll(id,revive),1000);
        return
      }
    }
  }catch(e){
    if(e.message!=='management_key_required'&&e.message!=='management_unauthorized')document.querySelector('#status').textContent='Job status unavailable'
  }
  reviveOAuthURL='';
  phoenixPolledJob='';
  phoenixClearLoginLink();
  if(reviveTab){try{reviveTab.close()}catch(e){}reviveTab=null}
  reviveNavigated=false;
  phoenixActionInFlight=false;
  phoenixClearCode();
  scan()
}

document.querySelector('#current-login').onclick=event=>{
  const url=phoenixValidatedOAuthURL(document.querySelector('#current-login').getAttribute('href'));
  if(!url){event.preventDefault();return}
  if(phoenixOpenReviveLogin(url))event.preventDefault()
};
document.querySelector('#management-key').onchange=event=>{phoenixManagementKey=String(event.target.value||'').trim();scan()};
document.querySelector('#management-key').onkeydown=event=>{if(event.key==='Enter'){event.preventDefault();phoenixManagementKey=String(event.target.value||'').trim();scan()}};
document.querySelector('#ignite').onclick=()=>run('ignite');
document.querySelector('#revive').onclick=()=>run('revive');
scan()
  </script>
</body>
</html>`

func dashboardPageHTML() []byte { return []byte(dashboardHTML) }
