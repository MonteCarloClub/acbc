package chainhash

import (
	"encoding/hex"
	"fmt"
)

// HashSize of array used to store hashes.  See Hash.
const HashSize = 32

// Hash is used in several of the bitcoin messages and common structures.  It
// typically represents the double sha256 of data.
type Hash [HashSize]byte

// MaxHashStringSize is the maximum length of a Hash hash string.
// re：一个字节8位，在ascii码中，一个字节8位（0-127）表示一个字符，用16进制表示这个字符比如5A代表字符Z
//    所以hash值若为ZZ，用十六进制编码就是5A5A，即十六进制编码的hash值的长度是解码后的两倍
const MaxHashStringSize = HashSize * 2

// ErrHashStrSize describes an error that indicates the caller specified a hash
// string that has too many characters.
var ErrHashStrSize = fmt.Errorf("max hash string length is %v bytes", MaxHashStringSize)

// NewHashFromStr creates a Hash from a hash string.  The string should be
// the hexadecimal string of a byte-reversed hash, but any missing characters
// result in zero padding at the end of the Hash.
func NewHashFromStr(hash string) (*Hash, error) {
	ret := new(Hash)
	err := Decode(ret, hash)
	if err != nil {
		return nil, err
	}
	return ret, nil
}

// Decode decodes the byte-reversed hexadecimal string encoding of a Hash to a
// destination.
// 将翻转过的十六进制编码的hash字符串（大端）解码为byte数组（小端）（这个hash是双重sha256）
func Decode(dst *Hash, src string) error {
	// Return error if hash string is too long.
	if len(src) > MaxHashStringSize {
		return ErrHashStrSize
	}

	// Hex decoder expects the hash to be a multiple of two.  When not, pad
	// with a leading zero.
	var srcBytes []byte
	if len(src)%2 == 0 {
		srcBytes = []byte(src)
	} else {
		srcBytes = make([]byte, 1+len(src))
		srcBytes[0] = '0'
		copy(srcBytes[1:], src)
	}
	// re: Hashes以小端的形式（byte数组）进行hash运算，以大端形式（Hash值）展示给人看。
	//     所以src是大端形式的16进制的字符串，内部定义的byte数组是小端形式
	// Hex decode the source bytes to a temporary destination.
	var reversedHash Hash
	_, err := hex.Decode(reversedHash[HashSize-hex.DecodedLen(len(srcBytes)):], srcBytes)
	if err != nil {
		return err
	}

	// Reverse copy from the temporary hash to destination.  Because the
	// temporary was zeroed, the written result will be correctly padded.
	// re：翻转reversedHash
	for i, b := range reversedHash[:HashSize/2] {
		dst[i], dst[HashSize-1-i] = reversedHash[HashSize-1-i], b
	}

	return nil
}

// String returns the Hash as the hexadecimal string of the byte-reversed
// hash.
func (hash Hash) String() string {
	for i := 0; i < HashSize/2; i++ {
		hash[i], hash[HashSize-1-i] = hash[HashSize-1-i], hash[i]
	}
	return hex.EncodeToString(hash[:])
}
