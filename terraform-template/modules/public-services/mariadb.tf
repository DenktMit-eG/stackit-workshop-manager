# MariaDB allocation takes around 12 minutes!

resource "stackit_mariadb_instance" "lz" {
  project_id = var.project_id
  name       = "${var.name_prefix}lz${var.name_suffix}"
  version    = "10.11"
  # smallest offering, cf CLI `stackit mariadb plans`
  plan_name  = "stackit-mariadb-1.4.10-single"
  parameters = {
    sgw_acl = "0.0.0.0/0"
    #sgw_acl = "193.148.160.0/19,45.129.40.0/21,45.135.244.0/22,0.0.0.0/0" # fixme global reachability
  }
}

resource "stackit_mariadb_credential" "lz-creds" {
  project_id  = var.project_id
  instance_id = stackit_mariadb_instance.lz.instance_id
}

locals {
  mariadb = {
    hostname = stackit_mariadb_credential.lz-creds.host
    port = stackit_mariadb_credential.lz-creds.port
    database_name = stackit_mariadb_credential.lz-creds.name
    username = stackit_mariadb_credential.lz-creds.username
    password = stackit_mariadb_credential.lz-creds.password
  }
}

output "mariadb_credentials" {
  value = local.mariadb
  sensitive = true
}
