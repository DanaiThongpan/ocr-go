package models

// ==========================================
// สำหรับ V1 และ V2 (คงไว้เหมือนเดิมเป๊ะ 100%)
// ==========================================
type SubjectGrade struct {
	Name    string  `json:"name"`
	Credits float64 `json:"credits"`
	GPA     float64 `json:"gpa"`
}

// ==========================================
// สำหรับ V3 (ใช้ Pointer เพื่อให้เป็น null ได้)
// ==========================================
type SubjectGradeV3 struct {
	Name    string   `json:"name"`
	Credits *float64 `json:"credits"`
	GPA     *float64 `json:"gpa"`
}

type OCRResponseData struct {
	Filename    string           `json:"filename"`
	Title       string           `json:"title"`
	FirstName   string           `json:"first_name"`
	LastName    string           `json:"last_name"`
	NationalID  string           `json:"national_id"`
	Pages       int              `json:"pages"`
	Subjects    []SubjectGradeV3 `json:"subjects"`
	RawText     string           `json:"raw_text"`
	DebugImages []string         `json:"debug_images,omitempty"`
}

type OCRResponse struct {
	Success bool            `json:"success"`
	Version string          `json:"version"`
	Data    OCRResponseData `json:"data"`
}