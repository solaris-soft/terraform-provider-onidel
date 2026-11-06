terraform {
  required_providers {
    onidel = {
      source = "solaris-soft/onidel"
    }
  }
}

provider "onidel" {}

variable "instance_type_id" {
  type        = string
  description = "An available Onidel instance type UUID."
}

variable "os_id" {
  type        = number
  description = "An Onidel OS template ID."
}

resource "onidel_virtual_machine" "example" {
  name          = "terraform-example"
  payment_cycle = "hourly"
  instance_type = var.instance_type_id
  location      = "Sydney"
  cpu           = 2
  ram           = 4096
  disk          = 40
  os            = var.os_id

  # Optional references to existing objects in Onidel:
  # ssh_keys          = ["ssh-key-uuid"]
  # vpcs              = ["vpc-uuid"]
  # firewall_group_id = "firewall-group-uuid"
  # startup_script_id = "startup-script-uuid"
  # ipv6              = true
}

output "ipv4" {
  value = onidel_virtual_machine.example.main_ipv4
}
