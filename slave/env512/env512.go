package env512

import (
	"os"
	"strconv"

	"github.com/joho/godotenv"
)

var (
	MasterIP     string
	SlaveIP      string
	PingInterval int
	Mode         string
	MachineName  string
	VNC_MIN_PORT int
	VNC_MAX_PORT int
)

func Setup() error {
	godotenv.Load(".env")
	MasterIP = os.Getenv("MASTER_IP")
	SlaveIP = os.Getenv("SLAVE_IP")
	Mode = os.Getenv("MODE")
	MachineName = os.Getenv("MACHINE_NAME")
	PingInterval, _ = strconv.Atoi(os.Getenv("PING_INTERVAL"))
	VNC_MIN_PORT, _ = strconv.Atoi(os.Getenv("VNC_MIN_PORT"))
	VNC_MAX_PORT, _ = strconv.Atoi(os.Getenv("VNC_MAX_PORT"))
	if PingInterval == 0 {
		PingInterval = 10 //default 10 seconds
	}

	if MasterIP == "" || SlaveIP == "" {
		panic("Master and Slave IPs must be set")
	}
	if Mode != "dev" {
		Mode = "prod"
	}

	if MachineName == "" {
		panic("MACHINE_NAME must be set")
	}

	if VNC_MIN_PORT == 0 || VNC_MAX_PORT == 0 || VNC_MIN_PORT < 5900 || VNC_MAX_PORT > 65535 || VNC_MIN_PORT > VNC_MAX_PORT {
		panic("VNC_MIN_PORT and VNC_MAX_PORT must be set and valid (5900-65535)")
	}

	return nil
}
