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
)

func Setup() error {
	godotenv.Load(".env")
	MasterIP = os.Getenv("MASTER_IP")
	SlaveIP = os.Getenv("SLAVE_IP")
	Mode = os.Getenv("MODE")
	MachineName = os.Getenv("MACHINE_NAME")
	PingInterval, _ = strconv.Atoi(os.Getenv("PING_INTERVAL"))
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
	return nil
}
