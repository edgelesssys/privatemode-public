{
  lib,
  buildContinuumGoModule,
}:
(buildContinuumGoModule {
  pname = "privatemode-internal-wasm-sdk";
  version = lib.continuumVersion;

  src = lib.continuumRepoRootSrc [
    "go.mod"
    "go.sum"
    "sdk/wasm"
    "internal/oss"
  ];

  tags = [
    "contrast_unstable_api"
  ];

  # Go Wasm binaries have references to file paths of the Go compiler.
  # These are allowed since they're obviously not used in the browser Wasm environment.
  allowGoReference = true;

  ldflags = [
    "-X 'github.com/edgelesssys/continuum/internal/oss/constants.version=${lib.continuumVersion}'"
  ];

  subPackages = [ "sdk/wasm" ];

  postInstall = ''
    mv $out/bin/js_wasm/wasm $out/bin/privatemode.wasm
    rm -rf $out/bin/js_wasm
  '';
}).overrideAttrs
  (
    _final: _prev: {
      env.CGO_ENABLED = "0";
      env.GOOS = "js";
      env.GOARCH = "wasm";
    }
  )
