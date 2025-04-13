# Project ZNAK

API для работы с системой "Честный знак" и управления заказами.

## Структура проекта

```
.
├── cmd/
│   └── api/              # Точка входа приложения
├── internal/
│   ├── api/             # API handlers и middleware
│   ├── config/          # Конфигурация приложения
│   ├── database/        # Работа с базой данных
│   ├── models/          # Модели данных
│   └── services/        # Бизнес-логика и сервисы
├── pkg/
│   ├── logger/          # Логирование
│   └── utils/           # Вспомогательные функции
├── docs/                # Документация
├── tests/               # Тесты
├── .github/
│   └── workflows/       # CI/CD пайплайны
├── Dockerfile
├── docker-compose.yml
├── .env.example
├── .gitignore
├── go.mod
└── go.sum
```

## Требования

- Go 1.21 или выше
- PostgreSQL 12 или выше
- Docker и Docker Compose
- Доступ к API "Честный знак"

## Установка

1. Клонируйте репозиторий:
```bash
git clone https://github.com/your-username/project-znak.git
cd project-znak
```

2. Установите зависимости:
```bash
go mod download
```

3. Создайте базу данных PostgreSQL:
```sql
CREATE DATABASE znak_db;
CREATE USER znak_user WITH PASSWORD 'your_secure_password';
GRANT ALL PRIVILEGES ON DATABASE znak_db TO znak_user;
```

4. Скопируйте и настройте .env файл:
```bash
cp .env.example .env
# Отредактируйте .env файл
```

5. Запустите миграции:
```bash
make migrate-up
```

## Запуск

### Локально

```bash
make run
```

### В Docker

```bash
make docker-run
```

Сервер будет доступен по адресу: http://localhost:8080

## Деплой в продакшен

### Подготовка сервера

1. Установите Docker и Docker Compose:
```bash
# Ubuntu/Debian
sudo apt-get update
sudo apt-get install docker.io docker-compose

# CentOS/RHEL
sudo yum install docker docker-compose
```

2. Создайте директорию для проекта:
```bash
sudo mkdir -p /opt/znak-api
sudo chown -R $USER:$USER /opt/znak-api
```

3. Настройте SSL сертификаты:
```bash
sudo mkdir -p /etc/letsencrypt/live/your-domain.com
# Поместите сертификаты в директорию
```

### Настройка GitHub Secrets

1. Перейдите в настройки репозитория
2. Выберите "Secrets and variables" -> "Actions"
3. Добавьте следующие секреты:
   - `DOCKERHUB_USERNAME`: Ваш логин в Docker Hub
   - `DOCKERHUB_TOKEN`: Токен доступа Docker Hub
   - `PRODUCTION_HOST`: IP или домен продакшен сервера
   - `PRODUCTION_USERNAME`: Пользователь для SSH
   - `PRODUCTION_SSH_KEY`: Приватный SSH ключ

### Мониторинг

#### Prometheus

1. Метрики доступны по адресу: `http://your-domain.com:9090/metrics`
2. Основные метрики:
   - `http_requests_total`: Общее количество запросов
   - `http_request_duration_seconds`: Время обработки запросов
   - `db_connections`: Количество соединений с БД
   - `memory_usage_bytes`: Использование памяти

#### Логирование

1. Логи доступны в JSON формате
2. Уровни логирования:
   - `debug`: Отладочная информация
   - `info`: Основная информация
   - `warn`: Предупреждения
   - `error`: Ошибки

3. Просмотр логов:
```bash
docker-compose logs -f
```

#### Health Checks

1. Endpoint: `http://your-domain.com:8080/health`
2. Проверяет:
   - Подключение к БД
   - Доступность API Честного знака
   - Состояние сервиса

### План отката

#### Автоматический откат

1. CI/CD автоматически откатится, если:
   - Тесты не проходят
   - Сканер безопасности обнаруживает уязвимости
   - Health check не проходит

#### Ручной откат

1. Откат к предыдущей версии:
```bash
cd /opt/znak-api
docker-compose pull znak-api:previous-version
docker-compose up -d
```

2. Восстановление базы данных:
```bash
# Создание бэкапа
pg_dump -U znak_user -d znak_db > backup.sql

# Восстановление
psql -U znak_user -d znak_db < backup.sql
```

3. Проверка работоспособности:
```bash
curl http://your-domain.com:8080/health
```

## API Endpoints

### Пользователи
- `POST /api/users/register` - Регистрация пользователя
- `GET /api/users` - Получение информации о пользователе

### Заказы
- `POST /api/orders` - Создание заказа
- `GET /api/orders` - Получение списка заказов
- `GET /api/orders/{id}` - Получение информации о заказе

### Платежи
- `POST /api/payments` - Создание платежа
- `GET /api/payments/{id}` - Получение статуса платежа

## Лицензия

MIT 