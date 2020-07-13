# frozen_string_literal: true

require "spec_helper"

RSpec.describe "NIO acceptables" do
  shared_context "an NIO acceptable" do
    let(:selector) { NIO::Selector.new }

    it "selects for read readiness" do
      waiting_monitor = selector.register(unacceptable_subject, :r)
      ready_monitor   = selector.register(acceptable_subject, :r)

      ready_monitors = selector.select
      expect(ready_monitors).to include ready_monitor
      expect(ready_monitors).not_to include waiting_monitor
    end
  end

  describe TCPServer do
    let :acceptable_subject do
      server = TCPServer.new("127.0.0.1", 0)
      TCPSocket.open("127.0.0.1", server.local_address.ip_port)
      server
    end

    let :unacceptable_subject do
      TCPServer.new("127.0.0.1", 0)
    end

    it_behaves_like "an NIO acceptable"
  end
end
