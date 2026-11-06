package provider

import (
	"context"
	"regexp"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/setvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/setplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/solaris-soft/terraform-provider-onidel/internal/client"
)

var uuidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

func uuidValidators() []validator.String {
	return []validator.String{stringvalidator.RegexMatches(uuidPattern, "must be a UUID")}
}

type virtualMachineResource struct{ data *providerData }

var (
	_ resource.ResourceWithConfigure      = (*virtualMachineResource)(nil)
	_ resource.ResourceWithImportState    = (*virtualMachineResource)(nil)
	_ resource.ResourceWithModifyPlan     = (*virtualMachineResource)(nil)
	_ resource.ResourceWithValidateConfig = (*virtualMachineResource)(nil)
)

type vmModel struct {
	ID                 types.String `tfsdk:"id"`
	Name               types.String `tfsdk:"name"`
	PaymentCycle       types.String `tfsdk:"payment_cycle"`
	InstanceType       types.String `tfsdk:"instance_type"`
	Location           types.String `tfsdk:"location"`
	CPU                types.Int64  `tfsdk:"cpu"`
	RAM                types.Int64  `tfsdk:"ram"`
	Disk               types.Int64  `tfsdk:"disk"`
	OS                 types.Int64  `tfsdk:"os"`
	SnapshotID         types.String `tfsdk:"snapshot_id"`
	ISOID              types.String `tfsdk:"iso_id"`
	BootMode           types.String `tfsdk:"boot_mode"`
	TeamID             types.String `tfsdk:"team_id"`
	SSHKeys            types.Set    `tfsdk:"ssh_keys"`
	VPCs               types.Set    `tfsdk:"vpcs"`
	FirewallGroupID    types.String `tfsdk:"firewall_group_id"`
	IPv6               types.Bool   `tfsdk:"ipv6"`
	DisableSSHBlocking types.Bool   `tfsdk:"disable_ssh_blocking"`
	StartupScriptID    types.String `tfsdk:"startup_script_id"`
	Password           types.String `tfsdk:"password"`
	MainIPv4           types.String `tfsdk:"main_ipv4"`
	MainIPv6           types.String `tfsdk:"main_ipv6"`
	Template           types.String `tfsdk:"template"`
	Status             types.String `tfsdk:"status"`
	CreateTimeout      types.String `tfsdk:"create_timeout"`
	UpdateTimeout      types.String `tfsdk:"update_timeout"`
}

func NewVirtualMachineResource() resource.Resource { return &virtualMachineResource{} }
func (r *virtualMachineResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_virtual_machine"
}
func (r *virtualMachineResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	replaceString := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	// Unconfigured computed values become unknown on any update. They must not
	// turn a rename into replacement merely because the API owns their defaults.
	replaceConfigured := []planmodifier.String{stringplanmodifier.RequiresReplaceIfConfigured()}
	positive := func(description string) schema.Int64Attribute {
		return schema.Int64Attribute{Required: true, Description: description, Validators: []validator.Int64{int64validator.AtLeast(1)}, PlanModifiers: []planmodifier.Int64{int64planmodifier.RequiresReplace()}}
	}
	optionalUUID := func(description string) schema.StringAttribute {
		return schema.StringAttribute{Optional: true, Description: description, Validators: uuidValidators(), PlanModifiers: replaceString}
	}
	uuidSet := func(description string) schema.SetAttribute {
		return schema.SetAttribute{Optional: true, ElementType: types.StringType, Description: description, Validators: []validator.Set{setvalidator.ValueStringsAre(uuidValidators()...)}, PlanModifiers: []planmodifier.Set{setplanmodifier.RequiresReplace()}}
	}
	resp.Schema = schema.Schema{Description: "An Onidel VM. Only name can be changed in place; provisioning changes replace the VM. Creation waits for active status. The API must return an ID on create (the published spec does not guarantee a response body); an unidentified create is never retried automatically. Import using the VM UUID; create-only fields cannot be reconstructed by the API and must be reconciled in configuration.", Attributes: map[string]schema.Attribute{
		"id":            schema.StringAttribute{Computed: true, Description: "VM UUID.", PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
		"name":          schema.StringAttribute{Required: true, Description: "VM name. Mutable by rename.", Validators: []validator.String{stringvalidator.LengthAtLeast(1)}},
		"payment_cycle": schema.StringAttribute{Required: true, Description: "Billing cycle.", Validators: []validator.String{stringvalidator.OneOf("hourly", "monthly", "quarterly", "semiannually", "annually", "biennially", "triennially")}, PlanModifiers: replaceString},
		"instance_type": schema.StringAttribute{Required: true, Description: "Instance type UUID.", Validators: uuidValidators(), PlanModifiers: replaceString},
		"location":      schema.StringAttribute{Required: true, Description: "Deployment location.", Validators: []validator.String{stringvalidator.LengthAtLeast(1)}, PlanModifiers: replaceString},
		"cpu":           positive("Number of virtual CPUs."), "ram": positive("RAM in MB."), "disk": positive("Disk size in GB."),
		"os":                   schema.Int64Attribute{Optional: true, Description: "OS template ID. Exactly one of os, snapshot_id, iso_id is required.", Validators: []validator.Int64{int64validator.AtLeast(1)}, PlanModifiers: []planmodifier.Int64{int64planmodifier.RequiresReplace()}},
		"snapshot_id":          optionalUUID("Snapshot UUID to restore. Mutually exclusive with os and iso_id."),
		"iso_id":               optionalUUID("ISO UUID to boot. Mutually exclusive with os and snapshot_id."),
		"boot_mode":            schema.StringAttribute{Optional: true, Computed: true, Description: "Firmware mode: bios or uefi. Omit to use the API default.", Validators: []validator.String{stringvalidator.OneOf("bios", "uefi")}, PlanModifiers: replaceConfigured},
		"team_id":              schema.StringAttribute{Optional: true, Computed: true, Description: "Team UUID. Defaults to the provider team, or empty for the account default.", Validators: uuidValidators(), PlanModifiers: replaceConfigured},
		"ssh_keys":             uuidSet("SSH key UUIDs installed during provisioning. Retained on refresh."),
		"vpcs":                 uuidSet("VPC UUIDs attached during provisioning. Retained on refresh."),
		"firewall_group_id":    schema.StringAttribute{Optional: true, Computed: true, Description: "Attached firewall group UUID, or null when detached.", Validators: uuidValidators(), PlanModifiers: replaceConfigured},
		"ipv6":                 schema.BoolAttribute{Optional: true, Computed: true, Default: booldefault.StaticBool(false), Description: "Enable IPv6 at provisioning. Retained on refresh.", PlanModifiers: []planmodifier.Bool{boolplanmodifier.RequiresReplace()}},
		"disable_ssh_blocking": schema.BoolAttribute{Optional: true, Computed: true, Default: booldefault.StaticBool(false), Description: "Allow outbound SSH at provisioning. Retained on refresh.", PlanModifiers: []planmodifier.Bool{boolplanmodifier.RequiresReplace()}},
		"startup_script_id":    optionalUUID("Startup script UUID executed on first boot."),
		"password":             schema.StringAttribute{Computed: true, Sensitive: true, Description: "Generated administrator password. Stored in Terraform state; protect the state backend."},
		"main_ipv4":            schema.StringAttribute{Computed: true, Description: "Primary IPv4 address."},
		"main_ipv6":            schema.StringAttribute{Computed: true, Description: "Primary IPv6 address."},
		"template":             schema.StringAttribute{Computed: true, Description: "OS template display name (not the provisioning OS ID)."},
		"status":               schema.StringAttribute{Computed: true, Description: "Current API status."},
		"create_timeout":       schema.StringAttribute{Optional: true, Computed: true, Default: stringdefault.StaticString("20m"), Description: "Maximum duration for provisioning and waiting for active, e.g. 20m. Changing this does not replace the VM.", Validators: []validator.String{durationValidator{}}},
		"update_timeout":       schema.StringAttribute{Optional: true, Computed: true, Default: stringdefault.StaticString("5m"), Description: "Maximum duration for renaming and waiting for the API to report the new name. Changing this does not replace the VM.", Validators: []validator.String{durationValidator{}}},
	}}
}
func (r *virtualMachineResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	var ok bool
	r.data, ok = req.ProviderData.(*providerData)
	if !ok {
		resp.Diagnostics.AddError("Invalid provider data", "Expected Onidel provider configuration.")
	}
}
func (r *virtualMachineResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var data vmModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	count, unknown := 0, false
	for _, v := range []attr.Value{data.OS, data.SnapshotID, data.ISOID} {
		if v.IsUnknown() {
			unknown = true
		} else if !v.IsNull() {
			count++
		}
	}
	if count > 1 || (!unknown && count != 1) {
		resp.Diagnostics.AddAttributeError(path.Root("os"), "Invalid image selection", "Exactly one of os, snapshot_id, or iso_id must be configured.")
	}
}
func (r *virtualMachineResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	if req.Plan.Raw.IsNull() || r.data == nil {
		return
	}
	var configTeam, priorTeam types.String
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root("team_id"), &configTeam)...)
	if !configTeam.IsNull() {
		return
	} // Unknown expressions must not silently use a default.
	team := types.StringValue(r.data.teamID)
	resp.Diagnostics.Append(resp.Plan.SetAttribute(ctx, path.Root("team_id"), team)...)
	if !req.State.Raw.IsNull() {
		resp.Diagnostics.Append(req.State.GetAttribute(ctx, path.Root("team_id"), &priorTeam)...)
		if !priorTeam.Equal(team) {
			resp.RequiresReplace = append(resp.RequiresReplace, path.Root("team_id"))
		}
	}
}

func (r *virtualMachineResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data vmModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	timeout, err := time.ParseDuration(data.CreateTimeout.ValueString())
	if err != nil || timeout <= 0 {
		resp.Diagnostics.AddError("Invalid create timeout", "create_timeout must be a positive Go duration.")
		return
	}
	input := client.NewVM{Name: data.Name.ValueString(), PaymentCycle: data.PaymentCycle.ValueString(), InstanceType: data.InstanceType.ValueString(), Location: data.Location.ValueString(), CPU: data.CPU.ValueInt64(), RAM: data.RAM.ValueInt64(), Disk: data.Disk.ValueInt64(), SnapshotID: data.SnapshotID.ValueString(), ISOID: data.ISOID.ValueString(), BootMode: data.BootMode.ValueString(), TeamID: effectiveTeam(data.TeamID, r.data.teamID), FirewallGroupID: data.FirewallGroupID.ValueString(), IPv6: data.IPv6.ValueBool(), DisableSSHBlocking: data.DisableSSHBlocking.ValueBool(), StartupScriptID: data.StartupScriptID.ValueString()}
	if !data.OS.IsNull() && !data.OS.IsUnknown() {
		os := data.OS.ValueInt64()
		input.OS = &os
	}
	resp.Diagnostics.Append(data.SSHKeys.ElementsAs(ctx, &input.SSHKeys, false)...)
	resp.Diagnostics.Append(data.VPCs.ElementsAs(ctx, &input.VPCs, false)...)
	if resp.Diagnostics.HasError() {
		return
	}
	createCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	vm, err := r.data.client.CreateVM(createCtx, input)
	if err != nil {
		resp.Diagnostics.AddError("Unable to create VM", err.Error())
		return
	}
	// Persist identity before polling. A timeout must not orphan a billable VM.
	data.ID = types.StringValue(vm.ID)
	data.clearUnknownOutputs()
	if vm.Password != "" {
		data.Password = types.StringValue(vm.Password)
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	vm, err = r.data.client.WaitVM(createCtx, vm.ID, input.TeamID)
	if err != nil {
		resp.Diagnostics.AddError("VM created but provisioning did not complete", "The VM ID has been saved in state. Inspect the VM before retrying; replacement may incur charges. "+err.Error())
		return
	}
	data.refresh(vm)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}
func (r *virtualMachineResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data vmModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	vm, err := r.data.client.GetVM(ctx, data.ID.ValueString(), effectiveTeam(data.TeamID, r.data.teamID))
	if client.IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Unable to read VM", err.Error())
		return
	}
	data.refresh(vm)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}
func (r *virtualMachineResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var data, prior vmModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &prior)...)
	if resp.Diagnostics.HasError() {
		return
	}
	timeout, err := time.ParseDuration(data.UpdateTimeout.ValueString())
	if err != nil || timeout <= 0 {
		resp.Diagnostics.AddError("Invalid update timeout", "update_timeout must be a positive Go duration.")
		return
	}
	updateCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	if !data.Name.Equal(prior.Name) {
		if err := r.data.client.UpdateVM(updateCtx, prior.ID.ValueString(), client.UpdateVM{Name: data.Name.ValueString(), TeamID: effectiveTeam(data.TeamID, r.data.teamID)}); err != nil {
			resp.Diagnostics.AddError("Unable to rename VM", err.Error())
			return
		}
	}
	// PATCH accepts asynchronously (202). Never publish the planned name until
	// the read API confirms it. On any failure the Framework retains prior state.
	vm, err := r.waitForName(updateCtx, prior.ID.ValueString(), effectiveTeam(prior.TeamID, r.data.teamID), data.Name.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Unable to confirm VM rename", "Prior state has been retained. Refresh to reconcile the remote VM before retrying. "+err.Error())
		return
	}
	prior.CreateTimeout = data.CreateTimeout
	prior.UpdateTimeout = data.UpdateTimeout
	prior.refresh(vm)
	resp.Diagnostics.Append(resp.State.Set(ctx, &prior)...)
}
func (r *virtualMachineResource) waitForName(ctx context.Context, id, teamID, name string) (*client.VM, error) {
	for {
		vm, err := r.data.client.GetVM(ctx, id, teamID)
		if err != nil {
			return nil, err
		}
		if vm.Name == name {
			return vm, nil
		}
		timer := time.NewTimer(2 * time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
}

func (r *virtualMachineResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data vmModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.data.client.DeleteVM(ctx, data.ID.ValueString()); err != nil && !client.IsNotFound(err) {
		resp.Diagnostics.AddError("Unable to delete VM", err.Error())
		return
	}
	resp.State.RemoveResource(ctx)
}
func (r *virtualMachineResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	if !uuidPattern.MatchString(req.ID) {
		resp.Diagnostics.AddError("Invalid import ID", "Import using a VM UUID.")
		return
	}
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
	if r.data != nil {
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("team_id"), r.data.teamID)...)
	}
}

func (m *vmModel) clearUnknownOutputs() {
	for _, v := range []*types.String{&m.Password, &m.MainIPv4, &m.MainIPv6, &m.Template, &m.Status, &m.BootMode, &m.FirewallGroupID} {
		if v.IsUnknown() {
			*v = types.StringNull()
		}
	}
}
func (m *vmModel) refresh(vm *client.VM) {
	// Do not guess unavailable create-only fields (OS ID, billing cycle, location,
	// instance type, networking inputs, scripts). Only map documented API outputs.
	if vm.Name != "" {
		m.Name = types.StringValue(vm.Name)
	}
	if vm.VCPU > 0 {
		m.CPU = types.Int64Value(vm.VCPU)
	}
	if vm.RAM > 0 {
		m.RAM = types.Int64Value(vm.RAM)
	}
	if vm.Disk > 0 {
		m.Disk = types.Int64Value(vm.Disk)
	}
	if vm.BootMode != "" {
		m.BootMode = types.StringValue(vm.BootMode)
	}
	m.FirewallGroupID = types.StringPointerValue(vm.FirewallGroupID)
	if vm.Password != "" {
		m.Password = types.StringValue(vm.Password)
	}
	m.MainIPv4 = types.StringValue(vm.MainIPv4)
	m.MainIPv6 = types.StringValue(vm.MainIPv6)
	m.Template = types.StringValue(vm.Template)
	m.Status = types.StringValue(vm.Status)
}

type durationValidator struct{}

func (durationValidator) Description(context.Context) string {
	return "must be a positive duration, such as 20m"
}
func (v durationValidator) MarkdownDescription(ctx context.Context) string { return v.Description(ctx) }
func (durationValidator) ValidateString(_ context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	d, err := time.ParseDuration(req.ConfigValue.ValueString())
	if err != nil || d <= 0 {
		resp.Diagnostics.AddAttributeError(req.Path, "Invalid duration", "Expected a positive duration, such as 20m.")
	}
}
