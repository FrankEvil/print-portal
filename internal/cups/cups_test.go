package cups

import (
	"context"
	"strings"
	"testing"
)

type fakeRunner struct {
	calls   [][]string
	stdout  map[string]string
	stderr  string
	failKey string
}

func (f *fakeRunner) Run(_ context.Context, name string, args ...string) (string, string, error) {
	f.calls = append(f.calls, append([]string{name}, args...))
	key := name
	if f.failKey != "" && key == f.failKey {
		return "", f.stderr, context.DeadlineExceeded
	}
	return f.stdout[key], "", nil
}

func TestParsePrinterStatuses(t *testing.T) {
	out := `printer DCP7060 is idle.  enabled since Wed 05 Nov 2026 12:00:00 AM CST
printer RAW is printing DCP7060-42.  enabled since Wed 05 Nov 2026 12:00:00 AM CST
printer OFF disabled since Wed 05 Nov 2026 12:00:00 AM CST -`
	got := parsePrinterStatuses(out)
	if len(got) != 3 {
		t.Fatalf("expected 3 printers, got %d", len(got))
	}
	if got["DCP7060"].State != "idle" {
		t.Errorf("DCP7060 state = %q, want idle", got["DCP7060"].State)
	}
	if got["RAW"].State != "printing" || got["RAW"].PrintingJob != "DCP7060-42" {
		t.Errorf("RAW = %+v, want printing DCP7060-42", got["RAW"])
	}
	if got["OFF"].State != "stopped" {
		t.Errorf("OFF state = %q, want stopped", got["OFF"].State)
	}
}

func TestParseJobLines(t *testing.T) {
	out := `DCP7060-123 alice 1024 Wed 05 Nov 2026 12:00:00 AM CST
DCP7060-124 bob 2048 Wed 05 Nov 2026 12:01:00 AM CST`
	jobs := parseJobLines(out)
	if len(jobs) != 2 {
		t.Fatalf("expected 2 jobs, got %d", len(jobs))
	}
	if jobs[0].ID != "DCP7060-123" || jobs[0].Queue != "DCP7060" || jobs[0].User != "alice" || jobs[0].Size != 1024 {
		t.Errorf("unexpected job: %+v", jobs[0])
	}
}

func TestParsePPDOptions(t *testing.T) {
	out := `PageSize/Page Size: A4 *Letter A5
Duplex/Two-Sided: None *DuplexNoTumble DuplexTumble`
	opts := parsePPDOptions(out)
	if len(opts) != 2 {
		t.Fatalf("expected 2 options, got %d", len(opts))
	}
	if opts[0].Keyword != "PageSize" || opts[0].Default != "Letter" {
		t.Errorf("unexpected option: %+v", opts[0])
	}
	if len(opts[0].Choices) != 3 {
		t.Errorf("expected 3 choices, got %v", opts[0].Choices)
	}
}

func TestParseModelLines(t *testing.T) {
	out := `drv:///brlaser.drv/br7060d.ppd Brother DCP-7060D
foomatic:HP-LaserJet_1020.ppd HP LaserJet 1020
drv:///gutenprint.5.3://escp2-ecomode/epson.ppd Epson Stylus
Library/Printers/PPDs/Contents/Resources/Brother DCP-7060D CUPS.gz Brother DCP-7060D CUPS`
	models := parseModelLines(out)
	if len(models) != 4 {
		t.Fatalf("expected 4 models, got %d", len(models))
	}
	if models[0].Driver != "brlaser" {
		t.Errorf("driver = %q, want brlaser", models[0].Driver)
	}
	if models[1].Driver != "foomatic" {
		t.Errorf("driver = %q, want foomatic", models[1].Driver)
	}
	if models[2].Driver != "gutenprint" {
		t.Errorf("driver = %q, want gutenprint", models[2].Driver)
	}
	if models[3].Name != "Library/Printers/PPDs/Contents/Resources/Brother DCP-7060D CUPS.gz" {
		t.Errorf("name = %q", models[3].Name)
	}
	if models[3].Description != "Brother DCP-7060D" {
		t.Errorf("description = %q", models[3].Description)
	}
	if models[3].Brand != "Brother" {
		t.Errorf("brand = %q", models[3].Brand)
	}
}

func TestSubmitParsesRequestID(t *testing.T) {
	runner := &fakeRunner{stdout: map[string]string{"lp": "request id is DCP7060-7 (1 file(s))"}}
	client := NewClient(runner, "127.0.0.1:631")
	id, err := client.Submit(context.Background(), "DCP7060", "/tmp/a.pdf", PrintOptions{
		PageRange: "1-2", Duplex: "two-sided-long-edge", Copies: 2, PaperSize: "A4", Orientation: "portrait", ColorMode: "monochrome",
	})
	if err != nil {
		t.Fatal(err)
	}
	if id != "DCP7060-7" {
		t.Errorf("id = %q, want DCP7060-7", id)
	}
	args := strings.Join(runner.calls[0], " ")
	for _, want := range []string{"-d DCP7060", "page-ranges=1-2", "sides=two-sided-long-edge", "-n 2"} {
		if !strings.Contains(args, want) {
			t.Errorf("lp args %q missing %q", args, want)
		}
	}
}

func TestParseDeviceLines(t *testing.T) {
	out := `direct usb://Brother/DCP-7060D?serial=E69807B3J831059
network ipp://192.168.2.50/ipp/print
network ipps://printer.local/ipp/print`
	devices := parseDeviceLines(out)
	if len(devices) != 3 {
		t.Fatalf("expected 3 devices, got %d", len(devices))
	}
	if devices[0].Name != "Brother/DCP-7060D" {
		t.Errorf("name = %q", devices[0].Name)
	}
	if devices[1].Transport != "network" {
		t.Errorf("transport = %q", devices[1].Transport)
	}
}

func TestParseScannerLines(t *testing.T) {
	out := "device `brother4:bus2;dev1' is a Brother DCP-7060D USB scanner"
	scanners := parseScannerLines(out)
	if len(scanners) != 1 || !strings.Contains(scanners[0].Name, "Brother") {
		t.Errorf("unexpected scanners: %+v", scanners)
	}
}
