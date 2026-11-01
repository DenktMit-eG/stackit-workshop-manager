# SKE Cluster allocation takes 8-12 minutes

resource "stackit_ske_cluster" "lz" {
  project_id             = var.project_id
  name                   = "${var.name_prefix}lz${var.name_suffix}"
  node_pools = [
    {
      name               = "p1" # this must not exceed 15 characters!
      #machine_type       = "c1a.2d" # one of the cheapest available
      machine_type       = "c1a.4d" # 4 CPU, 8 GB RAM; roughly 2 times the cost and power of c1a.2d
      minimum            = "2"
      maximum            = "10"
      availability_zones = ["eu01-2"]
    }
  ]
  maintenance = {
    enable_kubernetes_version_updates    = true
    enable_machine_image_version_updates = true
    start                                = "01:00:00Z"
    end                                  = "02:00:00Z"
  }
  extensions = {
    dns = {
      enabled = true,
      zones = [ stackit_dns_zone.m10.dns_name ]
    }
    observability = {
      enabled = true
      instance_id = stackit_observability_instance.lz.instance_id
    }
  }
}

resource "stackit_ske_kubeconfig" "lz" {
  project_id   = var.project_id
  cluster_name = stackit_ske_cluster.lz.name
  
  refresh = true
  expiration = 60*60*24*30 # one month (base unit: seconds). Max is 6 months
}

#resource "local_file" "kubeconfig_output" {
#  content  = stackit_ske_kubeconfig.lz.kube_config
#  filename = "${path.module}/generated/kubeconfig-${stackit_ske_cluster.lz.name}.yaml"
#}
