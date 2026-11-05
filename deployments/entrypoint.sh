#!/bin/sh
set -eu

# ---------------------------------------------------------------------------
# Print Portal container entrypoint: CUPS (127.0.0.1:631) + IPP gateway (8631)
# ---------------------------------------------------------------------------
mkdir -p /app/data /etc/cups /var/spool/cups /var/log/cups /run/cups /run/dbus /run/avahi-daemon /opt/print-portal/web

# Best-effort mDNS stack for the "allow discovery" switch.
dbus-daemon --system --fork >/dev/null 2>&1 || true
avahi-daemon --daemonize --no-drop-root >/dev/null 2>&1 || true

# Keep an existing cupsd.conf compatible with the IPP gateway split.
if [ -f /etc/cups/cupsd.conf ]; then
  sed -i 's/^Listen 0\.0\.0\.0:8631$/Listen 127.0.0.1:631/' /etc/cups/cupsd.conf
  sed -i 's/^Browsing Off$/Browsing On/' /etc/cups/cupsd.conf
fi

# Generate a minimal cupsd.conf on first run. The file lives on the
# cups-etc volume, so user changes survive container restarts.
if [ ! -f /etc/cups/cupsd.conf ]; then
  cat > /etc/cups/cupsd.conf <<'EOF'
LogLevel warn
MaxJobs 500
Listen 127.0.0.1:631
ServerName print-portal
WebInterface No
Browsing On
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
