FROM golang:1.25

WORKDIR /go/src/github.com/buger/jsonparser
ADD . /go/src/github.com/buger/jsonparser

RUN go get github.com/Jeffail/gabs
RUN go get github.com/bitly/go-simplejson
RUN go get github.com/pquerna/ffjson
RUN go get github.com/antonholmquist/jason
RUN go get github.com/mreiferson/go-ujson
RUN go get -tags=unsafe -u github.com/ugorji/go/codec
RUN go get github.com/mailru/easyjson
