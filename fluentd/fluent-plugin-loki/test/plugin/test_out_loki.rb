require "helper"
require "fluent/plugin/out_loki.rb"

class LokiOutputTest < Test::Unit::TestCase
  setup do
    Fluent::Test.setup
  end

  test "failure" do
    flunk
  end

  private

  def create_driver(conf)
    Fluent::Test::Driver::Output.new(Fluent::Plugin::LokiOutput).configure(conf)
  end
end
