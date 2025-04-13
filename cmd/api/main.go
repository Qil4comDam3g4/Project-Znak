package main

import (
	"context"
	"crypto/rand"
	"crypto/sha1"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"project-znak/internal/models"

	"github.com/jung-kurt/gofpdf"
	_ "github.com/lib/pq"
	"golang.org/x/time/rate"
)

// Конфигурация приложения
type Config struct {
	HTTPPort          string
	DBConfig          DBConfig
	ChestnyZnakConfig ChestnyZnakConfig
	PaymentConfig     PaymentConfig
}

type DBConfig struct {
	Host     string
	Port     string
	User     string
	Password string
	Name     string
}

type ChestnyZnakConfig struct {
	URL            string
	PrivateKeyPath string
	CertPath       string
}

type PaymentConfig struct {
	RobokassaLogin string
	RobokassaPass  string
}

var config Config

// Тип для ключей контекста, чтобы избежать коллизий
type contextKey string

// Константы для ключей контекста
const userIDKey contextKey = "userID"

// Инициализация конфигурации
func initConfig() Config {
	return Config{
		HTTPPort: getEnv("HTTP_PORT", "8080"),
		DBConfig: DBConfig{
			Host:     getEnv("DB_HOST", "localhost"),
			Port:     getEnv("DB_PORT", "5432"),
			User:     getEnv("DB_USER", "postgres"),
			Password: getEnv("DB_PASSWORD", ""),
			Name:     getEnv("DB_NAME", "my_bot_db"),
		},
		ChestnyZnakConfig: ChestnyZnakConfig{
			URL:            getEnv("CHESTNY_ZNAK_URL", "http://api.stage.mdlp.crpt.ru"),
			PrivateKeyPath: getEnv("PRIVATE_KEY_PATH", "/certs/private.pem"),
			CertPath:       getEnv("CERTIFICATE_PATH", "/certs/cert.pem"),
		},
		PaymentConfig: PaymentConfig{
			RobokassaLogin: getEnv("ROBOKASSA_LOGIN", ""),    //Тут проставить логин после регистрации
			RobokassaPass:  getEnv("ROBOKASSA_PASSWORD", ""), //Тут тоже самое
		},
	}
}

// Инициализация базы данных
func initDB(config DBConfig) (*sql.DB, error) {
	connStr := fmt.Sprintf(
		"host=%s port=%s user=%s password=%s dbname=%s sslmode=disable",
		config.Host,
		config.Port,
		config.User,
		config.Password,
		config.Name,
	)

	db, err := sql.Open("postgres", connStr)
	if err != nil {
		return nil, fmt.Errorf("ошибка подключения к БД: %w", err)
	}

	// Установка параметров соединения
	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(5 * time.Minute)

	if err = db.Ping(); err != nil {
		return nil, fmt.Errorf("ошибка проверки соединения: %w", err)
	}

	return db, nil
}

// Генерация PDF
func generateKIZPDF(kizs []string) (string, error) {
	// Создание директории для временных файлов, если не существует
	tempDir := "./temp"
	if err := os.MkdirAll(tempDir, 0755); err != nil {
		return "", fmt.Errorf("ошибка создания директории: %w", err)
	}

	pdf := gofpdf.New("P", "mm", "A4", "")
	pdf.AddPage()
	pdf.SetFont("Arial", "B", 16)
	pdf.Cell(40, 10, "Коды маркировки")
	pdf.Ln(12)

	pdf.SetFont("Arial", "", 12)
	for i, kiz := range kizs {
		pdf.Cell(0, 10, fmt.Sprintf("%d. %s", i+1, kiz))
		pdf.Ln(8)
	}

	// Использование временной директории и уникального имени файла
	filename := filepath.Join(tempDir, fmt.Sprintf("kizs_%d.pdf", time.Now().UnixNano()))
	if err := pdf.OutputFileAndClose(filename); err != nil {
		return "", fmt.Errorf("ошибка создания PDF: %w", err)
	}

	// Планирование удаления файла через некоторое время
	go func(fname string) {
		time.Sleep(1 * time.Hour)
		os.Remove(fname)
	}(filename)

	return filename, nil
}

// Модели данных API-ответов и запросов

type KIZRequestRecord struct {
	ID          int             `json:"id"`
	UserID      int             `json:"user_id"`
	TelegramID  int64           `json:"telegram_id"`
	INN         string          `json:"inn"`
	RequestTime time.Time       `json:"request_time"`
	Status      string          `json:"status"`
	RequestData json.RawMessage `json:"request_data,omitempty"`
}

type KIZResult struct {
	ID        int             `json:"id"`
	RequestID int             `json:"request_id"`
	KIZData   json.RawMessage `json:"kiz_data,omitempty"`
	FilePath  string          `json:"file_path,omitempty"`
	CreatedAt time.Time       `json:"created_at"`
}

// Структуры запросов и ответов для API
type UserRegistrationRequest struct {
	TelegramID int64  `json:"telegram_id"`
	INN        string `json:"inn"`
	Email      string `json:"email,omitempty"`
}

type PaymentRequest struct {
	TelegramID int64   `json:"telegram_id"`
	Amount     float64 `json:"amount"`
	ReturnURL  string  `json:"return_url,omitempty"`
}

type PaymentResponse struct {
	Status      string `json:"status"`
	Message     string `json:"message"`
	RedirectURL string `json:"redirect_url,omitempty"`
	PaymentID   int    `json:"payment_id,omitempty"`
	ErrorMsg    string `json:"error,omitempty"`
}

// Структура запроса
type KIZRequest struct {
	TelegramID int64    `json:"telegram_id"`
	GTINs      []string `json:"gtins"`
	INN        string   `json:"inn"`
}

// Структура ответа
type KIZResponse struct {
	Status   string   `json:"status"`
	Message  string   `json:"message"`
	KIZs     []string `json:"kizs,omitempty"`
	FilePath string   `json:"file_path,omitempty"`
	ErrorMsg string   `json:"error,omitempty"`
}

// Главная функция инициализации маршрутов
func setupRoutes(db *sql.DB, logger *log.Logger) http.Handler {
	mux := http.NewServeMux()

	// Существующие эндпоинты
	mux.HandleFunc("/api/kizs", kizHandler(db, logger))
	mux.HandleFunc("/health", healthCheckHandler())

	// Новые эндпоинты для пользователей
	mux.HandleFunc("/api/users", usersHandler(db, logger))
	mux.HandleFunc("/api/users/register", registerUserHandler(db, logger))

	// Эндпоинты для работы с историей запросов
	mux.HandleFunc("/api/requests", requestsHandler(db, logger))
	mux.HandleFunc("/api/requests/status", requestStatusHandler(db, logger))

	// Эндпоинты для оплаты
	mux.HandleFunc("/api/payments/create", createPaymentHandler(db, logger))
	mux.HandleFunc("/api/payments/callback", robokassaCallbackHandler(db, logger))
	mux.HandleFunc("/api/payments/status", paymentStatusHandler(db, logger))

	// Статическая документация API
	fileServer := http.FileServer(http.Dir("./docs"))
	mux.Handle("/docs/", http.StripPrefix("/docs/", fileServer))

	// Применение middleware
	handler := authMiddleware(db, logger)(mux)
	handler = logMiddleware(logger)(handler)
	handler = corsMiddleware(handler)
	handler = rateLimitMiddleware(10, 20)(handler) // 10 запросов в секунду с возможностью пика до 20

	return handler
}

// Обработчик для проверки статуса сервиса
func healthCheckHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Метод не поддерживается", http.StatusMethodNotAllowed)
			return
		}

		status := map[string]string{
			"status":    "ok",
			"timestamp": time.Now().Format(time.RFC3339),
			"version":   "1.0.0",
		}

		sendJSONResponse(w, status, http.StatusOK)
	}
}

// Обработчик для регистрации пользователей
func registerUserHandler(db *sql.DB, logger *log.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Метод не поддерживается", http.StatusMethodNotAllowed)
			return
		}

		var request UserRegistrationRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			logger.Printf("Ошибка декодирования JSON: %v", err)
			sendJSONResponse(w, map[string]string{
				"status":  "error",
				"message": "Неверный формат запроса",
				"error":   err.Error(),
			}, http.StatusBadRequest)
			return
		}
		defer r.Body.Close()

		// Валидация входных данных
		if request.TelegramID <= 0 || request.INN == "" {
			sendJSONResponse(w, map[string]string{
				"status":  "error",
				"message": "Обязательные поля не заполнены",
			}, http.StatusBadRequest)
			return
		}

		// Генерация API ключа
		apiKey := generateAPIKey()

		// Проверка существования пользователя
		var exists bool
		err := db.QueryRow("SELECT EXISTS(SELECT 1 FROM users WHERE telegram_id = $1)",
			request.TelegramID).Scan(&exists)
		if err != nil {
			logger.Printf("Ошибка проверки пользователя: %v", err)
			sendJSONResponse(w, map[string]string{
				"status":  "error",
				"message": "Ошибка при обработке запроса",
			}, http.StatusInternalServerError)
			return
		}

		var userID int
		if exists {
			// Обновление данных пользователя
			err = db.QueryRow("UPDATE users SET inn = $1, email = $2, last_active = $3, api_key = $4 WHERE telegram_id = $5 RETURNING id",
				request.INN, request.Email, time.Now(), apiKey, request.TelegramID).Scan(&userID)
		} else {
			// Создание нового пользователя
			err = db.QueryRow("INSERT INTO users (telegram_id, inn, email, api_key) VALUES ($1, $2, $3, $4) RETURNING id",
				request.TelegramID, request.INN, request.Email, apiKey).Scan(&userID)
		}

		if err != nil {
			logger.Printf("Ошибка сохранения пользователя: %v", err)
			sendJSONResponse(w, map[string]string{
				"status":  "error",
				"message": "Ошибка при сохранении данных",
			}, http.StatusInternalServerError)
			return
		}

		sendJSONResponse(w, map[string]interface{}{
			"status":  "success",
			"message": "Пользователь успешно зарегистрирован",
			"user_id": userID,
			"api_key": apiKey,
		}, http.StatusOK)
	}
}

// Обработчик для управления пользователями
func usersHandler(db *sql.DB, logger *log.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Получение информации о пользователе по TelegramID
		if r.Method == http.MethodGet {
			telegramID := r.URL.Query().Get("telegram_id")
			if telegramID == "" {
				sendJSONResponse(w, map[string]string{
					"status":  "error",
					"message": "Необходимо указать telegram_id",
				}, http.StatusBadRequest)
				return
			}

			var user models.User
			err := db.QueryRow(`
				SELECT id, telegram_id, inn, email, registered_at, last_active 
				FROM users WHERE telegram_id = $1
			`, telegramID).Scan(
				&user.ID,
				&user.TelegramID,
				&user.INN,
				&user.Email,
				&user.RegisteredAt,
				&user.LastActive,
			)

			if err == sql.ErrNoRows {
				sendJSONResponse(w, map[string]string{
					"status":  "error",
					"message": "Пользователь не найден",
				}, http.StatusNotFound)
				return
			} else if err != nil {
				logger.Printf("Ошибка запроса пользователя: %v", err)
				sendJSONResponse(w, map[string]string{
					"status":  "error",
					"message": "Ошибка при получении данных",
				}, http.StatusInternalServerError)
				return
			}

			sendJSONResponse(w, map[string]interface{}{
				"status": "success",
				"user":   user,
			}, http.StatusOK)
			return
		}

		http.Error(w, "Метод не поддерживается", http.StatusMethodNotAllowed)
	}
}

// Обработчик для истории запросов
func requestsHandler(db *sql.DB, logger *log.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Метод не поддерживается", http.StatusMethodNotAllowed)
			return
		}

		telegramID := r.URL.Query().Get("telegram_id")
		if telegramID == "" {
			sendJSONResponse(w, map[string]string{
				"status":  "error",
				"message": "Необходимо указать telegram_id",
			}, http.StatusBadRequest)
			return
		}

		limit := 10 // По умолчанию 10 записей
		limitParam := r.URL.Query().Get("limit")
		if limitParam != "" {
			if _, err := fmt.Sscanf(limitParam, "%d", &limit); err != nil {
				limit = 10
			}
		}

		rows, err := db.Query(`
			SELECT r.id, r.user_id, r.telegram_id, r.inn, r.request_time, r.status, r.request_data,
				   res.file_path
			FROM kiz_requests r
			LEFT JOIN kiz_results res ON r.id = res.request_id
			WHERE r.telegram_id = $1
			ORDER BY r.request_time DESC
			LIMIT $2
		`, telegramID, limit)

		if err != nil {
			logger.Printf("Ошибка запроса истории: %v", err)
			sendJSONResponse(w, map[string]string{
				"status":  "error",
				"message": "Ошибка при получении данных",
			}, http.StatusInternalServerError)
			return
		}
		defer rows.Close()

		var requests []map[string]any
		for rows.Next() {
			var req KIZRequestRecord
			var filePath sql.NullString
			var requestData sql.NullString

			if err := rows.Scan(&req.ID, &req.UserID, &req.TelegramID, &req.INN,
				&req.RequestTime, &req.Status, &requestData, &filePath); err != nil {
				logger.Printf("Ошибка сканирования строки: %v", err)
				continue
			}

			requestInfo := map[string]any{
				"id":           req.ID,
				"telegram_id":  req.TelegramID,
				"inn":          req.INN,
				"request_time": req.RequestTime,
				"status":       req.Status,
			}

			if requestData.Valid {
				requestInfo["request_data"] = json.RawMessage(requestData.String)
			}

			if filePath.Valid {
				requestInfo["file_path"] = filePath.String
			}

			requests = append(requests, requestInfo)
		}

		sendJSONResponse(w, map[string]any{
			"status":   "success",
			"requests": requests,
		}, http.StatusOK)
	}
}

// Обработчик статуса запроса
func requestStatusHandler(db *sql.DB, logger *log.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Метод не поддерживается", http.StatusMethodNotAllowed)
			return
		}

		requestID := r.URL.Query().Get("id")
		if requestID == "" {
			sendJSONResponse(w, map[string]string{
				"status":  "error",
				"message": "Необходимо указать id запроса",
			}, http.StatusBadRequest)
			return
		}

		var req KIZRequestRecord
		var filePath, kizData sql.NullString

		err := db.QueryRow(`
			SELECT r.id, r.user_id, r.telegram_id, r.inn, r.request_time, r.status, r.request_data,
				   res.file_path, res.kiz_data
			FROM kiz_requests r
			LEFT JOIN kiz_results res ON r.id = res.request_id
			WHERE r.id = $1
		`, requestID).Scan(
			&req.ID, &req.UserID, &req.TelegramID, &req.INN,
			&req.RequestTime, &req.Status, &req.RequestData, &filePath, &kizData,
		)

		if err == sql.ErrNoRows {
			sendJSONResponse(w, map[string]string{
				"status":  "error",
				"message": "Запрос не найден",
			}, http.StatusNotFound)
			return
		} else if err != nil {
			logger.Printf("Ошибка получения статуса: %v", err)
			sendJSONResponse(w, map[string]string{
				"status":  "error",
				"message": "Ошибка при получении данных",
			}, http.StatusInternalServerError)
			return
		}

		response := map[string]any{
			"status":       "success",
			"request_id":   req.ID,
			"telegram_id":  req.TelegramID,
			"inn":          req.INN,
			"request_time": req.RequestTime,
			"status_code":  req.Status,
		}

		if len(req.RequestData) > 0 {
			response["request_data"] = req.RequestData
		}

		if filePath.Valid {
			response["file_path"] = filePath.String
		}

		if kizData.Valid {
			var kizDataJSON json.RawMessage
			if err := json.Unmarshal([]byte(kizData.String), &kizDataJSON); err == nil {
				response["kiz_data"] = kizDataJSON
			}
		}

		sendJSONResponse(w, response, http.StatusOK)
	}
}

// Обработчик создания платежа
func createPaymentHandler(db *sql.DB, logger *log.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Метод не поддерживается", http.StatusMethodNotAllowed)
			return
		}

		var request PaymentRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			logger.Printf("Ошибка декодирования JSON: %v", err)
			sendJSONResponse(w, PaymentResponse{
				Status:   "error",
				Message:  "Неверный формат запроса",
				ErrorMsg: err.Error(),
			}, http.StatusBadRequest)
			return
		}
		defer r.Body.Close()

		// Проверка суммы
		if request.Amount <= 0 {
			sendJSONResponse(w, PaymentResponse{
				Status:  "error",
				Message: "Неверная сумма платежа",
			}, http.StatusBadRequest)
			return
		}

		// Получение ID пользователя
		var userID int
		err := db.QueryRow("SELECT id FROM users WHERE telegram_id = $1", request.TelegramID).Scan(&userID)
		if err == sql.ErrNoRows {
			sendJSONResponse(w, PaymentResponse{
				Status:  "error",
				Message: "Пользователь не найден",
			}, http.StatusNotFound)
			return
		} else if err != nil {
			logger.Printf("Ошибка получения пользователя: %v", err)
			sendJSONResponse(w, PaymentResponse{
				Status:  "error",
				Message: "Ошибка при обработке запроса",
			}, http.StatusInternalServerError)
			return
		}

		// Создание записи о платеже
		var paymentID int
		err = db.QueryRow(`
			INSERT INTO payments (user_id, amount, status)
			VALUES ($1, $2, 'pending')
			RETURNING id
		`, userID, request.Amount).Scan(&paymentID)

		if err != nil {
			logger.Printf("Ошибка создания платежа: %v", err)
			sendJSONResponse(w, PaymentResponse{
				Status:  "error",
				Message: "Ошибка создания платежа",
			}, http.StatusInternalServerError)
			return
		}

		// Получение робокасса конфига
		rk := config.PaymentConfig

		// Формирование URL для оплаты через Robokassa
		returnURL := request.ReturnURL
		if returnURL == "" {
			returnURL = "https://t.me/your_bot"
		}

		// Формирование подписи запроса
		// merchantLogin:OutSum:InvId:Пароль
		signature := fmt.Sprintf("%s:%g:%d:%s", rk.RobokassaLogin, request.Amount, paymentID, rk.RobokassaPass)
		signatureHash := fmt.Sprintf("%x", sha1.Sum([]byte(signature)))

		// Формирование URL для оплаты
		redirectURL := fmt.Sprintf(
			"https://auth.robokassa.ru/Merchant/Index.aspx?MerchantLogin=%s&OutSum=%g&InvId=%d&SignatureValue=%s&Desc=%s&Culture=ru",
			rk.RobokassaLogin, request.Amount, paymentID, signatureHash, "Оплата услуг",
		)

		sendJSONResponse(w, PaymentResponse{
			Status:      "success",
			Message:     "Платеж создан",
			PaymentID:   paymentID,
			RedirectURL: redirectURL,
		}, http.StatusOK)
	}
}

// Обработчик callback от Robokassa
func robokassaCallbackHandler(db *sql.DB, logger *log.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost && r.Method != http.MethodGet {
			http.Error(w, "Метод не поддерживается", http.StatusMethodNotAllowed)
			return
		}

		// Получение параметров
		r.ParseForm()

		invID := r.FormValue("InvId")
		outSum := r.FormValue("OutSum")
		signValue := r.FormValue("SignatureValue")

		// Валидация параметров
		if invID == "" || outSum == "" || signValue == "" {
			logger.Printf("Неверные параметры callback")
			http.Error(w, "Неверные параметры", http.StatusBadRequest)
			return
		}

		// Проверка подписи
		rk := config.PaymentConfig
		signature := fmt.Sprintf("%s:%s:%s:%s", rk.RobokassaLogin, outSum, invID, rk.RobokassaPass)
		expectedSign := fmt.Sprintf("%x", sha1.Sum([]byte(signature)))

		if signValue != expectedSign {
			logger.Printf("Неверная подпись: %s != %s", signValue, expectedSign)
			http.Error(w, "Неверная подпись", http.StatusForbidden)
			return
		}

		// Обновление статуса платежа
		paymentID, err := strconv.Atoi(invID)
		if err != nil {
			logger.Printf("Ошибка преобразования ID платежа: %v", err)
			http.Error(w, "Неверный ID платежа", http.StatusBadRequest)
			return
		}

		now := time.Now()
		_, err = db.Exec(`
			UPDATE payments 
			SET status = 'completed', completed_at = $1, robokassa_id = $2
			WHERE id = $3 AND status = 'pending'
		`, now, r.FormValue("Shp_TransactionId"), paymentID)

		if err != nil {
			logger.Printf("Ошибка обновления статуса платежа: %v", err)
			http.Error(w, "Ошибка обновления платежа", http.StatusInternalServerError)
			return
		}

		// Ответ для Robokassa
		w.Write([]byte("OK" + invID))
	}
}

// Обработчик статуса платежа
func paymentStatusHandler(db *sql.DB, logger *log.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Метод не поддерживается", http.StatusMethodNotAllowed)
			return
		}

		paymentIDStr := r.URL.Query().Get("id")
		if paymentIDStr == "" {
			sendJSONResponse(w, map[string]string{
				"status":  "error",
				"message": "Необходимо указать id платежа",
			}, http.StatusBadRequest)
			return
		}

		paymentID, err := strconv.Atoi(paymentIDStr)
		if err != nil {
			sendJSONResponse(w, map[string]string{
				"status":  "error",
				"message": "Некорректный ID платежа",
			}, http.StatusBadRequest)
			return
		}

		telegramIDStr := r.URL.Query().Get("telegram_id")
		if telegramIDStr == "" {
			sendJSONResponse(w, map[string]string{
				"status":  "error",
				"message": "Необходимо указать telegram_id",
			}, http.StatusBadRequest)
			return
		}

		telegramID, err := strconv.ParseInt(telegramIDStr, 10, 64)
		if err != nil {
			sendJSONResponse(w, map[string]string{
				"status":  "error",
				"message": "Некорректный telegram_id",
			}, http.StatusBadRequest)
			return
		}

		var payment models.Payment
		var completedAt sql.NullTime

		if err := db.QueryRow(`
			SELECT p.id, p.order_id, p.amount, p.status, p.transaction_id, p.created_at, p.completed_at, p.currency
			FROM payments p
			JOIN orders o ON p.order_id = o.id
			JOIN users u ON o.user_id = u.id
			WHERE p.id = $1 AND u.telegram_id = $2
		`, paymentID, telegramID).Scan(
			&payment.ID,
			&payment.OrderID,
			&payment.Amount,
			&payment.Status,
			&payment.TransactionID,
			&payment.CreatedAt,
			&completedAt,
			&payment.Currency,
		); err != nil {
			logger.Printf("Ошибка запроса статуса платежа: %v", err)
			sendJSONResponse(w, map[string]any{
				"status":  "error",
				"message": "Платеж не найден",
			}, http.StatusNotFound)
			return
		}

		if completedAt.Valid {
			payment.CompletedAt = &completedAt.Time
		}

		sendJSONResponse(w, map[string]any{
			"status":  "success",
			"payment": payment,
		}, http.StatusOK)
	}
}

// Дополнительные функции и middleware
func generateAPIKey() string {
	b := make([]byte, 16)
	_, err := rand.Read(b)
	if err != nil {
		return ""
	}
	return fmt.Sprintf("%x", b)
}

// Middleware для авторизации
func authMiddleware(db *sql.DB, logger *log.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Публичные маршруты, не требующие авторизации
			publicPaths := map[string]bool{
				"/health":                true,
				"/api/users/register":    true,
				"/api/payments/callback": true,
				"/docs/":                 true,
			}

			if publicPaths[r.URL.Path] || strings.HasPrefix(r.URL.Path, "/docs/") {
				next.ServeHTTP(w, r)
				return
			}

			// Проверка API ключа
			apiKey := r.Header.Get("X-API-Key")
			if apiKey == "" {
				// Если нет API ключа, можно продолжить для некоторых методов
				// но с ограниченным функционалом или с другой авторизацией
				// Например, для методов, которые работают с telegramID
				next.ServeHTTP(w, r)
				return
			}

			// Проверка API ключа в базе данных
			var userID int
			err := db.QueryRow("SELECT id FROM users WHERE api_key = $1", apiKey).Scan(&userID)
			if err != nil {
				if err != sql.ErrNoRows {
					logger.Printf("Ошибка проверки API ключа: %v", err)
				}
				// Не сообщаем клиенту о конкретной ошибке для безопасности
				http.Error(w, "Неавторизованный доступ", http.StatusUnauthorized)
				return
			}

			// Обновление времени последней активности
			_, err = db.Exec("UPDATE users SET last_active = $1 WHERE id = $2", time.Now(), userID)
			if err != nil {
				logger.Printf("Ошибка обновления времени активности: %v", err)
			}

			// Установка ID пользователя в контекст запроса
			ctx := context.WithValue(r.Context(), userIDKey, userID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// Middleware для ограничения частоты запросов
func rateLimitMiddleware(requestsPerSecond int, burst int) func(http.Handler) http.Handler {
	limiter := rate.NewLimiter(rate.Limit(requestsPerSecond), burst)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !limiter.Allow() {
				http.Error(w, "Слишком много запросов", http.StatusTooManyRequests)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// Обработчик запросов КИЗ
func kizHandler(db *sql.DB, logger *log.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Проверка метода
		if r.Method != http.MethodPost {
			http.Error(w, "Метод не поддерживается", http.StatusMethodNotAllowed)
			return
		}

		var request KIZRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			logger.Printf("Ошибка декодирования JSON: %v", err)
			sendJSONResponse(w, KIZResponse{
				Status:   "error",
				Message:  "Неверный формат запроса",
				ErrorMsg: err.Error(),
			}, http.StatusBadRequest)
			return
		}
		defer r.Body.Close()

		// Валидация запроса
		if request.TelegramID <= 0 || len(request.GTINs) == 0 || request.INN == "" {
			sendJSONResponse(w, KIZResponse{
				Status:  "error",
				Message: "Отсутствуют обязательные параметры",
			}, http.StatusBadRequest)
			return
		}

		// Заглушка для интеграции с ЧЗ
		// TODO: Заменить на реальную интеграцию с ЧЗ
		kizs := []string{"KIZ123456", "KIZ789012"}

		// Запись в БД информации о запросе
		_, err := db.Exec(
			"INSERT INTO kiz_requests (telegram_id, inn, request_time) VALUES ($1, $2, $3)",
			request.TelegramID, request.INN, time.Now(),
		)
		if err != nil {
			logger.Printf("Ошибка записи в БД: %v", err)
			// Продолжаем выполнение, это не критическая ошибка
		}

		// Генерация PDF
		filename, err := generateKIZPDF(kizs)
		if err != nil {
			logger.Printf("Ошибка генерации PDF: %v", err)
			sendJSONResponse(w, KIZResponse{
				Status:   "error",
				Message:  "Ошибка генерации PDF",
				ErrorMsg: err.Error(),
			}, http.StatusInternalServerError)
			return
		}

		sendJSONResponse(w, KIZResponse{
			Status:   "success",
			Message:  "КИЗы успешно сгенерированы",
			KIZs:     kizs,
			FilePath: filename,
		}, http.StatusOK)
	}
}

// Вспомогательная функция для отправки JSON-ответа
func sendJSONResponse(w http.ResponseWriter, response any, statusCode int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(response)
}

// Основная функция
func main() {
	// Настройка логгера
	logger := log.New(os.Stdout, "[API] ", log.LstdFlags|log.Lshortfile)

	// Инициализация конфигурации
	config = initConfig()

	// Инициализация базы данных
	db, err := initDB(config.DBConfig)
	if err != nil {
		logger.Fatalf("Ошибка инициализации БД: %v", err)
	}
	defer db.Close()

	// Создание таблиц, если они не существуют
	if err := createTables(db); err != nil {
		logger.Fatalf("Ошибка создания таблиц: %v", err)
	}

	// Настройка маршрутов и middleware
	handler := setupRoutes(db, logger)

	// Настройка сервера
	server := &http.Server{
		Addr:         ":" + config.HTTPPort,
		Handler:      handler,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	// Запуск сервера
	go func() {
		logger.Printf("Сервер запущен на порту %s", config.HTTPPort)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Fatalf("Ошибка сервера: %v", err)
		}
	}()

	// Создание директории для временных файлов
	if err := os.MkdirAll("./temp", 0755); err != nil {
		logger.Printf("Ошибка создания временной директории: %v", err)
	}

	// Запуск периодической очистки временных файлов
	go cleanupTempFiles(logger)

	// Health check endpoint
	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		// Проверяем подключение к базе данных
		err := db.Ping()
		if err != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			json.NewEncoder(w).Encode(map[string]string{"status": "error", "message": "Database connection failed"})
			return
		}

		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})

	// Обработка сигналов остановки
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	logger.Println("Завершение работы сервера...")

	// Graceful shutdown
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		logger.Fatalf("Ошибка завершения: %v", err)
	}
	logger.Println("Сервер остановлен")
}

// Функция периодической очистки временных файлов
func cleanupTempFiles(logger *log.Logger) {
	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()

	for range ticker.C {
		logger.Println("Очистка временных файлов...")
		files, err := filepath.Glob("./temp/*")
		if err != nil {
			logger.Printf("Ошибка поиска файлов: %v", err)
			continue
		}

		now := time.Now()
		for _, file := range files {
			info, err := os.Stat(file)
			if err != nil {
				logger.Printf("Ошибка получения информации о файле %s: %v", file, err)
				continue
			}

			// Удаление файлов старше 24 часов
			if now.Sub(info.ModTime()) > 24*time.Hour {
				if err := os.Remove(file); err != nil {
					logger.Printf("Ошибка удаления файла %s: %v", file, err)
				} else {
					logger.Printf("Удален файл: %s", file)
				}
			}
		}
	}
}

// Промежуточное ПО для логирования запросов
func logMiddleware(logger *log.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			logger.Printf("Запрос: %s %s", r.Method, r.URL.Path)
			next.ServeHTTP(w, r)
			logger.Printf("Запрос обработан за %v: %s %s", time.Since(start), r.Method, r.URL.Path)
		})
	}
}

// Промежуточное ПО для CORS
func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-API-Key")

		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// Создание необходимых таблиц
func createTables(db *sql.DB) error {
	queries := []string{
		`CREATE TABLE IF NOT EXISTS users (
			id SERIAL PRIMARY KEY,
			telegram_id BIGINT UNIQUE NOT NULL,
			inn TEXT NOT NULL,
			email TEXT,
			created_at TIMESTAMP NOT NULL DEFAULT NOW(),
			last_active TIMESTAMP NOT NULL DEFAULT NOW(),
			api_key TEXT UNIQUE
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

		`CREATE TABLE IF NOT EXISTS payments (
			id SERIAL PRIMARY KEY,
			user_id INT REFERENCES users(id),
			amount DECIMAL(10,2) NOT NULL,
			currency TEXT NOT NULL DEFAULT 'RUB',
			status TEXT NOT NULL DEFAULT 'pending',
			robokassa_id TEXT,
			created_at TIMESTAMP NOT NULL DEFAULT NOW(),
			completed_at TIMESTAMP
		);`,
	}

	for _, query := range queries {
		if _, err := db.Exec(query); err != nil {
			return fmt.Errorf("ошибка создания таблицы: %w", err)
		}
	}

	return nil
}

// Получение переменной окружения с дефолтным значением
func getEnv(key, defaultValue string) string {
	if value, exists := os.LookupEnv(key); exists && value != "" {
		return value
	}
	return defaultValue
}
