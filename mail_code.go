package main

// Mail-code integration keeps the repair queue independent from a particular
// mail client. Agent Autofill is the only source used by repair flows. Secrets
// and message bodies never enter Phoenix state or logs.

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"
)

var agentAutofillRequest = requestAgentAutofillCode

var (
	errAgentAutofillNotConfigured = errors.New("agent_autofill_not_configured")
	errAgentAutofillUnavailable   = errors.New("agent_autofill_unavailable")
	errAgentAutofillNoCode        = errors.New("verification_code_not_found")
)

var sixDigitCode = regexp.MustCompile(`^\d{6}$`)

type agentAutofillConfig struct {
	Command      string
	ServiceName  string
	DB           string
	AgentID      string
	Token        string
	TargetOrigin string
	Purpose      string
	Accounts     map[string]string // legacy compatibility only
}

type agentAutofillStatusSnapshot struct {
	Status  string `json:"status"`
	Sources struct {
		Configured     int `json:"configured"`
		Ready          int `json:"ready"`
		NeedsAttention int `json:"needs_attention"`
	} `json:"sources"`
}

// agentAutofillConfigFor uses the installed local connector by default. The
// legacy environment/token/source-map path remains readable only so an older
// deployment can migrate without a flag day; new services must use the stable
// service name and let Agent Autofill resolve the exact recipient.
func agentAutofillConfigFor(recipient string) (agentAutofillConfig, bool) {
	recipient = strings.ToLower(strings.TrimSpace(recipient))
	if recipient == "" {
		return agentAutofillConfig{}, false
	}
	command := strings.TrimSpace(os.Getenv("CPA_AGENT_AUTOFILL_COMMAND"))
	if command == "" {
		command = defaultAgentAutofillCommand()
	}
	targetOrigin := firstNonEmpty(strings.TrimSpace(os.Getenv("CPA_AGENT_AUTOFILL_TARGET_ORIGIN")), "https://auth.openai.com")
	purpose := firstNonEmpty(strings.TrimSpace(os.Getenv("CPA_AGENT_AUTOFILL_PURPOSE")), "cpa-phoenix-revive")
	serviceName := firstNonEmpty(strings.TrimSpace(os.Getenv("CPA_AGENT_AUTOFILL_SERVICE")), "cpa-phoenix")

	// Compatibility mode is selected only when the old credential variables are
	// present. It is never needed by the direct installed-service path.
	agentID := strings.TrimSpace(os.Getenv("CPA_AGENT_AUTOFILL_AGENT_ID"))
	token := strings.TrimSpace(os.Getenv("CPA_AGENT_AUTOFILL_AGENT_TOKEN"))
	if token == "" {
		token = strings.TrimSpace(os.Getenv("AGENT_AUTOFILL_AGENT_TOKEN"))
	}
	rawMap := strings.TrimSpace(os.Getenv("CPA_AGENT_AUTOFILL_ACCOUNT_MAP"))
	legacySource := strings.TrimSpace(os.Getenv("CPA_AGENT_AUTOFILL_ACCOUNT_ID"))
	if agentID != "" || token != "" || rawMap != "" || legacySource != "" {
		if agentID == "" || token == "" {
			return agentAutofillConfig{}, false
		}
		accounts := make(map[string]string)
		if rawMap != "" {
			if err := json.Unmarshal([]byte(rawMap), &accounts); err != nil {
				return agentAutofillConfig{}, false
			}
		}
		if legacySource != "" {
			accounts[recipient] = legacySource
		}
		normalized := make(map[string]string, len(accounts))
		for email, account := range accounts {
			normalizedEmail, normalizedAccount := strings.ToLower(strings.TrimSpace(email)), strings.TrimSpace(account)
			if normalizedEmail == "" || normalizedAccount == "" {
				continue
			}
			if existing, exists := normalized[normalizedEmail]; exists && existing != normalizedAccount {
				return agentAutofillConfig{}, false
			}
			normalized[normalizedEmail] = normalizedAccount
		}
		if strings.TrimSpace(normalized[recipient]) == "" {
			return agentAutofillConfig{}, false
		}
		return agentAutofillConfig{
			Command: command, DB: strings.TrimSpace(os.Getenv("CPA_AGENT_AUTOFILL_DB")),
			AgentID: agentID, Token: token, TargetOrigin: targetOrigin,
			Purpose: purpose, Accounts: normalized,
		}, true
	}
	return agentAutofillConfig{
		Command: command, ServiceName: serviceName, TargetOrigin: targetOrigin,
		Purpose: purpose,
	}, true
}

func defaultAgentAutofillCommand() string {
	if command, err := exec.LookPath("agent-autofill"); err == nil {
		return command
	}
	return "agent-autofill"
}

// inspectAgentAutofillStatus asks the installed local connector for its
// value-free readiness projection. An executable alone is not enough evidence
// that the encrypted broker, Keychain, and a recipient source are usable.
func inspectAgentAutofillStatus(command, db string) (string, string) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	args := []string{"quickstart", "--json"}
	if strings.TrimSpace(db) != "" {
		args = []string{"--db", strings.TrimSpace(db), "quickstart", "--json"}
	}
	cmd := exec.CommandContext(ctx, command, args...)
	cmd.Stderr = io.Discard
	output, err := cmd.Output()
	if err != nil {
		return "unavailable", "status_check_failed"
	}
	var snapshot agentAutofillStatusSnapshot
	if err := json.Unmarshal(output, &snapshot); err != nil || strings.TrimSpace(snapshot.Status) == "" {
		return "installed", "readiness_unverified"
	}
	// `quickstart` is intentionally value-free and does not open every saved
	// connector in its short-lived status process. A configured source with no
	// reported attention is therefore sufficient as a readiness hint for the
	// long-lived trusted local service; the actual OTP request still validates
	// the adapter and fails closed if it cannot read the mailbox.
	if snapshot.Sources.Configured > 0 && snapshot.Sources.NeedsAttention == 0 {
		if snapshot.Sources.Ready > 0 {
			return "ready", "broker_and_source_ready"
		}
		return "ready", "broker_and_source_configured"
	}
	return "needs_attention", "broker_or_source_needs_attention"
}

// mailSourceStatus is a small, non-secret projection for the dashboard. It
// tells a new user which OTP path Phoenix can use without probing a mailbox or
// exposing command-line credentials. Repair flows fail closed when Agent
// Autofill is unavailable; Phoenix never starts or scans a mail client.
func mailSourceStatus() map[string]any {
	command := strings.TrimSpace(os.Getenv("CPA_AGENT_AUTOFILL_COMMAND"))
	if command == "" {
		command = defaultAgentAutofillCommand()
	}
	resolved, err := exec.LookPath(command)
	status := "not_found"
	reason := "binary_not_found"
	if err == nil {
		// Compatibility deployments provide a recipient map and credentials
		// directly; the bounded request path is the readiness check there.
		legacy := strings.TrimSpace(os.Getenv("CPA_AGENT_AUTOFILL_AGENT_ID")) != "" ||
			strings.TrimSpace(os.Getenv("CPA_AGENT_AUTOFILL_AGENT_TOKEN")) != "" ||
			strings.TrimSpace(os.Getenv("AGENT_AUTOFILL_AGENT_TOKEN")) != "" ||
			strings.TrimSpace(os.Getenv("CPA_AGENT_AUTOFILL_ACCOUNT_MAP")) != "" ||
			strings.TrimSpace(os.Getenv("CPA_AGENT_AUTOFILL_ACCOUNT_ID")) != ""
		if legacy {
			status, reason = "ready", "legacy_request_path_configured"
		} else {
			status, reason = inspectAgentAutofillStatus(resolved, os.Getenv("CPA_AGENT_AUTOFILL_DB"))
		}
	}
	serviceName := firstNonEmpty(strings.TrimSpace(os.Getenv("CPA_AGENT_AUTOFILL_SERVICE")), "cpa-phoenix")
	return map[string]any{
		"preferred":       "agent_autofill",
		"agent_autofill":  status,
		"reason":          reason,
		"service":         serviceName,
		"fallback":        "none",
		"fallback_always": false,
	}
}

func userHomeDir() string {
	if home, err := os.UserHomeDir(); err == nil && strings.TrimSpace(home) != "" {
		return home
	}
	return "."
}

func detectRepairCode(recipient string, requestedAt time.Time) (thunderbirdCode, error) {
	if cfg, configured := agentAutofillConfigFor(recipient); configured {
		requestCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return agentAutofillRequest(requestCtx, cfg, recipient, requestedAt)
	}
	return thunderbirdCode{}, errAgentAutofillNotConfigured
}

type agentAutofillClient struct {
	mu          sync.Mutex
	command     string
	serviceName string
	db          string
	agentID     string
	token       string
	cmd         *exec.Cmd
	stdin       io.WriteCloser
	stdout      *bufio.Reader
	initialized bool
	nextID      int64
	retryKey    string
	retryAfter  time.Time
	retryCount  int
}

var defaultAgentAutofillClient = &agentAutofillClient{}

func (c *agentAutofillClient) resetLocked() {
	if c.stdin != nil {
		_ = c.stdin.Close()
	}
	if c.cmd != nil && c.cmd.Process != nil {
		_ = c.cmd.Process.Kill()
		_, _ = c.cmd.Process.Wait()
	}
	c.cmd, c.stdin, c.stdout = nil, nil, nil
	c.command, c.serviceName, c.db, c.agentID, c.token = "", "", "", "", ""
	c.initialized = false
}

func agentAutofillConfigKey(cfg agentAutofillConfig) string {
	return strings.Join([]string{cfg.Command, cfg.ServiceName, cfg.DB, cfg.AgentID, cfg.Token, cfg.TargetOrigin, cfg.Purpose}, "\x00")
}

func (c *agentAutofillClient) recordFailureLocked(cfg agentAutofillConfig) {
	key := agentAutofillConfigKey(cfg)
	if c.retryKey != key {
		c.retryKey, c.retryCount = key, 0
	}
	c.retryCount++
	shift := c.retryCount - 1
	if shift > 5 {
		shift = 5
	}
	delay := time.Second << shift
	if delay > 30*time.Second {
		delay = 30 * time.Second
	}
	c.retryAfter = time.Now().Add(delay)
}

func (c *agentAutofillClient) unavailableLocked(cfg agentAutofillConfig) error {
	c.recordFailureLocked(cfg)
	c.resetLocked()
	return errAgentAutofillUnavailable
}

func (c *agentAutofillClient) startLocked(ctx context.Context, cfg agentAutofillConfig) error {
	key := agentAutofillConfigKey(cfg)
	if c.retryKey != key {
		c.retryKey, c.retryCount, c.retryAfter = key, 0, time.Time{}
	}
	if !c.retryAfter.IsZero() && time.Now().Before(c.retryAfter) {
		return errAgentAutofillUnavailable
	}
	if c.cmd != nil && c.command == cfg.Command && c.db == cfg.DB && c.agentID == cfg.AgentID && c.token == cfg.Token && c.serviceName == cfg.ServiceName {
		return nil
	}
	c.resetLocked()
	// The broker is a long-lived JSON-lines child. Request contexts bound each
	// read below; tying the process lifetime to a ten-second read context would
	// kill a healthy server after every successful response.
	args := []string{"serve"}
	if cfg.ServiceName != "" {
		args = append(args, "--service", cfg.ServiceName)
		if strings.TrimSpace(cfg.DB) != "" {
			args = append(args, "--db", cfg.DB)
		}
	} else {
		args = append(args, "--db", cfg.DB)
	}
	cmd := exec.Command(cfg.Command, args...)
	// Give the child only the environment required to find Python/keychain and
	// authenticate the broker. In particular, no Phoenix state or OAuth data is
	// serialized into the command line.
	env := []string{"PATH=" + os.Getenv("PATH"), "HOME=" + userHomeDir()}
	if python := strings.TrimSpace(os.Getenv("AGENT_AUTOFILL_PYTHON")); python != "" {
		env = append(env, "AGENT_AUTOFILL_PYTHON="+python)
	}
	if cfg.Token != "" {
		// Only compatibility mode needs this old variable. Direct service mode
		// recovers the token inside Agent Autofill from Keychain.
		env = append(env, "AGENT_AUTOFILL_AGENT_TOKEN="+cfg.Token)
	}
	cmd.Env = env
	cmd.Stderr = io.Discard
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return c.unavailableLocked(cfg)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		return c.unavailableLocked(cfg)
	}
	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		return c.unavailableLocked(cfg)
	}
	c.command, c.db, c.agentID, c.token, c.serviceName = cfg.Command, cfg.DB, cfg.AgentID, cfg.Token, cfg.ServiceName
	c.cmd, c.stdin, c.stdout = cmd, stdin, bufio.NewReader(stdout)
	c.initialized = false
	return nil
}

func readAgentAutofillLine(ctx context.Context, reader *bufio.Reader) ([]byte, error) {
	result := make(chan struct {
		line []byte
		err  error
	}, 1)
	go func() {
		line, err := reader.ReadBytes('\n')
		result <- struct {
			line []byte
			err  error
		}{line, err}
	}()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case out := <-result:
		return out.line, out.err
	}
}

func (c *agentAutofillClient) call(ctx context.Context, cfg agentAutofillConfig, method string, args map[string]any) (json.RawMessage, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.startLocked(ctx, cfg); err != nil {
		return nil, errAgentAutofillUnavailable
	}
	write := func(req map[string]any) error {
		data, err := json.Marshal(req)
		if err != nil {
			return errAgentAutofillUnavailable
		}
		data = append(data, '\n')
		if _, err := c.stdin.Write(data); err != nil {
			return errAgentAutofillUnavailable
		}
		return nil
	}
	read := func() (map[string]any, error) {
		line, err := readAgentAutofillLine(ctx, c.stdout)
		if err != nil {
			return nil, errAgentAutofillUnavailable
		}
		var response map[string]any
		if json.Unmarshal(line, &response) != nil {
			return nil, errAgentAutofillUnavailable
		}
		return response, nil
	}
	if !c.initialized {
		if err := write(map[string]any{"jsonrpc": "2.0", "id": 0, "method": "initialize"}); err != nil {
			return nil, c.unavailableLocked(cfg)
		}
		if _, err := read(); err != nil {
			return nil, c.unavailableLocked(cfg)
		}
		if err := write(map[string]any{"jsonrpc": "2.0", "method": "notifications/initialized"}); err != nil {
			return nil, c.unavailableLocked(cfg)
		}
		c.initialized = true
		c.retryCount, c.retryAfter = 0, time.Time{}
	}
	c.nextID++
	if err := write(map[string]any{
		"jsonrpc": "2.0", "id": c.nextID, "method": "tools/call",
		"params": map[string]any{"name": method, "arguments": args},
	}); err != nil {
		return nil, c.unavailableLocked(cfg)
	}
	response, err := read()
	if err != nil {
		return nil, c.unavailableLocked(cfg)
	}
	if response["error"] != nil {
		classified := classifyAgentAutofillError(response["error"])
		if errors.Is(classified, errAgentAutofillNoCode) {
			return nil, classified
		}
		return nil, c.unavailableLocked(cfg)
	}
	result, _ := response["result"].(map[string]any)
	if result == nil {
		return nil, c.unavailableLocked(cfg)
	}
	if isError, _ := result["isError"].(bool); isError {
		classified := classifyAgentAutofillError(result["content"])
		if errors.Is(classified, errAgentAutofillNoCode) {
			return nil, classified
		}
		return nil, c.unavailableLocked(cfg)
	}
	content, _ := result["content"].([]any)
	if len(content) == 0 {
		return nil, c.unavailableLocked(cfg)
	}
	first, _ := content[0].(map[string]any)
	text, _ := first["text"].(string)
	if strings.TrimSpace(text) == "" {
		return nil, c.unavailableLocked(cfg)
	}
	return json.RawMessage(text), nil
}

func classifyAgentAutofillError(value any) error {
	// The broker's error text is intentionally reduced to stable categories.
	// It can contain provider details and must never reach Phoenix status.
	text := strings.ToLower(strings.TrimSpace(stringifyJSON(value)))
	if strings.Contains(text, "no unambiguous") || strings.Contains(text, "not found") || strings.Contains(text, "no recent") {
		return errAgentAutofillNoCode
	}
	return errAgentAutofillUnavailable
}

func stringifyJSON(value any) string {
	data, _ := json.Marshal(value)
	return string(data)
}

func requestAgentAutofillCode(ctx context.Context, cfg agentAutofillConfig, recipient string, requestedAt time.Time) (thunderbirdCode, error) {
	if requestedAt.IsZero() {
		return thunderbirdCode{}, errAgentAutofillNotConfigured
	}
	maxAge := int(time.Since(requestedAt).Seconds()) + 30
	if maxAge < 60 {
		maxAge = 60
	}
	if maxAge > 3600 {
		maxAge = 3600
	}
	method := "request_auth_code_for_recipient"
	args := map[string]any{
		"recipient": recipient, "target_origin": cfg.TargetOrigin,
		"purpose": cfg.Purpose, "max_age_seconds": maxAge,
	}
	if cfg.ServiceName == "" {
		// Compatibility with pre-pairing deployments. New callers never enter
		// this branch and never carry opaque account IDs.
		source := strings.TrimSpace(cfg.Accounts[strings.ToLower(strings.TrimSpace(recipient))])
		if source == "" {
			return thunderbirdCode{}, errAgentAutofillNotConfigured
		}
		method = "request_auth_code"
		args["agent_id"] = cfg.AgentID
		args["account_id"] = source
	}
	raw, err := defaultAgentAutofillClient.call(ctx, cfg, method, args)
	if err != nil {
		return thunderbirdCode{}, err
	}
	var result struct {
		Code       string `json:"code"`
		Provenance struct {
			ReceivedAt string `json:"received_at"`
		} `json:"provenance"`
	}
	if json.Unmarshal(raw, &result) != nil || !sixDigitCode.MatchString(result.Code) {
		return thunderbirdCode{}, errAgentAutofillUnavailable
	}
	receivedAt, err := time.Parse(time.RFC3339Nano, result.Provenance.ReceivedAt)
	if err != nil {
		return thunderbirdCode{}, errAgentAutofillUnavailable
	}
	// IMAP Date headers have only second-level precision.  A provider email
	// received in the same second as the OAuth request can therefore compare a
	// few milliseconds older than requestedAt even though it is the fresh code.
	// Allow that bounded precision skew without accepting an older message.
	if receivedAt.Before(requestedAt.Add(-2*time.Second)) || receivedAt.After(time.Now().Add(30*time.Second)) {
		return thunderbirdCode{}, errAgentAutofillNoCode
	}
	return thunderbirdCode{Code: result.Code, ReceivedAt: receivedAt, Recipient: strings.ToLower(strings.TrimSpace(recipient)), Source: "agent_autofill"}, nil
}

func closeAgentAutofillClient() {
	defaultAgentAutofillClient.mu.Lock()
	defaultAgentAutofillClient.resetLocked()
	defaultAgentAutofillClient.retryKey = ""
	defaultAgentAutofillClient.retryCount = 0
	defaultAgentAutofillClient.retryAfter = time.Time{}
	defaultAgentAutofillClient.mu.Unlock()
}
