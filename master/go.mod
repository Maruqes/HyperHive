module 512SvMan

go 1.25.0

require (
	github.com/Maruqes/512SvMan/api v0.0.0
	github.com/Maruqes/512SvMan/logger v0.0.0-00010101000000-000000000000
	github.com/SherClockHolmes/webpush-go v1.4.0
	github.com/evangwt/go-vncproxy v1.1.0
	github.com/go-chi/chi/v5 v5.2.3
	github.com/google/uuid v1.6.0
	github.com/gorilla/websocket v1.5.3
	github.com/joho/godotenv v1.5.1
	github.com/mattn/go-sqlite3 v1.14.32
	github.com/oschwald/geoip2-golang v1.9.0
	github.com/vishvananda/netlink v1.3.1
	golang.org/x/net v0.42.0
	golang.org/x/time v0.14.0
	golang.zx2c4.com/wireguard/wgctrl v0.0.0-20241231184526-a9ab2273dd10
	google.golang.org/grpc v1.76.0
	google.golang.org/protobuf v1.36.6
	libvirt.org/go/libvirt v1.11006.0
)

require (
	github.com/evangwt/go-bufcopy v0.1.1 // indirect
	github.com/golang-jwt/jwt/v5 v5.2.1 // indirect
	github.com/google/go-cmp v0.7.0 // indirect
	github.com/josharian/native v1.1.0 // indirect
	github.com/mdlayher/genetlink v1.3.2 // indirect
	github.com/mdlayher/netlink v1.7.2 // indirect
	github.com/mdlayher/socket v0.5.1 // indirect
	github.com/oschwald/maxminddb-golang v1.11.0 // indirect
	github.com/pkg/errors v0.9.1 // indirect
	github.com/vishvananda/netns v0.0.5 // indirect
	go.uber.org/multierr v1.10.0 // indirect
	go.uber.org/zap v1.27.0 // indirect
	golang.org/x/crypto v0.40.0 // indirect
	golang.org/x/sync v0.16.0 // indirect
	golang.org/x/sys v0.34.0 // indirect
	golang.org/x/text v0.27.0 // indirect
	golang.zx2c4.com/wireguard v0.0.0-20231211153847-12269c276173 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20250804133106-a7a43d27e69b // indirect
)

replace github.com/Maruqes/512SvMan/api => ../api

replace github.com/Maruqes/512SvMan/logger => ../logger
