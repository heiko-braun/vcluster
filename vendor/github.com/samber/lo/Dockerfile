
FROM golang:1.25.1

WORKDIR /go/src/github.com/samber/lo

COPY Makefile go.* ./

RUN make tools
