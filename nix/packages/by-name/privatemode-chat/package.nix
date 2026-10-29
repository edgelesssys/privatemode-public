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
  pname = "privatemode-chat";
  version = lib.continuumVersion;

  src = lib.continuumRepoRootSrc [
    "chat"
    "sdk/js"
    "internal/anonymous/policy.json"
  ];
  pnpmRoot = "chat";

  pnpmDeps = fetchPnpmDeps {
    inherit (finalAttrs) pname version;
    src = lib.continuumRepoRootSrc [
      "chat/.npmrc"
      "chat/package.json"
      "chat/pnpm-lock.yaml"
      "chat/pnpm-workspace.yaml"
    ];
    fetcherVersion = 4;
    sourceRoot = "${finalAttrs.src.name}/chat";
    hash = "sha256-d4htrcb2pqBytXqBxj58JUfxW2QFNsmFMSIrSCLZDnw=";
  };

  nativeBuildInputs = [
    nodejs
    pnpmConfigHook
    pnpm_12
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
