package gcp

import (
	"log/slog"
	"strconv"
	"time"
)

// HTTPRequest returns an slog.Attr that the GCP sink renders as the
// canonical Cloud Logging httpRequest structured field. Add it to any
// log line that summarizes an HTTP request and the Cloud Logging UI
// will surface Method, URL, Status, and Latency columns plus filter
// chips:
//
//	logger.LogAttrs(ctx, slog.LevelInfo, "request handled",
//	    gcp.HTTPRequest("GET", "/api/echo", 200, 45*time.Millisecond),
//	    slog.String("route", "/api/echo"),
//	)
//
// The emitted JSON shape:
//
//	{
//	    "severity": "INFO",
//	    "message":  "request handled",
//	    "httpRequest": {
//	        "requestMethod": "GET",
//	        "requestUrl":    "/api/echo",
//	        "status":        200,
//	        "latency":       "0.045s"
//	    }
//	}
//
// For requests where you have additional fields like userAgent or
// remoteIp, use HTTPRequestFull.
func HTTPRequest(method, url string, status int, latency time.Duration) slog.Attr {
	return slog.Group("httpRequest",
		slog.String("requestMethod", method),
		slog.String("requestUrl", url),
		slog.Int("status", status),
		slog.String("latency", latencySeconds(latency)),
	)
}

// HTTPRequestFull returns the same httpRequest attr as HTTPRequest plus
// any of the optional fields Cloud Logging recognises. Pass empty
// strings or zero for fields you do not want to emit.
//
// https://cloud.google.com/logging/docs/reference/v2/rest/v2/LogEntry#HttpRequest
func HTTPRequestFull(
	method, url string,
	status int,
	latency time.Duration,
	userAgent, remoteIP, referer, protocol string,
	requestSize, responseSize int64,
) slog.Attr {
	attrs := []any{
		slog.String("requestMethod", method),
		slog.String("requestUrl", url),
		slog.Int("status", status),
		slog.String("latency", latencySeconds(latency)),
	}
	if userAgent != "" {
		attrs = append(attrs, slog.String("userAgent", userAgent))
	}
	if remoteIP != "" {
		attrs = append(attrs, slog.String("remoteIp", remoteIP))
	}
	if referer != "" {
		attrs = append(attrs, slog.String("referer", referer))
	}
	if protocol != "" {
		attrs = append(attrs, slog.String("protocol", protocol))
	}
	if requestSize > 0 {
		attrs = append(attrs, slog.String("requestSize", strconv.FormatInt(requestSize, 10)))
	}
	if responseSize > 0 {
		attrs = append(attrs, slog.String("responseSize", strconv.FormatInt(responseSize, 10)))
	}
	return slog.Group("httpRequest", attrs...)
}

// latencySeconds formats a duration as the Cloud Logging "duration in
// seconds" string format, e.g. "0.045s". Three decimal places matches
// the granularity Cloud Logging displays in the UI.
func latencySeconds(d time.Duration) string {
	return strconv.FormatFloat(d.Seconds(), 'f', 3, 64) + "s"
}
