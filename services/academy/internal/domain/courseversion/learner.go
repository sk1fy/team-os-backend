package courseversion

// LearnerQuiz is a safe learner-facing projection. It has no field capable of
// exposing the authoring correct-answer flags.
type LearnerQuiz struct {
	ID           ID                `json:"id"`
	Questions    []LearnerQuestion `json:"questions"`
	PassingScore int               `json:"passingScore"`
	MaxAttempts  *int              `json:"maxAttempts,omitempty"`
}

// LearnerQuestion is a question without assessment secrets.
type LearnerQuestion struct {
	ID      ID              `json:"id"`
	Type    QuestionType    `json:"type"`
	Text    string          `json:"text"`
	Options []LearnerOption `json:"options"`
}

// LearnerOption contains only information needed to render an answer choice.
type LearnerOption struct {
	ID   ID     `json:"id"`
	Text string `json:"text"`
}

// LearnerView strips correct answers by construction and returns independent
// slices/pointers suitable for serialization.
func (quiz Quiz) LearnerView() LearnerQuiz {
	result := LearnerQuiz{
		ID:           quiz.ID,
		Questions:    make([]LearnerQuestion, len(quiz.Questions)),
		PassingScore: quiz.PassingScore,
		MaxAttempts:  cloneInt(quiz.MaxAttempts),
	}
	for questionIndex, question := range quiz.Questions {
		learnerQuestion := LearnerQuestion{
			ID:      question.ID,
			Type:    question.Type,
			Text:    question.Text,
			Options: make([]LearnerOption, len(question.Options)),
		}
		for optionIndex, option := range question.Options {
			learnerQuestion.Options[optionIndex] = LearnerOption{
				ID:   option.ID,
				Text: option.Text,
			}
		}
		result.Questions[questionIndex] = learnerQuestion
	}
	return result
}
