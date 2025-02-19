# Privatemode AI

This repository contains the source code of all components of [Privatemode](https://www.privatemode.ai) that are part of the [TCB](https://www.edgeless.systems/wiki/what-is-confidential-computing/threat-model#trusted-computing-base).
The build is reproducible.
This allows users to fully [verify the Privatemode service](https://docs.privatemode.ai/security#verifiability).

## License

You are allowed to inspect the code and build it for auditing and verification purposes. For details, see [LICENSE](LICENSE).

## How to build

You need to have

1. A Linux machine
2. [Docker](https://docs.docker.com/engine/install/)
3. A [Nix](https://nixos.org/) installation.
   To install Nix, we recommend the [Determinate Systems Nix installer](https://determinate.systems/posts/determinate-nix-installer/).

You can reproduce the relevant container images by running:

```sh
./scripts/calculate-image-digests.sh
```

### Desktop app

You need to have

- [Wails installation v2.9.1+](https://wails.io/docs/gettingstarted/installation)

before building the app with the following command:

```bash
cd privatemode-proxy/app
VITE_DEFAULT_MODEL="latest" VITE_API_BASE="" wails build -tags "contrast_unstable_api"
```

You may optionally set the `-platform` (cross-platform build) and `-nsis` flags (Windows installer).

After the successful build, you can find the app in the `privatemode-proxy/app/build/bin` directory.
