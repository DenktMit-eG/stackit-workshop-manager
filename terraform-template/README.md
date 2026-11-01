# Landing Zone Template for a STACKIT University Cloud Enablement Engineer Course

This directory contains a terraform project.

If you see a file called `terraform.tfstate` and a directory like `output`, this means it also
holds a provisionied state, i.e. there are resources allocated at STACKIT available for you
to inspect and use.

If you want to work with terraform (or OpenTofu, respectively), run `terraform init` once
to install the STACKIT terraform provider on your computer. You can then use `terraform show`
to display the current state.

Caveats if you want to work with terraform:

* You will need to make sure you have your service account key files at hand. Inspect
  `teilnehmer.auto.tfvar` to see what you need to provide.
