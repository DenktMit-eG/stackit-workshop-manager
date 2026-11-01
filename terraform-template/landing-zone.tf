
module "services" {
  source = "./modules/public-services"
  
  project_id = var.project_id
  domain = var.domain
  name_prefix = var.name_prefix
  name_suffix = var.name_suffix
}

module "jumphost1" {
  source = "./modules/jumphost"
  
  project_id = var.project_id
  domain = var.domain
  dns_zone_id = module.services.dns_zone_id
  name_prefix = var.name_prefix
  name_suffix = var.name_suffix
}

output "services" {
  value = module.services
  sensitive = true
}

output "jumphost1" {
  value = module.jumphost1
  sensitive = true
}
