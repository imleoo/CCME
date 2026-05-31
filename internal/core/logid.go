package core

import "fmt"

// formatLogID builds a stable id from timestamp + sequence.
func formatLogID(ts int64, seq int) string {
	return fmt.Sprintf("log_%d_%06d", ts, seq)
}
