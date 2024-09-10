{ config, lib, pkgs, ... }: {
  options = {
    services.nixpkgs-update-notifier = {
      enable = lib.mkEnableOption "nixpkgs-update-notifier";
    };
  };
  config =
    let cfg = config.services.nixpkgs-update-notifier;
    in lib.mkIf cfg.enable {
      systemd.services.nixpkgs-update-notifier = {
        wantedBy = [ "multi-user.target" ];
        after = [ "network.target" ];
        serviceConfig = {
          DynamicUser = true;
          ExecStart = "${pkgs.nixpkgs-update-notifier}/bin/nixpkgs-update-notifier --help";
        };
      };
    };
}
