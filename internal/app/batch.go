package app

import "strings"

type BatchMode int

const (
	BatchModeCLI BatchMode = iota
	BatchModeBot
)

type BatchAddItemStatus string

const (
	BatchAddStatusCreated          BatchAddItemStatus = "created"
	BatchAddStatusDuplicate        BatchAddItemStatus = "duplicate"
	BatchAddStatusFailedValidation BatchAddItemStatus = "failed_validation"
	BatchAddStatusFailedGeneration BatchAddItemStatus = "failed_generation"
)

type BatchAddItemResult struct {
	FrontRaw        string
	FrontNormalized string
	Status          BatchAddItemStatus
	Reason          string
}

type BatchAddSummary struct {
	Total             int
	Created           int
	SkippedDuplicates int
	Failed            int
}

type BatchAddReport struct {
	Items   []BatchAddItemResult
	Summary BatchAddSummary
}

func (r *BatchAddReport) AddItem(item BatchAddItemResult) {
	r.Items = append(r.Items, item)
	r.Summary.Total++
	switch item.Status {
	case BatchAddStatusCreated:
		r.Summary.Created++
	case BatchAddStatusDuplicate:
		r.Summary.SkippedDuplicates++
	case BatchAddStatusFailedGeneration, BatchAddStatusFailedValidation:
		r.Summary.Failed++
	}
}

func NormalizeBatchFronts(lines []string, mode BatchMode) []string {
	result := make([]string, 0, len(lines))
	for _, line := range lines {
		normalized := strings.TrimSpace(line)
		if normalized == "" {
			continue
		}
		if mode == BatchModeCLI && strings.HasPrefix(normalized, "#") {
			continue
		}
		result = append(result, normalized)
	}
	return result
}
