require "spec_helper"

describe Manticore::Cookie do
  context "created from a Client request" do
    let(:client) { Manticore::Client.new cookies: true }
    subject {
      response = client.get(local_server("/cookies/1/2"))
      expect(response.final_url.to_s).to eq local_server("/cookies/2/2")
      response.cookies["x"].first
    }

    its(:name) { is_expected.to eq "x" }
    its(:value) { is_expected.to eq "2" }
    its(:path) { is_expected.to eq "/" }
    its(:domain) { is_expected.to eq "localhost" }
  end

  let(:opts) { {} }
  subject {
    Manticore::Cookie.new({name: "foo", value: "bar"}.merge(opts))
  }

  its(:secure?) { is_expected.to be nil }
  its(:persistent?) { is_expected.to be nil }

  context "created as secure" do
    let(:opts) { {secure: true} }
    its(:secure?) { is_expected.to be true }
  end

  context "created as persistent" do
    let(:opts) { {persistent: true} }
    its(:persistent?) { is_expected.to be true }
  end
end
