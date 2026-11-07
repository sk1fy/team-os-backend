//go:build integration

package application

import (
	"context"
	"encoding/json"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

func TestArticleSnapshotAndPublicationRegressions(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	_, filename, _, _ := runtime.Caller(0)
	migrationsDir := filepath.Join(filepath.Dir(filename), "..", "..", "migrations")
	container, err := postgres.Run(ctx, "postgres:16-alpine",
		postgres.WithDatabase("kb"), postgres.WithUsername("kb"), postgres.WithPassword("kb"),
		postgres.WithInitScripts(
			filepath.Join(migrationsDir, "000001_init.up.sql"),
			filepath.Join(migrationsDir, "000002_article_version_uniqueness.up.sql"),
			filepath.Join(migrationsDir, "000003_outbox_aggregate_order.up.sql"),
			filepath.Join(migrationsDir, "000004_section_visibility.up.sql"),
			filepath.Join(migrationsDir, "000005_partner_access_and_reuse.up.sql"),
		),
		postgres.BasicWaitStrategies(),
	)
	if err != nil {
		t.Fatalf("postgres: %v", err)
	}
	t.Cleanup(func() { _ = testcontainers.TerminateContainer(container) })
	dsn, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	service, err := NewService(pool)
	if err != nil {
		t.Fatal(err)
	}

	companyID := uuid.New()
	admin := Actor{UserID: uuid.New(), CompanyID: companyID, Role: "admin"}
	partner := Actor{UserID: uuid.New(), CompanyID: companyID, Role: "partner"}
	section, err := service.CreateSection(ctx, admin, CreateSectionInput{Name: "Раздел"})
	if err != nil {
		t.Fatal(err)
	}
	content := json.RawMessage(`{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"v1"}]}]}`)
	copyAllowed := "copy_allowed"
	newArticle := func(t *testing.T, title string) Article {
		t.Helper()
		article, createErr := service.CreateArticle(ctx, admin, CreateArticleInput{
			SectionID: section.ID, Title: title, Content: content, Status: "published",
			RequiresAcknowledgement: true,
			PartnerAccess:           &PartnerAccessSettings{Mode: "selected", PartnerIDs: []uuid.UUID{partner.UserID}},
			PartnerReusePolicy:      &copyAllowed,
		})
		if createErr != nil {
			t.Fatal(createErr)
		}
		return article
	}
	snapshot := func(t *testing.T, article Article) ArticleSnapshotForCourseCopy {
		t.Helper()
		value, err := service.GetArticleSnapshotForCourseCopy(ctx, admin, article.ID, nil, &partner.UserID)
		if err != nil {
			t.Fatal(err)
		}
		return value
	}
	assertSnapshotPreserved := func(t *testing.T, original ArticleSnapshotForCourseCopy, before ArticleVersion) {
		t.Helper()
		versions, err := service.GetArticleVersions(ctx, admin, original.ArticleID)
		if err != nil {
			t.Fatal(err)
		}
		var found bool
		for _, version := range versions {
			if version.ID == original.ArticleVersionID {
				found = true
				if version.Version != original.Version || version.Title != original.Title || !jsonEqual(version.Content, original.Content) || version.AuthorID != before.AuthorID || !version.CreatedAt.Equal(before.CreatedAt) {
					t.Fatalf("снимок изменён: %+v", version)
				}
			}
		}
		if !found {
			t.Fatal("исходный UUID версии потерян")
		}
		var grants int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM article_snapshot_reuse_grants WHERE article_version_id=$1 AND content_hash=$2`, original.ArticleVersionID, original.ContentHash).Scan(&grants); err != nil {
			t.Fatal(err)
		}
		if grants != 1 {
			t.Fatalf("выдач исходного снимка=%d, ожидалась 1", grants)
		}
	}
	t.Run("update after copied snapshot preserves identity and metadata", func(t *testing.T) {
		article := newArticle(t, "Статья")
		original := snapshot(t, article)
		versions, err := service.GetArticleVersions(ctx, admin, article.ID)
		if err != nil || len(versions) != 1 {
			t.Fatalf("версии=%v err=%v", versions, err)
		}
		before := versions[0]
		ack := false
		metadata, err := service.UpdateArticle(ctx, admin, UpdateArticleInput{ID: article.ID, RequiresAcknowledgement: &ack})
		if err != nil || metadata.Version != article.Version {
			t.Fatalf("метаданные изменили версию: %+v err=%v", metadata, err)
		}
		title := "Новая статья"
		updated, err := service.UpdateArticle(ctx, admin, UpdateArticleInput{ID: article.ID, Title: &title})
		if err != nil {
			t.Fatalf("обновление после снимка: %v", err)
		}
		if updated.Version != 2 || updated.Title != title {
			t.Fatalf("обновление=%+v", updated)
		}
		assertSnapshotPreserved(t, original, before)
	})
	t.Run("rollback after copied current snapshot preserves grants", func(t *testing.T) {
		article := newArticle(t, "Статья")
		title := "Версия 2"
		updated, err := service.UpdateArticle(ctx, admin, UpdateArticleInput{ID: article.ID, Title: &title})
		if err != nil {
			t.Fatal(err)
		}
		original := snapshot(t, updated)
		versions, err := service.GetArticleVersions(ctx, admin, article.ID)
		if err != nil || len(versions) != 2 {
			t.Fatalf("версии=%v err=%v", versions, err)
		}
		rolled, err := service.RollbackArticle(ctx, admin, RollbackArticleInput{ArticleID: article.ID, VersionID: versions[1].ID})
		if err != nil {
			t.Fatalf("откат после снимка: %v", err)
		}
		if rolled.Version != 3 || rolled.Title != article.Title {
			t.Fatalf("откат=%+v", rolled)
		}
		assertSnapshotPreserved(t, original, versions[0])
	})
	t.Run("copied article delete returns conflict without deleting data or event", func(t *testing.T) {
		article := newArticle(t, "Статья")
		original := snapshot(t, article)
		versions, err := service.GetArticleVersions(ctx, admin, article.ID)
		if err != nil {
			t.Fatal(err)
		}
		assertKind(t, "удаление использованной статьи", func() error { return service.DeleteArticle(ctx, admin, article.ID) }, ErrorConflict)
		if _, err := service.GetArticle(ctx, admin, article.ID); err != nil {
			t.Fatal(err)
		}
		assertSnapshotPreserved(t, original, versions[0])
		var events int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM outbox WHERE aggregate_id=$1 AND subject='teamos.kb.article.deleted.v1'`, article.ID).Scan(&events); err != nil {
			t.Fatal(err)
		}
		if events != 0 {
			t.Fatalf("событий удаления=%d", events)
		}
	})
	t.Run("selected partner can acknowledge while other subjects cannot", func(t *testing.T) {
		article := newArticle(t, "Статья")
		if _, err := service.GetArticle(ctx, partner, article.ID); err != nil {
			t.Fatal(err)
		}
		if err := service.AcknowledgeArticle(ctx, partner, article.ID); err != nil {
			t.Fatalf("ознакомление выбранного партнёра: %v", err)
		}
		other := partner
		other.UserID = uuid.New()
		assertKind(t, "другой партнёр", func() error { return service.AcknowledgeArticle(ctx, other, article.ID) }, ErrorForbidden)
		other = partner
		other.CompanyID = uuid.New()
		assertKind(t, "другая компания", func() error { return service.AcknowledgeArticle(ctx, other, article.ID) }, ErrorNotFound)
		acks, err := service.GetAcknowledgements(ctx, admin, article.ID)
		if err != nil || len(acks) != 1 || acks[0].UserID != partner.UserID {
			t.Fatalf("ознакомления=%v err=%v", acks, err)
		}
	})
	t.Run("draft updates and rollback never emit content but publication does", func(t *testing.T) {
		article := newArticle(t, "Статья")
		draft := "draft"
		if _, err := service.UpdateArticle(ctx, admin, UpdateArticleInput{ID: article.ID, Status: &draft}); err != nil {
			t.Fatal(err)
		}
		title := "Неопубликованный текст"
		secret := json.RawMessage(`{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"Секретный черновик"}]}]}`)
		if _, err := service.UpdateArticle(ctx, admin, UpdateArticleInput{ID: article.ID, Title: &title, Content: secret}); err != nil {
			t.Fatal(err)
		}
		versions, err := service.GetArticleVersions(ctx, admin, article.ID)
		if err != nil {
			t.Fatal(err)
		}
		rolled, err := service.RollbackArticle(ctx, admin, RollbackArticleInput{ArticleID: article.ID, VersionID: versions[0].ID})
		if err != nil {
			t.Fatal(err)
		}
		if rolled.Status != "draft" {
			t.Fatalf("откат опубликовал черновик: %s", rolled.Status)
		}
		count := func() int {
			t.Helper()
			var events int
			if err := pool.QueryRow(ctx, `SELECT count(*) FROM outbox WHERE aggregate_id=$1 AND subject='teamos.kb.article.updated.v1'`, article.ID).Scan(&events); err != nil {
				t.Fatal(err)
			}
			return events
		}
		if got := count(); got != 0 {
			t.Fatalf("событий с неопубликованным текстом=%d, ожидалось 0", got)
		}
		if _, err := service.UpdateArticle(ctx, admin, UpdateArticleInput{ID: article.ID, Title: &title, Content: secret}); err != nil {
			t.Fatal(err)
		}
		if got := count(); got != 0 {
			t.Fatalf("событий после повторной правки черновика=%d", got)
		}
		published := "published"
		republished, err := service.UpdateArticle(ctx, admin, UpdateArticleInput{ID: article.ID, Status: &published})
		if err != nil {
			t.Fatal(err)
		}
		if got := count(); got != 1 {
			t.Fatalf("событий после публикации=%d, ожидалось 1", got)
		}
		var payload struct {
			Payload struct {
				Title   string          `json:"title"`
				Version int32           `json:"version"`
				Content json.RawMessage `json:"content"`
			} `json:"payload"`
		}
		var body []byte
		if err := pool.QueryRow(ctx, `SELECT payload FROM outbox WHERE aggregate_id=$1 AND subject='teamos.kb.article.updated.v1'`, article.ID).Scan(&body); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Fatal(err)
		}
		if payload.Payload.Title != republished.Title || payload.Payload.Version != republished.Version || !jsonEqual(payload.Payload.Content, republished.Content) {
			t.Fatalf("событие не содержит текущую публикацию: %s", body)
		}
		title = "Опубликованный текст"
		if _, err := service.UpdateArticle(ctx, admin, UpdateArticleInput{ID: article.ID, Title: &title}); err != nil {
			t.Fatal(err)
		}
		if got := count(); got != 2 {
			t.Fatalf("событий после правки публикации=%d, ожидалось 2", got)
		}
	})
	t.Run("missing and foreign section return not found", func(t *testing.T) {
		assertKind(t, "отсутствующий раздел", func() error { return service.DeleteSection(ctx, admin, uuid.New()) }, ErrorNotFound)
		other := admin
		other.CompanyID = uuid.New()
		assertKind(t, "раздел другой компании", func() error { return service.DeleteSection(ctx, other, section.ID) }, ErrorNotFound)
		empty, err := service.CreateSection(ctx, admin, CreateSectionInput{Name: "Пустой"})
		if err != nil {
			t.Fatal(err)
		}
		if err := service.DeleteSection(ctx, admin, empty.ID); err != nil {
			t.Fatal(err)
		}
		assertKind(t, "повторное удаление", func() error { return service.DeleteSection(ctx, admin, empty.ID) }, ErrorNotFound)
	})
}
