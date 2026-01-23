
FROM golang:1.26rc2

WORKDIR /go/src/github.com/samber/lo

COPY Makefile go.* ./

RUN make tools
