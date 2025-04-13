package config

import (
	"os"
	"testing"
	"time"
)

func TestLoadConfig(t *testing.T) {
	// Сохраняем текущие переменные окружения
	originalEnv := make(map[string]string)
	for _, env := range []string{
		"DB_PASSWORD",
		"CHESTNY_ZNAK_API_KEY",
		"SERVER_PORT",
		"DB_HOST",
	} {
		if value, exists := os.LookupEnv(env); exists {
			originalEnv[env] = value
		}
	}

	// Устанавливаем тестовые переменные окружения
	os.Setenv("DB_PASSWORD", "test_password")
	os.Setenv("CHESTNY_ZNAK_API_KEY", "test_api_key")
	os.Setenv("SERVER_PORT", "8081")
	os.Setenv("DB_HOST", "test_host")

	// Восстанавливаем переменные окружения после теста
	defer func() {
		for key, value := range originalEnv {
			os.Setenv(key, value)
		}
	}()

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Ошибка загрузки конфигурации: %v", err)
	}

	// Проверяем значения
	if cfg.Server.Port != "8081" {
		t.Errorf("Ожидался порт 8081, получен %s", cfg.Server.Port)
	}

	if cfg.Database.Host != "test_host" {
		t.Errorf("Ожидался хост test_host, получен %s", cfg.Database.Host)
	}

	if cfg.Database.Password != "test_password" {
		t.Errorf("Ожидался пароль test_password, получен %s", cfg.Database.Password)
	}

	if cfg.API.APIKey != "test_api_key" {
		t.Errorf("Ожидался API ключ test_api_key, получен %s", cfg.API.APIKey)
	}

	// Проверяем значения по умолчанию
	if cfg.Server.ReadTimeout != 15*time.Second {
		t.Errorf("Ожидался ReadTimeout 15s, получен %v", cfg.Server.ReadTimeout)
	}

	if cfg.Database.SSLMode != "disable" {
		t.Errorf("Ожидался SSLMode disable, получен %s", cfg.Database.SSLMode)
	}
}
