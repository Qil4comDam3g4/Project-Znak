package config

import (
	"fmt"
	"os"
	"time"
)

type Config struct {
	Server   ServerConfig
	Database DatabaseConfig
	API      APIConfig
	Logging  LoggingConfig
}

type ServerConfig struct {
	Port         string
	ReadTimeout  time.Duration
	WriteTimeout time.Duration
	IdleTimeout  time.Duration
}

type DatabaseConfig struct {
	Host     string
	Port     string
	User     string
	Password string
	Name     string
	SSLMode  string
}

type APIConfig struct {
	URL     string
	APIKey  string
	Timeout time.Duration
}

type LoggingConfig struct {
	Level string
	File  string
}

func Load() (*Config, error) {
	cfg := &Config{
		Server: ServerConfig{
			Port:         getEnv("SERVER_PORT", "8080"),
			ReadTimeout:  getDurationEnv("SERVER_READ_TIMEOUT", 15*time.Second),
			WriteTimeout: getDurationEnv("SERVER_WRITE_TIMEOUT", 15*time.Second),
			IdleTimeout:  getDurationEnv("SERVER_IDLE_TIMEOUT", 60*time.Second),
		},
		Database: DatabaseConfig{
			Host:     getEnv("DB_HOST", "localhost"),
			Port:     getEnv("DB_PORT", "5432"),
			User:     getEnv("DB_USER", "postgres"),
			Password: getEnv("DB_PASSWORD", ""),
			Name:     getEnv("DB_NAME", "znak_db"),
			SSLMode:  getEnv("DB_SSL_MODE", "disable"),
		},
		API: APIConfig{
			URL:     getEnv("CHESTNY_ZNAK_API_URL", "https://api.stage.mdlp.crpt.ru"),
			APIKey:  getEnv("CHESTNY_ZNAK_API_KEY", ""),
			Timeout: getDurationEnv("API_TIMEOUT", 30*time.Second),
		},
		Logging: LoggingConfig{
			Level: getEnv("LOG_LEVEL", "info"),
			File:  getEnv("LOG_FILE", ""),
		},
	}

	if err := cfg.validate(); err != nil {
		return nil, fmt.Errorf("ошибка валидации конфигурации: %w", err)
	}

	return cfg, nil
}

func (c *Config) validate() error {
	if c.Database.Password == "" {
		return fmt.Errorf("пароль базы данных не указан")
	}
	if c.API.APIKey == "" {
		return fmt.Errorf("API ключ не указан")
	}
	return nil
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func getDurationEnv(key string, defaultValue time.Duration) time.Duration {
	if value := os.Getenv(key); value != "" {
		if duration, err := time.ParseDuration(value); err == nil {
			return duration
		}
	}
	return defaultValue
}
