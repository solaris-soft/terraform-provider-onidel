# Terraform Provider for Onidel

An independent Terraform provider for [Onidel Cloud](https://onidel.com), maintained by Solaris Soft. Built with Go and the Terraform Plugin Framework.

## Scope

- `onidel_virtual_machine` resource: provision, refresh, rename, destroy, and import VMs.
- `onidel_virtual_machine` data source: look up an existing VM by UUID.
- OS templates, snapshots, and custom ISO deployment; SSH keys, VPCs, firewall and startup-script references at provisioning time.

This initial provider does **not** manage SSH keys, VPCs, firewalls, images, or other Onidel services as standalone resources. Changes to provisioning settings other than the VM name require replacement. Review replacement plans carefully: they destroy and recreate the VM.

## Build and use locally

Requires Go 1.24+ and Terraform 1.8+.

```sh
go mod download
go build -o terraform-provider-onidel .
```

Add a development override to your Terraform CLI config (`~/.terraformrc` on Linux/macOS). Replace the path with the **absolute directory containing the binary**:

```hcl
provider_installation {
  dev_overrides {
    "registry.terraform.io/solaris-soft/onidel" = "/absolute/path/to/terraform-provider-onidel"
  }
  direct {}
}
```

With this development override, run `terraform plan` and `terraform apply` directly; do **not** run `terraform init` for the standalone examples, since this provider is not yet published to the Terraform Registry. A public GitHub repository alone does not publish a Registry provider.

```sh
export ONIDEL_API_TOKEN='your-api-token'
# Optional default team:
export ONIDEL_TEAM_ID='your-team-uuid'
```

Create a configuration:

```hcl
terraform {
  required_providers {
    onidel = {
      source = "solaris-soft/onidel"
    }
  }
}

provider "onidel" {}

resource "onidel_virtual_machine" "example" {
  name          = "terraform-example"
  payment_cycle = "hourly"
  instance_type = "replace-with-instance-type-uuid"
  location      = "Sydney"
  cpu           = 2
  ram           = 4096
  disk          = 40
  os            = 2
}

output "ipv4" {
  value = onidel_virtual_machine.example.main_ipv4
}
```

Use actual instance-type UUIDs, OS IDs, available locations, and sizes from your account's [Onidel API](https://developers.onidel.com). RAM is in MiB and disk is in GiB according to the provisioning examples. Applying a configuration provisions **billable** infrastructure; this repository's tests use mock HTTP servers and do not create real VMs.

## Credentials and state

Create an API token in [Onidel account settings](https://cloud.onidel.com/account/settings). The provider sends it as a Bearer token to `https://api.cloud.onidel.com`. Prefer the `ONIDEL_API_TOKEN` environment variable over storing tokens in configuration. `ONIDEL_ENDPOINT` overrides the API endpoint for testing or compatible deployments.

The API token and VM password are marked sensitive, but **Terraform state can still contain secrets**. Use encrypted remote state, access controls, and appropriate secret rotation. Never commit state, credentials, or plan files.

## Import

```sh
terraform import onidel_virtual_machine.example 'existing-vm-uuid'
```

Set the default team in the provider or `ONIDEL_TEAM_ID` before importing a VM belonging to a non-default team. The API does not return all provisioning inputs; add the original create-time settings to configuration after import and inspect the resulting plan before applying.

## API compatibility

The current [Onidel OpenAPI specification](https://developers.onidel.com/api-reference/virtual-machine/provision-a-new-vm) declares HTTP 201 for provisioning but does **not** specify its response body. The client accepts a VM object/ID or a usable `Location` header. If Onidel returns no discoverable UUID, creation returns an explicit error rather than guessing from a name or retrying a potentially billable POST. Check your Onidel dashboard and import any VM created in that situation before retrying.

Provisioning waits for the VM to become active. If polling fails after an ID was returned, the provider retains that ID in state so the VM can be refreshed or destroyed. Create-only fields that are absent from GET responses retain their configured values; they cannot all be checked for remote drift.

## Development

```sh
go test ./...
go test -race ./...
go vet ./...
```

See [`docs/`](docs/) for provider, resource, and data-source references, and [`examples/`](examples/) for configuration samples.

## Design references

- [Onidel: provision a new VM](https://developers.onidel.com/api-reference/virtual-machine/provision-a-new-vm), plus the linked OpenAPI schemas for read, update, and destroy.
- [HashiCorp: writing custom Terraform providers](https://www.hashicorp.com/en/blog/writing-custom-terraform-providers): isolated upstream client, resource schemas, CRUD lifecycle, drift handling, and partial state. The article's legacy SDK examples are adapted to the current Plugin Framework.

This is a community provider, not an official Onidel or HashiCorp product.

## License

MIT. See [LICENSE](LICENSE).
