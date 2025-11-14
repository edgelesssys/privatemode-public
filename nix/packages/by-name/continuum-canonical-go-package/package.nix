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

  vendorHash = "sha256-zRU7wrZ+pbvdRIKSumMKgD4zB1cYZUP6u0KZUUHR0go=";

  doCheck = false;

  proxyVendor = true;
}
