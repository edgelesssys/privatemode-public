{ pkgs }:
# On macOS, run contrast via Docker.
if pkgs.stdenv.isDarwin then
  pkgs.writeShellScriptBin "contrast" ''
    tmp_dir="$(mktemp -d "''${TMPDIR:-/tmp}/contrast-tmp.XXXXXX")"
    cleanup() { rm -rf "$tmp_dir"; }
    trap cleanup EXIT
    docker run --rm --platform linux/amd64 -v .:/continuum -v "$tmp_dir":/tmp --workdir=/continuum contrast-cli:${pkgs.contrast.contrastVersion} "$@"
  ''
else
  pkgs.contrast
