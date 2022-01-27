package repocli

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	log "github.com/Donders-Institute/tg-toolset-golang/pkg/logger"
)

// trapCancel is a blocking function listening to interruption key strokes, such as Ctrl-C.
//
// The function is blocked until it receives a interruption key stroke, or until the
// context `ctx` is cancelled externally.
func trapCancel(ctx context.Context) {

	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGHUP, syscall.SIGTERM, syscall.SIGQUIT)
	defer signal.Stop(c)

	select {
	case s := <-c:
		log.Warnf("Got %v signal, cancelling ...\n", s)

	case <-ctx.Done():
	}
}
