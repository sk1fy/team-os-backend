package kbreuse

import (
	"encoding/json"
	"errors"
	"testing"
)

func TestPartnerAccessMatrix(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		access     PartnerAccess
		partnerID  ID
		want       bool
		validateAs error
	}{
		{name: "zero value fails closed", access: PartnerAccess{}, partnerID: "partner-1"},
		{name: "none", access: PartnerAccess{Mode: PartnerAccessNone}, partnerID: "partner-1"},
		{name: "all", access: PartnerAccess{Mode: PartnerAccessAll}, partnerID: "partner-1", want: true},
		{name: "selected included", access: PartnerAccess{Mode: PartnerAccessSelected, PartnerIDs: []ID{"partner-1", "partner-2"}}, partnerID: "partner-1", want: true},
		{name: "selected excluded", access: PartnerAccess{Mode: PartnerAccessSelected, PartnerIDs: []ID{"partner-2"}}, partnerID: "partner-1"},
		{name: "empty selected", access: PartnerAccess{Mode: PartnerAccessSelected}, partnerID: "partner-1", validateAs: ErrSelectedPartnersRequired},
		{name: "list with all", access: PartnerAccess{Mode: PartnerAccessAll, PartnerIDs: []ID{"partner-1"}}, partnerID: "partner-1", validateAs: ErrPartnerListForbidden},
		{name: "duplicate selected", access: PartnerAccess{Mode: PartnerAccessSelected, PartnerIDs: []ID{"partner-1", "partner-1"}}, partnerID: "partner-1", validateAs: ErrDuplicatePartnerID},
		{name: "empty caller", access: PartnerAccess{Mode: PartnerAccessAll}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if err := test.access.Validate(); !errors.Is(err, test.validateAs) {
				t.Fatalf("Validate() = %v, want %v", err, test.validateAs)
			}
			if got := test.access.Allows(test.partnerID); got != test.want {
				t.Fatalf("Allows() = %v, want %v", got, test.want)
			}
		})
	}
}

func TestArticleReuseAuthorizationOrder(t *testing.T) {
	t.Parallel()

	base := validPolicy()
	tests := []struct {
		name      string
		companyID ID
		partnerID ID
		mutate    func(*ArticlePolicy)
		wantErr   error
	}{
		{name: "allowed", companyID: "company-1", partnerID: "partner-1"},
		{name: "cross tenant", companyID: "company-2", partnerID: "partner-1", wantErr: ErrCompanyMismatch},
		{name: "draft article", companyID: "company-1", partnerID: "partner-1", mutate: func(policy *ArticlePolicy) { policy.Published = false }, wantErr: ErrPublishedArticleRequired},
		{name: "read denied", companyID: "company-1", partnerID: "partner-2", wantErr: ErrPartnerReadDenied},
		{name: "reuse default denied", companyID: "company-1", partnerID: "partner-1", mutate: func(policy *ArticlePolicy) { policy.Reuse = "" }, wantErr: ErrReuseNotAllowed},
		{name: "reuse explicitly denied", companyID: "company-1", partnerID: "partner-1", mutate: func(policy *ArticlePolicy) { policy.Reuse = ReuseNotAllowed }, wantErr: ErrReuseNotAllowed},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			policy := base
			if test.mutate != nil {
				test.mutate(&policy)
			}
			if err := policy.AuthorizeSnapshotCopy(test.companyID, test.partnerID); !errors.Is(err, test.wantErr) {
				t.Fatalf("AuthorizeSnapshotCopy() = %v, want %v", err, test.wantErr)
			}
		})
	}
}

func TestRevocationBlocksNewCopyButNotExistingSnapshot(t *testing.T) {
	t.Parallel()

	params := validCopyParams()
	copy, err := CopyForCourse(params)
	if err != nil {
		t.Fatal(err)
	}
	before := copy.Snapshot()
	if before.SourceArticleID != "article-1" || before.SourceArticleVersion != 3 ||
		string(before.Content) != `{"type":"doc"}` || len(before.FileIDs) != 2 || before.FileIDs[0] != before.FileIDs[1] ||
		before.FileIDs[0] == params.Source.FileIDs[0] {
		t.Fatalf("snapshot = %#v", before)
	}

	// Revoke permission and mutate the source after the copy.
	params.Policy.Reuse = ReuseNotAllowed
	params.Source.Title = "Изменённая статья"
	params.Source.Content[0] = 'X'
	if _, err := CopyForCourse(params); !errors.Is(err, ErrReuseNotAllowed) {
		t.Fatalf("copy after revoke error = %v", err)
	}
	after := copy.Snapshot()
	if after.Title != before.Title || string(after.Content) != string(before.Content) {
		t.Fatalf("existing snapshot changed after revoke: %#v", after)
	}

	// A caller cannot mutate the aggregate through its observed snapshot.
	after.Content[0] = 'Y'
	after.FileIDs[0] = "attacker"
	defensive := copy.Snapshot()
	if defensive.Content[0] == 'Y' || defensive.FileIDs[0] == "attacker" {
		t.Fatal("snapshot boundary is not defensive")
	}
}

func TestAttachmentsCloneOnlyAfterAuthorization(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		mutate    func(*CopyParams)
		wantErr   error
		wantCalls int
	}{
		{name: "allowed clones unique source once", wantCalls: 1},
		{name: "read denied clones nothing", mutate: func(params *CopyParams) { params.PartnerID = "partner-2" }, wantErr: ErrPartnerReadDenied},
		{name: "reuse denied clones nothing", mutate: func(params *CopyParams) { params.Policy.Reuse = ReuseNotAllowed }, wantErr: ErrReuseNotAllowed},
		{name: "mapper required", mutate: func(params *CopyParams) { params.MapFileID = nil }, wantErr: ErrFileMapperRequired},
		{name: "content validator required", mutate: func(params *CopyParams) { params.ValidateContent = nil }, wantErr: ErrContentValidatorRequired},
		{name: "source ID reuse rejected", mutate: func(params *CopyParams) {
			params.MapFileID = func(sourceID ID) ID { return sourceID }
		}, wantErr: ErrIndependentFileIDRequired, wantCalls: 0},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			params := validCopyParams()
			calls := 0
			originalMapper := params.MapFileID
			params.MapFileID = func(sourceID ID) ID {
				calls++
				return originalMapper(sourceID)
			}
			if test.mutate != nil {
				test.mutate(&params)
			}
			_, err := CopyForCourse(params)
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("error = %v, want %v", err, test.wantErr)
			}
			if test.name != "mapper required" && test.name != "source ID reuse rejected" && calls != test.wantCalls {
				t.Fatalf("clone calls = %d, want %d", calls, test.wantCalls)
			}
		})
	}
}

func validPolicy() ArticlePolicy {
	return ArticlePolicy{
		CompanyID: "company-1", ArticleID: "article-1", Version: 3, Published: true,
		Access: PartnerAccess{Mode: PartnerAccessSelected, PartnerIDs: []ID{"partner-1"}},
		Reuse:  ReuseCopyAllowed,
	}
}

func validCopyParams() CopyParams {
	return CopyParams{
		Policy: validPolicy(), RequestCompanyID: "company-1", PartnerID: "partner-1",
		Source: ArticleVersionSnapshot{
			CompanyID: "company-1", ArticleID: "article-1", Version: 3,
			Title: "Регламент", Content: json.RawMessage(`{"type":"doc"}`), FileIDs: []ID{"file-1", "file-1"},
		},
		ValidateContent: func(value json.RawMessage) error {
			if !json.Valid(value) {
				return ErrArticleContentRequired
			}
			return nil
		},
		MapFileID: func(sourceID ID) ID { return "copy-" + sourceID },
	}
}
