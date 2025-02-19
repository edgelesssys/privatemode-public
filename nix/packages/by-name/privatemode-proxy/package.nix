{
  lib,
  buildContinuumGoModule,
  dockerTools,
  buildEnv,
}:
let
  privatemode-proxy = buildContinuumGoModule {
    pname = "privatemode-proxy";
    version = lib.continuumVersion;
    tags = [
      "contrast_unstable_api"
    ];

    src = lib.continuumRepoRootSrc [
      "go.mod"
      "go.sum"
      "privatemode-proxy"
      "internal/gpl"
    ];

    ldflags = [
      "-X 'github.com/edgelesssys/continuum/internal/gpl/constants.version=${lib.continuumVersion}'"
    ];

    subPackages = [ "privatemode-proxy" ];

    meta.mainProgram = "privatemode-proxy";
  };
in
rec {
  bin = privatemode-proxy;

  image = dockerTools.buildLayeredImage {
    name = "privatemode-proxy";
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
    config.Entrypoint = [ "/bin/privatemode-proxy" ];
  };
}
