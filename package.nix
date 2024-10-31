{ pkgs }:
pkgs.buildGoModule {
  pname = "autobeam";
  version = "0";
  src = ./.;
  vendorHash = "sha256-Vn4WZzPWuSB9vzKVUAl4j4YmEorwm+4ljCXIJIG+yhM=";
}
