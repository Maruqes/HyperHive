module 512SvMan

go 1.25.0

require (
	github.com/Maruqes/512SvMan/api v0.0.0
	github.com/Maruqes/512SvMan/logger v0.0.0-00010101000000-000000000000
	github.com/Maruqes/512SvMan/protocol v0.0.0
	github.com/evangwt/go-vncproxy v1.1.0
	github.com/go-chi/chi/v5 v5.2.3
	github.com/google/uuid v1.6.0
	github.com/gorilla/websocket v1.5.3
	github.com/joho/godotenv v1.5.1
	github.com/mattn/go-sqlite3 v1.14.32
	golang.org/x/net v0.42.0
	google.golang.org/grpc v1.76.0
	libvirt.org/go/libvirt v1.11006.0
)

require (
	github.com/evangwt/go-bufcopy v0.1.1 // indirect
	github.com/pkg/errors v0.9.1 // indirect
	go.uber.org/multierr v1.10.0 // indirect
	go.uber.org/zap v1.27.0 // indirect
	golang.org/x/sys v0.34.0 // indirect
	golang.org/x/text v0.27.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20250804133106-a7a43d27e69b // indirect
	google.golang.org/protobuf v1.36.6 // indirect
)

replace github.com/Maruqes/512SvMan/api => ../api

replace github.com/Maruqes/512SvMan/logger => ../logger

replace github.com/Maruqes/512SvMan/protocol => ../protocol
