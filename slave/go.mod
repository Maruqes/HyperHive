module slave

go 1.25.0

require (
	github.com/Maruqes/512SvMan/api v0.0.0
	github.com/Maruqes/512SvMan/logger v0.0.0-20251001141129-5e5e217740cf
	github.com/joho/godotenv v1.5.1
	github.com/klauspost/cpuid/v2 v2.3.0
	github.com/shirou/gopsutil/v4 v4.25.9
	google.golang.org/grpc v1.75.1
	libvirt.org/go/libvirt v1.11006.0
)

require (
	github.com/cavaliergopher/grab/v3 v3.0.1 // indirect
	github.com/coreos/go-systemd/v22 v22.6.0 // indirect
	github.com/creack/pty v1.1.24 // indirect
	github.com/ebitengine/purego v0.9.0 // indirect
	github.com/go-cmd/cmd v1.4.3 // indirect
	github.com/go-ole/go-ole v1.2.6 // indirect
	github.com/godbus/dbus/v5 v5.1.0 // indirect
	github.com/lufia/plan9stats v0.0.0-20211012122336-39d0f177ccd0 // indirect
	github.com/md14454/gosensors v0.0.0-20180726083412-bded752ab001 // indirect
	github.com/power-devops/perfstat v0.0.0-20240221224432-82ca36839d55 // indirect
	github.com/tklauser/go-sysconf v0.3.15 // indirect
	github.com/tklauser/numcpus v0.10.0 // indirect
	github.com/yusufpapurcu/wmi v1.2.4 // indirect
	go.uber.org/multierr v1.11.0 // indirect
	go.uber.org/zap v1.27.0 // indirect
	golang.org/x/net v0.44.0 // indirect
	golang.org/x/sys v0.36.0 // indirect
	golang.org/x/text v0.29.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20250929231259-57b25ae835d4 // indirect
	google.golang.org/protobuf v1.36.9 // indirect
)

replace github.com/Maruqes/512SvMan/api => ../api

replace github.com/Maruqes/512SvMan/logger => ../logger
