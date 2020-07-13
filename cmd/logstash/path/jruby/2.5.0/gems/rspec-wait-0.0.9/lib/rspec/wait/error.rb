module RSpec
  module Wait
    class Error < StandardError; end

    class TimeoutError < Error; end
  end
end
