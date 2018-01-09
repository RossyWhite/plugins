package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"time"

	goahttp "goa.design/goa/http"
	"goa.design/goa/http/middleware/debugging"
	"goa.design/goa/http/middleware/logging"
	multiauth "goa.design/plugins/security/examples/multi_auth"
	securedservicesvr "goa.design/plugins/security/examples/multi_auth/gen/http/secured_service/server"
	securedservice "goa.design/plugins/security/examples/multi_auth/gen/secured_service"
)

func main() {
	// Define command line flags, add any other flag required to configure
	// the service.
	var (
		addr = flag.String("listen", ":8080", "HTTP listen `address`")
		dbg  = flag.Bool("debug", false, "Log request and response bodies")
	)
	flag.Parse()

	// Setup logger and goa log adapter. Replace logger with your own using
	// your log package of choice. The goa.design/middleware/logging/...
	// packages define log adapters for common log packages.
	var (
		logger  *log.Logger
		adapter logging.Adapter
	)
	{
		logger = log.New(os.Stderr, "[multiauth] ", log.Ltime)
		adapter = logging.Adapt(logger)
	}

	// Create the structs that implement the services.
	var (
		securedserviceSvc securedservice.Service
	)
	{
		securedserviceSvc = multiauth.NewSecuredService(logger)
	}

	// Wrap the services in endpoints that can be invoked from other
	// services potentially running in different processes.
	var (
		securedserviceEndpoints *securedservice.Endpoints
	)
	{
		securedserviceEndpoints = securedservice.NewSecureEndpoints(securedserviceSvc)
	}

	// Provide the transport specific request decoder and response encoder.
	// The goa http package has built-in support for JSON, XML and gob.
	// Other encodings can be used by providing the corresponding functions,
	// see goa.design/encoding.
	var (
		dec = goahttp.RequestDecoder
		enc = goahttp.ResponseEncoder
	)

	// Build the service HTTP request multiplexer and configure it to serve
	// HTTP requests to the service endpoints.
	var mux goahttp.Muxer
	{
		mux = goahttp.NewMuxer()
	}

	// Wrap the endpoints with the transport specific layers. The generated
	// server packages contains code generated from the design which maps
	// the service input and output data structures to HTTP requests and
	// responses.
	var (
		securedserviceServer *securedservicesvr.Server
	)
	{
		securedserviceServer = securedservicesvr.New(securedserviceEndpoints, mux, dec, enc)
	}

	// Configure the mux.
	securedservicesvr.Mount(mux, securedserviceServer)

	// Wrap the multiplexer with additional middlewares. Middlewares mounted
	// here apply to all the service endpoints.
	var handler http.Handler = mux
	{
		if *dbg {
			handler = debugging.New(mux, adapter)(handler)
		}
		handler = logging.New(adapter)(handler)
	}

	// Create channel used by both the signal handler and server goroutines
	// to notify the main goroutine when to stop the server.
	errc := make(chan error)

	// Setup interrupt handler. This optional step configures the process so
	// that SIGINT and SIGTERM signals cause the service to stop gracefully.
	go func() {
		c := make(chan os.Signal, 1)
		signal.Notify(c, os.Interrupt)
		errc <- fmt.Errorf("%s", <-c)
	}()

	// Start HTTP server using default configuration, change the code to
	// configure the server as required by your service.
	srv := &http.Server{Addr: *addr, Handler: handler}
	go func() {
		for _, m := range securedserviceServer.Mounts {
			logger.Printf("[INFO] service %q method %q mounted on %s %s", securedserviceServer.Service(), m.Method, m.Verb, m.Pattern)
		}
		logger.Printf("[INFO] listening on %s", *addr)
		errc <- srv.ListenAndServe()
	}()

	// Wait for signal.
	logger.Printf("exiting (%v)", <-errc)

	// Shutdown gracefully with a 30s timeout.
	ctx, _ := context.WithTimeout(context.Background(), 30*time.Second)
	srv.Shutdown(ctx)

	logger.Println("exited")
}