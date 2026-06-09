package auditor

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"time"
)

func (e *Entry) ComputeHMAC(key []byte) [32]byte {
	mac := hmac.New(sha256.New, key)
	seq := make([]byte, 8)
	binary.BigEndian.PutUint64(seq, e.Seq)
	mac.Write(seq)
	mac.Write(e.PrevHash[:])
	ts := make([]byte, 8)
	binary.BigEndian.PutUint64(ts, uint64(e.Timestamp))
	mac.Write(ts)
	mac.Write([]byte(e.Action))
	mac.Write([]byte(e.ActorID))
	mac.Write([]byte(e.SessionID))
	mac.Write(e.Payload)
	var h [32]byte
	copy(h[:], mac.Sum(nil))
	return h
}

func HashOf(e *Entry) [32]byte {
	h := sha256.Sum256(e.Marshal())
	return h
}

func (e *Entry) Marshal() []byte {
	seq := make([]byte, 8)
	binary.BigEndian.PutUint64(seq, e.Seq)
	ts := make([]byte, 8)
	binary.BigEndian.PutUint64(ts, uint64(e.Timestamp))
	out := make([]byte, 0, 8+32+8+len(e.Action)+len(e.ActorID)+len(e.SessionID)+len(e.Payload)+32)
	out = append(out, seq...)
	out = append(out, e.PrevHash[:]...)
	out = append(out, ts...)
	out = append(out, e.Action...)
	out = append(out, '\x00')
	out = append(out, e.ActorID...)
	out = append(out, '\x00')
	out = append(out, e.SessionID...)
	out = append(out, '\x00')
	out = append(out, e.Payload...)
	out = append(out, e.HMAC[:]...)
	return out
}

func NewEntry(prev [32]byte, seq uint64, action, actorID, sessionID string, payload []byte, hmacKey []byte) Entry {
	e := Entry{
		Seq:       seq,
		PrevHash:  prev,
		Timestamp: time.Now().UnixNano(),
		Action:    action,
		ActorID:   actorID,
		SessionID: sessionID,
		Payload:   payload,
	}
	e.HMAC = e.ComputeHMAC(hmacKey)
	return e
}

func VerifyEntry(e *Entry, key []byte) bool {
	expected := e.ComputeHMAC(key)
	return hmac.Equal(expected[:], e.HMAC[:])
}

func VerifyChain(entries []Entry, key []byte) int {
	for i := range entries {
		if !VerifyEntry(&entries[i], key) {
			return i
		}
		if i > 0 {
			expectedPrev := HashOf(&entries[i-1])
			if entries[i].PrevHash != expectedPrev {
				return i
			}
		}
	}
	return -1
}