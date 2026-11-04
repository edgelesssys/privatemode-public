{
  lib,
  buildGo127Module,
}:
buildGo127Module {
  pname = "continuum-canonical-go-package";
  version = lib.continuumVersion;

  src = lib.continuumRepoRootSrc [
    "go.mod"
    "go.sum"
  ];

  vendorHash = "sha256-m0FwhA+Ot1LbrXwWdwavjhlE44eF0WNuarSLqpgvWIk=";

  doCheck = false;

  proxyVendor = true;
}
