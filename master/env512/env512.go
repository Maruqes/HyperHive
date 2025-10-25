package env512

import (
	"os"
	"strconv"

	"github.com/joho/godotenv"
)

var (
	PingInterval      int
	Mode              string
	Qemu_UID          string
	Qemu_GID          string
	GRPC_TLS_PASSWORD string
)

func Setup() error {
	godotenv.Load(".env")
	PingInterval, _ = strconv.Atoi(os.Getenv("PING_INTERVAL"))
	GRPC_TLS_PASSWORD = os.Getenv("GRPC_TLS_PASSWORD")

	if GRPC_TLS_PASSWORD == "" {
		panic("GRPC_TLS_PASSWORD needs to be set")
	}

	Mode = os.Getenv("MODE")
	Qemu_UID = os.Getenv("QEMU_UID")
	Qemu_GID = os.Getenv("QEMU_GID")

	if PingInterval == 0 {
		PingInterval = 10 //default 10 seconds
	}
	if Mode != "dev" {
		Mode = "prod" //default prod
	}
	return nil
}
