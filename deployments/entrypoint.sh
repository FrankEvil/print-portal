#!/bin/sh
set -eu

# ---------------------------------------------------------------------------
# Print Portal container entrypoint: CUPS (8631) + printd (8088)
# ---------------------------------------------------------------------------
mkdir -p /app/data /etc/cups /var/spool/cups /var/log/cups /run/cups /opt/print-portal/web

# Generate a minimal cupsd.conf on first run. The file lives on the
# cups-etc volume, so user changes survive container restarts.
if [ ! -f /etc/cups/cupsd.conf ]; then
  cat > /etc/cups/cupsd.conf <<'EOF'
LogLevel warn
MaxJobs 500
Listen 0.0.0.0:8631
ServerName print-portal
WebInterface No
Browsing Off
DefaultShared Yes

<Location />
  Order allow,deny
  Allow all
</Location>

<Location /admin>
  Order allow,deny
  Allow localhost
</Location>

<Location /admin/conf>
  Order allow,deny
  Allow localhost
</Location>
EOF
fi

# Start CUPS in the foreground as a child process.
cupsd -f &
CUPS_PID=$!

cleanup() {
  kill "$CUPS_PID" 2>/dev/null || true
  exit 0
}
trap cleanup TERM INT

# Wait for the scheduler before starting the web service.
i=0
while [ "$i" -lt 60 ]; do
  if lpstat -r >/dev/null 2>&1; then
    break
  fi
  i=$((i + 1))
  sleep 0.5
done

exec /usr/local/bin/printd
