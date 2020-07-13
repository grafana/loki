require "spec_helper"

describe Manticore::Response do
  let(:client) { Manticore::Client.new }
  subject { client.get(local_server) }

  its(:headers) { is_expected.to be_a Hash }
  its(:body) { is_expected.to be_a String }
  its(:length) { is_expected.to be_a Fixnum }

  it "provides response header lookup via #[]" do
    expect(subject["Content-Type"]).to eq "application/json"
  end

  context "when a response contains repeated headers" do
    subject { client.get(local_server "/repeated_headers") }

    it "returns an array of values for headers with multiple occurrances" do
      expect(subject.headers["link"]).to eq ["foo", "bar"]
    end

    it "returns only the first value when using response#[]" do
      expect(subject["Link"]).to eq "foo"
    end
  end

  it "reads the body" do
    expect(subject.body).to match "Manticore"
  end

  it "reads the status code" do
    expect(subject.code).to eq 200
  end

  it "reads the status text" do
    expect(subject.message).to match "OK"
  end

  context "when the client is invoked with a block" do
    it "allows reading the body from a block" do
      response = client.get(local_server) do |response|
        expect(response.body).to match "Manticore"
      end

      expect(response.body).to match "Manticore"
    end

    it "does not read the body implicitly if called with a block" do
      response = client.get(local_server) { }
      expect { response.body }.to raise_exception(Manticore::StreamClosedException)
    end
  end

  context "when an entity fails to read" do
    it "releases the connection" do
      stats_before = client.pool_stats
      expect_any_instance_of(Manticore::EntityConverter).to receive(:read_entity).and_raise(Manticore::StreamClosedException)
      expect { client.get(local_server).call rescue nil }.to_not change { client.pool_stats[:available] }
    end
  end

  context "given a success handler" do
    let(:responses) { {} }
    let(:response) do
      client.get(url)
        .on_success { |resp| responses[:success] = true }
        .on_failure { responses[:failure] = true }
        .on_complete { responses[:complete] = true }
    end

    context "a succeeded request" do
      let(:url) { local_server }
      it "runs the success handler" do
        expect { response.call }.to change { responses[:success] }.to true
      end

      it "does not run the failure handler" do
        expect { response.call }.to_not change { responses[:failure] }
      end

      it "runs the completed handler" do
        expect { response.call }.to change { responses[:complete] }.to true
      end
    end

    context "a failed request" do
      let(:url) { local_server("/failearly") }
      it "does not run the success handler" do
        expect { response.call }.to_not change { responses[:success] }
      end

      it "runs the failure handler" do
        expect { response.call }.to change { responses[:failure] }.to true
      end

      it "runs the completed handler" do
        expect { response.call }.to change { responses[:complete] }.to true
      end
    end
  end
end
