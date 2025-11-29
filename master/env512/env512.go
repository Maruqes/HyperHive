package env512

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/joho/godotenv"
)

var (
	PingInterval            int
	Mode                    string
	Qemu_UID                string
	Qemu_GID                string
	MASTER_INTERNET_IP      string
	SPRITE_MIN              int
	SPRITE_MAX              int
	MAIN_LINK               string
	GoAccessEnablePanels    []string
	GoAccessDisablePanels   []string
	GoAccessGeoIPLicenseKey string
	GoAccessGeoIPEdition    string
	VapidPublicKey          string
	VapidPrivateKey         string
)

func Setup() error {
	godotenv.Load(".env")
	// Parse PING_INTERVAL; default to 15 seconds on parse error or when unset
	if s := os.Getenv("PING_INTERVAL"); s == "" {
		PingInterval = 15
	} else {
		if n, err := strconv.Atoi(s); err != nil {
			PingInterval = 15
		} else {
			PingInterval = n
		}
	}

	Mode = os.Getenv("MODE")
	Qemu_UID = os.Getenv("QEMU_UID")
	Qemu_GID = os.Getenv("QEMU_GID")
	MASTER_INTERNET_IP = os.Getenv("MASTER_INTERNET_IP")

	sMin := os.Getenv("SPRITE_MIN")
	sMax := os.Getenv("SPRITE_MAX")
	MAIN_LINK = os.Getenv("MAIN_LINK")
	GoAccessEnablePanels = splitAndTrimCSV(os.Getenv("GOACCESS_ENABLE_PANELS"))
	GoAccessDisablePanels = splitAndTrimCSV(os.Getenv("GOACCESS_DISABLE_PANELS"))
	GoAccessGeoIPLicenseKey = strings.TrimSpace(os.Getenv("GOACCESS_GEOIP_LICENSE_KEY"))
	GoAccessGeoIPEdition = strings.TrimSpace(os.Getenv("GOACCESS_GEOIP_EDITION"))
	if GoAccessGeoIPEdition == "" {
		GoAccessGeoIPEdition = "GeoLite2-City"
	}

	if MAIN_LINK == "" {
		panic("needs MAIN_LINK")
	}

	if Qemu_GID == "" || Qemu_UID == "" {
		return fmt.Errorf("QEMU_UID and QEMU_GID must be set")
	}

	if MASTER_INTERNET_IP == "" {
		return fmt.Errorf("MASTER_INTERNET_IP must be set")
	}

	if sMin == "" && sMax == "" {
		// both unset: leave SPRITE_MIN and SPRITE_MAX as zero (or defaults handled elsewhere)
	} else if sMin == "" || sMax == "" {
		return fmt.Errorf("both SPRITE_MIN and SPRITE_MAX must be set or both unset")
	} else {
		min, err := strconv.Atoi(sMin)
		if err != nil {
			return fmt.Errorf("invalid SPRITE_MIN: %w", err)
		}
		max, err := strconv.Atoi(sMax)
		if err != nil {
			return fmt.Errorf("invalid SPRITE_MAX: %w", err)
		}
		if min < 0 || max < 0 {
			return fmt.Errorf("SPRITE_MIN and SPRITE_MAX must be non-negative")
		}
		if min > max {
			return fmt.Errorf("SPRITE_MIN (%d) must be <= SPRITE_MAX (%d)", min, max)
		}
		SPRITE_MIN = min
		SPRITE_MAX = max
	}

	if PingInterval == 0 {
		PingInterval = 15 //default 15 seconds
	}
	if Mode != "dev" {
		Mode = "prod" //default prod
	}

	VapidPublicKey = strings.TrimSpace(os.Getenv("VAPID_PUBLIC_KEY"))
	VapidPrivateKey = strings.TrimSpace(os.Getenv("VAPID_PRIVATE_KEY"))

	return nil
}

func splitAndTrimCSV(val string) []string {
	if val == "" {
		return nil
	}
	parts := strings.Split(val, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		trimmed := strings.TrimSpace(p)
		if trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}
