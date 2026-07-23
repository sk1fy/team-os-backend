-- name: GetCourseVersionQuizForEnrollment :one
SELECT quiz.id, quiz.company_id, quiz.course_version_id,
    quiz.lesson_version_id, quiz.questions, quiz.passing_score,
    quiz.max_attempts, enrollment.course_id, enrollment.learner_type,
    enrollment.user_id, enrollment.external_learner_id,
    enrollment.progress_status, enrollment.access_status,
    enrollment.activated_at, enrollment.access_until
FROM course_enrollments AS enrollment
JOIN course_version_quizzes AS quiz
  ON quiz.company_id = enrollment.company_id
 AND quiz.course_version_id = enrollment.course_version_id
WHERE enrollment.company_id = sqlc.arg(company_id)
  AND enrollment.id = sqlc.arg(enrollment_id)
  AND quiz.id = sqlc.arg(quiz_version_id);

-- name: CountEnrollmentQuizAttempts :one
SELECT count(*)::integer
FROM quiz_attempts
WHERE company_id = sqlc.arg(company_id)
  AND enrollment_id = sqlc.arg(enrollment_id)
  AND quiz_version_id = sqlc.arg(quiz_version_id);

-- name: CreateEnrollmentQuizAttempt :one
INSERT INTO quiz_attempts (
    id, company_id, quiz_id, user_id, enrollment_id, quiz_version_id,
    answers, score, passed, pending_review, created_at
)
SELECT sqlc.arg(id), enrollment.company_id, legacy_quiz.id,
       CASE WHEN legacy_quiz.id IS NOT NULL THEN enrollment.user_id END,
       enrollment.id, quiz.id, sqlc.arg(answers), sqlc.arg(score),
       sqlc.arg(passed), sqlc.arg(pending_review), sqlc.arg(created_at)
FROM course_enrollments AS enrollment
JOIN course_version_quizzes AS quiz
  ON quiz.company_id = enrollment.company_id
 AND quiz.course_version_id = enrollment.course_version_id
LEFT JOIN quizzes AS legacy_quiz
  ON legacy_quiz.company_id = enrollment.company_id
 AND legacy_quiz.id = quiz.id
 AND enrollment.learner_type = 'user'
WHERE enrollment.company_id = sqlc.arg(company_id)
  AND enrollment.id = sqlc.arg(enrollment_id)
  AND quiz.id = sqlc.arg(quiz_version_id)
RETURNING id, company_id, enrollment_id, quiz_version_id, answers,
    score, passed, pending_review, reviewed_by_id, reviewed_at,
    review_comment, created_at;

-- name: ListEnrollmentQuizAttempts :many
SELECT id, company_id, enrollment_id, quiz_version_id, answers,
    score, passed, pending_review, reviewed_by_id, reviewed_at,
    review_comment, created_at
FROM quiz_attempts
WHERE company_id = sqlc.arg(company_id)
  AND enrollment_id = sqlc.arg(enrollment_id)
ORDER BY created_at DESC, id DESC;

-- name: GetEnrollmentQuizAttempt :one
SELECT id, company_id, enrollment_id, quiz_version_id, answers,
    score, passed, pending_review, reviewed_by_id, reviewed_at,
    review_comment, created_at
FROM quiz_attempts
WHERE company_id = sqlc.arg(company_id)
  AND enrollment_id = sqlc.arg(enrollment_id)
  AND id = sqlc.arg(id);

-- name: ReviewEnrollmentQuizAttempt :one
UPDATE quiz_attempts
SET pending_review = false,
    passed = sqlc.arg(passed),
    score = sqlc.arg(score),
    reviewed_by_id = sqlc.arg(reviewed_by_id),
    reviewed_at = sqlc.arg(reviewed_at),
    review_comment = sqlc.narg(review_comment)
WHERE company_id = sqlc.arg(company_id)
  AND enrollment_id = sqlc.arg(enrollment_id)
  AND id = sqlc.arg(id)
RETURNING id, company_id, enrollment_id, quiz_version_id, answers,
    score, passed, pending_review, reviewed_by_id, reviewed_at,
    review_comment, created_at;
