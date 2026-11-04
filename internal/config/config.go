package config

import "os"

// Config is the P1 runtime configuration.
type Config struct {
	Addr        string
	CUPSServer  string
	AdminToken  string
	MaxUploadMB int
}

func Load() Config {
	return Config{
		Addr:        env("PRINTD_ADDR", ":8088"),
		CUPSServer:  env("CUPS_SERVER", "127.0.0.1:8631"),
		AdminToken:  env("ADMIN_TOKEN", ""),
		MaxUploadMB: 200,
	}
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
