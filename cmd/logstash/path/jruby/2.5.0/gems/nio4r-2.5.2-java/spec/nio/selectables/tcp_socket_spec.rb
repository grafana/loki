# frozen_string_literal: true

require "spec_helper"

RSpec.describe TCPSocket do
  let(:addr) { "127.0.0.1" }

  let :readable_subject do
    server = TCPServer.new(addr, 0)
    sock = TCPSocket.new(addr, server.local_address.ip_port)
    peer = server.accept

    peer << "Xdata"
    peer.flush
    sock.read(1)

    sock
  end

  let :unreadable_subject do
    server = TCPServer.new(addr, 0)
    sock = TCPSocket.new(addr, server.local_address.ip_port)

    # Sanity check to make sure we actually produced an unreadable socket
    pending "Failed to produce an unreadable socket" if select([sock], [], [], 0)

    sock
  end

  let :writable_subject do
    server = TCPServer.new(addr, 0)
    TCPSocket.new(addr, server.local_address.ip_port)
  end

  let :unwritable_subject do
    server = TCPServer.new(addr, 0)
    sock = TCPSocket.new(addr, server.local_address.ip_port)

    # TODO: close this socket
    _peer = server.accept

    loop do
      sock.write_nonblock "X" * 1024
      _, writers = Kernel.select([], [sock], [], 0)

      break unless writers && writers.include?(sock)
    end

    # HAX: I think the kernel might manage to drain its buffer a bit even after
    # the socket first goes unwritable. Attempt to sleep past this and then
    # attempt to write again
    sleep 0.1

    # Once more for good measure!
    begin
      sock.write_nonblock "X" * 1024
    rescue Errno::EWOULDBLOCK
    end

    # Sanity check to make sure we actually produced an unwritable socket
    pending "Failed to produce an unwritable socket" if select([], [sock], [], 0)

    sock
  end

  let :pair do
    server = TCPServer.new(addr, 0)
    client = TCPSocket.new(addr, server.local_address.ip_port)
    [client, server.accept]
  end

  it_behaves_like "an NIO selectable"
  it_behaves_like "an NIO selectable stream"
  it_behaves_like "an NIO bidirectional stream"

  context :connect do
    include_context NIO::Selector

    it "selects writable when connected" do
      begin
        server = TCPServer.new(addr, 0)

        client = Socket.new(Socket::AF_INET, Socket::SOCK_STREAM, 0)
        monitor = selector.register(client, :w)

        expect do
          client.connect_nonblock server.local_address
        end.to raise_exception Errno::EINPROGRESS

        ready = selector.select(1)

        expect(ready).to include monitor
        result = client.getsockopt(::Socket::SOL_SOCKET, ::Socket::SO_ERROR)
        expect(result.unpack("i").first).to be_zero
      ensure
        server.close rescue nil
        selector.close rescue nil
      end
    end
  end
end
