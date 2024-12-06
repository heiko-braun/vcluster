
FROM golang:1.23.4

WORKDIR /go/src/github.com/samber/lo

COPY Makefile go.* ./

RUN make tools
