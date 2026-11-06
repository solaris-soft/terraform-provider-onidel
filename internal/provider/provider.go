// Package provider implements the Onidel Terraform provider.
package provider

import (
	"context"
	"os"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/solaris-soft/terraform-provider-onidel/internal/client"
)

type onidelProvider struct{ version string }
type providerModel struct {
	APIToken types.String `tfsdk:"api_token"`
	Endpoint types.String `tfsdk:"endpoint"`
	TeamID   types.String `tfsdk:"team_id"`
}
type providerData struct {
	client *client.Client
	teamID string
}

func New(version string) func() provider.Provider {
	return func() provider.Provider { return &onidelProvider{version: version} }
}
func (p *onidelProvider) Metadata(_ context.Context, _ provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "onidel"
	resp.Version = p.version
}
func (p *onidelProvider) Schema(_ context.Context, _ provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{Description: "Manage Onidel virtual machines.", Attributes: map[string]schema.Attribute{
		"api_token": schema.StringAttribute{Optional: true, Sensitive: true, Description: "API token. Defaults to ONIDEL_API_TOKEN. Treat Terraform state as sensitive."},
		"endpoint":  schema.StringAttribute{Optional: true, Description: "API base URL. Defaults to ONIDEL_ENDPOINT, then the public Onidel API."},
		"team_id":   schema.StringAttribute{Optional: true, Description: "Default team UUID. Defaults to ONIDEL_TEAM_ID, then the API account's default team."},
	}}
}
func (p *onidelProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	var data providerModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	values := map[string]types.String{"api_token": data.APIToken, "endpoint": data.Endpoint, "team_id": data.TeamID}
	resolved := make(map[string]string)
	for key, value := range values {
		if value.IsUnknown() {
			resp.Diagnostics.AddAttributeError(path.Root(key), "Unknown provider configuration", "This value must be known before the provider can be configured.")
			continue
		}
		if value.IsNull() {
			resolved[key] = os.Getenv("ONIDEL_" + strings.ToUpper(key))
		} else {
			resolved[key] = value.ValueString()
		}
	}
	if resp.Diagnostics.HasError() {
		return
	}
	if strings.TrimSpace(resolved["api_token"]) == "" {
		resp.Diagnostics.AddAttributeError(path.Root("api_token"), "Missing API token", "Set api_token or ONIDEL_API_TOKEN.")
	}
	for _, ch := range resolved["api_token"] {
		if ch <= ' ' || ch > '~' {
			resp.Diagnostics.AddAttributeError(path.Root("api_token"), "Invalid API token", "API tokens must contain only visible ASCII characters, without whitespace.")
			break
		}
	}
	if team := resolved["team_id"]; team != "" && !uuidPattern.MatchString(team) {
		resp.Diagnostics.AddAttributeError(path.Root("team_id"), "Invalid team UUID", "Set team_id or ONIDEL_TEAM_ID to a UUID.")
	}
	if resp.Diagnostics.HasError() {
		return
	}
	endpoint := resolved["endpoint"]
	if endpoint == "" {
		if !data.Endpoint.IsNull() {
			resp.Diagnostics.AddAttributeError(path.Root("endpoint"), "Invalid endpoint", "Endpoint must be an absolute HTTP or HTTPS URL.")
			return
		}
		endpoint = client.DefaultBaseURL
	}
	c, err := client.New(endpoint, resolved["api_token"])
	if err != nil {
		// Do not echo configuration values: URLs can contain credentials.
		resp.Diagnostics.AddAttributeError(path.Root("endpoint"), "Invalid endpoint", "Endpoint must be an absolute HTTP or HTTPS URL without credentials, query, or fragment.")
		return
	}
	configured := &providerData{client: c, teamID: resolved["team_id"]}
	resp.ResourceData = configured
	resp.DataSourceData = configured
}
func (p *onidelProvider) Resources(context.Context) []func() resource.Resource {
	return []func() resource.Resource{NewVirtualMachineResource}
}
func (p *onidelProvider) DataSources(context.Context) []func() datasource.DataSource {
	return []func() datasource.DataSource{NewVirtualMachineDataSource}
}

func effectiveTeam(team types.String, fallback string) string {
	if team.IsNull() {
		return fallback
	}
	return team.ValueString()
}
