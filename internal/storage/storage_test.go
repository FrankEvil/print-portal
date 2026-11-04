package storage

import (
	"path/filepath"
	"testing"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	store, err := Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Migrate(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	return store
}

func TestPrinterCRUD(t *testing.T) {
	store := newTestStore(t)
	p := &Printer{Name: "DCP", Model: "brlaser", PPDQueue: "dcp-ppd", RAWQueue: "dcp-raw", DeviceURI: "usb://x", IsDefault: true, IsShared: true}
	if err := store.CreatePrinter(p); err != nil {
		t.Fatal(err)
	}
	if p.ID == 0 {
		t.Fatal("expected id")
	}
	got, err := store.GetPrinterByName("DCP")
	if err != nil || got == nil {
		t.Fatalf("get printer: %v %v", got, err)
	}
	if !got.IsDefault {
		t.Error("expected default true")
	}
	got.Model = "updated"
	if err := store.UpdatePrinter(got); err != nil {
		t.Fatal(err)
	}
	got2, _ := store.GetPrinter(p.ID)
	if got2.Model != "updated" {
		t.Errorf("model = %q", got2.Model)
	}
	if err := store.DeletePrinter(p.ID); err != nil {
		t.Fatal(err)
	}
	got3, _ := store.GetPrinter(p.ID)
	if got3 != nil {
		t.Error("expected deleted")
	}
}

func TestJobLifecycle(t *testing.T) {
	store := newTestStore(t)
	p := &Printer{Name: "DCP", PPDQueue: "dcp-ppd", RAWQueue: "dcp-raw", DeviceURI: "usb://x"}
	if err := store.CreatePrinter(p); err != nil {
		t.Fatal(err)
	}
	j := &Job{PrinterID: p.ID, Source: "web", Filename: "a.pdf", Status: "pending", StoredPath: "/tmp/a.pdf"}
	if err := store.CreateJob(j); err != nil {
		t.Fatal(err)
	}
	claimed, err := store.ClaimNextPending()
	if err != nil || claimed == nil {
		t.Fatalf("claim: %v %v", claimed, err)
	}
	if claimed.Status != "converting" {
		t.Errorf("status = %q, want converting", claimed.Status)
	}
	if second, _ := store.ClaimNextPending(); second != nil {
		t.Error("second claim should be nil")
	}
	j.Status = "submitted"
	j.CupsJobID = "dcp-1"
	if err := store.UpdateJob(j); err != nil {
		t.Fatal(err)
	}
	got, _ := store.GetJobByCupsID("dcp-1")
	if got == nil || got.ID != j.ID {
		t.Error("expected job by cups id")
	}
	jobs, err := store.ListJobs(JobFilter{Source: "web", Limit: 10})
	if err != nil || len(jobs) != 1 {
		t.Fatalf("list jobs: %v %v", jobs, err)
	}
}

func TestDriverAndSettings(t *testing.T) {
	store := newTestStore(t)
	d := &Driver{Name: "brlaser", Version: "6", Kind: "builtin", State: "ready", ModelsCount: 2, License: "GPL-2.0"}
	if err := store.UpsertDriver(d); err != nil {
		t.Fatal(err)
	}
	list, err := store.ListDrivers()
	if err != nil || len(list) != 1 {
		t.Fatalf("list drivers: %v %v", list, err)
	}
	d.State = "degraded"
	if err := store.UpsertDriver(d); err != nil {
		t.Fatal(err)
	}
	got, _ := store.GetDriver(d.ID)
	if got.State != "degraded" {
		t.Errorf("state = %q", got.State)
	}
	if err := store.SetSetting("max_upload_mb", "100"); err != nil {
		t.Fatal(err)
	}
	v, ok, err := store.GetSetting("max_upload_mb")
	if err != nil || !ok || v != "100" {
		t.Errorf("setting = %q %v %v", v, ok, err)
	}
}
