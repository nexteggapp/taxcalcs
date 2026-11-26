//! Questions and notes, ported from Go's `taxyear/model/questions.go`.

use std::collections::HashSet;

use proto::scenariosv1;

/// One question a model's expansion raises, before it becomes a proto.
#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub struct Question {
    id: String,
    at: String,
    question: String,
    effect: String,
    answer_path: String,
    answers: Vec<String>,
}

/// Returns a question. id is stable across scenarios; at is the path of the
/// item or model it is about; answer_path is where an answer goes in the
/// scenario's inputs.
pub fn new_question(
    id: impl Into<String>,
    at: impl Into<String>,
    question: impl Into<String>,
    effect: impl Into<String>,
    answer_path: impl Into<String>,
    answers: &[&str],
) -> Question {
    Question {
        id: id.into(),
        at: at.into(),
        question: question.into(),
        effect: effect.into(),
        answer_path: answer_path.into(),
        answers: answers.iter().map(|a| a.to_string()).collect(),
    }
}

fn statement(text: &str) -> scenariosv1::Statement {
    scenariosv1::Statement {
        text: text.to_string(),
        ..Default::default()
    }
}

impl Question {
    /// The question's stable id.
    pub fn id(&self) -> &str {
        &self.id
    }

    /// Returns the question as the result carries it.
    pub fn proto(&self) -> scenariosv1::Question {
        scenariosv1::Question {
            id: self.id.clone(),
            at: self.at.clone(),
            question: statement(&self.question).into(),
            effect: statement(&self.effect).into(),
            answer_path: self.answer_path.clone(),
            answers: self.answers.iter().map(|a| statement(a)).collect(),
            ..Default::default()
        }
    }
}

/// Collects what an expansion asks and what it leaves out, each once.
#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub struct Notes {
    questions: Vec<Question>,
    left: Vec<String>,
    seen: HashSet<String>,
}

impl Notes {
    /// Adds a question, once per id.
    pub fn ask(&mut self, q: Question) {
        if !self.seen.insert(format!("q:{}", q.id)) {
            return;
        }
        self.questions.push(q);
    }

    /// Records something the figures leave out, once per text.
    pub fn leave_out(&mut self, text: impl Into<String>) {
        let text = text.into();
        if !self.seen.insert(format!("n:{text}")) {
            return;
        }
        self.left.push(text);
    }

    /// Adds another's notes to n.
    pub fn merge(&mut self, o: &Notes) {
        for q in &o.questions {
            self.ask(q.clone());
        }
        for t in &o.left {
            self.leave_out(t.clone());
        }
    }

    /// Returns the questions in the order asked.
    pub fn questions(&self) -> Vec<Question> {
        self.questions.clone()
    }

    /// Returns what the figures leave out, in the order recorded.
    pub fn left_out(&self) -> Vec<String> {
        self.left.clone()
    }
}
