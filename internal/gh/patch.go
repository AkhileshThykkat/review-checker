package gh

import (
	"regexp"
	"strconv"
	"strings"
)

// hunkHeaderRe matches "@@ -a,b +c,d @@" (counts optional).
var hunkHeaderRe = regexp.MustCompile(`^@@ -\d+(?:,\d+)? \+(\d+)(?:,\d+)? @@`)

// BuildPositionMap parses a unified-diff patch (the GitHub "patch" field for
// one file) and returns a map from new-file line number to diff position.
//
// GitHub review comments attach to a "position": the number of lines below
// the file's FIRST "@@" hunk header. The line directly under that header is
// position 1, and the count keeps increasing through later hunk headers.
// Only lines present on the new side of the diff (added "+" and context " ")
// get an entry; deleted "-" lines advance the position but map to no new
// line. Lines outside every hunk have no entry — comments pointing there
// must be dropped, GitHub rejects them.
func BuildPositionMap(patch string) map[int]int {
	positions := make(map[int]int)
	if patch == "" {
		return positions
	}

	position := 0
	newLine := 0
	seenFirstHunk := false

	for _, line := range strings.Split(patch, "\n") {
		if m := hunkHeaderRe.FindStringSubmatch(line); m != nil {
			start, _ := strconv.Atoi(m[1])
			newLine = start - 1
			if seenFirstHunk {
				position++
			}
			seenFirstHunk = true
			continue
		}
		if !seenFirstHunk {
			continue
		}

		position++
		switch {
		case strings.HasPrefix(line, "+"):
			newLine++
			positions[newLine] = position
		case strings.HasPrefix(line, "-"):
			// old-side line: advances position, no new-file line
		case strings.HasPrefix(line, `\`):
			// "\ No newline at end of file": advances position only
		default:
			// context line (leading space, or empty context line)
			newLine++
			positions[newLine] = position
		}
	}

	return positions
}
