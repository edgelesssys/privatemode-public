{
  lib,
  stdenv,
  nodejs,
  pnpm_10,
  pnpmConfigHook,
  fetchPnpmDeps,
}:
stdenv.mkDerivation (finalAttrs: {
  pname = "privatemode-internal-js-sdk";
  version = lib.continuumVersion;

  src = lib.continuumRepoRootSrc [
    "sdk/js"
  ];
  pnpmRoot = "sdk/js";

  pnpmDeps = fetchPnpmDeps {
    inherit (finalAttrs) pname version src;
    fetcherVersion = 3;
    sourceRoot = "${finalAttrs.src.name}/sdk/js";
    hash = "sha256-CDx0C2DdVd//ImEQPcaNLFKo0Yui/TsAMBMa1PJS3Rs=";
  };

  nativeBuildInputs = [
    nodejs
    pnpmConfigHook
    pnpm_10
  ];

  buildPhase = ''
    runHook preBuild

    pnpm --dir sdk/js run build

    runHook postBuild
  '';

  installPhase = ''
    runHook preInstall

    mkdir -p $out/share/
    cp -r sdk/js/dist/* $out/share/

    # TODO(msanft): Bundle the SDK here?

    runHook postInstall
  '';
})
