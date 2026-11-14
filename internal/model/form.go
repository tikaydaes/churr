package model

// FormOption is one option of a choice field.
type FormOption struct {
	ID        string    `json:"id"`
	Label     string    `json:"label"`
	Max       int       `json:"max,omitempty"`
	AddedByID AccountID `json:"added_by_id,omitempty"`
}

// FormField is one field of a form (docs/MESSAGE-FORMAT.md section 7).
type FormField struct {
	ID          string       `json:"id"`
	Type        string       `json:"type"`
	Label       string       `json:"label"`
	Help        string       `json:"help,omitempty"`
	Required    bool         `json:"required,omitempty"`
	Multiple    bool         `json:"multiple,omitempty"`
	Options     []FormOption `json:"options,omitempty"`
	Min         any          `json:"min,omitempty"`
	Max         any          `json:"max,omitempty"`
	Step        float64      `json:"step,omitempty"`
	Placeholder string       `json:"placeholder,omitempty"`
	MaxLength   int          `json:"max_length,omitempty"`
}

// FormButton is a button on a form.
type FormButton struct {
	ID          string  `json:"id"`
	Label       string  `json:"label"`
	Style       string  `json:"style,omitempty"` // default | primary | danger
	GroupID     GroupID `json:"group_id,omitempty"`
	GroupAction string  `json:"group_action,omitempty"` // join | leave
}

// FormAction is what the server does with an answer.
type FormAction struct {
	Type       string  `json:"type"` // tally | collect | thread | channel | group | bot
	NamePrefix string  `json:"name_prefix,omitempty"`
	GroupID    GroupID `json:"group_id,omitempty"`
	Level      Level   `json:"level,omitempty"`
}

// FormInput is a form as a client sends it when posting a message.
type FormInput struct {
	Preset            string       `json:"preset,omitempty"`
	Title             string       `json:"title,omitempty"`
	Fields            []FormField  `json:"fields"`
	Buttons           []FormButton `json:"buttons,omitempty"`
	Action            FormAction   `json:"action"`
	Anonymous         bool         `json:"anonymous,omitempty"`
	Results           string       `json:"results,omitempty"` // live | after_answer | after_close
	AllowAddedOptions bool         `json:"allow_added_options,omitempty"`
	AnswererGroupID   GroupID      `json:"answerer_group_id,omitempty"`
	OneAnswer         *bool        `json:"one_answer,omitempty"`
	ClosesAt          Time         `json:"closes_at,omitempty"`
}

// Form is how a message asks for input, as a reader sees it (schema Form).
// Tally, Voters and MyAnswer are filled per reader by the forms package.
type Form struct {
	ID                FormID                            `json:"id"`
	Preset            string                            `json:"preset,omitempty"`
	Title             string                            `json:"title,omitempty"`
	Fields            []FormField                       `json:"fields"`
	Buttons           []FormButton                      `json:"buttons,omitempty"`
	Action            FormAction                        `json:"action"`
	Anonymous         bool                              `json:"anonymous,omitempty"`
	Results           string                            `json:"results,omitempty"`
	AllowAddedOptions bool                              `json:"allow_added_options,omitempty"`
	AnswererGroupID   GroupID                           `json:"answerer_group_id,omitempty"`
	OneAnswer         *bool                             `json:"one_answer,omitempty"`
	ClosesAt          Time                              `json:"closes_at,omitempty"`
	ClosedAt          Time                              `json:"closed_at,omitempty"`
	AnswerCount       int                               `json:"answer_count"`
	Tally             map[string]map[string]int         `json:"tally,omitempty"`
	Voters            map[string]map[string][]AccountID `json:"voters,omitempty"`
	MyAnswer          map[string]any                    `json:"my_answer,omitempty"`
}
