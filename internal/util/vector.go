// Package util provides shared math and time helpers.
package util

import (
	"encoding/binary"
	"errors"
	"math"
	"math/rand"
)

// CosineSimilarity returns the cosine similarity between two equally-sized vectors.
// Returns 0 if either vector has zero magnitude.
func CosineSimilarity(a, b []float64) float64 {
	if len(a) != len(b) {
		return 0
	}
	var dot, na, nb float64
	for i := range a {
		dot += a[i] * b[i]
		na += a[i] * a[i]
		nb += b[i] * b[i]
	}
	denom := math.Sqrt(na) * math.Sqrt(nb)
	if denom == 0 {
		return 0
	}
	return dot / denom
}

// DotProduct assumes both inputs are unit-normalised and returns their dot product.
func DotProduct(a, b []float64) float64 {
	if len(a) != len(b) {
		return 0
	}
	var dot float64
	for i := range a {
		dot += a[i] * b[i]
	}
	return dot
}

// Normalize returns a copy of v scaled to unit length. Zero vectors are returned unchanged.
func Normalize(v []float64) []float64 {
	var norm float64
	for _, x := range v {
		norm += x * x
	}
	norm = math.Sqrt(norm)
	out := make([]float64, len(v))
	if norm == 0 {
		copy(out, v)
		return out
	}
	for i, x := range v {
		out[i] = x / norm
	}
	return out
}

// RandomVector returns a unit-normalised random vector of the given dimension.
func RandomVector(dim int, r *rand.Rand) []float64 {
	if r == nil {
		r = rand.New(rand.NewSource(1))
	}
	v := make([]float64, dim)
	for i := range v {
		v[i] = r.Float64()*2 - 1
	}
	return Normalize(v)
}

// EncodeVector serialises a float64 slice into a deterministic byte blob (little-endian).
func EncodeVector(v []float64) []byte {
	buf := make([]byte, 8*len(v))
	for i, x := range v {
		binary.LittleEndian.PutUint64(buf[i*8:], math.Float64bits(x))
	}
	return buf
}

// DecodeVector reverses EncodeVector.
func DecodeVector(b []byte) ([]float64, error) {
	if len(b)%8 != 0 {
		return nil, errors.New("vector blob length must be a multiple of 8")
	}
	v := make([]float64, len(b)/8)
	for i := range v {
		v[i] = math.Float64frombits(binary.LittleEndian.Uint64(b[i*8:]))
	}
	return v, nil
}
