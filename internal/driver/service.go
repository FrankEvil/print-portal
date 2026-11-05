package driver

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/liuzhaojun/print-portal/internal/cups"
	"github.com/liuzhaojun/print-portal/internal/storage"
)

// Service manages printer drivers, PPD files and the model index.
type Service struct {
	store   *storage.Store
	cups    *cups.Client
	dataDir string

	mu       sync.Mutex
	models   []cups.Model
	cachedAt time.Time
}

func NewService(store *storage.Store, cupsClient *cups.Client, dataDir string) *Service {
	return &Service{store: store, cups: cupsClient, dataDir: dataDir}
}

// Models returns the cached model index, refreshing when stale.
func (s *Service) Models(ctx context.Context) ([]cups.Model, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.models) > 0 && time.Since(s.cachedAt) < 30*time.Second {
		return s.models, nil
	}
	models, err := s.cups.ListModels(ctx)
	if err != nil {
		return nil, err
	}
	s.models = models
	s.cachedAt = time.Now()
	return models, nil
}

// SearchModels filters the model index by keyword.
func (s *Service) SearchModels(ctx context.Context, query string) ([]cups.Model, error) {
	models, err := s.Models(ctx)
	if err != nil {
		return nil, err
	}
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		out := make([]cups.Model, 0, len(models))
		out = append(out, models...)
		return out, nil
	}
	var out []cups.Model
	for _, m := range models {
		if strings.Contains(strings.ToLower(m.Name), q) || strings.Contains(strings.ToLower(m.Description), q) {
			out = append(out, m)
		}
	}
	return out, nil
}

// Refresh recomputes driver states from the installed packages and model index.
func (s *Service) Refresh(ctx context.Context) error {
	models, err := s.Models(ctx)
	if err != nil {
		return err
	}
	counts := map[string]int{}
	for _, m := range models {
		counts[m.Driver]++
	}
	builtins := []struct {
		name    string
		version string
		license string
	}{
		{"brlaser", packageVersion("printer-driver-brlaser"), "GPL-2.0"},
		{"foomatic", packageVersion("foomatic-db"), "GPL-2.0"},
	}
	for _, b := range builtins {
		state := "missing"
		if counts[b.name] > 0 {
			// foomatic is only a model index: PPDs may reference filters from
			// third-party driver packages that are not installed. Never claim
			// it is fully ready.
			if b.name == "foomatic" {
				state = "degraded"
			} else {
				state = "ready"
			}
		} else if b.name == "foomatic" {
			if _, err := os.Stat("/usr/share/foomatic"); err == nil {
				state = "degraded"
			}
		}
		d := &storage.Driver{
			Name:        b.name,
			Version:     b.version,
			Kind:        "builtin",
			Source:      "image",
			State:       state,
			ModelsCount: counts[b.name],
			License:     b.license,
		}
		if err := s.store.UpsertDriver(d); err != nil {
			return err
		}
	}
	if counts["everywhere"] > 0 {
		_ = s.store.UpsertDriver(&storage.Driver{
			Name: "ipp-everywhere", Version: "cups", Kind: "builtin", Source: "image",
			State: "ready", ModelsCount: counts["everywhere"], License: "LGPL-2.0",
		})
	}

	// Re-evaluate uploaded drivers.
	drivers, err := s.store.ListDrivers()
	if err != nil {
		return err
	}
	for i := range drivers {
		d := &drivers[i]
		if d.Kind == "builtin" {
			continue
		}
		if _, err := os.Stat(d.PackagePath); err != nil && d.PackagePath != "" && d.Kind == "package" {
			d.State = "missing"
			_ = s.store.UpsertDriver(d)
		}
	}
	return nil
}

func packageVersion(pkg string) string {
	out, err := exec.Command("dpkg-query", "-W", "-f=${Version}", pkg).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// InstallPPD validates and registers an uploaded PPD file.
func (s *Service) InstallPPD(name string, content []byte) (*storage.Driver, error) {
	if len(content) == 0 {
		return nil, fmt.Errorf("empty PPD file")
	}
	if !strings.Contains(string(content), "*PPD-Adobe") {
		return nil, fmt.Errorf("not a valid PPD file (missing *PPD-Adobe header)")
	}
	base := sanitizeName(strings.TrimSuffix(name, filepath.Ext(name)))
	dir := filepath.Join(s.dataDir, "drivers", "ppd")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	dst := filepath.Join(dir, base+".ppd")
	if err := os.WriteFile(dst, content, 0o644); err != nil {
		return nil, err
	}
	state := checkPPDFilters(string(content))
	d := &storage.Driver{
		Name:        base,
		Version:     "uploaded",
		Kind:        "ppd",
		Source:      "upload",
		PackagePath: dst,
		PPDDir:      dir,
		State:       state,
		ModelsCount: 1,
		License:     "unknown",
	}
	if err := s.store.UpsertDriver(d); err != nil {
		return nil, err
	}
	return d, nil
}

var cupsFilterRE = regexp.MustCompile(`(?m)^\*cupsFilter2?:\s*"[^"]*?\s+(\S+)\s*"`)

// checkPPDFilters returns ready when every filter referenced by the PPD exists.
func checkPPDFilters(ppd string) string {
	matches := cupsFilterRE.FindAllStringSubmatch(ppd, -1)
	if len(matches) == 0 {
		return "ready"
	}
	for _, m := range matches {
		name := m[1]
		if name == "-" || name == "0" {
			continue // no filter / raw passthrough
		}
		if strings.HasPrefix(name, "/") {
			if _, err := os.Stat(name); err != nil {
				return "degraded"
			}
			continue
		}
		if !filterExists(name) {
			return "degraded"
		}
	}
	return "ready"
}

func filterExists(name string) bool {
	dirs := []string{"/usr/lib/cups/filter", "/usr/local/lib/cups/filter", "/opt/print-portal/filters"}
	for _, dir := range dirs {
		if _, err := os.Stat(filepath.Join(dir, name)); err == nil {
			return true
		}
	}
	_, err := exec.LookPath(name)
	return err == nil
}

// InstallPackage installs an uploaded .deb or .tar.gz driver package.
func (s *Service) InstallPackage(ctx context.Context, filename string, tmpPath string) (*storage.Driver, error) {
	lower := strings.ToLower(filename)
	switch {
	case strings.HasSuffix(lower, ".deb"):
		if _, err := exec.LookPath("dpkg"); err != nil {
			return nil, fmt.Errorf("dpkg is not available on this host")
		}
		name := sanitizeName(strings.TrimSuffix(filename, ".deb"))
		// Keep the package on the persistent volume so it can be replayed
		// after container rebuilds.
		storeDir := filepath.Join(s.dataDir, "drivers", "packages")
		if err := os.MkdirAll(storeDir, 0o755); err != nil {
			return nil, err
		}
		persistentPath := filepath.Join(storeDir, name+".deb")
		if err := copyFile(tmpPath, persistentPath); err != nil {
			return nil, err
		}
		if out, err := exec.CommandContext(ctx, "dpkg", "-i", persistentPath).CombinedOutput(); err != nil {
			return nil, fmt.Errorf("dpkg -i failed: %s", strings.TrimSpace(string(out)))
		}
		// Optional maintenance manifest can live inside the .deb.
		if tmpDir, err := os.MkdirTemp("", "print-portal-driver-"); err == nil {
			if _, err := exec.CommandContext(ctx, "dpkg-deb", "-x", persistentPath, tmpDir).CombinedOutput(); err == nil {
				_ = s.saveMaintenanceManifest(name, tmpDir)
			}
			_ = os.RemoveAll(tmpDir)
		}
		d := &storage.Driver{Name: name, Version: "deb", Kind: "package", Source: "upload", PackagePath: persistentPath, State: "ready"}
		if err := s.store.UpsertDriver(d); err != nil {
			return nil, err
		}
		_ = s.Refresh(ctx)
		return d, nil
	case strings.HasSuffix(lower, ".tar.gz") || strings.HasSuffix(lower, ".tgz"):
		return s.installTarGz(filename, tmpPath)
	default:
		return nil, fmt.Errorf("unsupported package type: %s (expect .deb or .tar.gz)", filename)
	}
}

// ReplayInstalled re-installs persisted .deb driver packages after a
// container rebuild. Best-effort: failures are reported but do not stop startup.
func (s *Service) ReplayInstalled(ctx context.Context) error {
	drivers, err := s.store.ListDrivers()
	if err != nil {
		return err
	}
	var firstErr error
	for _, d := range drivers {
		if d.Kind != "package" || !strings.HasSuffix(strings.ToLower(d.PackagePath), ".deb") {
			continue
		}
		if _, err := os.Stat(d.PackagePath); err != nil {
			continue
		}
		if out, err := exec.CommandContext(ctx, "dpkg", "-i", d.PackagePath).CombinedOutput(); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("replay %s: %s", d.Name, strings.TrimSpace(string(out)))
		}
	}
	return firstErr
}

func (s *Service) installTarGz(filename, tmpPath string) (*storage.Driver, error) {
	base := sanitizeName(strings.TrimSuffix(strings.TrimSuffix(filename, ".tar.gz"), ".tgz"))
	dest := filepath.Join(s.dataDir, "drivers", "packages", base)
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return nil, err
	}
	if err := extractTarGz(tmpPath, dest); err != nil {
		return nil, err
	}
	_ = s.saveMaintenanceManifest(base, dest)
	ppdDir := filepath.Join(dest, "ppd")
	filterDir := filepath.Join(dest, "filter")
	var ppdCount int
	entries, _ := os.ReadDir(ppdDir)
	for _, e := range entries {
		if strings.HasSuffix(strings.ToLower(e.Name()), ".ppd") {
			ppdCount++
		}
	}
	state := "ready"
	if ppdCount == 0 {
		state = "missing"
	} else {
		// Copy filters into the CUPS filter directory when possible.
		if filters, err := os.ReadDir(filterDir); err == nil {
			for _, f := range filters {
				src := filepath.Join(filterDir, f.Name())
				dst := filepath.Join("/usr/lib/cups/filter", f.Name())
				if err := copyFile(src, dst); err != nil {
					state = "degraded"
				}
			}
		}
		// Validate referenced filters for each PPD.
		for _, e := range entries {
			if strings.HasSuffix(strings.ToLower(e.Name()), ".ppd") {
				body, err := os.ReadFile(filepath.Join(ppdDir, e.Name()))
				if err == nil && checkPPDFilters(string(body)) == "degraded" {
					state = "degraded"
				}
			}
		}
	}
	d := &storage.Driver{
		Name: base, Version: "tar.gz", Kind: "package", Source: "upload",
		PackagePath: dest, PPDDir: ppdDir, FilterDir: filterDir, State: state, ModelsCount: ppdCount, License: "unknown",
	}
	if err := s.store.UpsertDriver(d); err != nil {
		return nil, err
	}
	return d, nil
}

func extractTarGz(src, dest string) error {
	f, err := os.Open(src)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		target := filepath.Join(dest, filepath.Clean(hdr.Name))
		if !strings.HasPrefix(target, filepath.Clean(dest)+string(os.PathSeparator)) {
			return fmt.Errorf("unsafe path in archive: %s", hdr.Name)
		}
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			out, err := os.Create(target)
			if err != nil {
				return err
			}
			if _, err := io.Copy(out, tr); err != nil {
				out.Close()
				return err
			}
			out.Close()
		}
	}
	return nil
}

// Uninstall removes a non-builtin driver.
func (s *Service) Uninstall(ctx context.Context, id int64) error {
	d, err := s.store.GetDriver(id)
	if err != nil {
		return err
	}
	if d == nil {
		return fmt.Errorf("driver not found")
	}
	if d.Kind == "builtin" {
		return fmt.Errorf("builtin drivers cannot be uninstalled")
	}
	printers, err := s.store.ListPrinters()
	if err != nil {
		return err
	}
	for _, p := range printers {
		if d.PPDDir != "" && strings.HasPrefix(p.PPDFile, d.PPDDir) {
			return fmt.Errorf("driver is in use by printer %q", p.Name)
		}
		if d.Name != "" && strings.EqualFold(p.Model, d.Name) {
			return fmt.Errorf("driver is in use by printer %q", p.Name)
		}
	}
	switch {
	case d.Kind == "package" && strings.HasSuffix(d.PackagePath, ".deb"):
		out, err := exec.CommandContext(ctx, "dpkg", "-r", d.Name).CombinedOutput()
		if err != nil {
			return fmt.Errorf("dpkg -r failed: %s", strings.TrimSpace(string(out)))
		}
	case d.PackagePath != "":
		if err := os.RemoveAll(d.PackagePath); err != nil {
			return err
		}
	}
	return s.store.DeleteDriver(d.ID)
}

// SortedDrivers returns drivers sorted by name.
func (s *Service) SortedDrivers() ([]storage.Driver, error) {
	drivers, err := s.store.ListDrivers()
	if err != nil {
		return nil, err
	}
	sort.Slice(drivers, func(i, j int) bool { return drivers[i].Name < drivers[j].Name })
	return drivers, nil
}

func sanitizeName(name string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(name) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_' || r == '.' {
			b.WriteRune(r)
		} else {
			b.WriteRune('_')
		}
	}
	out := strings.Trim(b.String(), "._-")
	if out == "" {
		out = "driver"
	}
	return out
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}
