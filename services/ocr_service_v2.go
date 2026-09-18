package services

import (
	"math"
	"regexp"
	"strconv"
	"strings"

	"ocr-go/models"
)

// =========================================================
// V2: Extract Grades (แก้ปัญหาครอบตัด + ตัวเลขไทย + เดาคำ > 80%)
// =========================================================
func ExtractGradesV2(text string) []models.SubjectGrade {
	results := []models.SubjectGrade{}

	reComma := regexp.MustCompile(`(\d),(\d)`)
	text = reComma.ReplaceAllString(text, "${1}.${2}")

	thaiDigits := []string{"๐", "๑", "๒", "๓", "๔", "๕", "๖", "๗", "๘", "๙"}
	arabicDigits := []string{"0", "1", "2", "3", "4", "5", "6", "7", "8", "9"}
	for i, td := range thaiDigits {
		text = strings.ReplaceAll(text, td, arabicDigits[i])
	}

	badChars := []string{"|", "]", "}", ")", "[", "(", "\"", "'"}
	for _, char := range badChars {
		text = strings.ReplaceAll(text, char, " ")
	}

	lines := strings.Split(text, "\n")
	foundSubjects := make(map[string]bool)
	seenInvalid := make(map[string]bool)

	searchTargets := []struct {
		StandardName string
		Keywords     []string
	}{
		{"ภาษาไทย", []string{"ภาษาไทย"}},
		{"คณิตศาสตร์", []string{"คณิตศาสตร์", "คณิต", "สฑิกรรย์"}},
		{"วิทยาศาสตร์และเทคโนโลยี", []string{"วิทยาศาสตร์และเทคโนโลยี", "วิทยาศาสตร์"}},
		{"สังคมศึกษา ศาสนา และวัฒนธรรม", []string{"สังคมศึกษา", "ศาสนาและวัฒนธรรม", "สังคม"}},
		{"สุขศึกษาและพลศึกษา", []string{"สุขศึกษาและพลศึกษา", "สุขศึกษา", "พลศึกษา"}},
		{"ศิลปะ", []string{"ศิลปะ"}},
		{"การงานอาชีพ", []string{"การงานอาชีพและเทคโนโลยี", "การงานอาชีพ", "การงาน"}},
		{"ภาษาต่างประเทศ", []string{"ภาษาต่างประเทศ", "ภาษาอังกฤษ", "ต่างประเทศ", "สหัณเร", "เสหัณเร", "ภะเห", "ะ"}},
		{"การศึกษาค้นคว้าด้วยตนเอง (IS)", []string{"การศึกษาค้นคว้า", "ด้วยตนเอง", "is"}},
	}

	numberRegex := regexp.MustCompile(`([0-9]+(?:\.[0-9]+)?)`)
	reThaiText := regexp.MustCompile(`[ก-๙a-zA-Z]+`)

	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			continue
		}

		lineNoSpaces := strings.ToLower(strings.ReplaceAll(line, " ", ""))
		textBlocks := reThaiText.FindAllString(line, -1)

		for _, target := range searchTargets {
			if foundSubjects[target.StandardName] {
				continue
			}

			found := false
			for _, kw := range target.Keywords {
				kwClean := strings.ToLower(strings.ReplaceAll(kw, " ", ""))

				if strings.Contains(lineNoSpaces, kwClean) {
					found = true
					break
				}
				for _, block := range textBlocks {
					if len(block) >= 3 && similarity(block, kwClean) >= 0.80 {
						found = true
						break
					}
				}
				if found {
					break
				}
			}

			if found {
				matches := numberRegex.FindAllString(line, -1)
				foundValid := false
				var finalCredit, finalGPA float64

				if len(matches) >= 2 {
					for j := len(matches) - 1; j >= 1; j-- {
						creditStr := matches[j-1]
						gpaStr := matches[j]

						if j == len(matches)-1 && (gpaStr == "0" || gpaStr == "0.0" || gpaStr == "0.00") && len(matches) >= 3 {
							continue
						}

						credit, err1 := strconv.ParseFloat(creditStr, 64)
						gpa, err2 := strconv.ParseFloat(gpaStr, 64)

						if err1 == nil && err2 == nil {
							if gpa > 4.0 && gpa <= 400 {
								if gpa >= 100 {
									gpa = gpa / 100.0
								} else if gpa >= 10 {
									gpa = gpa / 10.0
								}
							}

							if gpa > 4.0 {
								continue
							}

							if credit >= 10 && !strings.Contains(creditStr, ".") {
								if credit >= 100 {
									credit = credit / 10.0
								} else {
									credit = credit / 10.0
								}
							}

							gpa = math.Round(gpa*100) / 100

							if gpa <= 4.0 && credit > 0 {
								finalCredit = credit
								finalGPA = gpa
								foundValid = true
								break
							}
						}
					}
				}

				if foundValid {
					results = append(results, models.SubjectGrade{
						Name:    target.StandardName,
						Credits: finalCredit,
						GPA:     finalGPA,
					})
					foundSubjects[target.StandardName] = true
				} else {
					seenInvalid[target.StandardName] = true
				}
			}
		}
	}

	for _, target := range searchTargets {
		if seenInvalid[target.StandardName] && !foundSubjects[target.StandardName] {
			results = append(results, models.SubjectGrade{
				Name:    target.StandardName,
				Credits: 0.0,
				GPA:     0.0,
			})
			foundSubjects[target.StandardName] = true
		}
	}

	orderedResults := []models.SubjectGrade{}
	for _, target := range searchTargets {
		for _, r := range results {
			if r.Name == target.StandardName {
				orderedResults = append(orderedResults, r)
				break
			}
		}
	}

	return orderedResults
}