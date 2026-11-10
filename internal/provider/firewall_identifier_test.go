package provider

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
)

func TestNumericFirewallRetainsUUIDAndDetectsDrift(t *testing.T) {
	api, server := newAPI(t)
	api.vm["firewall_group_id"] = 1089
	api.createResponse = `{"instance_id":"` + vmID + `"}`
	h := configuredHarness(t, server.URL)
	values := baseConfig()
	values["firewall_group_id"] = otherID
	config, created := h.create(values)
	checkRPC(t, nil, created.Diagnostics)
	attrs := decode(t, h.resourceType(), created.NewState)
	assertValue(t, attrs, "firewall_group_id", otherID)
	assertValue(t, attrs, "firewall_group_internal_id", int64(1089))

	read := h.read(created.NewState)
	checkRPC(t, nil, read.Diagnostics)
	plan := h.plan(config, read.NewState)
	if len(plan.RequiresReplace) != 0 {
		t.Fatal("a stable numeric firewall identifier must not replace the VM")
	}
	assertConsistent(t, h.resourceType(), read.NewState, plan.PlannedState)

	api.mu.Lock()
	api.vm["firewall_group_id"] = 1090
	api.mu.Unlock()
	drift := h.read(read.NewState)
	if !hasError(drift.Diagnostics) {
		t.Fatal("numeric firewall drift must not silently retain the configured UUID")
	}
	assertConsistent(t, h.resourceType(), read.NewState, drift.NewState)

	api.mu.Lock()
	api.vm["firewall_group_id"] = nil
	api.mu.Unlock()
	detached := h.read(read.NewState)
	checkRPC(t, nil, detached.Diagnostics)
	attrs = decode(t, h.resourceType(), detached.NewState)
	assertValue(t, attrs, "firewall_group_id", nil)
	assertValue(t, attrs, "firewall_group_internal_id", nil)
}

func TestNumericFirewallImportDoesNotInventUUID(t *testing.T) {
	api, server := newAPI(t)
	api.vm["firewall_group_id"] = 1089
	h := configuredHarness(t, server.URL)
	imported, err := h.server.ImportResourceState(testCtx, &tfprotov6.ImportResourceStateRequest{TypeName: vmType, ID: vmID})
	checkRPC(t, err, imported.Diagnostics)
	read := h.read(imported.ImportedResources[0].State)
	checkRPC(t, nil, read.Diagnostics)
	attrs := decode(t, h.resourceType(), read.NewState)
	assertValue(t, attrs, "firewall_group_id", nil)
	assertValue(t, attrs, "firewall_group_internal_id", int64(1089))
	api.mu.Lock()
	defer api.mu.Unlock()
	for _, call := range api.calls {
		if call != "GET /vm/"+vmID {
			t.Fatalf("import performed an unexpected API write: %s", call)
		}
	}
}
