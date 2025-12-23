module sidebar-server

go 1.24.2

require (
	github.com/gorilla/websocket v1.5.3
	github.com/joho/godotenv v1.5.1
	go.uber.org/zap v1.27.0
	gorm.io/driver/postgres v1.5.7
	gorm.io/gorm v1.25.10
	wework-sdk v0.0.0-00010101000000-000000000000
)

require (
	github.com/jackc/pgpassfile v1.0.0 // indirect
	github.com/jackc/pgservicefile v0.0.0-20221227161230-091c0ba34f0a // indirect
	github.com/jackc/pgx/v5 v5.4.3 // indirect
	github.com/jinzhu/inflection v1.0.0 // indirect
	github.com/jinzhu/now v1.1.5 // indirect
	go.uber.org/multierr v1.10.0 // indirect
	golang.org/x/crypto v0.14.0 // indirect
	golang.org/x/text v0.13.0 // indirect
)

replace wework-sdk => ./go_sdk
