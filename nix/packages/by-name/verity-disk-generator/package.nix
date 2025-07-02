{
  lib,
  pkgs,
}:
let
  util-linuxMinimal = pkgs.util-linux.override {
    systemdSupport = false;
  };
  verity_sh = pkgs.writeShellApplication {
    name = "verity.sh";
    bashOptions = [ ];
    runtimeInputs = with pkgs; [
      gitMinimal
      git-lfs
      util-linuxMinimal
      coreutils
      gnused
      findutils
      gptfdisk
      systemd
      erofs-utils
      cryptsetup
    ];
    text = builtins.readFile (lib.path.append lib.continuumRepoRoot "verity/verity.sh");
  };
  repartDir = pkgs.runCommand "repart-dir" { } ''
    mkdir -p $out/repart
    cp -r ${lib.path.append lib.continuumRepoRoot "verity/repart/"}/* $out/repart/
  '';
in
pkgs.dockerTools.buildImage {
  name = "verity-disk-generator";
  tag = lib.continuumVersion;

  copyToRoot = pkgs.buildEnv {
    name = "image-root";
    paths = [
      pkgs.bash
      pkgs.coreutils
      pkgs.dockerTools.caCertificates
      verity_sh
      repartDir
    ];
    pathsToLink = [
      "/etc"
      "/bin" # /bin/sh is required by git post-checkout hooks
      "/repart"
    ];
  };
  config = {
    Entrypoint = [ "${lib.getExe verity_sh}" ];
  };
}
