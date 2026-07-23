package kbreuse

import (
	"encoding/json"
	"errors"
	"strings"
)

var (
	ErrArticleTitleRequired      = errors.New("Для снимка статьи требуется название")
	ErrArticleContentRequired    = errors.New("Для снимка статьи требуется TipTap-документ")
	ErrContentValidatorRequired  = errors.New("Не настроена проверка TipTap JSON статьи")
	ErrInvalidArticleContent     = errors.New("Некорректный TipTap-документ статьи")
	ErrSourcePolicyMismatch      = errors.New("Снимок и правило доступа относятся к разным версиям статьи")
	ErrFileMapperRequired        = errors.New("Для вложений статьи требуется клонирование файлов")
	ErrSourceFileIDRequired      = errors.New("У вложения статьи отсутствует идентификатор")
	ErrIndependentFileIDRequired = errors.New("Для вложения снимка требуется новый идентификатор файла")
	ErrDuplicateDestinationFile  = errors.New("Клонированный идентификатор файла повторяется")
)

// ArticleVersionSnapshot is immutable article data returned by KB only after
// it has resolved the requested published version.
type ArticleVersionSnapshot struct {
	CompanyID ID
	ArticleID ID
	Version   int
	Title     string
	Content   json.RawMessage
	FileIDs   []ID
}

// FileIDMapper represents the Files clone boundary. Application resolves the
// actual idempotent clone saga before persisting the Academy snapshot.
type FileIDMapper func(sourceFileID ID) ID

// ContentValidator is the pure boundary for the shared TipTap validator.
type ContentValidator func(json.RawMessage) error

// CopyParams contains the KB policy and snapshot observed in one request.
type CopyParams struct {
	Policy           ArticlePolicy
	Source           ArticleVersionSnapshot
	RequestCompanyID ID
	PartnerID        ID
	ValidateContent  ContentValidator
	MapFileID        FileIDMapper
}

// CourseArticleSnapshot is provenance plus detached content stored inside a
// course-version lesson. It carries no live KB link or policy reference.
type CourseArticleSnapshot struct {
	SourceArticleID      ID
	SourceArticleVersion int
	Title                string
	Content              json.RawMessage
	FileIDs              []ID
}

// CopiedArticle owns one detached value so callers cannot mutate it through a
// returned slice. Later policy revocation cannot reach this aggregate.
type CopiedArticle struct {
	snapshot CourseArticleSnapshot
}

// CopyForCourse authorizes and deeply clones one article version. Attachments
// are cloned only after both read and reuse permission have succeeded.
func CopyForCourse(params CopyParams) (*CopiedArticle, error) {
	if err := params.Policy.AuthorizeSnapshotCopy(params.RequestCompanyID, params.PartnerID); err != nil {
		return nil, err
	}
	if err := validateSource(params.Source); err != nil {
		return nil, err
	}
	if params.Source.CompanyID != params.Policy.CompanyID ||
		params.Source.ArticleID != params.Policy.ArticleID ||
		params.Source.Version != params.Policy.Version {
		return nil, ErrSourcePolicyMismatch
	}
	if params.ValidateContent == nil {
		return nil, ErrContentValidatorRequired
	}
	if err := params.ValidateContent(params.Source.Content); err != nil {
		return nil, errors.Join(ErrInvalidArticleContent, err)
	}
	if len(params.Source.FileIDs) > 0 && params.MapFileID == nil {
		return nil, ErrFileMapperRequired
	}

	files := make([]ID, len(params.Source.FileIDs))
	seen := make(map[ID]ID, len(params.Source.FileIDs))
	used := make(map[ID]ID, len(params.Source.FileIDs))
	for index, sourceID := range params.Source.FileIDs {
		if sourceID == "" {
			return nil, ErrSourceFileIDRequired
		}
		if mapped, exists := seen[sourceID]; exists {
			files[index] = mapped
			continue
		}
		mapped := params.MapFileID(sourceID)
		if mapped == "" || mapped == sourceID {
			return nil, ErrIndependentFileIDRequired
		}
		if previousSource, exists := used[mapped]; exists && previousSource != sourceID {
			return nil, ErrDuplicateDestinationFile
		}
		seen[sourceID] = mapped
		used[mapped] = sourceID
		files[index] = mapped
	}

	return &CopiedArticle{snapshot: CourseArticleSnapshot{
		SourceArticleID:      params.Source.ArticleID,
		SourceArticleVersion: params.Source.Version,
		Title:                params.Source.Title,
		Content:              append(json.RawMessage(nil), params.Source.Content...),
		FileIDs:              files,
	}}, nil
}

// Snapshot returns a defensive copy detached from both KB and the caller.
func (c *CopiedArticle) Snapshot() CourseArticleSnapshot {
	if c == nil {
		return CourseArticleSnapshot{}
	}
	result := c.snapshot
	result.Content = append(json.RawMessage(nil), c.snapshot.Content...)
	result.FileIDs = append([]ID(nil), c.snapshot.FileIDs...)
	return result
}

func validateSource(source ArticleVersionSnapshot) error {
	switch {
	case source.CompanyID == "":
		return ErrCompanyIDRequired
	case source.ArticleID == "":
		return ErrArticleIDRequired
	case source.Version < 1:
		return ErrArticleVersionInvalid
	case strings.TrimSpace(source.Title) == "":
		return ErrArticleTitleRequired
	case len(source.Content) == 0:
		return ErrArticleContentRequired
	}
	return nil
}
