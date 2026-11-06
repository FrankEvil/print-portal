package config

import (
	"os"
	"strconv"
	"strings"
)

// Config is the P1 runtime configuration.
type Config struct {
	Addr        string
	AppName     string
	DataDir     string
	StaticDir   string
	AdminToken  string
	AuthMode    string
	SocketPath  string
	FnOSPrefix  string
	FnOSBaseURL string
	FnOSPort    string
	PublicHost  string
	NASRoots    string
	MaxUploadMB int
	CUPSServer  string
	IPPAddr     string
	Version     string
}

// Load reads configuration from environment variables.
func Load() Config {
	cfg := Config{
		Addr:        env("PRINTD_ADDR", ":8088"),
		AppName:     env("PRINT_PORTAL_APP_NAME", "print-portal"),
		DataDir:     env("PRINT_PORTAL_DATA", "data"),
		StaticDir:   env("PRINT_PORTAL_STATIC", ""),
		AdminToken:  env("ADMIN_TOKEN", ""),
		AuthMode:    env("PRINT_PORTAL_AUTH_MODE", "token"),
		SocketPath:  env("PRINTD_SOCKET", ""),
		FnOSPrefix:  env("PRINT_PORTAL_FNOS_PREFIX", ""),
		FnOSBaseURL: env("PRINT_PORTAL_FNOS_BASE_URL", ""),
		FnOSPort:    env("PRINT_PORTAL_FNOS_PORT", ""),
		PublicHost:  env("PRINT_PORTAL_PUBLIC_HOST", ""),
		NASRoots:    env("PRINT_PORTAL_NAS_ROOTS", ""),
		MaxUploadMB: envInt("MAX_UPLOAD_MB", 200),
		CUPSServer:  env("CUPS_SERVER", "127.0.0.1:631"),
		IPPAddr:     env("PRINT_PORTAL_IPP_ADDR", "0.0.0.0:8631"),
		Version:     env("PRINT_PORTAL_VERSION", "0.1.0"),
	}
	if cfg.FnOSPrefix == "" {
		cfg.FnOSPrefix = "/app/" + cfg.AppName
	}
	return cfg
}

// EnsureDataDir creates the data directory if it does not exist.
func (c Config) EnsureDataDir() error {
	if err := os.MkdirAll(c.DataDir, 0o755); err != nil {
		return err
	}
	for _, sub := range []string{"uploads", "jobs", "scans", "logs", "drivers/packages", "drivers/ppd"} {
		if err := os.MkdirAll(strings.TrimRight(c.DataDir, "/")+"/"+sub, 0o755); err != nil {
			return err
		}
	}
	return nil
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envInt(key string, fallback int) int {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		return fallback
	}
	return n
}
