// Package chunk 做文件偏移与块号的纯映射、切分与校验和。
package chunk

import "hash/fnv"

const DefaultBlockSize = 512 << 10

type ChunkID struct {
	File  string
	Index uint64
}

func ValidBlockSize(n int) bool {
	switch n {
	case 256 << 10, 512 << 10, 1 << 20, 4 << 20:
		return true
	}
	return false
}

func Count(sizeBytes int64, blockSize int) int {
	if sizeBytes <= 0 {
		return 0
	}
	return int((sizeBytes + int64(blockSize) - 1) / int64(blockSize))
}

func Split(sizeBytes int64, blockSize int) []ChunkID {
	n := Count(sizeBytes, blockSize)
	out := make([]ChunkID, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, ChunkID{Index: uint64(i)})
	}
	return out
}

func IndexAt(offset int64, blockSize int) uint64 {
	return uint64(offset / int64(blockSize))
}

func RangeOf(index uint64, blockSize int) (int64, int) {
	return int64(index) * int64(blockSize), blockSize
}

func Checksum(b []byte) uint64 {
	h := fnv.New64a()
	_, _ = h.Write(b)
	return h.Sum64()
}
