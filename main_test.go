package main

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

func TestPhoenixCapabilitiesDoNotRegisterProxyScheduler(t *testing.T) {
	caps := phoenixCapabilities()
	if !caps.ManagementAPI {
		t.Fatal("Phoenix must register its management API")
	}
	if caps.Scheduler {
		t.Fatal("Phoenix must not register as a proxy request scheduler")
	}
	if caps.UsagePlugin {
		t.Fatal("Phoenix must not register as a usage plugin")
	}
}

func TestOTPStatusRouteReturnsSanitizedConnectorState(t *testing.T) {
	t.Setenv("CPA_AGENT_AUTOFILL_COMMAND", "/missing/agent-autofill")
	response := routeManagement(managementRequest{Method: http.MethodGet, Path: "/v0/management/plugins/cpa-phoenix/otp/status"})
	if response.StatusCode != http.StatusOK {
		t.Fatalf("otp status=%d body=%s", response.StatusCode, response.Body)
	}
	var status map[string]any
	if err := json.Unmarshal(response.Body, &status); err != nil {
		t.Fatal(err)
	}
	if status["preferred"] != "agent_autofill" || status["fallback"] != "none" || status["agent_autofill"] != "not_found" {
		t.Fatalf("unexpected otp status: %#v", status)
	}
}

func TestOTPStatusReportsInstalledConnectorWithoutRecipientMap(t *testing.T) {
	command := filepath.Join(t.TempDir(), "agent-autofill")
	if err := os.WriteFile(command, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CPA_AGENT_AUTOFILL_COMMAND", command)
	t.Setenv("CPA_AGENT_AUTOFILL_SERVICE", "fixture-service")
	response := routeManagement(managementRequest{Method: http.MethodGet, Path: "/v0/management/plugins/cpa-phoenix/otp/status"})
	var status map[string]any
	if err := json.Unmarshal(response.Body, &status); err != nil {
		t.Fatal(err)
	}
	if status["agent_autofill"] != "installed" || status["reason"] != "readiness_unverified" || status["service"] != "fixture-service" {
		t.Fatalf("unexpected installed connector status: %#v", status)
	}
}
