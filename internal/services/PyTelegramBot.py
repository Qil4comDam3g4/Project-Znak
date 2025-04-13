import logging
import psycopg2  # type: ignore
from reportlab.lib.pagesizes import letter  # type: ignore
from reportlab.pdfgen import canvas  # type: ignore
from telegram import Update  # type: ignore
from telegram.ext import Updater, CommandHandler, CallbackContext  # type: ignore
import requests  # type: ignore
import os
import json
from typing import List, Dict, Optional, Any, Union

# Настройка логирования
logging.basicConfig(format='%(asctime)s - %(name)s - %(levelname)s - %(message)s', level=logging.INFO)
logger = logging.getLogger(__name__)

# Конфигурация подключения к БД
DB_CONFIG = {
    'dbname': os.getenv('DB_NAME', 'CentralDB'),
    'user': os.getenv('DB_USER', 'admin'),
    'password': os.getenv('DB_PASSWORD', '0x75937$$xb'),
    'host': os.getenv('DB_HOST', 'localhost'),
    'port': os.getenv('DB_PORT', '5432')
}

# URL Go-сервиса
GO_SERVICE_URL = os.getenv('GO_SERVICE_URL', "http://localhost:8080")
API_KIZS_ENDPOINT = "/api/v1/kizs"  # Обновленный эндпоинт в соответствии с Go-сервисом
API_PAYMENTS_ENDPOINT = "/api/v1/payments"  # Обновленный эндпоинт

def create_connection():
    #"""Создает соединение с базой данных PostgreSQL."""
    try:
        return psycopg2.connect(**DB_CONFIG)
    except psycopg2.Error as e:
        logger.error(f"Ошибка подключения к БД: {e}")
        return None

def generate_pdf(gtin_data: List[str], filename: str) -> None:
   #"""Генерирует PDF-файл со списком GTIN."""
    try:
        c = canvas.Canvas(filename, pagesize=letter)
        width, height = letter
        for index, gtin in enumerate(gtin_data):
            c.drawString(100, height - (index + 1) * 20, f"GTIN: {gtin}")
        c.save()
        logger.info(f"PDF-файл успешно создан: {filename}")
    except Exception as e:
        logger.error(f"Ошибка при генерации PDF: {e}")
        raise

def add_user(username: str, email: str, price: float) -> bool:
    #"""Добавляет пользователя в базу данных."""
    conn = create_connection()
    if conn is None:
        return False
    
    try:
        with conn:
            with conn.cursor() as cursor:
                cursor.execute(
                    'INSERT INTO users (username, email, price) VALUES (%s, %s, %s)',
                    (username, email, price)
                )
            conn.commit()
            logger.info(f"Пользователь {username} успешно добавлен")
            return True
    except psycopg2.Error as e:
        logger.error(f"Ошибка при добавлении пользователя: {e}")
        return False
    finally:
        if conn:
            conn.close()

def create_payment(amount: float, order_id: str, telegram_id: int) -> Optional[str]:
    #"""Создает платеж через Go-сервис и возвращает URL для оплаты."""
    data = {"amount": amount, "order_id": order_id}
    try:
        response = requests.post(
            f"{GO_SERVICE_URL}{API_PAYMENTS_ENDPOINT}",
            json=data,
            params={"telegram_id": telegram_id},
            timeout=10  # Добавлен таймаут
        )
        response.raise_for_status()
        result = response.json()
        
        if result.get("status") == "success":
            return result.get("payment_url")
        else:
            logger.error(f"Ошибка Robokassa: {result.get('message')}")
            return None
    except requests.exceptions.RequestException as e:
        logger.error(f"Ошибка запроса к сервису платежей: {e}")
        return None
    except json.JSONDecodeError as e:
        logger.error(f"Ошибка декодирования JSON ответа: {e}")
        return None

def request_kiz_command(update: Update, context: CallbackContext) -> None:
    #"""Обрабатывает команду /requestkiz для запроса КИЗ."""
    if len(context.args) < 2:
        update.message.reply_text("Используйте: /requestkiz <gtin1> <кол-во1> [<gtin2> <кол-во2>...] [inn <ИНН>]")
        return

    # Парсинг аргументов
    gtin_data = []
    inn = ""
    telegram_id = update.effective_user.id

    i = 0
    while i < len(context.args):
        if context.args[i].lower() == "inn" and i + 1 < len(context.args):
            inn = context.args[i+1]
            i += 2
            continue
        try:
            gtin, count = context.args[i], int(context.args[i+1])
            gtin_data.append({"gtin": gtin, "count": count})
            i += 2
        except (ValueError, IndexError):
            update.message.reply_text("Некорректный формат аргументов.")
            return

    # Проверка наличия данных
    if not gtin_data:
        update.message.reply_text("Необходимо указать хотя бы один GTIN с количеством.")
        return
    
    if not inn:
        update.message.reply_text("Необходимо указать ИНН (inn <номер>).")
        return

    # Отправка запроса в Go-сервис
    try:
        response = requests.post(
            f"{GO_SERVICE_URL}{API_KIZS_ENDPOINT}",
            json={"gtin_data": gtin_data, "inn": inn},
            params={"telegram_id": telegram_id},
            timeout=30  # Увеличенный таймаут для запроса КИЗ
        )
        response.raise_for_status()
        result = response.json()
        
        if result.get("status") == "success":
            message = result.get("message", "✅ КИЗы получены")
            
            # Добавляем информацию о КИЗах в сообщение
            if "kizs" in result and result["kizs"]:
                kizs_list = result["kizs"]
                if len(kizs_list) > 10:
                    # Если список слишком длинный, показываем только первые 10 элементов
                    message += f"\nПолучено {len(kizs_list)} КИЗов. Первые 10:\n" + "\n".join(kizs_list[:10]) + "\n..."
                else:
                    message += "\nСписок КИЗ:\n" + "\n".join(kizs_list)
            
            # Добавляем информацию о файлах
            if "file_paths" in result and result["file_paths"]:
                message += "\nФайлы: " + ", ".join(result["file_paths"])
            
            update.message.reply_text(message)
        else:
            update.message.reply_text(f"❌ Ошибка: {result.get('message', 'Неизвестная ошибка')}")
    except requests.exceptions.RequestException as e:
        logger.error(f"Ошибка запроса КИЗ: {e}")
        update.message.reply_text(f"🚫 Ошибка связи с сервером: {str(e)}")
    except json.JSONDecodeError as e:
        logger.error(f"Ошибка декодирования JSON ответа: {e}")
        update.message.reply_text("⚠️ Ошибка формата ответа сервера")
    except Exception as e:
        logger.error(f"Непредвиденная ошибка: {e}")
        update.message.reply_text(f"⚠️ Произошла ошибка: {str(e)}")

def start(update: Update, context: CallbackContext) -> None:
    #"""Обрабатывает команду /start."""
    user = update.effective_user
    update.message.reply_text(
        f"👋 Здравствуйте, {user.first_name}!\n\n"
        "Я бот для работы с Честным ЗНАКом. Доступные команды:\n"
        "/requestkiz - запросить КИЗы\n"
        "/pay - создать платеж"
    )

def pay_command(update: Update, context: CallbackContext) -> None:
    #"""Обрабатывает команду /pay для создания платежа."""
    if not context.args or len(context.args) < 2:
        update.message.reply_text("Используйте: /pay <сумма> <ID заказа>")
        return

    try:
        amount = float(context.args[0])
        if amount <= 0:
            update.message.reply_text("⚠️ Сумма должна быть положительным числом")
            return
            
        order_id = context.args[1]
        telegram_id = update.effective_user.id
        
        update.message.reply_text("⏳ Создание платежа...")
        
        payment_url = create_payment(amount, order_id, telegram_id)
        if payment_url:
            update.message.reply_text(f"🔗 Ссылка для оплаты: {payment_url}")
        else:
            update.message.reply_text("⚠️ Не удалось создать платеж. Пожалуйста, попробуйте позже.")
    except ValueError:
        update.message.reply_text("⚠️ Сумма должна быть числом. Пример: /pay 100.50 order123")
    except IndexError:
        update.message.reply_text("Используйте: /pay <сумма> <ID заказа>")
    except Exception as e:
        logger.error(f"Ошибка в команде оплаты: {e}")
        update.message.reply_text(f"⚠️ Произошла ошибка: {str(e)}")

def main() -> None:
    #"""Запускает бота."""
    token = os.getenv('7653712411:AAEcbimjAVzEG0uQSOiPUr5OCs8EnhyEVL0')
    if not token:
        logger.error("Не задан токен бота в переменных окружения 7653712411:AAEcbimjAVzEG0uQSOiPUr5OCs8EnhyEVL0")
        return
        
    try:
        updater = Updater(token)
        dp = updater.dispatcher
        
        # Регистрация обработчиков команд
        dp.add_handler(CommandHandler("start", start))
        dp.add_handler(CommandHandler("requestkiz", request_kiz_command))
        dp.add_handler(CommandHandler("pay", pay_command))
        
        # Запуск бота
        updater.start_polling()
        logger.info("Бот успешно запущен")
        
        # Ожидание остановки бота
        updater.idle()
    except Exception as e:
        logger.error(f"Ошибка запуска бота: {e}")

if __name__ == '__main__':
    main()