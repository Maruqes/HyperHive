package config

import (
	"errors"
	"net"
	"os"
)

type Config struct {
	Listen       string
	DatabasePath string
	ProxySecret  string
}

func Load() (Config, error) {
	config := Config{Listen: os.Getenv("SUPPORT_LISTEN"), DatabasePath: os.Getenv("SUPPORT_DB_PATH"), ProxySecret: os.Getenv("SUPPORT_PROXY_SECRET")}
	if config.Listen == "" {
		config.Listen = "127.0.0.1:8091"
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
