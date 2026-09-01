package models

type SubjectGrade struct {
	Name    string  `json:"name"`
	Credits float64 `json:"credits"`
	GPA     float64 `json:"gpa"`
}