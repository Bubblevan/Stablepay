package http

import (
	"bytes"
	"context"
	"fmt"
	"io"
	nethttp "net/http"
	"net/url"
	"strings"
	"time"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/app/server"
)

// The browser session and CSRF boundary lives in Commerce Runtime. The gateway
// forwards only its narrow MVP surface; payment-service's private order API is
// never exposed. Alipay's public notify route authenticates by RSA2 signature.
func RegisterProcurementProxy(h *server.Hertz, runtimeURL, paymentURL string) error {
	if runtimeURL != "" {
		handler, err := bufferedProxy(runtimeURL, "", "/api/mvp/")
		if err != nil {
			return err
		}
		h.Any("/api/mvp/*path", handler)
	}
	if paymentURL != "" {
		handler, err := bufferedProxy(paymentURL, "/v1/fiat/alipay/notify", "")
		if err != nil {
			return err
		}
		h.POST("/api/payments/alipay/notify", handler)
	}
	return nil
}
func bufferedProxy(base, fixedPath, prefix string) (app.HandlerFunc, error) {
	upstream, err := url.Parse(base)
	if err != nil || upstream.Host == "" || (upstream.Scheme != "http" && upstream.Scheme != "https") || upstream.User != nil || upstream.RawQuery != "" || upstream.Fragment != "" || strings.Trim(upstream.Path, "/") != "" {
		return nil, fmt.Errorf("invalid procurement upstream URL")
	}
	client := &nethttp.Client{Timeout: 35 * time.Second, CheckRedirect: func(*nethttp.Request, []*nethttp.Request) error { return nethttp.ErrUseLastResponse }}
	return func(ctx context.Context, c *app.RequestContext) {
		if len(c.Request.Body()) > 64<<10 {
			c.String(413, "request too large")
			return
		}
		path := fixedPath
		if path == "" {
			path = string(c.Request.URI().Path())
			if !strings.HasPrefix(path, prefix) {
				c.String(404, "not found")
				return
			}
		}
		target := strings.TrimRight(base, "/") + path
		if query := c.Request.URI().QueryString(); len(query) > 0 {
			target += "?" + string(query)
		}
		req, err := nethttp.NewRequestWithContext(ctx, string(c.Request.Method()), target, bytes.NewReader(c.Request.Body()))
		if err != nil {
			c.String(502, "upstream unavailable")
			return
		}
		for _, header := range []string{"Content-Type", "Cookie", "Origin", "Idempotency-Key", "X-Requested-With"} {
			if value := c.GetHeader(header); len(value) > 0 {
				req.Header.Set(header, string(value))
			}
		}
		response, err := client.Do(req)
		if err != nil {
			c.String(502, "procurement service unavailable")
			return
		}
		defer response.Body.Close()
		body, err := io.ReadAll(io.LimitReader(response.Body, (2<<20)+1))
		if err != nil || len(body) > 2<<20 {
			c.String(502, "invalid upstream response")
			return
		}
		for _, header := range []string{"Content-Type", "Cache-Control", "Content-Disposition", "X-Content-Type-Options"} {
			if value := response.Header.Get(header); value != "" {
				c.Response.Header.Set(header, value)
			}
		}
		for _, cookie := range response.Header.Values("Set-Cookie") {
			c.Response.Header.Add("Set-Cookie", cookie)
		}
		c.SetStatusCode(response.StatusCode)
		c.Response.SetBody(body)
	}, nil
}
