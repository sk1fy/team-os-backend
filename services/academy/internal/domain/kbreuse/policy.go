// Package kbreuse contains the least-privilege policy and snapshot-copy
// invariants for reusing Knowledge Base articles in partner courses. It has no
// dependency on KB persistence or transport.
package kbreuse

import (
	"errors"
	"slices"
)

// ID is an opaque company, partner, article, or file identifier.
type ID string

// PartnerAccessMode controls read visibility. The zero value is intentionally
// treated as none, so omitted configuration fails closed.
type PartnerAccessMode string

const (
	PartnerAccessNone     PartnerAccessMode = "none"
	PartnerAccessAll      PartnerAccessMode = "all_partners"
	PartnerAccessSelected PartnerAccessMode = "selected_partners"
)

// ReusePolicy controls whether a readable published article may be copied.
// The zero value is intentionally equivalent to not_allowed.
type ReusePolicy string

const (
	ReuseNotAllowed  ReusePolicy = "not_allowed"
	ReuseCopyAllowed ReusePolicy = "copy_allowed"
)

var (
	ErrUnknownPartnerAccessMode = errors.New("Неизвестный режим доступа партнёров")
	ErrSelectedPartnersRequired = errors.New("Для выборочного доступа требуется хотя бы один партнёр")
	ErrPartnerListForbidden     = errors.New("Список партнёров допустим только для выборочного доступа")
	ErrPartnerIDRequired        = errors.New("Для доступа требуется партнёр")
	ErrDuplicatePartnerID       = errors.New("Партнёр повторяется в настройках доступа")
	ErrUnknownReusePolicy       = errors.New("Неизвестное правило повторного использования статьи")
	ErrCompanyIDRequired        = errors.New("Для статьи требуется компания")
	ErrArticleIDRequired        = errors.New("Для статьи требуется идентификатор")
	ErrArticleVersionInvalid    = errors.New("Номер версии статьи должен быть больше нуля")
	ErrCompanyMismatch          = errors.New("Статья относится к другой компании")
	ErrPartnerReadDenied        = errors.New("Партнёру недоступна эта статья базы знаний")
	ErrReuseNotAllowed          = errors.New("Статью нельзя использовать в партнёрском курсе")
	ErrPublishedArticleRequired = errors.New("В курс можно копировать только опубликованную версию статьи")
)

// PartnerAccess is a defensive value object used by tree, list, search,
// direct-ID and snapshot-copy authorization.
type PartnerAccess struct {
	Mode       PartnerAccessMode
	PartnerIDs []ID
}

// NewPartnerAccess validates and defensively copies a visibility setting.
func NewPartnerAccess(mode PartnerAccessMode, partnerIDs []ID) (PartnerAccess, error) {
	result := PartnerAccess{Mode: mode, PartnerIDs: append([]ID(nil), partnerIDs...)}
	if err := result.Validate(); err != nil {
		return PartnerAccess{}, err
	}
	return result, nil
}

// Validate checks persisted settings. Empty mode is accepted as the safe
// default none.
func (p PartnerAccess) Validate() error {
	mode := p.effectiveMode()
	switch mode {
	case PartnerAccessNone, PartnerAccessAll:
		if len(p.PartnerIDs) != 0 {
			return ErrPartnerListForbidden
		}
	case PartnerAccessSelected:
		if len(p.PartnerIDs) == 0 {
			return ErrSelectedPartnersRequired
		}
		seen := make(map[ID]struct{}, len(p.PartnerIDs))
		for _, partnerID := range p.PartnerIDs {
			if partnerID == "" {
				return ErrPartnerIDRequired
			}
			if _, exists := seen[partnerID]; exists {
				return ErrDuplicatePartnerID
			}
			seen[partnerID] = struct{}{}
		}
	default:
		return ErrUnknownPartnerAccessMode
	}
	return nil
}

// Allows applies the same fail-closed visibility rule at every KB boundary.
func (p PartnerAccess) Allows(partnerID ID) bool {
	if partnerID == "" || p.Validate() != nil {
		return false
	}
	switch p.effectiveMode() {
	case PartnerAccessAll:
		return true
	case PartnerAccessSelected:
		return slices.Contains(p.PartnerIDs, partnerID)
	default:
		return false
	}
}

// Snapshot returns a defensive value for persistence or transport mapping.
func (p PartnerAccess) Snapshot() PartnerAccess {
	return PartnerAccess{Mode: p.effectiveMode(), PartnerIDs: append([]ID(nil), p.PartnerIDs...)}
}

func (p PartnerAccess) effectiveMode() PartnerAccessMode {
	if p.Mode == "" {
		return PartnerAccessNone
	}
	return p.Mode
}

// ArticlePolicy is the complete pre-resolved authorization context supplied
// by KB. Academy must never read the KB database directly.
type ArticlePolicy struct {
	CompanyID ID
	ArticleID ID
	Version   int
	Published bool
	Access    PartnerAccess
	Reuse     ReusePolicy
}

// Validate checks a policy independently of a caller.
func (p ArticlePolicy) Validate() error {
	switch {
	case p.CompanyID == "":
		return ErrCompanyIDRequired
	case p.ArticleID == "":
		return ErrArticleIDRequired
	case p.Version < 1:
		return ErrArticleVersionInvalid
	}
	if err := p.Access.Validate(); err != nil {
		return err
	}
	if p.Reuse != "" && p.Reuse != ReuseNotAllowed && p.Reuse != ReuseCopyAllowed {
		return ErrUnknownReusePolicy
	}
	return nil
}

// CanRead checks tenant and partner visibility without granting reuse.
func (p ArticlePolicy) CanRead(companyID, partnerID ID) bool {
	return p.Validate() == nil && p.Published && companyID != "" && companyID == p.CompanyID && p.Access.Allows(partnerID)
}

// AuthorizeSnapshotCopy enforces tenant, published state, read visibility and
// explicit reuse in that order. Revoking access or reuse blocks future calls.
func (p ArticlePolicy) AuthorizeSnapshotCopy(companyID, partnerID ID) error {
	if err := p.Validate(); err != nil {
		return err
	}
	if companyID == "" || companyID != p.CompanyID {
		return ErrCompanyMismatch
	}
	if !p.Published {
		return ErrPublishedArticleRequired
	}
	if !p.Access.Allows(partnerID) {
		return ErrPartnerReadDenied
	}
	if p.Reuse != ReuseCopyAllowed {
		return ErrReuseNotAllowed
	}
	return nil
}
