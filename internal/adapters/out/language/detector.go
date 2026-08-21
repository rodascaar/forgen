// Package language implementa detección de lenguaje y toolchain.
package language

import (
	"context"
	"os"
	"path/filepath"
	"sort"

	"github.com/rodascaar/forgen/internal/core/ports"
	"github.com/go-enry/go-enry/v2"
)

// maxFilesToAnalyze limita el escaneo del detector.
const maxFilesToAnalyze = 2000

// Detector identifica el lenguaje dominante de un directorio.
type Detector struct{}

// NewDetector construye el detector de lenguaje.
func NewDetector() *Detector { return &Detector{} }

// Detect implementa ports.LanguageDetector.
func (d *Detector) Detect(_ context.Context, dir string) (string, error) {
	tally := make(map[string]int64)

	err := filepath.WalkDir(dir, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if entry.IsDir() {
			if path != dir && enry.IsVendor(path) {
				return filepath.SkipDir
			}
			return nil
		}
		if len(tally) >= maxFilesToAnalyze {
			return nil
		}
		if enry.IsGenerated(path, nil) {
			return nil
		}
		if language, _ := enry.GetLanguageByExtension(path); language != "" {
			tally[language]++
			return nil
		}
		if language, _ := enry.GetLanguageByFilename(path); language != "" {
			tally[language]++
			return nil
		}
		return nil
	})
	if err != nil {
		return "", err
	}

	if len(tally) == 0 {
		return "", nil
	}

	type languageCount struct {
		language string
		count    int64
	}
	counts := make([]languageCount, 0, len(tally))
	for language, count := range tally {
		counts = append(counts, languageCount{language: language, count: count})
	}
	sort.Slice(counts, func(i, j int) bool { return counts[i].count > counts[j].count })
	return counts[0].language, nil
}

var _ ports.LanguageDetector = (*Detector)(nil)
