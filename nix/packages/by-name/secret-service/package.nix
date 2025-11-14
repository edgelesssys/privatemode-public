{
  lib,
  bash,
  buildContinuumGoModule,
  buildEnv,
  dockerTools,
  grpc-health-probe,
}:
let
  secret-service = buildContinuumGoModule {
    pname = "secret-service";
    version = lib.continuumVersion;

    src = lib.continuumRepoRootSrc [
      "go.mod"
      "go.sum"
      "secret-service"
      "internal/crypto"
      "internal/oss"
    ];

    ldflags = [
      "-X 'github.com/edgelesssys/continuum/internal/oss/constants.version=${lib.continuumVersion}'"
    ];

    subPackages = [ "secret-service" ];

    meta.mainProgram = "secret-service";
  };
in
rec {
  bin = secret-service;

  image = dockerTools.buildLayeredImage {
    name = "secret-service";
    tag = lib.continuumVersion;
    compressor = "zstd";

    contents = buildEnv {
      name = "image-root";
      paths = [
        bin
        bash
        grpc-health-probe
        dockerTools.caCertificates
      ];
      pathsToLink = [
        "/bin"
      ];
    };

    config = {
      Entrypoint = [ "${bin}/bin/secret-service" ];
      Healthcheck = {
        Test = [
          "CMD"
          "${grpc-health-probe}/bin/grpc_health_probe"
          "--addr=localhost:3001"
        ];
        Interval = 30000000000;
        Timeout = 2000000000;
        Retries = 3;
      };
    };
  };
}
