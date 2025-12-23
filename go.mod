module sidebar-server

go 1.24.2

require (
	github.com/gorilla/websocket v1.5.3
	github.com/joho/godotenv v1.5.1
	go.uber.org/zap v1.27.0
	wework-sdk v0.0.0-00010101000000-000000000000
)

require go.uber.org/multierr v1.10.0 // indirect

replace wework-sdk => ./go_sdk
