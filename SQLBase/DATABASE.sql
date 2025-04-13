-- Создание базы данных
CREATE DATABASE my_bot_db;

-- Подключение к базе данных
\c my_bot_db;

-- Установка расширения для UUID (если нужно)
CREATE EXTENSION IF NOT EXISTS "uuid-ossp";

-- Создание таблицы пользователей
CREATE TABLE users (
    id SERIAL PRIMARY KEY,
    first_name VARCHAR(50),
    last_name VARCHAR(50),
    middle_name VARCHAR(50),
    inn VARCHAR(12) UNIQUE NOT NULL CHECK (LENGTH(inn) = 10 OR LENGTH(inn) = 12),
    telegram_id BIGINT UNIQUE NOT NULL,
    email VARCHAR(100) CHECK (email ~* '^[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}$'),
    username VARCHAR(50),
    is_admin BOOLEAN DEFAULT FALSE,
    api_key VARCHAR(64) UNIQUE,
    registered_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    last_active TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT user_identity CHECK (telegram_id > 0)
);
COMMENT ON TABLE users IS 'Таблица пользователей системы';

-- Создание перечисления статусов заказа
CREATE TYPE order_status AS ENUM (
    'created', 'pending', 'paid', 'processed', 'completed', 'cancelled', 'refunded'
);

-- Создание таблицы заказов
CREATE TABLE orders (
    id SERIAL PRIMARY KEY,
    user_id INT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    total_amount DECIMAL(10, 2) NOT NULL CHECK (total_amount > 0),
    status order_status DEFAULT 'created',
    payment_id VARCHAR(50),
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT valid_order CHECK (total_amount > 0)
);
COMMENT ON TABLE orders IS 'Таблица заказов пользователей';

-- Создание таблицы товарных позиций заказа
CREATE TABLE order_items (
    id SERIAL PRIMARY KEY,
    order_id INT NOT NULL REFERENCES orders(id) ON DELETE CASCADE,
    gtin VARCHAR(14) NOT NULL CHECK (LENGTH(gtin) = 14),
    quantity INT NOT NULL CHECK (quantity > 0),
    price DECIMAL(10, 2) CHECK (price >= 0)
);
COMMENT ON TABLE order_items IS 'Детали заказов - товарные позиции';

-- Создание или обновление таблицы order_items с колонкой price если таблица уже существует
ALTER TABLE IF EXISTS order_items ADD COLUMN IF NOT EXISTS price DECIMAL(10, 2) CHECK (price >= 0);

-- Создание перечисления статусов платежа
CREATE TYPE payment_status AS ENUM (
    'pending', 'processing', 'completed', 'failed', 'refunded', 'cancelled'
);

-- Создание таблицы платежей
CREATE TABLE payments (
    id SERIAL PRIMARY KEY,
    order_id INT NOT NULL REFERENCES orders(id) ON DELETE RESTRICT,
    amount DECIMAL(10, 2) NOT NULL CHECK (amount > 0),
    status payment_status DEFAULT 'pending',
    transaction_id VARCHAR(100) UNIQUE,
    currency VARCHAR(3) DEFAULT 'RUB',
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    completed_at TIMESTAMP,
    CONSTRAINT payment_amount_positive CHECK (amount > 0)
);
COMMENT ON TABLE payments IS 'Платежные операции';

-- Обновление таблицы payments для согласования с моделями если колонка существует
ALTER TABLE IF EXISTS payments RENAME COLUMN IF EXISTS robokassa_id TO transaction_id;

-- Триггерная функция для обновления updated_at
CREATE OR REPLACE FUNCTION update_modified_column()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = CURRENT_TIMESTAMP;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- Триггер для обновления поля updated_at в таблице orders
CREATE TRIGGER update_orders_modtime
BEFORE UPDATE ON orders
FOR EACH ROW
EXECUTE FUNCTION update_modified_column();

-- Триггер функция для обновления статуса заказа при изменении статуса платежа
CREATE OR REPLACE FUNCTION update_order_status_on_payment()
RETURNS TRIGGER AS $$
BEGIN
    IF NEW.status = 'completed' AND OLD.status != 'completed' THEN
        UPDATE orders SET status = 'paid', updated_at = CURRENT_TIMESTAMP 
        WHERE id = NEW.order_id AND status = 'pending';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- Триггер для обновления статуса заказа при изменении статуса платежа
CREATE TRIGGER update_order_status
AFTER UPDATE ON payments
FOR EACH ROW
EXECUTE FUNCTION update_order_status_on_payment();

-- Индексы для ускорения часто используемых запросов
CREATE INDEX idx_users_telegram ON users(telegram_id);
CREATE INDEX idx_users_inn ON users(inn);
CREATE INDEX idx_orders_user ON orders(user_id);
CREATE INDEX idx_orders_status ON orders(status);
CREATE INDEX idx_payments_order ON payments(order_id);
CREATE INDEX idx_payments_status ON payments(status);

-- Представление для активных заказов
CREATE VIEW active_orders AS
SELECT o.*, u.telegram_id, u.first_name, u.last_name
FROM orders o
JOIN users u ON o.user_id = u.id
WHERE o.status IN ('created', 'pending', 'paid', 'processed')
ORDER BY o.created_at DESC;