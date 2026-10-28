module 512SvMan

go 1.27.3

require (
	github.com/Maruqes/512SvMan/api v0.0.0
	github.com/Maruqes/512SvMan/logger v0.0.0-20261022110010-0939c75ad430
	github.com/Maruqes/HyperHive/mcp v0.0.0
	github.com/SherClockHolmes/webpush-go v1.4.0
	github.com/evangwt/go-vncproxy v1.1.0
	github.com/go-chi/chi/v5 v5.5.0
	github.com/golang-jwt/jwt/v5 v5.3.1
	github.com/google/uuid v1.6.0
	github.com/gorilla/websocket v1.5.3
	github.com/joho/godotenv v1.5.1
	github.com/mattn/go-sqlite3 v1.14.52
	github.com/modelcontextprotocol/go-sdk v1.8.0
	github.com/oschwald/geoip2-golang v1.13.0
	github.com/vishvananda/netlink v1.3.1
	golang.org/x/crypto v0.58.0
	golang.org/x/net v0.61.0
	golang.org/x/oauth2 v0.37.0
	golang.org/x/sys v0.49.0
	golang.zx2c4.com/wireguard/wgctrl v0.0.0-20241231184526-a9ab2273dd10
	google.golang.org/grpc v1.84.0
	google.golang.org/protobuf v1.36.12
	libvirt.org/go/libvirt v1.12007.0
	hyperhive/support v0.0.0
)

require (
	github.com/evangwt/go-bufcopy v0.1.1 // indirect
	github.com/google/go-cmp v0.7.0 // indirect
	github.com/google/jsonschema-go v0.4.3 // indirect
	github.com/mdlayher/genetlink v1.4.0 // indirect
	github.com/mdlayher/netlink v1.11.2 // indirect
	github.com/mdlayher/socket v0.8.0 // indirect
	github.com/oschwald/maxminddb-golang v1.13.1 // indirect
	github.com/pkg/errors v0.9.1 // indirect
	github.com/segmentio/asm v1.2.1 // indirect
	github.com/segmentio/encoding v0.5.4 // indirect
	github.com/vishvananda/netns v0.0.5 // indirect
	github.com/yosida95/uritemplate/v3 v3.0.2 // indirect
	go.uber.org/multierr v1.11.0 // indirect
	go.uber.org/zap v1.28.0 // indirect
	golang.org/x/sync v0.24.0 // indirect
	golang.org/x/text v0.43.0 // indirect
	golang.org/x/time v0.16.0 // indirect
	golang.zx2c4.com/wireguard v0.0.0-20261006164505-2631ce99a06f // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20261019180522-ce908d29299b // indirect
)

replace github.com/Maruqes/512SvMan/api => ../api

replace github.com/Maruqes/512SvMan/logger => ../logger

replace github.com/Maruqes/HyperHive/mcp => ../mcp

replace hyperhive/support => ../support
