# 构建阶段
FROM --platform=$BUILDPLATFORM golang:1.24-alpine AS builder

WORKDIR /app

ARG TARGETARCH
ARG TARGETOS
ARG VERSION

COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 \
    GOOS=$TARGETOS \
    GOARCH=$TARGETARCH \
    go build -ldflags " \
        -s -w \
        -X curetmdbanime/internal/config.Version=${VERSION:-dev}" \
    -o ./bin/curetmdbanime \
    .

FROM alpine:latest

WORKDIR /opt/curetmdbanime/

COPY --from=builder /app/bin/curetmdbanime .

VOLUME /opt/data/
EXPOSE 8632
ENTRYPOINT ["/opt/curetmdbanime/curetmdbanime"]
