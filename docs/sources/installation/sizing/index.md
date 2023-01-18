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
 expected ingestion, retention rate and node type. It will always configure a
 [scalable](../../fundamentals/architecture/deployment-modes/#simple-scalable-deployment-mode) deployment. The storage needs to be configured after generation.

<div id="app">

  <label class="icon question" v-on:mouseover="help='node'" v-on:mouseleave="help=null">Node Type</label>
  <select name="node-type" v-model="node"> 
  <option v-for="node of nodes">{{ node }}</option>
  </select>

  <label class="fa fa-question" v-on:mouseover="help='ingest'" v-on:mouseleave="help=null">Ingest</label>
  <input v-model="ingest" name="ingest" placeholder="Desired ingest in GiB/day" type="number" max="1048576" min="0"/>

  <label class="fa fa-question" v-on:mouseover="help='retention'" v-on:mouseleave="help=null">Log retention period</label>
  <input v-model="retention" name="retention" placeholder="Desired retention period in days" type="number" min="0"/>

  <label class="fa fa-question" v-on:mouseover="help='queryperf'" v-on:mouseleave="help=null">Query performance</label>
  <div id="queryperf" style="display: inline-flex;">
  <label for="basic">
  <input type="radio" id="basic" value="Basic" v-model="queryperf"/>Basic
  </label>

  <label for="super">
  <input type="radio" id="super" value="Super" v-model="queryperf"/>Super
  </label>
  </div>

  <a v-bind:href="helmURL" class="primary-button">Generate and download values file</a>

  <blockquote v-if="help">
    <span v-if="help === 'ingest'">
    Defines the log volume in terrabytes expected to be ingested each day.
    </span>
    <span v-else-if="help === 'node'">
    Defines the node type of the Kubernetes cluster.
    </span>
    <span v-else-if="help === 'retention'">
    Defines how long the ingested logs should be kept.
    </span>
    <span v-else-if="help === 'queryperf'">
    Defines the expected query performance. Basic enables 3mbps. Super should be chosen if more query throughput is required.
    </span>
  </blockquote>
</div>

<script src="https://unpkg.com/vue@3/dist/vue.global.prod.js"></script>
.<style>

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
const API_URL = `https://logql-analyzer.grafana.net/next/api/sizing`
const { createApp } = Vue

createApp({
  data() {
    return {
      nodes: ["Loading..."],
      node: "Loading...",
      ingest: null,
      retention: null,
      queryperf: 'Basic',
      help: null
    }
  },

  computed: {
    helmURL() {
      const bytesDayIngest = this.ingest * 1024 * 1024 * 1024
      return `${API_URL}/helm?node-type=${encodeURIComponent(this.node)}&ingest=${encodeURIComponent(bytesDayIngest)}&retention=${encodeURIComponent(this.retention)}&queryperf=${encodeURIComponent(this.queryperf)}`
    }
  },

  created() {
    // fetch on init
    this.fetchNodeTypes()
  },

  methods: {
    async fetchNodeTypes() {
      const url = `${API_URL}/nodes`
      this.nodes = await (await fetch(url,{mode: 'cors'})).json()
    }
  }
}).mount('#app')
</script>
