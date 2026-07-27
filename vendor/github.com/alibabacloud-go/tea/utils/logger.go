package utils

import (
	"io"
	"log"
	"strings"
	"time"
)

type Logger struct {
	*log.Logger
	formatTemplate string
	isOpen         bool
	lastLogMsg     string
}

var defaultLoggerTemplate = `{time} {channel}: "{method} {uri} HTTP/{version}" {code} {cost} {hostname}`
var loggerParam = []string{"{time}", "{start_time}", "{ts}", "{channel}", "{pid}", "{host}", "{method}", "{uri}", "{version}", "{target}", "{hostname}", "{code}", "{error}", "{req_headers}", "{res_body}", "{res_headers}", "{cost}"}
var logChannel string

func InitLogMsg(fieldMap map[string]string) {
	for _, value := range loggerParam {
		fieldMap[value] = ""
	}
}

func (logger *Logger) SetFormatTemplate(template string) {
	logger.formatTemplate = template

}

func (logger *Logger) GetFormatTemplate() string {
	return logger.formatTemplate

}

func NewLogger(level string, channel string, out io.Writer, template string) *Logger {
	if level == "" {
		level = "info"
	}

	logChannel = "AlibabaCloud"
	if channel != "" {
		logChannel = channel
	}
	log := log.New(out, "["+strings.ToUpper(level)+"]", log.Lshortfile)
	if template == "" {
		template = defaultLoggerTemplate
	}

	return &Logger{
		Logger:         log,
		formatTemplate: template,
		isOpen:         true,
	}
}

func (logger *Logger) OpenLogger() {
	logger.isOpen = true
}

func (logger *Logger) CloseLogger() {
	logger.isOpen = false
}

func (logger *Logger) SetIsopen(isopen bool) {
	logger.isOpen = isopen
}

func (logger *Logger) GetIsopen() bool {
	return logger.isOpen
}

func (logger *Logger) SetLastLogMsg(lastLogMsg string) {
	logger.lastLogMsg = lastLogMsg
}

func (logger *Logger) GetLastLogMsg() string {
	return logger.lastLogMsg
}

func SetLogChannel(channel string) {
	logChannel = channel
}

func (logger *Logger) PrintLog(fieldMap map[string]string, err error) {
	if err != nil {
		fieldMap["{error}"] = err.Error()
	}
	fieldMap["{time}"] = time.Now().Format("2006-01-02 15:04:05")
	fieldMap["{ts}"] = getTimeInFormatISO8601()
	fieldMap["{channel}"] = logChannel
	if logger != nil {
		logMsg := logger.formatTemplate
		for key, value := range fieldMap {
			logMsg = strings.Replace(logMsg, key, value, -1)
		}
		logger.lastLogMsg = logMsg
		if logger.isOpen == true {
			logger.Output(2, logMsg)
		}
	}
}

func getTimeInFormatISO8601() (timeStr string) {
	gmt := time.FixedZone("GMT", 0)

	return time.Now().In(gmt).Format("2006-01-02T15:04:05Z")
}
