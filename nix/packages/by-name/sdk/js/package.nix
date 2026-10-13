{
  lib,
  stdenv,
  nodejs,
  pnpm_12,
  pnpmConfigHook,
  fetchPnpmDeps,
  sdk,
}:
stdenv.mkDerivation (finalAttrs: {
  pname = "privatemode-js-sdk";
  version = lib.continuumVersion;

  src = lib.continuumRepoRootSrc [
    "sdk/js"
  ];
  pnpmRoot = "sdk/js";

  pnpmDeps = fetchPnpmDeps {
    inherit (finalAttrs) pname version;
    src = lib.continuumRepoRootSrc [
      "sdk/js/package.json"
      "sdk/js/pnpm-lock.yaml"
      "sdk/js/pnpm-workspace.yaml"
    ];
    fetcherVersion = 4;
    sourceRoot = "${finalAttrs.src.name}/sdk/js";
    hash = "sha256-eGK4p6WiyvDIXJmW0Q5p1aS3L+7MMGuuPHTgOGy+YYI=";
  };

  nativeBuildInputs = [
    nodejs
    pnpmConfigHook
    pnpm_12
  ];

  buildPhase = ''
    runHook preBuild

    PRIVATEMODE_WASM_PATH=${sdk.wasm-internal}/bin/privatemode.wasm pnpm --dir sdk/js run build

    runHook postBuild
  '';

  installPhase = ''
    runHook preInstall

    npm --prefix sdk/js pkg set \
      "version=${lib.removePrefix "v" finalAttrs.version}" \
      "repository.type=git" \
      "repository.url=git+https://github.com/edgelesssys/continuum.git" \
      "repository.directory=sdk/js"

    mkdir -p $out/share/dist/
    cp sdk/js/package.json sdk/js/README.md $out/share/
    cp -r sdk/js/dist/* $out/share/dist/

    runHook postInstall
  '';
})
