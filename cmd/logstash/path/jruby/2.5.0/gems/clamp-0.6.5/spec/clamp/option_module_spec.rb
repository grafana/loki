require 'spec_helper'

describe Clamp::Command do

  include OutputCapture

  context "with included module" do

    let(:command) do

      shared_options = Module.new do
        extend Clamp::Option::Declaration
        option "--size", "SIZE", :default => 4
      end

      command_class = Class.new(Clamp::Command) do

        include shared_options

        def execute
          puts "size = #{size}"
        end

      end

      command_class.new("foo")

    end

    it "accepts options from included module" do
      command.run(["--size", "42"])
      expect(stdout).to eql "size = 42\n"
    end

  end

end
