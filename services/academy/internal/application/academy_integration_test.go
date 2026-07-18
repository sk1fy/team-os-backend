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
			id, company_id, course_id, assignee_type, assignee_id,
			resolved_user_ids, assigned_by_id
		) VALUES ($1, $2, $3, 'user', $4, ARRAY[$4]::uuid[], $5)`,
		uuid.New(), companyID, assignedCourseID, partnerID, managerID); err != nil {
		t.Fatalf("назначение партнёру: %v", err)
	}
	partner := Actor{CompanyID: companyID, UserID: partnerID, Role: "partner"}

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

	t.Run("KB событие изолировано по компании", func(t *testing.T) {
		articleID := uuid.New()
		courseID, sectionID, lessonID := uuid.New(), uuid.New(), uuid.New()
		seedAcademyCourse(t, ctx, pool, otherCompanyID, managerID, courseID, sectionID, lessonID, uuid.New())
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
	})

	t.Run("org события поддерживают снимки назначений", func(t *testing.T) {
		userID, positionID, departmentID := uuid.New(), uuid.New(), uuid.New()
		positionAssignmentID, departmentAssignmentID, userAssignmentID := uuid.New(), uuid.New(), uuid.New()
		if _, err = pool.Exec(ctx, `
			INSERT INTO assignments (id, company_id, course_id, assignee_type, assignee_id, resolved_user_ids, assigned_by_id)
			VALUES
			($1,$2,$3,'position',$4,'{}',$7),
			($5,$2,$3,'department',$6,'{}',$7),
			($8,$2,$3,'user',$9,'{}',$7)`,
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
		if _, err = pool.Exec(ctx, `
			INSERT INTO courses (id,company_id,title,status,author_id) VALUES ($1,$2,'Курс','published',$3);
			INSERT INTO course_sections (id,company_id,course_id,title,"order") VALUES
			($4,$2,$1,'A',0),($5,$2,$1,'B',1);
			INSERT INTO lessons (id,company_id,course_id,section_id,title,"order",content) VALUES
			($6,$2,$1,$4,'A',0,'{"type":"doc"}'),($7,$2,$1,$5,'B',0,'{"type":"doc"}')`,
			courseID, companyID, managerID, firstSectionID, secondSectionID, firstLessonID, secondLessonID); err != nil {
			t.Fatalf("подготовка перемещений: %v", err)
		}
		manager := Actor{CompanyID: companyID, UserID: managerID, Role: "admin"}
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
	_, filename, _, _ := runtime.Caller(0)
	migrationsDir := filepath.Join(filepath.Dir(filename), "..", "..", "migrations")
	container, err := postgres.Run(ctx, "postgres:16-alpine",
		postgres.WithDatabase("academy"), postgres.WithUsername("academy"),
		postgres.WithPassword("academy"), postgres.WithInitScripts(
			filepath.Join(migrationsDir, "000001_init.up.sql"),
			filepath.Join(migrationsDir, "000002_assignment_events_and_outbox.up.sql"),
			filepath.Join(migrationsDir, "000003_course_visibility_assignment_idempotency.up.sql"),
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

func seedAcademyCourse(
	t *testing.T, ctx context.Context, pool *pgxpool.Pool,
	companyID, authorID, courseID, sectionID, lessonID, quizID uuid.UUID,
) {
	t.Helper()
	if _, err := pool.Exec(ctx, `
		INSERT INTO courses (id,company_id,title,status,author_id) VALUES ($1,$2,'Курс','published',$3);
		INSERT INTO course_sections (id,company_id,course_id,title,"order") VALUES ($4,$2,$1,'Раздел',0);
		INSERT INTO lessons (id,company_id,course_id,section_id,title,"order",content) VALUES
		($5,$2,$1,$4,'Урок',0,'{"type":"doc"}');
		INSERT INTO quizzes (id,company_id,lesson_id,questions,passing_score) VALUES ($6,$2,$5,'[]',50)`,
		courseID, companyID, authorID, sectionID, lessonID, quizID); err != nil {
		t.Fatalf("подготовка курса: %v", err)
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
