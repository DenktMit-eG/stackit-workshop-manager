# STACKIT Cloud Foundry "organization" is created instantly

resource "stackit_scf_organization" "lz" {
  project_id = var.project_id
  name       = "${var.name_prefix}lz${var.name_suffix}"
  # quota_id = ...
  # suspended = false
}


data "stackit_scf_platform" "lz" {
  project_id  = var.project_id
  platform_id = stackit_scf_organization.lz.platform_id
}

resource "stackit_scf_organization_manager" "lz" {
  project_id = var.project_id
  org_id     = stackit_scf_organization.lz.org_id
}

locals {
  scf = {
    api_url = data.stackit_scf_platform.lz.api_url
    user = stackit_scf_organization_manager.lz.username
    password = stackit_scf_organization_manager.lz.password
  }
}

output "scf" {
  value = local.scf
  sensitive = true
}
