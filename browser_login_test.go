package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

type loginFixture struct {
	mu       sync.Mutex
	selected map[int]string
	resends  map[int]int
}

func (f *loginFixture) handler(w http.ResponseWriter, r *http.Request) {
	attempt, _ := strconv.Atoi(r.URL.Query().Get("attempt"))
	w.Header().Set("Content-Type", "text/html")
	switch r.URL.Path {
	case "/start":
		fmt.Fprintf(w, `<!doctype html><input name="email" type="email"><button id="continue">Continue</button><script>document.getElementById('continue').onclick=()=>location='/method?attempt=%d'</script>`, attempt)
	case "/method":
		fmt.Fprintf(w, `<!doctype html><button id="email-code">Continue with email</button><script>document.getElementById('email-code').onclick=()=>location='/otp?attempt=%d'</script>`, attempt)
	case "/otp":
		want := fmt.Sprintf("%06d", attempt)
		fmt.Fprintf(w, `<!doctype html><input name="code" autocomplete="one-time-code" maxlength="6"><button id="verify">Continue</button><button id="resend">Resend code</button><script>
verify.onclick=()=>{if(document.querySelector('input').value===%q)location='/workspace?attempt=%d'};
resend.onclick=()=>fetch('/resent?attempt=%d');</script>`, want, attempt, attempt)
	case "/workspace":
		disabled := ""
		if r.URL.Query().Get("disabled") == "1" {
			disabled = " disabled checked"
		}
		shownEmail := r.URL.Query().Get("shown_email")
		fmt.Fprintf(w, `<!doctype html><h1>Choose your workspace</h1><p>%s</p>
<label><input type="radio" name="workspace" value="personal">Personal</label>
<label><input type="radio" name="workspace" value="workspace-%d"%s>Team %d</label>
<button id="choose">Continue</button><script>choose.onclick=()=>{const e=document.querySelector(':checked');if(e)fetch('/selected?attempt=%d&id='+encodeURIComponent(e.value)).then(()=>location='/auth/callback?code=fixture')}</script>`, shownEmail, attempt, disabled, attempt, attempt)
	case "/password-method":
		fmt.Fprintf(w, `<!doctype html><input type="password" name="password"><button id="email-code">Email me a one-time code</button><script>document.getElementById('email-code').onclick=()=>location='/otp?attempt=%d'</script>`, attempt)
	case "/resent":
		f.mu.Lock()
		f.resends[attempt]++
		f.mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	case "/selected":
		f.mu.Lock()
		f.selected[attempt] = r.URL.Query().Get("id")
		f.mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	case "/auth/callback":
		_, _ = w.Write([]byte("done"))
	default:
		http.NotFound(w, r)
	}
}

func newFixtureLoginBrowser(t *testing.T, detector func(string, time.Time) (thunderbirdCode, error)) *loginBrowser {
	t.Helper()
	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.UserDataDir(t.TempDir()),
		chromedp.Headless,
		chromedp.DisableGPU,
		chromedp.NoFirstRun,
		chromedp.NoDefaultBrowserCheck,
	)
	b, err := newLoginBrowserWithOptions(context.Background(), opts, detector)
	if err != nil {
		t.Skipf("Chrome unavailable for browser fixture: %v", err)
	}
	t.Cleanup(func() { _ = b.Close() })
	return b
}

func TestLoginBrowserSequentialWorkspaceFlowAndFreshResend(t *testing.T) {
	fixture := &loginFixture{selected: make(map[int]string), resends: make(map[int]int)}
	server := httptest.NewServer(http.HandlerFunc(fixture.handler))
	defer server.Close()

	firstRequested := time.Now().Add(-time.Second)
	secondRequested := firstRequested.Add(time.Second)
	detector := func(_ string, cutoff time.Time) (thunderbirdCode, error) {
		if cutoff.After(secondRequested) {
			return thunderbirdCode{Code: "000002", ReceivedAt: time.Now()}, nil
		}
		if cutoff.Equal(secondRequested) {
			return thunderbirdCode{}, fmt.Errorf("verification_code_not_found")
		}
		return thunderbirdCode{Code: "000001", ReceivedAt: time.Now()}, nil
	}
	b := newFixtureLoginBrowser(t, detector)
	b.resendAfter = 50 * time.Millisecond

	for _, tc := range []struct {
		attempt int
		cutoff  time.Time
	}{
		{attempt: 1, cutoff: firstRequested},
		{attempt: 2, cutoff: secondRequested},
	} {
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		var statuses []string
		err := b.run(ctx, fmt.Sprintf("%s/start?attempt=%d", server.URL, tc.attempt), "same@example.test", fmt.Sprintf("workspace-%d", tc.attempt), tc.cutoff, func(s string) {
			statuses = append(statuses, s)
		}, true)
		cancel()
		if err != nil {
			t.Fatalf("attempt %d failed: %v (statuses %v)", tc.attempt, err, statuses)
		}
		if !containsStatus(statuses, "workspace_selected") || !containsStatus(statuses, "callback_reached") {
			t.Fatalf("attempt %d did not complete workspace/callback flow: %v", tc.attempt, statuses)
		}
	}

	fixture.mu.Lock()
	defer fixture.mu.Unlock()
	if fixture.selected[1] != "workspace-1" || fixture.selected[2] != "workspace-2" {
		t.Fatalf("wrong workspace selections: %#v", fixture.selected)
	}
	if fixture.resends[2] != 1 {
		t.Fatalf("second attempt should resend exactly once, got %d", fixture.resends[2])
	}
}

func TestLoginBrowserRefusesUnknownWorkspace(t *testing.T) {
	fixture := &loginFixture{selected: make(map[int]string), resends: make(map[int]int)}
	server := httptest.NewServer(http.HandlerFunc(fixture.handler))
	defer server.Close()
	b := newFixtureLoginBrowser(t, func(_ string, _ time.Time) (thunderbirdCode, error) {
		return thunderbirdCode{Code: "000003", ReceivedAt: time.Now()}, nil
	})

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	var statuses []string
	err := b.run(ctx, server.URL+"/start?attempt=3", "same@example.test", "workspace-that-is-not-present", time.Now().Add(-time.Second), func(s string) {
		statuses = append(statuses, s)
	}, true)
	if !errorsIsDeadline(err) {
		t.Fatalf("unknown workspace should remain open for manual action, got %v", err)
	}
	if !containsStatus(statuses, "manual_workspace_selection_required") {
		t.Fatalf("missing manual workspace report: %v", statuses)
	}
	fixture.mu.Lock()
	defer fixture.mu.Unlock()
	if selected := fixture.selected[3]; selected != "" {
		t.Fatalf("controller selected an unrequested workspace %q", selected)
	}
}

func TestLoginBrowserExactDisabledWorkspaceAndPasswordlessChoice(t *testing.T) {
	fixture := &loginFixture{selected: make(map[int]string), resends: make(map[int]int)}
	server := httptest.NewServer(http.HandlerFunc(fixture.handler))
	defer server.Close()
	b := newFixtureLoginBrowser(t, func(_ string, _ time.Time) (thunderbirdCode, error) {
		return thunderbirdCode{Code: "000004", ReceivedAt: time.Now()}, nil
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	err := b.run(ctx, server.URL+"/workspace?attempt=4&disabled=1", "same@example.test", "workspace-4", time.Now().Add(-time.Second), nil, true)
	cancel()
	if err != nil {
		t.Fatalf("disabled selected exact workspace did not continue: %v", err)
	}
	fixture.mu.Lock()
	if fixture.selected[4] != "workspace-4" {
		fixture.mu.Unlock()
		t.Fatalf("disabled exact workspace was not selected: %#v", fixture.selected)
	}
	fixture.mu.Unlock()

	ctx, cancel = context.WithTimeout(context.Background(), 6*time.Second)
	err = b.run(ctx, server.URL+"/password-method?attempt=4", "same@example.test", "workspace-4", time.Now().Add(-time.Second), nil, true)
	cancel()
	if err != nil {
		t.Fatalf("passwordless option on password page did not complete: %v", err)
	}
}

func TestLoginBrowserRecipientAndOriginGuards(t *testing.T) {
	fixture := &loginFixture{selected: make(map[int]string), resends: make(map[int]int)}
	server := httptest.NewServer(http.HandlerFunc(fixture.handler))
	defer server.Close()
	b := newFixtureLoginBrowser(t, func(_ string, _ time.Time) (thunderbirdCode, error) {
		return thunderbirdCode{Code: "000005", ReceivedAt: time.Now()}, nil
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	var statuses []string
	err := b.run(ctx, server.URL+"/workspace?attempt=5&shown_email=other%40example.test", "same@example.test", "workspace-5", time.Now().Add(-time.Second), func(s string) { statuses = append(statuses, s) }, true)
	cancel()
	if !errorsIsDeadline(err) || !containsStatus(statuses, "manual_recipient_mismatch") {
		t.Fatalf("mismatched workspace recipient was not paused: err=%v statuses=%v", err, statuses)
	}
	fixture.mu.Lock()
	if fixture.selected[5] != "" {
		fixture.mu.Unlock()
		t.Fatalf("mismatched recipient selected workspace %q", fixture.selected[5])
	}
	fixture.mu.Unlock()

	var foreignSubmitted atomic.Bool
	foreign := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/submitted" {
			foreignSubmitted.Store(true)
			return
		}
		fmt.Fprint(w, `<!doctype html><input name="code" autocomplete="one-time-code"><button id="verify">Continue</button><script>verify.onclick=()=>fetch('/submitted')</script>`)
	}))
	defer foreign.Close()
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, foreign.URL, http.StatusFound)
	}))
	defer origin.Close()
	ctx, cancel = context.WithTimeout(context.Background(), 2*time.Second)
	statuses = nil
	err = b.run(ctx, origin.URL, "same@example.test", "workspace-5", time.Now().Add(-time.Second), func(s string) { statuses = append(statuses, s) }, true)
	cancel()
	if !errorsIsDeadline(err) || !containsStatus(statuses, "manual_login_required") || foreignSubmitted.Load() {
		t.Fatalf("foreign origin was acted on: err=%v statuses=%v submitted=%v", err, statuses, foreignSubmitted.Load())
	}
}

func containsStatus(statuses []string, want string) bool {
	for _, status := range statuses {
		if status == want {
			return true
		}
	}
	return false
}

func errorsIsDeadline(err error) bool {
	return err != nil && strings.Contains(err.Error(), context.DeadlineExceeded.Error())
}
