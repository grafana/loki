{ lib, buildGoModule, fetchFromGitHub }:

buildGoModule rec {
  pname = "faillint";
  version = "v1.14.0";

  src = fetchFromGitHub {
    owner = "fatih";
    repo = "faillint";
    rev = "${version}";
    sha256 = "NV+wbu547mtTa6dTGv7poBwWXOmu5YjqbauzolCg5qs=";
  };

  vendorHash = "sha256-vWt4HneDA7YwXYnn8TbfWCKzSv7RcgXxn/HAh6a+htQ=";
  doCheck = false;
}
