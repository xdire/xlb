package xlb

import (
	"github.com/rs/zerolog"
	"os"
)

type zeroLogger struct {
	zl zerolog.Logger
}

func newZeroLogForName(name, id string) zeroLogger {
	return zeroLogger{zerolog.New(os.Stdout).
		Level(zerolog.ErrorLevel).With().Timestamp().
		Caller().Str("xlb", id).Logger(),
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
