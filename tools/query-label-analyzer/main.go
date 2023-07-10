package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/bsipos/thist"
	"github.com/dustin/go-humanize"
	"github.com/prometheus/prometheus/model/labels"
	"golang.org/x/exp/slices"

	"github.com/grafana/loki/pkg/logql/log"
	"github.com/grafana/loki/pkg/logql/syntax"
)

type parser struct {
	labelInMatcher      map[string]int
	labelValueInMatcher map[string]int
	labelInGroup        map[string]int
	matchers            map[string]int
	matchLabel          string
	labelCountHist      *thist.Hist
	lengthHist          *thist.Hist
	bytesHist           *thist.Hist
}

func main() {

	label := flag.String("label", "", "label to aggregate results on")
	flag.Parse()

	matchLabel := *label
	if label == nil {
		matchLabel = ""
	}

	//testQuery := `avg(sum(count_over_time({stream_filter="JPAGEC",environment="pro",platform=~"Openshift-Meccano Interxion 2|Openshift-Meccano Telecity 2",response_status!~"5..",route_paths_1="/itxrest/2/order/store/[^/]+/shipping/unique/?$",sre_obs_perf=~"1"} |="apigw-swapi.md.apps" | json | request_method="GET" | request_headers_user_agent=~".*Android.*" [1m])) by (response_status,request_method,route_paths_1,itxsessionid)) by (response_status,request_method,route_paths_1)`

	p := parser{
		labelInMatcher:      map[string]int{},
		labelValueInMatcher: map[string]int{},
		labelInGroup:        map[string]int{},
		matchers:            map[string]int{},
		matchLabel:          matchLabel,
		labelCountHist:      thist.NewHist(nil, "Matchers Per Query", "fixed", 10, false),
		lengthHist:          thist.NewHist(nil, "Query Length", "fixed", 10, false),
		bytesHist:           thist.NewHist(nil, "Query Bytes", "fixed", 10, false),
	}

	scanner := bufio.NewScanner(os.Stdin)
	//sb := strings.Builder{}
	for scanner.Scan() {
		query := scanner.Text()
		pr := log.NewLogfmtParser()
		builder := log.NewBaseLabelsBuilder()
		lb := builder.ForLabels(labels.Labels{}, 0)
		pr.Process(0, []byte(query), lb)
		//fmt.Println(lb.LabelsResult().String())
		q, _ := lb.Get("query")
		p.parseQuery(q)
		p.parseMetris(lb)
		//if strings.HasPrefix(query, "QUERY:") {
		//	query = strings.TrimPrefix(query, "QUERY:")
		//	if sb.Len() > 0 {
		//		p.parseQuery(sb.String())
		//	}
		//	sb.Reset()
		//	sb.WriteString(query)
		//	sb.WriteString("\n")
		//} else {
		//	sb.WriteString(query)
		//	sb.WriteString("\n")
		//}
	}

	p.printResult()

}

func (p *parser) parseMetris(lb *log.LabelsBuilder) {
	if l, ok := lb.Get("length"); ok {
		d, err := time.ParseDuration(l)
		if err == nil {
			p.lengthHist.Update(d.Hours())
		}
	}
	if b, ok := lb.Get("total_bytes"); ok {
		by, err := humanize.ParseBytes(b)
		if err == nil {
			p.bytesHist.Update(float64(by) / 1e9)
		}
	}
}

func (p *parser) parseQuery(query string) {
	e, err := syntax.ParseExpr(query)
	if err != nil {
		fmt.Println("failed to parse query", query, "err", err)
		return
	}

	matchersInQuery := 0

	e.Walk(func(e interface{}) {
		switch expr := e.(type) {
		case *syntax.MatchersExpr:

			if _, ok := p.matchers[expr.String()]; ok {
				p.matchers[expr.String()]++
			} else {
				p.matchers[expr.String()] = 1
			}

			for _, l := range expr.Mts {
				matchersInQuery++
				if p.matchLabel == "" || l.Name == p.matchLabel {
					if _, ok := p.labelInMatcher[l.Name]; ok {
						p.labelInMatcher[l.Name]++
					} else {
						p.labelInMatcher[l.Name] = 1
					}

					if _, ok := p.labelValueInMatcher[l.Name+"="+l.Value]; ok {
						p.labelValueInMatcher[l.Name+"="+l.Value]++
					} else {
						p.labelValueInMatcher[l.Name+"="+l.Value] = 1
					}
				}
			}
		case *syntax.VectorAggregationExpr:
			for _, g := range expr.Grouping.Groups {
				if p.matchLabel == "" || g == p.matchLabel {
					if _, ok := p.labelInGroup[g]; ok {
						p.labelInGroup[g]++
					} else {
						p.labelInGroup[g] = 1
					}
				}
			}
		case syntax.RangeAggregationExpr:
			for _, g := range expr.Grouping.Groups {
				if p.matchLabel == "" || g == p.matchLabel {
					if _, ok := p.labelInGroup[g]; ok {
						p.labelInGroup[g]++
					} else {
						p.labelInGroup[g] = 1
					}
				}
			}
		}
	})

	//if matchersInQuery > 2 {
	//	fmt.Println(query)
	//}

	p.labelCountHist.Update(float64(matchersInQuery))
}

func (p *parser) printResult() {
	type labelCount struct {
		name  string
		count int
	}

	// make slices so we can sort
	labelValueInMatherSlice := make([]*labelCount, 0, len(p.labelValueInMatcher))
	for k, v := range p.labelValueInMatcher {
		labelValueInMatherSlice = append(labelValueInMatherSlice, &labelCount{k, v})
	}
	slices.SortFunc[*labelCount](labelValueInMatherSlice, func(a *labelCount, b *labelCount) bool {
		return a.count < b.count
	})

	labelInGroupSlice := make([]*labelCount, 0, len(p.labelInGroup))
	for k, v := range p.labelInGroup {
		labelInGroupSlice = append(labelInGroupSlice, &labelCount{k, v})
	}
	slices.SortFunc[*labelCount](labelInGroupSlice, func(a *labelCount, b *labelCount) bool {
		return a.count < b.count
	})

	fmt.Println("Labels in matchers")
	fmt.Println()

	for _, v := range labelValueInMatherSlice {
		fmt.Printf("%s %d\n", v.name, v.count)
	}

	//fmt.Println()
	//fmt.Println()
	//fmt.Println("Labels in groupings")
	//fmt.Println()
	//for _, v := range labelInGroupSlice {
	//	fmt.Printf("%s %d\n", v.name, v.count)
	//}

	// make slices of labelInMatcher so we can sort
	labelInMatcherSlice := make([]*labelCount, 0, len(p.labelInMatcher))
	for k, v := range p.labelInMatcher {
		labelInMatcherSlice = append(labelInMatcherSlice, &labelCount{k, v})
	}
	slices.SortFunc[*labelCount](labelInMatcherSlice, func(a *labelCount, b *labelCount) bool {
		return a.count < b.count
	})
	fmt.Println()
	fmt.Println()
	fmt.Println("Labels in matchers")
	fmt.Println()
	for _, v := range labelInMatcherSlice {
		fmt.Printf("%s %d\n", v.name, v.count)
	}

	// make slices of matchers so we can sort
	matchersSlice := make([]*labelCount, 0, len(p.matchers))
	for k, v := range p.matchers {
		matchersSlice = append(matchersSlice, &labelCount{k, v})
	}
	slices.SortFunc[*labelCount](matchersSlice, func(a *labelCount, b *labelCount) bool {
		return a.count < b.count
	})

	fmt.Println()
	fmt.Println()
	fmt.Println("Matchers")
	fmt.Println()
	for _, v := range matchersSlice {
		fmt.Printf("%s %d\n", v.name, v.count)
	}

	fmt.Println(p.labelCountHist.Draw())
	fmt.Println(p.lengthHist.Draw())
	fmt.Println(p.bytesHist.Draw())
}
