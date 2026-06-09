package auditor

type Entry struct {
	Seq       uint64
	PrevHash  [32]byte
	Timestamp int64
	Action    string
	ActorID   string
	SessionID string
	Payload   []byte
	HMAC      [32]byte
}