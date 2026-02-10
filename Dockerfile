# 构建阶段
FROM golang:1.24-alpine AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -a -ldflags '-extldflags "-static"' -o ./bin/curetmdbanime ./main.go

FROM alpine:latest

WORKDIR /opt/curetmdbanime/

COPY --from=builder /app/bin/curetmdbanime .

VOLUME /opt/data/
EXPOSE 8632
ENTRYPOINT ["/opt/curetmdbanime/curetmdbanime"]
