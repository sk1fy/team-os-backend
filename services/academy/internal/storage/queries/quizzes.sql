-- name: GetQuizzes :many
SELECT id, company_id, lesson_id, questions, passing_score, max_attempts
FROM quizzes
WHERE company_id = $1
ORDER BY id;

-- name: GetLessonQuizzes :many
SELECT id, company_id, lesson_id, questions, passing_score, max_attempts
FROM quizzes
WHERE company_id = $1 AND lesson_id = $2
ORDER BY id;

-- name: GetQuizzesByCourseIds :many
SELECT q.id, q.company_id, q.lesson_id, q.questions, q.passing_score, q.max_attempts
FROM quizzes q
JOIN lessons l ON l.id = q.lesson_id AND l.company_id = q.company_id
WHERE q.company_id = $1 AND l.course_id = ANY(sqlc.arg(course_ids)::uuid[])
ORDER BY l.course_id, q.id;

-- name: GetQuiz :one
SELECT id, company_id, lesson_id, questions, passing_score, max_attempts
FROM quizzes
WHERE company_id = $1 AND id = $2;

-- name: CreateQuiz :one
INSERT INTO quizzes (id, company_id, lesson_id, questions, passing_score, max_attempts)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING id, company_id, lesson_id, questions, passing_score, max_attempts;

-- name: UpdateQuiz :one
UPDATE quizzes
SET questions = $3, passing_score = $4, max_attempts = $5
WHERE company_id = $1 AND id = $2
RETURNING id, company_id, lesson_id, questions, passing_score, max_attempts;

-- name: GetQuizAttempts :many
SELECT id, company_id, quiz_id, user_id,
    score, passed, pending_review, created_at
FROM quiz_attempts
WHERE company_id = $1 AND quiz_id IS NOT NULL AND user_id IS NOT NULL
ORDER BY created_at, id;

-- name: GetQuizAttemptsWithCourse :many
SELECT qa.id, qa.company_id, qa.quiz_id, qa.user_id, qa.score, qa.passed,
    qa.pending_review, qa.created_at, l.course_id
FROM quiz_attempts qa
JOIN quizzes q ON q.id = qa.quiz_id
JOIN lessons l ON l.id = q.lesson_id
WHERE qa.company_id = $1 AND qa.user_id IS NOT NULL
ORDER BY qa.created_at, qa.id;
