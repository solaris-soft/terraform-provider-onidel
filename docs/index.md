---
page_title: "Onidel Provider"
description: "Manage Onidel Cloud virtual machines with Terraform."
---

# Onidel Provider

The Onidel provider manages virtual machines through the Onidel Cloud API.

```hcl
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

See the repository README for local installation. This community provider is not automatically published to the Terraform Registry by creating the GitHub repository.

> Sensitive values may be stored in Terraform state. Protect state and never commit credentials.
