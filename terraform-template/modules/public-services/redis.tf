resource "stackit_redis_instance" "lz" {
  project_id = var.project_id
  name       = "${var.name_prefix}lz-tf${var.name_suffix}"
  version    = "7"
  plan_name  = "stackit-redis-1.4.10-single"  # a cheap one
  parameters = {
    sgw_acl                 = "193.148.160.0/19,45.129.40.0/21,45.135.244.0/22,0.0.0.0/0" # FIXME 0/0
    #enable_monitoring       = false
    #syslog                  = ["logs4.your-syslog-endpoint.com:54321"]
  }
}

resource "stackit_redis_credential" "lz" {
  project_id  = var.project_id
  instance_id = stackit_redis_instance.lz.instance_id
}



locals {
  redis = {
    host = stackit_redis_credential.lz.host # or load_balance_host
    port = stackit_redis_credential.lz.port
    username = stackit_redis_credential.lz.username
    password = stackit_redis_credential.lz.password
    # uri also available    
  }
}

output "redis_credentials" {
  value = local.redis
  sensitive = true
}
