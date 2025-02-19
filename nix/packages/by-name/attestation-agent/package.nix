{
  lib,
  buildContinuumGoModule,
  addDriverRunpath,
  gcc13,
  patchelf,
  dockerTools,
  coreutils,
}:
let
  attestation-agent = buildContinuumGoModule {
    pname = "attestation-agent";
    version = lib.continuumVersion;

    src = lib.continuumRepoRootSrc [
      "go.mod"
      "go.sum"
      "internal"
      "attestation-agent"
    ];

    nativeBuildInputs = [
      gcc13
      patchelf
    ];

    subPackages = [ "attestation-agent" ];

    env.CGO_ENABLED = "1";

    ldflags = [
      "-extldflags=-Wl,-z,lazy"
      "-X 'github.com/edgelesssys/continuum/internal/gpl/constants.version=${lib.continuumVersion}'"
    ];

    tags = [ "gpu" ];

    postFixup = ''
      # NixOS (OS Image) specific fix. On NixOS, all NVIDIA libraries will be linked to the path below for compatibility.
      patchelf $out/bin/attestation-agent \
        --add-rpath ${addDriverRunpath.driverLink}/lib
    '';

    meta.mainProgram = "attestation-agent";
  };
in
rec {
  bin = attestation-agent;

  # The Agent image needs to be started with the following flag to mount in the host's libnvidia-ml.so.1. It's not possible to bake it in
  # to the Docker image, as it is specific to the host's kernel and NVIDIA driver.
  # -v '/run/opengl-driver/lib:/run/opengl-driver/lib'
  image = dockerTools.buildLayeredImage {
    name = "attestation-agent";
    tag = lib.continuumVersion;
    maxLayers = 8;

    contents = builtins.attrValues {
      inherit (dockerTools) caCertificates binSh;
      inherit bin;
      # for ln
      inherit coreutils;
    };

    config = {
      Entrypoint = [ "${bin}/bin/attestation-agent" ];
    };
  };
}
