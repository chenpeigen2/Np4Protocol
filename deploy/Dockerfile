# syntax=docker/dockerfile:1

# Build stage: static binary (libp2p is pure Go — CGO not needed).
# GOPROXY is a build-arg so servers behind restricted networks can pass
# --build-arg GOPROXY=https://goproxy.cn,direct
FROM golang:1.26-alpine AS build
ARG GOPROXY=https://proxy.golang.org,direct
ENV GOPROXY=${GOPROXY}
WORKDIR /src
COPY go/go.mod go/go.sum ./
RUN go mod download
COPY go/ ./
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/np4bootstrap ./cmd/bootstrap/

# Runtime: alpine keeps a shell for debugging; busybox wget serves the
# container-internal healthcheck. The identity MUST live on a volume —
# losing /data/boot.id changes the peer ID and every client's address.
FROM alpine:3.20
COPY --from=build /out/np4bootstrap /usr/local/bin/np4bootstrap
VOLUME ["/data"]
EXPOSE 4000 8080
ENTRYPOINT ["/usr/local/bin/np4bootstrap"]
CMD ["start", "--port", "4000", "--identity", "/data/boot.id", "--web", "8080"]
