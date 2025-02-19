{
  stdenv,
  fetchurl,
}:
stdenv.mkDerivation {
  pname = "grpc_health_probe";
  version = "0.4.25";

  src = fetchurl {
    url = "https://github.com/grpc-ecosystem/grpc-health-probe/releases/download/v0.4.25/grpc_health_probe-linux-amd64";
    hash = "sha256-0UA3rZRRjqyNvlfBRtbCyoCPfzJgDuDEBX70sD7g5C4=";
  };

  dontUnpack = true;

  postInstall = ''
    mkdir -p $out/bin
    cp $src $out/bin/grpc_health_probe
    chmod +x $out/bin/grpc_health_probe
  '';
}
