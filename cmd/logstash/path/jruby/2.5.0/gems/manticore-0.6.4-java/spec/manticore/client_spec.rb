# encoding: utf-8
require "spec_helper"

java_import "org.apache.http.entity.mime.MultipartEntityBuilder"
java_import "org.apache.http.entity.ContentType"

describe Manticore::Client do
  let(:client) { Manticore::Client.new }

  it "fetches a URL and return a response" do
    expect(client.get(local_server)).to be_a Manticore::Response
  end

  it "resolves redirections" do
    response = client.get(local_server, headers: {"X-Redirect" => "/foobar"})
    expect(response.code).to eq 200
    expect(response.final_url).to eq URI(local_server("/foobar"))
  end

  it "accepts custom headers" do
    response = client.get(local_server, headers: {"X-Custom-Header" => "Blaznotts"})
    json = JSON.load(response.body)
    expect(json["headers"]["X-Custom-Header"]).to eq "Blaznotts"
  end

  it "accepts repeated header values" do
    response = client.get(local_server, headers: {"X-Custom-Header" => ["Whizzles", "Blaznotts"]})
    json = JSON.load(response.body)
    expect(json["headers"]["X-Custom-Header"].sort).to eq ["Blaznotts", "Whizzles"]
  end

  it "enables compression" do
    response = client.get(local_server)
    json = JSON.load(response.body)
    expect(json["headers"]).to have_key "Accept-Encoding"
    expect(json["headers"]["Accept-Encoding"]).to match "gzip"
  end

  it "authenticates" do
    expect(client.get(local_server("/auth")).code).to eq 401
    expect(client.get(local_server("/auth"), auth: {user: "user", pass: "pass"}).code).to eq 200
  end

  it "authenticates with eager auth" do
    expect(client.get(local_server("/auth"), auth: {user: "user", pass: "pass", eager: true}).code).to eq 200
  end

  it "proxies" do
    j = JSON.parse(client.get(local_server("/proxy"), proxy: "http://localhost:55442").body)
    expect(j["server_port"]).to eq 55442
    expect(j["uri"]["port"]).to eq 55441
  end

  it "automatically decodes application/JSON as UTF-8" do
    j = JSON.parse client.get(local_server("/json_utf8")).body
    expect(j["last_name"]).to eq "Töger"
  end

  context "via an authenticated proxy" do
    let(:proxy) { "http://localhost:55442" }
    let(:auth) { nil }
    let(:j) { JSON.parse req.body }

    context "with authentication as a hash" do
      let(:auth) { {user: "user", pass: "pass"} }
      let(:req) { client.get(local_server("/authproxy"), proxy: proxy, auth: auth) }

      it "proxies" do
        expect(j["server_port"]).to eq 55442
        expect(j["uri"]["port"]).to eq 55441
        expect(j["headers"]["Proxy-Authorization"]).to eq "Basic dXNlcjpwYXNz"
      end
    end

    context "with authentication as a string" do
      let(:proxy) { "http://user:pass@localhost:55442" }
      let(:req) { client.get(local_server("/authproxy"), proxy: proxy) }

      it "proxies" do
        expect(j["server_port"]).to eq 55442
        expect(j["uri"]["port"]).to eq 55441
        expect(j["headers"]["Proxy-Authorization"]).to eq "Basic dXNlcjpwYXNz"
      end
    end
  end

  describe "with a custom user agent" do
    let(:client) { Manticore::Client.new user_agent: "test-agent/1.0" }

    it "uses the specified UA" do
      response = client.get(local_server("/"))
      json = JSON.load(response.body)
      expect(json["headers"]["User-Agent"]).to eq "test-agent/1.0"
    end
  end

  describe "ignore_ssl_validation (deprecated option)" do
    context "when on" do
      let(:client) { Manticore::Client.new ssl: {verify: false} }

      it "does not break on SSL validation errors" do
        expect { client.get("https://localhost:55444/").body }.to_not raise_exception
      end
    end

    context "when off" do
      let(:client) { Manticore::Client.new ssl: {verify: true} }

      it "breaks on SSL validation errors" do
        expect { client.get("https://localhost:55444/").call }.to raise_exception(Manticore::ClientProtocolException)
      end
    end
  end

  describe "ssl settings" do
    describe "verify" do
      context "default" do
        let(:client) { Manticore::Client.new }

        it "breaks on SSL validation errors" do
          expect { client.get("https://localhost:55444/").call }.to raise_exception(Manticore::ClientProtocolException)
        end

        it "breaks on SSL expiry errors" do
          expect { client.get("https://localhost:55446/").call }.to raise_exception(Manticore::ClientProtocolException)
        end
      end

      context "when on and no trust store is given" do
        let(:client) { Manticore::Client.new :ssl => {:verify => :strict} }

        it "breaks on SSL validation errors" do
          expect { client.get("https://localhost:55444/").call }.to raise_exception(Manticore::ClientProtocolException)
        end
      end

      context "when on and custom trust store is given" do
        let(:client) { Manticore::Client.new :ssl => {verify: :strict, truststore: File.expand_path("../../ssl/truststore.jks", __FILE__), truststore_password: "test123"} }

        it "verifies the request and succeed" do
          expect { client.get("https://localhost:55444/").body }.to_not raise_exception
        end
      end

      context "when the client specifies a protocol list" do
        let(:client) { Manticore::Client.new :ssl => {verify: :strict, truststore: File.expand_path("../../ssl/truststore.jks", __FILE__), truststore_password: "test123", protocols: ["TLSv1", "TLSv1.1", "TLSv1.2"]} }

        it "verifies the request and succeed" do
          expect { client.get("https://localhost:55444/").body }.to_not raise_exception
        end
      end

      context "when on and custom trust store is given with the wrong password" do
        let(:client) { Manticore::Client.new :ssl => {verify: :strict, truststore: File.expand_path("../../ssl/truststore.jks", __FILE__), truststore_password: "wrongpass"} }

        it "fails to load the keystore" do
          expect { client.get("https://localhost:55444/").body }.to raise_exception(Java::JavaIo::IOException)
        end
      end

      context "when ca_file is given" do
        let(:client) { Manticore::Client.new :ssl => {verify: :strict, ca_file: File.expand_path("../../ssl/root-ca.crt", __FILE__)} }

        it "verifies the request and succeed" do
          expect { client.get("https://localhost:55444/").body }.to_not raise_exception
        end
      end

      context "when client_cert and client_key are given as file paths" do
        let(:client) {
          Manticore::Client.new(
            :ssl => {
              verify: :strict,
              ca_file: File.expand_path("../../ssl/root-ca.crt", __FILE__),
              client_cert: File.expand_path("../../ssl/client.crt", __FILE__),
              client_key: File.expand_path("../../ssl/client.key", __FILE__),
            },
          )
        }

        it "successfully auths requests" do
          expect(client.get("https://localhost:55445/").body).to match("hello")
        end
      end

      context "when client_cert and client_key are given as OpenSSL::X509::Certificate" do
        let(:client) {
          Manticore::Client.new(
            :ssl => {
              verify: :strict,
              ca_file: File.expand_path("../../ssl/root-ca.crt", __FILE__),
              client_cert: OpenSSL::X509::Certificate.new(File.read(File.expand_path("../../ssl/client.crt", __FILE__))),
              client_key: OpenSSL::PKey::RSA.new(File.read(File.expand_path("../../ssl/client.key", __FILE__))),
            },
          )
        }

        it "successfully auths requests" do
          expect(client.get("https://localhost:55445/").body).to match("hello")
        end
      end

      context "when client_cert and client_key are given as strings" do
        let(:client) {
          Manticore::Client.new(
            :ssl => {
              verify: :strict,
              ca_file: File.expand_path("../../ssl/root-ca.crt", __FILE__),
              client_cert: File.read(File.expand_path("../../ssl/client.crt", __FILE__)),
              client_key: File.read(File.expand_path("../../ssl/client.key", __FILE__)),
            },
          )
        }

        it "successfully auths requests" do
          expect(client.get("https://localhost:55445/").body).to match("hello")
        end
      end

      context "when off" do
        let(:client) { Manticore::Client.new :ssl => {:verify => :disable} }

        it "does not break on SSL validation errors" do
          expect { client.get("https://localhost:55444/").body }.to_not raise_exception
        end

        it "does not break on expired SSL certificates" do
          expect { client.get("https://localhost:55446/").body }.to_not raise_exception
        end
      end

      context "against a server that verifies clients" do
        context "when client cert auth is provided" do
          let(:client) {
            options = {
              truststore: File.expand_path("../../ssl/truststore.jks", __FILE__),
              truststore_password: "test123",
              keystore: File.expand_path("../../ssl/client.p12", __FILE__),
              keystore_password: "test123",
              verify: :strict,
            }
            Manticore::Client.new :ssl => options
          }

          it "successfully auths requests" do
            expect(client.get("https://localhost:55445/").body).to match("hello")
          end
        end

        context "when client cert auth is not provided" do
          let(:client) {
            options = {
              truststore: File.expand_path("../../ssl/truststore.jks", __FILE__),
              truststore_password: "test123",
              verify: :strict,
            }
            Manticore::Client.new :ssl => options
          }

          it "fails the request" do
            # oraclejdk7 throws a SocketException here, oraclejdk8/openjdk7 throw ClientProtocolException
            expect { client.get("https://localhost:55445/").body }.to raise_exception(Manticore::ManticoreException)
          end
        end
      end
    end

    describe ":cipher_suites" do
      skip
    end

    describe ":protocols" do
      skip
    end
  end

  describe "lazy evaluation" do
    it "does not call synchronous requests by default" do
      req = client.get(local_server)
      expect(req).to_not be_called
    end

    context "given a lazy request" do
      subject { client.get(local_server) }

      before do
        expect(subject).to_not be_called
        expect(subject).to receive(:call).once.and_call_original
      end

      specify { expect { subject.body }.to change { subject.called? } }
      specify { expect { subject.headers }.to change { subject.called? } }
      specify { expect { subject.final_url }.to change { subject.called? } }
      specify { expect { subject.code }.to change { subject.called? } }
      specify { expect { subject.length }.to change { subject.called? } }
      specify { expect { subject.cookies }.to change { subject.called? } }
    end

    it "automatically calls synchronous requests that pass a handler block" do
      req = client.get(local_server) { |r| }
      expect(req).to be_called
    end

    it "does not call asynchronous requests even if a block is passed" do
      req = client.async.get(local_server) { |r| }
      expect(req).to_not be_called
    end

    it "does not call asynchronous requests when on_success is passed" do
      req = client.async.get(local_server).on_success { |r| }
      expect(req).to_not be_called
    end

    it "calls async requests on client execution" do
      req = client.async.get(local_server).on_success { |r| }
      expect { client.execute! }.to change { req.called? }.from(false).to(true)
    end

    describe "with a bad port number" do
      it "returns a Manticore::InvalidArgumentException" do
        failure = nil
        client.async.get(local_server("/", 65536)).
          on_failure { |f| failure = f }
        client.execute!
        expect(failure).to be_a(Manticore::InvalidArgumentException)
      end
    end

    describe "mysterious failures" do
      it "still returns an error to on failure" do
        failure = nil
        # I'm not crazy about reaching into the client via instance_variable_get here, but it works.
        expect(client.instance_variable_get("@client")).to receive(:execute).and_raise(StandardError.new("Uh oh"))
        client.async.get(local_server).
          on_failure { |f| failure = f }
        client.execute!
        expect(failure).to be_a(StandardError)
      end
    end
  end

  context "when client-wide cookie management is disabled" do
    let(:client) { Manticore::Client.new cookies: false }

    it "persists cookies across multiple redirects from a single request" do
      response = client.get(local_server("/cookies/1/2"))
      expect(response.final_url.to_s).to eq local_server("/cookies/2/2")
      expect(response.cookies["x"]).to be_nil
      expect(response.headers["set-cookie"]).to match(/1/)
    end

    it "does not persist cookies between requests" do
      response = client.get(local_server("/cookies/1/2"))
      expect(response.final_url.to_s).to eq local_server("/cookies/2/2")
      expect(response.cookies["x"]).to be_nil
      expect(response.headers["set-cookie"]).to match(/1/)

      response = client.get(local_server("/cookies/1/2"))
      expect(response.final_url.to_s).to eq local_server("/cookies/2/2")
      expect(response.cookies["x"]).to be_nil
      expect(response.headers["set-cookie"]).to match(/1/)
    end
  end

  context "when client-wide cookie management is set to per-request" do
    let(:client) { Manticore::Client.new cookies: :per_request }

    it "persists cookies across multiple redirects from a single request" do
      response = client.get(local_server("/cookies/1/2"))
      expect(response.final_url.to_s).to eq local_server("/cookies/2/2")
      expect(response.headers["set-cookie"]).to match(/2/)
      expect(response.cookies["x"].first.value).to eq "2"
    end

    it "does not persist cookies between requests" do
      response = client.get(local_server("/cookies/1/2"))
      expect(response.final_url.to_s).to eq local_server("/cookies/2/2")
      expect(response.headers["set-cookie"]).to match(/2/)
      expect(response.cookies["x"].first.value).to eq "2"

      response = client.get(local_server("/cookies/1/2"))
      expect(response.final_url.to_s).to eq local_server("/cookies/2/2")
      expect(response.headers["set-cookie"]).to match(/2/)
      expect(response.cookies["x"].first.value).to eq "2"
    end
  end

  context "when client-wide cookie management is enabled" do
    let(:client) { Manticore::Client.new cookies: true }

    it "persists cookies across multiple redirects from a single request" do
      response = client.get(local_server("/cookies/1/2"))
      expect(response.final_url.to_s).to eq local_server("/cookies/2/2")
      expect(response.cookies["x"].first.value).to eq "2"
    end

    it "persists cookies between requests" do
      response = client.get(local_server("/cookies/1/2"))
      expect(response.final_url.to_s).to eq local_server("/cookies/2/2")
      expect(response.cookies["x"].first.value).to eq "2"

      response = client.get(local_server("/cookies/1/2"))
      expect(response.final_url.to_s).to eq local_server("/cookies/2/2")
      expect(response.cookies["x"].first.value).to eq "4"
    end
  end

  context "when compression is disabled" do
    let(:client) {
      Manticore::Client.new do |client, request_config|
        client.disable_content_compression
      end
    }

    it "disables compression" do
      response = client.get(local_server)
      json = JSON.load(response.body)
      expect(json["headers"]["Accept-Encoding"]).to be_nil
    end
  end

  context "when no response charset is specified" do
    let(:content_type) { "text/plain" }

    it "decodes response bodies according to the content-type header" do
      expect(client.get(local_server, headers: {"X-Content-Type" => content_type}).body.encoding.name).to eq "ISO-8859-1"
    end
  end

  context "when an invalid response charset is specified" do
    let(:content_type) { "text/plain; charset=bogus" }

    it "decodes the content as UTF-8" do
      expect(client.get(local_server, headers: {"X-Content-Type" => content_type}).body.encoding.name).to eq "ISO-8859-1"
    end
  end

  context "when the response charset is UTF-8" do
    let(:content_type) { "text/plain; charset=utf-8" }

    it "decodes response bodies according to the content-type header" do
      expect(client.get(local_server, headers: {"X-Content-Type" => content_type}).body.encoding.name).to eq "UTF-8"
    end
  end

  describe "#get" do
    it "works" do
      response = client.get(local_server)
      expect(JSON.load(response.body)["method"]).to eq "GET"
    end

    it "works with a URI object" do
      response = client.get(URI.parse local_server)
      expect(JSON.load(response.body)["method"]).to eq "GET"
    end

    it "send a query" do
      response = client.get local_server, query: {foo: "bar"}
      expect(CGI.parse(JSON.load(response.body)["uri"]["query"])["foo"]).to eq ["bar"]
    end

    it "sends a body" do
      response = client.get(local_server, body: "This is a post body")
      expect(JSON.load(response.body)["body"]).to eq "This is a post body"
    end

    it "can send an array of parameters as :params" do
      response = client.get(local_server, params: {"foo" => ["baz", "bar"], "bar" => {"baz" => ["bin", 1, :b]}})
      j = JSON.load(response.body)
      expect(j["body"]).to eq ""
      expect(j["uri"]["query"]).to include("foo=baz")
    end

    it "can send an array of parameters as :query" do
      response = client.get(local_server, query: {"foo" => ["baz", "bar"]})
      j = JSON.load(response.body)
      expect(j["body"]).to eq ""
      expect(j["uri"]["query"]).to include("foo=baz")
    end

    it "sends non-ASCII params" do
      response = client.get(local_server, query: {"∑" => "√"})
      j = JSON.load(response.body)
      expect(CGI.unescape j["uri"]["query"]).to eq "∑=√"
    end

    it "merges implicit query parameters with explicit ones" do
      response = client.get(local_server + "?foo=bar", query: {"baz" => "bin"})
      j = JSON.load(response.body)
      expect(CGI.parse j["uri"]["query"]).to eq({"foo" => ["bar"], "baz" => ["bin"]})
    end
  end

  describe "#post" do
    it "works" do
      response = client.post(local_server)
      expect(JSON.load(response.body)["method"]).to eq "POST"
    end

    it "sends a body" do
      response = client.post(local_server, body: "This is a post body")
      expect(JSON.load(response.body)["body"]).to eq "This is a post body"
    end

    it "sends a UTF-8 body" do
      response = client.post(local_server, body: "This is a post body ∑")
      expect(JSON.load(response.body)["body"]).to eq "This is a post body ∑"
    end

    it "sends params" do
      response = client.post(local_server, params: {key: "value"})
      expect(CGI.unescape(JSON.load(response.body)["body"])).to eq "key=value"
    end

    it "sends non-ASCII params" do
      response = client.post(local_server, params: {"∑" => "√"})
      expect(CGI.unescape(JSON.load(response.body)["body"])).to eq "∑=√"
    end

    it "sends a binary body" do
      bytes = [145, 167, 116, 101, 115, 116, 49, 50, 51].pack("c*").encode("binary")
      response = client.post(local_server, body: bytes, headers: {"X-Base64" => "1"})
      expect(Base64.decode64(JSON.load(response.body)["body"])).to eq bytes
    end

    it "sends an arbitrary entity" do
      f = open(File.expand_path(File.join(__FILE__, "..", "..", "spec_helper.rb")), "r").to_inputstream
      multipart_entity = MultipartEntityBuilder.create.add_text_body("foo", "bar").add_binary_body("whatever", f, ContentType::TEXT_PLAIN, __FILE__)
      response = client.post(local_server, entity: multipart_entity.build)
      expect(response.body).to match "RSpec.configure"
    end
  end

  describe "#put" do
    it "works" do
      response = client.put(local_server)
      expect(JSON.load(response.body)["method"]).to eq "PUT"
    end

    it "sends a body" do
      response = client.put(local_server, body: "This is a put body")
      expect(JSON.load(response.body)["body"]).to eq "This is a put body"
    end

    it "sends params" do
      response = client.put(local_server, params: {key: "value"})
      expect(JSON.load(response.body)["body"]).to eq "key=value"
    end
  end

  describe "#delete" do
    it "works" do
      response = client.delete(local_server)
      expect(JSON.load(response.body)["method"]).to eq "DELETE"
    end

    it "sends a body" do
      response = client.delete(local_server, body: "This is a delete body")
      expect(JSON.load(response.body)["body"]).to eq "This is a delete body"
    end
  end

  describe "#head" do
    it "works" do
      response = client.head(local_server)
      expect(JSON.load(response.body)).to be_nil
    end
  end

  describe "#options" do
    it "works" do
      response = client.options(local_server)
      expect(JSON.load(response.body)["method"]).to eq "OPTIONS"
    end
  end

  describe "#patch" do
    it "works" do
      response = client.patch(local_server)
      expect(JSON.load(response.body)["method"]).to eq "PATCH"
    end

    it "sends a body" do
      response = client.patch(local_server, body: "This is a patch body")
      expect(JSON.load(response.body)["body"]).to eq "This is a patch body"
    end

    it "sends params" do
      response = client.patch(local_server, params: {key: "value"})
      expect(JSON.load(response.body)["body"]).to eq "key=value"
    end
  end

  describe "#execute!" do
    it "performs multiple concurrent requests" do
      futures = [55441, 55442].map do |port|
        client.async.get("http://localhost:#{port}/?sleep=1").
          on_success do |response|
          Time.now.to_f
        end
      end

      client.execute!
      values = futures.map(&:callback_result)
      expect((values[0] - values[1]).abs).to be < 0.25
    end

    it "returns the results of the handler blocks" do
      [55441, 55442].each do |port|
        client.async.get("http://localhost:#{port}/").
          on_success { |response, request| "Result" }
      end

      expect(client.execute!.map(&:callback_result)).to eq ["Result", "Result"]
    end
  end

  describe "#clear_pending" do
    it "removes pending requests" do
      ran = false
      client.async.get("http://google.com").on_success { |r| ran = true }
      client.clear_pending
      expect(client.execute!).to be_empty
      expect(ran).to be false
    end
  end

  describe "#stub" do
    it "responds with a stubbed response until it is unstubbed" do
      client.stub(local_server, body: "body", code: 200)

      called = false
      2.times {
        client.get(local_server) do |response|
          called = true
          expect(response).to be_a Manticore::StubbedResponse
          expect(response.body).to eq "body"
          expect(response.code).to eq 200
        end
      }

      expect(called).to be true

      client.clear_stubs!
      client.get(local_server) do |response|
        expect(response).to be_a Manticore::Response
        expect(response.body).to match /Manticore/
        expect(response.code).to eq 200
      end
    end

    context "stubbing" do
      it "only the provided URLs" do
        client.stub local_server, body: "body"
        client.async.get(local_server).on_success { |r| expect(r).to be_a Manticore::StubbedResponse }
        client.async.get(local_server("/other")).on_success { |r| expect(r).to_not be_a Manticore::StubbedResponse }
        client.execute!
      end

      it "by regex matching" do
        client.stub %r{#{local_server("/foo")}}, body: "body"
        client.async.get(local_server("/foo")).on_success { |r| expect(r).to be_a Manticore::StubbedResponse }
        client.async.get(local_server("/bar")).on_success { |r| expect(r).to_not be_a Manticore::StubbedResponse }
        client.execute!
      end

      it "strictly matches string stubs" do
        client.stub local_server("/foo"), body: "body"
        client.async.get(local_server("/foo")).on_success { |r| expect(r).to be_a Manticore::StubbedResponse }
        client.async.get(local_server("/other")).on_success { |r| expect(r).to_not be_a Manticore::StubbedResponse }
        client.execute!
      end

      it "matches stubs with query strings" do
        url = "http://google.com?k=v"
        client.stub(url, body: "response body", code: 200)
        client.get(url) do |response|
          expect(response.body).to eq("response body")
        end
      end

      it "persists the stub for non-block calls" do
        url = "http://google.com"
        client.stub(url, body: "response body", code: 200)
        response = client.get(url)
        expect(response.body).to eq("response body")
      end
    end
  end

  describe "#close" do
    it "makes the client unusable" do
      client.close
      response = client.head(local_server)
      expect { response.body }.to raise_error(Manticore::ClientStoppedException)
    end
  end

  describe "keepalive" do
    let(:url) { "http://www.facebook.com/" }

    context "with keepalive" do
      it "adds the Connection: Keep-Alive header for http/1.0" do
        expect(client.get(url).request["Connection"]).to eq "Keep-Alive"
      end

      let(:client) { Manticore::Client.new keepalive: true, pool_max: 1 }

      it "keeps the connection open after a request" do
        skip
        response = client.get(url).call
        get_connection(client, url) do |conn|
          expect(conn.is_open).to be true
        end
      end
    end

    context "without keepalive" do
      let(:client) { Manticore::Client.new keepalive: false, pool_max: 1 }

      it "does not add the Connection: Keep-Alive header for http/1.0" do
        expect(client.get(url).request["Connection"]).to be_nil
      end

      it "closes the connection after a request" do
        skip
        response = client.get(url).call
        puts `netstat -apn`
        # get_connection(client, url) do |conn|
        #   conn.is_open.should be false
        # end
      end
    end
  end

  context "with a misbehaving endpoint" do
    let(:port) do
      p = 4000
      server = nil
      begin
        server = TCPServer.new p
      rescue Errno::EADDRINUSE
        p += 1
        retry
      ensure
        server.close
      end
      p
    end

    before do
      @socket = TCPServer.new port
      @server = Thread.new do
        loop do
          begin
            client = @socket.accept
            client.puts([
              "HTTP/1.1 200 OK",
              "Keep-Alive: timeout=3000",
              "Connection: Keep-Alive",
              "Content-Length: 6",
              "",
              "Hello!",
            ].join("\n"))
            client.close
          rescue IOError => e
            break
          end
        end
      end
    end

    after do
      @server.kill
    end

    let(:client) { Manticore::Client.new keepalive: true, pool_max: 1 }

    it "retries 3 times by default" do
      # The first time, reply with keepalive, then close the connection
      # The second connection should succeed

      request1 = client.get("http://localhost:#{port}/")
      request2 = client.get("http://localhost:#{port}/")
      expect { request1.call }.to_not raise_exception
      expect { request2.call }.to_not raise_exception

      expect(request1.times_retried).to eq 0
      expect(request2.times_retried).to eq 1
    end

    context "when the max retry is restrictive" do
      let(:client) { Manticore::Client.new keepalive: true, pool_max: 1, automatic_retries: 0 }

      it "retries 0 times and fail on the second request" do
        # The first time, reply with keepalive, then close the connection
        # The second connection should succeed
        expect { client.get("http://localhost:#{port}/").call }.to_not raise_exception
        expect { client.get("http://localhost:#{port}/").call }.to raise_exception(Manticore::SocketException)
      end
    end

    context "when keepalive is off" do
      let(:client) { Manticore::Client.new keepalive: false, pool_max: 1 }

      it "succeeds without any retries" do
        # The first time, reply with keepalive, then close the connection
        # The second connection should succeed
        request1 = client.get("http://localhost:#{port}/")
        request2 = client.get("http://localhost:#{port}/")
        expect { request1.call }.to_not raise_exception
        expect { request2.call }.to_not raise_exception

        expect(request1.times_retried).to eq 0
        expect(request2.times_retried).to eq 0
      end
    end

    after do
      Thread.kill @server
      begin
        @socket.close
      rescue IOError
        # pass
      end
    end
  end

  describe "with connection timeouts" do
    let(:client) { Manticore::Client.new request_timeout: 1, connect_timeout: 1, socket_timeout: 1 }

    it "times out" do
      expect { client.get(local_server "/?sleep=2").body }.to raise_exception(Manticore::SocketTimeout)
    end

    it "times out when custom request options are passed" do
      expect { client.get(local_server("/?sleep=2"), max_redirects: 5).body }.to raise_exception(Manticore::SocketTimeout)
    end
  end

  describe "request types:" do
    describe "sync" do
      it "returns a response" do
        expect(client.get(local_server)).to be_a Manticore::Response
      end

      it "evaluates immediately when given a block" do
        called = false
        expect { client.get(local_server) { called = true } }.to change { called }.to(true)
      end

      it "can be evaluated synchronously on-demand" do
        expect(client.get(local_server).body).to be_a String
      end
    end

    describe "async" do
      it "returns a response" do
        expect(client.async.get(local_server)).to be_a Manticore::Response
      end

      it "works with `parallel`" do
        expect(client.parallel.get(local_server)).to be_a Manticore::Response
      end

      it "works with `batch`" do
        expect(client.parallel.get(local_server)).to be_a Manticore::Response
      end

      it "does not evaluate immediately when given a block" do
        called = false
        expect { client.async.get(local_server) { called = true }; sleep 0.5 }.to_not change { called }
      end

      it "can be evaluated synchronously on-demand" do
        expect(client.async.get(local_server).body).to be_a String
      end
    end

    describe "background" do
      it "returns a response" do
        expect(client.background.get(local_server)).to be_a Manticore::Response
      end

      it "has the background flag set" do
        expect(client.background.get(local_server).background).to eq true
      end

      it "returns a future when called" do
        expect(client.background.get(local_server).call).to be_a Java::JavaUtilConcurrent::FutureTask
      end

      it "evaluates immediately when given a block" do
        called = false
        expect { (client.background.get(local_server) { called = true }).get }.to change { called }.to(true)
      end

      it "raises an exception when a sync method is called on it" do
        called = false
        expect { client.background.get(local_server).body }.to raise_exception(RuntimeError)
      end
    end
  end

  def get_connection(client, uri, &block)
    java_import "java.util.concurrent.TimeUnit"
    host = URI.parse(uri).host
    pool = client.instance_variable_get("@pool")
    req = pool.requestConnection(Java::OrgApacheHttpConnRouting::HttpRoute.new(Java::OrgApacheHttp::HttpHost.new(host)), nil)
    conn = req.get(3, TimeUnit::SECONDS)
    begin
      yield conn
    ensure
      pool.releaseConnection(conn, nil, 0, TimeUnit::SECONDS)
    end
  end
end
