{ pkgs, version, ... }:

pkgs.buildGoModule rec {
  pname = "nixpkgs-update-notifier";
  inherit version;

  src =
    let fs = pkgs.lib.fileset; in
    fs.toSource rec {
      root = ./.;
      fileset = fs.unions [
        ./COPYING
        ./README.md
        ./go.mod
        ./go.sum
        ./db
        (fs.fileFilter (file: file.hasExt "go") root)
      ];
    };

  vendorHash = "sha256-0lCJgBpkoIUCsfPxPNkRIOgp6k3PyuJTZ5NIL+WEtvo=";

  meta.mainProgram = pname;
}
