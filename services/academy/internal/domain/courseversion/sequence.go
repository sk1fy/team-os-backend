package courseversion

import (
	"errors"
	"sort"
)

var (
	ErrDuplicateVersionNumber = errors.New("Номер версии уже используется")
	ErrVersionNumberGap       = errors.New("Нумерация версий должна быть последовательной")
	ErrMultipleDrafts         = errors.New("У курса может быть только один черновик")
	ErrDraftAlreadyExists     = errors.New("У курса уже есть редактируемый черновик")
	ErrDraftMustBeLatest      = errors.New("Черновик должен быть последней версией курса")
	ErrVersionNumberConflict  = errors.New("Список версий изменился, повторите операцию")
)

// VersionRef is the minimal version information needed for allocation.
type VersionRef struct {
	Number int
	Status Status
}

// ValidateVersionSet validates the aggregate-wide invariants that cannot be
// checked on one Version: unique contiguous numbers and at most one latest
// draft.
func ValidateVersionSet(versions []VersionRef) error {
	if len(versions) == 0 {
		return nil
	}

	sorted := append([]VersionRef(nil), versions...)
	sort.Slice(sorted, func(left, right int) bool {
		return sorted[left].Number < sorted[right].Number
	})
	drafts := 0
	for index, version := range sorted {
		if version.Number < 1 {
			return ErrVersionNumberInvalid
		}
		if index > 0 && version.Number == sorted[index-1].Number {
			return ErrDuplicateVersionNumber
		}
		if version.Number != index+1 {
			return ErrVersionNumberGap
		}
		switch version.Status {
		case StatusDraft:
			drafts++
		case StatusPublished, StatusRetired:
		default:
			return ErrUnknownStatus
		}
	}
	if drafts > 1 {
		return ErrMultipleDrafts
	}
	if drafts == 1 && sorted[len(sorted)-1].Status != StatusDraft {
		return ErrDraftMustBeLatest
	}
	return nil
}

// PlanNextDraft allocates the next number from a locked version snapshot.
// expectedLatestNumber is an optimistic concurrency token: a stale command is
// rejected instead of silently publishing two proposals with the same number.
// The storage layer must still hold a course lock and enforce a unique index.
func PlanNextDraft(versions []VersionRef, expectedLatestNumber int) (int, error) {
	if err := ValidateVersionSet(versions); err != nil {
		return 0, err
	}
	latestNumber := len(versions)
	if expectedLatestNumber != latestNumber {
		return 0, ErrVersionNumberConflict
	}
	for _, version := range versions {
		if version.Status == StatusDraft {
			return 0, ErrDraftAlreadyExists
		}
	}
	return latestNumber + 1, nil
}
