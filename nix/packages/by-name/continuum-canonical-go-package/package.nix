{
  lib,
  buildGo125Module,
}:
buildGo125Module {
  pname = "continuum-canonical-go-package";
  version = lib.continuumVersion;

  src = lib.continuumRepoRootSrc [
    "go.mod"
    "go.sum"
  ];

  vendorHash = "sha256-o27qJayBRw22MYtiFOnRPkxAABEoDB+o5rvtchY9KZU=";

  doCheck = false;

  proxyVendor = true;
}
