package notation

import (
	"crypto/sha256"
	"encoding/hex"
)

// Rev fingerprints canonical notation text. A write carries the rev it was based
// on, so if someone moved notes in Live in the meantime the fingerprint no longer
// matches and the write is refused instead of quietly overwriting their edit.
func Rev(canonical string) string {
	sum := sha256.Sum256([]byte(canonical))
	return hex.EncodeToString(sum[:])[:12]
}
