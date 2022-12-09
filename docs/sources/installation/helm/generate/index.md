---
title: Generate Helm Chart Values
menuTitle: Generate Helm Chart Values
description: A sizing tool that generates Helm Chart values.
aliases:
  - /docs/installation/helm/generate
weight: 100
keywords: []
---

<link rel="stylesheet" href="../../../logql/analyzer/style.css">

# Generate Helm Chart Values
<!-- vale Grafana.Quotes = NO -->
<!-- vale Grafana.Quotes = YES -->

This tool helps to generate a Helm Charts `values.yaml` file based on specified
 expected ingestion, retention rate and node type.

<div id="app">

  <label>Node Type</label>
  <select name="node-type" v-model="node"> 
  <option v-for="node of nodes">{{ node }}</option>
  </select>

  <label>Ingest</label>
  <input v-model="ingest" name="ingest" placeholder="Desired ingest in TB/day" type="number" max="1000" min="0"/>

  <label>Log retention period</label>
  <input v-model="retention" name="retention" placeholder="Desired retention period in days" type="number" min="0"/>

  <label>Query performance</label>
  <div style="display: inline-flex;">
  <input type="radio" id="basic" value="Basic" v-model="queryperf"/>
  <label for="basic">Basic</label>

  <label for="super">
  <input style="display: block;"  type="radio" id="super" value="Super" v-model="queryperf"/>Super
  </label>
  </div>

  <button v-on:click="downloadHelmChartValues" class="primary-button">Generate and download values file</button>
</div>

<script src="https://unpkg.com/vue@3/dist/vue.global.prod.js"></script>

<script>
const API_URL = `http://localhost:3001/api/sizing`
const { createApp } = Vue

createApp({
  data() {
    return {
      nodes: [],
      node: null,
      ingest: null,
      retention: null,
      queryperf: 'Basic'
    }
  },

  created() {
    // fetch on init
    this.fetchNodeTypes()
  },

  methods: {
    async fetchNodeTypes() {
      const url = `${API_URL}/nodes`
      this.nodes = await (await fetch(url)).json()
    },
    downloadHelmChartValues() {
      const url =
      `${API_URL}/helm?node-type=${encodeURIComponent(this.node)}&ingest=${encodeURIComponent(this.ingest)}&retention=${encodeURIComponent(this.retention)}&queryperf=${encodeURIComponent(this.queryperf)}`
      window.open(url)
    }
  }
}).mount('#app')
</script>
