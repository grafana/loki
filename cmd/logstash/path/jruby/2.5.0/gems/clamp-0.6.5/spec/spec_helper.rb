require "rspec"
require "clamp"
require 'stringio'

RSpec.configure do |config|

  config.mock_with :rr

end

module OutputCapture

  def self.included(target)

    target.before do
      $stdout = @out = StringIO.new
      $stderr = @err = StringIO.new
    end

    target.after do
      $stdout = STDOUT
      $stderr = STDERR
    end

  end

  def stdout
    @out.string
  end

  def stderr
    @err.string
  end

end

module CommandFactory

  def given_command(name, &block)
    let(:command_class) do
      Class.new(Clamp::Command, &block)
    end
    let(:command) do
      command_class.new(name)
    end
  end

end

module SetEnv

  def set_env(name, value)
    if value
      ENV[name] = value
    else
      ENV.delete(name)
    end
  end

end
