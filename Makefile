PROTOC ?= protoc
GO_MODULE := github.com/Maruqes/512SvMan/api
PROTO_DIR := api/proto
PROTO_FILES := $(wildcard $(PROTO_DIR)/*.proto)
PROTO_INPUT := $(patsubst $(PROTO_DIR)/%,%,$(PROTO_FILES))
PROTO_PACKAGES := $(basename $(PROTO_INPUT))

setup: $(PROTO_FILES)
	mkdir -p $(addprefix $(PROTO_DIR)/,$(PROTO_PACKAGES))
	$(PROTOC) \
		--proto_path=$(PROTO_DIR) \
		--go_out=api \
		--go_opt=module=$(GO_MODULE) \
		--go-grpc_out=api \
		--go-grpc_opt=module=$(GO_MODULE) \
		$(PROTO_INPUT)
