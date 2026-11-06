---
page_title: "onidel_virtual_machine Resource"
description: "Provision and manage an Onidel virtual machine."
---

# onidel_virtual_machine (Resource)

Creates a billable Onidel VM, waits for it to become active, refreshes reported properties, renames it, and destroys it. All provisioning inputs except `name` require replacement when changed.

```hcl
resource "onidel_virtual_machine" "example" {
  name          = "example"
  payment_cycle = "hourly"
  instance_type = "your-instance-type-uuid"
  location      = "Sydney"
  cpu           = 2
  ram           = 4096
  disk          = 40
  os            = 2
}
```

## Provisioning arguments

| Attribute | Description |
| --- | --- |
| `name` | Required VM name; can be updated in place. |
| `payment_cycle` | Billing cycle: `hourly`, `monthly`, `quarterly`, `semiannually`, `annually`, `biennially`, or `triennially`. |
| `instance_type` | Required instance type UUID. |
| `location` | Required location supported by the chosen type, e.g. `Sydney`. |
| `cpu` | Required positive integer vCPU count. |
| `ram` | Required positive integer memory size (MiB). |
| `disk` | Required positive integer disk size (GiB). |
| `os` | OS template integer ID. Exactly one of `os`, `snapshot_id`, or `iso_id` is required. |
| `snapshot_id` | Existing snapshot UUID to deploy. |
| `iso_id` | Existing custom or system ISO UUID to boot. |
| `boot_mode` | Optional `bios` or `uefi` firmware override. |
| `team_id` | Optional team UUID; otherwise uses the provider/default team. |
| `ssh_keys` | Optional collection of existing SSH key UUIDs. |
| `vpcs` | Optional collection of existing VPC UUIDs. |
| `firewall_group_id` | Optional existing firewall group UUID. |
| `ipv6` | Enable IPv6 at provisioning time. Default `false`. |
| `disable_ssh_blocking` | Permit outgoing SSH at provisioning time. Default `false`. |
| `startup_script_id` | Optional existing startup script UUID; applies to OS/snapshot deployments. |
| `create_timeout` | Maximum provisioning duration as a Go duration string, e.g. `20m` (default). Changing this does not replace the VM. |
| `update_timeout` | Maximum rename-confirmation duration as a Go duration string, default `5m`. Changing this does not replace the VM. |

Use either an OS template, a snapshot, or an ISO, not multiple image sources. Changes to replacement-only fields destroy and recreate the VM; disks and data may be lost. The API does not report every provisioning input, so create-only values cannot all be refreshed from remote state.

## Returned attributes

The resource exposes `id`, `password` (sensitive), `main_ipv4`, `main_ipv6`, `template`, and `status`. The `cpu`, `ram`, `disk`, `name`, `boot_mode`, and `firewall_group_id` fields can be refreshed from corresponding API properties where supported. See `terraform providers schema -json` for the authoritative schema.

## Import

```sh
terraform import onidel_virtual_machine.example 'vm-uuid'
```

For non-default teams, configure provider `team_id` or `ONIDEL_TEAM_ID` first. GET does not return the original billing cycle, instance-type ID, location, image-source ID, or every provisioning option. Restore those original values in configuration after import and review the plan before applying.

## Provisioning response limitation

Onidel's documented 201 response has no defined body schema. The client requires a returned ID or usable `Location` header. If neither is present, it fails without retrying the POST; check the dashboard for a created VM and import it before retrying. When an ID is known but activation fails, the resource retains its ID in Terraform state.
