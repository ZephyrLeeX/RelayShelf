package messages

import (
	"crypto/sha256"
	"encoding/binary"
	"sort"

	"github.com/google/uuid"
)

const (
	OperationCreate     = "message.create"
	OperationDirectSend = "message.direct-send"
	OperationForward    = "message.forward"
)

func validIdempotencyKey(key string) bool {
	if len(key) < 1 || len(key) > 128 {
		return false
	}
	for i := range len(key) {
		if key[i] < 0x20 || key[i] > 0x7e {
			return false
		}
	}
	return true
}

type canonicalHash struct{ b []byte }

func (h *canonicalHash) bytes(value []byte) {
	var size [8]byte
	binary.BigEndian.PutUint64(size[:], uint64(len(value)))
	h.b = append(h.b, size[:]...)
	h.b = append(h.b, value...)
}
func (h *canonicalHash) string(value string)  { h.bytes([]byte(value)) }
func (h *canonicalHash) uuid(value uuid.UUID) { h.bytes(value[:]) }
func (h *canonicalHash) boolean(value bool) {
	if value {
		h.b = append(h.b, 1)
	} else {
		h.b = append(h.b, 0)
	}
}
func (h *canonicalHash) sum() [32]byte { return sha256.Sum256(h.b) }

func hashCreate(c CreateCommand) [32]byte {
	h := canonicalHash{}
	h.string(c.Body)
	h.string(c.BodyFormat)
	h.string(c.Lifecycle)
	h.boolean(c.Sensitive)
	ids := append([]uuid.UUID(nil), c.TagIDs...)
	sort.Slice(ids, func(i, j int) bool { return ids[i].String() < ids[j].String() })
	for _, value := range ids {
		h.uuid(value)
	}
	return h.sum()
}
func hashDirect(c DirectSendCommand) [32]byte {
	h := canonicalHash{}
	h.uuid(c.RecipientID)
	h.string(c.Body)
	h.string(c.BodyFormat)
	h.boolean(c.Sensitive)
	return h.sum()
}
func hashForward(c ForwardCommand) [32]byte {
	h := canonicalHash{}
	h.uuid(c.SourceID)
	h.bytes(binary.BigEndian.AppendUint64(nil, uint64(c.ExpectedVersion)))
	h.uuid(c.RecipientID)
	return h.sum()
}
