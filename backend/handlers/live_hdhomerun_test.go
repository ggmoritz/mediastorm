package handlers

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"novastream/config"
	"novastream/internal/auth"
	"novastream/models"
)

func hdHomeRunTestSettings() config.Settings {
	return config.Settings{Live: config.LiveSettings{Sources: []config.LivePlaylistSource{{
		ID: "tuner", Mode: "m3u", PlaylistURL: "http://192.168.1.100/lineup.m3u",
	}}}}
}

func TestHDHomeRunAuthorizationRequiresConfiguredCatalogChannel(t *testing.T) {
	const knownURL = "http://192.168.1.100:5004/auto/v5.1"
	for _, tc := range []struct {
		name, streamURL, catalogURL, query string
		want                               bool
	}{
		{"known channel", knownURL, knownURL, "sourceId=tuner&channelId=channel-5", true},
		{"legacy source parameter", knownURL, knownURL, "liveSourceId=tuner&channelId=channel-5", true},
		{"URL-only older client", knownURL, knownURL, "", true},
		{"unlisted channel", "http://192.168.1.100:5004/auto/v6.1", knownURL, "sourceId=tuner", false},
		{"wrong channel ID", knownURL, knownURL, "sourceId=tuner&channelId=channel-6", false},
		{"wrong source ID", knownURL, knownURL, "sourceId=other", false},
		{"other tuner", "http://192.168.1.101:5004/auto/v5.1", "http://192.168.1.101:5004/auto/v5.1", "sourceId=tuner", false},
		{"arbitrary listed endpoint", "http://192.168.1.100:5004/api/settings", "http://192.168.1.100:5004/api/settings", "sourceId=tuner", false},
		{"other listed port", "http://192.168.1.100:7777/auto/v5.1", "http://192.168.1.100:7777/auto/v5.1", "sourceId=tuner", false},
		{"different query", knownURL + "?duration=5", knownURL, "sourceId=tuner", false},
		{"listed stream query", knownURL + "?duration=5", knownURL + "?duration=5", "sourceId=tuner", true},
		{"specific tuner", "http://192.168.1.100:5004/tuner1/v5.1", "http://192.168.1.100:5004/tuner1/v5.1", "sourceId=tuner", true},
		{"frequency and program", "http://192.168.1.100:5004/auto/ch473000000-3", "http://192.168.1.100:5004/auto/ch473000000-3", "sourceId=tuner", true},
		{"encoded path", "http://192.168.1.100:5004/auto/%765.1", "http://192.168.1.100:5004/auto/%765.1", "sourceId=tuner", false},
		{"URL credentials", "http://user:pass@192.168.1.100:5004/auto/v5.1", "http://user:pass@192.168.1.100:5004/auto/v5.1", "sourceId=tuner", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			catalog := staticLiveChannelProvider{channels: []LiveChannel{{ID: "channel-5", SourceID: "tuner", URL: tc.catalogURL}}}
			request := httptest.NewRequest(http.MethodGet, "/live/hls/start?"+tc.query, nil)
			allowed, err := authorizeLiveStreamURL(request, tc.streamURL, staticSecurityConfigProvider{hdHomeRunTestSettings()}, catalog)
			if allowed != tc.want || (err == nil) != tc.want {
				t.Fatalf("HDHomeRun authorization=%v error=%v, want %v", allowed, err, tc.want)
			}
		})
	}
}

func TestHDHomeRunAuthorizationRejectsUnavailableOrDisabledSources(t *testing.T) {
	disabled := false
	for _, tc := range []struct {
		name     string
		playlist string
		enabled  *bool
		catalog  LiveChannelProvider
	}{
		{"ordinary playlist", "http://192.168.1.100/channels.m3u", nil, staticLiveChannelProvider{}},
		{"disabled source", "http://192.168.1.100/lineup.m3u", &disabled, staticLiveChannelProvider{}},
		{"catalog unavailable", "http://192.168.1.100/lineup.m3u", nil, nil},
		{"catalog fetch failed", "http://192.168.1.100/lineup.m3u", nil, staticLiveChannelProvider{err: errors.New("fetch failed")}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			settings := hdHomeRunTestSettings()
			settings.Live.Sources[0].PlaylistURL = tc.playlist
			settings.Live.Sources[0].Enabled = tc.enabled
			request := httptest.NewRequest(http.MethodGet, "/live/hls/start?sourceId=tuner", nil)
			if allowed, err := authorizeLiveStreamURL(request, "http://192.168.1.100:5004/auto/v5.1", staticSecurityConfigProvider{settings}, tc.catalog); allowed || err == nil {
				t.Fatalf("allowed=%v error=%v", allowed, err)
			}
		})
	}
}

func TestHDHomeRunAuthorizationHonorsProfileVisibility(t *testing.T) {
	settings := hdHomeRunTestSettings()
	settings.Live.Sources[0].AllowedProfiles = []string{"allowed"}
	stream := "http://192.168.1.100:5004/auto/v5.1"
	catalog := staticLiveChannelProvider{channels: []LiveChannel{{ID: "channel-5", SourceID: "tuner", URL: stream}}}
	for _, profile := range []string{"allowed", "blocked"} {
		request := httptest.NewRequest(http.MethodGet, "/live/hls/start?profileId="+profile, nil)
		allowed, err := authorizeLiveStreamURL(request, stream, staticSecurityConfigProvider{settings}, catalog)
		if want := profile == "allowed"; allowed != want || (err == nil) != want {
			t.Fatalf("profile=%s allowed=%v error=%v", profile, allowed, err)
		}
	}
}

func TestHDHomeRunChannelDoesNotAuthorizeGenericVideo(t *testing.T) {
	h := NewVideoHandler(false, "", "")
	h.SetConfigManager(staticSecurityConfigProvider{hdHomeRunTestSettings()})
	stream := "http://192.168.1.100:5004/auto/v5.1"
	h.SetLiveChannelProvider(staticLiveChannelProvider{channels: []LiveChannel{{ID: "channel-5", SourceID: "tuner", URL: stream}}})
	request := httptest.NewRequest(http.MethodGet, "/video/stream?sourceId=tuner&channelId=channel-5", nil)
	response := httptest.NewRecorder()
	if h.requireAllowedExternalPath(response, request, stream) || response.Code != http.StatusBadRequest {
		t.Fatalf("generic video accepted tuner exception: status=%d", response.Code)
	}
}

func TestHDHomeRunSessionRejectsForgedChannelBeforeCreatingSession(t *testing.T) {
	h := NewVideoHandlerWithProvider(true, stubBinary(t, "true"), stubBinary(t, "true"), t.TempDir(), nil)
	defer h.hlsManager.Shutdown()
	h.SetConfigManager(staticSecurityConfigProvider{hdHomeRunTestSettings()})
	h.SetLiveChannelProvider(staticLiveChannelProvider{channels: []LiveChannel{{ID: "channel-5", SourceID: "tuner", URL: "http://192.168.1.100:5004/auto/v5.1"}}})
	request := httptest.NewRequest(http.MethodGet, "/live/hls/start?sourceId=tuner&url="+url.QueryEscape("http://192.168.1.100:5004/auto/v6.1"), nil)
	response := httptest.NewRecorder()
	h.StartLiveHLSSession(response, request)
	if response.Code != http.StatusBadRequest || len(h.hlsManager.sessions) != 0 {
		t.Fatalf("forged channel accepted: status=%d sessions=%d", response.Code, len(h.hlsManager.sessions))
	}
}

func TestHDHomeRunLiveProfileRejectsAnotherAccount(t *testing.T) {
	users := fakeLiveUsageUsersProvider{users: map[string]models.User{"other": {ID: "other", AccountID: "account-b"}}}
	request := httptest.NewRequest(http.MethodGet, "/live/stream?profileId=other", nil)
	request = request.WithContext(context.WithValue(request.Context(), auth.ContextKeyAccountID, "account-a"))
	response := httptest.NewRecorder()
	if requireLiveStreamProfile(response, request, users) || response.Code != http.StatusNotFound {
		t.Fatal("another account's profile accepted")
	}
}

func TestHDHomeRunStreamClientRejectsRedirects(t *testing.T) {
	for _, target := range []string{
		"http://192.168.1.100:5004/api/settings",
		"http://192.168.1.100:5004/auto/v6.1",
		"http://192.168.1.100:7777/api/settings",
		"http://192.168.1.101:5004/auto/v5.1",
		"https://example.com/stream.ts",
	} {
		t.Run(target, func(t *testing.T) {
			client, err := newHDHomeRunStreamClient("http://192.168.1.100:5004/auto/v5.1", "")
			if err != nil {
				t.Fatal(err)
			}
			calls := 0
			client.Transport = discoveryTestTransport(func(r *http.Request) (*http.Response, error) {
				calls++
				return discoveryTestResponse(http.StatusFound, "", http.Header{"Location": []string{target}}), nil
			})
			resp, err := client.Get("http://192.168.1.100:5004/auto/v5.1")
			if resp != nil {
				resp.Body.Close()
			}
			if err == nil || !strings.Contains(err.Error(), "redirect is not allowed") || calls != 1 {
				t.Fatalf("redirect made %d requests; error=%v", calls, err)
			}
		})
	}
}
