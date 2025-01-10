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
        ./testdata
        (fs.fileFilter (file: file.hasExt "go") root)
      ];
    };

  vendorHash = "sha256-5BMTo5/0gESGLWRDHdVGnPsQuTxH0RBFgs6TEg3rbbU=";

  meta.mainProgram = pname;
}
