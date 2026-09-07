package main

/*
#include <stdint.h>
#include <stdlib.h>
typedef struct { void* ptr; size_t len; } cliproxy_buffer;
typedef int (*cliproxy_host_call_fn)(void*, const char*, const uint8_t*, size_t, cliproxy_buffer*);
typedef void (*cliproxy_host_free_fn)(void*, size_t);
typedef struct { uint32_t abi_version; void* host_ctx; cliproxy_host_call_fn call; cliproxy_host_free_fn free_buffer; } cliproxy_host_api;
typedef int (*cliproxy_plugin_call_fn)(char*, uint8_t*, size_t, cliproxy_buffer*);
typedef void (*cliproxy_plugin_free_fn)(void*, size_t);
typedef void (*cliproxy_plugin_shutdown_fn)(void);
typedef struct { uint32_t abi_version; cliproxy_plugin_call_fn call; cliproxy_plugin_free_fn free_buffer; cliproxy_plugin_shutdown_fn shutdown; } cliproxy_plugin_api;
static const cliproxy_host_api* stored_host;
static void store_host_api(const cliproxy_host_api* h) { stored_host = h; }
static int call_host_api(const char* method, const uint8_t* req, size_t n, cliproxy_buffer* out) {
  if (!stored_host || !stored_host->call) return 1;
  return stored_host->call(stored_host->host_ctx, method, req, n, out);
}
static void free_host_buffer(void* p, size_t n) { if (stored_host && stored_host->free_buffer && p) stored_host->free_buffer(p, n); }
extern int cliproxyPluginCall(char*, uint8_t*, size_t, cliproxy_buffer*);
extern void cliproxyPluginFree(void*, size_t);
extern void cliproxyPluginShutdown(void);
*/
import "C"

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
	"unsafe"
)

var timeNow = time.Now

const (
	abiVersion    = 1
	pluginID      = "cpa-phoenix"
	pluginVersion = "0.1.0"
)

type envelope struct {
	OK     bool            `json:"ok"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *envelopeError  `json:"error,omitempty"`
}
type envelopeError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}
type pluginMetadata struct {
	Name             string `json:"Name"`
	Version          string `json:"Version"`
	Author           string `json:"Author"`
	GitHubRepository string `json:"GitHubRepository"`
	Logo             string `json:"Logo"`
	ConfigFields     []any  `json:"ConfigFields"`
}
type pluginRegisterResponse struct {
	SchemaVersion int            `json:"schema_version"`
	Metadata      pluginMetadata `json:"metadata"`
	Capabilities  capabilities   `json:"capabilities"`
}
type capabilities struct {
	UsagePlugin   bool `json:"usage_plugin"`
	ManagementAPI bool `json:"management_api"`
	Scheduler     bool `json:"scheduler"`
}
type managementRegistrationResponse struct {
	Routes    []managementRoute `json:"routes"`
	Resources []resourceRoute   `json:"resources"`
}
type managementRoute struct {
	Method      string `json:"Method"`
	Path        string `json:"Path"`
	Description string `json:"Description,omitempty"`
}
type resourceRoute struct {
	Path        string `json:"Path"`
	Menu        string `json:"Menu"`
	Description string `json:"Description"`
}
type lifecycleRequest struct {
	ConfigYAML json.RawMessage `json:"config_yaml"`
}
type managementRequest struct {
	Method  string              `json:"Method"`
	Path    string              `json:"Path"`
	Headers map[string][]string `json:"Headers"`
	Query   map[string][]string `json:"Query"`
	Body    []byte              `json:"Body"`
}
type managementResponse struct {
	StatusCode int                 `json:"StatusCode"`
	Headers    map[string][]string `json:"Headers"`
	Body       []byte              `json:"Body"`
}

func main() {}

//export cliproxy_plugin_init
func cliproxy_plugin_init(host *C.cliproxy_host_api, plugin *C.cliproxy_plugin_api) C.int {
	if plugin == nil {
		return 1
	}
	C.store_host_api(host)
	plugin.abi_version = C.uint32_t(abiVersion)
	plugin.call = C.cliproxy_plugin_call_fn(C.cliproxyPluginCall)
	plugin.free_buffer = C.cliproxy_plugin_free_fn(C.cliproxyPluginFree)
	plugin.shutdown = C.cliproxy_plugin_shutdown_fn(C.cliproxyPluginShutdown)
	return 0
}

//export cliproxyPluginCall
func cliproxyPluginCall(method *C.char, request *C.uint8_t, requestLen C.size_t, response *C.cliproxy_buffer) C.int {
	if response != nil {
		response.ptr = nil
		response.len = 0
	}
	if method == nil {
		writeResponse(response, errorEnvelope("invalid_method", "method is required"))
		return 1
	}
	var req []byte
	if request != nil && requestLen > 0 {
		req = C.GoBytes(unsafe.Pointer(request), C.int(requestLen))
	}
	raw, err := handleMethod(C.GoString(method), req)
	if err != nil {
		writeResponse(response, errorEnvelope("plugin_error", "plugin request failed"))
		return 1
	}
	writeResponse(response, raw)
	return 0
}

//export cliproxyPluginFree
func cliproxyPluginFree(ptr unsafe.Pointer, _ C.size_t) {
	if ptr != nil {
		C.free(ptr)
	}
}

//export cliproxyPluginShutdown
func cliproxyPluginShutdown() { stopAllRevive(); globalJobs.stop(); closeStore() }

func writeResponse(out *C.cliproxy_buffer, raw []byte) {
	if out == nil {
		return
	}
	if len(raw) == 0 {
		raw = []byte(`{"ok":false}`)
	}
	p := C.CBytes(raw)
	out.ptr = p
	out.len = C.size_t(len(raw))
}
func okJSON(v any) []byte {
	raw, _ := json.Marshal(map[string]any{"ok": true, "result": v})
	return raw
}
func errorEnvelope(code, message string) []byte {
	raw, _ := json.Marshal(map[string]any{"ok": false, "error": map[string]any{"code": code, "message": message}})
	return raw
}
func jsonResponse(status int, value any) managementResponse {
	raw, _ := json.Marshal(value)
	return managementResponse{StatusCode: status, Headers: map[string][]string{"content-type": {"application/json"}, "cache-control": {"no-store"}}, Body: raw}
}
func hostCall(method string, payload any) (json.RawMessage, error) {
	b, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	m := C.CString(method)
	defer C.free(unsafe.Pointer(m))
	var out C.cliproxy_buffer
	var p *C.uint8_t
	if len(b) > 0 {
		ptr := C.CBytes(b)
		defer C.free(ptr)
		p = (*C.uint8_t)(ptr)
	}
	code := C.call_host_api(m, p, C.size_t(len(b)), &out)
	if out.ptr == nil || out.len == 0 {
		return nil, fmt.Errorf("host unavailable")
	}
	raw := C.GoBytes(out.ptr, C.int(out.len))
	C.free_host_buffer(out.ptr, out.len)
	if code != 0 {
		return nil, fmt.Errorf("host callback failed")
	}
	var env envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, errors.New("host response invalid")
	}
	if !env.OK {
		return nil, errors.New("host callback rejected")
	}
	return env.Result, nil
}

func handleMethod(method string, request []byte) ([]byte, error) {
	switch method {
	case "plugin.register", "plugin.reconfigure":
		var ignored lifecycleRequest
		if len(request) > 0 && json.Unmarshal(request, &ignored) != nil {
			return nil, errors.New("invalid lifecycle request")
		}
		return okJSON(pluginRegisterResponse{SchemaVersion: 1, Metadata: pluginMetadata{Name: "CPA Phoenix", Version: pluginVersion, Author: "lifan-builds", GitHubRepository: "https://github.com/lifan-builds/cpa-phoenix"}, Capabilities: capabilities{ManagementAPI: true}}), nil
	case "management.register":
		return okJSON(managementRegistrationResponse{Routes: []managementRoute{{"POST", "/plugins/cpa-phoenix/scan", "Refresh sanitized account counts."}, {"GET", "/plugins/cpa-phoenix/state", "Read sanitized job state."}, {"POST", "/plugins/cpa-phoenix/ignite", "Ignite all fresh accounts once."}, {"POST", "/plugins/cpa-phoenix/revive", "Revive all invalid accounts sequentially."}, {"POST", "/plugins/cpa-phoenix/revive/poll", "Advance the guided repair."}, {"POST", "/plugins/cpa-phoenix/revive/code", "Read a fresh verification code from Thunderbird."}, {"POST", "/plugins/cpa-phoenix/revive/cancel", "Cancel the guided repair."}}, Resources: []resourceRoute{{"/dashboard", "Phoenix", "Fresh-account activation and invalid-auth repair."}}}), nil
	case "management.handle":
		var req managementRequest
		if err := json.Unmarshal(request, &req); err != nil {
			return okJSON(jsonResponse(http.StatusBadRequest, map[string]string{"error": "bad_request"})), nil
		}
		return okJSON(handleManagement(req)), nil
	default:
		return errorEnvelope("unknown_method", "unknown method"), nil
	}
}

func handleManagement(req managementRequest) managementResponse {
	prefix := "/v0/management/plugins/" + pluginID
	if req.Path == "/v0/resource/plugins/"+pluginID+"/dashboard" && req.Method == http.MethodGet {
		return managementResponse{StatusCode: 200, Headers: map[string][]string{"content-type": {"text/html; charset=utf-8"}, "cache-control": {"no-store"}}, Body: dashboardPageHTML()}
	}
	if !strings.HasPrefix(req.Path, prefix) {
		return jsonResponse(404, map[string]string{"error": "not_found"})
	}
	return routeManagement(req)
}

func routeManagement(req managementRequest) managementResponse {
	path := strings.TrimPrefix(req.Path, "/v0/management/plugins/"+pluginID)
	switch {
	case path == "/scan" && req.Method == http.MethodPost:
		accounts, err := listAccounts()
		if err != nil {
			return jsonResponse(503, map[string]string{"error": "inventory_unavailable"})
		}
		fresh, invalid := 0, 0
		rows := make([]map[string]any, 0, len(accounts))
		now := timeNow()
		for _, a := range accounts {
			if !a.Physical {
				continue
			}
			if a.Physical && !a.Disabled && !a.Expired && !a.Unavailable {
				a = refreshQuota(a)
			}
			invalidRecord := invalidAuthDecision(a)
			if scanFreshEligible(a, now) {
				fresh++
			}
			if invalidRecord {
				invalid++
			}
			status := "healthy"
			if invalidRecord {
				status = "invalid"
			} else if a.Disabled {
				status = "disabled"
			} else if a.Expired {
				status = "expired"
			} else if a.Unavailable {
				status = "unavailable"
			}
			quotaState := "unknown"
			if freshQuota(a.Quota, now) {
				quotaState = "fresh"
			} else if len(quotaWindows(a.Quota)) > 0 {
				quotaState = "active"
			}
			rows = append(rows, map[string]any{"number": len(rows) + 1, "email": a.Email, "seat": stableSeatLabel(a), "status": status, "quota": quotaState})
		}
		return jsonResponse(200, map[string]any{"fresh": fresh, "invalid": invalid, "active": anyJobActive(), "active_job": activeJobProjection(), "last_job": latestJobProjection(), "accounts": rows, "repair_queue": incompleteRepairQueue()})
	case path == "/ignite" && req.Method == http.MethodPost:
		if !acknowledged(req.Body) {
			return jsonResponse(400, map[string]string{"error": "acknowledgement_required"})
		}
		accounts, err := listAccounts()
		if err != nil {
			return jsonResponse(503, map[string]string{"error": "inventory_unavailable"})
		}
		if countFreshActionable(accounts, timeNow()) == 0 {
			return jsonResponse(http.StatusConflict, map[string]string{"error": "no_actionable_accounts"})
		}
		id, err := globalJobs.start("ignite", len(accounts))
		if err != nil {
			return jsonResponse(409, map[string]string{"error": "job_active"})
		}
		go func() {
			ctx := globalJobs.context(id)
			current, inventoryErr := listAccounts()
			if inventoryErr != nil {
				updateJob(id, "completed", "inventory_unavailable", 0)
				return
			}
			if db, dbErr := openStore(); dbErr == nil {
				_, _ = db.Exec(`UPDATE jobs SET total=?,updated_at=? WHERE id=?`, len(current), time.Now().Unix(), id)
			}
			result := ignite(ctx, current, timeNow())
			updateJobResult(id, result)
			if ctx.Err() != nil {
				updateJob(id, "cancelled", "cancelled", len(accounts))
			} else {
				updateJob(id, "completed", "", len(current))
			}
		}()
		return jsonResponse(202, map[string]any{"job_id": id, "state": "running"})
	case path == "/revive" && req.Method == http.MethodPost:
		if !acknowledged(req.Body) {
			return jsonResponse(400, map[string]string{"error": "acknowledgement_required"})
		}
		browserMode, err := requestedReviveBrowserMode(req.Body)
		if err != nil {
			if errors.Is(err, errInvalidBrowserMode) {
				return jsonResponse(http.StatusBadRequest, map[string]string{"error": "invalid_browser_mode"})
			}
			return jsonResponse(http.StatusBadRequest, map[string]string{"error": "bad_request"})
		}
		result, err := beginRevive(req.Headers, browserMode)
		if err != nil {
			if errors.Is(err, errNoActionableAccounts) {
				return jsonResponse(http.StatusConflict, map[string]string{"error": "no_actionable_accounts"})
			}
			if errors.Is(err, errInvalidBrowserMode) {
				return jsonResponse(http.StatusBadRequest, map[string]string{"error": "invalid_browser_mode"})
			}
			return jsonResponse(409, map[string]string{"error": "job_active"})
		}
		return jsonResponse(202, result)
	case path == "/state" && req.Method == http.MethodGet:
		id := firstQuery(req.Query, "id", "")
		if id == "" {
			return jsonResponse(400, map[string]string{"error": "id_required"})
		}
		s, err := readJob(id)
		if err != nil {
			return jsonResponse(404, map[string]string{"error": "not_found"})
		}
		return jsonResponse(200, s)
	case path == "/revive/poll" && req.Method == http.MethodPost:
		id := firstQuery(req.Query, "id", "")
		if id == "" {
			return jsonResponse(400, map[string]string{"error": "id_required"})
		}
		return revivePoll(id)
	case path == "/revive/code" && req.Method == http.MethodPost:
		id := firstQuery(req.Query, "id", "")
		return reviveCode(id)
	case path == "/revive/cancel" && req.Method == http.MethodPost:
		id := firstQuery(req.Query, "id", "")
		if id == "" {
			return jsonResponse(http.StatusBadRequest, map[string]string{"error": "id_required"})
		}
		if !cancelRevive(id, req.Headers) {
			return jsonResponse(http.StatusNotFound, map[string]string{"error": "not_found"})
		}
		globalJobs.stopIf(id)
		return jsonResponse(200, map[string]string{"state": "cancelled"})
	default:
		return jsonResponse(404, map[string]string{"error": "not_found"})
	}
}
func acknowledged(raw []byte) bool {
	var v struct {
		Acknowledge string `json:"acknowledge"`
	}
	return json.Unmarshal(raw, &v) == nil && v.Acknowledge == "CPA_PHOENIX_ONE_CLICK"
}

func requestedReviveBrowserMode(raw []byte) (string, error) {
	var request struct {
		BrowserMode string `json:"browser_mode"`
	}
	if err := json.Unmarshal(raw, &request); err != nil {
		return "", err
	}
	return normalizeReviveBrowserMode(request.BrowserMode)
}
func firstQuery(q map[string][]string, k, f string) string {
	if q == nil || len(q[k]) == 0 {
		return f
	}
	return strings.TrimSpace(q[k][0])
}
