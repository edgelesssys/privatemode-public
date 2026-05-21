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

  vendorHash = "sha256-bIxVxk8iH1IyAAZzQdDh5eEZeRGdxBHpC3ANj3m7InE=";

  doCheck = false;

  proxyVendor = true;
}
