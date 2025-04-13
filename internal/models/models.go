package models

import (
	"errors"
	"strconv"
	"strings"
	"time"
)

// Константы для статусов заказа
const (
	OrderStatusCreated   = "created"
	OrderStatusPending   = "pending"
	OrderStatusPaid      = "paid"
	OrderStatusProcessed = "processed"
	OrderStatusCompleted = "completed"
	OrderStatusCancelled = "cancelled"
	OrderStatusRefunded  = "refunded"
)

// Константы для статусов платежа
const (
	PaymentStatusPending    = "pending"
	PaymentStatusProcessing = "processing"
	PaymentStatusCompleted  = "completed"
	PaymentStatusFailed     = "failed"
	PaymentStatusRefunded   = "refunded"
	PaymentStatusCancelled  = "cancelled"
)

// User представляет пользователя системы
type User struct {
	ID           int       `json:"id"`
	FirstName    string    `json:"first_name"`
	LastName     string    `json:"last_name"`
	MiddleName   string    `json:"middle_name,omitempty"`
	INN          string    `json:"inn"`                   // ИНН организации
	TelegramID   int64     `json:"telegram_id"`           // Уникальный ID в Telegram
	Email        string    `json:"email"`                 // Электронная почта
	Username     string    `json:"username"`              // Логин в системе
	IsAdmin      bool      `json:"is_admin"`              // Права администратора
	RegisteredAt time.Time `json:"registered_at"`         // Дата регистрации
	LastActive   time.Time `json:"last_active,omitempty"` // Время последней активности
	APIKey       string    `json:"api_key,omitempty"`     // API ключ для программного доступа
}

// Validate проверяет корректность данных пользователя
func (u *User) Validate() error {
	if u.TelegramID <= 0 {
		return errors.New("telegram_id должен быть положительным числом")
	}

	if u.INN == "" {
		return errors.New("ИНН не может быть пустым")
	}

	if u.Email != "" && !strings.Contains(u.Email, "@") {
		return errors.New("некорректный формат email")
	}

	return nil
}

// FullName возвращает полное имя пользователя
func (u *User) FullName() string {
	parts := []string{}

	if u.LastName != "" {
		parts = append(parts, u.LastName)
	}

	if u.FirstName != "" {
		parts = append(parts, u.FirstName)
	}

	if u.MiddleName != "" {
		parts = append(parts, u.MiddleName)
	}

	return strings.Join(parts, " ")
}

// OrderItem представляет товарную позицию в заказе
type OrderItem struct {
	ID       int     `json:"id"`
	GTIN     string  `json:"gtin"`            // Глобальный номер товара
	Quantity int     `json:"quantity"`        // Количество
	Price    float64 `json:"price,omitempty"` // Цена за единицу
}

// Validate проверяет корректность товарной позиции
func (oi *OrderItem) Validate() error {
	if oi.GTIN == "" {
		return errors.New("GTIN не может быть пустым")
	}

	if oi.Quantity <= 0 {
		return errors.New("количество должно быть положительным числом")
	}

	return nil
}

// Order представляет заказ пользователя
type Order struct {
	ID          int         `json:"id"`
	UserID      int         `json:"user_id"`              // Ссылка на пользователя
	Items       []OrderItem `json:"items"`                // Список товаров
	TotalAmount float64     `json:"total_amount"`         // Общая сумма
	Status      string      `json:"status"`               // Статус заказа
	PaymentID   string      `json:"payment_id"`           // ID платежа
	CreatedAt   time.Time   `json:"created_at"`           // Дата создания
	UpdatedAt   time.Time   `json:"updated_at,omitempty"` // Дата последнего обновления
}

// Validate проверяет корректность заказа
func (o *Order) Validate() error {
	if o.UserID <= 0 {
		return errors.New("ID пользователя должен быть положительным числом")
	}

	if len(o.Items) == 0 {
		return errors.New("заказ должен содержать хотя бы один товар")
	}

	for i, item := range o.Items {
		if err := item.Validate(); err != nil {
			return errors.New(
				"ошибка в товаре #" + strconv.Itoa(i+1) + ": " + err.Error())
		}
	}

	if o.TotalAmount <= 0 {
		return errors.New("сумма заказа должна быть положительным числом")
	}

	return nil
}

// IsValidStatus проверяет, является ли статус заказа допустимым
func (o *Order) IsValidStatus(status string) bool {
	validStatuses := []string{
		OrderStatusCreated,
		OrderStatusPending,
		OrderStatusPaid,
		OrderStatusProcessed,
		OrderStatusCompleted,
		OrderStatusCancelled,
		OrderStatusRefunded,
	}

	for _, s := range validStatuses {
		if s == status {
			return true
		}
	}

	return false
}

// CalculateTotal рассчитывает общую сумму заказа на основе товаров
func (o *Order) CalculateTotal() float64 {
	var total float64 = 0

	for _, item := range o.Items {
		if item.Price > 0 {
			total += float64(item.Quantity) * item.Price
		}
	}

	return total
}

// Payment представляет платежную операцию
type Payment struct {
	ID            int        `json:"id"`
	OrderID       int        `json:"order_id"`               // Связанный заказ
	Amount        float64    `json:"amount"`                 // Сумма платежа
	Status        string     `json:"status"`                 // Статус платежа
	TransactionID string     `json:"transaction_id"`         // ID транзакции
	CreatedAt     time.Time  `json:"created_at"`             // Дата создания платежа
	CompletedAt   *time.Time `json:"completed_at,omitempty"` // Дата завершения платежа
	Currency      string     `json:"currency,omitempty"`     // Валюта платежа
}

// Validate проверяет корректность данных платежа
func (p *Payment) Validate() error {
	if p.OrderID <= 0 {
		return errors.New("ID заказа должен быть положительным числом")
	}

	if p.Amount <= 0 {
		return errors.New("сумма платежа должна быть положительным числом")
	}

	return nil
}

// IsValidStatus проверяет, является ли статус платежа допустимым
func (p *Payment) IsValidStatus(status string) bool {
	validStatuses := []string{
		PaymentStatusPending,
		PaymentStatusProcessing,
		PaymentStatusCompleted,
		PaymentStatusFailed,
		PaymentStatusRefunded,
		PaymentStatusCancelled,
	}

	for _, s := range validStatuses {
		if s == status {
			return true
		}
	}

	return false
}

// IsCompleted проверяет, завершен ли платеж
func (p *Payment) IsCompleted() bool {
	return p.Status == PaymentStatusCompleted
}
