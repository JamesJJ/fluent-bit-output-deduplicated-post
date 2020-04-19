package main

import (
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"os"
	"strings"
)

type SimpleLogger struct {
	Debug *log.Logger
	Info  *log.Logger
	Error *log.Logger
}

func Logger(level string, prefix string) *SimpleLogger {

	var logSetup *SimpleLogger

	if strings.EqualFold(level, "DEBUG") {
		logSetup = setup(prefix, os.Stdout, os.Stdout, os.Stderr)
	} else if strings.EqualFold(level, "ERROR") {
		logSetup = setup(prefix, ioutil.Discard, ioutil.Discard, os.Stderr)
	} else if strings.EqualFold(level, "NONE") {
		logSetup = setup(prefix, ioutil.Discard, ioutil.Discard, ioutil.Discard)
	} else {
		logSetup = setup(prefix, ioutil.Discard, os.Stdout, os.Stderr)
	}
	return logSetup
}

func setup(prefix string, debugHandle io.Writer, infoHandle io.Writer, errorHandle io.Writer) *SimpleLogger {

	logger := SimpleLogger{}

	logger.Debug = log.New(
		debugHandle,
		fmt.Sprintf("[debug] %s", prefix),
		log.LstdFlags|log.Lmsgprefix,
	)

	logger.Info = log.New(
		infoHandle,
		fmt.Sprintf("[ info] %s", prefix),
		log.LstdFlags|log.Lmsgprefix,
	)

	logger.Error = log.New(
		errorHandle,
		fmt.Sprintf("[error] %s", prefix),
		log.LstdFlags|log.Lmsgprefix,
	)

	return &logger
}
