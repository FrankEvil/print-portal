#!/bin/sh
set -eu

# 说明：这是 P1 部署草案，CUPS 的 printers.conf/cupsd.conf 初始化将在实现阶段补齐。
mkdir -p /etc/cups /var/spool/cups /var/log/cups /app/data /opt/print-portal/web

if [ ! -f /etc/cups/cupsd.conf ]; then
  cp /usr/share/cups/cupsd.conf.default /etc/cups/cupsd.conf 2>/dev/null || true
fi

# CUPS 在前台启动；实际镜像应改为 s6-overlay 或 supervisord 管理。
cupsd -f &
CUPS_PID=$!

cleanup() {
  kill "$CUPS_PID" 2>/dev/null || true
  exit 0
}
trap cleanup TERM INT

# 等待 CUPS 就绪（后续实现中会加入健康检查）
sleep 2

exec /usr/local/bin/printd
