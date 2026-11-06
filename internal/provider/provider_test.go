package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

const (
	vmType  = "onidel_virtual_machine"
	vmID    = "c1d45377-b2a9-4407-ab8d-6909c34dfaac"
	teamID  = "6484a164-d355-4947-ac2c-e8c09fb24fb0"
	otherID = "53959c6e-73d5-4476-9b19-d78fb8efac2c"
)

var testCtx = context.Background()

type harness struct {
	t      *testing.T
	server tfprotov6.ProviderServer
	schema *tfprotov6.GetProviderSchemaResponse
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	s := providerserver.NewProtocol6(New("test")())()
	schema, err := s.GetProviderSchema(testCtx, &tfprotov6.GetProviderSchemaRequest{})
	checkRPC(t, err, schema.Diagnostics)
	return &harness{t: t, server: s, schema: schema}
}
func checkRPC(t *testing.T, err error, diags []*tfprotov6.Diagnostic) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range diags {
		if d.Severity == tfprotov6.DiagnosticSeverityError {
			t.Fatalf("%s: %s", d.Summary, d.Detail)
		}
	}
}
func hasError(diags []*tfprotov6.Diagnostic) bool {
	for _, d := range diags {
		if d.Severity == tfprotov6.DiagnosticSeverityError {
			return true
		}
	}
	return false
}
func dynamic(t *testing.T, typ tftypes.Type, values map[string]any) *tfprotov6.DynamicValue {
	t.Helper()
	var value tftypes.Value
	if values == nil {
		value = tftypes.NewValue(typ, nil)
	} else {
		attrs := map[string]tftypes.Value{}
		for name, at := range typ.(tftypes.Object).AttributeTypes {
			if v, ok := values[name].(tftypes.Value); ok {
				attrs[name] = v
			} else {
				attrs[name] = tftypes.NewValue(at, values[name])
			}
		}
		value = tftypes.NewValue(typ, attrs)
	}
	result, err := tfprotov6.NewDynamicValue(typ, value)
	if err != nil {
		t.Fatal(err)
	}
	return &result
}
func decode(t *testing.T, typ tftypes.Type, data *tfprotov6.DynamicValue) map[string]tftypes.Value {
	t.Helper()
	if data == nil {
		t.Fatal("nil state")
	}
	v, err := data.Unmarshal(typ)
	if err != nil {
		t.Fatal(err)
	}
	if v.IsNull() {
		return nil
	}
	var attrs map[string]tftypes.Value
	if err := v.As(&attrs); err != nil {
		t.Fatal(err)
	}
	return attrs
}
func (h *harness) configure(values map[string]any) []*tfprotov6.Diagnostic {
	h.t.Helper()
	resp, err := h.server.ConfigureProvider(testCtx, &tfprotov6.ConfigureProviderRequest{Config: dynamic(h.t, h.schema.Provider.ValueType(), values), TerraformVersion: "1.11.0"})
	if err != nil {
		h.t.Fatal(err)
	}
	return resp.Diagnostics
}
func (h *harness) resourceType() tftypes.Type { return h.schema.ResourceSchemas[vmType].ValueType() }
func (h *harness) config(values map[string]any) *tfprotov6.DynamicValue {
	return dynamic(h.t, h.resourceType(), values)
}
func (h *harness) nullState() *tfprotov6.DynamicValue { return h.config(nil) }
func (h *harness) plan(config, state *tfprotov6.DynamicValue) *tfprotov6.PlanResourceChangeResponse {
	h.t.Helper()
	attrs := decode(h.t, h.resourceType(), config)
	prior := decode(h.t, h.resourceType(), state)
	proposed := map[string]any{}
	for _, a := range h.schema.ResourceSchemas[vmType].Block.Attributes {
		value := attrs[a.Name]
		if a.Computed && value.IsNull() && prior != nil {
			value = prior[a.Name]
		}
		proposed[a.Name] = value
	}
	resp, err := h.server.PlanResourceChange(testCtx, &tfprotov6.PlanResourceChangeRequest{TypeName: vmType, Config: config, PriorState: state, ProposedNewState: h.config(proposed)})
	checkRPC(h.t, err, resp.Diagnostics)
	return resp
}
func (h *harness) apply(config, state *tfprotov6.DynamicValue, plan *tfprotov6.PlanResourceChangeResponse) *tfprotov6.ApplyResourceChangeResponse {
	h.t.Helper()
	resp, err := h.server.ApplyResourceChange(testCtx, &tfprotov6.ApplyResourceChangeRequest{TypeName: vmType, Config: config, PriorState: state, PlannedState: plan.PlannedState, PlannedPrivate: plan.PlannedPrivate})
	if err != nil {
		h.t.Fatal(err)
	}
	return resp
}
func (h *harness) create(values map[string]any) (*tfprotov6.DynamicValue, *tfprotov6.ApplyResourceChangeResponse) {
	h.t.Helper()
	config := h.config(values)
	plan := h.plan(config, h.nullState())
	result := h.apply(config, h.nullState(), plan)
	if !hasError(result.Diagnostics) {
		assertConsistent(h.t, h.resourceType(), plan.PlannedState, result.NewState)
	}
	return config, result
}
func (h *harness) read(state *tfprotov6.DynamicValue) *tfprotov6.ReadResourceResponse {
	h.t.Helper()
	resp, err := h.server.ReadResource(testCtx, &tfprotov6.ReadResourceRequest{TypeName: vmType, CurrentState: state})
	if err != nil {
		h.t.Fatal(err)
	}
	return resp
}
func (h *harness) destroy(state *tfprotov6.DynamicValue) *tfprotov6.ApplyResourceChangeResponse {
	return h.apply(h.nullState(), state, &tfprotov6.PlanResourceChangeResponse{PlannedState: h.nullState()})
}
func assertConsistent(t *testing.T, typ tftypes.Type, planned, actual *tfprotov6.DynamicValue) {
	t.Helper()
	p, a := decode(t, typ, planned), decode(t, typ, actual)
	for key, pv := range p {
		av := a[key]
		if !av.IsFullyKnown() {
			t.Errorf("%s remains unknown after apply", key)
		}
		if pv.IsFullyKnown() && !pv.Equal(av) {
			t.Errorf("%s changed a known planned value: %v -> %v", key, pv, av)
		}
	}
}
func assertValue(t *testing.T, attrs map[string]tftypes.Value, name string, want any) {
	t.Helper()
	got := attrs[name]
	if !got.Equal(tftypes.NewValue(got.Type(), want)) {
		t.Errorf("%s = %s, want %v", name, got, want)
	}
}
func baseConfig() map[string]any {
	return map[string]any{"name": "test-vm", "payment_cycle": "hourly", "instance_type": otherID, "location": "Sydney", "cpu": int64(2), "ram": int64(4096), "disk": int64(40), "os": int64(2)}
}
func stringSet(values ...string) []tftypes.Value {
	result := make([]tftypes.Value, len(values))
	for i, value := range values {
		result[i] = tftypes.NewValue(tftypes.String, value)
	}
	return result
}

// All lifecycle calls go to this local fake API. No acceptance-test credentials
// or paid infrastructure are needed, and the real client still serializes HTTP.
type fakeAPI struct {
	mu                    sync.Mutex
	vm                    map[string]any
	calls                 []string
	createBody, patchBody map[string]any
	statuses              map[string]int
	createResponse        string
	builds                int
	renameReads           int
	pendingName           string
	expectedTeam          string
}

func newAPI(t *testing.T) (*fakeAPI, *httptest.Server) {
	t.Helper()
	api := &fakeAPI{vm: map[string]any{"id": vmID, "name": "test-vm", "vcpu": 2, "ram": 4096, "disk": 40, "boot_mode": "uefi", "firewall_group_id": nil, "main_ipv4": "192.0.2.1", "main_ipv6": "", "template": "Ubuntu", "status": "active"}, statuses: map[string]int{}, createResponse: `{"id":"` + vmID + `","password":"creation-secret"}`, expectedTeam: teamID}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		api.mu.Lock()
		defer api.mu.Unlock()
		api.calls = append(api.calls, r.Method+" "+r.URL.Path)
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Error("missing bearer token")
		}
		w.Header().Set("Content-Type", "application/json")
		if status := api.statuses[r.Method]; status != 0 {
			w.WriteHeader(status)
			fmt.Fprint(w, `{"error":"remote-secret"}`)
			return
		}
		switch {
		case r.Method == "POST" && r.URL.Path == "/vm":
			if err := json.NewDecoder(r.Body).Decode(&api.createBody); err != nil {
				t.Error(err)
			}
			w.WriteHeader(201)
			fmt.Fprint(w, api.createResponse)
		case r.Method == "GET" && r.URL.Path == "/vm/"+vmID:
			if r.URL.Query().Get("team_id") != api.expectedTeam {
				t.Errorf("unexpected team query: %s", r.URL.RawQuery)
			}
			if api.builds > 0 {
				api.builds--
				vm := map[string]any{"id": vmID, "status": "building"}
				_ = json.NewEncoder(w).Encode(vm)
				return
			}
			if api.pendingName != "" {
				if api.renameReads > 0 {
					api.renameReads--
				} else {
					api.vm["name"] = api.pendingName
					api.pendingName = ""
				}
			}
			_ = json.NewEncoder(w).Encode(api.vm)
		case r.Method == "PATCH" && r.URL.Path == "/vm/"+vmID:
			if err := json.NewDecoder(r.Body).Decode(&api.patchBody); err != nil {
				t.Error(err)
			}
			api.pendingName = api.patchBody["name"].(string)
			w.WriteHeader(202)
		case r.Method == "DELETE" && r.URL.Path == "/vm/"+vmID:
			w.WriteHeader(204)
		default:
			t.Errorf("unexpected API call %s %s", r.Method, r.URL)
			w.WriteHeader(500)
		}
	}))
	t.Cleanup(server.Close)
	return api, server
}
func configuredHarness(t *testing.T, endpoint string) *harness {
	t.Helper()
	h := newHarness(t)
	checkRPC(t, nil, h.configure(map[string]any{"api_token": "test-token", "endpoint": endpoint, "team_id": teamID}))
	return h
}

func TestSchema(t *testing.T) {
	h := newHarness(t)
	if len(h.schema.ResourceSchemas) != 1 || len(h.schema.DataSourceSchemas) != 1 {
		t.Fatal("unexpected registration")
	}
	for _, sch := range []*tfprotov6.Schema{h.schema.Provider, h.schema.ResourceSchemas[vmType], h.schema.DataSourceSchemas[vmType]} {
		for _, a := range sch.Block.Attributes {
			if a.Description == "" {
				t.Errorf("missing description: %s", a.Name)
			}
			if (a.Name == "password" || a.Name == "api_token") && !a.Sensitive {
				t.Errorf("%s is not sensitive", a.Name)
			}
		}
	}
	meta, err := h.server.GetMetadata(testCtx, &tfprotov6.GetMetadataRequest{})
	checkRPC(t, err, meta.Diagnostics)
}

func TestProviderConfiguration(t *testing.T) {
	for _, tc := range []struct {
		name                           string
		config                         map[string]any
		envToken, envEndpoint, envTeam string
		wantError                      bool
	}{
		{name: "missing token", config: map[string]any{}, wantError: true},
		{name: "unknown token", config: map[string]any{"api_token": tftypes.UnknownValue}, envToken: "test-token", wantError: true},
		{name: "unknown endpoint", config: map[string]any{"api_token": "test-token", "endpoint": tftypes.UnknownValue}, wantError: true},
		{name: "unknown team", config: map[string]any{"api_token": "test-token", "team_id": tftypes.UnknownValue}, wantError: true},
		{name: "empty token does not fallback", config: map[string]any{"api_token": ""}, envToken: "test-token", wantError: true},
		{name: "whitespace token", config: map[string]any{"api_token": " \n"}, wantError: true},
		{name: "invalid token header", config: map[string]any{"api_token": "test\ntoken"}, wantError: true},
		{name: "bad endpoint", config: map[string]any{"api_token": "test-token", "endpoint": "ftp://example.com"}, wantError: true},
		{name: "credential endpoint", config: map[string]any{"api_token": "test-token", "endpoint": "https://user:secret@example.com"}, wantError: true},
		{name: "empty explicit endpoint", config: map[string]any{"api_token": "test-token", "endpoint": ""}, wantError: true},
		{name: "bad team", config: map[string]any{"api_token": "test-token", "team_id": "bad"}, wantError: true},
		{name: "bad environment team", config: map[string]any{}, envToken: "test-token", envTeam: "bad", wantError: true},
		{name: "default endpoint", config: map[string]any{"api_token": "test-token"}},
		{name: "explicit overrides environment", config: map[string]any{"api_token": "test-token", "endpoint": "http://127.0.0.1", "team_id": teamID}, envToken: "", envEndpoint: "bad", envTeam: "bad"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("ONIDEL_API_TOKEN", tc.envToken)
			t.Setenv("ONIDEL_ENDPOINT", tc.envEndpoint)
			t.Setenv("ONIDEL_TEAM_ID", tc.envTeam)
			h := newHarness(t)
			ds := h.configure(tc.config)
			if hasError(ds) != tc.wantError {
				t.Fatalf("diagnostics = %v, want error %v", ds, tc.wantError)
			}
			for _, d := range ds {
				if strings.Contains(d.Detail, "secret") || strings.Contains(d.Detail, "test-token") {
					t.Fatal("configuration secret leaked")
				}
			}
		})
	}
	t.Run("environment fallback makes authenticated request", func(t *testing.T) {
		_, server := newAPI(t)
		t.Setenv("ONIDEL_API_TOKEN", "test-token")
		t.Setenv("ONIDEL_ENDPOINT", server.URL)
		t.Setenv("ONIDEL_TEAM_ID", teamID)
		h := newHarness(t)
		checkRPC(t, nil, h.configure(map[string]any{}))
		result, err := h.server.ReadDataSource(testCtx, &tfprotov6.ReadDataSourceRequest{TypeName: vmType, Config: dynamic(t, h.schema.DataSourceSchemas[vmType].ValueType(), map[string]any{"id": vmID})})
		checkRPC(t, err, result.Diagnostics)
	})
}

func TestResourceValidation(t *testing.T) {
	h := newHarness(t)
	cases := []struct {
		name      string
		changes   map[string]any
		wantError bool
	}{
		{"valid OS", nil, false},
		{"valid snapshot", map[string]any{"os": nil, "snapshot_id": otherID}, false},
		{"valid ISO", map[string]any{"os": nil, "iso_id": otherID}, false},
		{"no image", map[string]any{"os": nil}, true},
		{"two images", map[string]any{"snapshot_id": otherID}, true},
		{"all images", map[string]any{"snapshot_id": otherID, "iso_id": otherID}, true},
		{"unknown image", map[string]any{"os": tftypes.UnknownValue}, false},
		{"unknown image with known image", map[string]any{"os": tftypes.UnknownValue, "iso_id": otherID}, false},
		{"two known plus unknown", map[string]any{"os": tftypes.UnknownValue, "iso_id": otherID, "snapshot_id": otherID}, true},
		{"zero cpu", map[string]any{"cpu": 0}, true},
		{"negative ram", map[string]any{"ram": -1}, true},
		{"zero disk", map[string]any{"disk": 0}, true},
		{"zero OS", map[string]any{"os": 0}, true},
		{"unknown size", map[string]any{"cpu": tftypes.UnknownValue}, false},
		{"bad billing", map[string]any{"payment_cycle": "weekly"}, true},
		{"bad boot mode", map[string]any{"boot_mode": "legacy"}, true},
		{"bad instance", map[string]any{"instance_type": "bad"}, true},
		{"bad SSH key", map[string]any{"ssh_keys": stringSet("bad")}, true},
		{"bad VPC", map[string]any{"vpcs": stringSet("bad")}, true},
		{"empty name", map[string]any{"name": ""}, true},
		{"bad timeout", map[string]any{"create_timeout": "forever"}, true},
		{"zero timeout", map[string]any{"create_timeout": "0s"}, true},
		{"negative timeout", map[string]any{"create_timeout": "-1m"}, true},
		{"unknown timeout", map[string]any{"create_timeout": tftypes.UnknownValue}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			values := baseConfig()
			for k, v := range tc.changes {
				values[k] = v
			}
			result, err := h.server.ValidateResourceConfig(testCtx, &tfprotov6.ValidateResourceConfigRequest{TypeName: vmType, Config: h.config(values)})
			if err != nil {
				t.Fatal(err)
			}
			if hasError(result.Diagnostics) != tc.wantError {
				t.Fatalf("diagnostics = %v, want error %v", result.Diagnostics, tc.wantError)
			}
		})
	}
}

func TestVMLifecycle(t *testing.T) {
	api, server := newAPI(t)
	api.builds = 1      // Exercise asynchronous polling through the real client.
	api.renameReads = 1 // PATCH returns before GET reflects the rename.
	h := configuredHarness(t, server.URL)
	values := baseConfig()
	values["ssh_keys"] = stringSet(otherID)
	values["vpcs"] = stringSet(otherID)
	values["startup_script_id"] = otherID
	config, created := h.create(values)
	checkRPC(t, nil, created.Diagnostics)
	attrs := decode(t, h.resourceType(), created.NewState)
	assertValue(t, attrs, "id", vmID)
	assertValue(t, attrs, "password", "creation-secret")
	assertValue(t, attrs, "status", "active")
	assertValue(t, attrs, "team_id", teamID)
	assertValue(t, attrs, "ipv6", false)
	assertValue(t, attrs, "disable_ssh_blocking", false)
	assertValue(t, attrs, "create_timeout", "20m")
	api.mu.Lock()
	body := api.createBody
	for key, want := range map[string]any{"name": "test-vm", "cpu": float64(2), "ram": float64(4096), "disk": float64(40), "os": float64(2), "team_id": teamID, "ipv6": false, "disable_ssh_blocking": false, "startup_script_id": otherID} {
		if body[key] != want {
			t.Errorf("POST %s = %v, want %v", key, body[key], want)
		}
	}
	if _, ok := body["boot_mode"]; ok {
		t.Error("unknown computed boot mode sent to API")
	}
	for _, key := range []string{"ssh_keys", "vpcs"} {
		if got := body[key].([]any); len(got) != 1 || got[0] != otherID {
			t.Errorf("bad %s", key)
		}
	}
	api.mu.Unlock()
	// A no-op plan must not replace or introduce perpetual diffs.
	unchanged := h.plan(config, created.NewState)
	if len(unchanged.RequiresReplace) != 0 {
		t.Fatal("unchanged resource requires replacement")
	}
	before, _ := created.NewState.Unmarshal(h.resourceType())
	after, _ := unchanged.PlannedState.Unmarshal(h.resourceType())
	if !before.Equal(after) {
		t.Fatalf("perpetual diff: %v -> %v", before, after)
	}

	// Read detects observable drift but leaves create-only inputs untouched.
	api.mu.Lock()
	api.vm["name"] = "renamed-outside"
	api.vm["vcpu"] = 4
	api.vm["main_ipv4"] = "192.0.2.2"
	api.mu.Unlock()
	read := h.read(created.NewState)
	checkRPC(t, nil, read.Diagnostics)
	drift := decode(t, h.resourceType(), read.NewState)
	assertValue(t, drift, "name", "renamed-outside")
	assertValue(t, drift, "cpu", 4)
	assertValue(t, drift, "main_ipv4", "192.0.2.2")
	for _, key := range []string{"os", "payment_cycle", "instance_type", "location", "ssh_keys", "vpcs", "startup_script_id", "ipv6", "disable_ssh_blocking", "password"} {
		if !drift[key].Equal(attrs[key]) {
			t.Errorf("read fabricated create-only drift for %s", key)
		}
	}
	// Rename only; PATCH must never contain provisioning fields.
	values["name"] = "new-name"
	values["cpu"] = 4
	config = h.config(values)
	plan := h.plan(config, read.NewState)
	if len(plan.RequiresReplace) != 0 {
		t.Fatalf("rename requires replacement: %v", plan.RequiresReplace)
	}
	updated := h.apply(config, read.NewState, plan)
	checkRPC(t, nil, updated.Diagnostics)
	assertConsistent(t, h.resourceType(), plan.PlannedState, updated.NewState)
	api.mu.Lock()
	if len(api.patchBody) != 2 || api.patchBody["name"] != "new-name" || api.patchBody["team_id"] != teamID {
		t.Errorf("unexpected PATCH: %v", api.patchBody)
	}
	api.mu.Unlock()
	deleted := h.destroy(updated.NewState)
	checkRPC(t, nil, deleted.Diagnostics)
	if decode(t, h.resourceType(), deleted.NewState) != nil {
		t.Fatal("delete retained state")
	}
}

func TestReplacementPlans(t *testing.T) {
	_, server := newAPI(t)
	h := configuredHarness(t, server.URL)
	values := baseConfig()
	_, created := h.create(values)
	checkRPC(t, nil, created.Diagnostics)
	changes := map[string]any{"payment_cycle": "monthly", "instance_type": vmID, "location": "Melbourne", "cpu": 4, "ram": 8192, "disk": 80, "os": 3, "snapshot_id": otherID, "iso_id": otherID, "boot_mode": "bios", "team_id": otherID, "ssh_keys": stringSet(otherID), "vpcs": stringSet(otherID), "firewall_group_id": otherID, "ipv6": true, "disable_ssh_blocking": true, "startup_script_id": otherID}
	for key, value := range changes {
		t.Run(key, func(t *testing.T) {
			config := baseConfig()
			config[key] = value
			if key == "snapshot_id" || key == "iso_id" {
				delete(config, "os")
			}
			plan := h.plan(h.config(config), created.NewState)
			if len(plan.RequiresReplace) == 0 {
				t.Errorf("%s change did not require replacement", key)
			}
		})
	}
	t.Run("timeout is operational not provisioning", func(t *testing.T) {
		values["create_timeout"] = "30m"
		config := h.config(values)
		plan := h.plan(config, created.NewState)
		if len(plan.RequiresReplace) != 0 {
			t.Fatalf("timeout change requires replacement: %v", plan.RequiresReplace)
		}
		updated := h.apply(config, created.NewState, plan)
		checkRPC(t, nil, updated.Diagnostics)
		assertConsistent(t, h.resourceType(), plan.PlannedState, updated.NewState)
	})
	t.Run("unknown team does not use provider default", func(t *testing.T) {
		config := baseConfig()
		config["team_id"] = tftypes.UnknownValue
		plan := h.plan(h.config(config), h.nullState())
		if decode(t, h.resourceType(), plan.PlannedState)["team_id"].IsKnown() {
			t.Fatal("unknown team silently defaulted")
		}
	})
	t.Run("provider team change requires replacement", func(t *testing.T) {
		checkRPC(t, nil, h.configure(map[string]any{"api_token": "test-token", "endpoint": server.URL, "team_id": otherID}))
		plan := h.plan(h.config(baseConfig()), created.NewState)
		if len(plan.RequiresReplace) == 0 {
			t.Fatal("provider team change did not replace")
		}
	})
}

func TestCreateFailures(t *testing.T) {
	for _, mode := range []string{"post failure", "missing ID", "terminal status", "timeout", "read failure"} {
		t.Run(mode, func(t *testing.T) {
			api, server := newAPI(t)
			h := configuredHarness(t, server.URL)
			config := baseConfig()
			api.mu.Lock()
			switch mode {
			case "post failure":
				api.statuses["POST"] = 402
			case "missing ID":
				api.createResponse = `{}`
			case "terminal status":
				api.vm["status"] = "suspended"
			case "timeout":
				api.builds = 100
				config["create_timeout"] = "50ms"
			case "read failure":
				api.statuses["GET"] = 403
			}
			api.mu.Unlock()
			_, result := h.create(config)
			if !hasError(result.Diagnostics) {
				t.Fatal("expected create error")
			}
			attrs := decode(t, h.resourceType(), result.NewState)
			if mode == "post failure" || mode == "missing ID" {
				if attrs != nil && !attrs["id"].IsNull() {
					t.Fatal("invented create identity")
				}
			} else {
				assertValue(t, attrs, "id", vmID)
				assertValue(t, attrs, "password", "creation-secret")
				for k, v := range attrs {
					if !v.IsFullyKnown() {
						t.Errorf("partial state %s unknown", k)
					}
				}
			}
			api.mu.Lock()
			defer api.mu.Unlock()
			posts := 0
			for _, c := range api.calls {
				if c == "POST /vm" {
					posts++
				}
			}
			if posts != 1 {
				t.Fatalf("created %d times", posts)
			}
			for _, d := range result.Diagnostics {
				if strings.Contains(d.Detail, "creation-secret") || strings.Contains(d.Detail, "remote-secret") {
					t.Fatal("secret leaked in diagnostics")
				}
			}
		})
	}
}

func TestReadDeleteAndUpdateFailures(t *testing.T) {
	for _, code := range []int{401, 403, 404, 500} {
		t.Run(fmt.Sprint(code), func(t *testing.T) {
			api, server := newAPI(t)
			h := configuredHarness(t, server.URL)
			_, created := h.create(baseConfig())
			checkRPC(t, nil, created.Diagnostics)
			api.mu.Lock()
			api.statuses["GET"] = code
			api.mu.Unlock()
			read := h.read(created.NewState)
			if code == 404 {
				checkRPC(t, nil, read.Diagnostics)
				if decode(t, h.resourceType(), read.NewState) != nil {
					t.Fatal("404 read did not remove state")
				}
			} else {
				if !hasError(read.Diagnostics) {
					t.Fatal("read error swallowed")
				}
				assertValue(t, decode(t, h.resourceType(), read.NewState), "id", vmID)
			}
			api.mu.Lock()
			api.statuses["DELETE"] = code
			api.mu.Unlock()
			deleted := h.destroy(created.NewState)
			if code == 404 {
				checkRPC(t, nil, deleted.Diagnostics)
				if decode(t, h.resourceType(), deleted.NewState) != nil {
					t.Fatal("404 delete did not remove state")
				}
			} else {
				if !hasError(deleted.Diagnostics) {
					t.Fatal("delete error swallowed")
				}
				assertValue(t, decode(t, h.resourceType(), deleted.NewState), "id", vmID)
			}
		})
	}
	for _, failPatch := range []bool{true, false} {
		t.Run(fmt.Sprintf("rename patch failure %v", failPatch), func(t *testing.T) {
			api, server := newAPI(t)
			h := configuredHarness(t, server.URL)
			_, created := h.create(baseConfig())
			checkRPC(t, nil, created.Diagnostics)
			api.mu.Lock()
			if failPatch {
				api.statuses["PATCH"] = 409
			} else {
				api.statuses["GET"] = 403
			}
			api.mu.Unlock()
			values := baseConfig()
			values["name"] = "new-name"
			config := h.config(values)
			plan := h.plan(config, created.NewState)
			result := h.apply(config, created.NewState, plan)
			if !hasError(result.Diagnostics) {
				t.Fatal("update error swallowed")
			}
			want := "test-vm" // A failed confirmation must retain prior state, too.
			attrs := decode(t, h.resourceType(), result.NewState)
			assertValue(t, attrs, "name", want)
			assertValue(t, attrs, "id", vmID)
		})
	}
}

func TestImport(t *testing.T) {
	api, server := newAPI(t)
	h := configuredHarness(t, server.URL)
	for _, id := range []string{"not-a-uuid", "", vmID + "/other"} {
		result, err := h.server.ImportResourceState(testCtx, &tfprotov6.ImportResourceStateRequest{TypeName: vmType, ID: id})
		if err != nil {
			t.Fatal(err)
		}
		if !hasError(result.Diagnostics) {
			t.Errorf("invalid import accepted: %q", id)
		}
	}
	result, err := h.server.ImportResourceState(testCtx, &tfprotov6.ImportResourceStateRequest{TypeName: vmType, ID: vmID})
	checkRPC(t, err, result.Diagnostics)
	if len(result.ImportedResources) != 1 {
		t.Fatal("expected one import")
	}
	state := result.ImportedResources[0].State
	attrs := decode(t, h.resourceType(), state)
	assertValue(t, attrs, "id", vmID)
	assertValue(t, attrs, "team_id", teamID)
	read := h.read(state)
	checkRPC(t, nil, read.Diagnostics)
	attrs = decode(t, h.resourceType(), read.NewState)
	assertValue(t, attrs, "name", "test-vm")
	assertValue(t, attrs, "cpu", 2)
	for _, key := range []string{"os", "payment_cycle", "instance_type", "location", "ipv6"} {
		if !attrs[key].IsNull() {
			t.Errorf("import fabricated %s", key)
		}
	}
	api.mu.Lock()
	defer api.mu.Unlock()
	if len(api.calls) != 1 || !strings.HasPrefix(api.calls[0], "GET ") {
		t.Fatal("import mutated remote VM")
	}
}

func TestDataSource(t *testing.T) {
	for _, explicitTeam := range []bool{false, true} {
		t.Run(fmt.Sprintf("explicit team %v", explicitTeam), func(t *testing.T) {
			api, server := newAPI(t)
			h := configuredHarness(t, server.URL)
			typ := h.schema.DataSourceSchemas[vmType].ValueType()
			values := map[string]any{"id": vmID}
			if explicitTeam {
				values["team_id"] = otherID
				api.mu.Lock()
				api.expectedTeam = otherID
				api.mu.Unlock()
			}
			config := dynamic(t, typ, values)
			result, err := h.server.ReadDataSource(testCtx, &tfprotov6.ReadDataSourceRequest{TypeName: vmType, Config: config})
			checkRPC(t, err, result.Diagnostics)
			attrs := decode(t, typ, result.State)
			assertValue(t, attrs, "id", vmID)
			assertValue(t, attrs, "cpu", 2)
			assertValue(t, attrs, "name", "test-vm")
			assertValue(t, attrs, "main_ipv4", "192.0.2.1")
			assertValue(t, attrs, "firewall_group_id", nil)
			api.mu.Lock()
			api.statuses["GET"] = 404
			api.mu.Unlock()
			missing, err := h.server.ReadDataSource(testCtx, &tfprotov6.ReadDataSourceRequest{TypeName: vmType, Config: config})
			if err != nil {
				t.Fatal(err)
			}
			if !hasError(missing.Diagnostics) {
				t.Fatal("missing data source silently succeeded")
			}
		})
	}
	h := newHarness(t)
	invalid, err := h.server.ValidateDataResourceConfig(testCtx, &tfprotov6.ValidateDataResourceConfigRequest{TypeName: vmType, Config: dynamic(t, h.schema.DataSourceSchemas[vmType].ValueType(), map[string]any{"id": "bad"})})
	if err != nil {
		t.Fatal(err)
	}
	if !hasError(invalid.Diagnostics) {
		t.Fatal("invalid lookup ID accepted")
	}
}
