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

  vendorHash = "sha256-KHUH+5WcoincXdiGfa+lbk1UnyUB9MQUQyUVxv3w8O8=";

  doCheck = false;

  proxyVendor = true;
}
