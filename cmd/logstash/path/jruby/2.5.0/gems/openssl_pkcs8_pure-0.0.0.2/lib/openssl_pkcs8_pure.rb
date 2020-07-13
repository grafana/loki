# openssl_pkcs8_pure: Output OpenSSL private key in PKCS8 format written with pure Ruby
#
# Copyright (c) 2017, T. Yamada under Ruby License (2-clause BSDL or Artistic).
#
# Check LICENSE terms.
#
# Note: MIT License is also applicable if that compresses LICENSE file.

require 'openssl'
require 'base64'

# See http://ruby-doc.org/stdlib/libdoc/openssl/rdoc/OpenSSL/PKey/DSA.html
class OpenSSL::PKey::DSA
	# Returns DSA (private) key in PKCS#8 DER format.
	def to_der_pkcs8
		#if public, just use x509 default output
		return to_der if !private?
		asn1=OpenSSL::ASN1.decode(to_der).value
		OpenSSL::ASN1::Sequence([
			OpenSSL::ASN1::Integer(0),
			OpenSSL::ASN1::Sequence([
				OpenSSL::ASN1::ObjectId("DSA"),
				OpenSSL::ASN1::Sequence([asn1[1],asn1[2],asn1[3]])
			]),
			OpenSSL::ASN1::OctetString(asn1[5].to_der)
		]).to_der
	end
	# Returns DSA (private) key in PKCS#8 PEM format.
	def to_pem_pkcs8
		return to_pem if !private?
		body=RUBY_VERSION<'1.9' ? Base64.encode64(to_der_pkcs8) : Base64.strict_encode64(to_der_pkcs8).chars.each_slice(64).map(&:join).join("\n")+"\n"
		"-----BEGIN PRIVATE KEY-----\n"+body+"-----END PRIVATE KEY-----\n"
	end
end

# See http://ruby-doc.org/stdlib/libdoc/openssl/rdoc/OpenSSL/PKey/RSA.html
class OpenSSL::PKey::RSA
	# Returns RSA (private) key in PKCS#8 DER format.
	def to_der_pkcs8
		#if public, just use x509 default output
		return to_der if !private?
		OpenSSL::ASN1::Sequence([
			OpenSSL::ASN1::Integer(0),
			OpenSSL::ASN1::Sequence([OpenSSL::ASN1::ObjectId("rsaEncryption"),OpenSSL::ASN1::Null.new(nil)]),
			OpenSSL::ASN1::OctetString(to_der)
		]).to_der
	end
	# Returns RSA (private) key in PKCS#8 PEM format.
	def to_pem_pkcs8
		return to_pem if !private?
		body=RUBY_VERSION<'1.9' ? Base64.encode64(to_der_pkcs8) : Base64.strict_encode64(to_der_pkcs8).chars.each_slice(64).map(&:join).join("\n")+"\n"
		"-----BEGIN PRIVATE KEY-----\n"+body+"-----END PRIVATE KEY-----\n"
	end
end

# See http://ruby-doc.org/stdlib/libdoc/openssl/rdoc/OpenSSL/PKey/EC.html
class OpenSSL::PKey::EC
	# Returns EC (private) key in PKCS#8 DER format.
	def to_der_pkcs8
		#[todo] OpenSSL::PKey::EC#public_key does not respond to to_pem
		#return to_der if !private?
		asn1=OpenSSL::ASN1.decode(to_der).value
		#curve_name=asn1[2].value[0].value
		curve_name=group.curve_name
		asn1.delete_at(2)
		asn1=OpenSSL::ASN1::Sequence(asn1)
		OpenSSL::ASN1::Sequence([
			OpenSSL::ASN1::Integer(0),
			OpenSSL::ASN1::Sequence([OpenSSL::ASN1::ObjectId("id-ecPublicKey"),OpenSSL::ASN1::ObjectId(curve_name)]),
			OpenSSL::ASN1::OctetString(asn1.to_der)
		]).to_der
	end
	# Returns EC (private) key in PKCS#8 PEM format.
	def to_pem_pkcs8
		#return to_pem if !private?
		body=RUBY_VERSION<'1.9' ? Base64.encode64(to_der_pkcs8) : Base64.strict_encode64(to_der_pkcs8).chars.each_slice(64).map(&:join).join("\n")+"\n"
		"-----BEGIN PRIVATE KEY-----\n"+body+"-----END PRIVATE KEY-----\n"
	end
end
