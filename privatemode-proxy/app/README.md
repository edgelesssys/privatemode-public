# README

## About

This is the official Wails Svelte-TS template.

## Initial Setup

The frontend code is consumed through a submodule, that needs to be initialized / updated:

```bash
git submodule update --init
```

## Live Development

```bash
VITE_DEFAULT_MODEL="latest" VITE_API_BASE="" wails dev
```

## Building

Adjust the platform variable to the one you want to build for:

```bash
just build-desktop-app darwin/arm64
```

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
