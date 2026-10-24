package model

import (
	scenariosv1 "github.com/nexteggapp/taxcalcs/scenarios/v1"
)

// Question is one question a model's expansion raises, before it becomes a
// proto.
type Question struct {
	id, at, question, effect, answerPath string
	answers                              []string
}

// NewQuestion returns a question. id is stable across scenarios; at is the
// path of the item or model it is about; answerPath is where an answer goes
// in the scenario's inputs.
func NewQuestion(id, at, question, effect, answerPath string, answers ...string) Question {
	return Question{id: id, at: at, question: question, effect: effect, answerPath: answerPath, answers: append([]string(nil), answers...)}
}

// ID is the question's stable id.
func (q Question) ID() string { return q.id }

// Proto returns the question as the result carries it.
func (q Question) Proto() *scenariosv1.Question {
	var answers []*scenariosv1.Statement
	for _, a := range q.answers {
		answers = append(answers, &scenariosv1.Statement{Text: a})
	}
	return &scenariosv1.Question{
		Id:         q.id,
		At:         q.at,
		Question:   &scenariosv1.Statement{Text: q.question},
		Effect:     &scenariosv1.Statement{Text: q.effect},
		AnswerPath: q.answerPath,
		Answers:    answers,
	}
}

// Notes collects what an expansion asks and what it leaves out, each once.
type Notes struct {
	questions []Question
	left      []string
	seen      map[string]bool
}

// Ask adds a question, once per id.
func (n *Notes) Ask(q Question) {
	if n.seen == nil {
		n.seen = map[string]bool{}
	}
	if n.seen["q:"+q.id] {
		return
	}
	n.seen["q:"+q.id] = true
	n.questions = append(n.questions, q)
}

// LeaveOut records something the figures leave out, once per text.
func (n *Notes) LeaveOut(text string) {
	if n.seen == nil {
		n.seen = map[string]bool{}
	}
	if n.seen["n:"+text] {
		return
	}
	n.seen["n:"+text] = true
	n.left = append(n.left, text)
}

// Merge adds another's notes to n.
func (n *Notes) Merge(o *Notes) {
	for _, q := range o.questions {
		n.Ask(q)
	}
	for _, t := range o.left {
		n.LeaveOut(t)
	}
}

// Questions returns the questions in the order asked.
func (n *Notes) Questions() []Question { return append([]Question(nil), n.questions...) }

// LeftOut returns what the figures leave out, in the order recorded.
func (n *Notes) LeftOut() []string { return append([]string(nil), n.left...) }
