package com.grafana.loki;

import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertFalse;

import org.junit.Before;
import org.junit.Test;

import ch.qos.logback.core.Context;
import ch.qos.logback.core.ContextBase;

public class ConfigTest {

	Context context = new ContextBase();
	private LokiAppender<Object> appender = new LokiAppender<Object>();
	private LokiConfig config = new LokiConfig();

	@Before
	public void setUp() throws Exception {
		appender.setContext(context);
		appender.setLokiConfig(config);
	}

	@Test
	public void testDefaultConfig() throws Exception {
		appender.start();
		assertEquals(config.getBatchSizeBytes(), 102400);
		assertEquals(config.getBatchWaitSeconds(), 1);
		assertEquals(config.getMinDelaySeconds(), 1);
		assertEquals(config.getMaxDelaySeconds(), 300);
		assertEquals(config.getMaxRetries(), 10);
		assertEquals(config.getTenantID(), null);
		appender.stop();
	}

	@Test
	public void testURLInvalid() {
		config.setUrl("htt://localhost:3100/loki/api/v1/push");
		appender.start();
		assertFalse(appender.isStarted());
		appender.stop();
	}

}
