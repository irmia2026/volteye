package wechatdb

const (
	PageSize    = 4096
	SaltSize    = 16
	IVSize      = 16
	HMACSize    = 64
	ReserveSize = IVSize + HMACSize
	KeySize     = 32
	RoundCount  = 256000
)

var SQLiteHeader = []byte("SQLite format 3\x00")
