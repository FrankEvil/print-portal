package scan

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/liuzhaojun/print-portal/internal/cups"
	"github.com/liuzhaojun/print-portal/internal/storage"
)

// Service runs SANE scans and records scan jobs.
type Service struct {
	store   *storage.Store
	dataDir string
	runner  cups.Runner
}

func NewService(store *storage.Store, dataDir string, runner cups.Runner) *Service {
	return &Service{store: store, dataDir: dataDir, runner: runner}
}

// Params are scan parameters from the web UI.
type Params struct {
	Scanner    string `json:"scanner"`
	Format     string `json:"format"`
	Resolution int    `json:"resolution"`
	ColorMode  string `json:"color_mode"`
}

func (p *Params) normalize() error {
	if strings.TrimSpace(p.Scanner) == "" {
		return fmt.Errorf("scanner is required")
	}
	switch strings.ToLower(p.Format) {
	case "", "pdf":
		p.Format = "pdf"
	case "jpeg", "jpg":
		p.Format = "jpeg"
	case "png":
		p.Format = "png"
	default:
		return fmt.Errorf("unsupported scan format: %s", p.Format)
	}
	if p.Resolution == 0 {
		p.Resolution = 300
	}
	switch p.Resolution {
	case 150, 300, 600:
	default:
		return fmt.Errorf("unsupported resolution: %d", p.Resolution)
	}
	switch strings.ToLower(p.ColorMode) {
	case "", "color":
		p.ColorMode = "Color"
	case "gray", "grayscale":
		p.ColorMode = "Gray"
	case "lineart", "blackwhite", "bw":
		p.ColorMode = "Lineart"
	default:
		return fmt.Errorf("unsupported color mode: %s", p.ColorMode)
	}
	return nil
}

// Scan runs scanimage synchronously and stores the result.
func (s *Service) Scan(ctx context.Context, p Params) (*storage.ScanJob, error) {
	if err := p.normalize(); err != nil {
		return nil, err
	}
	dir := filepath.Join(s.dataDir, "scans")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	filename := fmt.Sprintf("scan-%s.%s", time.Now().Format("20060102-150405"), p.Format)
	path := filepath.Join(dir, filename)
	args := []string{
		"-d", p.Scanner,
		"--format=" + p.Format,
		"--resolution=" + strconv.Itoa(p.Resolution),
		"--mode=" + p.ColorMode,
		"--output-file=" + path,
	}
	out, stderr, err := s.runner.Run(ctx, "scanimage", args...)
	_ = out
	job := &storage.ScanJob{
		Scanner:    p.Scanner,
		Filename:   filename,
		Format:     p.Format,
		Resolution: p.Resolution,
		ColorMode:  p.ColorMode,
		Status:     "completed",
		StoredPath: path,
	}
	if err != nil {
		job.Status = "failed"
		job.StoredPath = ""
		_ = s.store.CreateScanJob(job)
		return job, fmt.Errorf("scanimage failed: %s: %w", strings.TrimSpace(stderr), err)
	}
	if err := s.store.CreateScanJob(job); err != nil {
		return nil, err
	}
	return job, nil
}

// File returns the stored scan file path.
func (s *Service) File(id int64) (*storage.ScanJob, string, error) {
	job, err := s.store.GetScanJob(id)
	if err != nil {
		return nil, "", err
	}
	if job == nil || job.StoredPath == "" {
		return nil, "", fmt.Errorf("scan not found")
	}
	if _, err := os.Stat(job.StoredPath); err != nil {
		return nil, "", fmt.Errorf("scan file not found")
	}
	return job, job.StoredPath, nil
}
