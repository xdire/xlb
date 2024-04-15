package xlb

import (
	"github.com/rs/zerolog"
	"os"
)

type zeroLogger struct {
	zl zerolog.Logger
}

func newZeroLogForName(name, id, level string) zeroLogger {
	zLevel := zerolog.ErrorLevel
	if len(level) > 0 {
		newLevel, err := zerolog.ParseLevel(level)
		if err == nil {
			zLevel = newLevel
		}
	}
	return zeroLogger{zerolog.New(os.Stdout).
		Level(zLevel).With().Timestamp().
		Caller().Str(name, id).Logger(),
	}
}

func (z zeroLogger) Info(s string) {
	z.zl.Info().Msg(s)
}

func (z zeroLogger) Error(s string) {
	z.zl.Error().Msg(s)
}

func (z zeroLogger) Debug(s string) {
	z.zl.Debug().Msg(s)
}
