package main

import (
	"errors"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/grafana/loki/pkg/coprocessor/proto"
)

func main() {
	http.HandleFunc("/pre_query_by_XRay_traceID", PreQuery)
	err := http.ListenAndServe(":9093", nil)
	if err != nil {
		panic(err)
	}
}

func PreQuery(res http.ResponseWriter, request *http.Request) {
	req, err := io.ReadAll(request.Body)
	if err != nil {
		http.Error(res, err.Error(), http.StatusInternalServerError)
		return
	}
	queryPreQueryRequest := proto.QueryPreQueryRequest{}
	err = queryPreQueryRequest.Unmarshal(req)
	if err != nil {
		http.Error(res, err.Error(), http.StatusInternalServerError)
		return
	}

	logql := queryPreQueryRequest.Selector
	traceId := ExtractXRayTraceId(logql)
	if traceId == "" {
		http.Error(res, "logql do not contain XTray TraceID", http.StatusBadRequest)
		return
	}

	traceGenTime, err := ExtractTimeFromTraceID(traceId)
	if err != nil {
		http.Error(res, err.Error(), http.StatusInternalServerError)
		return
	}

	minThreshold := traceGenTime.Add(-time.Minute * 5).UnixMilli()
	maxThreshold := traceGenTime.Add(time.Minute * 25).UnixMilli()
	startMs := queryPreQueryRequest.Start.UnixMilli()
	endMs := queryPreQueryRequest.End.UnixMilli()
	if startMs <= maxThreshold && endMs >= minThreshold {
		//
		//   [case1: startMs ->endMs]  [case2: startMs -> endMs]      [ case 3: startMs -> endMs]
		//-------------------[minThreshold                       -->     maxThreshold]-----------
		//
		returnPass(true, res)
		return
	}
	returnPass(false, res)
}

func returnPass(pass bool, res http.ResponseWriter) {
	queryPreQueryResponse := proto.QueryPreQueryResponse{}
	queryPreQueryResponse.Pass = pass
	marshal, err := queryPreQueryResponse.Marshal()
	if err != nil {
		http.Error(res, err.Error(), http.StatusInternalServerError)
		return
	}
	res.WriteHeader(http.StatusOK)
	_, err = res.Write(marshal)
	if err != nil {
		http.Error(res, err.Error(), http.StatusInternalServerError)
	}
}

var XRayRegex = "[1]{1}-[0-9a-f]{8}-[0-9a-f]{24}"

func ExtractXRayTraceId(line string) string {
	rgx := regexp.MustCompile(XRayRegex)
	matchString := rgx.MatchString(line)
	if matchString {
		//1-63eaf698-bd430558a4f70617e12e9b71
		findString := rgx.FindString(line)
		return findString
	}
	return ""
}

func ExtractTimeFromTraceID(id string) (time.Time, error) {
	rgx := regexp.MustCompile(XRayRegex)
	matchString := rgx.MatchString(id)
	if matchString {
		//1-63eaf698-bd430558a4f70617e12e9b71
		findString := rgx.FindString(id)
		start := strings.Index(findString, "-")
		end := strings.LastIndex(findString, "-")
		timestampHex := findString[start+1 : end]
		timestamp, err := strconv.ParseInt(timestampHex, 16, 0)
		if err != nil {
			return time.Now(), err
		}
		timeF := time.Unix(timestamp, 0)
		return timeF, nil
	}
	return time.Now(), errors.New("ExtractTimeFromTraceID fail")
}
