//go:build integration

package application

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestAcademyCatalogFollowsPublicationAndCourseVisibility(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	pool := externalQuizTestPool(t, ctx)
	service, err := NewService(pool, nil, nil, nil)
	if err != nil {
		t.Fatalf("создание Academy service: %v", err)
	}

	now := time.Date(2026, 7, 28, 12, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return now }
	companyID, ownerID := uuid.New(), uuid.New()
	employee := Actor{CompanyID: companyID, UserID: uuid.New(), Role: "employee"}
	owner := Actor{CompanyID: companyID, UserID: ownerID, Role: "owner"}

	companyCourseID := seedCatalogCourse(
		t, ctx, pool, companyID, ownerID, "Курс для компании", "published", "company", now,
	)
	restrictedCourseID := seedCatalogCourse(
		t, ctx, pool, companyID, ownerID, "Курс по назначению", "published", "restricted", now,
	)
	for _, visibility := range []string{"restricted", "company", "public"} {
		seedCatalogCourse(
			t, ctx, pool, companyID, ownerID, "Черновик "+visibility, "draft", visibility, now,
		)
	}

	t.Run("опубликованный company-курс доступен сотруднику, остальные скрыты", func(t *testing.T) {
		page := getCatalogPage(t, ctx, service, employee)
		assertCatalogContainsExactly(t, page, companyCourseID)
		if catalogContains(page, restrictedCourseID) {
			t.Fatal("restricted-курс без назначения появился в каталоге")
		}
	})

	t.Run("черновики не появляются независимо от visibility", func(t *testing.T) {
		page := getCatalogPage(t, ctx, service, employee)
		if page.Total != 1 || len(page.Items) != 1 {
			t.Fatalf("каталог содержит черновик: total=%d items=%+v", page.Total, page.Items)
		}
	})

	t.Run("смена visibility опубликованного курса сразу обновляет каталог", func(t *testing.T) {
		restricted := "restricted"
		updated, updateErr := service.UpdateCourse(ctx, owner, UpdateCourseInput{
			ID: companyCourseID, Visibility: &restricted,
		})
		if updateErr != nil {
			t.Fatalf("смена visibility на restricted: %v", updateErr)
		}
		if updated.Visibility != restricted {
			t.Fatalf("UpdateCourse вернул visibility=%q, ожидалось %q", updated.Visibility, restricted)
		}
		assertCatalogContainsExactly(t, getCatalogPage(t, ctx, service, employee))

		public := "public"
		updated, updateErr = service.UpdateCourse(ctx, owner, UpdateCourseInput{
			ID: companyCourseID, Visibility: &public,
		})
		if updateErr != nil {
			t.Fatalf("смена visibility на public: %v", updateErr)
		}
		if updated.Visibility != public {
			t.Fatalf("UpdateCourse вернул visibility=%q, ожидалось %q", updated.Visibility, public)
		}
		assertCatalogContainsExactly(t, getCatalogPage(t, ctx, service, employee), companyCourseID)

		company := "company"
		updated, updateErr = service.UpdateCourse(ctx, owner, UpdateCourseInput{
			ID: companyCourseID, Visibility: &company,
		})
		if updateErr != nil {
			t.Fatalf("смена visibility на company: %v", updateErr)
		}
		if updated.Visibility != company {
			t.Fatalf("UpdateCourse вернул visibility=%q, ожидалось %q", updated.Visibility, company)
		}
		assertCatalogContainsExactly(t, getCatalogPage(t, ctx, service, employee), companyCourseID)
	})
}

func seedCatalogCourse(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	companyID, authorID uuid.UUID,
	title, status, visibility string,
	now time.Time,
) uuid.UUID {
	t.Helper()

	courseID, versionID := uuid.New(), uuid.New()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("начало подготовки курса каталога: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err = tx.Exec(ctx, `
		INSERT INTO courses (
			id, company_id, title, status, author_id, visibility,
			owner_type, created_by_id, lifecycle_status, distribution_status,
			created_at, updated_at
		) VALUES ($1,$2,$3,$4,$5,$6,'company',$5,'active','active',$7,$7)`,
		courseID, companyID, title, status, authorID, visibility, now,
	); err != nil {
		t.Fatalf("подготовка курса каталога: %v", err)
	}

	if status == "published" {
		if _, err = tx.Exec(ctx, `
			INSERT INTO course_versions (
				id, company_id, course_id, number, status, title, sequential,
				created_by_id, created_at, published_by_id, published_at, content_hash
			) VALUES ($1,$2,$3,1,'published',$4,true,$5,$6,$5,$6,repeat('a',64));
			UPDATE courses SET latest_published_version_id=$1 WHERE company_id=$2 AND id=$3`,
			versionID, companyID, courseID, title, authorID, now,
		); err != nil {
			t.Fatalf("подготовка опубликованной версии курса: %v", err)
		}
	} else {
		if _, err = tx.Exec(ctx, `
			INSERT INTO course_versions (
				id, company_id, course_id, number, status, title, sequential,
				created_by_id, created_at
			) VALUES ($1,$2,$3,1,'draft',$4,true,$5,$6);
			UPDATE courses SET current_draft_version_id=$1 WHERE company_id=$2 AND id=$3`,
			versionID, companyID, courseID, title, authorID, now,
		); err != nil {
			t.Fatalf("подготовка черновой версии курса: %v", err)
		}
	}

	if err = tx.Commit(ctx); err != nil {
		t.Fatalf("фиксация курса каталога: %v", err)
	}
	return courseID
}

func getCatalogPage(
	t *testing.T,
	ctx context.Context,
	service *Service,
	actor Actor,
) CatalogPage {
	t.Helper()
	page, err := service.GetAcademyCatalog(ctx, actor, CatalogQuery{Page: 1, PageSize: 20})
	if err != nil {
		t.Fatalf("получение каталога: %v", err)
	}
	return page
}

func assertCatalogContainsExactly(t *testing.T, page CatalogPage, want ...uuid.UUID) {
	t.Helper()
	if page.Total != int64(len(want)) || len(page.Items) != len(want) {
		t.Fatalf("каталог: total=%d items=%+v, ожидались id=%v", page.Total, page.Items, want)
	}
	for _, id := range want {
		if !catalogContains(page, id) {
			t.Fatalf("курс %s отсутствует в каталоге: %+v", id, page.Items)
		}
	}
}

func catalogContains(page CatalogPage, courseID uuid.UUID) bool {
	for _, item := range page.Items {
		if item.ID == courseID {
			return true
		}
	}
	return false
}
