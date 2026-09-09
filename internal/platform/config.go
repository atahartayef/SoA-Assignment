// Package platform holds the small amount of code every MaltaShop service needs:
// configuration, logging, database access and the JetStream client.
//
// It is a shared internal package rather than four copies. With twelve developers
// and two operations staff, one correct implementation of correlation IDs and
// message de-duplication is worth more than the independence four copies would buy.
package platform

import (
	"os"
	"strconv"
	"strings"
)

type Config struct {
	ServiceName string
	Version     string
	Environment string
	Port        string
	DatabaseURL string
	RedisURL    string
	NatsURL     string
	LogLevel    string
	OutboxPoll  int
	DeclineOver int64 // cents; the stubbed card limit used to demonstrate compensation
}

func Env(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok && value != "" {
		return value
	}
	return fallback
}

func EnvInt(key string, fallback int) int {
	if value, err := strconv.Atoi(Env(key, "")); err == nil {
		return value
	}
	return fallback
}

func Load() Config {
	return Config{
		ServiceName: Env("SERVICE_NAME", "unnamed-service"),
		Version:     Env("SERVICE_VERSION", "0.0.0"),
		Environment: Env("ENVIRONMENT", "local"),
		Port:        Env("PORT", "8080"),
		DatabaseURL: Env("DATABASE_URL", ""),
		RedisURL:    Env("REDIS_URL", ""),
		NatsURL:     Env("NATS_URL", "nats://nats:4222"),
		LogLevel:    strings.ToLower(Env("LOG_LEVEL", "info")),
		OutboxPoll:  EnvInt("OUTBOX_POLL_MS", 400),
		DeclineOver: int64(EnvInt("CARD_LIMIT_EUR", 750)) * 100,
	}
}
