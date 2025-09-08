{
  linkFarmFromDrvs,
  writeShellScriptBin,
  writeShellApplication,
  contrast,
  attestation-agent,
  disk-mounter,
  inference-proxy,
  privatemode-proxy,
  secret-service,
  pkgsCross,
  skopeo,
  crane,
  stdenvNoCC,
  mkMultiplatformOCIImage,
  lib,
  replaceVars,

  # Optional inputs - these are not available in the OSS build.
  apigateway ? null,
  gpu-injector ? null,
  fake-workload ? null,
  rim-cache ? null,
}:
let
  privatemode-proxy-amd64 = privatemode-proxy.image.overrideAttrs (_: {
    name = "privatemode-proxy-amd64.tar.gz";
  });

  privatemode-proxy-arm64 =
    pkgsCross.aarch64-multiplatform.privatemode-proxy.image.overrideAttrs
      (_: {
        name = "privatemode-proxy-arm64.tar.gz";
      });

  privatemode-proxy-multiplatform = mkMultiplatformOCIImage {
    name = "privatemode-proxy";
    imageArchPairs = [
      {
        arch = "amd64";
        oci-dir = toOciImage privatemode-proxy-amd64;
      }
      {
        arch = "arm64";
        oci-dir = toOciImage privatemode-proxy-arm64;
      }
    ];
  };

  oss-images = [
    attestation-agent.image
    disk-mounter.image
    inference-proxy.image
    secret-service.image
  ];

  images = oss-images ++ [
    apigateway.image
    gpu-injector.image
    fake-workload.image
    rim-cache.image
    contrast.image
  ];

  toOciImage =
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
    };

  docker-images = linkFarmFromDrvs "privatemode-docker-images" (
    images
    ++ [
      privatemode-proxy-amd64
      contrast.image
    ]
  );

  oci-images = linkFarmFromDrvs "privatemode-oci-images" (
    (map toOciImage images) ++ [ privatemode-proxy-multiplatform ]
  );

  oss-oci-images = linkFarmFromDrvs "privatemode-oss-oci-images" (
    (map toOciImage oss-images) ++ [ privatemode-proxy-multiplatform ]
  );

  load-images = writeShellScriptBin "load-images" ''
    echo -e "⚠️  \033[0;31mImages imported via Docker are NOT reproducible due to Docker's manifest format.\033[0m ⚠️"
    echo -e "\033[0;31mYou should NOT push images via Docker.\033[0m"

    for image in ${docker-images}/*; do
      docker load < "$image"
    done
  '';

  push-images = writeShellApplication {
    name = "push-images";
    runtimeInputs = [ crane ];
    text = builtins.readFile (
      replaceVars ./push-container-images.sh {
        IMAGE_DIR = oci-images;
      }
    );
  };
in
oci-images
// {
  inherit
    load-images
    push-images
    docker-images
    oss-oci-images
    ;
}
