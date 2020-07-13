package org.manticore;

import java.io.IOException;
import java.io.InputStream;
import org.jruby.Ruby;
import org.jruby.RubyClass;
import org.jruby.RubyString;
import org.jruby.RubyObject;
import org.jruby.RubyModule;
import org.jruby.util.ByteList;
import org.jruby.anno.JRubyMethod;
import org.jruby.runtime.Block;
import org.jruby.runtime.ThreadContext;
import org.jruby.runtime.load.Library;
import org.jruby.runtime.ObjectAllocator;
import org.jruby.runtime.builtin.IRubyObject;
import org.apache.http.HttpEntity;
import org.apache.http.util.EntityUtils;
import org.apache.http.protocol.HTTP;
import org.jcodings.Encoding;

public class Manticore implements Library {

  public void load(final Ruby ruby, boolean _dummy) {
    RubyModule manticore = ruby.defineModule("Manticore");
    RubyClass converter = ruby.defineClassUnder("EntityConverter", ruby.getObject(), new ObjectAllocator() {
        public IRubyObject allocate(Ruby ruby, RubyClass rc) {
            return new EntityConverter(ruby, rc);
        }
    }, manticore);
    converter.defineAnnotatedMethods(EntityConverter.class);
  }

  public class EntityConverter extends RubyObject {
    public EntityConverter(Ruby ruby, RubyClass rubyClass) {
      super(ruby, rubyClass);
    }

    @JRubyMethod(name = "read_entity")
    public IRubyObject readEntity(ThreadContext context, IRubyObject rEntity, Block block) throws IOException {
      HttpEntity entity = (HttpEntity)rEntity.toJava(HttpEntity.class);

      String charset = EntityUtils.getContentCharSet(entity);
      if (charset == null) {
        String mimeType = EntityUtils.getContentMimeType(entity);
        if ( mimeType != null && mimeType.startsWith("application/json") ) {
          charset = "UTF-8";
        } else {
          charset = HTTP.DEFAULT_CONTENT_CHARSET;
        }
      }

      Encoding encoding;
      try {
        encoding = context.getRuntime().getEncodingService().getEncodingFromString(charset);
      } catch(Throwable e) {
        encoding = context.getRuntime().getEncodingService().getEncodingFromString(HTTP.DEFAULT_CONTENT_CHARSET);
      }

      if(block.isGiven()) {
        return streamEntity(context, entity, encoding, block);
      } else {
        return readWholeEntity(context, entity, encoding);
      }
    }

    private IRubyObject readWholeEntity(ThreadContext context, HttpEntity entity, Encoding encoding) throws IOException {
      ByteList bl = new ByteList(EntityUtils.toByteArray(entity), false);
      return RubyString.newString(context.getRuntime(), bl, encoding);
    }

    private IRubyObject streamEntity(ThreadContext context, HttpEntity entity, Encoding encoding, Block block) throws IOException {
      InputStream instream = entity.getContent();
      if (instream == null) { return null; }
      String charset = EntityUtils.getContentCharSet(entity);
      if(charset == null) { charset = HTTP.DEFAULT_CONTENT_CHARSET; }

      int i = (int)entity.getContentLength();
      if (i < 0) { i = 4096; }

      if (charset == null) {
        charset = HTTP.DEFAULT_CONTENT_CHARSET;
      }

      try {
        byte[] tmp = new byte[4096];
        int l;
        while((l = instream.read(tmp)) != -1) {
          block.call( context, RubyString.newStringShared(context.getRuntime(), new ByteList(tmp, 0, l, false), encoding) );
        }
      } finally {
        instream.close();
      }
      return context.nil;
    }
  }
}
