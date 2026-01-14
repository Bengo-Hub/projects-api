package tender

import "errors"

// Service errors.
var (
	ErrNotFound          = errors.New("tender not found")
	ErrDocumentNotFound  = errors.New("tender document not found")
	ErrCommitteeNotFound = errors.New("tender committee not found")
	ErrMemberNotFound    = errors.New("committee member not found")
	ErrMeetingNotFound   = errors.New("tender meeting not found")
	ErrSectionNotFound   = errors.New("tender section not found")
	ErrSubmissionNotFound = errors.New("tender submission not found")
	ErrEvaluationNotFound = errors.New("tender evaluation not found")
	ErrDuplicateMember    = errors.New("user is already a committee member")
	ErrInvalidStatus      = errors.New("invalid status transition")
	ErrDeadlinePassed     = errors.New("tender deadline has passed")
)
