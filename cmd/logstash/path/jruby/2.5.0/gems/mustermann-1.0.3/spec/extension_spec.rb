# frozen_string_literal: true
require 'support'
require 'mustermann/extension'
require 'sinatra/base'
require 'rack/test'

describe Mustermann::Extension do
  include Rack::Test::Methods

  subject :app do
    Sinatra.new do
      set :environment, :test
      register Mustermann
    end
  end

  it 'sets up the extension' do
    app.should be_a(Mustermann::Extension)
  end

  context 'uses Sinatra-style patterns by default' do
    before { app.get('/:slug(.:extension)?') { params[:slug] } }
    example { get('/foo')     .body.should be == 'foo'  }
    example { get('/foo.')    .body.should be == 'foo.' }
    example { get('/foo.bar') .body.should be == 'foo'  }
    example { get('/a%20b')   .body.should be == 'a b'  }
  end

  describe :except do
    before { app.get('/auth/*', except: '/auth/login') { 'ok' } }
    example { get('/auth/dunno').should     be_ok }
    example { get('/auth/login').should_not be_ok }
  end

  describe :capture do
    context 'global' do
      before do
        app.set(:pattern, capture: { ext: %w[png jpg gif] })
        app.get('/:slug(.:ext)?') { params[:slug] }
      end

      example { get('/foo.bar').body.should be == 'foo.bar' }
      example { get('/foo.png').body.should be == 'foo'     }
    end

    context 'route local' do
      before do
        app.set(:pattern, nil)
        app.get('/:id', capture: /\d+/) { 'ok' }
      end

      example { get('/42').should be_ok }
      example { get('/foo').should_not be_ok }
    end

    context 'global and route local' do
      context 'global is a hash' do
        before do
          app.set(:pattern, capture: { id: /\d+/ })
          app.get('/:id(.:ext)?', capture: { ext: 'png' }) { ?a }
          app.get('/:id',         capture: { id: 'foo'  }) { ?b }
          app.get('/:id',         capture: :alpha)         { ?c }
        end

        example { get('/20')     .body.should be == ?a }
        example { get('/20.png') .body.should be == ?a }
        example { get('/foo')    .body.should be == ?b }
        example { get('/bar')    .body.should be == ?c }
      end

      context 'global is not a hash' do
        before do
          app.set(:pattern, capture: /\d+/)
          app.get('/:slug(.:ext)?', capture: { ext: 'png' }) { params[:slug] }
          app.get('/:slug', capture: :alpha) { 'ok' }
        end

        example { get('/20.png').should be_ok }
        example { get('/foo.png').should_not be_ok }
        example { get('/foo').should be_ok }

        example { get('/20.png') .body.should be == '20' }
        example { get('/42')     .body.should be == '42' }
        example { get('/foo')    .body.should be == 'ok' }
      end
    end
  end

  describe :pattern do
    describe :except do
      before { app.get('/auth/*', pattern: { except: '/auth/login' }) { 'ok' } }
      example { get('/auth/dunno').should     be_ok }
      example { get('/auth/login').should_not be_ok }
    end

    describe :capture do
      context 'route local' do
        before do
          app.set(:pattern, nil)
          app.get('/:id', pattern: { capture: /\d+/ }) { 'ok' }
        end

        example { get('/42').should be_ok }
        example { get('/foo').should_not be_ok }
      end

      context 'global and route local' do
        context 'global is a hash' do
          before do
            app.set(:pattern, capture: { id: /\d+/ })
            app.get('/:id(.:ext)?', pattern: { capture: { ext: 'png' }}) { ?a }
            app.get('/:id',         pattern: { capture: { id: 'foo'  }}) { ?b }
            app.get('/:id',         pattern: { capture: :alpha })        { ?c }
          end

          example { get('/20')     .body.should be == ?a }
          example { get('/20.png') .body.should be == ?a }
          example { get('/foo')    .body.should be == ?b }
          example { get('/bar')    .body.should be == ?c }
        end

        context 'global is not a hash' do
          before do
            app.set(:pattern, capture: /\d+/)
            app.get('/:slug(.:ext)?', pattern: { capture: { ext: 'png' }}) { params[:slug] }
            app.get('/:slug', pattern: { capture: :alpha }) { 'ok' }
          end

          example { get('/20.png').should be_ok }
          example { get('/foo.png').should_not be_ok }
          example { get('/foo').should be_ok }

          example { get('/20.png') .body.should be == '20' }
          example { get('/42')     .body.should be == '42' }
          example { get('/foo')    .body.should be == 'ok' }
        end
      end
    end

    describe :greedy do
      context 'default' do
        before { app.get('/:name.:ext') { params[:name] }}
        example { get('/foo.bar')     .body.should be == 'foo'     }
        example { get('/foo.bar.baz') .body.should be == 'foo.bar' }
      end

      context 'enabled' do
        before { app.get('/:name.:ext', pattern: { greedy: true }) { params[:name] }}
        example { get('/foo.bar')     .body.should be == 'foo'     }
        example { get('/foo.bar.baz') .body.should be == 'foo.bar' }
      end

      context 'disabled' do
        before { app.get('/:name.:ext', pattern: { greedy: false }) { params[:name] }}
        example { get('/foo.bar')     .body.should be == 'foo' }
        example { get('/foo.bar.baz') .body.should be == 'foo' }
      end

      context 'global' do
        before do
          app.set(:pattern, greedy: false)
          app.get('/:name.:ext') { params[:name] }
        end

        example { get('/foo.bar')     .body.should be == 'foo' }
        example { get('/foo.bar.baz') .body.should be == 'foo' }
      end
    end

    describe :space_matches_plus do
      context 'default' do
        before { app.get('/foo bar') { 'ok' }}
        example { get('/foo%20bar') .should be_ok }
        example { get('/foo+bar')   .should be_ok }
      end

      context 'enabled' do
        before { app.get('/foo bar', pattern: { space_matches_plus: true }) { 'ok' }}
        example { get('/foo%20bar') .should be_ok }
        example { get('/foo+bar')   .should be_ok }
      end

      context 'disabled' do
        before { app.get('/foo bar', pattern: { space_matches_plus: false }) { 'ok' }}
        example { get('/foo%20bar') .should     be_ok }
        example { get('/foo+bar')   .should_not be_ok }
      end

      context 'global' do
        before do
          app.set(:pattern, space_matches_plus: false)
          app.get('/foo bar') { 'ok' }
        end

        example { get('/foo%20bar') .should     be_ok }
        example { get('/foo+bar')   .should_not be_ok }
      end
    end

    describe :uri_decode do
      context 'default' do
        before { app.get('/&') { 'ok' }}
        example { get('/&')   .should be_ok }
        example { get('/%26') .should be_ok }
      end

      context 'enabled' do
        before { app.get('/&', pattern: { uri_decode: true }) { 'ok' }}
        example { get('/&')   .should be_ok }
        example { get('/%26') .should be_ok }
      end

      context 'disabled' do
        before { app.get('/&', pattern: { uri_decode: false }) { 'ok' }}
        example { get('/&')   .should     be_ok }
        example { get('/%26') .should_not be_ok }
      end

      context 'global' do
        before do
          app.set(:pattern, uri_decode: false)
          app.get('/&') { 'ok' }
        end

        example { get('/&')   .should     be_ok }
        example { get('/%26') .should_not be_ok }
      end
    end
  end

  describe :type do
    describe :identity do
      before do
        app.set(:pattern, type: :identity)
        app.get('/:foo') { 'ok' }
      end

      example { get('/:foo').should be_ok }
      example { get('/foo').should_not be_ok }
    end

    describe :rails do
      before do
        app.set(:pattern, type: :rails)
        app.get('/:slug(.:extension)') { params[:slug] }
      end

      example { get('/foo')     .body.should be == 'foo'  }
      example { get('/foo.')    .body.should be == 'foo.' }
      example { get('/foo.bar') .body.should be == 'foo'  }
      example { get('/a%20b')   .body.should be == 'a b'  }
    end

    describe :shell do
      before do
        app.set(:pattern, type: :shell)
        app.get('/{foo,bar}') { 'ok' }
      end

      example { get('/foo').should be_ok }
      example { get('/bar').should be_ok }
    end

    describe :simple do
      before do
        app.set(:pattern, type: :simple)
        app.get('/(a)') { 'ok' }
      end

      example { get('/(a)').should be_ok }
      example { get('/a').should_not be_ok }
    end

    describe :simple do
      before do
        app.set(:pattern, type: :template)
        app.get('/foo{/segments*}{.ext}') { "%p %p" % [params[:segments], params[:ext]] }
      end

      example { get('/foo/a.png').should be_ok }
      example { get('/foo/a').should_not be_ok }

      example { get('/foo/a.png').body.should be == '["a"] "png"' }
      example { get('/foo/a/b.png').body.should be == '["a", "b"] "png"' }
    end
  end

  context 'works with filters' do
    before do
      app.before('/auth/*', except: '/auth/login') { halt 'auth required' }
      app.get('/auth/login') { 'please log in' }
    end

    example { get('/auth/dunno').body.should be == 'auth required' }
    example { get('/auth/login').body.should be == 'please log in' }
  end
end
