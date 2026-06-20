package store

import "strings"

// BM25DocumentContent returns the text indexed into the lexical backend for a
// chunk. File path is included because direct path lookup is a first-class
// search job, while duplicate path prefixes are avoided for chunkers that
// already include the path in their searchable content.
func BM25DocumentContent(filePath, content string) string {
	filePath = strings.TrimSpace(filePath)
	if filePath == "" || strings.Contains(content, filePath) {
		return content
	}
	return "File path: " + filePath + "\n" + content
}
