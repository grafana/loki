# frozen_string_literal: true
require 'support'
require 'timeout'
require 'mustermann/regular'

describe Mustermann::Regular do
  extend Support::Pattern

  pattern '' do
    it { should     match('')  }
    it { should_not match('/') }

    it { should_not respond_to(:expand)       }
    it { should_not respond_to(:to_templates) }
  end

  pattern '/' do
    it { should     match('/')    }
    it { should_not match('/foo') }
  end

  pattern '/foo' do
    it { should     match('/foo')     }
    it { should_not match('/bar')     }
    it { should_not match('/foo.bar') }
  end

  pattern '/foo/bar' do
    it { should     match('/foo/bar')   }
    it { should_not match('/foo%2Fbar') }
    it { should_not match('/foo%2fbar') }
  end

  pattern '/(?<foo>.*)' do
    it { should match('/foo')       .capturing foo: 'foo'       }
    it { should match('/bar')       .capturing foo: 'bar'       }
    it { should match('/foo.bar')   .capturing foo: 'foo.bar'   }
    it { should match('/%0Afoo')    .capturing foo: '%0Afoo'    }
    it { should match('/foo%2Fbar') .capturing foo: 'foo%2Fbar' }
  end


  context 'with Regexp::EXTENDED' do
    let(:pattern) {
      %r{
        \/compare\/ # match any URL beginning with \/compare\/
        (.+)      # extract the full path (including any directories)
        \/         # match the final slash
        ([^.]+)   # match the first SHA1
        \.{2,3}   # match .. or ...
        (.+)      # match the second SHA1
      }x
    }
    example { expect { Timeout.timeout(1){ Mustermann::Regular.new(pattern) }}.not_to raise_error }
    it { expect(Mustermann::Regular.new(pattern)).to match('/compare/foo/bar..baz') }
  end

  describe :check_achnors do
    context 'raises on anchors' do
      example { expect { Mustermann::Regular.new('^foo')        }.to     raise_error(Mustermann::CompileError) }
      example { expect { Mustermann::Regular.new('foo$')        }.to     raise_error(Mustermann::CompileError) }
      example { expect { Mustermann::Regular.new('\Afoo')       }.to     raise_error(Mustermann::CompileError) }
      example { expect { Mustermann::Regular.new('foo\Z')       }.to     raise_error(Mustermann::CompileError) }
      example { expect { Mustermann::Regular.new('foo\z')       }.to     raise_error(Mustermann::CompileError) }
      example { expect { Mustermann::Regular.new(/^foo/)        }.to     raise_error(Mustermann::CompileError) }
      example { expect { Mustermann::Regular.new(/foo$/)        }.to     raise_error(Mustermann::CompileError) }
      example { expect { Mustermann::Regular.new(/\Afoo/)       }.to     raise_error(Mustermann::CompileError) }
      example { expect { Mustermann::Regular.new(/foo\Z/)       }.to     raise_error(Mustermann::CompileError) }
      example { expect { Mustermann::Regular.new(/foo\z/)       }.to     raise_error(Mustermann::CompileError) }
      example { expect { Mustermann::Regular.new('[^f]')        }.not_to raise_error }
      example { expect { Mustermann::Regular.new('\\\A')        }.not_to raise_error }
      example { expect { Mustermann::Regular.new('[[:digit:]]') }.not_to raise_error }
      example { expect { Mustermann::Regular.new(/[^f]/)        }.not_to raise_error }
      example { expect { Mustermann::Regular.new(/\\A/)         }.not_to raise_error }
      example { expect { Mustermann::Regular.new(/[[:digit:]]/) }.not_to raise_error }
    end

    context 'with check_anchors disabled' do
      example { expect { Mustermann::Regular.new('^foo',        check_anchors: false) }.not_to raise_error }
      example { expect { Mustermann::Regular.new('foo$',        check_anchors: false) }.not_to raise_error }
      example { expect { Mustermann::Regular.new('\Afoo',       check_anchors: false) }.not_to raise_error }
      example { expect { Mustermann::Regular.new('foo\Z',       check_anchors: false) }.not_to raise_error }
      example { expect { Mustermann::Regular.new('foo\z',       check_anchors: false) }.not_to raise_error }
      example { expect { Mustermann::Regular.new(/^foo/,        check_anchors: false) }.not_to raise_error }
      example { expect { Mustermann::Regular.new(/foo$/,        check_anchors: false) }.not_to raise_error }
      example { expect { Mustermann::Regular.new(/\Afoo/,       check_anchors: false) }.not_to raise_error }
      example { expect { Mustermann::Regular.new(/foo\Z/,       check_anchors: false) }.not_to raise_error }
      example { expect { Mustermann::Regular.new(/foo\z/,       check_anchors: false) }.not_to raise_error }
      example { expect { Mustermann::Regular.new('[^f]',        check_anchors: false) }.not_to raise_error }
      example { expect { Mustermann::Regular.new('\\\A',        check_anchors: false) }.not_to raise_error }
      example { expect { Mustermann::Regular.new('[[:digit:]]', check_anchors: false) }.not_to raise_error }
      example { expect { Mustermann::Regular.new(/[^f]/,        check_anchors: false) }.not_to raise_error }
      example { expect { Mustermann::Regular.new(/\\A/,         check_anchors: false) }.not_to raise_error }
      example { expect { Mustermann::Regular.new(/[[:digit:]]/, check_anchors: false) }.not_to raise_error }
    end
  end

  context "peeking" do
    subject(:pattern) { Mustermann::Regular.new("(?<name>[^/]+)") }

    describe :peek_size do
      example { pattern.peek_size("foo bar/blah")   .should be == "foo bar".size }
      example { pattern.peek_size("foo%20bar/blah") .should be == "foo%20bar".size }
      example { pattern.peek_size("/foo bar")       .should be_nil }
    end

    describe :peek_match do
      example { pattern.peek_match("foo bar/blah")   .to_s .should be == "foo bar" }
      example { pattern.peek_match("foo%20bar/blah") .to_s .should be == "foo%20bar" }
      example { pattern.peek_match("/foo bar")             .should be_nil }
    end

    describe :peek_params do
      example { pattern.peek_params("foo bar/blah")   .should be == [{"name" => "foo bar"}, "foo bar".size] }
      example { pattern.peek_params("foo%20bar/blah") .should be == [{"name" => "foo bar"}, "foo%20bar".size] }
      example { pattern.peek_params("/foo bar")       .should be_nil }
    end
  end
end
