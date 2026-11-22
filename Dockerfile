# syntax=docker/dockerfile:1
# 官方镜像与 Release 复用
FROM alpine:latest

ARG TARGETARCH

WORKDIR /opt/curetmdbanime/

COPY --chmod=0755 bin/curetmdbanime-linux-${TARGETARCH} ./curetmdbanime

VOLUME /opt/data/
EXPOSE 8632
ENTRYPOINT ["/opt/curetmdbanime/curetmdbanime"]
