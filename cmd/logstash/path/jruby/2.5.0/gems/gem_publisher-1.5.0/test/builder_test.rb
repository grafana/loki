require File.expand_path("../common", __FILE__)
require "gem_publisher/builder"

module GemPublisher
  class BuilderTest < MiniTest::Unit::TestCase
    def test_should_build_gem_and_return_filename
      cli_facade = mock
      cli_facade.expects(:execute).
        with("gem", "build", "foo.gemspec").
        returns(data_file("gem_build"))

      builder = Builder.new(cli_facade)
      assert_equal "example-0.0.3.gem", builder.build("foo.gemspec")
    end
  end
end
