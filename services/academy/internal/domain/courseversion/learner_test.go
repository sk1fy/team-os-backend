package courseversion

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestLearnerViewNeverExposesCorrectAnswers(t *testing.T) {
	t.Parallel()

	quiz := validQuiz("quiz-1")
	quiz.Questions = append(quiz.Questions, Question{
		ID: "question-2", Type: QuestionMultiple, Text: "Несколько ответов",
		Options: []Option{
			{ID: "option-3", Text: "Первый", Correct: true},
			{ID: "option-4", Text: "Второй", Correct: true},
			{ID: "option-5", Text: "Третий", Correct: false},
		},
	})

	view := quiz.LearnerView()
	encoded, err := json.Marshal(view)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	if strings.Contains(string(encoded), "correct") {
		t.Fatalf("learner JSON leaked correct answers: %s", encoded)
	}
	if len(view.Questions) != 2 || len(view.Questions[1].Options) != 3 {
		t.Fatalf("learner view lost renderable questions: %#v", view)
	}
	if view.Questions[0].Options[0].Text != "Верно" {
		t.Fatalf("option text = %q", view.Questions[0].Options[0].Text)
	}
}

func TestLearnerViewIsIndependent(t *testing.T) {
	t.Parallel()

	quiz := validQuiz("quiz-1")
	view := quiz.LearnerView()
	view.Questions[0].Text = "Изменено"
	view.Questions[0].Options[0].Text = "Изменено"
	if quiz.Questions[0].Text == "Изменено" || quiz.Questions[0].Options[0].Text == "Изменено" {
		t.Fatal("LearnerView() shares mutable slices with authoring quiz")
	}
}
