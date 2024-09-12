{ pkgs, version, ... }:

pkgs.buildGoModule rec {
  pname = "nixpkgs-update-notifier";
  inherit version;

  # TODO convert to whitelist
  src =
    let fs = pkgs.lib.fileset; in
    fs.toSource {
      root = ./.;
      fileset = fs.unions [
        ./COPYING
        ./README.md
        ./go.mod
        ./go.sum
        ./main.go
      ];
    };

  vendorHash = "sha256-jclP3ZgEe3xLDqNvQFs3tZIwtN3Mj4lumvG9lQVWb4Y=";

  meta.mainProgram = pname;
}
