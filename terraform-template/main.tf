 terraform {
  required_providers {
    stackit = {
      source = "stackitcloud/stackit"
      version = "0.120.0"
    }
  }
}

locals {
  stackit_service_account_key = file(var.stackit_service_account_key_file)
  stackit_private_key = file(var.stackit_private_key_file)
}

provider "stackit" {
  default_region = "eu01"
  
  service_account_key = local.stackit_service_account_key
  private_key         = local.stackit_private_key

  # or use:
  # export STACKIT_SERVICE_ACCOUNT_KEY_PATH=$(realpath ../../testproj-keys/sa-key-a200b5f8-2fe0-4786-ba52-32f59b68d212.json)
  # export STACKIT_PRIVATE_KEY_PATH=$(realpath ../../testproj-keys/sa-key-a200b5f8-2fe0-4786-ba52-32f59b68d212-private.pem)
  
}
