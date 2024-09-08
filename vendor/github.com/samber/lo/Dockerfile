
FROM golang:1.23.0

WORKDIR /go/src/github.com/samber/lo

COPY Makefile go.* ./

RUN make tools
