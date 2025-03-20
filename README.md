# Privatemode AI

This repository contains the source code of all components of [Privatemode](https://www.privatemode.ai) that are part of the [TCB](https://www.edgeless.systems/wiki/what-is-confidential-computing/threat-model#trusted-computing-base).
The build is reproducible.
This allows users to fully [verify the Privatemode service](https://docs.privatemode.ai/security#verifiability).

## License

You are allowed to inspect the code and build it for auditing and verification purposes. For details, see [LICENSE](LICENSE).

## Build and verify the container images

See the [Verification from source code](https://docs.privatemode.ai/guides/verify-source) guide in the Privatemode documentation.

## Build the desktop app

You need to have

- [Wails installation v2.9.1+](https://wails.io/docs/gettingstarted/installation)

before building the app with the following command:

```bash
cd privatemode-proxy/app
VITE_DEFAULT_MODEL="latest" VITE_API_BASE="" wails build -tags "contrast_unstable_api"
```

You may optionally set the `-platform` (cross-platform build) and `-nsis` flags (Windows installer).

After the successful build, you can find the app in the `privatemode-proxy/app/build/bin` directory.
