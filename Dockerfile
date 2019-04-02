FROM golang:alpine

RUN apk update; apk add curl netcat-openbsd

COPY . /root

WORKDIR /root
RUN GO111MODULE=on; go build -o /usr/local/bin/cell-controller-manager -mod=vendor ./cmd/controller-manager/main.go
RUN GO111MODULE=on; go build -o /usr/local/bin/cell-discovery          -mod=vendor ./cmd/discovery/main.go
RUN GO111MODULE=on; go build -o /usr/local/bin/cell-scheduler          -mod=vendor ./cmd/scheduler/main.go

RUN mv etcdctl /usr/local/bin
