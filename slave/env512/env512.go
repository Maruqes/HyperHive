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
)

func Setup() error {
	godotenv.Load(".env")
	MasterIP = os.Getenv("MASTER_IP")
	SlaveIP = os.Getenv("SLAVE_IP")
	PingInterval, _ = strconv.Atoi(os.Getenv("PING_INTERVAL"))
	if PingInterval == 0 {
		PingInterval = 10 //default 10 seconds
	}

	if MasterIP == "" || SlaveIP == "" {
		panic("Master and Slave IPs must be set")
	}
	return nil
}
