locals {
  standard_installation_image = {
    size        = 64 # GB
    source_type = "image"
    # see `stackit image list` for a list of all images, or
    # API access: https://docs.api.eu01.stackit.cloud/documentation/iaas/version/v2#tag/Images/operation/v2ListImagesInProject
    # if you use an illegal uuid here, the error will be "resource not found: image"
    source_id   = "7ea12ef3-34f9-4ce7-956b-2302e01461b1" # Ubuntu 26.04
    delete_on_termination = true
  }
  
  jumphost_name = "${var.name_prefix}m10-jumphost${var.name_suffix}"
  cloud_init_shell_scripts = fileset("${path.module}/cloud-init", "*.sh")
}

resource "random_password" "jumphost" {
  length           = 15
  # no special characters for easier copying and in order to avoid
  # escaping in cloud-init yaml file
  special          = false
}

output "jumphost_password" {
  value = random_password.jumphost.result
  sensitive = true
}

data "template_file" "jumphost_cloud_config" {
  template = file("${path.module}/cloud-init/cloud-init-jumphost.yaml.tpl")
  vars = {
    #ssh_public_key = stackit_key_pair.home.public_key,
    default_jumphost_password = random_password.jumphost.result,
    fqdn = stackit_dns_record_set.m10-jumphost.name
    # Caddy site address: FQDN without trailing dot
    servername = trimsuffix(stackit_dns_record_set.m10-jumphost.name, ".")
  }
}

data "cloudinit_config" "jumphost" {
  gzip          = false
  base64_encode = false

  part {
    filename     = "cloud-config.yaml"
    content_type = "text/cloud-config"
    content = data.template_file.jumphost_cloud_config.rendered
  }
  
  dynamic "part" {
    for_each = local.cloud_init_shell_scripts
    content {
      filename     = part.value
      content_type = "text/x-shellscript"
      content = file("${path.module}/cloud-init/${part.value}")
    }
  }
}

# created within 60 seconds
resource "stackit_server" "jumphost" {
  #count = 0 # disabled resource for the time being

  project_id   = var.project_id
  boot_volume  = local.standard_installation_image
  name         = local.jumphost_name
  machine_type = "g1.1" # flavors
  #keypair_name = stackit_key_pair.home.name
  
  user_data    = data.cloudinit_config.jumphost.rendered
  
  network_interfaces = [
    stackit_network_interface.nic1.network_interface_id
  ]    
}
