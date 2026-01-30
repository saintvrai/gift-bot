package config

import (
	"log"
	"os"
	"strconv"

	"github.com/joho/godotenv"
)

type Config struct {
	DB           PostgresConfig
	ServerConfig ServerConfig
	Telegram     TelegramConfig
}

type PostgresConfig struct {
	Host       string
	Port       string
	Username   string
	Name       string
	Password   string
	SSL        string
	Migrations string
}

type ServerConfig struct {
	Port     string
	GinMode  string
	Timezone string
}

type TelegramConfig struct {
	Token  string
	Secret string
}

var GlobalСonfig Config

func (c *Config) Init() {
	if err := godotenv.Load(); err != nil {
		log.Printf("No .env file found, using real environment: %v", err)
	}

	// Server
	c.ServerConfig.GinMode = getEnvWithDefault("SERVER_GINMODE", "debug")
	c.ServerConfig.Port = mustGetEnv("SERVER_PORT")
	c.ServerConfig.Timezone = "Europe/Moscow"

	// PostgreSQL
	c.DB.Host = mustGetEnv("PG_HOST")
	c.DB.Port = mustGetEnv("PG_PORT")
	c.DB.Username = mustGetEnv("PG_USER")
	c.DB.Name = mustGetEnv("PG_NAME")
	c.DB.Password = mustGetEnv("PG_PASSWORD")
	c.DB.SSL = getEnvWithDefault("PG_SSLMODE", "disable")
	c.DB.Migrations = "db/migrations"

	// Telegram
	c.Telegram.Token = mustGetEnv("TELEGRAM_TOKEN")
	c.Telegram.Secret = mustGetEnv("TELEGRAM_SECRET")
}

func mustGetEnv(key string) string {
	const op = "pkg/config/mustGetEnv"
	val, ok := os.LookupEnv(key)
	if !ok || val == "" {
		log.Fatalf("op: %s env variable %s is required but not set", op, key)
	}
	return val
}

//lint:ignore U1000 reserved for future config
func getEnvAsInt(key string) int {
	const op = "pkg/config/getEnvAsInt"
	s := mustGetEnv(key)
	i, err := strconv.Atoi(s)
	if err != nil {
		log.Fatalf("op: %s cannot parse %s=%q as int: %v", op, key, s, err)
	}
	return i
}

//lint:ignore U1000 reserved for future config
func getEnvAsBool(key string) bool {
	const op = "pkg/config/getEnvAsBool"
	s := mustGetEnv(key)
	b, err := strconv.ParseBool(s)
	if err != nil {
		log.Fatalf("op: %s cannot parse %s=%q as bool: %v", op, key, s, err)
	}
	return b
}

// getEnvWithDefault возвращает значение переменной окружения или значение по умолчанию
func getEnvWithDefault(key, defaultValue string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultValue
}
