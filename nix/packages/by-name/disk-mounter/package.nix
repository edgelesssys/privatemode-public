{
  lib,
  buildContinuumGoModule,
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
      "internal/gpl/constants"
      "internal/gpl/logging"
      "internal/gpl/process"
      "disk-mounter"
    ];

    nativeBuildInputs = [
      makeWrapper
    ];

    subPackages = [ "disk-mounter" ];

    ldflags = [
      "-extldflags=-Wl,-z,lazy"
      "-X 'github.com/edgelesssys/continuum/internal/gpl/constants.version=${lib.continuumVersion}'"
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

    contents = builtins.attrValues {
      inherit bin;
    };

    config = {
      Entrypoint = [ "${bin}/bin/disk-mounter" ];
    };
  };
}
