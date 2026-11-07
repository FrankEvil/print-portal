package job

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/liuzhaojun/print-portal/internal/convert"
	"github.com/liuzhaojun/print-portal/internal/cups"
	"github.com/liuzhaojun/print-portal/internal/storage"
)

// FileConverter is the subset of convert.Converter used by the job manager.
type FileConverter interface {
	ToPDF(ctx context.Context, src, dst string) error
	PageCount(ctx context.Context, pdf string) (int, error)
	SplitPDF(ctx context.Context, src, dstDir string, chunkPages int) ([]string, error)
	Thumbnail(ctx context.Context, pdf, dst string) error
	ThumbnailImage(ctx context.Context, src, dst string) error
}

// Manager owns the asynchronous print queue.
type Manager struct {
	store   *storage.Store
	cups    *cups.Client
	conv    FileConverter
	dataDir string
	logger  *log.Logger

	wake chan struct{}
	stop chan struct{}
	once sync.Once
}

func New(store *storage.Store, cupsClient *cups.Client, conv FileConverter, dataDir string, logger *log.Logger) *Manager {
	return &Manager{
		store:   store,
		cups:    cupsClient,
		conv:    conv,
		dataDir: dataDir,
		logger:  logger,
		wake:    make(chan struct{}, 16),
		stop:    make(chan struct{}),
	}
}

// Start runs the worker and status sync loop until ctx is cancelled.
func (m *Manager) Start(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-m.stop:
				return
			case <-m.wake:
			case <-ticker.C:
			}
			m.processAll(ctx)
			m.syncStatuses(ctx)
		}
	}()
}

// Stop terminates the worker loop.
func (m *Manager) Stop() {
	m.once.Do(func() { close(m.stop) })
}

// Wake nudges the worker to process pending jobs immediately.
func (m *Manager) Wake() {
	select {
	case m.wake <- struct{}{}:
	default:
	}
}

func (m *Manager) processAll(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		j, err := m.store.ClaimNextPending()
		if err != nil {
			m.logger.Printf("claim next job: %v", err)
			return
		}
		if j == nil {
			return
		}
		m.process(ctx, j)
	}
}

func (m *Manager) process(ctx context.Context, j *storage.Job) {
	jobDir := filepath.Join(m.dataDir, "jobs", fmt.Sprintf("%d", j.ID))
	if err := os.MkdirAll(jobDir, 0o755); err != nil {
		m.fail(j, "cannot create job directory: "+err.Error())
		return
	}
	pdfPath := filepath.Join(jobDir, "converted.pdf")
	convertCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	if err := m.conv.ToPDF(convertCtx, j.StoredPath, pdfPath); err != nil {
		m.fail(j, "convert failed: "+err.Error())
		return
	}
	m.logger.Printf("job %d converted to %s", j.ID, pdfPath)

	if pages, err := m.conv.PageCount(convertCtx, pdfPath); err == nil {
		j.Pages = pages
	}
	if err := validatePageRange(j.PageRange, j.Pages); err != nil {
		m.fail(j, err.Error())
		return
	}
	preview := filepath.Join(jobDir, "preview.png")
	if err := m.conv.Thumbnail(convertCtx, pdfPath, preview); err == nil {
		j.PreviewPath = "/api/jobs/" + fmt.Sprintf("%d", j.ID) + "/preview"
	}
	// Persist conversion results before submitting.
	_ = m.store.UpdateJob(j)

	// A cancellation can arrive while converting.
	fresh, err := m.store.GetJob(j.ID)
	if err == nil && fresh != nil && fresh.Status == "cancelled" {
		return
	}

	printer, err := m.store.GetPrinter(j.PrinterID)
	if err != nil || printer == nil {
		m.fail(j, "printer not found")
		return
	}
	opts := cups.PrintOptions{
		PageRange:   j.PageRange,
		Duplex:      j.Duplex,
		Copies:      j.Copies,
		PaperSize:   j.PaperSize,
		Orientation: j.Orientation,
		ColorMode:   j.ColorMode,
		Scaling:     j.Scaling,
	}
	// Large multi-page jobs are split into small CUPS jobs and submitted
	// one at a time. This keeps the USB/printer buffer from being flooded.
	chunkPages := settingInt(m.store, "chunk_pages", 4)
	if chunkPages > 0 && j.PageRange == "" && j.Pages > chunkPages {
		chunks, splitErr := m.conv.SplitPDF(convertCtx, pdfPath, filepath.Join(jobDir, "chunks"), chunkPages)
		if splitErr == nil && len(chunks) > 1 {
			if err := m.submitChunks(ctx, j, printer, chunks, opts); err != nil {
				m.fail(j, err.Error())
			}
			return
		}
		if splitErr != nil {
			m.logger.Printf("job %d chunking unavailable, fallback to single job: %v", j.ID, splitErr)
		}
	}
	renderedPath := pdfPath + ".raw"
	useRaw := true
	if err := m.cups.RenderToRaw(ctx, printer.PPDQueue, pdfPath, renderedPath, opts); err != nil {
		if errors.Is(err, cups.ErrNoPPD) {
			useRaw = false
		} else {
			m.fail(j, "render failed: "+err.Error())
			return
		}
	}
	submitCtx, submitCancel := context.WithTimeout(ctx, 60*time.Second)
	defer submitCancel()
	queue := printer.PPDQueue
	fileName := pdfPath
	if useRaw {
		queue = printer.RAWQueue
		fileName = renderedPath
	}
	cupsID, err := m.cups.Submit(submitCtx, queue, fileName, opts)
	if err != nil {
		m.fail(j, err.Error())
		return
	}
	j.Status = "submitted"
	j.CupsJobID = cupsID
	if err := m.store.UpdateJob(j); err != nil {
		m.logger.Printf("update job %d: %v", j.ID, err)
	}
	m.logger.Printf("job %d submitted as CUPS job %s", j.ID, cupsID)
}

func validatePageRange(value string, total int) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	for _, token := range strings.FieldsFunc(value, func(r rune) bool { return r == ' ' || r == ',' }) {
		parts := strings.SplitN(token, "-", 2)
		start, err := strconv.Atoi(parts[0])
		if err != nil || start < 1 {
			return fmt.Errorf("invalid page range %q", value)
		}
		end := start
		if len(parts) == 2 {
			end, err = strconv.Atoi(parts[1])
			if err != nil || end < start {
				return fmt.Errorf("invalid page range %q", value)
			}
		}
		if end > total {
			return fmt.Errorf("page range exceeds document (%d pages)", total)
		}
	}
	return nil
}

// submitChunks submits one small CUPS job at a time and waits for it to leave
// the CUPS queue before sending the next chunk.
func (m *Manager) submitChunks(ctx context.Context, j *storage.Job, printer *storage.Printer, chunks []string, opts cups.PrintOptions) error {
	copies := opts.Copies
	if copies < 1 {
		copies = 1
	}
	opts.Copies = 1
	for copyIndex := 0; copyIndex < copies; copyIndex++ {
		for _, chunk := range chunks {
			fresh, _ := m.store.GetJob(j.ID)
			if fresh != nil && fresh.Status == "cancelled" {
				return nil
			}
			renderedPath := chunk + ".raw"
			renderCtx, renderCancel := context.WithTimeout(ctx, 5*time.Minute)
			renderErr := m.cups.RenderToRaw(renderCtx, printer.PPDQueue, chunk, renderedPath, opts)
			renderCancel()
			queue := printer.RAWQueue
			fileName := renderedPath
			if renderErr != nil {
				if errors.Is(renderErr, cups.ErrNoPPD) {
					queue = printer.PPDQueue
					fileName = chunk
				} else {
					return renderErr
				}
			}
			submitCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
			cupsID, err := m.cups.Submit(submitCtx, queue, fileName, opts)
			cancel()
			if err != nil {
				return err
			}
			j.Status = "printing"
			j.CupsJobID = cupsID
			_ = m.store.UpdateJob(j)
			m.logger.Printf("job %d chunk submitted as CUPS job %s", j.ID, cupsID)
			if err := m.waitCUPSJob(ctx, cupsID); err != nil {
				_ = m.cups.Cancel(context.Background(), cupsID)
				return err
			}
		}
	}
	j.Status = "completed"
	return m.store.UpdateJob(j)
}

func (m *Manager) waitCUPSJob(ctx context.Context, jobID string) error {
	deadline := time.Now().Add(10 * time.Minute)
	for {
		if time.Now().After(deadline) {
			return errors.New("CUPS 分块任务超时，已停止继续打印")
		}
		active, err := m.cups.ListActiveJobs(ctx)
		if err == nil {
			found := false
			for _, job := range active {
				if job.ID == jobID {
					found = true
					break
				}
			}
			if !found {
				return nil
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
}

func (m *Manager) fail(j *storage.Job, msg string) {
	j.Status = "failed"
	j.ErrorMessage = msg
	_ = m.store.UpdateJob(j)
	m.logger.Printf("job %d failed: %s", j.ID, msg)
}

// syncStatuses reconciles application job states with CUPS and imports local jobs.
func (m *Manager) syncStatuses(ctx context.Context) {
	active, err := m.cups.ListActiveJobs(ctx)
	if err != nil {
		return
	}
	completed, _ := m.cups.ListAllJobs(ctx)
	activeIDs := map[string]cups.Job{}
	for _, j := range active {
		activeIDs[j.ID] = j
	}
	completedIDs := map[string]cups.Job{}
	for _, j := range completed {
		completedIDs[j.ID] = j
	}

	printing := map[string]string{} // queue -> job id
	printers, _ := m.cups.ListPrinters(ctx)
	for _, p := range printers {
		if p.PrintingJob != "" {
			printing[p.Name] = p.PrintingJob
		}
	}

	// Reconcile web jobs that have been submitted to CUPS.
	jobs, err := m.store.ListJobs(storage.JobFilter{Limit: 500})
	if err == nil {
		for i := range jobs {
			j := &jobs[i]
			if j.CupsJobID == "" {
				continue
			}
			switch j.Status {
			case "submitted", "queued", "printing", "held":
				if _, ok := activeIDs[j.CupsJobID]; ok {
					if printing[queueOfJob(j.CupsJobID)] == j.CupsJobID {
						j.Status = "printing"
					} else {
						j.Status = "queued"
					}
					_ = m.store.UpdateJobStatus(j.ID, j.Status, "")
				} else if _, ok := completedIDs[j.CupsJobID]; ok {
					_ = m.store.UpdateJobStatus(j.ID, "completed", "")
				}
			}
		}
	}

	// Import local (client-submitted) CUPS jobs into the task list.
	m.importLocalJobs(active, completed)
}

func queueOfJob(cupsID string) string {
	if idx := strings.LastIndex(cupsID, "-"); idx > 0 {
		return cupsID[:idx]
	}
	return cupsID
}

func recentCUPSJob(created string, maxAge time.Duration) bool {
	created = strings.TrimSpace(created)
	if created == "" {
		return false
	}
	t, err := time.Parse("Mon Jan 2 15:04:05 2006", created)
	if err != nil {
		return false
	}
	return time.Since(t) <= maxAge
}

func (m *Manager) importLocalJobs(active, completed []cups.Job) {
	printers, err := m.store.ListPrinters()
	if err != nil {
		return
	}
	queueToPrinter := map[string]*storage.Printer{}
	for i := range printers {
		queueToPrinter[printers[i].PPDQueue] = &printers[i]
		queueToPrinter[printers[i].RAWQueue] = &printers[i]
	}
	importOne := func(cj cups.Job, status, fallbackName string) {
		// Empty jobs are CUPS validation/retry artifacts, not real print jobs.
		if cj.Size <= 0 {
			return
		}
		printer := queueToPrinter[cj.Queue]
		if printer == nil {
			return
		}
		existing, err := m.store.GetJobByCupsID(cj.ID)
		if err != nil {
			return
		}
		if existing != nil {
			if existing.Source == "local" {
				if storedName, err := m.store.GetLocalJobName(cj.ID); err == nil && storedName != "" && existing.Filename != storedName {
					_ = m.store.UpdateJobFilename(existing.ID, storedName)
				} else if fallbackName != "" && existing.Filename == existing.CupsJobID {
					_ = m.store.UpdateJobFilename(existing.ID, fallbackName)
				}
				if existing.Status != status && status != "" {
					_ = m.store.UpdateJobStatus(existing.ID, status, "")
				}
			}
			return
		}
		filename := cj.Name
		if filename == "" {
			if storedName, err := m.store.GetLocalJobName(cj.ID); err == nil {
				filename = storedName
			}
		}
		if filename == "" {
			filename = fallbackName
		}
		if filename == "" {
			filename = cj.ID
		}
		j := &storage.Job{
			PrinterID: printer.ID,
			Source:    "local",
			Filename:  filename,
			CupsUser:  cj.User,
			Copies:    1,
			Status:    status,
			CupsJobID: cj.ID,
		}
		if err := m.store.CreateJob(j); err != nil {
			m.logger.Printf("import local job %s: %v", cj.ID, err)
		}
	}
	for _, cj := range active {
		importOne(cj, "queued", "")
	}
	// Completed local jobs are historical CUPS records and must not be
	// imported as new application tasks. Only refresh rows that were already
	// created while the job was active.
	for _, cj := range completed {
		if cj.Size <= 0 {
			continue
		}
		printer := queueToPrinter[cj.Queue]
		if printer == nil {
			continue
		}
		// Completed CUPS history is historical by definition. Import only
		// recent completions so old retry/validation artifacts do not flood
		// the task list, while AirPrint/ipptool jobs submitted just now still
		// become visible.
		if !recentCUPSJob(cj.Created, 10*time.Minute) {
			continue
		}
		name, _ := m.store.GetLocalJobName(cj.ID)
		if name == "" {
			name = "AirPrint / IPP 打印"
		}
		recent, err := m.store.HasRecentLocalJob(printer.ID, cj.User, name, time.Now().Add(-10*time.Minute).Format(time.RFC3339))
		if err != nil || recent {
			continue
		}
		importOne(cj, "completed", name)
	}
}

// Cancel cancels a job at both layers.
func (m *Manager) Cancel(ctx context.Context, id int64) error {
	j, err := m.store.GetJob(id)
	if err != nil {
		return err
	}
	if j == nil {
		return errors.New("job not found")
	}
	switch j.Status {
	case "completed", "failed", "cancelled":
		return fmt.Errorf("job is already %s", j.Status)
	}
	if j.CupsJobID != "" {
		if err := m.cups.Cancel(ctx, j.CupsJobID); err != nil {
			return err
		}
	}
	return m.store.UpdateJobStatus(j.ID, "cancelled", "")
}

// Retry creates a new job from a failed or cancelled web job.
func (m *Manager) Retry(id int64) (*storage.Job, error) {
	old, err := m.store.GetJob(id)
	if err != nil {
		return nil, err
	}
	if old == nil {
		return nil, errors.New("job not found")
	}
	if old.Source != "web" {
		return nil, errors.New("only web jobs can be retried")
	}
	if old.Status != "failed" && old.Status != "cancelled" {
		return nil, fmt.Errorf("job in state %s cannot be retried", old.Status)
	}
	clone := &storage.Job{
		PrinterID:   old.PrinterID,
		Source:      "web",
		Filename:    old.Filename,
		SourceIP:    old.SourceIP,
		Pages:       old.Pages,
		PageRange:   old.PageRange,
		Duplex:      old.Duplex,
		Copies:      old.Copies,
		PaperSize:   old.PaperSize,
		Orientation: old.Orientation,
		ColorMode:   old.ColorMode,
		Scaling:     old.Scaling,
		Status:      "pending",
		StoredPath:  old.StoredPath,
	}
	if err := m.store.CreateJob(clone); err != nil {
		return nil, err
	}
	m.Wake()
	return clone, nil
}

// KindName is a helper for API logging.
func KindName(path string) string { return string(convert.Detect(path)) }

func settingInt(store *storage.Store, key string, fallback int) int {
	value, ok, _ := store.GetSetting(key)
	if !ok || strings.TrimSpace(value) == "" {
		return fallback
	}
	n, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil {
		return fallback
	}
	return n
}
