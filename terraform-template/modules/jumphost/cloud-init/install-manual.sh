#!/bin/bash
#
# install a few programs which are not in Ubuntu repositories
#

wd=$(mktemp -d); cd $wd; trap 'rm -rf -- "$wd"' EXIT

curl -LO "https://dl.k8s.io/release/$(curl -L -s https://dl.k8s.io/release/stable.txt)/bin/linux/amd64/kubectl"
install -m 555 kubectl /usr/local/bin

wget https://releases.hashicorp.com/terraform/1.14.3/terraform_1.14.3_linux_amd64.zip
unzip terraform*zip
mv terraform /usr/local/bin

curl -L https://get.helm.sh/helm-v4.0.4-linux-amd64.tar.gz | tar xz
mv linux-amd64/helm /usr/local/bin

curl -L https://github.com/astral-sh/uv/releases/download/0.9.18/uv-x86_64-unknown-linux-gnu.tar.gz | tar xz
mv uv-x86_64-unknown-linux-gnu/* /usr/local/bin

curl -L https://github.com/stackitcloud/stackit-cli/releases/download/v0.49.0/stackit-cli_0.49.0_linux_amd64.tar.gz | tar xz
mv stackit /usr/local/bin

curl -L "https://packages.cloudfoundry.org/stable?release=linux64-binary&version=v8&source=github" | tar xz
mv cf* /usr/local/bin

curl -L https://github.com/buildpacks/pack/releases/download/v0.39.1/pack-v0.39.1-linux.tgz | tar xz
mv pack /usr/local/bin

wget https://github.com/argoproj/argo-cd/releases/latest/download/argocd-linux-amd64
install -m 555 argocd-linux-amd64 /usr/local/bin/argocd

curl -L https://github.com/microsoft/edit/releases/download/v1.2.1/edit-1.2.0-x86_64-linux-gnu.tar.zst | tar x --zstd
mv edit /usr/local/bin

curl -L https://github.com/derailed/k9s/releases/download/v0.50.16/k9s_Linux_amd64.tar.gz | tar xz
mv k9s /usr/local/bin

