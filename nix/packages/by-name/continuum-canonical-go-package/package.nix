{
  lib,
  buildGo124Module,
}:
buildGo124Module {
  pname = "continuum-canonical-go-package";
  version = lib.continuumVersion;

  src = lib.continuumRepoRootSrc [
    "go.mod"
    "go.sum"
  ];

  vendorHash = "sha256-SmGCqvNzz8NZLaDC4u5MHOZx8KMUpTDIGD2yXi7OGwE=";

  doCheck = false;

  proxyVendor = true;
}
