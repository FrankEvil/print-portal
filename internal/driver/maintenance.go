package driver

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// MaintenanceAction is one vendor-provided maintenance command declared in
// a driver's maintenance.json manifest.
type MaintenanceAction struct {
	ID          string   `json:"id"`
	Label       string   `json:"label"`
	Description string   `json:"description"`
	Command     []string `json:"command"`
	Timeout     int      `json:"timeout"`
	Confirm     bool     `json:"confirm"`
}

type maintenanceManifest struct {
	Version int                 `json:"version"`
	Actions []MaintenanceAction `json:"actions"`
}

// MaintenanceCapabilities describes what a driver can expose to the UI.
type MaintenanceCapabilities struct {
	Driver  string              `json:"driver"`
	Status  string              `json:"status"` // ready | none | parsing
	Message string              `json:"message,omitempty"`
	Actions []MaintenanceAction `json:"actions"`
}

type capabilityCache struct {
	Driver   string              `json:"driver"`
	Status   string              `json:"status"`
	Message  string              `json:"message,omitempty"`
	Actions  []MaintenanceAction `json:"actions"`
	ParsedAt string              `json:"parsed_at"`
}

var maintenanceID = regexp.MustCompile(`^[a-z0-9_-]+$`)

// MaintenanceCapabilities returns cached capabilities. If the driver has not
// been parsed yet, the caller should call EnsureCapabilities asynchronously.
func (s *Service) MaintenanceCapabilities(ctx context.Context, model string) *MaintenanceCapabilities {
	if cached := s.loadCapabilityCache(model); cached != nil {
		return cached
	}
	return &MaintenanceCapabilities{Status: "parsing", Message: "正在解析驱动维护能力"}
}

// EnsureCapabilities parses the selected driver once and caches the result.
func (s *Service) EnsureCapabilities(ctx context.Context, model string) *MaintenanceCapabilities {
	s.capMu.Lock()
	defer s.capMu.Unlock()
	if cached := s.loadCapabilityCache(model); cached != nil {
		return cached
	}
	driverName, actions := s.resolveManifest(ctx, model)
	status := "none"
	message := "当前驱动未提供维护接口"
	if len(actions) > 0 {
		status = "ready"
		message = ""
	}
	caps := &MaintenanceCapabilities{Driver: driverName, Status: status, Message: message, Actions: actions}
	cache := capabilityCache{Driver: driverName, Status: status, Message: message, Actions: actions, ParsedAt: time.Now().Format(time.RFC3339)}
	if data, err := json.Marshal(cache); err == nil {
		dir := filepath.Join(s.dataDir, "drivers", "capabilities")
		if err := os.MkdirAll(dir, 0o755); err == nil {
			_ = os.WriteFile(filepath.Join(dir, sanitizeName(model)+".json"), data, 0o644)
		}
	}
	return caps
}

func (s *Service) loadCapabilityCache(model string) *MaintenanceCapabilities {
	path := filepath.Join(s.dataDir, "drivers", "capabilities", sanitizeName(model)+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var cache capabilityCache
	if json.Unmarshal(data, &cache) != nil {
		return nil
	}
	return &MaintenanceCapabilities{Driver: cache.Driver, Status: cache.Status, Message: cache.Message, Actions: cache.Actions}
}

// resolveManifest finds and parses maintenance.json for the driver behind a PPD model.
func (s *Service) resolveManifest(ctx context.Context, model string) (string, []MaintenanceAction) {
	driverName := ""
	models, err := s.Models(ctx)
	if err == nil {
		for _, m := range models {
			if m.Name == model || m.Description == model {
				driverName = m.Driver
				break
			}
		}
	}
	candidates := []string{driverName, model, strings.TrimPrefix(driverName, "ppd/")}
	for _, candidate := range candidates {
		if strings.TrimSpace(candidate) == "" {
			continue
		}
		path := filepath.Join(s.dataDir, "drivers", "maintenance", sanitizeName(candidate)+".json")
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var manifest maintenanceManifest
		if err := json.Unmarshal(data, &manifest); err != nil {
			continue
		}
		var actions []MaintenanceAction
		for _, action := range manifest.Actions {
			if validMaintenanceAction(action) {
				actions = append(actions, action)
			}
		}
		if len(actions) > 0 {
			return candidate, actions
		}
	}
	return driverName, nil
}

// RunMaintenance executes one whitelisted action from the driver manifest.
func (s *Service) RunMaintenance(ctx context.Context, model, actionID string) (string, error) {
	caps := s.MaintenanceCapabilities(ctx, model)
	if caps.Status != "ready" {
		caps = s.EnsureCapabilities(ctx, model)
	}
	for _, action := range caps.Actions {
		if action.ID != actionID {
			continue
		}
		timeout := action.Timeout
		if timeout <= 0 || timeout > 300 {
			timeout = 60
		}
		runCtx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
		defer cancel()
		cmd := exec.CommandContext(runCtx, action.Command[0], action.Command[1:]...)
		var output bytes.Buffer
		cmd.Stdout = &output
		cmd.Stderr = &output
		err := cmd.Run()
		text := strings.TrimSpace(output.String())
		if err != nil {
			if runCtx.Err() == context.DeadlineExceeded {
				return text, fmt.Errorf("维护命令超时（%d 秒）", timeout)
			}
			return text, fmt.Errorf("维护命令失败: %w", err)
		}
		return text, nil
	}
	return "", fmt.Errorf("驱动未提供维护动作 %q", actionID)
}

func validMaintenanceAction(action MaintenanceAction) bool {
	if !maintenanceID.MatchString(action.ID) || strings.TrimSpace(action.Label) == "" || len(action.Command) == 0 {
		return false
	}
	if strings.ContainsAny(action.Command[0], " \t\n;&|`$") {
		return false
	}
	return true
}

// saveMaintenanceManifest copies an optional maintenance.json from a driver
// package into the persistent manifest directory.
func (s *Service) saveMaintenanceManifest(driverName, sourceDir string) error {
	source := filepath.Join(sourceDir, "maintenance.json")
	if _, err := os.Stat(source); err != nil {
		var found string
		_ = filepath.Walk(sourceDir, func(path string, info os.FileInfo, walkErr error) error {
			if walkErr == nil && !info.IsDir() && info.Name() == "maintenance.json" && found == "" {
				found = path
			}
			return nil
		})
		source = found
	}
	data, err := os.ReadFile(source)
	if err != nil {
		return nil // optional
	}
	var manifest maintenanceManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return fmt.Errorf("invalid maintenance.json: %w", err)
	}
	valid := manifest.Actions[:0]
	for _, action := range manifest.Actions {
		if validMaintenanceAction(action) {
			valid = append(valid, action)
		}
	}
	manifest.Actions = valid
	data, err = json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	dir := filepath.Join(s.dataDir, "drivers", "maintenance")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, sanitizeName(driverName)+".json"), data, 0o644)
}
