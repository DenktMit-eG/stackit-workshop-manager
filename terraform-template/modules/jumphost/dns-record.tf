# you can dig/nsresolve this example but it won't work in the browser
resource "stackit_dns_record_set" "m10-jumphost" {
  project_id = var.project_id
  zone_id    = var.dns_zone_id
  # FQDN: entry in the given domain as defined in other resource
  name       = "jumphost.${var.domain}"
  type       = "A"
  ttl        = 60 # seconds; short TTL for development, avoiding DNS caching problems
  comment    = "Managed by terraform"
  # do not forget the . at the end of a FQDN
  records    = [
    # stackit_network.net1.public_ip # do not use this, it won't be updated
    stackit_public_ip.ip1.ip
  ]
}

output "jumphost_fqdn" {
  value = stackit_dns_record_set.m10-jumphost.name
}

output "jumphost_ip" {
  value = stackit_public_ip.ip1.ip
}
