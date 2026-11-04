# syntax=docker/dockerfile:1
# -----------------------------------------------------------------------------
# Print Portal 一体化镜像
# 包含：Vue 静态资源 + Go 服务 + CUPS + 打印驱动 + 文档转换工具
# -----------------------------------------------------------------------------

FROM node:22-alpine AS web
WORKDIR /src/web
COPY web/package.json web/pnpm-lock.yaml* ./
RUN corepack enable && (pnpm install --frozen-lockfile || pnpm install)
COPY web/ ./
RUN pnpm build

FROM golang:1.23-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/printd ./cmd/printd

FROM debian:bookworm-slim
ENV DEBIAN_FRONTEND=noninteractive
ENV TZ=Asia/Shanghai
RUN apt-get update && apt-get install -y --no-install-recommends \
    cups \
    cups-client \
    cups-filters \
    cups-ipp-utils \
    printer-driver-brlaser \
    foomatic-db \
    foomatic-db-compressed-ppds \
    foomatic-db-engine \
    imagemagick \
    img2pdf \
    ghostscript \
    poppler-utils \
    libreoffice-core-nogui \
    libreoffice-writer-nogui \
    libreoffice-calc-nogui \
    libreoffice-impress-nogui \
    sane-utils \
    sane-airscan \
    avahi-utils \
    ca-certificates \
    tzdata \
    curl \
    && rm -rf /var/lib/apt/lists/*

RUN mkdir -p /opt/print-portal/web /app/data /etc/cups /var/spool/cups /var/log/cups
COPY --from=build /out/printd /usr/local/bin/printd
COPY --from=web /src/web/dist /opt/print-portal/web
COPY deployments/entrypoint.sh /entrypoint.sh
RUN chmod +x /entrypoint.sh

ENV PRINTD_ADDR=:8088
ENV CUPS_SERVER=127.0.0.1:8631
ENV PRINT_PORTAL_STATIC=/opt/print-portal/web
ENV PRINT_PORTAL_DATA=/app/data

EXPOSE 8088 8631
ENTRYPOINT ["/entrypoint.sh"]
