---
_build:
  list: false
noindex: true
title: Size the cluster 
menuTitle: Size the cluster 
description: Provides a tool that generates a Helm Chart values.yaml file based on expected ingestion, retention rate, and node type, to help size your Grafana deployment.
aliases:
  - ../installation/sizing/
  - ../installation/helm/generate
weight: 100
---

<link rel="stylesheet" href="../../query/analyzer/style.css">

# Size the cluster
<!-- vale Grafana.Quotes = NO -->
<!-- vale Grafana.Quotes = YES -->

This tool helps to generate a Helm Charts `values.yaml` file based on specified
 expected ingestion, retention rate and node type. It will always configure a
 [scalable]({{< relref "../../get-started/deployment-modes#simple-scalable" >}}) deployment. The storage needs to be configured after generation.

<div id="app">
  <label>Node Type<i class="fa fa-question" v-on:mouseover="help='node'" v-on:mouseleave="help=null"></i></label>
  <select name="node-type" v-model="node"> 
  <option v-for="node of nodes">{{ node }}</option>
  </select>
  <label>Ingest<i class="fa fa-question" v-on:mouseover="help='ingest'" v-on:mouseleave="help=null"></i></label>
  <div style="display: flex;">
    <input style="padding-right:4.5em;" v-model="ingestInGB" name="ingest" placeholder="Desired ingest in GB/day" type="number" max="1048576" min="0"/>
    <span style="margin: auto auto auto -4em;">GB/day</span>
  </div>
  <label>Log retention period<i class="fa fa-question" v-on:mouseover="help='retention'" v-on:mouseleave="help=null"></i></label>
  <div style="display: flex;">
    <input style="padding-right:4.5em;"  v-model="retention" name="retention" placeholder="Desired retention period in days" type="number" min="0"/>
    <span style="margin: auto auto auto -4em;">days</span>
  </div>
  <label>Query performance<i class="fa fa-question" v-on:mouseover="help='queryperf'" v-on:mouseleave="help=null"></i></label>
  <div id="queryperf" style="display: inline-flex;">
  <label for="basic">
  <input type="radio" id="basic" value="Basic" v-model="queryperf"/>Basic
  </label>
  <label for="super">
  <input type="radio" id="super" value="Super" v-model="queryperf"/>Super
  </label>
  </div>

  <div v-if="clusterSize">
    <table>
    <tr>
      <th>Read Replicas</th>
      <th>Write Replicas</th>
      <th>Nodes</th>
      <th>Cores</th>
      <th>Memory</th>
    </tr>
    <tr>
      <td>{{ clusterSize.TotalReadReplicas }}</td>
      <td>{{ clusterSize.TotalWriteReplicas }}</td>
      <td>{{ clusterSize.TotalNodes}}</td>
      <td>{{ clusterSize.TotalCoresRequest}}</td>
      <td>{{ clusterSize.TotalMemoryRequest}} GB</td>
    </tr>
    </table>
  </div>

  <a v-bind:href="helmURL" class="primary-button">Generate and download values file</a>

  <blockquote v-if="help">
    <span v-if="help === 'ingest'">
    Defines the log volume in gigabytes, ie 1e+9 bytes, expected to be ingested each day.
    </span>
    <span v-else-if="help === 'node'">
    Defines the node type of the Kubernetes cluster. Is a vendor or type
    missing? If so, add it to <code>pkg/sizing/node.go</code>.
    </span>
    <span v-else-if="help === 'retention'">
    Defines how long the ingested logs should be kept.
    </span>
    <span v-else-if="help === 'queryperf'">
    Defines the expected query performance. Basic is sized for a max query throughput of around 3GB/s. Super aims for 25% more throughput.
    </span>
  </blockquote>
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
const API_URL = `https://logql-analyzer.grafana.net/next/api/sizing`
const { createApp } = Vue

createApp({
  data() {
    return {
      nodes: ["Loading..."],
      node: "Loading...",
      bytesDayIngest: null,
      retention: null,
      queryperf: 'Basic',
      help: null,
      clusterSize: null
    }
  },

  computed: {
    helmURL() {
      return `${API_URL}/helm?${this.queryString}`
    },
    queryString() {
      return `node-type=${encodeURIComponent(this.node)}&ingest=${encodeURIComponent(this.bytesDayIngest)}&retention=${encodeURIComponent(this.retention)}&queryperf=${encodeURIComponent(this.queryperf)}`
    },
    ingestInGB: {
	get () {
                if (this.bytesDayIngest == null) {
                    return null
                }
                // Convert to GB
                return this.bytesDayIngest / 1000 / 1000 / 1000
	},
	set (gbDayIngest) {
		console.log(gbDayIngest)
		this.bytesDayIngest = gbDayIngest * 1000 * 1000 * 1000
		console.log(this.bytesDayIngest)
	}
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
    },
    async calculateClusterSize() {
      if (this.node == 'Loading...' || this.bytesDayIngest== null || this.retention == null) {
        return
      }
      const url = `${API_URL}/cluster?${this.queryString}`
      this.clusterSize = await (await fetch(url,{mode: 'cors'})).json()
    }
  },

  watch: {
    node:           'calculateClusterSize',
    bytesDayIngest: 'calculateClusterSize',
    retention:      'calculateClusterSize',
    queryperf:      'calculateClusterSize'
  }
}).mount('#app')
</script>
