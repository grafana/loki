#--
# Copyright (c) 2007-2013 Nick Sieger.
# See the file README.txt included with the distribution for
# software license details.
#++

require 'net/http/post/multipart'

RSpec.shared_context "net http multipart" do
  let(:temp_file) {"temp.txt"}
  let(:http_post) do
    Struct.new("HTTPPost", :content_length, :body_stream, :content_type) do
      def set_content_type(type, params = {})
        self.content_type = type + params.map{|k,v|"; #{k}=#{v}"}.join('')
      end
    end
  end
  
  after(:each) do
    File.delete(temp_file) rescue nil
  end
  
  def assert_results(post)
    expect(post.content_length).to be > 0
    expect(post.body_stream).to_not be_nil
    
    expect(post['content-type']).to be == "multipart/form-data; boundary=#{post.boundary}"
    
    body = post.body_stream.read
    boundary_regex = Regexp.quote(post.boundary)
    
    expect(body).to be =~ /1234567890/
    
    # ensure there is at least one boundary
    expect(body).to be =~ /^--#{boundary_regex}\r\n/
    
    # ensure there is an epilogue
    expect(body).to be =~ /^--#{boundary_regex}--\r\n/
    expect(body).to be =~ /text\/plain/
    
    if (body =~ /multivalueParam/)
      expect(body.scan(/^.*multivalueParam.*$/).size).to be == 2
    end
  end

  def assert_additional_headers_added(post, parts_headers)
    post.body_stream.rewind
    body = post.body_stream.read
    parts_headers.each do |part, headers|
      headers.each do |k,v|
        expect(body).to be =~ /#{k}: #{v}/
      end
    end
  end
end

RSpec.describe Net::HTTP::Post::Multipart do
  include_context "net http multipart"
  
  it "test_form_multipart_body" do
    File.open(TEMP_FILE, "w") {|f| f << "1234567890"}
    @io = File.open(TEMP_FILE)
    @io = UploadIO.new @io, "text/plain", TEMP_FILE
    assert_results Net::HTTP::Post::Multipart.new("/foo/bar", :foo => 'bar', :file => @io)
  end

  it "test_form_multipart_body_with_stringio" do
    @io = StringIO.new("1234567890")
    @io = UploadIO.new @io, "text/plain", TEMP_FILE
    assert_results Net::HTTP::Post::Multipart.new("/foo/bar", :foo => 'bar', :file => @io)
  end

  it "test_form_multiparty_body_with_parts_headers" do
    @io = StringIO.new("1234567890")
    @io = UploadIO.new @io, "text/plain", TEMP_FILE
    parts = { :text => 'bar', :file => @io }
    headers = {
      :parts => {
        :text => { "Content-Type" => "part/type" },
        :file => { "Content-Transfer-Encoding" => "part-encoding" }
      }
    }

    request = Net::HTTP::Post::Multipart.new("/foo/bar", parts, headers)
    assert_results request
    assert_additional_headers_added(request, headers[:parts])
  end

  it "test_form_multipart_body_with_array_value" do
    File.open(TEMP_FILE, "w") {|f| f << "1234567890"}
    @io = File.open(TEMP_FILE)
    @io = UploadIO.new @io, "text/plain", TEMP_FILE
    params = {:foo => ['bar', 'quux'], :file => @io}
    headers = { :parts => {
        :foo => { "Content-Type" => "application/json; charset=UTF-8" } } }
    post = Net::HTTP::Post::Multipart.new("/foo/bar", params, headers)
    
    expect(post.content_length).to be > 0
    expect(post.body_stream).to_not be_nil

    body = post.body_stream.read
    expect(body.lines.grep(/name="foo"/).length).to be == 2
    expect(body).to be =~ /Content-Type: application\/json; charset=UTF-8/
  end

  it "test_form_multipart_body_with_arrayparam" do
    File.open(TEMP_FILE, "w") {|f| f << "1234567890"}
    @io = File.open(TEMP_FILE)
    @io = UploadIO.new @io, "text/plain", TEMP_FILE
    assert_results Net::HTTP::Post::Multipart.new("/foo/bar", :multivalueParam => ['bar','bah'], :file => @io)
  end
end

RSpec.describe Net::HTTP::Put::Multipart do
  include_context "net http multipart"
  
  it "test_form_multipart_body_put" do
    File.open(TEMP_FILE, "w") {|f| f << "1234567890"}
    @io = File.open(TEMP_FILE)
    @io = UploadIO.new @io, "text/plain", TEMP_FILE
    assert_results Net::HTTP::Put::Multipart.new("/foo/bar", :foo => 'bar', :file => @io)
  end
end
