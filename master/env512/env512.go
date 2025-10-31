package env512

import (
	"os"
	"strconv"

	"github.com/joho/godotenv"
)

var (
	PingInterval int
	Mode         string
	Qemu_UID     string
	Qemu_GID     string
)

func Setup() error {
	godotenv.Load(".env")
	PingInterval, _ = strconv.Atoi(os.Getenv("PING_INTERVAL"))

	Mode = os.Getenv("MODE")
	Qemu_UID = os.Getenv("QEMU_UID")
	Qemu_GID = os.Getenv("QEMU_GID")

	if Qemu_GID == ""{
		panic("needs qemu gid")
	}
	if Qemu_UID == ""{
		panic("needs qemu uid")
	}

	if PingInterval == 0 {
		PingInterval = 15 //default 15 seconds
	}
	if Mode != "dev" {
		Mode = "prod" //default prod
	}
	return nil
}
