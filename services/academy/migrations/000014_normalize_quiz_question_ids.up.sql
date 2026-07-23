CREATE FUNCTION academy_normalize_quiz_question_ids(
    p_quiz_id uuid,
    p_questions jsonb
)
RETURNS jsonb
LANGUAGE sql
IMMUTABLE
AS $$
    SELECT COALESCE(
        jsonb_agg(
            jsonb_set(
                jsonb_set(
                    question.value,
                    '{id}',
                    to_jsonb(
                        CASE
                            WHEN COALESCE(question.value ->> 'id', '') ~*
                                '^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$'
                            THEN question.value ->> 'id'
                            ELSE md5(
                                p_quiz_id::text || ':question:' ||
                                question.ordinality::text
                            )::uuid::text
                        END
                    ),
                    true
                ),
                '{options}',
                COALESCE((
                    SELECT jsonb_agg(
                        jsonb_set(
                            option.value,
                            '{id}',
                            to_jsonb(
                                CASE
                                    WHEN COALESCE(option.value ->> 'id', '') ~*
                                        '^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$'
                                    THEN option.value ->> 'id'
                                    ELSE md5(
                                        p_quiz_id::text || ':question:' ||
                                        question.ordinality::text || ':option:' ||
                                        option.ordinality::text
                                    )::uuid::text
                                END
                            ),
                            true
                        )
                        ORDER BY option.ordinality
                    )
                    FROM jsonb_array_elements(
                        COALESCE(question.value -> 'options', '[]'::jsonb)
                    ) WITH ORDINALITY AS option(value, ordinality)
                ), '[]'::jsonb),
                true
            )
            ORDER BY question.ordinality
        ),
        '[]'::jsonb
    )
    FROM jsonb_array_elements(COALESCE(p_questions, '[]'::jsonb))
        WITH ORDINALITY AS question(value, ordinality)
$$;

CREATE FUNCTION academy_normalize_quiz_ids_trigger()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    NEW.questions := academy_normalize_quiz_question_ids(NEW.id, NEW.questions);
    RETURN NEW;
END
$$;

CREATE TRIGGER course_template_quiz_normalize_ids
BEFORE INSERT OR UPDATE OF questions
ON course_template_version_quizzes
FOR EACH ROW
EXECUTE FUNCTION academy_normalize_quiz_ids_trigger();

CREATE TRIGGER course_version_quiz_normalize_ids
BEFORE INSERT OR UPDATE OF questions
ON course_version_quizzes
FOR EACH ROW
EXECUTE FUNCTION academy_normalize_quiz_ids_trigger();

-- Repair system templates already created by migration 000008.
ALTER TABLE course_template_version_quizzes
    DISABLE TRIGGER course_template_quizzes_immutable_trigger;
UPDATE course_template_version_quizzes
SET questions = questions;
ALTER TABLE course_template_version_quizzes
    ENABLE TRIGGER course_template_quizzes_immutable_trigger;

-- Repair courses instantiated from affected system templates.
ALTER TABLE course_version_quizzes
    DISABLE TRIGGER course_version_quizzes_immutable_trigger;
UPDATE course_version_quizzes
SET questions = questions;
ALTER TABLE course_version_quizzes
    ENABLE TRIGGER course_version_quizzes_immutable_trigger;
