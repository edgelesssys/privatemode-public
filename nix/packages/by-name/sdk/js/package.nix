{
  lib,
  stdenv,
  nodejs,
  pnpm_11,
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
    inherit (finalAttrs) pname version src;
    fetcherVersion = 4;
    sourceRoot = "${finalAttrs.src.name}/sdk/js";
    hash = "sha256-tS9aa8R+6VqLa9EsSpgzr3KOLvCN2PlQZU8G4gHf/PQ=";
  };

  nativeBuildInputs = [
    nodejs
    pnpmConfigHook
    pnpm_11
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
