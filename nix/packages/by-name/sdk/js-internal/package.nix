{
  lib,
  stdenv,
  nodejs,
  pnpm_11,
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
    fetcherVersion = 4;
    sourceRoot = "${finalAttrs.src.name}/sdk/js";
    hash = "sha256-U0b9hXSEsKSgZt8fEa/jpNXMdSVLT0z81Vu8taDtsX4=";
  };

  nativeBuildInputs = [
    nodejs
    pnpmConfigHook
    pnpm_11
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
