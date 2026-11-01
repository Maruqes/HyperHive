package config

import (
	"errors"
	"net"
	"net/url"
	"os"
)

type Config struct {
	Listen       string
	DatabasePath string
	ProxySecret  string
	// MasterURL is where condition fires go (the master's internal automation API).
	MasterURL string
}

func Load() (Config, error) {
	config := Config{Listen: os.Getenv("SUPPORT_LISTEN"), DatabasePath: os.Getenv("SUPPORT_DB_PATH"), ProxySecret: os.Getenv("SUPPORT_PROXY_SECRET"), MasterURL: os.Getenv("SUPPORT_MASTER_URL")}
	if config.Listen == "" {
		config.Listen = "127.0.0.1:8091"
	}
	if config.MasterURL == "" {
		config.MasterURL = "http://127.0.0.1:9595"
	}
	if parsed, err := url.Parse(config.MasterURL); err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.User != nil {
		return Config{}, errors.New("SUPPORT_MASTER_URL must be an http(s) URL")
	}
	if config.DatabasePath == "" {
		config.DatabasePath = "./support.db"
	}
	if _, _, err := net.SplitHostPort(config.Listen); err != nil {
		return Config{}, err
	}
	if len(config.ProxySecret) < 32 {
		return Config{}, errors.New("SUPPORT_PROXY_SECRET must contain at least 32 bytes")
	}
	return config, nil
}
