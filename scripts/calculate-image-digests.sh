#!/usr/bin/env bash
# apart from being used in the CI, this script is part of the privatemode-public to help users with reproducing image hashes.
# therefore it also needs to work when executed in the repo 'privatemode-public'.

host="$1"
if [[ -z $host ]]; then
  host=$(hostname)
fi

dir="$2"
if [[ -z $dir ]]; then
  dir=$(mktemp -d)
fi

version=$(nix eval --impure --raw --expr '(import ./version.nix).version')

function build_and_load_image() {
  nix build ".#$1.image"
  cp -L result "$dir/$1.tar"
  docker load <result
}

services=(attestation-agent disk-mounter inference-proxy secret-service privatemode-proxy)

for image in "${services[@]}"; do
  build_and_load_image "${image}"
done

echo '{}' >"hashes-$host.json"

for image in "${services[@]}"; do
  hash=$(nix run nixpkgs#skopeo -- inspect "docker-daemon:${image}:$version" | jq -r '.Digest')
  jq --arg image "$image" --arg hash "$hash" \
    '. + {($image): $hash}' "hashes-$host.json" >tmp.json && mv tmp.json "hashes-$host.json"
done

echo "inspect image hashes by running: 'cat hashes-$host.json'"
echo "delete temporary directory by running: 'rm -rf $dir'"
