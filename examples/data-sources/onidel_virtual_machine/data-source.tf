terraform {
  required_providers {
    onidel = {
      source = "solaris-soft/onidel"
    }
  }
}

provider "onidel" {}

variable "vm_id" {
  type        = string
  description = "UUID of an existing Onidel VM."
}

data "onidel_virtual_machine" "existing" {
  id = var.vm_id
}

output "ipv4" {
  value = data.onidel_virtual_machine.existing.main_ipv4
}
