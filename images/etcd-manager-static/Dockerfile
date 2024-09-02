FROM golang:1.22 AS builder

RUN mkdir /workspace
WORKDIR /workspace
COPY go.mod go.mod
COPY go.sum go.sum
RUN go mod download
RUN CGO_ENABLED=0 go build k8s.io/klog/v2 go.etcd.io/etcd/client/v3

COPY cmd cmd
COPY pkg pkg


RUN CGO_ENABLED=0 go build -v -o /etcd-manager ./cmd/etcd-manager

#FROM scratch
# We at least need debian12 for sh, becaue we currently launch with a shell for log files
#FROM gcr.io/distroless/static-debian12:latest
FROM debian:12

COPY --from=builder /etcd-manager /etcd-manager

ENTRYPOINT [ "/etcd-manager" ]