lib = File.expand_path("../../lib", __FILE__)
$:.unshift lib unless $:.include?(lib)

require "minitest/autorun"
require "mocha/setup"

class MiniTest::Unit::TestCase
  def data_file(name)
    File.read(data_file_path(name))
  end

  def data_file_path(name)
    File.expand_path("../data/#{name}", __FILE__)
  end
end
