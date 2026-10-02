package imagelookup

import (
	"archive/tar"
	"context"
	"errors"
	"fmt"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/types"
	"gopkg.in/yaml.v3"
	"io"
	"path"
	"sort"
	"strings"
)

func (s *Service) findBuildYAML(ctx context.Context, image v1.Image, warnings []string) (BuildYAML, []string) {
	layers, err := image.Layers()
	if err != nil {
		return BuildYAML{Error: SafeError(err), Skipped: true}, append(warnings, "build.yaml scan could not enumerate image layers")
	}

	hiddenPaths := map[string]struct{}{}
	hiddenDirectories := map[string]struct{}{}
	var scanned int64
	skippedLargeLayers := 0
	skippedUnknownSizeLayers := 0
	for index := len(layers) - 1; index >= 0; index-- {
		if err := ctx.Err(); err != nil {
			return BuildYAML{Error: "build.yaml scan timed out", Skipped: true}, append(warnings, "build.yaml scan timed out")
		}
		compressedSize, sizeErr := layers[index].Size()
		if sizeErr != nil || compressedSize < 0 {
			skippedUnknownSizeLayers++
			continue
		}
		if compressedSize > s.maxBuildLayer {
			skippedLargeLayers++
			continue
		}
		mediaType, mediaErr := layers[index].MediaType()
		if mediaErr != nil {
			warnings = append(warnings, fmt.Sprintf("build.yaml scan skipped layer %d with unknown media type", index))
			continue
		}
		if mediaType == types.OCILayerZStd {
			warnings = append(warnings, fmt.Sprintf("build.yaml scan skipped zstd layer %d", index))
			continue
		}
		reader, openErr := layers[index].Uncompressed()
		if openErr != nil {
			warnings = append(warnings, fmt.Sprintf("build.yaml scan skipped unreadable layer %d", index))
			continue
		}
		candidate, layerErr := s.scanBuildYAMLLayer(ctx, reader, hiddenPaths, hiddenDirectories, &scanned)
		_ = reader.Close()
		if layerErr != nil {
			if errors.Is(layerErr, errImageLookupScanLimit) {
				reason := imageLookupBuildYAMLScanLimitReason(s.maxLayerScan)
				if skippedReason := imageLookupBuildYAMLSkipReason(skippedLargeLayers, skippedUnknownSizeLayers, s.maxBuildLayer); skippedReason != "" {
					reason = skippedReason + " " + reason
				}
				return BuildYAML{Reason: reason, Skipped: true}, append(warnings, reason)
			}
			warnings = append(warnings, fmt.Sprintf("build.yaml scan skipped malformed layer %d", index))
			continue
		}
		if candidate != nil {
			warnings = imageLookupAppendBuildYAMLSkipWarning(warnings, skippedLargeLayers, skippedUnknownSizeLayers, s.maxBuildLayer)
			result := BuildYAML{Found: true, Path: candidate.path, Raw: string(candidate.content)}
			var data map[string]any
			if yamlErr := yaml.Unmarshal(candidate.content, &data); yamlErr != nil {
				result.Error = "build.yaml was found but could not be parsed: " + SafeError(yamlErr)
				warnings = append(warnings, "build.yaml was found but contains invalid YAML")
			} else {
				result.Data = data
			}
			return result, warnings
		}
	}
	if reason := imageLookupBuildYAMLSkipReason(skippedLargeLayers, skippedUnknownSizeLayers, s.maxBuildLayer); reason != "" {
		return BuildYAML{Skipped: true, Reason: reason}, append(warnings, reason)
	}
	return BuildYAML{}, warnings
}

func imageLookupAppendBuildYAMLSkipWarning(warnings []string, skippedLarge, skippedUnknown int, limit int64) []string {
	if reason := imageLookupBuildYAMLSkipReason(skippedLarge, skippedUnknown, limit); reason != "" {
		return append(warnings, reason)
	}
	return warnings
}

func imageLookupBuildYAMLSkipReason(skippedLarge, skippedUnknown int, limit int64) string {
	reasons := make([]string, 0, 2)
	if skippedLarge > 0 {
		reasons = append(reasons, fmt.Sprintf(
			"Skipped %d layer%s larger than the %s safe scan limit.",
			skippedLarge,
			imageLookupPlural(skippedLarge),
			imageLookupByteSize(limit),
		))
	}
	if skippedUnknown > 0 {
		reasons = append(reasons, fmt.Sprintf(
			"Skipped %d layer%s because the compressed size could not be verified.",
			skippedUnknown,
			imageLookupPlural(skippedUnknown),
		))
	}
	return strings.Join(reasons, " ")
}

func imageLookupBuildYAMLScanLimitReason(limit int64) string {
	return fmt.Sprintf(
		"Stopped after reaching the %s cumulative uncompressed safe scan limit; remaining image layer data was not scanned.",
		imageLookupByteSize(limit),
	)
}

func imageLookupPlural(count int) string {
	if count == 1 {
		return ""
	}
	return "s"
}

func imageLookupByteSize(size int64) string {
	if size > 0 && size%(1<<20) == 0 {
		return fmt.Sprintf("%d MiB", size>>20)
	}
	if size > 0 && size%(1<<10) == 0 {
		return fmt.Sprintf("%d KiB", size>>10)
	}
	return fmt.Sprintf("%d bytes", size)
}

var errImageLookupScanLimit = errors.New("image layer scan limit exceeded")

type imageLookupBuildCandidate struct {
	path    string
	content []byte
}

type imageLookupCountingReader struct {
	reader io.Reader
	count  *int64
	limit  int64
}

func (r *imageLookupCountingReader) Read(buffer []byte) (int, error) {
	if *r.count >= r.limit {
		return 0, errImageLookupScanLimit
	}
	remaining := r.limit - *r.count
	if int64(len(buffer)) > remaining {
		buffer = buffer[:remaining]
	}
	n, err := r.reader.Read(buffer)
	*r.count += int64(n)
	if err == nil && *r.count >= r.limit {
		return n, errImageLookupScanLimit
	}
	return n, err
}

func (s *Service) scanBuildYAMLLayer(ctx context.Context, reader io.Reader, hiddenPaths, hiddenDirectories map[string]struct{}, scanned *int64) (*imageLookupBuildCandidate, error) {
	archive := tar.NewReader(&imageLookupCountingReader{reader: reader, count: scanned, limit: s.maxLayerScan})
	candidates := map[string][]byte{}
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		header, err := archive.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		cleaned, valid := imageLookupCleanArchivePath(header.Name)
		if !valid {
			continue
		}
		base := path.Base(cleaned)
		directory := path.Dir(cleaned)
		if base == ".wh..wh..opq" {
			hiddenDirectories[directory] = struct{}{}
			continue
		}
		if strings.HasPrefix(base, ".wh.") {
			hiddenPaths[path.Join(directory, strings.TrimPrefix(base, ".wh."))] = struct{}{}
			continue
		}
		if header.Typeflag != tar.TypeReg && header.Typeflag != tar.TypeRegA {
			continue
		}
		if base != "build.yaml" || imageLookupArchivePathHidden(cleaned, hiddenPaths, hiddenDirectories) {
			continue
		}
		if header.Size < 0 || header.Size > s.maxBuildYML {
			continue
		}
		content, readErr := io.ReadAll(io.LimitReader(archive, s.maxBuildYML+1))
		if readErr != nil {
			return nil, readErr
		}
		if int64(len(content)) > s.maxBuildYML {
			continue
		}
		candidates[cleaned] = content
	}
	if len(candidates) == 0 {
		return nil, nil
	}
	paths := make([]string, 0, len(candidates))
	for candidatePath := range candidates {
		paths = append(paths, candidatePath)
	}
	sort.Slice(paths, func(i, j int) bool {
		if len(paths[i]) != len(paths[j]) {
			return len(paths[i]) < len(paths[j])
		}
		return paths[i] < paths[j]
	})
	return &imageLookupBuildCandidate{path: paths[0], content: candidates[paths[0]]}, nil
}

func imageLookupCleanArchivePath(value string) (string, bool) {
	value = strings.TrimPrefix(strings.ReplaceAll(value, "\\", "/"), "./")
	if value == "" || strings.HasPrefix(value, "/") {
		return "", false
	}
	cleaned := path.Clean(value)
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return "", false
	}
	return cleaned, true
}

func imageLookupArchivePathHidden(value string, hiddenPaths, hiddenDirectories map[string]struct{}) bool {
	if _, hidden := hiddenPaths[value]; hidden {
		return true
	}
	for directory := path.Dir(value); directory != "." && directory != "/"; directory = path.Dir(directory) {
		if _, hidden := hiddenDirectories[directory]; hidden {
			return true
		}
	}
	return false
}

func imageLookupBoundedLabels(labels map[string]string) map[string]string {
	if len(labels) == 0 {
		return map[string]string{}
	}
	keys := make([]string, 0, len(labels))
	for key := range labels {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	if len(keys) > 256 {
		keys = keys[:256]
	}
	result := make(map[string]string, len(keys))
	for _, key := range keys {
		value := labels[key]
		if len(value) > 8192 {
			value = value[:8192]
		}
		result[key] = value
	}
	return result
}

func imageLookupBoundedStrings(values []string, maxItems, maxLength int) []string {
	if len(values) > maxItems {
		values = values[:maxItems]
	}
	result := make([]string, len(values))
	for index, value := range values {
		if len(value) > maxLength {
			value = value[:maxLength]
		}
		result[index] = value
	}
	return result
}

func imageLookupBoundedHistory(history []v1.History) []HistoryEntry {
	if len(history) == 0 {
		return []HistoryEntry{}
	}
	if len(history) > imageLookupMaxHistoryEntries {
		history = history[len(history)-imageLookupMaxHistoryEntries:]
	}
	result := make([]HistoryEntry, 0, len(history))
	for _, entry := range history {
		createdBy := entry.CreatedBy
		if len(createdBy) > imageLookupMaxHistoryText {
			createdBy = createdBy[:imageLookupMaxHistoryText]
		}
		comment := entry.Comment
		if len(comment) > imageLookupMaxHistoryText {
			comment = comment[:imageLookupMaxHistoryText]
		}
		result = append(result, HistoryEntry{
			Created:    imageLookupFormatTime(entry.Created.Time),
			CreatedBy:  createdBy,
			Comment:    comment,
			EmptyLayer: entry.EmptyLayer,
		})
	}
	return result
}
