package platform

// Truncate limits s to maxLen bytes and appends an ellipsis when shortened.
func Truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
