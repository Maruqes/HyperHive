package env512

import (
	"os"
	"strconv"

	"github.com/joho/godotenv"
)

var (
	NPM_USER_NAME  string
	NPM_USER_NICK  string
	NPM_USER_EMAIL string
	NPM_USER_PASS  string
	PingInterval   int
	Mode          string
)

func Setup() error {
	godotenv.Load(".env")
	NPM_USER_NAME = os.Getenv("NPM_USER_NAME")
	NPM_USER_NICK = os.Getenv("NPM_USER_NICK")
	NPM_USER_EMAIL = os.Getenv("NPM_USER_EMAIL")
	NPM_USER_PASS = os.Getenv("NPM_USER_PASS")
	PingInterval, _ = strconv.Atoi(os.Getenv("PING_INTERVAL"))
	Mode = os.Getenv("MODE")

	if PingInterval == 0 {
		PingInterval = 10 //default 10 seconds
	}
	if NPM_USER_NAME == "" || NPM_USER_NICK == "" || NPM_USER_EMAIL == "" || NPM_USER_PASS == "" {
		panic("NPM user environment variables not set")
	}
	if Mode != "dev"{
		Mode = "prod" //default prod
	}
	return nil
}
