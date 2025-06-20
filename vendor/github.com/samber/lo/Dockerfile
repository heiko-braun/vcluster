
FROM golang:1.25rc1

WORKDIR /go/src/github.com/samber/lo

COPY Makefile go.* ./

RUN make tools
