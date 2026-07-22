package template

import (
	"errors"
	"sort"
)

var (
	ErrDuplicateVersionNumber = errors.New("Номер версии шаблона уже используется")
	ErrVersionNumberGap       = errors.New("Нумерация версий шаблона должна быть последовательной")
	ErrMultipleDrafts         = errors.New("У шаблона может быть только один черновик")
	ErrDraftAlreadyExists     = errors.New("У шаблона уже есть редактируемый черновик")
	ErrDraftMustBeLatest      = errors.New("Черновик должен быть последней версией шаблона")
	ErrVersionNumberConflict  = errors.New("Список версий шаблона изменился, повторите операцию")
)

// VersionRef is the minimal state required for aggregate-wide numbering.
type VersionRef struct {
	Number int
	Status VersionStatus
}

// ValidateVersionSet enforces unique contiguous numbering and one latest
// draft. Storage must mirror these invariants with constraints and locking.
func ValidateVersionSet(versions []VersionRef) error {
	if len(versions) == 0 {
		return nil
	}
	sorted := append([]VersionRef(nil), versions...)
	sort.Slice(sorted, func(left, right int) bool { return sorted[left].Number < sorted[right].Number })
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
		case VersionDraft:
			drafts++
		case VersionPublished:
		default:
			return ErrUnknownVersionStatus
		}
	}
	if drafts > 1 {
		return ErrMultipleDrafts
	}
	if drafts == 1 && sorted[len(sorted)-1].Status != VersionDraft {
		return ErrDraftMustBeLatest
	}
	return nil
}

// PlanNextDraft allocates a proposal from a locked version snapshot. The
// expected latest number rejects stale commands before persistence.
func PlanNextDraft(versions []VersionRef, expectedLatestNumber int) (int, error) {
	if err := ValidateVersionSet(versions); err != nil {
		return 0, err
	}
	if len(versions) != expectedLatestNumber {
		return 0, ErrVersionNumberConflict
	}
	for _, version := range versions {
		if version.Status == VersionDraft {
			return 0, ErrDraftAlreadyExists
		}
	}
	return len(versions) + 1, nil
}
