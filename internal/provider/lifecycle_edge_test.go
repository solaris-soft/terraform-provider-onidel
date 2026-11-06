package provider

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
)

func TestRenameTimeoutRetainsPriorState(t *testing.T) {
	api, server := newAPI(t)
	h := configuredHarness(t, server.URL)
	_, created := h.create(baseConfig())
	checkRPC(t, nil, created.Diagnostics)
	api.mu.Lock()
	api.renameReads = 100
	api.mu.Unlock()
	values := baseConfig()
	values["name"] = "eventual-name"
	values["update_timeout"] = "50ms"
	config := h.config(values)
	result := h.apply(config, created.NewState, h.plan(config, created.NewState))
	if !hasError(result.Diagnostics) {
		t.Fatal("rename timeout was swallowed")
	}
	prior, _ := created.NewState.Unmarshal(h.resourceType())
	after, _ := result.NewState.Unmarshal(h.resourceType())
	if !prior.Equal(after) {
		t.Fatal("unconfirmed rename changed prior state")
	}
	api.mu.Lock()
	defer api.mu.Unlock()
	patches := 0
	for _, call := range api.calls {
		if call == "PATCH /vm/"+vmID {
			patches++
		}
	}
	if patches != 1 {
		t.Fatalf("PATCH was sent %d times", patches)
	}
}

func TestProvisioningImageSourcesAndOverrides(t *testing.T) {
	for _, source := range []string{"os", "snapshot_id", "iso_id"} {
		t.Run(source, func(t *testing.T) {
			api, server := newAPI(t)
			h := configuredHarness(t, server.URL)
			values := baseConfig()
			if source != "os" {
				delete(values, "os")
				values[source] = otherID
			}
			values["team_id"] = otherID
			values["boot_mode"] = "bios"
			values["firewall_group_id"] = otherID
			values["ipv6"] = true
			values["disable_ssh_blocking"] = true
			api.mu.Lock()
			api.expectedTeam = otherID
			api.vm["boot_mode"] = "bios"
			api.vm["firewall_group_id"] = otherID
			api.vm["password"] = "read-secret"
			api.mu.Unlock()
			_, created := h.create(values)
			checkRPC(t, nil, created.Diagnostics)
			attrs := decode(t, h.resourceType(), created.NewState)
			assertValue(t, attrs, "password", "read-secret")
			assertValue(t, attrs, "firewall_group_id", otherID)
			api.mu.Lock()
			defer api.mu.Unlock()
			for _, key := range []string{"os", "snapshot_id", "iso_id"} {
				_, present := api.createBody[key]
				if present != (key == source) {
					t.Errorf("unexpected image selector %s: %v", key, api.createBody)
				}
			}
			for key, want := range map[string]any{"team_id": otherID, "boot_mode": "bios", "firewall_group_id": otherID, "ipv6": true, "disable_ssh_blocking": true} {
				if api.createBody[key] != want {
					t.Errorf("%s not forwarded", key)
				}
			}
		})
	}
}

func TestAccountDefaultTeamAndNullImportInputs(t *testing.T) {
	t.Setenv("ONIDEL_TEAM_ID", "")
	api, server := newAPI(t)
	api.mu.Lock()
	api.expectedTeam = ""
	api.mu.Unlock()
	h := newHarness(t)
	checkRPC(t, nil, h.configure(map[string]any{"api_token": "test-token", "endpoint": server.URL}))
	_, created := h.create(baseConfig())
	checkRPC(t, nil, created.Diagnostics)
	assertValue(t, decode(t, h.resourceType(), created.NewState), "team_id", "")
	imported, err := h.server.ImportResourceState(testCtx, &tfprotov6.ImportResourceStateRequest{TypeName: vmType, ID: vmID})
	checkRPC(t, err, imported.Diagnostics)
	// Every create-only input is null after import. Refresh must decode those
	// into Framework types, not attempt to convert them into native Go scalars.
	refreshed := h.read(imported.ImportedResources[0].State)
	checkRPC(t, nil, refreshed.Diagnostics)
	assertValue(t, decode(t, h.resourceType(), refreshed.NewState), "team_id", "")
}
