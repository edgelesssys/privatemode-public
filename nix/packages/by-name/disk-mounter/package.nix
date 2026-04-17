{
  lib,
  bash,
  buildEnv,
  coreutils,
  cryptsetup,
  dockerTools,
  util-linux,
  writeShellApplication,
}:
rec {
  bin = writeShellApplication {
    name = "disk-mounter";

    runtimeInputs = [
      util-linux
      cryptsetup
      coreutils
    ];

    text = builtins.readFile ./disk-mounter.sh;
  };

  image = dockerTools.buildLayeredImage {
    name = "disk-mounter";
    tag = lib.continuumVersion;
    maxLayers = 8;
    compressor = "zstd";

    contents = buildEnv {
      name = "image-root";
      paths = [
        bin
        bash
      ];
      pathsToLink = [ "/bin" ];
    };

    config = {
      Entrypoint = [ "${bin}/bin/disk-mounter" ];
    };
  };
}
