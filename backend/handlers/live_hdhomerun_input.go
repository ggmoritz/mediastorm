package handlers

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"

	"novastream/internal/netproxy"
	"novastream/internal/requestsecurity"
)

// A native tuner stream is MPEG-TS, without redirects or nested playlists.
// Fetch it in Go and feed pipe:0 to FFmpeg/ffprobe so they cannot make unchecked
// secondary requests. The allowlist exists only for this single fetch client.
func newHDHomeRunStreamClient(rawURL, proxyURL string) (*http.Client, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil || !isHDHomeRunStreamURL(parsed) {
		return nil, errors.New("invalid HDHomeRun stream URL")
	}
	policy := func(host, port string) bool {
		return privateMediaEndpointKey(host, port) == privateMediaEndpointKey(parsed.Hostname(), "5004")
	}
	var client *http.Client
	if proxyURL == "" {
		client = requestsecurity.NewSafeHTTPClient(0, 10, policy)
		if transport, ok := client.Transport.(*http.Transport); ok {
			transport.ResponseHeaderTimeout = defaultStreamOpenTimeout
		}
	} else {
		client, err = netproxy.NewHTTPClientWithOptions(netproxy.HTTPClientOptions{ResponseHeaderTimeout: defaultStreamOpenTimeout}, proxyURL)
		if err != nil {
			return nil, err
		}
	}
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		// HDHomeRun serves the selected stream directly. A changed URL has not
		// passed catalog authorization, even on the same host or a public CDN.
		return errors.New("HDHomeRun stream redirect is not allowed")
	}
	return client, nil
}

func openHDHomeRunStream(ctx context.Context, rawURL, proxyURL string) (io.ReadCloser, error) {
	client, err := newHDHomeRunStreamClient(rawURL, proxyURL)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		client.CloseIdleConnections()
		return nil, err
	}
	req.Header.Set("User-Agent", liveStreamUserAgent)
	response, err := client.Do(req)
	if err != nil {
		client.CloseIdleConnections()
		return nil, err
	}
	if response.StatusCode != http.StatusOK {
		response.Body.Close()
		client.CloseIdleConnections()
		return nil, fmt.Errorf("HDHomeRun stream returned HTTP %d", response.StatusCode)
	}
	body := newInspectedTSBody(response.Body, hdHomeRunStreamLogLabel(rawURL))
	return hdHomeRunStreamBody{ReadCloser: body, client: client}, nil
}

// hdHomeRunStreamLogLabel keeps the channel in log lines. The path is safe to
// log: isHDHomeRunStreamURL only admits /auto|tunerN/vN.M or /chN tuner paths.
func hdHomeRunStreamLogLabel(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil || !isHDHomeRunStreamURL(parsed) {
		return "stream=" + requestsecurity.URLForLog(rawURL)
	}
	return "tuner=" + parsed.Host + " channel=" + parsed.Path
}

type hdHomeRunStreamBody struct {
	io.ReadCloser
	client *http.Client
}

func (body hdHomeRunStreamBody) Close() error {
	err := body.ReadCloser.Close()
	body.client.CloseIdleConnections()
	return err
}

func (h *VideoHandler) probeHDHomeRunQuality(ctx context.Context, streamURL, proxyURL string) (liveQualityResult, error) {
	if h.ffprobePath == "" {
		return liveQualityResult{}, errors.New("ffprobe unavailable")
	}
	body, err := openHDHomeRunStream(ctx, streamURL, proxyURL)
	if err != nil {
		return liveQualityResult{}, err
	}
	defer body.Close()
	args := []string{"-v", "error", "-probesize", "5000000", "-analyzeduration", "5000000",
		"-protocol_whitelist", "pipe", "-print_format", "json", "-show_streams", "-show_format", "-f", "mpegts", "-i", "pipe:0"}
	return h.runLiveQualityProbe(ctx, args, body)
}
