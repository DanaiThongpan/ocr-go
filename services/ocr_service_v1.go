package services

import (
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"ocr-go/models"

	"github.com/otiai10/gosseract/v2"
)

// =========================================================
// V1: ProcessPDF และ ExtractGrades (ตัวดั้งเดิม)
// =========================================================
func ProcessPDF(pdfPath string) (string, []models.SubjectGrade, int, error) {
	outputDir := os.TempDir()
	outputPrefix := filepath.Join(outputDir, "go-ocr-img")

	cmd := exec.Command("pdftoppm", "-png", pdfPath, outputPrefix)
	if err := cmd.Run(); err != nil {
		return "", nil, 0, fmt.Errorf("แปลง PDF ไม่สำเร็จ: %v", err)
	}

	matches, err := filepath.Glob(fmt.Sprintf("%s-*.png", outputPrefix))
	if err != nil || len(matches) == 0 {
		return "", nil, 0, fmt.Errorf("ไม่พบไฟล์รูปภาพที่ถูกแปลงจาก PDF")
	}

	defer func() {
		for _, m := range matches {
			os.Remove(m)
		}
	}()

	client := gosseract.NewClient()
	defer client.Close()
	client.SetLanguage("tha", "eng")

	var fullTextBuilder strings.Builder
	for _, imagePath := range matches {
		if client.SetImage(imagePath) == nil {
			if text, err := client.Text(); err == nil {
				fullTextBuilder.WriteString(text)
				fullTextBuilder.WriteString("\n\n---PAGE---\n\n")
			}
		}
	}

	fullText := fullTextBuilder.String()
	subjects := ExtractGrades(fullText)

	return fullText, subjects, len(matches), nil
}

func ExtractGrades(text string) []models.SubjectGrade {
	results := []models.SubjectGrade{}
	lines := strings.Split(text, "\n")
	foundSubjects := make(map[string]bool)

	searchTargets := []struct {
		StandardName string
		Keywords     []string
	}{
		{"ภาษาไทย", []string{"ภาษาไทย"}},
		{"คณิตศาสตร์", []string{"คณิตศาสตร์"}},
		{"วิทยาศาสตร์", []string{"วิทยาศาสตร์และเทคโนโลยี", "วิทยาศาสตร์"}},
		{"สังคมศึกษา ศาสนา และวัฒนธรรม", []string{"สังคมศึกษา ศาสนา และวัฒนธรรม", "สังคมศึกษา"}},
		{"สุขศึกษาและพลศึกษา", []string{"สุขศึกษาและพลศึกษา", "สุขศึกษา"}},
		{"ศิลปะ", []string{"ศิลปะ"}},
		{"การงานอาชีพและเทคโนโลยี", []string{"การงานอาชีพและเทคโนโลยี", "การงานอาชีพ"}},
		{"ภาษาต่างประเทศ", []string{"ภาษาต่างประเทศ", "ภาษาอังกฤษ"}},
	}

	numberRegex := regexp.MustCompile(`([0-9]+(?:\.[0-9]+)?)`)

	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			continue
		}

		for _, target := range searchTargets {
			if foundSubjects[target.StandardName] {
				continue
			}

			var foundKeyword string
			var idx int = -1
			for _, kw := range target.Keywords {
				idx = strings.Index(line, kw)
				if idx != -1 {
					foundKeyword = kw
					break
				}
			}

			if idx != -1 {
				afterText := line[idx+len(foundKeyword):]
				afterText = regexp.MustCompile(`[^0-9\.\s]`).ReplaceAllString(afterText, " ")
				matches := numberRegex.FindAllString(afterText, -1)

				if len(matches) >= 2 {
					creditStr, gpaStr := matches[0], matches[1]
					credit, errCredit := strconv.ParseFloat(creditStr, 64)
					gpa, errGpa := strconv.ParseFloat(gpaStr, 64)

					if errCredit != nil || errGpa != nil {
						continue
					}

					if credit >= 10 && !strings.Contains(creditStr, ".") {
						if credit >= 100 {
							credit = credit / 10.0
						} else {
							credit = credit / 10.0
						}
					}

					if gpa >= 10 && !strings.Contains(gpaStr, ".") {
						gpa = gpa / 100.0
					}

					gpa = math.Round(gpa*100) / 100

					if gpa <= 4.0 && credit > 0 {
						results = append(results, models.SubjectGrade{
							Name:    target.StandardName,
							Credits: credit,
							GPA:     gpa,
						})
						foundSubjects[target.StandardName] = true
					}
				}
			}
		}
	}

	for i, j := 0, len(results)-1; i < j; i, j = i+1, j-1 {
		results[i], results[j] = results[j], results[i]
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