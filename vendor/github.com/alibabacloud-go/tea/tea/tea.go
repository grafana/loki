package tea

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"math/rand"
	"net"
	"net/http"
	"net/url"
	"os"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/alibabacloud-go/debug/debug"
	"github.com/alibabacloud-go/tea/utils"

	"golang.org/x/net/proxy"
)

var debugLog = debug.Init("tea")

var hookDo = func(fn func(req *http.Request) (*http.Response, error)) func(req *http.Request) (*http.Response, error) {
	return fn
}

var basicTypes = []string{
	"int", "int16", "int64", "int32", "float32", "float64", "string", "bool", "uint64", "uint32", "uint16",
}

// Verify whether the parameters meet the requirements
var validateParams = []string{"require", "pattern", "maxLength", "minLength", "maximum", "minimum", "maxItems", "minItems"}

// CastError is used for cast type fails
type CastError struct {
	Message *string
}

// Request is used wrap http request
type Request struct {
	Protocol *string
	Port     *int
	Method   *string
	Pathname *string
	Domain   *string
	Headers  map[string]*string
	Query    map[string]*string
	Body     io.Reader
}

// Response is use d wrap http response
type Response struct {
	Body          io.ReadCloser
	StatusCode    *int
	StatusMessage *string
	Headers       map[string]*string
}

// SDKError struct is used save error code and message
type SDKError struct {
	Code               *string
	StatusCode         *int
	Message            *string
	Data               *string
	Stack              *string
	errMsg             *string
	Description        *string
	AccessDeniedDetail map[string]interface{}
}

// RuntimeObject is used for converting http configuration
type RuntimeObject struct {
	IgnoreSSL      *bool                  `json:"ignoreSSL" xml:"ignoreSSL"`
	ReadTimeout    *int                   `json:"readTimeout" xml:"readTimeout"`
	ConnectTimeout *int                   `json:"connectTimeout" xml:"connectTimeout"`
	LocalAddr      *string                `json:"localAddr" xml:"localAddr"`
	HttpProxy      *string                `json:"httpProxy" xml:"httpProxy"`
	HttpsProxy     *string                `json:"httpsProxy" xml:"httpsProxy"`
	NoProxy        *string                `json:"noProxy" xml:"noProxy"`
	MaxIdleConns   *int                   `json:"maxIdleConns" xml:"maxIdleConns"`
	Key            *string                `json:"key" xml:"key"`
	Cert           *string                `json:"cert" xml:"cert"`
	CA             *string                `json:"ca" xml:"ca"`
	Socks5Proxy    *string                `json:"socks5Proxy" xml:"socks5Proxy"`
	Socks5NetWork  *string                `json:"socks5NetWork" xml:"socks5NetWork"`
	Listener       utils.ProgressListener `json:"listener" xml:"listener"`
	Tracker        *utils.ReaderTracker   `json:"tracker" xml:"tracker"`
	Logger         *utils.Logger          `json:"logger" xml:"logger"`
}

type teaClient struct {
	sync.Mutex
	httpClient *http.Client
	ifInit     bool
}

var clientPool = &sync.Map{}

func (r *RuntimeObject) getClientTag(domain string) string {
	return strconv.FormatBool(BoolValue(r.IgnoreSSL)) + strconv.Itoa(IntValue(r.ReadTimeout)) +
		strconv.Itoa(IntValue(r.ConnectTimeout)) + StringValue(r.LocalAddr) + StringValue(r.HttpProxy) +
		StringValue(r.HttpsProxy) + StringValue(r.NoProxy) + StringValue(r.Socks5Proxy) + StringValue(r.Socks5NetWork) + domain
}

// NewRuntimeObject is used for shortly create runtime object
func NewRuntimeObject(runtime map[string]interface{}) *RuntimeObject {
	if runtime == nil {
		return &RuntimeObject{}
	}

	runtimeObject := &RuntimeObject{
		IgnoreSSL:      TransInterfaceToBool(runtime["ignoreSSL"]),
		ReadTimeout:    TransInterfaceToInt(runtime["readTimeout"]),
		ConnectTimeout: TransInterfaceToInt(runtime["connectTimeout"]),
		LocalAddr:      TransInterfaceToString(runtime["localAddr"]),
		HttpProxy:      TransInterfaceToString(runtime["httpProxy"]),
		HttpsProxy:     TransInterfaceToString(runtime["httpsProxy"]),
		NoProxy:        TransInterfaceToString(runtime["noProxy"]),
		MaxIdleConns:   TransInterfaceToInt(runtime["maxIdleConns"]),
		Socks5Proxy:    TransInterfaceToString(runtime["socks5Proxy"]),
		Socks5NetWork:  TransInterfaceToString(runtime["socks5NetWork"]),
		Key:            TransInterfaceToString(runtime["key"]),
		Cert:           TransInterfaceToString(runtime["cert"]),
		CA:             TransInterfaceToString(runtime["ca"]),
	}
	if runtime["listener"] != nil {
		runtimeObject.Listener = runtime["listener"].(utils.ProgressListener)
	}
	if runtime["tracker"] != nil {
		runtimeObject.Tracker = runtime["tracker"].(*utils.ReaderTracker)
	}
	if runtime["logger"] != nil {
		runtimeObject.Logger = runtime["logger"].(*utils.Logger)
	}
	return runtimeObject
}

// NewCastError is used for cast type fails
func NewCastError(message *string) (err error) {
	return &CastError{
		Message: message,
	}
}

// NewRequest is used shortly create Request
func NewRequest() (req *Request) {
	return &Request{
		Headers: map[string]*string{},
		Query:   map[string]*string{},
	}
}

// NewResponse is create response with http response
func NewResponse(httpResponse *http.Response) (res *Response) {
	res = &Response{}
	res.Body = httpResponse.Body
	res.Headers = make(map[string]*string)
	res.StatusCode = Int(httpResponse.StatusCode)
	res.StatusMessage = String(httpResponse.Status)
	return
}

// NewSDKError is used for shortly create SDKError object
func NewSDKError(obj map[string]interface{}) *SDKError {
	err := &SDKError{}
	if val, ok := obj["code"].(int); ok {
		err.Code = String(strconv.Itoa(val))
	} else if val, ok := obj["code"].(string); ok {
		err.Code = String(val)
	}

	if obj["message"] != nil {
		err.Message = String(obj["message"].(string))
	}
	if obj["description"] != nil {
		err.Description = String(obj["description"].(string))
	}
	if detail := obj["accessDeniedDetail"]; detail != nil {
		r := reflect.ValueOf(detail)
		if r.Kind().String() == "map" {
			res := make(map[string]interface{})
			tmp := r.MapKeys()
			for _, key := range tmp {
				res[key.String()] = r.MapIndex(key).Interface()
			}
			err.AccessDeniedDetail = res
		}
	}
	if data := obj["data"]; data != nil {
		r := reflect.ValueOf(data)
		if r.Kind().String() == "map" {
			res := make(map[string]interface{})
			tmp := r.MapKeys()
			for _, key := range tmp {
				res[key.String()] = r.MapIndex(key).Interface()
			}
			if statusCode := res["statusCode"]; statusCode != nil {
				if code, ok := statusCode.(int); ok {
					err.StatusCode = Int(code)
				} else if tmp, ok := statusCode.(string); ok {
					code, err_ := strconv.Atoi(tmp)
					if err_ == nil {
						err.StatusCode = Int(code)
					}
				} else if code, ok := statusCode.(*int); ok {
					err.StatusCode = code
				}
			}
		}
		byt := bytes.NewBuffer([]byte{})
		jsonEncoder := json.NewEncoder(byt)
		jsonEncoder.SetEscapeHTML(false)
		jsonEncoder.Encode(data)
		err.Data = String(string(bytes.TrimSpace(byt.Bytes())))
	}

	if statusCode, ok := obj["statusCode"].(int); ok {
		err.StatusCode = Int(statusCode)
	} else if status, ok := obj["statusCode"].(string); ok {
		statusCode, err_ := strconv.Atoi(status)
		if err_ == nil {
			err.StatusCode = Int(statusCode)
		}
	}

	return err
}

// Set ErrMsg by msg
func (err *SDKError) SetErrMsg(msg string) {
	err.errMsg = String(msg)
}

func (err *SDKError) Error() string {
	if err.errMsg == nil {
		str := fmt.Sprintf("SDKError:\n   StatusCode: %d\n   Code: %s\n   Message: %s\n   Data: %s\n",
			IntValue(err.StatusCode), StringValue(err.Code), StringValue(err.Message), StringValue(err.Data))
		err.SetErrMsg(str)
	}
	return StringValue(err.errMsg)
}

// Return message of CastError
func (err *CastError) Error() string {
	return StringValue(err.Message)
}

// Convert is use convert map[string]interface object to struct
func Convert(in interface{}, out interface{}) error {
	byt, _ := json.Marshal(in)
	decoder := jsonParser.NewDecoder(bytes.NewReader(byt))
	decoder.UseNumber()
	err := decoder.Decode(&out)
	return err
}

// Recover is used to format error
func Recover(in interface{}) error {
	if in == nil {
		return nil
	}
	return errors.New(fmt.Sprint(in))
}

// ReadBody is used read response body
func (response *Response) ReadBody() (body []byte, err error) {
	defer response.Body.Close()
	var buffer [512]byte
	result := bytes.NewBuffer(nil)

	for {
		n, err := response.Body.Read(buffer[0:])
		result.Write(buffer[0:n])
		if err != nil && err == io.EOF {
			break
		} else if err != nil {
			return nil, err
		}
	}
	return result.Bytes(), nil
}

func getTeaClient(tag string) *teaClient {
	client, ok := clientPool.Load(tag)
	if client == nil && !ok {
		client = &teaClient{
			httpClient: &http.Client{},
			ifInit:     false,
		}
		clientPool.Store(tag, client)
	}
	return client.(*teaClient)
}

// DoRequest is used send request to server
func DoRequest(request *Request, requestRuntime map[string]interface{}) (response *Response, err error) {
	runtimeObject := NewRuntimeObject(requestRuntime)
	fieldMap := make(map[string]string)
	utils.InitLogMsg(fieldMap)
	defer func() {
		if runtimeObject.Logger != nil {
			runtimeObject.Logger.PrintLog(fieldMap, err)
		}
	}()
	if request.Method == nil {
		request.Method = String("GET")
	}

	if request.Protocol == nil {
		request.Protocol = String("http")
	} else {
		request.Protocol = String(strings.ToLower(StringValue(request.Protocol)))
	}

	requestURL := ""
	request.Domain = request.Headers["host"]
	if request.Port != nil {
		request.Domain = String(fmt.Sprintf("%s:%d", StringValue(request.Domain), IntValue(request.Port)))
	}
	requestURL = fmt.Sprintf("%s://%s%s", StringValue(request.Protocol), StringValue(request.Domain), StringValue(request.Pathname))
	queryParams := request.Query
	// sort QueryParams by key
	q := url.Values{}
	for key, value := range queryParams {
		q.Add(key, StringValue(value))
	}
	querystring := q.Encode()
	if len(querystring) > 0 {
		if strings.Contains(requestURL, "?") {
			requestURL = fmt.Sprintf("%s&%s", requestURL, querystring)
		} else {
			requestURL = fmt.Sprintf("%s?%s", requestURL, querystring)
		}
	}
	debugLog("> %s %s", StringValue(request.Method), requestURL)

	httpRequest, err := http.NewRequest(StringValue(request.Method), requestURL, request.Body)
	if err != nil {
		return
	}
	httpRequest.Host = StringValue(request.Domain)

	client := getTeaClient(runtimeObject.getClientTag(StringValue(request.Domain)))
	client.Lock()
	if !client.ifInit {
		trans, err := getHttpTransport(request, runtimeObject)
		if err != nil {
			return nil, err
		}
		client.httpClient.Timeout = time.Duration(IntValue(runtimeObject.ReadTimeout)) * time.Millisecond
		client.httpClient.Transport = trans
		client.ifInit = true
	}
	client.Unlock()
	for key, value := range request.Headers {
		if value == nil || key == "content-length" {
			continue
		} else if key == "host" {
			httpRequest.Header["Host"] = []string{*value}
			delete(httpRequest.Header, "host")
		} else if key == "user-agent" {
			httpRequest.Header["User-Agent"] = []string{*value}
			delete(httpRequest.Header, "user-agent")
		} else {
			httpRequest.Header[key] = []string{*value}
		}
		debugLog("> %s: %s", key, StringValue(value))
	}
	contentlength, _ := strconv.Atoi(StringValue(request.Headers["content-length"]))
	event := utils.NewProgressEvent(utils.TransferStartedEvent, 0, int64(contentlength), 0)
	utils.PublishProgress(runtimeObject.Listener, event)

	putMsgToMap(fieldMap, httpRequest)
	startTime := time.Now()
	fieldMap["{start_time}"] = startTime.Format("2006-01-02 15:04:05")
	res, err := hookDo(client.httpClient.Do)(httpRequest)
	fieldMap["{cost}"] = time.Since(startTime).String()
	completedBytes := int64(0)
	if runtimeObject.Tracker != nil {
		completedBytes = runtimeObject.Tracker.CompletedBytes
	}
	if err != nil {
		event = utils.NewProgressEvent(utils.TransferFailedEvent, completedBytes, int64(contentlength), 0)
		utils.PublishProgress(runtimeObject.Listener, event)
		return
	}

	event = utils.NewProgressEvent(utils.TransferCompletedEvent, completedBytes, int64(contentlength), 0)
	utils.PublishProgress(runtimeObject.Listener, event)

	response = NewResponse(res)
	fieldMap["{code}"] = strconv.Itoa(res.StatusCode)
	fieldMap["{res_headers}"] = transToString(res.Header)
	debugLog("< HTTP/1.1 %s", res.Status)
	for key, value := range res.Header {
		debugLog("< %s: %s", key, strings.Join(value, ""))
		if len(value) != 0 {
			response.Headers[strings.ToLower(key)] = String(value[0])
		}
	}
	return
}

func getHttpTransport(req *Request, runtime *RuntimeObject) (*http.Transport, error) {
	trans := new(http.Transport)
	httpProxy, err := getHttpProxy(StringValue(req.Protocol), StringValue(req.Domain), runtime)
	if err != nil {
		return nil, err
	}
	if strings.ToLower(*req.Protocol) == "https" {
		if BoolValue(runtime.IgnoreSSL) != true {
			trans.TLSClientConfig = &tls.Config{
				InsecureSkipVerify: false,
			}
			if runtime.Key != nil && runtime.Cert != nil && StringValue(runtime.Key) != "" && StringValue(runtime.Cert) != "" {
				cert, err := tls.X509KeyPair([]byte(StringValue(runtime.Cert)), []byte(StringValue(runtime.Key)))
				if err != nil {
					return nil, err
				}
				trans.TLSClientConfig.Certificates = []tls.Certificate{cert}
			}
			if runtime.CA != nil && StringValue(runtime.CA) != "" {
				clientCertPool := x509.NewCertPool()
				ok := clientCertPool.AppendCertsFromPEM([]byte(StringValue(runtime.CA)))
				if !ok {
					return nil, errors.New("Failed to parse root certificate")
				}
				trans.TLSClientConfig.RootCAs = clientCertPool
			}
		} else {
			trans.TLSClientConfig = &tls.Config{
				InsecureSkipVerify: true,
			}
		}
	}
	if httpProxy != nil {
		trans.Proxy = http.ProxyURL(httpProxy)
		if httpProxy.User != nil {
			password, _ := httpProxy.User.Password()
			auth := httpProxy.User.Username() + ":" + password
			basic := "Basic " + base64.StdEncoding.EncodeToString([]byte(auth))
			req.Headers["Proxy-Authorization"] = String(basic)
		}
	}
	if runtime.Socks5Proxy != nil && StringValue(runtime.Socks5Proxy) != "" {
		socks5Proxy, err := getSocks5Proxy(runtime)
		if err != nil {
			return nil, err
		}
		if socks5Proxy != nil {
			var auth *proxy.Auth
			if socks5Proxy.User != nil {
				password, _ := socks5Proxy.User.Password()
				auth = &proxy.Auth{
					User:     socks5Proxy.User.Username(),
					Password: password,
				}
			}
			dialer, err := proxy.SOCKS5(strings.ToLower(StringValue(runtime.Socks5NetWork)), socks5Proxy.String(), auth,
				&net.Dialer{
					Timeout:   time.Duration(IntValue(runtime.ConnectTimeout)) * time.Millisecond,
					DualStack: true,
					LocalAddr: getLocalAddr(StringValue(runtime.LocalAddr)),
				})
			if err != nil {
				return nil, err
			}
			trans.Dial = dialer.Dial
		}
	} else {
		trans.DialContext = setDialContext(runtime)
	}
	if runtime.MaxIdleConns != nil && *runtime.MaxIdleConns > 0 {
		trans.MaxIdleConns = IntValue(runtime.MaxIdleConns)
		trans.MaxIdleConnsPerHost = IntValue(runtime.MaxIdleConns)
	}
	return trans, nil
}

func transToString(object interface{}) string {
	byt, _ := json.Marshal(object)
	return string(byt)
}

func putMsgToMap(fieldMap map[string]string, request *http.Request) {
	fieldMap["{host}"] = request.Host
	fieldMap["{method}"] = request.Method
	fieldMap["{uri}"] = request.URL.RequestURI()
	fieldMap["{pid}"] = strconv.Itoa(os.Getpid())
	fieldMap["{version}"] = strings.Split(request.Proto, "/")[1]
	hostname, _ := os.Hostname()
	fieldMap["{hostname}"] = hostname
	fieldMap["{req_headers}"] = transToString(request.Header)
	fieldMap["{target}"] = request.URL.Path + request.URL.RawQuery
}

func getNoProxy(protocol string, runtime *RuntimeObject) []string {
	var urls []string
	if runtime.NoProxy != nil && StringValue(runtime.NoProxy) != "" {
		urls = strings.Split(StringValue(runtime.NoProxy), ",")
	} else if rawurl := os.Getenv("NO_PROXY"); rawurl != "" {
		urls = strings.Split(rawurl, ",")
	} else if rawurl := os.Getenv("no_proxy"); rawurl != "" {
		urls = strings.Split(rawurl, ",")
	}

	return urls
}

func ToReader(obj interface{}) io.Reader {
	switch obj.(type) {
	case *string:
		tmp := obj.(*string)
		return strings.NewReader(StringValue(tmp))
	case []byte:
		return strings.NewReader(string(obj.([]byte)))
	case io.Reader:
		return obj.(io.Reader)
	default:
		panic("Invalid Body. Please set a valid Body.")
	}
}

func ToString(val interface{}) string {
	return fmt.Sprintf("%v", val)
}

func getHttpProxy(protocol, host string, runtime *RuntimeObject) (proxy *url.URL, err error) {
	urls := getNoProxy(protocol, runtime)
	for _, url := range urls {
		if url == host {
			return nil, nil
		}
	}
	if protocol == "https" {
		if runtime.HttpsProxy != nil && StringValue(runtime.HttpsProxy) != "" {
			proxy, err = url.Parse(StringValue(runtime.HttpsProxy))
		} else if rawurl := os.Getenv("HTTPS_PROXY"); rawurl != "" {
			proxy, err = url.Parse(rawurl)
		} else if rawurl := os.Getenv("https_proxy"); rawurl != "" {
			proxy, err = url.Parse(rawurl)
		}
	} else {
		if runtime.HttpProxy != nil && StringValue(runtime.HttpProxy) != "" {
			proxy, err = url.Parse(StringValue(runtime.HttpProxy))
		} else if rawurl := os.Getenv("HTTP_PROXY"); rawurl != "" {
			proxy, err = url.Parse(rawurl)
		} else if rawurl := os.Getenv("http_proxy"); rawurl != "" {
			proxy, err = url.Parse(rawurl)
		}
	}

	return proxy, err
}

func getSocks5Proxy(runtime *RuntimeObject) (proxy *url.URL, err error) {
	if runtime.Socks5Proxy != nil && StringValue(runtime.Socks5Proxy) != "" {
		proxy, err = url.Parse(StringValue(runtime.Socks5Proxy))
	}
	return proxy, err
}

func getLocalAddr(localAddr string) (addr *net.TCPAddr) {
	if localAddr != "" {
		addr = &net.TCPAddr{
			IP: []byte(localAddr),
		}
	}
	return addr
}

func setDialContext(runtime *RuntimeObject) func(cxt context.Context, net, addr string) (c net.Conn, err error) {
	return func(ctx context.Context, network, address string) (net.Conn, error) {
		if runtime.LocalAddr != nil && StringValue(runtime.LocalAddr) != "" {
			netAddr := &net.TCPAddr{
				IP: []byte(StringValue(runtime.LocalAddr)),
			}
			return (&net.Dialer{
				Timeout:   time.Duration(IntValue(runtime.ConnectTimeout)) * time.Second,
				DualStack: true,
				LocalAddr: netAddr,
			}).DialContext(ctx, network, address)
		}
		return (&net.Dialer{
			Timeout:   time.Duration(IntValue(runtime.ConnectTimeout)) * time.Second,
			DualStack: true,
		}).DialContext(ctx, network, address)
	}
}

func ToObject(obj interface{}) map[string]interface{} {
	result := make(map[string]interface{})
	byt, _ := json.Marshal(obj)
	err := json.Unmarshal(byt, &result)
	if err != nil {
		return nil
	}
	return result
}

func AllowRetry(retry interface{}, retryTimes *int) *bool {
	if IntValue(retryTimes) == 0 {
		return Bool(true)
	}
	retryMap, ok := retry.(map[string]interface{})
	if !ok {
		return Bool(false)
	}
	retryable, ok := retryMap["retryable"].(bool)
	if !ok || !retryable {
		return Bool(false)
	}

	maxAttempts, ok := retryMap["maxAttempts"].(int)
	if !ok || maxAttempts < IntValue(retryTimes) {
		return Bool(false)
	}
	return Bool(true)
}

func Merge(args ...interface{}) map[string]*string {
	finalArg := make(map[string]*string)
	for _, obj := range args {
		switch obj.(type) {
		case map[string]*string:
			arg := obj.(map[string]*string)
			for key, value := range arg {
				if value != nil {
					finalArg[key] = value
				}
			}
		default:
			byt, _ := json.Marshal(obj)
			arg := make(map[string]string)
			err := json.Unmarshal(byt, &arg)
			if err != nil {
				return finalArg
			}
			for key, value := range arg {
				if value != "" {
					finalArg[key] = String(value)
				}
			}
		}
	}

	return finalArg
}

func isNil(a interface{}) bool {
	defer func() {
		recover()
	}()
	vi := reflect.ValueOf(a)
	return vi.IsNil()
}

func ToMap(args ...interface{}) map[string]interface{} {
	isNotNil := false
	finalArg := make(map[string]interface{})
	for _, obj := range args {
		if obj == nil {
			continue
		}

		if isNil(obj) {
			continue
		}
		isNotNil = true

		switch obj.(type) {
		case map[string]*string:
			arg := obj.(map[string]*string)
			for key, value := range arg {
				if value != nil {
					finalArg[key] = StringValue(value)
				}
			}
		case map[string]interface{}:
			arg := obj.(map[string]interface{})
			for key, value := range arg {
				if value != nil {
					finalArg[key] = value
				}
			}
		case *string:
			str := obj.(*string)
			arg := make(map[string]interface{})
			err := json.Unmarshal([]byte(StringValue(str)), &arg)
			if err == nil {
				for key, value := range arg {
					if value != nil {
						finalArg[key] = value
					}
				}
			}
			tmp := make(map[string]string)
			err = json.Unmarshal([]byte(StringValue(str)), &tmp)
			if err == nil {
				for key, value := range arg {
					if value != "" {
						finalArg[key] = value
					}
				}
			}
		case []byte:
			byt := obj.([]byte)
			arg := make(map[string]interface{})
			err := json.Unmarshal(byt, &arg)
			if err == nil {
				for key, value := range arg {
					if value != nil {
						finalArg[key] = value
					}
				}
				break
			}
		default:
			val := reflect.ValueOf(obj)
			res := structToMap(val)
			for key, value := range res {
				if value != nil {
					finalArg[key] = value
				}
			}
		}
	}

	if !isNotNil {
		return nil
	}
	return finalArg
}

func structToMap(dataValue reflect.Value) map[string]interface{} {
	out := make(map[string]interface{})
	if !dataValue.IsValid() {
		return out
	}
	if dataValue.Kind().String() == "ptr" {
		if dataValue.IsNil() {
			return out
		}
		dataValue = dataValue.Elem()
	}
	if !dataValue.IsValid() {
		return out
	}
	dataType := dataValue.Type()
	if dataType.Kind().String() != "struct" {
		return out
	}
	for i := 0; i < dataType.NumField(); i++ {
		field := dataType.Field(i)
		name, containsNameTag := field.Tag.Lookup("json")
		if !containsNameTag {
			name = field.Name
		} else {
			strs := strings.Split(name, ",")
			name = strs[0]
		}
		fieldValue := dataValue.FieldByName(field.Name)
		if !fieldValue.IsValid() || fieldValue.IsNil() {
			continue
		}
		if field.Type.String() == "io.Reader" || field.Type.String() == "io.Writer" {
			continue
		} else if field.Type.Kind().String() == "struct" {
			out[name] = structToMap(fieldValue)
		} else if field.Type.Kind().String() == "ptr" &&
			field.Type.Elem().Kind().String() == "struct" {
			if fieldValue.Elem().IsValid() {
				out[name] = structToMap(fieldValue)
			}
		} else if field.Type.Kind().String() == "ptr" {
			if fieldValue.IsValid() && !fieldValue.IsNil() {
				out[name] = fieldValue.Elem().Interface()
			}
		} else if field.Type.Kind().String() == "slice" {
			tmp := make([]interface{}, 0)
			num := fieldValue.Len()
			for i := 0; i < num; i++ {
				value := fieldValue.Index(i)
				if !value.IsValid() {
					continue
				}
				if value.Type().Kind().String() == "ptr" &&
					value.Type().Elem().Kind().String() == "struct" {
					if value.IsValid() && !value.IsNil() {
						tmp = append(tmp, structToMap(value))
					}
				} else if value.Type().Kind().String() == "struct" {
					tmp = append(tmp, structToMap(value))
				} else if value.Type().Kind().String() == "ptr" {
					if value.IsValid() && !value.IsNil() {
						tmp = append(tmp, value.Elem().Interface())
					}
				} else {
					tmp = append(tmp, value.Interface())
				}
			}
			if len(tmp) > 0 {
				out[name] = tmp
			}
		} else {
			out[name] = fieldValue.Interface()
		}

	}
	return out
}

func Retryable(err error) *bool {
	if err == nil {
		return Bool(false)
	}
	if realErr, ok := err.(*SDKError); ok {
		if realErr.StatusCode == nil {
			return Bool(false)
		}
		code := IntValue(realErr.StatusCode)
		return Bool(code >= http.StatusInternalServerError)
	}
	return Bool(true)
}

func GetBackoffTime(backoff interface{}, retrytimes *int) *int {
	backoffMap, ok := backoff.(map[string]interface{})
	if !ok {
		return Int(0)
	}
	policy, ok := backoffMap["policy"].(string)
	if !ok || policy == "no" {
		return Int(0)
	}

	period, ok := backoffMap["period"].(int)
	if !ok || period == 0 {
		return Int(0)
	}

	maxTime := math.Pow(2.0, float64(IntValue(retrytimes)))
	return Int(rand.Intn(int(maxTime-1)) * period)
}

func Sleep(backoffTime *int) {
	sleeptime := time.Duration(IntValue(backoffTime)) * time.Second
	time.Sleep(sleeptime)
}

func Validate(params interface{}) error {
	if params == nil {
		return nil
	}
	requestValue := reflect.ValueOf(params)
	if requestValue.IsNil() {
		return nil
	}
	err := validate(requestValue.Elem())
	return err
}

// Verify whether the parameters meet the requirements
func validate(dataValue reflect.Value) error {
	if strings.HasPrefix(dataValue.Type().String(), "*") { // Determines whether the input is a structure object or a pointer object
		if dataValue.IsNil() {
			return nil
		}
		dataValue = dataValue.Elem()
	}
	dataType := dataValue.Type()
	for i := 0; i < dataType.NumField(); i++ {
		field := dataType.Field(i)
		valueField := dataValue.Field(i)
		for _, value := range validateParams {
			err := validateParam(field, valueField, value)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func validateParam(field reflect.StructField, valueField reflect.Value, tagName string) error {
	tag, containsTag := field.Tag.Lookup(tagName) // Take out the checked regular expression
	if containsTag && tagName == "require" {
		err := checkRequire(field, valueField)
		if err != nil {
			return err
		}
	}
	if strings.HasPrefix(field.Type.String(), "[]") { // Verify the parameters of the array type
		err := validateSlice(field, valueField, containsTag, tag, tagName)
		if err != nil {
			return err
		}
	} else if valueField.Kind() == reflect.Ptr { // Determines whether it is a pointer object
		err := validatePtr(field, valueField, containsTag, tag, tagName)
		if err != nil {
			return err
		}
	}
	return nil
}

func validateSlice(field reflect.StructField, valueField reflect.Value, containsregexpTag bool, tag, tagName string) error {
	if valueField.IsValid() && !valueField.IsNil() { // Determines whether the parameter has a value
		if containsregexpTag {
			if tagName == "maxItems" {
				err := checkMaxItems(field, valueField, tag)
				if err != nil {
					return err
				}
			}

			if tagName == "minItems" {
				err := checkMinItems(field, valueField, tag)
				if err != nil {
					return err
				}
			}
		}

		for m := 0; m < valueField.Len(); m++ {
			elementValue := valueField.Index(m)
			if elementValue.Type().Kind() == reflect.Ptr { // Determines whether the child elements of an array are of a basic type
				err := validatePtr(field, elementValue, containsregexpTag, tag, tagName)
				if err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func validatePtr(field reflect.StructField, elementValue reflect.Value, containsregexpTag bool, tag, tagName string) error {
	if elementValue.IsNil() {
		return nil
	}
	if isFilterType(elementValue.Elem().Type().String(), basicTypes) {
		if containsregexpTag {
			if tagName == "pattern" {
				err := checkPattern(field, elementValue.Elem(), tag)
				if err != nil {
					return err
				}
			}

			if tagName == "maxLength" {
				err := checkMaxLength(field, elementValue.Elem(), tag)
				if err != nil {
					return err
				}
			}

			if tagName == "minLength" {
				err := checkMinLength(field, elementValue.Elem(), tag)
				if err != nil {
					return err
				}
			}

			if tagName == "maximum" {
				err := checkMaximum(field, elementValue.Elem(), tag)
				if err != nil {
					return err
				}
			}

			if tagName == "minimum" {
				err := checkMinimum(field, elementValue.Elem(), tag)
				if err != nil {
					return err
				}
			}
		}
	} else {
		err := validate(elementValue)
		if err != nil {
			return err
		}
	}
	return nil
}

func checkRequire(field reflect.StructField, valueField reflect.Value) error {
	name, _ := field.Tag.Lookup("json")
	strs := strings.Split(name, ",")
	name = strs[0]
	if !valueField.IsNil() && valueField.IsValid() {
		return nil
	}
	return errors.New(name + " should be setted")
}

func checkPattern(field reflect.StructField, valueField reflect.Value, tag string) error {
	if valueField.IsValid() && valueField.String() != "" {
		value := valueField.String()
		r, _ := regexp.Compile("^" + tag + "$")
		if match := r.MatchString(value); !match { // Determines whether the parameter value satisfies the regular expression or not, and throws an error
			return errors.New(value + " is not matched " + tag)
		}
	}
	return nil
}

func checkMaxItems(field reflect.StructField, valueField reflect.Value, tag string) error {
	if valueField.IsValid() && valueField.String() != "" {
		maxItems, err := strconv.Atoi(tag)
		if err != nil {
			return err
		}
		length := valueField.Len()
		if maxItems < length {
			errMsg := fmt.Sprintf("The length of %s is %d which is more than %d", field.Name, length, maxItems)
			return errors.New(errMsg)
		}
	}
	return nil
}

func checkMinItems(field reflect.StructField, valueField reflect.Value, tag string) error {
	if valueField.IsValid() {
		minItems, err := strconv.Atoi(tag)
		if err != nil {
			return err
		}
		length := valueField.Len()
		if minItems > length {
			errMsg := fmt.Sprintf("The length of %s is %d which is less than %d", field.Name, length, minItems)
			return errors.New(errMsg)
		}
	}
	return nil
}

func checkMaxLength(field reflect.StructField, valueField reflect.Value, tag string) error {
	if valueField.IsValid() && valueField.String() != "" {
		maxLength, err := strconv.Atoi(tag)
		if err != nil {
			return err
		}
		length := valueField.Len()
		if valueField.Kind().String() == "string" {
			length = strings.Count(valueField.String(), "") - 1
		}
		if maxLength < length {
			errMsg := fmt.Sprintf("The length of %s is %d which is more than %d", field.Name, length, maxLength)
			return errors.New(errMsg)
		}
	}
	return nil
}

func checkMinLength(field reflect.StructField, valueField reflect.Value, tag string) error {
	if valueField.IsValid() {
		minLength, err := strconv.Atoi(tag)
		if err != nil {
			return err
		}
		length := valueField.Len()
		if valueField.Kind().String() == "string" {
			length = strings.Count(valueField.String(), "") - 1
		}
		if minLength > length {
			errMsg := fmt.Sprintf("The length of %s is %d which is less than %d", field.Name, length, minLength)
			return errors.New(errMsg)
		}
	}
	return nil
}

func checkMaximum(field reflect.StructField, valueField reflect.Value, tag string) error {
	if valueField.IsValid() && valueField.String() != "" {
		maximum, err := strconv.ParseFloat(tag, 64)
		if err != nil {
			return err
		}
		byt, _ := json.Marshal(valueField.Interface())
		num, err := strconv.ParseFloat(string(byt), 64)
		if err != nil {
			return err
		}
		if maximum < num {
			errMsg := fmt.Sprintf("The size of %s is %f which is greater than %f", field.Name, num, maximum)
			return errors.New(errMsg)
		}
	}
	return nil
}

func checkMinimum(field reflect.StructField, valueField reflect.Value, tag string) error {
	if valueField.IsValid() && valueField.String() != "" {
		minimum, err := strconv.ParseFloat(tag, 64)
		if err != nil {
			return err
		}

		byt, _ := json.Marshal(valueField.Interface())
		num, err := strconv.ParseFloat(string(byt), 64)
		if err != nil {
			return err
		}
		if minimum > num {
			errMsg := fmt.Sprintf("The size of %s is %f which is less than %f", field.Name, num, minimum)
			return errors.New(errMsg)
		}
	}
	return nil
}

// Determines whether realType is in filterTypes
func isFilterType(realType string, filterTypes []string) bool {
	for _, value := range filterTypes {
		if value == realType {
			return true
		}
	}
	return false
}

func TransInterfaceToBool(val interface{}) *bool {
	if val == nil {
		return nil
	}

	return Bool(val.(bool))
}

func TransInterfaceToInt(val interface{}) *int {
	if val == nil {
		return nil
	}

	return Int(val.(int))
}

func TransInterfaceToString(val interface{}) *string {
	if val == nil {
		return nil
	}

	return String(val.(string))
}

func Prettify(i interface{}) string {
	resp, _ := json.MarshalIndent(i, "", "   ")
	return string(resp)
}

func ToInt(a *int32) *int {
	return Int(int(Int32Value(a)))
}

func ToInt32(a *int) *int32 {
	return Int32(int32(IntValue(a)))
}
