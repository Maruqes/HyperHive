module slave

go 1.25.0

require (
	github.com/Maruqes/512SvMan/api/proto/hello v0.0.0
	google.golang.org/grpc v1.75.1
)

require (
	golang.org/x/net v0.41.0 // indirect
	golang.org/x/sys v0.33.0 // indirect
	golang.org/x/text v0.26.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20250707201910-8d1bb00bc6a7 // indirect
	google.golang.org/protobuf v1.36.6 // indirect
)

replace github.com/Maruqes/512SvMan/api/proto/hello => ../api/proto/hello
