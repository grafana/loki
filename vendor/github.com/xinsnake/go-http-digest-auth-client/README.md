# go-http-digest-auth-client
Golang Http Digest Authentication Client

This client implements [RFC7616 HTTP Digest Access Authentication](https://www.rfc-editor.org/rfc/rfc7616.txt)
and by now the basic features should work.

# Usage

```go
// import
import dac "github.com/xinsnake/go-http-digest-auth-client"

// create a new digest authentication request
dr := dac.NewRequest(username, password, method, uri, payload)
response1, err := dr.Execute()

// check error, get response

// reuse the existing digest authentication request so no extra request is needed
dr.UpdateRequest(username, password, method, uri, payload)
response2, err := dr.Execute()

// check error, get response
```

Or you can use it with `http.Request`

```go
t := dac.NewTransport(username, password)
req, err := http.NewRequest(method, uri, payload)

if err != nil {
    log.Fatalln(err)
}

resp, err := t.RoundTrip(req)
if err != nil {
    log.Fatalln(err)
}
defer resp.Body.Close()

fmt.Println(resp)
```
