package com.grafana.loki.http;

import com.grafana.loki.LokiConfig;

import ch.qos.logback.core.Context;
import ch.qos.logback.core.spi.ContextAwareBase;

public class RetryHandler extends ContextAwareBase {
	private int numRetries;

	private long delay;

	private long maxDelay;

	private int maxRetries;

	public RetryHandler(Context ctx, LokiConfig config) {
		this.maxRetries = config.getMaxRetries();
		this.delay = config.getMinDelaySeconds() * 1000;
		this.maxDelay = config.getMaxDelaySeconds() * 1000;
		this.numRetries = 1;
		this.setContext(ctx);
	}

	public boolean shouldRetry() {
		return (numRetries <= maxRetries);
	}

	public void waitUntilNextTry() {
		try {
			Thread.sleep(delay);
			setNextDelay();
		} catch (InterruptedException e) {
			addError("Error in RetryHandler. Error while executing sleep", e);
		}
	}

	public void retryRequest() {
		numRetries++;
		waitUntilNextTry();
	}

	public int getRetries() {
		return this.numRetries;
	}

	public void setNextDelay() {
		if (delay * 2 <= maxDelay) {
			delay = delay * 2;		
		} else {
			delay = maxDelay;
		}
	}
	
	public long getDelay() {
		return delay;
	}
}
