package client

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptrace"
	"time"
)

// HttpVersion represents the HTTP protocol version to use in the client.
type HttpVersion uint8

const (
	// HTTP1 represents HTTP/1.x protocol.
	HTTP1 HttpVersion = iota + 1
	// HTTP2 represents HTTP/2 protocol.
	HTTP2 HttpVersion = iota + 1

	UuidLogField = "req_uuid"
)

// ResponseHandler defines a function type to handle HTTP responses.
type ResponseHandler func(resp *http.Response) error

// ErrorHandler defines a function type to handle errors.
type ErrorHandler func(reqUuid string, err error) error

// DoTimeRepeatClient is an HTTP client that can repeat requests and log timing information.
type DoTimeRepeatClient struct {
	c      *http.Client  // underlying HTTP client
	req    *http.Request // base HTTP request to clone and send
	logger *slog.Logger  // logger for request tracing and timing
}

// DoTimeRepeat sends the HTTP request n times, handling responses and errors with the provided handlers.
// It logs timing and tracing information for each request.
//
//	ctx: context for request cancellation and deadlines
//	n: number of times to repeat the request
//	rh: handler for processing HTTP responses
//	eh: handler for processing errors
//
// Use the [ErrorHandler] parameter to define what errors should cause it to abort.
func (c *DoTimeRepeatClient) DoTimeRepeat(ctx context.Context, n int, rh ResponseHandler, eh ErrorHandler) error {
	for range n {
		reqUuid := rand.Text()
		req := c.req.Clone(ctx)
		req = AddTraceToRequest(reqUuid, req, c.logger)

		t1 := time.Now()
		resp, err := c.c.Do(req)
		if err := eh(reqUuid, err); err != nil {
			return err
		}
		if err := eh(reqUuid, rh(resp)); err != nil {
			return err
		}
		c.logger.Info("req completion", "status_code", resp.StatusCode, "max_time_nano", time.Since(t1).Nanoseconds(), UuidLogField, reqUuid)
	}
	return nil
}

// LogErr logs the error with the logger set at the client adding the request UUID information.
func (c *DoTimeRepeatClient) LogErr(reqUuid string, err error) error {
	if err != nil {
		c.logger.Error("req failed", "error", err, UuidLogField, reqUuid)
	}
	return nil
}

// NewDoTimeRepeatClient creates a new DoTimeRepeatClient with the given request, logger, and HTTP version.
//
//	req: base HTTP request to use for each repeated request
//	logger: logger for tracing and timing
//	httpV: HTTP protocol version to use
//
// Returns a pointer to DoTimeRepeatClient or an error if the HTTP client cannot be created.
func NewDoTimeRepeatClient(req *http.Request, logger *slog.Logger, httpV HttpVersion) (*DoTimeRepeatClient, error) {
	c, err := NewHTTPClient(httpV)
	if err != nil {
		return nil, fmt.Errorf("failed to create underlying HTTP client: %w", err)
	}
	return &DoTimeRepeatClient{c, req, logger}, nil
}

// NewHTTPClient creates a new *http.Client configured for the specified HTTP version.
//
//	httpV: HTTP protocol version to use
//
// Returns a pointer to http.Client or an error if the version is invalid.
func NewHTTPClient(httpV HttpVersion) (*http.Client, error) {
	protos := &http.Protocols{}
	switch httpV {
	case HTTP1:
		protos.SetHTTP1(true)
	case HTTP2:
		protos.SetHTTP2(true)
	default:
		return nil, fmt.Errorf("invalid HTTP version: %d", httpV)
	}

	transp := &http.Transport{
		Protocols: protos,
	}

	client := http.Client{
		Transport: transp,
	}

	return &client, nil
}

// AddTraceToRequest adds HTTP tracing to the given request for logging connection and DNS events.
//
//	reqUuid: unique identifier for the request
//	req: HTTP request to add tracing to
//	logger: logger for trace events
//
// Returns a new *http.Request with tracing enabled.
func AddTraceToRequest(reqUuid string, req *http.Request, logger *slog.Logger) *http.Request {
	req = req.WithContext(httptrace.WithClientTrace(req.Context(), &httptrace.ClientTrace{
		GetConn: func(hostPort string) {
			logger.Info("get conn", "port", hostPort, UuidLogField, reqUuid)
		},
		GotConn: func(gci httptrace.GotConnInfo) {
			logger.Info("got conn", "reused", gci.Reused, UuidLogField, reqUuid)
		},
		PutIdleConn: func(err error) {
			const label = "put idle conn"
			if err != nil {
				logger.Error(label, "error", err, UuidLogField, reqUuid)
				return
			}
			logger.Info(label, "status", true, UuidLogField, reqUuid)
		},
		GotFirstResponseByte: func() {
			logger.Info("ttfb", UuidLogField, reqUuid)
		},
		DNSStart: func(di httptrace.DNSStartInfo) {
			logger.Info("dns start", "host", di.Host, UuidLogField, reqUuid)
		},
		DNSDone: func(di httptrace.DNSDoneInfo) {
			logger.Info("dns done", UuidLogField, reqUuid)
		},
		ConnectStart: func(network, addr string) {
			logger.Info("connect start", "network", network, "addr", addr, UuidLogField, reqUuid)
		},
		ConnectDone: func(network, addr string, err error) {
			logger.Info("connect done", "network", network, "addr", addr, UuidLogField, reqUuid)
		},
		TLSHandshakeStart: func() {
			logger.Info("tls handshake start", UuidLogField, reqUuid)
		},
		TLSHandshakeDone: func(cs tls.ConnectionState, err error) {
			const label = "tls handshake done"
			if err != nil {
				logger.Error(label, "error", err, "server", cs.ServerName, UuidLogField, reqUuid)
			}
			logger.Info(label, "server", cs.ServerName, UuidLogField, reqUuid)
		},
	}))

	return req
}

// CloseBody closes the response body.
func CloseBody(resp *http.Response) error {
	if resp != nil {
		return resp.Body.Close()
	}
	return nil
}

// DrainCloseBody drains and closes the response body.
func DrainCloseBody(resp *http.Response) error {
	if resp != nil {
		_, err := io.Copy(io.Discard, resp.Body)
		return errors.Join(resp.Body.Close(), err)
	}
	return nil
}
