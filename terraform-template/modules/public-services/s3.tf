# S3 Allocation takes almost no time

resource "stackit_objectstorage_bucket" "lz" {
  ## General-Purpose exemplaric bucket
  project_id = var.project_id
  name       = "${var.name_prefix}lz${var.name_suffix}" # this will be the public bucket name
}

resource "stackit_objectstorage_bucket" "tfstate" {
  ## Supposed to be used to store a terraform state
  project_id = var.project_id
  name       = "${var.name_prefix}tfstate${var.name_suffix}"
}

resource "stackit_objectstorage_credentials_group" "lz" {
  project_id = var.project_id
  name       = "${var.name_prefix}lz-cg${var.name_suffix}"
}

resource "stackit_objectstorage_credential" "lz" {
  project_id           = var.project_id
  credentials_group_id = stackit_objectstorage_credentials_group.lz.credentials_group_id
  expiration_timestamp = "2027-12-31T00:00:00Z"
}

locals {
  parsed_url_path_style = regex(
    "(?:(?P<scheme>[^:/?#]+):)?(?://(?P<authority>[^/?#]*))?(?P<path>[^?#]*)(?:\\?(?P<query>[^#]*))?(?:#(?P<fragment>.*))?",
    stackit_objectstorage_bucket.lz.url_path_style
  )

  # this structure names follow the relevant part in the nextcloud helm chart
  s3 = {
    host = local.parsed_url_path_style.authority
    bucket = stackit_objectstorage_bucket.lz.name # local.parsed_url_path_style.path, includes "/"
    access_key = stackit_objectstorage_credential.lz.access_key
    secret_key = stackit_objectstorage_credential.lz.secret_access_key
  }
}

output "s3" {
  value = local.s3
  sensitive = true
}

#resource "local_file" "s3cmd_config" {
#  content  = templatefile("${path.module}/templates/s3cfg.tmpl", {
#    s3 = local.s3
#  })
#  filename = "${path.module}/generated/s3cfg"
#}
