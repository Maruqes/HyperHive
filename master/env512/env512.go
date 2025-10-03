package env512

import (
	"os"
	"strconv"

	"github.com/joho/godotenv"
)

var (
	PingInterval   int
	Mode          string
)

func Setup() error {
	godotenv.Load(".env")
	PingInterval, _ = strconv.Atoi(os.Getenv("PING_INTERVAL"))
	Mode = os.Getenv("MODE")

	if PingInterval == 0 {
		PingInterval = 10 //default 10 seconds
	}
	if Mode != "dev"{
		Mode = "prod" //default prod
	}
	return nil
}
