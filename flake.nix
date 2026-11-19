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
          vendorHash = "sha256-vcNnawDRxgulAodGTXwV2+41LIosni8FAfbSj1ZdTDQ=";

          # One of the modules, termsafe, is on the lab's Forgejo, which the
          # public Go proxy cannot see and whose certificate chains to a
          # private root. The derivation that downloads the modules (goModules,
          # which vendorHash is the hash of) fetches it directly: go asks
          # https://forgejo…/connerlab/termsafe?go-get=1 where the repository
          # is, then git clones it over https. go verifies the server with
          # SSL_CERT_FILE and git with GIT_SSL_CAINFO, and nixpkgs points both
          # at a bundle of public authorities. So, in that derivation only:
          # the bundle it was going to use, plus the root, for both programs;
          # and GOPRIVATE, so that go never asks proxy.golang.org or
          # sum.golang.org about that path. The version stays pinned in one
          # place, go.mod, and go.sum still checks what arrives.
          #
          # The variables are set in the build script, not as attributes: Nix
          # overwrites an attribute named NIX_SSL_CERT_FILE or GOPROXY with the
          # daemon's own value (impureEnvVars). A build that passes on a
          # machine whose own bundle holds the root proves nothing: see
          # README.md, "The module from the Forgejo".
          overrideModAttrs = _: prev: {
            preBuild = (prev.preBuild or "") + ''
              caBase=$NIX_SSL_CERT_FILE
              [ -r "$caBase" ] || caBase=${pkgs.cacert}/etc/ssl/certs/ca-bundle.crt
              { cat "$caBase"; echo; cat ${./nix/connerlab-root-ca.crt}; } > "$TMPDIR/ca-bundle.crt"
              export NIX_SSL_CERT_FILE="$TMPDIR/ca-bundle.crt"
              export SSL_CERT_FILE="$NIX_SSL_CERT_FILE" GIT_SSL_CAINFO="$NIX_SSL_CERT_FILE"
              export GOPRIVATE=forgejo.kube.internal.conn3r.xyz
              echo "go modules: $caBase plus the connerlabPKI root; GOPRIVATE=$GOPRIVATE"
            '';
          };

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

          # termsafe is on the lab's Forgejo: go fetches it from there and
          # keeps its path away from proxy.golang.org and sum.golang.org.
          # Trusting the Forgejo's certificate is left to the machine (its own
          # CA bundle or keychain, or the variables CI sets for every step).
          GOPRIVATE = "forgejo.kube.internal.conn3r.xyz";
        };
      });

      formatter = forAll (pkgs: pkgs.nixpkgs-fmt);
    };
}
