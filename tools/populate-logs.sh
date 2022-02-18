#!/bin/bash

TS=$(date +%s%N)
body=$(jq -rn --arg ts "$TS" '{"streams": [{ "stream": { "label1": "foo", "label2":"bar" }, "values": [ [ $ts, "abc" ] ] }]}')
curl -v -H "Content-Type: application/json" -XPOST -s "http://localhost:3100/loki/api/v1/push" --data-raw "$body"

TS=$(date +%s%N)
body=$(jq -rn --arg ts "$TS" '{"streams": [{ "stream": { "label1": "foo", "label2":"bar" }, "values": [ [ $ts, "def" ] ] }]}')
curl -v -H "Content-Type: application/json" -XPOST -s "http://localhost:3100/loki/api/v1/push" --data-raw "$body"

TS=$(date +%s%N)
body=$(jq -rn --arg ts "$TS" '{"streams": [{ "stream": { "label1": "foo", "label2":"baz" }, "values": [ [ $ts, "abc" ] ] }]}')
curl -v -H "Content-Type: application/json" -XPOST -s "http://localhost:3100/loki/api/v1/push" --data-raw "$body"

TS=$(date +%s%N)
body=$(jq -rn --arg ts "$TS" '{"streams": [{ "stream": { "label1": "foo", "label2":"baz" }, "values": [ [ $ts, "def" ] ] }]}')
curl -v -H "Content-Type: application/json" -XPOST -s "http://localhost:3100/loki/api/v1/push" --data-raw "$body"
