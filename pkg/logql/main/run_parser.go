package main

import (
	"C"
	"encoding/json"
	"fmt"
	"github.com/grafana/loki/pkg/logql/log"
	"github.com/grafana/loki/pkg/logql/syntax"
	"text/template"
	"text/template/parse"
)

type TemplateWrapper struct {
	*template.Template
}

func (tw *TemplateWrapper) MarshalJSON() ([]byte, error) {
	type Alias TemplateWrapper

	// Convert the parse.Tree into a JSON-compatible format.
	slice := serializeNodes([]map[string]interface{}{}, tw.Root.Nodes)
	return json.Marshal(slice)
}

func toMapFunc(s interface{}) map[string]interface{} {
	marshalled, _ := json.Marshal(s)
	m := make(map[string]interface{})
	err := json.Unmarshal(marshalled, &m)
	if err != nil {
		return nil
	}
	return m
}

func serializeNodes(list []map[string]interface{}, nodes []parse.Node) []map[string]interface{} {
	for _, node := range nodes {
		switch n := node.(type) {
		case *parse.ActionNode:
			// Handle action nodes here
			var commandsList [][]map[string]interface{}
			for _, commandNode := range n.Pipe.Cmds {
				var argsList []map[string]interface{}
				for _, argNode := range commandNode.Args {
					if argNode.Type() != 13 {
						argsList = append(argsList, toMapFunc(argNode))
					} else {
						numNode, _ := argNode.(*parse.NumberNode)
						serializedNumNode := map[string]interface{}{
							"NodeType":   numNode.Type(),
							"Int64":      numNode.Int64,
							"Uint64":     numNode.Uint64,
							"Float64":    numNode.Float64,
							"IsFlot":     numNode.IsFloat,
							"IsInt":      numNode.IsInt,
							"IsComplex":  numNode.IsComplex,
							"Complex128": fmt.Sprintf("%g", numNode.Complex128),
						}
						argsList = append(argsList, serializedNumNode)
					}
				}
				commandsList = append(commandsList, argsList)
			}

			actionNode := map[string]interface{}{
				"NodeType": n.Type(),
				"Pipe":     commandsList,
				"Pos":      n.Pos,
				"Line":     n.Line,
			}
			list = append(list, actionNode)

		case *parse.ListNode:
			serializeNodes(list, nodes)

		default:
			list = append(list, toMapFunc(n))
		}
	}
	return list
}

func main() {
	formatter, err := log.NewFormatter("Name: {{(fromJson .).name}}, Age: {{(fromJson .).age}}")
	if err != nil {
		println(err.Error())
	}

	tmpl := formatter.Template
	v := &TemplateWrapper{tmpl}
	marshalled, err := json.Marshal(v)
	println(string(marshalled))
}

//export toTemplateAST
func toTemplateAST(template *C.char) *C.char {
	formatter, _ := log.NewFormatter(string(C.GoString(template)))
	v := &TemplateWrapper{formatter.Template}
	marshalled, err := json.Marshal(v)
	if err != nil {
		return C.CString(err.Error())
	}
	return C.CString(string(marshalled))
}

//export toAST
func toAST(query *C.char) *C.char {
	queryS := C.GoString(query)
	expr, err := syntax.ParseExpr(queryS)
	if err != nil {
		return C.CString(err.Error())
	}

	marshalled, _ := json.Marshal(expr)
	return C.CString(string(marshalled))
}
