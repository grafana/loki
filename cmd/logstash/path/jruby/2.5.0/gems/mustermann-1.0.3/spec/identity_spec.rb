# frozen_string_literal: true
require 'support'
require 'mustermann/identity'

describe Mustermann::Identity do
  extend Support::Pattern

  pattern '' do
    it { should     match('')  }
    it { should_not match('/') }

    it { should respond_to(:expand)       }
    it { should respond_to(:to_templates) }


    it { should     generate_template('')              }
    it { should     expand.to('')                      }
    it { should     expand(:ignore, a: 10).to('')      }
    it { should     expand(:append, a: 10).to('?a=10') }
    it { should_not expand(:raise,  a: 10)             }
    it { should_not expand(a: 10)                      }

    example do
      pattern.match('').inspect.should be == '#<Mustermann::SimpleMatch "">'
    end
  end

  pattern '/' do
    it { should     match('/')    }
    it { should_not match('/foo') }

    example { pattern.params('/').should be == {} }
    example { pattern.params('').should be_nil }

    it { should generate_template('/') }
    it { should expand.to('/')         }
  end

  pattern '/foo' do
    it { should     match('/foo')     }
    it { should_not match('/bar')     }
    it { should_not match('/foo.bar') }
  end

  pattern '/foo/bar' do
    it { should match('/foo/bar')   }
    it { should match('/foo%2Fbar') }
    it { should match('/foo%2fbar') }
  end

  pattern '/:foo' do
    it { should     match('/:foo')    }
    it { should     match('/%3Afoo')  }
    it { should_not match('/foo')     }
    it { should_not match('/foo?')    }
    it { should_not match('/foo/bar') }
    it { should_not match('/')        }
    it { should_not match('/foo/')    }

    it { should generate_template('/:foo') }
    it { should expand.to('/:foo')         }
  end

  pattern '/föö' do
    it { should match("/f%C3%B6%C3%B6") }
  end

  pattern '/test$/' do
    it { should match('/test$/') }
  end

  pattern '/te+st/' do
    it { should     match('/te+st/') }
    it { should_not match('/test/')  }
    it { should_not match('/teest/') }
  end

  pattern "/path with spaces" do
    it { should     match('/path%20with%20spaces') }
    it { should_not match('/path%2Bwith%2Bspaces') }
    it { should_not match('/path+with+spaces')     }
    it { should generate_template('/path%20with%20spaces') }
  end

  pattern '/foo&bar' do
    it { should match('/foo&bar') }
  end

  pattern '/test.bar' do
    it { should     match('/test.bar') }
    it { should_not match('/test0bar') }
  end

  pattern '/foo/bar', uri_decode: false do
    it { should     match('/foo/bar')   }
    it { should_not match('/foo%2Fbar') }
    it { should_not match('/foo%2fbar') }
  end

  pattern "/path with spaces", uri_decode: false do
    it { should_not match('/path%20with%20spaces') }
    it { should_not match('/path%2Bwith%2Bspaces') }
    it { should_not match('/path+with+spaces')     }
  end

  context "peeking" do
    subject(:pattern) { Mustermann::Identity.new("foo bar") }

    describe :peek_size do
      example { pattern.peek_size("foo bar blah")   .should be == "foo bar".size }
      example { pattern.peek_size("foo%20bar blah") .should be == "foo%20bar".size }
      example { pattern.peek_size("foobar")         .should be_nil }
    end

    describe :peek_match do
      example { pattern.peek_match("foo bar blah").to_s   .should be == "foo bar" }
      example { pattern.peek_match("foo%20bar blah").to_s .should be == "foo%20bar" }
      example { pattern.peek_match("foobar")              .should be_nil }
    end

    describe :peek_params do
      example { pattern.peek_params("foo bar blah")   .should be == [{}, "foo bar".size] }
      example { pattern.peek_params("foo%20bar blah") .should be == [{}, "foo%20bar".size] }
      example { pattern.peek_params("foobar")         .should be_nil }
    end
  end
end
