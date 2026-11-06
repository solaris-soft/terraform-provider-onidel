---
page_title: "onidel_virtual_machine Data Source"
description: "Look up an existing Onidel virtual machine by UUID."
---

# onidel_virtual_machine (Data Source)

Reads a VM without managing its lifecycle. A missing VM produces an error.

```hcl
data "onidel_virtual_machine" "existing" {
  id = "existing-vm-uuid"
}

output "ipv4" {
  value = data.onidel_virtual_machine.existing.main_ipv4
}
```

## Arguments

- `id` (required): VM UUID.
- `team_id` (optional): Team UUID. Defaults to the provider team or your Onidel account's default team.

## Returned attributes

The VM's name, password (sensitive), CPU/memory/disk sizes, primary IPv4/IPv6 addresses, OS template, boot mode, firewall group, and status. See `terraform providers schema -json` for exact field names.

The password may be stored in Terraform state even though display is redacted. Protect your state accordingly.
