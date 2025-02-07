
FROM golang:1.24rc2

WORKDIR /go/src/github.com/samber/lo

COPY Makefile go.* ./

RUN make tools
