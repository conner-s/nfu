{
  description = "nfu — a small flake-input updater (the piece nh is missing)";

  inputs.nixpkgs.url = "github:nixos/nixpkgs/nixos-unstable";

  outputs = { self, nixpkgs }:
    let
      systems = [ "aarch64-darwin" "x86_64-darwin" "aarch64-linux" "x86_64-linux" ];
      forAll = f: nixpkgs.lib.genAttrs systems (system: f nixpkgs.legacyPackages.${system});
    in
    {
      packages = forAll (pkgs: {
        default = self.packages.${pkgs.stdenv.hostPlatform.system}.nfu;

        nfu = pkgs.buildGoModule {
          pname = "nfu";
          version = "0.1.0";
          src = ./.;

          # Hash of the vendored Go dependencies. To regenerate after changing
          # go.mod/go.sum: set this to lib.fakeHash, run `nix build .#nfu`, and
          # paste the `got:` hash it reports.
          vendorHash = "sha256-aan6fL3HrFowlBLonWif4eQhKwgIpuQBOwo93G7kZmo=";

          nativeBuildInputs = [ pkgs.makeWrapper ];

          # nfu shells out to `nix` (for flake metadata/update) and `git` (for the
          # optional flake.lock commit). `nix` MUST come from the host — its
          # config, features, and substituters are the user's, not ours — so this
          # only *suffixes* PATH: a host nix/git always wins, and git is merely a
          # fallback for minimal environments.
          postInstall = ''
            wrapProgram $out/bin/nfu \
              --suffix PATH : ${nixpkgs.lib.makeBinPath [ pkgs.git ]}
          '';

          meta = {
            description = "Flake-input updater with a persistent config and a preview-before-commit feel";
            homepage = "https://github.com/conner-s/nfu";
            mainProgram = "nfu";
            platforms = systems;
          };
        };
      });

      # Dogfood: a home-manager module that installs nfu and writes its config.
      # Import it and set programs.nfu.settings.flakeRoot.
      homeManagerModules.default = import ./modules/home-manager.nix self;

      # `nix flake check` gate: building the package runs buildGoModule's default
      # checkPhase (`go test ./...`), so the tree cannot pass checks with a
      # failing suite. nfu's unit tests are pure (they parse fixture lock JSON),
      # so this needs no live nix.
      checks = forAll (pkgs: {
        nfu = self.packages.${pkgs.stdenv.hostPlatform.system}.nfu;
      });

      devShells = forAll (pkgs: {
        default = pkgs.mkShell {
          packages = [ pkgs.go pkgs.gopls pkgs.git ];
        };
      });

      formatter = forAll (pkgs: pkgs.nixpkgs-fmt);
    };
}
