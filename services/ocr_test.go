package services

import (
	"testing"
)

func TestExtractStudentInfo(t *testing.T) {
	// ชุดจำลอง Ground Truth & OCR Raw Text 
	// ที่สกัดมาจากไฟล์ ปพ.1 ที่เคยมีปัญหา (มีชื่อบิดา/มารดา แทรกอยู่)
	mockRawOCRText := `
	โรงเรียนมัธยมแห่งหนึ่ง
	ชื่อบิดา นาย สมพงษ์ วันทา
	ชื่อมารดา นาง สมศรี วันทา
	ชื่อผู้ปกครอง นาย สมพงษ์ วันทา
	ชื่อ นาย ณัฐวัต วันทา
	เลขประจำตัวประชาชน 1-2345-67890-12-3
	ผลการเรียนเฉลี่ยสะสม GPAX 3.50
	`

	// เรียกใช้งานฟังก์ชันที่ถูกปรับจูนแล้ว
	prefix, firstName, lastName, nationalID := ExtractStudentInfo(mockRawOCRText)
	gpax := ExtractGPAX(mockRawOCRText)

	// Ground Truth (Expected Results)
	expectedPrefix := "นาย"
	expectedFirstName := "ณัฐวัต"
	expectedLastName := "วันทา"
	expectedID := "1-2345-67890-12-3"
	expectedGPAX := 3.50

	// Assertions (ทดสอบว่ารอดจากขยะ "บิดา/มารดา" จริงหรือไม่)
	if prefix != expectedPrefix {
		t.Errorf("❌ ผิดพลาด: คำนำหน้า หวังผล %s แต่ได้ %s", expectedPrefix, prefix)
	}
	if firstName != expectedFirstName {
		t.Errorf("❌ ผิดพลาด: ชื่อจริง หวังผล %s แต่ได้ %s", expectedFirstName, firstName)
	}
	if lastName != expectedLastName {
		t.Errorf("❌ ผิดพลาด: นามสกุล หวังผล %s แต่ได้ %s", expectedLastName, lastName)
	}
	if nationalID != expectedID {
		t.Errorf("❌ ผิดพลาด: เลขบัตร หวังผล %s แต่ได้ %s", expectedID, nationalID)
	}
	if gpax == nil || *gpax != expectedGPAX {
		t.Errorf("❌ ผิดพลาด: GPAX หวังผล %f แต่ได้ %v", expectedGPAX, gpax)
	}
}
