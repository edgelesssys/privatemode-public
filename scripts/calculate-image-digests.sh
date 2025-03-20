#!/usr/bin/env bash
# apart from being used in the CI, this script is part of the privatemode-public to help users with reproducing image hashes.
# therefore it also needs to work when executed in the repo 'privatemode-public'.
set -eo pipefail

host="$1"
if [[ -z $host ]]; then
  host=$(hostname)
fi

dir="$2"
if [[ -z $dir ]]; then
  dir=$(mktemp -d)
fi

nix build .#privatemode-container-images.oss-oci-images --out-link "$dir/images"

echo '{}' >"hashes-$host.json"

for image in "${dir}/images/"*; do
  hash=$(jq -r '.manifests[0].digest' <"${image}/index.json")
  image_name=$(basename "${image}")
  jq --arg image "${image_name}" --arg hash "${hash}" \
    '. + {($image): $hash}' "hashes-${host}.json" >tmp.json && mv tmp.json "hashes-${host}.json"
done

echo "inspect image hashes by running: 'cat hashes-${host}.json'"
echo "delete temporary directory by running: 'rm -rf $dir'"
