require File.expand_path("../common", __FILE__)
require "gem_publisher"

module GemPublisher
  class IntegrationTest < MiniTest::Unit::TestCase
    def setup
      Open3.stubs(:capture3)
    end

    def expect_cli(command, response = "")
      Open3.expects(:capture3).
        with(command).
        returns([response, "", stub(:exitstatus => 0)])
    end

    def test_should_build_and_tag_and_publish
      gemspec = data_file_path("example.gemspec")
      expect_cli "git ls-remote --tags origin", data_file("tags")
      expect_cli "gem build #{gemspec}", data_file("gem_build")
      expect_cli "gem push example-0.0.3.gem"
      expect_cli "git rev-parse HEAD", "1234abcd"
      expect_cli "git update-ref refs/tags/v0.0.3 1234abcd"
      expect_cli "git push origin tag v0.0.3"
      GemPublisher.publish_if_updated gemspec
    end

    def test_should_build_and_tag_and_publish_to_gemfury
      gemspec = data_file_path("example.gemspec")
      expect_cli "git ls-remote --tags origin", data_file("tags")
      expect_cli "gem build #{gemspec}", data_file("gem_build")
      expect_cli "fury push example-0.0.3.gem"
      expect_cli "git rev-parse HEAD", "1234abcd"
      expect_cli "git update-ref refs/tags/v0.0.3 1234abcd"
      expect_cli "git push origin tag v0.0.3"
      GemPublisher.publish_if_updated gemspec, :gemfury
    end

    def test_should_build_and_tag_and_publish_to_gemfury_as_given_user
      gemspec = data_file_path("example.gemspec")
      expect_cli "git ls-remote --tags origin", data_file("tags")
      expect_cli "gem build #{gemspec}", data_file("gem_build")
      expect_cli "fury push example-0.0.3.gem --as foo"
      expect_cli "git rev-parse HEAD", "1234abcd"
      expect_cli "git update-ref refs/tags/v0.0.3 1234abcd"
      expect_cli "git push origin tag v0.0.3"
      GemPublisher.publish_if_updated gemspec, :gemfury, :as => "foo"
    end
  end
end
