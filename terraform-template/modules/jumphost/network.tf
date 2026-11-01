# creation takes around 10 seconds
resource "stackit_network" "net1" {
  project_id = var.project_id
  name       = "${var.name_prefix}m10-net1${var.name_suffix}"
  routed = true
}

# creation about 25 seconds
resource "stackit_public_ip" "ip1" {
  project_id = var.project_id
  network_interface_id = stackit_network_interface.nic1.network_interface_id
}


resource "stackit_security_group" "g1" {
  project_id = var.project_id
  name       = "${var.name_prefix}m10-g1${var.name_suffix}"
  stateful   = true
}

resource "stackit_security_group_rule" "rule1" {
  project_id        = var.project_id
  security_group_id = stackit_security_group.g1.security_group_id
  direction         = "ingress"
  ether_type        = "IPv4"
}

# creation takes around 5 seconds
resource "stackit_network_interface" "nic1" {
  project_id = var.project_id
  network_id         = stackit_network.net1.network_id # won't work.
  security_group_ids = [stackit_security_group.g1.security_group_id]
}

#output "public_ip" {
#  value = stackit_network.net1.public-ip
#}
