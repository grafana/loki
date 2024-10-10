{ lib, buildGoModule, fetchFromGitHub }:

buildGoModule {
  pname = "faillint";
  version = "1.13.0";

  src = fetchFromGitHub {
    owner = "trevorwhitney";
    repo = "faillint";
    rev = "main";
    sha256 = "NV+wbu547mtTa6dTGv7poBwWXOmu5YjqbauzolCg5qs=";
  };

  vendorHash = "sha256-vWt4HneDA7YwXYnn8TbfWCKzSv7RcgXxn/HAh6a+htQ=";
  doCheck = false;
}
