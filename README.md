# Privatemode AI

This repository contains the source code of all components of [Privatemode](https://www.privatemode.ai) that are part of the [TCB](https://www.edgeless.systems/wiki/what-is-confidential-computing/threat-model#trusted-computing-base).
The build is reproducible.
This allows users to fully [verify the Privatemode service](https://docs.privatemode.ai/security#verifiability).

## License

You are allowed to inspect the code and build it for auditing and verification purposes. For details, see [LICENSE](LICENSE).

## Build and verify the container images

See the [Verification from source code](https://docs.privatemode.ai/guides/verify-source) guide in the Privatemode documentation.

## Build the desktop app

Ensure that the following programs are installed:

- [Nushell](https://www.nushell.sh/)
- [NodeJS / NPM](https://nodejs.org/en)

before building the app with the following command:

```bash
./scripts/build-app.nu v<x>.<y>.<z> <target>
```

Where `v<x>.<y>.<z>` corresponds to the version number of the release (e.g. v1.30.0).

`<target>` can be one of the following:

- `rpm`: RPM package for RedHat-based Linux distributions
- `deb`: Debian package for Debian-based Linux distributions
- `dmg`: MacOS disk image installer
- `msix`: Windows installer
