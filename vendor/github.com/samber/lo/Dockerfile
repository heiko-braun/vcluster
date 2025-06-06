
FROM golang:1.24.4

WORKDIR /go/src/github.com/samber/lo

COPY Makefile go.* ./

RUN make tools
