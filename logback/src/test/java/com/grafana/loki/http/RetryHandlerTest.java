package com.grafana.loki.http;

import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertFalse;
import static org.junit.Assert.assertTrue;

import org.junit.After;
import org.junit.Before;
import org.junit.Test;

import com.grafana.loki.Constants;
import com.grafana.loki.LokiConfig;

import ch.qos.logback.core.Context;
import ch.qos.logback.core.ContextBase;

public class RetryHandlerTest {
	private Context context = new ContextBase();

	private LokiConfig config;

	@Before
	public void setup() {
		config = new LokiConfig();
	}

	@After
	public void tearDown() {
		config = null;
	}

	@Test
	public void testRetryHandlerWithDefaults() throws Exception {
		config.setMinDelaySeconds(Constants.MINDELAYSECONDS);
		config.setMaxDelaySeconds(Constants.MAXDELAYSECONDS);
		config.setMaxRetries(Constants.MAXRETRIES);

		RetryHandler retryHandler = new RetryHandler(context, config);
		assertTrue(retryHandler.shouldRetry());
		retryHandler.retryRequest();
		assertTrue(retryHandler.shouldRetry());
		assertEquals(Constants.MINDELAYSECONDS * 2 * 1000, retryHandler.getDelay());
	}

	@Test
	public void testRetryHandlerWithCustomValues() throws Exception {
		config.setMinDelaySeconds(1);
		config.setMaxDelaySeconds(2);
		config.setMaxRetries(2);

		RetryHandler retryHandler = new RetryHandler(context, config);

		assertTrue(retryHandler.shouldRetry());

		retryHandler.retryRequest();
		assertTrue(retryHandler.shouldRetry());
		assertEquals(Constants.MINDELAYSECONDS * 2000, retryHandler.getDelay());

		retryHandler.retryRequest();
		assertFalse(retryHandler.shouldRetry());
		assertEquals(Constants.MINDELAYSECONDS * 2000, retryHandler.getDelay());
	}
}
