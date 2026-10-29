package ascli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type failure struct {
	code int
	text string
}

func (e *failure) Error() string          { return e.text }
func fail(code int, message string) error { return &failure{code, message} }

// Only loopback HTTP is allowed, for local development and tests.
func parseURL(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.User != nil || u.Opaque != "" || u.Fragment != "" {
		return nil, fail(2, "expected an absolute HTTPS URL without credentials or fragment")
	}
	ip := net.ParseIP(u.Hostname())
	loopback := strings.EqualFold(u.Hostname(), "localhost") || (ip != nil && ip.IsLoopback())
	if u.Scheme != "https" && !(u.Scheme == "http" && loopback) {
		return nil, fail(2, "URLs must use HTTPS (HTTP is allowed only on loopback)")
	}
	return u, nil
}

func origin(raw string) (string, error) {
	u, err := parseURL(raw)
	if err != nil {
		return "", err
	}
	if (u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.ForceQuery {
		return "", fail(2, "API URL must contain only the scheme, hostname and optional port")
	}
	return u.Scheme + "://" + u.Host, nil
}

func presigned(u *url.URL) bool {
	q := u.Query()
	return q.Get("X-Amz-Signature") != "" || q.Get("Signature") != "" || q.Get("x-oss-signature") != ""
}

func newClient(o options) (*http.Client, *http.Transport) {
	t := http.DefaultTransport.(*http.Transport).Clone()
	t.DisableCompression = true // preserve object bytes and Content-Length
	t.ResponseHeaderTimeout = o.idleTimeout
	t.TLSHandshakeTimeout = o.idleTimeout
	t.DialContext = (&net.Dialer{Timeout: o.idleTimeout, KeepAlive: 30 * time.Second}).DialContext
	return &http.Client{Transport: t, Timeout: o.timeout, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 10 {
			return errors.New("too many redirects")
		}
		if _, err := parseURL(req.URL.String()); err != nil {
			return err
		}
		if via[0].URL.Scheme == "https" && req.URL.Scheme != "https" {
			return errors.New("HTTPS downgrade")
		}
		// Authenticated requests must never send grants to redirects, including
		// subdomains which net/http may otherwise consider trusted.
		if via[0].Header.Get("Authorization") != "" || via[0].Method != http.MethodGet {
			return http.ErrUseLastResponse
		}
		return nil
	}}, t
}

// idleBody cancels a stalled response. The request deadline also bounds streams
// which keep making progress indefinitely. No timer is left after Close.
type idleBody struct {
	io.ReadCloser
	timer  *time.Timer
	idle   time.Duration
	cancel context.CancelFunc
}

func (b *idleBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	if n > 0 {
		b.timer.Reset(b.idle)
	}
	return n, err
}
func (b *idleBody) Close() error {
	b.timer.Stop()
	b.cancel()
	return b.ReadCloser.Close()
}

func (d *downloader) request(ctx context.Context, method, raw, grant string, body io.Reader) (*http.Response, error) {
	if _, err := parseURL(raw); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(ctx)
	r, err := http.NewRequestWithContext(ctx, method, raw, body)
	if err != nil {
		cancel()
		return nil, fail(1, "cannot create HTTP request")
	}
	r.Header.Set("User-Agent", "ascli/"+d.version)
	if body != nil {
		r.Header.Set("Content-Type", "application/json")
	}
	if grant != "" {
		r.Header.Set("Authorization", "Bearer "+grant)
	}
	resp, err := d.client.Do(r)
	if err != nil {
		cancel()
		// net/url.Error includes the complete URL, potentially containing a
		// password, share token or S3 signature. Never emit it to CI logs.
		return nil, fail(1, "HTTP request failed (network, TLS, redirect or timeout)")
	}
	resp.Body = &idleBody{ReadCloser: resp.Body, timer: time.AfterFunc(d.options.idleTimeout, cancel), idle: d.options.idleTimeout, cancel: cancel}
	return resp, nil
}

var errorCodePattern = regexp.MustCompile(`^[A-Z0-9_]{1,80}$`)

func responseError(resp *http.Response, api bool) error {
	code := 1
	if resp.StatusCode == 401 || resp.StatusCode == 403 || resp.StatusCode == 404 || resp.StatusCode == 410 || resp.StatusCode == 429 {
		code = 3
	}
	message := fmt.Sprintf("HTTP %d", resp.StatusCode)
	if api {
		var payload struct {
			Error struct {
				Code string `json:"code"`
			} `json:"error"`
		}
		if json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&payload) == nil && errorCodePattern.MatchString(payload.Error.Code) {
			message += " (" + payload.Error.Code + ")"
		}
	}
	if n, err := strconv.Atoi(resp.Header.Get("Retry-After")); err == nil && n >= 0 {
		message += fmt.Sprintf("; retry after %d seconds", n)
	}
	return fail(code, message)
}

func (d *downloader) json(ctx context.Context, method, raw, grant string, body io.Reader, dst any) error {
	resp, err := d.request(ctx, method, raw, grant, body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return responseError(resp, true)
	}
	dec := json.NewDecoder(io.LimitReader(resp.Body, 16<<20))
	if err := dec.Decode(dst); err != nil {
		return fail(1, "invalid or oversized JSON response")
	}
	if dec.Decode(new(any)) != io.EOF {
		return fail(1, "invalid or oversized JSON response")
	}
	return nil
}
