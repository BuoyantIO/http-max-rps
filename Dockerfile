FROM golang:1.8.0-alpine

WORKDIR /go/src/http-max-rps

ADD . /go/src/github.com/buoyantio/http-max-rps

RUN go build -o /go/bin/http-max-rps /go/src/github.com/buoyantio/http-max-rps/main.go

ENTRYPOINT ["/go/bin/http-max-rps"]
