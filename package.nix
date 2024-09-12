{ pkgs, version, ... }:

pkgs.buildGoModule rec {
  pname = "nixpkgs-update-notifier";
  inherit version;

  src = pkgs.nix-gitignore.gitignoreSource [
    "flake.lock"
    "flake.nix"
    "module.nix"
    "package.nix"
  ] ./.;

  vendorHash = "sha256-jclP3ZgEe3xLDqNvQFs3tZIwtN3Mj4lumvG9lQVWb4Y=";

  meta.mainProgram = pname;
}
