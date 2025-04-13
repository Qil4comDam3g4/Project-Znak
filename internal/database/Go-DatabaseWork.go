package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"os"
	"time"

	"project-znak/models"

	_ "github.com/lib/pq"
)

// GTINData определяет данные о товаре по GTIN
type GTINData struct {
	GTIN  string `json:"gtin"`
	Count int    `json:"count"`
}

// Инициализация БД
func initDB() *sql.DB {
	connStr := fmt.Sprintf(
		"host=%s port=%s user=%s password=%s dbname=%s sslmode=disable",
		os.Getenv("DB_HOST"),
		os.Getenv("DB_PORT"),
		os.Getenv("DB_USER"),
		os.Getenv("DB_PASSWORD"),
		os.Getenv("DB_NAME"))

	db, err := sql.Open("postgres", connStr)
	if err != nil {
		log.Fatal("Ошибка подключения к БД:", err)
	}

	// Конфигурация пула соединений
	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(5 * time.Minute)

	if err = db.Ping(); err != nil {
		log.Fatal("Ошибка проверки соединения:", err)
	}

	log.Println("Успешное подключение к PostgreSQL")
	return db
}

// Расширенные функции работы с БД
func createUserTx(tx *sql.Tx, user *models.User) error {
	err := tx.QueryRow(
		`INSERT INTO users 
        (first_name, last_name, middle_name, inn, telegram_id, email, username, api_key) 
        VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
        RETURNING id, registered_at`,
		user.FirstName,
		user.LastName,
		user.MiddleName,
		user.INN,
		user.TelegramID,
		user.Email,
		user.Username,
		user.APIKey,
	).Scan(&user.ID, &user.RegisteredAt)

	return err
}

func createOrderTx(tx *sql.Tx, order *models.Order, items []models.OrderItem) error {
	err := tx.QueryRow(
		`INSERT INTO orders 
        (user_id, total_amount, status, payment_id)
        VALUES ($1, $2, $3, $4)
        RETURNING id, created_at`,
		order.UserID,
		order.TotalAmount,
		order.Status,
		order.PaymentID,
	).Scan(&order.ID, &order.CreatedAt)

	if err != nil {
		return err
	}

	for i := range items {
		_, err = tx.Exec(
			`INSERT INTO order_items 
            (order_id, gtin, quantity, price)
            VALUES ($1, $2, $3, $4)`,
			order.ID,
			items[i].GTIN,
			items[i].Quantity,
			items[i].Price,
		)
		if err != nil {
			return err
		}
	}
	return nil
}

// Функция для подготовки заказа на основе данных о товарах
func prepareOrder(telegramID int64, gtins []GTINData) (models.Order, []models.OrderItem) {
	var order models.Order
	var items []models.OrderItem
	var totalAmount float64 = 0

	// Заполняем основную информацию о заказе
	order = models.Order{
		Status: models.OrderStatusCreated,
	}

	// Подготавливаем список товаров для заказа
	for _, gtin := range gtins {
		// Здесь можно добавить расчет стоимости на основе GTIN
		// Примерная цена за единицу
		unitPrice := 100.0 // Заглушка

		item := models.OrderItem{
			GTIN:     gtin.GTIN,
			Quantity: gtin.Count,
			Price:    unitPrice,
		}

		totalAmount += float64(gtin.Count) * unitPrice
		items = append(items, item)
	}

	// Обновляем общую сумму заказа
	order.TotalAmount = totalAmount

	return order, items
}

// Обработчики
func handleKIZRequest(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	var request struct {
		TelegramID int64      `json:"telegram_id"`
		GTINs      []GTINData `json:"gtin_data"`
		INN        string     `json:"inn"`
	}

	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		respondError(w, http.StatusBadRequest, "Неверный формат запроса")
		return
	}

	tx, err := db.Begin()
	if err != nil {
		respondError(w, http.StatusInternalServerError, "Ошибка транзакции")
		return
	}
	defer tx.Rollback()

	userID, err := getUserID(tx, request.TelegramID)
	if err != nil {
		log.Printf("Ошибка поиска пользователя: %v", err)
		respondError(w, http.StatusInternalServerError, "Ошибка БД")
		return
	}

	if userID == 0 {
		respondError(w, http.StatusUnauthorized, "Пользователь не найден")
		return
	}

	order, items := prepareOrder(request.TelegramID, request.GTINs)
	order.UserID = userID // Устанавливаем ID пользователя

	if err := createOrderTx(tx, &order, items); err != nil {
		log.Printf("Ошибка создания заказа: %v", err)
		respondError(w, http.StatusInternalServerError, "Ошибка создания заказа")
		return
	}

	if err := tx.Commit(); err != nil {
		respondError(w, http.StatusInternalServerError, "Ошибка фиксации транзакции")
		return
	}

	// Генерация КИЗ через API Честного знака
	kizs, err := requestKIZFromExternalAPI(request.GTINs, request.INN)
	if err != nil {
		log.Printf("Ошибка запроса КИЗ: %v", err)
		// Продолжаем выполнение, заказ уже создан
	}

	respondJSON(w, http.StatusCreated, map[string]any{
		"order_id": order.ID,
		"status":   "created",
		"kizs":     kizs,
	})
}

// Заглушка для запроса КИЗ из внешнего API
func requestKIZFromExternalAPI(gtins []GTINData, inn string) ([]string, error) {
	// Заглушка для тестирования
	kizs := []string{"KIZ123456", "KIZ789012"}
	return kizs, nil
}

func handlePayment(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	var paymentData struct {
		TelegramID int64   `json:"telegram_id"`
		Amount     float64 `json:"amount"`
		OrderID    int     `json:"order_id"`
	}

	if err := json.NewDecoder(r.Body).Decode(&paymentData); err != nil {
		respondError(w, http.StatusBadRequest, "Неверный формат запроса")
		return
	}

	tx, err := db.Begin()
	if err != nil {
		respondError(w, http.StatusInternalServerError, "Ошибка транзакции")
		return
	}
	defer tx.Rollback()

	userID, err := getUserID(tx, paymentData.TelegramID)
	if err != nil || userID == 0 {
		respondError(w, http.StatusUnauthorized, "Пользователь не авторизован")
		return
	}

	// Создаем платеж
	payment := models.Payment{
		OrderID: paymentData.OrderID,
		Amount:  paymentData.Amount,
		Status:  models.PaymentStatusPending,
	}

	// Сохраняем платеж в БД
	err = tx.QueryRow(
		`INSERT INTO payments (order_id, amount, status) 
         VALUES ($1, $2, $3) 
         RETURNING id, transaction_id, created_at`,
		payment.OrderID,
		payment.Amount,
		payment.Status,
	).Scan(&payment.ID, &payment.TransactionID, &payment.CreatedAt)

	if err != nil {
		tx.Rollback()
		respondJSON(w, http.StatusInternalServerError, map[string]interface{}{
			"status":  "error",
			"message": "Ошибка создания платежа",
		})
		return
	}

	if err := tx.Commit(); err != nil {
		respondError(w, http.StatusInternalServerError, "Ошибка фиксации")
		return
	}

	// Генерация ссылки на оплату через Robokassa
	paymentURL := generatePaymentURL(payment.ID, payment.Amount)

	respondJSON(w, http.StatusOK, map[string]any{
		"payment_id":  payment.ID,
		"status":      "created",
		"payment_url": paymentURL,
	})
}

// Генерация URL для оплаты через Robokassa
func generatePaymentURL(paymentID int, amount float64) string {
	// Заглушка, в реальном приложении здесь будет логика формирования URL
	return fmt.Sprintf("https://auth.robokassa.ru/Merchant/Index.aspx?MerchantLogin=test&OutSum=%g&InvId=%d&SignatureValue=test", amount, paymentID)
}

// Вспомогательные функции
func getUserID(tx *sql.Tx, telegramID int64) (int, error) {
	var id int
	err := tx.QueryRow(
		"SELECT id FROM users WHERE telegram_id = $1",
		telegramID,
	).Scan(&id)

	if err == sql.ErrNoRows {
		return 0, nil
	}
	return id, err
}

func respondJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

func respondError(w http.ResponseWriter, status int, message string) {
	respondJSON(w, status, map[string]string{"error": message})
}

func main() {
	db := initDB()
	defer db.Close()

	// Создание таблиц, если их нет
	createTables(db)

	// Обновленные обработчики с передачей db в качестве параметра
	http.HandleFunc("/api/kizs", func(w http.ResponseWriter, r *http.Request) {
		handleKIZRequest(w, r, db)
	})

	http.HandleFunc("/api/payments", func(w http.ResponseWriter, r *http.Request) {
		handlePayment(w, r, db)
	})

	http.HandleFunc("/api/users/register", func(w http.ResponseWriter, r *http.Request) {
		handleUserRegistration(w, r, db)
	})

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	log.Printf("Сервер запущен на порту %s", port)
	log.Fatal(http.ListenAndServe(":"+port, nil))
}

// Создание необходимых таблиц
func createTables(db *sql.DB) {
	queries := []string{
		`CREATE TABLE IF NOT EXISTS users (
			id SERIAL PRIMARY KEY,
			first_name VARCHAR(50),
			last_name VARCHAR(50),
			middle_name VARCHAR(50),
			inn VARCHAR(12),
			telegram_id BIGINT UNIQUE NOT NULL,
			email VARCHAR(100),
			username VARCHAR(50),
			registered_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			api_key TEXT UNIQUE,
			last_active TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		);`,

		`CREATE TABLE IF NOT EXISTS orders (
			id SERIAL PRIMARY KEY,
			user_id INT REFERENCES users(id) ON DELETE CASCADE,
			order_date TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			total_amount DECIMAL(10, 2) NOT NULL,
			status VARCHAR(20) DEFAULT 'created',
			payment_id VARCHAR(50)
		);`,

		`CREATE TABLE IF NOT EXISTS order_items (
			id SERIAL PRIMARY KEY,
			order_id INT REFERENCES orders(id) ON DELETE CASCADE,
			gtin VARCHAR(14) NOT NULL,
			quantity INT NOT NULL
		);`,

		`CREATE TABLE IF NOT EXISTS payments (
			id SERIAL PRIMARY KEY,
			user_id INT REFERENCES users(id) ON DELETE SET NULL,
			order_id INT REFERENCES orders(id) ON DELETE SET NULL,
			amount DECIMAL(10, 2) NOT NULL,
			currency VARCHAR(3) DEFAULT 'RUB',
			status VARCHAR(20) DEFAULT 'pending',
			transaction_id VARCHAR(100),
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			completed_at TIMESTAMP
		);`,

		`CREATE TABLE IF NOT EXISTS kiz_requests (
			id SERIAL PRIMARY KEY,
			user_id INT REFERENCES users(id),
			telegram_id BIGINT NOT NULL,
			inn TEXT NOT NULL,
			request_time TIMESTAMP NOT NULL,
			status TEXT NOT NULL DEFAULT 'pending',
			request_data JSONB
		);`,

		`CREATE TABLE IF NOT EXISTS kiz_results (
			id SERIAL PRIMARY KEY,
			request_id INT REFERENCES kiz_requests(id),
			kiz_data JSONB,
			file_path TEXT,
			created_at TIMESTAMP NOT NULL DEFAULT NOW()
		);`,
	}

	for _, query := range queries {
		_, err := db.Exec(query)
		if err != nil {
			log.Printf("Ошибка создания таблицы: %v", err)
		}
	}
}

// Обработчик для регистрации новых пользователей
func handleUserRegistration(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	if r.Method != http.MethodPost {
		http.Error(w, "Метод не поддерживается", http.StatusMethodNotAllowed)
		return
	}

	var request struct {
		FirstName  string `json:"first_name"`
		LastName   string `json:"last_name"`
		MiddleName string `json:"middle_name,omitempty"`
		INN        string `json:"inn"`
		TelegramID int64  `json:"telegram_id"`
		Email      string `json:"email,omitempty"`
		Username   string `json:"username,omitempty"`
	}

	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		respondError(w, http.StatusBadRequest, "Неверный формат запроса")
		return
	}
	defer r.Body.Close()

	// Валидация входных данных
	if request.TelegramID <= 0 || request.INN == "" {
		respondError(w, http.StatusBadRequest, "Обязательные поля не заполнены")
		return
	}

	// Создание транзакции
	tx, err := db.Begin()
	if err != nil {
		respondError(w, http.StatusInternalServerError, "Ошибка транзакции")
		return
	}
	defer tx.Rollback()

	// Генерация API ключа
	apiKey := generateRandomAPIKey()

	user := &models.User{
		FirstName:  request.FirstName,
		LastName:   request.LastName,
		MiddleName: request.MiddleName,
		INN:        request.INN,
		TelegramID: request.TelegramID,
		Email:      request.Email,
		Username:   request.Username,
		APIKey:     apiKey,
		LastActive: time.Now(),
	}

	if err := createUserTx(tx, user); err != nil {
		log.Printf("Ошибка создания пользователя: %v", err)
		respondError(w, http.StatusInternalServerError, "Ошибка регистрации пользователя")
		return
	}

	if err := tx.Commit(); err != nil {
		respondError(w, http.StatusInternalServerError, "Ошибка фиксации транзакции")
		return
	}

	respondJSON(w, http.StatusCreated, map[string]any{
		"user_id": user.ID,
		"api_key": user.APIKey,
		"message": "Пользователь успешно зарегистрирован",
	})
}

// Генерация случайного API ключа
func generateRandomAPIKey() string {
	b := make([]byte, 16)
	_, err := rand.Read(b)
	if err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return fmt.Sprintf("%x", b)
}
