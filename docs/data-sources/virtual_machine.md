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

`name`, `password` (sensitive), `cpu`, `ram`, `disk`, `main_ipv4`, `main_ipv6`, `template`, `boot_mode`, `firewall_group_id`, `firewall_group_internal_id`, and `status`.

When the API reports a numeric firewall database ID instead of a UUID, the
number is exposed as `firewall_group_internal_id` and `firewall_group_id` is
null. The data source does not guess the corresponding public UUID.

The password may be stored in Terraform state even though display is redacted. Protect your state accordingly.
