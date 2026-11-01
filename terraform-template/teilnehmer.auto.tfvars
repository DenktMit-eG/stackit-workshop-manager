# Course participants are supposed to (at least) change values in this file
# in order to make this terraform project work.

# Please inspect variables.tf for human readable help on their meaning!

project_id = "xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx"

name_prefix = "tn"
name_suffix = "g1"

# Free stackit subdomains:
# .runs.onstackit.cloud
# .stackit.{rocks,gg,zone,run}
domain = "customername-lzN.runs.onstackit.cloud"

stackit_service_account_key_file = "path-to-your/sa-key-xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx.json"
stackit_private_key_file = "path-to-your/sa-key-xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx-private.pem"
