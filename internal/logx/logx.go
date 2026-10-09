package logx

import (
	"fmt"
	"strings"

	"github.com/sirupsen/logrus"

	"server_monitor_service/internal/config"
)

// Init 根据配置初始化 logrus 全局日志（FR-12）。
func Init(cfg config.LogConfig) error {
	level, err := parseLevel(cfg.Level)
	if err != nil {
		return err
	}
	logrus.SetLevel(level)

	switch strings.ToLower(cfg.Format) {
	case "json":
		logrus.SetFormatter(&logrus.JSONFormatter{})
	default:
		logrus.SetFormatter(&logrus.TextFormatter{FullTimestamp: true})
	}
	return nil
}

func parseLevel(s string) (logrus.Level, error) {
	if s == "" {
		return logrus.InfoLevel, nil
	}
	lvl, err := logrus.ParseLevel(strings.ToLower(s))
	if err != nil {
		return logrus.InfoLevel, fmt.Errorf("非法日志级别 %q: %w", s, err)
	}
	return lvl, nil
}

// Logger 返回带字段的日志入口。
func Logger(fields map[string]interface{}) *logrus.Entry {
	return logrus.WithFields(fields)
}
