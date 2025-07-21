# Verity protected model disks

Models deployed in Privatemode are stored on [dm-verity](https://docs.kernel.org/admin-guide/device-mapper/verity.html) protected disks.
This ensures users can independently verify the integrity and content of a model before sending data to it.

The disks are formatted using [`systemd-repart`](https://www.freedesktop.org/software/systemd/man/latest/systemd-repart.html).
The configuration files can be found in the [repart](./repart/) directory.

To ensure consistent tooling, scripts are run using a Nix-built container image.
Images are published at `ghcr.io/edgelesssys/privatemode/verity-disk-generator`.

You can also build the image yourself using `nix`:

```bash
registry=<your_registry>
nix build .#verity-disk-generator
skopeo copy docker-archive:result oci:result-oci --insecure-policy --tmpdir .
crane push result-oci ${registry}/verity-disk-generator:latest
```

The following is needed to set up a verity protected disk:

- The URL of the repository to download the model from, for example `https://huggingface.co/facebook/opt-125m`
- The commit hash to download the model at, for example `27dcfa74d334bc871f3234de431e71c6eeba5dd6`
- A block device or disk image to create the disk on
  For local testing you can set up a disk image using the following command:

  ```bash
  disk_size=1G
  truncate -s $disk_size data.disk
  ```

  Note that the disk size must be slightly larger than the model repository, as we also need space for the verity hash tree.

- An access token to authenticate with the model repository
  For HuggingFace, you can follow the official [documentation](https://huggingface.co/docs/hub/en/security-tokens) to generate a token.

- (Optional) A list of files, or wildcards, to exclude from copying from the model repository.
  Used to keep the disk size small in case the model repository contains model files in multiple formats.

Since some OS settings, such as SELinux, can interfere with the setup, e.g. by setting SELinux specific information on the disk, we recommend running the setup on a fresh Ubuntu 24.04 installation.
From a fresh installation, only [Docker engine](https://docs.docker.com/engine/install/ubuntu/) is additionally required.

Once your environment is ready, you can create your verity protected disk.
Assuming you want to reproduce a model disk for the `facebook/opt-125m` model at commit `27dcfa74d334bc871f3234de431e71c6eeba5dd6`, run the following command:

```bash
registry=<your_registry>
GIT_PAT=<your_git_pat>
EXCLUDE_GIT_FILES="example-file-*.txt"
MODEL_SOURCE=https://huggingface.co/facebook/opt-125m
COMMIT_HASH=27dcfa74d334bc871f3234de431e71c6eeba5dd6
DISK_SIZE_GB=1

truncate -s ${DISK_SIZE_GB}G data.disk
touch repart.json

docker run --rm -it \
    --privileged \
    -v ${PWD}/repart.json:/repart.json \
    -v ${PWD}/data.disk:/data.disk \
    -e GIT_PAT=${GIT_PAT} \
    -e EXCLUDE_GIT_FILES="${EXCLUDE_GIT_FILES}" \
    ${registry}/verity-disk-generator:latest \
    "/data.disk" $MODEL_SOURCE $COMMIT_HASH
```

On success, the repart output will be saved to `repart.json`.
Retrieve the verity root hash using the following command:

```bash
jq -r '.[0].roothash' repart.json
```

Compare this hash to the value set for the `--root-hash` flag of the `disk-mounter` container of the workload you want to verify.
