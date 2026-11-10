//go:build integration

package application

import (
	"context"
	"errors"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/sk1fy/team-os-backend/services/company/internal/storage/db"
)

type amoEntryCheckerFunc func(context.Context, AmoEntrySubject) (bool, error)

func (f amoEntryCheckerFunc) Check(ctx context.Context, s AmoEntrySubject) (bool, error) {
	return f(ctx, s)
}

type amoEntryFailIssuer struct{}

func (amoEntryFailIssuer) Issue(string, string, string, []string, []string, ...[]string) (string, time.Time, error) {
	return "", time.Time{}, errors.New("injected signing failure")
}

func entryTestInput(account, actor, email string) AmoWidgetEntryInput {
	return AmoWidgetEntryInput{AmoEntrySubject: AmoEntrySubject{AccountID: account, ActorID: actor, IntegrationID: uuid.NewString(), InstallationID: uuid.NewString()}, Intent: "create", Name: "Тестовый Администратор", Email: email}
}

func TestAmoCompanyEntryTransactions(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 6*time.Minute)
	defer cancel()
	pool := companyAccessTestPool(t, ctx)
	clock := &companyRegistrationTestClock{value: time.Now().UTC()}
	first, second := companyRegistrationTestService(t, pool, clock), companyRegistrationTestService(t, pool, clock)
	allowed := amoEntryCheckerFunc(func(context.Context, AmoEntrySubject) (bool, error) { return true, nil })
	first.amoEntrySubjectChecker, second.amoEntrySubjectChecker = allowed, allowed
	count := func(query string, args ...any) int {
		t.Helper()
		var n int
		if err := pool.QueryRow(ctx, query, args...).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	provision := func(input AmoWidgetEntryInput) AmoWidgetEntryResult {
		t.Helper()
		r, err := first.ProvisionAmoWidgetEntry(ctx, input)
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	assertNoExchangeCommit := func(companyID uuid.UUID) {
		t.Helper()
		if count(`SELECT count(*) FROM amo_company_entries WHERE company_id=$1 AND consumed_at IS NOT NULL`, companyID) != 0 || count(`SELECT count(*) FROM sessions WHERE company_id=$1`, companyID) != 0 || count(`SELECT count(*) FROM session_families WHERE company_id=$1`, companyID) != 0 {
			t.Fatal("denied exchange consumed code or created session")
		}
	}

	t.Run("actual status ignores reservation and enter does not create", func(t *testing.T) {
		input := entryTestInput("810001", "1", "admin@example.test")
		if _, err := first.IssueCompanyRegistrationToken(ctx, "rakurs", input.AccountID); err != nil {
			t.Fatal(err)
		}
		status, err := first.GetAmoEntryCompanyStatus(ctx, input.AccountID)
		if err != nil || status.State != "absent" {
			t.Fatalf("reservation became company %+v %v", status, err)
		}
		input.Intent = "enter"
		if _, err = first.ProvisionAmoWidgetEntry(ctx, input); err == nil {
			t.Fatal("stale enter created company")
		}
		if count(`SELECT count(*) FROM companies WHERE amo_account_id=$1`, input.AccountID) != 0 {
			t.Fatal("enter provisioned absent company")
		}
	})

	t.Run("contradictory legacy account blocks both accounts without writes", func(t *testing.T) {
		var companyID uuid.UUID
		if err := pool.QueryRow(ctx, `INSERT INTO companies(name,amo_account_id) VALUES ('Несогласованная','111') RETURNING id`).Scan(&companyID); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `INSERT INTO company_integrations(company_id,provider,external_account_id) VALUES ($1,'rakurs','222')`, companyID); err != nil {
			t.Fatal(err)
		}
		for _, accountID := range []string{"111", "222"} {
			status, err := first.GetAmoEntryCompanyStatus(ctx, accountID)
			if err != nil || status.State != "blocked" || status.CompanyID == nil || *status.CompanyID != companyID {
				t.Fatalf("account %s status=%+v error=%v", accountID, status, err)
			}
			for _, intent := range []string{"enter", "create"} {
				input := entryTestInput(accountID, "1", "mismatch@example.test")
				input.Intent = intent
				_, err = first.ProvisionAmoWidgetEntry(ctx, input)
				var appErr *Error
				if !errors.As(err, &appErr) || appErr.Kind != ErrorForbidden {
					t.Fatalf("account %s intent %s error=%v, want forbidden", accountID, intent, err)
				}
			}
		}
		if count(`SELECT count(*) FROM companies WHERE amo_account_id IN ('111','222')`) != 1 || count(`SELECT count(*) FROM company_integrations WHERE external_account_id IN ('111','222')`) != 1 {
			t.Fatal("denied entry changed account bindings")
		}
		for _, table := range []string{"users", "user_external_identities", "amo_company_entries", "outbox", "access_links", "credentials"} {
			if count("SELECT count(*) FROM "+table+" WHERE company_id=$1", companyID) != 0 {
				t.Fatalf("denied entry wrote %s", table)
			}
		}
		assertNoExchangeCommit(companyID)
	})

	t.Run("canonical integration permits null legacy account", func(t *testing.T) {
		input := entryTestInput("810012", "1", "canonical@example.test")
		entry := provision(input)
		if _, err := pool.Exec(ctx, `UPDATE companies SET amo_account_id=NULL WHERE id=$1`, entry.CompanyID); err != nil {
			t.Fatal(err)
		}
		status, err := first.GetAmoEntryCompanyStatus(ctx, input.AccountID)
		if err != nil || status.State != "present" || status.CompanyID == nil || *status.CompanyID != entry.CompanyID {
			t.Fatalf("canonical integration status=%+v error=%v", status, err)
		}
		input.Intent, input.Email = "enter", ""
		again := provision(input)
		if again.CompanyID != entry.CompanyID || again.UserID != entry.UserID {
			t.Fatal("null legacy account changed canonical subject")
		}
		if _, err = first.ExchangeAmoCompanyEntry(ctx, again.Code, SessionMeta{}); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("replicas widgets actors owner and outbox", func(t *testing.T) {
		inputs := []AmoWidgetEntryInput{entryTestInput("810002", "1", "first@example.test"), entryTestInput("810002", "2", "second@example.test")}
		var results [2]AmoWidgetEntryResult
		var errs [2]error
		var wg sync.WaitGroup
		start := make(chan struct{})
		for i, s := range []*Service{first, second} {
			wg.Add(1)
			go func(i int, s *Service) {
				defer wg.Done()
				<-start
				results[i], errs[i] = s.ProvisionAmoWidgetEntry(ctx, inputs[i])
			}(i, s)
		}
		close(start)
		wg.Wait()
		for _, err := range errs {
			if err != nil {
				t.Fatal(err)
			}
		}
		if results[0].CompanyID != results[1].CompanyID || results[0].UserID == results[1].UserID || results[0].Code == results[1].Code {
			t.Fatal("account or actor identity collision")
		}
		company := results[0].CompanyID
		if count(`SELECT count(*) FROM company_integrations WHERE company_id=$1`, company) != 1 || count(`SELECT count(*) FROM users WHERE company_id=$1`, company) != 2 || count(`SELECT count(*) FROM users WHERE company_id=$1 AND role='owner'`, company) != 1 {
			t.Fatal("non-atomic account resolution")
		}
		var owner uuid.UUID
		if err := pool.QueryRow(ctx, `SELECT owner_id FROM companies WHERE id=$1`, company).Scan(&owner); err != nil {
			t.Fatal(err)
		}
		for i, input := range inputs {
			input.IntegrationID, input.InstallationID = uuid.NewString(), uuid.NewString()
			input.Email = ""
			input.Name = "changed"
			again := provision(input)
			if again.CompanyID != company || again.UserID != results[i].UserID || again.Code == results[i].Code {
				t.Fatal("lost response retry changed subject or reused code")
			}
		}
		var preserved uuid.UUID
		if err := pool.QueryRow(ctx, `SELECT owner_id FROM companies WHERE id=$1`, company).Scan(&preserved); err != nil {
			t.Fatal(err)
		}
		if preserved != owner || count(`SELECT count(*) FROM outbox WHERE company_id=$1 AND subject='teamos.company.company.created.v1'`, company) != 1 || count(`SELECT count(*) FROM outbox WHERE company_id=$1 AND subject='teamos.org.user.created.v1'`, company) != 2 {
			t.Fatal("owner transferred or duplicate outbox")
		}
		if count(`SELECT count(*) FROM access_links WHERE company_id=$1`, company) != 0 || count(`SELECT count(*) FROM credentials WHERE company_id=$1`, company) != 0 {
			t.Fatal("widget entry created permanent credential")
		}
		var grants []string
		if err := pool.QueryRow(ctx, `SELECT entitlements FROM company_integrations WHERE company_id=$1`, company).Scan(&grants); err != nil || len(grants) != 0 {
			t.Fatalf("implicit product grants %v %v", grants, err)
		}
	})

	t.Run("profile rollback email collisions and tenant separation", func(t *testing.T) {
		input := entryTestInput("810003", "1", "")
		if _, err := first.ProvisionAmoWidgetEntry(ctx, input); err == nil {
			t.Fatal("missing new email accepted")
		}
		if count(`SELECT count(*) FROM companies WHERE amo_account_id=$1`, input.AccountID) != 0 {
			t.Fatal("missing profile committed fragments")
		}
		input.Email = "same@example.test"
		original := provision(input)
		input.ActorID = "2"
		if _, err := first.ProvisionAmoWidgetEntry(ctx, input); err == nil {
			t.Fatal("same email merged different CRM actor")
		}
		if count(`SELECT count(*) FROM users WHERE company_id=$1`, original.CompanyID) != 1 {
			t.Fatal("email conflict committed user")
		}
		input.AccountID = "810004"
		other := provision(input)
		if other.CompanyID == original.CompanyID || other.UserID == original.UserID {
			t.Fatal("cross-company email merged subject")
		}
		input.AccountID = "810003"
		input.ActorID = "1"
		input.Email = ""
		again := provision(input)
		if again.UserID != original.UserID {
			t.Fatal("existing linked identity required fresh email")
		}
		var email, name string
		if err := pool.QueryRow(ctx, `SELECT email,first_name FROM users WHERE id=$1`, original.UserID).Scan(&email, &name); err != nil {
			t.Fatal(err)
		}
		if email != "same@example.test" || name != "Тестовый" {
			t.Fatal("stored linked profile overwritten")
		}
	})

	t.Run("legacy amo users reuse own identity without reactivation", func(t *testing.T) {
		for i, state := range []string{"active", "deactivated", "deleted"} {
			t.Run(state, func(t *testing.T) {
				input := entryTestInput(strconv.Itoa(830000+i), "7", "")
				input.Intent = "enter"
				var companyID, userID uuid.UUID
				if err := pool.QueryRow(ctx, `INSERT INTO companies(name,amo_account_id) VALUES ('Прежняя',$1) RETURNING id`, input.AccountID).Scan(&companyID); err != nil {
					t.Fatal(err)
				}
				if err := pool.QueryRow(ctx, `INSERT INTO users(company_id,email,first_name,role,status,source,external_id) VALUES ($1,'legacy@example.test','Сохранённый','employee','active','amo',$2) RETURNING id`, companyID, input.ActorID).Scan(&userID); err != nil {
					t.Fatal(err)
				}
				if state == "deactivated" {
					if _, err := pool.Exec(ctx, `UPDATE users SET status='deactivated' WHERE id=$1`, userID); err != nil {
						t.Fatal(err)
					}
				} else if state == "deleted" {
					if _, err := pool.Exec(ctx, `UPDATE users SET external_deleted_at=now() WHERE id=$1`, userID); err != nil {
						t.Fatal(err)
					}
				}
				entry, err := first.ProvisionAmoWidgetEntry(ctx, input)
				if state != "active" {
					var appErr *Error
					if !errors.As(err, &appErr) || appErr.Kind != ErrorForbidden {
						t.Fatalf("inactive legacy user error=%v, want forbidden", err)
					}
					for _, table := range []string{"company_integrations", "user_external_identities", "amo_company_entries", "outbox"} {
						if count("SELECT count(*) FROM "+table+" WHERE company_id=$1", companyID) != 0 {
							t.Fatalf("inactive legacy user committed %s", table)
						}
					}
					if count(`SELECT count(*) FROM users WHERE id=$1 AND role='employee' AND (status='deactivated' OR external_deleted_at IS NOT NULL)`, userID) != 1 {
						t.Fatal("inactive legacy user was reactivated")
					}
					return
				}
				if err != nil || entry.CompanyID != companyID || entry.UserID != userID {
					t.Fatalf("legacy subject changed: entry=%+v error=%v", entry, err)
				}
				if count(`SELECT count(*) FROM users WHERE company_id=$1`, companyID) != 1 || count(`SELECT count(*) FROM users WHERE id=$1 AND role='admin' AND email='legacy@example.test' AND first_name='Сохранённый'`, userID) != 1 || count(`SELECT count(*) FROM user_external_identities WHERE company_id=$1 AND user_id=$2 AND external_user_id=$3`, companyID, userID, input.ActorID) != 1 {
					t.Fatal("legacy user duplicated or stored profile overwritten")
				}
				if count(`SELECT count(*) FROM companies WHERE id=$1 AND owner_id IS NOT NULL`, companyID) != 0 || count(`SELECT count(*) FROM outbox WHERE company_id=$1 AND subject IN ('teamos.company.company.created.v1','teamos.org.user.created.v1')`, companyID) != 0 {
					t.Fatal("legacy reuse claimed company or emitted creation")
				}
				login, err := first.ExchangeAmoCompanyEntry(ctx, entry.Code, SessionMeta{})
				if err != nil || login.User.Role != "admin" {
					t.Fatalf("legacy admin exchange error=%v", err)
				}
			})
		}
	})

	t.Run("failed handoff insert rolls back company user and outbox", func(t *testing.T) {
		_, err := pool.Exec(ctx, `CREATE FUNCTION reject_entry_insert() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'injected entry write failure'; END $$; CREATE TRIGGER reject_entry_insert BEFORE INSERT ON amo_company_entries FOR EACH ROW EXECUTE FUNCTION reject_entry_insert()`)
		if err != nil {
			t.Fatal(err)
		}
		input := entryTestInput("810005", "1", "rollback@example.test")
		_, provisionErr := first.ProvisionAmoWidgetEntry(ctx, input)
		if _, err = pool.Exec(ctx, `DROP TRIGGER reject_entry_insert ON amo_company_entries; DROP FUNCTION reject_entry_insert()`); err != nil {
			t.Fatal(err)
		}
		if provisionErr == nil {
			t.Fatal("fault was not exercised")
		}
		if count(`SELECT count(*) FROM companies WHERE amo_account_id=$1`, input.AccountID) != 0 || count(`SELECT count(*) FROM users WHERE email=$1`, input.Email) != 0 || count(`SELECT count(*) FROM outbox WHERE payload::text LIKE '%810005%'`) != 0 {
			t.Fatal("failed provisioning committed fragments")
		}
		provision(input)
	})

	t.Run("parallel redemption exactly one session replay rejected before callback", func(t *testing.T) {
		entry := provision(entryTestInput("810006", "1", "replay@example.test"))
		var callbacks atomic.Int32
		checker := amoEntryCheckerFunc(func(context.Context, AmoEntrySubject) (bool, error) { callbacks.Add(1); return true, nil })
		first.amoEntrySubjectChecker, second.amoEntrySubjectChecker = checker, checker
		defer func() { first.amoEntrySubjectChecker, second.amoEntrySubjectChecker = allowed, allowed }()
		var wins atomic.Int32
		var wg sync.WaitGroup
		start := make(chan struct{})
		for _, s := range []*Service{first, second} {
			wg.Add(1)
			go func(s *Service) {
				defer wg.Done()
				<-start
				if _, err := s.ExchangeAmoCompanyEntry(ctx, entry.Code, SessionMeta{}); err == nil {
					wins.Add(1)
				}
			}(s)
		}
		close(start)
		wg.Wait()
		if wins.Load() != 1 || count(`SELECT count(*) FROM sessions WHERE company_id=$1`, entry.CompanyID) != 1 {
			t.Fatal("handoff admitted more than one session")
		}
		before := callbacks.Load()
		if _, err := first.ExchangeAmoCompanyEntry(ctx, entry.Code, SessionMeta{}); err == nil {
			t.Fatal("consumed code replay succeeded")
		}
		if _, err := first.ExchangeAmoCompanyEntry(ctx, "unknown", SessionMeta{}); err == nil {
			t.Fatal("unknown code accepted")
		}
		if callbacks.Load() != before {
			t.Fatal("replay or unknown code reached Core")
		}
	})

	t.Run("session rollback leaves handoff usable", func(t *testing.T) {
		entry := provision(entryTestInput("810007", "1", "session-rollback@example.test"))
		issuer := first.issuer
		first.issuer = amoEntryFailIssuer{}
		_, err := first.ExchangeAmoCompanyEntry(ctx, entry.Code, SessionMeta{})
		first.issuer = issuer
		if err == nil {
			t.Fatal("signer fault not reached")
		}
		if count(`SELECT count(*) FROM amo_company_entries WHERE company_id=$1 AND consumed_at IS NOT NULL`, entry.CompanyID) != 0 || count(`SELECT count(*) FROM sessions WHERE company_id=$1`, entry.CompanyID) != 0 || count(`SELECT count(*) FROM session_families WHERE company_id=$1`, entry.CompanyID) != 0 {
			t.Fatal("failed exchange committed fragments")
		}
		if _, err = first.ExchangeAmoCompanyEntry(ctx, entry.Code, SessionMeta{}); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("Core denied and unavailable preserve code for fresh check", func(t *testing.T) {
		input := entryTestInput("810008", "1", "callback@example.test")
		entry := provision(input)
		for _, callbackErr := range []error{nil, errors.New("callback unavailable")} {
			first.amoEntrySubjectChecker = amoEntryCheckerFunc(func(_ context.Context, subject AmoEntrySubject) (bool, error) {
				if subject != input.AmoEntrySubject {
					t.Error("immutable originating subject changed")
				}
				return false, callbackErr
			})
			if _, err := first.ExchangeAmoCompanyEntry(ctx, entry.Code, SessionMeta{}); err == nil {
				t.Fatal("failed Core authorization accepted")
			}
		}
		first.amoEntrySubjectChecker = allowed
		if count(`SELECT count(*) FROM amo_company_entries WHERE company_id=$1 AND consumed_at IS NOT NULL`, entry.CompanyID) != 0 || count(`SELECT count(*) FROM sessions WHERE company_id=$1`, entry.CompanyID) != 0 {
			t.Fatal("failed Core check consumed code")
		}
		if _, err := first.ExchangeAmoCompanyEntry(ctx, entry.Code, SessionMeta{}); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("expired code skips Core", func(t *testing.T) {
		entry := provision(entryTestInput("810009", "1", "expired@example.test"))
		first.amoEntrySubjectChecker = amoEntryCheckerFunc(func(context.Context, AmoEntrySubject) (bool, error) {
			t.Error("expired code reached Core")
			return true, nil
		})
		clock.Add(time.Minute)
		_, err := first.ExchangeAmoCompanyEntry(ctx, entry.Code, SessionMeta{})
		first.amoEntrySubjectChecker = allowed
		if err == nil {
			t.Fatal("expired code accepted")
		}
	})

	t.Run("code expiring during Core callback is not consumed", func(t *testing.T) {
		entry := provision(entryTestInput("810013", "1", "callback-expiry@example.test"))
		var callbacks int
		first.amoEntrySubjectChecker = amoEntryCheckerFunc(func(context.Context, AmoEntrySubject) (bool, error) {
			callbacks++
			clock.Add(time.Minute)
			return true, nil
		})
		defer func() { first.amoEntrySubjectChecker = allowed }()
		_, err := first.ExchangeAmoCompanyEntry(ctx, entry.Code, SessionMeta{})
		assertCompanyRegistrationCode(t, err, "AMO_ENTRY_INVALID")
		if callbacks != 1 {
			t.Fatalf("callback count=%d, want 1", callbacks)
		}
		assertNoExchangeCommit(entry.CompanyID)
	})

	t.Run("account mismatch introduced during callback denies locked exchange", func(t *testing.T) {
		input := entryTestInput("810014", "1", "callback-mismatch@example.test")
		entry := provision(input)
		var callbacks int
		first.amoEntrySubjectChecker = amoEntryCheckerFunc(func(ctx context.Context, subject AmoEntrySubject) (bool, error) {
			callbacks++
			if subject != input.AmoEntrySubject {
				t.Error("originating subject changed")
			}
			_, err := pool.Exec(ctx, `UPDATE companies SET amo_account_id='810015' WHERE id=$1`, entry.CompanyID)
			return true, err
		})
		defer func() { first.amoEntrySubjectChecker = allowed }()
		_, err := first.ExchangeAmoCompanyEntry(ctx, entry.Code, SessionMeta{})
		var appErr *Error
		if !errors.As(err, &appErr) || appErr.Kind != ErrorForbidden || callbacks != 1 {
			t.Fatalf("callback count=%d error=%v, want forbidden after callback", callbacks, err)
		}
		assertNoExchangeCommit(entry.CompanyID)
		if _, err = pool.Exec(ctx, `UPDATE companies SET amo_account_id=$2 WHERE id=$1`, entry.CompanyID, input.AccountID); err != nil {
			t.Fatal(err)
		}
		first.amoEntrySubjectChecker = allowed
		if _, err = first.ExchangeAmoCompanyEntry(ctx, entry.Code, SessionMeta{}); err != nil {
			t.Fatalf("denied mismatch consumed handoff: %v", err)
		}
	})

	t.Run("local revocations deny provision and exchange", func(t *testing.T) {
		for i, mutation := range []string{
			`UPDATE users SET status='deactivated' WHERE id=$1`,
			`UPDATE users SET external_deleted_at=now() WHERE id=$1`,
			`UPDATE user_external_identities SET status='deactivated' WHERE user_id=$1`,
			`UPDATE company_integrations SET status='frozen' WHERE company_id=(SELECT company_id FROM users WHERE id=$1)`,
			`UPDATE companies SET status='frozen' WHERE id=(SELECT company_id FROM users WHERE id=$1)`,
		} {
			input := entryTestInput(strconv.Itoa(820000+i), "1", "denied@example.test")
			entry := provision(input)
			if _, err := pool.Exec(ctx, mutation, entry.UserID); err != nil {
				t.Fatal(err)
			}
			if _, err := first.ExchangeAmoCompanyEntry(ctx, entry.Code, SessionMeta{}); err == nil {
				t.Fatal("local revocation ignored at exchange")
			}
			if _, err := first.ProvisionAmoWidgetEntry(ctx, input); err == nil {
				t.Fatal("local revocation reactivated at provision")
			}
			if count(`SELECT count(*) FROM sessions WHERE company_id=$1`, entry.CompanyID) != 0 {
				t.Fatal("revoked subject has session")
			}
		}
	})

	t.Run("callback holds no database locks and local state rechecked", func(t *testing.T) {
		input := entryTestInput("810010", "1", "race@example.test")
		entry := provision(input)
		arrived, release := make(chan struct{}), make(chan struct{})
		first.amoEntrySubjectChecker = amoEntryCheckerFunc(func(ctx context.Context, _ AmoEntrySubject) (bool, error) {
			close(arrived)
			select {
			case <-release:
				return true, nil
			case <-ctx.Done():
				return false, ctx.Err()
			}
		})
		result := make(chan error, 1)
		go func() { _, err := first.ExchangeAmoCompanyEntry(ctx, entry.Code, SessionMeta{}); result <- err }()
		<-arrived
		updateCtx, stop := context.WithTimeout(ctx, 5*time.Second)
		_, updateErr := pool.Exec(updateCtx, `UPDATE users SET status='deactivated' WHERE id=$1`, entry.UserID)
		stop()
		close(release)
		exchangeErr := <-result
		first.amoEntrySubjectChecker = allowed
		if updateErr != nil {
			t.Fatal("Core network call held DB lock", updateErr)
		}
		if exchangeErr == nil {
			t.Fatal("local check/use race allowed revoked user")
		}
	})

	t.Run("ownerless existing company never claimed and employee links remain ordinary", func(t *testing.T) {
		input := entryTestInput("810011", "1", "later-admin@example.test")
		var companyID uuid.UUID
		if err := pool.QueryRow(ctx, `INSERT INTO companies(name,amo_account_id) VALUES ('Существующая',$1) RETURNING id`, input.AccountID).Scan(&companyID); err != nil {
			t.Fatal(err)
		}
		entry := provision(input)
		if entry.CompanyID != companyID || count(`SELECT count(*) FROM companies WHERE id=$1 AND owner_id IS NOT NULL`, companyID) != 0 || count(`SELECT count(*) FROM users WHERE id=$1 AND role='admin'`, entry.UserID) != 1 {
			t.Fatal("existing ownerless company claimed")
		}
		employeeID := uuid.New()
		if _, err := db.New(pool).CreateUser(ctx, db.CreateUserParams{ID: employeeID, CompanyID: companyID, Email: "employee@example.test", FirstName: "Сотрудник", Role: "employee", Status: "active"}); err != nil {
			t.Fatal(err)
		}
		admin := Actor{CompanyID: companyID, UserID: entry.UserID, Role: "admin"}
		link, err := first.SetLinkAccess(ctx, admin, employeeID)
		if err != nil {
			t.Fatal(err)
		}
		for range 2 {
			login, err := first.LoginWithAccessLink(ctx, link.Token, SessionMeta{})
			if err != nil || login.Session.User.Role != "employee" {
				t.Fatalf("ordinary reusable employee link %+v %v", login, err)
			}
			actor := Actor{CompanyID: companyID, UserID: employeeID, Role: login.Session.User.Role}
			if _, err = first.GetUserAccess(ctx, actor, entry.UserID); err == nil {
				t.Fatal("employee session accessed admin-only endpoint")
			}
		}
		if err = first.RevokeAccess(ctx, admin, entry.UserID); err != nil {
			t.Fatal(err)
		}
		if _, exchangeErr := first.ExchangeAmoCompanyEntry(ctx, entry.Code, SessionMeta{}); exchangeErr == nil {
			t.Fatal("revoke-all left minted entry usable")
		}
		if _, err = first.ProvisionAmoWidgetEntry(ctx, input); err == nil {
			t.Fatal("revoke-all silently reactivated CRM identity")
		}
	})
}
