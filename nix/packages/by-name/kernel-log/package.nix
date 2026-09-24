{
  lib,
  buildContinuumGoModule,
  dockerTools,
}:
rec {
  bin = buildContinuumGoModule {
    pname = "kernel-log";
    version = lib.continuumVersion;

    src = lib.continuumRepoRootSrc [
      "go.mod"
      "go.sum"
      "misc/kernel-log"
    ];

    subPackages = [ "misc/kernel-log" ];

    meta.mainProgram = "kernel-log";
  };

  image = dockerTools.buildLayeredImage {
    name = "kernel-log";
    tag = lib.continuumVersion;
    compressor = "zstd";

    contents = [ bin ];

    config.Entrypoint = [ "${bin}/bin/kernel-log" ];
  };
}
