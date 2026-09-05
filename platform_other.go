//go:build !windows

package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
)

func platformFlags(_ *flag.FlagSet, _ *launchOptions) {}

func runPlatform(options launchOptions) error { return runConsole(options) }

func writeCommandOutput(text string, isError bool) {
	out := os.Stdout
	if isError {
		out = os.Stderr
	}
	fmt.Fprint(out, text)
}

func reportPlatformError(err error, _ bool) { log.Print(err) }

func notifyConsoleSignals(shutdown chan os.Signal, _ <-chan struct{}) (func(), error) {
	signal.Notify(shutdown, os.Interrupt, syscall.SIGTERM)
	return func() { signal.Stop(shutdown) }, nil
}
