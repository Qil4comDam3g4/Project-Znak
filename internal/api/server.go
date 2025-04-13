package main

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"github.com/jung-kurt/gofpdf"
)

// Конфигурация приложения
type Config struct {
	ChestnyZnakAPIURL string
	PrivateKeyPath    string
	CertificatePath   string
	TelegramBotToken  string
	TelegramChatID    int64
	RobokassaLogin    string
	RobokassaPassword string
	Port              string
}

var config Config

// Структуры данных для работы с API
type GTINData struct {
	GTIN  string `json:"gtin"`
	Count int    `json:"count"`
}

type KIZRequestData struct {
	GTINData []GTINData `json:"gtin_data"`
	INN      string     `json:"inn"`
}

type KIZResponse struct {
	Status    string   `json:"status"`
	Message   string   `json:"message"`
	KIZs      []string `json:"kizs,omitempty"`
	FilePaths []string `json:"file_paths,omitempty"`
}

type PaymentRequestData struct {
	Amount  float64 `json:"amount"`
	OrderID string  `json:"order_id"`
}

type PaymentResponse struct {
	Status     string `json:"status"`
	Message    string `json:"message"`
	PaymentURL string `json:"payment_url,omitempty"`
}

// Загрузка конфигурации из переменных окружения
func loadConfig() Config {
	return Config{
		ChestnyZnakAPIURL: getEnv("CHESTNY_ZNAK_API_URL", "https://example.chestnyznak.ru/api/v3/"),
		PrivateKeyPath:    getEnv("PRIVATE_KEY_PATH", ""),
		CertificatePath:   getEnv("CERTIFICATE_PATH", ""),
		TelegramBotToken:  getEnv("TELEGRAM_BOT_TOKEN", ""),
		TelegramChatID:    getIntEnv("TELEGRAM_CHAT_ID", 0),
		RobokassaLogin:    getEnv("ROBOKASSA_LOGIN", ""),
		RobokassaPassword: getEnv("ROBOKASSA_PASSWORD", ""),
		Port:              getEnv("PORT", "8080"),
	}
}

// Получение строковой переменной окружения с дефолтным значением
func getEnv(key, defaultValue string) string {
	value := os.Getenv(key)
	if value == "" {
		return defaultValue
	}
	return value
}

// Получение целочисленной переменной окружения с дефолтным значением
func getIntEnv(key string, defaultValue int64) int64 {
	valStr := os.Getenv(key)
	if valStr == "" {
		return defaultValue
	}
	val, err := strconv.ParseInt(valStr, 10, 64)
	if err != nil {
		log.Printf("Некорректное значение %s: %v", key, err)
		return defaultValue
	}
	return val
}

// Проверка конфигурации на валидность
func validateConfig(cfg Config) error {
	if cfg.PrivateKeyPath == "" || cfg.CertificatePath == "" {
		log.Print("ВНИМАНИЕ: Пути к файлам ЭЦП не заданы")
	}

	if cfg.TelegramBotToken == "" || cfg.TelegramChatID == 0 {
		return errors.New("необходимо указать токен бота и ID чата Telegram")
	}

	if cfg.RobokassaLogin == "" || cfg.RobokassaPassword == "" {
		return errors.New("необходимо указать логин и пароль Robokassa")
	}

	return nil
}

// Загрузка приватного ключа из файла
func loadPrivateKey(path string) (crypto.PrivateKey, error) {
	keyData, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("ошибка чтения ключа: %w", err)
	}

	block, _ := pem.Decode(keyData)
	if block == nil {
		return nil, fmt.Errorf("неверный PEM-формат")
	}

	privateKey, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("ошибка парсинга ключа: %w", err)
	}

	return privateKey, nil
}

// Загрузка сертификата из файла
func loadCertificate(path string) (*x509.Certificate, error) {
	certData, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("ошибка чтения сертификата: %w", err)
	}

	block, _ := pem.Decode(certData)
	if block == nil {
		return nil, fmt.Errorf("неверный PEM-формат")
	}

	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("ошибка парсинга сертификата: %w", err)
	}

	return cert, nil
}

// Подписание данных с использованием закрытого ключа
func signData(data []byte, privateKey crypto.PrivateKey) ([]byte, error) {
	hasher := crypto.SHA256.New()
	hasher.Write(data)
	hashed := hasher.Sum(nil)

	signer, ok := privateKey.(crypto.Signer)
	if !ok {
		return nil, fmt.Errorf("неподдерживаемый тип ключа")
	}

	signature, err := signer.Sign(rand.Reader, hashed, crypto.SHA256)
	if err != nil {
		return nil, fmt.Errorf("ошибка подписи данных: %w", err)
	}

	return signature, nil
}

// Запрос кодов маркировки из API Честного ЗНАКа
func requestKIZs(ctx context.Context, requestData KIZRequestData) (KIZResponse, error) {
	privateKey, err := loadPrivateKey(config.PrivateKeyPath)
	if err != nil {
		log.Printf("Ошибка загрузки ключа: %v", err)
		return KIZResponse{Status: "error", Message: "Ошибка ЭЦП"}, err
	}

	cert, err := loadCertificate(config.CertificatePath)
	if err != nil {
		log.Printf("Ошибка загрузки сертификата: %v", err)
		return KIZResponse{Status: "error", Message: "Ошибка сертификата"}, err
	}

	body, err := json.Marshal(requestData)
	if err != nil {
		return KIZResponse{Status: "error", Message: "Ошибка формирования запроса"}, err
	}

	signature, err := signData(body, privateKey)
	if err != nil {
		log.Printf("Ошибка подписи: %v", err)
		return KIZResponse{Status: "error", Message: "Ошибка подписи"}, err
	}

	client := &http.Client{Timeout: 30 * time.Second}
	req, err := http.NewRequestWithContext(ctx, "POST", config.ChestnyZnakAPIURL+"kizs", bytes.NewReader(body))
	if err != nil {
		return KIZResponse{Status: "error", Message: "Ошибка создания запроса"}, err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Signature", base64.StdEncoding.EncodeToString(signature))
	req.Header.Set("X-Certificate", base64.StdEncoding.EncodeToString(cert.Raw))

	resp, err := client.Do(req)
	if err != nil {
		log.Printf("Ошибка запроса: %v", err)
		return KIZResponse{Status: "error", Message: "Ошибка соединения"}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		log.Printf("API вернуло ошибку: %d, тело: %s", resp.StatusCode, string(bodyBytes))
		return KIZResponse{Status: "error", Message: fmt.Sprintf("API вернуло ошибку: %d", resp.StatusCode)}, fmt.Errorf("API вернуло ошибку: %d", resp.StatusCode)
	}

	var result KIZResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return KIZResponse{Status: "error", Message: "Ошибка декодирования ответа"}, err
	}

	// Генерация файлов с кодами маркировки
	filePaths, err := generateKIZFiles(result.KIZs)
	if err != nil {
		log.Printf("Ошибка генерации файлов: %v", err)
		// Продолжаем работу, так как основные данные получены
	}

	result.FilePaths = filePaths
	return result, nil
}

// Генерация файлов с кодами маркировки
func generateKIZFiles(kizs []string) ([]string, error) {
	if len(kizs) == 0 {
		return nil, nil
	}

	// Создание директории для временных файлов, если не существует
	tempDir := "./temp"
	if err := os.MkdirAll(tempDir, 0755); err != nil {
		return nil, fmt.Errorf("ошибка создания директории: %w", err)
	}

	// Генерация PDF-файла
	timestamp := time.Now().Format("20060102-150405")
	filename := filepath.Join(tempDir, fmt.Sprintf("kizs_%s.pdf", timestamp))

	if err := generatePDF(kizs, filename); err != nil {
		return nil, fmt.Errorf("ошибка создания PDF: %w", err)
	}

	return []string{filename}, nil
}

// Генерация PDF-файла со списком кодов маркировки
func generatePDF(kizs []string, filename string) error {
	pdf := gofpdf.New("P", "mm", "A4", "")
	pdf.AddPage()
	pdf.SetFont("Arial", "B", 16)
	pdf.Cell(40, 10, "Список кодов маркировки")
	pdf.Ln(12)
	pdf.SetFont("Arial", "", 12)

	for i, kiz := range kizs {
		pdf.Cell(0, 10, fmt.Sprintf("%d. %s", i+1, kiz))
		pdf.Ln(8)
	}

	return pdf.OutputFileAndClose(filename)
}

// Создание платежа через Robokassa
func createPayment(ctx context.Context, requestData PaymentRequestData) (PaymentResponse, error) {
	// Проверка входных данных
	if requestData.Amount <= 0 {
		return PaymentResponse{
			Status:  "error",
			Message: "Некорректная сумма платежа",
		}, errors.New("сумма платежа должна быть положительной")
	}

	if requestData.OrderID == "" {
		return PaymentResponse{
			Status:  "error",
			Message: "Не указан ID заказа",
		}, errors.New("отсутствует ID заказа")
	}

	// Проверяем на отмену операции
	select {
	case <-ctx.Done():
		return PaymentResponse{
			Status:  "error",
			Message: "Операция отменена",
		}, ctx.Err()
	default:
		// Продолжаем выполнение
	}

	// Формирование подписи для Robokassa
	// Пример: login:amount:orderID:password
	signatureStr := fmt.Sprintf("%s:%g:%s:%s",
		config.RobokassaLogin,
		requestData.Amount,
		requestData.OrderID,
		config.RobokassaPassword)

	// Создание хеша подписи
	hasher := crypto.SHA256.New()
	hasher.Write([]byte(signatureStr))
	signature := fmt.Sprintf("%x", hasher.Sum(nil))

	// Формирование URL для оплаты
	paymentURL := fmt.Sprintf(
		"https://auth.robokassa.ru/Merchant/Index.aspx?MerchantLogin=%s&OutSum=%g&InvId=%s&SignatureValue=%s",
		config.RobokassaLogin,
		requestData.Amount,
		requestData.OrderID,
		signature,
	)

	return PaymentResponse{
		Status:     "success",
		Message:    "URL для оплаты сформирован",
		PaymentURL: paymentURL,
	}, nil
}

// Обработчик HTTP-запросов для получения кодов маркировки
func handleKIZRequest(w http.ResponseWriter, r *http.Request) {
	// Проверка метода запроса
	if r.Method != http.MethodPost {
		http.Error(w, "Метод не поддерживается", http.StatusMethodNotAllowed)
		return
	}

	// Получение telegram_id из параметров запроса
	telegramIDStr := r.URL.Query().Get("telegram_id")
	var telegramID int64
	var err error
	if telegramIDStr != "" {
		telegramID, err = strconv.ParseInt(telegramIDStr, 10, 64)
		if err != nil {
			log.Printf("Некорректный telegram_id: %v", err)
			// Продолжаем выполнение, так как параметр опциональный
		}
	}

	// Ограничение размера тела запроса
	r.Body = http.MaxBytesReader(w, r.Body, 1048576) // 1MB

	// Декодирование тела запроса
	var data KIZRequestData
	if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
		log.Printf("Ошибка декодирования запроса: %v", err)
		http.Error(w, "Некорректный формат запроса", http.StatusBadRequest)
		return
	}

	// Валидация данных запроса
	if len(data.GTINData) == 0 || data.INN == "" {
		http.Error(w, "Отсутствуют обязательные параметры", http.StatusBadRequest)
		return
	}

	// Логирование запроса
	log.Printf("Получен запрос КИЗ: ИНН=%s, GTINs=%d, telegram_id=%d",
		data.INN, len(data.GTINData), telegramID)

	// Запрос кодов маркировки
	response, err := requestKIZs(r.Context(), data)
	if err != nil {
		log.Printf("Ошибка при запросе КИЗ: %v", err)
		// Продолжаем выполнение, так как в response уже содержится информация об ошибке
	}

	// Отправка ответа
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		log.Printf("Ошибка кодирования ответа: %v", err)
		http.Error(w, "Внутренняя ошибка сервера", http.StatusInternalServerError)
		return
	}
}

// Обработчик HTTP-запросов для создания платежа
func handlePaymentRequest(w http.ResponseWriter, r *http.Request) {
	// Проверка метода запроса
	if r.Method != http.MethodPost {
		http.Error(w, "Метод не поддерживается", http.StatusMethodNotAllowed)
		return
	}

	// Получение telegram_id из параметров запроса
	telegramIDStr := r.URL.Query().Get("telegram_id")
	var telegramID int64
	var err error
	if telegramIDStr != "" {
		telegramID, err = strconv.ParseInt(telegramIDStr, 10, 64)
		if err != nil {
			log.Printf("Некорректный telegram_id: %v", err)
			// Возможно, стоит вернуть ошибку здесь, если ID нужен для авторизации
			http.Error(w, "Некорректный идентификатор пользователя", http.StatusBadRequest)
			return
		}
	}

	// Ограничение размера тела запроса
	r.Body = http.MaxBytesReader(w, r.Body, 1048576) // 1MB

	// Декодирование тела запроса
	var data PaymentRequestData
	if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
		log.Printf("Ошибка декодирования запроса: %v", err)
		http.Error(w, "Некорректный формат запроса", http.StatusBadRequest)
		return
	}

	// Логирование запроса
	log.Printf("Получен запрос платежа: amount=%.2f, orderID=%s, telegram_id=%d",
		data.Amount, data.OrderID, telegramID)

	// Создание платежа
	response, err := createPayment(r.Context(), data)
	if err != nil {
		log.Printf("Ошибка при создании платежа: %v", err)
		// Продолжаем выполнение, так как в response уже содержится информация об ошибке
	}

	// Отправка ответа
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		log.Printf("Ошибка кодирования ответа: %v", err)
		http.Error(w, "Внутренняя ошибка сервера", http.StatusInternalServerError)
		return
	}
}

// Middleware для логирования запросов
func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		log.Printf("Получен запрос: %s %s", r.Method, r.URL.Path)
		next.ServeHTTP(w, r)
		log.Printf("Запрос обработан за %s: %s %s", time.Since(start), r.Method, r.URL.Path)
	})
}

func main() {
	// Загрузка конфигурации
	config = loadConfig()

	// Проверка конфигурации
	if err := validateConfig(config); err != nil {
		log.Fatalf("Ошибка в конфигурации: %v", err)
	}

	// Создание мультиплексора для HTTP-запросов
	mux := http.NewServeMux()

	// Регистрация обработчиков - эндпоинты API v1
	mux.HandleFunc("/api/v1/kizs", handleKIZRequest)
	mux.HandleFunc("/api/v1/payments", handlePaymentRequest)

	// Дополнительные эндпоинты для обратной совместимости с Python клиентом
	mux.HandleFunc("/kizs", handleKIZRequest)
	mux.HandleFunc("/pay", handlePaymentRequest)

	// Добавляем эндпоинт для проверки работоспособности сервера
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{
			"status": "ok",
			"time":   time.Now().Format(time.RFC3339),
		})
	})

	// Применение middleware
	handler := loggingMiddleware(mux)

	// Настройка сервера
	server := &http.Server{
		Addr:         ":" + config.Port,
		Handler:      handler,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Создание директории для временных файлов
	if err := os.MkdirAll("./temp", 0755); err != nil {
		log.Printf("Ошибка создания директории для временных файлов: %v", err)
	}

	// Запуск сервера в отдельной горутине
	go func() {
		log.Printf("Сервер запущен на порту %s", config.Port)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Ошибка запуска сервера: %v", err)
		}
	}()

	// Обработка сигналов остановки
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt, syscall.SIGTERM)
	<-quit

	log.Println("Завершение работы сервера...")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		log.Fatalf("Ошибка при остановке сервера: %v", err)
	}

	log.Println("Сервер остановлен")
}
