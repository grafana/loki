# frozen_string_literal: true
require 'support'
require 'mustermann'

describe Mustermann::Concat do
  describe Mustermann::Concat::Native do
    context "sinatra + sinatra" do
      subject(:pattern) { Mustermann.new("/:foo") +  Mustermann.new("/:bar") }
      its(:class) { should be == Mustermann::Sinatra }
      its(:to_s)  { should be == "/{foo}/{bar}" }
    end

    context "sinatra + string" do
      subject(:pattern) { Mustermann.new("/:foo") +  "/:bar" }
      its(:class) { should be == Mustermann::Sinatra }
      its(:to_s)  { should be == "/{foo}/\\:bar" }
    end

    context "regular + regular" do
      subject(:pattern) { Mustermann.new(/foo/) +  Mustermann.new(/bar/) }
      its(:class) { should be == Mustermann::Regular }
      its(:to_s)  { should be == "foobar" }
    end

    context "sinatra + rails" do
      subject(:pattern) { Mustermann.new("/:foo") +  Mustermann.new("/:bar", type: :rails) }
      its(:class) { should be == Mustermann::Sinatra }
      its(:to_s)  { should be == "/{foo}/{bar}" }
    end

    context "sinatra + flask" do
      subject(:pattern) { Mustermann.new("/:foo") +  Mustermann.new("/<bar>", type: :flask) }
      its(:class) { should be == Mustermann::Sinatra }
      its(:to_s)  { should be == "/{foo}/{bar}" }
    end

    context "sinatra + flask (typed)" do
      subject(:pattern) { Mustermann.new("/:foo") +  Mustermann.new("/<int:bar>", type: :flask) }
      its(:class) { should be == Mustermann::Concat }
      its(:to_s)  { should be == '(sinatra:"/:foo" + flask:"/<int:bar>")' }
    end

    context "sinatra + sinatra (different options)" do
      subject(:pattern) { Mustermann.new("/:foo") +  Mustermann.new("/:bar", uri_decode: false) }
      its(:class) { should be == Mustermann::Concat }
      its(:to_s)  { should be == '(sinatra:"/:foo" + sinatra:"/:bar")' }
    end

    context "sinatra + rails (different options)" do
      subject(:pattern) { Mustermann.new("/:foo") +  Mustermann.new("/:bar", type: :rails, uri_decode: false) }
      its(:class) { should be == Mustermann::Concat }
      its(:to_s)  { should be == '(sinatra:"/:foo" + rails:"/:bar")' }
    end

    context "sinatra + rails (different options) + sinatra" do
      subject(:pattern) { Mustermann.new("/:foo") +  Mustermann.new("/:bar", type: :rails, uri_decode: false) + Mustermann.new("/:baz") }
      its(:class) { should be == Mustermann::Concat }
      its(:to_s)  { should be == '(sinatra:"/:foo" + rails:"/:bar" + sinatra:"/:baz")' }
    end

    context "sinatra + (sinatra + regular)" do
      subject(:pattern) { Mustermann.new("/foo") + (Mustermann.new("/bar") + Mustermann.new(/baz/)) }
      its(:class) { should be == Mustermann::Concat }
      its(:to_s)  { should be == '(sinatra:"/foo/bar" + regular:"baz")' }
    end

    context "sinatra + (sinatra + rails (different options) + sinatra)" do
      subject(:pattern) { Mustermann.new("/foo") + (Mustermann.new("/bar") + Mustermann.new("/baz", type: :rails, uri_decode: false) + Mustermann.new("/boo")) }
      its(:class) { should be == Mustermann::Concat }
      its(:to_s)  { should be == '(sinatra:"/foo/bar" + rails:"/baz" + sinatra:"/boo")' }
    end
  end

  subject(:pattern) { Mustermann::Concat.new("/:foo", "/:bar") }

  describe :=== do
    example { (pattern === "/foo/bar")  .should be true  }
    example { (pattern === "/foo/bar/") .should be false }
    example { (pattern === "/foo")      .should be false }
  end

  describe :match do
    it { should match("/foo/bar").capturing(foo: "foo", bar: "bar") }
    it { should_not match("/foo/bar/") }
    it { should_not match("/foo/") }
  end

  describe :params do
    example { pattern.params("/foo/bar")  .should be == { "foo" => "foo", "bar" => "bar" }}
    example { pattern.params("/foo/bar/") .should be_nil }
    example { pattern.params("/foo")      .should be_nil }
  end

  describe :peek do
    example { pattern.peek("/foo/bar/baz")  .should be == "/foo/bar" }
    example { pattern.peek("/foo")          .should be_nil }
  end

  describe :peek_params do
    example { pattern.peek_params("/foo/bar/baz")  .should be == [{ "foo" => "foo", "bar" => "bar" }, 8]}
    example { pattern.peek_params("/foo")          .should be_nil }
  end

  describe :peek_match do
    example { pattern.peek_match("/foo/bar/baz").to_s .should be == "/foo/bar" }
    example { pattern.peek_match("/foo")              .should be_nil }
  end

  describe :peek_size do
    example { pattern.peek_size("/foo/bar/baz")  .should be == 8 }
    example { pattern.peek_size("/foo")          .should be_nil }
  end

  describe :expand do
    it { should expand(foo: :bar, bar: :foo)                    .to('/bar/foo')        }
    it { should expand(:append, foo: :bar, bar: :foo, baz: 42)  .to('/bar/foo?baz=42') }
    it { should_not expand(foo: :bar) }
  end

  describe :to_templates do
    subject(:pattern) { Mustermann::Concat.new("/:foo|:bar", "(/:baz)?") }
    it { should generate_template("/{foo}/{baz}") }
    it { should generate_template("{bar}/{baz}") }
    it { should generate_template("/{foo}") }
    it { should generate_template("{bar}") }
  end
end
