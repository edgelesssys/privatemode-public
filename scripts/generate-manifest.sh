#!/usr/bin/env nix
#! nix shell nixpkgs#yq-go -c bash
# shellcheck shell=bash

set -euo pipefail

dir=$(dirname "$0")/..
contrast_version=$(cat "$dir/contrast-version")
base_url=https://github.com/edgelesssys/contrast/releases/download/$contrast_version

# download Contrast CLI if not already existing with the correct version
./contrast -v | grep "contrast version $contrast_version" || wget --backups=1 "$base_url/contrast" && chmod +x contrast

# generate and adjust manifest
./contrast generate --disable-updates --reference-values metal-qemu-snp-gpu "$dir/deployment.yaml"
SECRET_SERVICE_DOMAIN=staging.secret.privatemode.ai
SECRET_SERVICE_K8S_DOMAIN=secret-service-internal.$(yq 'select(.metadata.name == "secret-service") | .metadata.namespace' "$dir/deployment.yaml").svc.cluster.local
export SECRET_SERVICE_DOMAIN SECRET_SERVICE_K8S_DOMAIN
"$dir/scripts/adjust-manifest.sh" manifest.json
