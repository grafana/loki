require File.expand_path("../common", __FILE__)
require "gem_publisher/cli_facade"

module GemPublisher
  class CliFacadeTest < MiniTest::Unit::TestCase
    def test_should_raise_exception_if_command_failed
      status = stub(:exitstatus => 1)
      Open3.stubs(:capture3).returns(["stdout", "stderr", status])
      assert_raises CliFacade::Error do
        CliFacade.new.execute("status")
      end
    end

    def test_should_include_all_output_in_error
      status = stub(:exitstatus => 1)
      Open3.stubs(:capture3).returns(["stdout", "stderr", status])
      err = assert_raises CliFacade::Error do
        CliFacade.new.execute("status")
      end
      assert_equal "stderr\nstdout", err.message

      Open3.stubs(:capture3).returns(["stdout", "", status])
      err = assert_raises CliFacade::Error do
        CliFacade.new.execute("status")
      end
      assert_equal "stdout", err.message
    end

    def test_should_return_stdout_if_command_succeeded
      status = stub(:exitstatus => 0)
      Open3.stubs(:capture3).returns(["stdout", "stderr", status])
      assert_equal "stdout", CliFacade.new.execute("status")
    end

    def test_should_escape_and_pass_in_all_commands
      status = stub(:exitstatus => 0)
      Open3.expects(:capture3).
        with("foo bar \\*").
        returns(["stdout", "stderr", status])
      CliFacade.new.execute("foo", "bar", "*")
    end
  end
end
