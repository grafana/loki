final: prev: {
  golangci-lint = prev.callPackage
    "${prev.path}/pkgs/development/tools/golangci-lint"
    {
      buildGoModule = args:
        prev.buildGoModule (args // rec {
          version = "1.51.2";

          src = prev.fetchFromGitHub rec {
            owner = "golangci";
            repo = "golangci-lint";
            rev = "v${version}";
            sha256 = "F2rkVZ5ia9/wyTw1WIeizFnuaHoS2A8VzVOGDcshy64=";
          };

          vendorHash =
            "sha256-JO/mRJB3gRTtBj6pW1267/xXUtalTJo0p3q5e34vqTs=";

          ldflags = [
            "-s"
            "-w"
            "-X main.version=${version}"
            "-X main.commit=v${version}"
            "-X main.date=19700101-00:00:00"
          ];
        });
    };
}
