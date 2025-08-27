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

  vendorHash = "sha256-NsyL5JxXhEgdoIE2mCVeh+6WrQWjRu7c4QKPsxI4erc=";

  doCheck = false;

  proxyVendor = true;
}
