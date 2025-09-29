setup:
	protoc --go_out=. --go-grpc_out=. api/proto/protocol.proto
	cd api/proto/protocol && go mod init protocol