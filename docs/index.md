---
page_title: "Onidel Provider"
description: "Manage Onidel Cloud virtual machines with Terraform."
---

# Onidel Provider

The Onidel provider manages virtual machines through the [Onidel Cloud API](https://developers.onidel.com). This is a community provider maintained by Solaris Soft, not an official Onidel or HashiCorp product.

## Example Usage

Set `ONIDEL_API_TOKEN` to an API token from your [Onidel account settings](https://cloud.onidel.com/account/settings).

```hcl
terraform {
  required_providers {
    onidel = {
      source = "solaris-soft/onidel"
    }
  }
}

provider "onidel" {
  # Prefer ONIDEL_API_TOKEN in the environment.
  # team_id = "your-team-uuid"
}
```

## Configuration

| Attribute | Description |
| --- | --- |
| `api_token` | Sensitive API token. Falls back to `ONIDEL_API_TOKEN`. Required at runtime. |
| `endpoint` | API base URL. Falls back to `ONIDEL_ENDPOINT`, then `https://api.cloud.onidel.com`. |
| `team_id` | Default team UUID. Falls back to `ONIDEL_TEAM_ID`. Omit to use your Onidel default team. |

A resource or data source can override the provider's default team using its `team_id` attribute.

After the provider is published, install it with `terraform init`. For unreleased development builds, follow the [repository's local installation instructions](https://github.com/solaris-soft/terraform-provider-onidel#build-and-use-locally) instead.

> Sensitive values may be stored in Terraform state. Protect state and never commit credentials.
