{
  lib,
  bash,
  buildContinuumGoModule,
  buildEnv,
  coreutils,
  cryptsetup,
  dockerTools,
  makeWrapper,
  util-linux,
}:
rec {
  bin = buildContinuumGoModule {
    version = lib.continuumVersion;

    pname = "disk-mounter";

    src = lib.continuumRepoRootSrc [
      "go.mod"
      "go.sum"
      "internal/oss/constants"
      "internal/oss/logging"
      "internal/oss/process"
      "disk-mounter"
    ];

    nativeBuildInputs = [
      makeWrapper
    ];

    subPackages = [ "disk-mounter" ];

    ldflags = [
      "-extldflags=-Wl,-z,lazy"
      "-X 'github.com/edgelesssys/continuum/internal/oss/constants.version=${lib.continuumVersion}'"
    ];

    postFixup = ''
      wrapProgram $out/bin/disk-mounter \
        --set PATH ${
          lib.makeBinPath [
            # for veritysetup
            cryptsetup
            # for mount, lsblk
            util-linux
            # for mknod
            coreutils
          ]
        }
    '';

    meta.mainProgram = "disk-mounter";
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
        util-linux
      ];
    };

    config = {
      Entrypoint = [ "${bin}/bin/disk-mounter" ];
    };
  };
}
