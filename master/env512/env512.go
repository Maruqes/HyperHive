package env512

import (
	"fmt"
	"os"
	"strconv"

	"github.com/joho/godotenv"
)

var (
	PingInterval int
	Mode         string
	Qemu_UID     string
	Qemu_GID     string
	SPRITE_MIN   int
	SPRITE_MAX   int
)

func Setup() error {
	godotenv.Load(".env")
	PingInterval, _ = strconv.Atoi(os.Getenv("PING_INTERVAL"))

	Mode = os.Getenv("MODE")
	Qemu_UID = os.Getenv("QEMU_UID")
	Qemu_GID = os.Getenv("QEMU_GID")

	sMin := os.Getenv("SPRITE_MIN")
	sMax := os.Getenv("SPRITE_MAX")

	if Qemu_GID == "" || Qemu_UID == "" {
		return fmt.Errorf("QEMU_UID and QEMU_GID must be set")
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
	return nil
}
