#!/usr/bin/env bash

set -euo pipefail

if [ "$#" -ne 3 ]; then
  echo "Usage: $0 <model-name> <model-idx> <root-hash>"
  exit 1
fi

model_name="$1"
model_idx="$2"
root_hash="$3"

modelweights="modelweights-${model_name}-${model_idx}"

# Compute the length of the actual erofs data, i.e. the offset of the
# verity hash tree.
# Offset 0x24 (265 * 4 = 0x424, minus superblock offset of 0x400) in
# the erofs superblock is the block count of the filesystem as a
# uint32 (little-endian).
block_count="$(dd if="/dev/${modelweights}" bs=4 skip=265 count=1 | od -An -tu4)"
# Multiply by the block size (4096) to get the offset of the hash tree.
hash_offset="$((block_count * 4096))"

mkdir "/mnt/${modelweights}"

veritysetup open --hash-offset "${hash_offset}" \
  "/dev/${modelweights}" "${modelweights}" \
  "/dev/${modelweights}" "${root_hash}"

mount -o ro "/dev/mapper/${modelweights}" "/mnt/${modelweights}"

sleep inf
