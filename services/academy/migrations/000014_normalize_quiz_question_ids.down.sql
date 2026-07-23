DROP TRIGGER IF EXISTS course_template_quiz_normalize_ids
    ON course_template_version_quizzes;
DROP TRIGGER IF EXISTS course_version_quiz_normalize_ids
    ON course_version_quizzes;
DROP FUNCTION IF EXISTS academy_normalize_quiz_ids_trigger();
DROP FUNCTION IF EXISTS academy_normalize_quiz_question_ids(uuid, jsonb);
