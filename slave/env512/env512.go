package env512

import (
	"os"
	"strconv"

	"github.com/joho/godotenv"
	"google.golang.org/grpc"
)

var (
	MasterIP     string
	SlaveIP      string
	PingInterval int
	Mode         string
	MachineName  string
	VNC_MIN_PORT int
	VNC_MAX_PORT int
	OTHER_SLAVES []string
	Conn         *grpc.ClientConn
	Qemu_UID     string
	Qemu_GID     string
)

func SetConn(conn *grpc.ClientConn) {
	Conn = conn
}

func Setup() error {
	godotenv.Load(".env")
	MasterIP = os.Getenv("MASTER_IP")
	SlaveIP = os.Getenv("SLAVE_IP")
	Mode = os.Getenv("MODE")
	MachineName = os.Getenv("MACHINE_NAME")
	Qemu_UID = os.Getenv("QEMU_UID")
	Qemu_GID = os.Getenv("QEMU_GID")
	PingInterval, _ = strconv.Atoi(os.Getenv("PING_INTERVAL"))
	VNC_MIN_PORT, _ = strconv.Atoi(os.Getenv("VNC_MIN_PORT"))
	VNC_MAX_PORT, _ = strconv.Atoi(os.Getenv("VNC_MAX_PORT"))

	if PingInterval == 0 {
		PingInterval = 15 // default 15 seconds
	}

	if MasterIP == "" || SlaveIP == "" {
		panic("MASTER_IP and SLAVE_IP must be set")
	}

	if Qemu_UID == "" || Qemu_GID == "" {
		panic("QEMU_UID and QEMU_GID must be set")
	}

	if Mode != "dev" {
		Mode = "prod"
	}

	if MachineName == "" {
		panic("MACHINE_NAME must be set")
	}

	if VNC_MIN_PORT == 0 || VNC_MAX_PORT == 0 || VNC_MIN_PORT < 35000 || VNC_MAX_PORT > 65535 || VNC_MIN_PORT > VNC_MAX_PORT {
		panic("VNC_MIN_PORT and VNC_MAX_PORT must be set and valid (35000-65535)")
	}

	// OTHER_SLAVE1_IP, OTHER_SLAVE2_IP, ...
	for i := 1; ; i++ {
		envVar := "OTHER_SLAVE" + strconv.Itoa(i) + "_IP"
		ip := os.Getenv(envVar)
		if ip == "" {
			break
		}
		OTHER_SLAVES = append(OTHER_SLAVES, ip)
	}

	return nil
}
