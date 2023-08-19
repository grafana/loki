final: prev: {
  helm-docs = prev.callPackage
    "${prev.path}/pkgs/applications/networking/cluster/helm-docs"
    {
      buildGoModule = args:
        prev.buildGoModule (args // rec {
          version = "1.11.0";

          src = prev.fetchFromGitHub {
            owner = "norwoodj";
            repo = "helm-docs";
            rev = "v${version}";
            sha256 = "sha256-476ZhjRwHlNJFkHzY8qQ7WbAUUpFNSoxXLGX9esDA/E=";
          };

          vendorSha256 = "sha256-xXwunk9rmzZEtqmSo8biuXnAjPp7fqWdQ+Kt9+Di9N8=";

          ldflags = [
            "-w"
            "-s"
            "-X main.version=v${version}"
          ];
        });
    };
}
