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

  vendorHash = "sha256-tjY1Fl2pOs0FcVRKXzaERM18kSHAn9IdXw1BDSV7ThE=";

  meta.mainProgram = pname;
}
