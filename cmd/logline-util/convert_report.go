package main

import (
	"fmt"
	"html/template"
	"os"
)

type reportData struct {
	SrcPath      string
	DstPath      string
	Terms        int
	Docs         uint32
	Threshold    string
	EncodingName string
	SrcMB        string
	DstMB        string
	DeltaPct     string
	DeltaMB      string
	Duration     string

	// Read benchmarks
	HasBench     bool
	InQueries    int
	InTotalIOs   int64
	InTotalIOMB  string
	InDuration   string
	OutQueries   int
	OutTotalIOs  int64
	OutTotalIOMB string
	OutDuration  string
	IOReduction  string

	// Verify (nil if not run)
	HasVerify  bool
	Matched    int
	Sentinel   int
	Mismatch   int
	OutMissing int
	OutExtra   int
	VerifyDur  string
}

func writeReport(path string, s *convertStats) error {
	srcMB := float64(s.SrcBytes) / 1024 / 1024
	dstMB := float64(s.DstBytes) / 1024 / 1024
	deltaPct := (dstMB - srcMB) / srcMB * 100

	d := reportData{
		SrcPath:      s.SrcPath,
		DstPath:      s.DstPath,
		Terms:        s.Terms,
		Docs:         s.Docs,
		Threshold:    fmt.Sprintf("%.2f", s.Threshold),
		EncodingName: s.EncodingName,
		SrcMB:        fmt.Sprintf("%.1f", srcMB),
		DstMB:        fmt.Sprintf("%.1f", dstMB),
		DeltaPct:     fmt.Sprintf("%+.1f%%", deltaPct),
		DeltaMB:      fmt.Sprintf("%+.1f", dstMB-srcMB),
		Duration:     s.Duration.String(),
	}

	if s.InputBench != nil && s.OutputBench != nil {
		d.HasBench = true
		d.InQueries = s.InputBench.Queries
		d.InTotalIOs = s.InputBench.TotalIOs
		d.InTotalIOMB = fmt.Sprintf("%.1f", float64(s.InputBench.TotalIOBytes)/1024/1024)
		d.InDuration = s.InputBench.Duration.String()
		d.OutQueries = s.OutputBench.Queries
		d.OutTotalIOs = s.OutputBench.TotalIOs
		d.OutTotalIOMB = fmt.Sprintf("%.1f", float64(s.OutputBench.TotalIOBytes)/1024/1024)
		d.OutDuration = s.OutputBench.Duration.String()
		if s.InputBench.TotalIOs > 0 {
			d.IOReduction = fmt.Sprintf("%+.0f%%",
				(float64(s.OutputBench.TotalIOs)-float64(s.InputBench.TotalIOs))/float64(s.InputBench.TotalIOs)*100)
		}
	}

	if s.Verify != nil {
		d.HasVerify = true
		d.Matched = s.Verify.Matched
		d.Sentinel = s.Verify.Sentinel
		d.Mismatch = s.Verify.Mismatch
		d.OutMissing = s.Verify.OutMissing
		d.OutExtra = s.Verify.OutExtra
		d.VerifyDur = s.Verify.Duration.String()
	}

	tmpl, err := template.New("report").Parse(htmlTemplate)
	if err != nil {
		return fmt.Errorf("parse template: %w", err)
	}

	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create report: %w", err)
	}
	defer f.Close()

	return tmpl.Execute(f, d)
}

const htmlTemplate = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<title>Logline Index Conversion Report</title>
<script src="https://cdn.jsdelivr.net/npm/chart.js@4"></script>
<style>
  :root { --bg: #0f172a; --card: #1e293b; --border: #334155; --text: #e2e8f0; --muted: #94a3b8; --green: #34d399; --red: #f87171; --blue: #60a5fa; --amber: #fbbf24; }
  * { margin: 0; padding: 0; box-sizing: border-box; }
  body { font-family: 'Inter', -apple-system, sans-serif; background: var(--bg); color: var(--text); padding: 2rem; max-width: 900px; margin: 0 auto; }
  h1 { font-size: 1.3rem; margin-bottom: 0.25rem; }
  .sub { color: var(--muted); font-size: 0.8rem; margin-bottom: 1.5rem; }
  .grid { display: grid; grid-template-columns: 1fr 1fr; gap: 1rem; margin-bottom: 1rem; }
  .card { background: var(--card); border: 1px solid var(--border); border-radius: 10px; padding: 1rem 1.2rem; }
  .card h2 { font-size: 0.85rem; margin-bottom: 0.6rem; font-weight: 600; color: var(--muted); text-transform: uppercase; letter-spacing: 0.04em; }
  .full { grid-column: 1 / -1; }
  .stat { display: flex; justify-content: space-between; padding: 0.3rem 0; border-bottom: 1px solid var(--border); font-size: 0.85rem; }
  .stat:last-child { border-bottom: none; }
  .stat .label { color: var(--muted); }
  .stat .value { font-weight: 600; font-variant-numeric: tabular-nums; }
  .big { font-size: 2rem; font-weight: 700; line-height: 1; }
  .big-label { color: var(--muted); font-size: 0.75rem; margin-top: 0.25rem; }
  .green { color: var(--green); }
  .red { color: var(--red); }
  .blue { color: var(--blue); }
  .amber { color: var(--amber); }
  canvas { max-height: 200px; }
  .badge { display: inline-block; padding: 0.15rem 0.5rem; border-radius: 4px; font-size: 0.75rem; font-weight: 600; }
  .badge-ok { background: #064e3b; color: var(--green); }
  .badge-warn { background: #78350f; color: var(--amber); }
  table { width: 100%; border-collapse: collapse; font-size: 0.85rem; margin-top: 0.5rem; }
  th, td { padding: 0.35rem 0.6rem; text-align: right; border-bottom: 1px solid var(--border); }
  th { color: var(--muted); font-weight: 500; font-size: 0.7rem; text-transform: uppercase; letter-spacing: 0.04em; }
  td:first-child, th:first-child { text-align: left; }
</style>
</head>
<body>

<h1>Logline Index — Conversion Report</h1>
<p class="sub">encoding: {{.EncodingName}} · density threshold: {{.Threshold}} · {{.Duration}}</p>

<div class="grid">

<!-- Size comparison -->
<div class="card" style="text-align:center">
  <div class="big green">{{.DeltaPct}}</div>
  <div class="big-label">Size Reduction</div>
  <div style="margin-top:0.5rem; font-size:0.8rem; color:var(--muted)">{{.DeltaMB}} MB</div>
</div>

<div class="card">
  <h2>Size</h2>
  <div class="stat"><span class="label">Input</span><span class="value">{{.SrcMB}} MB</span></div>
  <div class="stat"><span class="label">Output</span><span class="value">{{.DstMB}} MB</span></div>
  <canvas id="sizeChart" style="margin-top:0.5rem"></canvas>
</div>

<!-- Conversion details -->
<div class="card full">
  <h2>Conversion</h2>
  <div class="stat"><span class="label">Input</span><span class="value" style="font-size:0.75rem">{{.SrcPath}}</span></div>
  <div class="stat"><span class="label">Output</span><span class="value" style="font-size:0.75rem">{{.DstPath}}</span></div>
  <div class="stat"><span class="label">Output Encoding</span><span class="value">{{.EncodingName}}</span></div>
  <div class="stat"><span class="label">Density Threshold</span><span class="value">{{.Threshold}}</span></div>
  <div class="stat"><span class="label">Terms</span><span class="value">{{.Terms}}</span></div>
  <div class="stat"><span class="label">Documents</span><span class="value">{{.Docs}}</span></div>
  <div class="stat"><span class="label">Conversion Time</span><span class="value">{{.Duration}}</span></div>
</div>

{{if .HasBench}}
<!-- Read Benchmarks -->
<div class="card full">
  <h2>Read Benchmark ({{.InQueries}} single-term queries)</h2>
  <table>
    <tr><th></th><th>Total I/Os</th><th>Data Read</th><th>Duration</th></tr>
    <tr>
      <td>Input</td>
      <td>{{.InTotalIOs}}</td>
      <td>{{.InTotalIOMB}} MB</td>
      <td>{{.InDuration}}</td>
    </tr>
    <tr>
      <td>Output</td>
      <td>{{.OutTotalIOs}}</td>
      <td>{{.OutTotalIOMB}} MB</td>
      <td>{{.OutDuration}}</td>
    </tr>
  </table>
  {{if .IOReduction}}<p style="color:var(--muted); font-size:0.75rem; margin-top:0.5rem">I/O count change: {{.IOReduction}}</p>{{end}}
</div>
{{end}}

{{if .HasVerify}}
<!-- Verification -->
<div class="card full">
  <h2>Verification</h2>
  <div style="display:flex; gap:2rem; margin-bottom:0.5rem">
    <div style="text-align:center; flex:1">
      <div class="big blue">{{.Matched}}</div>
      <div class="big-label">Exact Match</div>
    </div>
    <div style="text-align:center; flex:1">
      <div class="big amber">{{.Sentinel}}</div>
      <div class="big-label">Sentinel (Dense)</div>
    </div>
    <div style="text-align:center; flex:1">
      {{if gt .Mismatch 0}}<div class="big red">{{.Mismatch}}</div>
      {{else}}<div class="big green">0</div>{{end}}
      <div class="big-label">Mismatched</div>
    </div>
  </div>
  <div class="stat"><span class="label">Missing in output</span><span class="value">{{.OutMissing}}</span></div>
  <div class="stat"><span class="label">Extra in output</span><span class="value">{{.OutExtra}}</span></div>
  <div class="stat"><span class="label">Verify Duration</span><span class="value">{{.VerifyDur}}</span></div>
  <div style="margin-top:0.5rem">
    {{if and (eq .Mismatch 0) (eq .OutMissing 0) (eq .OutExtra 0)}}
      <span class="badge badge-ok">PASS — all non-sentinel terms match exactly</span>
    {{else}}
      <span class="badge badge-warn">WARNING — unexpected differences found</span>
    {{end}}
  </div>
</div>
{{end}}

</div>

<script>
new Chart(document.getElementById('sizeChart'), {
  type: 'bar',
  data: {
    labels: ['Input', 'Output'],
    datasets: [{
      data: [{{.SrcMB}}, {{.DstMB}}],
      backgroundColor: ['#475569', '#34d399'],
      borderRadius: 4
    }]
  },
  options: {
    indexAxis: 'y',
    plugins: { legend: { display: false }, tooltip: { callbacks: { label: c => c.raw.toFixed(0) + ' MB' } } },
    scales: {
      x: { grid: { color: '#334155' }, ticks: { color: '#94a3b8', callback: v => v + ' MB' } },
      y: { grid: { display: false }, ticks: { color: '#e2e8f0', font: { size: 12 } } }
    }
  }
});
</script>
</body>
</html>`
