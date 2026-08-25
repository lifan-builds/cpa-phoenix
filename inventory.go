package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const codexQuotaUsageURL = "https://chatgpt.com/backend-api/wham/usage"

// authFileEntry mirrors only the fields needed for an exact, explicitly
// Codex-scoped inventory. Tokens are decoded ephemerally and never returned.
type authFileEntry struct {
	Account       string `json:"account"`
	AccountType   string `json:"account_type"`
	AuthIndex     string `json:"auth_index"`
	Disabled      bool   `json:"disabled"`
	Email         string `json:"email"`
	Expired       bool   `json:"expired"`
	ID            string `json:"id"`
	Label         string `json:"label"`
	Name          string `json:"name"`
	Path          string `json:"path"`
	Plan          string `json:"plan"`
	PlanType      string `json:"plan_type"`
	Provider      string `json:"provider"`
	Source        string `json:"source"`
	Status        string `json:"status"`
	StatusMessage string `json:"status_message"`
	Subscription  string `json:"subscription"`
	Type          string `json:"type"`
	Unavailable   bool   `json:"unavailable"`
	RuntimeOnly   bool   `json:"runtime_only"`
	ModTime       string `json:"modtime"`
	UpdatedAt     string `json:"updated_at"`
}

type authListResponse struct {
	Files []authFileEntry `json:"files"`
}

type authRuntimeResponse struct {
	// CPA's host.auth.get_runtime wrapper contains the current auth record.
	// It does not expose an HTTP response status. In particular, any
	// recent_requests projection is an aggregate of time/success/failed
	// counters, not a status-code source, and is intentionally not modelled.
	Auth authFileEntry `json:"auth"`
}

type account struct {
	Key              string
	AuthIndex        string
	AuthID           string
	Email            string
	AccountID        string
	Provider         string
	AuthFile         string
	AuthPath         string
	AuthDir          string
	Physical         bool
	Disabled         bool
	Expired          bool
	Unavailable      bool
	Status           string
	StatusMessage    string
	Quota            quotaSnapshot
	AccessToken      bool
	AccessTokenValue string
	UpdatedAt        string
	RepairState      string
	Quarantine       string
	SeatLabel        string
	QuotaStatusCode  int
}

func stableSeatLabel(a account) string {
	seed := strings.TrimSpace(a.AccountID)
	if seed == "" {
		seed = strings.TrimSpace(a.Key)
	}
	sum := sha256.Sum256([]byte(seed))
	return "seat-" + hex.EncodeToString(sum[:])[:8]
}

func explicitCodex(v ...string) bool {
	for _, s := range v {
		switch strings.ToLower(strings.TrimSpace(s)) {
		case "codex", "openai", "chatgpt":
			return true
		}
	}
	return false
}

func fileName(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return ""
	}
	b := filepath.Base(filepath.Clean(v))
	if b == "." || b == ".." || !strings.EqualFold(filepath.Ext(b), ".json") {
		return ""
	}
	return b
}

// accountKey excludes paths and email so replacing a physical file or sharing
// an email cannot collapse distinct seats. It is only an opaque internal key.
func accountKey(e authFileEntry) string {
	identity := strings.TrimSpace(e.AuthIndex)
	if identity == "" {
		identity = strings.TrimSpace(e.ID)
	}
	if identity == "" {
		return ""
	}
	// host.auth.list does not expose the private ChatGPT account identifier;
	// the stable host identity is the auth index plus the host record ID.
	// The account identifier is hydrated ephemerally from host.auth.get only
	// when a repair row needs replacement validation.
	seed := strings.Join([]string{"codex", identity, strings.TrimSpace(e.ID)}, "\x00")
	sum := sha256.Sum256([]byte(seed))
	return hex.EncodeToString(sum[:])
}

func listAccounts() ([]account, error) {
	raw, err := hostCall("host.auth.list", map[string]any{})
	if err != nil {
		return nil, err
	}
	var response authListResponse
	if err := json.Unmarshal(raw, &response); err != nil {
		return nil, err
	}
	out := make([]account, 0, len(response.Files))
	indexCounts := make(map[string]int)
	for _, e := range response.Files {
		if !explicitCodex(e.Provider, e.Type) || e.RuntimeOnly {
			continue
		}
		idx := firstNonEmpty(e.AuthIndex, e.ID)
		if idx != "" {
			indexCounts[idx]++
		}
	}
	for _, e := range response.Files {
		if !explicitCodex(e.Provider, e.Type) || e.RuntimeOnly {
			continue
		}
		idx := strings.TrimSpace(e.AuthIndex)
		if idx == "" {
			idx = strings.TrimSpace(e.ID)
		}
		key := accountKey(e)
		authPath, authDir, authFile, physical := exactPhysicalAuthPath(e)
		// Do not infer a physical record from a filename alone. The host's
		// source/path metadata and the configured top-level directory must agree.
		if idx == "" || key == "" {
			continue
		}
		if indexCounts[idx] != 1 {
			continue
		}
		out = append(out, account{
			Key: key, AuthIndex: idx, AuthID: strings.TrimSpace(e.ID), Email: strings.TrimSpace(firstNonEmpty(e.Email, e.Account)), Provider: "codex", AuthFile: authFile, AuthPath: authPath, AuthDir: authDir, Physical: physical, Disabled: e.Disabled || strings.EqualFold(e.Status, "disabled"), Expired: e.Expired || strings.EqualFold(e.Status, "expired"), Unavailable: e.Unavailable, Status: strings.TrimSpace(e.Status), StatusMessage: strings.TrimSpace(e.StatusMessage), UpdatedAt: firstNonEmpty(e.UpdatedAt, e.ModTime),
		})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out, nil
}

func exactPhysicalAuthPath(e authFileEntry) (path, dir, name string, physical bool) {
	if e.RuntimeOnly || strings.EqualFold(strings.TrimSpace(e.Source), "memory") || strings.EqualFold(strings.TrimSpace(e.Source), "runtime") {
		return "", "", "", false
	}
	configured := configuredAuthDir()
	if configured == "" {
		return "", "", "", false
	}
	configuredAbs, err := filepath.Abs(configured)
	if err != nil {
		return "", "", "", false
	}
	var selected string
	for _, candidate := range []string{e.Path, e.Name, e.ID} {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" || !filepath.IsAbs(candidate) {
			continue
		}
		candidateAbs, err := filepath.Abs(candidate)
		if err != nil || filepath.Dir(candidateAbs) != configuredAbs {
			// An explicit absolute host path outside CPA's top-level auth
			// directory is evidence that this record is not safe to mutate;
			// do not fall back to another metadata field.
			return "", "", "", false
		}
		candidateName := fileName(candidateAbs)
		if candidateName == "" || filepath.Base(candidateAbs) != candidateName {
			return "", "", "", false
		}
		if selected == "" {
			selected = candidateAbs
		} else if selected != candidateAbs {
			// Path/name/ID disagree about the physical file. Treat the
			// record as ambiguous instead of guessing which file to move.
			return "", "", "", false
		}
	}
	if selected == "" {
		return "", "", "", false
	}
	// Metadata alone does not prove that CPA still has a mutable physical
	// record. Require one existing regular top-level file and reject symlinks so
	// both the read-only scan and any later repair/quarantine share the same
	// physical-record boundary.
	info, err := os.Lstat(selected)
	if err != nil || !info.Mode().IsRegular() {
		return "", "", "", false
	}
	name = filepath.Base(selected)
	return selected, configuredAbs, name, true
}

func configuredAuthDir() string {
	if value := strings.TrimSpace(os.Getenv("CPA_AUTH_DIR")); value != "" && filepath.IsAbs(value) {
		return filepath.Clean(value)
	}
	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		return ""
	}
	return filepath.Join(home, ".cli-proxy-api")
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func runtimeAuthRecord(a account) (authFileEntry, bool) {
	raw, err := hostCall("host.auth.get_runtime", map[string]string{"auth_index": a.AuthIndex})
	if err != nil {
		return authFileEntry{}, false
	}
	var r authRuntimeResponse
	if json.Unmarshal(raw, &r) != nil {
		return authFileEntry{}, false
	}
	return r.Auth, true
}

// invalidAuthDecision is shared by scan and the Revive queue. The quota
// endpoint's exact HTTP 401 is held only on the in-memory account projection;
// host runtime data contributes only its explicit auth status/marker fields.
// No recent_requests aggregate or synthetic top-level status field is parsed.
func invalidAuthDecision(a account) bool {
	if !a.Physical {
		return false
	}
	if invalidAccount(a, 0) {
		return true
	}
	runtime, ok := runtimeAuthRecord(a)
	if !ok {
		return false
	}
	return invalidAuthRuntimeRecord(a, runtime)
}

func invalidAuthRuntimeRecord(a account, runtime authFileEntry) bool {
	return invalidAccount(account{Physical: a.Physical, Status: runtime.Status, StatusMessage: runtime.StatusMessage}, 0)
}

func credentialFromRaw(raw json.RawMessage) (token, accountID string) {
	var value struct {
		AccessToken     string          `json:"access_token"`
		AccessTokenAlt  string          `json:"accessToken"`
		Credential      string          `json:"credential"`
		Token           string          `json:"token"`
		AccountID       string          `json:"account_id"`
		AccountIDAlt    string          `json:"accountId"`
		ChatGPTAccount  string          `json:"chatgpt_account_id"`
		ChatGPTAccount2 string          `json:"chatgptAccountId"`
		JSON            json.RawMessage `json:"json"`
	}
	if json.Unmarshal(raw, &value) != nil {
		return "", ""
	}
	token = firstNonEmpty(value.AccessToken, value.AccessTokenAlt, value.Credential, value.Token)
	accountID = firstNonEmpty(value.AccountID, value.AccountIDAlt, value.ChatGPTAccount, value.ChatGPTAccount2)
	if len(value.JSON) > 0 {
		var nested map[string]any
		if json.Unmarshal(value.JSON, &nested) == nil {
			if token == "" {
				for _, key := range []string{"access_token", "accessToken", "token"} {
					if candidate, ok := nested[key].(string); ok && strings.TrimSpace(candidate) != "" {
						token = strings.TrimSpace(candidate)
						break
					}
				}
			}
			if accountID == "" {
				for _, key := range []string{"account_id", "accountId", "chatgpt_account_id", "chatgptAccountId"} {
					if candidate, ok := nested[key].(string); ok && strings.TrimSpace(candidate) != "" {
						accountID = strings.TrimSpace(candidate)
						break
					}
				}
			}
		}
	}
	return token, accountID
}

// hostCredential keeps the host lookup and ephemeral credential decoding in
// one inventory-owned path. Callers may use the returned token only for the
// bounded operation they are about to perform; neither the raw wrapper nor
// the token is retained in durable account projections.
func hostCredential(a account) (token, accountID string, ok bool) {
	raw, err := hostCall("host.auth.get", map[string]string{"auth_index": a.AuthIndex})
	if err != nil {
		return "", "", false
	}
	token, accountID = credentialFromRaw(raw)
	return token, accountID, token != ""
}

func credentialAvailable(a account) bool {
	if strings.TrimSpace(a.AccessTokenValue) != "" {
		return true
	}
	_, _, ok := hostCredential(a)
	return ok
}

func refreshCredential(a account) account {
	if a.AccessTokenValue != "" {
		a.AccessToken = true
		return a
	}
	token, accountID, ok := hostCredential(a)
	if ok {
		a.AccessTokenValue, a.AccessToken = token, true
	}
	if a.AccountID == "" {
		a.AccountID = accountID
	}
	return a
}

// resolveRepairAccount first honors the durable host key captured when the
// queue was created. CPA may issue a different host key after a same-account
// file refresh, so only when that key is absent do we hydrate candidates and
// match the private account ID exactly. Email is deliberately never a
// fallback because multiple teams can share one address.
func resolveRepairAccount(accounts []account, expected account) (account, bool) {
	var byKey account
	keyMatches := 0
	for _, candidate := range accounts {
		if !candidate.Physical || candidate.Key != expected.Key {
			continue
		}
		byKey = candidate
		keyMatches++
	}
	if keyMatches == 1 {
		if strings.TrimSpace(expected.AccountID) == "" {
			// A durable host key is not enough to identify a same-email team.
			// Rows without the captured private account ID are skipped rather
			// than allowing a stale key to choose a physical file.
			return account{}, false
		}
		// A durable host key is only a routing hint once we have captured the
		// private account ID. Hydrate and verify it before trusting the key;
		// stale CPA keys must not let a same-email team be quarantined.
		verified := byKey
		if verified.AccountID == "" {
			verified = refreshCredential(verified)
		}
		if strings.TrimSpace(verified.AccountID) == strings.TrimSpace(expected.AccountID) {
			return verified, true
		}
	}
	if keyMatches > 1 || strings.TrimSpace(expected.AccountID) == "" {
		return account{}, false
	}
	var byAccountID account
	accountMatches := 0
	for _, candidate := range accounts {
		if !candidate.Physical {
			continue
		}
		candidate = refreshCredential(candidate)
		if strings.TrimSpace(candidate.AccountID) != strings.TrimSpace(expected.AccountID) {
			continue
		}
		byAccountID = candidate
		accountMatches++
	}
	if accountMatches != 1 {
		return account{}, false
	}
	return byAccountID, true
}

func refreshQuota(a account) account {
	// Status belongs only to this probe; a transport failure must not reuse a
	// previous response from the same in-memory account value.
	a.QuotaStatusCode = 0
	token, accountID, ok := hostCredential(a)
	if !ok {
		return a
	}
	if a.AccountID == "" {
		a.AccountID = accountID
	}
	request, err := http.NewRequestWithContext(context.Background(), http.MethodGet, codexQuotaUsageURL, nil)
	if err != nil {
		return a
	}
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("OpenAI-Beta", "codex-1")
	request.Header.Set("User-Agent", "codex_cli_rs/0.76.0")
	if a.AccountID != "" {
		request.Header.Set("Chatgpt-Account-Id", a.AccountID)
	}
	client := &http.Client{Timeout: 15 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Do(request)
	if err != nil {
		return a
	}
	defer response.Body.Close()
	// Keep only the numeric provider result for auth classification. The body
	// is parsed for quota freshness and never retained.
	a.QuotaStatusCode = response.StatusCode
	body, _ := io.ReadAll(io.LimitReader(response.Body, 2<<20))
	if response.StatusCode >= 200 && response.StatusCode < 300 {
		a.Quota = parseCodexQuotaPayload(body, time.Now().Unix())
	}
	return a
}

func replacementMatches(before, after []account, expected account) (account, bool) {
	beforeByKey := make(map[string]account, len(before))
	for _, a := range before {
		beforeByKey[a.Key] = a
	}
	var match account
	found := 0
	for _, a := range after {
		if !a.Physical {
			continue
		}
		// Inventory metadata does not include ChatGPT account_id. Hydrate each
		// candidate ephemerally before comparing it to the private identity
		// captured before quarantine; no token leaves this function.
		if expected.AccountID != "" && a.AccountID == "" {
			a = captureRepairIdentity(a)
		}
		// A replacement without the exact private account ID is never safe to
		// accept. Shared email addresses identify a pool, not a seat.
		same := expected.AccountID != "" && a.AccountID != "" && expected.AccountID == a.AccountID
		if same {
			prior, existed := beforeByKey[a.Key]
			// A pre-existing seat is not the replacement. A same-key
			// replacement is accepted only when CPA reports a changed
			// physical update marker after quarantine; without a predecessor
			// marker there is no proof that this record was newly written.
			if a.Key == expected.Key {
				if !existed || !replacementMarkerChanged(prior, a) {
					continue
				}
			} else if existed {
				continue
			}
			match, found = a, found+1
		}
	}
	return match, found == 1
}

func replacementMarkerChanged(before, after account) bool {
	if before.AuthPath != after.AuthPath || before.AuthFile != after.AuthFile {
		return true
	}
	if strings.TrimSpace(before.UpdatedAt) == "" || strings.TrimSpace(after.UpdatedAt) == "" {
		return false
	}
	return before.UpdatedAt != after.UpdatedAt
}
