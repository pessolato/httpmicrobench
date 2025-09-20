package main

import (
	"context"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"syscall"

	"github.com/pessolato/httpmicrobench/pkg/client"
	"github.com/pessolato/httpmicrobench/pkg/osutil"
)

func main() {
	endpointUrl := ""
	numOfReqs := 1000
	drainClose := false
	httpVersion := 1
	osutil.ExitOnErr(
		osutil.Load(
			osutil.NewEnvVar("TARGET_ENDPOINT_URI", &endpointUrl, true),
			osutil.NewEnvVar("NUMBER_OF_REQUESTS", &numOfReqs, false),
			osutil.NewEnvVar("MUST_DRAIN_AND_CLOSE", &drainClose, false),
			osutil.NewEnvVar("CLIENT_HTTP_VERSION", &httpVersion, false),
		))
	_, err := url.Parse(endpointUrl)
	osutil.ExitOnErr(err)

	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpointUrl, nil)
	osutil.ExitOnErr(err)

	c, err := client.NewDoTimeRepeatClient(req, logger, client.HttpVersion(httpVersion))
	osutil.ExitOnErr(err)

	respHandler := client.CloseBody
	if drainClose {
		respHandler = client.DrainCloseBody
	}

	err = c.DoTimeRepeat(ctx, numOfReqs, respHandler, c.LogErr)
	osutil.ExitOnErr(err)
}
