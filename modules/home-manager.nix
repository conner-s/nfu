# home-manager module for nfu.
#
# Usage in your home-manager config:
#
#   imports = [ inputs.nfu.homeManagerModules.default ];
#   programs.nfu = {
#     enable = true;
#     theme = "gruvbox-dark";
#     settings = {
#       flakeRoot = "~/Projects/nixos-config";
#       commit = false;   # set true to auto-commit flake.lock after updates
#     };
#     # Declarative deploy hosts + projects for `nfu project` (optional).
#     hosts.Fabricator = { hostname = "192.0.2.10"; deployArgs = [ "--magic-rollback" "false" ]; };
#     projects.totality = { inputs = [ "totality" ]; hosts = [ "Fabricator" ]; };
#     # A bundle: one update, one rebuild, each host once.
#     projects.workstation = { includes = [ "totality" "npk" "nfu" ]; };
#   };
#
# `nfu totality` updates the totality input; `nfu project run totality` updates
# it, rebuilds the local host, then deploys .#Fabricator. Hosts land in
# ~/.config/nfu/config.toml; each project is written to
# ~/.config/nfu/projects/<name>.toml (read-only). `nfu project add` writes extra
# projects beside them.
self:
{ config, lib, pkgs, ... }:
let
  cfg = config.programs.nfu;
  tomlFormat = pkgs.formats.toml { };

  hostType = lib.types.submodule {
    options = {
      hostname = lib.mkOption {
        type = lib.types.str;
        default = "";
        description = "Informational address of the host (deploy-rs takes the real one from the flake's deploy.nodes).";
      };
      deployArgs = lib.mkOption {
        type = lib.types.listOf lib.types.str;
        default = [ ];
        example = [ "--magic-rollback" "false" ];
        description = "Extra arguments passed to `deploy` before the .#<host> installable.";
      };
    };
  };

  projectType = lib.types.submodule {
    options = {
      inputs = lib.mkOption {
        type = lib.types.listOf lib.types.str;
        default = [ ];
        description = "Flake inputs this project updates. Required unless the project bundles others with `includes`.";
      };
      includes = lib.mkOption {
        type = lib.types.listOf lib.types.str;
        default = [ ];
        example = [ "npk" "nfu" "totality" ];
        description = ''
          Other projects this one bundles. A run updates the union of their inputs
          in one lock write, rebuilds once, and deploys each host once — editing a
          member changes what the bundle covers, with no second copy of its input
          list to drift.
        '';
      };
      hosts = lib.mkOption {
        type = lib.types.listOf lib.types.str;
        default = [ ];
        description = "Deploy hosts to activate after the local switch, in order.";
      };
      localSwitch = lib.mkOption {
        type = lib.types.nullOr lib.types.bool;
        default = null;
        description = "Whether a run rebuilds the local host. Null means the default (true); false for a remote-only project.";
      };
      description = lib.mkOption {
        type = lib.types.str;
        default = "";
        description = "Optional human note shown in `nfu project list`.";
      };
    };
  };

  # config.toml holds the base settings plus the declarative host catalog. The
  # typed theme option wins over a settings.theme freeform entry.
  hostToToml = h: lib.filterAttrs (_: v: v != "" && v != [ ]) {
    inherit (h) hostname deployArgs;
  };
  configToml = cfg.settings
    // lib.optionalAttrs (cfg.theme != null) { theme = cfg.theme; }
    // lib.optionalAttrs (cfg.hosts != { }) {
      hosts = lib.mapAttrs (_: hostToToml) cfg.hosts;
    };

  # One TOML file per project (name = filename), dropping empty/unset fields.
  projectToToml = p: lib.filterAttrs (_: v: v != null && v != "" && v != [ ]) {
    inherit (p) inputs includes hosts description localSwitch;
  };
  projectFiles = lib.mapAttrs'
    (name: p: lib.nameValuePair "nfu/projects/${name}.toml" {
      source = tomlFormat.generate "nfu-project-${name}.toml" (projectToToml p);
    })
    cfg.projects;

  writeConfig = cfg.settings != { } || cfg.hosts != { } || cfg.theme != null;
in
{
  options.programs.nfu = {
    enable = lib.mkEnableOption "nfu, a small flake-input updater";

    package = lib.mkOption {
      type = lib.types.package;
      default = self.packages.${pkgs.stdenv.hostPlatform.system}.nfu;
      defaultText = lib.literalExpression "nfu.packages.\${system}.nfu";
      description = "The nfu package to install.";
    };

    settings = lib.mkOption {
      type = tomlFormat.type;
      default = { };
      example = lib.literalExpression ''
        {
          flakeRoot = "~/Projects/nixos-config";
          commit = false;
        }
      '';
      description = ''
        nfu configuration, written to ~/.config/nfu/config.toml.
        See config.example.toml in the nfu repo for the schema.
      '';
    };

    theme = lib.mkOption {
      # Keep in sync with the Go theme registry (internal/theme).
      type = lib.types.nullOr (lib.types.enum [ "default" "gruvbox-dark" "gruvbox-light" "muted" ]);
      default = null;
      example = "gruvbox-dark";
      description = ''
        Color theme for nfu's TUIs. null leaves the choice to the built-in
        default (ANSI palette codes, inheriting the terminal scheme).
        Shorthand for settings.theme, and takes precedence over it.
      '';
    };

    hosts = lib.mkOption {
      type = lib.types.attrsOf hostType;
      default = { };
      example = lib.literalExpression ''
        { Fabricator = { hostname = "192.0.2.10"; deployArgs = [ "--magic-rollback" "false" ]; }; }
      '';
      description = "Declarative deploy hosts (deploy-rs nodes) for `nfu project`, written to config.toml.";
    };

    projects = lib.mkOption {
      type = lib.types.attrsOf projectType;
      default = { };
      example = lib.literalExpression ''
        {
          totality = { inputs = [ "totality" ]; hosts = [ "Fabricator" ]; };
          inference = { inputs = [ "personal-packages" ]; hosts = [ "Fabricator" ]; localSwitch = false; };
          workstation = { includes = [ "totality" "inference" ]; };
        }
      '';
      description = ''
        Declarative projects for `nfu project`, one read-only file each under
        ~/.config/nfu/projects/. `nfu project add` writes mutable ones beside them.
      '';
    };
  };

  config = lib.mkIf cfg.enable {
    home.packages = [ cfg.package ];

    xdg.configFile = lib.mkMerge [
      (lib.mkIf writeConfig {
        "nfu/config.toml".source = tomlFormat.generate "nfu-config.toml" configToml;
      })
      projectFiles
    ];

    assertions = lib.mapAttrsToList
      (name: p: {
        assertion = p.inputs != [ ] || p.includes != [ ];
        message = "programs.nfu.projects.${name}: needs `inputs` of its own, or `includes` naming projects to bundle";
      })
      cfg.projects;
  };
}
