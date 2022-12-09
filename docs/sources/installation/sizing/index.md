---
title: Size the cluster 
menuTitle: Size the cluster 
description: A sizing tool that generates Helm Chart values.
aliases:
  - /docs/installation/helm/generate
weight: 100
keywords: []
---

<link rel="stylesheet" href="../../logql/analyzer/style.css">

# Size the cluster
<!-- vale Grafana.Quotes = NO -->
<!-- vale Grafana.Quotes = YES -->

This tool helps to generate a Helm Charts `values.yaml` file based on specified
 expected ingestion, retention rate and node type.

<div id="app">

  <label class="icon question">Node Type</label>
  <select name="node-type" v-model="node"> 
  <option v-for="node of nodes">{{ node }}</option>
  </select>

  <label class="icon question">Ingest</label>
  <input v-model="ingest" name="ingest" placeholder="Desired ingest in TB/day" type="number" max="1000" min="0"/>

  <label class="icon question">Log retention period</label>
  <input v-model="retention" name="retention" placeholder="Desired retention period in days" type="number" min="0"/>

  <label class="icon question">Query performance</label>
  <div id="queryperf" style="display: inline-flex;">
  <label for="basic">
  <input type="radio" id="basic" value="Basic" v-model="queryperf"/>Basic
  </label>

  <label for="super">
  <input type="radio" id="super" value="Super" v-model="queryperf"/>Super
  </label>
  </div>

  <a v-bind:href="helmURL" class="primary-button">Generate and download values file</a>
</div>

<script src="https://unpkg.com/vue@3/dist/vue.global.prod.js"></script>

<style>

#app label.icon.question::after {
  content: '\f29c';
  display: inline-block;
  font: normal normal normal 14px/1 FontAwesome;
  padding-left: 8px;
}

#app #queryperf label {
  padding: 1em;
  text-align: center;
}

#app #queryperf label input {
  display: block;
}

#app a {
  padding: .5em;

}
</style>

<script>
const API_URL = `http://logql-analyzer.grafana.net/next/api/sizing`
const { createApp } = Vue

createApp({
  data() {
    return {
      nodes: ["Loading..."],
      node: "Loading...",
      ingest: null,
      retention: null,
      queryperf: 'Basic'
    }
  },

  computed: {
    helmURL() {
      return `${API_URL}/helm?node-type=${encodeURIComponent(this.node)}&ingest=${encodeURIComponent(this.ingest)}&retention=${encodeURIComponent(this.retention)}&queryperf=${encodeURIComponent(this.queryperf)}`
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
    }
  }
}).mount('#app')
</script>
