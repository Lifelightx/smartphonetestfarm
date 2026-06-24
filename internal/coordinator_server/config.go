package coordinator_server

import (
	"os"
	"strconv"
)

type Config struct {
	GRPCPort    int
	PostgresURI string
}

func LoadConfig() Config {
	port := 9000
	if pStr := os.Getenv("COORDINATOR_GRPC_PORT"); pStr != "" {
		if p, err := strconv.Atoi(pStr); err == nil {
			port = p
		}
	}
	dbURI := "postgres://postgres:123456@localhost:5455/protean?sslmode=disable"
	if uri := os.Getenv("COORDINATOR_POSTGRES_URI"); uri != "" {
		dbURI = uri
	}
	return Config{
		GRPCPort:    port,
		PostgresURI: dbURI,
	}
}
