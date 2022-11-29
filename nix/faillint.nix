{ lib, buildGoModule, fetchFromGitHub }:

buildGoModule rec {
  pname = "faillint";
  version = "1.10.0";

  src = fetchFromGitHub {
    owner = "fatih";
    repo = "faillint";
    rev = "v${version}";
    sha256 = "0y8m39iir7cry3svmwvv9fbfld6y5k6asimmhs0f4rk8f9czriv8";
  };

  vendorSha256 = "yN8KHpHfBN9Gv6BVdA5AQhYSJ7fwH9k9ZgkuNYHF0kc=";
  doCheck = false;
}
