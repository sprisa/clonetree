ARG GO_VERSION=1.24
FROM golang:${GO_VERSION}-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOWORK=off go build -trimpath -o /out/clonetree . \
    && CGO_ENABLED=0 GOWORK=off go test -c -o /out/clonetree-e2e.test ./e2e

FROM ubuntu:24.04
RUN apt-get update \
    && DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends \
       xfsprogs btrfs-progs util-linux e2fsprogs \
    && rm -rf /var/lib/apt/lists/*
COPY --from=build /out/clonetree /out/clonetree-e2e.test /usr/local/bin/
COPY e2e/linux.sh /usr/local/bin/clonetree-linux-e2e
ENTRYPOINT ["bash", "/usr/local/bin/clonetree-linux-e2e"]
