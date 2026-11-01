# These variables can be assigned to in the *.auto.tfvars file

variable "project_id" {
  type = string
  description = "STACKIT project where to work in"
  # typical value: # default = "2230f266-afb1-4cd0-ab79-da618a047d62"   # University/DM Playground
}

variable "domain" {
  type = string
  description = <<-EOT
    Subdomain below .stackit.zone you want to allocate.
    Free stackit subdomains:
     .runs.onstackit.cloud
     .stackit.{rocks,gg,zone,run}
  EOT
  # typical value:
  # default = "sk-m10.stackit.zone"
}

variable "dns_zone_id" {
  type = string
  description = "UUID of generated STACKIT DNS zone where to operate in"
}


#variable "stackit_service_account_key_file" {
#  type = string
#  description = "Filename, endslike sa-key-....json"
#  # typical value:
#  # default = file("../../testproj-keys/sa-key-a200b5f8-2fe0-4786-ba52-32f59b68d212.json")
#}

#variable "stackit_private_key_file" {
#  type = string
#  description = "Filename, ends like sa-key-...-private.pem"
#  # typical value:
#  # default = file("../../testproj-keys/sa-key-a200b5f8-2fe0-4786-ba52-32f59b68d212-private.pem")
#}
  
variable "name_prefix" {
  type = string
  default = "training"
  description = <<-EOT
    Common prefix (=beginning) for all named STACKIT resources to be allocated. Will be visible in Web Portal and CLI.
    In a shared organization, consider your initials such as "John Doe" becomes "jd".
    If you organization is shared beyond this course, consider a context such as "training-jd"
    
    Attention: Assert that Name prefix + name suffix string length <= 11, because SKE node pool has a maximum length of 15.
    That is, a rule of thumb is a name prefix and name suffix not exceed 4 characters each in length!
  EOT
}

variable "name_suffix" {
  type = string
  default = "1"
  description = <<-EOT
    Common suffix (=ending) for all named STACKIT resources to be allocated. Will be visible in Web Portal and CLI.
    If you run tf multiple times and runs fail or you end in a sick state, consider increasing this number and
    delete old (stale, dangling) resources manually via web browser or CLI.
  EOT
}

