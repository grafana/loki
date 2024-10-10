{ lib, buildGoModule, fetchFromGitHub }:

buildGoModule rec {
  pname = "faillint";
  version = "24d8e46ee21be34778b85fce26fed0ad50210141";

  src = fetchFromGitHub {
    owner = "fatih";
    repo = "faillint";
    rev = "${version}";
    sha256 = "NV+wbu547mtTa6dTGv7poBwWXOmu5YjqbauzolCg5qs=";
  };

  vendorHash = "sha256-vWt4HneDA7YwXYnn8TbfWCKzSv7RcgXxn/HAh6a+htQ=";
  doCheck = false;
}
