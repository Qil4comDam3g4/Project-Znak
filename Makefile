.PHONY: build run test clean docker-build docker-run

# Переменные
APP_NAME=znak-api
DOCKER_IMAGE=znak-api:latest

# Сборка приложения
build:
	go build -o $(APP_NAME) ./main.go

# Запуск приложения локально
run:
	go run main.go

# Запуск тестов
test:
	go test ./...

# Очистка артефактов сборки
clean:
	rm -f $(APP_NAME)
	docker system prune -f

# Сборка Docker образа
docker-build:
	docker build -t $(DOCKER_IMAGE) .

# Запуск в Docker
docker-run:
	docker-compose up --build

# Остановка Docker контейнеров
docker-stop:
	docker-compose down

# Проверка кода
lint:
	golangci-lint run

# Миграции базы данных
migrate-up:
	migrate -path ./migrations -database "postgres://${DB_USER}:${DB_PASSWORD}@${DB_HOST}:${DB_PORT}/${DB_NAME}?sslmode=disable" up

migrate-down:
	migrate -path ./migrations -database "postgres://${DB_USER}:${DB_PASSWORD}@${DB_HOST}:${DB_PORT}/${DB_NAME}?sslmode=disable" down

# Помощь
help:
	@echo "Доступные команды:"
	@echo "  make build         - Сборка приложения"
	@echo "  make run          - Запуск приложения локально"
	@echo "  make test         - Запуск тестов"
	@echo "  make clean        - Очистка артефактов"
	@echo "  make docker-build - Сборка Docker образа"
	@echo "  make docker-run   - Запуск в Docker"
	@echo "  make docker-stop  - Остановка Docker контейнеров"
	@echo "  make lint         - Проверка кода"
	@echo "  make migrate-up   - Применение миграций"
	@echo "  make migrate-down - Откат миграций" 