module sidebar-server

go 1.24.2

require (
	github.com/gorilla/websocket v1.5.3
	github.com/joho/godotenv v1.5.1
	wework-sdk v0.0.0-00010101000000-000000000000
)

replace wework-sdk => ./go_sdk
