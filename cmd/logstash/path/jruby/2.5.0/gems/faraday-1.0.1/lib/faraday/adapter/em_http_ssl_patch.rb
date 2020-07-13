# frozen_string_literal: true

require 'openssl'
require 'em-http'

# EventMachine patch to make SSL work.
module EmHttpSslPatch
  def ssl_verify_peer(cert_string)
    begin
      @last_seen_cert = OpenSSL::X509::Certificate.new(cert_string)
    rescue OpenSSL::X509::CertificateError
      return false
    end

    unless certificate_store.verify(@last_seen_cert)
      raise OpenSSL::SSL::SSLError,
            %(unable to verify the server certificate for "#{host}")
    end

    begin
      certificate_store.add_cert(@last_seen_cert)
    rescue OpenSSL::X509::StoreError => e
      raise e unless e.message == 'cert already in hash table'
    end
    true
  end

  def ssl_handshake_completed
    return true unless verify_peer?

    unless verified_cert_identity?
      raise OpenSSL::SSL::SSLError,
            %(host "#{host}" does not match the server certificate)
    end

    true
  end

  def verify_peer?
    parent.connopts.tls[:verify_peer]
  end

  def verified_cert_identity?
    OpenSSL::SSL.verify_certificate_identity(@last_seen_cert, host)
  end

  def host
    parent.uri.host
  end

  def certificate_store
    @certificate_store ||= begin
      store = OpenSSL::X509::Store.new
      store.set_default_paths
      ca_file = parent.connopts.tls[:cert_chain_file]
      store.add_file(ca_file) if ca_file
      store
    end
  end
end

EventMachine::HttpStubConnection.include(EmHttpSslPatch)
