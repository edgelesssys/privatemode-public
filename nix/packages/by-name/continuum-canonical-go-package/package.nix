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

  vendorHash = "sha256-lldrB6txABkuEXoEg8Fe2oqAyDq2AXXbJT++vE64UPg=";

  doCheck = false;

  proxyVendor = true;
}
