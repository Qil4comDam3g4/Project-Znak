package logger

import (
	"io"
	"os"
	"time"

	"github.com/sirupsen/logrus"
)

var log *logrus.Logger

func Init(level string, file string) error {
	log = logrus.New()
	log.SetFormatter(&logrus.JSONFormatter{
		TimestampFormat: time.RFC3339,
	})

	// Установка уровня логирования
	lvl, err := logrus.ParseLevel(level)
	if err != nil {
		return err
	}
	log.SetLevel(lvl)

	// Настройка вывода
	if file != "" {
		f, err := os.OpenFile(file, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
		if err != nil {
			return err
		}
		log.SetOutput(io.MultiWriter(os.Stdout, f))
	} else {
		log.SetOutput(os.Stdout)
	}

	return nil
}

// GetLogger возвращает инстанс логгера
func GetLogger() *logrus.Logger {
	if log == nil {
		log = logrus.New()
		log.SetFormatter(&logrus.JSONFormatter{
			TimestampFormat: time.RFC3339,
		})
		log.SetOutput(os.Stdout)
		log.SetLevel(logrus.InfoLevel)
	}
	return log
}

func Info(args ...interface{}) {
	log.Info(args...)
}

func Error(args ...interface{}) {
	log.Error(args...)
}

func Debug(args ...interface{}) {
	log.Debug(args...)
}

func Fatal(args ...interface{}) {
	log.Fatal(args...)
}

func WithFields(fields logrus.Fields) *logrus.Entry {
	return log.WithFields(fields)
}
