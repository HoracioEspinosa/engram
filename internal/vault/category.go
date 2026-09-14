package vault

import (
	"fmt"
	"strings"
)

// Category is one of the eleven closed category directories a task folder may
// contain. The set is closed on purpose: a directory that does not name one of
// them is not a category, and the scanner reports it instead of inventing a
// twelfth bucket.
type Category string

const (
	CategoryAnalysis    Category = "analysis"
	CategoryPlans       Category = "plans"
	CategoryRunbooks    Category = "runbooks"
	CategoryReports     Category = "reports"
	CategoryPatches     Category = "patches"
	CategoryEvidences   Category = "evidences"
	CategoryEvidencesQA Category = "evidences-qa"
	CategoryBenchmarks  Category = "benchmarks"
	CategoryScripts     Category = "scripts"
	CategoryAssets      Category = "assets"
	CategoryExports     Category = "exports"
)

var allCategories = []Category{
	CategoryAnalysis,
	CategoryPlans,
	CategoryRunbooks,
	CategoryReports,
	CategoryPatches,
	CategoryEvidences,
	CategoryEvidencesQA,
	CategoryBenchmarks,
	CategoryScripts,
	CategoryAssets,
	CategoryExports,
}

// ErrUnknownCategory is returned by ParseCategory for a directory name outside
// the closed set.
var ErrUnknownCategory = fmt.Errorf("vault: unknown category")

// Categories returns the closed set, in scan order.
func Categories() []Category {
	out := make([]Category, len(allCategories))
	copy(out, allCategories)
	return out
}

// ParseCategory maps a directory name to its category. Surrounding blanks and
// letter case are forgiven; anything else is rejected.
func ParseCategory(name string) (Category, error) {
	normalized := Category(strings.ToLower(strings.TrimSpace(name)))
	for _, c := range allCategories {
		if c == normalized {
			return c, nil
		}
	}
	return "", fmt.Errorf("%w: %q", ErrUnknownCategory, name)
}
