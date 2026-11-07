package storage

import (
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

// Store wraps the SQLite database.
type Store struct {
	db *sql.DB
}

// Open opens (and creates if needed) the SQLite database.
func Open(path string) (*Store, error) {
	dsn := fmt.Sprintf("file:%s?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)", path)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if err := db.Ping(); err != nil {
		return nil, err
	}
	return &Store{db: db}, nil
}

// Close closes the database.
func (s *Store) Close() error { return s.db.Close() }

// DB exposes the underlying database for diagnostics.
func (s *Store) DB() *sql.DB { return s.db }

// Migrate applies embedded migrations that have not run yet.
func (s *Store) Migrate() error {
	if _, err := s.db.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (
		version INTEGER PRIMARY KEY,
		applied_at TEXT NOT NULL
	)`); err != nil {
		return err
	}
	entries, err := migrationFS.ReadDir("migrations")
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		var version int
		if _, err := fmt.Sscanf(entry.Name(), "%d_", &version); err != nil || version == 0 {
			continue
		}
		var exists int
		if err := s.db.QueryRow(`SELECT COUNT(1) FROM schema_migrations WHERE version = ?`, version).Scan(&exists); err != nil {
			return err
		}
		if exists > 0 {
			continue
		}
		body, err := migrationFS.ReadFile("migrations/" + entry.Name())
		if err != nil {
			return err
		}
		for _, stmt := range splitStatements(string(body)) {
			if _, err := s.db.Exec(stmt); err != nil {
				return fmt.Errorf("migration %s: %w", entry.Name(), err)
			}
		}
		if _, err := s.db.Exec(`INSERT INTO schema_migrations(version, applied_at) VALUES (?, ?)`, version, now()); err != nil {
			return err
		}
	}
	return nil
}

func splitStatements(script string) []string {
	var out []string
	for _, chunk := range strings.Split(script, ";") {
		stmt := strings.TrimSpace(chunk)
		if stmt == "" {
			continue
		}
		// Drop pure comment lines.
		lines := strings.Split(stmt, "\n")
		var kept []string
		for _, line := range lines {
			t := strings.TrimSpace(line)
			if t == "" || strings.HasPrefix(t, "--") {
				continue
			}
			kept = append(kept, line)
		}
		clean := strings.TrimSpace(strings.Join(kept, "\n"))
		if clean != "" {
			out = append(out, clean)
		}
	}
	return out
}

func now() string { return time.Now().Format(time.RFC3339) }

// ---- Printers ----

type Printer struct {
	ID             int64  `json:"id"`
	Name           string `json:"name"`
	Model          string `json:"model"`
	PPDQueue       string `json:"ppd_queue"`
	RAWQueue       string `json:"raw_queue"`
	DeviceURI      string `json:"device_uri"`
	PPDFile        string `json:"ppd_file"`
	IsDefault      bool   `json:"is_default"`
	IsShared       bool   `json:"is_shared"`        // allow LAN access
	AllowDiscovery bool   `json:"allow_discovery"`  // advertise via CUPS/DNS-SD
	AirPrint       bool   `json:"airprint_enabled"` // advertise as AirPrint
	CreatedAt      string `json:"created_at"`
	UpdatedAt      string `json:"updated_at"`
}

const printerColumns = `id, name, model, ppd_queue, raw_queue, device_uri, ppd_file, is_default, is_shared, allow_discovery, airprint_enabled, created_at, updated_at`

func scanPrinter(scanner interface{ Scan(...any) error }) (*Printer, error) {
	var p Printer
	var isDefault, isShared, allowDiscovery, airPrint int
	if err := scanner.Scan(&p.ID, &p.Name, &p.Model, &p.PPDQueue, &p.RAWQueue, &p.DeviceURI, &p.PPDFile, &isDefault, &isShared, &allowDiscovery, &airPrint, &p.CreatedAt, &p.UpdatedAt); err != nil {
		return nil, err
	}
	p.IsDefault = isDefault == 1
	p.IsShared = isShared == 1
	p.AllowDiscovery = allowDiscovery == 1
	p.AirPrint = airPrint == 1
	return &p, nil
}

func (s *Store) ListPrinters() ([]Printer, error) {
	rows, err := s.db.Query(`SELECT ` + printerColumns + ` FROM printers ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Printer
	for rows.Next() {
		p, err := scanPrinter(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *p)
	}
	return out, rows.Err()
}

func (s *Store) GetPrinter(id int64) (*Printer, error) {
	row := s.db.QueryRow(`SELECT `+printerColumns+` FROM printers WHERE id = ?`, id)
	p, err := scanPrinter(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return p, err
}

func (s *Store) GetPrinterByName(name string) (*Printer, error) {
	row := s.db.QueryRow(`SELECT `+printerColumns+` FROM printers WHERE name = ?`, name)
	p, err := scanPrinter(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return p, err
}

func (s *Store) GetPrinterByQueue(queue string) (*Printer, error) {
	row := s.db.QueryRow(`SELECT `+printerColumns+` FROM printers WHERE ppd_queue = ? OR raw_queue = ?`, queue, queue)
	p, err := scanPrinter(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return p, err
}

func (s *Store) CreatePrinter(p *Printer) error {
	t := now()
	res, err := s.db.Exec(`INSERT INTO printers(name, model, ppd_queue, raw_queue, device_uri, ppd_file, is_default, is_shared, allow_discovery, airprint_enabled, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		p.Name, p.Model, p.PPDQueue, p.RAWQueue, p.DeviceURI, p.PPDFile, boolInt(p.IsDefault), boolInt(p.IsShared), boolInt(p.AllowDiscovery), boolInt(p.AirPrint), t, t)
	if err != nil {
		return err
	}
	p.ID, _ = res.LastInsertId()
	p.CreatedAt, p.UpdatedAt = t, t
	return nil
}

func (s *Store) UpdatePrinter(p *Printer) error {
	_, err := s.db.Exec(`UPDATE printers SET name=?, model=?, ppd_queue=?, raw_queue=?, device_uri=?, ppd_file=?, is_default=?, is_shared=?, allow_discovery=?, airprint_enabled=?, updated_at=? WHERE id=?`,
		p.Name, p.Model, p.PPDQueue, p.RAWQueue, p.DeviceURI, p.PPDFile, boolInt(p.IsDefault), boolInt(p.IsShared), boolInt(p.AllowDiscovery), boolInt(p.AirPrint), now(), p.ID)
	return err
}

func (s *Store) DeletePrinter(id int64) error {
	_, err := s.db.Exec(`DELETE FROM printers WHERE id=?`, id)
	return err
}

func (s *Store) SetDefaultPrinter(id int64) error {
	if _, err := s.db.Exec(`UPDATE printers SET is_default=0`); err != nil {
		return err
	}
	_, err := s.db.Exec(`UPDATE printers SET is_default=1, updated_at=? WHERE id=?`, now(), id)
	return err
}

func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

// ---- Jobs ----

type Job struct {
	ID           int64  `json:"id"`
	PrinterID    int64  `json:"printer_id"`
	Source       string `json:"source"`
	Filename     string `json:"filename"`
	SourceIP     string `json:"source_ip"`
	CupsUser     string `json:"cups_user"`
	Pages        int    `json:"pages"`
	PageRange    string `json:"page_range"`
	Duplex       string `json:"duplex"`
	Copies       int    `json:"copies"`
	PaperSize    string `json:"paper_size"`
	Orientation  string `json:"orientation"`
	ColorMode    string `json:"color_mode"`
	Scaling      string `json:"scaling"`
	Status       string `json:"status"`
	CupsJobID    string `json:"cups_job_id"`
	ErrorMessage string `json:"error_message"`
	StoredPath   string `json:"-"`
	PreviewPath  string `json:"preview_path,omitempty"`
	CreatedAt    string `json:"created_at"`
	UpdatedAt    string `json:"updated_at"`
}

const jobColumns = `id, printer_id, source, filename, source_ip, cups_user, pages, page_range, duplex, copies, paper_size, orientation, color_mode, scaling, status, cups_job_id, error_message, stored_path, preview_path, created_at, updated_at`

func scanJob(scanner interface{ Scan(...any) error }) (*Job, error) {
	var j Job
	if err := scanner.Scan(&j.ID, &j.PrinterID, &j.Source, &j.Filename, &j.SourceIP, &j.CupsUser, &j.Pages, &j.PageRange, &j.Duplex, &j.Copies, &j.PaperSize, &j.Orientation, &j.ColorMode, &j.Scaling, &j.Status, &j.CupsJobID, &j.ErrorMessage, &j.StoredPath, &j.PreviewPath, &j.CreatedAt, &j.UpdatedAt); err != nil {
		return nil, err
	}
	return &j, nil
}

func (s *Store) CreateJob(j *Job) error {
	t := now()
	if j.Copies <= 0 {
		j.Copies = 1
	}
	if j.Scaling == "" {
		j.Scaling = "auto"
	}
	res, err := s.db.Exec(`INSERT INTO jobs(printer_id, source, filename, source_ip, cups_user, pages, page_range, duplex, copies, paper_size, orientation, color_mode, scaling, status, cups_job_id, error_message, stored_path, preview_path, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		j.PrinterID, j.Source, j.Filename, j.SourceIP, j.CupsUser, j.Pages, j.PageRange, j.Duplex, j.Copies, j.PaperSize, j.Orientation, j.ColorMode, j.Scaling, j.Status, j.CupsJobID, j.ErrorMessage, j.StoredPath, j.PreviewPath, t, t)
	if err != nil {
		return err
	}
	j.ID, _ = res.LastInsertId()
	j.CreatedAt, j.UpdatedAt = t, t
	return nil
}

func (s *Store) GetJob(id int64) (*Job, error) {
	row := s.db.QueryRow(`SELECT `+jobColumns+` FROM jobs WHERE id = ?`, id)
	j, err := scanJob(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return j, err
}

func (s *Store) GetJobByCupsID(cupsID string) (*Job, error) {
	row := s.db.QueryRow(`SELECT `+jobColumns+` FROM jobs WHERE cups_job_id = ? LIMIT 1`, cupsID)
	j, err := scanJob(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return j, err
}

type JobFilter struct {
	Status string
	Source string
	Query  string
	Limit  int
	Offset int
}

func (s *Store) ListJobs(f JobFilter) ([]Job, error) {
	q := `SELECT ` + jobColumns + ` FROM jobs WHERE 1=1`
	var args []any
	if f.Status != "" {
		q += ` AND status = ?`
		args = append(args, f.Status)
	}
	if f.Source != "" {
		q += ` AND source = ?`
		args = append(args, f.Source)
	}
	if f.Query != "" {
		like := "%" + f.Query + "%"
		q += ` AND (filename LIKE ? OR cups_user LIKE ? OR status LIKE ?)`
		args = append(args, like, like, like)
	}
	q += ` ORDER BY id DESC`
	if f.Limit <= 0 || f.Limit > 500 {
		f.Limit = 100
	}
	q += ` LIMIT ? OFFSET ?`
	args = append(args, f.Limit, f.Offset)
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Job
	for rows.Next() {
		j, err := scanJob(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *j)
	}
	return out, rows.Err()
}

func (s *Store) UpdateJob(j *Job) error {
	_, err := s.db.Exec(`UPDATE jobs SET status=?, cups_job_id=?, error_message=?, pages=?, stored_path=?, preview_path=?, updated_at=? WHERE id=?`,
		j.Status, j.CupsJobID, j.ErrorMessage, j.Pages, j.StoredPath, j.PreviewPath, now(), j.ID)
	return err
}

func (s *Store) UpdateJobStatus(id int64, status, errMsg string) error {
	_, err := s.db.Exec(`UPDATE jobs SET status=?, error_message=?, updated_at=? WHERE id=?`, status, errMsg, now(), id)
	return err
}

// UpdateJobStatusUnlessHeld updates an active job without overwriting a
// concurrent explicit pause (held) written by the admin API.
func (s *Store) UpdateJobStatusUnlessHeld(id int64, status, errMsg string) error {
	_, err := s.db.Exec(`UPDATE jobs SET status=?, error_message=?, updated_at=? WHERE id=? AND status NOT IN ('held','paused')`, status, errMsg, now(), id)
	return err
}

// ClearFinishedJobs removes completed/failed/cancelled job history while
// keeping active jobs and their state intact.
func (s *Store) ClearFinishedJobs() (int64, error) {
	_, _ = s.db.Exec(`DELETE FROM local_job_names WHERE cups_job_id IN (
		SELECT cups_job_id FROM jobs
		WHERE source='local' AND status IN ('completed','failed','cancelled')
	)`)
	res, err := s.db.Exec(`DELETE FROM jobs WHERE status IN ('completed','failed','cancelled')`)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// SaveLocalJobName persists the document name captured by the IPP gateway
// for a CUPS job created by a LAN client.
func (s *Store) SaveLocalJobName(cupsJobID, name string) error {
	name = strings.TrimSpace(name)
	if cupsJobID == "" || name == "" {
		return nil
	}
	_, err := s.db.Exec(`INSERT INTO local_job_names(cups_job_id, name, updated_at) VALUES (?, ?, ?)
		ON CONFLICT(cups_job_id) DO UPDATE SET name=excluded.name, updated_at=excluded.updated_at`,
		cupsJobID, name, now())
	return err
}

// UpdateJobFilename replaces a job's display filename.
func (s *Store) UpdateJobFilename(id int64, filename string) error {
	_, err := s.db.Exec(`UPDATE jobs SET filename=?, updated_at=? WHERE id=?`, filename, now(), id)
	return err
}

// GetLocalJobName returns the captured document name for a CUPS job.
func (s *Store) GetLocalJobName(cupsJobID string) (string, error) {
	var name string
	err := s.db.QueryRow(`SELECT name FROM local_job_names WHERE cups_job_id = ?`, cupsJobID).Scan(&name)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return name, err
}

// HasRecentLocalJob reports whether an equivalent local task was already
// imported recently. It is used to collapse CUPS retry jobs that have distinct
// CUPS job IDs but the same client document name.
func (s *Store) HasRecentLocalJob(printerID int64, user, filename, since string) (bool, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(1) FROM jobs
		WHERE source='local' AND printer_id=? AND cups_user=? AND filename=? AND created_at >= ?`,
		printerID, user, filename, since).Scan(&n)
	return n > 0, err
}

// ClaimNextPending atomically moves the oldest pending job to converting and returns it.
func (s *Store) ClaimNextPending() (*Job, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	row := tx.QueryRow(`SELECT ` + jobColumns + ` FROM jobs WHERE status = 'pending' ORDER BY id LIMIT 1`)
	j, err := scanJob(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	res, err := tx.Exec(`UPDATE jobs SET status='converting', updated_at=? WHERE id=? AND status='pending'`, now(), j.ID)
	if err != nil {
		return nil, err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return nil, nil
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	j.Status = "converting"
	return j, nil
}

// ResetOrphanJobs puts converting jobs back to pending on startup.
func (s *Store) ResetOrphanJobs() error {
	_, err := s.db.Exec(`UPDATE jobs SET status='pending', updated_at=? WHERE status IN ('converting','submitted') AND cups_job_id = ''`, now())
	return err
}

// ---- Drivers ----

type Driver struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Version     string `json:"version"`
	Kind        string `json:"kind"`
	Source      string `json:"source"`
	PackagePath string `json:"-"`
	PPDDir      string `json:"-"`
	FilterDir   string `json:"-"`
	State       string `json:"state"`
	ModelsCount int    `json:"models_count"`
	License     string `json:"license"`
	InstalledAt string `json:"installed_at"`
	UpdatedAt   string `json:"updated_at"`
}

func (s *Store) ListDrivers() ([]Driver, error) {
	rows, err := s.db.Query(`SELECT id, name, version, kind, source, package_path, ppd_dir, filter_dir, state, models_count, license, installed_at, updated_at FROM drivers ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Driver
	for rows.Next() {
		var d Driver
		if err := rows.Scan(&d.ID, &d.Name, &d.Version, &d.Kind, &d.Source, &d.PackagePath, &d.PPDDir, &d.FilterDir, &d.State, &d.ModelsCount, &d.License, &d.InstalledAt, &d.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func (s *Store) GetDriver(id int64) (*Driver, error) {
	row := s.db.QueryRow(`SELECT id, name, version, kind, source, package_path, ppd_dir, filter_dir, state, models_count, license, installed_at, updated_at FROM drivers WHERE id=?`, id)
	var d Driver
	err := row.Scan(&d.ID, &d.Name, &d.Version, &d.Kind, &d.Source, &d.PackagePath, &d.PPDDir, &d.FilterDir, &d.State, &d.ModelsCount, &d.License, &d.InstalledAt, &d.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &d, err
}

func (s *Store) UpsertDriver(d *Driver) error {
	t := now()
	var id int64
	err := s.db.QueryRow(`SELECT id FROM drivers WHERE name=?`, d.Name).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		res, err := s.db.Exec(`INSERT INTO drivers(name, version, kind, source, package_path, ppd_dir, filter_dir, state, models_count, license, installed_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			d.Name, d.Version, d.Kind, d.Source, d.PackagePath, d.PPDDir, d.FilterDir, d.State, d.ModelsCount, d.License, t, t)
		if err != nil {
			return err
		}
		d.ID, _ = res.LastInsertId()
		d.InstalledAt, d.UpdatedAt = t, t
		return nil
	}
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`UPDATE drivers SET version=?, kind=?, source=?, package_path=?, ppd_dir=?, filter_dir=?, state=?, models_count=?, license=?, updated_at=? WHERE id=?`,
		d.Version, d.Kind, d.Source, d.PackagePath, d.PPDDir, d.FilterDir, d.State, d.ModelsCount, d.License, t, id)
	if err == nil {
		d.ID = id
	}
	return err
}

func (s *Store) DeleteDriver(id int64) error {
	_, err := s.db.Exec(`DELETE FROM drivers WHERE id=?`, id)
	return err
}

// ---- Settings ----

func (s *Store) GetSetting(key string) (string, bool, error) {
	var v string
	err := s.db.QueryRow(`SELECT value FROM settings WHERE key=?`, key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	return v, err == nil, err
}

func (s *Store) SetSetting(key, value string) error {
	_, err := s.db.Exec(`INSERT INTO settings(key, value, updated_at) VALUES (?, ?, ?)
		ON CONFLICT(key) DO UPDATE SET value=excluded.value, updated_at=excluded.updated_at`, key, value, now())
	return err
}

// ---- Discovered devices ----

type Device struct {
	ID        int64  `json:"id"`
	Kind      string `json:"kind"`
	Name      string `json:"name"`
	Model     string `json:"model"`
	Brand     string `json:"brand"`
	DeviceURI string `json:"device_uri"`
	IP        string `json:"ip"`
	Source    string `json:"source"`
	Metadata  string `json:"metadata"`
	LastSeen  string `json:"last_seen"`
}

func (s *Store) ReplaceDevices(kind string, devices []Device) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM discovered_devices WHERE kind=?`, kind); err != nil {
		return err
	}
	t := now()
	for _, d := range devices {
		if _, err := tx.Exec(`INSERT INTO discovered_devices(kind, name, model, device_uri, ip, source, metadata, last_seen) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			kind, d.Name, d.Model, d.DeviceURI, d.IP, d.Source, d.Metadata, t); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) ListDevices(kind string) ([]Device, error) {
	rows, err := s.db.Query(`SELECT id, kind, name, model, device_uri, ip, source, metadata, last_seen FROM discovered_devices WHERE kind=? ORDER BY id`, kind)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Device
	for rows.Next() {
		var d Device
		if err := rows.Scan(&d.ID, &d.Kind, &d.Name, &d.Model, &d.DeviceURI, &d.IP, &d.Source, &d.Metadata, &d.LastSeen); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// ---- Scan jobs ----

type ScanJob struct {
	ID         int64  `json:"id"`
	Scanner    string `json:"scanner"`
	Filename   string `json:"filename"`
	Format     string `json:"format"`
	Resolution int    `json:"resolution"`
	ColorMode  string `json:"color_mode"`
	Status     string `json:"status"`
	StoredPath string `json:"-"`
	CreatedAt  string `json:"created_at"`
	UpdatedAt  string `json:"updated_at"`
}

func (s *Store) CreateScanJob(j *ScanJob) error {
	t := now()
	res, err := s.db.Exec(`INSERT INTO scan_jobs(scanner, filename, format, resolution, color_mode, status, stored_path, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, j.Scanner, j.Filename, j.Format, j.Resolution, j.ColorMode, j.Status, j.StoredPath, t, t)
	if err != nil {
		return err
	}
	j.ID, _ = res.LastInsertId()
	j.CreatedAt, j.UpdatedAt = t, t
	return nil
}

func (s *Store) GetScanJob(id int64) (*ScanJob, error) {
	row := s.db.QueryRow(`SELECT id, scanner, filename, format, resolution, color_mode, status, stored_path, created_at, updated_at FROM scan_jobs WHERE id=?`, id)
	var j ScanJob
	err := row.Scan(&j.ID, &j.Scanner, &j.Filename, &j.Format, &j.Resolution, &j.ColorMode, &j.Status, &j.StoredPath, &j.CreatedAt, &j.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &j, err
}
