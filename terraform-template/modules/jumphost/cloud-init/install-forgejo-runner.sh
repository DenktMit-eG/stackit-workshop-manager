#!/bin/bash
#
# This script downloads the "forgejo-runner" standalone executable for
# hosted Git CI at STACKIT. We will use it later to conveniently spin up a Github-style actions
# runner on this node with a registration token shown at group level in our forgejo
# web instance.
#
# This code stems from https://forgejo.org/docs/latest/admin/actions/runner-installation/
# but currently the docs lack easy copy&paste, therefore the most convenient way is to
# put it as another "startup script" which also demonstrates how cloud init works.
#
# This file will be deposited at /var/lib/cloud/instance/scripts/install-forgejo-runners.sh
# and can be also called from there at later times
# 

echo Forgejo Runner Installation starting...

export ARCH=$(uname -m | sed 's/x86_64/amd64/;s/aarch64/arm64/')
export RUNNER_VERSION=$(curl -X 'GET' https://data.forgejo.org/api/v1/repos/forgejo/runner/releases/latest | jq .name -r | cut -c 2-)
export FORGEJO_URL="https://code.forgejo.org/forgejo/runner/releases/download/v${RUNNER_VERSION}/forgejo-runner-${RUNNER_VERSION}-linux-${ARCH}"
wget -O forgejo-runner ${FORGEJO_URL}
chmod +x forgejo-runner
wget -O forgejo-runner.asc ${FORGEJO_URL}.asc
gpg --keyserver hkps://keys.openpgp.org --recv EB114F5E6C0DC2BCDD183550A4B61A2DC5923710
gpg --verify forgejo-runner.asc forgejo-runner && echo "✓ Verified" || echo "✗ Failed"
mv forgejo-runner /usr/local/bin/
