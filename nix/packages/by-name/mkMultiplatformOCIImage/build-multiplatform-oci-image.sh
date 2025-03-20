#!/usr/bin/env bash

# This script combines multiple OCI directories into a single, multi-platform OCI image.
# Multi-platform images work by:
# - Adding all layers into a shared blob store (blobs/sha256)
# - Creating an architecture-specific index for each platform, appending this into the '.manifests'
#   field of a shared index file. This shared file is then hashed and stored in the blob store by
#   its hash.
# - The top-level index file then gets a reference to that hash, and is copied to the output directory.
# - The output directory is completed by adding an oci-layout file that contains some version metadata.

set -euo pipefail

if [ "$#" -ne 1 ]; then
  echo "Usage: $0 <output-dir>"
  exit 1
fi

out="$1"

mkdir -p "${out}/blobs/sha256"

while read -r line; do
  arch="$(echo "$line" | cut -d' ' -f1)"
  dir="$(echo "$line" | cut -d' ' -f2-)"

  digest="$(jq -r ".manifests[0].digest" <"${dir}/index.json")"
  size="$(wc -c <"${dir}/blobs/sha256/$(echo "${digest}" | cut -d: -f2)")"

  cp --update=none "${dir}/blobs/sha256/"* "${out}/blobs/sha256"

  cat <<EOF
    {
      "digest": "${digest}",
      "mediaType": "application/vnd.oci.image.manifest.v1+json",
      "platform": {
        "architecture": "${arch}",
        "os": "linux"
      },
      "size": ${size}
    }
EOF
done |
  jq -s '{manifests: ., "mediaType":"application/vnd.oci.image.index.v1+json","schemaVersion":2}' >"${out}/my-index.json"

size="$(wc -c <"${out}/my-index.json")"
digest="$(sha256sum <"${out}/my-index.json" | cut -d' ' -f1)"

mv "${out}/my-index.json" "${out}/blobs/sha256/${digest}"
cat >"${out}/index.json" <<EOF
{
  "schemaVersion": 2,
  "manifests": [
    {
      "mediaType": "application/vnd.oci.image.index.v1+json",
      "digest": "sha256:${digest}",
      "size": $size
    }
  ]
}
EOF

echo '{"imageLayoutVersion":"1.0.0"}' >"${out}/oci-layout"
