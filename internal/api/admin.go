package api

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/liuzhaojun/print-portal/internal/cups"
	"github.com/liuzhaojun/print-portal/internal/printer"
	"github.com/liuzhaojun/print-portal/internal/storage"
)

func (s *Server) handleOverview(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	version, _ := s.cups.Version(ctx)
	running, _ := s.cups.SchedulerRunning(ctx)
	printers, _ := s.store.ListPrinters()
	jobs, _ := s.store.ListJobs(storage.JobFilter{Limit: 500})
	active := 0
	completedToday := 0
	today := time.Now().Format("2006-01-02")
	for _, j := range jobs {
		switch j.Status {
		case "pending", "converting", "submitted", "queued", "printing":
			active++
		case "completed":
			if strings.HasPrefix(j.UpdatedAt, today) {
				completedToday++
			}
		}
	}
	defaultPrinter := ""
	for _, p := range printers {
		if p.IsDefault {
			defaultPrinter = p.Name
		}
	}
	state := "stopped"
	if running {
		state = "running"
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"cups": map[string]any{
			"version": version, "state": state, "server_name": s.cfg.CUPSServer,
			"ports": []int{8631}, "web_interface": true, "share_printers": true,
		},
		"printers":             len(printers),
		"active_jobs":          active,
		"completed_jobs_today": completedToday,
		"default_printer":      defaultPrinter,
	})
}

type adminPrinter struct {
	storage.Printer
	State        string   `json:"state"`
	StateReasons []string `json:"state_reasons"`
	Accepting    bool     `json:"accepting"`
	QueueLength  int      `json:"queue_length"`
	PPDAddress   string   `json:"ppd_address"`
	RAWAddress   string   `json:"raw_address"`
}

func (s *Server) adminPrinterList(ctx context.Context) ([]adminPrinter, error) {
	printers, err := s.store.ListPrinters()
	if err != nil {
		return nil, err
	}
	statuses, _ := s.cups.ListPrinters(ctx)
	byQueue := map[string]cups.PrinterStatus{}
	for _, st := range statuses {
		byQueue[st.Name] = st
	}
	active, _ := s.cups.ListActiveJobs(ctx)
	queueLen := map[string]int{}
	for _, cj := range active {
		queueLen[cj.Queue]++
	}
	out := make([]adminPrinter, 0, len(printers))
	for _, p := range printers {
		st := byQueue[p.PPDQueue]
		// If only the RAW queue exists, show a degraded state instead of
		// "unknown"; web printing needs the PPD queue to be present.
		if st.State == "" {
			if raw, ok := byQueue[p.RAWQueue]; ok {
				st = raw
				st.State = "degraded"
				st.StateReasons = append(st.StateReasons, "PPD 队列缺失")
			}
		}
		ap := adminPrinter{
			Printer: p, State: st.State, StateReasons: st.StateReasons, Accepting: st.Accepting,
			QueueLength: queueLen[p.PPDQueue] + queueLen[p.RAWQueue],
			PPDAddress:  fmt.Sprintf("ipp://%s/printers/%s", displayServer(s.cfg.CUPSServer), p.PPDQueue),
			RAWAddress:  fmt.Sprintf("ipp://%s/printers/%s", displayServer(s.cfg.CUPSServer), p.RAWQueue),
		}
		if ap.State == "" {
			ap.State = "unknown"
		}
		out = append(out, ap)
	}
	return out, nil
}

func displayServer(s string) string {
	if strings.HasPrefix(s, "127.0.0.1") || strings.HasPrefix(s, "localhost") {
		return "192.168.2.30:8631"
	}
	return s
}

func (s *Server) handleAdminPrinters(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		list, err := s.adminPrinterList(ctx)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal_error", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"printers": list})
	case http.MethodPost:
		var in printer.CreateInput
		if err := decodeJSON(r, &in); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
			return
		}
		p, err := s.printers.Create(r.Context(), in)
		if err != nil {
			writeError(w, http.StatusConflict, "create_failed", err.Error())
			return
		}
		writeJSON(w, http.StatusCreated, p)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
	}
}

func (s *Server) handleAdminPrinter(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	switch r.Method {
	case http.MethodGet:
		p, err := s.store.GetPrinterByName(name)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal_error", err.Error())
			return
		}
		if p == nil {
			writeError(w, http.StatusNotFound, "not_found", "printer not found")
			return
		}
		writeJSON(w, http.StatusOK, p)
	case http.MethodPut:
		var in printer.CreateInput
		if err := decodeJSON(r, &in); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
			return
		}
		p, err := s.printers.Update(r.Context(), name, in)
		if err != nil {
			writeError(w, http.StatusConflict, "update_failed", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, p)
	case http.MethodDelete:
		if err := s.printers.Delete(r.Context(), name); err != nil {
			writeError(w, http.StatusConflict, "delete_failed", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
	default:
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
	}
}

func (s *Server) handlePrinterAction(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	p, err := s.store.GetPrinterByName(name)
	if err != nil || p == nil {
		writeError(w, http.StatusNotFound, "not_found", "printer not found")
		return
	}
	action := strings.TrimPrefix(r.URL.Path, "/api/admin/printers/"+name+"/")
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	queues := []string{p.PPDQueue, p.RAWQueue}
	switch action {
	case "pause":
		for _, q := range queues {
			if err := s.cups.Pause(ctx, q); err != nil {
				writeError(w, http.StatusConflict, "pause_failed", err.Error())
				return
			}
		}
	case "resume":
		for _, q := range queues {
			if err := s.cups.Resume(ctx, q); err != nil {
				writeError(w, http.StatusConflict, "resume_failed", err.Error())
				return
			}
		}
	case "accept":
		for _, q := range queues {
			if err := s.cups.Accept(ctx, q); err != nil {
				writeError(w, http.StatusConflict, "accept_failed", err.Error())
				return
			}
		}
	case "reject":
		for _, q := range queues {
			if err := s.cups.Reject(ctx, q); err != nil {
				writeError(w, http.StatusConflict, "reject_failed", err.Error())
				return
			}
		}
	case "purge":
		for _, q := range queues {
			if err := s.cups.Purge(ctx, q); err != nil {
				writeError(w, http.StatusConflict, "purge_failed", err.Error())
				return
			}
		}
	case "test":
		id, err := s.printers.TestPage(ctx, name)
		if err != nil {
			writeError(w, http.StatusConflict, "test_failed", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "submitted", "cups_job_id": id})
		return
	default:
		writeError(w, http.StatusNotFound, "not_found", "unknown action")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handlePPDOptions(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	p, err := s.store.GetPrinterByName(name)
	if err != nil || p == nil {
		writeError(w, http.StatusNotFound, "not_found", "printer not found")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	switch r.Method {
	case http.MethodGet:
		opts, err := s.cups.PPDOptions(ctx, p.PPDQueue)
		if err != nil {
			writeError(w, http.StatusConflict, "ppd_options_failed", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"options": opts})
	case http.MethodPut:
		var body struct {
			Options map[string]string `json:"options"`
		}
		if err := decodeJSON(r, &body); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
			return
		}
		if err := s.cups.SetPPDOptions(ctx, p.PPDQueue, body.Options); err != nil {
			writeError(w, http.StatusConflict, "ppd_options_failed", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	default:
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
	}
}

func (s *Server) handleDiscoveryPrinters(w http.ResponseWriter, r *http.Request) {
	devices, err := s.discovery.DiscoverPrinters(r.Context())
	if err != nil {
		writeError(w, http.StatusConflict, "discovery_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"devices": devices})
}

func (s *Server) handleDiscoveryScanStart(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Scope string `json:"scope"`
	}
	if r.Body != nil {
		_ = decodeJSON(r, &body)
	}
	id := s.discovery.StartScan(body.Scope)
	writeJSON(w, http.StatusAccepted, map[string]string{"scan_id": id, "status": "running"})
}

func (s *Server) handleDiscoveryScanStatus(w http.ResponseWriter, r *http.Request) {
	state, ok := s.discovery.ScanStatus(r.PathValue("id"))
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "scan not found")
		return
	}
	writeJSON(w, http.StatusOK, state)
}

func (s *Server) handleDiscoveryScanners(w http.ResponseWriter, r *http.Request) {
	devices, err := s.discovery.DiscoverScanners(r.Context())
	if err != nil {
		writeError(w, http.StatusConflict, "discovery_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"devices": devices})
}

func (s *Server) handleDiscoverAndCreate(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name      string `json:"name"`
		DeviceURI string `json:"device_uri"`
		Model     string `json:"model"`
		PPDQueue  string `json:"ppd_queue"`
		RAWQueue  string `json:"raw_queue"`
		IsDefault bool   `json:"is_default"`
	}
	if err := decodeJSON(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	if in.Name == "" {
		in.Name = "Printer"
	}
	slug := slugify(in.Name)
	if in.PPDQueue == "" {
		in.PPDQueue = slug + "-ppd"
	}
	if in.RAWQueue == "" {
		in.RAWQueue = slug + "-raw"
	}
	if in.Model == "" {
		lowerURI := strings.ToLower(in.DeviceURI)
		switch {
		case strings.HasPrefix(lowerURI, "ipp://") || strings.HasPrefix(lowerURI, "ipps://"):
			in.Model = "everywhere"
		case strings.Contains(lowerURI, "brother") && strings.Contains(lowerURI, "7060"):
			in.Model = "drv:///brlaser.drv/br7060d.ppd"
		case strings.Contains(lowerURI, "brother"):
			in.Model = "drv:///brlaser.drv/brlaser.ppd"
		default:
			writeError(w, http.StatusUnprocessableEntity, "model_required", "无法自动匹配驱动，请手动选择 PPD 型号")
			return
		}
	}
	p, err := s.printers.Create(r.Context(), printer.CreateInput{
		Name: in.Name, Model: in.Model, PPDQueue: in.PPDQueue, RAWQueue: in.RAWQueue,
		DeviceURI: in.DeviceURI, IsDefault: in.IsDefault, IsShared: true,
	})
	if err != nil {
		writeError(w, http.StatusConflict, "create_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, p)
}

func (s *Server) handleDrivers(w http.ResponseWriter, r *http.Request) {
	if err := s.drivers.Refresh(r.Context()); err != nil {
		writeError(w, http.StatusConflict, "driver_refresh_failed", err.Error())
		return
	}
	list, err := s.drivers.SortedDrivers()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"drivers": list})
}

func (s *Server) handleDriverInstall(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(200 << 20); err != nil {
		writeError(w, http.StatusRequestEntityTooLarge, "upload_too_large", err.Error())
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_file", "file is required")
		return
	}
	defer file.Close()
	name := sanitizeFilename(header.Filename)
	lower := strings.ToLower(name)
	if strings.HasSuffix(lower, ".ppd") {
		data := make([]byte, 0)
		buf := make([]byte, 64*1024)
		for {
			n, err := file.Read(buf)
			data = append(data, buf[:n]...)
			if err != nil {
				break
			}
		}
		d, err := s.drivers.InstallPPD(name, data)
		if err != nil {
			writeError(w, http.StatusUnprocessableEntity, "install_failed", err.Error())
			return
		}
		writeJSON(w, http.StatusCreated, d)
		return
	}
	tmpDir := filepath.Join(s.cfg.DataDir, "drivers", "tmp")
	_ = os.MkdirAll(tmpDir, 0o755)
	tmpPath := filepath.Join(tmpDir, fmt.Sprintf("%d-%s", time.Now().UnixNano(), name))
	out, err := os.Create(tmpPath)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "storage_error", err.Error())
		return
	}
	if _, err := out.ReadFrom(file); err != nil {
		out.Close()
		writeError(w, http.StatusInternalServerError, "storage_error", err.Error())
		return
	}
	out.Close()
	d, err := s.drivers.InstallPackage(r.Context(), name, tmpPath)
	if err != nil {
		_ = os.Remove(tmpPath)
		writeError(w, http.StatusUnprocessableEntity, "install_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, d)
}

func (s *Server) handleDriverUninstall(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_id", err.Error())
		return
	}
	if err := s.drivers.Uninstall(r.Context(), id); err != nil {
		writeError(w, http.StatusConflict, "uninstall_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (s *Server) handleDriverRefresh(w http.ResponseWriter, r *http.Request) {
	if err := s.drivers.Refresh(r.Context()); err != nil {
		writeError(w, http.StatusConflict, "refresh_failed", err.Error())
		return
	}
	list, _ := s.drivers.SortedDrivers()
	writeJSON(w, http.StatusOK, map[string]any{"drivers": list})
}

func (s *Server) handleDriverModels(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	brand := strings.TrimSpace(r.URL.Query().Get("brand"))
	idStr := r.PathValue("id")
	var targetDriver string
	if idStr != "" {
		id, err := strconv.ParseInt(idStr, 10, 64)
		if err == nil {
			if d, _ := s.store.GetDriver(id); d != nil {
				targetDriver = d.Name
			}
		}
	}
	models, err := s.drivers.SearchModels(r.Context(), q)
	if err != nil {
		writeError(w, http.StatusConflict, "models_failed", err.Error())
		return
	}
	filtered := make([]cups.Model, 0, len(models))
	for _, m := range models {
		if targetDriver != "" && !strings.EqualFold(m.Driver, targetDriver) {
			continue
		}
		if brand != "" && !strings.EqualFold(m.Brand, brand) {
			continue
		}
		filtered = append(filtered, m)
	}
	models = filtered
	total := len(models)
	limit := atoiDefault(r.URL.Query().Get("limit"), 800)
	if limit <= 0 || limit > 5000 {
		limit = 800
	}
	offset := atoiDefault(r.URL.Query().Get("offset"), 0)
	if offset < 0 {
		offset = 0
	}
	if offset > len(models) {
		offset = len(models)
	}
	page := models[offset:]
	truncated := false
	if len(page) > limit {
		page = page[:limit]
		truncated = true
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"models": page, "total": total, "offset": offset, "limit": limit, "truncated": truncated,
	})
}

func (s *Server) handleCUPSStatus(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	version, _ := s.cups.Version(ctx)
	running, _ := s.cups.SchedulerRunning(ctx)
	printers, _ := s.cups.ListPrinters(ctx)
	state := "stopped"
	if running {
		state = "running"
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"version": version, "state": state, "server_name": s.cfg.CUPSServer,
		"printers": printers,
	})
}

var defaultCUPSSettings = map[string]string{
	"web_interface":        "true",
	"share_printers":       "true",
	"browsing":             "false",
	"log_level":            "warn",
	"max_jobs":             "500",
	"preserve_job_history": "true",
	"preserve_job_files":   "false",
}

// cupsSettingDirective maps an API setting to a cupsd.conf directive/value.
func cupsSettingDirective(key, value string) (string, string, bool) {
	boolValue := "No"
	if value == "true" {
		boolValue = "Yes"
	}
	switch key {
	case "web_interface":
		return "WebInterface", boolValue, true
	case "share_printers":
		return "DefaultShared", boolValue, true
	case "browsing":
		onOff := "Off"
		if value == "true" {
			onOff = "On"
		}
		return "Browsing", onOff, true
	case "log_level":
		return "LogLevel", value, true
	case "max_jobs":
		return "MaxJobs", value, true
	case "preserve_job_history":
		return "PreserveJobHistory", boolValue, true
	case "preserve_job_files":
		return "PreserveJobFiles", boolValue, true
	}
	return "", "", false
}

// parseCupsdSettings overlays directives found in cupsd.conf onto defaults.
func parseCupsdSettings(content string, base map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range base {
		out[k] = v
	}
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		key := strings.ToLower(fields[0])
		val := strings.Join(fields[1:], " ")
		switch key {
		case "webinterface":
			out["web_interface"] = yesNoToBool(val)
		case "defaultshared":
			out["share_printers"] = yesNoToBool(val)
		case "browsing":
			out["browsing"] = yesNoToBool(val)
		case "loglevel":
			out["log_level"] = val
		case "maxjobs":
			out["max_jobs"] = val
		case "preservejobhistory":
			out["preserve_job_history"] = yesNoToBool(val)
		case "preservejobfiles":
			out["preserve_job_files"] = yesNoToBool(val)
		}
	}
	return out
}

func yesNoToBool(v string) string {
	if strings.EqualFold(v, "yes") || strings.EqualFold(v, "on") || strings.EqualFold(v, "true") {
		return "true"
	}
	return "false"
}

// updateCupsdDirective replaces a directive or appends it.
func updateCupsdDirective(content, directive, value string) string {
	lines := strings.Split(content, "\n")
	found := false
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		fields := strings.Fields(trimmed)
		if len(fields) > 0 && strings.EqualFold(fields[0], directive) {
			lines[i] = directive + " " + value
			found = true
		}
	}
	if !found {
		lines = append(lines, directive+" "+value)
	}
	return strings.Join(lines, "\n")
}

func (s *Server) handleCUPSSettings(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		out := map[string]string{}
		for k, def := range defaultCUPSSettings {
			out[k] = def
		}
		if body, err := os.ReadFile(s.cupsConfigPath()); err == nil {
			out = parseCupsdSettings(string(body), out)
		}
		for k := range defaultCUPSSettings {
			if v, ok, _ := s.store.GetSetting("cups_" + k); ok {
				out[k] = v
			}
		}
		writeJSON(w, http.StatusOK, out)
	case http.MethodPut:
		var body map[string]string
		if err := decodeJSON(r, &body); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
			return
		}
		path := s.cupsConfigPath()
		if old, err := os.ReadFile(path); err == nil {
			content := string(old)
			for k, v := range body {
				if directive, val, ok := cupsSettingDirective(k, v); ok {
					content = updateCupsdDirective(content, directive, val)
				}
			}
			tmp := filepath.Join(os.TempDir(), fmt.Sprintf("cupsd-settings-%d.conf", time.Now().UnixNano()))
			if err := os.WriteFile(tmp, []byte(content), 0o600); err != nil {
				writeError(w, http.StatusInternalServerError, "storage_error", err.Error())
				return
			}
			defer os.Remove(tmp)
			ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
			defer cancel()
			if err := s.cups.ValidateConfig(ctx, tmp); err != nil {
				writeError(w, http.StatusUnprocessableEntity, "invalid_config", err.Error())
				return
			}
			backupPath := path + ".bak." + time.Now().Format("20060102-150405")
			if err := os.WriteFile(backupPath, old, 0o600); err != nil {
				writeError(w, http.StatusInternalServerError, "backup_failed", err.Error())
				return
			}
			if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
				writeError(w, http.StatusInternalServerError, "storage_error", err.Error())
				return
			}
			if err := s.cups.Reload(ctx); err != nil {
				_ = os.WriteFile(path, old, 0o600)
				writeError(w, http.StatusConflict, "reload_failed", "配置已回滚："+err.Error())
				return
			}
		} else {
			// No writable cupsd.conf (e.g. dev host): apply toggles via cupsctl.
			for k, v := range body {
				if k == "web_interface" || k == "share_printers" || k == "browsing" {
					_ = s.cups.SetServerOption(r.Context(), k, v == "true")
				}
			}
		}
		for k, v := range body {
			if _, ok := defaultCUPSSettings[k]; !ok {
				continue
			}
			if err := s.store.SetSetting("cups_"+k, v); err != nil {
				writeError(w, http.StatusInternalServerError, "storage_error", err.Error())
				return
			}
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	default:
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
	}
}

func (s *Server) cupsConfigPath() string {
	if p := os.Getenv("CUPSD_CONF"); p != "" {
		return p
	}
	return "/etc/cups/cupsd.conf"
}

func (s *Server) handleCUPSConfig(w http.ResponseWriter, r *http.Request) {
	path := s.cupsConfigPath()
	switch r.Method {
	case http.MethodGet:
		body, err := os.ReadFile(path)
		if err != nil {
			writeError(w, http.StatusNotFound, "not_found", "cupsd.conf not available: "+err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"path": path, "content": string(body)})
	case http.MethodPut:
		var body struct {
			Content string `json:"content"`
		}
		if err := decodeJSON(r, &body); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
			return
		}
		if strings.TrimSpace(body.Content) == "" {
			writeError(w, http.StatusUnprocessableEntity, "invalid_config", "content is empty")
			return
		}
		tmp := filepath.Join(os.TempDir(), fmt.Sprintf("cupsd-%d.conf", time.Now().UnixNano()))
		if err := os.WriteFile(tmp, []byte(body.Content), 0o600); err != nil {
			writeError(w, http.StatusInternalServerError, "storage_error", err.Error())
			return
		}
		defer os.Remove(tmp)
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()
		if err := s.cups.ValidateConfig(ctx, tmp); err != nil {
			writeError(w, http.StatusUnprocessableEntity, "invalid_config", err.Error())
			return
		}
		old, readErr := os.ReadFile(path)
		backupPath := path + ".bak." + time.Now().Format("20060102-150405")
		if readErr == nil {
			if err := os.WriteFile(backupPath, old, 0o600); err != nil {
				writeError(w, http.StatusInternalServerError, "backup_failed", err.Error())
				return
			}
		}
		if err := os.WriteFile(path, []byte(body.Content), 0o600); err != nil {
			writeError(w, http.StatusInternalServerError, "storage_error", err.Error())
			return
		}
		if err := s.cups.Reload(ctx); err != nil {
			if readErr == nil {
				_ = os.WriteFile(path, old, 0o600)
			}
			writeError(w, http.StatusConflict, "reload_failed", "配置已回滚："+err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "saved", "backup": backupPath})
	default:
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
	}
}

func (s *Server) handleCUPSReload(w http.ResponseWriter, r *http.Request) {
	if err := s.cups.Reload(r.Context()); err != nil {
		writeError(w, http.StatusConflict, "reload_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "reloaded"})
}

func tailFile(path string, lines int) ([]string, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	all := splitLines(string(body))
	if lines <= 0 || lines > len(all) {
		lines = len(all)
	}
	return all[len(all)-lines:], nil
}

func (s *Server) handleCUPSLogs(w http.ResponseWriter, r *http.Request) {
	path := os.Getenv("CUPS_ERROR_LOG")
	if path == "" {
		path = "/var/log/cups/error_log"
	}
	lines := atoiDefault(r.URL.Query().Get("lines"), 200)
	out, err := tailFile(path, lines)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "CUPS log not available: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"path": path, "lines": out})
}

func (s *Server) handleAdminJobs(w http.ResponseWriter, r *http.Request) {
	jobs, err := s.store.ListJobs(storage.JobFilter{
		Status: r.URL.Query().Get("status"),
		Source: r.URL.Query().Get("source"),
		Limit:  atoiDefault(r.URL.Query().Get("limit"), 200),
		Offset: atoiDefault(r.URL.Query().Get("offset"), 0),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"jobs": s.enrichJobs(jobs)})
}

func (s *Server) handleAdminJobAction(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_id", err.Error())
		return
	}
	action := ""
	for _, a := range []string{"cancel", "hold", "release", "restart"} {
		if strings.HasSuffix(r.URL.Path, "/"+a) {
			action = a
			break
		}
	}
	j, err := s.store.GetJob(id)
	if err != nil || j == nil {
		writeError(w, http.StatusNotFound, "not_found", "job not found")
		return
	}
	switch action {
	case "cancel":
		if err := s.jobs.Cancel(r.Context(), id); err != nil {
			writeError(w, http.StatusConflict, "cancel_failed", err.Error())
			return
		}
	case "hold":
		if j.CupsJobID == "" {
			writeError(w, http.StatusConflict, "hold_failed", "job has no CUPS id")
			return
		}
		if err := s.cups.HoldJob(r.Context(), j.CupsJobID); err != nil {
			writeError(w, http.StatusConflict, "hold_failed", err.Error())
			return
		}
	case "release":
		if j.CupsJobID == "" {
			writeError(w, http.StatusConflict, "release_failed", "job has no CUPS id")
			return
		}
		if err := s.cups.ReleaseJob(r.Context(), j.CupsJobID); err != nil {
			writeError(w, http.StatusConflict, "release_failed", err.Error())
			return
		}
	case "restart":
		if j.CupsJobID == "" {
			writeError(w, http.StatusConflict, "restart_failed", "job has no CUPS id")
			return
		}
		if err := s.cups.RestartJob(r.Context(), j.CupsJobID); err != nil {
			writeError(w, http.StatusConflict, "restart_failed", err.Error())
			return
		}
	default:
		writeError(w, http.StatusNotFound, "not_found", "unknown action")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

var allowedSettings = map[string]string{
	"max_upload_mb":        "200",
	"max_job_pages":        "100",
	"max_job_mb":           "100",
	"temp_retention_hours": "24",
	"job_history_days":     "30",
	"default_printer_id":   "0",
	"language":             "zh",
	"theme":                "auto",
	"public_host":          "",
	"cups_port":            "8631",
	"web_port":             "8088",
}

func (s *Server) handleSettings(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		out := map[string]string{}
		for k, def := range allowedSettings {
			v, ok, _ := s.store.GetSetting(k)
			if !ok {
				v = def
			}
			out[k] = v
		}
		writeJSON(w, http.StatusOK, out)
	case http.MethodPut:
		var body map[string]string
		if err := decodeJSON(r, &body); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
			return
		}
		for k, v := range body {
			if _, ok := allowedSettings[k]; !ok {
				continue
			}
			if err := s.store.SetSetting(k, v); err != nil {
				writeError(w, http.StatusInternalServerError, "storage_error", err.Error())
				return
			}
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	default:
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
	}
}

func (s *Server) handleLogs(w http.ResponseWriter, r *http.Request) {
	kind := r.URL.Query().Get("kind")
	lines := atoiDefault(r.URL.Query().Get("lines"), 200)
	var path string
	switch kind {
	case "cups":
		path = os.Getenv("CUPS_ERROR_LOG")
		if path == "" {
			path = "/var/log/cups/error_log"
		}
	default:
		path = filepath.Join(s.cfg.DataDir, "logs", "printd.log")
	}
	out, err := tailFile(path, lines)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "log not available: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"path": path, "lines": out})
}

func (s *Server) handleBackup(w http.ResponseWriter, r *http.Request) {
	writeError(w, http.StatusNotImplemented, "not_implemented", "backup/restore is planned for P1.1")
}

// sortedKeys is a helper kept for deterministic API responses.
func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
