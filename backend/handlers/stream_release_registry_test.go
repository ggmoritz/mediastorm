package handlers

import (
	"path/filepath"
	"testing"

	"novastream/models"
	"novastream/services/badstreams"
)

func newTestStreamReleaseRegistry(t *testing.T) (*streamReleaseRegistry, *badstreams.Service) {
	t.Helper()
	service := badstreams.New(filepath.Join(t.TempDir(), "bad_streams.json"))
	return &streamReleaseRegistry{releases: make(map[string]streamRelease), badStreams: service}, service
}

func hocusPocusResult() models.NZBResult {
	return models.NZBResult{
		Title:       "Hocus.Pocus.1993.2160p.DSNP.WEB-DL.DDP.5.1.DV.HDR.H.265-PiRaTeS",
		ServiceType: models.ServiceTypeUsenet,
		Attributes:  map[string]string{"provider": "NZBGeek"},
	}
}

func TestStreamReleaseRegistryMarksReleaseBadOnMissingArticles(t *testing.T) {
	registry, service := newTestStreamReleaseRegistry(t)
	result := hocusPocusResult()
	registry.remember("/webdav/Hocus.pocus.1993.mkv", result)

	// The read path reports the path without the /webdav prefix.
	if !registry.markBad("/Hocus.pocus.1993.mkv", streamFailureRecord{Reason: "article_not_found"}) {
		t.Fatal("markBad returned false for a missing-article failure")
	}
	if !service.IsBad(result) {
		t.Fatal("release was not marked bad")
	}
}

func TestStreamReleaseRegistryIgnoresTransientFailures(t *testing.T) {
	registry, service := newTestStreamReleaseRegistry(t)
	result := hocusPocusResult()
	registry.remember("/webdav/Hocus.pocus.1993.mkv", result)

	for _, reason := range []string{"provider_unavailable", "stream_not_found"} {
		if registry.markBad("/Hocus.pocus.1993.mkv", streamFailureRecord{Reason: reason}) {
			t.Fatalf("markBad marked a release bad for %s", reason)
		}
	}
	if service.IsBad(result) {
		t.Fatal("release was marked bad for a transient failure")
	}
}

func TestStreamReleaseRegistryIgnoresUnknownPaths(t *testing.T) {
	registry, service := newTestStreamReleaseRegistry(t)
	if registry.markBad("/other.mkv", streamFailureRecord{Reason: "article_not_found"}) {
		t.Fatal("markBad marked an unresolved path")
	}
	if len(service.List()) != 0 {
		t.Fatal("registry recorded an entry for an unresolved path")
	}
}
