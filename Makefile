setup:
	protoc --go_out=. --go-grpc_out=. api/proto/protocol.proto
	protoc --go_out=. --go-grpc_out=. api/proto/nfs.proto 
	cd api/proto/protocol && go mod init github.com/Maruqes/512SvMan/api/proto/protocol || true
	cd api/proto/nfs && go mod init github.com/Maruqes/512SvMan/api/proto/nfs || true