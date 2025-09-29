setup:
	protoc --go_out=. --go-grpc_out=. api/proto/protocol.proto
	protoc --go_out=. --go-grpc_out=. api/proto/nfs.proto 
	cd api/proto/protocol && go mod init protocol || true
	cd api/proto/nfs && go mod init nfs || true