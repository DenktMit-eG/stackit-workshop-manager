resource "stackit_dns_zone" "m10" {
  project_id    = var.project_id
  name          = "${var.name_prefix}m10-tf${var.name_suffix}"
  # free stackit subdomains:
  #   .runs.onstackit.cloud
  #   .stackit.{rocks,gg,zone,run}
  dns_name      = var.domain
  contact_email = "hostmaster@stackit.cloud"
  type          = "primary"
  acl           = "192.168.0.0/24"
  description   = "Managed by terraform, for landing zone"
  default_ttl   = 3600
}

output "dns_zone_id" {
  value = stackit_dns_zone.m10.zone_id
}
