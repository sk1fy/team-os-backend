//go:build integration

package application

import (
	"context"
	"errors"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	eventsv1 "github.com/sk1fy/team-os-backend/contracts/gen/go/events/v1"
	"github.com/sk1fy/team-os-backend/pkg/eventbus"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/structpb"
)

func TestAcademyAuthorizationConsumersAndOrdering(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	pool := academyTestPool(t, ctx)
	service, err := NewService(pool, nil, nil, nil)
	if err != nil {
		t.Fatalf("создание сервиса: %v", err)
	}

	companyID, otherCompanyID := uuid.New(), uuid.New()
	managerID, partnerID := uuid.New(), uuid.New()
	assignedCourseID, hiddenCourseID := uuid.New(), uuid.New()
	assignedSectionID, hiddenSectionID := uuid.New(), uuid.New()
	assignedLessonID, hiddenLessonID := uuid.New(), uuid.New()
	assignedQuizID, hiddenQuizID := uuid.New(), uuid.New()
	seedAcademyCourse(t, ctx, pool, companyID, managerID, assignedCourseID, assignedSectionID, assignedLessonID, assignedQuizID)
	seedAcademyCourse(t, ctx, pool, companyID, managerID, hiddenCourseID, hiddenSectionID, hiddenLessonID, hiddenQuizID)
	if _, err = pool.Exec(ctx, `
		INSERT INTO assignments (
			id, company_id, course_id, course_version_id, assignee_type, assignee_id,
			resolved_user_ids, assigned_by_id
		) VALUES ($1, $2, $3, $3, 'user', $4, ARRAY[$4]::uuid[], $5)`,
		uuid.New(), companyID, assignedCourseID, partnerID, managerID); err != nil {
		t.Fatalf("назначение партнёру: %v", err)
	}
	partner := Actor{CompanyID: companyID, UserID: partnerID, Role: "partner"}
	manager := Actor{CompanyID: companyID, UserID: managerID, Role: "admin"}

	t.Run("company created создаёт системные шаблоны идемпотентно", func(t *testing.T) {
		event := academyEvent(t, companyID, &eventsv1.CompanyCreatedPayload{
			CompanyId: companyID.String(), Name: "Тестовая компания", OwnerUserId: managerID.String(),
		})
		processed, handleErr := service.HandleCompanyCreated(ctx, event)
		if handleErr != nil || !processed {
			t.Fatalf("company.created: processed=%v err=%v", processed, handleErr)
		}
		processed, handleErr = service.HandleCompanyCreated(ctx, event)
		if handleErr != nil || processed {
			t.Fatalf("повтор company.created: processed=%v err=%v", processed, handleErr)
		}
		var templates int
		if queryErr := pool.QueryRow(ctx, `
			SELECT count(*) FROM course_templates
			WHERE company_id=$1 AND template_type='system'`, companyID).Scan(&templates); queryErr != nil || templates != 10 {
			t.Fatalf("системные шаблоны: count=%d err=%v", templates, queryErr)
		}
	})

	t.Run("partner видит только назначенный курс", func(t *testing.T) {
		if _, err = service.GetCourse(ctx, partner, assignedCourseID); err != nil {
			t.Fatalf("назначенный курс недоступен: %v", err)
		}
		hiddenCourse, hiddenCourseErr := service.GetCourse(ctx, partner, hiddenCourseID)
		assertForbidden(t, hiddenCourse, hiddenCourseErr)
		if _, sectionErr := service.GetCourseSections(ctx, partner, hiddenCourseID); sectionErr == nil {
			t.Fatal("разделы неназначенного курса доступны")
		}
		lessons, listErr := service.GetLessons(ctx, partner, nil)
		if listErr != nil || len(lessons) != 1 || lessons[0].CourseID != assignedCourseID {
			t.Fatalf("список уроков партнёра: lessons=%v err=%v", lessons, listErr)
		}
		quizzes, listErr := service.GetQuizzes(ctx, partner, nil)
		if listErr != nil || len(quizzes) != 1 || quizzes[0].ID != assignedQuizID {
			t.Fatalf("список тестов партнёра: quizzes=%v err=%v", quizzes, listErr)
		}
		_, completeErr := service.MarkLessonComplete(ctx, partner, MarkLessonCompleteInput{
			CourseID: hiddenCourseID, LessonID: hiddenLessonID,
		})
		if completeErr == nil {
			t.Fatal("партнёр отметил урок неназначенного курса")
		}
	})

	t.Run("ownership запрещает перекрёстное изменение курсов", func(t *testing.T) {
		created, createErr := service.CreateCourse(ctx, partner, CreateCourseInput{Title: "Курс партнёра"})
		if createErr != nil {
			t.Fatalf("создание курса партнёра: %v", createErr)
		}
		if created.OwnerType != "partner" || created.OwnerUserID == nil || *created.OwnerUserID != partnerID {
			t.Fatalf("неверный владелец курса: %+v", created)
		}
		newTitle := "Обновлённый курс партнёра"
		if _, updateErr := service.UpdateCourse(ctx, partner, UpdateCourseInput{ID: created.ID, Title: &newTitle}); updateErr != nil {
			t.Fatalf("партнёр не изменил свой курс: %v", updateErr)
		}
		if _, updateErr := service.UpdateCourse(ctx, manager, UpdateCourseInput{ID: created.ID, Title: &newTitle}); !isApplicationError(updateErr, ErrorForbidden) {
			t.Fatalf("admin изменил партнёрский оригинал: %v", updateErr)
		}
		if deleteErr := service.DeleteCourse(ctx, manager, created.ID); !isApplicationError(deleteErr, ErrorForbidden) {
			t.Fatalf("admin удалил партнёрский оригинал: %v", deleteErr)
		}
		if _, updateErr := service.UpdateCourse(ctx, partner, UpdateCourseInput{ID: assignedCourseID, Title: &newTitle}); !isApplicationError(updateErr, ErrorForbidden) {
			t.Fatalf("партнёр изменил курс компании: %v", updateErr)
		}
		employeeCourses, listErr := service.GetCourses(ctx, Actor{CompanyID: companyID, UserID: uuid.New(), Role: "employee"})
		if listErr != nil {
			t.Fatalf("список курсов сотрудника: %v", listErr)
		}
		for _, value := range employeeCourses {
			if value.ID == created.ID {
				t.Fatal("сотрудник увидел партнёрский курс")
			}
		}
	})

	t.Run("archive restore и soft delete сохраняют данные", func(t *testing.T) {
		courseID, sectionID := uuid.New(), uuid.New()
		seedAcademyCourse(t, ctx, pool, companyID, managerID, courseID, sectionID, uuid.New(), uuid.New())
		archived, archiveErr := service.ArchiveCourse(ctx, manager, courseID)
		if archiveErr != nil || archived.LifecycleStatus != "archived" || archived.ArchivedAt == nil {
			t.Fatalf("архивирование: course=%+v err=%v", archived, archiveErr)
		}
		restored, restoreErr := service.RestoreCourse(ctx, manager, courseID)
		if restoreErr != nil || restored.LifecycleStatus != "active" || restored.ArchivedAt != nil {
			t.Fatalf("восстановление: course=%+v err=%v", restored, restoreErr)
		}
		if deleteErr := service.DeleteCourse(ctx, manager, courseID); deleteErr != nil {
			t.Fatalf("soft delete: %v", deleteErr)
		}
		var lifecycle string
		if queryErr := pool.QueryRow(ctx, `SELECT lifecycle_status FROM courses WHERE id=$1`, courseID).Scan(&lifecycle); queryErr != nil {
			t.Fatalf("удалённая строка курса не сохранена: %v", queryErr)
		}
		if lifecycle != "deleted" {
			t.Fatalf("lifecycle=%q, want deleted", lifecycle)
		}
		var sectionCount int
		if queryErr := pool.QueryRow(ctx, `SELECT count(*) FROM course_sections WHERE id=$1`, sectionID).Scan(&sectionCount); queryErr != nil || sectionCount != 1 {
			t.Fatalf("soft delete затронул раздел: count=%d err=%v", sectionCount, queryErr)
		}
		if _, getErr := service.GetCourse(ctx, Actor{CompanyID: companyID, UserID: uuid.New(), Role: "employee"}, courseID); !isApplicationError(getErr, ErrorNotFound) {
			t.Fatalf("удалённый курс доступен сотруднику: %v", getErr)
		}
	})

	t.Run("KB событие изолировано по компании", func(t *testing.T) {
		articleID := uuid.New()
		courseID, sectionID, lessonID := uuid.New(), uuid.New(), uuid.New()
		seedAcademyCourse(t, ctx, pool, otherCompanyID, managerID, courseID, sectionID, lessonID, uuid.New())
		draftCourse, createErr := service.CreateCourse(ctx, manager, CreateCourseInput{Title: "Черновик с KB link"})
		if createErr != nil || draftCourse.CurrentDraftVersionID == nil {
			t.Fatalf("создание черновика с KB link: course=%+v err=%v", draftCourse, createErr)
		}
		var draftSectionID uuid.UUID
		if queryErr := pool.QueryRow(ctx, `
			SELECT id FROM course_version_sections
			WHERE company_id=$1 AND course_version_id=$2
			ORDER BY "order", id LIMIT 1`,
			companyID, *draftCourse.CurrentDraftVersionID).Scan(&draftSectionID); queryErr != nil {
			t.Fatalf("раздел черновика с KB link: %v", queryErr)
		}
		sourceType := "kb_link"
		draftLesson, createErr := service.CreateCourseVersionLesson(ctx, manager, CreateCourseVersionLessonInput{
			VersionID: *draftCourse.CurrentDraftVersionID, SectionVersionID: draftSectionID,
			Title: "Исходное название", Content: defaultLessonContent,
			SourceType: &sourceType, SourceArticleID: &articleID,
		})
		if createErr != nil {
			t.Fatalf("создание KB link в черновике: %v", createErr)
		}
		if _, err = pool.Exec(ctx, `
			UPDATE lessons SET source_article_id=$1, source_article_title=title, source_mode='link'
			WHERE id = ANY($2::uuid[])`, articleID, []uuid.UUID{assignedLessonID, lessonID}); err != nil {
			t.Fatalf("link-уроки: %v", err)
		}
		content, _ := structpb.NewStruct(map[string]any{"type": "doc", "content": []any{}})
		event := academyEvent(t, companyID, &eventsv1.KbArticleUpdatedPayload{
			ArticleId: articleID.String(), Version: 2, Title: "Обновлено", Content: content,
		})
		if _, err = service.HandleKbArticleUpdated(ctx, event); err != nil {
			t.Fatalf("обработка KB события: %v", err)
		}
		var ownTitle, otherTitle string
		if err = pool.QueryRow(ctx, `SELECT title FROM lessons WHERE id=$1`, assignedLessonID).Scan(&ownTitle); err != nil {
			t.Fatal(err)
		}
		if err = pool.QueryRow(ctx, `SELECT title FROM lessons WHERE id=$1`, lessonID).Scan(&otherTitle); err != nil {
			t.Fatal(err)
		}
		if ownTitle != "Обновлено" || otherTitle == "Обновлено" {
			t.Fatalf("tenant isolation нарушен: own=%q other=%q", ownTitle, otherTitle)
		}
		var draftTitle string
		var sourceVersion int32
		if err = pool.QueryRow(ctx, `
			SELECT title, source_article_version
			FROM course_version_lessons WHERE id=$1`,
			draftLesson.ID).Scan(&draftTitle, &sourceVersion); err != nil {
			t.Fatalf("чтение обновлённого KB link черновика: %v", err)
		}
		if draftTitle != "Обновлено" || sourceVersion != 2 {
			t.Fatalf("KB link черновика не обновлён: title=%q version=%d", draftTitle, sourceVersion)
		}
	})

	t.Run("org события поддерживают снимки назначений", func(t *testing.T) {
		userID, positionID, departmentID := uuid.New(), uuid.New(), uuid.New()
		positionAssignmentID, departmentAssignmentID, userAssignmentID := uuid.New(), uuid.New(), uuid.New()
		if _, err = pool.Exec(ctx, `
			INSERT INTO assignments (id, company_id, course_id, course_version_id, assignee_type, assignee_id, resolved_user_ids, assigned_by_id)
			VALUES
			($1,$2,$3,$3,'position',$4,'{}',$7),
			($5,$2,$3,$3,'department',$6,'{}',$7),
			($8,$2,$3,$3,'user',$9,'{}',$7)`,
			positionAssignmentID, companyID, assignedCourseID, positionID,
			departmentAssignmentID, departmentID, managerID, userAssignmentID, userID); err != nil {
			t.Fatalf("назначения org: %v", err)
		}
		created := academyEvent(t, companyID, &eventsv1.OrgUserCreatedPayload{User: &eventsv1.OrgUserSnapshot{
			UserId: userID.String(), Status: eventsv1.OrgUserStatus_ORG_USER_STATUS_ACTIVE,
			PositionIds: []string{positionID.String()}, DepartmentIds: []string{departmentID.String()},
		}})
		if _, err = service.HandleOrgUserCreated(ctx, created); err != nil {
			t.Fatalf("org.user.created: %v", err)
		}
		assertAssignmentContains(t, ctx, pool, positionAssignmentID, userID, true)
		assertAssignmentContains(t, ctx, pool, departmentAssignmentID, userID, true)
		assertAssignmentContains(t, ctx, pool, userAssignmentID, userID, true)

		deactivated := academyEvent(t, companyID, &eventsv1.OrgUserDeactivatedPayload{UserId: userID.String()})
		if _, err = service.HandleOrgUserDeactivated(ctx, deactivated); err != nil {
			t.Fatalf("org.user.deactivated: %v", err)
		}
		assertAssignmentContains(t, ctx, pool, positionAssignmentID, userID, false)
		assertAssignmentContains(t, ctx, pool, departmentAssignmentID, userID, false)
		assertAssignmentContains(t, ctx, pool, userAssignmentID, userID, false)
	})

	t.Run("встречные перемещения уроков не создают deadlock", func(t *testing.T) {
		courseID, firstSectionID, secondSectionID := uuid.New(), uuid.New(), uuid.New()
		firstLessonID, secondLessonID := uuid.New(), uuid.New()
		if _, err = pool.Exec(ctx,
			`INSERT INTO courses (id,company_id,title,status,author_id) VALUES ($1,$2,'Курс','published',$3)`,
			courseID, companyID, managerID); err != nil {
			t.Fatalf("подготовка курса для перемещений: %v", err)
		}
		if _, err = pool.Exec(ctx, `
			INSERT INTO course_sections (id,company_id,course_id,title,"order") VALUES
			($1,$2,$3,'A',0),($4,$2,$3,'B',1)`,
			firstSectionID, companyID, courseID, secondSectionID); err != nil {
			t.Fatalf("подготовка разделов для перемещений: %v", err)
		}
		if _, err = pool.Exec(ctx, `
			INSERT INTO lessons (id,company_id,course_id,section_id,title,"order",content) VALUES
			($1,$2,$3,$4,'A',0,'{"type":"doc"}'),($5,$2,$3,$6,'B',0,'{"type":"doc"}')`,
			firstLessonID, companyID, courseID, firstSectionID, secondLessonID, secondSectionID); err != nil {
			t.Fatalf("подготовка уроков для перемещений: %v", err)
		}
		start := make(chan struct{})
		errorsChannel := make(chan error, 2)
		var wait sync.WaitGroup
		for _, move := range []MoveLessonInput{
			{ID: firstLessonID, SectionID: secondSectionID, Order: 0},
			{ID: secondLessonID, SectionID: firstSectionID, Order: 0},
		} {
			wait.Add(1)
			go func(input MoveLessonInput) {
				defer wait.Done()
				<-start
				_, moveErr := service.MoveLesson(ctx, manager, input)
				errorsChannel <- moveErr
			}(move)
		}
		close(start)
		wait.Wait()
		close(errorsChannel)
		for moveErr := range errorsChannel {
			if moveErr != nil {
				t.Fatalf("перемещение: %v", moveErr)
			}
		}
	})
}

func academyTestPool(t *testing.T, ctx context.Context) *pgxpool.Pool {
	t.Helper()
	testcontainers.SkipIfProviderIsNotHealthy(t)
	_, filename, _, _ := runtime.Caller(0)
	migrationsDir := filepath.Join(filepath.Dir(filename), "..", "..", "migrations")
	container, err := postgres.Run(ctx, "postgres:16-alpine",
		postgres.WithDatabase("academy"), postgres.WithUsername("academy"),
		postgres.WithPassword("academy"), postgres.WithInitScripts(
			filepath.Join(migrationsDir, "000001_init.up.sql"),
			filepath.Join(migrationsDir, "000002_assignment_events_and_outbox.up.sql"),
			filepath.Join(migrationsDir, "000003_course_visibility_assignment_idempotency.up.sql"),
			filepath.Join(migrationsDir, "000004_course_ownership_lifecycle_audit.up.sql"),
			filepath.Join(migrationsDir, "000005_immutable_course_versions.up.sql"),
			filepath.Join(migrationsDir, "000006_version_pinned_enrollments.up.sql"),
			filepath.Join(migrationsDir, "000007_partner_courses_and_restrictions.up.sql"),
			filepath.Join(migrationsDir, "000008_course_templates_and_kb_snapshots.up.sql"),
			filepath.Join(migrationsDir, "000009_external_learners_and_personal_accesses.up.sql"),
			filepath.Join(migrationsDir, "000010_external_campaigns_and_analytics.up.sql"),
			filepath.Join(migrationsDir, "000011_self_enrollment.up.sql"),
			filepath.Join(migrationsDir, "000012_external_quiz_attempts.up.sql"),
			filepath.Join(migrationsDir, "000013_enrollment_mutation_idempotency.up.sql"),
			filepath.Join(migrationsDir, "000014_normalize_quiz_question_ids.up.sql"),
			filepath.Join(migrationsDir, "000015_course_partner_audience.up.sql"),
		),
		postgres.BasicWaitStrategies(),
	)
	if err != nil {
		t.Fatalf("запуск PostgreSQL: %v", err)
	}
	t.Cleanup(func() { _ = testcontainers.TerminateContainer(container) })
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

func isApplicationError(err error, kind ErrorKind) bool {
	var applicationErr *Error
	return errors.As(err, &applicationErr) && applicationErr.Kind == kind
}

func seedAcademyCourse(
	t *testing.T, ctx context.Context, pool *pgxpool.Pool,
	companyID, authorID, courseID, sectionID, lessonID, quizID uuid.UUID,
) {
	t.Helper()
	if _, err := pool.Exec(ctx,
		`INSERT INTO courses (id,company_id,title,status,author_id) VALUES ($1,$2,'Курс','published',$3)`,
		courseID, companyID, authorID); err != nil {
		t.Fatalf("подготовка курса: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO course_sections (id,company_id,course_id,title,"order") VALUES ($1,$2,$3,'Раздел',0)`,
		sectionID, companyID, courseID); err != nil {
		t.Fatalf("подготовка раздела курса: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO lessons (id,company_id,course_id,section_id,title,"order",content)
		VALUES ($1,$2,$3,$4,'Урок',0,'{"type":"doc"}')`,
		lessonID, companyID, courseID, sectionID); err != nil {
		t.Fatalf("подготовка урока: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO quizzes (id,company_id,lesson_id,questions,passing_score) VALUES ($1,$2,$3,'[]',50)`,
		quizID, companyID, lessonID); err != nil {
		t.Fatalf("подготовка теста: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO course_versions (
			id, company_id, course_id, number, status, title, created_by_id
		) VALUES ($1,$2,$1,1,'draft','Курс',$3);
		INSERT INTO course_version_sections (
			id, company_id, course_version_id, stable_key, title, "order"
		) VALUES ($4,$2,$1,$4,'Раздел',0);
			INSERT INTO course_version_lessons (
				id, company_id, course_version_id, section_version_id, stable_key,
				title, "order", content
			) VALUES ($5,$2,$1,$4,$5,'Урок',0,'{"type":"doc"}');
			INSERT INTO course_version_quizzes (
				id, company_id, course_version_id, lesson_version_id, questions, passing_score
			) VALUES ($6,$2,$1,$5,'[]',50);
			UPDATE course_version_lessons SET quiz_version_id=$6 WHERE id=$5;
			UPDATE course_versions
			SET status='published', published_by_id=$3, published_at=now(), content_hash=encode(digest($1::text,'sha256'),'hex')
			WHERE id=$1;
			UPDATE courses SET latest_published_version_id=$1 WHERE id=$1`,
		pgx.QueryExecModeSimpleProtocol,
		courseID, companyID, authorID, sectionID, lessonID, quizID); err != nil {
		t.Fatalf("подготовка опубликованной версии курса: %v", err)
	}
}

func academyEvent(t *testing.T, companyID uuid.UUID, payload proto.Message) eventbus.Event {
	t.Helper()
	encoded, err := protojson.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	return eventbus.Event{
		EventID: uuid.NewString(), OccurredAt: time.Now().UTC(),
		CompanyID: companyID.String(), ActorID: uuid.NewString(), Payload: encoded,
	}
}

func assertForbidden(t *testing.T, _ Course, err error) {
	t.Helper()
	var applicationErr *Error
	if !errors.As(err, &applicationErr) || applicationErr.Kind != ErrorForbidden {
		t.Fatalf("ожидался forbidden, получено %v", err)
	}
}

func assertAssignmentContains(
	t *testing.T, ctx context.Context, pool *pgxpool.Pool,
	assignmentID, userID uuid.UUID, want bool,
) {
	t.Helper()
	var contains bool
	if err := pool.QueryRow(ctx, `SELECT $2 = ANY(resolved_user_ids) FROM assignments WHERE id=$1`, assignmentID, userID).
		Scan(&contains); err != nil {
		t.Fatal(err)
	}
	if contains != want {
		t.Fatalf("assignment %s contains=%v, want=%v", assignmentID, contains, want)
	}
}
