{ pkgs, buildGoModule, fetchFromGitHub }:
buildGoModule rec {
  pname = "chart-releaser";
  version = "1.4.0";

  src = fetchFromGitHub {
    owner = "helm";
    repo = pname;
    rev = "v${version}";
    sha256 = "1x3r4cyzk4p9shbg2xzgvfbfk9kc12hqy9xzpd6l0fbyhdh91qxw";
  };

  vendorHash = "sha256-GYsP5LXZA/swgg7xsOkOAITj/E7euEpP0Uk2KzUzBbI=";

  postPatch = ''
    substituteInPlace pkg/config/config.go \
      --replace "\"/etc/cr\"," "\"$out/etc/cr\","
  '';

  ldflags = [
    "-w"
    "-s"
    "-X github.com/helm/chart-testing/v3/ct/cmd.Version=${version}"
    "-X github.com/helm/chart-testing/v3/ct/cmd.GitCommit=${src.rev}"
    "-X github.com/helm/chart-testing/v3/ct/cmd.BuildDate=19700101-00:00:00"
  ];

  # tests require git to be available
  nativeBuildInputs = with pkgs; [ git installShellFiles ];

  postInstall = ''
    installShellCompletion --cmd cr \
      --bash <($out/bin/cr completion bash) \
      --zsh <($out/bin/cr completion zsh) \
      --fish <($out/bin/cr completion fish) \
  '';
}
