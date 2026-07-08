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

  vendorHash = "sha256-oi6n/xPGfD0s49eD8FX8w+L24lFDFGLM8XycejDcdB4=";

  doCheck = false;

  proxyVendor = true;
}
