package driver

import (
	"context"
	"io"
	"log"
	"path/filepath"
	"strings"
	"testing"

	"github.com/liuzhaojun/print-portal/internal/cups"
	"github.com/liuzhaojun/print-portal/internal/storage"
)

type fakeRunner struct{ output string }

func (f fakeRunner) Run(_ context.Context, name string, args ...string) (string, string, error) {
	if name == "lpinfo" && strings.Contains(strings.Join(args, " "), "-m") {
		return f.output, "", nil
	}
	return "", "", nil
}

func newStore(t *testing.T) (*storage.Store, string) {
	t.Helper()
	dir := t.TempDir()
	store, err := storage.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Migrate(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	return store, dir
}

func TestRefreshBuiltinDrivers(t *testing.T) {
	store, dir := newStore(t)
	runner := fakeRunner{output: "drv:///brlaser.drv/br7060d.ppd Brother DCP-7060D\nfoomatic:HP.ppd HP LaserJet\n"}
	svc := NewService(store, cups.NewClient(runner, ""), dir)
	if err := svc.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	drivers, err := store.ListDrivers()
	if err != nil {
		t.Fatal(err)
	}
	found := map[string]string{}
	for _, d := range drivers {
		found[d.Name] = d.State
	}
	if found["brlaser"] != "ready" {
		t.Errorf("brlaser state = %q, want ready", found["brlaser"])
	}
	if found["foomatic"] != "degraded" {
		t.Errorf("foomatic state = %q, want degraded (index only)", found["foomatic"])
	}
}

func TestInstallPPD(t *testing.T) {
	store, dir := newStore(t)
	svc := NewService(store, cups.NewClient(fakeRunner{}, ""), dir)
	ppd := "*PPD-Adobe: \"4.3\"\n*cupsFilter: \"application/vnd.cups-raster 0 -\"\n"
	d, err := svc.InstallPPD("My Printer", []byte(ppd))
	if err != nil {
		t.Fatal(err)
	}
	if d.Kind != "ppd" || d.State != "ready" {
		t.Errorf("driver = %+v", d)
	}
	if _, err := svc.InstallPPD("bad.ppd", []byte("not a ppd")); err == nil {
		t.Error("expected invalid PPD error")
	}
}

func TestUninstallProtection(t *testing.T) {
	store, dir := newStore(t)
	svc := NewService(store, cups.NewClient(fakeRunner{}, ""), dir)
	ppd := "*PPD-Adobe: \"4.3\"\n"
	d, err := svc.InstallPPD("owned", []byte(ppd))
	if err != nil {
		t.Fatal(err)
	}
	p := &storage.Printer{Name: "P", PPDQueue: "p-ppd", RAWQueue: "p-raw", DeviceURI: "usb://x", PPDFile: filepath.Join(d.PPDDir, "owned.ppd")}
	if err := store.CreatePrinter(p); err != nil {
		t.Fatal(err)
	}
	if err := svc.Uninstall(context.Background(), d.ID); err == nil {
		t.Error("expected in-use uninstall to fail")
	}
	_ = log.New(io.Discard, "", 0)
}
