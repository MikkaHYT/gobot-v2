package bot

import (
	"gobot/internal/helpers"
	"gobot/internal/logger"
)

func init() {
	helpers.LogWarn = logger.Warnf
	helpers.LogError = logger.Errorf
}

var (
	Debugf             = logger.Debugf
	Infof              = logger.Infof
	Warnf              = logger.Warnf
	Errorf             = logger.Errorf
	Fatalf             = logger.Fatalf
	InitLogger         = logger.InitLogger
	SendConsoleWebhook = logger.SendConsoleWebhook
)
