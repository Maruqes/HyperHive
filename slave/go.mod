module slave

go 1.27.3

require (
	github.com/Maruqes/512SvMan/api v0.0.0-20261022110010-0939c75ad430
	github.com/Maruqes/512SvMan/logger v0.0.0-20261022110010-0939c75ad430
	github.com/cavaliergopher/grab/v3 v3.0.1
	github.com/coreos/go-systemd/v22 v22.7.0
	github.com/google/uuid v1.6.0
	github.com/joho/godotenv v1.5.1
	github.com/shirou/gopsutil/v4 v4.26.9
	go.opentelemetry.io/proto/otlp v1.11.1
	golang.org/x/sys v0.49.0
	google.golang.org/grpc v1.84.0
	google.golang.org/protobuf v1.36.12
	libvirt.org/go/libvirt v1.12007.0
)

require (
	github.com/ebitengine/purego v0.11.2 // indirect
	github.com/go-ole/go-ole v1.3.0 // indirect
	github.com/godbus/dbus/v5 v5.2.4 // indirect
	github.com/grpc-ecosystem/grpc-gateway/v2 v2.32.0 // indirect
	github.com/lufia/plan9stats v0.0.0-20260802145828-341c2f0c90b5 // indirect
	github.com/power-devops/perfstat v0.0.0-20260916203055-22a1a467d9f0 // indirect
	github.com/tklauser/go-sysconf v0.4.0 // indirect
	github.com/tklauser/numcpus v0.12.0 // indirect
	github.com/yusufpapurcu/wmi v1.2.4 // indirect
	go.uber.org/multierr v1.11.0 // indirect
	go.uber.org/zap v1.28.0 // indirect
	golang.org/x/net v0.61.0 // indirect
	golang.org/x/text v0.43.0 // indirect
	google.golang.org/genproto/googleapis/api v0.0.0-20261019180522-ce908d29299b // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20261019180522-ce908d29299b // indirect
)

replace github.com/Maruqes/512SvMan/api => ../api

replace github.com/Maruqes/512SvMan/logger => ../logger
