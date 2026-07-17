package history

import (
	"bytes"
	"fmt"
	"os"
)

const scanChunkSize = 64 * 1024 // 64 KB per backward read

// scanTurnsBackward reads the file backward from startOffset, counting renderable
// user messages until targetTurns is reached or the file beginning is hit.
//
// startOffset must be at a complete-line boundary (EOF or a cursor-derived byte offset).
//
// Returns:
//   - The byte offset of the first (oldest) counted turn, or 0 if none found.
//   - The number of renderable turns actually found (may be < targetTurns at EOF).
//   - An error only for unexpected I/O failures.
func scanTurnsBackward(f *os.File, fileSize int64, startOffset int64, targetTurns int, agent string) (newOffset int64, turnsFound int, err error) {
	if startOffset <= 0 || targetTurns <= 0 {
		return 0, 0, nil
	}

	pos := startOffset
	var leftover []byte       // partial first line carried from previous chunk
	var firstTurnOffset int64 = -1

	for pos > 0 && turnsFound < targetTurns {
		chunkSize := int64(scanChunkSize)
		if pos < chunkSize {
			chunkSize = pos
		}
		chunkStart := pos - chunkSize

		buf := make([]byte, chunkSize)
		n, readErr := f.ReadAt(buf, chunkStart)
		if readErr != nil && readErr != os.ErrClosed {
			// os.ErrClosed is possible if the file was closed underneath us; treat as EOF
			if !isEOFLike(readErr) {
				return 0, turnsFound, fmt.Errorf("read at offset %d: %w", chunkStart, readErr)
			}
		}
		buf = buf[:n]

		lines := bytes.Split(buf, []byte{'\n'})

		// Attach leftover from the previous (newer) chunk to the last line of this chunk.
		// The "last" line is the one closest to pos (most recent in the file).
		if len(leftover) > 0 && len(lines) > 0 {
			lines[len(lines)-1] = append(lines[len(lines)-1], leftover...)
			leftover = nil
		}

		// If chunkStart > 0, the very first line in lines[] may be partial
		// (we started reading mid-line). Carry it into the next iteration.
		startIdx := 0
		if chunkStart > 0 && len(lines) > 0 {
			leftover = lines[0]
			startIdx = 1
		}

		// Compute byte offset for each line so we can return the correct cursor offset.
		type lineInfo struct {
			data   []byte
			offset int64
		}
		var lineInfos []lineInfo
		offset := chunkStart
		for i := startIdx; i < len(lines); i++ {
			lineInfos = append(lineInfos, lineInfo{lines[i], offset})
			offset += int64(len(lines[i])) + 1 // +1 for '\n' delimiter
		}

		// Process in reverse (most recent first)
		for i := len(lineInfos) - 1; i >= 0 && turnsFound < targetTurns; i-- {
			li := lineInfos[i]
			if len(bytes.TrimSpace(li.data)) == 0 {
				continue
			}
			if isRenderableUserMessage(li.data, agent) {
				turnsFound++
				firstTurnOffset = li.offset
			}
		}

		pos = chunkStart
	}

	// If leftover was never attached (file ended exactly at line boundary
	// before the final chunk), process it now.
	if len(leftover) > 0 && turnsFound < targetTurns {
		if isRenderableUserMessage(leftover, agent) {
			turnsFound++
			firstTurnOffset = 0
		}
	}

	return firstTurnOffset, turnsFound, nil
}

func isEOFLike(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return msg == "EOF" || msg == "unexpected EOF"
}
