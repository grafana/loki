package main

import (
	"encoding/json"
	"fmt"
	"html/template"
	"os"
	"sort"

	"github.com/grafana/loki/v3/pkg/logline"
	"github.com/grafana/loki/v3/pkg/logline/format"
)

func writeDumpReport(path string, reader logline.Reader, version, filePath string, includeGramFreq bool, gramSamples int) error {
	header := reader.ReadHeader()
	fileSize := int64(0)
	if fi, err := os.Stat(filePath); err == nil {
		fileSize = fi.Size()
	}

	encoding, _ := format.ParseFlags(header.Flags)

	d := dumpReportData{
		FilePath:  filePath,
		SizeMB:    fmt.Sprintf("%.1f", float64(fileSize)/1024/1024),
		Version:   version,
		Encoding:  fmt.Sprintf("%d", encoding),
		Terms:     fmt.Sprintf("%d", header.TermCount),
		Docs:      fmt.Sprintf("%d", header.DocumentCount),
		TermCount: header.TermCount,
		DocCount:  header.DocumentCount,
	}

	if includeGramFreq {
		gf, err := collectGramFrequency(reader, gramSamples)
		if err != nil {
			return fmt.Errorf("gram frequency: %w", err)
		}
		d.HasGramFreq = true
		d.GramFreq = gf
	}

	tmpl, err := template.New("dump").Parse(dumpHTMLTemplate)
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

type gramFreqData struct {
	MaxFreq    uint64
	MedianFreq uint64
	P95Freq    uint64
	P99Freq    uint64
	PointsJSON template.JS
}

func collectGramFrequency(reader logline.Reader, samples int) (*gramFreqData, error) {
	header := reader.ReadHeader()
	fmt.Fprintf(os.Stderr, "Collecting gram frequencies (%d terms)...\n", header.TermCount)

	it, err := reader.NewTermIterator()
	if err != nil {
		return nil, err
	}

	freqs := make([]uint64, 0, header.TermCount)
	count := 0
	for it.Next() {
		bm := it.Bitmap()
		if !bm.MatchesAll {
			freqs = append(freqs, bm.Roaring.GetCardinality())
		}
		count++
		if count%500_000 == 0 {
			fmt.Fprintf(os.Stderr, "  processed %d terms...\n", count)
		}
	}
	if err := it.Err(); err != nil {
		return nil, err
	}

	sort.Slice(freqs, func(i, j int) bool { return freqs[i] < freqs[j] })
	n := len(freqs)
	if n == 0 {
		return &gramFreqData{}, nil
	}

	numPts := samples
	if numPts > n {
		numPts = n
	}

	type point struct {
		X float64 `json:"x"`
		Y uint64  `json:"y"`
	}
	points := make([]point, numPts)
	for i := 0; i < numPts; i++ {
		idx := int(float64(i) * float64(n-1) / float64(numPts-1))
		pct := float64(idx) / float64(n-1) * 100
		points[i] = point{X: pct, Y: freqs[idx]}
	}

	pointsBytes, err := json.Marshal(points)
	if err != nil {
		return nil, err
	}

	return &gramFreqData{
		MaxFreq:    freqs[n-1],
		MedianFreq: freqs[n/2],
		P95Freq:    freqs[int(float64(n)*0.95)],
		P99Freq:    freqs[int(float64(n)*0.99)],
		PointsJSON: template.JS(pointsBytes),
	}, nil
}

type dumpReportData struct {
	FilePath  string
	SizeMB    string
	Version   string
	Encoding  string
	Terms     string
	Docs      string
	TermCount uint64
	DocCount  uint32

	HasGramFreq bool
	GramFreq    *gramFreqData
}

const dumpHTMLTemplate = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<title>Logline Index — Dump Report</title>
<style>
  :root { --bg: #0f172a; --card: #1e293b; --border: #334155; --text: #e2e8f0; --muted: #94a3b8; --green: #34d399; --blue: #60a5fa; --amber: #fbbf24; --red: #f85149; }
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
  .chart-wrap { position: relative; width: 100%; margin-top: 0.75rem; }
  canvas { width: 100%; height: 300px; display: block; border: 1px solid var(--border); border-radius: 6px; background: #161b22; }
  .tooltip { position: absolute; display: none; background: #1c2128; border: 1px solid var(--border); border-radius: 4px; padding: 6px 10px; font-size: 12px; color: var(--text); pointer-events: none; white-space: nowrap; }
  .controls { margin-top: 0.5rem; font-size: 0.8rem; color: var(--muted); }
  .controls label { cursor: pointer; }
  .controls input { margin-right: 4px; }
</style>
</head>
<body>

<h1>Logline Index — Dump Report</h1>
<p class="sub">{{.FilePath}}</p>

<div class="grid">

<div class="card">
  <h2>Index</h2>
  <div class="stat"><span class="label">Size</span><span class="value">{{.SizeMB}} MB</span></div>
  <div class="stat"><span class="label">Version</span><span class="value">{{.Version}}</span></div>
  <div class="stat"><span class="label">Encoding</span><span class="value">{{.Encoding}}</span></div>
</div>

<div class="card">
  <h2>Contents</h2>
  <div class="stat"><span class="label">Terms</span><span class="value">{{.Terms}}</span></div>
  <div class="stat"><span class="label">Documents</span><span class="value">{{.Docs}}</span></div>
</div>

{{if .HasGramFreq}}
<div class="card full">
  <h2>N-Gram Frequency Distribution</h2>
  <div style="display:flex; gap:2rem; margin-bottom:0.5rem; font-size:0.85rem">
    <div><span style="color:var(--muted)">Max:</span> <strong>{{.GramFreq.MaxFreq}}</strong> docs</div>
    <div><span style="color:var(--muted)">Median:</span> <strong>{{.GramFreq.MedianFreq}}</strong> docs</div>
    <div><span style="color:var(--muted)">P95:</span> <strong>{{.GramFreq.P95Freq}}</strong> docs</div>
    <div><span style="color:var(--muted)">P99:</span> <strong>{{.GramFreq.P99Freq}}</strong> docs</div>
  </div>
  <div class="controls">
    <label><input type="checkbox" id="logScale"> Log scale (Y axis)</label>
  </div>
  <div class="chart-wrap">
    <canvas id="freqChart"></canvas>
    <div class="tooltip" id="tooltip"></div>
  </div>
</div>

<script>
const raw = {{.GramFreq.PointsJSON}};
const canvas = document.getElementById('freqChart');
const ctx = canvas.getContext('2d');
const tooltip = document.getElementById('tooltip');
const logCheck = document.getElementById('logScale');
const PAD = { top: 30, right: 30, bottom: 50, left: 80 };

function resize() {
  const rect = canvas.getBoundingClientRect();
  canvas.width = rect.width * devicePixelRatio;
  canvas.height = rect.height * devicePixelRatio;
  ctx.setTransform(devicePixelRatio, 0, 0, devicePixelRatio, 0, 0);
}
function maxY() { return raw[raw.length - 1].y; }
function yVal(v, log) { return !log ? v : (v <= 0 ? 0 : Math.log10(v)); }
function fmtNum(n) { return n.toLocaleString(); }

function draw() {
  resize();
  const w = canvas.width / devicePixelRatio, h = canvas.height / devicePixelRatio;
  const cw = w - PAD.left - PAD.right, ch = h - PAD.top - PAD.bottom;
  const log = logCheck.checked;
  const yMax = yVal(maxY(), log);
  const yMin = log ? yVal(Math.max(1, raw[0].y), log) : 0;
  const yRange = yMax - yMin || 1;
  ctx.clearRect(0, 0, w, h);

  ctx.strokeStyle = '#21262d'; ctx.fillStyle = '#8b949e'; ctx.font = '11px monospace';
  ctx.textAlign = 'right'; ctx.textBaseline = 'middle';
  for (let i = 0; i <= 8; i++) {
    const frac = i / 8, py = PAD.top + ch - frac * ch;
    const val = log ? Math.pow(10, yMin + frac * yRange) : frac * yMax;
    ctx.beginPath(); ctx.moveTo(PAD.left, py); ctx.lineTo(w - PAD.right, py); ctx.stroke();
    ctx.fillText(fmtNum(Math.round(val)), PAD.left - 8, py);
  }
  ctx.textAlign = 'center'; ctx.textBaseline = 'top';
  for (let p = 0; p <= 100; p += 10) {
    const px = PAD.left + (p / 100) * cw;
    ctx.beginPath(); ctx.moveTo(px, PAD.top); ctx.lineTo(px, PAD.top + ch); ctx.stroke();
    ctx.fillText(p + '%', px, PAD.top + ch + 6);
  }
  ctx.save(); ctx.translate(14, PAD.top + ch / 2); ctx.rotate(-Math.PI / 2);
  ctx.textAlign = 'center'; ctx.fillText('Doc count' + (log ? ' (log₁₀)' : ''), 0, 0); ctx.restore();

  ctx.beginPath();
  let firstPx;
  for (let i = 0; i < raw.length; i++) {
    const px = PAD.left + (raw[i].x / 100) * cw;
    const py = PAD.top + ch - ((yVal(raw[i].y, log) - yMin) / yRange) * ch;
    if (i === 0) { ctx.moveTo(px, py); firstPx = px; } else ctx.lineTo(px, py);
  }
  const lastPx = PAD.left + (raw[raw.length - 1].x / 100) * cw;
  ctx.lineTo(lastPx, PAD.top + ch); ctx.lineTo(firstPx, PAD.top + ch); ctx.closePath();
  const grad = ctx.createLinearGradient(0, PAD.top, 0, PAD.top + ch);
  grad.addColorStop(0, 'rgba(31,111,235,0.35)'); grad.addColorStop(1, 'rgba(31,111,235,0.02)');
  ctx.fillStyle = grad; ctx.fill();

  ctx.beginPath();
  for (let i = 0; i < raw.length; i++) {
    const px = PAD.left + (raw[i].x / 100) * cw;
    const py = PAD.top + ch - ((yVal(raw[i].y, log) - yMin) / yRange) * ch;
    if (i === 0) ctx.moveTo(px, py); else ctx.lineTo(px, py);
  }
  ctx.strokeStyle = '#58a6ff'; ctx.lineWidth = 1.5; ctx.stroke();

  [{pct:50,label:'median',val:{{.GramFreq.MedianFreq}},color:'#3fb950'},
   {pct:95,label:'P95',val:{{.GramFreq.P95Freq}},color:'#d29922'},
   {pct:99,label:'P99',val:{{.GramFreq.P99Freq}},color:'#f85149'}].forEach(m => {
    const px = PAD.left + (m.pct / 100) * cw;
    ctx.beginPath(); ctx.strokeStyle = m.color; ctx.lineWidth = 1; ctx.setLineDash([4, 4]);
    ctx.moveTo(px, PAD.top); ctx.lineTo(px, PAD.top + ch); ctx.stroke(); ctx.setLineDash([]);
    ctx.fillStyle = m.color; ctx.font = 'bold 11px monospace'; ctx.textAlign = 'left'; ctx.textBaseline = 'top';
    ctx.fillText(m.label + ' = ' + fmtNum(m.val), px + 4, PAD.top + 4);
  });
}

canvas.addEventListener('mousemove', e => {
  const rect = canvas.getBoundingClientRect();
  const mx = e.clientX - rect.left, cw = rect.width - PAD.left - PAD.right;
  const pct = ((mx - PAD.left) / cw) * 100;
  if (pct < 0 || pct > 100) { tooltip.style.display = 'none'; return; }
  let lo = 0, hi = raw.length - 1;
  while (lo < hi) { const mid = (lo + hi) >> 1; if (raw[mid].x < pct) lo = mid + 1; else hi = mid; }
  const pt = raw[lo];
  tooltip.innerHTML = 'Percentile: <b>' + pt.x.toFixed(1) + '%</b><br>Doc count: <b>' + fmtNum(pt.y) + '</b>';
  tooltip.style.display = 'block'; tooltip.style.left = (mx + 12) + 'px'; tooltip.style.top = '40px';
});
canvas.addEventListener('mouseleave', () => { tooltip.style.display = 'none'; });
logCheck.addEventListener('change', draw);
window.addEventListener('resize', draw);
draw();
</script>
{{end}}

</div>
</body>
</html>`
