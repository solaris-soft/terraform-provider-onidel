terraform {
  required_providers {
    onidel = {
      source = "solaris-soft/onidel"
    }
  }
}

# Set ONIDEL_API_TOKEN in your environment.
# Optional: ONIDEL_TEAM_ID for the default team.
provider "onidel" {}
