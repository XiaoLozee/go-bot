package botd

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	adminapi "github.com/XiaoLozee/go-bot/internal/admin/api"
	"github.com/XiaoLozee/go-bot/internal/config"
	"github.com/XiaoLozee/go-bot/internal/plugin/externalexec"
	"github.com/XiaoLozee/go-bot/internal/runtime"
)

func Run(args []string) error {
	if len(args) > 0 {
		switch args[0] {
		case "external-plugin":
			return externalexec.RunEmbedded(args[1:])
		case "scaffold":
			return runScaffold(args[1:])
		}
	}

	fs := flag.NewFlagSet("botd", flag.ContinueOnError)
	configPath := fs.String("config", "", "配置文件路径，默认依次尝试 configs/config.yml 和 configs/config.example.yml")
	if err := fs.Parse(args); err != nil {
		return err
	}

	cfg, configFile, err := config.LoadWithFallback(*configPath)
	if err != nil {
		return err
	}

	logger := NewLogger(cfg.App.LogLevel, cfg.Storage.Logs.Dir)
	logger.Info("启动", "stage", "config", "status", "loaded", "file", configFile)

	rt, err := runtime.New(cfg, configFile, logger)
	if err != nil {
		return fmt.Errorf("创建运行时失败: %w", err)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	if err := rt.Start(ctx); err != nil {
		return fmt.Errorf("启动运行时失败: %w", err)
	}
	defer func() {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer stopCancel()
		if err := rt.Stop(stopCtx); err != nil {
			logger.Error("停止运行时失败", "error", err)
		}
	}()

	var adminServer *http.Server
	if cfg.Server.Admin.Enabled || cfg.Server.WebUI.Enabled {
		handler := adminapi.NewRouter(logger, rt)
		adminServer = &http.Server{
			Addr:              cfg.Server.Admin.Listen,
			Handler:           handler,
			ReadHeaderTimeout: 5 * time.Second,
		}

		go func() {
			logger.Info("启动",
				"stage", "admin_http",
				"status", "listening",
				"listen", cfg.Server.Admin.Listen,
				"admin_enabled", cfg.Server.Admin.Enabled,
				"webui_enabled", cfg.Server.WebUI.Enabled,
				"webui_base_path", cfg.Server.WebUI.BasePath,
			)
			if err := adminServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				logger.Error("管理 HTTP 服务运行失败", "error", err)
				cancel()
			}
		}()
	}

	<-ctx.Done()
	logger.Info("收到退出信号，准备关闭")

	if adminServer != nil {
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer shutdownCancel()
		if err := adminServer.Shutdown(shutdownCtx); err != nil {
			logger.Error("关闭管理 HTTP 服务失败", "error", err)
		}
	}
	return nil
}

func NewLogger(level string, logDir ...string) *slog.Logger {
	var slogLevel slog.Level
	switch level {
	case "debug":
		slogLevel = slog.LevelDebug
	case "warn":
		slogLevel = slog.LevelWarn
	case "error":
		slogLevel = slog.LevelError
	default:
		slogLevel = slog.LevelInfo
	}

	writer := io.Writer(os.Stdout)
	if len(logDir) > 0 {
		file, err := openAppLogFile(logDir[0])
		if err != nil {
			fmt.Fprintf(os.Stderr, "open app log file failed: %v\n", err)
		} else {
			writer = io.MultiWriter(os.Stdout, file)
		}
	}

	handler := slog.NewTextHandler(writer, &slog.HandlerOptions{Level: slogLevel})
	return slog.New(handler)
}

func openAppLogFile(logDir string) (*os.File, error) {
	dir := filepath.Clean(strings.TrimSpace(logDir))
	if dir == "." || dir == "" {
		dir = filepath.Clean("./data/logs")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	return os.OpenFile(filepath.Join(dir, "app.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
}
