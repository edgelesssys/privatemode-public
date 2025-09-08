# README

## About

This is the official Wails Svelte-TS template.

## Initial Setup

The frontend code is consumed through a submodule, that needs to be initialized / updated:

```bash
git submodule update --init
```

## Live Development

### Frontend

Running the app via wails allows to debug the frontend: in the app right-click -> inspect or run a browser against the wails server.

```bash
ldflags="-X github.com/edgelesssys/continuum/internal/gpl/constants.version=$(just print-version)"
VITE_VERSION=$(just print-version) wails dev -tags "contrast_unstable_api"  -ldflags="${ldflags}"
```

## Building

Adjust the platform variable to the one you want to build for:

```bash
just build-desktop-app darwin/arm64
```

## Configuration File

The native app has a configuration file to allow configuration of default settings such as the API KEY before use (see [Getting started](/docs/docs/guides/desktop-app.md#Getting-started)).

## Signing and Notarization on MacOS

Assuming an installation of [gon](https://github.com/Bearer/gon), this will also create `.dmg` file for installation:

```bash
AC_PASSWORD=<PWD> gon ./gon.json
```

Or the native way:

```bash
codesign --force --deep --verify --verbose --entitlements app.entitlements --options runtime --sign "Developer ID Application: Edgeless Systems GmbH (4D7MAN249M)" ./build/bin/Continuum.app
cd ./build/bin && ditto -c -k --keepParent Continuum.app continuum.zip
xcrun notarytool submit ./build/bin/continuum.zip --apple-id "as@edgeless.systems" --password <PWD> --team-id "4D7MAN249M" --wait
```
