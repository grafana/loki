require File.expand_path("../common", __FILE__)
require "gem_publisher/git_remote"

module GemPublisher
  class GitRemoteTest < MiniTest::Unit::TestCase
    def test_should_list_remote_tags
      cli_facade = mock
      cli_facade.stubs(:execute).
        with("git", "ls-remote", "--tags", "origin").
        returns(data_file("tags"))

      remote = GitRemote.new("origin", cli_facade)
      expected = %w[v0.0.10 v0.0.11 v0.0.6 v0.0.7 v0.0.9]
      assert_equal expected.sort, remote.tags.sort
    end

    def test_should_push_tag_to_remote
      cli_facade = mock
      sha1 = "5294fac0c70956209494b69bc1a8c38192f6a931"
      cli_facade.stubs(:execute).
        with("git", "rev-parse", "HEAD").
        returns("#{sha1}\n")
      cli_facade.expects(:execute).
        with("git", "update-ref", "refs/tags/nameoftag", sha1)
      cli_facade.expects(:execute).
        with("git", "push", "origin", "tag", "nameoftag")

      remote = GitRemote.new("origin", cli_facade)
      remote.add_tag("nameoftag")
    end
  end
end
