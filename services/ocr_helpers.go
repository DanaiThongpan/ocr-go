package services

// =========================================================
// Helper Functions (ใช้งานร่วมกันทุก Version)
// =========================================================

func floatPtr(v float64) *float64 {
	return &v
}

func levenshtein(a, b string) int {
	aRunes, bRunes := []rune(a), []rune(b)
	n, m := len(aRunes), len(bRunes)
	if n == 0 {
		return m
	}
	if m == 0 {
		return n
	}

	d := make([][]int, n+1)
	for i := range d {
		d[i] = make([]int, m+1)
		d[i][0] = i
	}
	for j := 0; j <= m; j++ {
		d[0][j] = j
	}

	for i := 1; i <= n; i++ {
		for j := 1; j <= m; j++ {
			cost := 1
			if aRunes[i-1] == bRunes[j-1] {
				cost = 0
			}
			minVal := d[i-1][j] + 1
			if d[i][j-1]+1 < minVal {
				minVal = d[i][j-1] + 1
			}
			if d[i-1][j-1]+cost < minVal {
				minVal = d[i-1][j-1] + cost
			}
			d[i][j] = minVal
		}
	}
	return d[n][m]
}

func similarity(a, b string) float64 {
	aRunes, bRunes := []rune(a), []rune(b)
	maxLen := len(aRunes)
	if len(bRunes) > maxLen {
		maxLen = len(bRunes)
	}
	if maxLen == 0 {
		return 1.0
	}
	return 1.0 - (float64(levenshtein(a, b)) / float64(maxLen))
}