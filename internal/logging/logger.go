package logging

import (
	"fmt"
	"log"
	"os"
	"time"
)

type Logger struct {
	verbose bool
	logger  *log.Logger
}

func NewLogger(verbose bool) *Logger {
	return &Logger{
		verbose: verbose,
		logger:  log.New(os.Stdout, "", 0),
	}
}

func (l *Logger) timestamp() string {
	return time.Now().Format("2006-01-02 15:04:05")
}

func (l *Logger) Info(format string, args ...interface{}) {
	message := fmt.Sprintf(format, args...)
	l.logger.Printf("[%s] INFO: %s", l.timestamp(), message)
}

func (l *Logger) Debug(format string, args ...interface{}) {
	if l.verbose {
		message := fmt.Sprintf(format, args...)
		l.logger.Printf("[%s] DEBUG: %s", l.timestamp(), message)
	}
}

func (l *Logger) Error(format string, args ...interface{}) {
	message := fmt.Sprintf(format, args...)
	l.logger.Printf("[%s] ERROR: %s", l.timestamp(), message)
}

func (l *Logger) Fatal(format string, args ...interface{}) {
	message := fmt.Sprintf(format, args...)
	l.logger.Printf("[%s] FATAL: %s", l.timestamp(), message)
	os.Exit(1)
}

func (l *Logger) Warn(format string, args ...interface{}) {
	message := fmt.Sprintf(format, args...)
	l.logger.Printf("[%s] WARN: %s", l.timestamp(), message)
}