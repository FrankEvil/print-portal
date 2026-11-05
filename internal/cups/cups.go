package cups

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
)

// Runner abstracts external command execution, so the CUPS layer is testable.
type Runner interface {
	Run(ctx context.Context, name string, args ...string) (stdout, stderr string, err error)
}

// ExecRunner runs real commands.
type ExecRunner struct{}

func (ExecRunner) Run(ctx context.Context, name string, args ...string) (string, string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	// Force a stable English locale so command output parsing is predictable
	// on localized hosts (macOS with zh_CN, NAS systems, ...).
	cmd.Env = append(os.Environ(), "LC_ALL=C", "LANG=C")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	return stdout.String(), stderr.String(), err
}

// Client is a thin wrapper around the CUPS command line tools.
type Client struct {
	runner Runner
	server string
}

func NewClient(runner Runner, server string) *Client {
	return &Client{runner: runner, server: server}
}

func (c *Client) command(ctx context.Context, name string, args ...string) (string, string, error) {
	if c.server != "" {
		args = append([]string{"-h", c.server}, args...)
	}
	return c.runner.Run(ctx, name, args...)
}

func (c *Client) Version(ctx context.Context) (string, error) {
	if out, _, err := c.runner.Run(ctx, "cups-config", "--version"); err == nil && strings.TrimSpace(out) != "" {
		return strings.TrimSpace(out), nil
	}
	// cups-config is part of the -dev package; fall back to the dpkg version.
	if out, _, err := c.runner.Run(ctx, "dpkg-query", "-W", "-f=${Version}", "cups"); err == nil && strings.TrimSpace(out) != "" {
		return strings.TrimSpace(out), nil
	}
	return "", fmt.Errorf("cannot determine CUPS version")
}

func (c *Client) SchedulerRunning(ctx context.Context) (bool, error) {
	out, _, err := c.command(ctx, "lpstat", "-r")
	if err != nil {
		return false, err
	}
	lower := strings.ToLower(out)
	if strings.Contains(lower, "not running") || strings.Contains(out, "未运行") {
		return false, nil
	}
	return strings.Contains(lower, "running") || strings.Contains(out, "正在运行"), nil
}

// PrinterStatus is the runtime state of one CUPS queue.
type PrinterStatus struct {
	Name         string   `json:"name"`
	State        string   `json:"state"` // idle / printing / stopped
	StateReasons []string `json:"state_reasons"`
	DeviceURI    string   `json:"device_uri"`
	MakeModel    string   `json:"make_model"`
	PrintingJob  string   `json:"printing_job,omitempty"`
}

var printerLineRE = regexp.MustCompile(`^printer\s+(\S+)\s+(.*)$`)

// parsePrinterStatuses parses `lpstat -p` output.
func parsePrinterStatuses(out string) map[string]*PrinterStatus {
	result := map[string]*PrinterStatus{}
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		m := printerLineRE.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		name := m[1]
		raw := strings.TrimSpace(m[2])
		rest := strings.ToLower(raw)
		rest = strings.TrimPrefix(rest, "is ")
		st := &PrinterStatus{Name: name, State: "stopped"}
		switch {
		case strings.HasPrefix(rest, "idle"):
			st.State = "idle"
		case strings.HasPrefix(rest, "printing"):
			st.State = "printing"
			lowerRaw := strings.ToLower(raw)
			if idx := strings.Index(lowerRaw, "printing "); idx >= 0 {
				job := raw[idx+len("printing "):]
				job = strings.TrimSpace(strings.SplitN(job, ".", 2)[0])
				if job != "" {
					st.PrintingJob = job
				}
			}
		case strings.HasPrefix(rest, "disabled") || strings.HasPrefix(rest, "stopped"):
			st.State = "stopped"
		}
		if strings.Contains(rest, "disabled since") || strings.Contains(rest, "stopped") {
			st.State = "stopped"
		}
		result[name] = st
	}
	return result
}

// parseDeviceURIs parses `lpstat -v` output.
func parseDeviceURIs(out string) map[string]string {
	result := map[string]string{}
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "device for ") {
			continue
		}
		rest := strings.TrimPrefix(line, "device for ")
		parts := strings.SplitN(rest, ": ", 2)
		if len(parts) == 2 {
			result[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
		}
	}
	return result
}

func (c *Client) ListPrinters(ctx context.Context) ([]PrinterStatus, error) {
	pOut, _, err := c.command(ctx, "lpstat", "-p")
	if err != nil && pOut == "" {
		return nil, err
	}
	vOut, _, _ := c.command(ctx, "lpstat", "-v")
	statuses := parsePrinterStatuses(pOut)
	uris := parseDeviceURIs(vOut)
	out := make([]PrinterStatus, 0, len(statuses))
	for name, st := range statuses {
		st.DeviceURI = uris[name]
		out = append(out, *st)
	}
	return out, nil
}

// Job is a task as reported by CUPS.
type Job struct {
	ID      string `json:"id"`
	Queue   string `json:"queue"`
	User    string `json:"user"`
	Size    int64  `json:"size"`
	State   string `json:"state"`
	Name    string `json:"name"`
	Created string `json:"created"`
}

var jobLineRE = regexp.MustCompile(`^(\S+?)-(\d+)\s+(\S+)\s+(\d+)\s+(.*)$`)

// parseJobLines parses `lpstat -o` style output.
func parseJobLines(out string) []Job {
	var jobs []Job
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		m := jobLineRE.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		size, _ := strconv.ParseInt(m[4], 10, 64)
		jobs = append(jobs, Job{
			ID:      m[1] + "-" + m[2],
			Queue:   m[1],
			User:    m[3],
			Size:    size,
			Created: strings.TrimSpace(m[5]),
		})
	}
	return jobs
}

// ListActiveJobs returns jobs CUPS still considers active.
func (c *Client) ListActiveJobs(ctx context.Context) ([]Job, error) {
	out, _, err := c.command(ctx, "lpstat", "-W", "not-completed", "-o")
	if err != nil {
		return nil, err
	}
	return parseJobLines(out), nil
}

// ListAllJobs returns jobs including completed history.
func (c *Client) ListAllJobs(ctx context.Context) ([]Job, error) {
	out, _, err := c.command(ctx, "lpstat", "-W", "all", "-o")
	if err != nil {
		return nil, err
	}
	return parseJobLines(out), nil
}

// parseRequestID extracts the job id from `lp` output.
func parseRequestID(out string) string {
	re := regexp.MustCompile(`request id is\s+(\S+)`)
	m := re.FindStringSubmatch(out)
	if m == nil {
		return ""
	}
	return strings.TrimSpace(m[1])
}

// PrintOptions are the webpage printing parameters.
type PrintOptions struct {
	PageRange   string
	Duplex      string
	Copies      int
	PaperSize   string
	Orientation string
	ColorMode   string
	Scaling     string
}

// Submit sends a file to a CUPS queue and returns the CUPS job id.
func (c *Client) Submit(ctx context.Context, queue, file string, opts PrintOptions) (string, error) {
	args := []string{"-d", queue}
	if opts.PageRange != "" {
		args = append(args, "-o", "page-ranges="+opts.PageRange)
	}
	switch opts.Duplex {
	case "two-sided-long-edge", "two-sided-short-edge":
		args = append(args, "-o", "sides="+opts.Duplex)
	default:
		args = append(args, "-o", "sides=one-sided")
	}
	if opts.PaperSize != "" {
		args = append(args, "-o", "media="+opts.PaperSize)
	}
	if opts.Orientation == "landscape" {
		args = append(args, "-o", "orientation-requested=4")
	}
	switch opts.ColorMode {
	case "monochrome":
		args = append(args, "-o", "print-color-mode=monochrome")
	case "color":
		args = append(args, "-o", "print-color-mode=color")
	}
	switch opts.Scaling {
	case "fit", "auto":
		args = append(args, "-o", "fit-to-page")
	case "fill":
		args = append(args, "-o", "fill")
	}
	copies := opts.Copies
	if copies <= 0 {
		copies = 1
	}
	args = append(args, "-n", strconv.Itoa(copies), file)
	out, stderr, err := c.command(ctx, "lp", args...)
	if err != nil {
		return "", fmt.Errorf("lp failed: %s: %w", strings.TrimSpace(stderr), err)
	}
	id := parseRequestID(out)
	if id == "" {
		return "", fmt.Errorf("could not parse lp output: %s", strings.TrimSpace(out))
	}
	return id, nil
}

func (c *Client) Cancel(ctx context.Context, jobID string) error {
	_, stderr, err := c.command(ctx, "cancel", jobID)
	if err != nil {
		return fmt.Errorf("cancel failed: %s: %w", strings.TrimSpace(stderr), err)
	}
	return nil
}

func (c *Client) Pause(ctx context.Context, queue string) error {
	_, stderr, err := c.command(ctx, "cupsdisable", queue)
	return wrapErr("cupsdisable", stderr, err)
}

func (c *Client) Resume(ctx context.Context, queue string) error {
	_, stderr, err := c.command(ctx, "cupsenable", queue)
	return wrapErr("cupsenable", stderr, err)
}

func (c *Client) Accept(ctx context.Context, queue string) error {
	_, stderr, err := c.command(ctx, "cupsaccept", queue)
	return wrapErr("cupsaccept", stderr, err)
}

func (c *Client) Reject(ctx context.Context, queue string) error {
	_, stderr, err := c.command(ctx, "cupsreject", queue)
	return wrapErr("cupsreject", stderr, err)
}

// Purge cancels all jobs in a queue.
func (c *Client) Purge(ctx context.Context, queue string) error {
	_, stderr, err := c.command(ctx, "cancel", "-a", queue)
	return wrapErr("cancel -a", stderr, err)
}

func wrapErr(what, stderr string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s failed: %s: %w", what, strings.TrimSpace(stderr), err)
}

// AddPrinter creates a queue. If model is non-empty, -m is used; if ppdPath is
// non-empty, -P is used (explicit PPD file).
func (c *Client) AddPrinter(ctx context.Context, queue, deviceURI, model, ppdPath string) error {
	args := []string{"-p", queue, "-v", deviceURI, "-E"}
	if ppdPath != "" {
		args = append(args, "-P", ppdPath)
	} else if model != "" && model != "raw" {
		args = append(args, "-m", model)
	}
	_, stderr, err := c.command(ctx, "lpadmin", args...)
	return wrapErr("lpadmin", stderr, err)
}

func (c *Client) ModifyPrinter(ctx context.Context, queue, deviceURI string) error {
	_, stderr, err := c.command(ctx, "lpadmin", "-p", queue, "-v", deviceURI, "-E")
	return wrapErr("lpadmin", stderr, err)
}

func (c *Client) DeletePrinter(ctx context.Context, queue string) error {
	_, stderr, err := c.command(ctx, "lpadmin", "-x", queue)
	return wrapErr("lpadmin -x", stderr, err)
}

func (c *Client) SetDefault(ctx context.Context, queue string) error {
	_, stderr, err := c.command(ctx, "lpadmin", "-d", queue)
	return wrapErr("lpadmin -d", stderr, err)
}

// PPDOption is one selectable option from a PPD.
type PPDOption struct {
	Keyword     string   `json:"keyword"`
	Text        string   `json:"text"`
	Default     string   `json:"default"`
	Choices     []string `json:"choices"`
	ChoiceTexts []string `json:"choice_texts"`
}

var optionLineRE = regexp.MustCompile(`^([^/]+)/([^:]+):\s*(.*)$`)

// parsePPDOptions parses `lpoptions -l` output.
func parsePPDOptions(out string) []PPDOption {
	var opts []PPDOption
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		m := optionLineRE.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		opt := PPDOption{Keyword: strings.TrimSpace(m[1]), Text: strings.TrimSpace(m[2])}
		for _, choice := range strings.Fields(m[3]) {
			name := strings.TrimPrefix(choice, "*")
			if strings.HasPrefix(choice, "*") {
				opt.Default = name
			}
			opt.Choices = append(opt.Choices, name)
		}
		if len(opt.Choices) > 1 {
			opts = append(opts, opt)
		}
	}
	return opts
}

func (c *Client) PPDOptions(ctx context.Context, queue string) ([]PPDOption, error) {
	out, stderr, err := c.command(ctx, "lpoptions", "-p", queue, "-l")
	if err != nil {
		return nil, fmt.Errorf("lpoptions failed: %s: %w", strings.TrimSpace(stderr), err)
	}
	return parsePPDOptions(out), nil
}

// Model is one PPD entry from `lpinfo -m`.
type Model struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Driver      string `json:"driver"`
	Brand       string `json:"brand"`
}

// parseModelLines parses `lpinfo -m` output.
func parseModelLines(out string) []Model {
	var models []Model
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		name := fields[0]
		desc := cleanModelDescription(strings.Join(fields[1:], " "))
		driver := modelDriver(name)
		models = append(models, Model{Name: name, Description: desc, Driver: driver, Brand: brandFromDescription(desc)})
	}
	return models
}

func cleanModelDescription(desc string) string {
	// macOS's lpinfo includes the local PPD filename in the description, e.g.
	// "DCP-1510 series CUPS.gz Brother DCP-1510 series CUPS". Keep the human
	// readable tail.
	if idx := strings.Index(desc, " CUPS.gz "); idx >= 0 {
		desc = desc[idx+len(" CUPS.gz "):]
	}
	desc = strings.TrimSuffix(desc, " CUPS")
	return strings.TrimSpace(desc)
}

var brandNames = map[string]string{
	"brother": "Brother", "hp": "HP", "hewlett-packard": "HP", "canon": "Canon",
	"epson": "Epson", "samsung": "Samsung", "xerox": "Xerox", "lexmark": "Lexmark",
	"ricoh": "Ricoh", "kyocera": "Kyocera", "konica": "Konica", "minolta": "Konica",
	"dell": "Dell", "lenovo": "Lenovo", "pantum": "Pantum", "oki": "OKI", "sharp": "Sharp",
	"toshiba": "Toshiba", "fuji": "Fuji", "panasonic": "Panasonic", "konica-minolta": "Konica",
}

func brandFromDescription(desc string) string {
	fields := strings.Fields(desc)
	if len(fields) == 0 {
		return "其他"
	}
	// Prefer any known brand name anywhere in the description; some PPD
	// descriptions start with the model ("DCP-1510 series ... Brother ...").
	for _, field := range fields {
		clean := strings.ToLower(strings.Trim(field, ",;()[]{}"))
		if canonical, ok := brandNames[clean]; ok {
			return canonical
		}
	}
	first := strings.Trim(fields[0], ",;()[]")
	return first
}

func modelDriver(name string) string {
	switch {
	case strings.HasPrefix(name, "drv:///brlaser"):
		return "brlaser"
	case strings.HasPrefix(name, "drv:///"):
		trimmed := strings.TrimPrefix(name, "drv:///")
		if idx := strings.Index(trimmed, "."); idx > 0 {
			return trimmed[:idx]
		}
	case strings.HasPrefix(name, "foomatic:"):
		return "foomatic"
	case name == "everywhere":
		return "everywhere"
	}
	if idx := strings.Index(name, ":"); idx > 0 {
		return name[:idx]
	}
	return "other"
}

func (c *Client) ListModels(ctx context.Context) ([]Model, error) {
	out, stderr, err := c.command(ctx, "lpinfo", "-m")
	if err != nil {
		return nil, fmt.Errorf("lpinfo -m failed: %s: %w", strings.TrimSpace(stderr), err)
	}
	return parseModelLines(out), nil
}

// Device is a discovered device from `lpinfo -v` / `lpinfo -l -v`.
type Device struct {
	Name      string `json:"name"`
	URI       string `json:"uri"`
	Kind      string `json:"kind"` // printer / scanner
	Transport string `json:"transport"`
	Model     string `json:"model"`
	DeviceID  string `json:"device_id"`
}

var deviceLineRE = regexp.MustCompile(`^(direct|network|serial|parallel)\s+(\S+)(?:\s+(.*))?$`)

func parseDeviceLines(out string) []Device {
	var devices []Device
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		m := deviceLineRE.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		d := Device{URI: m[2], Transport: m[1], Name: m[2], Kind: "printer"}
		// `lpinfo -v` also prints bare backend names such as "network ipp"
		// (a protocol capability, not a device). Keep only real URIs.
		if !strings.Contains(d.URI, "://") {
			continue
		}
		if d.Transport == "direct" && strings.HasPrefix(d.URI, "usb://") {
			d.Name = strings.TrimPrefix(d.URI, "usb://")
			if idx := strings.Index(d.Name, "?"); idx > 0 {
				d.Name = d.Name[:idx]
			}
		}
		devices = append(devices, d)
	}
	return devices
}

func (c *Client) ListDevices(ctx context.Context) ([]Device, error) {
	out, stderr, err := c.command(ctx, "lpinfo", "-v")
	if err != nil {
		return nil, fmt.Errorf("lpinfo -v failed: %s: %w", strings.TrimSpace(stderr), err)
	}
	return parseDeviceLines(out), nil
}

// Scanner is a SANE scanner.
type Scanner struct {
	Name string `json:"name"`
	URI  string `json:"uri"`
}

var scanLineRE = regexp.MustCompile("device `([^']+)' is a (.+)")

func parseScannerLines(out string) []Scanner {
	var scanners []Scanner
	for _, line := range strings.Split(out, "\n") {
		m := scanLineRE.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		scanners = append(scanners, Scanner{Name: strings.TrimSpace(m[2]), URI: strings.TrimSpace(m[1])})
	}
	return scanners
}

func (c *Client) ListScanners(ctx context.Context) ([]Scanner, error) {
	out, stderr, err := c.command(ctx, "scanimage", "-L")
	if err != nil {
		return nil, fmt.Errorf("scanimage -L failed: %s: %w", strings.TrimSpace(stderr), err)
	}
	return parseScannerLines(out), nil
}

// CUPSConfig is the subset of cupsd.conf state surfaced by the admin console.
type CUPSConfig struct {
	ServerName         string `json:"server_name"`
	Ports              []int  `json:"ports"`
	WebInterface       bool   `json:"web_interface"`
	DefaultShared      bool   `json:"default_shared"`
	Browsing           bool   `json:"browsing"`
	LogLevel           string `json:"log_level"`
	MaxJobs            int    `json:"max_jobs"`
	PreserveJobHistory bool   `json:"preserve_job_history"`
	PreserveJobFiles   bool   `json:"preserve_job_files"`
}

// SetPPDOptions applies default options to a queue.
func (c *Client) SetPPDOptions(ctx context.Context, queue string, opts map[string]string) error {
	args := []string{"-p", queue}
	for k, v := range opts {
		args = append(args, "-o", k+"="+v)
	}
	_, stderr, err := c.command(ctx, "lpoptions", args...)
	return wrapErr("lpoptions", stderr, err)
}

// HoldJob holds a CUPS job.
func (c *Client) HoldJob(ctx context.Context, jobID string) error {
	_, stderr, err := c.command(ctx, "lp", "-i", jobID, "-H", "hold")
	return wrapErr("lp -H hold", stderr, err)
}

// ReleaseJob releases a held CUPS job.
func (c *Client) ReleaseJob(ctx context.Context, jobID string) error {
	_, stderr, err := c.command(ctx, "lp", "-i", jobID, "-H", "resume")
	return wrapErr("lp -H resume", stderr, err)
}

// RestartJob restarts a CUPS job.
func (c *Client) RestartJob(ctx context.Context, jobID string) error {
	_, stderr, err := c.command(ctx, "lp", "-i", jobID, "-H", "restart")
	return wrapErr("lp -H restart", stderr, err)
}

// ValidateConfig checks a cupsd.conf file with `cupsd -t`.
func (c *Client) ValidateConfig(ctx context.Context, path string) error {
	_, stderr, err := c.runner.Run(ctx, "cupsd", "-t", "-c", path)
	if err != nil {
		return fmt.Errorf("cupsd -t failed: %s: %w", strings.TrimSpace(stderr), err)
	}
	return nil
}

// Reload asks cupsd to reload its configuration.
func (c *Client) Reload(ctx context.Context) error {
	_, stderr, err := c.runner.Run(ctx, "pkill", "-HUP", "cupsd")
	if err != nil {
		return fmt.Errorf("reload cupsd failed: %s: %w", strings.TrimSpace(stderr), err)
	}
	return nil
}

// SetServerOption toggles a cupsctl server option.
func (c *Client) SetServerOption(ctx context.Context, option string, enabled bool) error {
	flag := "--" + option
	if !enabled {
		flag = "--no-" + option
	}
	_, stderr, err := c.runner.Run(ctx, "cupsctl", flag)
	if err != nil {
		return fmt.Errorf("cupsctl %s failed: %s: %w", flag, strings.TrimSpace(stderr), err)
	}
	return nil
}
