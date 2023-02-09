package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"

	"golang.org/x/exp/slices"

	"github.com/grafana/loki/pkg/logql/syntax"
)

func main() {

	label := flag.String("label", "", "label to aggregate results on")
	flag.Parse()

	matchLabel := *label
	if label == nil {
		matchLabel = ""
	}

	//testQuery := `avg(sum(count_over_time({stream_filter="JPAGEC",environment="pro",platform=~"Openshift-Meccano Interxion 2|Openshift-Meccano Telecity 2",response_status!~"5..",route_paths_1="/itxrest/2/order/store/[^/]+/shipping/unique/?$",sre_obs_perf=~"1"} |="apigw-swapi.md.apps" | json | request_method="GET" | request_headers_user_agent=~".*Android.*" [1m])) by (response_status,request_method,route_paths_1,itxsessionid)) by (response_status,request_method,route_paths_1)`

	labelValueInMatcher := map[string]int{}
	labelInGroup := map[string]int{}

	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		query := scanner.Text()

		e, err := syntax.ParseExpr(query)
		if err != nil {
			fmt.Println("failed to parse query", query)
			continue
		}

		e.Walk(func(e interface{}) {
			switch expr := e.(type) {
			case *syntax.MatchersExpr:
				for _, l := range expr.Mts {
					if matchLabel == "" || l.Name == matchLabel {
						if _, ok := labelValueInMatcher[l.Name+"="+l.Value]; ok {
							labelValueInMatcher[l.Name+"="+l.Value]++
						} else {
							labelValueInMatcher[l.Name+"="+l.Value] = 1
						}
					}
				}
			case *syntax.VectorAggregationExpr:
				for _, g := range expr.Grouping.Groups {
					if matchLabel == "" || g == matchLabel {
						if _, ok := labelInGroup[g]; ok {
							labelInGroup[g]++
						} else {
							labelInGroup[g] = 1
						}
					}
				}
			case syntax.RangeAggregationExpr:
				for _, g := range expr.Grouping.Groups {
					if matchLabel == "" || g == matchLabel {
						if _, ok := labelInGroup[g]; ok {
							labelInGroup[g]++
						} else {
							labelInGroup[g] = 1
						}
					}
				}
			}
		})

	}

	type labelCount struct {
		name  string
		count int
	}

	// make slices so we can sort
	labelValueInMatherSlice := make([]*labelCount, 0, len(labelValueInMatcher))
	for k, v := range labelValueInMatcher {
		labelValueInMatherSlice = append(labelValueInMatherSlice, &labelCount{k, v})
	}
	slices.SortFunc[*labelCount](labelValueInMatherSlice, func(a *labelCount, b *labelCount) bool {
		return a.count < b.count
	})

	labelInGroupSlice := make([]*labelCount, 0, len(labelInGroup))
	for k, v := range labelInGroup {
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

	fmt.Println()
	fmt.Println()
	fmt.Println("Labels in groupings")
	fmt.Println()
	for _, v := range labelInGroupSlice {
		fmt.Printf("%s %d\n", v.name, v.count)
	}

}
