package logger

import (
	"log/slog"
	"os"
)

var Log *slog.Logger

func Init() {
	textHandler := slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		AddSource: true,
	})

	Log = slog.New(textHandler)
	slog.SetDefault(Log)
}
