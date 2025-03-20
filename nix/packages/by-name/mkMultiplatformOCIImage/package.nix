{
  stdenvNoCC,
  writeShellApplication,
  jq,
}:
{
  imageArchPairs,
  name,
}:
let
  build-multiplatform-oci-image = writeShellApplication {
    name = "build-multiplatform-oci-image";
    runtimeInputs = [ jq ];
    text = builtins.readFile ./build-multiplatform-oci-image.sh;
  };
in
stdenvNoCC.mkDerivation {
  inherit name;

  nativeBuildInputs = [ build-multiplatform-oci-image ];

  dontUnpack = true;

  buildPhase = ''
    runHook preBuild

    mkdir -p $out

    build-multiplatform-oci-image $out <<EOF
    ${builtins.concatStringsSep "\n" (map (pair: "${pair.arch} ${pair.oci-dir}") imageArchPairs)}
    EOF

    runHook postBuild
  '';
}
