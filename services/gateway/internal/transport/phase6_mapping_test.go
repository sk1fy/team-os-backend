package transport

import (
	"testing"
	"time"

	"github.com/google/uuid"
	companyv1 "github.com/sk1fy/team-os-backend/contracts/gen/go/company/v1"
	filesv1 "github.com/sk1fy/team-os-backend/contracts/gen/go/files/v1"
	"github.com/sk1fy/team-os-backend/services/gateway/internal/api"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestScheduleTemplateRoundTrip(t *testing.T) {
	start := "2026-07-01"
	proto := &companyv1.ScheduleTemplate{Type: "cycle", On: uint32Pointer(2), Off: uint32Pointer(2), Start: "09:00", End: "21:00", CycleStart: &start}
	openAPI, err := scheduleTemplateFromProto(proto)
	if err != nil {
		t.Fatal(err)
	}
	converted, err := scheduleTemplateToProto(openAPI)
	if err != nil {
		t.Fatal(err)
	}
	if converted.GetType() != "cycle" || converted.GetOn() != 2 || converted.GetCycleStart() != start {
		t.Fatalf("unexpected template: %#v", converted)
	}
}

func TestDistributionGroupMapping(t *testing.T) {
	id, member := uuid.New(), uuid.New()
	now := time.Now().UTC()
	value, err := distributionGroupFromProto(&companyv1.DistributionGroup{Id: id.String(), Name: "Продажи", Active: true, Algorithm: companyv1.DistributionAlgorithm_DISTRIBUTION_ALGORITHM_ROUND_ROBIN, MemberIds: []string{member.String()}, Source: "Сайт", DealLimit: 10, UnclaimedMinutes: 15, CreatedAt: timestamppb.New(now)})
	if err != nil {
		t.Fatal(err)
	}
	if value.Id != id || value.Algorithm != api.RoundRobin || len(value.MemberIds) != 1 || value.MemberIds[0] != member {
		t.Fatalf("unexpected group: %#v", value)
	}
}

func TestUploadedFileMappingDoesNotExposeTenantMetadata(t *testing.T) {
	id := uuid.New()
	value, err := uploadedFileFromProto(&filesv1.FileMetadata{Id: id.String(), CompanyId: uuid.NewString(), UploadedBy: uuid.NewString(), Name: "report.pdf", ContentType: "application/pdf", Size: 42, Purpose: filesv1.FilePurpose_FILE_PURPOSE_ATTACHMENT, CreatedAt: timestamppb.Now()})
	if err != nil {
		t.Fatal(err)
	}
	if value.Id != id || value.Purpose != api.FilePurposeAttachment || value.Name != "report.pdf" {
		t.Fatalf("unexpected file: %#v", value)
	}
}

func uint32Pointer(value uint32) *uint32 { return &value }
