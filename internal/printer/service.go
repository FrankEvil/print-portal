package printer

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/liuzhaojun/print-portal/internal/cups"
	"github.com/liuzhaojun/print-portal/internal/storage"
)

// Service manages printer records and the matching CUPS queues.
type Service struct {
	store *storage.Store
	cups  *cups.Client
}

func NewService(store *storage.Store, cupsClient *cups.Client) *Service {
	return &Service{store: store, cups: cupsClient}
}

type CreateInput struct {
	Name      string `json:"name"`
	Model     string `json:"model"`
	PPDQueue  string `json:"ppd_queue"`
	RAWQueue  string `json:"raw_queue"`
	DeviceURI string `json:"device_uri"`
	PPDFile   string `json:"ppd_file"`
	IsDefault bool   `json:"is_default"`
	IsShared  bool   `json:"is_shared"`
}

func (in CreateInput) validate() error {
	if strings.TrimSpace(in.Name) == "" {
		return errors.New("name is required")
	}
	if strings.TrimSpace(in.PPDQueue) == "" || strings.TrimSpace(in.RAWQueue) == "" {
		return errors.New("ppd_queue and raw_queue are required")
	}
	if in.PPDQueue == in.RAWQueue {
		return errors.New("ppd_queue and raw_queue must differ")
	}
	if strings.TrimSpace(in.DeviceURI) == "" {
		return errors.New("device_uri is required")
	}
	if strings.TrimSpace(in.Model) == "" && strings.TrimSpace(in.PPDFile) == "" {
		return errors.New("model or ppd_file is required for the PPD queue")
	}
	for _, queue := range []string{in.PPDQueue, in.RAWQueue} {
		if strings.ContainsAny(queue, " \t\n/#") {
			return fmt.Errorf("invalid queue name %q", queue)
		}
	}
	return nil
}

// Create makes a printer record and both CUPS queues.
func (s *Service) Create(ctx context.Context, in CreateInput) (*storage.Printer, error) {
	if err := in.validate(); err != nil {
		return nil, err
	}
	existing, err := s.store.GetPrinterByName(in.Name)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return nil, fmt.Errorf("printer %q already exists", in.Name)
	}
	if err := s.cups.AddPrinter(ctx, in.PPDQueue, in.DeviceURI, in.Model, in.PPDFile); err != nil {
		return nil, fmt.Errorf("create PPD queue: %w", err)
	}
	if err := s.cups.AddPrinter(ctx, in.RAWQueue, in.DeviceURI, "", ""); err != nil {
		_ = s.cups.DeletePrinter(ctx, in.PPDQueue)
		return nil, fmt.Errorf("create RAW queue: %w", err)
	}
	if in.IsDefault {
		_ = s.cups.SetDefault(ctx, in.PPDQueue)
	}
	p := &storage.Printer{
		Name:      in.Name,
		Model:     in.Model,
		PPDQueue:  in.PPDQueue,
		RAWQueue:  in.RAWQueue,
		DeviceURI: in.DeviceURI,
		PPDFile:   in.PPDFile,
		IsDefault: in.IsDefault,
		IsShared:  in.IsShared,
	}
	if err := s.store.CreatePrinter(p); err != nil {
		_ = s.cups.DeletePrinter(ctx, in.PPDQueue)
		_ = s.cups.DeletePrinter(ctx, in.RAWQueue)
		return nil, err
	}
	return p, nil
}

// Update changes an existing printer. Queue names are immutable in P1.
func (s *Service) Update(ctx context.Context, name string, in CreateInput) (*storage.Printer, error) {
	p, err := s.store.GetPrinterByName(name)
	if err != nil {
		return nil, err
	}
	if p == nil {
		return nil, errors.New("printer not found")
	}
	if in.PPDQueue != "" && in.PPDQueue != p.PPDQueue {
		return nil, errors.New("ppd_queue cannot be renamed")
	}
	if in.RAWQueue != "" && in.RAWQueue != p.RAWQueue {
		return nil, errors.New("raw_queue cannot be renamed")
	}
	if in.DeviceURI != "" && in.DeviceURI != p.DeviceURI {
		if err := s.cups.ModifyPrinter(ctx, p.PPDQueue, in.DeviceURI); err != nil {
			return nil, fmt.Errorf("update PPD queue: %w", err)
		}
		if err := s.cups.ModifyPrinter(ctx, p.RAWQueue, in.DeviceURI); err != nil {
			return nil, fmt.Errorf("update RAW queue: %w", err)
		}
		p.DeviceURI = in.DeviceURI
	}
	if in.Model != "" && in.Model != p.Model {
		if err := s.cups.AddPrinter(ctx, p.PPDQueue, p.DeviceURI, in.Model, in.PPDFile); err != nil {
			return nil, fmt.Errorf("update PPD/model: %w", err)
		}
		p.Model = in.Model
		p.PPDFile = in.PPDFile
	}
	if in.Name != "" && in.Name != p.Name {
		p.Name = in.Name
	}
	p.IsShared = in.IsShared
	p.IsDefault = in.IsDefault
	if err := s.store.UpdatePrinter(p); err != nil {
		return nil, err
	}
	if p.IsDefault {
		_ = s.cups.SetDefault(ctx, p.PPDQueue)
		if err := s.store.SetDefaultPrinter(p.ID); err != nil {
			return nil, err
		}
	}
	return p, nil
}

func (s *Service) Delete(ctx context.Context, name string) error {
	p, err := s.store.GetPrinterByName(name)
	if err != nil {
		return err
	}
	if p == nil {
		return errors.New("printer not found")
	}
	if err := s.cups.DeletePrinter(ctx, p.PPDQueue); err != nil {
		return err
	}
	if err := s.cups.DeletePrinter(ctx, p.RAWQueue); err != nil {
		return err
	}
	return s.store.DeletePrinter(p.ID)
}

// TestPage submits a built-in one-page PDF to the PPD queue.
func (s *Service) TestPage(ctx context.Context, name string) (string, error) {
	p, err := s.store.GetPrinterByName(name)
	if err != nil {
		return "", err
	}
	if p == nil {
		return "", errors.New("printer not found")
	}
	tmp, err := os.CreateTemp("", "print-portal-test-*.pdf")
	if err != nil {
		return "", err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(makeTestPDF()); err != nil {
		tmp.Close()
		return "", err
	}
	tmp.Close()
	return s.cups.Submit(ctx, p.PPDQueue, tmp.Name(), cups.PrintOptions{
		PaperSize:   "A4",
		Duplex:      "one-sided",
		Copies:      1,
		Orientation: "portrait",
		ColorMode:   "monochrome",
		Scaling:     "fit",
	})
}

// makeTestPDF builds a minimal valid one-page PDF with a test message.
func makeTestPDF() []byte {
	var b bytes.Buffer
	content := "BT /F1 24 Tf 72 760 Td (Print Portal) Tj 0 -36 Td /F1 12 Tf (Test Page) Tj ET\n"
	objects := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 595 842] /Resources << /Font << /F1 5 0 R >> >> /Contents 4 0 R >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%sendstream", len(content), content),
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
	}
	b.WriteString("%PDF-1.4\n")
	offsets := make([]int, len(objects)+1)
	for i, obj := range objects {
		offsets[i+1] = b.Len()
		fmt.Fprintf(&b, "%d 0 obj\n%s\nendobj\n", i+1, obj)
	}
	xref := b.Len()
	fmt.Fprintf(&b, "xref\n0 %d\n0000000000 65535 f \n", len(objects)+1)
	for i := 1; i <= len(objects); i++ {
		fmt.Fprintf(&b, "%010d 00000 n \n", offsets[i])
	}
	fmt.Fprintf(&b, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objects)+1, xref)
	return b.Bytes()
}

// EnsureUploadDir is a small helper used by the API.
func EnsureUploadDir(dataDir string) (string, error) {
	dir := filepath.Join(dataDir, "uploads")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return dir, nil
}

var _ = time.Now
