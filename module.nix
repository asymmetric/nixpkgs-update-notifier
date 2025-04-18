{
  config,
  lib,
  pkgs,
  ...
}:
{
  options = {
    services.nixpkgs-update-notifier = {
      enable = lib.mkEnableOption "nixpkgs-update-notifier";
      username = lib.mkOption { type = lib.types.str; };
      passwordFile = lib.mkOption { type = lib.types.path; };
      dataDir = lib.mkOption {
        type = lib.types.path;
        default = "/var/lib/nixpkgs-update-notifier";
      };
      timers = lib.mkOption {
        description = "Timers";
        type = lib.types.submodule {
          options = {
            update = lib.mkOption {
              type = lib.types.nullOr lib.types.str;
              default = null;
            };
            jsblob = lib.mkOption {
              type = lib.types.nullOr lib.types.str;
              default = null;
            };
          };
        };
      };
      debug = lib.mkOption {
        type = lib.types.bool;
        default = false;
      };

      service = lib.mkOption {
        type = lib.types.submodule {
          options = {
            startLimitIntervalSec = lib.mkOption {
              type = lib.types.int;
              default = 60;
            };
            startLimitBurst = lib.mkOption {
              type = lib.types.int;
              default = 10;
            };
          };
        };
      };
    };
  };

  config =
    let
      cfg = config.services.nixpkgs-update-notifier;
    in
    lib.mkIf cfg.enable {
      systemd.services.nixpkgs-update-notifier = {
        wantedBy = [ "multi-user.target" ];
        after = [ "network.target" ];
        startLimitIntervalSec = cfg.service.startLimitIntervalSec;
        startLimitBurst = cfg.service.startLimitBurst;
        serviceConfig = {
          Restart = "on-failure";
          # emitted by `fatal`
          RestartPreventExitStatus = [ 100 ];
          EnvironmentFile = cfg.passwordFile;
          ExecStart = toString [
            (lib.getExe pkgs.nixpkgs-update-notifier)
            "-matrix.username ${cfg.username}"
            "-db ${cfg.dataDir}/data.db"
            (lib.optionalString (cfg.timers.update != null) "-timers.update ${cfg.timers.update}")
            (lib.optionalString (cfg.timers.jsblob != null) "-timers.jsblob ${cfg.timers.jsblob}")
            (lib.optionalString cfg.debug "-debug")
          ];
          StateDirectory = "nixpkgs-update-notifier";
        };
      };
    };
}
