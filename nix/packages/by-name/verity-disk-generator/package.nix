{
  coreutils,
  cryptsetup,
  erofs-utils,
  gawk,
  git-lfs,
  gitMinimal,
  lib,
  pkgs,
  util-linuxMinimal,
}:
let
  verity_sh = pkgs.writeShellApplication {
    name = "verity.sh";
    bashOptions = [ ];
    runtimeInputs = [
      coreutils
      cryptsetup
      erofs-utils
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
