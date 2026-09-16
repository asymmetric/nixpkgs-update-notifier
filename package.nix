{ pkgs, version, ... }:

pkgs.buildGo127Module rec {
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

  vendorHash = "sha256-guctodo8VX6HZG4OYKXPSU6eaSXGrjrvkcgPkBVyirU=";

  meta.mainProgram = pname;
}
