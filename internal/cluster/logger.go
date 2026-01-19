package cluster

import (
	"io"
	"log"

	"github.com/hashicorp/go-hclog"
)

// newNoOpHCLogger creates a no-op hclog.Logger for Raft to avoid excessive logging.
func newNoOpHCLogger() hclog.Logger {
	return hclog.New(&hclog.LoggerOptions{
		Name:   "raft",
		Level:  hclog.Off,
		Output: io.Discard,
	})
}

// newHCLogger creates an hclog.Logger from a standard log.Logger.
func newHCLogger(stdLogger *log.Logger, level hclog.Level) hclog.Logger {
	return hclog.New(&hclog.LoggerOptions{
		Name:   "raft",
		Level:  level,
		Output: stdLogger.Writer(),
	})
}
