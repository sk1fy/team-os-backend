package seed

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

type recordedExec struct {
	queries       []string
	failSubstring string
}

func (exec *recordedExec) Exec(
	_ context.Context,
	query string,
	_ ...any,
) (pgconn.CommandTag, error) {
	exec.queries = append(exec.queries, query)
	if exec.failSubstring != "" && strings.Contains(query, exec.failSubstring) {
		return pgconn.CommandTag{}, errors.New("database failure")
	}
	return pgconn.NewCommandTag("INSERT 0 1"), nil
}

func TestApplyUsesDependencyOrderAndAllRepositorySteps(t *testing.T) {
	dataset, err := Normalize(minimalFixtures(), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	exec := &recordedExec{}
	if err := Apply(context.Background(), exec, dataset, "$argon2id$test"); err != nil {
		t.Fatal(err)
	}

	joined := strings.Join(exec.queries, "\n")
	for _, fragment := range []string{
		"SET CONSTRAINTS ALL DEFERRED",
		"INSERT INTO companies",
		"INSERT INTO users",
		"INSERT INTO credentials",
		"INSERT INTO departments",
		"UPDATE departments",
		"INSERT INTO positions",
		"DELETE FROM user_positions",
		"INSERT INTO user_positions",
		"UPDATE companies",
	} {
		if !strings.Contains(joined, fragment) {
			t.Fatalf("repository queries do not contain %q:\n%s", fragment, joined)
		}
	}
	if strings.Index(joined, "INSERT INTO departments") > strings.Index(joined, "INSERT INTO positions") {
		t.Fatalf("positions were written before departments:\n%s", joined)
	}
	if strings.LastIndex(joined, "UPDATE companies") < strings.Index(joined, "INSERT INTO users") {
		t.Fatalf("owner was assigned before users:\n%s", joined)
	}
}

func TestApplyAddsEntityContextToDatabaseErrors(t *testing.T) {
	dataset, err := Normalize(minimalFixtures(), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	exec := &recordedExec{failSubstring: "INSERT INTO positions"}
	err = Apply(context.Background(), exec, dataset, "$argon2id$test")
	if err == nil || !strings.Contains(err.Error(), "сохранить должность") {
		t.Fatalf("Apply() error = %v", err)
	}
}
