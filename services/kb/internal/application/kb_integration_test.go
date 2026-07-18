//go:build integration

package application

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	domainaccess "github.com/sk1fy/team-os-backend/services/kb/internal/domain/access"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

func TestSectionAccessAndArticleLifecycle(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("не удалось определить путь к миграциям")
	}
	migrationsDir := filepath.Join(filepath.Dir(filename), "..", "..", "migrations")
	container, err := postgres.Run(ctx, "postgres:16-alpine",
		postgres.WithDatabase("kb"),
		postgres.WithUsername("kb"),
		postgres.WithPassword("kb"),
		postgres.WithInitScripts(
			filepath.Join(migrationsDir, "000001_init.up.sql"),
			filepath.Join(migrationsDir, "000002_article_version_uniqueness.up.sql"),
			filepath.Join(migrationsDir, "000003_outbox_aggregate_order.up.sql"),
			filepath.Join(migrationsDir, "000004_section_visibility.up.sql"),
		),
		postgres.BasicWaitStrategies(),
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
		t.Fatalf("строка подключения: %v", err)
	}
	pool, err := pgxpool.New(ctx, connectionString)
	if err != nil {
		t.Fatalf("подключение к PostgreSQL: %v", err)
	}
	t.Cleanup(pool.Close)
	service, err := NewService(pool)
	if err != nil {
		t.Fatalf("создание сервиса: %v", err)
	}

	companyID := uuid.New()
	salesDept := uuid.New()
	supportDept := uuid.New()
	admin := Actor{UserID: uuid.New(), CompanyID: companyID, Role: "admin"}
	salesEmployee := Actor{
		UserID: uuid.New(), CompanyID: companyID, Role: "employee",
		DepartmentIDs: []uuid.UUID{salesDept},
	}
	supportEmployee := Actor{
		UserID: uuid.New(), CompanyID: companyID, Role: "employee",
		DepartmentIDs: []uuid.UUID{supportDept},
	}
	partner := Actor{UserID: uuid.New(), CompanyID: companyID, Role: "partner"}

	commonSection, err := service.CreateSection(ctx, admin, CreateSectionInput{Name: "Общие правила"})
	if err != nil {
		t.Fatalf("создание открытого раздела: %v", err)
	}
	salesSection, err := service.CreateSection(ctx, admin, CreateSectionInput{
		Name: "Отдел продаж",
		Access: &AccessSettings{
			Scope:         domainaccess.ScopeCustom,
			DepartmentIDs: []uuid.UUID{salesDept},
		},
	})
	if err != nil {
		t.Fatalf("создание раздела с ограниченным доступом: %v", err)
	}

	t.Run("видимость разделов по ролям", func(t *testing.T) {
		tests := []struct {
			name  string
			actor Actor
			want  int
		}{
			{"админ видит все разделы", admin, 2},
			{"сотрудник отдела видит оба раздела", salesEmployee, 2},
			{"сотрудник другого отдела видит только открытый", supportEmployee, 1},
			{"партнёр не видит company-разделы", partner, 0},
		}
		for _, test := range tests {
			sections, sectionsErr := service.GetSections(ctx, test.actor)
			if sectionsErr != nil {
				t.Fatalf("%s: %v", test.name, sectionsErr)
			}
			if len(sections) != test.want {
				t.Errorf("%s: получено %d разделов, ожидалось %d", test.name, len(sections), test.want)
			}
		}
	})

	content := json.RawMessage(`{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"Скрипт продаж"}]}]}`)
	article, err := service.CreateArticle(ctx, admin, CreateArticleInput{
		SectionID:               salesSection.ID,
		Title:                   "Скрипт первого звонка",
		Content:                 content,
		Status:                  "published",
		RequiresAcknowledgement: true,
	})
	if err != nil {
		t.Fatalf("публикация статьи: %v", err)
	}

	t.Run("доступ к статье наследуется от раздела", func(t *testing.T) {
		if _, readErr := service.GetArticle(ctx, salesEmployee, article.ID); readErr != nil {
			t.Errorf("сотрудник отдела должен читать статью: %v", readErr)
		}
		assertKind(t, "чтение чужим отделом", func() error {
			_, err := service.GetArticle(ctx, supportEmployee, article.ID)
			return err
		}, ErrorForbidden)
	})

	t.Run("создание статьи требует прав управления", func(t *testing.T) {
		assertKind(t, "создание статьи сотрудником", func() error {
			_, err := service.CreateArticle(ctx, salesEmployee, CreateArticleInput{
				SectionID: commonSection.ID, Title: "Черновик", Content: content, Status: "draft",
			})
			return err
		}, ErrorForbidden)
	})

	t.Run("обновление создаёт новую версию", func(t *testing.T) {
		newTitle := "Скрипт первого звонка v2"
		updated, updateErr := service.UpdateArticle(ctx, admin, UpdateArticleInput{
			ID: article.ID, Title: &newTitle,
		})
		if updateErr != nil {
			t.Fatalf("обновление статьи: %v", updateErr)
		}
		if updated.Version != article.Version+1 || updated.Title != newTitle {
			t.Errorf("после обновления version=%d title=%q, ожидалось version=%d title=%q",
				updated.Version, updated.Title, article.Version+1, newTitle)
		}
		versions, versionsErr := service.GetArticleVersions(ctx, admin, article.ID)
		if versionsErr != nil {
			t.Fatalf("список версий: %v", versionsErr)
		}
		if len(versions) == 0 {
			t.Error("ожидалась как минимум одна сохранённая версия статьи")
		}
	})

	t.Run("ознакомление фиксируется один раз", func(t *testing.T) {
		if ackErr := service.AcknowledgeArticle(ctx, salesEmployee, article.ID); ackErr != nil {
			t.Fatalf("ознакомление: %v", ackErr)
		}
		if ackErr := service.AcknowledgeArticle(ctx, salesEmployee, article.ID); ackErr != nil {
			t.Fatalf("повторное ознакомление должно быть идемпотентным: %v", ackErr)
		}
		acks, acksErr := service.GetAcknowledgements(ctx, admin, article.ID)
		if acksErr != nil {
			t.Fatalf("список ознакомлений: %v", acksErr)
		}
		if len(acks) != 1 {
			t.Errorf("получено %d ознакомлений, ожидалось 1", len(acks))
		}
	})
}

func assertKind(t *testing.T, name string, run func() error, want ErrorKind) {
	t.Helper()
	err := run()
	if err == nil {
		t.Errorf("%s: ожидалась ошибка, получен nil", name)
		return
	}
	var appErr *Error
	if !errors.As(err, &appErr) || appErr.Kind != want {
		t.Errorf("%s: получено %v, ожидалась ошибка Kind=%d", name, err, want)
	}
}
