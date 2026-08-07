//go:build integration

package application

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"fmt"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	sharedauth "github.com/sk1fy/team-os-backend/pkg/auth"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

func TestProvisioningLifecycle(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	pool := provisioningTestPool(t, ctx)
	clock := &provisioningTestClock{value: time.Date(2026, time.August, 6, 9, 0, 0, 0, time.UTC)}
	service := provisioningTestService(t, pool, clock)

	for _, initiatorRole := range []string{"owner", "admin"} {
		t.Run(initiatorRole+" activates first", func(t *testing.T) {
			input := provisioningInput("happy-"+initiatorRole, initiatorRole)
			provisioned, err := service.ProvisionCompany(ctx, input)
			if err != nil {
				t.Fatal(err)
			}
			if !provisioned.Created || provisioned.CompanyStatus != "onboarding" || provisioned.BootstrapToken == "" ||
				provisioned.InitiatorRole != initiatorRole {
				t.Fatalf("provisioned = %+v", provisioned)
			}
			var activationEvents int
			if err = pool.QueryRow(ctx, `SELECT count(*) FROM outbox WHERE company_id=$1 AND subject=$2`,
				provisioned.CompanyID, "teamos.org.user.activation_created.v1").Scan(&activationEvents); err != nil {
				t.Fatal(err)
			}
			if activationEvents != 2 {
				t.Fatalf("activation_created count=%d, want 2", activationEvents)
			}
			secondExternalUserID := input.Admin.ExternalUserID
			if initiatorRole == "admin" {
				secondExternalUserID = input.Owner.ExternalUserID
			}
			secondContinuation, err := service.IssueSsoToken(ctx, IssueSsoTokenInput{
				Provider: input.Provider, ExternalAccountID: input.ExternalAccountID,
				ExternalUserID: secondExternalUserID,
			})
			if err != nil || secondContinuation.Kind != "onboarding" || secondContinuation.Token == "" {
				t.Fatalf("second participant continuation=%+v err=%v", secondContinuation, err)
			}
			if _, err = service.GetBootstrapActivation(ctx, secondContinuation.Token); err != nil {
				t.Fatalf("second participant activation: %v", err)
			}

			first, err := service.CompleteBootstrapActivation(ctx, CompleteBootstrapInput{
				Token: provisioned.BootstrapToken, Password: "first-password",
			}, SessionMeta{})
			if err != nil {
				t.Fatal(err)
			}
			if first.Onboarding.Completed || first.Onboarding.CompanyStatus != "onboarding" ||
				first.Onboarding.PendingUser == nil || first.Onboarding.ActivationToken == nil ||
				first.Session.AccessToken == "" || first.Session.RefreshToken == "" {
				t.Fatalf("first activation = %+v", first)
			}
			wantPendingRole := "admin"
			if initiatorRole == "admin" {
				wantPendingRole = "owner"
			}
			if first.Onboarding.PendingUser.Role != wantPendingRole || first.Onboarding.PendingUser.Status != "invited" {
				t.Fatalf("pending user = %+v", first.Onboarding.PendingUser)
			}
			if _, err = service.InviteUser(ctx, Actor{
				UserID: first.Session.User.ID, CompanyID: first.Session.User.CompanyID, Role: first.Session.User.Role,
			}, InviteUserInput{Role: "owner"}); !isApplicationKind(err, ErrorValidation) {
				t.Fatalf("owner invite error=%v", err)
			}

			assertPendingBootstrapGuards(t, ctx, service, first.Session.User, *first.Onboarding.PendingUser)

			actor := Actor{
				UserID: first.Session.User.ID, CompanyID: first.Session.User.CompanyID,
				Role: first.Session.User.Role,
			}
			statusBeforeReissue, err := service.GetOnboardingStatus(ctx, actor)
			if err != nil || statusBeforeReissue.PendingUser == nil || statusBeforeReissue.ActivationToken != nil {
				t.Fatalf("onboarding status before reissue=%+v err=%v", statusBeforeReissue, err)
			}
			oldSecondToken := *first.Onboarding.ActivationToken
			reissued, err := service.ReissueOnboardingActivation(ctx, actor)
			if err != nil || reissued.ActivationToken == nil || *reissued.ActivationToken == oldSecondToken {
				t.Fatalf("reissued activation=%+v err=%v", reissued, err)
			}
			_, err = service.GetBootstrapActivation(ctx, oldSecondToken)
			assertApplicationCode(t, err, ErrorCodeBootstrapInvalid)

			secondToken := *reissued.ActivationToken
			second, err := service.CompleteBootstrapActivation(ctx, CompleteBootstrapInput{
				Token: secondToken, Password: "second-password",
			}, SessionMeta{})
			if err != nil {
				t.Fatal(err)
			}
			if !second.Onboarding.Completed || second.Onboarding.CompanyStatus != "active" ||
				second.Onboarding.PendingUser != nil || second.Onboarding.ActivationToken != nil {
				t.Fatalf("second activation = %+v", second.Onboarding)
			}
			_, err = service.CompleteBootstrapActivation(ctx, CompleteBootstrapInput{
				Token: secondToken, Password: "second-password",
			}, SessionMeta{})
			assertApplicationCode(t, err, ErrorCodeBootstrapConsumed)

			var status string
			var credentials int
			if err = pool.QueryRow(ctx, `SELECT status FROM companies WHERE id=$1`, provisioned.CompanyID).Scan(&status); err != nil {
				t.Fatal(err)
			}
			if err = pool.QueryRow(ctx, `SELECT count(*) FROM credentials WHERE company_id=$1`, provisioned.CompanyID).Scan(&credentials); err != nil {
				t.Fatal(err)
			}
			if status != "active" || credentials != 2 {
				t.Fatalf("status=%q credentials=%d", status, credentials)
			}
			assertOutboxSubjects(t, ctx, pool, provisioned.CompanyID, map[string]int{
				"teamos.company.company.provisioned.v1":  1,
				"teamos.company.company.created.v1":      1,
				"teamos.company.onboarding.completed.v1": 1,
				"teamos.org.user.activated.v1":           2,
				"teamos.org.user.updated.v1":             2,
			})
		})
	}

	t.Run("replay, conflict, and concurrent provisioning", func(t *testing.T) {
		input := provisioningInput("replay", "owner")
		first, err := service.ProvisionCompany(ctx, input)
		if err != nil {
			t.Fatal(err)
		}
		replayed, err := service.ProvisionCompany(ctx, input)
		if err != nil {
			t.Fatal(err)
		}
		if replayed.Created || replayed.CompanyID != first.CompanyID || replayed.BootstrapToken == first.BootstrapToken {
			t.Fatalf("replayed=%+v first=%+v", replayed, first)
		}
		_, err = service.GetBootstrapActivation(ctx, first.BootstrapToken)
		assertApplicationCode(t, err, ErrorCodeBootstrapInvalid)
		conflicting := input
		conflicting.CompanyName = "Другая компания"
		_, err = service.ProvisionCompany(ctx, conflicting)
		assertApplicationCode(t, err, ErrorCodeProvisioningConflict)

		concurrent := provisioningInput("concurrent", "admin")
		var wait sync.WaitGroup
		wait.Add(2)
		results := make([]ProvisionCompanyResult, 2)
		errorsFound := make([]error, 2)
		for index := range results {
			go func(index int) {
				defer wait.Done()
				results[index], errorsFound[index] = service.ProvisionCompany(ctx, concurrent)
			}(index)
		}
		wait.Wait()
		for _, callErr := range errorsFound {
			if callErr != nil {
				t.Fatalf("concurrent provisioning: %v", callErr)
			}
		}
		if results[0].CompanyID != results[1].CompanyID || results[0].Created == results[1].Created {
			t.Fatalf("concurrent results = %+v / %+v", results[0], results[1])
		}
		var count int
		if err = pool.QueryRow(ctx, `SELECT count(*) FROM company_integrations WHERE provider=$1 AND external_account_id=$2`,
			concurrent.Provider, concurrent.ExternalAccountID).Scan(&count); err != nil || count != 1 {
			t.Fatalf("integration count=%d err=%v", count, err)
		}
	})

	t.Run("mid-transaction failure rolls back the whole aggregate", func(t *testing.T) {
		existing := provisioningInput("rollback-existing", "owner")
		if _, err := service.ProvisionCompany(ctx, existing); err != nil {
			t.Fatal(err)
		}
		failing := provisioningInput("rollback-target", "admin")
		failing.Owner.Email = existing.Owner.Email
		if _, err := service.ProvisionCompany(ctx, failing); err == nil {
			t.Fatal("provisioning with duplicate global email succeeded")
		}
		var companies, integrations, users int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM companies WHERE name=$1`, failing.CompanyName).Scan(&companies); err != nil {
			t.Fatal(err)
		}
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM company_integrations WHERE provider=$1 AND external_account_id=$2`,
			failing.Provider, failing.ExternalAccountID).Scan(&integrations); err != nil {
			t.Fatal(err)
		}
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM user_external_identities WHERE provider=$1 AND external_account_id=$2`,
			failing.Provider, failing.ExternalAccountID).Scan(&users); err != nil {
			t.Fatal(err)
		}
		if companies != 0 || integrations != 0 || users != 0 {
			t.Fatalf("partial aggregate remains: companies=%d integrations=%d identities=%d", companies, integrations, users)
		}
	})

	t.Run("expiry and explicit rotation", func(t *testing.T) {
		input := provisioningInput("expiry", "owner")
		provisioned, err := service.ProvisionCompany(ctx, input)
		if err != nil {
			t.Fatal(err)
		}
		issued, err := service.IssueSsoToken(ctx, IssueSsoTokenInput{
			Provider: input.Provider, ExternalAccountID: input.ExternalAccountID,
			ExternalUserID: input.Owner.ExternalUserID,
		})
		if err != nil || issued.Kind != "onboarding" {
			t.Fatalf("first continuation=%+v err=%v", issued, err)
		}
		rotated, err := service.IssueSsoToken(ctx, IssueSsoTokenInput{
			Provider: input.Provider, ExternalAccountID: input.ExternalAccountID,
			ExternalUserID: input.Owner.ExternalUserID,
		})
		if err != nil || rotated.Kind != "onboarding" || rotated.Token == issued.Token {
			t.Fatalf("rotated continuation=%+v err=%v", rotated, err)
		}
		for _, invalidated := range []string{provisioned.BootstrapToken, issued.Token} {
			_, err = service.GetBootstrapActivation(ctx, invalidated)
			assertApplicationCode(t, err, ErrorCodeBootstrapInvalid)
		}
		if _, err = service.GetBootstrapActivation(ctx, rotated.Token); err != nil {
			t.Fatalf("latest activation: %v", err)
		}
		clock.Add(25 * time.Hour)
		_, err = service.GetBootstrapActivation(ctx, rotated.Token)
		assertApplicationCode(t, err, ErrorCodeBootstrapExpired)
		clock.Add(-25 * time.Hour)
	})

	t.Run("two concurrent bootstrap redemptions", func(t *testing.T) {
		input := provisioningInput("concurrent-bootstrap", "owner")
		provisioned, err := service.ProvisionCompany(ctx, input)
		if err != nil {
			t.Fatal(err)
		}
		results := make([]CompleteBootstrapResult, 2)
		errorsFound := make([]error, 2)
		var wait sync.WaitGroup
		wait.Add(2)
		for index := range results {
			go func(index int) {
				defer wait.Done()
				results[index], errorsFound[index] = service.CompleteBootstrapActivation(
					ctx,
					CompleteBootstrapInput{Token: provisioned.BootstrapToken, Password: "concurrent-password"},
					SessionMeta{},
				)
			}(index)
		}
		wait.Wait()
		assertOneSuccessfulRedemption(t, errorsFound, ErrorCodeBootstrapConsumed)
	})

	t.Run("bootstrap revalidates user status and role", func(t *testing.T) {
		input := provisioningInput("bootstrap-revalidation", "admin")
		provisioned, err := service.ProvisionCompany(ctx, input)
		if err != nil {
			t.Fatal(err)
		}
		var companyID, userID uuid.UUID
		if err = pool.QueryRow(ctx, `
			SELECT identity.company_id, identity.user_id
			FROM user_external_identities AS identity
			WHERE identity.provider=$1 AND identity.external_account_id=$2
			  AND identity.external_user_id=$3
		`, input.Provider, input.ExternalAccountID, input.InitiatorExternalUserID).Scan(&companyID, &userID); err != nil {
			t.Fatal(err)
		}
		if _, err = pool.Exec(ctx, `UPDATE users SET status='deactivated' WHERE company_id=$1 AND id=$2`, companyID, userID); err != nil {
			t.Fatal(err)
		}
		_, err = service.CompleteBootstrapActivation(ctx, CompleteBootstrapInput{
			Token: provisioned.BootstrapToken, Password: "bootstrap-password",
		}, SessionMeta{})
		assertApplicationCode(t, err, ErrorCodeExternalUserDeactivated)
		if _, err = pool.Exec(ctx, `UPDATE users SET status='invited', role='employee' WHERE company_id=$1 AND id=$2`, companyID, userID); err != nil {
			t.Fatal(err)
		}
		_, err = service.CompleteBootstrapActivation(ctx, CompleteBootstrapInput{
			Token: provisioned.BootstrapToken, Password: "bootstrap-password",
		}, SessionMeta{})
		assertApplicationCode(t, err, ErrorCodeBootstrapInvalid)
	})

	t.Run("SSO expiry, revocation, and two concurrent redemptions", func(t *testing.T) {
		input := provisioningInput("sso-token-state", "owner")
		companyID := activateProvisioningCompany(t, ctx, service, input)
		identity := IssueSsoTokenInput{
			Provider: input.Provider, ExternalAccountID: input.ExternalAccountID,
			ExternalUserID: input.Owner.ExternalUserID,
		}

		expiring, err := service.IssueSsoToken(ctx, identity)
		if err != nil {
			t.Fatal(err)
		}
		clock.Add(2 * time.Minute)
		_, err = service.ExchangeSsoToken(ctx, expiring.Token, SessionMeta{})
		assertApplicationCode(t, err, ErrorCodeSSOExpired)
		clock.Add(-2 * time.Minute)

		revoked, err := service.IssueSsoToken(ctx, identity)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = pool.Exec(ctx, `
			UPDATE sso_tokens SET revoked_at=$2
			WHERE company_id=$1 AND consumed_at IS NULL AND revoked_at IS NULL
		`, companyID, clock.Now()); err != nil {
			t.Fatal(err)
		}
		_, err = service.ExchangeSsoToken(ctx, revoked.Token, SessionMeta{})
		assertApplicationCode(t, err, ErrorCodeSSOInvalid)

		concurrent, err := service.IssueSsoToken(ctx, identity)
		if err != nil {
			t.Fatal(err)
		}
		errorsFound := make([]error, 2)
		var wait sync.WaitGroup
		wait.Add(2)
		for index := range errorsFound {
			go func(index int) {
				defer wait.Done()
				_, errorsFound[index] = service.ExchangeSsoToken(ctx, concurrent.Token, SessionMeta{})
			}(index)
		}
		wait.Wait()
		assertOneSuccessfulRedemption(t, errorsFound, ErrorCodeSSOConsumed)
	})

	t.Run("SSO rotation, freeze, deactivation, and tenant isolation", func(t *testing.T) {
		input := provisioningInput("sso", "owner")
		companyID := activateProvisioningCompany(t, ctx, service, input)
		identity := IssueSsoTokenInput{
			Provider: input.Provider, ExternalAccountID: input.ExternalAccountID,
			ExternalUserID: input.Owner.ExternalUserID,
		}
		first, err := service.IssueSsoToken(ctx, identity)
		if err != nil || first.Kind != "sso" {
			t.Fatalf("first SSO=%+v err=%v", first, err)
		}
		second, err := service.IssueSsoToken(ctx, identity)
		if err != nil || second.Token == first.Token {
			t.Fatalf("second SSO=%+v err=%v", second, err)
		}
		_, err = service.ExchangeSsoToken(ctx, first.Token, SessionMeta{})
		assertApplicationCode(t, err, ErrorCodeSSOInvalid)
		if _, err = service.ExchangeSsoToken(ctx, second.Token, SessionMeta{}); err != nil {
			t.Fatalf("exchange SSO: %v", err)
		}
		_, err = service.ExchangeSsoToken(ctx, second.Token, SessionMeta{})
		assertApplicationCode(t, err, ErrorCodeSSOConsumed)

		frozenToken, err := service.IssueSsoToken(ctx, identity)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = pool.Exec(ctx, `UPDATE company_integrations SET status='frozen', frozen_at=now() WHERE company_id=$1`, companyID); err != nil {
			t.Fatal(err)
		}
		_, err = service.ExchangeSsoToken(ctx, frozenToken.Token, SessionMeta{})
		assertApplicationCode(t, err, ErrorCodeIntegrationFrozen)
		_, err = service.IssueSsoToken(ctx, identity)
		assertApplicationCode(t, err, ErrorCodeIntegrationFrozen)
		if _, err = pool.Exec(ctx, `UPDATE company_integrations SET status='active', frozen_at=NULL WHERE company_id=$1`, companyID); err != nil {
			t.Fatal(err)
		}
		if _, err = pool.Exec(ctx, `UPDATE companies SET status='frozen' WHERE id=$1`, companyID); err != nil {
			t.Fatal(err)
		}
		_, err = service.ExchangeSsoToken(ctx, frozenToken.Token, SessionMeta{})
		assertApplicationCode(t, err, ErrorCodeIntegrationFrozen)
		if _, err = pool.Exec(ctx, `UPDATE companies SET status='active' WHERE id=$1`, companyID); err != nil {
			t.Fatal(err)
		}
		if _, err = pool.Exec(ctx, `UPDATE company_integrations SET status='suspended' WHERE company_id=$1`, companyID); err != nil {
			t.Fatal(err)
		}
		_, err = service.ExchangeSsoToken(ctx, frozenToken.Token, SessionMeta{})
		assertApplicationCode(t, err, ErrorCodeIntegrationFrozen)
		if _, err = pool.Exec(ctx, `UPDATE company_integrations SET status='active' WHERE company_id=$1`, companyID); err != nil {
			t.Fatal(err)
		}
		if _, err = pool.Exec(ctx, `UPDATE users SET status='deactivated' WHERE company_id=$1 AND id=(
			SELECT user_id FROM user_external_identities WHERE company_id=$1 AND external_user_id=$2
		)`, companyID, identity.ExternalUserID); err != nil {
			t.Fatal(err)
		}
		_, err = service.ExchangeSsoToken(ctx, frozenToken.Token, SessionMeta{})
		assertApplicationCode(t, err, ErrorCodeExternalUserDeactivated)
		if _, err = pool.Exec(ctx, `UPDATE users SET status='active' WHERE company_id=$1 AND id=(
			SELECT user_id FROM user_external_identities WHERE company_id=$1 AND external_user_id=$2
		)`, companyID, identity.ExternalUserID); err != nil {
			t.Fatal(err)
		}
		if _, err = pool.Exec(ctx, `UPDATE user_external_identities SET status='deactivated' WHERE company_id=$1 AND external_user_id=$2`,
			companyID, identity.ExternalUserID); err != nil {
			t.Fatal(err)
		}
		_, err = service.IssueSsoToken(ctx, identity)
		assertApplicationCode(t, err, ErrorCodeExternalUserDeactivated)
		if _, err = pool.Exec(ctx, `UPDATE user_external_identities SET status='active' WHERE company_id=$1 AND external_user_id=$2`,
			companyID, identity.ExternalUserID); err != nil {
			t.Fatal(err)
		}
		if _, err = pool.Exec(ctx, `UPDATE users SET role='employee' WHERE company_id=$1 AND id=(
			SELECT user_id FROM user_external_identities WHERE company_id=$1 AND external_user_id=$2
		)`, companyID, identity.ExternalUserID); err != nil {
			t.Fatal(err)
		}
		_, err = service.ExchangeSsoToken(ctx, frozenToken.Token, SessionMeta{})
		assertApplicationCode(t, err, ErrorCodeSSOInvalid)

		other := provisioningInput("other-tenant", "owner")
		otherResult, err := service.ProvisionCompany(ctx, other)
		if err != nil {
			t.Fatal(err)
		}
		_, err = service.IssueSsoToken(ctx, IssueSsoTokenInput{
			Provider: input.Provider, ExternalAccountID: input.ExternalAccountID,
			ExternalUserID: other.Owner.ExternalUserID,
		})
		if !isApplicationKind(err, ErrorNotFound) {
			t.Fatalf("cross-tenant SSO error=%v", err)
		}
		var otherOwnerID uuid.UUID
		if err = pool.QueryRow(ctx, `SELECT owner_id FROM companies WHERE id=$1`, otherResult.CompanyID).Scan(&otherOwnerID); err != nil {
			t.Fatal(err)
		}
		if _, err = pool.Exec(ctx, `UPDATE companies SET owner_id=$2 WHERE id=$1`, companyID, otherOwnerID); err == nil {
			t.Fatal("cross-tenant owner foreign key accepted")
		}
	})
}

type provisioningTestClock struct {
	mu    sync.Mutex
	value time.Time
}

func (c *provisioningTestClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.value
}

func (c *provisioningTestClock) Add(delta time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.value = c.value.Add(delta)
}

func provisioningTestService(t *testing.T, pool *pgxpool.Pool, clock *provisioningTestClock) *Service {
	t.Helper()
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	service, err := NewService(pool, sharedauth.NewTokenIssuer(privateKey, "test-company", "test-api", time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	service.now = clock.Now
	return service
}

func provisioningInput(suffix, initiatorRole string) ProvisionCompanyInput {
	owner := ProvisioningParticipantInput{
		ExternalUserID: "owner-" + suffix, Email: "owner-" + suffix + "@example.com",
		FirstName: "Иван", LastName: "Владелец",
	}
	admin := ProvisioningParticipantInput{
		ExternalUserID: "admin-" + suffix, Email: "admin-" + suffix + "@example.com",
		FirstName: "Анна", LastName: "Администратор",
	}
	initiator := owner.ExternalUserID
	if initiatorRole == "admin" {
		initiator = admin.ExternalUserID
	}
	return ProvisionCompanyInput{
		Provider: "rakurs", ExternalAccountID: "account-" + suffix,
		CompanyName: "Компания " + suffix, InitiatorExternalUserID: initiator,
		Owner: owner, Admin: admin, IdempotencyKey: "create-" + suffix,
	}
}

func activateProvisioningCompany(
	t *testing.T,
	ctx context.Context,
	service *Service,
	input ProvisionCompanyInput,
) uuid.UUID {
	t.Helper()
	provisioned, err := service.ProvisionCompany(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	first, err := service.CompleteBootstrapActivation(ctx, CompleteBootstrapInput{
		Token: provisioned.BootstrapToken, Password: "first-password",
	}, SessionMeta{})
	if err != nil || first.Onboarding.ActivationToken == nil {
		t.Fatalf("first activation=%+v err=%v", first, err)
	}
	second, err := service.CompleteBootstrapActivation(ctx, CompleteBootstrapInput{
		Token: *first.Onboarding.ActivationToken, Password: "second-password",
	}, SessionMeta{})
	if err != nil || !second.Onboarding.Completed {
		t.Fatalf("second activation=%+v err=%v", second, err)
	}
	return provisioned.CompanyID
}

func assertPendingBootstrapGuards(
	t *testing.T,
	ctx context.Context,
	service *Service,
	actorUser User,
	pending BootstrapParticipant,
) {
	t.Helper()
	actor := Actor{UserID: actorUser.ID, CompanyID: actorUser.CompanyID, Role: actorUser.Role}
	status := "active"
	_, err := service.UpdateUser(ctx, actor, UpdateUserInput{ID: pending.UserID, Status: &status})
	assertApplicationCode(t, err, ErrorCodePendingBootstrapLocked)
	err = service.DeleteUser(ctx, actor, pending.UserID)
	if pending.Role == "owner" {
		if err == nil {
			t.Fatal("pending owner deletion succeeded")
		}
		return
	}
	assertApplicationCode(t, err, ErrorCodePendingBootstrapLocked)
}

func assertApplicationCode(t *testing.T, err error, code string) {
	t.Helper()
	var applicationErr *Error
	if !errors.As(err, &applicationErr) || applicationErr.Code != code {
		t.Fatalf("error=%v code=%q, want %q", err, applicationErrCode(applicationErr), code)
	}
}

func assertOneSuccessfulRedemption(t *testing.T, errorsFound []error, rejectedCode string) {
	t.Helper()
	succeeded, rejected := 0, 0
	for _, err := range errorsFound {
		if err == nil {
			succeeded++
			continue
		}
		var applicationErr *Error
		if errors.As(err, &applicationErr) && applicationErr.Code == rejectedCode {
			rejected++
			continue
		}
		t.Fatalf("unexpected redemption error: %v", err)
	}
	if succeeded != 1 || rejected != 1 {
		t.Fatalf("redemptions succeeded=%d rejected=%d, want 1/1; errors=%v", succeeded, rejected, errorsFound)
	}
}

func applicationErrCode(err *Error) string {
	if err == nil {
		return ""
	}
	return err.Code
}

func isApplicationKind(err error, kind ErrorKind) bool {
	var applicationErr *Error
	return errors.As(err, &applicationErr) && applicationErr.Kind == kind
}

func assertOutboxSubjects(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	companyID uuid.UUID,
	want map[string]int,
) {
	t.Helper()
	rows, err := pool.Query(ctx, `SELECT subject, count(*) FROM outbox WHERE company_id=$1 GROUP BY subject`, companyID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	actual := make(map[string]int)
	for rows.Next() {
		var subject string
		var count int
		if err = rows.Scan(&subject, &count); err != nil {
			t.Fatal(err)
		}
		actual[subject] = count
	}
	for subject, count := range want {
		if actual[subject] != count {
			t.Fatalf("subject %s count=%d, want %d; all=%v", subject, actual[subject], count, actual)
		}
	}
}

func provisioningTestPool(t *testing.T, ctx context.Context) *pgxpool.Pool {
	t.Helper()
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("не удалось определить путь к миграциям")
	}
	migrationsDir := filepath.Join(filepath.Dir(filename), "..", "..", "migrations")
	initScripts := make([]string, 0, 9)
	for migration, name := range []string{
		"init", "phase6_schedule_distribution", "amo_users", "remove_amo_from_user_names",
		"validate_phone", "employee_access", "user_profiles_access_audit",
		"employee_sections_lifecycle", "provisioning",
	} {
		initScripts = append(initScripts, filepath.Join(migrationsDir, fmt.Sprintf("%06d_%s.up.sql", migration+1, name)))
	}
	container, err := postgres.Run(ctx, "postgres:16-alpine",
		postgres.WithDatabase("company"), postgres.WithUsername("company"), postgres.WithPassword("company"),
		postgres.WithInitScripts(initScripts...), postgres.BasicWaitStrategies(),
	)
	if err != nil {
		t.Fatalf("запуск PostgreSQL: %v", err)
	}
	t.Cleanup(func() {
		if terminateErr := testcontainers.TerminateContainer(container); terminateErr != nil {
			t.Errorf("остановка PostgreSQL: %v", terminateErr)
		}
	})
	connectionString, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	pool, err := pgxpool.New(ctx, connectionString)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return pool
}
