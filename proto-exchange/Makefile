# proto-exchange — генерация gRPC-стабов (команда из ТЗ).
.PHONY: proto

proto:
	cd exchange && protoc --go_out=. --go_opt=paths=source_relative --go-grpc_out=. --go-grpc_opt=paths=source_relative --experimental_allow_proto3_optional exchange.proto