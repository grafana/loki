require File.expand_path(File.dirname(__FILE__)+'/spec_helper')

def getarchive(f)
	arc={}
	seq=''
	line=f.gets
	line.chomp!
	name=line[1..-1]
	while line=f.gets
		line.chomp!
		if line[0,1]=='>'
			arc[name]=seq
			seq=''
			name=line[1..-1]
		else
			seq+=line+"\n"
		end
	end
	arc[name]=seq
	arc
end

#spec_keys.txt: fasta-like editable text archive
arc=File.open(File.expand_path(File.dirname(__FILE__)+'/spec_keys.txt')){|f|getarchive(f)}

DSA_jruby_msg='OpenSSL::ASN1::Integer#to_der is broken in jruby'
EC_jruby_msg='OpenSSL::PKey::EC is unstable in jruby'

['DSA','RSA','EC'].each{|type|
describe "OpenSSL::PKey::"+type do
	klass=OpenSSL::PKey.const_get(type)
	it "reads PKCS1 key" do
		if type=='EC'&&OpenSSL::PKey::EC.instance_method(:initialize).arity==0
			pending 'OpenSSL::PKey::EC.new is unusable on this platform'
		end
		klass.new(arc[type+'_PKCS1']).is_a?(klass).should be true
	end
	it "converts PKCS1 to PKCS8" do
		pending 'PEM Line fold is different on Ruby 1.8' if RUBY_VERSION<'1.9'
		if defined?(RUBY_ENGINE)&&RUBY_ENGINE=='jruby'
			pending DSA_jruby_msg if type=='DSA'
			pending EC_jruby_msg if type=='EC'
		end

		pkcs8=klass.new(arc[type+'_PKCS1']).to_pem_pkcs8
		pkcs8.should eq arc[type+'_PKCS8']
		klass.new(pkcs8).is_a?(klass).should be true
	end
	if RUBY_VERSION>='1.9'&&ENV['OPENSSL']
		it "converts PKCS1 to correct PKCS8" do
			if defined?(RUBY_ENGINE)&&RUBY_ENGINE=='jruby'
				pending DSA_jruby_msg if type=='DSA'
				pending EC_jruby_msg if type=='EC'
			end

			pkcs8=klass.new(arc[type+'_PKCS1']).to_pem_pkcs8
			pkcs8_io=IO.popen(ENV['OPENSSL']+' pkcs8 -topk8 -nocrypt','r+b'){|io|
				io.puts arc[type+'_PKCS1']
				io.close_write
				io.read
			}
			pkcs8.should eq pkcs8_io
		end
	end
end
}
