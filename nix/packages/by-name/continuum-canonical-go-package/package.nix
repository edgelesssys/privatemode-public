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

  vendorHash = "sha256-C8yAPDd/PNCqUCCCPFJ8sHZnmwvkIxCPkZQC4MH08RI=";

  doCheck = false;

  proxyVendor = true;
}
