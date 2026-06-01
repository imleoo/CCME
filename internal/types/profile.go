package types

import "time"

// UserProfile is the structured long-term portrait of a user. It lives
// alongside the cascade and is updated incrementally by the application.
type UserProfile struct {
	UserID             string              `json:"userId" yaml:"userId"`
	DisplayName        string              `json:"displayName,omitempty" yaml:"displayName,omitempty"`
	Tags               []string            `json:"tags,omitempty" yaml:"tags,omitempty"`
	Facts              map[string]any      `json:"facts,omitempty" yaml:"facts,omitempty"`
	Patterns           []ProfilePattern    `json:"patterns,omitempty" yaml:"patterns,omitempty"`
	Preferences        *ProfilePreferences `json:"preferences,omitempty" yaml:"preferences,omitempty"`
	ActivePlan         *ProfileActivePlan  `json:"activePlan,omitempty" yaml:"activePlan,omitempty"`
	CommunicationStyle string              `json:"communicationStyle,omitempty" yaml:"communicationStyle,omitempty"`
	CreatedAt          time.Time           `json:"createdAt" yaml:"createdAt"`
	UpdatedAt          time.Time           `json:"updatedAt" yaml:"updatedAt"`
}

// ProfilePattern is a recurring behavioural/cognitive pattern observed about
// the user. Confidence + evidence let downstream consumers reason about how
// strongly to act on it.
type ProfilePattern struct {
	ID         string    `json:"id" yaml:"id"`
	Name       string    `json:"name" yaml:"name"`
	Evidence   []string  `json:"evidence,omitempty" yaml:"evidence,omitempty"`
	Confidence float64   `json:"confidence" yaml:"confidence"`
	LastSeen   time.Time `json:"lastSeen" yaml:"lastSeen"`
}

// ProfilePreferences captures the user's preferred and rejected interaction styles.
type ProfilePreferences struct {
	Tone                 string   `json:"tone,omitempty" yaml:"tone,omitempty"`
	RejectedAdviceStyles []string `json:"rejectedAdviceStyles,omitempty" yaml:"rejectedAdviceStyles,omitempty"`
}

// ProfileActivePlan tracks the user's currently-active improvement plan.
type ProfileActivePlan struct {
	Goal        string `json:"goal,omitempty" yaml:"goal,omitempty"`
	CurrentStep string `json:"currentStep,omitempty" yaml:"currentStep,omitempty"`
	LastAction  string `json:"lastAction,omitempty" yaml:"lastAction,omitempty"`
	Status      string `json:"status,omitempty" yaml:"status,omitempty"` // in_progress | completed | abandoned
}
