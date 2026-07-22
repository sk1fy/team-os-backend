package course

import "errors"

// LifecycleTransition is a user-visible course lifecycle command.
type LifecycleTransition string

const (
	TransitionArchive LifecycleTransition = "archive"
	TransitionRestore LifecycleTransition = "restore"
	TransitionDelete  LifecycleTransition = "delete"
)

var ErrUnknownLifecycleTransition = errors.New("Неизвестный переход жизненного цикла курса")

// LifecycleEffects is a persistence-neutral plan for dependent aggregates.
// The application layer applies these effects transactionally to links,
// campaigns and enrollments while retaining their history.
type LifecycleEffects struct {
	DisableNewDistribution     bool
	DisablePendingActivation   bool
	FreezeUnfinishedExternal   bool
	CloseLinksAndCampaigns     bool
	CloseEnrollments           bool
	HideIncompleteContent      bool
	HideAllLearnerContent      bool
	PreserveHistory            bool
	ReactivateFrozenEnrollment bool
}

// PlanLifecycle applies the root transition to a copy and returns the exact
// dependent effects. Course restore deliberately has no implicit enrollment
// or campaign reactivation.
func PlanLifecycle(root Course, transition LifecycleTransition) (Course, LifecycleEffects, error) {
	updated := root
	switch transition {
	case TransitionArchive:
		if err := updated.Archive(); err != nil {
			return root, LifecycleEffects{}, err
		}
		return updated, LifecycleEffects{
			DisableNewDistribution:   true,
			DisablePendingActivation: true,
			FreezeUnfinishedExternal: true,
			HideIncompleteContent:    true,
			PreserveHistory:          true,
		}, nil
	case TransitionRestore:
		if err := updated.Restore(); err != nil {
			return root, LifecycleEffects{}, err
		}
		return updated, LifecycleEffects{
			PreserveHistory:            true,
			ReactivateFrozenEnrollment: false,
		}, nil
	case TransitionDelete:
		if err := updated.Delete(); err != nil {
			return root, LifecycleEffects{}, err
		}
		return updated, LifecycleEffects{
			DisableNewDistribution:   true,
			DisablePendingActivation: true,
			CloseLinksAndCampaigns:   true,
			CloseEnrollments:         true,
			HideAllLearnerContent:    true,
			PreserveHistory:          true,
		}, nil
	default:
		return root, LifecycleEffects{}, ErrUnknownLifecycleTransition
	}
}
