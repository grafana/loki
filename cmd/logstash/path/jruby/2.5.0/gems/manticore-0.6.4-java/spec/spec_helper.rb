# encoding: utf-8
require "rubygems"
require "bundler/setup"
require "simplecov"

SimpleCov.start do
  add_filter "spec/"
end

require "manticore"
require "zlib"
require "json"
require "rack"
require "webrick"
require "webrick/https"
require "openssl"
require "rspec/its"
require "logger"

PORT = 55441

def local_server(path = "/", port = PORT)
  URI.join("http://localhost:#{port}", path).to_s
end

Thread.abort_on_exception = true
Thread.report_on_exception = false if Thread.respond_to?(:report_on_exception)

def read_nonblock(socket)
  buffer = ""
  loop {
    begin
      buffer << socket.read_nonblock(4096)
    rescue Errno::EAGAIN
      # Resource temporarily unavailable - read would block
      break
    end
  }
  buffer.force_encoding("UTF-8")
end

def start_server(port = PORT)
  @servers ||= {}
  @servers[port] = Thread.new {
    Net::HTTP::Server.run(port: port, log: open("/dev/null", "a")) do |request, stream|
      query = Rack::Utils.parse_query(request[:uri][:query].to_s)
      if query["sleep"]
        sleep(query["sleep"].to_f)
      end

      if cl = request[:headers]["Content-Length"] || request[:headers]["Transfer-Encoding"] == "chunked"
        request[:body] = read_nonblock(stream.socket)
      end

      content_type = request[:headers]["X-Content-Type"] || "application/json"
      if request[:uri][:path] == "/auth"
        if request[:headers]["Authorization"] == "Basic dXNlcjpwYXNz"
          payload = JSON.dump(request)
          [200, {"Content-Type" => content_type, "Content-Length" => payload.length}, [payload]]
        else
          [401, {"WWW-Authenticate" => 'Basic realm="test"'}, [""]]
        end
      elsif request[:uri][:path] == "/failearly"
        # Return an invalid HTTP response
        []
      elsif match = request[:uri][:path].match(/\/cookies\/(\d)\/(\d)/)
        cookie_value = (request[:headers]["Cookie"] || "x=0").split("=").last.to_i
        if match[1].to_i == match[2].to_i
          [200, {"Set-Cookie" => "x=#{cookie_value + 1}; Path=/"}, [""]]
        else
          [301, {"Set-Cookie" => "x=#{cookie_value + 1}; Path=/", "Location" => "/cookies/#{match[1].to_i + 1}/#{match[2]}"}, [""]]
        end
      elsif request[:uri][:path] == "/proxy"
        payload = JSON.dump(request.merge(server_port: port))
        [200, {"Content-Type" => content_type, "Content-Length" => payload.length}, [payload]]
      elsif request[:uri][:path] == "/json_utf8"
        payload = JSON.dump("first_name" => "Mark", "last_name" => "Töger")
        [200, {"Content-Type" => "application/json", "Content-Length" => payload.length}, [payload]]
      elsif request[:uri][:path] == "/authproxy"
        payload = JSON.dump(request.merge(server_port: port))
        if request[:headers]["Proxy-Authorization"] == "Basic dXNlcjpwYXNz"
          [200, {"Content-Type" => content_type, "Content-Length" => payload.length}, [payload]]
        else
          [407, {"Proxy-Authenticate" => 'Basic realm="localhost'}, [payload]]
        end
      elsif request[:uri][:path] == "/keepalive"
        payload = JSON.dump(request.merge(server_port: port))
        [200, {"Content-Type" => content_type, "Content-Length" => payload.length, "Keep-Alive" => "timeout=60"}, [payload]]
      elsif request[:uri][:path] == "/repeated_headers"
        payload = JSON.dump(request.merge(server_port: port))
        [200, {"Link" => ["foo", "bar"]}, [payload]]
      elsif request[:headers]["X-Redirect"] && request[:uri][:path] != request[:headers]["X-Redirect"]
        [301, {"Location" => local_server(request[:headers]["X-Redirect"])}, [""]]
      else
        if request[:headers]["Accept-Encoding"] && request[:headers]["Accept-Encoding"].match("gzip")
          out = StringIO.new("", "w")
          io = Zlib::GzipWriter.new(out, 2)

          request[:body] = Base64.encode64(request[:body]) if request[:headers]["X-Base64"]
          io.write JSON.dump(request)
          io.close
          payload = out.string
          [200, {"Content-Type" => content_type, "Content-Encoding" => "gzip", "Content-Length" => payload.length}, [payload]]
        else
          payload = JSON.dump(request)
          [200, {"Content-Type" => content_type, "Content-Length" => payload.length}, [payload]]
        end
      end
    end
  }
end

def stop_servers
  @servers.values.each(&:kill) if @servers
end

def start_ssl_server(port, options = {})
  cert_name = [
    %w[CN localhost],
  ]
  cert_file = options[:cert] || File.expand_path("../ssl/host.crt", __FILE__)
  cert = OpenSSL::X509::Certificate.new File.read(cert_file)
  cert.version = 0  # HACK: Work around jruby-openssl in jruby-head not setting cert.version
  pkey = OpenSSL::PKey::RSA.new File.read(File.expand_path("../ssl/host.key", __FILE__))
  @servers[port] = Thread.new {
    server = WEBrick::HTTPServer.new(
      {
        :Port => port,
        :SSLEnable => true,
        :SSLCertificate => cert,
        :SSLPrivateKey => pkey,
        :AccessLog => [],
        :Logger => WEBrick::Log.new("/dev/null"),
      }.merge(options)
    )
    server.mount_proc "/" do |req, res|
      res.body = "hello!"
    end

    server.start
  }
end

RSpec.configure do |c|
  require "net/http/server"

  c.before(:suite) {
    @server = {}
    start_server 55441
    start_server 55442
    start_ssl_server 55444
    start_ssl_server 55445, :SSLVerifyClient => OpenSSL::SSL::VERIFY_PEER | OpenSSL::SSL::VERIFY_FAIL_IF_NO_PEER_CERT, :SSLCACertificateFile => File.expand_path("../ssl/root-ca.crt", __FILE__)
    start_ssl_server 55446, cert: File.expand_path("../ssl/host-expired.crt", __FILE__)

    Manticore.disable_httpcomponents_logging!
  }

  c.after(:suite) { stop_servers }
end
