# Returns the current Continuum version, as defined in `version.nix`.

{ lib }:
let
  versionFile = import ../../../../version.nix;

  version =
    if (lib.hasAttr "version" versionFile) then
      versionFile.version
    else
      builtins.throw "The `version` attribute must be set in `version.nix`";
in
version
