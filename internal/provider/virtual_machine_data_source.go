package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/solaris-soft/terraform-provider-onidel/internal/client"
)

type virtualMachineDataSource struct{ data *providerData }

var _ datasource.DataSourceWithConfigure = (*virtualMachineDataSource)(nil)

type vmDataSourceModel struct {
	ID                      types.String `tfsdk:"id"`
	TeamID                  types.String `tfsdk:"team_id"`
	Name                    types.String `tfsdk:"name"`
	CPU                     types.Int64  `tfsdk:"cpu"`
	RAM                     types.Int64  `tfsdk:"ram"`
	Disk                    types.Int64  `tfsdk:"disk"`
	Password                types.String `tfsdk:"password"`
	MainIPv4                types.String `tfsdk:"main_ipv4"`
	MainIPv6                types.String `tfsdk:"main_ipv6"`
	Template                types.String `tfsdk:"template"`
	BootMode                types.String `tfsdk:"boot_mode"`
	FirewallGroupID         types.String `tfsdk:"firewall_group_id"`
	FirewallGroupInternalID types.Int64  `tfsdk:"firewall_group_internal_id"`
	Status                  types.String `tfsdk:"status"`
}

func NewVirtualMachineDataSource() datasource.DataSource { return &virtualMachineDataSource{} }
func (d *virtualMachineDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_virtual_machine"
}
func (d *virtualMachineDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{Description: "Read an existing Onidel VM by UUID. Only fields returned by the API are exposed.", Attributes: map[string]schema.Attribute{
		"id":                         schema.StringAttribute{Required: true, Description: "VM UUID.", Validators: uuidValidators()},
		"team_id":                    schema.StringAttribute{Optional: true, Description: "Team UUID. Defaults to the provider team or API account default.", Validators: uuidValidators()},
		"name":                       schema.StringAttribute{Computed: true, Description: "VM name."},
		"cpu":                        schema.Int64Attribute{Computed: true, Description: "Number of virtual CPUs."},
		"ram":                        schema.Int64Attribute{Computed: true, Description: "RAM in MB."},
		"disk":                       schema.Int64Attribute{Computed: true, Description: "Disk size in GB."},
		"password":                   schema.StringAttribute{Computed: true, Sensitive: true, Description: "Administrator password, if returned by the API. Stored in Terraform state."},
		"main_ipv4":                  schema.StringAttribute{Computed: true, Description: "Primary IPv4 address."},
		"main_ipv6":                  schema.StringAttribute{Computed: true, Description: "Primary IPv6 address."},
		"template":                   schema.StringAttribute{Computed: true, Description: "OS template display name."},
		"boot_mode":                  schema.StringAttribute{Computed: true, Description: "Firmware mode."},
		"firewall_group_id":          schema.StringAttribute{Computed: true, Description: "Attached firewall UUID when reported; null for a detached firewall or when only its numeric internal ID is available."},
		"firewall_group_internal_id": schema.Int64Attribute{Computed: true, Description: "Numeric firewall database ID when returned instead of a public UUID."},
		"status":                     schema.StringAttribute{Computed: true, Description: "Current API status."},
	}}
}
func (d *virtualMachineDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	var ok bool
	d.data, ok = req.ProviderData.(*providerData)
	if !ok {
		resp.Diagnostics.AddError("Invalid provider data", "Expected Onidel provider configuration.")
	}
}
func (d *virtualMachineDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var data vmDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	vm, err := d.data.client.GetVM(ctx, data.ID.ValueString(), effectiveTeam(data.TeamID, d.data.teamID))
	if err != nil {
		if client.IsNotFound(err) {
			resp.Diagnostics.AddError("VM not found", "No VM exists with the requested ID in this team.")
		} else {
			resp.Diagnostics.AddError("Unable to read VM", err.Error())
		}
		return
	}
	data.Name = types.StringValue(vm.Name)
	data.CPU = types.Int64Value(vm.VCPU)
	data.RAM = types.Int64Value(vm.RAM)
	data.Disk = types.Int64Value(vm.Disk)
	data.Password = types.StringValue(vm.Password)
	data.MainIPv4 = types.StringValue(vm.MainIPv4)
	data.MainIPv6 = types.StringValue(vm.MainIPv6)
	data.Template = types.StringValue(vm.Template)
	data.BootMode = types.StringValue(vm.BootMode)
	data.Status = types.StringValue(vm.Status)
	data.FirewallGroupID = types.StringPointerValue(vm.FirewallGroupID)
	data.FirewallGroupInternalID = types.Int64PointerValue(vm.FirewallGroupInternalID)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}
