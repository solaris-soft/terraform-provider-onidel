# Implementation contract

Module: github.com/solaris-soft/terraform-provider-onidel. Go 1.24+, Terraform Plugin Framework v1.15.1 (modern equivalent of lifecycle guidance in HashiCorp blog).

Sources: https://developers.onidel.com/api-reference/virtual-machine/provision-a-new-vm and https://www.hashicorp.com/en/blog/writing-custom-terraform-providers . Local reference spec /tmp/onidel-openapi.yaml; blog /tmp/hashicorp-provider.md; auth /tmp/onidel-auth.md. Agents can read those absolute paths. Do not commit downloaded docs.

## Client (internal/client)

Implement exported interface concretely as *Client:
- New(baseURL, token string) (*Client, error); DefaultBaseURL constant
- CreateVM(ctx context.Context, input NewVM) (*VM, error)
- GetVM(ctx context.Context, id, teamID string) (*VM, error)
- UpdateVM(ctx context.Context, id string, input UpdateVM) error
- DeleteVM(ctx context.Context, id string) error
- IsNotFound(err error) bool
- WaitVM(ctx context.Context, id, teamID string) (*VM, error): poll GET while building/restoring, return active, error terminal failures. Respect context.

NewVM fields: Name, PaymentCycle, InstanceType, Location strings; CPU,RAM,Disk int64; OS *int64; SnapshotID,ISOID,BootMode,TeamID strings; SSHKeys,VPCs []string; FirewallGroupID string; IPv6,DisableSSHBlocking bool; StartupScriptID string. Exact snake_case API JSON per source (CPU cpu, OS os).
VM fields: ID,Name,Password,MainIPv4,MainIPv6,Template,BootMode,Status strings; VCPU,RAM,Disk int64; FirewallGroupID *string. Use exact API JSON tags. UpdateVM fields: Name string, TeamID string (only rename initially).

IMPORTANT: spec does NOT document POST /vm 201 response body or Location header. Safely accept direct VM object or {id: UUID}; optionally Location; error explicitly if no ID, never invent an ID or retry POST. Never identify by name or list heuristic. Document limitation.

## Provider (internal/provider and main.go)

Use above client contract. Provider New(version string) func() provider.Provider. Provider api_token sensitive env ONIDEL_API_TOKEN and endpoint env ONIDEL_ENDPOINT, optional team_id env ONIDEL_TEAM_ID. Resource onidel_virtual_machine; VM data source onidel_virtual_machine lookup by id and team_id. Expose provisioning fields and computed API fields with clear descriptions. Name is mutable via PATCH; other inputs replacement-only initially. Enforce exactly one os/snapshot_id/iso_id, positive CPU/RAM/disk, billing enum. Optional bool false defaults. Retain create-only inputs on refresh; only map known API outputs (no fabricated drift). Import UUID support, provider team default handling, context timeout for async create, partial state ID before wait, not found removes resource. Tests with httptest no paid calls. Register plugin address registry.terraform.io/solaris-soft/onidel. Provider agent owns main.go, internal/provider, go.mod/go.sum. Client agent owns internal/client only. Each commit on its branch; do not merge other branches.
