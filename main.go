package main

import (
	"log/slog"
	"os"

	botdapp "github.com/XiaoLozee/go-bot/internal/app/botd"
)

func main() {
	if err := botdapp.Run(os.Args[1:]); err != nil {
		slog.Error("go-bot 启动失败", "error", err)
		os.Exit(1)
	}
}
