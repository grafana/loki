# frozen_string_literal: true
require 'support'
require 'mustermann/mapper'

describe Mustermann::Mapper do
  describe :initialize do
    context 'accepts a block with no arguments, using the return value' do
      subject(:mapper) { Mustermann::Mapper.new(additional_values: :raise) {{ "/foo" => "/bar" }}}
      its(:to_h) { should be == { Mustermann.new("/foo") => Mustermann::Expander.new("/bar") } }
      example { mapper['/foo'].should be == '/bar' }
      example { mapper['/fox'].should be == '/fox' }
    end

    context 'accepts a block with argument, passes instance to it' do
      subject(:mapper) { Mustermann::Mapper.new(additional_values: :raise) { |m| m["/foo"] = "/bar" }}
      its(:to_h) { should be == { Mustermann.new("/foo") => Mustermann::Expander.new("/bar") } }
      example { mapper['/foo'].should be == '/bar' }
      example { mapper['/fox'].should be == '/fox' }
    end

    context 'accepts mappings followed by options' do
      subject(:mapper) { Mustermann::Mapper.new("/foo" => "/bar", additional_values: :raise) }
      its(:to_h) { should be == { Mustermann.new("/foo") => Mustermann::Expander.new("/bar") } }
      example { mapper['/foo'].should be == '/bar' }
      example { mapper['/fox'].should be == '/fox' }
    end

    context 'accepts options followed by mappings' do
      subject(:mapper) { Mustermann::Mapper.new(additional_values: :raise, "/foo" => "/bar") }
      its(:to_h) { should be == { Mustermann.new("/foo") => Mustermann::Expander.new("/bar") } }
      example { mapper['/foo'].should be == '/bar' }
      example { mapper['/fox'].should be == '/fox' }
    end

    context 'allows specifying type' do
      subject(:mapper) { Mustermann::Mapper.new(additional_values: :raise, type: :rails, "/foo" => "/bar") }
      its(:to_h) { should be == { Mustermann.new("/foo", type: :rails) => Mustermann::Expander.new("/bar", type: :rails) } }
      example { mapper['/foo'].should be == '/bar' }
      example { mapper['/fox'].should be == '/fox' }
    end
  end

  describe :convert do
    subject(:mapper) { Mustermann::Mapper.new }

    context 'it maps params' do
      before { mapper["/:a"] = "/:a.html" }
      example { mapper["/foo"]      .should be == "/foo.html" }
      example { mapper["/foo/bar"]  .should be == "/foo/bar"  }
    end

    context 'it supports named splats' do
      before { mapper["/*a"] = "/*a.html" }
      example { mapper["/foo"]      .should be == "/foo.html"     }
      example { mapper["/foo/bar"]  .should be == "/foo/bar.html" }
    end

    context 'can map from patterns' do
      before { mapper[Mustermann.new("/:a")] = "/:a.html" }
      example { mapper["/foo"]      .should be == "/foo.html" }
      example { mapper["/foo/bar"]  .should be == "/foo/bar"  }
    end

    context 'can map to patterns' do
      before { mapper[Mustermann.new("/:a")] = Mustermann.new("/:a.html") }
      example { mapper["/foo"]      .should be == "/foo.html" }
      example { mapper["/foo/bar"]  .should be == "/foo/bar"  }
    end

    context 'can map to expanders' do
      before { mapper[Mustermann.new("/:a")] = Mustermann::Expander.new("/:a.html") }
      example { mapper["/foo"]      .should be == "/foo.html" }
      example { mapper["/foo/bar"]  .should be == "/foo/bar"  }
    end

    context 'can map to array' do
      before { mapper["/:a"] = ["/:a.html", "/:a.:f"] }
      example { mapper["/foo"]             .should be == "/foo.html" }
      example { mapper["/foo", "f" => 'x'] .should be == "/foo.x"    }
      example { mapper["/foo", f: 'x']     .should be == "/foo.x"    }
      example { mapper["/foo/bar"]         .should be == "/foo/bar"  }
    end
  end
end
