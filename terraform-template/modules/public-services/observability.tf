resource "stackit_observability_instance" "lz" {
  project_id                             = var.project_id
  name                                   = "${var.name_prefix}lz${var.name_suffix}"
  plan_name                              = "Observability-Starter-EU01" # cheapest!
  acl                                    = ["1.1.1.1/32", "0.0.0.0/0"]
  grafana_admin_enabled                  = true
  metrics_retention_days                 = 90
  metrics_retention_days_5m_downsampling = 90
  metrics_retention_days_1h_downsampling = 90
}

resource "stackit_observability_credential" "lz-creds" {
  project_id  = var.project_id
  instance_id = stackit_observability_instance.lz.instance_id
}

locals {
  grafana_access = {
    # read-only props from stackit_observability_instance:
    dashboard_url = stackit_observability_instance.lz.dashboard_url
    grafana_url = stackit_observability_instance.lz.grafana_url
    # various other urls
    grafana_admin_pwd = stackit_observability_instance.lz.grafana_initial_admin_password
    grafana_admin_user = stackit_observability_instance.lz.grafana_initial_admin_user
    # grafana_stackit_observability_instance.lz.grafana_public_read_access # should be RW
  }

  generic_observability_credentials = {
    username = stackit_observability_credential.lz-creds.username
    password  = stackit_observability_credential.lz-creds.password
  }
}

output "observability_urls" {
  value = local.grafana_access
  sensitive = true
}
