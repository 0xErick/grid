package utils

import (
	"github.com/davecgh/go-spew/spew"
	log "github.com/go-ozzo/ozzo-log"
)

type Log interface {
	logger(level string, a ...interface{})
}

type Logger struct {
}

func (c Logger) logger(level string, a ...interface{}) {
	console := log.NewLogger()
	console.Targets = append(console.Targets, log.NewConsoleTarget())

	err := console.Open()
	if err != nil {
		return
	}

	switch level {
	case "Info":
		console.Info("%v", a...)
	case "Error":
		console.Error("%v", a...)
	case "Debug":
		console.Debug("%v", a...)
	default:
		console.Debug("%v", a...)
	}

	defer console.Close()

}

func Info(any interface{}, a ...interface{}) {
	console := Logger{}
	console.logger("Info", a)

	if any != nil {
		spew.Dump(any)
	}

}

func Error(err error, a ...interface{}) {
	console := Logger{}
	console.logger("Error", a)
	spew.Dump(err)
}
