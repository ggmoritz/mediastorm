package streaming

import (
	"context"
	"encoding/xml"
	"fmt"
	"log"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"novastream/internal/nzb/metadata"
	metapb "novastream/internal/nzb/metadata/proto"
)

var (
	filenameSanitizer    = regexp.MustCompile(`[^a-zA-Z0-9._-]+`)
	pathSeparatorCleaner = strings.NewReplacer("/", "_", "\\", "_")
)

type Registry struct {
	metadata *metadata.MetadataService
	root     string
	maxAge   time.Duration
	mu       sync.Mutex
}

type Registration struct {
	Path string
	Size int64
}

func NewRegistry(metadataService *metadata.MetadataService, root string, maxAge time.Duration) *Registry {
	return &Registry{metadata: metadataService, root: root, maxAge: maxAge}
}

func (r *Registry) Register(ctx context.Context, fileName string, nzbBytes []byte) (*Registration, error) {
	if r.metadata == nil {
		return nil, fmt.Errorf("metadata service not configured")
	}

	log.Printf("[streaming] register begin file=%q bytes=%d", strings.TrimSpace(fileName), len(nzbBytes))

	parsed, err := parseNZB(nzbBytes)
	if err != nil {
		return nil, fmt.Errorf("parse nzb: %w", err)
	}

	if len(parsed.Segments) == 0 {
		return nil, fmt.Errorf("nzb contains no segments")
	}

	// Use the actual decoded size from yEnc header if available
	totalSize := parsed.DecodedSize
	if totalSize <= 0 {
		// Fallback: estimate from encoded segment sizes (will be ~2-3% too large)
		log.Printf("[streaming] WARN: no yEnc size found, using encoded size estimate")
		for _, seg := range parsed.Segments {
			if seg.Bytes > 0 {
				totalSize += seg.Bytes
			}
		}
	}

	if totalSize <= 0 {
		return nil, fmt.Errorf("unable to determine file size")
	}

	// Build segment metadata with encoded sizes only.
	// We cannot accurately pre-calculate decoded segment boundaries because yEnc
	// overhead varies per segment. The decoder will determine actual boundaries at runtime.
	segmentData := make([]*metapb.SegmentData, 0, len(parsed.Segments))
	for _, seg := range parsed.Segments {
		if seg.Bytes <= 0 || seg.ID == "" {
			continue
		}

		segmentData = append(segmentData, &metapb.SegmentData{
			SegmentSize: seg.Bytes, // Encoded size from NZB
			StartOffset: 0,         // Will be determined by decoder at runtime
			EndOffset:   0,         // Will be determined by decoder at runtime
			Id:          strings.TrimSpace(seg.ID),
		})
	}

	if len(segmentData) == 0 {
		return nil, fmt.Errorf("nzb segments missing size or ids")
	}

	log.Printf("[streaming] register parsed subject=%q segments=%d totalSize=%d (decoded)", parsed.FileName, len(segmentData), totalSize)

	id := uuid.NewString()
	safeName := sanitizeFileName(parsed.FileName)
	if safeName == "" {
		safeName = sanitizeFileName(fileName)
	}
	if safeName == "" {
		safeName = "stream.bin"
	}

	relativePath := filepath.Join(id, safeName)
	// The public path is slash-separated on every platform.
	publicPath := path.Join("streams", id, safeName)
	streamDir := filepath.Join(r.root, id)

	nzbFileName := sanitizeFileName(fileName)
	if nzbFileName == "" {
		nzbFileName = id + ".nzb"
	}
	if lower := strings.ToLower(nzbFileName); !strings.HasSuffix(lower, ".nzb") {
		nzbFileName += ".nzb"
	}
	nzbPath := filepath.Join(streamDir, nzbFileName)

	meta := r.metadata.CreateFileMetadata(totalSize, nzbPath, metapb.FileStatus_FILE_STATUS_HEALTHY, segmentData, metapb.Encryption_NONE, "", "")

	r.mu.Lock()
	defer r.mu.Unlock()

	if err := r.cleanupExpiredLocked(); err != nil {
		return nil, err
	}

	if err := os.MkdirAll(streamDir, 0o755); err != nil {
		return nil, fmt.Errorf("create stream directory: %w", err)
	}

	if err := os.WriteFile(nzbPath, nzbBytes, 0o644); err != nil {
		return nil, fmt.Errorf("write nzb file: %w", err)
	}

	if err := r.metadata.WriteFileMetadata(relativePath, meta); err != nil {
		return nil, fmt.Errorf("write metadata: %w", err)
	}

	log.Printf("[streaming] register complete path=%q size=%d nzb=%q", publicPath, totalSize, nzbPath)
	return &Registration{Path: publicPath, Size: totalSize}, nil
}

func (r *Registry) cleanupExpiredLocked() error {
	if r.maxAge <= 0 || r.root == "" {
		return nil
	}

	entries, err := os.ReadDir(r.root)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("list stream root: %w", err)
	}
	if err != nil {
		return nil
	}

	cutoff := time.Now().Add(-r.maxAge)

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if entry.Name() == "streams" {
			legacyDir := filepath.Join(r.root, entry.Name())
			legacyEntries, err := os.ReadDir(legacyDir)
			if err != nil {
				continue
			}
			for _, legacy := range legacyEntries {
				if !legacy.IsDir() {
					continue
				}
				legacyInfo, err := legacy.Info()
				if err != nil {
					continue
				}
				if legacyInfo.ModTime().Before(cutoff) {
					dirPath := filepath.Join(legacyDir, legacy.Name())
					log.Printf("[streaming] cleanup removing legacy %s (age=%s)", dirPath, time.Since(legacyInfo.ModTime()))
					_ = os.RemoveAll(dirPath)
				}
			}
			continue
		}
		dirPath := filepath.Join(r.root, entry.Name())
		info, err := entry.Info()
		if err != nil {
			continue
		}
		if info.ModTime().Before(cutoff) {
			log.Printf("[streaming] cleanup removing %s (age=%s)", dirPath, time.Since(info.ModTime()).String())
			if err := os.RemoveAll(dirPath); err != nil {
				log.Printf("[streaming] cleanup failed for %s: %v", dirPath, err)
			}
		}
	}
	return nil
}

func sanitizeFileName(name string) string {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return ""
	}

	sanitized := pathSeparatorCleaner.Replace(trimmed)
	sanitized = filenameSanitizer.ReplaceAllString(sanitized, "_")
	sanitized = strings.TrimLeft(sanitized, "._-")
	sanitized = strings.Trim(sanitized, "_")
	return sanitized
}

type nzbFile struct {
	FileName    string
	Segments    []nzbSegment
	DecodedSize int64 // Actual decoded file size from yEnc header
}

type nzbSegment struct {
	Bytes int64  `xml:"bytes,attr"`
	ID    string `xml:",chardata"`
}

type nzbRoot struct {
	Files []struct {
		Subject string       `xml:"subject,attr"`
		Segs    []nzbSegment `xml:"segments>segment"`
	} `xml:"file"`
}

func parseNZB(data []byte) (*nzbFile, error) {
	var root nzbRoot
	if err := xml.Unmarshal(data, &root); err != nil {
		return nil, err
	}
	if len(root.Files) == 0 {
		return nil, fmt.Errorf("nzb has no files")
	}
	var (
		selected          nzbFile
		bestTotal         int64
		bestExtensionRank = len(videoPreference) + 1
	)

	for _, f := range root.Files {
		subject := strings.TrimSpace(f.Subject)
		if shouldSkipSubjectForStreaming(subject) {
			continue
		}

		var total int64
		segments := make([]nzbSegment, 0, len(f.Segs))
		for _, seg := range f.Segs {
			if seg.Bytes <= 0 || strings.TrimSpace(seg.ID) == "" {
				continue
			}
			total += seg.Bytes
			segments = append(segments, seg)
		}

		if total == 0 || len(segments) == 0 {
			continue
		}

		// Extract actual decoded size from yEnc header in subject
		decodedSize := extractYEncSize(subject)

		ext := detectContainerExt(subject)
		rank, ok := videoPreference[ext]
		if !ok {
			rank = len(videoPreference) + 1
		}

		if selected.Segments == nil || rank < bestExtensionRank || (rank == bestExtensionRank && total > bestTotal) {
			selected.FileName = f.Subject
			selected.Segments = segments
			selected.DecodedSize = decodedSize
			bestExtensionRank = rank
			bestTotal = total
		}
	}

	if len(selected.Segments) == 0 {
		return nil, fmt.Errorf("nzb file contains no usable segments")
	}
	return &selected, nil
}

// extractYEncSize parses the actual decoded file size from NZB subject line.
// Format: "filename" yEnc <size> (part/total)
// Example: [1/8] - "video.mkv" yEnc  1314508577 (1/1834)
func extractYEncSize(subject string) int64 {
	// Look for "yEnc" followed by whitespace and a number
	yencIdx := strings.Index(subject, "yEnc")
	if yencIdx == -1 {
		return 0
	}

	// Find the part after "yEnc"
	afterYenc := subject[yencIdx+4:]

	// Extract numbers from the string
	var numStr strings.Builder
	foundDigit := false
	for _, ch := range afterYenc {
		if ch >= '0' && ch <= '9' {
			numStr.WriteRune(ch)
			foundDigit = true
		} else if foundDigit && (ch == ' ' || ch == '(' || ch == '\t') {
			// Stop at first space or parenthesis after number
			break
		}
	}

	if numStr.Len() == 0 {
		return 0
	}

	size, err := strconv.ParseInt(numStr.String(), 10, 64)
	if err != nil {
		return 0
	}

	return size
}

var skipSubjectFragments = []string{
	".par2",
	".par",
	".srr",
	".sfv",
	".nfo",
}

func shouldSkipSubjectForStreaming(subject string) bool {
	lower := strings.ToLower(subject)
	for _, fragment := range skipSubjectFragments {
		if strings.Contains(lower, fragment) {
			return true
		}
	}
	return false
}

var videoPreference = map[string]int{
	".mp4":  0,
	".mkv":  1,
	".ts":   2,
	".m2ts": 3,
	".mts":  4,
	".avi":  5,
	".mov":  6,
	".webm": 7,
}
