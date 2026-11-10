package grpc

import (
	"context"
	"testing"

	companyv1 "github.com/sk1fy/team-os-backend/contracts/gen/go/company/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

func TestBrowserEntryDeniedEvenWithGatewayCredential(t *testing.T) {
	const credential = "test-gateway-company-credential-at-least-32-bytes"
	server := NewServer(nil, nil, credential)
	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs("x-teamos-service", "provisioning", "x-teamos-service-token", credential))
	bootstrap, err := server.BootstrapAmoCompany(ctx, &companyv1.BootstrapAmoCompanyRequest{
		AmoAccountId: "31355990", SelfUserId: "101", Users: []*companyv1.AmoCompanyBootstrapUser{{Id: "101", IsAdmin: true, IsActive: true}},
	})
	if status.Code(err) != codes.PermissionDenied || bootstrap != nil {
		t.Fatalf("bootstrap=%v error=%v", bootstrap, err)
	}
	login, err := server.AmoAdminSelfLogin(ctx, &companyv1.AmoAdminSelfLoginRequest{
		AmoAccountId: "31355990", SelfUserId: "102", Users: []*companyv1.AmoAdminUserAssertion{{Id: "102", IsAdmin: true, IsActive: true}},
	})
	if status.Code(err) != codes.PermissionDenied || login != nil {
		t.Fatalf("login=%v error=%v", login, err)
	}
}
