#!/usr/bin/env bash

set -euo pipefail

cd -- "$(dirname -- "${BASH_SOURCE[0]}")"

mkdir -p output

terraform output -json | jq -r '
  map_values(.value) as $root
  | $root
  | paths(scalars) as $path
  | ($path | map(tostring) | join("_") | ascii_upcase) as $key
  | "\($key)=\($root | getpath($path) | tostring | @sh)"
' > output/project.env

project="$(basename "$PWD")"

# largish 200MB stuff, recover with "terraform init"
rm -r .terraform

cd ..
rm -f "${project}.zip"

zip -9 -r "${project}.zip" "$project"

echo "Created: $(pwd)/${project}.zip"
