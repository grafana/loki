require "spec_helper"

describe Manticore::StubbedResponse do
  subject {
    Manticore::StubbedResponse.stub(
      body: "test body",
      code: 200,
      headers: {
        "Content-Type" => "text/plain",
        "Set-Cookie" => ["k=v; path=/; domain=localhost", "k=v; path=/sub; domain=sub.localhost", "k2=v2;2 path=/; domain=localhost"],
      },
      cookies: {"test" => Manticore::Cookie.new(name: "test", value: "something", path: "/")},
    ).call
  }

  it { is_expected.to be_a Manticore::Response }
  its(:body) { is_expected.to eq "test body" }
  its(:code) { is_expected.to eq 200 }

  it "persists the set headers" do
    expect(subject.headers["content-type"]).to eq "text/plain"
  end

  it "sets content-length from the body" do
    expect(subject.headers["content-length"]).to eq "9"
  end

  it "persists cookies passed as explicit cookie objects" do
    expect(subject.cookies["test"].first.value).to eq "something"
  end

  it "calls on_success handlers" do
    called = false
    Manticore::StubbedResponse.stub.on_success { |resp| called = true }.call
    expect(called).to be true
  end

  it "should persist cookies passed in set-cookie" do
    expect(subject.cookies["k"].map(&:value)).to match_array ["v", "v"]
    expect(subject.cookies["k"].map(&:path)).to match_array ["/", "/sub"]
    expect(subject.cookies["k"].map(&:domain)).to match_array ["localhost", "sub.localhost"]
    expect(subject.cookies["k2"].first.value).to eq "v2"
  end

  it "passes bodies to blocks for streaming reads" do
    total = ""; subject.body { |chunk| total << chunk }
    expect(total).to eq("test body")
  end
end
