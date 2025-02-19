# Returns a package set originating from the root of the Continuum repository.
# The `files` attribute is a list of paths relative to the root of the repository.

{ lib }:
files:
let
  filteredFiles = lib.map (subpath: lib.path.append lib.continuumRepoRoot subpath) files;
in
lib.fileset.toSource {
  root = lib.continuumRepoRoot;
  fileset = lib.fileset.unions filteredFiles;
}
