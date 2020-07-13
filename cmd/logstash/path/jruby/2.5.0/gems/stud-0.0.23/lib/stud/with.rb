module Stud
  module With
    # Run a block with arguments. This is sometimes useful in lieu of
    # explicitly assigning variables.
    # 
    # I find mainly that using 'with' is a clue that I can factor out
    # a given segment of code into a method or function.
    #
    # Example usage:
    #
    #   with(TCPSocket.new("google.com", 80)) do |s|
    #     s.write("GET / HTTP/1.0\r\nHost: google.com\r\n\r\n")
    #     puts s.read
    #     s.close
    #   end
    def with(*args, &block)
      block.call(*args)
    end

    extend self
  end
end
