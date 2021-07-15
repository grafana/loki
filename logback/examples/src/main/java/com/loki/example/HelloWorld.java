package com.loki.example;

import java.net.URI;

import org.slf4j.Logger;
import org.slf4j.LoggerFactory;
import org.slf4j.Marker;
import org.slf4j.MarkerFactory;

import ch.qos.logback.classic.LoggerContext;

public class HelloWorld {

	public static void main(String[] args) {
		LoggerContext lc = (LoggerContext) LoggerFactory.getILoggerFactory();
		Logger logger = LoggerFactory.getLogger("com.grafana.loki.HelloWorld");
		logger.debug("Hello world.");

		Marker marker = MarkerFactory.getMarker("testmarker");
		marker.add(MarkerFactory.getMarker("child1"));
		marker.add(MarkerFactory.getMarker("child2"));
		marker.add(MarkerFactory.getMarker("child3"));
		try {
			URI uri = new URI("http:// test:3100");
			logger.info("URI is: " + uri);
		} catch (Exception e) {
			logger.error(marker, "URI exception: ", e);

		}

		int a = 0;
		int b = 2;
		try {
			int c = b / a;
		} catch (Exception e) {
			System.out.println(e);
			logger.error("division error: ", e);
		}

		lc.stop();
	}
}
