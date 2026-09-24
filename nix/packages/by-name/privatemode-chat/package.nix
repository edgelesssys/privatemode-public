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
  pname = "privatemode-chat";
  version = lib.continuumVersion;

  src = lib.continuumRepoRootSrc [
    "chat"
    "sdk/js"
  ];
  pnpmRoot = "chat";

  pnpmDeps = fetchPnpmDeps {
    inherit (finalAttrs) pname version src;
    fetcherVersion = 4;
    sourceRoot = "${finalAttrs.src.name}/chat";
    hash = "sha256-o6UHKwXiWV1jKOs2mcVM96s3mWeNHbS0xwe347K7Qcc=";
  };

  nativeBuildInputs = [
    nodejs
    pnpmConfigHook
    pnpm_11
  ];

  buildPhase = ''
    runHook preBuild

    # Hacky way to pass the VITE_ environment variables into Nix.
    # TODO(msanft): Find a cleaner way to do this.
    set -a
    source chat/.env
    set +a

    rm chat/static/privatemode.wasm
    if [[ "''${LOCAL_BUILD:-}" == "true" ]]; then
      cp ${sdk.wasm-internal}/bin/privatemode.wasm chat/static/privatemode.wasm
    fi

    # pnpmConfigHook breaks the symlink, so we need to create it ourselves.
    # Vite needs package.json (for exports/main) and dist/ (built output).
    rm chat/node_modules/privatemode-ai
    ln -s ../../sdk/js chat/node_modules/privatemode-ai
    ln -s ${sdk.js}/share/dist chat/node_modules/privatemode-ai/dist

    pnpm --dir chat run build

    runHook postBuild
  '';

  installPhase = ''
    runHook preInstall

    mkdir -p $out/share/
    cp -r chat/build/. $out/share/

    runHook postInstall
  '';

  passthru.deps = finalAttrs.pnpmDeps;
})
