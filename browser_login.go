package main

import (
	"context"
	"errors"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
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
	allocCtx        context.Context
	browserCtx      context.Context
	cancelAlloc     context.CancelFunc
	cancelBrowser   context.CancelFunc
	detectCode      func(string, time.Time) (thunderbirdCode, error)
	resendAfter     time.Duration
	thunderbirdOnce sync.Once
	closeOnce       sync.Once
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
	return newLoginBrowserWithOptions(ctx, opts, detectThunderbirdCode)
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
		detector = detectThunderbirdCode
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
	return b.run(ctx, oauthURL, email, accountID, requestedAt, report, false)
}

func (b *loginBrowser) run(ctx context.Context, oauthURL, email, accountID string, requestedAt time.Time, report func(string), allowFixture bool) error {
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

	// Thunderbird is launched at most once for the lifetime of this controller.
	// It performs mailbox synchronization; Phoenix only reads the local mailbox.
	b.thunderbirdOnce.Do(func() {
		if runtime.GOOS == "darwin" {
			_ = exec.Command("/usr/bin/open", "-gja", "Thunderbird").Run()
		}
	})

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
				reportOnce("verification_code_waiting")
			}
		}
		var state string
		script := loginPageScript(u.Scheme+"://"+u.Host, email, accountID, code, !emailSubmitted, !codeOptionClicked, !codeSubmitted, !workspaceSelected)
		if err := chromedp.Run(tabCtx, chromedp.Evaluate(script, &state)); err != nil {
			consecutiveBrowserErrors++
			continue
		}
		switch state {
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

func loginPageScript(origin, email, accountID, code string, allowEmail, allowCodeOption, allowCodeSubmit, allowWorkspace bool) string {
	return `(function(){
const allowedOrigin=` + strconv.Quote(origin) + `, email=` + strconv.Quote(email) + `, accountID=` + strconv.Quote(accountID) + `, code=` + strconv.Quote(code) + `;
const visible=e=>!!e&&!e.disabled&&e.getAttribute('aria-disabled')!=='true'&&e.getClientRects().length>0;
const text=e=>(e.innerText||e.textContent||e.value||'').trim().toLowerCase();
const click=e=>{e.focus();e.click();};
const set=(e,v)=>{const p=Object.getPrototypeOf(e),d=Object.getOwnPropertyDescriptor(p,'value');if(d&&d.set)d.set.call(e,v);else e.value=v;e.dispatchEvent(new Event('input',{bubbles:true}));e.dispatchEvent(new Event('change',{bubbles:true}));};
const body=text(document.body);const shownEmails=()=>((document.body.innerText||'').toLowerCase().match(/[a-z0-9.!#$%&'*+/=?^_` + "`" + `{|}~-]+@[a-z0-9.-]+\.[a-z]{2,}/g)||[]);
if(location.origin!==allowedOrigin)return 'manual_login_required';
if(document.querySelector('iframe[src*="captcha" i],iframe[src*="arkose" i],iframe[src*="challenge" i],input[name="cf-turnstile-response"]')||/verify (you are|that you are) human|captcha/.test(body)||document.title.trim().toLowerCase()==='just a moment...')return 'manual_captcha_required';
const allChoices=[...document.querySelectorAll('input[type="radio"],button,[role="radio"],[data-workspace-id]')];const choices=allChoices.filter(visible);
const workspacePage=choices.some(e=>e.matches('input[type="radio"],[role="radio"],[data-workspace-id]'))||/choose (a |your )?(workspace|account)|select (a |your )?(workspace|account)/.test(body);
if(workspacePage){const emails=shownEmails();if(emails.length&&!emails.some(v=>v===email.toLowerCase()))return 'manual_recipient_mismatch';const stable=e=>e.value||e.getAttribute('data-workspace-id')||e.getAttribute('data-account-id')||'';const exact=allChoices.find(e=>stable(e)===accountID);if(exact&&(visible(exact)||(exact.disabled&&exact.checked))&&` + strconv.FormatBool(allowWorkspace) + `){if(!exact.disabled)click(exact);const submit=[...document.querySelectorAll('button,input[type="submit"]')].find(e=>visible(e)&&/continue|next|select/.test(text(e)));if(submit)click(submit);return 'workspace_selected';}if(!exact||!visible(exact))return 'manual_workspace_selection_required';}
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
