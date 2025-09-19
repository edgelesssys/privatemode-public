{
  skopeo,
  lib,
  stdenvNoCC,
}:
docker-tarball:
let
  isZst = lib.strings.hasSuffix ".tar.zst" docker-tarball.name;
in
stdenvNoCC.mkDerivation {
  name = "${lib.strings.removeSuffix (if isZst then ".tar.zst" else ".tar.gz") docker-tarball.name}";
  src = docker-tarball;
  dontUnpack = true;
  nativeBuildInputs = [ skopeo ];
  buildPhase = ''
    runHook preBuild
    skopeo copy docker-archive:$src oci:$out --insecure-policy --tmpdir . ${lib.optionalString isZst "--dest-compress-format=zstd"}
    runHook postBuild
  '';
}
