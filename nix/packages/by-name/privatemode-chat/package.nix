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
    "app/web"
    "sdk/js"
  ];
  pnpmRoot = "app/web";

  pnpmDeps = fetchPnpmDeps {
    inherit (finalAttrs) pname version src;
    fetcherVersion = 3;
    sourceRoot = "${finalAttrs.src.name}/app/web";
    hash = "sha256-81vN4VgK5qaWu6eDTsm7xVw9M7UTd6gfFaZjn3E0okg=";
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
    source app/web/.env
    set +a

    rm app/web/static/privatemode.wasm
    if [[ "''${LOCAL_BUILD:-}" == "true" ]]; then
      cp ${sdk.wasm-internal}/bin/privatemode.wasm app/web/static/privatemode.wasm
    fi

    # pnpmConfigHook breaks the symlink, so we need to create it ourselves.
    # Vite needs package.json (for exports/main) and dist/ (built output).
    rm app/web/node_modules/privatemode-ai
    ln -s ../../../sdk/js app/web/node_modules/privatemode-ai
    ln -s ${sdk.js-internal}/share app/web/node_modules/privatemode-ai/dist

    pnpm --dir app/web run build

    runHook postBuild
  '';

  installPhase = ''
    runHook preInstall

    mkdir -p $out/share/
    cp -r app/web/build/. $out/share/

    runHook postInstall
  '';

  passthru.deps = finalAttrs.pnpmDeps;
})
