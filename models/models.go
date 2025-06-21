package models

func (session *Session) CanApproveQuestions() bool {
	return session.Role == "moderator" || session.Role == "admin"
}

func (session *Session) CanManageUsers() bool {
	return session.Role == "admin"
}

func (session *Session) CanEditQuestion(question *Question) bool {
	// Admins and moderators can edit any question
	if session.Role == "admin" || session.Role == "moderator" {
		return true
	}
	// Users can only edit their own pending questions
	return session.UserID == question.CreatedBy && question.Status == "pending"
}
