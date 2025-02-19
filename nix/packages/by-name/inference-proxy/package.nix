{
  lib,
  buildContinuumGoModule,
  dockerTools,
  buildEnv,
}:
let
  inference-proxy = buildContinuumGoModule {
    pname = "inference-proxy";
    version = lib.continuumVersion;

    src = lib.continuumRepoRootSrc [
      "go.mod"
      "go.sum"
      "inference-proxy"
      "internal"
    ];

    ldflags = [
      "-X 'github.com/edgelesssys/continuum/internal/gpl/constants.version=${lib.continuumVersion}'"
    ];

    subPackages = [ "inference-proxy" ];

    meta.mainProgram = "inference-proxy";
  };
in
rec {
  bin = inference-proxy;

  image = dockerTools.buildLayeredImage {
    name = "inference-proxy";
    tag = lib.continuumVersion;

    contents = buildEnv {
      name = "image-root";
      paths = [
        bin
        dockerTools.caCertificates
      ];
      pathsToLink = [
        "/bin"
        "/etc"
      ];
    };

    config.Entrypoint = [ "${bin}/bin/inference-proxy" ];
  };
}
