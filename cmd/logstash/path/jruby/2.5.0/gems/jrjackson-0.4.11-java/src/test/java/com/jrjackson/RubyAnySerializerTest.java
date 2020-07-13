package com.jrjackson;

import com.fasterxml.jackson.core.JsonEncoding;
import com.fasterxml.jackson.core.JsonGenerator;
import com.fasterxml.jackson.databind.SerializerProvider;
import org.jcodings.specific.UTF8Encoding;
import org.jruby.CompatVersion;
import org.jruby.Ruby;
import org.jruby.RubyHash;
import org.jruby.RubyInstanceConfig;
import org.jruby.ext.bigdecimal.RubyBigDecimal;
import org.jruby.util.ByteList;
import org.junit.Before;
import org.junit.Test;

import java.io.ByteArrayOutputStream;

import static org.hamcrest.CoreMatchers.equalTo;
import static org.hamcrest.CoreMatchers.is;
import static org.hamcrest.MatcherAssert.assertThat;

public class RubyAnySerializerTest {
    private static boolean setupDone = false;
    public static Ruby ruby;

    @Before
    public void setUp() throws Exception {
        if (setupDone) return;

        RubyInstanceConfig config_19 = new RubyInstanceConfig();
        config_19.setCompatVersion(CompatVersion.RUBY1_9);
        ruby = Ruby.newInstance(config_19);
        RubyBigDecimal.createBigDecimal(ruby); // we need to do 'require "bigdecimal"'
//        JrubyTimestampExtLibrary.createTimestamp(ruby);
        setupDone = true;
    }


    @Test
    public void testSerialize() throws Exception {
        RubyHash rh = RubyHash.newHash(ruby);
        rh.put("somekey", 123);

        SerializerProvider provider = RubyJacksonModule.createProvider();

        ByteArrayOutputStream baos = new ByteArrayOutputStream();
        JsonGenerator jgen = RubyJacksonModule.factory.createGenerator(
                baos, JsonEncoding.UTF8);

        RubyAnySerializer.instance.serialize(rh, jgen, provider);
        jgen.close();
        ByteList bl = new ByteList(baos.toByteArray(),
                UTF8Encoding.INSTANCE);
        assertThat(bl.toString(), is(equalTo("{\"somekey\":123}")));
    }
}