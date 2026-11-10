package grpc

import (
	"context"
	"testing"

	companyv1 "github.com/sk1fy/team-os-backend/contracts/gen/go/company/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

func TestAmoCompanyEntryProvisioningRPCRequiresInternalServiceCredential(t *testing.T) {
	server := NewServer(nil, nil, "backend-private-service-credential-32-bytes")
	for _, ctx := range []context.Context{
		context.Background(),
		metadata.NewIncomingContext(context.Background(), metadata.Pairs("x-teamos-service", "provisioning", "x-teamos-service-token", "wrong")),
		metadata.NewIncomingContext(context.Background(), metadata.Pairs("x-teamos-service", "provisioning", "x-teamos-service-token", "backend-private-service-credential-32-bytes", "x-teamos-service-token", "wrong")),
	} {
		_, err := server.GetAmoEntryCompanyStatus(ctx, &companyv1.GetAmoEntryCompanyStatusRequest{AccountId: "123"})
		if status.Code(err) != codes.Unauthenticated {
			t.Fatalf("status authorization %v", err)
		}
		_, err = server.ProvisionAmoWidgetEntry(ctx, &companyv1.ProvisionAmoWidgetEntryRequest{AccountId: "123"})
		if status.Code(err) != codes.Unauthenticated {
			t.Fatalf("provisioning authorization %v", err)
		}
	}
}
