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

      systemdService = lib.mkOption {
        description = "Systemd service config";
        type = lib.types.submodule {
          default = { };
          options = {
            restartSec = lib.mkOption {
              description = "RestartSec";
              type = with lib.types; nullOr int;
              default = 10;
            };
            restartSteps = lib.mkOption {
              description = "RestartSteps";
              type = with lib.types; nullOr int;
              default = 10;
            };
            restartMaxDelaySec = lib.mkOption {
              description = "RestartMaxDelaySec";
              type = with lib.types; nullOr int;
              default = 300;
            };

            # startLimitIntervalSec = lib.mkOption {
            #   description = "StartLimitIntervalSec";
            #   type = with lib.types; nullOr int;
            #   default = 60;
            # };
            # startLimitBurst = lib.mkOption {
            #   description = "StartLimitBurst";
            #   type = with lib.types; nullOr int;
            #   default = 10;
            # };
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
        serviceConfig = {
          Restart = "on-failure";
          RestartSec = cfg.systemdService.restartSec;
          RestartSteps = cfg.systemdService.restartSteps;
          RestartMaxDelaySec = cfg.systemdService.restartMaxDelaySec;

          # startLimitIntervalSec = cfg.systemdService.startLimitIntervalSec;
          # startLimitBurst = cfg.systemdService.startLimitBurst;

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
