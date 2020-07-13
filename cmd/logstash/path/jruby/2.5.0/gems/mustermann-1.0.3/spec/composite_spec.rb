# frozen_string_literal: true
require 'support'
require 'mustermann'

describe Mustermann::Composite do
  describe :new do
    example 'with no argument' do
      expect { Mustermann::Composite.new }.
        to raise_error(ArgumentError, 'cannot create empty composite pattern')
    end

    example 'with one argument' do
      pattern = Mustermann.new('/foo')
      Mustermann::Composite.new(pattern).should be == pattern
    end

    example 'with supported type specific arguments' do
      Mustermann::Composite.new("/a", "/b", greedy: true)
    end

    example 'with unsupported type specific arguments' do
      expect { Mustermann::Composite.new("/a", "/b", greedy: true, type: :identity) }.to raise_error(ArgumentError)
    end
  end

  context :| do
    subject(:pattern) { Mustermann.new('/foo/:name', '/:first/:second') }

    describe :== do
      example { subject.should     be == subject                                                       }
      example { subject.should     be == Mustermann.new('/foo/:name', '/:first/:second')               }
      example { subject.should_not be == Mustermann.new('/foo/:name')                                  }
      example { subject.should_not be == Mustermann.new('/foo/:name', '/:first/:second', operator: :&) }
    end

    describe :=== do
      example { subject.should     be === "/foo/bar" }
      example { subject.should     be === "/fox/bar" }
      example { subject.should_not be === "/foo"     }
    end

    describe :params do
      example { subject.params("/foo/bar") .should be == { "name"  => "bar" }                    }
      example { subject.params("/fox/bar") .should be == { "first" => "fox", "second" => "bar" } }
      example { subject.params("/foo")     .should be_nil                                        }
    end

    describe :=== do
      example { subject.should     match("/foo/bar") }
      example { subject.should     match("/fox/bar") }
      example { subject.should_not match("/foo")     }
    end

    describe :expand do
      example { subject.should respond_to(:expand) }
      example { subject.expand(name: 'bar')                 .should be == '/foo/bar' }
      example { subject.expand(first: 'fox', second: 'bar') .should be == '/fox/bar' }

      context "without expandable patterns" do
        subject(:pattern) { Mustermann.new('/foo/:name', '/:first/:second', type: :simple) }
        example { subject.should_not respond_to(:expand) }
        example { expect { subject.expand(name: 'bar') }.to raise_error(NotImplementedError) }
      end
    end

    describe :to_templates do
      example { should respond_to(:to_templates) }
      example { should generate_templates('/foo/{name}', '/{first}/{second}') }

      context "without patterns implementing to_templates" do
        subject(:pattern) { Mustermann.new('/foo/:name', '/:first/:second', type: :simple) }
        example { should_not respond_to(:to_templates) }
        example { expect { subject.to_templates }.to raise_error(NotImplementedError) }
      end
    end

    describe :eql? do
      example { should     be_eql(pattern)                                                       }
      example { should     be_eql(Mustermann.new('/foo/:name', '/:first/:second', operator: :|)) }
      example { should_not be_eql(Mustermann.new('/bar/:name', '/:first/:second', operator: :|)) }
      example { should_not be_eql(Mustermann.new('/foo/:name', '/:first/:second', operator: :&)) }
    end
  end

  context :& do
    subject(:pattern) { Mustermann.new('/foo/:name', '/:first/:second', operator: :&) }

    describe :== do
      example { subject.should     be == subject                                                       }
      example { subject.should     be == Mustermann.new('/foo/:name', '/:first/:second', operator: :&) }
      example { subject.should_not be == Mustermann.new('/foo/:name')                                  }
      example { subject.should_not be == Mustermann.new('/foo/:name', '/:first/:second')               }
    end

    describe :=== do
      example { subject.should     be === "/foo/bar" }
      example { subject.should_not be === "/fox/bar" }
      example { subject.should_not be === "/foo"     }
    end

    describe :params do
      example { subject.params("/foo/bar") .should be == { "name"  => "bar" } }
      example { subject.params("/fox/bar") .should be_nil                     }
      example { subject.params("/foo")     .should be_nil                     }
    end

    describe :match do
      example { subject.should     match("/foo/bar") }
      example { subject.should_not match("/fox/bar") }
      example { subject.should_not match("/foo")     }
    end

    describe :expand do
      example { subject.should_not respond_to(:expand) }
      example { expect { subject.expand(name: 'bar') }.to raise_error(NotImplementedError) }
    end
  end

  context :^ do
    subject(:pattern) { Mustermann.new('/foo/:name', '/:first/:second', operator: :^) }

    describe :== do
      example { subject.should     be == subject                                                       }
      example { subject.should_not be == Mustermann.new('/foo/:name', '/:first/:second')               }
      example { subject.should_not be == Mustermann.new('/foo/:name')                                  }
      example { subject.should_not be == Mustermann.new('/foo/:name', '/:first/:second', operator: :&) }
    end

    describe :=== do
      example { subject.should_not be === "/foo/bar" }
      example { subject.should     be === "/fox/bar" }
      example { subject.should_not be === "/foo"     }
    end

    describe :params do
      example { subject.params("/foo/bar") .should be_nil                                        }
      example { subject.params("/fox/bar") .should be == { "first" => "fox", "second" => "bar" } }
      example { subject.params("/foo")     .should be_nil                                        }
    end

    describe :match do
      example { subject.should_not match("/foo/bar") }
      example { subject.should     match("/fox/bar") }
      example { subject.should_not match("/foo")     }
    end

    describe :expand do
      example { subject.should_not respond_to(:expand) }
      example { expect { subject.expand(name: 'bar') }.to raise_error(NotImplementedError) }
    end
  end

  describe :inspect do
    let(:sinatra)  { Mustermann.new('x')                  }
    let(:shell)    { Mustermann.new('x', type: :shell)    }
    let(:identity) { Mustermann.new('x', type: :identity) }

    example { (sinatra | shell)            .inspect.should include('(sinatra:"x" | shell:"x")')                  }
    example { (sinatra ^ shell)            .inspect.should include('(sinatra:"x" ^ shell:"x")')                  }
    example { (sinatra | shell | identity) .inspect.should include('(sinatra:"x" | shell:"x" | identity:"x")')   }
    example { (sinatra | shell & identity) .inspect.should include('(sinatra:"x" | (shell:"x" & identity:"x"))') }
  end
end
