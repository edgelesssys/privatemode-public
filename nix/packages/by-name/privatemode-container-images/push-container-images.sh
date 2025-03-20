#!/usr/bin/env bash

set -euo pipefail

if [ "$#" -ne 2 ]; then
  echo "Usage: $0 <registry> <target-tag>"
  exit 1
fi

REGISTRY="$1"
TARGET_TAG="$2"
IMAGE_DIR=@IMAGE_DIR@

echo "Pushing images in ${IMAGE_DIR} to ${REGISTRY}"
echo "Using target tag ${TARGET_TAG}"

for image in "${IMAGE_DIR}/"*; do
  target="${REGISTRY}/$(basename "${image}"):${TARGET_TAG}"
  echo "Pushing ${image} to ${target}"
  crane push "${image}" "${target}"
done
