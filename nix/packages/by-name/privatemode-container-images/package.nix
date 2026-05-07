{
  linkFarmFromDrvs,
  writeShellScriptBin,
  writeShellApplication,
  pkgsCross,
  crane,
  mkMultiplatformOCIImage,
  toOciImage,
  replaceVars,
  linuxPkgs,
}:
let
  privatemode-proxy-amd64 = linuxPkgs.privatemode-proxy.image.overrideAttrs (_: {
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
    linuxPkgs.attestation-agent.image
    linuxPkgs.disk-mounter.image
    linuxPkgs.inference-proxy.image
    linuxPkgs.secret-service.image
  ];

  images = oss-images ++ [
    linuxPkgs.apigateway.image
    linuxPkgs.disk-daemon.image
    linuxPkgs.fake-workload.image
    linuxPkgs.rim-cache.image
    linuxPkgs.contrast.image
    linuxPkgs.availability-test.image
  ];

  docker-images = linkFarmFromDrvs "privatemode-docker-images" (
    images
    ++ [
      privatemode-proxy-amd64
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
