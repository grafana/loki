# frozen_string_literal: true
require 'support'
require 'mustermann/expander'

describe Mustermann::Expander do
  it 'expands a pattern' do
    expander = Mustermann::Expander.new("/:foo.jpg")
    expander.expand(foo: 42).should be == "/42.jpg"
  end

  it 'expands multiple patterns' do
    expander = Mustermann::Expander.new << "/:foo.:ext" << "/:foo"
    expander.expand(foo: 42, ext: 'jpg').should be == "/42.jpg"
    expander.expand(foo: 23).should be == "/23"
  end

  it 'supports setting pattern options' do
    expander = Mustermann::Expander.new(type: :rails) << "/:foo(.:ext)" << "/:bar"
    expander.expand(foo: 42, ext: 'jpg').should be == "/42.jpg"
    expander.expand(foo: 42).should be == "/42"
  end

  it 'supports combining different pattern styles' do
    expander = Mustermann::Expander.new << Mustermann.new("/:foo(.:ext)", type: :rails) << Mustermann.new("/:bar", type: :sinatra)
    expander.expand(foo: 'pony', ext: 'jpg').should be == '/pony.jpg'
    expander.expand(bar: 23).should be == "/23"
  end

  it 'ignores nil values' do
    expander = Mustermann::Expander.new << Mustermann.new("/:foo(.:ext)?")
    expander.expand(foo: 'pony', ext: nil).should be == '/pony'
  end

  it 'supports splat' do
    expander = Mustermann::Expander.new << Mustermann.new("/foo/*/baz")
    expander.expand(splat: 'bar').should be == '/foo/bar/baz'
  end

  it 'supports multiple splats' do
    expander = Mustermann::Expander.new << Mustermann.new("/foo/*/bar/*")
    expander.expand(splat: [123, 456]).should be == '/foo/123/bar/456'
  end

  it 'supports identity patterns' do
    expander = Mustermann::Expander.new('/:foo', type: :identity)
    expander.expand.should be == '/:foo'
  end

  describe :additional_values do
    context "illegal value" do
      example { expect { Mustermann::Expander.new(additional_values: :foo) }.to raise_error(ArgumentError) }
      example { expect { Mustermann::Expander.new('/').expand(:foo, a: 10) }.to raise_error(ArgumentError) }
    end

    context :raise do
      subject(:expander) { Mustermann::Expander.new('/:a', additional_values: :raise) }
      example { expander.expand(a: ?a).should be == '/a' }
      example { expect { expander.expand(a: ?a, b: ?b) }.to raise_error(Mustermann::ExpandError) }
      example { expect { expander.expand(b: ?b) }.to raise_error(Mustermann::ExpandError) }
    end

    context :ignore do
      subject(:expander) { Mustermann::Expander.new('/:a', additional_values: :ignore) }
      example { expander.expand(a: ?a).should be == '/a' }
      example { expander.expand(a: ?a, b: ?b).should be == '/a' }
      example { expect { expander.expand(b: ?b) }.to raise_error(Mustermann::ExpandError) }
    end

    context :append do
      subject(:expander) { Mustermann::Expander.new('/:a', additional_values: :append) }
      example { expander.expand(a: ?a).should be == '/a' }
      example { expander.expand(a: ?a, b: ?b).should be == '/a?b=b' }
      example { expect { expander.expand(b: ?b) }.to raise_error(Mustermann::ExpandError) }
    end
  end

  describe :cast do
    subject(:expander) { Mustermann::Expander.new('/:a(/:b)?') }

    example { expander.cast          { "FOOBAR"          }.expand(a: "foo")           .should be == "/FOOBAR"   }
    example { expander.cast          { |v| v.upcase      }.expand(a: "foo")           .should be == "/FOO"      }
    example { expander.cast          { |v| v.upcase      }.expand(a: "foo", b: "bar") .should be == "/FOO/BAR"  }
    example { expander.cast(:a)      { |v| v.upcase      }.expand(a: "foo", b: "bar") .should be == "/FOO/bar"  }
    example { expander.cast(:a, :b)  { |v| v.upcase      }.expand(a: "foo", b: "bar") .should be == "/FOO/BAR"  }
    example { expander.cast(Integer) { |k,v| "#{k}_#{v}" }.expand(a: "foo", b: 42)    .should be == "/foo/b_42" }

    example do
      expander.cast(:a) { |v| v.upcase   }
      expander.cast(:b) { |v| v.downcase }
      expander.expand(a: "fOo", b: "bAr").should be == "/FOO/bar"
    end
  end

  describe :== do
    example { Mustermann::Expander.new('/foo')               .should     be == Mustermann::Expander.new('/foo') }
    example { Mustermann::Expander.new('/foo')               .should_not be == Mustermann::Expander.new('/bar') }
    example { Mustermann::Expander.new('/foo', type: :rails) .should     be == Mustermann::Expander.new('/foo', type: :rails) }
    example { Mustermann::Expander.new('/foo', type: :rails) .should_not be == Mustermann::Expander.new('/foo', type: :sinatra) }
  end

  describe :hash do
    example { Mustermann::Expander.new('/foo')               .hash.should     be == Mustermann::Expander.new('/foo').hash }
    example { Mustermann::Expander.new('/foo')               .hash.should_not be == Mustermann::Expander.new('/bar').hash }
    example { Mustermann::Expander.new('/foo', type: :rails) .hash.should     be == Mustermann::Expander.new('/foo', type: :rails).hash }
    example { Mustermann::Expander.new('/foo', type: :rails) .hash.should_not be == Mustermann::Expander.new('/foo', type: :sinatra).hash }
  end

  describe :eql? do
    example { Mustermann::Expander.new('/foo')               .should     be_eql Mustermann::Expander.new('/foo') }
    example { Mustermann::Expander.new('/foo')               .should_not be_eql Mustermann::Expander.new('/bar') }
    example { Mustermann::Expander.new('/foo', type: :rails) .should     be_eql Mustermann::Expander.new('/foo', type: :rails) }
    example { Mustermann::Expander.new('/foo', type: :rails) .should_not be_eql Mustermann::Expander.new('/foo', type: :sinatra) }
  end

  describe :equal? do
    example { Mustermann::Expander.new('/foo')               .should_not be_equal Mustermann::Expander.new('/foo') }
    example { Mustermann::Expander.new('/foo')               .should_not be_equal Mustermann::Expander.new('/bar') }
    example { Mustermann::Expander.new('/foo', type: :rails) .should_not be_equal Mustermann::Expander.new('/foo', type: :rails) }
    example { Mustermann::Expander.new('/foo', type: :rails) .should_not be_equal Mustermann::Expander.new('/foo', type: :sinatra) }
  end
end
