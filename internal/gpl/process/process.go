// Copyright (c) Edgeless Systems GmbH
// SPDX-License-Identifier: GPL-3.0-only

// Package process defines utility functions used for running the main process of a Go binary.
package process

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sync"
)

// SignalContext returns a context that is canceled on the handed signal.
// The signal isn't watched after its first occurrence. Call the cancel
// function to ensure the internal goroutine is stopped and the signal isn't
// watched any longer.
func SignalContext(ctx context.Context, sig os.Signal) (context.Context, context.CancelFunc) {
	sigCtx, stop := signal.NotifyContext(ctx, sig)
	done := make(chan struct{}, 1)
	stopDone := make(chan struct{}, 1)

	go func() {
		defer func() { stopDone <- struct{}{} }()
		defer stop()
		select {
		case <-sigCtx.Done():
			fmt.Println("\rSignal caught. Press ctrl+c again to terminate the program immediately.")
		case <-done:
		}
	}()

	cancelFunc := func() {
		done <- struct{}{}
		<-stopDone
	}

	return sigCtx, cancelFunc
}

// HTTPServeContext runs an [*http.Server] and takes care of shutting it down when the context is canceled.
// Should the server not define a [*tls.Config], the server will start without TLS.
// This function blocks until the server is shut down and returns an error if the server failed to shut down
// or run properly.
func HTTPServeContext(ctx context.Context, server *http.Server, listener net.Listener, log *slog.Logger) error {
	var wg sync.WaitGroup
	serveErr := make(chan error, 1)

	wg.Add(1)
	go func() {
		defer wg.Done()
		if server.TLSConfig == nil {
			log.Info("Starting HTTP server without TLS", "endpoint", server.Addr)
			serveErr <- server.Serve(listener)
		} else {
			log.Info("Starting HTTPS server", "endpoint", server.Addr)
			serveErr <- server.ServeTLS(listener, "", "")
		}
	}()

	var err error
	wg.Add(1)
	go func() {
		defer wg.Done()
		select {
		case <-ctx.Done():
			log.Info("Shutting down server")
			err = server.Shutdown(ctx)
		case err = <-serveErr:
		}
	}()

	wg.Wait()
	return err
}
