package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDetectRepairCodePrefersConfiguredAgentAutofill(t *testing.T) {
	oldRequest := agentAutofillRequest
	defer func() { agentAutofillRequest = oldRequest }()
	setAgentAutofillTestEnv(t, `{"Mailbox@Example.test":"source-7"}`)
	requestedAt := time.Now().Add(-time.Second)
	calledAgent := false
	agentAutofillRequest = func(_ context.Context, cfg agentAutofillConfig, recipient string, cutoff time.Time) (thunderbirdCode, error) {
		calledAgent = true
		if cfg.AgentID != "phoenix-test" || cfg.Accounts["mailbox@example.test"] != "source-7" || recipient != "Mailbox@Example.test" || cutoff != requestedAt {
			t.Fatalf("wrong Agent Autofill request: agent=%q source=%q recipient=%q cutoff=%v", cfg.AgentID, cfg.Accounts["mailbox@example.test"], recipient, cutoff)
		}
		return thunderbirdCode{Code: "123456", ReceivedAt: time.Now(), Recipient: recipient}, nil
	}
	code, err := detectRepairCode("Mailbox@Example.test", requestedAt)
	if err != nil || code.Code != "123456" || !calledAgent {
		t.Fatalf("Agent Autofill lookup failed: code_present=%v err=%v agent=%v", code.Code != "", err, calledAgent)
	}
}

func TestDetectRepairCodeRequiresAgentAutofillForEveryRecipient(t *testing.T) {
	oldRequest := agentAutofillRequest
	defer func() { agentAutofillRequest = oldRequest }()
	setAgentAutofillTestEnv(t, `{"other@example.test":"source-7"}`)
	agentAutofillRequest = func(context.Context, agentAutofillConfig, string, time.Time) (thunderbirdCode, error) {
		return thunderbirdCode{}, nil
	}

	code, err := detectRepairCode("unmapped@example.test", time.Now().Add(-time.Second))
	if !errors.Is(err, errAgentAutofillNoCode) && !errors.Is(err, errAgentAutofillUnavailable) && !errors.Is(err, errAgentAutofillNotConfigured) {
		t.Fatalf("unexpected Agent Autofill error: code_present=%v err=%v", code.Code != "", err)
	}
}

func TestAgentAutofillDefaultsToInstalledServiceConnector(t *testing.T) {
	for _, key := range []string{
		"CPA_AGENT_AUTOFILL_COMMAND", "CPA_AGENT_AUTOFILL_DB",
		"CPA_AGENT_AUTOFILL_AGENT_ID", "CPA_AGENT_AUTOFILL_AGENT_TOKEN",
		"AGENT_AUTOFILL_AGENT_TOKEN", "CPA_AGENT_AUTOFILL_ACCOUNT_MAP",
		"CPA_AGENT_AUTOFILL_ACCOUNT_ID",
	} {
		t.Setenv(key, "")
	}
	cfg, ok := agentAutofillConfigFor("owner@example.test")
	if !ok || cfg.ServiceName != "cpa-phoenix" || cfg.AgentID != "" || cfg.Token != "" || len(cfg.Accounts) != 0 {
		t.Fatalf("unexpected direct service config: ok=%v service=%q agent=%q token=%v accounts=%v", ok, cfg.ServiceName, cfg.AgentID, cfg.Token != "", cfg.Accounts)
	}
}

func TestMailSourceStatusDoesNotExposeConnectorSecrets(t *testing.T) {
	t.Setenv("CPA_AGENT_AUTOFILL_COMMAND", "/definitely/missing/agent-autofill")
	t.Setenv("CPA_AGENT_AUTOFILL_AGENT_TOKEN", "private-token")
	status := mailSourceStatus()
	if status["preferred"] != "agent_autofill" || status["agent_autofill"] != "not_found" || status["fallback"] != "none" {
		t.Fatalf("unexpected mail status: %#v", status)
	}
	if strings.Contains(strings.ToLower(fmt.Sprint(status)), "private-token") {
		t.Fatal("mail status must not expose connector credentials")
	}
}

func TestMailSourceStatusChecksValueFreeConnectorReadiness(t *testing.T) {
	command := filepath.Join(t.TempDir(), "agent-autofill")
	if err := os.WriteFile(command, []byte("#!/bin/sh\nprintf '%s\\n' '{\"status\":\"ready\",\"sources\":{\"configured\":1,\"ready\":1,\"needs_attention\":0}}'\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CPA_AGENT_AUTOFILL_COMMAND", command)
	status := mailSourceStatus()
	if status["agent_autofill"] != "ready" || status["reason"] != "broker_and_source_ready" {
		t.Fatalf("value-free readiness was not verified: %#v", status)
	}
}

func TestMailSourceStatusAcceptsConfiguredTrustedServiceSource(t *testing.T) {
	command := filepath.Join(t.TempDir(), "agent-autofill")
	if err := os.WriteFile(command, []byte("#!/bin/sh\nprintf '%s\\n' '{\"status\":\"needs-attention\",\"sources\":{\"configured\":12,\"ready\":0,\"needs_attention\":0}}'\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CPA_AGENT_AUTOFILL_COMMAND", command)
	status := mailSourceStatus()
	if status["agent_autofill"] != "ready" || status["reason"] != "broker_and_source_configured" {
		t.Fatalf("configured trusted service source should be usable: %#v", status)
	}
}

func TestMailSourceStatusSeparatesInstalledFromNeedsAttention(t *testing.T) {
	command := filepath.Join(t.TempDir(), "agent-autofill")
	if err := os.WriteFile(command, []byte("#!/bin/sh\nprintf '%s\\n' '{\"status\":\"needs-attention\",\"sources\":{\"configured\":1,\"ready\":0,\"needs_attention\":1}}'\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CPA_AGENT_AUTOFILL_COMMAND", command)
	status := mailSourceStatus()
	if status["agent_autofill"] != "needs_attention" || status["reason"] != "broker_or_source_needs_attention" {
		t.Fatalf("connector attention state was not surfaced: %#v", status)
	}
}

func TestDetectRepairCodeFailsClosedWhenAgentAutofillUnavailable(t *testing.T) {
	oldRequest := agentAutofillRequest
	defer func() { agentAutofillRequest = oldRequest }()
	setAgentAutofillTestEnv(t, `{"account@example.test":"source-7"}`)
	agentAutofillRequest = func(context.Context, agentAutofillConfig, string, time.Time) (thunderbirdCode, error) {
		return thunderbirdCode{}, errAgentAutofillUnavailable
	}
	code, err := detectRepairCode("account@example.test", time.Now().Add(-time.Second))
	if !errors.Is(err, errAgentAutofillUnavailable) || code.Code != "" {
		t.Fatalf("Agent Autofill outage should remain actionable: code_present=%v source=%q err=%v", code.Code != "", code.Source, err)
	}
	if code.Source == "thunderbird_fallback" {
		t.Fatal("Thunderbird fallback must remain disabled")
	}
}

func TestClassifyAgentAutofillErrorSanitizesDetails(t *testing.T) {
	secret := "agent-token-and-private-message"
	if got := classifyAgentAutofillError(map[string]any{"message": secret}); !errors.Is(got, errAgentAutofillUnavailable) {
		t.Fatalf("unexpected sanitized error: %v", got)
	}
	if got := gotString(classifyAgentAutofillError(map[string]any{"message": secret})); got == secret || got != errAgentAutofillUnavailable.Error() {
		t.Fatalf("Agent Autofill details leaked: %q", got)
	}
}

func TestRequestAgentAutofillCodeUsesJSONLinesServer(t *testing.T) {
	server := filepath.Join(t.TempDir(), "agent-autofill-fixture.sh")
	script := `#!/bin/sh
while IFS= read -r line; do
  case "$line" in
    *'"method":"initialize"'*) echo '{"jsonrpc":"2.0","id":0,"result":{}}' ;;
    *'"method":"tools/call"'*) echo '{"jsonrpc":"2.0","id":1,"result":{"content":[{"type":"text","text":"{\"code\":\"123456\",\"provenance\":{\"received_at\":\"2026-01-01T00:01:00Z\"}}"}],"isError":false}}' ;;
  esac
done
`
	if err := os.WriteFile(server, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	closeAgentAutofillClient()
	t.Cleanup(closeAgentAutofillClient)
	cfg := agentAutofillConfig{
		Command: server, DB: "fixture.db", AgentID: "phoenix-test", Token: "fixture-token",
		TargetOrigin: "https://auth.openai.com", Purpose: "cpa-phoenix-revive",
		Accounts: map[string]string{"account@example.test": "source-7"},
	}
	// Provider Date headers are second-precision; accept a code timestamp one
	// second before the request when it is otherwise the fresh response.
	code, err := requestAgentAutofillCode(context.Background(), cfg, "account@example.test", time.Date(2026, 1, 1, 0, 1, 1, 0, time.UTC))
	if err != nil || code.Code != "123456" || code.Recipient != "account@example.test" {
		t.Fatalf("JSON-lines Agent Autofill request failed: code_present=%v recipient_present=%v err=%v", code.Code != "", code.Recipient != "", err)
	}
	_, err = requestAgentAutofillCode(context.Background(), cfg, "account@example.test", time.Date(2026, 1, 1, 0, 1, 3, 0, time.UTC))
	if !errors.Is(err, errAgentAutofillNoCode) {
		t.Fatalf("older code must remain rejected: %v", err)
	}
}

func gotString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func setAgentAutofillTestEnv(t *testing.T, accountMap string) {
	t.Helper()
	t.Setenv("CPA_AGENT_AUTOFILL_AGENT_ID", "phoenix-test")
	t.Setenv("CPA_AGENT_AUTOFILL_AGENT_TOKEN", "test-token")
	t.Setenv("CPA_AGENT_AUTOFILL_ACCOUNT_MAP", accountMap)
	_ = os.Unsetenv("CPA_AGENT_AUTOFILL_ACCOUNT_ID")
}
