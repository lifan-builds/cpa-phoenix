package main

import (
	"context"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
)

// loginBrowser owns a dedicated Chrome process. It deliberately uses its own
// profile: neither Codex nor the user's everyday browser is needed or touched.
type loginBrowser struct {
	allocCtx      context.Context
	browserCtx    context.Context
	cancelAlloc   context.CancelFunc
	cancelBrowser context.CancelFunc
	detectCode    func(string, time.Time) (thunderbirdCode, error)
	resendAfter   time.Duration
	closeOnce     sync.Once
}

func newLoginBrowser(ctx context.Context) (*loginBrowser, error) {
	root, err := userStateRoot()
	if err != nil {
		return nil, errors.New("browser_unavailable")
	}
	profile := filepath.Join(root, "cpa-phoenix", "browser")
	if err := os.MkdirAll(profile, 0o700); err != nil {
		return nil, errors.New("browser_unavailable")
	}
	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.UserDataDir(profile),
		chromedp.Flag("headless", false),
		chromedp.Flag("disable-first-run-ui", true),
		chromedp.NoDefaultBrowserCheck,
		chromedp.NoFirstRun,
	)
	// Both UI Pass and the Agent skill path read through the same injected
	// detector seam; tests can exercise the exact mailbox boundary without
	// changing the production implementation.
	b, err := newLoginBrowserWithOptions(ctx, opts, repairDetectCode)
	if err != nil {
		return nil, err
	}
	return b, nil
}

func newLoginBrowserWithOptions(ctx context.Context, opts []chromedp.ExecAllocatorOption, detector func(string, time.Time) (thunderbirdCode, error)) (*loginBrowser, error) {
	allocCtx, cancelAlloc := chromedp.NewExecAllocator(ctx, opts...)
	browserCtx, cancelBrowser := chromedp.NewContext(allocCtx)
	err := chromedp.Run(browserCtx)
	if err != nil {
		cancelBrowser()
		cancelAlloc()
		return nil, errors.New("browser_unavailable")
	}
	if detector == nil {
		detector = detectRepairCode
	}
	return &loginBrowser{
		allocCtx: allocCtx, browserCtx: browserCtx,
		cancelAlloc: cancelAlloc, cancelBrowser: cancelBrowser,
		detectCode: detector, resendAfter: 45 * time.Second,
	}, nil
}

func (b *loginBrowser) Close() error {
	if b == nil {
		return nil
	}
	b.closeOnce.Do(func() {
		// A plain context cancel returns before Chrome has finished flushing its
		// profile. That races t.TempDir cleanup in tests and can leave a live
		// process touching the next repair attempt's browser state. Ask Chrome
		// to close gracefully and wait briefly before tearing down the allocator.
		if b.browserCtx != nil {
			closeCtx, cancel := context.WithTimeout(b.browserCtx, 5*time.Second)
			_ = chromedp.Cancel(closeCtx)
			cancel()
		}
		if b.cancelBrowser != nil {
			b.cancelBrowser()
		}
		if b.cancelAlloc != nil {
			b.cancelAlloc()
		}
	})
	return nil
}

func (b *loginBrowser) Run(ctx context.Context, oauthURL, email, accountID string, requestedAt time.Time, report func(string)) error {
	return b.runWorkspace(ctx, oauthURL, email, accountID, "", -1, requestedAt, report, false)
}

// RunWorkspaceTrial permits the managed browser to try a non-Personal choice
// when the provider does not expose the opaque account ID. Phoenix validates
// the callback afterwards; a mismatch simply advances to the next trial.
func (b *loginBrowser) RunWorkspaceTrial(ctx context.Context, oauthURL, email, accountID, workspaceName string, trial int, requestedAt time.Time, report func(string)) error {
	return b.runWorkspace(ctx, oauthURL, email, accountID, workspaceName, trial, requestedAt, report, false)
}

func (b *loginBrowser) run(ctx context.Context, oauthURL, email, accountID string, requestedAt time.Time, report func(string), allowFixture bool) error {
	return b.runWorkspace(ctx, oauthURL, email, accountID, "", -1, requestedAt, report, allowFixture)
}

func (b *loginBrowser) runWorkspace(ctx context.Context, oauthURL, email, accountID, workspaceName string, workspaceTrial int, requestedAt time.Time, report func(string), allowFixture bool) error {
	if b == nil || b.browserCtx == nil {
		return errors.New("browser_unavailable")
	}
	u, err := url.Parse(oauthURL)
	if err != nil || u.User != nil || (!allowFixture && (u.Scheme != "https" || !strings.EqualFold(u.Hostname(), "auth.openai.com"))) || (allowFixture && u.Scheme != "http") {
		return errors.New("oauth_url_invalid")
	}
	email = strings.TrimSpace(email)
	accountID = strings.TrimSpace(accountID)
	if email == "" || accountID == "" || requestedAt.IsZero() {
		return errors.New("browser_automation_failed")
	}
	if report == nil {
		report = func(string) {}
	}

	reported := make(map[string]bool)
	reportOnce := func(status string) {
		if !reported[status] {
			reported[status] = true
			report(status)
		}
	}

	tabCtx, cancelTab := chromedp.NewContext(b.browserCtx)
	defer cancelTab()
	stopAttemptCancel := context.AfterFunc(ctx, cancelTab)
	defer stopAttemptCancel()
	var callbackSeen atomic.Bool
	chromedp.ListenTarget(tabCtx, func(event any) {
		if frame, ok := event.(*page.EventFrameNavigated); ok && isOAuthCallback(frame.Frame.URL) {
			callbackSeen.Store(true)
		}
	})
	if err := chromedp.Run(tabCtx, chromedp.Navigate(oauthURL)); err != nil {
		if !callbackSeen.Load() {
			timer := time.NewTimer(500 * time.Millisecond)
			select {
			case <-timer.C:
			case <-ctx.Done():
				timer.Stop()
				return ctx.Err()
			}
		}
		if callbackSeen.Load() {
			reportOnce("callback_reached")
			return nil
		}
		return errors.New("browser_navigation_failed")
	}
	reportOnce("login_opened")

	cutoff := requestedAt
	startedWaiting := time.Now()
	otpSeen := false
	var code string
	emailSubmitted, codeOptionClicked, codeSubmitted, workspaceSelected := false, false, false, false
	var codeSubmittedAt time.Time
	resendClicked := false
	consecutiveBrowserErrors := 0
	ticker := time.NewTicker(350 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
		if callbackSeen.Load() {
			reportOnce("callback_reached")
			return nil
		}
		var location string
		if err := chromedp.Run(tabCtx, chromedp.Location(&location)); err != nil {
			consecutiveBrowserErrors++
			if consecutiveBrowserErrors >= 3 {
				if callbackSeen.Load() {
					reportOnce("callback_reached")
				} else {
					reportOnce("login_window_closed")
				}
				return nil
			}
			continue
		}
		consecutiveBrowserErrors = 0
		if isOAuthCallback(location) {
			reportOnce("callback_reached")
			return nil
		}

		if code == "" && !codeSubmitted && otpSeen {
			if found, detectErr := b.detectCode(email, cutoff); detectErr == nil && len(found.Code) == 6 {
				code = found.Code
			} else {
				if errors.Is(detectErr, errAgentAutofillUnavailable) {
					reportOnce("agent_autofill_unavailable")
				}
				reportOnce("verification_code_waiting")
			}
		}
		var state string
		script := loginPageScriptWithWorkspace(u.Scheme+"://"+u.Host, email, accountID, workspaceName, workspaceTrial, code, !emailSubmitted, !codeOptionClicked, !codeSubmitted, !workspaceSelected)
		if err := chromedp.Run(tabCtx, chromedp.Evaluate(script, &state)); err != nil {
			consecutiveBrowserErrors++
			continue
		}
		switch state {
		case "provider_auth_error":
			reportOnce(state)
			return errors.New(state)
		case "email_submitted":
			emailSubmitted = true
			reportOnce(state)
		case "email_code_requested":
			codeOptionClicked = true
			startedWaiting = time.Now()
			reportOnce(state)
		case "verification_code_submitted":
			codeSubmitted = true
			codeSubmittedAt = time.Now()
			code = ""
			reportOnce(state)
		case "verification_code_waiting":
			if !otpSeen {
				otpSeen = true
				startedWaiting = time.Now()
			}
			reportOnce(state)
			if codeSubmitted && !resendClicked && time.Since(codeSubmittedAt) >= 3*time.Second {
				// The provider left the same OTP control visible after submission,
				// which means the code was rejected or expired. Permit one fresh
				// resend rather than repeatedly submitting the stale value.
				codeSubmitted = false
				startedWaiting = time.Now().Add(-b.resendAfter)
			}
		case "workspace_selected":
			workspaceSelected = true
			reportOnce(state)
		case "manual_workspace_selection_required", "manual_recipient_mismatch", "manual_password_required", "manual_captcha_required", "manual_login_required":
			reportOnce(state)
		default:
			if strings.HasPrefix(state, "workspace_selected:") {
				workspaceSelected = true
				reportOnce("workspace_selected")
				if name := sanitizeWorkspaceName(strings.TrimPrefix(state, "workspace_selected:")); name != "" && report != nil {
					report("workspace_name:" + name)
				}
			}
		}

		if !resendClicked && otpSeen && state == "verification_code_waiting" && !codeSubmitted && time.Since(startedWaiting) >= b.resendAfter {
			var resent bool
			if err := chromedp.Run(tabCtx, chromedp.Evaluate(resendCodeScript, &resent)); err == nil && resent {
				resendClicked = true
				cutoff = time.Now()
				code = ""
				reportOnce("verification_code_resent")
			}
		}
	}
}

func isOAuthCallback(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	host := strings.ToLower(u.Hostname())
	return (host == "127.0.0.1" || host == "localhost") && u.Path == "/auth/callback"
}

func sanitizeWorkspaceName(raw string) string {
	fields := strings.Fields(strings.TrimSpace(raw))
	if len(fields) == 0 {
		return ""
	}
	name := strings.Join(fields, " ")
	if len(name) > 200 {
		name = name[:200]
	}
	return strings.TrimSpace(name)
}

func loginPageScript(origin, email, accountID, code string, allowEmail, allowCodeOption, allowCodeSubmit, allowWorkspace bool) string {
	return loginPageScriptWithWorkspace(origin, email, accountID, "", -1, code, allowEmail, allowCodeOption, allowCodeSubmit, allowWorkspace)
}

func loginPageScriptWithWorkspace(origin, email, accountID, workspaceName string, workspaceTrial int, code string, allowEmail, allowCodeOption, allowCodeSubmit, allowWorkspace bool) string {
	return `(function(){
const allowedOrigin=` + strconv.Quote(origin) + `, email=` + strconv.Quote(email) + `, accountID=` + strconv.Quote(accountID) + `, workspaceName=` + strconv.Quote(workspaceName) + `, workspaceTrial=` + strconv.Itoa(workspaceTrial) + `, code=` + strconv.Quote(code) + `;
const visible=e=>!!e&&!e.disabled&&e.getAttribute('aria-disabled')!=='true'&&e.getClientRects().length>0;
const text=e=>(e.innerText||e.textContent||e.value||'').trim().toLowerCase();
const click=e=>{e.focus();e.click();};
const set=(e,v)=>{const p=Object.getPrototypeOf(e),d=Object.getOwnPropertyDescriptor(p,'value');if(d&&d.set)d.set.call(e,v);else e.value=v;e.dispatchEvent(new Event('input',{bubbles:true}));e.dispatchEvent(new Event('change',{bubbles:true}));};
const normalize=v=>String(v||'').trim().toLowerCase().replace(/\\s+/g,' ');
const choiceName=e=>{const label=e.closest&&e.closest('label');if(label)return (label.innerText||label.textContent||'').trim();return (e.getAttribute('aria-label')||e.parentElement&&e.parentElement.innerText||e.innerText||e.value||'').trim();};
const body=text(document.body),title=(document.title||'').trim().toLowerCase();const shownEmails=()=>((document.body.innerText||'').toLowerCase().match(/[a-z0-9.!#$%&'*+/=?^_` + "`" + `{|}~-]+@[a-z0-9.-]+\.[a-z]{2,}/g)||[]);
if(location.origin!==allowedOrigin)return 'manual_login_required';
if(/authentication\s+error/.test(title)||/your session has ended/.test(body)||(/authentication\s+error/.test(body)&&/error[_ ]code\s*[:=]/.test(body)))return 'provider_auth_error';
if(document.querySelector('iframe[src*="captcha" i],iframe[src*="arkose" i],iframe[src*="challenge" i],input[name="cf-turnstile-response"]')||/verify (you are|that you are) human|captcha/.test(body)||document.title.trim().toLowerCase()==='just a moment...')return 'manual_captcha_required';
const allChoices=[...document.querySelectorAll('input[type="radio"],button,[role="radio"],[data-workspace-id]')];const choices=allChoices.filter(visible);
const workspacePage=choices.some(e=>e.matches('input[type="radio"],[role="radio"],[data-workspace-id]'))||/choose (a |your )?(workspace|account)|select (a |your )?(workspace|account)/.test(body);
	if(workspacePage){const emails=shownEmails();if(emails.length&&!emails.some(v=>v===email.toLowerCase()))return 'manual_recipient_mismatch';const stable=e=>e.value||e.getAttribute('data-workspace-id')||e.getAttribute('data-account-id')||'';const exact=allChoices.find(e=>stable(e)===accountID);const exactReady=exact&&(visible(exact)||(exact.disabled&&exact.checked));let selected=exactReady?exact:null;if(!selected&&` + strconv.FormatBool(allowWorkspace) + `&&workspaceTrial>=0){const hinted=normalize(workspaceName);const action=/^(continue|next|select|cancel|back|skip|log out)$/i;const eligible=choices.filter(e=>{const id=stable(e),name=choiceName(e);if(action.test(normalize(name)))return false;if(id&&id===accountID)return false;return !!name&&!/\\bpersonal\\b/.test(normalize(id+' '+name));});if(hinted&&workspaceTrial===0){const named=eligible.filter(e=>normalize(choiceName(e))===hinted);if(named.length===1)selected=named[0];}if(!selected&&workspaceTrial<eligible.length)selected=eligible[workspaceTrial];}if(selected){if(!selected.disabled)click(selected);const submit=[...document.querySelectorAll('button,input[type="submit"]')].find(e=>visible(e)&&/continue|next|select/.test(text(e)));if(submit)click(submit);return 'workspace_selected:'+choiceName(selected);}return 'manual_workspace_selection_required';}
const otp=[...document.querySelectorAll('input')].find(e=>visible(e)&&(e.autocomplete==='one-time-code'||/code|otp/.test((e.name||'')+' '+(e.id||''))||e.maxLength===6));
if(otp){const shown=shownEmails();if(shown.length&&!shown.some(v=>v===email.toLowerCase()))return 'manual_recipient_mismatch';if(code&&` + strconv.FormatBool(allowCodeSubmit) + `){set(otp,code);const submit=[...document.querySelectorAll('button,input[type="submit"]')].find(e=>visible(e)&&/continue|verify|next|submit/.test(text(e)));if(submit)click(submit);return 'verification_code_submitted';}return 'verification_code_waiting';}
const emailInput=[...document.querySelectorAll('input[type="email"],input[name="email"],input[autocomplete="username"]')].find(visible);
if(emailInput&&` + strconv.FormatBool(allowEmail) + `){set(emailInput,email);const submit=[...document.querySelectorAll('button,input[type="submit"]')].find(e=>visible(e)&&/continue|next|submit/.test(text(e)));if(submit)click(submit);return 'email_submitted';}
const emailCode=[...document.querySelectorAll('button,[role="button"],a')].find(e=>visible(e)&&/email (me )?(a )?(one-time )?code|code (to|via) email|continue with email|send.*code|use a one-time code|log in with a code/.test(text(e)));
if(emailCode&&` + strconv.FormatBool(allowCodeOption) + `){click(emailCode);return 'email_code_requested';}
const otherMethod=[...document.querySelectorAll('button,[role="button"],a')].find(e=>visible(e)&&/try another method|use another method|other (login|sign.in) method/.test(text(e)));
if(otherMethod&&` + strconv.FormatBool(allowCodeOption) + `){click(otherMethod);return 'idle';}
const password=[...document.querySelectorAll('input[type="password"]')].find(visible);if(password)return 'manual_password_required';
if(body&&document.readyState==='complete')return 'manual_login_required';return 'idle';
})()`
}

const resendCodeScript = `(function(){const visible=e=>!!e&&!e.disabled&&e.getClientRects().length>0;const text=e=>(e.innerText||e.textContent||e.value||'').trim().toLowerCase();const e=[...document.querySelectorAll('button,[role="button"],a')].find(e=>visible(e)&&/resend|send (a )?new code/.test(text(e)));if(!e)return false;e.click();return true;})()`
