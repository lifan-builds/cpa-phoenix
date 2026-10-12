package main

const dashboardHTML = `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width,initial-scale=1">
  <title>CPA Phoenix</title>
  <style>
    :root{color-scheme:light;--bg:#f5f6f8;--surface:#fff;--border:#e5e7eb;--border-strong:#cdd2db;--text:#202735;--muted:#636e80;--subtle:#8992a1;--primary:#8b8680;--primary-hover:#7f7a74;--primary-active:#726d67;--success:#10b981;--success-bg:#d1fae5;--success-text:#065f46;--error:#c65746;--error-bg:#fbece9;--error-text:#a33c30;--amber:#a76413;--amber-bg:#fff2dc;--neutral-bg:#eef0f4;--neutral-text:#636e80;--accent:#cf623b;--accent-bg:#fcf0e9;--radius:16px;--shadow:0 3px 16px rgba(25,35,50,.025)}
    *{box-sizing:border-box}body{margin:0;background:var(--bg);color:var(--text);font:14px/1.55 system-ui,-apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif}button,input,select{font:inherit}a{color:var(--accent)}.shell{max-width:1200px;margin:auto;padding:0 32px 48px}.brand{display:flex;align-items:center;gap:12px;padding:24px 0;border-bottom:1px solid var(--border)}.brand-mark{display:grid;place-items:center;width:40px;height:40px;border-radius:12px;background:var(--accent);color:white}.brand-mark svg{width:28px;height:28px}.brand-name{font-size:17px;font-weight:750;letter-spacing:-.5px}.brand small{display:block;font-size:11px;color:var(--muted)}.header-note{margin-left:auto;color:var(--muted);font-size:12px}.intro{padding:32px 0 24px}.eyebrow{font-size:10px;text-transform:uppercase;letter-spacing:1.8px;font-weight:750;color:var(--accent)}h1{font-size:32px;letter-spacing:-1.2px;line-height:1.2;margin:8px 0 10px}h2,h3,p{margin:0}p{color:var(--muted)}.summary{display:grid;grid-template-columns:repeat(4,minmax(0,1fr));gap:14px;margin-bottom:28px}.metric{padding:18px 22px;background:var(--surface);border:1px solid var(--border);border-radius:var(--radius)}.metric-label{font-size:12px;color:var(--muted);display:flex;align-items:center;gap:7px}.dot{height:7px;width:7px;border-radius:50%;background:var(--subtle)}.dot.green{background:var(--success)}.dot.orange{background:var(--accent)}.dot.red{background:var(--error)}.metric strong{display:block;font-size:30px;line-height:1.2;letter-spacing:-1px;margin:10px 0 5px}.metric small{color:var(--muted);font-size:11px}.section-title{font-size:15px;letter-spacing:-.2px;margin-bottom:12px}main{display:grid;gap:18px;grid-template-columns:repeat(2,minmax(0,1fr))}section{background:var(--surface);border:1px solid var(--border);border-radius:var(--radius);padding:24px;box-shadow:var(--shadow)}.action-card{display:flex;flex-direction:column;align-items:flex-start}.card-heading{display:flex;align-items:center;gap:12px;margin-bottom:12px}.action-icon{display:grid;place-items:center;width:40px;height:40px;border-radius:12px;background:var(--accent-bg);color:var(--accent);font-size:23px}.repair-icon{background:var(--neutral-bg);color:var(--text)}section h2{font-size:17px;letter-spacing:-.4px}.action-card>p{min-height:44px;font-size:13px;margin-bottom:16px;max-width:440px}.count-badge,.status-badge{display:inline-flex;align-items:center;border-radius:6px;padding:3px 8px;font-size:11px;font-weight:650}.count-badge{background:var(--neutral-bg);color:var(--neutral-text);margin-top:5px}.status-badge.success{background:var(--success-bg);color:var(--success-text)}.status-badge.error{background:var(--error-bg);color:var(--error-text)}.status-badge.amber{background:var(--amber-bg);color:var(--amber)}.status-badge.neutral{background:var(--neutral-bg);color:var(--neutral-text)}button{padding:10px 16px;border:1px solid transparent;border-radius:8px;background:var(--accent);color:#fff;font-weight:650;cursor:pointer;transition:filter .15s,transform .15s}button:hover:not(:disabled){filter:brightness(.92)}button:active:not(:disabled){transform:translateY(1px)}button:disabled{opacity:.45;cursor:not-allowed}button:focus-visible,input:focus-visible,select:focus-visible,summary:focus-visible,a:focus-visible{outline:3px solid var(--accent);outline-offset:3px}.action-card button{width:100%;margin-top:auto}#revive{background:var(--text);color:var(--surface)}.option{display:flex;align-items:flex-start;gap:8px;font-size:12px;color:var(--muted);margin-top:14px}input[type=checkbox]{accent-color:var(--accent);width:15px;height:15px;flex-shrink:0;margin:2px 0}.action-note{font-size:11px;color:var(--muted);margin-top:14px}.schedule-panel{margin-top:18px;padding:0}.quota-panel{margin-top:18px}.quota-panel>p{font-size:13px;max-width:700px}.quota-fields{display:grid;grid-template-columns:minmax(220px,1fr) auto;gap:16px;align-items:end;margin-top:16px}.quota-fields .field label{display:block;font-size:12px;font-weight:600;margin-bottom:6px}.quota-fields select{width:100%}#quota-status{margin-top:14px;font-size:12px;overflow-wrap:anywhere}.quota-help{font-size:12px;margin-top:8px;max-width:760px}summary{cursor:pointer;list-style:none;padding:20px 24px;display:flex;align-items:center;gap:12px}summary::-webkit-details-marker{display:none}.schedule-icon{font-size:22px;color:var(--muted)}summary strong{font-size:14px}summary small{display:block;font-size:12px;color:var(--muted)}.disclosure{margin-left:auto;color:var(--muted);transition:transform .15s}details[open] .disclosure{transform:rotate(180deg)}#ignite-schedule{border-top:1px solid var(--border);padding:20px 24px}.schedule-fields{display:grid;grid-template-columns:160px minmax(200px,1fr) auto;gap:16px;align-items:end;margin-top:16px}.field label{display:block;font-size:12px;font-weight:600;margin-bottom:6px}input:not([type=checkbox]),select{border:1px solid var(--border-strong);border-radius:8px;background:var(--surface);color:var(--text);padding:9px 12px;min-width:0} .field input{width:100%}#schedule-status{margin-top:14px;font-size:12px;overflow-wrap:anywhere}.schedule-help{font-size:12px;margin-top:8px;max-width:760px}.activity{display:flex;gap:12px;align-items:baseline;border:1px solid var(--border);background:var(--surface);border-radius:10px;padding:14px 18px;margin:22px 0}.activity strong{font-size:12px;white-space:nowrap}#status{font-size:12px;overflow-wrap:anywhere}#auth-fallback{display:none;margin-bottom:20px;padding:18px;border:1px solid var(--amber);border-radius:12px;background:var(--amber-bg)}#auth-fallback.on{display:block}#auth-fallback p{font-size:12px;margin-bottom:10px}#management-key{display:block;width:100%;margin-top:6px}.inventory{padding:0;overflow:hidden}.inventory-head{display:flex;align-items:center;justify-content:space-between;gap:16px;padding:22px 24px}.inventory-head h2{font-size:16px}.inventory-head p{font-size:12px;margin-top:3px}.filters{display:flex;gap:8px}#account-search{width:230px}.table-wrap{overflow-x:auto}table{border-collapse:collapse;width:100%;text-align:left;white-space:nowrap}th{font-size:10px;letter-spacing:.8px;text-transform:uppercase;color:var(--muted);background:var(--bg);font-weight:650;padding:11px 24px;border-block:1px solid var(--border)}td{padding:14px 24px;border-bottom:1px solid var(--border);font-size:12px}tbody tr:last-child td{border-bottom:0}tbody tr:hover{background:var(--bg)}.account-email{font-weight:600}.row-number{color:var(--subtle);font-weight:400;margin-right:12px;font-variant-numeric:tabular-nums}.seat{color:var(--muted)}.empty-state{padding:38px 24px;text-align:center;color:var(--muted)}.empty-state strong{display:block;color:var(--text);margin-bottom:5px}.inventory-foot{font-size:11px;color:var(--muted);padding:12px 24px;border-top:1px solid var(--border)}.queue-title{padding:18px 24px 10px;font-size:13px;border-top:1px solid var(--border)}#agent-login{background:var(--surface);padding:20px;border:1px solid var(--amber);border-radius:12px;margin-bottom:20px}#agent-login label{display:block;margin-top:12px}#verification-code{max-width:140px;margin:0 10px}footer{display:flex;justify-content:space-between;gap:16px;font-size:11px;color:var(--muted);padding-top:20px}
    @media(prefers-color-scheme:dark){:root{color-scheme:dark;--bg:#151412;--surface:#1d1b18;--border:#3a3530;--border-strong:#514b43;--text:#f6f4f1;--muted:#b5aea5;--subtle:#938b82;--success-bg:#064e3b4d;--success-text:#6ee7b7;--error-bg:#c657463d;--error-text:#f1b0a6;--amber:#f5bd69;--amber-bg:#f59e0b22;--neutral-bg:#35312c;--neutral-text:#c9c3bb;--accent:#e5845d;--accent-bg:#e5845d19}}
    @media(max-width:850px){.inventory-head{align-items:flex-start;flex-direction:column}.filters{width:100%}#account-search{flex:1;width:100%}.schedule-fields{grid-template-columns:1fr 2fr}.schedule-fields button{grid-column:1/-1}.metric{padding:16px}.shell{padding-inline:20px}}
    @media(max-width:600px){.shell{padding-inline:16px}.header-note{display:none}.intro{padding-top:24px}h1{font-size:28px}.summary{grid-template-columns:repeat(2,minmax(0,1fr));gap:10px;margin-bottom:24px}main{grid-template-columns:1fr}section{padding:20px}.metric strong{font-size:26px}.action-card>p{min-height:0}.inventory-head{padding:20px}.filters{flex-direction:column}.schedule-fields,.quota-fields{grid-template-columns:1fr}.activity{align-items:flex-start;flex-direction:column;gap:4px}th,td{padding:12px 16px}footer{flex-direction:column;gap:4px}}
    @media(prefers-reduced-motion:reduce){*,*::before,*::after{scroll-behavior:auto!important;transition:none!important;animation:none!important}}
    .topbar{display:flex;align-items:center;justify-content:space-between;gap:16px;margin-top:18px}.topbar-actions{display:flex;gap:8px;align-items:center}.topbar-actions button{padding:8px 12px;background:var(--surface);border-color:var(--border-strong);color:var(--text);font-size:12px}.topbar-actions button:hover:not(:disabled){background:var(--neutral-bg)}.connection{display:inline-flex;align-items:center;gap:7px;font-size:12px;color:var(--muted)}.connection-dot{width:8px;height:8px;border-radius:50%;background:var(--subtle)}.connection-dot.ready{background:var(--success)}.connection-dot.warn{background:var(--amber)}.section-nav{display:flex;gap:8px;overflow-x:auto;padding:14px 0 2px;scrollbar-width:none}.section-nav a{white-space:nowrap;color:var(--muted);font-size:12px;text-decoration:none;padding:6px 10px;border-radius:7px}.section-nav a:hover,.section-nav a:focus-visible{background:var(--accent-bg);color:var(--accent)}.setup-guide{display:grid;gap:18px;margin-bottom:22px;border:1px solid var(--border-strong);background:linear-gradient(135deg,var(--surface),var(--accent-bg));box-shadow:var(--shadow)}.setup-guide[hidden]{display:none}.setup-head{display:flex;justify-content:space-between;gap:16px;align-items:flex-start}.setup-head p{font-size:13px;margin-top:5px;max-width:680px}.setup-actions{display:flex;gap:8px;flex-wrap:wrap}.setup-actions button{background:transparent;color:var(--text);border-color:var(--border-strong);padding:8px 12px;font-size:12px}.setup-actions button:hover:not(:disabled){background:var(--surface)}.setup-steps{display:grid;grid-template-columns:repeat(3,minmax(0,1fr));gap:10px}.setup-optional{border-top:1px solid var(--border);padding-top:12px}.setup-optional>summary{padding:0;color:var(--muted);font-size:12px}.setup-optional>summary strong{font-size:12px}.setup-optional .setup-steps{grid-template-columns:repeat(2,minmax(0,1fr));margin-top:12px}.setup-step{position:relative;padding:14px;background:rgba(255,255,255,.48);border:1px solid var(--border);border-radius:11px;min-height:128px}.setup-step[data-state=done]{border-color:var(--success);background:var(--success-bg)}.setup-step[data-state=attention]{border-color:var(--amber);background:var(--amber-bg)}.setup-step-number{display:grid;place-items:center;width:24px;height:24px;border-radius:50%;background:var(--neutral-bg);color:var(--muted);font-size:11px;font-weight:750;margin-bottom:10px}.setup-step[data-state=done] .setup-step-number{background:var(--success);color:white}.setup-step h3{font-size:13px;margin-bottom:5px}.setup-step p{font-size:11px;line-height:1.45}.setup-step a{display:inline-block;margin-top:9px;font-size:11px;font-weight:650}.setup-foot{display:flex;justify-content:space-between;gap:12px;align-items:center;padding-top:2px}.setup-progress{font-size:12px;color:var(--muted)}.next-action{display:flex;align-items:flex-start;gap:12px;margin:0 0 22px;padding:16px 18px;border-left:4px solid var(--accent);background:var(--surface);border-radius:10px;box-shadow:var(--shadow)}.next-action strong{display:block;font-size:13px;margin-bottom:3px}.next-action p{font-size:12px}.next-action .action-link{margin-left:auto;white-space:nowrap;font-size:12px;font-weight:650}.metrics-heading{display:flex;align-items:baseline;justify-content:space-between;gap:16px;margin-bottom:12px}.metrics-heading p{font-size:12px}.action-card{min-height:238px}.repair-paths{display:grid;gap:8px;width:100%;margin:4px 0 4px}.repair-path{padding:10px 12px;border:1px solid var(--border);border-radius:10px;background:var(--bg)}.repair-path strong{display:block;font-size:12px}.repair-path p{font-size:11px;line-height:1.4;margin-top:3px}.repair-path.agent{background:var(--accent-bg);border-color:var(--border-strong)}.repair-path .option{margin-top:8px}.repair-path-note{font-size:11px;color:var(--muted);margin-top:10px}.advanced-tools{margin-top:18px;padding:0}.advanced-tools>summary{padding:18px 24px}.advanced-tools .advanced-body{display:grid;gap:18px;padding:0 24px 24px}.advanced-tools .schedule-panel,.advanced-tools .quota-panel{margin-top:0}.advanced-tools .quota-panel{border:1px solid var(--border);box-shadow:none}.advanced-tools .schedule-panel{border:1px solid var(--border)}.advanced-tools .schedule-panel details{border:0}.advanced-tools .schedule-panel summary{padding:18px}.advanced-tools .quota-panel{padding:20px}.advanced-tools .quota-panel .card-heading{margin-bottom:8px}.repair-options{width:100%;margin:4px 0 8px}.repair-options>summary{padding:0;color:var(--muted);font-size:11px}.repair-options>summary strong{font-size:11px}.repair-options .repair-paths{margin-top:10px}.action-card .action-topline{display:flex;align-items:center;justify-content:space-between;width:100%;gap:10px}.action-card .action-topline small{color:var(--muted);font-size:11px}.subsection{scroll-margin-top:14px}.status-badge{white-space:nowrap}.status-badge.info{background:var(--accent-bg);color:var(--accent)}.activity{scroll-margin-top:14px}.activity strong{color:var(--text)}#agent-login{scroll-margin-top:14px}.inventory{scroll-margin-top:14px}
    .repair-paths{grid-template-columns:repeat(2,minmax(0,1fr));margin:4px 0 12px}.repair-path{display:flex;align-items:flex-start;gap:9px;cursor:pointer;min-height:106px}.repair-path input{margin-top:3px;accent-color:var(--accent)}.repair-path.selected{border-color:var(--accent);box-shadow:0 0 0 2px var(--accent-bg)}.repair-path-copy{display:grid;gap:4px}.repair-path-copy small{font-size:11px;line-height:1.45;color:var(--muted)}.secondary-action{background:var(--surface);border-color:var(--border-strong);color:var(--text);font-size:12px}.action-card .secondary-action{margin-top:8px}.secondary-action:hover:not(:disabled){background:var(--neutral-bg)}.activity-copy{display:grid;gap:3px;min-width:0}.activity-copy p{overflow-wrap:anywhere}.activity-actions{margin-left:auto;display:flex;align-items:center}.job-progress{font-size:11px;color:var(--muted)}.repair-summary{display:flex;align-items:center;gap:10px;border:1px solid var(--border);background:var(--surface);border-radius:10px;padding:12px 18px;margin:-10px 0 22px}.repair-summary strong{font-size:12px;white-space:nowrap}.repair-summary p{font-size:12px;overflow-wrap:anywhere}.queue-reason{display:block;margin-top:4px;color:var(--muted);font-size:11px}.queue-title{display:flex;align-items:baseline;justify-content:space-between;gap:8px}.queue-title small{color:var(--muted);font-size:11px;font-weight:400}@media(max-width:600px){.repair-paths{grid-template-columns:1fr}.activity-actions{margin-left:0}.repair-summary{align-items:flex-start;flex-direction:column}}
    @media(prefers-color-scheme:dark){.setup-step{background:rgba(29,27,24,.72)}.setup-step[data-state=done]{background:var(--success-bg)}.setup-step[data-state=attention]{background:var(--amber-bg)}}
    @media(max-width:1000px){.setup-steps{grid-template-columns:repeat(3,minmax(0,1fr))}}
    @media(max-width:600px){.topbar{align-items:flex-start;flex-direction:column}.topbar-actions{width:100%}.topbar-actions button{flex:1}.setup-head{flex-direction:column}.setup-actions{width:100%}.setup-actions button{flex:1}.setup-steps{grid-template-columns:1fr 1fr}.setup-step{min-height:0}.setup-foot{align-items:flex-start;flex-direction:column}.next-action{flex-direction:column}.next-action .action-link{margin-left:0}}
  </style>
</head>
<body>
  <div class="shell">
    <header class="brand"><span class="brand-mark" aria-hidden="true"><svg viewBox="0 0 24 24" fill="none" xmlns="http://www.w3.org/2000/svg"><path d="M12 2c.8 3.7 4.8 4.8 5.8 8.2.9 3.1-.8 6.4-3.8 7.7 1.3-1.9.9-4.2-.6-5.6-.2 2.5-1.8 4.1-3.9 5.1-2.4-1.2-3.8-3.5-3.4-6.1.4-2.9 3.2-4.6 5.9-9.3Z" fill="currentColor"/></svg></span><div><div class="brand-name">CPA Phoenix</div><small>Keep Codex accounts ready</small></div><span class="header-note">Connect once. See the next action.</span></header>
    <div class="topbar"><span class="connection"><span id="connection-dot" class="connection-dot" aria-hidden="true"></span><span id="connection-state">Checking CPA connection…</span></span><div class="topbar-actions"><button id="refresh" type="button">Refresh</button><button id="getting-started" type="button">Getting started</button></div></div>
    <nav class="section-nav" aria-label="Dashboard sections"><a href="#overview">Overview</a><a href="#maintenance">Maintenance</a><a href="#automation">Automation</a><a href="#inventory-title">Accounts</a></nav>
    <div class="intro"><span class="eyebrow">Quick start</span><h1>Keep your Codex accounts ready.</h1><p>Connect once. Phoenix checks your accounts and shows the next useful action.</p></div>
    <div id="auth-fallback"><strong>Connect to CPA</strong><p>Open CPA management, then enter the management key here to load accounts and enable maintenance.</p><a id="open-cpa-management" href="/management.html" target="_blank" rel="noopener noreferrer">Open CPA management</a><label for="management-key">CPA Management key (kept in memory only)</label><input id="management-key" type="password" autocomplete="off" spellcheck="false" placeholder="Enter key, then press Enter"></div>
    <section id="setup-guide" class="setup-guide" aria-labelledby="setup-title" hidden><div class="setup-head"><div><span class="eyebrow">Getting started</span><h2 id="setup-title">Three steps to your first useful result.</h2><p>Connect, check your accounts, and fix anything that needs attention. Optional tools stay below.</p></div><div class="setup-actions"><button id="setup-dismiss" type="button">Hide guide</button><button id="setup-complete" type="button">Done</button></div></div><div class="setup-steps"><article id="setup-step-connect" class="setup-step" data-state="pending"><span class="setup-step-number">1</span><h3>Connect</h3><p>Enter the CPA management key when Phoenix asks for it.</p><a href="#auth-fallback">Connect</a></article><article id="setup-step-inventory" class="setup-step" data-state="pending"><span class="setup-step-number">2</span><h3>Check accounts</h3><p>Phoenix scans your current CPA inventory.</p><a href="#inventory-title">View accounts</a></article><article id="setup-step-repair" class="setup-step" data-state="pending"><span class="setup-step-number">3</span><h3>Fix what needs attention</h3><p>Use one button for invalid accounts; fresh accounts are optional.</p><a href="#maintenance">Open actions</a></article></div><details class="setup-optional"><summary><strong>Optional tools</strong><span>Quota protection and Daily Ignite</span></summary><div class="setup-steps"><article id="setup-step-quota" class="setup-step" data-state="pending"><span class="setup-step-number">+</span><h3>Protect quota</h3><p>Choose one workspace if you want Phoenix to preserve its quota.</p><a href="#quota-title">Review quota</a></article><article id="setup-step-schedule" class="setup-step" data-state="pending"><span class="setup-step-number">+</span><h3>Automate warmup</h3><p>Set a daily time for fresh-account activation.</p><a href="#automation">Review schedule</a></article></div></details><div class="setup-foot"><span id="setup-progress" class="setup-progress">Waiting for the first account check.</span><span class="setup-progress">Reopen this guide from Getting started.</span></div></section>
    <section id="overview" class="subsection"><div class="metrics-heading"><h2 class="section-title">Overview</h2><p id="overview-note">Actions unlock when counts are ready.</p></div>
    <div class="summary" aria-label="Account overview">
      <div class="metric"><span class="metric-label"><span class="dot"></span>Total accounts</span><strong id="total-count">—</strong><small>Workspaces in CPA</small></div>
      <div class="metric"><span class="metric-label"><span class="dot green"></span>Healthy</span><strong id="healthy-count">—</strong><small>Accounts with valid access</small></div>
      <div class="metric"><span class="metric-label"><span class="dot orange"></span>Fresh &amp; eligible</span><strong id="fresh-count">—</strong><small>Ready for Ignite</small></div>
      <div class="metric"><span class="metric-label"><span class="dot red"></span>Needs repair</span><strong id="invalid-count">—</strong><small>Invalid accounts in CPA</small></div>
    </div></section>
    <div id="next-action" class="next-action" hidden><span class="action-icon" aria-hidden="true">→</span><div><strong id="next-action-title">Next step</strong><p id="next-action-copy"></p></div><a id="next-action-link" class="action-link" href="#maintenance">Open</a></div>
    <h2 id="maintenance" class="section-title subsection">Maintain your accounts</h2>
    <main aria-label="Maintenance actions">
      <section class="action-card"><div class="card-heading"><span class="action-icon" aria-hidden="true">↗</span><div><h2>Warm up fresh accounts</h2><span id="fresh" class="count-badge" aria-label="fresh account count">Scanning…</span></div></div><p>Send one small request through each eligible fresh seat.</p><button id="ignite" disabled>Warm up fresh accounts</button><span class="action-note">Only fresh seats are included.</span></section>
    <section class="action-card repair-card"><div class="card-heading"><span class="action-icon repair-icon" aria-hidden="true">↻</span><div><h2>Fix invalid accounts</h2><span id="invalid" class="count-badge" aria-label="repair queue count">Scanning…</span></div></div><p>Start here when an account shows 401 or invalid access. Choose how Phoenix should work through the queue.</p><div class="repair-paths" role="radiogroup" aria-label="Choose a repair path"><label id="repair-path-auto" class="repair-path selected"><input id="automatic-mode" name="repair-mode" type="radio" value="automatic" checked><span class="repair-path-copy"><strong>Repair in Phoenix</strong><small>Run from this dashboard. Phoenix opens its managed Chrome window and retrieves fresh codes.</small></span></label><label id="repair-path-agent" class="repair-path agent"><input id="agent-mode" name="repair-mode" type="radio" value="agent"><span class="repair-path-copy"><strong>Repair with Agent skill</strong><small>Use regular Chrome with the Phoenix repair skill. Phoenix still owns the queue; Agent Autofill provides fresh email codes.</small></span></label></div><button id="revive" disabled>Fix invalid accounts</button><button id="recover-skipped" type="button" hidden>Retry paused or timed-out seats in regular Chrome</button><span id="repair-path-note" class="repair-path-note" role="status" aria-live="polite">Automatic browser selected.</span><span id="otp-status" class="repair-path-note" role="status" aria-live="polite">Email codes: Agent Autofill only.</span><a id="otp-setup-link" class="repair-path-note" href="https://github.com/lifan-builds/agent-autofill" target="_blank" rel="noopener noreferrer">Set up Agent Autofill</a></section>
    </main>
    <details id="advanced-settings" class="advanced-tools subsection"><summary><span class="schedule-icon" aria-hidden="true">⋯</span><div><strong>Optional tools</strong><small>Daily warmup and quota protection</small></div><span class="disclosure" aria-hidden="true">⌄</span></summary><div class="advanced-body"><section id="automation" class="schedule-panel subsection"><details><summary><span class="schedule-icon" aria-hidden="true">◷</span><div><strong>Daily warmup</strong><small id="schedule-summary">Set a daily time to activate fresh accounts automatically.</small></div><span class="disclosure" aria-hidden="true">⌄</span></summary><form id="ignite-schedule"><label class="option"><input id="schedule-enabled" type="checkbox"> Warm up automatically every day</label><div class="schedule-fields"><div class="field"><label for="schedule-time">Daily time</label><input id="schedule-time" type="time" value="09:00" required></div><div class="field"><label for="schedule-timezone">Time zone</label><input id="schedule-timezone" type="text" value="America/Los_Angeles" required spellcheck="false" aria-describedby="timezone-help"></div><button id="schedule-save" type="submit" disabled>Save schedule</button></div><p id="schedule-status" role="status" aria-live="polite">Loading schedule…</p><p class="schedule-help" id="timezone-help">Runs while CPA is running, even with this page closed. One missed run is caught up after downtime.</p></form></details></section><section class="quota-panel" aria-labelledby="quota-title"><div class="card-heading"><span class="action-icon" aria-hidden="true">⌁</span><div><h2 id="quota-title">Protect one account's quota</h2><p>Optional: choose one workspace for Phoenix to protect when its 5-hour usage threshold is reached.</p></div></div><form id="quota-floor"><label class="option"><input id="quota-enabled" type="checkbox"> Protect this account automatically</label><div class="quota-fields"><div class="field"><label for="quota-account">Account</label><select id="quota-account" aria-describedby="quota-help"><option value="">Loading accounts…</option></select></div><button id="quota-save" type="submit" disabled>Save protection</button></div><p id="quota-threshold" class="quota-help" role="status" aria-live="polite">Configured 5-hour Codex usage threshold: loading…</p><p id="quota-status" role="status" aria-live="polite">Loading quota protection…</p><p class="quota-help" id="quota-help">The account choice is optional. The weekly window is informational; re-enabling follows the primary 5-hour window reset. Existing settings stay unchanged until you save.</p></form></section></div></details>
    <div class="activity"><div class="activity-copy"><strong>Activity</strong><p id="status" role="status" aria-live="polite">Scanning accounts…</p><p id="job-progress" class="job-progress" role="status" aria-live="polite" hidden></p></div><div class="activity-actions"><button id="cancel-revive" class="secondary-action" type="button" hidden disabled>Stop repair</button></div></div><div id="repair-summary" class="repair-summary" role="status" aria-live="polite" hidden><strong>Last repair</strong><p id="repair-summary-text"></p></div>
    <div id="agent-login" hidden><a id="current-login" target="_blank" rel="noopener noreferrer">Open current login</a><p id="workspace-help" role="note">Choose the saved or most likely non-Personal workspace; Phoenix will keep trying choices until the queued account is repaired.</p><label>Verification code <input id="verification-code" readonly autocomplete="off"></label><span id="verification-state" role="status"></span><p>OpenAI rejected or deactivated this login? Skip this seat to continue the queue. It will remain unresolved.</p><button id="skip-current" type="button">Skip rejected login and continue</button></div>
    <section class="inventory" aria-labelledby="inventory-title"><div class="inventory-head"><div><h2 id="inventory-title">Account status</h2><p>Access and quota status across your workspaces.</p></div><div class="filters"><input id="account-search" type="search" placeholder="Search email or workspace…" aria-label="Search accounts by email or workspace"><select id="account-filter" aria-label="Filter accounts"><option value="all">All accounts</option><option value="healthy">Healthy</option><option value="invalid">Invalid</option><option value="fresh">Fresh quota</option></select></div></div><div id="accounts"><div class="empty-state">Loading account inventory…</div></div><div class="inventory-foot" id="account-results" role="status" aria-live="polite">Waiting for CPA…</div></section>
    <footer><span>CPA Phoenix · Account maintenance center</span><span id="scan-time">Actions unlock when counts are ready.</span></footer>
  </div>
  <script>
const cpaStoragePrefix='enc::v1::',cpaSecureStorageSalt='cli-proxy-api-webui::secure-storage';
let phoenixManagementKey='',phoenixRejectedManagementKey='',phoenixActionInFlight=false,phoenixActiveJob=false,phoenixScanSequence=0,phoenixPollSequence=0,phoenixPolledJob='';
let phoenixActiveKind='',phoenixCancelBusy=false,phoenixRecoveryBusy=false,phoenixOTPLoaded=false;
let phoenixAttempt='',phoenixCodeBusy=false,phoenixSkipBusy=false;
let phoenixScheduleLoaded=false,phoenixScheduleDirty=false,phoenixScheduleSaving=false;
const phoenixOnboardingStorageKey='cpa-phoenix:onboarding:v1';
let phoenixSetup={connected:false,inventory:false,quota:'pending',schedule:'pending',repair:'pending'};
let phoenixOnboardingSeen=readStorageText(safeStorage('local'),phoenixOnboardingStorageKey)==='seen';

function phoenixSetConnection(state,label){
  const dot=document.querySelector('#connection-dot'),text=document.querySelector('#connection-state');
  if(dot)dot.className='connection-dot '+(state==='ready'?'ready':state==='warn'?'warn':'');
  if(text)text.textContent=label;
}
function phoenixSetSetupStep(id,state){
  const step=document.querySelector(id);if(!step||!step.dataset)return;
  step.dataset.state=state;
  const number=step.querySelector('.setup-step-number');
  if(number&&!number.dataset.number)number.dataset.number=number.textContent;
  if(number)number.textContent=state==='done'?'✓':number.dataset.number;
}
function phoenixUpdateSetupGuide(){
  const guide=document.querySelector('#setup-guide');if(!guide)return;
  phoenixSetSetupStep('#setup-step-connect',phoenixSetup.connected?'done':'attention');
  phoenixSetSetupStep('#setup-step-inventory',phoenixSetup.inventory?'done':'pending');
  phoenixSetSetupStep('#setup-step-quota',phoenixSetup.quota==='enabled'||phoenixSetup.quota==='off'?'done':phoenixSetup.quota==='unavailable'||phoenixSetup.quota==='attention'?'attention':'pending');
  phoenixSetSetupStep('#setup-step-schedule',phoenixSetup.schedule==='enabled'||phoenixSetup.schedule==='off'?'done':phoenixSetup.schedule==='unavailable'?'attention':'pending');
  phoenixSetSetupStep('#setup-step-repair',phoenixSetup.repair==='done'?'done':phoenixSetup.repair==='attention'?'attention':'pending');
  const done=[phoenixSetup.connected,phoenixSetup.inventory,phoenixSetup.repair==='done'].filter(Boolean).length;
  const progress=document.querySelector('#setup-progress');
  if(progress)progress.textContent=done===3?'Core setup complete. Optional tools are below.':!phoenixSetup.connected?'Start by connecting to CPA.':!phoenixSetup.inventory?'Loading your accounts…':'Fix invalid accounts to finish setup.';
  guide.hidden=phoenixOnboardingSeen;
}
function phoenixMarkOnboardingSeen(){
  try{const storage=safeStorage('local');if(storage)storage.setItem(phoenixOnboardingStorageKey,'seen')}catch(e){}
  phoenixOnboardingSeen=true;phoenixUpdateSetupGuide();
}
function phoenixOpenOnboarding(){
  phoenixOnboardingSeen=false;phoenixUpdateSetupGuide();
  const guide=document.querySelector('#setup-guide');
  if(guide&&guide.scrollIntoView){
    const reduced=Boolean(window.matchMedia&&window.matchMedia('(prefers-reduced-motion: reduce)').matches);
    guide.scrollIntoView({behavior:reduced?'auto':'smooth',block:'start'});
  }
}
function phoenixRenderNextAction(){
  const panel=document.querySelector('#next-action'),title=document.querySelector('#next-action-title'),copy=document.querySelector('#next-action-copy'),link=document.querySelector('#next-action-link');
  if(!panel||!title||!copy||!link)return;
  let state;
  if(!phoenixSetup.connected){state=['Connect to CPA','Enter the management key so Phoenix can check your workspaces.','#auth-fallback','Connect'];}
  else if(!phoenixSetup.inventory){state=['Waiting for account inventory','Refresh after CPA responds with your account list.','#overview','Refresh'];}
  else if(phoenixActiveJob||phoenixActionInFlight){state=['Maintenance in progress','Phoenix is working through the current job. Keep this page open for live status.','#status','View activity'];}
  else if(Number(document.querySelector('#total-count').textContent||0)===0){state=['No accounts found','Open CPA management to add or connect a workspace, then refresh Phoenix.','/management.html','Open CPA management'];}
  else if(Number(document.querySelector('#invalid-count').textContent||0)>0||phoenixQueueRows.some(row=>row.state!=='repaired')){state=['Repair needs attention','Review invalid or unresolved accounts. Timed-out seats can be retried from the repair card.','#maintenance','Review repair'];}
  else if(Number(document.querySelector('#fresh-count').textContent||0)>0){state=['Fresh accounts are ready','Warmup is optional. Run it only when you want to activate fresh seats.','#ignite','Open warmup'];}
  else if(phoenixSetup.quota==='attention'){state=['Review quota protection','Phoenix needs a fresh quota reading before it can confirm the optional setting.','#advanced-settings','Open optional tools'];}
  else if(phoenixSetup.quota==='unavailable'||phoenixSetup.schedule==='unavailable'){state=['Optional tools unavailable','Refresh later if you want to configure quota protection or Daily warmup.','#advanced-settings','Open optional tools'];}
  else{state=['You are up to date','No maintenance action is required right now.','#inventory-title','Review accounts'];}
  title.textContent=state[0];copy.textContent=state[1];link.href=state[2];link.textContent=state[3];panel.hidden=false;
}

function renderIgniteSchedule(result){
  document.querySelector('#schedule-enabled').checked=Boolean(result.enabled);
  document.querySelector('#schedule-time').value=result.time;
  document.querySelector('#schedule-timezone').value=result.timezone;
  const format=value=>value?new Date(value*1000).toLocaleString(undefined,{timeZone:result.timezone}):'';
  let status=result.enabled?'Next run: '+format(result.next_run)+' · '+result.timezone:'Daily Ignite is off';
  if(result.last_run)status+=' · Last run: '+format(result.last_run)+(result.last_outcome?' — '+String(result.last_outcome).replaceAll('_',' '):'');
  document.querySelector('#schedule-status').textContent=status;
  document.querySelector('#schedule-summary').textContent=result.enabled?'Daily at '+result.time+' · '+result.timezone:'Off · Set a daily time to activate fresh accounts automatically.';
  phoenixSetup.schedule=result.enabled?'enabled':'off';phoenixUpdateSetupGuide();phoenixRenderNextAction();
}
async function loadIgniteSchedule(){
  try{
    const response=await authenticatedFetch('/v0/management/plugins/cpa-phoenix/ignite/schedule');
    if(!response.ok)throw Error('unavailable');
    const result=await response.json();
    if(!phoenixScheduleDirty&&!phoenixScheduleSaving)renderIgniteSchedule(result);
    phoenixScheduleLoaded=true;
    document.querySelector('#schedule-save').disabled=false;
  }catch(e){phoenixSetup.schedule='unavailable';phoenixUpdateSetupGuide();phoenixRenderNextAction();document.querySelector('#schedule-status').textContent='Schedule unavailable. Refresh the page to retry.'}
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
function phoenixShowAuthPrompt(message){
  document.querySelector('#auth-fallback').classList.add('on');
  if(message)document.querySelector('#status').textContent=message;
  const field=document.querySelector('#management-key');
  if(field&&field.focus)field.focus();
}
function requireManagementKey(){
  const key=managementKey();
  if(!key){
    phoenixShowAuthPrompt('CPA authentication required');
    throw Error('management_key_required')
  }
  return key
}
function clearManagementKey(rejectedKey=''){
  phoenixRejectedManagementKey=rejectedKey||phoenixManagementKey;
  phoenixManagementKey='';
  document.querySelector('#management-key').value='';
  phoenixShowAuthPrompt('CPA authentication rejected')
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
let phoenixQuotaLoaded=false,phoenixQuotaDirty=false,phoenixQuotaSaving=false;

function renderQuotaFloor(result){
  const select=document.querySelector('#quota-account'),accounts=Array.isArray(result.accounts)?result.accounts:[],current=String(result.target_seat||'');
  const text=value=>String(value||'unknown').replace(/[&<>"']/g,c=>({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]));
  select.innerHTML='<option value="">Choose an account…</option>'+accounts.map(row=>'<option value="'+text(row.seat||'')+'">'+text(row.email)+' · '+text(row.seat)+' · '+text(String(row.status||'unknown').replaceAll('_',' '))+'</option>').join('');
  select.value=current;
  document.querySelector('#quota-enabled').checked=Boolean(result.enabled);
  select.disabled=!accounts.length;
  const threshold=Number(result.threshold_percent),thresholdText=Number.isFinite(threshold)&&threshold>0?'Configured 5-hour usage threshold: '+threshold+'%':'Configured 5-hour usage threshold unavailable';
  document.querySelector('#quota-threshold').textContent=thresholdText;
  const target=current?(accounts.find(row=>row.seat===current)||{}):{};
  const status=String(result.last_status||'unknown').toLowerCase();
  const label=target.email?(target.email+(current?' · '+current:'')):(current?'target '+current:'the selected account');
  const targetStatus=String(result.target_status||'unknown').toLowerCase(),observedAt=Number(result.last_observed_at||0),hasObservation=Number.isFinite(observedAt)&&observedAt>0;
  const disabledMismatch=(status==='disabled'||status==='disabled_waiting_reset')&&targetStatus!=='disabled';
  const enabledMismatch=(status==='allowed'||status==='enabled')&&targetStatus!=='healthy';
  const stateMismatch=Boolean(current&&(disabledMismatch||enabledMismatch));
  let statusText;
  if(!result.enabled){
    statusText='Quota preservation is off'+(current?' · existing target '+current+' remains configured':'');
  }else if(stateMismatch){
    statusText='Protection needs attention for '+label+' · current account state is '+targetStatus+' while the last observation was '+status.replaceAll('_',' ')+'. Protection is unconfirmed; refresh CPA.';
  }else if(status==='disabled'){
    statusText='Protection active for '+label+' · disabled by Phoenix at the 5-hour threshold; re-enable follows the primary 5-hour reset. Last observed state.';
  }else if(status==='disabled_waiting_reset'){
    statusText='Protection active for '+label+' · disabled by Phoenix and waiting for the primary 5-hour reset. Last observed state.';
  }else if(status==='allowed'){
    statusText='Protection enabled for '+label+' · currently allowed below the 5-hour threshold. Last observed state.';
  }else if(status==='enabled'){
    statusText='Protection enabled for '+label+' · re-enabled after the primary 5-hour reset. Last observed state.';
  }else if(status==='disable_failed'){
    statusText='Protection needs attention for '+label+' · Disable failed: Phoenix could not disable it at the 5-hour threshold, so protection is not confirmed. Check CPA and refresh.';
  }else if(status==='enable_failed'){
    statusText='Protection needs attention for '+label+' · Enable failed: Phoenix could not re-enable it after the primary 5-hour reset, so the current state is unconfirmed. Check CPA and refresh.';
  }else if(status==='externally_disabled'){
    statusText='Protection unchanged for '+label+' · the account is disabled outside Phoenix; Phoenix will leave it unchanged.';
  }else if(status==='target_missing'){
    statusText='Protection target missing · choose an account and save again.';
  }else if(status==='inventory_unavailable'){
    statusText='Protection status unavailable · account inventory could not be loaded. Refresh to retry.';
  }else{
    statusText='Protection configured'+(current?' for '+label:'')+' · '+(hasObservation?'last observation is unconfirmed; waiting for a valid 5-hour usage reading.':'waiting for the first valid 5-hour usage reading.');
  }
  document.querySelector('#quota-status').textContent=statusText;
  const attention=['disable_failed','enable_failed','target_missing','inventory_unavailable','externally_disabled'].includes(status)||stateMismatch||(status==='unknown'&&hasObservation);
  phoenixSetup.quota=!result.enabled?'off':attention?'attention':status==='unknown'?'pending':'enabled';
  phoenixUpdateSetupGuide();phoenixRenderNextAction();
}
async function loadQuotaFloor(){
  try{
    const response=await authenticatedFetch('/v0/management/plugins/cpa-phoenix/quota-floor');
    if(!response.ok)throw Error('unavailable');
    const result=await response.json();
    if(!phoenixQuotaDirty&&!phoenixQuotaSaving)renderQuotaFloor(result);
    phoenixQuotaLoaded=true;
    document.querySelector('#quota-save').disabled=false;
  }catch(e){phoenixSetup.quota='unavailable';phoenixUpdateSetupGuide();phoenixRenderNextAction();document.querySelector('#quota-status').textContent='Quota preservation unavailable. Refresh the page to retry.'}
}
async function saveQuotaFloor(event){
  event.preventDefault();
  if(phoenixQuotaSaving)return;
  const enabled=document.querySelector('#quota-enabled').checked,seat=document.querySelector('#quota-account').value;
  if(enabled&&!seat){document.querySelector('#quota-status').textContent='Choose an account before enabling quota preservation.';return}
  phoenixQuotaSaving=true;
  const button=document.querySelector('#quota-save');button.disabled=true;
  try{
    const result=await post('quota-floor',{acknowledge:'CPA_PHOENIX_ONE_CLICK',enabled,target_seat:seat});
    renderQuotaFloor(result);
    phoenixQuotaDirty=false;
    document.querySelector('#status').textContent=enabled?'Quota preservation saved.':'Quota preservation turned off.';
  }catch(e){document.querySelector('#quota-status').textContent='Could not save quota preservation. Check the selected account and try again.'}
  finally{button.disabled=false;phoenixQuotaSaving=false}
}

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
  accounts.innerHTML=rows.length?table('<th scope="col">Access</th><th scope="col">Quota</th>',rows.map(row=>'<tr>'+identity(row)+'<td>'+badge(row.status,row.status==='invalid'?'error':row.status==='healthy'?'success':'amber')+'</td><td>'+badge(row.quota,row.quota==='fresh'?'success':row.quota==='active'?'amber':'neutral')+'</td></tr>').join('')):'<div class="empty-state"><strong>'+(phoenixAccountRows.length?'No matching accounts':'No accounts found')+'</strong>'+(phoenixAccountRows.length?'Try another search or change the status filter.':'Accounts will appear here when CPA has workspace records. <a id=\"empty-open-cpa\" href=\"/management.html\" target=\"_blank\" rel=\"noopener noreferrer\">Open CPA management</a>')+'</div>';
  if(phoenixQueueRows.length)accounts.innerHTML+='<h3 class="queue-title"><span>Unresolved repair queue · '+phoenixQueueRows.length+'</span><small>Paused or timed-out seats can be retried here.</small></h3>'+table('<th scope="col">Repair status</th>',phoenixQueueRows.map(row=>'<tr>'+identity(row)+'<td>'+badge(row.state,row.state==='failed'||row.state==='cancelled'?'error':row.state==='repaired'?'success':'amber')+(row.quarantined?' <span class="status-badge neutral">quarantined</span>':'')+(row.reason?' <span class="queue-reason">'+text(String(row.reason).replaceAll('_',' '))+'</span>':'')+'</td></tr>').join(''));
  document.querySelector('#account-results').textContent='Showing '+rows.length+' of '+phoenixAccountRows.length+' accounts'+(phoenixQueueRows.length?' · Repair queue shown separately':'');
}
function phoenixUpdateCancelButton(){
  const button=document.querySelector('#cancel-revive');
  if(!button)return;
  const active=phoenixActionInFlight||phoenixActiveJob,show=active&&phoenixActiveKind==='revive';
  button.hidden=!show;
  button.disabled=!phoenixPolledJob||phoenixCancelBusy;
  button.textContent=phoenixCancelBusy?'Stopping…':'Stop repair';
}
function phoenixSetActionAvailability(fresh,invalid,active,queue){
  const rows=queue||[],locked=phoenixActionInFlight||Boolean(active),hasQueue=Boolean(rows.length),hasResumableQueue=rows.some(row=>row.state!=='skipped'&&row.state!=='repaired'),timedOutRows=rows.filter(row=>row.state==='skipped'&&(row.reason==='oauth_timeout'||row.recoverable===true)),hasCurrent=Number(invalid||0)>0,allSkipped=hasQueue&&!hasResumableQueue;
  const button=document.querySelector('#revive'),recoverButton=document.querySelector('#recover-skipped');
  document.querySelector('#ignite').disabled=locked||Number(fresh||0)<=0;
  document.querySelector('#agent-mode').disabled=locked;
  document.querySelector('#automatic-mode').disabled=locked;
  // A queue containing only skipped rows cannot be restarted by the normal
  // action. Keep the button disabled until the backend reports actionable work.
  button.disabled=locked||(!hasResumableQueue&&(allSkipped||!hasCurrent));
  button.textContent=active?'Fixing invalid accounts':(hasResumableQueue||(!allSkipped&&hasCurrent)?'Fix invalid accounts':hasQueue?'Review unresolved accounts':'No invalid accounts');
  recoverButton.disabled=locked||timedOutRows.length===0;
  recoverButton.hidden=false;
  recoverButton.textContent=timedOutRows.length?'Retry paused or timed-out accounts ('+timedOutRows.length+')':'Retry paused or timed-out accounts';
  recoverButton.title=timedOutRows.length?'Retry seats paused by a timeout or workspace mismatch.':'No paused or timed-out seats are ready to retry.';
  document.querySelector('#invalid').textContent=hasQueue?(String(rows.length)+' unresolved in repair queue · '+String(invalid||0)+' still invalid in CPA'):String(invalid||0)+' actionable';
  phoenixUpdateCancelButton();
}
function phoenixUpdateRepairPath(){
  const agent=Boolean(document.querySelector('#agent-mode').checked),note=document.querySelector('#repair-path-note'),automatic=document.querySelector('#repair-path-auto'),agentChoice=document.querySelector('#repair-path-agent');
  if(automatic&&automatic.classList&&automatic.classList.toggle)automatic.classList.toggle('selected',!agent);
  if(agentChoice&&agentChoice.classList&&agentChoice.classList.toggle)agentChoice.classList.toggle('selected',agent);
  if(note)note.textContent=agent?'Regular Chrome selected · Phoenix still owns the queue. Agent Autofill provides fresh email codes.':'Automatic browser selected.';
}
function phoenixParsedJob(job){
  let result=job&&job.result;
  if(typeof result==='string'){try{result=JSON.parse(result)}catch(e){result=null}}
  return {job:job||{},result};
}
function phoenixCount(source,keys){
  if(!source||typeof source!=='object')return null;
  for(const key of keys){if(Object.prototype.hasOwnProperty.call(source,key)&&Number.isFinite(Number(source[key])))return Number(source[key])}
  return null;
}
function phoenixRepairSummary(job){
  const parsed=phoenixParsedJob(job),raw=parsed.result&&typeof parsed.result==='object'?parsed.result:null,summary=job&&job.summary&&typeof job.summary==='object'?job.summary:(raw&&raw.summary&&typeof raw.summary==='object'?raw.summary:null);
  if(typeof (job&&job.summary)==='string'&&job.summary.trim())return job.summary.trim();
  if(raw&&typeof raw.summary==='string'&&raw.summary.trim())return raw.summary.trim();
  const source=summary||job||raw,parts=[];
  const repaired=phoenixCount(source,['repaired','repaired_count','success']);
  const skipped=phoenixCount(source,['skipped','skipped_count']);
  const failed=phoenixCount(source,['failed','failed_count']);
  if(repaired!==null)parts.push(repaired+' repaired');
  if(skipped!==null)parts.push(skipped+' skipped');
  if(failed!==null)parts.push(failed+' failed');
  return parts.join(' · ');
}
function phoenixRenderJobProgress(job){
  const parsed=phoenixParsedJob(job),source=parsed.job||{},done=Number(source.done||0),total=Number(source.total||0),parts=[];
  if(total>0)parts.push(done+'/'+total+' complete');
  const currentEmail=source.current_email||source.email,currentSeat=source.current_seat||source.seat;
  if(currentEmail){parts.push('Current: '+String(currentEmail)+(currentSeat?' · '+String(currentSeat):''))}
  if(source.automation_status)parts.push('Next: '+phoenixAutomationMessage(source.automation_status));
  return parts.join(' · ');
}
function phoenixRenderJobUI(job,isRepair){
  const repair=typeof isRepair==='boolean'?isRepair:String(job&&job.kind||'')==='revive';
  const progress=document.querySelector('#job-progress'),panel=document.querySelector('#repair-summary'),summaryText=document.querySelector('#repair-summary-text'),state=String(job&&job.state||'running'),progressText=phoenixRenderJobProgress(job),summary=repair?phoenixRepairSummary(job):'',terminal=['completed','failed','cancelled'].includes(state);
  if(progress){progress.textContent=progressText;progress.hidden=!progressText}
  if(panel&&summaryText){summaryText.textContent=terminal?(summary||phoenixRenderJob(job)):'';panel.hidden=!(repair&&terminal&&(summary||phoenixRenderJob(job)))}
}
function phoenixClearRepairSummary(){
  const panel=document.querySelector('#repair-summary'),progress=document.querySelector('#job-progress');
  if(panel)panel.hidden=true;
  if(progress){progress.hidden=true;progress.textContent=''}
}
function phoenixRenderJob(job){
  const parsed=phoenixParsedJob(job),result=parsed.result;
  if(result&&typeof result==='object'&&Object.prototype.hasOwnProperty.call(result,'sent')){
    return 'Warmup finished · '+(result.sent||0)+' activated, '+(result.skipped||0)+' skipped'+(result.unknown?' · '+result.unknown+' need a closer look':'')
  }
  const done=job.done||0,total=job.total||0,state=String(job.state||'running'),summary=phoenixRepairSummary(job),message=job.reason?' · '+phoenixAutomationMessage(job.reason):'';
  if(state==='completed')return 'Finished · '+(summary||done+'/'+total+' accounts')+message;
  if(state==='failed')return 'Needs attention · '+(summary||done+'/'+total+' accounts')+message;
  if(state==='cancelled')return 'Stopped · '+(summary||done+'/'+total+' accounts')+message;
  if(state==='superseded')return job.reason==='owner_reset'?'Skipped repair history cleared.':'Repair queue superseded.';
  return 'Working through '+done+'/'+total+' accounts…'+message
}
function phoenixAutomationMessage(status){
  return ({browser_starting:'Opening Phoenix’s Chrome window…',browser_ready:'Chrome is ready',
    browser_unavailable:'Chrome could not start. Install Google Chrome, then resume the queue.',
    browser_navigation_failed:'Could not open the login page. Check Phoenix’s Chrome window.',
    browser_automation_failed:'Finish sign-in in Phoenix’s Chrome window.',
    login_opened:'Opening sign-in…',email_submitted:'Email entered',email_code_requested:'Verification email requested',
    agent_autofill_unavailable:'Agent Autofill is unavailable; connect it before continuing.',verification_code_waiting:'Waiting for a fresh code from Agent Autofill…',verification_code_submitted:'Verification code entered',
    verification_code_resent:'New verification email requested',workspace_selected:'Workspace selected; checking the replacement…',
    manual_workspace_selection_required:'Choose the workspace in Phoenix’s Chrome window to continue.',
    manual_password_required:'Complete the password step in Phoenix’s Chrome window.',
    manual_captcha_required:'Complete the challenge in Phoenix’s Chrome window.',
    manual_login_required:'Finish the current sign-in step in Phoenix’s Chrome window.',
    manual_recipient_mismatch:'The login page shows a different email. Switch to the queued account in Phoenix’s Chrome window.',
    provider_auth_error:'OpenAI rejected the login session. Complete sign-in again, then resume the repair queue.',
    callback_reached:'Login complete; validating the replacement…',login_window_closed:'Login window closed; checking authorization…'
  })[status]||String(status||'')
}
async function loadOTPStatus(){
  const status=document.querySelector('#otp-status'),link=document.querySelector('#otp-setup-link');
  if(!status)return;
  try{
    const response=await authenticatedFetch('/v0/management/plugins/cpa-phoenix/otp/status');
    if(!response.ok)throw Error('otp_unavailable');
    const result=await response.json();
    phoenixOTPLoaded=true;
    const connector=String(result.agent_autofill||'');
    if(connector==='ready'){
      status.textContent='Agent Autofill ready for email codes.';
      if(link)link.textContent='Agent Autofill settings';
    }else if(connector==='installed'){
      status.textContent='Agent Autofill is installed, but broker readiness is unverified. Run its status check before repairing.';
      if(link)link.textContent='Check Agent Autofill';
    }else if(connector==='needs_attention'||connector==='unavailable'){
      status.textContent='Agent Autofill needs attention before Phoenix can retrieve email codes.';
      if(link)link.textContent='Fix Agent Autofill';
    }else{
      status.textContent='Connect Agent Autofill to retrieve email codes before repairing.';
      if(link)link.textContent='Set up Agent Autofill';
    }
  }catch(e){
    status.textContent='Email code status unavailable · connect Agent Autofill, then refresh.';
  }
}

async function scan(){
  const sequence=++phoenixScanSequence;
  try{
    const result=await post('scan');
    if(sequence!==phoenixScanSequence)return;
    phoenixSetup.connected=true;phoenixSetup.inventory=true;phoenixSetConnection('ready','Connected to CPA · checked just now');
    phoenixActiveJob=Boolean(result.active_job);
    phoenixActiveKind=result.active_job&&result.active_job.kind?String(result.active_job.kind):'';
    phoenixSetup.repair=(result.active_job&&result.active_job.kind==='revive')||Number(result.invalid||0)>0||(result.repair_queue||[]).length?'attention':'done';
    if(!phoenixScheduleLoaded&&!phoenixScheduleDirty)phoenixSetup.schedule='pending';
    if(!phoenixQuotaLoaded&&!phoenixQuotaDirty)phoenixSetup.quota='pending';
    if(!phoenixScheduleLoaded)loadIgniteSchedule();
    if(!phoenixQuotaLoaded)loadQuotaFloor();
    if(!phoenixOTPLoaded)loadOTPStatus();
    document.querySelector('#fresh').textContent=Number(result.fresh||0)+' eligible';
    document.querySelector('#total-count').textContent=(result.accounts||[]).length;
    document.querySelector('#healthy-count').textContent=(result.accounts||[]).filter(row=>row.status==='healthy').length;
    document.querySelector('#fresh-count').textContent=Number(result.fresh||0);
    document.querySelector('#invalid-count').textContent=Number(result.invalid||0);
    document.querySelector('#scan-time').textContent='Last checked '+new Date().toLocaleTimeString([],{hour:'2-digit',minute:'2-digit'});
    document.querySelector('#overview-note').textContent='Last checked '+new Date().toLocaleTimeString([],{hour:'2-digit',minute:'2-digit'});
    phoenixRenderAccounts(result.accounts,result.repair_queue);
    phoenixSetActionAvailability(result.fresh,result.invalid,result.active,result.repair_queue);
    phoenixUpdateSetupGuide();phoenixRenderNextAction();
    if(result.active_job&&result.active_job.kind==='revive'){
      if(phoenixPolledJob!==result.active_job.id){phoenixPolledJob=result.active_job.id;poll(result.active_job.id,true)}
    }else{
      phoenixPolledJob='';
      phoenixActiveKind='';
      phoenixClearAgentLogin();
      if(result.last_job){phoenixRenderJobUI(result.last_job,String(result.last_job.kind||'')==='revive');document.querySelector('#status').textContent=phoenixRenderJob(result.last_job)}else{phoenixClearRepairSummary();document.querySelector('#status').textContent='Accounts checked. '+((result.accounts||[]).length===0?'CPA returned no accounts. Add or connect a workspace, then refresh.':(Number(result.fresh||0)+Number(result.invalid||0)>0?'Choose a maintenance action to get started.':'No maintenance needed right now.'))}
    }
    document.querySelector('#auth-fallback').classList.remove('on')
  }catch(e){
    if(sequence!==phoenixScanSequence)return;
    phoenixPollSequence++;
    phoenixPolledJob='';phoenixActiveJob=false;phoenixActiveKind='';phoenixSetup.connected=false;phoenixSetup.inventory=false;phoenixSetup.repair='pending';
    if(!phoenixScheduleDirty){phoenixScheduleLoaded=false;phoenixSetup.schedule='unavailable'}else{phoenixSetup.schedule='pending'}
    if(!phoenixQuotaDirty){phoenixQuotaLoaded=false;phoenixSetup.quota='unavailable'}else{phoenixSetup.quota='pending'}
    phoenixClearAgentLogin();
    phoenixSetConnection(e.message==='management_key_required'||e.message==='management_unauthorized'?'warn':'','CPA connection unavailable');
    phoenixSetActionAvailability(0,0,true,[]);
    document.querySelector('#revive').textContent='Fix invalid accounts';
    document.querySelector('#fresh').textContent='Count unavailable';
    document.querySelector('#invalid').textContent='Count unavailable';
    document.querySelector('#total-count').textContent='—';
    document.querySelector('#healthy-count').textContent='—';
    document.querySelector('#fresh-count').textContent='—';
    document.querySelector('#invalid-count').textContent='—';
    document.querySelector('#overview-note').textContent='Account inventory unavailable · refresh to retry.';
    document.querySelector('#scan-time').textContent='Scan unavailable · Refresh the page to retry.';
    phoenixRenderAccounts([],[]);
    document.querySelector('#account-results').textContent='Account data unavailable';
    if(!phoenixQuotaDirty)document.querySelector('#quota-status').textContent='Quota preservation unavailable. Refresh to retry.';
    if(!phoenixScheduleDirty)document.querySelector('#schedule-status').textContent='Schedule unavailable. Refresh to retry.';
    phoenixUpdateSetupGuide();phoenixRenderNextAction();
    if(e.message!=='management_key_required'&&e.message!=='management_unauthorized')document.querySelector('#status').textContent='Scan unavailable'
  }
}
async function run(kind){
  if(phoenixActionInFlight)return;
  phoenixActionInFlight=true;
  phoenixActiveJob=true;
  phoenixActiveKind=kind==='ignite'?'ignite':'revive';
  phoenixClearRepairSummary();
  phoenixSetActionAvailability(0,0,true,[]);
  document.querySelector('#status').textContent=kind==='recover'?'Preparing timed-out account retry…':'Working…';
  try{
    const body={acknowledge:'CPA_PHOENIX_ONE_CLICK'};
    if(kind==='revive'&&document.querySelector('#agent-mode').checked)body.browser_mode='agent';
    if(kind==='recover'){
      body.browser_mode='agent';
      const retryable=phoenixQueueRows.filter(row=>row.state==='skipped'&&(row.reason==='oauth_timeout'||row.recoverable===true));
      // A visible queue may contain resumable rows from more than one
      // terminal job. Recovery owns one durable job at a time, so submit one
      // job's seats and let the next scan expose the next batch.
      const recoveryJob=retryable.find(row=>row.job_id&&String(row.job_id))?.job_id;
      body.seats=retryable.filter(row=>!recoveryJob||row.job_id===recoveryJob).map(row=>row.seat).filter(Boolean);
    }
    const endpoint=kind==='recover'?'revive/recover':kind;
    const result=await post(endpoint,body);
    document.querySelector('#status').textContent=kind==='recover'?'Starting timed-out account retry…':kind==='revive'?'Starting invalid-account fixes…':'Starting fresh-account warmup…';
    if(result.job_id){if(kind==='revive'||kind==='recover')phoenixPolledJob=result.job_id;poll(result.job_id,kind==='revive'||kind==='recover')}
  }catch(e){
    phoenixActionInFlight=false;
    phoenixActiveJob=false;
    phoenixActiveKind='';
    if(e.message!=='management_key_required'&&e.message!=='management_unauthorized')document.querySelector('#status').textContent='Action unavailable';
    scan()
  }
}
async function poll(id,revive=false){
  const pollSequence=phoenixPollSequence;
  const endpoint=revive?('/v0/management/plugins/cpa-phoenix/revive/poll?id='+encodeURIComponent(id)):('/v0/management/plugins/cpa-phoenix/state?id='+encodeURIComponent(id));
  try{
    const response=await authenticatedFetch(endpoint,{method:revive?'POST':'GET'});
    if(pollSequence!==phoenixPollSequence)return;
    if(response.ok){
      const result=await response.json();
      if(pollSequence!==phoenixPollSequence)return;
      phoenixRenderJobUI(result,revive);
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
    if(pollSequence!==phoenixPollSequence)return;
    if(e.message!=='management_key_required'&&e.message!=='management_unauthorized')document.querySelector('#status').textContent='Job status unavailable'
  }
  phoenixPolledJob='';
  phoenixActiveJob=false;
  phoenixActiveKind='';
  phoenixClearAgentLogin();
  phoenixActionInFlight=false;
  scan()
}

async function phoenixCancelRevive(){
  if(phoenixCancelBusy||!phoenixPolledJob)return;
  const id=phoenixPolledJob,button=document.querySelector('#cancel-revive');
  phoenixCancelBusy=true;phoenixUpdateCancelButton();
  phoenixPollSequence++;
  if(button)button.disabled=true;
  document.querySelector('#status').textContent='Stopping repair…';
  try{
    const response=await authenticatedFetch('/v0/management/plugins/cpa-phoenix/revive/cancel?id='+encodeURIComponent(id),{method:'POST'});
    if(!response.ok)throw Error('cancel_failed');
    phoenixPolledJob='';phoenixActiveJob=false;phoenixActiveKind='';phoenixActionInFlight=false;phoenixClearAgentLogin();
    document.querySelector('#status').textContent='Repair stopped. The unresolved queue is still below.';
    await scan();
  }catch(e){
    if(e.message!=='management_key_required'&&e.message!=='management_unauthorized')document.querySelector('#status').textContent='Could not stop repair. Try again.';
    phoenixCancelBusy=false;phoenixUpdateCancelButton();
  }finally{phoenixCancelBusy=false;phoenixUpdateCancelButton()}
}

function phoenixClearAgentLogin(){
  phoenixAttempt='';
  document.querySelector('#agent-login').hidden=true;
  document.querySelector('#current-login').removeAttribute('href');
  document.querySelector('#verification-code').value='';
  document.querySelector('#skip-current').disabled=false;
}
function phoenixShowAgentLogin(result){
  // Native CPA may cross one HTML serialization boundary before the URL
  // reaches this page. Decode exactly one layer at the final navigation
  // boundary as a defense-in-depth measure. Keep the fragment opaque: native
  // URLs can legitimately contain a nested entity there, and decoding it a
  // second time would change the signed URL bytes.
  const rawOAuthURL=String(result.oauth_url||''),fragmentAt=rawOAuthURL.indexOf('#');
  const oauthURL=(fragmentAt<0?rawOAuthURL.slice(0):rawOAuthURL.slice(0,fragmentAt)).replaceAll('&amp;','&')+(fragmentAt<0?'':rawOAuthURL.slice(fragmentAt));
  const url=new URL(oauthURL);
  if(url.origin!=='https://auth.openai.com'||url.username||url.password||[...url.searchParams.keys()].some(key=>key.startsWith('amp;')))throw Error('oauth_url_invalid');
  if(phoenixAttempt!==result.attempt){
    phoenixAttempt=result.attempt;
    document.querySelector('#verification-code').value='';
    document.querySelector('#verification-state').textContent='Waiting for fresh mail…';
  }
  const link=document.querySelector('#current-login');
  link.setAttribute('href',oauthURL);
  link.textContent='Open current login · '+String(result.email||'')+' · '+String(result.seat||'');
  const workspaceHint=String(result.workspace_name||'');
  document.querySelector('#workspace-help').textContent='Target workspace seat: '+String(result.seat||'unknown')+(workspaceHint?' · saved workspace: '+workspaceHint:'')+'. Choose the saved or most likely non-Personal workspace; Phoenix will keep trying choices until the queued account is repaired.';
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
    // Keep the code only in the live input property. The HTML value attribute
    // and innerText are intentionally not used as read interfaces.
    document.querySelector('#verification-code').value=result.detected?String(result.code||''):'';
    document.querySelector('#verification-state').textContent=result.detected?'Code ready':String(result.reason||'Waiting for a fresh code from Agent Autofill…');
  }catch(e){}finally{phoenixCodeBusy=false}
}
async function phoenixSkipCurrent(){
  if(phoenixSkipBusy||!phoenixPolledJob||!phoenixAttempt)return;
  const id=phoenixPolledJob,attempt=phoenixAttempt,button=document.querySelector('#skip-current');
  phoenixSkipBusy=true;
  button.disabled=true;
  try{
    await post('revive/skip',{job_id:id,attempt,reason:'provider_auth_error'});
    if(phoenixPolledJob===id&&phoenixAttempt===attempt)document.querySelector('#status').textContent='Rejected login skipped; continuing repair queue…';
  }catch(e){
    if(e.message!=='management_key_required'&&e.message!=='management_unauthorized')document.querySelector('#status').textContent='Could not skip this attempt. Check the current queue state.';
  }finally{phoenixSkipBusy=false;button.disabled=false}
}

document.querySelector('#management-key').onchange=event=>{phoenixManagementKey=String(event.target.value||'').trim();scan()};
document.querySelector('#management-key').onkeydown=event=>{if(event.key==='Enter'){event.preventDefault();phoenixManagementKey=String(event.target.value||'').trim();scan()}};
document.querySelector('#refresh').onclick=()=>{phoenixQuotaLoaded=false;phoenixScheduleLoaded=false;phoenixOTPLoaded=false;scan()};
document.querySelector('#getting-started').onclick=phoenixOpenOnboarding;
document.querySelector('#setup-dismiss').onclick=phoenixMarkOnboardingSeen;
document.querySelector('#setup-complete').onclick=phoenixMarkOnboardingSeen;
document.querySelector('#account-search').oninput=phoenixFilterAccounts;
document.querySelector('#account-filter').onchange=phoenixFilterAccounts;
document.querySelector('#ignite').onclick=()=>run('ignite');
document.querySelector('#ignite-schedule').onsubmit=saveIgniteSchedule;
document.querySelector('#ignite-schedule').oninput=()=>{phoenixScheduleDirty=true;phoenixSetup.schedule='pending';phoenixUpdateSetupGuide()};
document.querySelector('#quota-floor').onsubmit=saveQuotaFloor;
document.querySelector('#quota-floor').oninput=()=>{phoenixQuotaDirty=true;phoenixSetup.quota='pending';phoenixUpdateSetupGuide()};
document.querySelector('#quota-floor').onchange=()=>{phoenixQuotaDirty=true;phoenixSetup.quota='pending';phoenixUpdateSetupGuide()};
document.querySelector('#agent-mode').onchange=phoenixUpdateRepairPath;
document.querySelector('#automatic-mode').onchange=phoenixUpdateRepairPath;
document.querySelector('#revive').onclick=()=>run('revive');
document.querySelector('#recover-skipped').onclick=()=>run('recover');
document.querySelector('#cancel-revive').onclick=phoenixCancelRevive;
document.querySelector('#skip-current').onclick=phoenixSkipCurrent;
phoenixUpdateRepairPath();
phoenixUpdateSetupGuide();
scan()
  </script>
</body>
</html>`

func dashboardPageHTML() []byte { return []byte(dashboardHTML) }
