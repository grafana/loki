package com.grafana.loki;

import java.time.Instant;
import java.util.concurrent.BlockingQueue;

import ch.qos.logback.core.Context;
import ch.qos.logback.core.spi.ContextAwareBase;

public class Ticker extends ContextAwareBase implements Runnable {
	private BlockingQueue<Long> ticker;
	
	private long duration;
	
	private volatile boolean running = true;
	
	public Ticker(BlockingQueue<Long> ticker, LokiConfig config, Context ctx) {
		this.ticker = ticker;
		this.duration = config.getBatchWaitSeconds() * 1000;
		this.setContext(ctx);
	}
	
	@Override
	public void run() {
		try {
			 while (running) {
				 Thread.sleep(duration);
				 Instant instant = Instant.now().plusMillis(duration);
				 ticker.add(instant.getEpochSecond());
			 }
		} catch (InterruptedException e) {
			addError("Exception in Ticker thread sleep", e);
		}
	}
	
	public void terminate() {
		running = false;
	}

}
