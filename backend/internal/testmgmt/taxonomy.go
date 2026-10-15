package testmgmt

import (
	"encoding/json"
	"fmt"
	"slices"
)

// Taxonomy classifies a case, not its results or automation identity.
type Taxonomy struct {
	Level            string   `json:"level"`
	Purposes         []string `json:"purposes"`
	Tags             []string `json:"tags"`
	ExecutionModes   []string `json:"executionModes"`
	PerformanceTypes []string `json:"performanceTypes"`
}

var taxonomyLevels = []string{"", "unit", "integration", "system", "acceptance"}
var taxonomyPurposes = []string{"functional", "performance", "security", "usability", "accessibility", "compatibility", "recovery"}
var taxonomyTags = []string{"smoke", "regression", "e2e"}
var taxonomyModes = []string{"manual", "automated"}
var taxonomyPerformance = []string{"load", "stress", "spike", "soak", "volume", "scalability"}

func normalizeTaxonomy(t Taxonomy) (Taxonomy, error) {
	if !slices.Contains(taxonomyLevels, t.Level) {
		return Taxonomy{}, fmt.Errorf("%w: taxonomy level", ErrInvalid)
	}
	for _, entry := range []struct {
		values  *[]string
		allowed []string
	}{
		{&t.Purposes, taxonomyPurposes}, {&t.Tags, taxonomyTags},
		{&t.ExecutionModes, taxonomyModes}, {&t.PerformanceTypes, taxonomyPerformance},
	} {
		values := append([]string{}, (*entry.values)...)
		seen := map[string]bool{}
		for _, value := range values {
			if !slices.Contains(entry.allowed, value) || seen[value] {
				return Taxonomy{}, fmt.Errorf("%w: invalid or duplicate taxonomy value", ErrInvalid)
			}
			seen[value] = true
		}
		slices.Sort(values)
		*entry.values = values
	}
	if len(t.PerformanceTypes) > 0 && !slices.Contains(t.Purposes, "performance") {
		return Taxonomy{}, fmt.Errorf("%w: performanceTypes requires performance purpose", ErrInvalid)
	}
	return t, nil
}

func taxonomyJSON(t Taxonomy) ([]byte, error) {
	normalized, err := normalizeTaxonomy(t)
	if err != nil {
		return nil, err
	}
	return json.Marshal(normalized)
}

func (f CaseFilter) taxonomyArgs() ([]any, error) {
	values := []string{f.Level, f.Purpose, f.Tag, f.ExecutionMode, f.PerformanceType}
	options := [][]string{taxonomyLevels, taxonomyPurposes, taxonomyTags, taxonomyModes, taxonomyPerformance}
	args := make([]any, len(values))
	for i, v := range values {
		if v != "" && !slices.Contains(options[i], v) {
			return nil, fmt.Errorf("%w: invalid taxonomy filter", ErrInvalid)
		}
		args[i] = v
	}
	return args, nil
}
