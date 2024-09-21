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

  vendorHash = "sha256-0lCJgBpkoIUCsfPxPNkRIOgp6k3PyuJTZ5NIL+WEtvo=";

  meta.mainProgram = pname;
}
