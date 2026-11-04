#!/bin/sh
# End-to-end integration test for Print Portal.
# Runs the real server binary against fake CUPS command line tools.
set -eu

cd "$(dirname "$0")/.."
ROOT="$(pwd)"
WORK="$(mktemp -d "${TMPDIR:-/tmp}/print-portal-it.XXXXXX")"
trap 'kill "$SERVER_PID" 2>/dev/null || true; rm -rf "$WORK"' EXIT

GO="$ROOT/.tools/go/bin/go"
if [ ! -x "$GO" ]; then
  echo "go toolchain not found at $GO" >&2
  exit 1
fi

echo "== building printd =="
PATH="$ROOT/.tools/go/bin:$PATH" GOPATH="$ROOT/.tools/gopath" GOCACHE="$ROOT/.tools/gocache" GOFLAGS=-mod=mod \
  "$GO" build -o "$WORK/printd" ./cmd/printd

echo "== creating fake CUPS tools =="
FAKE="$WORK/bin"
mkdir -p "$FAKE" "$WORK/state"
cat > "$FAKE/lp" <<'EOF'
#!/bin/sh
STATE="${FAKE_CUPS_STATE:-/tmp}"
n=$(cat "$STATE/counter" 2>/dev/null || echo 0)
n=$((n + 1))
echo "$n" > "$STATE/counter"
echo "request id is dcp-ppd-$n (1 file(s))"
EOF
cat > "$FAKE/lpstat" <<'EOF'
#!/bin/sh
args="$*"
case "$args" in
  *" -r"*) echo "scheduler is running" ;;
  *" -p"*) echo "printer dcp-ppd is idle.  enabled since Wed 05 Nov 2026 12:00:00 AM CST" ;;
  *" -v"*) echo "device for dcp-ppd: usb://Brother/DCP-7060D?serial=TEST" ;;
  *) exit 0 ;;
esac
EOF
cat > "$FAKE/lpadmin" <<'EOF'
#!/bin/sh
exit 0
EOF
cat > "$FAKE/lpoptions" <<'EOF'
#!/bin/sh
case "$*" in
  *" -l"*) printf 'PageSize/Page Size: A4 *Letter A5\nDuplex/Two-Sided: None *DuplexNoTumble DuplexTumble\n' ;;
esac
exit 0
EOF
cat > "$FAKE/lpinfo" <<'EOF'
#!/bin/sh
case "$*" in
  *" -m"*) echo "drv:///brlaser.drv/br7060d.ppd Brother DCP-7060D" ;;
  *" -v"*) echo "direct usb://Brother/DCP-7060D?serial=TEST" ;;
  *) exit 0 ;;
esac
EOF
cat > "$FAKE/cups-config" <<'EOF'
#!/bin/sh
echo "2.4.2"
EOF
cat > "$FAKE/cupsctl" <<'EOF'
#!/bin/sh
exit 0
EOF
cat > "$FAKE/scanimage" <<'EOF'
#!/bin/sh
echo "no scanners" >&2
exit 1
EOF
for tool in cupsenable cupsdisable cupsaccept cupsreject cancel pkill; do
  printf '#!/bin/sh\nexit 0\n' > "$FAKE/$tool"
done
chmod +x "$FAKE"/*

echo "== starting printd =="
PATH="$FAKE:$PATH" \
FAKE_CUPS_STATE="$WORK/state" \
PRINTD_ADDR=127.0.0.1:18088 \
PRINT_PORTAL_DATA="$WORK/data" \
ADMIN_TOKEN=testtoken \
CUPS_SERVER=127.0.0.1:631 \
"$WORK/printd" > "$WORK/server.log" 2>&1 &
SERVER_PID=$!

for i in $(seq 1 50); do
  if curl -fsS "http://127.0.0.1:18088/healthz" >/dev/null 2>&1; then break; fi
  sleep 0.2
done

fail() { echo "FAIL: $*" >&2; echo "--- server log ---" >&2; tail -50 "$WORK/server.log" >&2; exit 1; }

echo "== health =="
curl -fsS "http://127.0.0.1:18088/healthz" | grep -q '"status":"ok"' || fail "health"

echo "== static frontend =="
curl -fsS "http://127.0.0.1:18088/" | grep -qi "<div id=\"app\"" || fail "frontend index"

echo "== create printer =="
curl -fsS -X POST "http://127.0.0.1:18088/api/admin/printers" \
  -H "X-Admin-Token: testtoken" -H "Content-Type: application/json" \
  -d '{"name":"Brother DCP-7060D","model":"brlaser","ppd_queue":"dcp-ppd","raw_queue":"dcp-raw","device_uri":"usb://Brother/DCP-7060D?serial=TEST","is_default":true,"is_shared":true}' \
  | grep -q '"name":"Brother DCP-7060D"' || fail "create printer"

echo "== list printers =="
curl -fsS "http://127.0.0.1:18088/api/printers" | grep -q '"queue":"dcp-ppd"' || fail "list printers"

echo "== public ppd options =="
curl -fsS "http://127.0.0.1:18088/api/printers/1/ppd-options" | grep -q 'PageSize' || fail "public ppd options"

echo "== submit print job =="
printf '%%PDF-1.4\n1 0 obj\n<<>>\nendobj\ntrailer\n<<>>\n%%%%EOF\n' > "$WORK/test.pdf"
RESP=$(curl -fsS -X POST "http://127.0.0.1:18088/api/print" \
  -F "file=@$WORK/test.pdf;type=application/pdf" \
  -F "printer_id=1" -F "copies=2" -F "duplex=two-sided-long-edge")
echo "$RESP"
JOB_ID=$(echo "$RESP" | sed -n 's/.*"job_id":\([0-9]*\).*/\1/p')
[ -n "$JOB_ID" ] || fail "print response has no job_id"

echo "== wait for background worker =="
STATUS=""
for i in $(seq 1 50); do
  STATUS=$(curl -fsS "http://127.0.0.1:18088/api/jobs/$JOB_ID" | sed -n 's/.*"status":"\([a-z]*\)".*/\1/p')
  [ "$STATUS" = "submitted" ] && break
  sleep 0.2
done
[ "$STATUS" = "submitted" ] || fail "job status = $STATUS, want submitted"

echo "== job detail fields =="
DETAIL=$(curl -fsS "http://127.0.0.1:18088/api/jobs/$JOB_ID")
echo "$DETAIL" | grep -q '"copies":2' || fail "copies"
echo "$DETAIL" | grep -q '"printer_name":"Brother DCP-7060D"' || fail "printer_name"

echo "== drivers =="
curl -fsS "http://127.0.0.1:18088/api/admin/drivers" -H "X-Admin-Token: testtoken" | grep -q '"name":"brlaser"' || fail "drivers"

echo "== test page =="
curl -fsS -X POST "http://127.0.0.1:18088/api/admin/printers/Brother%20DCP-7060D/test" -H "X-Admin-Token: testtoken" | grep -q '"cups_job_id"' || fail "test page"

echo "== overview =="
curl -fsS "http://127.0.0.1:18088/api/admin/overview" -H "X-Admin-Token: testtoken" | grep -q '"printers":1' || fail "overview"

echo "== cups status =="
curl -fsS "http://127.0.0.1:18088/api/admin/cups/status" -H "X-Admin-Token: testtoken" | grep -q '"state":"running"' || fail "cups status"

echo "== settings roundtrip =="
curl -fsS -X PUT "http://127.0.0.1:18088/api/admin/settings" -H "X-Admin-Token: testtoken" -H "Content-Type: application/json" -d '{"max_upload_mb":"50"}' >/dev/null
curl -fsS "http://127.0.0.1:18088/api/admin/settings" -H "X-Admin-Token: testtoken" | grep -q '"max_upload_mb":"50"' || fail "settings"

echo "== driver models =="
curl -fsS "http://127.0.0.1:18088/api/admin/drivers/models?q=DCP" -H "X-Admin-Token: testtoken" | grep -q 'DCP-7060D' || fail "driver models"

echo "== discovery =="
curl -fsS "http://127.0.0.1:18088/api/admin/discovery/printers" -H "X-Admin-Token: testtoken" | grep -q 'usb://Brother/DCP-7060D' || fail "discovery"

echo "== cancel and retry =="
RESP2=$(curl -fsS -X POST "http://127.0.0.1:18088/api/print" \
  -F "file=@$WORK/test.pdf;type=application/pdf" -F "printer_id=1")
JOB2=$(echo "$RESP2" | sed -n 's/.*"job_id":\([0-9]*\).*/\1/p')
STATUS2=""
for i in $(seq 1 50); do
  STATUS2=$(curl -fsS "http://127.0.0.1:18088/api/jobs/$JOB2" | sed -n 's/.*"status":"\([a-z]*\)".*/\1/p')
  [ "$STATUS2" = "submitted" ] && break
  sleep 0.2
done
curl -fsS -X DELETE "http://127.0.0.1:18088/api/jobs/$JOB2" | grep -q '"status":"cancelled"' || fail "cancel"
RETRY=$(curl -fsS -X POST "http://127.0.0.1:18088/api/jobs/$JOB2/retry")
NEW_ID=$(echo "$RETRY" | sed -n 's/.*"job_id":\([0-9]*\).*/\1/p')
[ -n "$NEW_ID" ] && [ "$NEW_ID" -gt "$JOB2" ] || fail "retry"

echo "== admin jobs list =="
curl -fsS "http://127.0.0.1:18088/api/admin/jobs" -H "X-Admin-Token: testtoken" | grep -q '"jobs"' || fail "admin jobs"

echo "== app logs =="
curl -fsS "http://127.0.0.1:18088/api/admin/logs" -H "X-Admin-Token: testtoken" | grep -q '"lines"' || fail "app logs"

echo "== unauthorized admin =="
CODE=$(curl -s -o /dev/null -w '%{http_code}' "http://127.0.0.1:18088/api/admin/overview")
[ "$CODE" = "401" ] || fail "admin without token = $CODE"

echo "PASS: integration test"
