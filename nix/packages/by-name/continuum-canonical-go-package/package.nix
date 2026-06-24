{
  lib,
  buildGo126Module,
}:
buildGo126Module {
  pname = "continuum-canonical-go-package";
  version = lib.continuumVersion;

  src = lib.continuumRepoRootSrc [
    "go.mod"
    "go.sum"
  ];

  vendorHash = "sha256-oDFr7XGoKdrgWwQyuPtz5cEQwWIxBTnRtIJSGoXfzNM=";

  doCheck = false;

  proxyVendor = true;
}
