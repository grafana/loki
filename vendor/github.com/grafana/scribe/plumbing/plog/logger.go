package plog

import (
	"github.com/sirupsen/logrus"
)

func New(level logrus.Level) *logrus.Logger {
	logger := logrus.New()

	logger.SetLevel(level)
	logger.SetFormatter(&logrus.TextFormatter{})

	return logger
}
