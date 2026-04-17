{
  coreutils,
  cryptsetup,
  erofs-utils,
  fetchurl,
  gawk,
  git-lfs,
  gitMinimal,
  lib,
  pkgs,
  util-linuxMinimal,
}:
let
  # Cryptsetup and erofs-utils are the only dependencies that should
  # influence the model disk build, therefore we explictly pin them
  # here. Should we ever require to update these, e.g. in case of a
  # security update in either of them, we need to a) recreate all used
  # model disk in the cluster / charts/models.yaml, or b) add a
  # separate build with the new versions and edit the verification docs
  # to mention the new version gate.
  cryptsetup_2_8_4 = cryptsetup.overrideAttrs (_old: {
    version = "2.8.4";
    src = fetchurl {
      url = "mirror://kernel/linux/utils/cryptsetup/v2.8/cryptsetup-2.8.4.tar.xz";
      hash = "sha256-RD5G+JZMmsx4D0Va+7jiOqDo7X7FBM/FngT0BvoeioM=";
    };
  });

  erofs-utils_1_9 = erofs-utils.overrideAttrs (_old: {
    # Even thoug models.yaml says 1.9, we used erofs-utils 1.9.1 for
    # creating the model disks. The wrong version was inserted as the
    # VERSION file wasn't updated upstream, causing erofs-utils 1.9
    # and 1.9.1 to report the same 1.9 version.
    # See: https://github.com/erofs/erofs-utils/commits/b0df634780792cf3678a317b349ddc090d5e8daa/VERSION
    version = "1.9.1";
    src = fetchurl {
      url = "https://git.kernel.org/pub/scm/linux/kernel/git/xiang/erofs-utils.git/snapshot/erofs-utils-1.9.1.tar.gz";
      hash = "sha256-qe9atnxLjS0+ntcfOc0Ai9plMUKnINijlaNvERDQxDI=";
    };
  });

  verity_sh = pkgs.writeShellApplication {
    name = "verity.sh";
    bashOptions = [ ];
    runtimeInputs = [
      coreutils
      cryptsetup_2_8_4
      erofs-utils_1_9
      gawk
      gitMinimal
      git-lfs
      util-linuxMinimal
    ];
    text = builtins.readFile (lib.path.append lib.continuumRepoRoot "verity/verity.sh");
  };

  normalize_model_name_sh = pkgs.writeShellApplication {
    name = "normalize-model-name.sh";
    bashOptions = [ ];
    runtimeInputs = [
      coreutils
    ];
    text = builtins.readFile (lib.path.append lib.continuumRepoRoot "verity/normalize-model-name.sh");
  };
in
pkgs.dockerTools.buildImage {
  name = "verity-disk-generator";
  tag = lib.continuumVersion;
  compressor = "zstd";

  copyToRoot = pkgs.buildEnv {
    name = "image-root";
    paths = [
      pkgs.bash
      pkgs.coreutils
      pkgs.dockerTools.caCertificates
      verity_sh
      normalize_model_name_sh
    ];
    pathsToLink = [
      "/etc"
      "/bin" # /bin/sh is required by git post-checkout hooks
    ];
  };

  extraCommands = "mkdir -m 1777 tmp";

  config = {
    Entrypoint = [ "${lib.getExe verity_sh}" ];
  };
}
