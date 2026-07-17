package history

import (
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"
)

// cursor identifies a position in a session file for pagination.
// It is opaque to clients; only the server encodes/decodes it.
type cursor struct {
	SessionID  string
	ByteOffset int64
	FileSize   int64 // snapshot file size at cursor creation time; used to detect truncation
}

// EncodeCursor produces an opaque cursor string.
func EncodeCursor(sessionID string, byteOffset, fileSize int64) string {
	payload := fmt.Sprintf("%s|%d|%d", sessionID, byteOffset, fileSize)
	return base64.RawURLEncoding.EncodeToString([]byte(payload))
}

// DecodeCursor parses an opaque cursor string.
func DecodeCursor(s string) (cursor, error) {
	decoded, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return cursor{}, fmt.Errorf("invalid cursor encoding")
	}
	parts := strings.SplitN(string(decoded), "|", 3)
	if len(parts) != 3 {
		return cursor{}, fmt.Errorf("invalid cursor format")
	}

	sessionID := parts[0]
	if sessionID == "" {
		return cursor{}, fmt.Errorf("empty session id in cursor")
	}

	byteOffset, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil || byteOffset < 0 {
		return cursor{}, fmt.Errorf("invalid byte offset in cursor")
	}

	fileSize, err := strconv.ParseInt(parts[2], 10, 64)
	if err != nil || fileSize < 0 {
		return cursor{}, fmt.Errorf("invalid file size in cursor")
	}

	return cursor{
		SessionID:  sessionID,
		ByteOffset: byteOffset,
		FileSize:   fileSize,
	}, nil
}
